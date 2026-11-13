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

// Device-local memory of recent reports, used for one-tap "Happened again".

export interface RecentEntry {
  problemId: string;
  title: string;
  minutes: number;
  at: string;
}

const KEY = "turnbacks.recent";

export function loadRecent(): RecentEntry[] {
  try {
    return JSON.parse(localStorage.getItem(KEY) ?? "[]") as RecentEntry[];
  } catch {
    return [];
  }
}

export function rememberRecent(e: Omit<RecentEntry, "at">) {
  const list = [
    { ...e, at: new Date().toISOString() },
    ...loadRecent().filter((r) => r.problemId !== e.problemId),
  ];
  try {
    localStorage.setItem(KEY, JSON.stringify(list.slice(0, 6)));
  } catch {
    /* ignore */
  }
}
