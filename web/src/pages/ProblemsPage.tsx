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

import { Plus } from "lucide-react";
import { useDeferredValue, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useCategories, useProblemSearch } from "@/api/hooks";
import { CreateProblemDialog } from "@/components/CreateProblemDialog";
import { StatusBadge } from "@/components/StatusBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatCount, formatRelative } from "@/lib/format";

const PAGE = 25;

export function ProblemsPage() {
  const [q, setQ] = useState("");
  const [category, setCategory] = useState("");
  const [status, setStatus] = useState("active,monitoring");
  const [offset, setOffset] = useState(0);
  const [creating, setCreating] = useState(false);
  const deferred = useDeferredValue(q);
  const categories = useCategories();
  const navigate = useNavigate();
  const list = useProblemSearch({
    q: deferred || undefined,
    category_id: category || undefined,
    status,
    limit: PAGE,
    offset,
  });

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle>Problems</CardTitle>
        <Button onClick={() => setCreating(true)}>
          <Plus /> New problem
        </Button>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-col gap-2 sm:flex-row">
          <Input
            aria-label="Search problems"
            placeholder="Search by title, alias or description"
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setOffset(0);
            }}
          />
          <NativeSelect
            aria-label="Category"
            className="sm:w-56"
            value={category}
            onChange={(e) => {
              setCategory(e.target.value);
              setOffset(0);
            }}
          >
            <option value="">All categories</option>
            {categories.data?.items.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </NativeSelect>
          <NativeSelect
            aria-label="Status"
            className="sm:w-48"
            value={status}
            onChange={(e) => {
              setStatus(e.target.value);
              setOffset(0);
            }}
          >
            <option value="active,monitoring">Open</option>
            <option value="resolved">Resolved</option>
            <option value="archived">Archived</option>
            <option value="active,monitoring,resolved,archived">All</option>
          </NativeSelect>
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Problem</TableHead>
              <TableHead>Category</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="text-right">Last 30 days</TableHead>
              <TableHead>Last reported</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.data?.items.map((p) => (
              <TableRow key={p.id}>
                <TableCell>
                  <Link to={`/problems/${p.id}`} className="font-medium hover:underline">
                    {p.title}
                  </Link>
                  {p.alias && (
                    <Badge variant="outline" className="ml-2">
                      {p.alias}
                    </Badge>
                  )}
                  {p.is_demo && (
                    <Badge variant="warning" className="ml-2">
                      demo
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="text-muted-foreground">{p.category_name}</TableCell>
                <TableCell>
                  <StatusBadge status={p.status} />
                </TableCell>
                <TableCell className="text-right tabular-nums">{formatCount(p.recent_count ?? 0)}</TableCell>
                <TableCell className="text-muted-foreground">
                  {p.last_occurred_at ? formatRelative(p.last_occurred_at) : "never"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {list.data?.items.length === 0 && <p className="text-sm text-muted-foreground">No problems match.</p>}
        {list.data && list.data.total > PAGE && (
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">
              {offset + 1}–{Math.min(offset + PAGE, list.data.total)} of {list.data.total}
            </span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={offset === 0}
                onClick={() => setOffset(offset - PAGE)}
              >
                Previous
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={offset + PAGE >= list.data.total}
                onClick={() => setOffset(offset + PAGE)}
              >
                Next
              </Button>
            </div>
          </div>
        )}
      </CardContent>
      <CreateProblemDialog
        open={creating}
        initialTitle={q}
        onOpenChange={setCreating}
        onCreated={(p) => navigate(`/problems/${p.id}`)}
        onUseExisting={(p) => navigate(`/problems/${p.id}`)}
      />
    </Card>
  );
}
