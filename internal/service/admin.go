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
	"turnbacks/internal/store"
)

// ---- Settings ----

// SettingsInput updates organisation settings.
type SettingsInput struct {
	OrgName                string
	Currency               string
	DefaultHourlyRateMinor int64
	ReportingTimezone      string
	RecoverablePct         int
	LargeDurationMinutes   int
	NoteRetentionDays      *int
	ReporterRetentionDays  *int
}

// GetSettings returns organisation settings (public).
func (s *Service) GetSettings(ctx context.Context) (sqlc.OrgSetting, error) {
	st, _, err := s.Settings(ctx)
	return st, err
}

// UpdateSettings updates organisation settings (administrators only).
func (s *Service) UpdateSettings(ctx context.Context, a domain.Actor, in SettingsInput) (sqlc.OrgSetting, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.OrgSetting{}, err
	}
	in.OrgName = strings.TrimSpace(in.OrgName)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	var v domain.Validator
	v.Check(len(in.OrgName) >= 1 && len(in.OrgName) <= 200, "org_name", "must be between 1 and 200 characters")
	v.Check(domain.ValidCurrency(in.Currency), "currency", "must be a three-letter ISO 4217 code")
	v.Check(in.DefaultHourlyRateMinor >= 0 && in.DefaultHourlyRateMinor <= 100_000_00, "default_hourly_rate_minor", "must be between 0 and 10,000,000")
	if _, err := time.LoadLocation(in.ReportingTimezone); err != nil || in.ReportingTimezone == "" || strings.EqualFold(in.ReportingTimezone, "local") {
		v.Add("reporting_timezone", "must be an IANA timezone name such as Europe/London")
	}
	v.Check(in.RecoverablePct >= 0 && in.RecoverablePct <= 100, "recoverable_pct", "must be between 0 and 100")
	v.Check(in.LargeDurationMinutes >= 1 && in.LargeDurationMinutes <= domain.MaxDurationMinutes, "large_duration_minutes", "must be between 1 and 1440")
	if in.NoteRetentionDays != nil {
		v.Check(*in.NoteRetentionDays >= 1 && *in.NoteRetentionDays <= 36500, "note_retention_days", "must be between 1 and 36500, or null")
	}
	if in.ReporterRetentionDays != nil {
		v.Check(*in.ReporterRetentionDays >= 1 && *in.ReporterRetentionDays <= 36500, "reporter_retention_days", "must be between 1 and 36500, or null")
	}
	if err := v.Err(); err != nil {
		return sqlc.OrgSetting{}, err
	}
	return s.store.UpdateSettings(ctx, sqlc.UpdateSettingsParams{
		OrgName: in.OrgName, Currency: in.Currency, DefaultHourlyRateMinor: in.DefaultHourlyRateMinor,
		ReportingTimezone: in.ReportingTimezone, RecoverablePct: int32(in.RecoverablePct),
		LargeDurationMinutes: int32(in.LargeDurationMinutes), NoteRetentionDays: int32Ptr(in.NoteRetentionDays),
		ReporterRetentionDays: int32Ptr(in.ReporterRetentionDays),
	})
}

func int32Ptr(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p)
	return &v
}

// ---- Teams ----

// TeamInput creates or updates a team.
type TeamInput struct {
	Name            string
	Active          bool
	HourlyRateMinor *int64
	// Headcount is the team size, used to show reporting participation.
	Headcount *int32
	IsDemo    bool
}

func (in *TeamInput) validate() error {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	var v domain.Validator
	v.Check(len(in.Name) >= 1 && len(in.Name) <= 100, "name", "must be between 1 and 100 characters")
	if in.HourlyRateMinor != nil {
		v.Check(*in.HourlyRateMinor >= 0 && *in.HourlyRateMinor <= 100_000_00, "hourly_rate_minor", "must be between 0 and 10,000,000")
	}
	if in.Headcount != nil {
		v.Check(*in.Headcount >= 1 && *in.Headcount <= 1_000_000, "headcount", "must be at least 1")
	}
	return v.Err()
}

func mapNameConflict(err error, what string) error {
	if _, ok := store.UniqueViolation(err); ok {
		return &domain.Error{Kind: domain.KindConflict, Code: "name_taken", Message: "a " + what + " with that name already exists",
			Fields: []domain.FieldError{{Field: "name", Message: "already in use"}}}
	}
	return err
}

// ListTeams lists teams (public).
func (s *Service) ListTeams(ctx context.Context, activeOnly bool) ([]sqlc.Team, error) {
	var active *bool
	if activeOnly {
		t := true
		active = &t
	}
	return s.store.ListTeams(ctx, active)
}

// CreateTeam creates a team (administrators only).
func (s *Service) CreateTeam(ctx context.Context, a domain.Actor, in TeamInput) (sqlc.Team, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.Team{}, err
	}
	if err := in.validate(); err != nil {
		return sqlc.Team{}, err
	}
	t, err := s.store.CreateTeam(ctx, sqlc.CreateTeamParams{Name: in.Name, HourlyRateMinor: in.HourlyRateMinor, Headcount: in.Headcount, IsDemo: in.IsDemo})
	return t, mapNameConflict(err, "team")
}

// UpdateTeam updates a team (administrators only).
func (s *Service) UpdateTeam(ctx context.Context, a domain.Actor, id uuid.UUID, in TeamInput) (sqlc.Team, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.Team{}, err
	}
	if err := in.validate(); err != nil {
		return sqlc.Team{}, err
	}
	t, err := s.store.UpdateTeam(ctx, sqlc.UpdateTeamParams{ID: id, Name: in.Name, Active: in.Active, HourlyRateMinor: in.HourlyRateMinor, Headcount: in.Headcount})
	if err != nil {
		return t, mapNameConflict(notFoundOr(err, "team"), "team")
	}
	return t, nil
}

// ---- Categories ----

// CategoryInput creates or updates a category.
type CategoryInput struct {
	Name        string
	Description string
	Active      bool
	SortOrder   int
}

func (in *CategoryInput) validate() error {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Description = strings.TrimSpace(in.Description)
	var v domain.Validator
	v.Check(len(in.Name) >= 1 && len(in.Name) <= 100, "name", "must be between 1 and 100 characters")
	v.Check(len(in.Description) <= 1000, "description", "must be at most 1000 characters")
	return v.Err()
}

// ListCategories lists categories (public).
func (s *Service) ListCategories(ctx context.Context, activeOnly bool) ([]sqlc.Category, error) {
	var active *bool
	if activeOnly {
		t := true
		active = &t
	}
	return s.store.ListCategories(ctx, active)
}

// CreateCategory creates a category (administrators only).
func (s *Service) CreateCategory(ctx context.Context, a domain.Actor, in CategoryInput) (sqlc.Category, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.Category{}, err
	}
	if err := in.validate(); err != nil {
		return sqlc.Category{}, err
	}
	c, err := s.store.CreateCategory(ctx, sqlc.CreateCategoryParams{Name: in.Name, Description: in.Description, SortOrder: int32(in.SortOrder)})
	return c, mapNameConflict(err, "category")
}

// UpdateCategory updates a category (administrators only).
func (s *Service) UpdateCategory(ctx context.Context, a domain.Actor, id uuid.UUID, in CategoryInput) (sqlc.Category, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.Category{}, err
	}
	if err := in.validate(); err != nil {
		return sqlc.Category{}, err
	}
	c, err := s.store.UpdateCategory(ctx, sqlc.UpdateCategoryParams{ID: id, Name: in.Name, Description: in.Description, Active: in.Active, SortOrder: int32(in.SortOrder)})
	if err != nil {
		return c, mapNameConflict(notFoundOr(err, "category"), "category")
	}
	return c, nil
}
