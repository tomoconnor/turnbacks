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
import { Layers, Pencil, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { useCategories, useInvalidateStats, useSettings, useTeams, useTurnbacks } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { formatDateTime, formatMinutes, majorToMinor, minorToMajor } from "@/lib/format";
import { useSession } from "@/lib/session";
import { AuditTab } from "./admin/AuditTab";
import { WebhooksTab } from "./admin/WebhooksTab";

function errMsg(e: unknown) {
  if (e instanceof ApiError && e.fields.length)
    return e.fields.map((f) => `${f.field}: ${f.message}`).join("; ");
  return e instanceof Error ? e.message : "Request failed";
}

export function AdminPage() {
  const { admin, adminEnabled, loading } = useSession();
  if (loading) return null;
  if (!adminEnabled)
    return (
      <p className="text-sm text-muted-foreground">
        Administration is disabled on this server (no admin client CA or single sign-on administrators are
        configured).
      </p>
    );
  if (!admin) return <AdminCertificateRequired />;
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold tracking-tight">Administration</h1>
      <Tabs defaultValue="reports">
        <TabsList>
          <TabsTrigger value="reports">Reports</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
          <TabsTrigger value="teams">Teams</TabsTrigger>
          <TabsTrigger value="categories">Categories</TabsTrigger>
          <TabsTrigger value="webhooks">Webhooks</TabsTrigger>
          <TabsTrigger value="audit">Audit log</TabsTrigger>
        </TabsList>
        <TabsContent value="reports">
          <ReportsTab />
        </TabsContent>
        <TabsContent value="settings">
          <SettingsTab />
        </TabsContent>
        <TabsContent value="teams">
          <TeamsTab />
        </TabsContent>
        <TabsContent value="categories">
          <CategoriesTab />
        </TabsContent>
        <TabsContent value="webhooks">
          <WebhooksTab />
        </TabsContent>
        <TabsContent value="audit">
          <AuditTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function AdminCertificateRequired() {
  const { adminMethods, user } = useSession();
  const sso = adminMethods.includes("sso");
  const certs = adminMethods.includes("certificate");
  return (
    <Card className="mx-auto max-w-lg">
      <CardHeader>
        <CardTitle>Administration</CardTitle>
        <CardDescription>
          {certs
            ? "Administration needs an admin client certificate. This browser did not present one."
            : "Administration is limited to administrators."}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {sso && (
          <p>
            {user ? (
              <>
                You are signed in as <strong>{user}</strong>, who is not an administrator.
              </>
            ) : (
              "You are not signed in through single sign-on."
            )}{" "}
            Ask to be added to the administrators&apos; group.
          </p>
        )}
        {certs && <CertificateSteps />}
        <p className="text-muted-foreground">
          Recording reports and the dashboards never need a certificate.
        </p>
      </CardContent>
    </Card>
  );
}

function CertificateSteps() {
  return (
    <>
      <p>If you are an administrator:</p>
      <ol className="list-decimal space-y-1 pl-5">
        <li>
          Ask another administrator for your certificate bundle (a <code>.p12</code> file) and its password.
        </li>
        <li>
          Import it into your browser or keychain. On macOS, double-click the file; in Firefox, use Settings →
          Certificates → Your Certificates → Import.
        </li>
        <li>Quit and reopen the browser, then reload this page and choose the certificate when asked.</li>
      </ol>
    </>
  );
}

function SettingsTab() {
  const s = useSettings();
  const qc = useQueryClient();
  const [form, setForm] = useState<Schemas["SettingsInput"] | null>(null);
  useEffect(() => {
    if (s.data && !form) {
      const {
        org_name,
        currency,
        default_hourly_rate_minor,
        reporting_timezone,
        recoverable_pct,
        large_duration_minutes,
        note_retention_days,
        reporter_retention_days,
      } = s.data;
      setForm({
        org_name,
        currency,
        default_hourly_rate_minor,
        reporting_timezone,
        recoverable_pct,
        large_duration_minutes,
        note_retention_days,
        reporter_retention_days,
      });
    }
  }, [s.data, form]);
  const save = useMutation({
    mutationFn: (body: Schemas["SettingsInput"]) => unwrap(api.PUT("/settings", { body })),
    onSuccess: () => {
      void qc.invalidateQueries();
      toast.success("Settings saved.");
    },
    onError: (e) => toast.error(errMsg(e)),
  });
  if (!form || !s.data) return null;
  const exp = s.data.currency_exponent;
  const num = (v: string) => (v === "" ? null : Number(v));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Organisation settings</CardTitle>
        <CardDescription>
          These assumptions drive every cost estimate. Changes apply retroactively to all reports.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="grid max-w-3xl gap-4 sm:grid-cols-2"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate(form);
          }}
        >
          <div className="space-y-1.5">
            <Label htmlFor="s-org">Organisation name</Label>
            <Input
              id="s-org"
              value={form.org_name}
              onChange={(e) => setForm({ ...form, org_name: e.target.value })}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="s-cur">Currency (ISO 4217)</Label>
            <Input
              id="s-cur"
              maxLength={3}
              value={form.currency}
              onChange={(e) => setForm({ ...form, currency: e.target.value.toUpperCase() })}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="s-rate">Default fully loaded hourly employment cost</Label>
            <Input
              id="s-rate"
              type="number"
              min={0}
              step="any"
              value={minorToMajor(form.default_hourly_rate_minor, exp)}
              onChange={(e) =>
                setForm({ ...form, default_hourly_rate_minor: majorToMinor(Number(e.target.value), exp) })
              }
            />
            <p className="text-xs text-muted-foreground">
              Salary plus employment overheads. Team-specific rates override this.
            </p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="s-tz">Reporting timezone</Label>
            <Input
              id="s-tz"
              value={form.reporting_timezone}
              onChange={(e) => setForm({ ...form, reporting_timezone: e.target.value })}
            />
            <p className="text-xs text-muted-foreground">
              IANA name, e.g. Europe/London. Defines day, week and month boundaries.
            </p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="s-rec">Recoverable share of lost time (%)</Label>
            <Input
              id="s-rec"
              type="number"
              min={0}
              max={100}
              value={form.recoverable_pct}
              onChange={(e) => setForm({ ...form, recoverable_pct: Number(e.target.value) })}
            />
            <p className="text-xs text-muted-foreground">
              Default assumption for projected benefits in business cases.
            </p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="s-large">Confirm durations longer than (minutes)</Label>
            <Input
              id="s-large"
              type="number"
              min={1}
              max={1440}
              value={form.large_duration_minutes}
              onChange={(e) => setForm({ ...form, large_duration_minutes: Number(e.target.value) })}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="s-nret">Delete notes after (days)</Label>
            <Input
              id="s-nret"
              type="number"
              min={1}
              placeholder="Keep"
              value={form.note_retention_days ?? ""}
              onChange={(e) => setForm({ ...form, note_retention_days: num(e.target.value) })}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="s-rret">Remove reporter links after (days)</Label>
            <Input
              id="s-rret"
              type="number"
              min={1}
              placeholder="Keep"
              value={form.reporter_retention_days ?? ""}
              onChange={(e) => setForm({ ...form, reporter_retention_days: num(e.target.value) })}
            />
            <p className="text-xs text-muted-foreground">
              Reporter addresses on older reports are removed; those reports no longer count towards “people
              affected”.
            </p>
          </div>
          <div className="sm:col-span-2">
            <Button type="submit" disabled={save.isPending}>
              Save settings
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function TeamsTab() {
  const teams = useTeams(true);
  const s = useSettings();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const exp = s.data?.currency_exponent ?? 2;
  const refresh = () => void qc.invalidateQueries({ queryKey: ["teams"] });

  async function create(e: React.FormEvent) {
    e.preventDefault();
    try {
      await unwrap(api.POST("/teams", { body: { name } }));
      setName("");
      refresh();
    } catch (err) {
      toast.error(errMsg(err));
    }
  }
  async function update(t: Schemas["Team"], patch: Partial<Schemas["TeamInput"]>) {
    try {
      await unwrap(
        api.PATCH("/teams/{id}", {
          params: { path: { id: t.id } },
          body: { name: t.name, active: t.active, hourly_rate_minor: t.hourly_rate_minor, ...patch },
        }),
      );
      refresh();
    } catch (err) {
      toast.error(errMsg(err));
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Teams</CardTitle>
        <CardDescription>
          Optional team-specific hourly rates override the organisation default.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <form onSubmit={create} className="flex max-w-md gap-2">
          <Input
            aria-label="New team name"
            placeholder="New team name"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <Button type="submit" disabled={!name.trim()}>
            Add
          </Button>
        </form>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Headcount</TableHead>
              <TableHead>Hourly rate ({s.data?.currency})</TableHead>
              <TableHead>Active</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {teams.data?.items.map((t) => (
              <TableRow key={t.id}>
                <TableCell>
                  {t.name} {t.is_demo && <Badge variant="warning">demo</Badge>}
                </TableCell>
                <TableCell>
                  <Input
                    aria-label={`Headcount for ${t.name}`}
                    className="w-24"
                    type="number"
                    min={1}
                    placeholder="—"
                    defaultValue={t.headcount ?? ""}
                    onBlur={(e) => {
                      const v =
                        e.target.value === "" ? null : Math.max(1, Math.round(Number(e.target.value)));
                      if (v !== t.headcount) void update(t, { headcount: v });
                    }}
                  />
                </TableCell>
                <TableCell>
                  <Input
                    aria-label={`Hourly rate for ${t.name}`}
                    className="w-32"
                    type="number"
                    min={0}
                    step="any"
                    placeholder="Default"
                    defaultValue={t.hourly_rate_minor != null ? minorToMajor(t.hourly_rate_minor, exp) : ""}
                    onBlur={(e) => {
                      const v = e.target.value === "" ? null : majorToMinor(Number(e.target.value), exp);
                      if (v !== t.hourly_rate_minor) void update(t, { hourly_rate_minor: v });
                    }}
                  />
                </TableCell>
                <TableCell>
                  <Switch
                    aria-label={`${t.name} active`}
                    checked={t.active}
                    onCheckedChange={(v) => void update(t, { active: v })}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

function CategoriesTab() {
  const cats = useCategories(true);
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const refresh = () => void qc.invalidateQueries({ queryKey: ["categories"] });
  async function create(e: React.FormEvent) {
    e.preventDefault();
    try {
      await unwrap(api.POST("/categories", { body: { name, sort_order: 500 } }));
      setName("");
      refresh();
    } catch (err) {
      toast.error(errMsg(err));
    }
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Categories</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <form onSubmit={create} className="flex max-w-md gap-2">
          <Input
            aria-label="New category name"
            placeholder="New category"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <Button type="submit" disabled={!name.trim()}>
            Add
          </Button>
        </form>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Active</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {cats.data?.items.map((c) => (
              <TableRow key={c.id}>
                <TableCell>{c.name}</TableCell>
                <TableCell className="text-muted-foreground">{c.description}</TableCell>
                <TableCell>
                  <Switch
                    aria-label={`${c.name} active`}
                    checked={c.active}
                    onCheckedChange={async (v) => {
                      try {
                        await unwrap(
                          api.PATCH("/categories/{id}", {
                            params: { path: { id: c.id } },
                            body: {
                              name: c.name,
                              description: c.description,
                              sort_order: c.sort_order,
                              active: v,
                            },
                          }),
                        );
                        refresh();
                      } catch (err) {
                        toast.error(errMsg(err));
                      }
                    }}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

const REPORTS_PAGE = 50;

function ReportsTab() {
  const [reporter, setReporter] = useState("");
  const [offset, setOffset] = useState(0);
  const [editing, setEditing] = useState<Schemas["Turnback"] | null>(null);
  const filter = reporter.trim();
  const list = useTurnbacks({
    period: "last_90_days",
    reporter: filter || undefined,
    limit: REPORTS_PAGE,
    offset,
  });
  const invalidate = useInvalidateStats();

  async function remove(t: Schemas["Turnback"], batch: boolean) {
    const msg = batch ? "Delete every occurrence recorded together with this one?" : "Delete this report?";
    if (!window.confirm(msg)) return;
    try {
      await unwrap(api.DELETE("/turnbacks/{id}", { params: { path: { id: t.id }, query: { batch } } }));
      invalidate();
      toast.success(batch ? "Batch deleted." : "Report deleted.");
    } catch (err) {
      toast.error(errMsg(err));
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Reports (last 90 days)</CardTitle>
        <CardDescription>
          Audit and correct individual reports. The reporter is the network address the report came from (one
          VPN address per person) or the Slack user. It is visible only to administrators.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <Input
          aria-label="Filter by reporter"
          placeholder="Filter by reporter, e.g. ip:10.8.0.12 or slack:T123/U456"
          className="max-w-md"
          value={reporter}
          onChange={(e) => {
            setReporter(e.target.value);
            setOffset(0);
          }}
        />
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>When</TableHead>
              <TableHead>Problem</TableHead>
              <TableHead>Time</TableHead>
              <TableHead>Reporter</TableHead>
              <TableHead>Source</TableHead>
              <TableHead className="sr-only">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody data-testid="audit-rows">
            {list.data?.items.map((t) => (
              <TableRow key={t.id}>
                <TableCell className="whitespace-nowrap">{formatDateTime(t.occurred_at)}</TableCell>
                <TableCell>
                  {t.problem_title}
                  {t.batch_id && (
                    <Badge variant="secondary" className="ml-2">
                      <Layers /> batch
                    </Badge>
                  )}
                  {t.note && <div className="max-w-80 truncate text-xs text-muted-foreground">{t.note}</div>}
                </TableCell>
                <TableCell className="whitespace-nowrap">{formatMinutes(t.duration_minutes)}</TableCell>
                <TableCell>
                  {t.reporter ? (
                    <button
                      type="button"
                      className="font-mono text-xs underline-offset-2 hover:underline"
                      onClick={() => setReporter(t.reporter!)}
                      title="Show only this reporter"
                    >
                      {t.reporter}
                    </button>
                  ) : (
                    <span className="text-xs text-muted-foreground">unknown</span>
                  )}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">{t.source}</TableCell>
                <TableCell className="text-right whitespace-nowrap">
                  <Button variant="ghost" size="icon" aria-label="Edit report" onClick={() => setEditing(t)}>
                    <Pencil />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Delete report"
                    onClick={() => void remove(t, false)}
                  >
                    <Trash2 />
                  </Button>
                  {t.batch_id && (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Delete the whole batch"
                      onClick={() => void remove(t, true)}
                    >
                      <Layers />
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {list.data && list.data.items.length === 0 && (
          <p className="text-sm text-muted-foreground">No reports.</p>
        )}
        {list.data && list.data.total > REPORTS_PAGE && (
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">
              {offset + 1}–{Math.min(offset + REPORTS_PAGE, list.data.total)} of {list.data.total}
            </span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={offset === 0}
                onClick={() => setOffset(offset - REPORTS_PAGE)}
              >
                Previous
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={offset + REPORTS_PAGE >= list.data.total}
                onClick={() => setOffset(offset + REPORTS_PAGE)}
              >
                Next
              </Button>
            </div>
          </div>
        )}
      </CardContent>
      {editing && <EditReportDialog turnback={editing} onClose={() => setEditing(null)} />}
    </Card>
  );
}

function toLocalInput(iso: string) {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function EditReportDialog({ turnback, onClose }: { turnback: Schemas["Turnback"]; onClose: () => void }) {
  const [minutes, setMinutes] = useState(String(turnback.duration_minutes));
  const [note, setNote] = useState(turnback.note ?? "");
  const [estimated, setEstimated] = useState(turnback.duration_estimated);
  const [when, setWhen] = useState(toLocalInput(turnback.occurred_at));
  const [error, setError] = useState<string | null>(null);
  const invalidate = useInvalidateStats();

  async function save(confirm = false): Promise<void> {
    try {
      await unwrap(
        api.PATCH("/turnbacks/{id}", {
          params: { path: { id: turnback.id } },
          body: {
            problem_id: turnback.problem_id,
            duration_minutes: Number(minutes),
            duration_estimated: estimated,
            note: note.trim() || null,
            occurred_at: new Date(when).toISOString(),
            confirm_large_duration: confirm,
          },
        }),
      );
      invalidate();
      toast.success("Report updated.");
      onClose();
    } catch (e) {
      if (e instanceof ApiError && e.code === "duration_confirmation_required") {
        if (window.confirm(e.message)) return save(true);
        return;
      }
      setError(e instanceof Error ? e.message : "Update failed");
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Correct report · {turnback.problem_title}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1">
            <Label htmlFor="e-min">Minutes lost</Label>
            <Input
              id="e-min"
              type="number"
              min={1}
              max={1440}
              value={minutes}
              onChange={(e) => setMinutes(e.target.value)}
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              className="size-4"
              checked={estimated}
              onChange={(e) => setEstimated(e.target.checked)}
            />
            The duration is an estimate
          </label>
          <div className="space-y-1">
            <Label htmlFor="e-when">When</Label>
            <Input id="e-when" type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} />
          </div>
          <div className="space-y-1">
            <Label htmlFor="e-note">Note</Label>
            <Textarea id="e-note" value={note} maxLength={1000} onChange={(e) => setNote(e.target.value)} />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => void save()}>Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
