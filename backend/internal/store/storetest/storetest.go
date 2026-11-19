// Package storetest gives each test its own Postgres schema. Tests skip when
// TEST_DATABASE_URL is unset; `make test` always sets it.
package storetest

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"quizroom/internal/format"
	"quizroom/internal/mistakes"
	"quizroom/internal/store"
)

// Open returns a store on a fresh schema, migrated and seeded, plus the URL
// that reopens it. The schema is dropped when the test ends.
func Open(t *testing.T) (*store.Store, string) {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema := fmt.Sprintf("t_%d_%d", time.Now().UnixNano(), rand.IntN(1_000_000))
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("test database: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)

	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	url := base + sep + "search_path=" + schema
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		st.Close()
		ctx := context.Background()
		if conn, err := pgx.Connect(ctx, base); err == nil {
			_, _ = conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
			conn.Close(ctx)
		}
	})
	return st, url
}

// New is Open without the URL, plus the eleven Docker and Kubernetes mistakes
// in mistakes.json: active, due now and in that order, as older installs were
// seeded. Use Open for a store with no mistakes.
func New(t *testing.T) *store.Store {
	t.Helper()
	st, _ := Open(t)
	var ms []struct{ Topic, Subtopic, Belief, Correction string }
	if err := json.Unmarshal(mistakesJSON, &ms); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, m := range ms {
		if _, _, err := st.AddMistake(context.Background(), m.Topic, m.Subtopic, m.Belief, m.Correction, mistakes.Seed, now); err != nil {
			t.Fatalf("adding test mistake %q: %v", m.Belief, err)
		}
	}
	return st
}

//go:embed mistakes.json
var mistakesJSON []byte

var serial atomic.Int64

// AddQuestions stores n valid single-answer questions with distinct stems.
// Option 0 is correct.
func AddQuestions(t *testing.T, st *store.Store, topic, subtopic string, n, difficulty int) []store.Question {
	t.Helper()
	qs := make([]format.Question, n)
	for i := range qs {
		qs[i] = format.Question{
			Topic: topic, Subtopic: subtopic, Kind: format.Single, Difficulty: difficulty,
			Stem: fmt.Sprintf("Test question %d about %s/%s?", serial.Add(1), topic, subtopic),
			Options: []format.Option{
				{Text: "right", Correct: true, Explanation: "this one is right"},
				{Text: "wrong one", Explanation: "not this"},
				{Text: "wrong two", Explanation: "not this either"},
				{Text: "wrong three", Explanation: "nor this"},
			},
		}
	}
	added, dropped, err := st.ApplyReply(context.Background(), 0, format.Clean{Questions: qs}, time.Now().UTC())
	if err != nil || len(dropped) != 0 || len(added) != n {
		t.Fatalf("AddQuestions: added %d, dropped %v, err %v", len(added), dropped, err)
	}
	return added
}
