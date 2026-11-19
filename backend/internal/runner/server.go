package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Server is the runner's HTTP API. Env is the runner's own environment, which
// holds the credential; Claude is the binary name (default "claude"), looked
// up on Env["PATH"].
type Server struct {
	Env    map[string]string
	Claude string

	mu sync.Mutex // one call at a time
}

// Handler serves GET /health and POST /claude. It never logs request bodies.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /claude", s.claude)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) binary() string {
	if s.Claude != "" {
		return s.Claude
	}
	return "claude"
}

// lookPath resolves name on the given PATH, not the runner process's own.
func lookPath(name, pathEnv string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", errors.New(name + " not found on PATH")
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	auth := AuthMode(s.Env)
	if auth == "" {
		auth = "none"
	}
	writeJSON(w, http.StatusOK, Health{OK: true, Claude: s.version(r.Context()), Auth: auth})
}

func (s *Server) version(ctx context.Context) string {
	dir, err := os.MkdirTemp("", "probe-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir)
	path, err := lookPath(s.binary(), s.Env["PATH"])
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = ChildEnv(s.Env, dir, dir)
	cmd.Dir = dir
	out, _ := cmd.Output()
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line
}

func (s *Server) claude(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too_large"})
		return
	}
	req, err := ParseRequest(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request", "detail": err.Error()})
		return
	}
	auth := AuthMode(s.Env)
	if auth == "" {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "not_signed_in"})
		return
	}
	if !s.mu.TryLock() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "busy"})
		return
	}
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.run(r.Context(), req, auth == "api_key"))
}

// readTail returns the last MaxOutput bytes of a file.
func readTail(f *os.File) string {
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return ""
	}
	start := max(0, end-MaxOutput)
	buf := make([]byte, end-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	return string(buf)
}

func (s *Server) run(ctx context.Context, req Request, apiKey bool) Response {
	started := time.Now()
	fail := func(msg string) Response {
		return Response{ExitCode: 127, DurationMs: time.Since(started).Milliseconds(), Stderr: msg}
	}
	dir, err := os.MkdirTemp("", "claude-")
	if err != nil {
		return fail("failed to create a work directory: " + err.Error())
	}
	defer os.RemoveAll(dir)
	config, home, work := filepath.Join(dir, "config"), filepath.Join(dir, "home"), filepath.Join(dir, "work")
	for _, d := range []string{config, home, work} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return fail("failed to create a work directory: " + err.Error())
		}
	}
	path, err := lookPath(s.binary(), s.Env["PATH"])
	if err != nil {
		return fail("failed to start claude: " + err.Error())
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutS)*time.Second)
	defer cancel()
	argv := Argv(req, apiKey)
	cmd := exec.CommandContext(ctx, path, argv[1:]...)
	cmd.Env = ChildEnv(s.Env, config, home)
	cmd.Dir = work
	cmd.Stdin = strings.NewReader(req.Prompt)
	// Files, not pipes: a grandchild that keeps a pipe open would make Wait
	// block until WaitDelay even after claude itself has exited.
	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		return fail("failed to create an output file: " + err.Error())
	}
	defer stdout.Close()
	stderr, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		return fail("failed to create an output file: " + err.Error())
	}
	defer stderr.Close()
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT) }
	cmd.WaitDelay = StopGrace

	runErr := cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // reap stragglers in the group
	}
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	} else if runErr != nil {
		return fail("failed to start claude: " + runErr.Error())
	}
	if code == -1 || (code == 0 && ctx.Err() != nil) {
		code = 130 // stopped by a signal or the timeout
	}
	return Response{
		ExitCode:   code,
		DurationMs: time.Since(started).Milliseconds(),
		Stdout:     readTail(stdout),
		Stderr:     readTail(stderr),
	}
}
