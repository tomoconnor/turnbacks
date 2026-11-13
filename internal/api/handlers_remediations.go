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

	"turnbacks/internal/auth"
	"turnbacks/internal/domain"
	"turnbacks/internal/reporting"
	"turnbacks/internal/service"
)

type remediationRequest struct {
	ImplementedOn           string `json:"implemented_on"`
	Description             string `json:"description"`
	ImplementationCostMinor int64  `json:"implementation_cost_minor"`
	AnnualCostMinor         int64  `json:"annual_cost_minor"`
	ExpectedReductionPct    int    `json:"expected_reduction_pct"`
}

func (req remediationRequest) input() (service.RemediationInput, error) {
	d, err := reporting.ParseDate(req.ImplementedOn)
	if err != nil {
		return service.RemediationInput{}, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid date",
			Fields: []domain.FieldError{{Field: "implemented_on", Message: "must be a date in YYYY-MM-DD format"}}}
	}
	return service.RemediationInput{ImplementedOn: d, Description: req.Description, ImplementationCostMinor: req.ImplementationCostMinor,
		AnnualCostMinor: req.AnnualCostMinor, ExpectedReductionPct: req.ExpectedReductionPct}, nil
}

func (s *Server) listRemediations(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	rows, err := s.Service.ListRemediations(r.Context(), auth.ActorFrom(r.Context()), id)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]Remediation, 0, len(rows))
	for _, x := range rows {
		out = append(out, toRemediation(x))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) createRemediation(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req remediationRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	in, err := req.input()
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	rem, err := s.Service.CreateRemediation(r.Context(), auth.ActorFrom(r.Context()), id, in)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRemediation(rem))
}

func (s *Server) updateRemediation(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req remediationRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	in, err := req.input()
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	rem, err := s.Service.UpdateRemediation(r.Context(), auth.ActorFrom(r.Context()), id, in)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toRemediation(rem))
}

func (s *Server) deleteRemediation(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	if err := s.Service.DeleteRemediation(r.Context(), auth.ActorFrom(r.Context()), id); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) remediationComparison(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	window, err := queryInt(r, "window_days", reporting.DefaultComparisonWindowDays)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	c, err := s.Service.CompareRemediation(r.Context(), auth.ActorFrom(r.Context()), id, window)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toComparison(c))
}

func (s *Server) businessCaseParams(r *http.Request) (service.BusinessCaseParams, error) {
	id, err := pathUUID(r, "id")
	if err != nil {
		return service.BusinessCaseParams{}, err
	}
	p := service.BusinessCaseParams{ProblemID: id}
	q := r.URL.Query()
	if q.Get("period") != "" || q.Get("from") != "" {
		preset := q.Get("period")
		if preset == "" {
			preset = reporting.PresetCustom
		}
		per, err := s.Service.ResolvePeriod(r.Context(), preset, q.Get("from"), q.Get("to"))
		if err != nil {
			return p, err
		}
		p.Period = &per
	}
	var e1, e2, e3, e4 error
	p.ImplementationCostMinor, e1 = queryInt64Ptr(r, "implementation_cost_minor")
	p.AnnualCostMinor, e2 = queryInt64Ptr(r, "annual_cost_minor")
	p.ExpectedReductionPct, e3 = queryInt64Ptr(r, "expected_reduction_pct")
	p.RecoverablePct, e4 = queryInt64Ptr(r, "recoverable_pct")
	return p, firstErr(e1, e2, e3, e4)
}

func (s *Server) businessCase(w http.ResponseWriter, r *http.Request) {
	p, err := s.businessCaseParams(r)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	bc, err := s.Service.BusinessCase(r.Context(), auth.ActorFrom(r.Context()), p)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toBusinessCase(bc))
}
