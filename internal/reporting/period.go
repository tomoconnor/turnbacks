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

// Package reporting contains pure reporting logic: period resolution in the
// organisation's reporting timezone, financial projections and before/after
// remediation windows. Database aggregation lives in SQL (db/queries); this
// package turns those aggregates into reports.
package reporting

import (
	"fmt"
	"time"

	"turnbacks/internal/domain"
)

// Preset names accepted by ResolvePeriod.
const (
	PresetLast7Days  = "last_7_days"
	PresetLast30Days = "last_30_days"
	PresetLast90Days = "last_90_days"
	PresetThisYear   = "this_year"
	PresetCustom     = "custom"
)

// Bucket granularities for time series.
const (
	BucketDay   = "day"
	BucketWeek  = "week"
	BucketMonth = "month"
)

// MaxCustomRangeDays bounds custom reporting ranges.
const MaxCustomRangeDays = 3660

// Period is a half-open UTC interval [From, To) together with the calendar
// dates it represents in the reporting timezone (FirstDay..LastDay inclusive).
type Period struct {
	Preset   string
	From     time.Time
	To       time.Time
	FirstDay Date
	LastDay  Date
	Location *time.Location
}

// Days returns the number of calendar days covered.
func (p Period) Days() int { return p.LastDay.Sub(p.FirstDay) + 1 }

// Date is a calendar date without a time component.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the calendar date of t in loc.
func DateOf(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date{y, m, d}
}

// ParseDate parses YYYY-MM-DD.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", s)
	}
	return Date{t.Year(), t.Month(), t.Day()}, nil
}

// String formats as YYYY-MM-DD.
func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day) }

// Start returns local midnight of d in loc. time.Date normalises DST gaps.
func (d Date) Start(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// AddDays returns d shifted by n days.
func (d Date) AddDays(n int) Date {
	t := time.Date(d.Year, d.Month, d.Day+n, 12, 0, 0, 0, time.UTC)
	return Date{t.Year(), t.Month(), t.Day()}
}

// Sub returns the number of days from o to d.
func (d Date) Sub(o Date) int {
	a := time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC)
	b := time.Date(o.Year, o.Month, o.Day, 12, 0, 0, 0, time.UTC)
	return int(a.Sub(b).Round(24*time.Hour) / (24 * time.Hour))
}

// Before reports whether d is before o.
func (d Date) Before(o Date) bool { return d.Sub(o) < 0 }

// Time returns the date as a UTC midnight time.Time (for DATE columns).
func (d Date) Time() time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

// NewPeriod builds a period from inclusive calendar dates.
func NewPeriod(preset string, first, last Date, loc *time.Location) Period {
	return Period{
		Preset:   preset,
		From:     first.Start(loc).UTC(),
		To:       last.AddDays(1).Start(loc).UTC(),
		FirstDay: first,
		LastDay:  last,
		Location: loc,
	}
}

// ResolvePeriod turns a preset (or custom from/to dates) into a Period in loc.
// "Last N days" includes today.
func ResolvePeriod(preset, from, to string, now time.Time, loc *time.Location) (Period, error) {
	today := DateOf(now, loc)
	switch preset {
	case "", PresetLast30Days:
		return NewPeriod(PresetLast30Days, today.AddDays(-29), today, loc), nil
	case PresetLast7Days:
		return NewPeriod(preset, today.AddDays(-6), today, loc), nil
	case PresetLast90Days:
		return NewPeriod(preset, today.AddDays(-89), today, loc), nil
	case PresetThisYear:
		return NewPeriod(preset, Date{today.Year, time.January, 1}, today, loc), nil
	case PresetCustom:
		var v domain.Validator
		f, err := ParseDate(from)
		if err != nil {
			v.Add("from", err.Error())
		}
		t, err := ParseDate(to)
		if err != nil {
			v.Add("to", err.Error())
		}
		if err := v.Err(); err != nil {
			return Period{}, err
		}
		if t.Before(f) {
			v.Add("to", "must not be before from")
		} else if t.Sub(f) > MaxCustomRangeDays {
			v.Add("to", fmt.Sprintf("range must not exceed %d days", MaxCustomRangeDays))
		}
		if err := v.Err(); err != nil {
			return Period{}, err
		}
		return NewPeriod(preset, f, t, loc), nil
	default:
		return Period{}, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid period",
			Fields: []domain.FieldError{{Field: "period", Message: "must be one of last_7_days, last_30_days, last_90_days, this_year, custom"}}}
	}
}

// Previous returns the preceding equivalent period. For year-to-date it is the
// same span of the previous year; otherwise it is the immediately preceding
// span of identical length.
func (p Period) Previous() Period {
	if p.Preset == PresetThisYear {
		first := Date{p.FirstDay.Year - 1, time.January, 1}
		last := Date{p.LastDay.Year - 1, p.LastDay.Month, p.LastDay.Day}
		// Normalise 29 Feb to 28 Feb in non-leap previous year.
		lt := time.Date(last.Year, last.Month, last.Day, 12, 0, 0, 0, time.UTC)
		if lt.Month() != last.Month {
			last = Date{last.Year, last.Month, last.Day - 1}
		}
		return NewPeriod(PresetCustom, first, last, p.Location)
	}
	n := p.Days()
	return NewPeriod(PresetCustom, p.FirstDay.AddDays(-n), p.FirstDay.AddDays(-1), p.Location)
}

// DefaultBucket chooses a sensible time-series granularity for the period.
func (p Period) DefaultBucket() string {
	switch d := p.Days(); {
	case d <= 45:
		return BucketDay
	case d <= 200:
		return BucketWeek
	default:
		return BucketMonth
	}
}

// ValidBucket reports whether b is a known granularity.
func ValidBucket(b string) bool {
	return b == BucketDay || b == BucketWeek || b == BucketMonth
}
