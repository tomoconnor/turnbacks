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

package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
	"turnbacks/internal/reporting"
	"turnbacks/internal/service"
	"turnbacks/internal/store"
	"turnbacks/internal/testdb"
)

type fixture struct {
	t     *testing.T
	ctx   context.Context
	st    *store.Store
	svc   *service.Service
	now   time.Time
	admin domain.Actor
	alice domain.Actor // one VPN address = one person
	bob   domain.Actor
	catID uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Parallel()
	st := testdb.New(t)
	f := &fixture{t: t, ctx: context.Background(), st: st, svc: service.New(st, testdb.Logger())}
	// Fixed clock: Wednesday 15 July 2026, 12:00 UTC (13:00 BST).
	f.now = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	f.svc.SetClock(func() time.Time { return f.now })
	f.admin = domain.Actor{Reporter: "ip:10.8.0.1", Admin: true}
	f.alice = domain.Actor{Reporter: "ip:10.8.0.12"}
	f.bob = domain.Actor{Reporter: "ip:10.8.0.13"}
	cats, err := f.svc.ListCategories(f.ctx, true)
	if err != nil || len(cats) == 0 {
		t.Fatalf("categories: %v", err)
	}
	f.catID = cats[0].ID
	return f
}

func (f *fixture) problem(title string, alias *string) sqlc.Problem {
	f.t.Helper()
	p, err := f.svc.CreateProblem(f.ctx, f.alice, service.ProblemInput{Title: title, CategoryID: f.catID, Alias: alias})
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) submit(a domain.Actor, problemID uuid.UUID, minutes int, at time.Time) service.TurnbackResult {
	f.t.Helper()
	return f.submitTeam(a, problemID, minutes, at, nil)
}

func (f *fixture) submitTeam(a domain.Actor, problemID uuid.UUID, minutes int, at time.Time, team *uuid.UUID) service.TurnbackResult {
	f.t.Helper()
	res, err := f.svc.SubmitTurnback(f.ctx, a, service.TurnbackInput{
		ID: uuid.New(), ProblemID: problemID, DurationMinutes: minutes, DurationEstimated: true, OccurredAt: &at, Source: domain.SourceWeb, TeamID: team,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return res
}

func (f *fixture) week() reporting.Period {
	f.t.Helper()
	per, err := f.svc.ResolvePeriod(f.ctx, reporting.PresetLast7Days, "", "")
	if err != nil {
		f.t.Fatal(err)
	}
	return per
}

func wantKind(t *testing.T, err error, kind domain.Kind, code string) {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok || de.Kind != kind || (code != "" && de.Code != code) {
		t.Fatalf("want domain error kind=%d code=%q, got %v", kind, code, err)
	}
}

func strp(s string) *string { return &s }

func TestTurnbackCreationAndValidation(t *testing.T) {
	f := newFixture(t)
	p := f.problem("Corporate VPN disconnects", strp("vpn"))

	res := f.submit(f.alice, p.ID, 10, f.now.Add(-time.Minute))
	if !res.Created || res.Turnback.Reporter == nil || *res.Turnback.Reporter != "ip:10.8.0.12" || res.UndoToken == "" {
		t.Fatalf("unexpected result %+v", res)
	}

	base := service.TurnbackInput{ProblemID: p.ID, DurationMinutes: 10, Source: domain.SourceWeb}
	cases := []func(in *service.TurnbackInput){
		func(in *service.TurnbackInput) { in.DurationMinutes = 0 },
		func(in *service.TurnbackInput) { in.DurationMinutes = 1441 },
		func(in *service.TurnbackInput) { t := f.now.Add(time.Hour); in.OccurredAt = &t },
		func(in *service.TurnbackInput) { t := f.now.AddDate(-2, 0, 0); in.OccurredAt = &t },
		func(in *service.TurnbackInput) { in.Source = "fax" },
		func(in *service.TurnbackInput) { in.ProblemID = uuid.Nil },
		func(in *service.TurnbackInput) { in.ProblemID = uuid.New() },
		func(in *service.TurnbackInput) { s := string(make([]rune, 1001)); in.Note = &s },
		func(in *service.TurnbackInput) { id := uuid.New(); in.TeamID = &id },
	}
	for _, mut := range cases {
		in := base
		in.ID = uuid.New()
		mut(&in)
		_, err := f.svc.SubmitTurnback(f.ctx, f.alice, in)
		wantKind(t, err, domain.KindInvalid, "")
	}

	in := base
	in.ID, in.DurationMinutes = uuid.New(), 240
	_, err := f.svc.SubmitTurnback(f.ctx, f.alice, in)
	wantKind(t, err, domain.KindConfirmationRequired, "duration_confirmation_required")
	in.ConfirmLargeDuration = true
	if _, err := f.svc.SubmitTurnback(f.ctx, f.alice, in); err != nil {
		t.Fatalf("confirmed large duration rejected: %v", err)
	}
}

func TestIdempotentSubmission(t *testing.T) {
	f := newFixture(t)
	p := f.problem("CI runner unavailable", nil)
	at := f.now.Add(-30 * time.Minute)
	in := service.TurnbackInput{ID: uuid.New(), ProblemID: p.ID, DurationMinutes: 15, OccurredAt: &at, Source: domain.SourcePWAOffline, Note: strp("queued offline")}

	first, err := f.svc.SubmitTurnback(f.ctx, f.alice, in)
	if err != nil || !first.Created {
		t.Fatalf("first: %v %+v", err, first)
	}
	again, err := f.svc.SubmitTurnback(f.ctx, f.alice, in)
	if err != nil || again.Created || again.Turnback.ID != first.Turnback.ID || again.UndoToken != "" {
		t.Fatalf("replay: %v %+v", err, again)
	}
	// An offline report retried after the laptop changed network (new address)
	// is still the same report: replayed, original reporter kept.
	moved, err := f.svc.SubmitTurnback(f.ctx, domain.Actor{Reporter: "ip:192.168.1.50"}, in)
	if err != nil || moved.Created || *moved.Turnback.Reporter != "ip:10.8.0.12" {
		t.Fatalf("replay from new address: %v %+v", err, moved)
	}
	if !again.Turnback.OccurredAt.Equal(at) {
		t.Errorf("original occurrence time not preserved: %s", again.Turnback.OccurredAt)
	}
	diff := in
	diff.DurationMinutes = 30
	_, err = f.svc.SubmitTurnback(f.ctx, f.alice, diff)
	wantKind(t, err, domain.KindConflict, "idempotency_conflict")

	var n int
	if err := f.st.Pool.QueryRow(f.ctx, "SELECT count(*) FROM turnbacks").Scan(&n); err != nil || n != 1 {
		t.Fatalf("expected exactly one stored turnback, got %d (%v)", n, err)
	}
}

func TestPeopleAffectedCountsDistinctReporters(t *testing.T) {
	f := newFixture(t)
	p := f.problem("SSO login loop", nil)
	at := f.now.Add(-time.Minute)
	f.submit(f.alice, p.ID, 5, at)
	f.submit(f.alice, p.ID, 5, at)
	f.submit(f.bob, p.ID, 5, at)
	f.submit(domain.Actor{}, p.ID, 5, at) // unknown address
	sum, err := f.svc.AnalyticsSummary(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week()})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Current.Turnbacks != 4 || sum.Current.DistinctReporters != 2 || sum.Current.UnattributedTurnbacks != 1 {
		t.Errorf("summary = %+v", sum.Current)
	}
}

func TestUndoAndCorrection(t *testing.T) {
	f := newFixture(t)
	p := f.problem("Dev environment rebuild", nil)
	q := f.problem("Other problem", nil)
	res := f.submit(f.alice, p.ID, 10, f.now.Add(-time.Hour))

	wantKind(t, f.svc.DeleteTurnback(f.ctx, f.bob, res.Turnback.ID, false, ""), domain.KindForbidden, "undo_not_allowed")
	wantKind(t, f.svc.DeleteTurnback(f.ctx, f.bob, res.Turnback.ID, false, "wrong-token"), domain.KindForbidden, "undo_not_allowed")
	// The token is a capability: it works within the window from anywhere.
	if err := f.svc.DeleteTurnback(f.ctx, f.bob, res.Turnback.ID, false, res.UndoToken); err != nil {
		t.Fatalf("undo with token: %v", err)
	}

	// After the undo window the token no longer works; an admin can still delete.
	late := f.submit(f.alice, p.ID, 10, f.now.Add(-time.Hour))
	f.now = f.now.Add(service.UndoWindow + time.Minute)
	wantKind(t, f.svc.DeleteTurnback(f.ctx, f.alice, late.Turnback.ID, false, late.UndoToken), domain.KindForbidden, "undo_not_allowed")

	// Corrections are admin-only.
	upd := service.TurnbackUpdate{ProblemID: q.ID, DurationMinutes: 20, DurationEstimated: false, OccurredAt: f.now.Add(-2 * time.Hour)}
	_, err := f.svc.UpdateTurnback(f.ctx, f.alice, late.Turnback.ID, upd)
	wantKind(t, err, domain.KindForbidden, "admin_required")
	got, err := f.svc.UpdateTurnback(f.ctx, f.admin, late.Turnback.ID, upd)
	if err != nil || got.Turnback.DurationMinutes != 20 || got.Turnback.ProblemID != q.ID {
		t.Fatalf("admin update: %v %+v", err, got)
	}
	if err := f.svc.DeleteTurnback(f.ctx, f.admin, late.Turnback.ID, false, ""); err != nil {
		t.Fatal(err)
	}
}

func TestProblemMerge(t *testing.T) {
	f := newFixture(t)
	src := f.problem("VPN keeps dropping", strp("vpn-drop"))
	tgt := f.problem("Corporate VPN disconnects", nil)
	f.submit(f.alice, src.ID, 10, f.now.Add(-time.Hour))
	f.submit(f.bob, src.ID, 5, f.now.Add(-2*time.Hour))
	f.submit(f.alice, tgt.ID, 15, f.now.Add(-3*time.Hour))
	if _, err := f.svc.CreateRemediation(f.ctx, f.admin, src.ID, service.RemediationInput{ImplementedOn: reporting.Date{Year: 2026, Month: 6, Day: 1}, Description: "patched client"}); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.MergeProblems(f.ctx, f.alice, src.ID, tgt.ID)
	wantKind(t, err, domain.KindForbidden, "admin_required")
	_, err = f.svc.MergeProblems(f.ctx, f.admin, src.ID, src.ID)
	wantKind(t, err, domain.KindInvalid, "")

	res, err := f.svc.MergeProblems(f.ctx, f.admin, src.ID, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.MovedTurnbacks != 2 || res.MovedRemediations != 1 || res.Target.Alias == nil || *res.Target.Alias != "vpn-drop" {
		t.Errorf("merge = %+v", res)
	}
	sum, _ := f.svc.AnalyticsSummary(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week(), ProblemID: &tgt.ID})
	if sum.Current.Turnbacks != 3 || sum.Current.Minutes != 30 || sum.Current.DistinctReporters != 2 {
		t.Errorf("target totals = %+v", sum.Current)
	}
	late := f.submit(f.alice, src.ID, 5, f.now.Add(-time.Minute))
	if late.Turnback.ProblemID != tgt.ID {
		t.Errorf("late submission not redirected: %s", late.Turnback.ProblemID)
	}
	_, err = f.svc.MergeProblems(f.ctx, f.admin, src.ID, tgt.ID)
	wantKind(t, err, domain.KindConflict, "problem_merged")
}

func TestProblemCreationSearchAndAliases(t *testing.T) {
	f := newFixture(t)
	f.problem("Corporate VPN disconnects", strp("vpn"))
	_, err := f.svc.CreateProblem(f.ctx, f.bob, service.ProblemInput{Title: "Other VPN thing", CategoryID: f.catID, Alias: strp("VPN")})
	wantKind(t, err, domain.KindConflict, "alias_taken")
	_, err = f.svc.CreateProblem(f.ctx, f.bob, service.ProblemInput{Title: "x", CategoryID: f.catID})
	wantKind(t, err, domain.KindInvalid, "")
	_, err = f.svc.CreateProblem(f.ctx, f.bob, service.ProblemInput{Title: "Bad URL", CategoryID: f.catID, ExternalURL: strp("javascript:alert(1)")})
	wantKind(t, err, domain.KindInvalid, "")

	sim, err := f.svc.SimilarProblems(f.ctx, f.alice, "VPN disconnect")
	if err != nil || len(sim) == 0 || sim[0].Title != "Corporate VPN disconnects" {
		t.Fatalf("similar = %+v %v", sim, err)
	}
	items, total, err := f.svc.ListProblems(f.ctx, f.alice, service.ProblemListParams{Query: "vpn", Page: service.Page{Limit: 10}})
	if err != nil || total != 1 || items[0].Problem.Alias == nil {
		t.Fatalf("search = %d %v", total, err)
	}
	_, total, _ = f.svc.ListProblems(f.ctx, f.alice, service.ProblemListParams{Query: "%", Page: service.Page{Limit: 10}})
	if total != 0 {
		t.Errorf("wildcard search matched %d", total)
	}
}

func TestAdminOnlyOperations(t *testing.T) {
	f := newFixture(t)
	p := f.problem("Approval bottleneck", nil)
	settings := service.SettingsInput{OrgName: "Acme", Currency: "GBP", DefaultHourlyRateMinor: 5000, ReportingTimezone: "Europe/London", RecoverablePct: 100, LargeDurationMinutes: 120}
	checks := map[string]error{
		"settings": func() error { _, e := f.svc.UpdateSettings(f.ctx, f.alice, settings); return e }(),
		"team": func() error {
			_, e := f.svc.CreateTeam(f.ctx, f.alice, service.TeamInput{Name: "X", Active: true})
			return e
		}(),
		"category": func() error {
			_, e := f.svc.CreateCategory(f.ctx, f.alice, service.CategoryInput{Name: "X", Active: true})
			return e
		}(),
		"problem": func() error {
			_, e := f.svc.UpdateProblem(f.ctx, f.alice, p.ID, service.ProblemInput{Title: "x y z", CategoryID: f.catID, Status: "resolved"})
			return e
		}(),
		"remediation": func() error {
			_, e := f.svc.CreateRemediation(f.ctx, f.alice, p.ID, service.RemediationInput{ImplementedOn: reporting.Date{Year: 2026, Month: 1, Day: 1}, Description: "x"})
			return e
		}(),
		"audit": func() error {
			_, _, e := f.svc.ListTurnbacks(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week()}, "ip:10.8.0.12", service.Page{Limit: 1})
			return e
		}(),
	}
	for name, err := range checks {
		de, ok := domain.AsError(err)
		if !ok || de.Code != "admin_required" {
			t.Errorf("%s: want admin_required, got %v", name, err)
		}
	}
	// Reporting is open to everyone.
	if _, err := f.svc.TeamBreakdown(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week()}); err != nil {
		t.Error(err)
	}
	if _, err := f.svc.BusinessCase(f.ctx, f.alice, service.BusinessCaseParams{ProblemID: p.ID}); err != nil {
		t.Error(err)
	}
	if _, err := f.svc.UpdateSettings(f.ctx, f.admin, settings); err != nil {
		t.Error(err)
	}
}

func TestReporterVisibleToAdminsOnly(t *testing.T) {
	f := newFixture(t)
	p := f.problem("DNS failures", nil)
	f.submit(f.alice, p.ID, 5, f.now.Add(-time.Hour))
	f.submit(f.bob, p.ID, 5, f.now.Add(-time.Hour))

	rows, _, err := f.svc.ListTurnbacks(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week()}, "", service.Page{Limit: 10})
	if err != nil || len(rows) != 2 {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Reporter != nil {
			t.Errorf("reporter leaked to non-admin: %v", *r.Reporter)
		}
	}
	rows, total, err := f.svc.ListTurnbacks(f.ctx, f.admin, service.AnalyticsFilter{Period: f.week()}, "ip:10.8.0.13", service.Page{Limit: 10})
	if err != nil || total != 1 || rows[0].Reporter == nil || *rows[0].Reporter != "ip:10.8.0.13" {
		t.Fatalf("admin audit filter: %v %d %+v", err, total, rows)
	}
}

func TestFinancialCalculationsAndTeamRates(t *testing.T) {
	f := newFixture(t)
	expensive := int64(9000) // £90/h
	team, err := f.svc.CreateTeam(f.ctx, f.admin, service.TeamInput{Name: "Payments", Active: true, HourlyRateMinor: &expensive})
	if err != nil {
		t.Fatal(err)
	}
	p := f.problem("Flaky CI", nil)
	at := f.now.Add(-time.Hour)
	f.submit(f.alice, p.ID, 10, at)            // org rate £50: 833.33p
	f.submit(f.alice, p.ID, 10, at)            // 833.33p
	f.submitTeam(f.bob, p.ID, 7, at, &team.ID) // team rate £90: 1050p
	f.submit(f.alice, p.ID, 1, at)             // 83.33p
	sum, err := f.svc.AnalyticsSummary(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week()})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Current.CostMinor != 2800 || sum.Current.Minutes != 28 || sum.Current.Turnbacks != 4 {
		t.Fatalf("summary = %+v", sum.Current)
	}
	rank, _, err := f.svc.ProblemRanking(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week()}, service.SortByCost, service.Page{Limit: 10})
	if err != nil || len(rank) != 1 || rank[0].CostMinor != 2800 || rank[0].CumulativeShareBp != 10000 {
		t.Fatalf("ranking = %+v %v", rank, err)
	}
	teams, err := f.svc.TeamBreakdown(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week()})
	if err != nil || len(teams) != 2 {
		t.Fatalf("teams = %+v %v", teams, err)
	}
	st, _ := f.svc.GetSettings(f.ctx)
	_, err = f.svc.UpdateSettings(f.ctx, f.admin, service.SettingsInput{OrgName: st.OrgName, Currency: "GBP", DefaultHourlyRateMinor: 6000,
		ReportingTimezone: st.ReportingTimezone, RecoverablePct: 100, LargeDurationMinutes: 120})
	if err != nil {
		t.Fatal(err)
	}
	sum, _ = f.svc.AnalyticsSummary(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week()})
	if sum.Current.CostMinor != 3150 {
		t.Errorf("after rate change = %d", sum.Current.CostMinor)
	}
}

func TestTimeWindowAggregationAndTimezones(t *testing.T) {
	f := newFixture(t)
	p := f.problem("DNS failures", nil)
	f.submit(f.alice, p.ID, 10, time.Date(2026, 7, 13, 23, 30, 0, 0, time.UTC)) // 00:30 BST 14 July
	f.submit(f.alice, p.ID, 5, time.Date(2026, 7, 13, 22, 59, 0, 0, time.UTC))  // 23:59 BST 13 July

	per, err := f.svc.ResolvePeriod(f.ctx, reporting.PresetCustom, "2026-07-14", "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	sum, _ := f.svc.AnalyticsSummary(f.ctx, f.alice, service.AnalyticsFilter{Period: per})
	if sum.Current.Turnbacks != 1 || sum.Current.Minutes != 10 || sum.Previous.FirstDay.String() != "2026-07-13" || sum.Prior.Minutes != 5 {
		t.Errorf("14 July (London) = %+v prev=%+v", sum.Current, sum.Prior)
	}
	per, _ = f.svc.ResolvePeriod(f.ctx, reporting.PresetCustom, "2026-07-12", "2026-07-15")
	pts, err := f.svc.AnalyticsTimeseries(f.ctx, f.alice, service.AnalyticsFilter{Period: per}, reporting.BucketDay)
	if err != nil || len(pts) != 4 {
		t.Fatalf("points = %d %v", len(pts), err)
	}
	for i, want := range []int64{0, 5, 10, 0} {
		if pts[i].Minutes != want {
			t.Errorf("day %d minutes = %d, want %d", i, pts[i].Minutes, want)
		}
	}
	per, _ = f.svc.ResolvePeriod(f.ctx, reporting.PresetCustom, "2026-05-15", "2026-07-15")
	pts, _ = f.svc.AnalyticsTimeseries(f.ctx, f.alice, service.AnalyticsFilter{Period: per}, reporting.BucketMonth)
	if len(pts) != 3 || pts[2].Turnbacks != 2 || pts[0].Start.Format(time.DateOnly) != "2026-05-01" {
		t.Errorf("monthly = %+v", pts)
	}
}

func TestRemediationComparisonAndBusinessCase(t *testing.T) {
	f := newFixture(t)
	p := f.problem("Corporate VPN disconnects", strp("vpn"))
	for d := 1; d <= 14; d++ {
		day := time.Date(2026, 6, 16+d, 10, 0, 0, 0, time.UTC)
		f.submit(f.alice, p.ID, 10, day)
		f.submit(f.bob, p.ID, 10, day.Add(time.Hour))
		if d%2 == 0 {
			f.submit(f.alice, p.ID, 10, time.Date(2026, 7, d, 10, 0, 0, 0, time.UTC))
		}
	}
	rem, err := f.svc.CreateRemediation(f.ctx, f.admin, p.ID, service.RemediationInput{
		ImplementedOn: reporting.Date{Year: 2026, Month: 7, Day: 1}, Description: "New VPN", ImplementationCostMinor: 1_000_000,
		AnnualCostMinor: 100_000, ExpectedReductionPct: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.svc.CompareRemediation(f.ctx, f.alice, rem.ID, 90)
	if err != nil {
		t.Fatal(err)
	}
	if c.Windows.WindowDays != 14 || c.Before.Turnbacks != 28 || c.After.Turnbacks != 7 || c.Before.DistinctReporters != 2 || c.After.DistinctReporters != 1 {
		t.Errorf("comparison = %+v / %+v / %+v", c.Windows, c.Before, c.After)
	}
	bc, err := f.svc.BusinessCase(f.ctx, f.alice, service.BusinessCaseParams{ProblemID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if bc.AssumptionsSource != "remediation" || bc.Totals.Turnbacks != 35 || bc.Totals.CostMinor != 29167 || bc.Totals.DistinctReporters != 2 {
		t.Errorf("business case = %s %+v", bc.AssumptionsSource, bc.Totals)
	}
	bad := int64(150)
	_, err = f.svc.BusinessCase(f.ctx, f.alice, service.BusinessCaseParams{ProblemID: p.ID, ExpectedReductionPct: &bad})
	wantKind(t, err, domain.KindInvalid, "")
}

func TestBusinessCaseNotesAreAnonymised(t *testing.T) {
	f := newFixture(t)
	p := f.problem("Access requests", nil)
	at := f.now.Add(-time.Hour)
	_, err := f.svc.SubmitTurnback(f.ctx, f.alice, service.TurnbackInput{ID: uuid.New(), ProblemID: p.ID, DurationMinutes: 5, OccurredAt: &at,
		Source: domain.SourceWeb, Note: strp("asked jane.doe@example.com and @bob for access again")})
	if err != nil {
		t.Fatal(err)
	}
	bc, err := f.svc.BusinessCase(f.ctx, f.alice, service.BusinessCaseParams{ProblemID: p.ID})
	if err != nil || len(bc.Notes) != 1 || bc.Notes[0].Note != "asked [email] and @[someone] for access again" {
		t.Fatalf("%v %+v", err, bc.Notes)
	}
}

func TestRetentionClearsNotesAndAddresses(t *testing.T) {
	f := newFixture(t)
	p := f.problem("Old problem", nil)
	f.svc.SetClock(time.Now)
	at := time.Now().Add(-40 * 24 * time.Hour)
	if _, err := f.svc.SubmitTurnback(f.ctx, f.alice, service.TurnbackInput{ID: uuid.New(), ProblemID: p.ID, DurationMinutes: 5, OccurredAt: &at, Source: domain.SourceWeb, Note: strp("old note")}); err != nil {
		t.Fatal(err)
	}
	days := 30
	st, _ := f.svc.GetSettings(f.ctx)
	if _, err := f.svc.UpdateSettings(f.ctx, f.admin, service.SettingsInput{OrgName: st.OrgName, Currency: st.Currency, DefaultHourlyRateMinor: st.DefaultHourlyRateMinor,
		ReportingTimezone: st.ReportingTimezone, RecoverablePct: 100, LargeDurationMinutes: 120, NoteRetentionDays: &days, ReporterRetentionDays: &days}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RunMaintenance(f.ctx); err != nil {
		t.Fatal(err)
	}
	var note, reporter *string
	if err := f.st.Pool.QueryRow(f.ctx, "SELECT note, reporter FROM turnbacks").Scan(&note, &reporter); err != nil {
		t.Fatal(err)
	}
	if note != nil || reporter != nil {
		t.Errorf("retention not applied: note=%v reporter=%v", note, reporter)
	}
}

func TestQuantityRecordsIndividualOccurrences(t *testing.T) {
	f := newFixture(t)
	p := f.problem("Corporate VPN disconnects", nil)
	at := f.now.Add(-time.Hour)
	in := service.TurnbackInput{ID: uuid.New(), ProblemID: p.ID, DurationMinutes: 10, OccurredAt: &at, Source: domain.SourceWeb, Quantity: 15}

	res, err := f.svc.SubmitTurnback(f.ctx, f.alice, in)
	if err != nil || !res.Created || res.Quantity != 15 || res.Turnback.BatchID == nil || *res.Turnback.BatchID != in.ID {
		t.Fatalf("submit: %v %+v", err, res)
	}
	sum, _ := f.svc.AnalyticsSummary(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week()})
	if sum.Current.Turnbacks != 15 || sum.Current.Minutes != 150 || sum.Current.CostMinor != 12500 || sum.Current.DistinctReporters != 1 {
		t.Fatalf("summary = %+v", sum.Current)
	}
	again, err := f.svc.SubmitTurnback(f.ctx, f.alice, in)
	if err != nil || again.Created || again.Quantity != 15 {
		t.Fatalf("replay: %v %+v", err, again)
	}
	diff := in
	diff.Quantity = 14
	_, err = f.svc.SubmitTurnback(f.ctx, f.alice, diff)
	wantKind(t, err, domain.KindConflict, "idempotency_conflict")

	big := service.TurnbackInput{ID: uuid.New(), ProblemID: p.ID, DurationMinutes: 5, Source: domain.SourceWeb, Quantity: 30}
	_, err = f.svc.SubmitTurnback(f.ctx, f.alice, big)
	wantKind(t, err, domain.KindConfirmationRequired, "quantity_confirmation_required")
	big.Quantity, big.ConfirmLargeDuration = 101, true
	_, err = f.svc.SubmitTurnback(f.ctx, f.alice, big)
	wantKind(t, err, domain.KindInvalid, "validation_failed")

	// Undo removes the whole batch.
	if err := f.svc.DeleteTurnback(f.ctx, f.alice, res.Turnback.ID, true, res.UndoToken); err != nil {
		t.Fatal(err)
	}
	sum, _ = f.svc.AnalyticsSummary(f.ctx, f.alice, service.AnalyticsFilter{Period: f.week()})
	if sum.Current.Turnbacks != 0 {
		t.Errorf("after batch undo: %+v", sum.Current)
	}
}
