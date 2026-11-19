// Command quizroom is the Quizroom binary: `serve` runs the backend API,
// `runner` runs the Claude wrapper in the runner container, and `openapi`
// prints the API description the frontend's types are generated from.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"quizroom/internal/api"
	"quizroom/internal/chat"
	"quizroom/internal/quiz"
	"quizroom/internal/runner"
	"quizroom/internal/store"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: quizroom serve | runner | openapi")
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		runServe()
	case "runner":
		runRunner()
	case "openapi":
		b, err := api.OpenAPIJSON()
		if err != nil {
			log.Fatal(err)
		}
		_, _ = os.Stdout.Write(b)
	default:
		usage()
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envMap() map[string]string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

// listen serves h until SIGINT or SIGTERM, then shuts down within grace.
func listen(addr string, h http.Handler, grace time.Duration, onStop func()) {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		if onStop != nil {
			onStop()
		}
		sctx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Printf("listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func runRunner() {
	s := &runner.Server{Env: envMap()}
	listen(":"+getenv("PORT", fmt.Sprint(runner.Port)), s.Handler(), runner.StopGrace+10*time.Second, nil)
}

func runServe() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	st, err := store.Open(ctx, url)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	quizzes := &quiz.Service{St: st}
	bctx, bcancel := context.WithTimeout(context.Background(), 60*time.Second)
	if err := quizzes.BackfillRD(bctx); err != nil {
		log.Fatalf("backfilling rating deviations: %v", err)
	}
	bcancel()

	rc := &chat.Client{BaseURL: getenv("RUNNER_URL", "http://runner:9000")}
	worker := &chat.Worker{St: st, Runner: rc}
	wctx, stopWorker := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		worker.Run(wctx)
		close(stopped)
	}()

	h := api.NewHandler(api.Deps{Quiz: quizzes, Chat: &chat.Service{St: st}, Store: st, Runner: rc}, api.Hosts(os.Getenv("ALLOWED_HOSTS")))
	listen(":"+getenv("PORT", "8080"), h, 10*time.Second, stopWorker)
	select {
	case <-stopped:
	case <-time.After(15 * time.Second):
		log.Print("worker did not stop in time")
	}
}
