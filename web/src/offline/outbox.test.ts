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

import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, NetworkError, type Schemas } from "@/api/client";
import { db, resetDbForTests, type TurnbackPayload } from "./db";
import { Outbox, type Sender } from "./outbox";

let n = 0;
function payload(): TurnbackPayload {
  n++;
  return {
    id: `00000000-0000-4000-8000-${String(n).padStart(12, "0")}`,
    problem_id: "p1",
    duration_minutes: 10,
    occurred_at: "2026-01-01T10:00:00.000Z",
    source: "web",
  };
}

function ok(p: TurnbackPayload): Schemas["SubmitResult"] {
  return {
    replayed: false,
    quantity: p.quantity ?? 1,
    turnback: {
      id: p.id,
      problem_id: p.problem_id,
      problem_title: "VPN",
      team_id: null,
      duration_minutes: p.duration_minutes,
      duration_estimated: true,
      note: null,
      occurred_at: p.occurred_at!,
      submitted_at: p.occurred_at!,
      source: p.source ?? "web",
      is_demo: false,
      batch_id: null,
    },
  };
}

beforeEach(async () => {
  const d = await db();
  await d.clear("outbox");
  d.close();
  resetDbForTests();
  Object.defineProperty(navigator, "onLine", { value: true, configurable: true });
});

describe("Outbox", () => {
  it("sends immediately and removes the item on success", async () => {
    const sender = vi.fn<Sender>(async (p) => ok(p));
    const o = new Outbox(sender);
    const out = await o.submit(payload(), "VPN");
    expect(out.kind).toBe("sent");
    expect(o.snapshot()).toHaveLength(0);
    expect(sender.mock.calls[0][0].source).toBe("web");
  });

  it("keeps the item when the network is down and retries with the same id", async () => {
    const sender = vi
      .fn<Sender>()
      .mockRejectedValueOnce(new NetworkError())
      .mockImplementation(async (p) => ok(p));
    const o = new Outbox(sender);
    const p = payload();
    const out = await o.submit(p, "VPN");
    expect(out.kind).toBe("queued");
    expect(o.snapshot()).toHaveLength(1);
    expect(o.snapshot()[0]).toMatchObject({ status: "pending", attempts: 1, queuedOffline: true });

    await o.flush();
    expect(o.snapshot()).toHaveLength(0);
    const retried = sender.mock.calls[1][0];
    expect(retried.id).toBe(p.id); // idempotency key preserved
    expect(retried.occurred_at).toBe(p.occurred_at); // original occurrence time preserved
    expect(retried.source).toBe("pwa_offline");
  });

  it("queues without sending when the browser is offline", async () => {
    Object.defineProperty(navigator, "onLine", { value: false, configurable: true });
    const sender = vi.fn<Sender>();
    const o = new Outbox(sender);
    const out = await o.submit(payload(), "VPN");
    expect(out.kind).toBe("queued");
    expect(sender).not.toHaveBeenCalled();
  });

  it("survives a reload (persisted in IndexedDB)", async () => {
    const o1 = new Outbox(vi.fn<Sender>().mockRejectedValue(new NetworkError()));
    await o1.submit(payload(), "VPN");
    const o2 = new Outbox(vi.fn<Sender>());
    await o2.ready();
    expect(o2.snapshot()).toHaveLength(1);
  });

  it("treats a duplicate retry (idempotent replay) as success", async () => {
    const sender = vi
      .fn<Sender>()
      .mockRejectedValueOnce(new ApiError(503, { code: "unavailable", message: "busy" }))
      .mockImplementation(async (p) => ({ ...ok(p), replayed: true }));
    const o = new Outbox(sender);
    await o.submit(payload(), "VPN");
    await o.flush();
    await o.flush();
    expect(o.snapshot()).toHaveLength(0);
    expect(sender).toHaveBeenCalledTimes(2);
  });

  it("returns immediate validation errors to the form, but keeps rejected background items visible", async () => {
    const reject = new ApiError(422, { code: "validation_failed", message: "bad" });
    const o = new Outbox(vi.fn<Sender>().mockRejectedValue(reject));
    const out = await o.submit(payload(), "VPN");
    expect(out.kind).toBe("rejected");
    expect(o.snapshot()).toHaveLength(0);

    const sender = vi.fn<Sender>().mockRejectedValueOnce(new NetworkError()).mockRejectedValue(reject);
    const o2 = new Outbox(sender);
    await o2.submit(payload(), "VPN");
    await o2.flush();
    expect(o2.snapshot()).toHaveLength(1);
    expect(o2.snapshot()[0]).toMatchObject({ status: "failed", lastError: "bad" });
    await o2.discard(o2.snapshot()[0].id);
    expect(o2.snapshot()).toHaveLength(0);
  });
});
