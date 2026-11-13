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

import { ArrowDownRight, ArrowUpRight, Minus } from "lucide-react";
import type { ReactNode } from "react";
import { Card, CardContent } from "@/components/ui/card";
import { formatChange } from "@/lib/format";
import { cn } from "@/lib/utils";

/** A headline metric. For friction, an increase is bad, so "up" is shown in red. */
export function StatCard({
  label,
  value,
  changeBp,
  hint,
  testId,
}: {
  label: string;
  value: ReactNode;
  changeBp?: number | null;
  hint?: string;
  testId?: string;
}) {
  const up = changeBp != null && changeBp > 0;
  const down = changeBp != null && changeBp < 0;
  return (
    <Card className="gap-1 py-4" data-testid={testId}>
      <CardContent className="space-y-1 px-4">
        <div className="text-xs font-medium text-muted-foreground">{label}</div>
        <div className="text-2xl font-semibold tabular-nums">{value}</div>
        {changeBp !== undefined && (
          <div
            className={cn(
              "flex items-center gap-1 text-xs",
              up && "text-red-700 dark:text-red-400",
              down && "text-emerald-700 dark:text-emerald-400",
              !up && !down && "text-muted-foreground",
            )}
            title="Change against the preceding equivalent period"
          >
            {up ? (
              <ArrowUpRight className="size-3" />
            ) : down ? (
              <ArrowDownRight className="size-3" />
            ) : (
              <Minus className="size-3" />
            )}
            {formatChange(changeBp)} vs previous
          </div>
        )}
        {hint && <div className="text-xs text-muted-foreground">{hint}</div>}
      </CardContent>
    </Card>
  );
}
