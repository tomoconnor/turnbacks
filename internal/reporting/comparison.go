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

import "time"

// DefaultComparisonWindowDays is the default before/after window length.
const DefaultComparisonWindowDays = 90

// MinMeaningfulWindowDays is the minimum window for which a comparison is
// presented without an "insufficient data" warning.
const MinMeaningfulWindowDays = 14

// ComparisonWindows are equal-length windows either side of a remediation.
type ComparisonWindows struct {
	Before       Period
	After        Period
	WindowDays   int
	Insufficient bool // true when the after-window is too short to be meaningful
}

// RemediationWindows computes equivalent observation windows around the
// remediation date. The after-window starts on the implementation date and is
// limited to complete days elapsed so far (today excluded), so both windows
// always have identical lengths.
func RemediationWindows(implemented Date, requestedDays int, now time.Time, loc *time.Location) ComparisonWindows {
	if requestedDays <= 0 {
		requestedDays = DefaultComparisonWindowDays
	}
	today := DateOf(now, loc)
	elapsed := today.Sub(implemented) // complete days since implementation, excluding today
	w := requestedDays
	if elapsed < w {
		w = elapsed
	}
	if w < 0 {
		w = 0
	}
	cw := ComparisonWindows{WindowDays: w, Insufficient: w < MinMeaningfulWindowDays}
	if w == 0 {
		cw.Before = NewPeriod(PresetCustom, implemented, implemented.AddDays(-1), loc)
		cw.After = cw.Before
		cw.Before.To = cw.Before.From
		cw.After.To = cw.After.From
		return cw
	}
	cw.Before = NewPeriod(PresetCustom, implemented.AddDays(-w), implemented.AddDays(-1), loc)
	cw.After = NewPeriod(PresetCustom, implemented, implemented.AddDays(w-1), loc)
	return cw
}
