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
import { createProblem, orgTurnbacks7d, pickProblem, problemCount, unique } from "./helpers";

test("records a turnback against an existing problem and sees updated dashboard statistics", async ({
  page,
}) => {
  const problem = await createProblem(page.request, unique("Corporate VPN disconnects"));
  const before = await orgTurnbacks7d(page.request);

  const started = Date.now();
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "What got in your way?" })).toBeVisible();
  await pickProblem(page, problem.title);
  await page.getByRole("radio", { name: "10 min" }).click();
  await page.getByTestId("submit-turnback").click();
  await expect(page.getByText(`Recorded 10 min · ${problem.title}`)).toBeVisible();
  const elapsed = Date.now() - started;
  expect(elapsed, "recording should take well under ten seconds").toBeLessThan(10_000);

  // The statistics on the page refresh immediately.
  await expect(page.getByTestId("stat-turnbacks")).toHaveText(String(before + 1));

  // And the dashboard reflects it.
  await page.getByRole("link", { name: "Dashboard" }).click();
  await page.getByRole("combobox", { name: "Reporting period" }).selectOption("last_7_days");
  await expect(page.getByTestId("metric-turnbacks")).toContainText(String(before + 1));
  await expect(page.getByTestId("top-problems")).toContainText(problem.title);
});

test("happened again records a repeat with one click", async ({ page }) => {
  const problem = await createProblem(page.request, unique("CI runner unavailable"));
  await page.goto("/");
  await pickProblem(page, problem.title);
  await page.keyboard.press("2"); // 5 min preset
  await page.keyboard.press("Enter");
  await expect(page.getByText(`Recorded 5 min · ${problem.title}`)).toBeVisible();
  const count = await problemCount(page.request, problem.id);
  await page.getByRole("button", { name: `Happened again: ${problem.title}, 5 min` }).click();
  await expect.poll(() => problemCount(page.request, problem.id)).toBe(count + 1);
});

test("creates a new problem from search, with duplicate suggestions", async ({ page }) => {
  const existing = await createProblem(page.request, unique("Internal DNS resolution failures"));
  await page.goto("/");
  const title = unique("Expense approval portal timeout");
  await page.getByRole("combobox", { name: "Search problems" }).fill(title);
  await page.getByRole("option", { name: /as a new problem/ }).click();

  const dialog = page.getByRole("dialog", { name: "New problem" });
  await expect(dialog.getByLabel("Title")).toHaveValue(title);
  // Similar-title suggestions appear while typing.
  await dialog.getByLabel("Title").fill("Internal DNS failures");
  await expect(dialog.getByText(existing.title)).toBeVisible();
  await dialog.getByLabel("Title").fill(title);
  await dialog.getByLabel("Category").selectOption({ label: "Processes" });
  await dialog.getByRole("button", { name: "Create problem" }).click();

  await expect(page.getByTestId("selected-problem")).toHaveText(title);
  await page.getByRole("radio", { name: "15 min" }).click();
  await page.getByTestId("submit-turnback").click();
  await expect(page.getByText(`Recorded 15 min · ${title}`)).toBeVisible();
});

test("records one problem several times with a quantity, and undo removes them all", async ({ page }) => {
  const problem = await createProblem(page.request, unique("VPN disconnected repeatedly"));
  const before = await problemCount(page.request, problem.id);

  await page.goto("/");
  await pickProblem(page, problem.title);
  await page.keyboard.press("3"); // 10 min
  const qty = page.getByLabel("How many times?");
  await qty.fill("15");
  await expect(page.getByTestId("quantity-total")).toHaveText("= 2 h 30 min in total");
  await page.getByRole("button", { name: "Record 15 turnbacks" }).click();
  await expect(page.getByText(`Recorded 15 × 10 min · ${problem.title}`)).toBeVisible();
  await expect.poll(() => problemCount(page.request, problem.id)).toBe(before + 15);

  await page.getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => problemCount(page.request, problem.id)).toBe(before);
});
