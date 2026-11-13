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

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { toast, Toaster } from "sonner";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SessionProvider } from "@/lib/session";
import { db, resetDbForTests } from "@/offline/db";
import { outbox } from "@/offline/outbox";
import { json, mockApi, settings, team, vpn } from "@/test/server";
import { buildPayload, RecordPage } from "./RecordPage";

function setup(extra: Parameters<typeof mockApi>[0] = {}) {
  const calls = mockApi({
    "GET /session": () => json(200, { admin: false, admin_enabled: true, slack_enabled: false }),
    "GET /teams": () => json(200, { items: [team] }),
    "GET /settings": () => json(200, settings),
    "GET /problems/quick": () => json(200, { frequent: [vpn], recent: [] }),
    "GET /categories": () => json(200, { items: [] }),
    "GET /problems": () => json(200, { items: [], total: 0, limit: 8, offset: 0 }),
    "GET /analytics/summary": () =>
      json(200, {
        current: {
          turnbacks: 5,
          minutes: 50,
          cost_minor: 0,
          distinct_reporters: 1,
          unattributed_turnbacks: 0,
          estimated_turnbacks: 5,
          distinct_problems: 1,
          avg_minutes: 10,
        },
      }),
    "POST /turnbacks": (_req, body) => {
      const b = body as { id: string; duration_minutes: number; quantity?: number };
      return json(201, {
        replayed: false,
        quantity: b.quantity ?? 1,
        undo_token: "undo-token-1",
        turnback: {
          ...b,
          problem_title: vpn.title,
          team_id: null,
          duration_estimated: true,
          note: null,
          submitted_at: "",
          source: "web",
          is_demo: false,
        },
      });
    },
    ...extra,
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <SessionProvider>
          <RecordPage />
          <Toaster />
        </SessionProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return calls;
}

beforeEach(async () => {
  // sonner keeps toasts in a module-level store; start each test clean.
  toast.dismiss();
  await new Promise((r) => setTimeout(r, 450));
  localStorage.clear();
  const d = await db();
  await d.clear("outbox");
  d.close();
  resetDbForTests();
  Object.defineProperty(navigator, "onLine", { value: true, configurable: true });
});

describe("RecordPage", () => {
  it("records a turnback with a quick problem and a preset", async () => {
    const user = userEvent.setup();
    const calls = setup();
    await user.click(await screen.findByRole("button", { name: /Corporate VPN disconnects/ }));
    await user.click(screen.getByRole("radio", { name: "10 min" }));
    await user.click(screen.getByRole("button", { name: /More options/ }));
    await user.type(screen.getByLabelText(/Note/), "dropped again");
    await user.click(screen.getByTestId("submit-turnback"));

    await screen.findByText(/Recorded 10 min · Corporate VPN disconnects/);
    const post = calls.find((c) => c.method === "POST" && c.path === "/api/v1/turnbacks");
    expect(post?.body).toMatchObject({
      problem_id: vpn.id,
      duration_minutes: 10,
      note: "dropped again",
      source: "web",
      team_id: null,
    });
    expect((post?.body as { id: string }).id).toMatch(/^[0-9a-f-]{36}$/);
    // Form reset and "Happened again" now offered.
    expect(screen.queryByTestId("selected-problem")).not.toBeInTheDocument();
    expect(
      await screen.findByRole("button", { name: /Happened again: Corporate VPN disconnects, 10 min/ }),
    ).toBeInTheDocument();
  });

  it("supports keyboard-only recording", async () => {
    const user = userEvent.setup();
    const calls = setup();
    await screen.findByRole("button", { name: /Corporate VPN disconnects/ });
    await user.keyboard("1");
    expect(screen.getByTestId("selected-problem")).toHaveTextContent(vpn.title);
    await user.keyboard("4"); // 15 min preset
    await user.keyboard("{Enter}");
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    expect(calls.find((c) => c.method === "POST")?.body).toMatchObject({ duration_minutes: 15 });
  });

  it("asks for confirmation of unusually long durations", async () => {
    const user = userEvent.setup();
    const calls = setup();
    await user.click(await screen.findByRole("button", { name: /Corporate VPN disconnects/ }));
    await user.type(screen.getByLabelText("Custom minutes"), "300");
    await user.click(screen.getByTestId("submit-turnback"));
    expect(await screen.findByText(/Is that right\?/)).toBeInTheDocument();
    expect(calls.some((c) => c.method === "POST")).toBe(false);
    await user.click(screen.getByRole("button", { name: "Yes, record it" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST")?.body).toMatchObject({
        duration_minutes: 300,
        confirm_large_duration: true,
      }),
    );
  });

  it("records the same problem several times with a quantity", async () => {
    const user = userEvent.setup();
    const calls = setup();
    // Duration first, then the problem: either order works.
    await user.click(await screen.findByRole("radio", { name: "10 min" }));
    await user.click(screen.getByRole("button", { name: /Corporate VPN disconnects/ }));
    const qty = screen.getByLabelText("How many times?");
    await user.clear(qty);
    await user.type(qty, "15");
    expect(screen.getByTestId("quantity-total")).toHaveTextContent("= 2 h 30 min in total");
    await user.click(screen.getByRole("button", { name: "Record 15 turnbacks" }));
    await screen.findByText(/Recorded 15 × 10 min · Corporate VPN disconnects/);
    expect(calls.find((c) => c.method === "POST")?.body).toMatchObject({
      duration_minutes: 10,
      quantity: 15,
    });
  });

  it("adjusts the quantity from the keyboard and confirms large quantities", async () => {
    const user = userEvent.setup();
    const calls = setup();
    await screen.findByRole("button", { name: /Corporate VPN disconnects/ });
    await user.keyboard("1"); // problem
    await user.keyboard("3"); // 10 min
    await user.keyboard("+++");
    expect(screen.getByLabelText("How many times?")).toHaveValue(4);
    await user.keyboard("-");
    expect(screen.getByLabelText("How many times?")).toHaveValue(3);

    const qty = screen.getByLabelText("How many times?");
    await user.clear(qty);
    await user.type(qty, "30");
    await user.click(screen.getByTestId("submit-turnback"));
    expect(await screen.findByText(/30 occurrences at once is a lot/)).toBeInTheDocument();
    expect(calls.some((c) => c.method === "POST")).toBe(false);
    await user.click(screen.getByRole("button", { name: "Yes, record it" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST")?.body).toMatchObject({
        quantity: 30,
        confirm_large_duration: true,
      }),
    );
  });

  it("remembers the chosen team and sends it with reports", async () => {
    const user = userEvent.setup();
    const calls = setup();
    await screen.findByRole("option", { name: team.name });
    await user.selectOptions(screen.getByLabelText("Your team"), team.name);
    expect(localStorage.getItem("turnbacks.team")).toBe(team.id);
    await user.click(screen.getByRole("button", { name: /Corporate VPN disconnects/ }));
    await user.click(screen.getByRole("radio", { name: "5 min" }));
    await user.click(screen.getByTestId("submit-turnback"));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST")?.body).toMatchObject({ team_id: team.id }),
    );
  });

  it("queues the report on the device when the server is unreachable", async () => {
    const user = userEvent.setup();
    setup({
      "POST /turnbacks": () => {
        throw new TypeError("Failed to fetch");
      },
    });
    await user.click(await screen.findByRole("button", { name: /Corporate VPN disconnects/ }));
    await user.click(screen.getByRole("radio", { name: "5 min" }));
    await user.click(screen.getByTestId("submit-turnback"));
    expect(await screen.findByText(/Saved on this device/)).toBeInTheDocument();
    expect(outbox.snapshot()).toHaveLength(1);
    expect(outbox.snapshot()[0]).toMatchObject({ status: "pending" });
  });

  it("shows server validation errors without losing the form", async () => {
    const user = userEvent.setup();
    setup({
      "POST /turnbacks": () =>
        json(422, { error: { code: "problem_archived", message: "this problem is archived" } }),
    });
    await user.click(await screen.findByRole("button", { name: /Corporate VPN disconnects/ }));
    await user.click(screen.getByRole("radio", { name: "5 min" }));
    await user.click(screen.getByTestId("submit-turnback"));
    expect(await screen.findByRole("alert")).toHaveTextContent("this problem is archived");
    expect(screen.getByTestId("selected-problem")).toBeInTheDocument();
  });
});

describe("buildPayload", () => {
  it("fixes the occurrence time at record time", () => {
    const now = new Date("2026-03-01T12:00:00Z");
    const p = buildPayload({
      problemId: "p",
      minutes: 5,
      estimated: true,
      note: "  ",
      teamId: "t1",
      when: "60",
      confirmLarge: false,
      now,
    });
    expect(p.occurred_at).toBe("2026-03-01T11:00:00.000Z");
    expect(p.note).toBeNull();
    expect(p.team_id).toBe("t1");
  });
});

describe("undo", () => {
  it("offers Undo briefly, then the confirmation disappears after 5 seconds", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      setup();
      await user.click(await screen.findByRole("button", { name: /Corporate VPN disconnects/ }));
      await user.click(screen.getByRole("radio", { name: "5 min" }));
      await user.click(screen.getByTestId("submit-turnback"));
      expect(await screen.findByRole("button", { name: "Undo" })).toBeInTheDocument();
      await act(() => vi.advanceTimersByTimeAsync(4000));
      expect(screen.getByRole("button", { name: "Undo" })).toBeInTheDocument();
      await act(() => vi.advanceTimersByTimeAsync(2000));
      await waitFor(() => expect(screen.queryByRole("button", { name: "Undo" })).not.toBeInTheDocument());
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("shortcuts", () => {
  it("N opens the new-problem dialog and presets show their keys", async () => {
    const user = userEvent.setup();
    setup();
    await screen.findByRole("button", { name: /Corporate VPN disconnects/ });
    expect(screen.getByRole("radio", { name: "10 min" }).querySelector("kbd")).toHaveTextContent("3");
    await user.keyboard("n");
    expect(await screen.findByRole("dialog", { name: "New problem" })).toBeInTheDocument();
  });
});
