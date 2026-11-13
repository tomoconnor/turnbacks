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

import { openDB, type DBSchema, type IDBPDatabase } from "idb";
import type { Schemas } from "@/api/client";

export type TurnbackPayload = Schemas["TurnbackInput"];

export type OutboxStatus = "pending" | "failed";

/** A turnback stored on the device until the server confirms it. */
export interface OutboxItem {
  id: string; // == payload.id (client-generated UUID, idempotency key)
  payload: TurnbackPayload;
  problemTitle: string;
  createdAt: string;
  attempts: number;
  status: OutboxStatus;
  lastError?: string;
  queuedOffline: boolean;
}

interface TurnbacksDB extends DBSchema {
  outbox: { key: string; value: OutboxItem; indexes: { byCreated: string } };
  cache: { key: string; value: { key: string; value: unknown; savedAt: string } };
}

let dbPromise: Promise<IDBPDatabase<TurnbacksDB>> | null = null;

export function db(): Promise<IDBPDatabase<TurnbacksDB>> {
  if (!dbPromise) {
    dbPromise = openDB<TurnbacksDB>("turnbacks", 1, {
      upgrade(d) {
        const outbox = d.createObjectStore("outbox", { keyPath: "id" });
        outbox.createIndex("byCreated", "createdAt");
        d.createObjectStore("cache", { keyPath: "key" });
      },
    });
  }
  return dbPromise;
}

/** For tests: forget the cached connection. */
export function resetDbForTests() {
  dbPromise = null;
}

// Minimal offline cache of non-personal reference data (problem names,
// organisation settings) so the reporting UI works without a network.
export async function saveCache(key: string, value: unknown): Promise<void> {
  try {
    await (await db()).put("cache", { key, value, savedAt: new Date().toISOString() });
  } catch {
    // Storage may be unavailable (private browsing); the app still works online.
  }
}

export async function loadCache<T>(key: string): Promise<T | undefined> {
  try {
    const row = await (await db()).get("cache", key);
    return row?.value as T | undefined;
  } catch {
    return undefined;
  }
}

/** Remove cached reference data (used on sign-out). */
export async function clearCache(): Promise<void> {
  try {
    await (await db()).clear("cache");
  } catch {
    /* ignore */
  }
}
