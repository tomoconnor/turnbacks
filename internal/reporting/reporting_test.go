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

package reporting

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestResolvePeriodPresets(t *testing.T) {
	london := mustLoc(t, "Europe/London")
	// 23:30 UTC on 2026-07-14 is 00:30 on 15 July in London (BST).
	now := time.Date(2026, 7, 14, 23, 30, 0, 0, time.UTC)

	p, err := ResolvePeriod(PresetLast7Days, "", "", now, london)
	if err != nil {
		t.Fatal(err)
	}
	if p.FirstDay.String() != "2026-07-09" || p.LastDay.String() != "2026-07-15" {
		t.Fatalf("got %s..%s", p.FirstDay, p.LastDay)
	}
	if want := time.Date(2026, 7, 8, 23, 0, 0, 0, time.UTC); !p.From.Equal(want) {
		t.Errorf("From = %s, want %s", p.From, want)
	}
	if want := time.Date(2026, 7, 15, 23, 0, 0, 0, time.UTC); !p.To.Equal(want) {
		t.Errorf("To = %s, want %s", p.To, want)
	}
	if p.Days() != 7 {
		t.Errorf("Days = %d", p.Days())
	}
	prev := p.Previous()
	if prev.FirstDay.String() != "2026-07-02" || prev.LastDay.String() != "2026-07-08" || !prev.To.Equal(p.From) {
		t.Errorf("previous = %s..%s", prev.FirstDay, prev.LastDay)
	}

	y, _ := ResolvePeriod(PresetThisYear, "", "", now, london)
	if y.FirstDay.String() != "2026-01-01" {
		t.Errorf("this_year first = %s", y.FirstDay)
	}
	// 1 Jan in London is GMT, so midnight local == midnight UTC.
	if !y.From.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("this_year from = %s", y.From)
	}
	yp := y.Previous()
	if yp.FirstDay.String() != "2025-01-01" || yp.LastDay.String() != "2025-07-15" {
		t.Errorf("this_year previous = %s..%s", yp.FirstDay, yp.LastDay)
	}
}

func TestResolvePeriodDSTDayLength(t *testing.T) {
	london := mustLoc(t, "Europe/London")
	// Clocks go forward on 2026-03-29; that local day is 23 hours long.
	p, err := ResolvePeriod(PresetCustom, "2026-03-29", "2026-03-29", time.Now(), london)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.To.Sub(p.From); got != 23*time.Hour {
		t.Errorf("DST day length = %s, want 23h", got)
	}
}

func TestResolvePeriodCustomValidation(t *testing.T) {
	loc := time.UTC
	if _, err := ResolvePeriod(PresetCustom, "2026-02-10", "2026-02-01", time.Now(), loc); err == nil {
		t.Error("expected error for reversed range")
	}
	if _, err := ResolvePeriod(PresetCustom, "bad", "2026-02-01", time.Now(), loc); err == nil {
		t.Error("expected error for bad date")
	}
	if _, err := ResolvePeriod("fortnight", "", "", time.Now(), loc); err == nil {
		t.Error("expected error for unknown preset")
	}
}

func TestPreviousLeapDay(t *testing.T) {
	p := NewPeriod(PresetThisYear, Date{2028, 1, 1}, Date{2028, 2, 29}, time.UTC)
	prev := p.Previous()
	if prev.LastDay.String() != "2027-02-28" {
		t.Errorf("got %s", prev.LastDay)
	}
}

func TestProject(t *testing.T) {
	// 90 days observed: 120 occurrences, 1800 minutes, £1,500.00 cost.
	o := Observation{Days: 90, Turnbacks: 120, Minutes: 1800, CostMinor: 150000}
	a := Assumptions{ImplementationCostMinor: 300000, AnnualCostMinor: 60000, ExpectedReductionPct: 80, RecoverablePct: 100}
	p := Project(o, a)
	if p.AnnualisedCostMinor != 608333 { // 150000*365/90 = 608333.33
		t.Errorf("annualised = %d", p.AnnualisedCostMinor)
	}
	if p.MonthlyCostMinor != 50694 { // 608333.33/12 = 50694.44
		t.Errorf("monthly = %d", p.MonthlyCostMinor)
	}
	if p.AnnualisedTurnbacks != 487 {
		t.Errorf("annual turnbacks = %d", p.AnnualisedTurnbacks)
	}
	if p.GrossAnnualBenefitMinor != 486667 { // 608333.33*0.8
		t.Errorf("gross = %d", p.GrossAnnualBenefitMinor)
	}
	if p.NetAnnualBenefitMinor != 426667 {
		t.Errorf("net = %d", p.NetAnnualBenefitMinor)
	}
	// payback = 300000 / (426666.67/12) = 8.4375 months -> 84 tenths
	if !p.PaybackAchievable || p.PaybackMonthsTenths != 84 {
		t.Errorf("payback = %d %v", p.PaybackMonthsTenths, p.PaybackAchievable)
	}
	if p.FirstYearNetMinor != 126667 || p.ThreeYearNetMinor != 980000 {
		t.Errorf("first=%d three=%d", p.FirstYearNetMinor, p.ThreeYearNetMinor)
	}

	// Recoverable share halves the benefit.
	a.RecoverablePct = 50
	if p2 := Project(o, a); p2.GrossAnnualBenefitMinor != 243333 {
		t.Errorf("gross at 50%% recoverable = %d", p2.GrossAnnualBenefitMinor)
	}

	// Ongoing cost exceeding benefit: no payback.
	a.AnnualCostMinor = 1_000_000
	if p3 := Project(o, a); p3.PaybackAchievable || p3.NetAnnualBenefitMinor >= 0 {
		t.Errorf("expected no payback, got %+v", p3)
	}
}

func TestAssumptionsValidate(t *testing.T) {
	if err := (Assumptions{ExpectedReductionPct: 101}).Validate(); err == nil {
		t.Error("expected error")
	}
	if err := (Assumptions{ImplementationCostMinor: -1}).Validate(); err == nil {
		t.Error("expected error")
	}
	if err := (Assumptions{ExpectedReductionPct: 50, RecoverablePct: 100}).Validate(); err != nil {
		t.Error(err)
	}
}

func TestRemediationWindows(t *testing.T) {
	loc := time.UTC
	impl := Date{2026, 3, 1}
	now := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC) // 45 full days elapsed
	w := RemediationWindows(impl, 90, now, loc)
	if w.WindowDays != 45 || w.Insufficient {
		t.Fatalf("window = %d insufficient=%v", w.WindowDays, w.Insufficient)
	}
	if w.Before.FirstDay.String() != "2026-01-15" || w.Before.LastDay.String() != "2026-02-28" {
		t.Errorf("before = %s..%s", w.Before.FirstDay, w.Before.LastDay)
	}
	if w.After.FirstDay.String() != "2026-03-01" || w.After.LastDay.String() != "2026-04-14" {
		t.Errorf("after = %s..%s", w.After.FirstDay, w.After.LastDay)
	}
	if w.Before.Days() != w.After.Days() {
		t.Error("windows must be equal length")
	}
	if !w.Before.To.Equal(w.After.From) {
		t.Error("windows must be contiguous")
	}

	short := RemediationWindows(impl, 90, time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC), loc)
	if short.WindowDays != 4 || !short.Insufficient {
		t.Errorf("short = %+v", short)
	}
	future := RemediationWindows(Date{2026, 5, 1}, 90, now, loc)
	if future.WindowDays != 0 || !future.Insufficient {
		t.Errorf("future = %+v", future)
	}
}
