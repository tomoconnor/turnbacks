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
)

// Money is always handled as int64 minor currency units (e.g. pence).
// Intermediate calculations use math/big.Rat so no floating point arithmetic
// is involved; results are rounded half away from zero, matching PostgreSQL's
// round(numeric).

// RoundRat rounds r to the nearest integer, halves away from zero.
func RoundRat(r *big.Rat) int64 {
	num := new(big.Int).Set(r.Num())
	den := r.Denom()
	neg := num.Sign() < 0
	if neg {
		num.Neg(num)
	}
	// floor((2*num + den) / (2*den))
	two := big.NewInt(2)
	n2 := new(big.Int).Mul(num, two)
	n2.Add(n2, den)
	d2 := new(big.Int).Mul(den, two)
	q := new(big.Int).Quo(n2, d2)
	if neg {
		q.Neg(q)
	}
	return q.Int64()
}

// CostMinor returns the estimated productivity cost in minor units for the
// given minutes at an hourly rate expressed in minor units:
// minutes × rate / 60, rounded.
func CostMinor(minutes, hourlyRateMinor int64) int64 {
	return RoundRat(big.NewRat(minutes*hourlyRateMinor, 60))
}

// Ratio returns a/b as a *big.Rat. b must be non-zero.
func Ratio(a, b int64) *big.Rat { return big.NewRat(a, b) }

// ScaleMinor returns round(v × num / den).
func ScaleMinor(v, num, den int64) int64 {
	r := new(big.Rat).SetInt64(v)
	r.Mul(r, big.NewRat(num, den))
	return RoundRat(r)
}

// ChangeBasisPoints returns the relative change from before to after in basis
// points (1/100 of a percent). ok is false when before is zero, since a
// relative change from zero is undefined.
func ChangeBasisPoints(before, after int64) (bp int64, ok bool) {
	if before == 0 {
		return 0, false
	}
	return RoundRat(big.NewRat((after-before)*10000, before)), true
}

// CurrencyExponent returns the number of minor-unit digits for an ISO 4217
// currency code (2 for most currencies).
func CurrencyExponent(code string) int {
	switch code {
	case "JPY", "KRW", "ISK", "CLP", "VND", "UGX", "PYG", "XAF", "XOF", "RWF", "KMF", "GNF", "DJF", "VUV", "XPF", "BIF":
		return 0
	case "BHD", "KWD", "OMR", "JOD", "TND", "LYD", "IQD":
		return 3
	}
	return 2
}

// FormatMinor renders a minor-unit amount as a plain decimal string, e.g.
// 123456 with exponent 2 becomes "1234.56". Used for CSV exports.
func FormatMinor(v int64, exponent int) string {
	if exponent == 0 {
		return big.NewInt(v).String()
	}
	neg := v < 0
	if neg {
		v = -v
	}
	pow := int64(1)
	for range exponent {
		pow *= 10
	}
	s := big.NewInt(v/pow).String() + "." + leftPad(big.NewInt(v%pow).String(), exponent)
	if neg {
		s = "-" + s
	}
	return s
}

func leftPad(s string, n int) string {
	for len(s) < n {
		s = "0" + s
	}
	return s
}
