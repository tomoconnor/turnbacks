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

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useMemo, type ReactNode } from "react";
import { api, NetworkError, unwrap } from "@/api/client";

/**
 * Turnbacks has no user accounts of its own. The session says how people are
 * identified (network address or single sign-on), whether this request comes
 * from an administrator, and whether the server is reachable.
 */
interface SessionState {
  loading: boolean;
  /** "ip": people are network addresses; "header": single sign-on users. */
  identityMode: "ip" | "header";
  /** The single sign-on user, when known. */
  user: string;
  admin: boolean;
  /** The administrator's certificate name or single sign-on user, when admin. */
  adminName: string;
  /** How administration was granted: certificate, sso or dev. */
  adminVia: string;
  adminEnabled: boolean;
  /** How administration can be granted on this server. */
  adminMethods: ("certificate" | "sso")[];
  slackEnabled: boolean;
  /** True when the server could not be reached. */
  offline: boolean;
}

const SessionContext = createContext<SessionState | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["session"],
    queryFn: () => unwrap(api.GET("/session")),
    staleTime: 60_000,
    retry: (count, err) => !(err instanceof NetworkError) && count < 2,
  });

  useEffect(() => {
    const onOnline = () => qc.invalidateQueries({ queryKey: ["session"] });
    window.addEventListener("online", onOnline);
    return () => window.removeEventListener("online", onOnline);
  }, [qc]);

  const value = useMemo<SessionState>(
    () => ({
      loading: q.isPending,
      identityMode: q.data?.identity_mode ?? "ip",
      user: q.data?.user ?? "",
      admin: q.data?.admin ?? false,
      adminName: q.data?.admin_name ?? "",
      adminVia: q.data?.admin_via ?? "",
      adminEnabled: q.data?.admin_enabled ?? false,
      adminMethods: q.data?.admin_methods ?? [],
      slackEnabled: q.data?.slack_enabled ?? false,
      offline: q.error instanceof NetworkError,
    }),
    [q.data, q.error, q.isPending],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionState {
  const ctx = useContext(SessionContext);
  if (!ctx) throw new Error("useSession outside SessionProvider");
  return ctx;
}

const TEAM_KEY = "turnbacks.team";

/** The reporter's team, remembered on this device. */
export function loadTeam(): string {
  try {
    return localStorage.getItem(TEAM_KEY) ?? "";
  } catch {
    return "";
  }
}

export function saveTeam(id: string) {
  try {
    if (id) localStorage.setItem(TEAM_KEY, id);
    else localStorage.removeItem(TEAM_KEY);
  } catch {
    /* storage unavailable */
  }
}
