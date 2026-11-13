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

import { Download, Info } from "lucide-react";
import { useState } from "react";
import { Link, useSearchParams } from "react-router";
import { downloadUrl } from "@/api/client";
import {
  useCategories,
  useCategoryBreakdown,
  useChanges,
  useRanking,
  useRemediationOverview,
  useSettings,
  useSummary,
  useTeamBreakdown,
  useTeams,
  useTimeseries,
  type AnalyticsParams,
} from "@/api/hooks";
import {
  BeforeAfterChart,
  HorizontalBars,
  metricFormatter,
  metricValue,
  ParetoChart,
  TimeSeriesChart,
  type Metric,
} from "@/components/charts/Charts";
import { ChangesCard } from "@/components/ChangeMarkers";
import { PeriodPicker } from "@/components/PeriodPicker";
import { StatCard } from "@/components/StatCard";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { NativeSelect } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Segmented } from "@/components/ui/segmented";
import { formatChange, formatCount, formatDate, formatHours, formatShare } from "@/lib/format";
import { useSession } from "@/lib/session";
import { useMoney } from "@/lib/useMoney";

const METRIC_LABEL: Record<Metric, string> = { count: "Frequency", minutes: "Time lost", cost: "Est. cost" };

function useFilters(): [AnalyticsParams, (p: Partial<AnalyticsParams>) => void] {
  const [sp, setSp] = useSearchParams();
  const params: AnalyticsParams = {
    period: (sp.get("period") as AnalyticsParams["period"]) ?? "last_30_days",
    from: sp.get("from") ?? undefined,
    to: sp.get("to") ?? undefined,
    category_id: sp.get("category_id") ?? undefined,
    team_id: sp.get("team_id") ?? undefined,
  };
  const update = (p: Partial<AnalyticsParams>) => {
    const next = { ...params, ...p };
    const out = new URLSearchParams();
    for (const [k, v] of Object.entries(next)) if (v) out.set(k, String(v));
    if (next.period !== "custom") {
      out.delete("from");
      out.delete("to");
    }
    setSp(out, { replace: true });
  };
  return [params, update];
}

export function DashboardPage() {
  const [f, setF] = useFilters();
  const [metric, setMetric] = useState<Metric>("cost");
  const [rankMetric, setRankMetric] = useState<Metric>("cost");
  const { fmt } = useMoney();
  const settings = useSettings();
  const categories = useCategories();
  const teams = useTeams();

  const summary = useSummary(f);
  const series = useTimeseries(f);
  const ranking = useRanking({ ...f, sort: rankMetric, limit: 10 });
  const pareto = useRanking({ ...f, sort: "cost", limit: 15 });
  const cats = useCategoryBreakdown(f);
  const teamRows = useTeamBreakdown(f);
  const remediations = useRemediationOverview();
  const { identityMode } = useSession();
  const changes = useChanges(
    { from: summary.data?.period.start, to: summary.data?.period.end },
    !!summary.data,
  );

  const s = summary.data;
  const exportLink = (dataset: string) =>
    downloadUrl("/analytics/export", {
      dataset,
      period: f.period,
      from: f.from,
      to: f.to,
      category_id: f.category_id,
      team_id: f.team_id,
    });
  const rankFmt = metricFormatter(rankMetric, fmt);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
          {s && (
            <p className="text-sm text-muted-foreground">
              {formatDate(s.period.from)} – {formatDate(s.period.to)} ({s.period.timezone}) compared with{" "}
              {formatDate(s.previous_period.from)} – {formatDate(s.previous_period.to)}
            </p>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <PeriodPicker value={f} onChange={(v) => setF(v)} />
          <NativeSelect
            aria-label="Category filter"
            className="w-44"
            value={f.category_id ?? ""}
            onChange={(e) => setF({ category_id: e.target.value || undefined })}
          >
            <option value="">All categories</option>
            {categories.data?.items.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </NativeSelect>
          <NativeSelect
            aria-label="Team filter"
            className="w-44"
            value={f.team_id ?? ""}
            onChange={(e) => setF({ team_id: e.target.value || undefined })}
          >
            <option value="">All teams</option>
            {teams.data?.items.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </NativeSelect>
        </div>
      </div>

      <section aria-label="Headline metrics" className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <StatCard
          testId="metric-turnbacks"
          label="Reported turnbacks"
          value={s ? formatCount(s.current.turnbacks) : "—"}
          changeBp={s?.change.turnbacks_bp}
        />
        <StatCard
          testId="metric-hours"
          label="Reported time lost"
          value={s ? formatHours(s.current.minutes) : "—"}
          changeBp={s?.change.minutes_bp}
        />
        <StatCard
          testId="metric-cost"
          label="Est. productivity cost"
          value={s ? fmt(s.current.cost_minor, { whole: true }) : "—"}
          changeBp={s?.change.cost_bp}
          hint="Estimate · not cash spent"
        />
        <StatCard
          label="People affected"
          value={s ? formatCount(s.current.distinct_reporters) : "—"}
          hint={
            identityMode === "header"
              ? "distinct people (single sign-on users)"
              : "distinct network addresses and chat users"
          }
        />
        <StatCard
          label="Avg. per turnback"
          value={s ? `${s.current.avg_minutes} min` : "—"}
          hint={s ? `previous ${s.previous.avg_minutes} min` : undefined}
        />
        <StatCard label="Distinct problems" value={s ? formatCount(s.current.distinct_problems) : "—"} />
      </section>

      <Card>
        <CardHeader className="flex-row flex-wrap items-center justify-between gap-2">
          <div>
            <CardTitle className="text-base">Over time</CardTitle>
            <CardDescription>By {series.data?.bucket ?? "day"} in the reporting timezone</CardDescription>
          </div>
          <Segmented
            label="Metric"
            value={metric}
            onChange={setMetric}
            options={[
              { value: "count", label: "Turnbacks" },
              { value: "minutes", label: "Hours lost" },
              { value: "cost", label: "Est. cost" },
            ]}
          />
        </CardHeader>
        <CardContent>
          {series.data && (
            <TimeSeriesChart
              points={series.data.points}
              bucket={series.data.bucket}
              metric={metric}
              money={fmt}
              timeZone={settings.data?.reporting_timezone}
              markers={changes.data?.items
                .filter((m) => !f.problem_id || !m.problem_id || m.problem_id === f.problem_id)
                .map((m) => ({ id: m.id, occurredAt: m.occurred_at, title: m.title }))}
            />
          )}
        </CardContent>
      </Card>

      <div className="grid gap-6 xl:grid-cols-2">
        <Card>
          <CardHeader className="flex-row flex-wrap items-center justify-between gap-2">
            <CardTitle className="text-base">Top problems</CardTitle>
            <Segmented
              label="Rank by"
              value={rankMetric}
              onChange={setRankMetric}
              options={(Object.keys(METRIC_LABEL) as Metric[]).map((m) => ({
                value: m,
                label: METRIC_LABEL[m],
              }))}
            />
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Problem</TableHead>
                  <TableHead className="text-right">{METRIC_LABEL[rankMetric]}</TableHead>
                  <TableHead className="text-right">Share</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody data-testid="top-problems">
                {ranking.data?.items.map((r) => (
                  <TableRow key={r.problem_id}>
                    <TableCell>
                      <Link to={`/problems/${r.problem_id}`} className="hover:underline">
                        {r.title}
                      </Link>
                      <div className="text-xs text-muted-foreground">{r.category_name}</div>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {rankFmt(metricValue(rankMetric, r))}
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground tabular-nums">
                      {formatShare(r.share_bp)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {ranking.data?.items.length === 0 && (
              <p className="py-4 text-sm text-muted-foreground">No turnbacks in this period.</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Pareto: where the estimated cost concentrates</CardTitle>
            <CardDescription>{pareto.data && paretoSummary(pareto.data.items)}</CardDescription>
          </CardHeader>
          <CardContent>
            {pareto.data && pareto.data.items.length > 0 && (
              <ParetoChart items={pareto.data.items} metric="cost" money={fmt} />
            )}
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-6 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">By category (est. cost)</CardTitle>
          </CardHeader>
          <CardContent>
            {cats.data && (
              <HorizontalBars
                data={cats.data.items.map((c) => ({ name: c.name, value: c.cost_minor }))}
                format={(v) => fmt(v, { compact: true })}
              />
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Remediations: before and after</CardTitle>
            <CardDescription>
              Turnbacks in equal windows either side of each remediation. Observational, not proof of cause.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {remediations.data && remediations.data.items.length === 0 && (
              <p className="text-sm text-muted-foreground">No remediations recorded yet.</p>
            )}
            {remediations.data && remediations.data.items.length > 0 && (
              <>
                <BeforeAfterChart items={remediations.data.items.filter((c) => c.window_days > 0)} />
                <ul className="space-y-1 text-xs text-muted-foreground">
                  {remediations.data.items.map((c) => (
                    <li key={c.remediation.id}>
                      <Link className="underline" to={`/problems/${c.remediation.problem_id}`}>
                        {c.problem_title}
                      </Link>
                      : {c.window_days}-day windows, turnbacks {formatChange(c.change.turnbacks_bp)}, time
                      lost {formatChange(c.change.minutes_bp)}
                      {c.insufficient_data && " (too soon to judge)"}
                    </li>
                  ))}
                </ul>
              </>
            )}
          </CardContent>
        </Card>
      </div>

      <ChangesCard />

      {
        <Card>
          <CardHeader>
            <CardTitle className="text-base">By team</CardTitle>
            <CardDescription>
              Teams report at different rates. Compare participation before comparing totals; low
              participation means under-reporting, not fewer problems.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Team</TableHead>
                  <TableHead className="text-right">Turnbacks</TableHead>
                  <TableHead className="text-right">Time lost</TableHead>
                  <TableHead className="text-right">Est. cost</TableHead>
                  <TableHead className="text-right">Reporting participation</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {teamRows.data?.items.map((t) => (
                  <TableRow key={t.team_id ?? "none"}>
                    <TableCell>
                      {t.team_name ?? <span className="text-muted-foreground">No team / anonymous</span>}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{formatCount(t.turnbacks)}</TableCell>
                    <TableCell className="text-right tabular-nums">{formatHours(t.minutes)}</TableCell>
                    <TableCell className="text-right tabular-nums">
                      {fmt(t.cost_minor, { whole: true })}
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground tabular-nums">
                      {t.participation_bp != null
                        ? `${t.distinct_reporters} of ${t.headcount} (${formatShare(t.participation_bp)})`
                        : "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      }

      <Alert variant="info">
        <Info />
        <AlertDescription>
          <p>
            Turnbacks are voluntary self-reports: missing reports are not evidence of zero disruption.
            {s &&
              s.current.turnbacks > 0 &&
              ` ${Math.round((s.current.estimated_turnbacks / s.current.turnbacks) * 100)}% of durations in this period are estimates.`}{" "}
            Estimated productivity cost = reported hours ×{" "}
            {settings.data ? fmt(settings.data.default_hourly_rate_minor) : "the configured rate"}/h (or a
            team-specific rate). It is not cash expenditure and is not necessarily recoverable.
          </p>
        </AlertDescription>
      </Alert>

      {
        <Card className="no-print">
          <CardHeader>
            <CardTitle className="text-base">Export (CSV)</CardTitle>
            <CardDescription>
              “This view” uses the filters above; “Everything” contains all data, for analysis elsewhere or
              moving to another system. Only administrators’ exports include who reported what.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              <span className="w-full text-xs font-medium text-muted-foreground sm:w-28">This view</span>
              {[
                ["problems", "Problems"],
                ["timeseries", "Time series"],
                ["categories", "Categories"],
                ["teams", "Teams"],
                ["turnbacks", "All occurrences"],
              ].map(([k, label]) => (
                <Button key={k} asChild variant="outline" size="sm">
                  <a href={exportLink(k)} download>
                    <Download /> {label}
                  </a>
                </Button>
              ))}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <span className="w-full text-xs font-medium text-muted-foreground sm:w-28">Everything</span>
              {[
                ["turnbacks", "All turnbacks"],
                ["problems", "All problems"],
                ["remediations", "Remediations"],
                ["changes", "Changes"],
              ].map(([k, label]) => (
                <Button key={k} asChild variant="outline" size="sm">
                  <a href={downloadUrl(`/exports/${k}`, {})} download>
                    <Download /> {label}
                  </a>
                </Button>
              ))}
            </div>
          </CardContent>
        </Card>
      }
    </div>
  );
}

function paretoSummary(items: { cumulative_share_bp: number }[]): string {
  if (items.length === 0) return "No data in this period.";
  const idx = items.findIndex((i) => i.cumulative_share_bp >= 8000);
  if (idx < 0)
    return `The top ${items.length} problems account for ${formatShare(items[items.length - 1].cumulative_share_bp)} of estimated cost.`;
  return `${idx + 1} problem${idx === 0 ? "" : "s"} account${idx === 0 ? "s" : ""} for ${formatShare(items[idx].cumulative_share_bp)} of estimated cost.`;
}
