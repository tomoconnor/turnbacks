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

import type { AnalyticsParams } from "@/api/hooks";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/select";

export const PERIOD_LABELS: Record<AnalyticsParams["period"], string> = {
  last_7_days: "Last 7 days",
  last_30_days: "Last 30 days",
  last_90_days: "Last 90 days",
  this_year: "This year",
  custom: "Custom range",
};

export function PeriodPicker({
  value,
  onChange,
}: {
  value: Pick<AnalyticsParams, "period" | "from" | "to">;
  onChange: (v: Pick<AnalyticsParams, "period" | "from" | "to">) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <NativeSelect
        aria-label="Reporting period"
        className="w-40"
        value={value.period}
        onChange={(e) => {
          const period = e.target.value as AnalyticsParams["period"];
          if (period === "custom") {
            const to = new Date().toISOString().slice(0, 10);
            const from = new Date(Date.now() - 29 * 864e5).toISOString().slice(0, 10);
            onChange({ period, from: value.from ?? from, to: value.to ?? to });
          } else onChange({ period });
        }}
      >
        {Object.entries(PERIOD_LABELS).map(([k, v]) => (
          <option key={k} value={k}>
            {v}
          </option>
        ))}
      </NativeSelect>
      {value.period === "custom" && (
        <>
          <Input
            type="date"
            aria-label="From"
            className="w-40"
            value={value.from ?? ""}
            onChange={(e) => onChange({ ...value, from: e.target.value })}
          />
          <span className="text-sm text-muted-foreground">to</span>
          <Input
            type="date"
            aria-label="To"
            className="w-40"
            value={value.to ?? ""}
            onChange={(e) => onChange({ ...value, to: e.target.value })}
          />
        </>
      )}
    </div>
  );
}
