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

// API contract types. These are deliberately separate from the generated
// database models (db/sqlc) and are documented in api/openapi.yaml.

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
	"turnbacks/internal/reporting"
	"turnbacks/internal/service"
)

// List is a paginated collection.
type List[T any] struct {
	Items  []T   `json:"items"`
	Total  int64 `json:"total"`
	Limit  int32 `json:"limit"`
	Offset int32 `json:"offset"`
}

// Settings is the organisation configuration.
type Settings struct {
	OrgName                string    `json:"org_name"`
	Currency               string    `json:"currency"`
	CurrencyExponent       int       `json:"currency_exponent"`
	DefaultHourlyRateMinor int64     `json:"default_hourly_rate_minor"`
	ReportingTimezone      string    `json:"reporting_timezone"`
	RecoverablePct         int32     `json:"recoverable_pct"`
	LargeDurationMinutes   int32     `json:"large_duration_minutes"`
	NoteRetentionDays      *int32    `json:"note_retention_days"`
	ReporterRetentionDays  *int32    `json:"reporter_retention_days"`
	UpdatedAt              time.Time `json:"updated_at"`
	DemoDataPresent        bool      `json:"demo_data_present"`
}

func toSettings(s sqlc.OrgSetting) Settings {
	return Settings{
		OrgName: s.OrgName, Currency: s.Currency, CurrencyExponent: domain.CurrencyExponent(s.Currency),
		DefaultHourlyRateMinor: s.DefaultHourlyRateMinor, ReportingTimezone: s.ReportingTimezone,
		RecoverablePct: s.RecoverablePct, LargeDurationMinutes: s.LargeDurationMinutes,
		NoteRetentionDays: s.NoteRetentionDays, ReporterRetentionDays: s.ReporterRetentionDays,
		UpdatedAt: s.UpdatedAt,
	}
}

// Team is a team.
type Team struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Active          bool      `json:"active"`
	HourlyRateMinor *int64    `json:"hourly_rate_minor"`
	Headcount       *int32    `json:"headcount"`
	IsDemo          bool      `json:"is_demo"`
}

func toTeam(t sqlc.Team) Team {
	return Team{ID: t.ID, Name: t.Name, Active: t.Active, HourlyRateMinor: t.HourlyRateMinor, Headcount: t.Headcount, IsDemo: t.IsDemo}
}

// Category is a problem category.
type Category struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
	SortOrder   int32     `json:"sort_order"`
}

func toCategory(c sqlc.Category) Category {
	return Category{ID: c.ID, Name: c.Name, Description: c.Description, Active: c.Active, SortOrder: c.SortOrder}
}

// Problem is a recurring source of friction.
type Problem struct {
	ID             uuid.UUID  `json:"id"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	CategoryID     uuid.UUID  `json:"category_id"`
	CategoryName   string     `json:"category_name"`
	Status         string     `json:"status"`
	Alias          *string    `json:"alias"`
	ExternalURL    *string    `json:"external_url"`
	MergedIntoID   *uuid.UUID `json:"merged_into_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ResolvedAt     *time.Time `json:"resolved_at"`
	IsDemo         bool       `json:"is_demo"`
	RecentCount    *int64     `json:"recent_count,omitempty"`
	LastOccurredAt *time.Time `json:"last_occurred_at,omitempty"`
}

func toProblem(p sqlc.Problem, categoryName string) Problem {
	return Problem{
		ID: p.ID, Title: p.Title, Description: p.Description, CategoryID: p.CategoryID, CategoryName: categoryName,
		Status: p.Status, Alias: p.Alias, ExternalURL: p.ExternalUrl, MergedIntoID: p.MergedIntoID,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, ResolvedAt: p.ResolvedAt, IsDemo: p.IsDemo,
	}
}

// QuickProblem is a compact problem reference for quick actions.
type QuickProblem struct {
	ID             uuid.UUID  `json:"id"`
	Title          string     `json:"title"`
	Alias          *string    `json:"alias"`
	Status         string     `json:"status"`
	CategoryID     uuid.UUID  `json:"category_id"`
	RecentCount    int64      `json:"recent_count"`
	LastOccurredAt *time.Time `json:"last_occurred_at"`
}

// QuickProblems lists the most frequently reported problems (last 30 days).
type QuickProblems struct {
	Frequent []QuickProblem `json:"frequent"`
}

func toQuick(in []service.QuickProblem) []QuickProblem {
	out := make([]QuickProblem, 0, len(in))
	for _, q := range in {
		out = append(out, QuickProblem{ID: q.ID, Title: q.Title, Alias: q.Alias, Status: q.Status, CategoryID: q.CategoryID, RecentCount: q.RecentCount, LastOccurredAt: q.LastOccurredAt})
	}
	return out
}

// SimilarProblem is a suggestion for a possibly duplicate problem.
type SimilarProblem struct {
	ID     uuid.UUID `json:"id"`
	Title  string    `json:"title"`
	Alias  *string   `json:"alias"`
	Status string    `json:"status"`
	Score  float32   `json:"score"`
}

// MergeResult reports a problem merge.
type MergeResult struct {
	Target            Problem `json:"target"`
	MovedTurnbacks    int64   `json:"moved_turnbacks"`
	MovedRemediations int64   `json:"moved_remediations"`
}

// Turnback is a recorded occurrence. Reporter (an IP address or Slack user)
// is only populated for administrators.
type Turnback struct {
	ID                uuid.UUID  `json:"id"`
	ProblemID         uuid.UUID  `json:"problem_id"`
	ProblemTitle      string     `json:"problem_title"`
	TeamID            *uuid.UUID `json:"team_id"`
	DurationMinutes   int32      `json:"duration_minutes"`
	DurationEstimated bool       `json:"duration_estimated"`
	Note              *string    `json:"note"`
	OccurredAt        time.Time  `json:"occurred_at"`
	SubmittedAt       time.Time  `json:"submitted_at"`
	Source            string     `json:"source"`
	IsDemo            bool       `json:"is_demo"`
	// BatchID links occurrences that were recorded together (quantity > 1).
	BatchID  *uuid.UUID `json:"batch_id"`
	Reporter *string    `json:"reporter,omitempty"`
}

func toTurnback(t sqlc.Turnback, title string, admin bool) Turnback {
	out := Turnback{
		ID: t.ID, ProblemID: t.ProblemID, ProblemTitle: title, TeamID: t.TeamID, DurationMinutes: t.DurationMinutes,
		DurationEstimated: t.DurationEstimated, Note: t.Note, OccurredAt: t.OccurredAt, SubmittedAt: t.SubmittedAt,
		Source: t.Source, IsDemo: t.IsDemo, BatchID: t.BatchID,
	}
	if admin {
		out.Reporter = t.Reporter
	}
	return out
}

// SubmitResult is returned from turnback submission.
type SubmitResult struct {
	Turnback Turnback `json:"turnback"`
	// Replayed is true when this ID had already been recorded with identical
	// content (an idempotent retry); no new record was created.
	Replayed bool `json:"replayed"`
	// Quantity is the number of occurrences recorded by this submission.
	Quantity int `json:"quantity"`
	// UndoToken allows the submitter to delete this submission (and its
	// batch) for a short time. Absent on replays.
	UndoToken string `json:"undo_token,omitempty"`
}

// Period describes a resolved reporting window.
type Period struct {
	Preset   string    `json:"preset"`
	From     string    `json:"from"` // first calendar day (inclusive)
	To       string    `json:"to"`   // last calendar day (inclusive)
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"` // exclusive
	Days     int       `json:"days"`
	Timezone string    `json:"timezone"`
}

func toPeriod(p reporting.Period) Period {
	return Period{Preset: p.Preset, From: p.FirstDay.String(), To: p.LastDay.String(), Start: p.From, End: p.To, Days: p.Days(), Timezone: p.Location.String()}
}

// Totals are aggregate measures.
type Totals struct {
	Turnbacks             int64       `json:"turnbacks"`
	Minutes               int64       `json:"minutes"`
	CostMinor             int64       `json:"cost_minor"`
	DistinctReporters     int64       `json:"distinct_reporters"`
	UnattributedTurnbacks int64       `json:"unattributed_turnbacks"`
	EstimatedTurnbacks    int64       `json:"estimated_turnbacks"`
	DistinctProblems      int64       `json:"distinct_problems"`
	AvgMinutes            json.Number `json:"avg_minutes"`
}

func toTotals(t service.Totals) Totals {
	tenths := t.AvgMinutesTenths()
	return Totals{
		Turnbacks: t.Turnbacks, Minutes: t.Minutes, CostMinor: t.CostMinor, DistinctReporters: t.DistinctReporters,
		UnattributedTurnbacks: t.UnattributedTurnbacks, EstimatedTurnbacks: t.EstimatedTurnbacks, DistinctProblems: t.DistinctProblems,
		AvgMinutes: json.Number(fmt.Sprintf("%d.%d", tenths/10, tenths%10)),
	}
}

// Change holds relative changes in basis points (null when undefined, i.e.
// the baseline is zero).
type Change struct {
	TurnbacksBP *int64 `json:"turnbacks_bp"`
	MinutesBP   *int64 `json:"minutes_bp"`
	CostBP      *int64 `json:"cost_bp"`
}

func bp(before, after int64) *int64 {
	v, ok := domain.ChangeBasisPoints(before, after)
	if !ok {
		return nil
	}
	return &v
}

func toChange(before, after service.Totals) Change {
	return Change{TurnbacksBP: bp(before.Turnbacks, after.Turnbacks), MinutesBP: bp(before.Minutes, after.Minutes), CostBP: bp(before.CostMinor, after.CostMinor)}
}

// Summary is the headline dashboard response.
type Summary struct {
	Period         Period `json:"period"`
	PreviousPeriod Period `json:"previous_period"`
	Current        Totals `json:"current"`
	Previous       Totals `json:"previous"`
	Change         Change `json:"change"`
}

// TimePoint is one time-series bucket.
type TimePoint struct {
	Start     string `json:"start"`
	Turnbacks int64  `json:"turnbacks"`
	Minutes   int64  `json:"minutes"`
	CostMinor int64  `json:"cost_minor"`
}

func toPoints(in []service.TimeBucket) []TimePoint {
	out := make([]TimePoint, 0, len(in))
	for _, b := range in {
		out = append(out, TimePoint{Start: b.Start.Format(time.DateOnly), Turnbacks: b.Turnbacks, Minutes: b.Minutes, CostMinor: b.CostMinor})
	}
	return out
}

// Timeseries is a bucketed time series.
type Timeseries struct {
	Period Period      `json:"period"`
	Bucket string      `json:"bucket"`
	Points []TimePoint `json:"points"`
}

// RankedProblem is one row of a problem ranking / Pareto analysis.
type RankedProblem struct {
	ProblemID         uuid.UUID `json:"problem_id"`
	Title             string    `json:"title"`
	Alias             *string   `json:"alias"`
	Status            string    `json:"status"`
	CategoryName      string    `json:"category_name"`
	Turnbacks         int64     `json:"turnbacks"`
	Minutes           int64     `json:"minutes"`
	CostMinor         int64     `json:"cost_minor"`
	DistinctReporters int64     `json:"distinct_reporters"`
	ShareBP           int32     `json:"share_bp"`
	CumulativeShareBP int32     `json:"cumulative_share_bp"`
}

// Ranking is a ranked list of problems.
type Ranking struct {
	Period Period          `json:"period"`
	Sort   string          `json:"sort"`
	Items  []RankedProblem `json:"items"`
	Total  int64           `json:"total"`
	Limit  int32           `json:"limit"`
	Offset int32           `json:"offset"`
}

// CategoryRow is one category breakdown row.
type CategoryRow struct {
	CategoryID       uuid.UUID `json:"category_id"`
	Name             string    `json:"name"`
	Turnbacks        int64     `json:"turnbacks"`
	Minutes          int64     `json:"minutes"`
	CostMinor        int64     `json:"cost_minor"`
	DistinctProblems int64     `json:"distinct_problems"`
}

// TeamRow is one team breakdown row, including participation so that teams
// with different reporting rates are not naively compared.
type TeamRow struct {
	TeamID            *uuid.UUID `json:"team_id"`
	TeamName          *string    `json:"team_name"`
	Turnbacks         int64      `json:"turnbacks"`
	Minutes           int64      `json:"minutes"`
	CostMinor         int64      `json:"cost_minor"`
	DistinctReporters int64      `json:"distinct_reporters"`
	Headcount         *int32     `json:"headcount"`
	ParticipationBP   *int64     `json:"participation_bp"`
}

// Breakdown wraps breakdown rows with their period.
type Breakdown[T any] struct {
	Period Period `json:"period"`
	Items  []T    `json:"items"`
}

// Remediation is a recorded improvement.
type Remediation struct {
	ID                      uuid.UUID `json:"id"`
	ProblemID               uuid.UUID `json:"problem_id"`
	ImplementedOn           string    `json:"implemented_on"`
	Description             string    `json:"description"`
	ImplementationCostMinor int64     `json:"implementation_cost_minor"`
	AnnualCostMinor         int64     `json:"annual_cost_minor"`
	ExpectedReductionPct    int32     `json:"expected_reduction_pct"`
	CreatedAt               time.Time `json:"created_at"`
	IsDemo                  bool      `json:"is_demo"`
}

func toRemediation(r sqlc.Remediation) Remediation {
	return Remediation{
		ID: r.ID, ProblemID: r.ProblemID, ImplementedOn: r.ImplementedOn.Format(time.DateOnly), Description: r.Description,
		ImplementationCostMinor: r.ImplementationCostMinor, AnnualCostMinor: r.AnnualCostMinor,
		ExpectedReductionPct: r.ExpectedReductionPct, CreatedAt: r.CreatedAt, IsDemo: r.IsDemo,
	}
}

// WindowTotals pairs a window with its totals.
type WindowTotals struct {
	Period Period `json:"period"`
	Totals Totals `json:"totals"`
}

// Comparison is an observational before/after comparison.
type Comparison struct {
	Remediation      Remediation  `json:"remediation"`
	ProblemTitle     string       `json:"problem_title"`
	WindowDays       int          `json:"window_days"`
	InsufficientData bool         `json:"insufficient_data"`
	Before           WindowTotals `json:"before"`
	After            WindowTotals `json:"after"`
	Change           Change       `json:"change"`
	Caveat           string       `json:"caveat"`
}

const comparisonCaveat = "Observational comparison of equal-length windows before and after the remediation date. " +
	"Changes in reporting behaviour, staffing, seasonality or other events may also explain differences; this does not establish causation."

func toComparison(c service.RemediationComparison) Comparison {
	return Comparison{
		Remediation: toRemediation(c.Remediation), ProblemTitle: c.ProblemTitle, WindowDays: c.Windows.WindowDays,
		InsufficientData: c.Windows.Insufficient,
		Before:           WindowTotals{Period: toPeriod(c.Windows.Before), Totals: toTotals(c.Before)},
		After:            WindowTotals{Period: toPeriod(c.Windows.After), Totals: toTotals(c.After)},
		Change:           toChange(c.Before, c.After), Caveat: comparisonCaveat,
	}
}

// Assumptions are the adjustable business-case inputs.
type Assumptions struct {
	ImplementationCostMinor int64  `json:"implementation_cost_minor"`
	AnnualCostMinor         int64  `json:"annual_cost_minor"`
	ExpectedReductionPct    int64  `json:"expected_reduction_pct"`
	RecoverablePct          int64  `json:"recoverable_pct"`
	Source                  string `json:"source"`
}

// Projection is the business-case projection. Money in minor units.
type Projection struct {
	AnnualisedTurnbacks     int64        `json:"annualised_turnbacks"`
	AnnualisedMinutes       int64        `json:"annualised_minutes"`
	AnnualisedCostMinor     int64        `json:"annualised_cost_minor"`
	MonthlyTurnbacks        int64        `json:"monthly_turnbacks"`
	MonthlyMinutes          int64        `json:"monthly_minutes"`
	MonthlyCostMinor        int64        `json:"monthly_cost_minor"`
	GrossAnnualBenefitMinor int64        `json:"gross_annual_benefit_minor"`
	NetAnnualBenefitMinor   int64        `json:"net_annual_benefit_minor"`
	FirstYearNetMinor       int64        `json:"first_year_net_minor"`
	ThreeYearNetMinor       int64        `json:"three_year_net_minor"`
	PaybackMonths           *json.Number `json:"payback_months"`
}

// Note is an anonymised representative note.
type Note struct {
	Note            string    `json:"note"`
	OccurredAt      time.Time `json:"occurred_at"`
	DurationMinutes int32     `json:"duration_minutes"`
}

// BusinessCase is the full business case for a problem.
type BusinessCase struct {
	Problem                Problem      `json:"problem"`
	Period                 Period       `json:"period"`
	Totals                 Totals       `json:"totals"`
	Assumptions            Assumptions  `json:"assumptions"`
	Projection             Projection   `json:"projection"`
	Trend                  []TimePoint  `json:"trend"`
	Notes                  []Note       `json:"notes"`
	Remediations           []Comparison `json:"remediations"`
	OrgName                string       `json:"org_name"`
	Currency               string       `json:"currency"`
	CurrencyExponent       int          `json:"currency_exponent"`
	DefaultHourlyRateMinor int64        `json:"default_hourly_rate_minor"`
	GeneratedAt            time.Time    `json:"generated_at"`
	Methodology            []string     `json:"methodology"`
}

func toBusinessCase(bc service.BusinessCase) BusinessCase {
	pr := bc.Projection
	out := BusinessCase{
		Problem: toProblem(bc.Problem.Problem, bc.Problem.CategoryName), Period: toPeriod(bc.Period), Totals: toTotals(bc.Totals),
		Assumptions: Assumptions{
			ImplementationCostMinor: bc.Assumptions.ImplementationCostMinor, AnnualCostMinor: bc.Assumptions.AnnualCostMinor,
			ExpectedReductionPct: bc.Assumptions.ExpectedReductionPct, RecoverablePct: bc.Assumptions.RecoverablePct, Source: bc.AssumptionsSource,
		},
		Projection: Projection{
			AnnualisedTurnbacks: pr.AnnualisedTurnbacks, AnnualisedMinutes: pr.AnnualisedMinutes, AnnualisedCostMinor: pr.AnnualisedCostMinor,
			MonthlyTurnbacks: pr.MonthlyTurnbacks, MonthlyMinutes: pr.MonthlyMinutes, MonthlyCostMinor: pr.MonthlyCostMinor,
			GrossAnnualBenefitMinor: pr.GrossAnnualBenefitMinor, NetAnnualBenefitMinor: pr.NetAnnualBenefitMinor,
			FirstYearNetMinor: pr.FirstYearNetMinor, ThreeYearNetMinor: pr.ThreeYearNetMinor,
		},
		Trend: toPoints(bc.Trend), Notes: []Note{}, Remediations: []Comparison{},
		OrgName: bc.OrgName, Currency: bc.Currency, CurrencyExponent: domain.CurrencyExponent(bc.Currency),
		DefaultHourlyRateMinor: bc.DefaultRateMinor, GeneratedAt: bc.GeneratedAt, Methodology: methodology,
	}
	if pr.PaybackAchievable {
		n := json.Number(fmt.Sprintf("%d.%d", pr.PaybackMonthsTenths/10, pr.PaybackMonthsTenths%10))
		out.Projection.PaybackMonths = &n
	}
	for _, n := range bc.Notes {
		out.Notes = append(out.Notes, Note{Note: n.Note, OccurredAt: n.OccurredAt, DurationMinutes: n.DurationMinutes})
	}
	for _, c := range bc.Remediations {
		out.Remediations = append(out.Remediations, toComparison(c))
	}
	return out
}

var methodology = []string{
	"Occurrences are individual self-reported turnbacks; they are voluntary and under-reporting is likely. Missing reports are not evidence of zero disruption.",
	"People affected counts distinct reporters: one network address (each person has their own VPN address) or one Slack user.",
	"Time lost is the sum of reported durations. Many durations are estimates chosen from presets.",
	"Estimated productivity cost = reported hours lost × fully loaded hourly employment cost (team-specific rate where configured, otherwise the organisation default), calculated with exact decimal arithmetic and rounded once.",
	"Estimated productivity cost is not cash expenditure and is not guaranteed to be recoverable as savings.",
	"Annualised figures linearly extrapolate the observation period to 365 days; monthly figures are annualised figures divided by 12.",
	"Projected gross annual benefit = annualised estimated cost × expected reduction in occurrences × share of lost time assumed recoverable.",
	"Projected net annual benefit = gross benefit − ongoing annual cost of the remediation. Payback period = implementation cost ÷ (net annual benefit ÷ 12).",
	"Before/after remediation comparisons use equal-length windows either side of the implementation date and are observational, not causal.",
}
