// Package runner wraps `claude -p` for the runner container, the only place
// that holds the Claude credential. It is a Go port of the jobsearch app's
// runner/guard.py and runner/server.py: Claude gets no tools, a throwaway
// config directory per call, a scrubbed environment and exactly one credential.
package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	Port            = 9000
	DefaultTimeoutS = 600
	MaxTimeoutS     = 900
	MaxBody         = 2 << 20
	MaxPrompt       = 400_000
	MaxSystem       = 100_000
	MaxOutput       = 200 << 10
	StopGrace       = 30 * time.Second
)

// Models are the model aliases a request may ask for.
var Models = []string{"sonnet", "opus", "haiku"}

// KeepEnv are the only variables copied into claude's environment. The
// credential is added separately by ChildEnv, never copied wholesale.
var KeepEnv = []string{"PATH", "TZ", "DISABLE_UPDATES", "DISABLE_AUTOUPDATER"}

// Request is one call to Claude.
type Request struct {
	System   string          `json:"system"`
	Prompt   string          `json:"prompt"`
	Schema   json.RawMessage `json:"schema"`
	Model    string          `json:"model"`
	TimeoutS int             `json:"timeout_s"`
}

// Response is what the claude process did.
type Response struct {
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
}

// Health reports the CLI version and which credential is in use.
type Health struct {
	OK     bool   `json:"ok"`
	Claude string `json:"claude"`
	Auth   string `json:"auth"` // "api_key", "subscription" or "none"
}

// AuthMode is "api_key" when an API key is set (it wins), "subscription" for
// the plan token, and "" when neither is. Empty values count as unset, since
// Compose always defines both variables.
func AuthMode(env map[string]string) string {
	switch {
	case env["ANTHROPIC_API_KEY"] != "":
		return "api_key"
	case env["CLAUDE_CODE_OAUTH_TOKEN"] != "":
		return "subscription"
	}
	return ""
}

// ChildEnv builds claude's environment: the KeepEnv variables, exactly one
// credential, and a per-call config directory and home so nothing persists.
func ChildEnv(parent map[string]string, configDir, home string) []string {
	var env []string
	for _, k := range KeepEnv {
		if v, ok := parent[k]; ok {
			env = append(env, k+"="+v)
		}
	}
	switch AuthMode(parent) {
	case "api_key":
		env = append(env, "ANTHROPIC_API_KEY="+parent["ANTHROPIC_API_KEY"])
	case "subscription":
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+parent["CLAUDE_CODE_OAUTH_TOKEN"])
	}
	return append(env, "CLAUDE_CONFIG_DIR="+configDir, "HOME="+home)
}

// Argv is the claude command line: no tools, structured JSON output, no
// settings, skills, MCP servers or saved sessions. Never --bare, which ignores
// CLAUDE_CODE_OAUTH_TOKEN. With an API key, spending is capped per call.
func Argv(r Request, apiKey bool) []string {
	argv := []string{
		"claude", "-p", "--tools", "", "--model", r.Model, "--output-format", "json",
		"--json-schema", string(r.Schema), "--system-prompt", r.System, "--strict-mcp-config",
		"--disable-slash-commands", "--setting-sources", "", "--no-session-persistence",
	}
	if apiKey {
		argv = append(argv, "--max-budget-usd", "1")
	}
	return argv
}

// ParseRequest decodes and checks a /claude body.
func ParseRequest(body []byte) (Request, error) {
	var r Request
	if err := json.Unmarshal(body, &r); err != nil {
		return Request{}, errors.New("invalid JSON")
	}
	if strings.TrimSpace(r.Prompt) == "" || utf8.RuneCountInString(r.Prompt) > MaxPrompt {
		return Request{}, fmt.Errorf("prompt must be a non-empty string of at most %d characters", MaxPrompt)
	}
	if utf8.RuneCountInString(r.System) > MaxSystem {
		return Request{}, fmt.Errorf("system must be at most %d characters", MaxSystem)
	}
	if t := bytes.TrimSpace(r.Schema); len(t) == 0 || t[0] != '{' {
		return Request{}, errors.New("schema must be an object")
	}
	if !slices.Contains(Models, r.Model) {
		return Request{}, fmt.Errorf("model must be one of %v", Models)
	}
	switch {
	case r.TimeoutS < 0:
		return Request{}, errors.New("timeout_s must be positive")
	case r.TimeoutS == 0:
		r.TimeoutS = DefaultTimeoutS
	case r.TimeoutS > MaxTimeoutS:
		r.TimeoutS = MaxTimeoutS
	}
	return r, nil
}
