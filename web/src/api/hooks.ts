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

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { loadCache, saveCache } from "@/offline/db";
import { api, NetworkError, unwrap, type Schemas } from "./client";

export type AnalyticsParams = {
  period: "last_7_days" | "last_30_days" | "last_90_days" | "this_year" | "custom";
  from?: string;
  to?: string;
  category_id?: string;
  team_id?: string;
  problem_id?: string;
};

function clean<T extends object>(o: T): T {
  return Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined && v !== "")) as T;
}

/** Query that falls back to an offline cache of non-personal reference data. */
function useCachedQuery<T>(key: unknown[], cacheKey: string, fn: () => Promise<T>) {
  return useQuery({
    queryKey: key,
    queryFn: async () => {
      try {
        const data = await fn();
        void saveCache(cacheKey, data);
        return data;
      } catch (e) {
        if (e instanceof NetworkError) {
          const cached = await loadCache<T>(cacheKey);
          if (cached !== undefined) return cached;
        }
        throw e;
      }
    },
    staleTime: 30_000,
  });
}

export function useSettings() {
  return useCachedQuery(["settings"], "settings", () => unwrap(api.GET("/settings")));
}

export function useCategories(includeInactive = false) {
  return useCachedQuery(["categories", includeInactive], `categories:${includeInactive}`, () =>
    unwrap(api.GET("/categories", { params: { query: { include_inactive: includeInactive } } })),
  );
}

export function useTeams(includeInactive = false) {
  return useQuery({
    queryKey: ["teams", includeInactive],
    queryFn: () => unwrap(api.GET("/teams", { params: { query: { include_inactive: includeInactive } } })),
    staleTime: 60_000,
  });
}

export function useQuickProblems() {
  return useCachedQuery(["problems", "quick"], "problems:quick", () => unwrap(api.GET("/problems/quick")));
}

/** All active problems, cached for offline search. */
export function useAllActiveProblems() {
  return useCachedQuery(["problems", "all-active"], "problems:all", () =>
    unwrap(api.GET("/problems", { params: { query: { limit: 200 } } })),
  );
}

export function useProblemSearch(params: {
  q?: string;
  category_id?: string;
  status?: string;
  limit?: number;
  offset?: number;
}) {
  return useQuery({
    queryKey: ["problems", "search", params],
    queryFn: () => unwrap(api.GET("/problems", { params: { query: clean(params) } })),
    placeholderData: keepPreviousData,
  });
}

export function useSimilarProblems(title: string) {
  return useQuery({
    queryKey: ["problems", "similar", title],
    queryFn: () => unwrap(api.GET("/problems/similar", { params: { query: { title } } })),
    enabled: title.trim().length >= 3,
    staleTime: 10_000,
  });
}

export function useProblem(id: string | undefined) {
  return useQuery({
    queryKey: ["problem", id],
    queryFn: () => unwrap(api.GET("/problems/{id}", { params: { path: { id: id! } } })),
    enabled: !!id,
  });
}

export function useRemediations(problemId: string | undefined) {
  return useQuery({
    queryKey: ["remediations", problemId],
    queryFn: () => unwrap(api.GET("/problems/{id}/remediations", { params: { path: { id: problemId! } } })),
    enabled: !!problemId,
  });
}

export function useTurnbacks(
  p: Partial<AnalyticsParams> & { reporter?: string; limit?: number; offset?: number },
  enabled = true,
) {
  return useQuery({
    queryKey: ["turnbacks", "list", p],
    queryFn: () => unwrap(api.GET("/turnbacks", { params: { query: clean(p) } })),
    enabled,
    placeholderData: keepPreviousData,
  });
}

export function useSummary(p: AnalyticsParams, enabled = true) {
  return useQuery({
    queryKey: ["analytics", "summary", p],
    queryFn: () => unwrap(api.GET("/analytics/summary", { params: { query: clean(p) } })),
    enabled,
    placeholderData: keepPreviousData,
  });
}

export function useTimeseries(p: AnalyticsParams & { bucket?: "day" | "week" | "month" }, enabled = true) {
  return useQuery({
    queryKey: ["analytics", "timeseries", p],
    queryFn: () => unwrap(api.GET("/analytics/timeseries", { params: { query: clean(p) } })),
    enabled,
    placeholderData: keepPreviousData,
  });
}

export function useRanking(
  p: Omit<AnalyticsParams, "problem_id"> & {
    sort: "count" | "minutes" | "cost";
    limit?: number;
    offset?: number;
  },
  enabled = true,
) {
  return useQuery({
    queryKey: ["analytics", "problems", p],
    queryFn: () => unwrap(api.GET("/analytics/problems", { params: { query: clean(p) } })),
    enabled,
    placeholderData: keepPreviousData,
  });
}

export function useCategoryBreakdown(p: AnalyticsParams, enabled = true) {
  return useQuery({
    queryKey: ["analytics", "categories", p],
    queryFn: () =>
      unwrap(
        api.GET("/analytics/categories", {
          params: { query: clean({ period: p.period, from: p.from, to: p.to, team_id: p.team_id }) },
        }),
      ),
    enabled,
    placeholderData: keepPreviousData,
  });
}

export function useTeamBreakdown(p: AnalyticsParams, enabled = true) {
  return useQuery({
    queryKey: ["analytics", "teams", p],
    queryFn: () =>
      unwrap(
        api.GET("/analytics/teams", {
          params: { query: clean({ period: p.period, from: p.from, to: p.to, category_id: p.category_id }) },
        }),
      ),
    enabled,
    placeholderData: keepPreviousData,
  });
}

export function useRemediationOverview(enabled = true) {
  return useQuery({
    queryKey: ["analytics", "remediations"],
    queryFn: () => unwrap(api.GET("/analytics/remediations")),
    enabled,
  });
}

export type BusinessCaseParams = {
  period?: AnalyticsParams["period"];
  from?: string;
  to?: string;
  implementation_cost_minor?: number;
  annual_cost_minor?: number;
  expected_reduction_pct?: number;
  recoverable_pct?: number;
};

export function useBusinessCase(id: string | undefined, p: BusinessCaseParams) {
  return useQuery({
    queryKey: ["business-case", id, p],
    queryFn: () =>
      unwrap(api.GET("/problems/{id}/business-case", { params: { path: { id: id! }, query: clean(p) } })),
    enabled: !!id,
    placeholderData: keepPreviousData,
  });
}

/** Invalidate everything derived from turnbacks after a write. */
export function useInvalidateStats() {
  const qc = useQueryClient();
  return useCallback(() => {
    void qc.invalidateQueries({ queryKey: ["analytics"] });
    void qc.invalidateQueries({ queryKey: ["problems"] });
    void qc.invalidateQueries({ queryKey: ["turnbacks"] });
    void qc.invalidateQueries({ queryKey: ["business-case"] });
  }, [qc]);
}

export function useCreateProblem() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas["ProblemInput"]) => unwrap(api.POST("/problems", { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["problems"] }),
  });
}

export function useUpdateProblem(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas["ProblemInput"]) =>
      unwrap(api.PATCH("/problems/{id}", { params: { path: { id } }, body })),
    onSuccess: (p) => {
      qc.setQueryData(["problem", id], p);
      void qc.invalidateQueries({ queryKey: ["problems"] });
    },
  });
}

// ---- Change markers ---------------------------------------------------------

export function useChanges(p: { from?: string; to?: string; problem_id?: string }, enabled = true) {
  return useQuery({
    queryKey: ["changes", "list", p],
    queryFn: () => unwrap(api.GET("/changes", { params: { query: clean(p) } })),
    enabled,
  });
}

export function useChangeOverview(problemId: string | undefined, windowDays: number) {
  return useQuery({
    queryKey: ["changes", "overview", problemId ?? "", windowDays],
    queryFn: () =>
      unwrap(
        api.GET("/analytics/changes", {
          params: { query: clean({ problem_id: problemId, window_days: windowDays }) },
        }),
      ),
    placeholderData: keepPreviousData,
  });
}

export function useCreateChange() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas["ChangeMarkerInput"]) => unwrap(api.POST("/changes", { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["changes"] }),
  });
}

export function useDeleteChange() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE("/changes/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["changes"] }),
  });
}

// ---- Webhooks (admin) -------------------------------------------------------

export function useWebhooks() {
  return useQuery({ queryKey: ["webhooks"], queryFn: () => unwrap(api.GET("/webhooks")) });
}

export function useWebhookDeliveries(id: string | undefined) {
  return useQuery({
    queryKey: ["webhooks", "deliveries", id],
    queryFn: () => unwrap(api.GET("/webhooks/{id}/deliveries", { params: { path: { id: id! } } })),
    enabled: !!id,
    refetchInterval: 5_000,
  });
}

export function useInboundHooks() {
  return useQuery({ queryKey: ["inbound-hooks"], queryFn: () => unwrap(api.GET("/inbound-hooks")) });
}

// ---- Audit log (admin) ------------------------------------------------------

export function useAudit(p: { before_id?: number; limit?: number }) {
  return useQuery({
    queryKey: ["audit", p],
    queryFn: () => unwrap(api.GET("/audit", { params: { query: clean(p) } })),
    placeholderData: keepPreviousData,
  });
}
