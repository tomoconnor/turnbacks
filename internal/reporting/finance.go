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
	"math/big"

	"turnbacks/internal/domain"
)

// Observation summarises what was actually reported for a problem over an
// observation window.
type Observation struct {
	Days              int   // length of the observation window in days (>= 1)
	Turnbacks         int64 // reported occurrences
	Minutes           int64 // reported minutes lost
	CostMinor         int64 // estimated productivity cost, minor units
	DistinctReporters int64 // identifiable affected people (anonymous excluded)
}

// Assumptions are user-adjustable inputs to a business case. They are
// estimates, not facts, and are shown alongside every projection.
type Assumptions struct {
	ImplementationCostMinor int64 // one-off cost of the remediation
	AnnualCostMinor         int64 // ongoing annual cost of the remediation
	ExpectedReductionPct    int64 // expected reduction in occurrences, 0..100
	RecoverablePct          int64 // share of lost time that is recoverable capacity, 0..100
}

// Validate checks assumption ranges.
func (a Assumptions) Validate() error {
	var v domain.Validator
	v.Check(a.ImplementationCostMinor >= 0 && a.ImplementationCostMinor <= domain.MaxMoneyMinor, "implementation_cost_minor", "must be between 0 and 1000000000000")
	v.Check(a.AnnualCostMinor >= 0 && a.AnnualCostMinor <= domain.MaxMoneyMinor, "annual_cost_minor", "must be between 0 and 1000000000000")
	v.Check(a.ExpectedReductionPct >= 0 && a.ExpectedReductionPct <= 100, "expected_reduction_pct", "must be between 0 and 100")
	v.Check(a.RecoverablePct >= 0 && a.RecoverablePct <= 100, "recoverable_pct", "must be between 0 and 100")
	return v.Err()
}

// Projection is the result of a business case calculation. All money values
// are minor units. Annualised values are linear extrapolations of the
// observation window.
type Projection struct {
	AnnualisedTurnbacks     int64
	AnnualisedMinutes       int64
	AnnualisedCostMinor     int64
	MonthlyTurnbacks        int64
	MonthlyMinutes          int64
	MonthlyCostMinor        int64
	GrossAnnualBenefitMinor int64 // annualised cost × reduction × recoverable
	NetAnnualBenefitMinor   int64 // gross − ongoing annual cost
	FirstYearNetMinor       int64 // net annual − implementation cost
	ThreeYearNetMinor       int64 // 3 × net annual − implementation cost
	PaybackMonthsTenths     int64 // payback in tenths of a month; valid only if PaybackAchievable
	PaybackAchievable       bool
}

// Project computes a business case projection from an observation and
// assumptions using exact rational arithmetic.
func Project(o Observation, a Assumptions) Projection {
	days := int64(o.Days)
	if days < 1 {
		days = 1
	}
	var p Projection
	p.AnnualisedTurnbacks = domain.ScaleMinor(o.Turnbacks, 365, days)
	p.AnnualisedMinutes = domain.ScaleMinor(o.Minutes, 365, days)
	p.AnnualisedCostMinor = domain.ScaleMinor(o.CostMinor, 365, days)
	p.MonthlyTurnbacks = domain.ScaleMinor(o.Turnbacks, 365, days*12)
	p.MonthlyMinutes = domain.ScaleMinor(o.Minutes, 365, days*12)
	p.MonthlyCostMinor = domain.ScaleMinor(o.CostMinor, 365, days*12)

	// Gross benefit uses the exact (unrounded) annualised cost.
	gross := new(big.Rat).Mul(big.NewRat(o.CostMinor, days), big.NewRat(365, 1))
	gross.Mul(gross, big.NewRat(a.ExpectedReductionPct*a.RecoverablePct, 10000))
	p.GrossAnnualBenefitMinor = domain.RoundRat(gross)

	net := new(big.Rat).Sub(gross, new(big.Rat).SetInt64(a.AnnualCostMinor))
	p.NetAnnualBenefitMinor = domain.RoundRat(net)
	first := new(big.Rat).Sub(net, new(big.Rat).SetInt64(a.ImplementationCostMinor))
	p.FirstYearNetMinor = domain.RoundRat(first)
	three := new(big.Rat).Mul(net, big.NewRat(3, 1))
	three.Sub(three, new(big.Rat).SetInt64(a.ImplementationCostMinor))
	p.ThreeYearNetMinor = domain.RoundRat(three)

	if net.Sign() > 0 {
		// months = implementation / (net / 12), in tenths.
		months := new(big.Rat).Mul(new(big.Rat).SetInt64(a.ImplementationCostMinor), big.NewRat(12*10, 1))
		months.Quo(months, net)
		p.PaybackMonthsTenths = domain.RoundRat(months)
		p.PaybackAchievable = true
	}
	return p
}
