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

package service

import (
	"context"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
	"turnbacks/internal/reporting"
	"turnbacks/internal/store"
)

// Change-marker sources.
const (
	MarkerSourceWeb     = "web"
	MarkerSourceAPI     = "api"
	MarkerSourceCLI     = "cli"
	MarkerSourceWebhook = "webhook"
)

// MarkerInput creates or updates a change marker: a point in time when
// something changed, optionally for one problem.
type MarkerInput struct {
	OccurredAt  *time.Time // defaults to now
	Title       string
	Description string
	URL         *string
	ProblemID   *uuid.UUID
	Source      string
	IsDemo      bool
}

func (s *Service) validateMarker(ctx context.Context, in *MarkerInput) error {
	in.Title, in.Description = strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	in.URL = trimPtr(in.URL)
	now := s.now().UTC()
	if in.OccurredAt == nil {
		in.OccurredAt = &now
	}
	t := in.OccurredAt.UTC().Truncate(time.Microsecond)
	in.OccurredAt = &t

	var v domain.Validator
	n := utf8.RuneCountInString(in.Title)
	v.Check(n >= 1 && n <= 200, "title", "must be between 1 and 200 characters")
	v.Check(utf8.RuneCountInString(in.Description) <= 2000, "description", "must be at most 2000 characters")
	if in.URL != nil {
		u, err := url.Parse(*in.URL)
		v.Check(err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(*in.URL) <= 2000,
			"url", "must be an http or https URL")
	}
	v.Check(!t.After(now.Add(maxClockSkew)), "occurred_at", "must not be in the future")
	v.Check(!t.Before(now.AddDate(-5, 0, 0)), "occurred_at", "must be within the last five years")
	switch in.Source {
	case MarkerSourceWeb, MarkerSourceAPI, MarkerSourceCLI, MarkerSourceWebhook:
	default:
		v.Add("source", "must be web, api, cli or webhook")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if in.ProblemID != nil {
		p, err := s.store.GetProblem(ctx, *in.ProblemID)
		if err != nil {
			if store.IsNoRows(err) {
				return &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "unknown problem",
					Fields: []domain.FieldError{{Field: "problem_id", Message: "does not exist"}}}
			}
			return err
		}
		// A change to a merged problem applies to the problem it was merged into.
		if p.Problem.MergedIntoID != nil {
			in.ProblemID = p.Problem.MergedIntoID
		}
	}
	return nil
}

// Marker is a change marker with its problem title (nil for organisation-wide
// markers).
type Marker struct {
	Marker       sqlc.ChangeMarker
	ProblemTitle *string
}

// CreateMarker records a change marker. Anyone may mark a change.
func (s *Service) CreateMarker(ctx context.Context, a domain.Actor, in MarkerInput) (Marker, error) {
	if err := s.validateMarker(ctx, &in); err != nil {
		return Marker{}, err
	}
	m, err := s.store.CreateMarker(ctx, sqlc.CreateMarkerParams{
		OccurredAt: *in.OccurredAt, Title: in.Title, Description: in.Description, Url: in.URL,
		ProblemID: in.ProblemID, Source: in.Source, CreatedBy: strPtr(a.Reporter), IsDemo: in.IsDemo,
	})
	if err != nil {
		return Marker{}, err
	}
	out, err := s.getMarker(ctx, m.ID)
	if err != nil {
		return Marker{}, err
	}
	s.log.InfoContext(ctx, "change marked", "marker_id", m.ID, "problem_id", m.ProblemID, "source", m.Source)
	if !in.IsDemo {
		s.emitLogged(ctx, markerEvent(s.link, out.Marker, out.ProblemTitle))
	}
	return out, nil
}

func (s *Service) getMarker(ctx context.Context, id uuid.UUID) (Marker, error) {
	r, err := s.store.GetMarker(ctx, id)
	if err != nil {
		return Marker{}, notFoundOr(err, "change marker")
	}
	return Marker{Marker: r.ChangeMarker, ProblemTitle: r.ProblemTitle}, nil
}

// UpdateMarker changes a change marker (administrators only).
func (s *Service) UpdateMarker(ctx context.Context, a domain.Actor, id uuid.UUID, in MarkerInput) (Marker, error) {
	if err := requireAdmin(a); err != nil {
		return Marker{}, err
	}
	existing, err := s.getMarker(ctx, id)
	if err != nil {
		return Marker{}, err
	}
	in.Source = existing.Marker.Source
	if err := s.validateMarker(ctx, &in); err != nil {
		return Marker{}, err
	}
	if _, err := s.store.UpdateMarker(ctx, sqlc.UpdateMarkerParams{
		ID: id, OccurredAt: *in.OccurredAt, Title: in.Title, Description: in.Description, Url: in.URL, ProblemID: in.ProblemID,
	}); err != nil {
		return Marker{}, notFoundOr(err, "change marker")
	}
	return s.getMarker(ctx, id)
}

// DeleteMarker removes a change marker (administrators only).
func (s *Service) DeleteMarker(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	if err := requireAdmin(a); err != nil {
		return err
	}
	n, err := s.store.DeleteMarker(ctx, id)
	if err == nil && n == 0 {
		return domain.NotFound("change marker")
	}
	return err
}

// MarkerFilter selects change markers.
type MarkerFilter struct {
	From, To  *time.Time
	ProblemID *uuid.UUID
	// IncludeGlobal adds organisation-wide markers when ProblemID is set.
	IncludeGlobal bool
	Limit         int32
}

// ListMarkers lists change markers, most recent first.
func (s *Service) ListMarkers(ctx context.Context, a domain.Actor, f MarkerFilter) ([]Marker, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	rows, err := s.store.ListMarkers(ctx, sqlc.ListMarkersParams{
		FromTs: f.From, ToTs: f.To, ProblemID: f.ProblemID, IncludeGlobal: f.IncludeGlobal, Lim: f.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Marker, 0, len(rows))
	for _, r := range rows {
		out = append(out, Marker{Marker: r.ChangeMarker, ProblemTitle: r.ProblemTitle})
	}
	return out, nil
}

// Window is a half-open time range [From, To).
type Window struct {
	From, To time.Time
}

// MarkerComparison compares equal-length windows immediately before and after
// a change marker. It is observational and does not establish causation.
type MarkerComparison struct {
	Marker Marker
	Before Window
	After  Window
	// Window is the length of each window: the requested length, or the time
	// elapsed since the change if that is shorter.
	Window time.Duration
	// Insufficient is true when the windows are too short to be meaningful.
	Insufficient   bool
	BeforeTotals   Totals
	AfterTotals    Totals
	RequestedDays  int
	ElapsedSeconds int64
}

// MarkerWindows returns equal windows either side of t, each as long as
// requested or as the time elapsed since t, whichever is shorter.
func MarkerWindows(t time.Time, requested time.Duration, now time.Time) (before, after Window, length time.Duration) {
	length = min(requested, now.Sub(t))
	if length < 0 {
		length = 0
	}
	length = length.Truncate(time.Minute)
	return Window{From: t.Add(-length), To: t}, Window{From: t, To: t.Add(length)}, length
}

func (s *Service) compareMarker(ctx context.Context, m Marker, windowDays int) (MarkerComparison, error) {
	if windowDays <= 0 {
		windowDays = 28
	}
	now := s.now()
	before, after, length := MarkerWindows(m.Marker.OccurredAt, time.Duration(windowDays)*24*time.Hour, now)
	c := MarkerComparison{
		Marker: m, Before: before, After: after, Window: length, RequestedDays: windowDays,
		Insufficient:   length < time.Duration(reporting.MinMeaningfulWindowDays)*24*time.Hour,
		ElapsedSeconds: int64(now.Sub(m.Marker.OccurredAt).Seconds()),
	}
	if length == 0 {
		return c, nil
	}
	f := AnalyticsFilter{ProblemID: m.Marker.ProblemID}
	var err error
	if c.BeforeTotals, err = s.totals(ctx, before.From, before.To, f); err != nil {
		return c, err
	}
	c.AfterTotals, err = s.totals(ctx, after.From, after.To, f)
	return c, err
}

func validWindowDays(windowDays int) error {
	if windowDays < 0 || windowDays > 365 {
		return &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid window",
			Fields: []domain.FieldError{{Field: "window_days", Message: "must be between 1 and 365"}}}
	}
	return nil
}

// CompareMarker compares equal windows before and after a change marker.
func (s *Service) CompareMarker(ctx context.Context, a domain.Actor, id uuid.UUID, windowDays int) (MarkerComparison, error) {
	if err := validWindowDays(windowDays); err != nil {
		return MarkerComparison{}, err
	}
	m, err := s.getMarker(ctx, id)
	if err != nil {
		return MarkerComparison{}, err
	}
	return s.compareMarker(ctx, m, windowDays)
}

// MarkerOverview compares the most recent change markers (optionally only
// those for one problem, plus organisation-wide ones).
func (s *Service) MarkerOverview(ctx context.Context, a domain.Actor, problemID *uuid.UUID, windowDays int) ([]MarkerComparison, error) {
	if err := validWindowDays(windowDays); err != nil {
		return nil, err
	}
	markers, err := s.ListMarkers(ctx, a, MarkerFilter{ProblemID: problemID, IncludeGlobal: true, Limit: 20})
	if err != nil {
		return nil, err
	}
	out := make([]MarkerComparison, 0, len(markers))
	for _, m := range markers {
		c, err := s.compareMarker(ctx, m, windowDays)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
