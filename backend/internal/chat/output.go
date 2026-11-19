package chat

import (
	"encoding/json"
	"errors"
	"strings"

	"quizroom/internal/format"
)

// ErrFormat means Claude's output had no valid structured reply.
var ErrFormat = errors.New("Claude's reply didn't match the format, so nothing was added. Press Retry.")

// ClaudeError carries the message Claude itself reported, such as a usage limit.
type ClaudeError struct{ Message string }

func (e ClaudeError) Error() string { return "Claude reported an error: " + e.Message }

// ParseOutput reads `claude -p --output-format json` output: the structured
// reply and the cost (non-zero only when an API key is in use).
func ParseOutput(stdout string) (format.Reply, float64, error) {
	var out struct {
		IsError          bool             `json:"is_error"`
		Result           string           `json:"result"`
		StructuredOutput *json.RawMessage `json:"structured_output"`
		TotalCostUSD     float64          `json:"total_cost_usd"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		return format.Reply{}, 0, ErrFormat
	}
	if out.IsError {
		msg := strings.TrimSpace(out.Result)
		if msg == "" {
			return format.Reply{}, out.TotalCostUSD, ErrFormat
		}
		return format.Reply{}, out.TotalCostUSD, ClaudeError{Message: msg}
	}
	if out.StructuredOutput == nil {
		return format.Reply{}, out.TotalCostUSD, ErrFormat
	}
	var r format.Reply
	if err := json.Unmarshal(*out.StructuredOutput, &r); err != nil {
		return format.Reply{}, out.TotalCostUSD, ErrFormat
	}
	return r, out.TotalCostUSD, nil
}
