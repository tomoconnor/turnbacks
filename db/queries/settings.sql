-- name: GetSettings :one
SELECT * FROM org_settings WHERE id = 1;

-- name: UpdateSettings :one
UPDATE org_settings SET
    org_name = @org_name,
    currency = @currency,
    default_hourly_rate_minor = @default_hourly_rate_minor,
    reporting_timezone = @reporting_timezone,
    recoverable_pct = @recoverable_pct,
    large_duration_minutes = @large_duration_minutes,
    note_retention_days = sqlc.narg('note_retention_days'),
    reporter_retention_days = sqlc.narg('reporter_retention_days'),
    updated_at = now()
WHERE id = 1
RETURNING *;

-- name: DemoDataPresent :one
SELECT (EXISTS (SELECT 1 FROM problems WHERE is_demo) OR EXISTS (SELECT 1 FROM turnbacks WHERE is_demo))::boolean AS present;

-- Copyright (c) 2026 Tom O'Connor <tom@twinhelix.org>
--
-- Permission is hereby granted, free of charge, to any person obtaining a copy
-- of this software and associated documentation files (the "Software"), to deal
-- in the Software without restriction, including without limitation the rights
-- to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
-- copies of the Software, and to permit persons to whom the Software is
-- furnished to do so, subject to the following conditions:
--
-- The above copyright notice and this permission notice shall be included in
-- all copies or substantial portions of the Software.
--
-- THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
-- IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
-- FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
-- AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
-- LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
-- OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
-- SOFTWARE.
