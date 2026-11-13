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

import { z } from "zod";
import type { Schemas } from "@/api/client";

// Client-side validation mirrors the OpenAPI contract for fast feedback. The
// server remains the authority and re-validates everything.

export const durationSchema = z
  .number({ error: "Enter a number of minutes" })
  .int("Use whole minutes")
  .min(1, "At least 1 minute")
  .max(1440, "At most 1440 minutes (24 hours)");

export const quantitySchema = z
  .number({ error: "Enter how many times" })
  .int("Use a whole number")
  .min(1, "At least once")
  .max(100, "At most 100 at a time");

export const noteSchema = z.string().max(1000, "Notes are limited to 1000 characters");

export const aliasSchema = z
  .string()
  .regex(/^[a-z0-9][a-z0-9-]{0,31}$/, "Lowercase letters, digits and hyphens only (max 32)");

export const problemInputSchema = z.object({
  title: z.string().trim().min(3, "At least 3 characters").max(120, "At most 120 characters"),
  description: z.string().trim().max(4000).optional(),
  category_id: z.uuid("Choose a category"),
  alias: aliasSchema.nullable().optional(),
  external_url: z
    .url({ protocol: /^https?$/, error: "Must be an http(s) URL" })
    .nullable()
    .optional(),
  status: z.enum(["active", "monitoring", "resolved", "archived"]).optional(),
}) satisfies z.ZodType<Schemas["ProblemInput"]>;

export const remediationInputSchema = z.object({
  implemented_on: z.iso.date("Choose a date"),
  description: z.string().trim().min(1, "Describe the remediation").max(4000),
  implementation_cost_minor: z.number().int().min(0),
  annual_cost_minor: z.number().int().min(0),
  expected_reduction_pct: z.number().int().min(0).max(100),
}) satisfies z.ZodType<Schemas["RemediationInput"]>;

/** Map zod issues to a field → message record. */
export function fieldErrors(err: z.ZodError): Record<string, string> {
  const out: Record<string, string> = {};
  for (const i of err.issues) {
    const k = String(i.path[0] ?? "form");
    if (!out[k]) out[k] = i.message;
  }
  return out;
}
