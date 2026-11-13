-- name: ListCategories :many
SELECT * FROM categories
WHERE (sqlc.narg('active')::boolean IS NULL OR active = sqlc.narg('active'))
ORDER BY sort_order, lower(name);

-- name: GetCategory :one
SELECT * FROM categories WHERE id = @id;

-- name: CreateCategory :one
INSERT INTO categories (name, description, sort_order)
VALUES (@name, @description, @sort_order)
RETURNING *;

-- name: UpdateCategory :one
UPDATE categories SET name = @name, description = @description, active = @active, sort_order = @sort_order
WHERE id = @id
RETURNING *;

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
