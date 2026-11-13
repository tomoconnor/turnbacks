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
	"strings"

	"github.com/google/uuid"

	"turnbacks/internal/auth"
	"turnbacks/internal/service"
)

func (s *Server) listProblems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err1 := queryInt(r, "limit", 50)
	offset, err2 := queryInt(r, "offset", 0)
	cat, err3 := queryUUID(r, "category_id")
	if err := firstErr(err1, err2, err3); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var statuses []string
	if st := q.Get("status"); st != "" {
		statuses = strings.Split(st, ",")
	}
	p := service.NormalisePage(limit, offset, 50, 200)
	items, total, err := s.Service.ListProblems(r.Context(), auth.ActorFrom(r.Context()), service.ProblemListParams{
		Query: q.Get("q"), CategoryID: cat, Statuses: statuses, Page: p,
	})
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := List[Problem]{Items: make([]Problem, 0, len(items)), Total: total, Limit: p.Limit, Offset: p.Offset}
	for _, it := range items {
		pr := toProblem(it.Problem, it.CategoryName)
		rc := it.RecentCount
		pr.RecentCount, pr.LastOccurredAt = &rc, it.LastOccurredAt
		out.Items = append(out.Items, pr)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) quickProblems(w http.ResponseWriter, r *http.Request) {
	fr, err := s.Service.QuickProblems(r.Context())
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, QuickProblems{Frequent: toQuick(fr)})
}

func (s *Server) similarProblems(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Service.SimilarProblems(r.Context(), auth.ActorFrom(r.Context()), r.URL.Query().Get("title"))
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]SimilarProblem, 0, len(rows))
	for _, p := range rows {
		out = append(out, SimilarProblem{ID: p.ID, Title: p.Title, Alias: p.Alias, Status: p.Status, Score: p.Score})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) getProblem(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	p, err := s.Service.GetProblem(r.Context(), auth.ActorFrom(r.Context()), id)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProblem(p.Problem, p.CategoryName))
}

type problemRequest struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CategoryID  uuid.UUID `json:"category_id"`
	Status      string    `json:"status"`
	Alias       *string   `json:"alias"`
	ExternalURL *string   `json:"external_url"`
}

func (req problemRequest) input() service.ProblemInput {
	return service.ProblemInput{Title: req.Title, Description: req.Description, CategoryID: req.CategoryID, Status: req.Status, Alias: req.Alias, ExternalURL: req.ExternalURL}
}

func (s *Server) createProblem(w http.ResponseWriter, r *http.Request) {
	var req problemRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	a := auth.ActorFrom(r.Context())
	p, err := s.Service.CreateProblem(r.Context(), a, req.input())
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	full, err := s.Service.GetProblem(r.Context(), a, p.ID)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/problems/"+p.ID.String())
	writeJSON(w, http.StatusCreated, toProblem(full.Problem, full.CategoryName))
}

func (s *Server) updateProblem(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req problemRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	a := auth.ActorFrom(r.Context())
	if _, err := s.Service.UpdateProblem(r.Context(), a, id, req.input()); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	full, err := s.Service.GetProblem(r.Context(), a, id)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProblem(full.Problem, full.CategoryName))
}

type mergeRequest struct {
	TargetID uuid.UUID `json:"target_id"`
}

func (s *Server) mergeProblem(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req mergeRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	a := auth.ActorFrom(r.Context())
	res, err := s.Service.MergeProblems(r.Context(), a, id, req.TargetID)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	full, err := s.Service.GetProblem(r.Context(), a, res.Target.ID)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MergeResult{Target: toProblem(full.Problem, full.CategoryName), MovedTurnbacks: res.MovedTurnbacks, MovedRemediations: res.MovedRemediations})
}
