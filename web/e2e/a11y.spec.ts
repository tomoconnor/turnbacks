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

import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import { actAsAdmin, createProblem, unique } from "./helpers";

// Automated WCAG 2.1 A/AA checks of the main screens. Automated tools catch a
// subset of issues; see docs/accessibility.md for the manual review.
for (const colorScheme of ["light", "dark"] as const) {
  test.describe(`${colorScheme} theme`, () => {
    test.use({ colorScheme });

    test("key pages have no detectable WCAG A/AA violations", async ({ page }) => {
      const problem = await createProblem(page.request, unique("Accessibility check problem"));
      await page.request.post("/api/v1/turnbacks", {
        data: { id: crypto.randomUUID(), problem_id: problem.id, duration_minutes: 10 },
      });

      const pages: [string, string][] = [
        ["/", "What got in your way?"],
        ["/dashboard", "Dashboard"],
        ["/problems", "Problems"],
        [`/problems/${problem.id}`, problem.title],
        [`/problems/${problem.id}/business-case`, problem.title],
        ["/admin", "Administration needs an admin client certificate"],
      ];
      for (const [path, heading] of pages) {
        await page.goto(path);
        await expect(page.getByText(heading).first()).toBeVisible();
        await page.waitForLoadState("networkidle");
        const results = await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
          .analyze();
        const summary = results.violations.map(
          (v) =>
            `${v.id} (${v.impact}): ${v.nodes
              .map((n) => n.target.join(" "))
              .slice(0, 3)
              .join(", ")}`,
        );
        expect(summary, `violations on ${path}`).toEqual([]);
      }

      // The administration screens, with an admin certificate.
      await actAsAdmin(page);
      await page.goto("/admin");
      await expect(page.getByRole("heading", { name: "Administration" })).toBeVisible();
      await page.waitForLoadState("networkidle");
      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();
      expect(
        results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`),
      ).toEqual([]);

      // The webhooks and audit log tabs.
      for (const tab of ["Webhooks", "Audit log"]) {
        await page.getByRole("tab", { name: tab }).click();
        await page.waitForLoadState("networkidle");
        const r = await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
          .analyze();
        expect(
          r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`),
          `violations on the ${tab} tab`,
        ).toEqual([]);
      }
    });
  });
}

test("the theme button cycles System, Light and Dark and is remembered", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/");
  const html = page.locator("html");
  const toggle = page.getByTestId("theme-toggle");
  await expect(toggle).toHaveAccessibleName(/Theme: System/);
  await expect(html).not.toHaveClass(/dark/);

  await toggle.click(); // → Light
  await expect(toggle).toHaveAccessibleName(/Theme: Light/);
  await toggle.click(); // → Dark
  await expect(html).toHaveClass(/dark/);

  await page.reload();
  await expect(html).toHaveClass(/dark/);
  await expect(page.getByTestId("theme-toggle")).toHaveAccessibleName(/Theme: Dark/);

  await page.getByTestId("theme-toggle").click(); // → System (light here)
  await expect(html).not.toHaveClass(/dark/);
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(html).toHaveClass(/dark/);
});
