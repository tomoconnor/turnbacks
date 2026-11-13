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

import { useQueryClient } from "@tanstack/react-query";
import { Copy, History, KeyRound, Send, Trash2 } from "lucide-react";
import { useId, useState } from "react";
import { toast } from "sonner";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { useInboundHooks, useWebhookDeliveries, useWebhooks } from "@/api/hooks";
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
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatDateTime, formatRelative } from "@/lib/format";

function errMsg(e: unknown) {
  if (e instanceof ApiError && e.fields.length)
    return e.fields.map((f) => `${f.field}: ${f.message}`).join("; ");
  return e instanceof Error ? e.message : "Request failed";
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success("Copied");
  } catch {
    toast.error("Could not copy; select the text and copy it instead");
  }
}

const formatLabels: Record<Schemas["WebhookFormat"], string> = {
  json: "JSON (signed)",
  slack: "Slack",
  markdown: "Mattermost / Rocket.Chat",
  teams: "Microsoft Teams (Workflows)",
};

const eventLabels: Record<Schemas["WebhookEvent"], string> = {
  "turnback.recorded": "Turnback recorded",
  "problem.created": "New problem",
  "change.marked": "Change marked",
  "remediation.recorded": "Remediation recorded",
};

const kindLabels: Record<Schemas["InboundHook"]["kind"], string> = {
  json: "JSON (scripts, CI)",
  mattermost: "Mattermost slash command",
  teams: "Microsoft Teams outgoing webhook",
};

export function WebhooksTab() {
  return (
    <div className="space-y-6">
      <OutboundCard />
      <InboundCard />
    </div>
  );
}

/** Shows a secret once, with a copy button. */
function SecretDialog({
  title,
  description,
  secret,
  extra,
  onClose,
}: {
  title: string;
  description: string;
  secret: string;
  extra?: React.ReactNode;
  onClose: () => void;
}) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2">
          <code
            className="min-w-0 flex-1 rounded border bg-muted px-2 py-1.5 text-xs break-all"
            data-testid="secret"
          >
            {secret}
          </code>
          <Button variant="outline" size="icon" aria-label="Copy" onClick={() => void copy(secret)}>
            <Copy />
          </Button>
        </div>
        {extra}
        <DialogFooter>
          <Button onClick={onClose}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function OutboundCard() {
  const hooks = useWebhooks();
  const qc = useQueryClient();
  const id = useId();
  const [form, setForm] = useState({
    name: "",
    url: "",
    format: "markdown" as Schemas["WebhookFormat"],
    events: ["problem.created", "change.marked", "remediation.recorded"] as Schemas["WebhookEvent"][],
  });
  const [secret, setSecret] = useState<{ name: string; secret: string } | null>(null);
  const [history, setHistory] = useState<Schemas["Webhook"] | null>(null);
  const refresh = () => void qc.invalidateQueries({ queryKey: ["webhooks"] });

  async function create(e: React.FormEvent) {
    e.preventDefault();
    try {
      const w = await unwrap(api.POST("/webhooks", { body: { ...form, enabled: true } }));
      setForm({ ...form, name: "", url: "" });
      if (w.format === "json" && w.secret) setSecret({ name: w.name, secret: w.secret });
      else toast.success("Webhook added. Use “Send test” to check it.");
      refresh();
    } catch (err) {
      toast.error(errMsg(err));
    }
  }
  async function update(w: Schemas["Webhook"], patch: Partial<Schemas["WebhookInput"]>) {
    try {
      await unwrap(
        api.PATCH("/webhooks/{id}", {
          params: { path: { id: w.id } },
          body: {
            name: w.name,
            url: w.url,
            format: w.format,
            events: w.events,
            enabled: w.enabled,
            ...patch,
          },
        }),
      );
      refresh();
    } catch (err) {
      toast.error(errMsg(err));
    }
  }
  async function act(fn: () => Promise<unknown>, ok?: string) {
    try {
      await fn();
      if (ok) toast.success(ok);
      refresh();
    } catch (err) {
      toast.error(errMsg(err));
    }
  }
  const toggleEvent = (ev: Schemas["WebhookEvent"]) =>
    setForm({
      ...form,
      events: form.events.includes(ev) ? form.events.filter((x) => x !== ev) : [...form.events, ev],
    });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Outbound webhooks</CardTitle>
        <CardDescription>
          Tell other systems when things happen: post to a chat channel (Slack, Mattermost, Rocket.Chat,
          Microsoft Teams) or send signed JSON to your own service. Payloads never include who reported
          something. Failed deliveries are retried for several hours.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <form onSubmit={create} className="space-y-3 rounded-md border p-3">
          <div className="grid gap-3 md:grid-cols-[1fr_2fr_1fr]">
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-name`}>Name</Label>
              <Input
                id={`${id}-name`}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="#platform channel"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-url`}>URL</Label>
              <Input
                id={`${id}-url`}
                type="url"
                value={form.url}
                onChange={(e) => setForm({ ...form, url: e.target.value })}
                placeholder="https://chat.example.com/hooks/…"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-format`}>Format</Label>
              <NativeSelect
                id={`${id}-format`}
                value={form.format}
                onChange={(e) => setForm({ ...form, format: e.target.value as Schemas["WebhookFormat"] })}
              >
                {Object.entries(formatLabels).map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </NativeSelect>
            </div>
          </div>
          <fieldset className="flex flex-wrap gap-x-4 gap-y-2">
            <legend className="mb-1 text-sm font-medium">Events</legend>
            {Object.entries(eventLabels).map(([ev, l]) => (
              <label key={ev} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  className="size-4 accent-primary"
                  checked={form.events.includes(ev as Schemas["WebhookEvent"])}
                  onChange={() => toggleEvent(ev as Schemas["WebhookEvent"])}
                />
                {l}
              </label>
            ))}
          </fieldset>
          <Button type="submit" disabled={!form.name.trim() || !form.url.trim() || form.events.length === 0}>
            Add webhook
          </Button>
        </form>

        {hooks.data?.items.length === 0 && (
          <p className="text-sm text-muted-foreground">No outbound webhooks.</p>
        )}
        {!!hooks.data?.items.length && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Format and events</TableHead>
                <TableHead>Last delivery</TableHead>
                <TableHead>Enabled</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {hooks.data.items.map((w) => (
                <TableRow key={w.id}>
                  <TableCell className="max-w-64">
                    <div className="font-medium">{w.name}</div>
                    <div className="truncate text-xs text-muted-foreground" title={w.url}>
                      {w.url}
                    </div>
                  </TableCell>
                  <TableCell className="text-xs">
                    <div>{formatLabels[w.format]}</div>
                    <div className="text-muted-foreground">
                      {w.events.map((e) => eventLabels[e]).join(", ")}
                    </div>
                  </TableCell>
                  <TableCell className="text-xs">
                    <DeliveryStatus d={w.last_delivery} />
                  </TableCell>
                  <TableCell>
                    <Switch
                      aria-label={`${w.name} enabled`}
                      checked={w.enabled}
                      onCheckedChange={(v) => void update(w, { enabled: v })}
                    />
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Send a test to ${w.name}`}
                        title="Send test"
                        onClick={() =>
                          void act(
                            () => unwrap(api.POST("/webhooks/{id}/test", { params: { path: { id: w.id } } })),
                            "Test queued",
                          )
                        }
                      >
                        <Send />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Delivery history for ${w.name}`}
                        title="Delivery history"
                        onClick={() => setHistory(w)}
                      >
                        <History />
                      </Button>
                      {w.format === "json" && (
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`New signing secret for ${w.name}`}
                          title="New signing secret"
                          onClick={() =>
                            void act(async () => {
                              if (
                                !window.confirm("Replace the signing secret? The receiver must be updated.")
                              )
                                return;
                              const r = await unwrap(
                                api.POST("/webhooks/{id}/rotate-secret", { params: { path: { id: w.id } } }),
                              );
                              if (r.secret) setSecret({ name: r.name, secret: r.secret });
                            })
                          }
                        >
                          <KeyRound />
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Delete ${w.name}`}
                        title="Delete"
                        onClick={() =>
                          void act(async () => {
                            if (!window.confirm(`Delete the webhook “${w.name}” and its delivery history?`))
                              return;
                            await unwrap(api.DELETE("/webhooks/{id}", { params: { path: { id: w.id } } }));
                          })
                        }
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
      {secret && (
        <SecretDialog
          title={`Signing secret for “${secret.name}”`}
          description="Copy it now; it is not shown again. Each request carries X-Turnbacks-Signature: sha256=HMAC-SHA256(secret, timestamp + '.' + body), with the timestamp in X-Turnbacks-Timestamp."
          secret={secret.secret}
          onClose={() => setSecret(null)}
        />
      )}
      {history && <DeliveriesDialog hook={history} onClose={() => setHistory(null)} />}
    </Card>
  );
}

function DeliveryStatus({ d }: { d?: Schemas["WebhookDelivery"] }) {
  if (!d) return <span className="text-muted-foreground">None yet</span>;
  const variant = d.status === "delivered" ? "success" : d.status === "failed" ? "destructive" : "warning";
  return (
    <div className="space-y-0.5">
      <Badge variant={variant}>
        {d.status}
        {d.last_status_code ? ` · ${d.last_status_code}` : ""}
      </Badge>
      <div className="text-muted-foreground">{formatRelative(d.created_at)}</div>
      {d.status !== "delivered" && d.last_error && (
        <div className="max-w-48 truncate text-destructive" title={d.last_error}>
          {d.last_error}
        </div>
      )}
    </div>
  );
}

function DeliveriesDialog({ hook, onClose }: { hook: Schemas["Webhook"]; onClose: () => void }) {
  const d = useWebhookDeliveries(hook.id);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Deliveries: {hook.name}</DialogTitle>
          <DialogDescription>
            The 50 most recent. Delivered and failed entries are kept for 30 days.
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-96 overflow-y-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Queued</TableHead>
                <TableHead>Event</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Attempts</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {d.data?.items.map((x) => (
                <TableRow key={x.id}>
                  <TableCell className="text-xs whitespace-nowrap">{formatDateTime(x.created_at)}</TableCell>
                  <TableCell className="text-xs">{x.event}</TableCell>
                  <TableCell className="text-xs">
                    <DeliveryStatus d={x} />
                    {x.next_attempt_at && (
                      <div className="text-muted-foreground">
                        next try {formatDateTime(x.next_attempt_at)}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="text-xs">{x.attempts}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {d.data?.items.length === 0 && <p className="text-sm text-muted-foreground">No deliveries yet.</p>}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function InboundCard() {
  const hooks = useInboundHooks();
  const qc = useQueryClient();
  const id = useId();
  const [form, setForm] = useState({ name: "", kind: "json" as Schemas["InboundHook"]["kind"], secret: "" });
  const [created, setCreated] = useState<Schemas["InboundHook"] | null>(null);
  const refresh = () => void qc.invalidateQueries({ queryKey: ["inbound-hooks"] });

  async function create(e: React.FormEvent) {
    e.preventDefault();
    try {
      const h = await unwrap(
        api.POST("/inbound-hooks", {
          body: { name: form.name, kind: form.kind, secret: form.kind === "json" ? undefined : form.secret },
        }),
      );
      setForm({ ...form, name: "", secret: "" });
      setCreated(h);
      refresh();
    } catch (err) {
      toast.error(errMsg(err));
    }
  }
  async function act(fn: () => Promise<unknown>) {
    try {
      await fn();
      refresh();
    } catch (err) {
      toast.error(errMsg(err));
    }
  }

  const secretHelp =
    form.kind === "mattermost"
      ? "Create the slash command in Mattermost (Integrations → Slash Commands, request URL as shown after adding, method POST), then paste the token Mattermost generates here. Add this hook first with any placeholder if you need its URL, then replace the token."
      : form.kind === "teams"
        ? "Create an outgoing webhook in the Teams channel (Manage team → Apps → Create an outgoing webhook) with the URL shown after adding, then paste the security token Teams shows. Use the same name; people @mention it."
        : "";

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Inbound webhooks</CardTitle>
        <CardDescription>
          Let other systems record turnbacks or mark changes: scripts and CI pipelines (JSON), Mattermost
          slash commands, or Microsoft Teams outgoing webhooks. Chat users type the same commands as in Slack,
          e.g. <code>vpn 10 disconnected again</code>.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <form onSubmit={create} className="space-y-3 rounded-md border p-3">
          <div className="grid gap-3 md:grid-cols-[1fr_1fr_2fr]">
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-name`}>Name</Label>
              <Input
                id={`${id}-name`}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder={form.kind === "teams" ? "Turnbacks" : "Deploy pipeline"}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor={`${id}-kind`}>Kind</Label>
              <NativeSelect
                id={`${id}-kind`}
                value={form.kind}
                onChange={(e) => setForm({ ...form, kind: e.target.value as Schemas["InboundHook"]["kind"] })}
              >
                {Object.entries(kindLabels).map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </NativeSelect>
            </div>
            {form.kind !== "json" && (
              <div className="space-y-1.5">
                <Label htmlFor={`${id}-secret`}>{form.kind === "teams" ? "Security token" : "Token"}</Label>
                <Input
                  id={`${id}-secret`}
                  value={form.secret}
                  onChange={(e) => setForm({ ...form, secret: e.target.value })}
                  autoComplete="off"
                />
              </div>
            )}
          </div>
          {secretHelp && <p className="text-xs text-muted-foreground">{secretHelp}</p>}
          <Button type="submit" disabled={!form.name.trim() || (form.kind !== "json" && !form.secret.trim())}>
            Add inbound webhook
          </Button>
        </form>

        {hooks.data?.items.length === 0 && (
          <p className="text-sm text-muted-foreground">No inbound webhooks.</p>
        )}
        {!!hooks.data?.items.length && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>URL</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead>Enabled</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {hooks.data.items.map((h) => (
                <TableRow key={h.id}>
                  <TableCell>
                    <div className="font-medium">{h.name}</div>
                    <div className="text-xs text-muted-foreground">{kindLabels[h.kind]}</div>
                  </TableCell>
                  <TableCell className="max-w-72">
                    <div className="flex items-center gap-1">
                      <code className="truncate text-xs" title={h.url}>
                        {h.url}
                      </code>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Copy URL of ${h.name}`}
                        onClick={() => void copy(h.url)}
                      >
                        <Copy />
                      </Button>
                    </div>
                  </TableCell>
                  <TableCell className="text-xs">
                    {h.last_used_at ? formatRelative(h.last_used_at) : "Never"}
                  </TableCell>
                  <TableCell>
                    <Switch
                      aria-label={`${h.name} enabled`}
                      checked={h.enabled}
                      onCheckedChange={(v) =>
                        void act(() =>
                          unwrap(
                            api.PATCH("/inbound-hooks/{id}", {
                              params: { path: { id: h.id } },
                              body: { name: h.name, enabled: v },
                            }),
                          ),
                        )
                      }
                    />
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Replace the credential of ${h.name}`}
                        title={h.kind === "json" ? "New token" : "Replace token"}
                        onClick={() =>
                          void act(async () => {
                            let secret: string | undefined;
                            if (h.kind === "json") {
                              if (
                                !window.confirm("Issue a new token? The old one stops working immediately.")
                              )
                                return;
                            } else {
                              secret =
                                window.prompt(
                                  `Paste the new ${h.kind === "teams" ? "security token" : "token"}`,
                                ) ?? undefined;
                              if (!secret) return;
                            }
                            const r = await unwrap(
                              api.POST("/inbound-hooks/{id}/credential", {
                                params: { path: { id: h.id } },
                                body: { secret },
                              }),
                            );
                            if (r.token) setCreated(r);
                            else toast.success("Token replaced");
                          })
                        }
                      >
                        <KeyRound />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Delete ${h.name}`}
                        title="Delete"
                        onClick={() =>
                          void act(async () => {
                            if (!window.confirm(`Delete the inbound webhook “${h.name}”?`)) return;
                            await unwrap(
                              api.DELETE("/inbound-hooks/{id}", { params: { path: { id: h.id } } }),
                            );
                          })
                        }
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
      {created &&
        (created.token ? (
          <SecretDialog
            title={`Token for “${created.name}”`}
            description="Copy it now; it is not shown again. Send it as a bearer token."
            secret={created.token}
            extra={
              <pre className="overflow-x-auto rounded border bg-muted p-2 text-xs">{`curl -X POST ${created.url} \\
  -H 'Authorization: Bearer <token>' \\
  -H 'Content-Type: application/json' \\
  -d '{"problem": "vpn", "minutes": 10}'

# Mark a change (e.g. at the end of a deployment):
  -d '{"action": "change", "title": "Deployed VPN 2.4", "problem": "vpn"}'`}</pre>
            }
            onClose={() => setCreated(null)}
          />
        ) : (
          <SecretDialog
            title={`URL for “${created.name}”`}
            description={
              created.kind === "teams"
                ? "Use this as the callback URL of the Teams outgoing webhook."
                : "Use this as the request URL of the Mattermost slash command (method POST)."
            }
            secret={created.url}
            onClose={() => setCreated(null)}
          />
        ))}
    </Card>
  );
}
