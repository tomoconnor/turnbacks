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

// Presentation helpers. All arithmetic on money happens on the server; these
// functions only render integer minor units.

export function minorToMajor(minor: number, exponent: number): number {
  return minor / 10 ** exponent;
}

export function majorToMinor(major: number, exponent: number): number {
  return Math.round(major * 10 ** exponent);
}

export function formatMoney(
  minor: number,
  currency: string,
  exponent: number,
  opts: { compact?: boolean; whole?: boolean } = {},
): string {
  const value = minorToMajor(minor, exponent);
  try {
    return new Intl.NumberFormat("en-GB", {
      style: "currency",
      currency,
      notation: opts.compact ? "compact" : "standard",
      maximumFractionDigits: opts.compact ? 1 : opts.whole ? 0 : exponent,
      minimumFractionDigits: opts.compact || opts.whole ? 0 : exponent,
    }).format(value);
  } catch {
    return `${currency} ${value.toFixed(exponent)}`;
  }
}

export function formatHours(minutes: number): string {
  const h = minutes / 60;
  return `${h.toLocaleString("en-GB", { maximumFractionDigits: h < 10 ? 1 : 0 })} h`;
}

export function formatMinutes(minutes: number): string {
  if (minutes < 60) return `${minutes} min`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m === 0 ? `${h} h` : `${h} h ${m} min`;
}

export function formatCount(n: number): string {
  return n.toLocaleString("en-GB");
}

/** Basis points (1/100 %) to a signed percentage string. */
export function formatChange(bp: number | null | undefined): string {
  if (bp === null || bp === undefined) return "n/a";
  const pct = bp / 100;
  const sign = pct > 0 ? "+" : "";
  return `${sign}${pct.toLocaleString("en-GB", { maximumFractionDigits: Math.abs(pct) < 10 ? 1 : 0 })}%`;
}

export function formatShare(bp: number): string {
  return `${(bp / 100).toLocaleString("en-GB", { maximumFractionDigits: 1 })}%`;
}

export function formatDate(iso: string): string {
  // Calendar dates (YYYY-MM-DD) are rendered without timezone shifting.
  const [y, m, d] = iso.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  });
}

export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("en-GB", { dateStyle: "medium", timeStyle: "short" });
}

export function formatRelative(iso: string, now = Date.now()): string {
  const diff = (now - new Date(iso).getTime()) / 1000;
  if (diff < 60) return "just now";
  if (diff < 3600) return `${Math.floor(diff / 60)} min ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} h ago`;
  if (diff < 7 * 86400) return `${Math.floor(diff / 86400)} d ago`;
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}
