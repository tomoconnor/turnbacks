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
	"encoding/csv"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"turnbacks/db/sqlc"
	"turnbacks/internal/auth"
	"turnbacks/internal/domain"
	"turnbacks/internal/service"
)

// csvSafe neutralises spreadsheet formula injection in user-supplied text.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

var filenameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func startCSV(w http.ResponseWriter, name string) *csv.Writer {
	name = filenameUnsafe.ReplaceAllString(name, "-")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, name))
	return csv.NewWriter(w)
}

func i64(v int64) string { return strconv.FormatInt(v, 10) }

func hours(minutes int64) string { return domain.FormatMinor(domain.ScaleMinor(minutes, 100, 60), 2) }

func pct(bp int64) string { return domain.FormatMinor(bp, 2) }

// analyticsExport streams CSV exports. The reporter column (IP address or
// Slack user) is included in the occurrence export for administrators only.
func (s *Server) analyticsExport(w http.ResponseWriter, r *http.Request) {
	a := auth.ActorFrom(r.Context())
	f, err := s.filter(r)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	st, _, err := s.Service.Settings(r.Context())
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	exp := domain.CurrencyExponent(st.Currency)
	money := func(v int64) string { return domain.FormatMinor(v, exp) }
	cur := st.Currency
	suffix := f.Period.FirstDay.String() + "_" + f.Period.LastDay.String()
	ds := r.URL.Query().Get("dataset")

	switch ds {
	case "problems":
		rows, _, err := s.Service.ProblemRanking(r.Context(), a, f, r.URL.Query().Get("sort"), service.Page{Limit: 10000})
		if err != nil {
			s.errs.respond(w, r, err)
			return
		}
		cw := startCSV(w, "turnbacks-problems_"+suffix)
		_ = cw.Write([]string{"problem_id", "title", "category", "status", "turnbacks", "hours_lost", "estimated_cost_" + cur, "distinct_reporters", "share_pct", "cumulative_share_pct"})
		for _, p := range rows {
			_ = cw.Write([]string{p.ID.String(), csvSafe(p.Title), csvSafe(p.CategoryName), p.Status, i64(p.Turnbacks), hours(p.Minutes), money(p.CostMinor), i64(p.DistinctReporters), pct(int64(p.ShareBp)), pct(int64(p.CumulativeShareBp))})
		}
		cw.Flush()
	case "timeseries":
		bucket := r.URL.Query().Get("bucket")
		pts, err := s.Service.AnalyticsTimeseries(r.Context(), a, f, bucket)
		if err != nil {
			s.errs.respond(w, r, err)
			return
		}
		cw := startCSV(w, "turnbacks-timeseries_"+suffix)
		_ = cw.Write([]string{"bucket_start", "turnbacks", "hours_lost", "estimated_cost_" + cur})
		for _, p := range pts {
			_ = cw.Write([]string{p.Start.Format(time.DateOnly), i64(p.Turnbacks), hours(p.Minutes), money(p.CostMinor)})
		}
		cw.Flush()
	case "categories":
		rows, err := s.Service.CategoryBreakdown(r.Context(), a, f)
		if err != nil {
			s.errs.respond(w, r, err)
			return
		}
		cw := startCSV(w, "turnbacks-categories_"+suffix)
		_ = cw.Write([]string{"category", "turnbacks", "hours_lost", "estimated_cost_" + cur, "distinct_problems"})
		for _, c := range rows {
			_ = cw.Write([]string{csvSafe(c.Name), i64(c.Turnbacks), hours(c.Minutes), money(c.CostMinor), i64(c.DistinctProblems)})
		}
		cw.Flush()
	case "teams":
		rows, err := s.Service.TeamBreakdown(r.Context(), a, f)
		if err != nil {
			s.errs.respond(w, r, err)
			return
		}
		cw := startCSV(w, "turnbacks-teams_"+suffix)
		_ = cw.Write([]string{"team", "turnbacks", "hours_lost", "estimated_cost_" + cur, "distinct_reporters", "headcount"})
		for _, t := range rows {
			name := "(no team)"
			if t.TeamName != nil {
				name = *t.TeamName
			}
			headcount := ""
			if t.Headcount != nil {
				headcount = strconv.Itoa(int(*t.Headcount))
			}
			_ = cw.Write([]string{csvSafe(name), i64(t.Turnbacks), hours(t.Minutes), money(t.CostMinor), i64(t.DistinctReporters), headcount})
		}
		cw.Flush()
	case "turnbacks":
		cw := startCSV(w, "turnbacks-occurrences_"+suffix)
		header := []string{"turnback_id", "occurred_at_utc", "problem", "team", "duration_minutes", "duration_estimated", "source", "batch_id", "note"}
		if a.Admin {
			header = append(header, "reporter")
		}
		_ = cw.Write(header)
		const pageSize = 1000
		for off := int32(0); ; off += pageSize {
			rows, _, err := s.Service.ListTurnbacks(r.Context(), a, f, "", service.Page{Limit: pageSize, Offset: off})
			if err != nil {
				s.Log.ErrorContext(r.Context(), "export failed mid-stream", "error", err)
				return
			}
			for _, t := range rows {
				team, note := "", ""
				if t.TeamName != nil {
					team = *t.TeamName
				}
				if t.Note != nil {
					note = *t.Note
				}
				batch, reporter := "", ""
				if t.BatchID != nil {
					batch = t.BatchID.String()
				}
				if t.Reporter != nil {
					reporter = *t.Reporter
				}
				row := []string{t.ID.String(), t.OccurredAt.UTC().Format(time.RFC3339), csvSafe(t.ProblemTitle), csvSafe(team),
					strconv.Itoa(int(t.DurationMinutes)), strconv.FormatBool(t.DurationEstimated), t.Source, batch, csvSafe(note)}
				if a.Admin {
					row = append(row, csvSafe(reporter))
				}
				_ = cw.Write(row)
			}
			if len(rows) < pageSize {
				break
			}
		}
		cw.Flush()
	default:
		s.errs.respond(w, r, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid dataset",
			Fields: []domain.FieldError{{Field: "dataset", Message: "must be problems, timeseries, categories, teams or turnbacks"}}})
	}
}

func (s *Server) businessCaseCSV(w http.ResponseWriter, r *http.Request) {
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
	exp := domain.CurrencyExponent(bc.Currency)
	money := func(v int64) string { return domain.FormatMinor(v, exp) }
	pr := bc.Projection
	cw := startCSV(w, "business-case_"+bc.Problem.Problem.Title)
	rows := [][]string{
		{"section", "metric", "value", "unit"},
		{"problem", "title", csvSafe(bc.Problem.Problem.Title), ""},
		{"problem", "category", csvSafe(bc.Problem.CategoryName), ""},
		{"problem", "status", bc.Problem.Problem.Status, ""},
		{"observation", "period_start", bc.Period.FirstDay.String(), "date"},
		{"observation", "period_end", bc.Period.LastDay.String(), "date"},
		{"observation", "days", strconv.Itoa(bc.Period.Days()), "days"},
		{"observation", "reported_occurrences", i64(bc.Totals.Turnbacks), "count"},
		{"observation", "people_affected", i64(bc.Totals.DistinctReporters), "count"},
		{"observation", "unattributed_occurrences", i64(bc.Totals.UnattributedTurnbacks), "count"},
		{"observation", "reported_hours_lost", hours(bc.Totals.Minutes), "hours"},
		{"observation", "estimated_productivity_cost", money(bc.Totals.CostMinor), bc.Currency},
		{"projection", "monthly_estimated_cost", money(pr.MonthlyCostMinor), bc.Currency},
		{"projection", "annualised_occurrences", i64(pr.AnnualisedTurnbacks), "count"},
		{"projection", "annualised_hours_lost", hours(pr.AnnualisedMinutes), "hours"},
		{"projection", "annualised_estimated_cost", money(pr.AnnualisedCostMinor), bc.Currency},
		{"assumption", "implementation_cost", money(bc.Assumptions.ImplementationCostMinor), bc.Currency},
		{"assumption", "ongoing_annual_cost", money(bc.Assumptions.AnnualCostMinor), bc.Currency},
		{"assumption", "expected_reduction", strconv.FormatInt(bc.Assumptions.ExpectedReductionPct, 10), "percent"},
		{"assumption", "recoverable_share_of_time", strconv.FormatInt(bc.Assumptions.RecoverablePct, 10), "percent"},
		{"assumption", "default_hourly_rate", money(bc.DefaultRateMinor), bc.Currency},
		{"projection", "gross_annual_productivity_benefit", money(pr.GrossAnnualBenefitMinor), bc.Currency},
		{"projection", "net_annual_productivity_benefit", money(pr.NetAnnualBenefitMinor), bc.Currency},
		{"projection", "first_year_net", money(pr.FirstYearNetMinor), bc.Currency},
		{"projection", "three_year_net", money(pr.ThreeYearNetMinor), bc.Currency},
	}
	if pr.PaybackAchievable {
		rows = append(rows, []string{"projection", "payback_period", fmt.Sprintf("%d.%d", pr.PaybackMonthsTenths/10, pr.PaybackMonthsTenths%10), "months"})
	} else {
		rows = append(rows, []string{"projection", "payback_period", "not achieved", ""})
	}
	for _, t := range bc.Trend {
		rows = append(rows, []string{"trend", t.Start.Format("2006-01"), i64(t.Turnbacks) + " occurrences; " + hours(t.Minutes) + " h; " + money(t.CostMinor) + " " + bc.Currency, "month"})
	}
	for _, m := range methodology {
		rows = append(rows, []string{"methodology", "", m, ""})
	}
	_ = cw.WriteAll(rows)
}

func optStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func optTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// fullExport streams complete, unfiltered datasets for analysis elsewhere or
// migration. Reporter columns are included for administrators only.
func (s *Server) fullExport(w http.ResponseWriter, r *http.Request) {
	a := auth.ActorFrom(r.Context())
	ds := chi.URLParam(r, "dataset")
	day := time.Now().UTC().Format(time.DateOnly)
	switch ds {
	case "turnbacks":
		cw := startCSV(w, "turnbacks-all-turnbacks_"+day)
		header := []string{"turnback_id", "occurred_at_utc", "submitted_at_utc", "problem_id", "problem", "category", "team",
			"duration_minutes", "duration_estimated", "source", "batch_id", "note", "is_demo"}
		if a.Admin {
			header = append(header, "reporter")
		}
		_ = cw.Write(header)
		err := s.Service.ExportTurnbacks(r.Context(), a, func(t sqlc.ExportTurnbacksRow) error {
			batch := ""
			if t.BatchID != nil {
				batch = t.BatchID.String()
			}
			row := []string{t.ID.String(), t.OccurredAt.UTC().Format(time.RFC3339), t.SubmittedAt.UTC().Format(time.RFC3339),
				t.ProblemID.String(), csvSafe(t.ProblemTitle), csvSafe(t.CategoryName), csvSafe(optStr(t.TeamName)),
				strconv.Itoa(int(t.DurationMinutes)), strconv.FormatBool(t.DurationEstimated), t.Source, batch,
				csvSafe(optStr(t.Note)), strconv.FormatBool(t.IsDemo)}
			if a.Admin {
				row = append(row, csvSafe(optStr(t.Reporter)))
			}
			return cw.Write(row)
		})
		if err != nil {
			s.Log.ErrorContext(r.Context(), "export failed mid-stream", "error", err)
		}
		cw.Flush()
	case "problems":
		rows, err := s.Service.ExportProblems(r.Context())
		if err != nil {
			s.errs.respond(w, r, err)
			return
		}
		cw := startCSV(w, "turnbacks-all-problems_"+day)
		_ = cw.Write([]string{"problem_id", "title", "description", "category", "status", "alias", "external_url",
			"merged_into_id", "created_at_utc", "updated_at_utc", "resolved_at_utc", "turnbacks_all_time", "is_demo"})
		for _, p := range rows {
			merged := ""
			if p.MergedIntoID != nil {
				merged = p.MergedIntoID.String()
			}
			_ = cw.Write([]string{p.ID.String(), csvSafe(p.Title), csvSafe(p.Description), csvSafe(p.CategoryName), p.Status,
				optStr(p.Alias), csvSafe(optStr(p.ExternalUrl)), merged, p.CreatedAt.UTC().Format(time.RFC3339),
				p.UpdatedAt.UTC().Format(time.RFC3339), optTime(p.ResolvedAt), i64(p.TurnbacksAllTime), strconv.FormatBool(p.IsDemo)})
		}
		cw.Flush()
	case "remediations":
		rows, err := s.Service.ExportRemediations(r.Context())
		if err != nil {
			s.errs.respond(w, r, err)
			return
		}
		st, _, err := s.Service.Settings(r.Context())
		if err != nil {
			s.errs.respond(w, r, err)
			return
		}
		exp := domain.CurrencyExponent(st.Currency)
		cw := startCSV(w, "turnbacks-all-remediations_"+day)
		_ = cw.Write([]string{"remediation_id", "problem_id", "problem", "implemented_on", "description",
			"implementation_cost_" + st.Currency, "annual_cost_" + st.Currency, "expected_reduction_pct", "created_at_utc", "is_demo"})
		for _, x := range rows {
			rm := x.Remediation
			_ = cw.Write([]string{rm.ID.String(), rm.ProblemID.String(), csvSafe(x.ProblemTitle), rm.ImplementedOn.Format(time.DateOnly),
				csvSafe(rm.Description), domain.FormatMinor(rm.ImplementationCostMinor, exp), domain.FormatMinor(rm.AnnualCostMinor, exp),
				strconv.Itoa(int(rm.ExpectedReductionPct)), rm.CreatedAt.UTC().Format(time.RFC3339), strconv.FormatBool(rm.IsDemo)})
		}
		cw.Flush()
	case "changes":
		rows, err := s.Service.ExportMarkers(r.Context(), a)
		if err != nil {
			s.errs.respond(w, r, err)
			return
		}
		cw := startCSV(w, "turnbacks-all-changes_"+day)
		header := []string{"change_id", "occurred_at_utc", "title", "description", "url", "problem_id", "problem", "source", "created_at_utc", "is_demo"}
		if a.Admin {
			header = append(header, "created_by")
		}
		_ = cw.Write(header)
		for _, x := range rows {
			m := x.ChangeMarker
			pid := ""
			if m.ProblemID != nil {
				pid = m.ProblemID.String()
			}
			row := []string{m.ID.String(), m.OccurredAt.UTC().Format(time.RFC3339), csvSafe(m.Title), csvSafe(m.Description),
				csvSafe(optStr(m.Url)), pid, csvSafe(optStr(x.ProblemTitle)), m.Source, m.CreatedAt.UTC().Format(time.RFC3339),
				strconv.FormatBool(m.IsDemo)}
			if a.Admin {
				row = append(row, csvSafe(optStr(m.CreatedBy)))
			}
			_ = cw.Write(row)
		}
		cw.Flush()
	default:
		s.errs.respond(w, r, domain.NotFound("export dataset"))
	}
}
