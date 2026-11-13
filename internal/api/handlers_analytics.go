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
	"turnbacks/internal/service"
)

// filter parses the common analytics query parameters.
func (s *Server) filter(r *http.Request) (service.AnalyticsFilter, error) {
	q := r.URL.Query()
	var f service.AnalyticsFilter
	var err error
	if f.Period, err = s.Service.ResolvePeriod(r.Context(), q.Get("period"), q.Get("from"), q.Get("to")); err != nil {
		return f, err
	}
	var e1, e2, e3 error
	f.ProblemID, e1 = queryUUID(r, "problem_id")
	f.CategoryID, e2 = queryUUID(r, "category_id")
	f.TeamID, e3 = queryUUID(r, "team_id")
	return f, firstErr(e1, e2, e3)
}

func (s *Server) analyticsSummary(w http.ResponseWriter, r *http.Request) {
	f, err := s.filter(r)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	sum, err := s.Service.AnalyticsSummary(r.Context(), auth.ActorFrom(r.Context()), f)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, Summary{
		Period: toPeriod(sum.Period), PreviousPeriod: toPeriod(sum.Previous),
		Current: toTotals(sum.Current), Previous: toTotals(sum.Prior), Change: toChange(sum.Prior, sum.Current),
	})
}

func (s *Server) analyticsTimeseries(w http.ResponseWriter, r *http.Request) {
	f, err := s.filter(r)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = f.Period.DefaultBucket()
	}
	pts, err := s.Service.AnalyticsTimeseries(r.Context(), auth.ActorFrom(r.Context()), f, bucket)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, Timeseries{Period: toPeriod(f.Period), Bucket: bucket, Points: toPoints(pts)})
}

func (s *Server) analyticsProblems(w http.ResponseWriter, r *http.Request) {
	f, err := s.filter(r)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	limit, err1 := queryInt(r, "limit", 10)
	offset, err2 := queryInt(r, "offset", 0)
	if err := firstErr(err1, err2); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	p := service.NormalisePage(limit, offset, 10, 200)
	sortBy := r.URL.Query().Get("sort")
	rows, total, err := s.Service.ProblemRanking(r.Context(), auth.ActorFrom(r.Context()), f, sortBy, p)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	if sortBy == "" {
		sortBy = service.SortByCost
	}
	out := Ranking{Period: toPeriod(f.Period), Sort: sortBy, Items: make([]RankedProblem, 0, len(rows)), Total: total, Limit: p.Limit, Offset: p.Offset}
	for _, row := range rows {
		out.Items = append(out.Items, RankedProblem{
			ProblemID: row.ID, Title: row.Title, Alias: row.Alias, Status: row.Status, CategoryName: row.CategoryName,
			Turnbacks: row.Turnbacks, Minutes: row.Minutes, CostMinor: row.CostMinor, DistinctReporters: row.DistinctReporters,
			ShareBP: row.ShareBp, CumulativeShareBP: row.CumulativeShareBp,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) analyticsCategories(w http.ResponseWriter, r *http.Request) {
	f, err := s.filter(r)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	rows, err := s.Service.CategoryBreakdown(r.Context(), auth.ActorFrom(r.Context()), f)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := Breakdown[CategoryRow]{Period: toPeriod(f.Period), Items: make([]CategoryRow, 0, len(rows))}
	for _, c := range rows {
		out.Items = append(out.Items, CategoryRow{CategoryID: c.ID, Name: c.Name, Turnbacks: c.Turnbacks, Minutes: c.Minutes, CostMinor: c.CostMinor, DistinctProblems: c.DistinctProblems})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) analyticsTeams(w http.ResponseWriter, r *http.Request) {
	f, err := s.filter(r)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	rows, err := s.Service.TeamBreakdown(r.Context(), auth.ActorFrom(r.Context()), f)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := Breakdown[TeamRow]{Period: toPeriod(f.Period), Items: make([]TeamRow, 0, len(rows))}
	for _, t := range rows {
		row := TeamRow{TeamID: t.TeamID, TeamName: t.TeamName, Turnbacks: t.Turnbacks, Minutes: t.Minutes, CostMinor: t.CostMinor,
			DistinctReporters: t.DistinctReporters, Headcount: t.Headcount}
		if t.TeamID != nil && t.Headcount != nil && *t.Headcount > 0 {
			v := domain.ScaleMinor(t.DistinctReporters, 10000, int64(*t.Headcount))
			row.ParticipationBP = &v
		}
		out.Items = append(out.Items, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) analyticsRemediations(w http.ResponseWriter, r *http.Request) {
	window, err := queryInt(r, "window_days", 90)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	rows, err := s.Service.RemediationOverview(r.Context(), auth.ActorFrom(r.Context()), window)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]Comparison, 0, len(rows))
	for _, c := range rows {
		out = append(out, toComparison(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
