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

// Package seed generates realistic, clearly-marked demonstration data
// (six months of turnbacks). Every row it creates has is_demo = true so that
// it can be identified in the UI and removed with Purge. It is only ever run
// explicitly via `turnbacks seed` and refuses to run in production.
package seed

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"turnbacks/db/sqlc"
	"turnbacks/internal/store"
)

// ErrAlreadySeeded is returned when demo data already exists.
var ErrAlreadySeeded = errors.New("demo data already present; run `turnbacks seed --purge` first")

type problemSpec struct {
	title, alias, category, description, status string
	perWorkday                                  float64 // mean occurrences per working day
	durations                                   []int   // typical durations (minutes)
	notes                                       []string
	remediation                                 *remediationSpec
}

type remediationSpec struct {
	daysAgo                 int
	description             string
	implementationCost      int64
	annualCost              int64
	expectedReduction       int32
	actualRemainingFraction float64
}

var teams = []struct {
	name string
	rate *int64
}{
	{"Platform Engineering", nil},
	{"Payments", ptr(int64(6000))},
	{"Mobile", nil},
	{"Data & Analytics", ptr(int64(5500))},
	{"Internal Tools", nil},
}

// demoPeople is the number of fictional people. Each reports from its own
// address in the documentation range 198.51.100.0/24 (RFC 5737), as each
// person has their own VPN address.
const demoPeople = 32

// demoHeadcount is each demo team's size; slightly larger than the number of
// demo reporters per team so participation is realistic (not everyone reports).
const demoHeadcount = 8

var problems = []problemSpec{
	{
		title: "Corporate VPN disconnects", alias: "vpn", category: "Networking", status: "monitoring",
		description: "The corporate VPN client drops the connection, forcing re-authentication and losing SSH and database sessions.",
		perWorkday:  9, durations: []int{5, 5, 10, 10, 10, 15, 15, 30},
		notes: []string{"disconnected again during deploy", "lost SSH session to bastion", "had to re-enter MFA three times", "dropped mid-call with customer", "reconnect loop for ten minutes", "VPN client froze, had to reboot"},
		remediation: &remediationSpec{daysAgo: 56, description: "Replaced legacy VPN concentrator with a zero-trust access proxy for engineering systems.",
			implementationCost: 1800000, annualCost: 450000, expectedReduction: 70, actualRemainingFraction: 0.3},
	},
	{
		title: "CI runner intermittently unavailable", alias: "ci", category: "CI/CD", status: "active",
		description: "Shared CI runners go offline or jobs queue indefinitely, unrelated to the code being built.",
		perWorkday:  7, durations: []int{10, 15, 15, 30, 30, 60},
		notes: []string{"job stuck in pending for 25 min", "runner lost mid-build, had to retry", "retried pipeline three times", "cache volume full again"},
	},
	{
		title: "Development environment provisioning delays", alias: "devenv", category: "Development Environment", status: "active",
		description: "Provisioning or rebuilding a development environment takes far longer than expected or fails part-way.",
		perWorkday:  3, durations: []int{30, 30, 60, 60, 90, 120},
		notes: []string{"container image rebuild from scratch", "dependency mirror timed out", "had to rebuild the whole environment after OS update"},
	},
	{
		title: "SSO authentication failures", alias: "sso", category: "Identity and Access", status: "active",
		description: "Single sign-on redirects loop or fail, blocking access to internal tools.",
		perWorkday:  4, durations: []int{5, 5, 10, 10, 15},
		notes: []string{"login loop on the wiki", "MFA push never arrived", "session expired mid-form, lost work"},
	},
	{
		title: "Internal DNS resolution failures", alias: "dns", category: "Networking", status: "active",
		description: "Internal hostnames intermittently fail to resolve.",
		perWorkday:  2, durations: []int{5, 10, 10, 15, 30},
		notes: []string{"couldn't resolve the artifact registry", "staging hostnames not resolving"},
	},
	{
		title: "Manual change approval bottleneck", alias: "approval", category: "Processes", status: "active",
		description: "Production changes wait for manual sign-off from a small group of approvers.",
		perWorkday:  2.5, durations: []int{15, 30, 30, 60, 60, 120},
		notes: []string{"waited half a day for CAB sign-off", "approver on leave, no delegate", "re-submitted change form for formatting"},
	},
	{
		title: "Artifact registry slow downloads", alias: "registry", category: "Internal Tools", status: "active",
		description: "Package and image downloads from the internal registry are very slow at peak times.",
		perWorkday:  1.5, durations: []int{5, 10, 15},
	},
}

func ptr[T any](v T) *T { return &v }

// Result summarises what was seeded.
type Result struct {
	Teams, People, Problems, Turnbacks, Remediations int
}

// Run inserts demo data in a single transaction. now anchors the six-month
// window; seed makes the output deterministic.
func Run(ctx context.Context, st *store.Store, now time.Time, seed uint64) (Result, error) {
	var res Result
	var existing int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM problems WHERE is_demo`).Scan(&existing); err != nil {
		return res, err
	}
	if existing > 0 {
		return res, ErrAlreadySeeded
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // deterministic demo data, not security sensitive
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		loc = time.UTC
	}

	err = pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		teamIDs := make([]uuid.UUID, 0, len(teams))
		for _, t := range teams {
			hc := int32(demoHeadcount)
			row, err := q.CreateTeam(ctx, sqlc.CreateTeamParams{Name: t.name + " (demo)", HourlyRateMinor: t.rate, Headcount: &hc, IsDemo: true})
			if err != nil {
				return fmt.Errorf("create team: %w", err)
			}
			teamIDs = append(teamIDs, row.ID)
		}
		res.Teams = len(teamIDs)

		type person struct {
			reporter string
			team     uuid.UUID
			weight   float64 // propensity to report: participation varies by person
		}
		var people []person
		for i := range demoPeople {
			people = append(people, person{
				reporter: fmt.Sprintf("ip:198.51.100.%d", i+10),
				team:     teamIDs[i%len(teamIDs)],
				weight:   0.3 + rng.Float64()*1.4,
			})
		}
		res.People = len(people)
		pick := func() person {
			var total float64
			for _, p := range people {
				total += p.weight
			}
			x := rng.Float64() * total
			for _, p := range people {
				if x -= p.weight; x <= 0 {
					return p
				}
			}
			return people[len(people)-1]
		}

		cats, err := q.ListCategories(ctx, nil)
		if err != nil {
			return err
		}
		catByName := map[string]uuid.UUID{}
		for _, c := range cats {
			catByName[c.Name] = c.ID
		}

		start := now.AddDate(0, -6, 0)
		var rows [][]any
		for _, ps := range problems {
			alias := ps.alias
			p, err := q.CreateProblem(ctx, sqlc.CreateProblemParams{
				Title: ps.title, Description: ps.description, CategoryID: catByName[ps.category], Alias: &alias, IsDemo: true,
			})
			if err != nil {
				return fmt.Errorf("create problem %q: %w", ps.title, err)
			}
			if _, err := tx.Exec(ctx, `UPDATE problems SET created_at = $2, status = $3 WHERE id = $1`, p.ID, start, ps.status); err != nil {
				return err
			}
			res.Problems++

			var remDate time.Time
			if r := ps.remediation; r != nil {
				d := now.In(loc).AddDate(0, 0, -r.daysAgo)
				remDate = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
				if _, err := q.CreateRemediation(ctx, sqlc.CreateRemediationParams{
					ProblemID: p.ID, ImplementedOn: time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC),
					Description: r.description, ImplementationCostMinor: r.implementationCost, AnnualCostMinor: r.annualCost,
					ExpectedReductionPct: r.expectedReduction, IsDemo: true,
				}); err != nil {
					return err
				}
				res.Remediations++
				// The point in time the fix went live, for before/after comparisons.
				desc, _, _ := strings.Cut(r.description, ".")
				if _, err := q.CreateMarker(ctx, sqlc.CreateMarkerParams{
					OccurredAt: remDate.Add(9*time.Hour + 30*time.Minute), Title: "Rolled out: " + desc,
					ProblemID: &p.ID, Source: "web", IsDemo: true,
				}); err != nil {
					return err
				}
			}

			sl := start.In(loc)
			for day := time.Date(sl.Year(), sl.Month(), sl.Day(), 0, 0, 0, 0, loc); day.Before(now); day = day.AddDate(0, 0, 1) {
				wd := day.Weekday()
				if (wd == time.Saturday || wd == time.Sunday) && rng.Float64() > 0.05 {
					continue
				}
				lambda := ps.perWorkday
				if wd == time.Monday {
					lambda *= 1.25
				}
				lambda *= 0.85 + 0.3*math.Sin(float64(day.YearDay())/14)
				if ps.remediation != nil && !day.Before(remDate) {
					lambda *= ps.remediation.actualRemainingFraction
				}
				for range poisson(rng, lambda) {
					ts := time.Date(day.Year(), day.Month(), day.Day(), 8+rng.IntN(10), rng.IntN(60), rng.IntN(60), 0, loc)
					if !ts.Before(now) {
						continue
					}
					dur := ps.durations[rng.IntN(len(ps.durations))]
					pr := pick()
					// A few reports come in without a team chosen.
					var team any = pr.team
					if rng.Float64() < 0.1 {
						team = nil
					}
					var note any
					if len(ps.notes) > 0 && rng.Float64() < 0.2 {
						note = ps.notes[rng.IntN(len(ps.notes))]
					}
					source := "web"
					switch x := rng.Float64(); {
					case x < 0.08:
						source = "pwa_offline"
					case x < 0.25:
						source = "slack"
					}
					sub := ts.Add(time.Duration(rng.IntN(120)) * time.Second)
					if source == "pwa_offline" {
						sub = ts.Add(time.Duration(10+rng.IntN(120)) * time.Minute)
					}
					if sub.After(now) {
						sub = now
					}
					rows = append(rows, []any{uuid.New(), p.ID, pr.reporter, team, int32(dur), rng.Float64() < 0.85, note, ts.UTC(), sub.UTC(), sub.UTC(), source, true})
				}
			}
		}
		res.Turnbacks = len(rows)
		_, err = tx.CopyFrom(ctx, pgx.Identifier{"turnbacks"},
			[]string{"id", "problem_id", "reporter", "team_id", "duration_minutes", "duration_estimated", "note", "occurred_at", "submitted_at", "updated_at", "source", "is_demo"},
			pgx.CopyFromRows(rows))
		return err
	})
	return res, err
}

// poisson samples a Poisson-distributed count (Knuth; fine for small lambda).
func poisson(rng *rand.Rand, lambda float64) int {
	if lambda <= 0 {
		return 0
	}
	l, k, p := math.Exp(-lambda), 0, 1.0
	for {
		p *= rng.Float64()
		if p <= l {
			return k
		}
		k++
	}
}

// Purge removes all demo data, including any turnbacks recorded against demo
// problems. Real records are untouched.
func Purge(ctx context.Context, st *store.Store) (int64, error) {
	var total int64
	err := pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error {
		for _, stmt := range []string{
			`DELETE FROM turnbacks WHERE is_demo OR problem_id IN (SELECT id FROM problems WHERE is_demo)`,
			`DELETE FROM remediations WHERE is_demo OR problem_id IN (SELECT id FROM problems WHERE is_demo)`,
			`DELETE FROM change_markers WHERE is_demo OR problem_id IN (SELECT id FROM problems WHERE is_demo)`,
			`UPDATE problems SET merged_into_id = NULL WHERE merged_into_id IN (SELECT id FROM problems WHERE is_demo)`,
			`DELETE FROM problems WHERE is_demo`,
			`DELETE FROM teams WHERE is_demo`,
		} {
			tag, err := tx.Exec(ctx, stmt)
			if err != nil {
				return err
			}
			total += tag.RowsAffected()
		}
		return nil
	})
	return total, err
}
