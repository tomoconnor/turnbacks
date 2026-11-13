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

import { useCallback, useEffect, useSyncExternalStore } from "react";
import { useInvalidateStats } from "@/api/hooks";
import { useSession } from "@/lib/session";
import { outbox } from "./outbox";

export function useOutboxItems() {
  return useSyncExternalStore(
    (fn) => outbox.subscribe(fn),
    () => outbox.snapshot(),
  );
}

export function useOnline() {
  return useSyncExternalStore(
    (fn) => {
      window.addEventListener("online", fn);
      window.addEventListener("offline", fn);
      return () => {
        window.removeEventListener("online", fn);
        window.removeEventListener("offline", fn);
      };
    },
    () => navigator.onLine,
  );
}

/** Background synchronisation of the outbox. Mount once. */
export function useOutboxSync() {
  const invalidate = useInvalidateStats(); // stable (memoised on the query client)
  const session = useSession();

  // Stable: the effect below must not re-run (and re-flush) on every render.
  const flush = useCallback(async () => {
    const before = outbox.snapshot().length;
    await outbox.flush();
    if (outbox.snapshot().length < before) invalidate();
  }, [invalidate]);

  useEffect(() => {
    void flush();
    const onOnline = () => void flush();
    const onVisible = () => document.visibilityState === "visible" && void flush();
    window.addEventListener("online", onOnline);
    document.addEventListener("visibilitychange", onVisible);
    const timer = window.setInterval(() => void flush(), 30_000);
    return () => {
      window.removeEventListener("online", onOnline);
      document.removeEventListener("visibilitychange", onVisible);
      window.clearInterval(timer);
    };
  }, [flush]);

  // Once the server says who is signed in, send what that person queued.
  const known = !session.loading && !session.offline;
  useEffect(() => {
    if (!known) return;
    outbox.setIdentity(session.user);
    void flush();
  }, [known, session.user, flush]);

  return flush;
}
