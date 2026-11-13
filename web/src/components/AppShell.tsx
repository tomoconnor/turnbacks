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

import { CloudOff, Menu, Monitor, Moon, ShieldCheck, Sun, UploadCloud, UserRound, X } from "lucide-react";
import { Suspense, useEffect, useState } from "react";
import { Link, NavLink, Outlet } from "react-router";
import { useSettings } from "@/api/hooks";
import { ShortcutsDialog } from "@/components/ShortcutsDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useSession } from "@/lib/session";
import { type Theme, useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { useOnline, useOutboxItems, useOutboxSync } from "@/offline/useOutbox";

function NavItem({ to, children, onClick }: { to: string; children: string; onClick?: () => void }) {
  return (
    <NavLink
      to={to}
      end={to === "/"}
      onClick={onClick}
      className={({ isActive }) =>
        cn(
          "rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent",
          isActive ? "bg-accent text-foreground" : "text-muted-foreground",
        )
      }
    >
      {children}
    </NavLink>
  );
}

export function AppShell() {
  const { admin, adminName, adminVia, adminEnabled, offline, identityMode, user } = useSession();
  const online = useOnline();
  const items = useOutboxItems();
  const settings = useSettings();
  const [menuOpen, setMenuOpen] = useState(false);
  const [shortcuts, setShortcuts] = useState(false);
  useOutboxSync();

  const pending = items.filter((i) => i.status !== "failed").length;
  const failed = items.filter((i) => i.status === "failed").length;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (t.closest("input, textarea, select, [contenteditable=true]")) return;
      if (e.key === "?" && !e.metaKey && !e.ctrlKey) {
        e.preventDefault();
        setShortcuts(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const close = () => setMenuOpen(false);
  const nav = (
    <>
      <NavItem to="/" onClick={close}>
        Record
      </NavItem>
      <NavItem to="/dashboard" onClick={close}>
        Dashboard
      </NavItem>
      <NavItem to="/problems" onClick={close}>
        Problems
      </NavItem>
      {adminEnabled && (
        <NavItem to="/admin" onClick={close}>
          Admin
        </NavItem>
      )}
    </>
  );

  return (
    <div className="min-h-dvh bg-muted/30">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:rounded focus:bg-background focus:p-2"
      >
        Skip to content
      </a>
      <header className="no-print sticky top-0 z-40 border-b bg-background/95 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-4 px-4">
          <Link to="/" className="flex items-center gap-2 font-semibold">
            <img src="/favicon.svg" alt="" className="size-7" />
            <span>Turnbacks</span>
          </Link>
          <nav aria-label="Main" className="hidden items-center gap-1 md:flex">
            {nav}
          </nav>
          <div className="ml-auto flex items-center gap-2">
            {(offline || !online) && (
              <Badge
                variant="warning"
                title="The server cannot be reached. Reports are queued on this device."
              >
                <CloudOff /> Offline
              </Badge>
            )}
            {(pending > 0 || failed > 0) && (
              <Link to="/outbox" aria-label={`${pending} unsent, ${failed} failed`}>
                <Badge variant={failed > 0 ? "destructive" : "secondary"} data-testid="pending-count">
                  <UploadCloud /> {pending > 0 && `${pending} unsent`}
                  {pending > 0 && failed > 0 && " · "}
                  {failed > 0 && `${failed} failed`}
                </Badge>
              </Link>
            )}
            {admin ? (
              <Badge
                variant="secondary"
                title={
                  adminVia === "sso"
                    ? "Administrator through single sign-on"
                    : adminVia === "dev"
                      ? "Administrator in development (this machine)"
                      : "Administrator with a client certificate"
                }
              >
                <ShieldCheck /> <span className="hidden sm:inline">Admin · {adminName}</span>
              </Badge>
            ) : (
              user && (
                <Badge
                  variant="outline"
                  title="Signed in through single sign-on"
                  className="hidden sm:inline-flex"
                >
                  <UserRound /> {user}
                </Badge>
              )
            )}
            <ThemeButton />
            <Button
              variant="ghost"
              size="icon"
              className="md:hidden"
              aria-label="Menu"
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((v) => !v)}
            >
              {menuOpen ? <X /> : <Menu />}
            </Button>
          </div>
        </div>
        {menuOpen && (
          <nav aria-label="Mobile" className="flex flex-col gap-1 border-t px-4 py-2 md:hidden">
            {nav}
          </nav>
        )}
      </header>
      {settings.data?.demo_data_present && (
        <div className="no-print border-b border-amber-200 bg-amber-50 px-4 py-1.5 text-center text-xs text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100">
          This instance contains demonstration data (marked “demo”). Remove it with{" "}
          <code>turnbacks seed --purge</code>.
        </div>
      )}
      <main id="main" className="mx-auto max-w-6xl px-4 py-6">
        <Suspense fallback={<div className="p-8 text-sm text-muted-foreground">Loading…</div>}>
          <Outlet />
        </Suspense>
      </main>
      <footer className="no-print mx-auto max-w-6xl px-4 pb-8 text-xs text-muted-foreground">
        {settings.data?.org_name} · Turnbacks measures friction, not people. Reports are aggregated; only
        administrators can see {identityMode === "header" ? "who" : "which network address"} a report came
        from.{" "}
        <button className="underline" onClick={() => setShortcuts(true)}>
          Keyboard shortcuts
        </button>
      </footer>
      <ShortcutsDialog open={shortcuts} onOpenChange={setShortcuts} />
    </div>
  );
}

const themes: { value: Theme; label: string; Icon: typeof Sun }[] = [
  { value: "system", label: "System", Icon: Monitor },
  { value: "light", label: "Light", Icon: Sun },
  { value: "dark", label: "Dark", Icon: Moon },
];

// Cycles System → Light → Dark.
function ThemeButton() {
  const { theme, setTheme } = useTheme();
  const i = themes.findIndex((t) => t.value === theme);
  const current = themes[i];
  const next = themes[(i + 1) % themes.length];
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={`Theme: ${current.label}. Switch to ${next.label}`}
      title={`Theme: ${current.label}`}
      onClick={() => setTheme(next.value)}
      data-testid="theme-toggle"
    >
      <current.Icon />
    </Button>
  );
}
