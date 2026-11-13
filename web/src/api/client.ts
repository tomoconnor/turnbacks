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

import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

/** Generated API schema types (from api/openapi.yaml). */
export type Schemas = components["schemas"];

// Absolute base URL (same origin) so the client also works where relative
// Request URLs are unsupported (e.g. tests).
export const api = createClient<paths>({
  baseUrl: `${globalThis.location?.origin ?? ""}/api/v1`,
  credentials: "same-origin",
});

type ErrorEnvelope = Schemas["Error"]["error"];

/** An error response from the API, carrying the server's stable error code. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fields: NonNullable<ErrorEnvelope["fields"]>;
  readonly requestId?: string;

  constructor(status: number, body?: Partial<ErrorEnvelope>) {
    super(body?.message ?? `Request failed (${status})`);
    this.name = "ApiError";
    this.status = status;
    this.code = body?.code ?? "unknown";
    this.fields = body?.fields ?? [];
    this.requestId = body?.request_id;
  }

  fieldError(field: string): string | undefined {
    return this.fields.find((f) => f.field === field)?.message;
  }
}

/** The request never reached the server (offline, DNS, VPN down...). */
export class NetworkError extends Error {
  constructor(cause?: unknown) {
    super("Network unavailable");
    this.name = "NetworkError";
    this.cause = cause;
  }
}

interface FetchResult<T> {
  data?: T;
  error?: unknown;
  response: Response;
}

/** Resolve an openapi-fetch call to its data, throwing ApiError/NetworkError. */
export async function unwrap<T>(call: Promise<FetchResult<T>>): Promise<T> {
  let res: FetchResult<T>;
  try {
    res = await call;
  } catch (e) {
    throw new NetworkError(e);
  }
  if (!res.response.ok) {
    const env = (res.error as { error?: ErrorEnvelope } | undefined)?.error;
    throw new ApiError(res.response.status, env);
  }
  return res.data as T;
}

/** Build a query string for plain (non-JSON) downloads such as CSV exports. */
export function downloadUrl(
  path: string,
  params: Record<string, string | number | undefined | null>,
): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== "") q.set(k, String(v));
  }
  const qs = q.toString();
  return `/api/v1${path}${qs ? `?${qs}` : ""}`;
}
