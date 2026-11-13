-- Full-data exports, streamed in keyset-paginated pages.

-- name: ExportTurnbacks :many
SELECT t.id, t.occurred_at, t.submitted_at, t.problem_id, p.title AS problem_title, c.name AS category_name,
       tm.name AS team_name, t.duration_minutes, t.duration_estimated, t.source, t.batch_id, t.note,
       t.reporter, t.is_demo
FROM turnbacks t
JOIN problems p ON p.id = t.problem_id
JOIN categories c ON c.id = p.category_id
LEFT JOIN teams tm ON tm.id = t.team_id
WHERE (t.occurred_at, t.id) > (@after_occurred_at::timestamptz, @after_id::uuid)
ORDER BY t.occurred_at, t.id
LIMIT @lim;

-- name: ExportProblems :many
SELECT p.id, p.title, p.description, c.name AS category_name, p.status, p.alias, p.external_url,
       p.merged_into_id, p.created_at, p.updated_at, p.resolved_at, p.is_demo,
       (SELECT count(*) FROM turnbacks t WHERE t.problem_id = p.id)::bigint AS turnbacks_all_time
FROM problems p JOIN categories c ON c.id = p.category_id
ORDER BY p.created_at, p.id;

-- name: ExportRemediations :many
SELECT sqlc.embed(r), p.title AS problem_title
FROM remediations r JOIN problems p ON p.id = r.problem_id
ORDER BY r.implemented_on, r.created_at;

-- name: ExportMarkers :many
SELECT sqlc.embed(m), p.title AS problem_title
FROM change_markers m LEFT JOIN problems p ON p.id = m.problem_id
ORDER BY m.occurred_at, m.created_at;

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
