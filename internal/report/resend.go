// Package report builds the daily report e-mail and sends it through the
// Resend API (https://resend.com/docs/api-reference/emails/send-email).
//
// The report is a summary only — screen time, top applications, categories,
// alerts and tracking gaps for the previous day. Window titles are never
// included, even when they are recorded.
package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultEndpoint is Resend's send-email endpoint.
const DefaultEndpoint = "https://api.resend.com/emails"

// Email is one message.
type Email struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

// Resend sends e-mail through the Resend HTTP API.
type Resend struct {
	Endpoint string       // empty: DefaultEndpoint
	HTTP     *http.Client // nil: a client with a 20 s timeout
}

// ErrRejected marks an error Resend answered with a 4xx status (other than
// 429): retrying the same message will not help.
var ErrRejected = errors.New("rejected by Resend")

// Send delivers m and returns Resend's message id. idempotencyKey, when not
// empty, makes a retry of the same message (after a timeout, say) safe: Resend
// sends it at most once. The API key never appears in a returned error.
func (r *Resend) Send(ctx context.Context, apiKey, idempotencyKey string, m Email) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("no Resend API key is set")
	}
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	hc := r.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	body, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("bad Resend endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "parent-tracker")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := hc.Do(req)
	if err != nil {
		// a *url.Error names the endpoint, which is not secret; the key is in a
		// header and never part of it
		return "", fmt.Errorf("cannot reach Resend: %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	var out struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		Name    string `json:"name"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode/100 == 2 {
		return out.ID, nil
	}
	msg := strings.TrimSpace(out.Message)
	if msg == "" {
		msg = resp.Status
	}
	msg = strings.ReplaceAll(msg, apiKey, "[key]")
	if resp.StatusCode/100 == 4 && resp.StatusCode != http.StatusTooManyRequests {
		return "", fmt.Errorf("%w (%d): %s", ErrRejected, resp.StatusCode, msg)
	}
	return "", fmt.Errorf("Resend answered %d: %s", resp.StatusCode, msg)
}
