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

import { expect, test } from "@playwright/test";
import { actAsAdmin, createProblem, unique } from "./helpers";

test("anyone can mark a change and see before and after", async ({ page }) => {
  const problem = await createProblem(page.request, unique("Printer queue jams"));
  await page.request.post("/api/v1/turnbacks", {
    data: {
      id: crypto.randomUUID(),
      problem_id: problem.id,
      duration_minutes: 10,
      occurred_at: new Date(Date.now() - 2 * 3600_000).toISOString(),
    },
  });

  await page.goto(`/problems/${problem.id}`);
  await page.getByTestId("mark-change").click();
  const dialog = page.getByRole("dialog", { name: "Mark a change" });
  await dialog.getByLabel("What changed?").fill("Replaced the print server");
  await expect(dialog.getByLabel("Applies to")).toHaveValue(problem.id);
  await dialog.getByRole("button", { name: "Mark change" }).click();

  const row = page.getByTestId("change-row").filter({ hasText: "Replaced the print server" });
  await expect(row).toBeVisible();
  await expect(row).toContainText("this problem");
  // Marked just now: there is no "after" yet.
  await expect(row).toContainText(/Too soon to compare|Before/);

  // It shows on the dashboard as well, and non-admins cannot delete it.
  await page.goto("/dashboard");
  await expect(page.getByTestId("change-row").filter({ hasText: "Replaced the print server" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Delete change Replaced the print server" })).toHaveCount(0);
});

test("administrators manage webhooks and see the audit log", async ({ page }) => {
  await actAsAdmin(page);
  await page.goto("/admin");
  await page.getByRole("tab", { name: "Webhooks" }).click();

  // An inbound JSON webhook: the token is shown once, and works.
  const name = unique("CI pipeline");
  const inbound = page.locator("form").filter({ hasText: "Add inbound webhook" });
  await inbound.getByLabel("Name").fill(name);
  await inbound.getByRole("button", { name: "Add inbound webhook" }).click();
  const tokenDialog = page.getByRole("dialog", { name: `Token for “${name}”` });
  const token = (await tokenDialog.getByTestId("secret").textContent())!.trim();
  expect(token).toMatch(/^tbhook_/);
  await tokenDialog.getByRole("button", { name: "Done" }).click();
  await expect(page.getByText(name, { exact: true })).toBeVisible();

  const hooks = await (await page.request.get("/api/v1/inbound-hooks")).json();
  const hook = hooks.items.find((h: { name: string }) => h.name === name);
  const res = await page.request.post(`/api/v1/hooks/${hook.id}`, {
    headers: { Authorization: `Bearer ${token}`, "X-Client-Cert": "" },
    data: { action: "change", title: unique("Deployed from CI") },
  });
  expect(res.status(), await res.text()).toBe(201);

  // An outbound webhook.
  const out = page.locator("form").filter({ hasText: "Add webhook" });
  await out.getByLabel("Name").fill(unique("Platform channel"));
  await out.getByLabel("URL").fill("https://chat.example.com/hooks/abc");
  await out.getByRole("button", { name: "Add webhook" }).click();
  await expect(page.getByText("chat.example.com/hooks/abc")).toBeVisible();

  // Each of those administrative actions is in the audit log.
  await page.getByRole("tab", { name: "Audit log" }).click();
  await expect(page.getByText("POST /inbound-hooks").first()).toBeVisible();
  await expect(page.getByText("POST /webhooks").first()).toBeVisible();
  const csv = await page.request.get("/api/v1/audit/export");
  expect(csv.headers()["content-type"]).toContain("text/csv");
  expect(await csv.text()).toContain("e2e-admin,certificate,POST,/api/v1/inbound-hooks");
});
