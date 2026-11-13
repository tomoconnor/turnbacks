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

import { fetchMock } from "./setup";

type Handler = (req: Request, body: unknown) => Response | Promise<Response>;

export function json(status: number, body?: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** Route mocked fetch calls by "METHOD /path" (path without query string). */
export function mockApi(routes: Record<string, Handler>) {
  const calls: { method: string; path: string; body: unknown }[] = [];
  fetchMock.mockImplementation(async (input, init) => {
    const req = input instanceof Request ? input : new Request(new URL(input, "http://localhost"), init);
    const url = new URL(req.url, "http://localhost");
    const text = req.method === "GET" ? "" : await req.clone().text();
    const body = text ? JSON.parse(text) : undefined;
    const key = `${req.method} ${url.pathname.replace(/^\/api\/v1/, "")}`;
    calls.push({ method: req.method, path: url.pathname + url.search, body });
    const h = routes[key];
    if (!h) return json(404, { error: { code: "not_found", message: `unmocked ${key}` } });
    return h(req, body);
  });
  return calls;
}

export const team = {
  id: "44444444-4444-4444-8444-444444444444",
  name: "Platform",
  active: true,
  hourly_rate_minor: null,
  headcount: 8,
  is_demo: false,
};

export const settings = {
  org_name: "Acme",
  currency: "GBP",
  currency_exponent: 2,
  default_hourly_rate_minor: 5000,
  reporting_timezone: "Europe/London",
  recoverable_pct: 100,
  large_duration_minutes: 120,
  note_retention_days: null,
  reporter_retention_days: null,
  updated_at: "2026-01-01T00:00:00Z",
  demo_data_present: false,
};

export const vpn = {
  id: "22222222-2222-4222-8222-222222222222",
  title: "Corporate VPN disconnects",
  alias: "vpn",
  status: "active" as const,
  category_id: "33333333-3333-4333-8333-333333333333",
  recent_count: 42,
  last_occurred_at: "2026-01-01T00:00:00Z",
};
