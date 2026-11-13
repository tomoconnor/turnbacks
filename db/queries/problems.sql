-- name: GetProblem :one
SELECT sqlc.embed(p), c.name AS category_name
FROM problems p JOIN categories c ON c.id = p.category_id
WHERE p.id = @id;

-- name: GetProblemForUpdate :one
SELECT * FROM problems WHERE id = @id FOR UPDATE;

-- name: GetProblemByAlias :one
SELECT * FROM problems WHERE alias = lower(@alias) AND merged_into_id IS NULL;

-- name: ListProblems :many
-- q is a pre-escaped ILIKE fragment; q_raw is the raw text for similarity ranking.
SELECT sqlc.embed(p), c.name AS category_name,
    st.recent_count, st.last_occurred_at,
    count(*) OVER () AS total_count
FROM problems p
JOIN categories c ON c.id = p.category_id
LEFT JOIN LATERAL (
    SELECT count(*) FILTER (WHERE t.occurred_at >= now() - interval '30 days') AS recent_count,
           max(t.occurred_at) AS last_occurred_at
    FROM turnbacks t WHERE t.problem_id = p.id
) st ON true
WHERE p.merged_into_id IS NULL
  AND p.status = ANY (@statuses::text[])
  AND (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id'))
  AND (sqlc.narg('q')::text IS NULL
       OR p.title ILIKE '%' || sqlc.narg('q') || '%'
       OR p.alias ILIKE sqlc.narg('q') || '%'
       OR p.description ILIKE '%' || sqlc.narg('q') || '%'
       OR word_similarity(lower(sqlc.narg('q_raw')::text), lower(p.title)) > 0.4)
ORDER BY
    (p.alias IS NOT NULL AND p.alias = lower(sqlc.narg('q_raw')::text)) DESC,
    word_similarity(lower(coalesce(sqlc.narg('q_raw')::text, '')), lower(p.title)) DESC,
    st.recent_count DESC NULLS LAST,
    lower(p.title),
    p.id
LIMIT @lim OFFSET @off;

-- name: FrequentProblems :many
SELECT p.id, p.title, p.alias, p.status, p.category_id, count(t.id)::bigint AS recent_count,
       max(t.occurred_at)::timestamptz AS last_occurred_at
FROM problems p
JOIN turnbacks t ON t.problem_id = p.id AND t.occurred_at >= @since
WHERE p.merged_into_id IS NULL AND p.status IN ('active', 'monitoring')
GROUP BY p.id
ORDER BY recent_count DESC, lower(p.title)
LIMIT @lim;

-- name: SimilarProblems :many
SELECT p.id, p.title, p.alias, p.status,
       greatest(similarity(lower(p.title), lower(@title)), word_similarity(lower(@title), lower(p.title)))::real AS score
FROM problems p
WHERE p.merged_into_id IS NULL
  AND p.status <> 'archived'
  AND (lower(p.title) % lower(@title) OR word_similarity(lower(@title), lower(p.title)) > 0.5)
ORDER BY score DESC, lower(p.title)
LIMIT 5;

-- name: CreateProblem :one
INSERT INTO problems (title, description, category_id, alias, external_url, is_demo)
VALUES (@title, @description, @category_id, sqlc.narg('alias'), sqlc.narg('external_url'), @is_demo)
RETURNING *;

-- name: UpdateProblem :one
UPDATE problems SET
    title = @title,
    description = @description,
    category_id = @category_id,
    status = @status,
    alias = sqlc.narg('alias'),
    external_url = sqlc.narg('external_url'),
    resolved_at = CASE
        WHEN @status = 'resolved' THEN coalesce(resolved_at, now())
        WHEN @status IN ('active', 'monitoring') THEN NULL
        ELSE resolved_at END,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ReassignTurnbacks :execrows
UPDATE turnbacks SET problem_id = @target_id, updated_at = now() WHERE problem_id = @source_id;

-- name: ReassignRemediations :execrows
UPDATE remediations SET problem_id = @target_id WHERE problem_id = @source_id;

-- name: RepointMergedProblems :exec
UPDATE problems SET merged_into_id = @target_id, updated_at = now() WHERE merged_into_id = @source_id;

-- name: MarkProblemMerged :exec
UPDATE problems SET merged_into_id = @target_id, status = 'archived', alias = NULL, updated_at = now()
WHERE id = @source_id;

-- name: SetProblemAliasIfEmpty :exec
UPDATE problems SET alias = @alias, updated_at = now() WHERE id = @id AND alias IS NULL;

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
