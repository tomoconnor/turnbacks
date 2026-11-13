-- name: AnalyticsSummary :one
SELECT
    count(*)::bigint AS turnbacks,
    coalesce(sum(t.duration_minutes), 0)::bigint AS minutes,
    coalesce(round(sum(t.duration_minutes::numeric * coalesce(tm.hourly_rate_minor, s.default_hourly_rate_minor)) / 60), 0)::bigint AS cost_minor,
    count(DISTINCT t.reporter)::bigint AS distinct_reporters,
    count(*) FILTER (WHERE t.reporter IS NULL)::bigint AS unattributed_turnbacks,
    count(*) FILTER (WHERE t.duration_estimated)::bigint AS estimated_turnbacks,
    count(DISTINCT t.problem_id)::bigint AS distinct_problems
FROM turnbacks t
JOIN problems p ON p.id = t.problem_id
LEFT JOIN teams tm ON tm.id = t.team_id
CROSS JOIN org_settings s
WHERE s.id = 1
  AND t.occurred_at >= @from_ts AND t.occurred_at < @to_ts
  AND (sqlc.narg('problem_id')::uuid IS NULL OR t.problem_id = sqlc.narg('problem_id'))
  AND (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id'))
  AND (sqlc.narg('team_id')::uuid IS NULL OR t.team_id = sqlc.narg('team_id'));

-- name: AnalyticsTimeseries :many
-- Buckets are calendar day/week/month boundaries in the reporting timezone.
WITH buckets AS (
    SELECT gs::timestamp AS bucket_start
    FROM generate_series(
        date_trunc(sqlc.arg(bucket)::text, timezone(sqlc.arg(tz)::text, sqlc.arg(from_ts)::timestamptz)),
        date_trunc(sqlc.arg(bucket)::text, timezone(sqlc.arg(tz)::text, sqlc.arg(to_ts)::timestamptz - interval '1 microsecond')),
        ('1 ' || sqlc.arg(bucket)::text)::interval) AS gs
),
agg AS (
    SELECT date_trunc(sqlc.arg(bucket)::text, timezone(sqlc.arg(tz)::text, t.occurred_at)) AS bucket_start,
           count(*) AS turnbacks,
           sum(t.duration_minutes) AS minutes,
           sum(t.duration_minutes::numeric * coalesce(tm.hourly_rate_minor, s.default_hourly_rate_minor)) AS cost_mm
    FROM turnbacks t
    JOIN problems p ON p.id = t.problem_id
    LEFT JOIN teams tm ON tm.id = t.team_id
    CROSS JOIN org_settings s
    WHERE s.id = 1
      AND t.occurred_at >= sqlc.arg(from_ts)::timestamptz AND t.occurred_at < sqlc.arg(to_ts)::timestamptz
      AND (sqlc.narg('problem_id')::uuid IS NULL OR t.problem_id = sqlc.narg('problem_id'))
      AND (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id'))
      AND (sqlc.narg('team_id')::uuid IS NULL OR t.team_id = sqlc.narg('team_id'))
    GROUP BY 1
)
SELECT b.bucket_start::date AS bucket_date,
       coalesce(a.turnbacks, 0)::bigint AS turnbacks,
       coalesce(a.minutes, 0)::bigint AS minutes,
       coalesce(round(a.cost_mm / 60), 0)::bigint AS cost_minor
FROM buckets b
LEFT JOIN agg a ON a.bucket_start = b.bucket_start
ORDER BY b.bucket_start;

-- name: AnalyticsProblemRanking :many
-- Ranked problems with Pareto cumulative share of the chosen metric.
WITH agg AS (
    SELECT p.id, p.title, p.alias, p.status, c.name AS category_name,
           count(*)::numeric AS turnbacks,
           sum(t.duration_minutes)::numeric AS minutes,
           sum(t.duration_minutes::numeric * coalesce(tm.hourly_rate_minor, s.default_hourly_rate_minor)) AS cost_mm,
           count(DISTINCT t.reporter) AS distinct_reporters
    FROM turnbacks t
    JOIN problems p ON p.id = t.problem_id
    JOIN categories c ON c.id = p.category_id
    LEFT JOIN teams tm ON tm.id = t.team_id
    CROSS JOIN org_settings s
    WHERE s.id = 1
      AND t.occurred_at >= @from_ts AND t.occurred_at < @to_ts
      AND (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id'))
      AND (sqlc.narg('team_id')::uuid IS NULL OR t.team_id = sqlc.narg('team_id'))
    GROUP BY p.id, c.name
),
ranked AS (
    SELECT agg.*,
           CASE @sort_by::text WHEN 'count' THEN turnbacks WHEN 'minutes' THEN minutes ELSE cost_mm END AS metric
    FROM agg
),
windowed AS (
    SELECT ranked.*,
           sum(metric) OVER (ORDER BY metric DESC, id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS cumulative_metric,
           sum(metric) OVER () AS total_metric,
           count(*) OVER () AS total_count
    FROM ranked
)
SELECT id, title, alias, status, category_name,
       turnbacks::bigint AS turnbacks,
       minutes::bigint AS minutes,
       round(cost_mm / 60)::bigint AS cost_minor,
       distinct_reporters::bigint AS distinct_reporters,
       CASE WHEN total_metric > 0 THEN round(metric * 10000 / total_metric)::int ELSE 0 END AS share_bp,
       CASE WHEN total_metric > 0 THEN round(cumulative_metric * 10000 / total_metric)::int ELSE 0 END AS cumulative_share_bp,
       total_count::bigint AS total_count
FROM windowed
ORDER BY metric DESC, id
LIMIT @lim OFFSET @off;

-- name: AnalyticsCategoryBreakdown :many
SELECT c.id, c.name,
       count(*)::bigint AS turnbacks,
       sum(t.duration_minutes)::bigint AS minutes,
       round(sum(t.duration_minutes::numeric * coalesce(tm.hourly_rate_minor, s.default_hourly_rate_minor)) / 60)::bigint AS cost_minor,
       count(DISTINCT t.problem_id)::bigint AS distinct_problems
FROM turnbacks t
JOIN problems p ON p.id = t.problem_id
JOIN categories c ON c.id = p.category_id
LEFT JOIN teams tm ON tm.id = t.team_id
CROSS JOIN org_settings s
WHERE s.id = 1
  AND t.occurred_at >= @from_ts AND t.occurred_at < @to_ts
  AND (sqlc.narg('team_id')::uuid IS NULL OR t.team_id = sqlc.narg('team_id'))
GROUP BY c.id, c.name
ORDER BY cost_minor DESC, c.name;

-- name: AnalyticsTeamBreakdown :many
-- Includes reporting participation so teams are not compared on raw totals alone.
SELECT tm.id AS team_id, tm.name AS team_name,
       count(*)::bigint AS turnbacks,
       sum(t.duration_minutes)::bigint AS minutes,
       round(sum(t.duration_minutes::numeric * coalesce(tm.hourly_rate_minor, s.default_hourly_rate_minor)) / 60)::bigint AS cost_minor,
       count(DISTINCT t.reporter)::bigint AS distinct_reporters,
       tm.headcount
FROM turnbacks t
JOIN problems p ON p.id = t.problem_id
LEFT JOIN teams tm ON tm.id = t.team_id
CROSS JOIN org_settings s
WHERE s.id = 1
  AND t.occurred_at >= @from_ts AND t.occurred_at < @to_ts
  AND (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id'))
GROUP BY tm.id, tm.name, tm.headcount
ORDER BY cost_minor DESC, tm.name NULLS LAST;

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
