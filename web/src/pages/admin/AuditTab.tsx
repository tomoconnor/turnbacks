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

import { Download } from "lucide-react";
import { useState } from "react";
import { downloadUrl } from "@/api/client";
import { useAudit } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatDateTime } from "@/lib/format";

const PAGE = 50;

export function AuditTab() {
  const [cursors, setCursors] = useState<number[]>([]);
  const before = cursors[cursors.length - 1];
  const audit = useAudit({ before_id: before, limit: PAGE });
  const items = audit.data?.items ?? [];

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-start justify-between gap-2">
        <div>
          <CardTitle className="text-base">Audit log</CardTitle>
          <CardDescription>
            Every change made by an administrator, and every time an administrator viewed or exported who
            reported what. {audit.data && `${audit.data.total} entries.`}
          </CardDescription>
        </div>
        <Button asChild variant="outline" size="sm">
          <a href={downloadUrl("/audit/export", {})} download>
            <Download /> Export CSV
          </a>
        </Button>
      </CardHeader>
      <CardContent className="space-y-3">
        {audit.data && items.length === 0 && <p className="text-sm text-muted-foreground">Nothing yet.</p>}
        {items.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>Administrator</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>Result</TableHead>
                <TableHead>From</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((e) => (
                <TableRow key={e.id}>
                  <TableCell className="text-xs whitespace-nowrap">{formatDateTime(e.at)}</TableCell>
                  <TableCell className="text-xs">
                    {e.admin}{" "}
                    <Badge variant="outline" className="ml-1">
                      {e.via}
                    </Badge>
                  </TableCell>
                  <TableCell className="max-w-96 text-xs">
                    <code>
                      {e.method} {e.action.replace(/^\/api\/v1/, "")}
                    </code>
                    {e.target && <div className="truncate text-muted-foreground">{e.target}</div>}
                  </TableCell>
                  <TableCell>
                    <Badge variant={e.status < 400 ? "success" : "destructive"}>{e.status}</Badge>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{e.client}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <div className="flex gap-2">
          {cursors.length > 0 && (
            <Button variant="outline" size="sm" onClick={() => setCursors(cursors.slice(0, -1))}>
              Newer
            </Button>
          )}
          {items.length === PAGE && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setCursors([...cursors, items[items.length - 1].id])}
            >
              Older
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
