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
	"math/big"
	"time"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
	"turnbacks/internal/reporting"
)

// AnalyticsFilter scopes analytics queries.
type AnalyticsFilter struct {
	Period     reporting.Period
	ProblemID  *uuid.UUID
	CategoryID *uuid.UUID
	TeamID     *uuid.UUID
}

// Totals are aggregate figures for a window.
type Totals struct {
	Turnbacks             int64
	Minutes               int64
	CostMinor             int64
	DistinctReporters     int64
	UnattributedTurnbacks int64
	EstimatedTurnbacks    int64
	DistinctProblems      int64
}

// AvgMinutesTenths returns the mean duration per occurrence in tenths of a
// minute, or 0 when there are no occurrences.
func (t Totals) AvgMinutesTenths() int64 {
	if t.Turnbacks == 0 {
		return 0
	}
	return domain.RoundRat(big.NewRat(t.Minutes*10, t.Turnbacks))
}

// ResolvePeriod resolves a reporting period in the organisation timezone.
func (s *Service) ResolvePeriod(ctx context.Context, preset, from, to string) (reporting.Period, error) {
	_, loc, err := s.Settings(ctx)
	if err != nil {
		return reporting.Period{}, err
	}
	return reporting.ResolvePeriod(preset, from, to, s.now(), loc)
}

func (s *Service) totals(ctx context.Context, from, to time.Time, f AnalyticsFilter) (Totals, error) {
	r, err := s.store.AnalyticsSummary(ctx, sqlc.AnalyticsSummaryParams{
		FromTs: from, ToTs: to, ProblemID: f.ProblemID, CategoryID: f.CategoryID, TeamID: f.TeamID,
	})
	if err != nil {
		return Totals{}, err
	}
	return Totals{
		Turnbacks: r.Turnbacks, Minutes: r.Minutes, CostMinor: r.CostMinor, DistinctReporters: r.DistinctReporters,
		UnattributedTurnbacks: r.UnattributedTurnbacks, EstimatedTurnbacks: r.EstimatedTurnbacks, DistinctProblems: r.DistinctProblems,
	}, nil
}

// Summary is the headline dashboard result.
type Summary struct {
	Period   reporting.Period
	Previous reporting.Period
	Current  Totals
	Prior    Totals
}

// AnalyticsSummary returns headline metrics with the preceding equivalent period.
func (s *Service) AnalyticsSummary(ctx context.Context, a domain.Actor, f AnalyticsFilter) (Summary, error) {
	res := Summary{Period: f.Period, Previous: f.Period.Previous()}
	var err error
	if res.Current, err = s.totals(ctx, f.Period.From, f.Period.To, f); err != nil {
		return res, err
	}
	res.Prior, err = s.totals(ctx, res.Previous.From, res.Previous.To, f)
	return res, err
}

// TimeBucket is one point of a time series.
type TimeBucket struct {
	Start     time.Time // calendar date of bucket start in the reporting timezone
	Turnbacks int64
	Minutes   int64
	CostMinor int64
}

// AnalyticsTimeseries returns totals bucketed by day, week or month.
func (s *Service) AnalyticsTimeseries(ctx context.Context, a domain.Actor, f AnalyticsFilter, bucket string) ([]TimeBucket, error) {
	return s.timeseries(ctx, f, bucket)
}

func (s *Service) timeseries(ctx context.Context, f AnalyticsFilter, bucket string) ([]TimeBucket, error) {
	if bucket == "" {
		bucket = f.Period.DefaultBucket()
	}
	if !reporting.ValidBucket(bucket) {
		return nil, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid bucket",
			Fields: []domain.FieldError{{Field: "bucket", Message: "must be day, week or month"}}}
	}
	rows, err := s.store.AnalyticsTimeseries(ctx, sqlc.AnalyticsTimeseriesParams{
		Bucket: bucket, Tz: f.Period.Location.String(), FromTs: f.Period.From, ToTs: f.Period.To,
		ProblemID: f.ProblemID, CategoryID: f.CategoryID, TeamID: f.TeamID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]TimeBucket, 0, len(rows))
	for _, r := range rows {
		out = append(out, TimeBucket{Start: r.BucketDate, Turnbacks: r.Turnbacks, Minutes: r.Minutes, CostMinor: r.CostMinor})
	}
	return out, nil
}

// Ranking sort keys.
const (
	SortByCount   = "count"
	SortByMinutes = "minutes"
	SortByCost    = "cost"
)

// ProblemRanking returns problems ranked by the chosen metric, including each
// problem's share and the cumulative (Pareto) share of the total.
func (s *Service) ProblemRanking(ctx context.Context, a domain.Actor, f AnalyticsFilter, sortBy string, p Page) ([]sqlc.AnalyticsProblemRankingRow, int64, error) {
	switch sortBy {
	case "":
		sortBy = SortByCost
	case SortByCount, SortByMinutes, SortByCost:
	default:
		return nil, 0, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid sort",
			Fields: []domain.FieldError{{Field: "sort", Message: "must be count, minutes or cost"}}}
	}
	rows, err := s.store.AnalyticsProblemRanking(ctx, sqlc.AnalyticsProblemRankingParams{
		FromTs: f.Period.From, ToTs: f.Period.To, CategoryID: f.CategoryID, TeamID: f.TeamID,
		SortBy: sortBy, Lim: p.Limit, Off: p.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].TotalCount
	}
	return rows, total, nil
}

// CategoryBreakdown returns totals by category.
func (s *Service) CategoryBreakdown(ctx context.Context, a domain.Actor, f AnalyticsFilter) ([]sqlc.AnalyticsCategoryBreakdownRow, error) {
	return s.store.AnalyticsCategoryBreakdown(ctx, sqlc.AnalyticsCategoryBreakdownParams{FromTs: f.Period.From, ToTs: f.Period.To, TeamID: f.TeamID})
}

// TeamBreakdown returns totals by team with reporting participation.
func (s *Service) TeamBreakdown(ctx context.Context, a domain.Actor, f AnalyticsFilter) ([]sqlc.AnalyticsTeamBreakdownRow, error) {
	return s.store.AnalyticsTeamBreakdown(ctx, sqlc.AnalyticsTeamBreakdownParams{FromTs: f.Period.From, ToTs: f.Period.To, CategoryID: f.CategoryID})
}

// TurnbackRecord is an occurrence in the occurrence list.
type TurnbackRecord = sqlc.ListTurnbacksRow

// ListTurnbacks lists occurrences. The reporter (an IP address or Slack user)
// is included for administrators only — the audit view — and is blanked for
// everyone else. Filtering by reporter is likewise admin-only.
func (s *Service) ListTurnbacks(ctx context.Context, a domain.Actor, f AnalyticsFilter, reporter string, p Page) ([]TurnbackRecord, int64, error) {
	if reporter != "" && !a.Admin {
		return nil, 0, requireAdmin(a)
	}
	rows, err := s.store.ListTurnbacks(ctx, sqlc.ListTurnbacksParams{
		Reporter: strPtr(reporter), FromTs: f.Period.From, ToTs: f.Period.To,
		ProblemID: f.ProblemID, CategoryID: f.CategoryID, TeamID: f.TeamID, Lim: p.Limit, Off: p.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].TotalCount
	}
	if !a.Admin {
		for i := range rows {
			rows[i].Reporter = nil
		}
	}
	return rows, total, nil
}
