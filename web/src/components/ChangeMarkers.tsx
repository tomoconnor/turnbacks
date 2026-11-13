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

import { ExternalLink, Flag, Trash2 } from "lucide-react";
import { useId, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { ApiError, type Schemas } from "@/api/client";
import { useAllActiveProblems, useChangeOverview, useCreateChange, useDeleteChange } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { formatChange, formatCount, formatDateTime, formatHours } from "@/lib/format";
import { useSession } from "@/lib/session";

/** "YYYY-MM-DDTHH:mm" in local time, for datetime-local inputs. */
function localNow(): string {
  const d = new Date();
  d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
  return d.toISOString().slice(0, 16);
}

function windowLabel(seconds: number): string {
  if (seconds >= 86400) {
    const days = Math.floor(seconds / 86400);
    return `${days} day${days === 1 ? "" : "s"}`;
  }
  const hours = Math.floor(seconds / 3600);
  if (hours >= 1) return `${hours} hour${hours === 1 ? "" : "s"}`;
  return `${Math.max(0, Math.floor(seconds / 60))} min`;
}

export function MarkChangeDialog({
  problem,
  onClose,
}: {
  /** Preselects this problem; otherwise the change is organisation-wide by default. */
  problem?: { id: string; title: string };
  onClose: () => void;
}) {
  const id = useId();
  const problems = useAllActiveProblems();
  const create = useCreateChange();
  const [form, setForm] = useState({
    title: "",
    when: localNow(),
    url: "",
    description: "",
    problem_id: problem?.id ?? "",
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const set =
    (k: keyof typeof form) =>
    (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) =>
      setForm({ ...form, [k]: e.target.value });

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (!form.title.trim()) return setErrors({ title: "Say what changed" });
    try {
      await create.mutateAsync({
        title: form.title.trim(),
        occurred_at: new Date(form.when).toISOString(),
        url: form.url.trim() || null,
        description: form.description.trim(),
        problem_id: form.problem_id || null,
      });
      toast.success("Change marked. Before and after will fill in as reports arrive.");
      onClose();
    } catch (err) {
      if (err instanceof ApiError && err.fields.length) {
        setErrors(Object.fromEntries(err.fields.map((f) => [f.field, f.message])));
      } else {
        toast.error(err instanceof Error ? err.message : "Could not save");
      }
    }
  }

  const options = problems.data?.items ?? [];
  const hasPreselected = problem && !options.some((p) => p.id === problem.id);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Mark a change</DialogTitle>
          <DialogDescription>
            Flag the moment something changed, such as a fix, an upgrade or a new process. Turnbacks compares
            equal periods before and after it.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={save} className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor={`${id}-title`}>What changed?</Label>
            <Input
              id={`${id}-title`}
              autoFocus
              placeholder="e.g. VPN client 2.3 rolled out"
              value={form.title}
              onChange={set("title")}
              maxLength={200}
              aria-invalid={!!errors.title}
            />
            {errors.title && <p className="text-sm text-destructive">{errors.title}</p>}
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-when`}>When</Label>
              <Input id={`${id}-when`} type="datetime-local" value={form.when} onChange={set("when")} />
              {errors.occurred_at && <p className="text-sm text-destructive">{errors.occurred_at}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-problem`}>Applies to</Label>
              <NativeSelect id={`${id}-problem`} value={form.problem_id} onChange={set("problem_id")}>
                <option value="">Everything (organisation-wide)</option>
                {hasPreselected && <option value={problem.id}>{problem.title}</option>}
                {options.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.title}
                  </option>
                ))}
              </NativeSelect>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor={`${id}-url`}>Link (optional)</Label>
            <Input
              id={`${id}-url`}
              type="url"
              placeholder="https://…"
              value={form.url}
              onChange={set("url")}
              aria-invalid={!!errors.url}
            />
            {errors.url && <p className="text-sm text-destructive">{errors.url}</p>}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor={`${id}-desc`}>Details (optional)</Label>
            <Textarea
              id={`${id}-desc`}
              value={form.description}
              onChange={set("description")}
              maxLength={2000}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              Mark change
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

const windows = [7, 14, 28, 90];

/** Before/after comparisons for recent changes (one problem's, or all). */
export function ChangesCard({ problem }: { problem?: { id: string; title: string } }) {
  const { admin } = useSession();
  const [windowDays, setWindowDays] = useState(28);
  const [marking, setMarking] = useState(false);
  const overview = useChangeOverview(problem?.id, windowDays);
  const del = useDeleteChange();
  const items = overview.data?.items ?? [];
  const selectId = useId();

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-start justify-between gap-2">
        <div>
          <CardTitle className="text-base">Changes: before and after</CardTitle>
          <CardDescription>
            Turnbacks{problem ? " for this problem" : ""} in equal periods either side of each change.
            Observational, not proof of cause.
          </CardDescription>
        </div>
        <div className="flex items-center gap-2">
          <Label htmlFor={selectId} className="sr-only">
            Comparison window
          </Label>
          <NativeSelect
            id={selectId}
            className="w-32"
            value={windowDays}
            onChange={(e) => setWindowDays(Number(e.target.value))}
          >
            {windows.map((d) => (
              <option key={d} value={d}>
                {d} days
              </option>
            ))}
          </NativeSelect>
          <Button size="sm" variant="outline" onClick={() => setMarking(true)} data-testid="mark-change">
            <Flag /> Mark a change
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {overview.data && items.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No changes marked yet. Mark one when you ship a fix, so you can see whether it helped.
          </p>
        )}
        <ul className="space-y-3">
          {items.map((c) => (
            <ChangeRow
              key={c.marker.id}
              c={c}
              showProblem={!problem}
              onDelete={
                admin
                  ? () => {
                      if (window.confirm(`Delete the change “${c.marker.title}”?`)) del.mutate(c.marker.id);
                    }
                  : undefined
              }
            />
          ))}
        </ul>
        {items.length > 0 && <p className="mt-3 text-xs text-muted-foreground">{items[0].caveat}</p>}
      </CardContent>
      {marking && <MarkChangeDialog problem={problem} onClose={() => setMarking(false)} />}
    </Card>
  );
}

function ChangeRow({
  c,
  showProblem,
  onDelete,
}: {
  c: Schemas["MarkerComparison"];
  showProblem: boolean;
  onDelete?: () => void;
}) {
  const m = c.marker;
  const b = c.before.totals;
  const a = c.after.totals;
  const delta = c.change.turnbacks_bp;
  return (
    <li className="rounded-md border p-3 text-sm" data-testid="change-row">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-0.5">
          <div className="flex flex-wrap items-center gap-2 font-medium">
            <span>{m.title}</span>
            {m.url && (
              <a
                href={m.url}
                target="_blank"
                rel="noreferrer noopener"
                className="inline-flex items-center text-muted-foreground"
                aria-label="Open link to the change"
              >
                <ExternalLink className="size-3.5" />
              </a>
            )}
            {m.is_demo && <Badge variant="warning">demo</Badge>}
            {m.source === "webhook" && <Badge variant="outline">via webhook</Badge>}
          </div>
          <p className="text-xs text-muted-foreground">
            {formatDateTime(m.occurred_at)} ·{" "}
            {m.problem_id ? (
              showProblem ? (
                <Link className="underline" to={`/problems/${m.problem_id}`}>
                  {m.problem_title}
                </Link>
              ) : (
                "this problem"
              )
            ) : (
              "organisation-wide"
            )}
          </p>
        </div>
        {onDelete && (
          <Button variant="ghost" size="icon" aria-label={`Delete change ${m.title}`} onClick={onDelete}>
            <Trash2 />
          </Button>
        )}
      </div>
      {c.window_seconds === 0 ? (
        <p className="mt-2 text-xs text-muted-foreground">Too soon to compare.</p>
      ) : (
        <dl className="mt-2 grid grid-cols-3 gap-2 text-xs">
          <div>
            <dt className="text-muted-foreground">Before ({windowLabel(c.window_seconds)})</dt>
            <dd className="tabular-nums">
              {formatCount(b.turnbacks)} · {formatHours(b.minutes)}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">After</dt>
            <dd className="tabular-nums">
              {formatCount(a.turnbacks)} · {formatHours(a.minutes)}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Change</dt>
            <dd
              className={
                delta == null
                  ? "tabular-nums"
                  : delta < 0
                    ? "text-emerald-700 tabular-nums dark:text-emerald-400"
                    : delta > 0
                      ? "text-red-700 tabular-nums dark:text-red-400"
                      : "tabular-nums"
              }
            >
              {formatChange(delta)}
              {c.insufficient_data && <span className="text-muted-foreground"> (early)</span>}
            </dd>
          </div>
        </dl>
      )}
    </li>
  );
}
