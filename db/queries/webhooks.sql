-- name: CreateWebhook :one
INSERT INTO webhooks (name, url, format, events, secret, enabled)
VALUES (@name, @url, @format, @events, @secret, @enabled)
RETURNING *;

-- name: GetWebhook :one
SELECT * FROM webhooks WHERE id = @id;

-- name: ListWebhooks :many
SELECT * FROM webhooks ORDER BY lower(name);

-- name: LatestDeliveries :many
-- The most recent delivery of each webhook.
SELECT DISTINCT ON (webhook_id) * FROM webhook_deliveries ORDER BY webhook_id, created_at DESC;

-- name: UpdateWebhook :one
UPDATE webhooks SET name = @name, url = @url, format = @format, events = @events, enabled = @enabled, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: RotateWebhookSecret :one
UPDATE webhooks SET secret = @secret, updated_at = now() WHERE id = @id RETURNING *;

-- name: DeleteWebhook :execrows
DELETE FROM webhooks WHERE id = @id;

-- name: EnqueueDeliveries :execrows
-- Queues the event for every enabled webhook subscribed to it.
INSERT INTO webhook_deliveries (webhook_id, event, payload)
SELECT w.id, @event, @payload FROM webhooks w WHERE w.enabled AND @event::text = ANY (w.events);

-- name: EnqueueDelivery :one
INSERT INTO webhook_deliveries (webhook_id, event, payload) VALUES (@webhook_id, @event, @payload) RETURNING *;

-- name: ClaimDueDeliveries :many
-- Leases due deliveries so that several replicas never send the same one at
-- the same time. A delivery whose worker dies is retried when the lease ends.
UPDATE webhook_deliveries d SET next_attempt_at = now() + make_interval(secs => @lease_seconds::double precision)
FROM (
    SELECT id FROM webhook_deliveries
    WHERE status = 'pending' AND next_attempt_at <= now()
    ORDER BY next_attempt_at
    LIMIT @lim
    FOR UPDATE SKIP LOCKED
) due
WHERE d.id = due.id
RETURNING d.*;

-- name: MarkDeliveryDelivered :exec
UPDATE webhook_deliveries SET status = 'delivered', attempts = attempts + 1, last_status_code = sqlc.narg('status_code'),
    last_error = NULL, delivered_at = now()
WHERE id = @id;

-- name: MarkDeliveryFailedAttempt :exec
UPDATE webhook_deliveries SET attempts = attempts + 1, last_status_code = sqlc.narg('status_code'),
    last_error = @last_error, next_attempt_at = @next_attempt_at,
    status = CASE WHEN @give_up::boolean THEN 'failed' ELSE 'pending' END
WHERE id = @id;

-- name: ListDeliveries :many
SELECT * FROM webhook_deliveries WHERE webhook_id = @webhook_id ORDER BY created_at DESC LIMIT @lim;

-- name: DeleteOldDeliveries :execrows
DELETE FROM webhook_deliveries WHERE status <> 'pending' AND created_at < now() - interval '30 days';

-- name: CreateInboundHook :one
INSERT INTO inbound_hooks (name, kind, token_hash, hmac_secret, enabled)
VALUES (@name, @kind, sqlc.narg('token_hash'), sqlc.narg('hmac_secret'), @enabled)
RETURNING *;

-- name: GetInboundHook :one
SELECT * FROM inbound_hooks WHERE id = @id;

-- name: ListInboundHooks :many
SELECT * FROM inbound_hooks ORDER BY lower(name);

-- name: UpdateInboundHook :one
UPDATE inbound_hooks SET name = @name, enabled = @enabled WHERE id = @id RETURNING *;

-- name: SetInboundHookCredential :one
UPDATE inbound_hooks SET token_hash = sqlc.narg('token_hash'), hmac_secret = sqlc.narg('hmac_secret') WHERE id = @id RETURNING *;

-- name: DeleteInboundHook :execrows
DELETE FROM inbound_hooks WHERE id = @id;

-- name: TouchInboundHook :exec
UPDATE inbound_hooks SET last_used_at = now() WHERE id = @id;

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
