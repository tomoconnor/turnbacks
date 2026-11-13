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
	"strings"
	"time"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
	"turnbacks/internal/reporting"
)

// RemediationInput creates or updates a remediation record.
type RemediationInput struct {
	ImplementedOn           reporting.Date
	Description             string
	ImplementationCostMinor int64
	AnnualCostMinor         int64
	ExpectedReductionPct    int
	IsDemo                  bool
}

func (in *RemediationInput) validate() error {
	in.Description = strings.TrimSpace(in.Description)
	var v domain.Validator
	v.Check(in.ImplementedOn.Year >= 2000 && in.ImplementedOn.Year <= 2100, "implemented_on", "must be a valid date")
	v.Check(len(in.Description) >= 1 && len(in.Description) <= 4000, "description", "must be between 1 and 4000 characters")
	v.Check(in.ImplementationCostMinor >= 0 && in.ImplementationCostMinor <= domain.MaxMoneyMinor, "implementation_cost_minor", "must be between 0 and 1000000000000")
	v.Check(in.AnnualCostMinor >= 0 && in.AnnualCostMinor <= domain.MaxMoneyMinor, "annual_cost_minor", "must be between 0 and 1000000000000")
	v.Check(in.ExpectedReductionPct >= 0 && in.ExpectedReductionPct <= 100, "expected_reduction_pct", "must be between 0 and 100")
	return v.Err()
}

// ListRemediations lists remediations for a problem.
func (s *Service) ListRemediations(ctx context.Context, a domain.Actor, problemID uuid.UUID) ([]sqlc.Remediation, error) {
	return s.store.ListRemediationsForProblem(ctx, problemID)
}

// CreateRemediation records a remediation (administrators only).
func (s *Service) CreateRemediation(ctx context.Context, a domain.Actor, problemID uuid.UUID, in RemediationInput) (sqlc.Remediation, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.Remediation{}, err
	}
	if err := in.validate(); err != nil {
		return sqlc.Remediation{}, err
	}
	p, err := s.store.GetProblem(ctx, problemID)
	if err != nil {
		return sqlc.Remediation{}, notFoundOr(err, "problem")
	}
	if p.Problem.MergedIntoID != nil {
		return sqlc.Remediation{}, domain.Conflict("problem_merged", "this problem has been merged into another")
	}
	r, err := s.store.CreateRemediation(ctx, sqlc.CreateRemediationParams{
		ProblemID: problemID, ImplementedOn: in.ImplementedOn.Time(), Description: in.Description,
		ImplementationCostMinor: in.ImplementationCostMinor, AnnualCostMinor: in.AnnualCostMinor,
		ExpectedReductionPct: int32(in.ExpectedReductionPct), IsDemo: in.IsDemo,
	})
	if err == nil {
		s.log.InfoContext(ctx, "remediation recorded", "remediation_id", r.ID, "problem_id", problemID)
		if !in.IsDemo {
			s.emitLogged(ctx, remediationEvent(s.link, r, p.Problem.Title))
		}
	}
	return r, err
}

// UpdateRemediation updates a remediation (administrators only).
func (s *Service) UpdateRemediation(ctx context.Context, a domain.Actor, id uuid.UUID, in RemediationInput) (sqlc.Remediation, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.Remediation{}, err
	}
	if err := in.validate(); err != nil {
		return sqlc.Remediation{}, err
	}
	r, err := s.store.UpdateRemediation(ctx, sqlc.UpdateRemediationParams{
		ID: id, ImplementedOn: in.ImplementedOn.Time(), Description: in.Description,
		ImplementationCostMinor: in.ImplementationCostMinor, AnnualCostMinor: in.AnnualCostMinor,
		ExpectedReductionPct: int32(in.ExpectedReductionPct),
	})
	return r, notFoundOr(err, "remediation")
}

// DeleteRemediation deletes a remediation (administrators only).
func (s *Service) DeleteRemediation(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	if err := requireAdmin(a); err != nil {
		return err
	}
	n, err := s.store.DeleteRemediation(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.NotFound("remediation")
	}
	return nil
}

// RemediationComparison is an observational before/after comparison over
// equal-length windows. It does not establish causation.
type RemediationComparison struct {
	Remediation  sqlc.Remediation
	ProblemTitle string
	Windows      reporting.ComparisonWindows
	Before       Totals
	After        Totals
}

func (s *Service) compare(ctx context.Context, r sqlc.Remediation, title string, windowDays int, loc *time.Location) (RemediationComparison, error) {
	impl := reporting.Date{Year: r.ImplementedOn.Year(), Month: r.ImplementedOn.Month(), Day: r.ImplementedOn.Day()}
	w := reporting.RemediationWindows(impl, windowDays, s.now(), loc)
	c := RemediationComparison{Remediation: r, ProblemTitle: title, Windows: w}
	if w.WindowDays == 0 {
		return c, nil
	}
	f := AnalyticsFilter{ProblemID: &r.ProblemID}
	var err error
	if c.Before, err = s.totals(ctx, w.Before.From, w.Before.To, f); err != nil {
		return c, err
	}
	c.After, err = s.totals(ctx, w.After.From, w.After.To, f)
	return c, err
}

// CompareRemediation compares equal windows before and after a remediation.
func (s *Service) CompareRemediation(ctx context.Context, a domain.Actor, id uuid.UUID, windowDays int) (RemediationComparison, error) {
	if windowDays < 0 || windowDays > 365 {
		return RemediationComparison{}, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid window",
			Fields: []domain.FieldError{{Field: "window_days", Message: "must be between 1 and 365"}}}
	}
	r, err := s.store.GetRemediation(ctx, id)
	if err != nil {
		return RemediationComparison{}, notFoundOr(err, "remediation")
	}
	p, err := s.store.GetProblem(ctx, r.ProblemID)
	if err != nil {
		return RemediationComparison{}, err
	}
	_, loc, err := s.Settings(ctx)
	if err != nil {
		return RemediationComparison{}, err
	}
	return s.compare(ctx, r, p.Problem.Title, windowDays, loc)
}

// RemediationOverview compares every recorded remediation (most recent first).
func (s *Service) RemediationOverview(ctx context.Context, a domain.Actor, windowDays int) ([]RemediationComparison, error) {
	_, loc, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListAllRemediations(ctx, 50)
	if err != nil {
		return nil, err
	}
	out := make([]RemediationComparison, 0, len(rows))
	for _, r := range rows {
		c, err := s.compare(ctx, r.Remediation, r.ProblemTitle, windowDays, loc)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
