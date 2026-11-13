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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to the Turnbacks API.
type Client struct {
	base string
	http *http.Client
}

// APIError is an error response from the server.
type APIError struct {
	Status  int
	Code    string
	Message string
	Fields  []struct{ Field, Message string }
}

func (e *APIError) Error() string {
	if len(e.Fields) > 0 {
		parts := make([]string, 0, len(e.Fields))
		for _, f := range e.Fields {
			parts = append(parts, f.Field+": "+f.Message)
		}
		return e.Message + " (" + strings.Join(parts, "; ") + ")"
	}
	return e.Message
}

// errUnreachable marks network-level failures (server down, VPN down…).
var errUnreachable = errors.New("server unreachable")

func newClient(base string, timeout time.Duration) *Client {
	return &Client{base: strings.TrimRight(base, "/") + "/api/v1", http: &http.Client{Timeout: timeout}}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "turnbacks-cli/"+version)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", errUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusBadGateway {
		return fmt.Errorf("%w: HTTP %d", errUnreachable, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		var env struct {
			Error struct {
				Code    string
				Message string
				Fields  []struct{ Field, Message string }
			}
		}
		_ = json.Unmarshal(data, &env)
		if env.Error.Message == "" {
			env.Error.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return &APIError{Status: resp.StatusCode, Code: env.Error.Code, Message: env.Error.Message, Fields: env.Error.Fields}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// API types (only the fields the CLI uses).

// Problem is a recurring source of friction.
type Problem struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Alias        *string `json:"alias"`
	CategoryName string  `json:"category_name"`
	Status       string  `json:"status"`
	RecentCount  int64   `json:"recent_count"`
}

// Team is a team.
type Team struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// Category is a problem category.
type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Submission is a turnback to record (also the offline queue format).
type Submission struct {
	ID                   string  `json:"id"`
	ProblemID            string  `json:"problem_id"`
	TeamID               *string `json:"team_id,omitempty"`
	DurationMinutes      int     `json:"duration_minutes"`
	Quantity             int     `json:"quantity"`
	Note                 *string `json:"note,omitempty"`
	OccurredAt           string  `json:"occurred_at"`
	Source               string  `json:"source"`
	ConfirmLargeDuration bool    `json:"confirm_large_duration,omitempty"`
}

// queued wraps a submission with its title for the local queue file.
type queuedSubmission struct {
	Submission
	ProblemTitle string `json:"problem_title"`
}

// SubmitResult is the API response to a submission.
type SubmitResult struct {
	Turnback struct {
		ID           string `json:"id"`
		ProblemTitle string `json:"problem_title"`
	} `json:"turnback"`
	Replayed  bool   `json:"replayed"`
	Quantity  int    `json:"quantity"`
	UndoToken string `json:"undo_token"`
}

// Settings are the organisation settings the CLI needs.
type Settings struct {
	OrgName          string `json:"org_name"`
	Currency         string `json:"currency"`
	CurrencyExponent int    `json:"currency_exponent"`
}

// Totals are aggregate measures.
type Totals struct {
	Turnbacks         int64       `json:"turnbacks"`
	Minutes           int64       `json:"minutes"`
	CostMinor         int64       `json:"cost_minor"`
	DistinctReporters int64       `json:"distinct_reporters"`
	AvgMinutes        json.Number `json:"avg_minutes"`
}

// Summary is the headline analytics response.
type Summary struct {
	Period struct {
		From, To, Timezone string
	} `json:"period"`
	Current Totals `json:"current"`
	Change  struct {
		TurnbacksBP *int64 `json:"turnbacks_bp"`
		MinutesBP   *int64 `json:"minutes_bp"`
		CostBP      *int64 `json:"cost_bp"`
	} `json:"change"`
}

// RankedProblem is one ranking row.
type RankedProblem struct {
	Title     string `json:"title"`
	Turnbacks int64  `json:"turnbacks"`
	Minutes   int64  `json:"minutes"`
	CostMinor int64  `json:"cost_minor"`
}
