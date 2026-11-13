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

import {
  Bar,
  BarChart,
  CartesianGrid,
  ComposedChart,
  Legend,
  Line,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { Schemas } from "@/api/client";
import { formatCount, formatDate, formatHours } from "@/lib/format";

export type Metric = "count" | "minutes" | "cost";

const axisStyle = { fontSize: 12, fill: "var(--muted-foreground)" };
const tooltipStyle = {
  contentStyle: {
    borderRadius: 8,
    border: "1px solid var(--border)",
    fontSize: 12,
    background: "var(--popover)",
  },
};

export function metricValue(
  m: Metric,
  p: { turnbacks: number; minutes: number; cost_minor: number },
): number {
  return m === "count" ? p.turnbacks : m === "minutes" ? p.minutes : p.cost_minor;
}

export function metricFormatter(
  m: Metric,
  money: (v: number, o?: { compact?: boolean; whole?: boolean }) => string,
) {
  return (v: number) =>
    m === "count" ? formatCount(v) : m === "minutes" ? formatHours(v) : money(v, { whole: true });
}

function shortDate(iso: string, bucket: string) {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y, m - 1, d));
  if (bucket === "month")
    return dt.toLocaleDateString("en-GB", { month: "short", year: "numeric", timeZone: "UTC" });
  return dt.toLocaleDateString("en-GB", { day: "numeric", month: "short", timeZone: "UTC" });
}

/** The calendar date (YYYY-MM-DD) of a timestamp in a timezone. */
export function dateInZone(iso: string, timeZone: string): string {
  try {
    return new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(new Date(iso));
  } catch {
    return iso.slice(0, 10);
  }
}

export interface ChartMarker {
  id: string;
  occurredAt: string;
  title: string;
}

/** The start of the bucket that contains date, or undefined if outside the range. */
function bucketFor(starts: string[], date: string): string | undefined {
  let found: string | undefined;
  for (const s of starts) {
    if (s <= date) found = s;
    else break;
  }
  return found;
}

export function TimeSeriesChart({
  points,
  bucket,
  metric,
  money,
  height = 260,
  markers = [],
  timeZone = "UTC",
}: {
  points: Schemas["TimePoint"][];
  bucket: string;
  metric: Metric;
  money: (v: number, o?: { compact?: boolean; whole?: boolean }) => string;
  height?: number;
  /** Changes drawn as dashed vertical lines in the bucket they fall in. */
  markers?: ChartMarker[];
  /** Reporting timezone, to place markers in the right bucket. */
  timeZone?: string;
}) {
  const fmt = metricFormatter(metric, money);
  const data = points.map((p) => ({
    label: shortDate(p.start, bucket),
    start: p.start,
    value: metricValue(metric, p),
  }));
  const starts = data.map((d) => d.start);
  const lastEnd = points.length ? points[points.length - 1].start : "";
  const placed = new Map<string, string[]>();
  for (const m of markers) {
    const day = dateInZone(m.occurredAt, timeZone);
    const at = bucketFor(starts, day);
    if (!at || (bucket === "day" && day > lastEnd)) continue;
    placed.set(at, [...(placed.get(at) ?? []), m.title]);
  }
  const axisFmt = (v: number) =>
    metric === "cost"
      ? money(v, { compact: true })
      : metric === "minutes"
        ? `${Math.round(v / 60)}h`
        : String(v);
  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={data} margin={{ top: placed.size ? 22 : 8, right: 8, bottom: 0, left: 0 }}>
        <CartesianGrid vertical={false} stroke="var(--border)" />
        <XAxis
          dataKey="start"
          tick={axisStyle}
          tickLine={false}
          axisLine={false}
          minTickGap={16}
          tickFormatter={(v: string) => shortDate(v, bucket)}
        />
        <YAxis tick={axisStyle} tickLine={false} axisLine={false} width={56} tickFormatter={axisFmt} />
        <Tooltip
          {...tooltipStyle}
          formatter={(v) => [
            fmt(Number(v)),
            metric === "count" ? "Turnbacks" : metric === "minutes" ? "Time lost" : "Est. cost",
          ]}
          labelFormatter={(_, payload) =>
            payload?.[0]
              ? `${bucket === "day" ? "" : `${bucket} of `}${formatDate(payload[0].payload.start)}`
              : ""
          }
        />
        <Bar dataKey="value" fill="var(--chart-1)" radius={[3, 3, 0, 0]} isAnimationActive={false} />
        {[...placed].map(([at, titles]) => (
          <ReferenceLine
            key={at}
            x={at}
            stroke="var(--chart-2)"
            strokeWidth={2}
            strokeDasharray="4 3"
            ifOverflow="extendDomain"
            label={{
              value: titles.length > 1 ? `${titles.length} changes` : truncate(titles[0], 24),
              position: "top",
              fontSize: 11,
              fill: "var(--muted-foreground)",
            }}
          />
        ))}
      </BarChart>
    </ResponsiveContainer>
  );
}

function truncate(s: string, n: number) {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s;
}

export function ParetoChart({
  items,
  metric,
  money,
}: {
  items: Schemas["RankedProblem"][];
  metric: Metric;
  money: (v: number, o?: { compact?: boolean; whole?: boolean }) => string;
}) {
  const fmt = metricFormatter(metric, money);
  const data = items.map((i) => ({
    name: i.alias ?? (i.title.length > 18 ? `${i.title.slice(0, 17)}…` : i.title),
    title: i.title,
    value: metricValue(metric, i),
    cumulative: i.cumulative_share_bp / 100,
  }));
  return (
    <ResponsiveContainer width="100%" height={300}>
      <ComposedChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
        <CartesianGrid vertical={false} stroke="var(--border)" />
        <XAxis dataKey="name" tick={axisStyle} tickLine={false} axisLine={false} interval={0} height={40} />
        <YAxis
          yAxisId="v"
          tick={axisStyle}
          tickLine={false}
          axisLine={false}
          width={56}
          tickFormatter={(v: number) =>
            metric === "cost"
              ? money(v, { compact: true })
              : metric === "minutes"
                ? `${Math.round(v / 60)}h`
                : String(v)
          }
        />
        <YAxis
          yAxisId="p"
          orientation="right"
          domain={[0, 100]}
          tick={axisStyle}
          tickLine={false}
          axisLine={false}
          width={40}
          tickFormatter={(v: number) => `${v}%`}
        />
        <Tooltip
          {...tooltipStyle}
          formatter={(v, name) =>
            name === "Cumulative share" ? [`${Number(v).toFixed(1)}%`, name] : [fmt(Number(v)), name]
          }
          labelFormatter={(_, p) => p?.[0]?.payload.title ?? ""}
        />
        <Legend wrapperStyle={{ fontSize: 12 }} />
        <ReferenceLine
          yAxisId="p"
          y={80}
          stroke="var(--chart-4)"
          strokeDasharray="4 4"
          label={{ value: "80%", fontSize: 11, fill: "var(--chart-4)", position: "insideTopRight" }}
        />
        <Bar
          yAxisId="v"
          dataKey="value"
          name={metric === "count" ? "Turnbacks" : metric === "minutes" ? "Time lost" : "Est. cost"}
          fill="var(--chart-1)"
          radius={[3, 3, 0, 0]}
          isAnimationActive={false}
        />
        <Line
          yAxisId="p"
          dataKey="cumulative"
          name="Cumulative share"
          stroke="var(--chart-2)"
          strokeWidth={2}
          dot={{ r: 3 }}
          isAnimationActive={false}
        />
      </ComposedChart>
    </ResponsiveContainer>
  );
}

export function HorizontalBars({
  data,
  format,
  height,
}: {
  data: { name: string; value: number }[];
  format: (v: number) => string;
  height?: number;
}) {
  data = data.map((d) => ({ ...d, name: d.name.length > 24 ? `${d.name.slice(0, 23)}…` : d.name }));
  return (
    <ResponsiveContainer width="100%" height={height ?? Math.max(120, data.length * 36 + 20)}>
      <BarChart data={data} layout="vertical" margin={{ top: 0, right: 16, bottom: 0, left: 0 }}>
        <CartesianGrid horizontal={false} stroke="var(--border)" />
        <XAxis type="number" tick={axisStyle} tickLine={false} axisLine={false} tickFormatter={format} />
        <YAxis
          type="category"
          dataKey="name"
          tick={axisStyle}
          tickLine={false}
          axisLine={false}
          width={170}
        />
        <Tooltip {...tooltipStyle} formatter={(v) => [format(Number(v)), ""]} />
        <Bar dataKey="value" fill="var(--chart-3)" radius={[0, 3, 3, 0]} isAnimationActive={false} />
      </BarChart>
    </ResponsiveContainer>
  );
}

export function BeforeAfterChart({ items }: { items: Schemas["Comparison"][] }) {
  const data = items.map((c) => ({
    name: c.problem_title.length > 22 ? `${c.problem_title.slice(0, 21)}…` : c.problem_title,
    Before: c.before.totals.turnbacks,
    After: c.after.totals.turnbacks,
  }));
  return (
    <ResponsiveContainer width="100%" height={Math.max(160, items.length * 70)}>
      <BarChart data={data} layout="vertical" margin={{ top: 0, right: 16, bottom: 0, left: 0 }}>
        <CartesianGrid horizontal={false} stroke="var(--border)" />
        <XAxis type="number" tick={axisStyle} tickLine={false} axisLine={false} />
        <YAxis
          type="category"
          dataKey="name"
          tick={axisStyle}
          tickLine={false}
          axisLine={false}
          width={160}
        />
        <Tooltip {...tooltipStyle} />
        <Legend wrapperStyle={{ fontSize: 12 }} />
        <Bar dataKey="Before" fill="var(--chart-4)" radius={[0, 3, 3, 0]} isAnimationActive={false} />
        <Bar dataKey="After" fill="var(--chart-3)" radius={[0, 3, 3, 0]} isAnimationActive={false} />
      </BarChart>
    </ResponsiveContainer>
  );
}
