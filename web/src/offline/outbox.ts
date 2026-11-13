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

import { ApiError, NetworkError, api, unwrap, type Schemas } from "@/api/client";
import { db, type OutboxItem, type TurnbackPayload } from "./db";

export type SendOutcome =
  | { kind: "sent"; result: Schemas["SubmitResult"] }
  | { kind: "queued"; item: OutboxItem }
  | { kind: "rejected"; error: ApiError };

type Listener = () => void;

/** Sends a payload to the server. Replaceable in tests. */
export type Sender = (payload: TurnbackPayload) => Promise<Schemas["SubmitResult"]>;

const defaultSender: Sender = (payload) => unwrap(api.POST("/turnbacks", { body: payload }));

function isRetryable(e: unknown): boolean {
  if (e instanceof NetworkError) return true;
  if (e instanceof ApiError) return e.status >= 500 || e.status === 408 || e.status === 429;
  return true;
}

/**
 * The outbox stores every submission in IndexedDB *before* sending it, so a
 * report is never lost if the network (or the VPN) fails at the moment it is
 * recorded. Items are retried automatically and are only removed after the
 * server confirms them (201 created, or 200 idempotent replay). Rejected items
 * are kept, visibly, until the user explicitly discards them.
 */
export class Outbox {
  private listeners = new Set<Listener>();
  private items: OutboxItem[] = [];
  private flushing: Promise<void> | null = null;
  private loaded: Promise<void>;
  private sender: Sender;
  /** The signed-in single sign-on user ("" if none); undefined until known. */
  private identity: string | undefined;

  constructor(sender: Sender = defaultSender) {
    this.sender = sender;
    this.loaded = this.reload();
  }

  subscribe(fn: Listener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  snapshot(): OutboxItem[] {
    return this.items;
  }

  ready(): Promise<void> {
    return this.loaded;
  }

  /** Records who is signed in now (the session's single sign-on user, or ""). */
  setIdentity(user: string) {
    if (this.identity === user) return;
    this.identity = user;
    this.items = [...this.items]; // new snapshot: what can be sent has changed
    this.emit();
  }

  /**
   * Whether an item may be sent now: items recorded by a signed-in user are
   * held while anyone else (or nobody yet known) is signed in, so that one
   * person's reports are never recorded as another's.
   */
  canSend(item: OutboxItem): boolean {
    return !item.owner || item.owner === this.identity;
  }

  private emit() {
    for (const l of this.listeners) l();
  }

  private async reload() {
    try {
      const all = await (await db()).getAllFromIndex("outbox", "byCreated");
      this.items = all;
    } catch {
      this.items = [];
    }
    this.emit();
  }

  private async put(item: OutboxItem) {
    await (await db()).put("outbox", item);
    await this.reload();
  }

  private async remove(id: string) {
    await (await db()).delete("outbox", id);
    await this.reload();
  }

  /** Persist a new submission and try to send it immediately. */
  async submit(payload: TurnbackPayload, problemTitle: string): Promise<SendOutcome> {
    await this.loaded;
    const offline = typeof navigator !== "undefined" && navigator.onLine === false;
    const item: OutboxItem = {
      id: payload.id,
      payload,
      problemTitle,
      createdAt: new Date().toISOString(),
      attempts: 0,
      status: "pending",
      queuedOffline: offline,
      owner: this.identity || undefined,
    };
    await this.put(item);
    if (offline) return { kind: "queued", item };
    return this.send(item, true);
  }

  /** Attempt to deliver one item. */
  private async send(item: OutboxItem, immediate: boolean): Promise<SendOutcome> {
    const payload: TurnbackPayload = {
      ...item.payload,
      source: item.queuedOffline || item.attempts > 0 ? "pwa_offline" : (item.payload.source ?? "web"),
    };
    try {
      const result = await this.sender(payload);
      await this.remove(item.id);
      return { kind: "sent", result };
    } catch (e) {
      if (isRetryable(e)) {
        const updated: OutboxItem = {
          ...item,
          attempts: item.attempts + 1,
          queuedOffline: true,
          status: "pending",
          lastError: e instanceof Error ? e.message : String(e),
        };
        await this.put(updated);
        return { kind: "queued", item: updated };
      }
      const err = e as ApiError;
      if (immediate) {
        // The user is looking at the form and sees the error straight away,
        // so the draft is returned to them rather than kept in the outbox.
        await this.remove(item.id);
      } else {
        await this.put({ ...item, status: "failed", lastError: err.message });
      }
      return { kind: "rejected", error: err };
    }
  }

  /**
   * Send all queued items. Stops at the first network failure (we are
   * probably still offline).
   */
  flush(): Promise<void> {
    if (this.flushing) return this.flushing;
    this.flushing = (async () => {
      await this.loaded;
      for (const item of [...this.items]) {
        if (item.status === "failed" || !this.canSend(item)) continue;
        const out = await this.send(item, false);
        if (out.kind === "queued") break;
      }
    })().finally(() => {
      this.flushing = null;
    });
    return this.flushing;
  }

  /** Manually retry a failed or waiting item. */
  async retry(id: string): Promise<SendOutcome | undefined> {
    await this.loaded;
    const item = this.items.find((i) => i.id === id);
    if (!item || !this.canSend(item)) return undefined;
    return this.send({ ...item, status: "pending" }, false);
  }

  /** Explicitly discard an item (never done automatically). */
  async discard(id: string) {
    await this.remove(id);
  }
}

export const outbox = new Outbox();
