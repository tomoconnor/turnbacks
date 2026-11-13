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

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, FileBarChart, GitMerge, Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import {
  useCategories,
  useChanges,
  useProblem,
  useProblemSearch,
  useRemediations,
  useSettings,
  useSummary,
  useTimeseries,
  useUpdateProblem,
} from "@/api/hooks";
import { ChangesCard } from "@/components/ChangeMarkers";
import { TimeSeriesChart } from "@/components/charts/Charts";
import { StatCard } from "@/components/StatCard";
import { StatusBadge } from "@/components/StatusBadge";
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
import { formatCount, formatDate, formatHours } from "@/lib/format";
import { fieldErrors, problemInputSchema, remediationInputSchema } from "@/lib/schemas";
import { useMoney } from "@/lib/useMoney";
import { useSession } from "@/lib/session";

export function ProblemDetailPage() {
  const { id } = useParams();
  const { admin: isAdmin, identityMode } = useSession();
  const settings = useSettings();
  const problem = useProblem(id);
  const remediations = useRemediations(id);
  const summary = useSummary({ period: "last_90_days", problem_id: id }, !!id);
  const series = useTimeseries({ period: "last_90_days", problem_id: id, bucket: "week" }, !!id);
  const changes = useChanges({ problem_id: id, from: summary.data?.period.start }, !!id && !!summary.data);
  const { fmt } = useMoney();
  const [editing, setEditing] = useState(false);
  const [merging, setMerging] = useState(false);
  const [addingRem, setAddingRem] = useState(false);

  if (problem.error) return <p className="text-sm text-destructive">Problem not found.</p>;
  const p = problem.data;
  if (!p) return <p className="text-sm text-muted-foreground">Loading…</p>;
  const s = summary.data;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-semibold tracking-tight">{p.title}</h1>
            <StatusBadge status={p.status} />
            {p.alias && <Badge variant="outline">alias: {p.alias}</Badge>}
            {p.is_demo && <Badge variant="warning">demo</Badge>}
          </div>
          <p className="text-sm text-muted-foreground">
            {p.category_name}
            {p.external_url && (
              <>
                {" · "}
                <a
                  href={p.external_url}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="inline-flex items-center gap-1 underline"
                >
                  Linked issue <ExternalLink className="size-3" />
                </a>
              </>
            )}
          </p>
          {p.merged_into_id && (
            <p className="text-sm">
              Merged into{" "}
              <Link className="underline" to={`/problems/${p.merged_into_id}`}>
                another problem
              </Link>
              .
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          {
            <Button asChild>
              <Link to={`/problems/${p.id}/business-case`}>
                <FileBarChart /> Business case
              </Link>
            </Button>
          }
          {isAdmin && !p.merged_into_id && (
            <>
              <Button variant="outline" onClick={() => setEditing(true)}>
                <Pencil /> Edit
              </Button>
              <Button variant="outline" onClick={() => setMerging(true)}>
                <GitMerge /> Merge
              </Button>
            </>
          )}
        </div>
      </div>
      {p.description && <p className="max-w-3xl text-sm whitespace-pre-wrap">{p.description}</p>}

      {s && (
        <>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <StatCard
              label="Turnbacks (90 days)"
              value={formatCount(s.current.turnbacks)}
              changeBp={s.change.turnbacks_bp}
            />
            <StatCard
              label="Reported time lost"
              value={formatHours(s.current.minutes)}
              changeBp={s.change.minutes_bp}
            />
            <StatCard
              label="Est. productivity cost"
              value={fmt(s.current.cost_minor, { whole: true })}
              changeBp={s.change.cost_bp}
              hint="Estimate, not cash spent"
            />
            <StatCard
              label="People affected"
              value={formatCount(s.current.distinct_reporters)}
              hint={identityMode === "header" ? "distinct people" : "distinct addresses and chat users"}
            />
          </div>
          {series.data && (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Turnbacks per week (last 90 days)</CardTitle>
              </CardHeader>
              <CardContent>
                <TimeSeriesChart
                  points={series.data.points}
                  bucket="week"
                  metric="count"
                  money={fmt}
                  height={200}
                  timeZone={settings.data?.reporting_timezone}
                  markers={changes.data?.items.map((m) => ({
                    id: m.id,
                    occurredAt: m.occurred_at,
                    title: m.title,
                  }))}
                />
              </CardContent>
            </Card>
          )}
        </>
      )}

      {!p.merged_into_id && <ChangesCard problem={{ id: p.id, title: p.title }} />}

      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <div>
            <CardTitle className="text-base">Remediations</CardTitle>
            <CardDescription>
              Improvements made to address this problem. History is never deleted.
            </CardDescription>
          </div>
          {isAdmin && !p.merged_into_id && (
            <Button size="sm" variant="outline" onClick={() => setAddingRem(true)}>
              <Plus /> Record remediation
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {remediations.data?.items.length === 0 && (
            <p className="text-sm text-muted-foreground">None recorded.</p>
          )}
          <ul className="space-y-3">
            {remediations.data?.items.map((r) => (
              <li key={r.id} className="flex items-start justify-between gap-3 rounded-md border p-3 text-sm">
                <div>
                  <div className="font-medium">{formatDate(r.implemented_on)}</div>
                  <p>{r.description}</p>
                  <p className="text-xs text-muted-foreground">
                    Implementation {fmt(r.implementation_cost_minor, { whole: true })} · ongoing{" "}
                    {fmt(r.annual_cost_minor, { whole: true })}/yr · expected reduction{" "}
                    {r.expected_reduction_pct}%
                  </p>
                </div>
                {isAdmin && <DeleteRemediation id={r.id} problemId={p.id} />}
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>

      {editing && <EditProblemDialog problem={p} onClose={() => setEditing(false)} />}
      {merging && <MergeDialog problem={p} onClose={() => setMerging(false)} />}
      {addingRem && <RemediationDialog problem={p} onClose={() => setAddingRem(false)} />}
    </div>
  );
}

function DeleteRemediation({ id, problemId }: { id: string; problemId: string }) {
  const qc = useQueryClient();
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label="Delete remediation"
      onClick={async () => {
        if (!window.confirm("Delete this remediation record?")) return;
        await unwrap(api.DELETE("/remediations/{id}", { params: { path: { id } } }));
        void qc.invalidateQueries({ queryKey: ["remediations", problemId] });
      }}
    >
      <Trash2 />
    </Button>
  );
}

function EditProblemDialog({ problem, onClose }: { problem: Schemas["Problem"]; onClose: () => void }) {
  const categories = useCategories();
  const update = useUpdateProblem(problem.id);
  const [form, setForm] = useState({
    title: problem.title,
    description: problem.description,
    category_id: problem.category_id,
    status: problem.status,
    alias: problem.alias ?? "",
    external_url: problem.external_url ?? "",
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const set =
    (k: keyof typeof form) =>
    (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) =>
      setForm({ ...form, [k]: e.target.value });

  async function save(e: React.FormEvent) {
    e.preventDefault();
    const parsed = problemInputSchema.safeParse({
      ...form,
      alias: form.alias.trim() ? form.alias.trim().toLowerCase() : null,
      external_url: form.external_url.trim() || null,
    });
    if (!parsed.success) return setErrors(fieldErrors(parsed.error));
    try {
      await update.mutateAsync(parsed.data);
      toast.success("Problem updated.");
      onClose();
    } catch (err) {
      if (err instanceof ApiError) setErrors(Object.fromEntries(err.fields.map((f) => [f.field, f.message])));
      toast.error(err instanceof Error ? err.message : "Update failed");
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Edit problem</DialogTitle>
        </DialogHeader>
        <form onSubmit={save} className="space-y-3">
          <Field label="Title" error={errors.title}>
            <Input value={form.title} onChange={set("title")} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Category" error={errors.category_id}>
              <NativeSelect value={form.category_id} onChange={set("category_id")}>
                {categories.data?.items.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </NativeSelect>
            </Field>
            <Field label="Status" error={errors.status}>
              <NativeSelect value={form.status} onChange={set("status")}>
                <option value="active">Active</option>
                <option value="monitoring">Monitoring</option>
                <option value="resolved">Resolved</option>
                <option value="archived">Archived</option>
              </NativeSelect>
            </Field>
          </div>
          <Field label="Alias" error={errors.alias}>
            <Input value={form.alias} onChange={set("alias")} />
          </Field>
          <Field label="External issue URL" error={errors.external_url}>
            <Input value={form.external_url} onChange={set("external_url")} placeholder="https://…" />
          </Field>
          <Field label="Description" error={errors.description}>
            <Textarea value={form.description} onChange={set("description")} />
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={update.isPending}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function Field({
  label,
  error,
  children,
}: {
  label: string;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      {children}
      {error && <p className="text-sm text-destructive">{error}</p>}
    </div>
  );
}

function MergeDialog({ problem, onClose }: { problem: Schemas["Problem"]; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [target, setTarget] = useState<Schemas["Problem"] | null>(null);
  const results = useProblemSearch({ q: q || undefined, limit: 8 });
  const qc = useQueryClient();
  const navigate = useNavigate();
  const merge = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/problems/{id}/merge", {
          params: { path: { id: problem.id } },
          body: { target_id: target!.id },
        }),
      ),
    onSuccess: (res) => {
      toast.success(`Merged. ${res.moved_turnbacks} turnbacks moved to “${res.target.title}”.`);
      void qc.invalidateQueries();
      navigate(`/problems/${res.target.id}`);
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : "Merge failed"),
  });

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Merge “{problem.title}” into…</DialogTitle>
          <DialogDescription>
            All turnbacks and remediations move to the chosen problem. This problem is archived and future
            reports against it are redirected. No history is lost.
          </DialogDescription>
        </DialogHeader>
        <Input
          aria-label="Search target problem"
          placeholder="Search problems"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <ul className="max-h-60 divide-y overflow-y-auto rounded-md border">
          {results.data?.items
            .filter((r) => r.id !== problem.id)
            .map((r) => (
              <li key={r.id}>
                <button
                  type="button"
                  className={`w-full p-2 text-left text-sm hover:bg-accent ${target?.id === r.id ? "bg-accent font-medium" : ""}`}
                  onClick={() => setTarget(r)}
                  aria-pressed={target?.id === r.id}
                >
                  {r.title}
                </button>
              </li>
            ))}
        </ul>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={!target || merge.isPending} onClick={() => merge.mutate()}>
            Merge
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RemediationDialog({ problem, onClose }: { problem: Schemas["Problem"]; onClose: () => void }) {
  const { fmt, exponent } = useMoney();
  const qc = useQueryClient();
  const [form, setForm] = useState({
    implemented_on: new Date().toISOString().slice(0, 10),
    description: "",
    implementation_cost: "0",
    annual_cost: "0",
    expected_reduction_pct: "50",
    status: problem.status === "active" ? "monitoring" : problem.status,
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const set =
    (k: keyof typeof form) =>
    (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) =>
      setForm({ ...form, [k]: e.target.value });

  async function save(e: React.FormEvent) {
    e.preventDefault();
    const parsed = remediationInputSchema.safeParse({
      implemented_on: form.implemented_on,
      description: form.description,
      implementation_cost_minor: Math.round(Number(form.implementation_cost) * 10 ** exponent),
      annual_cost_minor: Math.round(Number(form.annual_cost) * 10 ** exponent),
      expected_reduction_pct: Number(form.expected_reduction_pct),
    });
    if (!parsed.success) return setErrors(fieldErrors(parsed.error));
    try {
      await unwrap(
        api.POST("/problems/{id}/remediations", { params: { path: { id: problem.id } }, body: parsed.data }),
      );
      if (form.status !== problem.status) {
        await unwrap(
          api.PATCH("/problems/{id}", {
            params: { path: { id: problem.id } },
            body: {
              title: problem.title,
              description: problem.description,
              category_id: problem.category_id,
              status: form.status as Schemas["ProblemStatus"],
              alias: problem.alias,
              external_url: problem.external_url,
            },
          }),
        );
      }
      void qc.invalidateQueries({ queryKey: ["remediations", problem.id] });
      void qc.invalidateQueries({ queryKey: ["problem", problem.id] });
      toast.success("Remediation recorded.");
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not save");
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Record remediation</DialogTitle>
          <DialogDescription>
            Costs are actual or planned expenditure, distinct from estimated productivity cost.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={save} className="space-y-3">
          <Field label="Implementation date" error={errors.implemented_on}>
            <Input type="date" value={form.implemented_on} onChange={set("implemented_on")} />
          </Field>
          <Field label="What was done?" error={errors.description}>
            <Textarea value={form.description} onChange={set("description")} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field
              label={`One-off cost (${fmt(0).replace(/[\d.,\s]/g, "")})`}
              error={errors.implementation_cost_minor}
            >
              <Input
                type="number"
                min={0}
                step="any"
                value={form.implementation_cost}
                onChange={set("implementation_cost")}
              />
            </Field>
            <Field label="Ongoing cost per year" error={errors.annual_cost_minor}>
              <Input
                type="number"
                min={0}
                step="any"
                value={form.annual_cost}
                onChange={set("annual_cost")}
              />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Expected reduction in occurrences (%)" error={errors.expected_reduction_pct}>
              <Input
                type="number"
                min={0}
                max={100}
                value={form.expected_reduction_pct}
                onChange={set("expected_reduction_pct")}
              />
            </Field>
            <Field label="Set problem status">
              <NativeSelect value={form.status} onChange={set("status")}>
                <option value="active">Active</option>
                <option value="monitoring">Monitoring</option>
                <option value="resolved">Resolved</option>
              </NativeSelect>
            </Field>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
