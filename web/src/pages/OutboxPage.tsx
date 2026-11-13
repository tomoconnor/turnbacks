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

import { RefreshCw, Trash2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useInvalidateStats } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { formatDateTime, formatMinutes } from "@/lib/format";
import { outbox } from "@/offline/outbox";
import { useOutboxItems } from "@/offline/useOutbox";

const statusLabel = {
  pending: "Waiting to send",
  failed: "Rejected",
} as const;

export function OutboxPage() {
  const items = useOutboxItems();
  const invalidate = useInvalidateStats();
  const [busy, setBusy] = useState(false);

  async function retryAll() {
    setBusy(true);
    try {
      for (const i of items) await outbox.retry(i.id);
      invalidate();
      const left = outbox.snapshot().length;
      if (left === 0) toast.success("All queued reports were sent.");
      else toast.warning(`${left} report(s) could not be sent yet.`);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Unsent reports</CardTitle>
        <CardDescription>
          Reports recorded while the server was unreachable are stored on this device and sent automatically.
          Each has a unique id, so retrying never creates duplicates. Nothing here is discarded unless you
          choose to.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {items.length === 0 ? (
          <p className="text-sm text-muted-foreground" data-testid="outbox-empty">
            Nothing waiting — everything has been sent.
          </p>
        ) : (
          <>
            <Button onClick={retryAll} disabled={busy} data-testid="retry-all">
              <RefreshCw className={busy ? "animate-spin" : ""} /> Retry now
            </Button>
            <ul className="divide-y rounded-md border">
              {items.map((i) => (
                <li key={i.id} className="flex flex-wrap items-center gap-3 p-3 text-sm">
                  <div className="flex-1">
                    <div className="font-medium">{i.problemTitle}</div>
                    <div className="text-xs text-muted-foreground">
                      {(i.payload.quantity ?? 1) > 1 ? `${i.payload.quantity} × ` : ""}
                      {formatMinutes(i.payload.duration_minutes)} · happened{" "}
                      {formatDateTime(i.payload.occurred_at!)} · {i.attempts} attempt(s)
                    </div>
                    {i.lastError && (
                      <div className="text-xs text-muted-foreground">Last error: {i.lastError}</div>
                    )}
                  </div>
                  {outbox.canSend(i) ? (
                    <Badge variant={i.status === "failed" ? "destructive" : "secondary"}>
                      {statusLabel[i.status]}
                    </Badge>
                  ) : (
                    <Badge variant="outline" title="Sent when this person is signed in on this device">
                      Recorded by {i.owner}
                    </Badge>
                  )}
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Discard this report"
                    onClick={async () => {
                      if (window.confirm("Discard this unsent report? It will not be recorded."))
                        await outbox.discard(i.id);
                    }}
                  >
                    <Trash2 />
                  </Button>
                </li>
              ))}
            </ul>
          </>
        )}
      </CardContent>
    </Card>
  );
}
