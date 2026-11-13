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

type submitRequest struct {
	ID                   uuid.UUID  `json:"id"`
	ProblemID            uuid.UUID  `json:"problem_id"`
	TeamID               *uuid.UUID `json:"team_id"`
	DurationMinutes      int        `json:"duration_minutes"`
	DurationEstimated    *bool      `json:"duration_estimated"`
	Note                 *string    `json:"note"`
	OccurredAt           *time.Time `json:"occurred_at"`
	Source               string     `json:"source"`
	ConfirmLargeDuration bool       `json:"confirm_large_duration"`
	Quantity             *int       `json:"quantity"`
}

func (s *Server) submitTurnback(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	if req.Source == "" {
		req.Source = domain.SourceAPI
	}
	if !domain.ClientSubmittableSource(req.Source) {
		s.errs.respond(w, r, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid source",
			Fields: []domain.FieldError{{Field: "source", Message: "must be web, pwa_offline, api or cli"}}})
		return
	}
	estimated := true
	if req.DurationEstimated != nil {
		estimated = *req.DurationEstimated
	}
	quantity := 1
	if req.Quantity != nil {
		quantity = *req.Quantity
		if quantity < 1 {
			s.errs.respond(w, r, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid quantity",
				Fields: []domain.FieldError{{Field: "quantity", Message: "must be between 1 and 100"}}})
			return
		}
	}
	a := auth.ActorFrom(r.Context())
	res, err := s.Service.SubmitTurnback(r.Context(), a, service.TurnbackInput{
		Quantity: quantity, ID: req.ID, ProblemID: req.ProblemID, TeamID: req.TeamID,
		DurationMinutes: req.DurationMinutes, DurationEstimated: estimated, Note: req.Note,
		OccurredAt: req.OccurredAt, Source: req.Source, ConfirmLargeDuration: req.ConfirmLargeDuration,
	})
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	status := http.StatusCreated
	if !res.Created {
		status = http.StatusOK
	}
	writeJSON(w, status, SubmitResult{
		Turnback: toTurnback(res.Turnback, res.ProblemTitle, a.Admin), Replayed: !res.Created,
		Quantity: res.Quantity, UndoToken: res.UndoToken,
	})
}

type updateTurnbackRequest struct {
	ProblemID            uuid.UUID `json:"problem_id"`
	DurationMinutes      int       `json:"duration_minutes"`
	DurationEstimated    bool      `json:"duration_estimated"`
	Note                 *string   `json:"note"`
	OccurredAt           time.Time `json:"occurred_at"`
	ConfirmLargeDuration bool      `json:"confirm_large_duration"`
}

func (s *Server) updateTurnback(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req updateTurnbackRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	a := auth.ActorFrom(r.Context())
	res, err := s.Service.UpdateTurnback(r.Context(), a, id, service.TurnbackUpdate(req))
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTurnback(res.Turnback, res.ProblemTitle, a.Admin))
}

func (s *Server) deleteTurnback(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	q := r.URL.Query()
	if err := s.Service.DeleteTurnback(r.Context(), auth.ActorFrom(r.Context()), id, q.Get("batch") == "true", q.Get("undo_token")); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listTurnbacks(w http.ResponseWriter, r *http.Request) {
	f, err := s.filter(r)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	limit, err1 := queryInt(r, "limit", 50)
	offset, err2 := queryInt(r, "offset", 0)
	if err := firstErr(err1, err2); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	p := service.NormalisePage(limit, offset, 50, 500)
	a := auth.ActorFrom(r.Context())
	rows, total, err := s.Service.ListTurnbacks(r.Context(), a, f, r.URL.Query().Get("reporter"), p)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := List[Turnback]{Items: make([]Turnback, 0, len(rows)), Total: total, Limit: p.Limit, Offset: p.Offset}
	for _, t := range rows {
		out.Items = append(out.Items, Turnback{
			ID: t.ID, ProblemID: t.ProblemID, ProblemTitle: t.ProblemTitle, TeamID: t.TeamID, DurationMinutes: t.DurationMinutes,
			DurationEstimated: t.DurationEstimated, Note: t.Note, OccurredAt: t.OccurredAt, SubmittedAt: t.SubmittedAt,
			Source: t.Source, IsDemo: t.IsDemo, BatchID: t.BatchID, Reporter: t.Reporter,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
