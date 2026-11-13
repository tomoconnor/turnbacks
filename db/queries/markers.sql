-- name: CreateMarker :one
INSERT INTO change_markers (occurred_at, title, description, url, problem_id, source, created_by, is_demo)
VALUES (@occurred_at, @title, @description, sqlc.narg('url'), sqlc.narg('problem_id'), @source, sqlc.narg('created_by'), @is_demo)
RETURNING *;

-- name: GetMarker :one
SELECT sqlc.embed(m), p.title AS problem_title
FROM change_markers m LEFT JOIN problems p ON p.id = m.problem_id
WHERE m.id = @id;

-- name: UpdateMarker :one
UPDATE change_markers SET occurred_at = @occurred_at, title = @title, description = @description,
    url = sqlc.narg('url'), problem_id = sqlc.narg('problem_id')
WHERE id = @id
RETURNING *;

-- name: DeleteMarker :execrows
DELETE FROM change_markers WHERE id = @id;

-- name: ListMarkers :many
-- With problem_id: that problem's markers, plus organisation-wide markers when
-- include_global is true. Without: every marker.
SELECT sqlc.embed(m), p.title AS problem_title
FROM change_markers m LEFT JOIN problems p ON p.id = m.problem_id
WHERE (sqlc.narg('from_ts')::timestamptz IS NULL OR m.occurred_at >= sqlc.narg('from_ts'))
  AND (sqlc.narg('to_ts')::timestamptz IS NULL OR m.occurred_at < sqlc.narg('to_ts'))
  AND (sqlc.narg('problem_id')::uuid IS NULL
       OR m.problem_id = sqlc.narg('problem_id')
       OR (@include_global::boolean AND m.problem_id IS NULL))
ORDER BY m.occurred_at DESC, m.created_at DESC
LIMIT @lim;

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
