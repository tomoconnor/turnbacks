-- name: CreateRemediation :one
INSERT INTO remediations (problem_id, implemented_on, description, implementation_cost_minor,
                          annual_cost_minor, expected_reduction_pct, is_demo)
VALUES (@problem_id, @implemented_on, @description, @implementation_cost_minor,
        @annual_cost_minor, @expected_reduction_pct, @is_demo)
RETURNING *;

-- name: GetRemediation :one
SELECT * FROM remediations WHERE id = @id;

-- name: UpdateRemediation :one
UPDATE remediations SET implemented_on = @implemented_on, description = @description,
    implementation_cost_minor = @implementation_cost_minor, annual_cost_minor = @annual_cost_minor,
    expected_reduction_pct = @expected_reduction_pct
WHERE id = @id
RETURNING *;

-- name: DeleteRemediation :execrows
DELETE FROM remediations WHERE id = @id;

-- name: ListRemediationsForProblem :many
SELECT * FROM remediations WHERE problem_id = @problem_id ORDER BY implemented_on DESC, created_at DESC;

-- name: ListAllRemediations :many
SELECT sqlc.embed(r), p.title AS problem_title
FROM remediations r JOIN problems p ON p.id = r.problem_id
ORDER BY r.implemented_on DESC, r.created_at DESC
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
