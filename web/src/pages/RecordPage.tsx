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
import { Check, ChevronDown, Minus, Plus, Users, Zap } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import { useInvalidateStats, useQuickProblems, useSettings, useSummary, useTeams } from "@/api/hooks";
import { CreateProblemDialog } from "@/components/CreateProblemDialog";
import { ProblemPicker, type PickedProblem } from "@/components/ProblemPicker";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
import { formatCount, formatHours, formatMinutes } from "@/lib/format";
import { loadRecent, rememberRecent, type RecentEntry } from "@/lib/recent";
import { durationSchema, noteSchema, quantitySchema } from "@/lib/schemas";
import { loadTeam, saveTeam, useSession } from "@/lib/session";
import { cn } from "@/lib/utils";
import { uuidv4 } from "@/lib/uuid";
import type { TurnbackPayload } from "@/offline/db";
import { outbox } from "@/offline/outbox";

export const PRESETS = [1, 5, 10, 15, 30, 60] as const;

/** Quantities above this ask for confirmation (the server enforces the same rule). */
export const LARGE_QUANTITY = 20;

type When = "now" | "15" | "60" | "custom";

function isTyping(el: EventTarget | null) {
  return (
    el instanceof HTMLElement &&
    !!el.closest("input, textarea, select, [contenteditable=true], [role=dialog]")
  );
}

function toLocalInput(d: Date) {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Build the submission payload; the occurrence time is fixed at record time. */
export function buildPayload(args: {
  problemId: string;
  minutes: number;
  quantity?: number;
  estimated: boolean;
  note: string;
  teamId?: string;
  when: When;
  customTime?: string;
  confirmLarge: boolean;
  now?: Date;
}): TurnbackPayload {
  const now = args.now ?? new Date();
  let occurred = now;
  if (args.when === "15") occurred = new Date(now.getTime() - 15 * 60_000);
  if (args.when === "60") occurred = new Date(now.getTime() - 60 * 60_000);
  if (args.when === "custom" && args.customTime) occurred = new Date(args.customTime);
  const note = args.note.trim();
  return {
    id: uuidv4(),
    problem_id: args.problemId,
    duration_minutes: args.minutes,
    quantity: args.quantity ?? 1,
    duration_estimated: args.estimated,
    note: note ? note : null,
    occurred_at: occurred.toISOString(),
    source: "web",
    team_id: args.teamId || null,
    confirm_large_duration: args.confirmLarge,
  };
}

function describe(minutes: number, quantity: number) {
  return quantity > 1 ? `${quantity} × ${formatMinutes(minutes)}` : formatMinutes(minutes);
}

export function RecordPage() {
  const { offline } = useSession();
  const teams = useTeams();
  const settings = useSettings();
  const quick = useQuickProblems();
  const invalidate = useInvalidateStats();
  const qc = useQueryClient();
  const searchRef = useRef<HTMLInputElement>(null);
  const submitRef = useRef<HTMLButtonElement>(null);

  const [problem, setProblem] = useState<PickedProblem | null>(null);
  const [minutes, setMinutes] = useState<number | null>(null);
  const [custom, setCustom] = useState("");
  const [quantity, setQuantity] = useState("1");
  const [estimated, setEstimated] = useState(true);
  const [note, setNote] = useState("");
  const [teamId, setTeamId] = useState(loadTeam);
  const [when, setWhen] = useState<When>("now");
  const [customTime, setCustomTime] = useState(() => toLocalInput(new Date()));
  const [moreOpen, setMoreOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [creating, setCreating] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<null | { message: string; run: () => void }>(null);
  const [recent, setRecent] = useState<RecentEntry[]>(loadRecent);

  // Forget a remembered team that no longer exists or is inactive.
  useEffect(() => {
    if (teamId && teams.data && !teams.data.items.some((t) => t.id === teamId)) {
      setTeamId("");
      saveTeam("");
    }
  }, [teamId, teams.data]);

  const frequent = useMemo(() => (quick.data?.frequent ?? []).slice(0, 6), [quick.data]);
  const largeThreshold = settings.data?.large_duration_minutes ?? 120;
  const effectiveMinutes = minutes ?? (custom ? Number(custom) : null);
  const qty = Number(quantity) || 0;
  const optionsChanged = note.trim() !== "" || when !== "now" || !estimated;

  const reset = useCallback(() => {
    setProblem(null);
    setMinutes(null);
    setCustom("");
    setQuantity("1");
    setNote("");
    setEstimated(true);
    setWhen("now");
    setMoreOpen(false);
    setError(null);
  }, []);

  const pick = useCallback((p: PickedProblem) => {
    setProblem(p);
    setError(null);
  }, []);

  const bumpQuantity = useCallback((d: number) => {
    setQuantity((q) => String(Math.min(100, Math.max(1, (Number(q) || 1) + d))));
  }, []);

  const send = useCallback(
    async (
      p: PickedProblem,
      mins: number,
      count: number,
      opts: { confirmLarge?: boolean; quickRepeat?: boolean } = {},
    ) => {
      const payload = buildPayload({
        problemId: p.id,
        minutes: mins,
        quantity: count,
        estimated: opts.quickRepeat ? true : estimated,
        note: opts.quickRepeat ? "" : note,
        teamId,
        when: opts.quickRepeat ? "now" : when,
        customTime,
        confirmLarge: !!opts.confirmLarge,
      });
      setBusy(true);
      try {
        const out = await outbox.submit(payload, p.title);
        const what = describe(mins, count);
        switch (out.kind) {
          case "sent": {
            rememberRecent({ problemId: p.id, title: p.title, minutes: mins });
            setRecent(loadRecent());
            invalidate();
            const t = out.result.turnback;
            toast.success(`Recorded ${what} · ${t.problem_title}`, {
              description: "Thanks — it all adds up.",
              // Undo is a short safety net, not a fixture: gone after 5 seconds.
              duration: 5000,
              action: out.result.undo_token
                ? {
                    label: "Undo",
                    onClick: async () => {
                      await api.DELETE("/turnbacks/{id}", {
                        params: {
                          path: { id: t.id },
                          query: { batch: out.result.quantity > 1, undo_token: out.result.undo_token },
                        },
                      });
                      invalidate();
                      toast("Removed.");
                    },
                  }
                : undefined,
            });
            reset();
            searchRef.current?.focus();
            break;
          }
          case "queued":
            rememberRecent({ problemId: p.id, title: p.title, minutes: mins });
            setRecent(loadRecent());
            toast.info(`Saved on this device · ${p.title}`, {
              description:
                "The server can't be reached. It will be sent automatically when you're back online.",
            });
            reset();
            break;
          case "rejected":
            if (
              out.error.code === "duration_confirmation_required" ||
              out.error.code === "quantity_confirmation_required"
            ) {
              setConfirm({
                message: out.error.message,
                run: () => void send(p, mins, count, { ...opts, confirmLarge: true }),
              });
            } else {
              setError(out.error.message);
            }
            break;
        }
      } finally {
        setBusy(false);
        void qc.invalidateQueries({ queryKey: ["problems", "quick"] });
      }
    },
    [estimated, note, teamId, when, customTime, invalidate, reset, qc],
  );

  const submit = useCallback(() => {
    if (!problem) {
      setError("Choose what got in your way.");
      return;
    }
    const d = durationSchema.safeParse(effectiveMinutes ?? NaN);
    if (!d.success) {
      setError(effectiveMinutes == null ? "Choose how long it cost you." : d.error.issues[0].message);
      return;
    }
    const q = quantitySchema.safeParse(qty);
    if (!q.success) {
      setError(q.error.issues[0].message);
      return;
    }
    const n = noteSchema.safeParse(note);
    if (!n.success) {
      setError(n.error.issues[0].message);
      return;
    }
    const reasons: string[] = [];
    if (d.data > largeThreshold)
      reasons.push(`${formatMinutes(d.data)} is more than ${formatMinutes(largeThreshold)}`);
    if (q.data > LARGE_QUANTITY) reasons.push(`${q.data} occurrences at once is a lot`);
    if (reasons.length > 0) {
      setConfirm({
        message: `${reasons.join(", and ")}. Unusually large entries have a big effect on totals, so please confirm.`,
        run: () => void send(problem, d.data, q.data, { confirmLarge: true }),
      });
      return;
    }
    void send(problem, d.data, q.data);
  }, [problem, effectiveMinutes, qty, note, largeThreshold, send]);

  // Keyboard shortcuts.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) {
        if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
          e.preventDefault();
          submit();
        }
        return;
      }
      if (isTyping(e.target)) return;
      if (e.key === "/") {
        e.preventDefault();
        searchRef.current?.focus();
      } else if (e.key === "Escape") {
        reset();
      } else if ((e.key === "n" || e.key === "N") && !offline) {
        e.preventDefault();
        setCreating("");
      } else if (e.key === "+" || e.key === "=") {
        bumpQuantity(1);
      } else if (e.key === "-") {
        bumpQuantity(-1);
      } else if (e.key === "Enter" && problem) {
        e.preventDefault();
        submit();
      } else if (/^[1-9]$/.test(e.key)) {
        const i = Number(e.key) - 1;
        if (!problem) {
          const f = frequent[i];
          if (f) {
            e.preventDefault();
            pick({ id: f.id, title: f.title });
          }
        } else if (i < PRESETS.length) {
          e.preventDefault();
          setMinutes(PRESETS[i]);
          setCustom("");
          submitRef.current?.focus();
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [frequent, problem, pick, reset, submit, bumpQuantity, offline]);

  const again = recent.slice(0, 4);
  const total = effectiveMinutes && qty > 1 ? effectiveMinutes * qty : null;

  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">What got in your way?</h1>
        <WeekStats />
      </div>

      {offline && (
        <Alert variant="warning">
          <AlertDescription>
            You're offline. Reports are saved on this device and sent automatically when the connection
            returns.
          </AlertDescription>
        </Alert>
      )}

      <Card className="gap-0 overflow-hidden py-0">
        {/* 1. Problem */}
        <section aria-labelledby="problem-heading" className="space-y-3 p-4 sm:p-5">
          <h2 id="problem-heading" className="sr-only">
            Problem
          </h2>
          {problem ? (
            <div className="flex items-center gap-3">
              <Check className="size-5 shrink-0 text-success" aria-hidden />
              <span className="flex-1 text-lg font-medium" data-testid="selected-problem">
                {problem.title}
              </span>
              <Button variant="ghost" size="sm" onClick={() => setProblem(null)}>
                Change <kbd className="ml-1 hidden sm:inline">Esc</kbd>
              </Button>
            </div>
          ) : (
            <>
              {again.length > 0 && (
                <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Happened again">
                  <span className="text-xs font-medium text-muted-foreground">Again?</span>
                  {again.map((r) => (
                    <Button
                      key={r.problemId}
                      variant="outline"
                      size="sm"
                      disabled={busy}
                      onClick={() =>
                        void send({ id: r.problemId, title: r.title }, r.minutes, 1, { quickRepeat: true })
                      }
                      aria-label={`Happened again: ${r.title}, ${formatMinutes(r.minutes)}`}
                    >
                      <Zap className="text-amber-500" /> {r.title}
                      <span className="text-muted-foreground">{formatMinutes(r.minutes)}</span>
                    </Button>
                  ))}
                </div>
              )}
              {frequent.length > 0 && (
                <div
                  className="grid grid-cols-2 gap-2 md:grid-cols-3"
                  role="group"
                  aria-label="Common problems"
                >
                  {frequent.map((f, i) => (
                    <button
                      key={f.id}
                      type="button"
                      onClick={() => pick({ id: f.id, title: f.title })}
                      className="flex min-h-12 items-center gap-2.5 rounded-lg border bg-background px-3 py-2 text-left text-[0.8rem] transition-colors hover:border-primary/40 hover:bg-accent sm:text-sm"
                    >
                      <kbd aria-hidden className="hidden sm:inline">
                        {i + 1}
                      </kbd>
                      <span className="flex-1 leading-tight font-medium">{f.title}</span>
                      <span className="sr-only">, {f.recent_count} reports in 30 days</span>
                    </button>
                  ))}
                </div>
              )}
              <div className="flex items-start gap-2">
                <div className="flex-1">
                  <ProblemPicker ref={searchRef} onPick={pick} onCreate={(t) => setCreating(t)} />
                </div>
                {!offline && (
                  <Button variant="outline" className="h-11" onClick={() => setCreating("")}>
                    <Plus /> <span className="sr-only sm:not-sr-only">New</span>
                    <span className="sr-only"> problem</span>
                    <kbd aria-hidden className="hidden sm:inline">
                      N
                    </kbd>
                  </Button>
                )}
              </div>
            </>
          )}
        </section>

        {/* 2. Time lost */}
        <section aria-labelledby="time-heading" className="space-y-4 border-t p-4 sm:p-5">
          <div className="space-y-2">
            <h2 id="time-heading" className="text-sm font-medium">
              Time lost{qty > 1 ? " each time" : ""}
            </h2>
            <div
              className="flex flex-wrap items-center gap-2"
              role="radiogroup"
              aria-label="Duration presets"
            >
              {PRESETS.map((p, i) => (
                <button
                  key={p}
                  type="button"
                  role="radio"
                  aria-checked={minutes === p}
                  onClick={() => {
                    setMinutes(p);
                    setCustom("");
                  }}
                  className={cn(
                    "h-11 min-w-16 rounded-md border px-3 text-sm font-medium transition-colors hover:bg-accent",
                    minutes === p && "border-primary bg-primary text-primary-foreground hover:bg-primary/90",
                  )}
                  title={problem ? `Shortcut: ${i + 1}` : "Choose a problem first, then press 1–6"}
                >
                  <kbd
                    aria-hidden
                    className={cn(
                      "mr-1.5 hidden sm:inline",
                      !problem && "opacity-40",
                      minutes === p &&
                        "border-primary-foreground/30 bg-primary-foreground/15 text-primary-foreground",
                    )}
                  >
                    {i + 1}
                  </kbd>
                  {p} min
                </button>
              ))}
              <Label htmlFor="custom-minutes" className="sr-only">
                Custom minutes
              </Label>
              <Input
                id="custom-minutes"
                type="number"
                inputMode="numeric"
                min={1}
                max={1440}
                placeholder="Other min"
                className="h-11 w-28"
                value={custom}
                onChange={(e) => {
                  setCustom(e.target.value);
                  setMinutes(null);
                }}
              />
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <Label htmlFor="quantity" className="text-sm font-medium">
              How many times?
            </Label>
            <div className="flex items-center">
              <Button
                type="button"
                variant="outline"
                size="icon"
                className="h-11 w-11 rounded-r-none"
                aria-label="One fewer"
                onClick={() => bumpQuantity(-1)}
                disabled={qty <= 1}
              >
                <Minus />
              </Button>
              <Input
                id="quantity"
                type="number"
                inputMode="numeric"
                min={1}
                max={100}
                value={quantity}
                onChange={(e) => setQuantity(e.target.value)}
                onBlur={() => setQuantity(String(Math.min(100, Math.max(1, Math.round(qty) || 1))))}
                className="h-11 w-16 rounded-none border-x-0 text-center tabular-nums"
              />
              <Button
                type="button"
                variant="outline"
                size="icon"
                className="h-11 w-11 rounded-l-none"
                aria-label="One more"
                onClick={() => bumpQuantity(1)}
                disabled={qty >= 100}
              >
                <Plus />
              </Button>
            </div>
            <span className="text-sm text-muted-foreground" aria-live="polite" data-testid="quantity-total">
              {total ? (
                `= ${formatMinutes(total)} in total`
              ) : (
                <span className="hidden sm:inline">Press + / − to change</span>
              )}
            </span>
          </div>
        </section>

        {moreOpen && (
          <section
            id="more-options"
            aria-label="More options"
            className="grid gap-4 border-t p-4 sm:grid-cols-2 sm:p-5"
          >
            <div className="space-y-1.5">
              <Label htmlFor="when">When</Label>
              <NativeSelect id="when" value={when} onChange={(e) => setWhen(e.target.value as When)}>
                <option value="now">Just now</option>
                <option value="15">About 15 minutes ago</option>
                <option value="60">About an hour ago</option>
                <option value="custom">Earlier…</option>
              </NativeSelect>
              {when === "custom" && (
                <Input
                  type="datetime-local"
                  aria-label="When it happened"
                  value={customTime}
                  max={toLocalInput(new Date())}
                  onChange={(e) => setCustomTime(e.target.value)}
                />
              )}
              <label className="flex items-center gap-2 pt-2 text-sm">
                <input
                  type="checkbox"
                  className="size-4 accent-primary"
                  checked={estimated}
                  onChange={(e) => setEstimated(e.target.checked)}
                />
                The duration is an estimate
              </label>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="note">
                Note <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              <Textarea
                id="note"
                value={note}
                maxLength={1000}
                onChange={(e) => setNote(e.target.value)}
                placeholder="e.g. disconnected during deploy"
                rows={3}
              />
              <p className="text-xs text-muted-foreground">May be quoted, anonymised, in business cases.</p>
            </div>
          </section>
        )}

        {/* Submit */}
        <form
          className="flex flex-wrap items-center gap-x-4 gap-y-3 border-t bg-muted/40 p-4 sm:px-5"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Button
            ref={submitRef}
            type="submit"
            size="lg"
            disabled={busy || !problem || !effectiveMinutes}
            data-testid="submit-turnback"
          >
            {qty > 1 ? `Record ${qty} turnbacks` : "Record turnback"}
          </Button>
          <div className="flex items-center gap-2">
            <Label htmlFor="team" className="font-normal text-muted-foreground">
              <Users className="size-4" />
              <span className="sr-only">Your team</span>
            </Label>
            <NativeSelect
              id="team"
              className="h-9 w-44"
              value={teamId}
              onChange={(e) => {
                setTeamId(e.target.value);
                saveTeam(e.target.value);
              }}
              title="Your team (remembered on this device)"
            >
              <option value="">No team</option>
              {teams.data?.items.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </NativeSelect>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-expanded={moreOpen}
            aria-controls="more-options"
            onClick={() => setMoreOpen((v) => !v)}
          >
            More options
            {optionsChanged && !moreOpen && (
              <span className="size-1.5 rounded-full bg-primary" aria-label="(changed)" />
            )}
            <ChevronDown className={cn("transition-transform", moreOpen && "rotate-180")} />
          </Button>
          {error && (
            <p role="alert" className="w-full text-sm text-destructive">
              {error}
            </p>
          )}
        </form>
      </Card>

      <p className="hidden text-center text-xs text-muted-foreground sm:block">
        <kbd>1</kbd>–<kbd>6</kbd> pick · <kbd>N</kbd> new problem · <kbd>+</kbd>/<kbd>−</kbd> times ·{" "}
        <kbd>Enter</kbd> record · <kbd>?</kbd> all shortcuts
      </p>

      <CreateProblemDialog
        open={creating !== null}
        initialTitle={creating ?? ""}
        onOpenChange={(o) => !o && setCreating(null)}
        onCreated={(p) => pick({ id: p.id, title: p.title })}
        onUseExisting={pick}
      />

      <Dialog open={confirm !== null} onOpenChange={(o) => !o && setConfirm(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Is that right?</DialogTitle>
            <DialogDescription>{confirm?.message}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirm(null)}>
              Change it
            </Button>
            <Button
              onClick={() => {
                const fn = confirm?.run;
                setConfirm(null);
                fn?.();
              }}
            >
              Yes, record it
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function WeekStats() {
  const week = useSummary({ period: "last_7_days" });
  const s = week.data?.current;
  if (!s) return null;
  return (
    <p className="text-sm text-muted-foreground">
      This week:{" "}
      <span data-testid="stat-turnbacks" className="font-medium text-foreground tabular-nums">
        {formatCount(s.turnbacks)}
      </span>{" "}
      turnbacks · {formatHours(s.minutes)} lost
    </p>
  );
}
