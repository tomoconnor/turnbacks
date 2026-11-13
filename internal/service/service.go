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

// Package service implements Turnbacks business rules: validation,
// authorisation decisions, idempotent submission, problem merging, analytics
// orchestration and business-case calculation. It is transport-agnostic and is
// used by the HTTP API and the Slack integration alike.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
	"turnbacks/internal/store"
	"turnbacks/internal/webhook"
)

// Service is the application service. It is safe for concurrent use.
type Service struct {
	store *store.Store
	log   *slog.Logger
	now   func() time.Time

	publicURL string        // for links in webhook events; may be empty
	wake      chan struct{} // nudges the webhook worker after an event is queued
	hooks     *http.Client  // outbound webhook client
}

// New constructs a Service.
func New(st *store.Store, log *slog.Logger) *Service {
	return &Service{store: st, log: log, now: time.Now, wake: make(chan struct{}, 1),
		hooks: webhook.NewClient(10*time.Second, false)}
}

// SetPublicURL sets the base URL used for links in webhook events.
func (s *Service) SetPublicURL(u string) { s.publicURL = strings.TrimRight(u, "/") }

// SetWebhookClient overrides the outbound webhook HTTP client.
func (s *Service) SetWebhookClient(c *http.Client) { s.hooks = c }

// link returns an absolute link to path in the web app, or "".
func (s *Service) link(path string) string {
	if s.publicURL == "" {
		return ""
	}
	return s.publicURL + path
}

// SetClock overrides the clock (tests only).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Now returns the service clock time.
func (s *Service) Now() time.Time { return s.now() }

// Page holds pagination parameters.
type Page struct {
	Limit  int32
	Offset int32
}

// NormalisePage clamps pagination parameters.
func NormalisePage(limit, offset int, def, max int) Page {
	if limit <= 0 {
		limit = def
	}
	if limit > max {
		limit = max
	}
	if offset < 0 {
		offset = 0
	}
	return Page{Limit: int32(limit), Offset: int32(offset)}
}

// Settings returns organisation settings and the parsed reporting location.
func (s *Service) Settings(ctx context.Context) (sqlc.OrgSetting, *time.Location, error) {
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		return st, nil, fmt.Errorf("load settings: %w", err)
	}
	loc, err := time.LoadLocation(st.ReportingTimezone)
	if err != nil {
		s.log.Error("invalid reporting timezone in settings; falling back to UTC", "tz", st.ReportingTimezone)
		loc = time.UTC
	}
	return st, loc, nil
}

func requireAdmin(a domain.Actor) error {
	if !a.Admin {
		return &domain.Error{Kind: domain.KindForbidden, Code: "admin_required", Message: "this action requires an admin client certificate"}
	}
	return nil
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

// escapeLike escapes ILIKE wildcards in user input.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func notFoundOr(err error, what string) error {
	if store.IsNoRows(err) {
		return domain.NotFound(what)
	}
	return err
}

// asTimePtr converts an untyped nullable timestamp scanned by pgx.
func asTimePtr(v any) *time.Time {
	if t, ok := v.(time.Time); ok {
		return &t
	}
	return nil
}

func uuidPtr(u uuid.UUID) *uuid.UUID { return &u }

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
