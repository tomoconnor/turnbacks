/**
 * Copyright (c) 2026 Tom O'Connor <tom@twinhelix.org>
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

import { ArrowLeft, Download, Printer } from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useParams } from "react-router";
import { downloadUrl } from "@/api/client";
import { useBusinessCase, type AnalyticsParams, type BusinessCaseParams } from "@/api/hooks";
import { TimeSeriesChart } from "@/components/charts/Charts";
import { PeriodPicker } from "@/components/PeriodPicker";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  formatChange,
  formatCount,
  formatDate,
  formatDateTime,
  formatHours,
  formatMoney,
  majorToMinor,
  minorToMajor,
} from "@/lib/format";

type Period = Pick<AnalyticsParams, "period" | "from" | "to">;

function useDebounced<T>(value: T, ms = 350): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

export function BusinessCasePage() {
  const { id } = useParams();
  const [period, setPeriod] = useState<Period | null>(null);
  type Overrides = Partial<
    Pick<
      BusinessCaseParams,
      "implementation_cost_minor" | "annual_cost_minor" | "expected_reduction_pct" | "recoverable_pct"
    >
  >;
  const [overrides, setOverrides] = useState<Overrides>({});
  const debounced = useDebounced(overrides);
  const params: BusinessCaseParams = { ...(period ?? {}), ...debounced };
  const bc = useBusinessCase(id, params);
  const d = bc.data;

  if (bc.error) return <p className="text-sm text-destructive">{bc.error.message}</p>;
  if (!d) return <p className="text-sm text-muted-foreground">Calculating…</p>;

  const money = (v: number, o?: { compact?: boolean; whole?: boolean }) =>
    formatMoney(v, d.currency, d.currency_exponent, o);
  const pr = d.projection;
  const a = d.assumptions;
  const majorInput = (key: "implementation_cost_minor" | "annual_cost_minor") => ({
    type: "number" as const,
    min: 0,
    step: "any",
    value: minorToMajor(overrides[key] ?? a[key], d.currency_exponent),
    onChange: (e: React.ChangeEvent<HTMLInputElement>) =>
      setOverrides({
        ...overrides,
        [key]: Math.max(0, majorToMinor(Number(e.target.value) || 0, d.currency_exponent)),
      }),
  });
  const pctInput = (key: "expected_reduction_pct" | "recoverable_pct") => ({
    type: "number" as const,
    min: 0,
    max: 100,
    value: overrides[key] ?? a[key],
    onChange: (e: React.ChangeEvent<HTMLInputElement>) =>
      setOverrides({
        ...overrides,
        [key]: Math.min(100, Math.max(0, Math.round(Number(e.target.value) || 0))),
      }),
  });
  const csv = downloadUrl(`/problems/${d.problem.id}/business-case/export`, { ...params });

  return (
    <article className="space-y-6 print:space-y-4" data-testid="business-case">
      <div className="no-print flex flex-wrap items-center justify-between gap-3">
        <Button asChild variant="ghost" size="sm">
          <Link to={`/problems/${d.problem.id}`}>
            <ArrowLeft /> Back to problem
          </Link>
        </Button>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" asChild>
            <a href={csv} download>
              <Download /> CSV
            </a>
          </Button>
          <Button size="sm" onClick={() => window.print()}>
            <Printer /> Print / save as PDF
          </Button>
        </div>
      </div>

      <header className="space-y-1">
        <p className="text-sm text-muted-foreground">Business case · {d.org_name}</p>
        <h1 className="text-2xl font-semibold tracking-tight">{d.problem.title}</h1>
        <p className="text-sm text-muted-foreground">
          {d.problem.category_name} · status {d.problem.status} · observation period{" "}
          {formatDate(d.period.from)} – {formatDate(d.period.to)} ({d.period.days} days, {d.period.timezone})
          · generated {formatDateTime(d.generated_at)}
        </p>
        {d.problem.description && <p className="max-w-3xl pt-2 text-sm">{d.problem.description}</p>}
      </header>

      <div className="no-print flex flex-wrap items-center gap-2">
        <span className="text-sm text-muted-foreground">Observation period:</span>
        <PeriodPicker
          value={period ?? { period: "custom", from: d.period.from, to: d.period.to }}
          onChange={setPeriod}
        />
        {period && (
          <Button variant="link" size="sm" onClick={() => setPeriod(null)}>
            Reset to since first report
          </Button>
        )}
      </div>

      <section className="grid gap-4 md:grid-cols-2 print:grid-cols-2">
        <Card className="print-break-avoid">
          <CardHeader>
            <CardTitle className="text-base">What was observed</CardTitle>
            <CardDescription>Reported facts for the observation period</CardDescription>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
              <Metric label="Reported occurrences" value={formatCount(d.totals.turnbacks)} />
              <Metric
                label="People affected"
                value={formatCount(d.totals.distinct_reporters)}
                sub="distinct network addresses and Slack users"
              />
              <Metric
                label="Reported time lost"
                value={formatHours(d.totals.minutes)}
                sub={`${formatCount(d.totals.estimated_turnbacks)} of ${formatCount(d.totals.turnbacks)} durations estimated`}
              />
              <Metric
                label="Estimated productivity cost"
                value={money(d.totals.cost_minor, { whole: true })}
                sub="estimate, not cash spent"
              />
              <Metric
                label="Average monthly impact"
                value={money(pr.monthly_cost_minor, { whole: true })}
                sub={`${formatCount(pr.monthly_turnbacks)} occurrences · ${formatHours(pr.monthly_minutes)}`}
              />
              <Metric
                label="Annualised impact"
                value={money(pr.annualised_cost_minor, { whole: true })}
                sub={`${formatCount(pr.annualised_turnbacks)} occurrences · ${formatHours(pr.annualised_minutes)}`}
              />
            </dl>
          </CardContent>
        </Card>

        <Card className="print-break-avoid">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              Proposed remediation <Badge variant="outline">assumptions: {a.source}</Badge>
            </CardTitle>
            <CardDescription>
              Adjust the assumptions; the projection is recalculated on the server.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-3">
              <AssumptionInput
                id="impl"
                label={`One-off cost (${d.currency})`}
                value={money(a.implementation_cost_minor, { whole: true })}
                input={majorInput("implementation_cost_minor")}
              />
              <AssumptionInput
                id="annual"
                label={`Ongoing cost / year (${d.currency})`}
                value={money(a.annual_cost_minor, { whole: true })}
                input={majorInput("annual_cost_minor")}
              />
              <AssumptionInput
                id="reduction"
                label="Expected reduction in occurrences (%)"
                value={`${a.expected_reduction_pct}%`}
                input={pctInput("expected_reduction_pct")}
              />
              <AssumptionInput
                id="recoverable"
                label="Lost time that is recoverable (%)"
                value={`${a.recoverable_pct}%`}
                input={pctInput("recoverable_pct")}
              />
            </div>
            <dl className="grid grid-cols-2 gap-x-4 gap-y-3 border-t pt-4 text-sm">
              <Metric
                label="Projected gross annual productivity benefit"
                value={money(pr.gross_annual_benefit_minor, { whole: true })}
              />
              <Metric
                label="Projected net annual benefit"
                value={money(pr.net_annual_benefit_minor, { whole: true })}
                sub="after ongoing cost"
              />
              <Metric
                label="Estimated payback period"
                value={pr.payback_months != null ? `${pr.payback_months} months` : "Not achieved"}
                testId="payback"
              />
              <Metric
                label="Net over three years"
                value={money(pr.three_year_net_minor, { whole: true })}
                sub={`first year ${money(pr.first_year_net_minor, { whole: true })}`}
              />
            </dl>
          </CardContent>
        </Card>
      </section>

      <Alert variant="warning" className="print-break-avoid">
        <AlertDescription>
          Estimated productivity cost and projected benefits are estimates derived from self-reported time and
          an assumed hourly cost of {money(d.default_hourly_rate_minor)} (or team-specific rates). They are
          not guaranteed, recoverable cash savings. The remediation costs above are expenditure.
        </AlertDescription>
      </Alert>

      <Card className="print-break-avoid">
        <CardHeader>
          <CardTitle className="text-base">Trend (estimated cost per month)</CardTitle>
        </CardHeader>
        <CardContent>
          <TimeSeriesChart points={d.trend} bucket="month" metric="cost" money={money} height={220} />
        </CardContent>
      </Card>

      {d.remediations.length > 0 && (
        <Card className="print-break-avoid">
          <CardHeader>
            <CardTitle className="text-base">Remediation history: before and after</CardTitle>
            <CardDescription>{d.remediations[0].caveat}</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Remediation</TableHead>
                  <TableHead>Window</TableHead>
                  <TableHead className="text-right">Occurrences</TableHead>
                  <TableHead className="text-right">Time lost</TableHead>
                  <TableHead className="text-right">Est. cost</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {d.remediations.map((c) => (
                  <TableRow key={c.remediation.id}>
                    <TableCell>
                      <div className="font-medium">{formatDate(c.remediation.implemented_on)}</div>
                      <div className="text-xs text-muted-foreground">{c.remediation.description}</div>
                    </TableCell>
                    <TableCell className="text-xs">
                      {c.window_days} days each side
                      {c.insufficient_data && (
                        <Badge variant="warning" className="ml-1">
                          too soon
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {c.before.totals.turnbacks} → {c.after.totals.turnbacks}
                      <div className="text-xs text-muted-foreground">
                        {formatChange(c.change.turnbacks_bp)}
                      </div>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatHours(c.before.totals.minutes)} → {formatHours(c.after.totals.minutes)}
                      <div className="text-xs text-muted-foreground">{formatChange(c.change.minutes_bp)}</div>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {money(c.before.totals.cost_minor, { whole: true })} →{" "}
                      {money(c.after.totals.cost_minor, { whole: true })}
                      <div className="text-xs text-muted-foreground">{formatChange(c.change.cost_bp)}</div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {d.notes.length > 0 && (
        <Card className="print-break-avoid">
          <CardHeader>
            <CardTitle className="text-base">In their words</CardTitle>
            <CardDescription>Representative recent notes, anonymised</CardDescription>
          </CardHeader>
          <CardContent>
            <ul className="space-y-2 text-sm">
              {d.notes.map((n, i) => (
                <li key={i} className="border-l-2 pl-3">
                  “{n.note}”{" "}
                  <span className="text-xs text-muted-foreground">
                    — {formatDate(n.occurred_at.slice(0, 10))}, {n.duration_minutes} min
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <Card className="print-break-avoid">
        <CardHeader>
          <CardTitle className="text-base">Methodology</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
            {d.methodology.map((m) => (
              <li key={m}>{m}</li>
            ))}
          </ul>
        </CardContent>
      </Card>
    </article>
  );
}

function Metric({
  label,
  value,
  sub,
  testId,
}: {
  label: string;
  value: string;
  sub?: string;
  testId?: string;
}) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-lg font-semibold tabular-nums" data-testid={testId}>
        {value}
      </dd>
      {sub && <dd className="text-xs text-muted-foreground">{sub}</dd>}
    </div>
  );
}

function AssumptionInput({
  id,
  label,
  value,
  input,
}: {
  id: string;
  label: string;
  value: string;
  input: React.ComponentProps<typeof Input>;
}) {
  return (
    <div className="space-y-1">
      <Label htmlFor={id} className="text-xs leading-snug">
        {label}
      </Label>
      <Input id={id} className="no-print" {...input} />
      <div className="hidden text-sm font-medium print:block">{value}</div>
    </div>
  );
}
