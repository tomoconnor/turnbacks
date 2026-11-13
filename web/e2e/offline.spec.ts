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
import { createProblem, pickProblem, problemCount, unique } from "./helpers";

test("offline submission is queued and synchronised when connectivity returns", async ({ page, context }) => {
  const problem = await createProblem(page.request, unique("VPN drops while offline"));
  const before = await problemCount(page.request, problem.id);

  await page.goto("/");
  await pickProblem(page, problem.title);
  await page.getByRole("radio", { name: "30 min" }).click();

  await context.setOffline(true);
  const recordedAt = new Date();
  await page.getByTestId("submit-turnback").click();
  await expect(page.getByText(`Saved on this device · ${problem.title}`)).toBeVisible();
  await expect(page.getByTestId("pending-count")).toContainText("1 unsent");

  // Still reloadable offline: the service worker serves the app shell and the
  // queued report survives in IndexedDB.
  await page.reload();
  await expect(page.getByTestId("pending-count")).toContainText("1 unsent");

  await context.setOffline(false);
  await expect(page.getByTestId("pending-count")).toBeHidden({ timeout: 15_000 });
  await expect.poll(() => problemCount(page.request, problem.id)).toBe(before + 1);

  const list = await (
    await page.request.get(`/api/v1/turnbacks?period=last_7_days&problem_id=${problem.id}&limit=1`)
  ).json();
  const t = list.items[0];
  expect(t.problem_title).toBe(problem.title);
  expect(t.source).toBe("pwa_offline");
  // Original occurrence time preserved (not the sync time).
  expect(Math.abs(new Date(t.occurred_at).getTime() - recordedAt.getTime())).toBeLessThan(5_000);
});

test.describe("lost responses", () => {
  // page.route cannot intercept requests that pass through a service worker.
  test.use({ serviceWorkers: "block" });

  test("a retried submission whose first response was lost is not duplicated", async ({ page }) => {
    const problem = await createProblem(page.request, unique("Flaky CI cache"));
    const before = await problemCount(page.request, problem.id);

    // The first POST reaches the server and is stored, but the response is
    // dropped (as when a VPN flaps mid-request). The client queues and retries.
    let posts = 0;
    await page.route("**/api/v1/turnbacks", async (route) => {
      if (route.request().method() !== "POST") return route.continue();
      posts++;
      if (posts === 1) {
        await route.fetch();
        return route.abort("connectionreset");
      }
      return route.continue();
    });

    await page.goto("/");
    await pickProblem(page, problem.title);
    await page.getByRole("radio", { name: "10 min" }).click();
    await page.getByTestId("submit-turnback").click();
    await expect(page.getByText(`Saved on this device · ${problem.title}`)).toBeVisible();

    await page.getByTestId("pending-count").click();
    await page.getByTestId("retry-all").click();
    await expect(page.getByTestId("outbox-empty")).toBeVisible();
    expect(posts).toBe(2);
    await expect.poll(() => problemCount(page.request, problem.id)).toBe(before + 1);
  });
});
