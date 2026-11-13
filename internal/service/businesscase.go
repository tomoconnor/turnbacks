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
	"regexp"
	"time"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
	"turnbacks/internal/reporting"
)

// Default assumptions used when no remediation has been recorded.
const defaultExpectedReductionPct = 50

// BusinessCaseParams requests a business case. Nil assumption fields fall
// back to the latest remediation record, then to defaults.
type BusinessCaseParams struct {
	ProblemID               uuid.UUID
	Period                  *reporting.Period
	ImplementationCostMinor *int64
	AnnualCostMinor         *int64
	ExpectedReductionPct    *int64
	RecoverablePct          *int64
}

// BusinessCaseNote is an anonymised representative note.
type BusinessCaseNote struct {
	Note            string
	OccurredAt      time.Time
	DurationMinutes int32
}

// BusinessCase is the full business case for one problem.
type BusinessCase struct {
	Problem           sqlc.GetProblemRow
	Period            reporting.Period
	Totals            Totals
	Assumptions       reporting.Assumptions
	AssumptionsSource string // "custom", "remediation" or "defaults"
	Projection        reporting.Projection
	Trend             []TimeBucket
	Notes             []BusinessCaseNote
	Remediations      []RemediationComparison
	Currency          string
	DefaultRateMinor  int64
	OrgName           string
	GeneratedAt       time.Time
}

var (
	emailRe   = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	mentionRe = regexp.MustCompile(`(^|\s)@[A-Za-z0-9._\-]+`)
)

// anonymiseNote strips obvious personal identifiers from free text.
func anonymiseNote(s string) string {
	s = emailRe.ReplaceAllString(s, "[email]")
	s = mentionRe.ReplaceAllString(s, "$1@[someone]")
	return s
}

// BusinessCase builds a business case for a problem.
func (s *Service) BusinessCase(ctx context.Context, a domain.Actor, p BusinessCaseParams) (BusinessCase, error) {
	var bc BusinessCase
	settings, loc, err := s.Settings(ctx)
	if err != nil {
		return bc, err
	}
	bc.Currency, bc.DefaultRateMinor, bc.OrgName, bc.GeneratedAt = settings.Currency, settings.DefaultHourlyRateMinor, settings.OrgName, s.now().UTC()

	if bc.Problem, err = s.store.GetProblem(ctx, p.ProblemID); err != nil {
		return bc, notFoundOr(err, "problem")
	}

	// Observation period: explicit, or from the first occurrence (at most one
	// year back) to today.
	if p.Period != nil {
		bc.Period = *p.Period
	} else {
		today := reporting.DateOf(s.now(), loc)
		first := today.AddDays(-89)
		fo, err := s.store.FirstOccurrenceForProblem(ctx, p.ProblemID)
		if err != nil {
			return bc, err
		}
		if t := asTimePtr(fo); t != nil {
			first = reporting.DateOf(*t, loc)
			if earliest := today.AddDays(-364); first.Before(earliest) {
				first = earliest
			}
		}
		bc.Period = reporting.NewPeriod(reporting.PresetCustom, first, today, loc)
	}

	f := AnalyticsFilter{Period: bc.Period, ProblemID: &p.ProblemID}
	if bc.Totals, err = s.totals(ctx, bc.Period.From, bc.Period.To, f); err != nil {
		return bc, err
	}
	if bc.Trend, err = s.timeseries(ctx, f, reporting.BucketMonth); err != nil {
		return bc, err
	}
	notes, err := s.store.ListProblemNotes(ctx, sqlc.ListProblemNotesParams{ProblemID: p.ProblemID, FromTs: bc.Period.From, ToTs: bc.Period.To, Lim: 8})
	if err != nil {
		return bc, err
	}
	for _, n := range notes {
		bc.Notes = append(bc.Notes, BusinessCaseNote{Note: anonymiseNote(n.Note), OccurredAt: n.OccurredAt, DurationMinutes: n.DurationMinutes})
	}

	rems, err := s.store.ListRemediationsForProblem(ctx, p.ProblemID)
	if err != nil {
		return bc, err
	}
	for _, r := range rems {
		c, err := s.compare(ctx, r, bc.Problem.Problem.Title, reporting.DefaultComparisonWindowDays, loc)
		if err != nil {
			return bc, err
		}
		bc.Remediations = append(bc.Remediations, c)
	}

	// Assumptions.
	as := reporting.Assumptions{ExpectedReductionPct: defaultExpectedReductionPct, RecoverablePct: int64(settings.RecoverablePct)}
	bc.AssumptionsSource = "defaults"
	if len(rems) > 0 {
		latest := rems[0]
		as.ImplementationCostMinor = latest.ImplementationCostMinor
		as.AnnualCostMinor = latest.AnnualCostMinor
		as.ExpectedReductionPct = int64(latest.ExpectedReductionPct)
		bc.AssumptionsSource = "remediation"
	}
	custom := false
	if p.ImplementationCostMinor != nil {
		as.ImplementationCostMinor, custom = *p.ImplementationCostMinor, true
	}
	if p.AnnualCostMinor != nil {
		as.AnnualCostMinor, custom = *p.AnnualCostMinor, true
	}
	if p.ExpectedReductionPct != nil {
		as.ExpectedReductionPct, custom = *p.ExpectedReductionPct, true
	}
	if p.RecoverablePct != nil {
		as.RecoverablePct, custom = *p.RecoverablePct, true
	}
	if custom {
		bc.AssumptionsSource = "custom"
	}
	if err := as.Validate(); err != nil {
		return bc, err
	}
	bc.Assumptions = as
	bc.Projection = reporting.Project(reporting.Observation{
		Days: bc.Period.Days(), Turnbacks: bc.Totals.Turnbacks, Minutes: bc.Totals.Minutes,
		CostMinor: bc.Totals.CostMinor, DistinctReporters: bc.Totals.DistinctReporters,
	}, as)
	return bc, nil
}
