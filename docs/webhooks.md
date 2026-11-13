# Webhooks

Webhooks connect Turnbacks to other systems without a dedicated integration for each:

- **Outbound**: Turnbacks tells a chat channel or your own service when something happens.
- **Inbound**: other systems record turnbacks or mark changes. Covers scripts and CI pipelines,
  Mattermost slash commands and Microsoft Teams.

Both are managed by administrators in **Admin → Webhooks** (or the `/webhooks` and `/inbound-hooks`
API, see [api.md](api.md)). Every change to them is in the audit log.

## Outbound webhooks

Each webhook has a URL, a format and the events it subscribes to.

| Event | When |
| --- | --- |
| `turnback.recorded` | A report is recorded (one event per submission, with its `quantity`) |
| `problem.created` | A new problem is created |
| `change.marked` | A change marker is added ([change markers](#change-markers)) |
| `remediation.recorded` | An administrator records a remediation |
| `test` | The **Send test** button; always sent, whatever the subscriptions |

Demonstration data never produces events. `turnback.recorded` is queued in the same transaction as the
report; the others immediately after their change is saved.

| Format | Body | For |
| --- | --- | --- |
| `json` | The event envelope (below), signed | Your own services |
| `slack` | `{"text": …}` with Slack link syntax | Slack incoming webhooks; Discord via its Slack-compatible URL (append `/slack` to the Discord webhook URL) |
| `markdown` | `{"text": …}` with Markdown links | Mattermost, Rocket.Chat and other Slack-like incoming webhooks |
| `teams` | A message with an Adaptive Card | Microsoft Teams **Workflows** ("Post to a channel when a webhook request is received") |

Chat formats show a one-line summary, e.g. *Turnbacks: 3 × 10 min lost to “VPN drops” (Platform)*,
and a link to the problem. Links need `TURNBACKS_PUBLIC_URL`; without it they are left out.

### The JSON envelope

```json
{
  "event": "turnback.recorded",
  "occurred_at": "2026-11-13T09:42:07Z",
  "summary": "Turnback: 10 min lost to “VPN drops”",
  "url": "https://turnbacks.example.com/problems/86008ae3-…",
  "data": {
    "turnback_id": "0f8c7a52-…", "batch_id": null,
    "problem": { "id": "86008ae3-…", "title": "VPN drops", "url": "https://turnbacks.example.com/problems/86008ae3-…" },
    "duration_minutes": 10, "quantity": 1, "team": "Platform", "source": "web",
    "note": "disconnected again", "occurred_at": "2026-11-13T09:41:00Z"
  }
}
```

The reporter is **never** included in any payload. Notes are included in `turnback.recorded` (they are
visible to everyone in the app anyway), so do not send that event to places with a wider audience
than Turnbacks itself.

### Headers and signatures

| Header | Value |
| --- | --- |
| `X-Turnbacks-Event` | The event name |
| `X-Turnbacks-Delivery` | Unique delivery ID; the same on retries, so receivers can ignore duplicates |
| `X-Turnbacks-Timestamp` | Unix seconds when this attempt was sent (`json` only) |
| `X-Turnbacks-Signature` | `sha256=` + hex HMAC-SHA256 of `<timestamp>.<body>`, keyed with the webhook's secret (`json` only) |

The secret (`whsec_…`) is shown once when the webhook is created or its secret replaced. Chat formats
are not signed: the chat system authenticates the URL itself, so keep those URLs secret.

Verifying in Python:

```python
import hashlib, hmac, time

def verify(secret: str, headers, body: bytes, tolerance=300) -> bool:
    ts = headers["X-Turnbacks-Timestamp"]
    if abs(time.time() - int(ts)) > tolerance:
        return False
    expected = "sha256=" + hmac.new(secret.encode(), f"{ts}.".encode() + body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, headers["X-Turnbacks-Signature"])
```

In Go, `turnbacks/internal/webhook.Verify(secret, timestamp, body, signature)` does the same.

### Delivery and retries

- Deliveries are queued in PostgreSQL (`webhook_deliveries`) and sent by a background worker,
  immediately after an event is queued and every 15 seconds for retries. Several replicas can run it
  safely: each delivery is leased to one worker at a time.
- A `2xx` response is success. Network errors, `5xx`, `408` and `429` are retried after 30 seconds,
  then 1, 2, 4… minutes, at most 6 hours apart, for up to 10 attempts. Other `4xx` responses are not
  retried. Delivery is therefore **at least once**: deduplicate on `X-Turnbacks-Delivery`.
- Requests time out after 10 seconds and redirects are not followed.
- Admin → Webhooks shows each webhook's last delivery, and the history icon shows the last 50.
  Delivered and failed records are deleted after 30 days.

### Which addresses webhooks may reach

Webhook URLs are entered by administrators, but a webhook is still a request made from inside your
network, so the address actually connected to (after DNS resolution) is checked:

| Address | Allowed |
| --- | --- |
| Private and public addresses | Yes (chat servers are often internal) |
| Link-local, including cloud metadata (`169.254.169.254`, `fe80::/10`) | Never |
| Unspecified and multicast | Never |
| Loopback | Only with `TURNBACKS_WEBHOOK_ALLOW_LOOPBACK=true` (development) |

No HTTP proxy is used, so the check cannot be bypassed through one. A refused address fails the
delivery without retries. On Kubernetes with the chart's NetworkPolicy enabled, allow egress to the webhook
destinations with `networkPolicy.webhookEgress` (otherwise deliveries are blocked).

## Inbound webhooks

Each inbound webhook has a URL, `/api/v1/hooks/{id}`, and authenticates with its own credential, not
with the browser's same-origin rules. Unknown, disabled and unauthenticated requests all get `401`, so
hook IDs cannot be probed. Bodies are limited to 64 KiB. If Turnbacks is behind single sign-on, leave
`/api/v1/hooks/` outside it ([identity.md](identity.md)).

| Kind | Credential | Reporter | Source |
| --- | --- | --- | --- |
| `json` | Bearer token generated by Turnbacks (`tbhook_…`) | `hook:<name>` or `hook:<name>:<reporter>` | `webhook` |
| `mattermost` | The token Mattermost generates for the slash command | `mattermost:<team>/<user>` | `mattermost` |
| `teams` | The security token Teams generates for the outgoing webhook | `teams:<Azure AD object ID>` | `teams` |

Tokens are stored as SHA-256 hashes (`json`, `mattermost`). The Teams key is needed to check
signatures, so it is stored as given. **Replace credential** issues a new token (`json`) or stores a
new one from the other system. Admin → Webhooks shows when each hook was last used.

### JSON (scripts, CI, monitoring)

```sh
# Record a turnback (problem by alias, or "problem_id")
curl -X POST https://turnbacks.example.com/api/v1/hooks/<id> \
  -H "Authorization: Bearer $TURNBACKS_HOOK_TOKEN" -H 'Content-Type: application/json' \
  -d '{"problem": "ci", "minutes": 20, "note": "runner vanished", "reporter": "nightly-build"}'

# Mark a change, e.g. at the end of a deployment
curl -X POST https://turnbacks.example.com/api/v1/hooks/<id> \
  -H "Authorization: Bearer $TURNBACKS_HOOK_TOKEN" -H 'Content-Type: application/json' \
  -d '{"action": "change", "title": "Deployed VPN gateway 2.4", "problem": "vpn", "url": "https://ci.example.com/run/812"}'
```

| Field | Action | Meaning |
| --- | --- | --- |
| `action` | | `turnback` (default) or `change` |
| `problem` / `problem_id` | both | Problem alias or ID (required for `turnback`; omit for an organisation-wide change) |
| `minutes`, `quantity`, `note`, `occurred_at` | `turnback` | As for `POST /turnbacks` |
| `id` | `turnback` | Idempotency key: a retry with the same `id` returns `200` instead of recording twice |
| `confirm` | `turnback` | Confirms an unusually long duration or large quantity |
| `reporter` | `turnback` | Optional label recorded as `hook:<name>:<reporter>` (admin-only, like every reporter) |
| `title`, `description`, `url`, `occurred_at` | `change` | As for `POST /changes` |

Responses: `201` with what was recorded, `200` for an idempotent replay, `422` for validation errors
(same error format as the rest of the API).

### Mattermost slash command

1. In Admin → Webhooks, add an inbound webhook of kind **Mattermost slash command** (any placeholder
   token of 16+ characters will do for now) and copy its URL.
2. In Mattermost: **Integrations → Slash Commands → Add**. Command `turnback`, request URL as copied,
   method `POST`.
3. Copy the token Mattermost shows and use **Replace credential** to store it.

People then type the same commands as in Slack ([slack.md](slack.md)): `/turnback vpn 10 disconnected
again`, `/turnback list`. Replies are ephemeral. The token is accepted from the `token` form field or an
`Authorization: Token …` header.

### Microsoft Teams outgoing webhook

1. In Teams: **Manage team → Apps → Create an outgoing webhook**. Name it (e.g. `Turnbacks`) and set
   the callback URL to the hook's URL (add the hook first with a placeholder if you need the URL).
2. Teams shows a security token once: store it in the hook with **Replace credential**. Use the same
   name for the hook, because replies tell people to type `@Turnbacks …`.
3. In a channel: `@Turnbacks vpn 10 disconnected again`.

Requests are verified with Teams' `Authorization: HMAC …` signature. The mention and any formatting are
removed before the command is read. Each Teams message ID is recorded once, even if Teams retries.

### Replay protection

Slack requests carry a timestamp and a signature that is remembered, so a captured request cannot be
replayed. Mattermost and Teams send nothing equivalent, so their protection is the secrecy of the token
(and of the traffic, which is HTTPS). Teams retries are idempotent by message ID; Mattermost requests
are each recorded. JSON hooks are idempotent when the caller supplies `id`.

## Change markers

A change marker records the moment something changed, such as *VPN client 2.3 rolled out*. Turnbacks
then compares equal periods immediately before and after it. Webhooks are one way to create them (the
`change` action above), so a deployment pipeline can mark every release automatically. See
[api.md](api.md#change-markers) for the full description.
