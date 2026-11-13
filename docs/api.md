# REST API

The complete, machine-readable contract is the OpenAPI 3.1 document at
[`api/openapi.yaml`](../api/openapi.yaml), also served at `GET /api/v1/openapi.yaml`. TypeScript types for
the frontend are generated from it (`make api-types`). A Go test fails if any route is undocumented or
any documented operation is missing.

## Conventions

- Base path `/api/v1`. JSON request and response bodies (`Content-Type: application/json`).
  Unknown request fields are rejected. Bodies are limited to 1 MiB.
- **Access**: no user accounts. Anyone who can reach the server may record and read. Each report's
  *reporter* counts towards "people affected" and is visible to administrators only: the client
  address (`ip:…`) by default, or the single sign-on user (`user:…`) with
  `TURNBACKS_IDENTITY=header` ([identity.md](identity.md)). Administration requires a TLS client
  certificate from the admin CA ([admin-certificates.md](admin-certificates.md)) or, with single
  sign-on, an admin group or user. There is no sign-in endpoint.
- **CSRF**: state-changing requests must be same-origin (Fetch Metadata / `Origin`) and JSON. Clients
  that send neither header (curl, `tb`) are accepted. Inbound webhooks (`/hooks/{id}`) are exempt:
  they authenticate with their own credentials ([webhooks.md](webhooks.md)).
- **Audit**: administrators' state-changing requests, and their reads of reporter data, are recorded
  in the audit log ([below](#audit-log)).
- **Money** is integer minor units (`*_minor`); the currency and its exponent are in `GET /settings`.
- **Percentages** in analytics are basis points (`*_bp`; 1250 = 12.5 %). Assumption percentages are
  whole numbers.
- **Dates** `YYYY-MM-DD` are calendar days in the organisation's reporting timezone; timestamps are
  RFC 3339 UTC.
- **Pagination**: `limit` and `offset`; list responses include `items`, `total`, `limit` and `offset`.
- **Request IDs**: every response has `X-Request-ID` (a well-formed incoming one is reused), which
  also appears in error bodies and logs.
- **Caching**: API responses are `Cache-Control: no-store`.

### Errors

```json
{
  "error": {
    "code": "validation_failed",
    "message": "one or more fields are invalid",
    "request_id": "6a1f0c2d9b3e4a5f6c7d8e9f",
    "fields": [{ "field": "duration_minutes", "message": "must be between 1 and 1440 minutes" }]
  }
}
```

| Status | Typical codes |
| --- | --- |
| 400 | `bad_request` (malformed JSON, unknown field) |
| 401 | `unauthorised` (inbound webhooks only: unknown or disabled hook, invalid credential) |
| 403 | `admin_required`, `undo_not_allowed`, `csrf_rejected` |
| 404 | `not_found` |
| 409 | `idempotency_conflict`, `alias_taken`, `name_taken`, `problem_merged` |
| 415 | `unsupported_media_type` |
| 422 | `validation_failed`, `duration_confirmation_required`, `quantity_confirmation_required`, `problem_archived` |
| 500 | `internal` (details only in server logs, correlated by request ID) |

## Authorisation

| Capability | Everyone | Admin |
| --- | :-: | :-: |
| Record reports; undo with the returned token within 15 minutes; create problems | ✓ | ✓ |
| Mark changes; before/after comparisons | ✓ | ✓ |
| All analytics, business cases, occurrence list, CSV and full exports (without reporter) | ✓ | ✓ |
| Reporters (list, `?reporter=` filter, CSV columns, who marked a change) | | ✓ |
| Correct or delete any report; edit, merge or archive problems; remediations; edit or delete changes | | ✓ |
| Settings, teams, categories, webhooks, audit log | | ✓ |

## Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/session` | Identity mode and single sign-on user; whether this request is an administrator (name, and `admin_via`: `certificate`, `sso` or `dev`); how administration can be granted; whether Slack is enabled |
| GET/PUT | `/settings` | Organisation settings (PUT: admin) |
| GET/POST, PATCH | `/teams`, `/teams/{id}` | Teams, with optional hourly rate and headcount (writes: admin) |
| GET/POST, PATCH | `/categories`, `/categories/{id}` | Categories (writes: admin) |
| GET/POST | `/problems` | Search (`q`, `category_id`, `status`) / create |
| GET | `/problems/quick` | Most frequently reported problems (30 days) |
| GET | `/problems/similar?title=` | Duplicate suggestions |
| GET/PATCH | `/problems/{id}` | Read / update (admin) |
| POST | `/problems/{id}/merge` | Merge into `target_id` (admin) |
| GET | `/problems/{id}/business-case[/export]` | Business case JSON / CSV |
| GET/POST | `/problems/{id}/remediations` | Remediations (POST: admin) |
| PATCH/DELETE | `/remediations/{id}` | Update / delete remediation (admin) |
| GET | `/remediations/{id}/comparison` | Before/after comparison |
| POST | `/turnbacks` | Record (idempotent) |
| GET | `/turnbacks` | Occurrence list; admins also get `reporter` and `?reporter=` |
| PATCH | `/turnbacks/{id}` | Correct (admin) |
| DELETE | `/turnbacks/{id}` | Delete: admin, or `?undo_token=` within 15 minutes; `?batch=true` deletes the whole batch |
| GET | `/analytics/summary`, `/timeseries`, `/problems`, `/categories`, `/teams`, `/remediations` | Analytics |
| GET | `/analytics/export?dataset=` | CSV: `problems`, `timeseries`, `categories`, `teams`, `turnbacks` |
| GET | `/analytics/changes` | Before/after comparisons for the 20 most recent changes (`problem_id`, `window_days`) |
| GET/POST | `/changes` | Change markers (`from`, `to`, `problem_id`, `include_global`) / mark a change |
| PATCH/DELETE | `/changes/{id}` | Update / delete a change marker (admin) |
| GET | `/changes/{id}/comparison` | Before/after comparison for one change (`window_days`, default 28) |
| GET | `/exports/{dataset}` | Full CSV, all time: `turnbacks`, `problems`, `remediations`, `changes` |
| GET/POST, PATCH/DELETE | `/webhooks`, `/webhooks/{id}` | Outbound webhooks (admin) |
| POST | `/webhooks/{id}/test`, `/webhooks/{id}/rotate-secret` | Queue a test event / replace the signing secret (admin) |
| GET | `/webhooks/{id}/deliveries` | The 50 most recent deliveries (admin) |
| GET/POST, PATCH/DELETE | `/inbound-hooks`, `/inbound-hooks/{id}` | Inbound webhooks (admin) |
| POST | `/inbound-hooks/{id}/credential` | New token (json) or new Mattermost/Teams secret (admin) |
| GET | `/audit`, `/audit/export` | Admin audit log, JSON (`from`, `to`, `before_id`, `limit`) / CSV (admin) |
| POST | `/hooks/{id}` | Inbound webhook endpoint (authenticated by the hook's credential) |
| POST | `/integrations/slack/commands` | Slack slash command (signature-authenticated) |

Analytics endpoints accept `period` (`last_7_days`, `last_30_days` *(default)*, `last_90_days`,
`this_year`, or `custom` with `from`/`to`) and the filters `category_id`, `team_id` and `problem_id`
where relevant. Rankings accept `sort` = `count` | `minutes` | `cost`.

Outside `/api/v1`: `GET /healthz` (liveness) and `GET /readyz` (database reachable).

## Recording a turnback

```http
POST /api/v1/turnbacks
Content-Type: application/json

{
  "id": "0f8c7a52-2d39-4b4e-9d84-5e0b3b4f6a21",
  "problem_id": "86008ae3-e8f9-410b-a99a-768eeb6dc526",
  "team_id": "8452d375-7c72-4c03-975c-da53d9aac7d0",
  "duration_minutes": 10,
  "quantity": 1,
  "duration_estimated": true,
  "note": "disconnected again",
  "occurred_at": "2026-11-11T09:42:00Z",
  "source": "pwa_offline"
}
```

| Situation | Response |
| --- | --- |
| New `id` | `201` `{ "turnback": {…}, "replayed": false, "quantity": 1, "undo_token": "…" }` |
| Same `id` and same content (a retry, even from another network) | `200` `{ "turnback": <original>, "replayed": true }`, with no undo token |
| Same `id`, different content | `409 idempotency_conflict` |
| Duration above the confirmation threshold | `422 duration_confirmation_required`: resend with `"confirm_large_duration": true` |
| `quantity` above 20 | `422 quantity_confirmation_required`: resend with `"confirm_large_duration": true` |
| Problem merged since the client cached it | Recorded against the merge target |
| Problem archived | `422 problem_archived` |

`quantity` (1–100, default 1) records that many identical occurrences, e.g. 10 minutes, 15 times. Each
is its own row (no counters are stored). The rows share `batch_id = id`, and the other rows' IDs are
derived from `id`, so a retried batch is replayed rather than duplicated. `occurred_at` defaults to the
server time; it may be at most 5 minutes in the future and at most a year in the past. `source` may be
`web`, `pwa_offline`, `api` or `cli` (`slack`, `webhook`, `mattermost` and `teams` are reserved for
the integrations).

To undo within 15 minutes:
`DELETE /api/v1/turnbacks/{id}?undo_token=<token>` (add `&batch=true` for a batch).

## Change markers

A change marker is a point in time when something changed (*VPN client 2.3 rolled out*), for one
problem or, without `problem_id`, for everything. Anyone may mark a change; administrators can edit and
delete them.

```http
POST /api/v1/changes
Content-Type: application/json

{ "title": "VPN client 2.3 rolled out", "problem_id": "86008ae3-…", "occurred_at": "2026-10-14T09:30:00Z",
  "url": "https://change.example.com/CHG-1042", "description": "" }
```

`occurred_at` defaults to now; it may not be in the future or more than five years ago. `source` is
`web` (default), `api` or `cli`; `webhook` is set by inbound webhooks. Changes appear as dashed lines on
the dashboard and problem charts.

`GET /changes/{id}/comparison?window_days=28` compares two windows of equal length immediately either
side of the exact time of the change: `[t − w, t)` and `[t, t + w)`. `w` is `window_days` (1–365,
default 28) or the time elapsed since the change, whichever is shorter, so a change marked three hours
ago compares three hours either side. Windows shorter than 14 days are flagged `insufficient_data`.
Organisation-wide changes compare all turnbacks; others compare that problem's. Like remediation
comparisons, this is observational and does not establish causation. The response includes `before`
and `after` (`start`, `end`, `totals`), `change` in basis points and `window_seconds`.

`created_by` (the reporter key of whoever marked it) is returned to administrators only.

## Full exports

`GET /exports/{dataset}` streams a complete CSV, ignoring the dashboard filters:

| Dataset | Contents |
| --- | --- |
| `turnbacks` | Every report, oldest first, with problem, category, team, source, batch and note; `reporter` column for administrators |
| `problems` | Every problem, including merged and archived ones, with aliases, links and an all-time count |
| `remediations` | Every remediation, with costs in the organisation currency |
| `changes` | Every change marker; `created_by` column for administrators |

As with the other exports, cells that a spreadsheet would treat as formulas are prefixed with `'`.

## Audit log

Recorded automatically for administrators' requests:

- every state-changing request (`POST`, `PUT`, `PATCH`, `DELETE`), successful or not;
- reads that show who reported what: `GET /turnbacks`, `/exports/turnbacks`, `/exports/changes`,
  `/analytics/export?dataset=turnbacks`, `/audit`, `/audit/export` and `/webhooks/{id}/deliveries`.

Each entry has `at`, `admin` (certificate name or single sign-on user), `via` (`certificate`, `sso` or
`dev`), `method`, `action` (the route, e.g. `/api/v1/problems/{id}/merge`), `target` (the route
parameters, plus the query string for reads), `status`, `client` (the administrator's own reporter key)
and `request_id`. Request bodies are not stored. `GET /audit` pages newest first with `before_id`;
`GET /audit/export` returns CSV. Entries are kept indefinitely.

## Examples

```sh
curl -s 'https://turnbacks.example.com/api/v1/analytics/summary?period=last_90_days'
curl -s 'https://turnbacks.example.com/api/v1/analytics/problems?sort=minutes&limit=5'
curl -OJ 'https://turnbacks.example.com/api/v1/analytics/export?dataset=problems&period=this_year'
curl -OJ 'https://turnbacks.example.com/api/v1/exports/turnbacks'
curl -s -X POST 'https://turnbacks.example.com/api/v1/changes' -H 'Content-Type: application/json' \
  -d '{"title": "Upgraded the CI runners", "occurred_at": "2026-11-12T16:00:00Z"}'

# Administration
curl --cert ca/issued/jane.crt --key ca/issued/jane.key \
  'https://turnbacks.example.com/api/v1/turnbacks?reporter=ip:10.8.0.12'
curl --cert ca/issued/jane.crt --key ca/issued/jane.key -OJ \
  'https://turnbacks.example.com/api/v1/audit/export'
```

Inbound webhook examples are in [webhooks.md](webhooks.md).

For everyday terminal use, see the [`tb` CLI](cli.md).
