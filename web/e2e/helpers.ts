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

import { readFileSync } from "node:fs";
import { expect, type APIRequestContext, type Page } from "@playwright/test";

/**
 * The e2e admin certificate (issued by start-server.sh) as base64 DER, the
 * form in which Caddy forwards a verified TLS client certificate.
 */
export function adminCertHeader(): string {
  const pem = readFileSync(`${process.env.E2E_CA_DIR ?? "../bin/e2e-ca"}/issued/e2e-admin.crt`, "utf8");
  return pem.replace(/-----[A-Z ]+-----/g, "").replace(/\s+/g, "");
}

/** Make every request from this page's browser context present the admin certificate. */
export async function actAsAdmin(page: Page) {
  await page.context().setExtraHTTPHeaders({ "X-Client-Cert": adminCertHeader() });
}

/** Stop presenting the admin certificate. */
export async function actAsEveryone(page: Page) {
  await page.context().setExtraHTTPHeaders({});
}

export async function categoryId(req: APIRequestContext): Promise<string> {
  const body = await (await req.get("/api/v1/categories")).json();
  return body.items[0].id;
}

export async function createProblem(req: APIRequestContext, title: string, alias?: string) {
  const res = await req.post("/api/v1/problems", {
    data: { title, category_id: await categoryId(req), alias: alias ?? null },
  });
  expect(res.status(), await res.text()).toBe(201);
  return (await res.json()) as { id: string; title: string };
}

/** Number of reports recorded against a problem in the last 7 days. */
export async function problemCount(req: APIRequestContext, problemId: string): Promise<number> {
  const res = await req.get(`/api/v1/turnbacks?period=last_7_days&problem_id=${problemId}&limit=1`);
  return (await res.json()).total;
}

export async function orgTurnbacks7d(req: APIRequestContext): Promise<number> {
  const res = await req.get("/api/v1/analytics/summary?period=last_7_days");
  return (await res.json()).current.turnbacks;
}

export function unique(prefix: string) {
  return `${prefix} ${Date.now().toString(36)}${Math.floor(Math.random() * 1000)}`;
}

/** Select a problem on the Record page via search. */
export async function pickProblem(page: Page, title: string) {
  await page.getByRole("combobox", { name: "Search problems" }).fill(title.split(" ").slice(0, 3).join(" "));
  await page.getByRole("option", { name: new RegExp(title.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")) }).click();
  await expect(page.getByTestId("selected-problem")).toHaveText(title);
}
