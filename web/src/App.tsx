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

import { lazy, Suspense } from "react";
import { Route, Routes } from "react-router";
import { Toaster } from "sonner";
import { useTheme } from "@/lib/theme";
import { AppShell } from "@/components/AppShell";

// Chart-heavy pages are split out so the reporting screen loads quickly.
const AdminPage = lazy(() => import("@/pages/AdminPage").then((m) => ({ default: m.AdminPage })));
const BusinessCasePage = lazy(() =>
  import("@/pages/BusinessCasePage").then((m) => ({ default: m.BusinessCasePage })),
);
const DashboardPage = lazy(() => import("@/pages/DashboardPage").then((m) => ({ default: m.DashboardPage })));
const ProblemDetailPage = lazy(() =>
  import("@/pages/ProblemDetailPage").then((m) => ({ default: m.ProblemDetailPage })),
);
import { NotFoundPage } from "@/pages/NotFoundPage";
import { OutboxPage } from "@/pages/OutboxPage";
import { ProblemsPage } from "@/pages/ProblemsPage";
import { RecordPage } from "@/pages/RecordPage";

export function App() {
  const { resolved } = useTheme();
  return (
    <>
      <Routes>
        <Route
          element={
            <Suspense fallback={<div className="p-8 text-sm text-muted-foreground">Loading…</div>}>
              <AppShell />
            </Suspense>
          }
        >
          <Route index element={<RecordPage />} />
          <Route path="dashboard" element={<DashboardPage />} />
          <Route path="problems" element={<ProblemsPage />} />
          <Route path="problems/:id" element={<ProblemDetailPage />} />
          <Route path="problems/:id/business-case" element={<BusinessCasePage />} />
          <Route path="outbox" element={<OutboxPage />} />
          <Route path="admin" element={<AdminPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Routes>
      <Toaster position="bottom-center" richColors closeButton theme={resolved} />
    </>
  );
}
