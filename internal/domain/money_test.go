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

package domain

import (
	"math/big"
	"testing"
)

func TestRoundRat(t *testing.T) {
	cases := []struct {
		num, den int64
		want     int64
	}{
		{5, 2, 3}, {-5, 2, -3}, {7, 3, 2}, {8, 3, 3}, {1, 2, 1}, {0, 7, 0}, {-1, 3, 0}, {299, 100, 3},
	}
	for _, c := range cases {
		if got := RoundRat(big.NewRat(c.num, c.den)); got != c.want {
			t.Errorf("RoundRat(%d/%d) = %d, want %d", c.num, c.den, got, c.want)
		}
	}
}

func TestCostMinor(t *testing.T) {
	cases := []struct {
		minutes, rate, want int64
	}{
		{60, 5000, 5000},  // one hour at £50
		{10, 5000, 833},   // 833.33p
		{15, 5000, 1250},  // quarter hour
		{1, 5000, 83},     // 83.33p
		{90, 7550, 11325}, // 1.5h at £75.50
		{0, 5000, 0},
		{3, 10, 1}, // 0.5p rounds half away from zero
	}
	for _, c := range cases {
		if got := CostMinor(c.minutes, c.rate); got != c.want {
			t.Errorf("CostMinor(%d, %d) = %d, want %d", c.minutes, c.rate, got, c.want)
		}
	}
}

func TestChangeBasisPoints(t *testing.T) {
	if bp, ok := ChangeBasisPoints(100, 40); !ok || bp != -6000 {
		t.Errorf("got %d %v, want -6000 true", bp, ok)
	}
	if bp, ok := ChangeBasisPoints(3, 4); !ok || bp != 3333 {
		t.Errorf("got %d %v, want 3333 true", bp, ok)
	}
	if _, ok := ChangeBasisPoints(0, 5); ok {
		t.Error("expected undefined change from zero")
	}
}

func TestScaleMinor(t *testing.T) {
	if got := ScaleMinor(100000, 365, 90); got != 405556 {
		t.Errorf("ScaleMinor = %d, want 405556", got)
	}
}

func TestFormatMinor(t *testing.T) {
	cases := []struct {
		v    int64
		exp  int
		want string
	}{{123456, 2, "1234.56"}, {5, 2, "0.05"}, {-1050, 2, "-10.50"}, {700, 0, "700"}, {1234, 3, "1.234"}}
	for _, c := range cases {
		if got := FormatMinor(c.v, c.exp); got != c.want {
			t.Errorf("FormatMinor(%d,%d) = %s, want %s", c.v, c.exp, got, c.want)
		}
	}
}
