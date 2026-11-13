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
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
	"turnbacks/internal/store"
)

// ProblemListParams filters problem listings.
type ProblemListParams struct {
	Query      string
	CategoryID *uuid.UUID
	Statuses   []string
	Page       Page
}

// ProblemListItem is a problem with lightweight usage statistics.
type ProblemListItem struct {
	Problem        sqlc.Problem
	CategoryName   string
	RecentCount    int64
	LastOccurredAt *time.Time
}

// ListProblems searches problems. Merged problems are never listed.
func (s *Service) ListProblems(ctx context.Context, a domain.Actor, p ProblemListParams) ([]ProblemListItem, int64, error) {
	statuses := p.Statuses
	if len(statuses) == 0 {
		statuses = []string{domain.StatusActive, domain.StatusMonitoring}
	}
	for _, st := range statuses {
		if !domain.ValidProblemStatus(st) {
			return nil, 0, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid status filter",
				Fields: []domain.FieldError{{Field: "status", Message: "unknown status " + st}}}
		}
	}
	params := sqlc.ListProblemsParams{Statuses: statuses, CategoryID: p.CategoryID, Lim: p.Page.Limit, Off: p.Page.Offset}
	if q := strings.TrimSpace(p.Query); q != "" {
		if len(q) > 100 {
			q = q[:100]
		}
		esc := escapeLike(q)
		params.Q = &esc
		params.QRaw = &q
	}
	rows, err := s.store.ListProblems(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	out := make([]ProblemListItem, 0, len(rows))
	var total int64
	for _, r := range rows {
		total = r.TotalCount
		out = append(out, ProblemListItem{Problem: r.Problem, CategoryName: r.CategoryName, RecentCount: r.RecentCount, LastOccurredAt: asTimePtr(r.LastOccurredAt)})
	}
	return out, total, nil
}

// QuickProblem is a compact problem reference for quick-action buttons.
type QuickProblem struct {
	ID             uuid.UUID
	Title          string
	Alias          *string
	Status         string
	CategoryID     uuid.UUID
	RecentCount    int64
	LastOccurredAt *time.Time
}

// QuickProblems returns the organisation's most frequently reported
// problems in the last 30 days, for quick-action buttons.
func (s *Service) QuickProblems(ctx context.Context) ([]QuickProblem, error) {
	fr, err := s.store.FrequentProblems(ctx, sqlc.FrequentProblemsParams{Since: s.now().Add(-30 * 24 * time.Hour), Lim: 8})
	if err != nil {
		return nil, err
	}
	out := make([]QuickProblem, 0, len(fr))
	for _, r := range fr {
		t := r.LastOccurredAt
		out = append(out, QuickProblem{ID: r.ID, Title: r.Title, Alias: r.Alias, Status: r.Status, CategoryID: r.CategoryID, RecentCount: r.RecentCount, LastOccurredAt: &t})
	}
	return out, nil
}

// SimilarProblems suggests existing problems whose titles resemble title.
func (s *Service) SimilarProblems(ctx context.Context, a domain.Actor, title string) ([]sqlc.SimilarProblemsRow, error) {
	title = strings.TrimSpace(title)
	if len(title) < 3 {
		return []sqlc.SimilarProblemsRow{}, nil
	}
	return s.store.SimilarProblems(ctx, title)
}

// GetProblem loads a problem with its category name.
func (s *Service) GetProblem(ctx context.Context, a domain.Actor, id uuid.UUID) (sqlc.GetProblemRow, error) {
	p, err := s.store.GetProblem(ctx, id)
	return p, notFoundOr(err, "problem")
}

// ProblemInput is used to create or update a problem.
type ProblemInput struct {
	Title       string
	Description string
	CategoryID  uuid.UUID
	Status      string // update only
	Alias       *string
	ExternalURL *string
	IsDemo      bool
}

func (in *ProblemInput) normalise() {
	in.Title = strings.Join(strings.Fields(in.Title), " ")
	in.Description = strings.TrimSpace(in.Description)
	in.Alias = trimPtr(in.Alias)
	if in.Alias != nil {
		l := strings.ToLower(*in.Alias)
		in.Alias = &l
	}
	in.ExternalURL = trimPtr(in.ExternalURL)
}

func (in ProblemInput) validate(update bool) error {
	var v domain.Validator
	v.Check(len(in.Title) >= 3 && len(in.Title) <= 120, "title", "must be between 3 and 120 characters")
	v.Check(len(in.Description) <= 4000, "description", "must be at most 4000 characters")
	v.Check(in.CategoryID != uuid.Nil, "category_id", "is required")
	if in.Alias != nil && !domain.ValidAlias(*in.Alias) {
		v.Add("alias", "must be 1-32 lowercase letters, digits or hyphens, starting with a letter or digit")
	}
	if in.ExternalURL != nil {
		u, err := url.Parse(*in.ExternalURL)
		v.Check(err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(*in.ExternalURL) <= 2000,
			"external_url", "must be an absolute http(s) URL")
	}
	if update {
		v.Check(domain.ValidProblemStatus(in.Status), "status", "must be active, monitoring, resolved or archived")
	}
	return v.Err()
}

func mapProblemWriteErr(err error) error {
	if c, ok := store.UniqueViolation(err); ok && c == "problems_alias_key" {
		return &domain.Error{Kind: domain.KindConflict, Code: "alias_taken", Message: "that alias is already used by another problem",
			Fields: []domain.FieldError{{Field: "alias", Message: "already in use"}}}
	}
	if _, ok := store.ForeignKeyViolation(err); ok {
		return &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "unknown category",
			Fields: []domain.FieldError{{Field: "category_id", Message: "does not exist"}}}
	}
	return err
}

// CreateProblem creates a problem. Anyone may create problems; no approval
// is required.
func (s *Service) CreateProblem(ctx context.Context, a domain.Actor, in ProblemInput) (sqlc.Problem, error) {
	in.normalise()
	in.Status = domain.StatusActive
	if err := in.validate(false); err != nil {
		return sqlc.Problem{}, err
	}
	cat, err := s.store.GetCategory(ctx, in.CategoryID)
	if err != nil || !cat.Active {
		return sqlc.Problem{}, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "unknown or inactive category",
			Fields: []domain.FieldError{{Field: "category_id", Message: "does not exist or is inactive"}}}
	}
	p, err := s.store.CreateProblem(ctx, sqlc.CreateProblemParams{
		Title: in.Title, Description: in.Description, CategoryID: in.CategoryID,
		Alias: in.Alias, ExternalUrl: in.ExternalURL, IsDemo: in.IsDemo,
	})
	if err != nil {
		return p, mapProblemWriteErr(err)
	}
	s.log.InfoContext(ctx, "problem created", "problem_id", p.ID)
	if !in.IsDemo {
		s.emitLogged(ctx, problemEvent(s.link, p, cat.Name))
	}
	return p, nil
}

// UpdateProblem updates a problem (administrators only).
func (s *Service) UpdateProblem(ctx context.Context, a domain.Actor, id uuid.UUID, in ProblemInput) (sqlc.Problem, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.Problem{}, err
	}
	in.normalise()
	if err := in.validate(true); err != nil {
		return sqlc.Problem{}, err
	}
	existing, err := s.store.GetProblem(ctx, id)
	if err != nil {
		return sqlc.Problem{}, notFoundOr(err, "problem")
	}
	if existing.Problem.MergedIntoID != nil {
		return sqlc.Problem{}, domain.Conflict("problem_merged", "this problem has been merged into another and cannot be edited")
	}
	p, err := s.store.UpdateProblem(ctx, sqlc.UpdateProblemParams{
		ID: id, Title: in.Title, Description: in.Description, CategoryID: in.CategoryID, Status: in.Status,
		Alias: in.Alias, ExternalUrl: in.ExternalURL,
	})
	if err != nil {
		return p, mapProblemWriteErr(err)
	}
	return p, nil
}

// MergeResult reports what a merge moved.
type MergeResult struct {
	Target            sqlc.Problem
	MovedTurnbacks    int64
	MovedRemediations int64
}

// MergeProblems merges source into target (administrators only). All
// turnbacks and remediations move to the target; the source is archived and
// marked as merged so that late (e.g. offline) submissions are redirected.
// History is preserved: no occurrence records are deleted.
func (s *Service) MergeProblems(ctx context.Context, a domain.Actor, sourceID, targetID uuid.UUID) (MergeResult, error) {
	var res MergeResult
	if err := requireAdmin(a); err != nil {
		return res, err
	}
	if sourceID == targetID {
		return res, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "cannot merge a problem into itself",
			Fields: []domain.FieldError{{Field: "target_id", Message: "must differ from the source problem"}}}
	}
	err := s.store.InTx(ctx, func(q *sqlc.Queries) error {
		// Lock both rows in a stable order to avoid deadlocks.
		first, second := sourceID, targetID
		if strings.Compare(first.String(), second.String()) > 0 {
			first, second = second, first
		}
		locked := map[uuid.UUID]sqlc.Problem{}
		for _, id := range []uuid.UUID{first, second} {
			p, err := q.GetProblemForUpdate(ctx, id)
			if err != nil {
				return notFoundOr(err, "problem")
			}
			locked[id] = p
		}
		src, tgt := locked[sourceID], locked[targetID]
		if src.MergedIntoID != nil {
			return domain.Conflict("problem_merged", "source problem has already been merged")
		}
		if tgt.MergedIntoID != nil {
			return domain.Conflict("problem_merged", "target problem has been merged into another problem")
		}
		var err error
		if res.MovedTurnbacks, err = q.ReassignTurnbacks(ctx, sqlc.ReassignTurnbacksParams{TargetID: targetID, SourceID: sourceID}); err != nil {
			return err
		}
		if res.MovedRemediations, err = q.ReassignRemediations(ctx, sqlc.ReassignRemediationsParams{TargetID: targetID, SourceID: sourceID}); err != nil {
			return err
		}
		if err := q.RepointMergedProblems(ctx, sqlc.RepointMergedProblemsParams{TargetID: &targetID, SourceID: &sourceID}); err != nil {
			return err
		}
		if err := q.MarkProblemMerged(ctx, sqlc.MarkProblemMergedParams{TargetID: &targetID, SourceID: sourceID}); err != nil {
			return err
		}
		// Keep the source alias working (e.g. for Slack) if the target has none.
		if src.Alias != nil {
			if err := q.SetProblemAliasIfEmpty(ctx, sqlc.SetProblemAliasIfEmptyParams{ID: targetID, Alias: src.Alias}); err != nil {
				return err
			}
		}
		updated, err := q.GetProblem(ctx, targetID)
		if err != nil {
			return err
		}
		res.Target = updated.Problem
		return nil
	})
	if err == nil {
		s.log.InfoContext(ctx, "problems merged", "source_id", sourceID, "target_id", targetID,
			"moved_turnbacks", res.MovedTurnbacks, "moved_remediations", res.MovedRemediations)
	}
	return res, err
}

// resolveProblemForSubmission follows merge redirects and rejects archived
// problems. Resolved problems still accept submissions: a recurrence after
// resolution is valuable evidence.
func resolveProblemForSubmission(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (sqlc.Problem, error) {
	for range 10 {
		p, err := q.GetProblem(ctx, id)
		if err != nil {
			if store.IsNoRows(err) {
				return sqlc.Problem{}, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "unknown problem",
					Fields: []domain.FieldError{{Field: "problem_id", Message: "does not exist"}}}
			}
			return sqlc.Problem{}, err
		}
		if p.Problem.MergedIntoID != nil {
			id = *p.Problem.MergedIntoID
			continue
		}
		if p.Problem.Status == domain.StatusArchived {
			return sqlc.Problem{}, &domain.Error{Kind: domain.KindInvalid, Code: "problem_archived", Message: "this problem is archived and no longer accepts reports",
				Fields: []domain.FieldError{{Field: "problem_id", Message: "problem is archived"}}}
		}
		return p.Problem, nil
	}
	return sqlc.Problem{}, domain.Errorf(domain.KindInternal, "merge_cycle", "problem merge chain too deep")
}

// ProblemByAlias resolves a short alias (used by Slack).
func (s *Service) ProblemByAlias(ctx context.Context, alias string) (sqlc.Problem, error) {
	p, err := s.store.GetProblemByAlias(ctx, strings.ToLower(strings.TrimSpace(alias)))
	return p, notFoundOr(err, "problem")
}
