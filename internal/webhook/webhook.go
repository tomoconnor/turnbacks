// Copyright (c) 2026 Tom O'Connor <tom@twinhelix.org>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Package webhook formats, signs and sends outbound webhook requests.
//
// Every event is stored as an Envelope. At delivery time it is rendered in
// the webhook's format:
//
//	json      the envelope itself, signed with HMAC-SHA256
//	slack     {"text": …} with Slack link syntax (Slack incoming webhooks)
//	markdown  {"text": …} with Markdown links (Mattermost, Rocket.Chat, …)
//	teams     an Adaptive Card message (Microsoft Teams "Workflows" webhooks)
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Formats.
const (
	FormatJSON     = "json"
	FormatSlack    = "slack"
	FormatMarkdown = "markdown"
	FormatTeams    = "teams"
)

// Formats lists the supported formats.
var Formats = []string{FormatJSON, FormatSlack, FormatMarkdown, FormatTeams}

// Events.
const (
	EventTurnbackRecorded    = "turnback.recorded"
	EventProblemCreated      = "problem.created"
	EventChangeMarked        = "change.marked"
	EventRemediationRecorded = "remediation.recorded"
	// EventTest is sent by the "Send test" button and is not subscribable.
	EventTest = "test"
)

// Events lists the events a webhook can subscribe to.
var Events = []string{EventTurnbackRecorded, EventProblemCreated, EventChangeMarked, EventRemediationRecorded}

// Envelope is the stored form of every event, and the body of json webhooks.
type Envelope struct {
	Event      string    `json:"event"`
	OccurredAt time.Time `json:"occurred_at"`
	// Summary is a one-line, plain-text description for chat formats.
	Summary string `json:"summary"`
	// URL links to the relevant page in Turnbacks, when the public URL is known.
	URL  string `json:"url,omitempty"`
	Data any    `json:"data"`
}

// Request headers sent with every delivery.
const (
	HeaderEvent     = "X-Turnbacks-Event"
	HeaderDelivery  = "X-Turnbacks-Delivery"
	HeaderTimestamp = "X-Turnbacks-Timestamp"
	HeaderSignature = "X-Turnbacks-Signature"
)

// Sign returns the signature header value: "sha256=" + hex HMAC-SHA256 of
// "<timestamp>.<body>" keyed with the webhook secret.
func Sign(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks a signature produced by Sign (for receivers and tests).
func Verify(secret string, timestamp int64, body []byte, signature string) bool {
	return hmac.Equal([]byte(Sign(secret, timestamp, body)), []byte(signature))
}

// Render produces the request body for a format from a stored envelope.
func Render(format string, stored []byte) ([]byte, error) {
	if format == FormatJSON {
		return stored, nil
	}
	var env struct {
		Summary string `json:"summary"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(stored, &env); err != nil {
		return nil, err
	}
	switch format {
	case FormatSlack:
		text := slackEscape(env.Summary)
		if env.URL != "" {
			text += " <" + env.URL + "|Open in Turnbacks>"
		}
		return json.Marshal(map[string]string{"text": text})
	case FormatMarkdown:
		text := env.Summary
		if env.URL != "" {
			text += " [Open in Turnbacks](" + env.URL + ")"
		}
		return json.Marshal(map[string]string{"text": text})
	case FormatTeams:
		body := []map[string]any{{"type": "TextBlock", "text": env.Summary, "wrap": true}}
		card := map[string]any{
			"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
			"type":    "AdaptiveCard", "version": "1.4", "body": body,
		}
		if env.URL != "" {
			card["actions"] = []map[string]any{{"type": "Action.OpenUrl", "title": "Open in Turnbacks", "url": env.URL}}
		}
		return json.Marshal(map[string]any{
			"type": "message",
			"attachments": []map[string]any{{
				"contentType": "application/vnd.microsoft.card.adaptive", "contentUrl": nil, "content": card,
			}},
		})
	default:
		return nil, fmt.Errorf("unknown format %q", format)
	}
}

func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// ValidateURL checks a webhook URL as entered by an administrator.
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(raw) > 2000 {
		return errors.New("must be an http or https URL")
	}
	if u.User != nil {
		return errors.New("must not contain a user name or password")
	}
	return nil
}

// ErrForbiddenAddress is returned when a webhook URL resolves to an address
// that webhooks may not reach.
var ErrForbiddenAddress = errors.New("address not allowed for webhooks")

// allowed reports whether a webhook may connect to ip. Private addresses are
// allowed (chat servers are often internal); link-local addresses (cloud
// metadata services) and unspecified/multicast addresses never are, and
// loopback only when explicitly enabled.
func allowed(ip netip.Addr, allowLoopback bool) bool {
	ip = ip.Unmap()
	switch {
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsUnspecified(), ip.IsMulticast():
		return false
	case ip.IsLoopback():
		return allowLoopback
	}
	return true
}

// NewClient returns the HTTP client used for deliveries. It checks the
// address actually dialled (after DNS resolution, so DNS rebinding cannot
// bypass it), does not follow redirects and times out after timeout.
func NewClient(timeout time.Duration, allowLoopback bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !allowed(ap.Addr(), allowLoopback) {
				return ErrForbiddenAddress
			}
			return nil
		},
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil // a proxy would hide the destination from the address check
	tr.DialContext = dialer.DialContext
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Delivery is one request to send.
type Delivery struct {
	ID     string
	Event  string
	URL    string
	Format string
	Secret string
	Stored []byte // the stored Envelope
}

// Result is the outcome of a delivery attempt.
type Result struct {
	StatusCode int // 0 when no response was received
	Err        error
	// Retry is false when retrying cannot help (most 4xx responses).
	Retry bool
}

// OK reports whether the delivery succeeded.
func (r Result) OK() bool { return r.Err == nil }

// Send delivers d once.
func Send(ctx context.Context, c *http.Client, d Delivery, now time.Time) Result {
	body, err := Render(d.Format, d.Stored)
	if err != nil {
		return Result{Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, strings.NewReader(string(body)))
	if err != nil {
		return Result{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Turnbacks-Webhook/1")
	req.Header.Set(HeaderEvent, d.Event)
	req.Header.Set(HeaderDelivery, d.ID)
	if d.Format == FormatJSON {
		ts := now.Unix()
		req.Header.Set(HeaderTimestamp, strconv.FormatInt(ts, 10))
		req.Header.Set(HeaderSignature, Sign(d.Secret, ts, body))
	}
	resp, err := c.Do(req)
	if err != nil {
		return Result{Err: err, Retry: !errors.Is(err, ErrForbiddenAddress)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return Result{StatusCode: resp.StatusCode}
	}
	retry := resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests
	return Result{StatusCode: resp.StatusCode, Err: fmt.Errorf("HTTP %d", resp.StatusCode), Retry: retry}
}

// Backoff returns the delay before retry number attempt (1-based).
func Backoff(attempt int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < attempt && d < 6*time.Hour; i++ {
		d *= 2
	}
	return min(d, 6*time.Hour)
}

// MaxAttempts is the number of attempts before a delivery is marked failed.
const MaxAttempts = 10
