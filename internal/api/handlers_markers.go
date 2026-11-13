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

package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"turnbacks/internal/auth"
	"turnbacks/internal/domain"
	"turnbacks/internal/service"
)

// ChangeMarker is a point in time when something changed.
type ChangeMarker struct {
	ID           uuid.UUID  `json:"id"`
	OccurredAt   time.Time  `json:"occurred_at"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	URL          *string    `json:"url"`
	ProblemID    *uuid.UUID `json:"problem_id"`
	ProblemTitle *string    `json:"problem_title"`
	Source       string     `json:"source"`
	// CreatedBy is the reporter key of whoever marked the change (administrators only).
	CreatedBy *string   `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	IsDemo    bool      `json:"is_demo"`
}

func toMarker(m service.Marker, admin bool) ChangeMarker {
	out := ChangeMarker{
		ID: m.Marker.ID, OccurredAt: m.Marker.OccurredAt, Title: m.Marker.Title, Description: m.Marker.Description,
		URL: m.Marker.Url, ProblemID: m.Marker.ProblemID, ProblemTitle: m.ProblemTitle, Source: m.Marker.Source,
		CreatedAt: m.Marker.CreatedAt, IsDemo: m.Marker.IsDemo,
	}
	if admin {
		out.CreatedBy = m.Marker.CreatedBy
	}
	return out
}

// TimeWindow is a half-open time range with its totals.
type TimeWindow struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"` // exclusive
	Totals Totals    `json:"totals"`
}

// MarkerComparison compares equal windows before and after a change.
type MarkerComparison struct {
	Marker           ChangeMarker `json:"marker"`
	WindowSeconds    int64        `json:"window_seconds"`
	RequestedDays    int          `json:"requested_days"`
	InsufficientData bool         `json:"insufficient_data"`
	Before           TimeWindow   `json:"before"`
	After            TimeWindow   `json:"after"`
	Change           Change       `json:"change"`
	Caveat           string       `json:"caveat"`
}

const markerCaveat = "Observational comparison of equal-length windows immediately before and after the change. " +
	"Other changes, reporting behaviour, staffing or seasonality may also explain differences; this does not establish causation."

func toMarkerComparison(c service.MarkerComparison, admin bool) MarkerComparison {
	return MarkerComparison{
		Marker: toMarker(c.Marker, admin), WindowSeconds: int64(c.Window.Seconds()), RequestedDays: c.RequestedDays,
		InsufficientData: c.Insufficient,
		Before:           TimeWindow{Start: c.Before.From, End: c.Before.To, Totals: toTotals(c.BeforeTotals)},
		After:            TimeWindow{Start: c.After.From, End: c.After.To, Totals: toTotals(c.AfterTotals)},
		Change:           toChange(c.BeforeTotals, c.AfterTotals), Caveat: markerCaveat,
	}
}

func queryTime(r *http.Request, name string) (*time.Time, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid query parameter",
			Fields: []domain.FieldError{{Field: name, Message: "must be an RFC 3339 timestamp"}}}
	}
	return &t, nil
}

type markerRequest struct {
	OccurredAt  *time.Time `json:"occurred_at"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	URL         *string    `json:"url"`
	ProblemID   *uuid.UUID `json:"problem_id"`
	Source      string     `json:"source"`
}

func (s *Server) listMarkers(w http.ResponseWriter, r *http.Request) {
	from, e1 := queryTime(r, "from")
	to, e2 := queryTime(r, "to")
	pid, e3 := queryUUID(r, "problem_id")
	if err := firstErr(e1, e2, e3); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	a := auth.ActorFrom(r.Context())
	rows, err := s.Service.ListMarkers(r.Context(), a, service.MarkerFilter{
		From: from, To: to, ProblemID: pid, IncludeGlobal: r.URL.Query().Get("include_global") != "false",
	})
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]ChangeMarker, 0, len(rows))
	for _, m := range rows {
		out = append(out, toMarker(m, a.Admin))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) createMarker(w http.ResponseWriter, r *http.Request) {
	var req markerRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	src := req.Source
	switch src {
	case "":
		src = service.MarkerSourceWeb
	case service.MarkerSourceWeb, service.MarkerSourceAPI, service.MarkerSourceCLI:
	default:
		s.errs.respond(w, r, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid source",
			Fields: []domain.FieldError{{Field: "source", Message: "must be web, api or cli"}}})
		return
	}
	a := auth.ActorFrom(r.Context())
	m, err := s.Service.CreateMarker(r.Context(), a, service.MarkerInput{
		OccurredAt: req.OccurredAt, Title: req.Title, Description: req.Description, URL: req.URL, ProblemID: req.ProblemID, Source: src,
	})
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMarker(m, a.Admin))
}

func (s *Server) updateMarker(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req markerRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	a := auth.ActorFrom(r.Context())
	m, err := s.Service.UpdateMarker(r.Context(), a, id, service.MarkerInput{
		OccurredAt: req.OccurredAt, Title: req.Title, Description: req.Description, URL: req.URL, ProblemID: req.ProblemID,
	})
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMarker(m, a.Admin))
}

func (s *Server) deleteMarker(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	if err := s.Service.DeleteMarker(r.Context(), auth.ActorFrom(r.Context()), id); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) markerComparison(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	window, err := queryInt(r, "window_days", 28)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	a := auth.ActorFrom(r.Context())
	c, err := s.Service.CompareMarker(r.Context(), a, id, window)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMarkerComparison(c, a.Admin))
}

func (s *Server) analyticsChanges(w http.ResponseWriter, r *http.Request) {
	window, err := queryInt(r, "window_days", 28)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	pid, err := queryUUID(r, "problem_id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	a := auth.ActorFrom(r.Context())
	rows, err := s.Service.MarkerOverview(r.Context(), a, pid, window)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]MarkerComparison, 0, len(rows))
	for _, c := range rows {
		out = append(out, toMarkerComparison(c, a.Admin))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
