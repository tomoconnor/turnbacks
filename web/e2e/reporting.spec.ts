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
import { actAsAdmin, actAsEveryone, adminCertHeader, createProblem, unique } from "./helpers";

test("anyone can generate a business case and adjust assumptions", async ({ page }) => {
  const problem = await createProblem(page.request, unique("Dev environment provisioning delays"));
  for (const minutes of [30, 60, 30, 15]) {
    const res = await page.request.post("/api/v1/turnbacks", {
      data: { id: crypto.randomUUID(), problem_id: problem.id, duration_minutes: minutes, source: "api" },
    });
    expect(res.status()).toBe(201);
  }

  await page.goto(`/problems/${problem.id}`);
  await page.getByRole("link", { name: "Business case" }).click();
  const bc = page.getByTestId("business-case");
  await expect(bc.getByRole("heading", { name: problem.title })).toBeVisible();
  await expect(bc.getByText("2.3 h")).toBeVisible(); // 135 minutes
  await expect(bc.getByText("Methodology")).toBeVisible();
  await expect(page.getByTestId("payback")).toHaveText("0 months");
  await page.getByLabel("One-off cost (GBP)").fill("5000");
  await expect(page.getByTestId("payback")).not.toHaveText("0 months");
  await expect(bc.getByText("assumptions: custom")).toBeVisible();

  const csv = await page.request.get(`/api/v1/problems/${problem.id}/business-case/export`);
  expect(csv.headers()["content-type"]).toContain("text/csv");
  expect(await csv.text()).toContain("reported_occurrences,4");
});

test("administration requires a client certificate; reporter addresses are admin-only", async ({ page }) => {
  const problem = await createProblem(page.request, unique("Access request backlog"));
  const sub = await page.request.post("/api/v1/turnbacks", {
    data: { id: crypto.randomUUID(), problem_id: problem.id, duration_minutes: 5 },
  });
  expect(sub.status()).toBe(201);

  // Without a certificate: admin operations are refused and addresses hidden.
  expect((await page.request.put("/api/v1/settings", { data: {} })).status()).toBe(403);
  const pub = await (
    await page.request.get(`/api/v1/turnbacks?problem_id=${problem.id}&period=last_7_days`)
  ).json();
  expect(pub.items[0].reporter).toBeUndefined();
  await page.goto(`/problems/${problem.id}`);
  await expect(page.getByRole("button", { name: "Merge" })).toHaveCount(0);
  await page.goto("/admin");
  await expect(page.getByText("Administration needs an admin client certificate")).toBeVisible();

  // A corrupted certificate is not an admin certificate.
  const bad = adminCertHeader().slice(0, -12) + "AAAAAAAAAAAA";
  const forged = await page.request.put("/api/v1/settings", { data: {}, headers: { "X-Client-Cert": bad } });
  expect(forged.status()).toBe(403);

  // With the certificate (as forwarded by the TLS proxy).
  await actAsAdmin(page);
  await page.reload();
  await expect(page.getByRole("heading", { name: "Administration" })).toBeVisible();
  await expect(page.getByText("Admin · e2e-admin")).toBeVisible();
  await expect(page.getByTestId("audit-rows")).toContainText(/ip:(127\.0\.0\.1|::1)/);

  await page.goto(`/problems/${problem.id}`);
  await expect(page.getByRole("button", { name: "Merge" })).toBeVisible();

  // No certificate, no admin rights.
  await actAsEveryone(page);
  await page.reload();
  await expect(page.getByRole("button", { name: "Merge" })).toHaveCount(0);
  expect((await page.request.put("/api/v1/settings", { data: {} })).status()).toBe(403);
});
