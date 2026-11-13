# Financial calculations

Turnbacks turns self-reported lost time into an **estimate** of productivity cost and uses it to
support (not prove) investment decisions. Every figure is labelled, every assumption is visible and
editable, and the methodology is printed on each business case.

## Terms

| Term | Meaning | Source |
| --- | --- | --- |
| **Reported occurrences** | Number of turnback records | Counted rows |
| **Reported time lost** | Sum of reported durations | Self-reported (often an estimate — see `duration_estimated`) |
| **Estimated productivity cost** | Time lost valued at a fully loaded hourly employment cost | Calculation; *not* cash spent |
| **Actual expenditure** | Implementation and ongoing costs of a remediation | Entered by administrators |
| **Projected financial benefit** | Expected reduction in estimated productivity cost after a remediation | Projection from assumptions |

Estimated productivity cost is **not** recoverable cash. People rarely sit idle when blocked, some of
the time may be absorbed elsewhere, and self-reports are incomplete. The *recoverable share* assumption
(default from settings, adjustable per business case) exists to make this explicit.

## Estimated productivity cost

```
cost = reported minutes × hourly rate ÷ 60
```

- The hourly rate is the **team's** rate when the report has a team with a configured rate,
  otherwise the **organisation default** (default £50.00/h). Reports without a team use the default.
- Rates are **current** settings: changing a rate re-prices historical reports. This keeps estimates
  consistent and the assumption transparent (“at today's £50/h…”). Record a remediation or export data
  if you need a frozen snapshot.

### Precision

- Money is stored in integer **minor units** (pence). There is no floating-point arithmetic anywhere
  in the calculation path.
- In SQL, each aggregate computes `sum(minutes × rate_minor)` exactly as `NUMERIC` (“minute-pence”)
  and divides by 60 **once**, rounding half away from zero. Totals therefore never accumulate per-row
  rounding error, and the dashboard total equals an independent recomputation (verified by
  `TestSeedTotalsAgree`).
- Per-problem figures are each rounded once, so the sum of per-problem costs can differ from the total
  by at most one minor unit per problem.
- Go uses `math/big.Rat` for projections with the same rounding rule.

Example: one 10-minute report at £50/h is 10 × 5000 ÷ 60 = 833.33… → **£8.33**. Three such reports
total 3 × 10 × 5000 = 150 000 minute-pence ÷ 60 = 2500p = **£25.00** exactly, whereas rounding each
report first would give 3 × 833p = £24.99. Rounding happens once, at the end.

## Dashboard metrics

| Metric | Definition |
| --- | --- |
| Reported turnbacks | `count(*)` in the period |
| Reported time lost | `sum(duration_minutes)`, shown in hours |
| Est. productivity cost | as above |
| People affected | Distinct reporters: network addresses (each person has their own VPN address) and Slack users |
| Avg. per turnback | minutes ÷ occurrences, one decimal |
| Change vs previous | `(current − previous) ÷ previous`, in basis points; “n/a” when previous is zero. The previous period is the same-length window immediately before (for “this year”: the same dates last year) |

Periods are calendar days in the organisation's reporting timezone. “Last 30 days” includes today.

## Business case

For a problem and an **observation period** (default: from the first report, at most 365 days back, to
today):

```
days                  = calendar days in the observation period
annualised cost       = observed cost × 365 ÷ days
monthly impact        = annualised cost ÷ 12
gross annual benefit  = annualised cost × expected reduction % × recoverable %
net annual benefit    = gross annual benefit − ongoing annual cost
first-year net        = net annual benefit − implementation cost
three-year net        = 3 × net annual benefit − implementation cost
payback (months)      = implementation cost ÷ (net annual benefit ÷ 12)   [only if net > 0]
```

Assumptions default, in order, to: values supplied in the request → the most recent remediation
record for the problem → built-in defaults (no costs, 50 % expected reduction, recoverable share from
settings). The response states which source was used.

### Worked example

90 days observed, 120 occurrences, 30 h lost at £50/h → £1,500.00 estimated cost.

| Step | Value |
| --- | --- |
| Annualised cost | 1500 × 365 ÷ 90 = **£6,083.33** |
| Monthly impact | **£506.94** |
| Assumptions | £3,000 implementation, £600/yr ongoing, 80 % reduction, 100 % recoverable |
| Gross annual benefit | 6083.33 × 0.8 = **£4,866.67** |
| Net annual benefit | 4866.67 − 600 = **£4,266.67** |
| Payback | 3000 ÷ (4266.67 ÷ 12) = **8.4 months** |
| Three-year net | 3 × 4266.67 − 3000 = **£9,800.00** |

(These figures are asserted in `internal/reporting/reporting_test.go`.)

## Before-and-after comparisons

For each remediation, Turnbacks compares two **equal-length windows**: the `N` days before the
implementation date and the `N` days from it, where `N` = min(requested window, complete days elapsed
since implementation), default 90. Windows shorter than 14 days are flagged “too soon to judge”.

Reported changes in frequency, time lost and estimated cost are **observational**: changes in reporting
behaviour, staffing, seasonality or other events can also explain them. The UI and API say so.

## Data-quality safeguards

- Unusually long durations (above `large_duration_minutes`, default 120) and quantities above 20
  require explicit confirmation.
- A quantity creates individual occurrences (15 × 10 min = 15 occurrences, 150 minutes) linked by a
  `batch_id`, so bulk entries can be identified and undone together.
- The share of estimated durations is shown on the dashboard.
- Team comparisons show reporting participation (distinct reporters ÷ team headcount), because low
  participation means under-reporting, not fewer problems.
- Missing reports are never treated as zero disruption — the dashboard states this.
