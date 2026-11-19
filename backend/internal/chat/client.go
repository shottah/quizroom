// Package chat turns chat messages into Claude calls: the runner client, the
// prompt, the reply parsing, and the job worker that stores what comes back.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"quizroom/internal/runner"
)

var (
	ErrNotSignedIn = errors.New("claude is not signed in")
	ErrBusy        = errors.New("the runner is busy")
	ErrUnavailable = errors.New("the runner is unreachable")
)

// Client talks to the runner container.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	// Longer than the runner's own maximum call time.
	return &http.Client{Timeout: time.Duration(runner.MaxTimeoutS+60) * time.Second}
}

// Health asks the runner which credential it has and its CLI version.
func (c *Client) Health(ctx context.Context) (runner.Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return runner.Health{}, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return runner.Health{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	var h runner.Health
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return runner.Health{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return h, nil
}

// Call runs one claude call through the runner.
func (c *Client) Call(ctx context.Context, r runner.Request) (runner.Response, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return runner.Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/claude", bytes.NewReader(body))
	if err != nil {
		return runner.Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return runner.Response{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusPreconditionFailed:
		return runner.Response{}, ErrNotSignedIn
	case http.StatusConflict:
		return runner.Response{}, ErrBusy
	default:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return runner.Response{}, fmt.Errorf("runner returned %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	var out runner.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return runner.Response{}, fmt.Errorf("runner reply: %w", err)
	}
	return out, nil
}
