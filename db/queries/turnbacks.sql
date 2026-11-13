-- name: InsertTurnback :one
INSERT INTO turnbacks (id, problem_id, reporter, team_id, duration_minutes, duration_estimated,
                       note, occurred_at, source, is_demo, batch_id, undo_token_hash, submitted_at, updated_at)
VALUES (@id, @problem_id, sqlc.narg('reporter'), sqlc.narg('team_id'), @duration_minutes, @duration_estimated,
        sqlc.narg('note'), @occurred_at, @source, @is_demo, sqlc.narg('batch_id'), sqlc.narg('undo_token_hash'),
        @submitted_at, @submitted_at)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: GetTurnback :one
SELECT sqlc.embed(t), p.title AS problem_title
FROM turnbacks t JOIN problems p ON p.id = t.problem_id
WHERE t.id = @id;

-- name: UpdateTurnback :one
UPDATE turnbacks SET
    problem_id = @problem_id,
    duration_minutes = @duration_minutes,
    duration_estimated = @duration_estimated,
    note = sqlc.narg('note'),
    occurred_at = @occurred_at,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteTurnback :execrows
DELETE FROM turnbacks WHERE id = @id;

-- name: DeleteTurnbackBatch :execrows
DELETE FROM turnbacks WHERE batch_id = @batch_id;

-- name: CountTurnbackBatch :one
SELECT count(*)::bigint FROM turnbacks WHERE batch_id = @batch_id;

-- name: ListTurnbacks :many
-- The reporter is returned for the administrators' audit view only; the
-- service blanks it for everyone else.
SELECT t.id, t.problem_id, p.title AS problem_title, t.team_id, tm.name AS team_name,
       t.duration_minutes, t.duration_estimated, t.note, t.occurred_at, t.submitted_at,
       t.source, t.is_demo, t.batch_id, t.reporter,
       count(*) OVER () AS total_count
FROM turnbacks t
JOIN problems p ON p.id = t.problem_id
LEFT JOIN teams tm ON tm.id = t.team_id
WHERE t.occurred_at >= @from_ts AND t.occurred_at < @to_ts
  AND (sqlc.narg('problem_id')::uuid IS NULL OR t.problem_id = sqlc.narg('problem_id'))
  AND (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id'))
  AND (sqlc.narg('team_id')::uuid IS NULL OR t.team_id = sqlc.narg('team_id'))
  AND (sqlc.narg('reporter')::text IS NULL OR t.reporter = sqlc.narg('reporter'))
ORDER BY t.occurred_at DESC, t.id
LIMIT @lim OFFSET @off;

-- name: ListProblemNotes :many
SELECT t.note::text AS note, t.occurred_at, t.duration_minutes
FROM turnbacks t
WHERE t.problem_id = @problem_id AND t.note IS NOT NULL AND btrim(t.note) <> ''
  AND t.occurred_at >= @from_ts AND t.occurred_at < @to_ts
ORDER BY t.occurred_at DESC
LIMIT @lim;

-- name: ApplyNoteRetention :execrows
UPDATE turnbacks SET note = NULL
WHERE note IS NOT NULL AND occurred_at < now() - make_interval(days => @days::int);

-- name: ApplyReporterRetention :execrows
UPDATE turnbacks SET reporter = NULL
WHERE reporter IS NOT NULL AND occurred_at < now() - make_interval(days => @days::int);

-- name: FirstOccurrenceForProblem :one
SELECT min(occurred_at) AS first_occurred_at FROM turnbacks WHERE problem_id = @problem_id;

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
