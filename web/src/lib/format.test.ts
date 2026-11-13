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

import { describe, expect, it } from "vitest";
import { formatChange, formatHours, formatMinutes, formatMoney, majorToMinor, minorToMajor } from "./format";

describe("format", () => {
  it("formats money from minor units", () => {
    expect(formatMoney(123456, "GBP", 2)).toBe("£1,234.56");
    expect(formatMoney(123456, "GBP", 2, { whole: true })).toBe("£1,235");
    expect(formatMoney(700, "JPY", 0)).toBe("JP¥700");
  });
  it("converts between major and minor units without float drift", () => {
    expect(majorToMinor(50.1, 2)).toBe(5010);
    expect(majorToMinor(0.29, 2)).toBe(29);
    expect(minorToMajor(5010, 2)).toBe(50.1);
  });
  it("formats durations", () => {
    expect(formatMinutes(10)).toBe("10 min");
    expect(formatMinutes(90)).toBe("1 h 30 min");
    expect(formatMinutes(120)).toBe("2 h");
    expect(formatHours(90)).toBe("1.5 h");
    expect(formatHours(6000)).toBe("100 h");
  });
  it("formats basis-point changes", () => {
    expect(formatChange(-2084)).toBe("-21%");
    expect(formatChange(-584)).toBe("-5.8%");
    expect(formatChange(1500)).toBe("+15%");
    expect(formatChange(null)).toBe("n/a");
    expect(formatChange(0)).toBe("0%");
  });
});
