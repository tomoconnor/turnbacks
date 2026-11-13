# Architecture

Turnbacks is a **modular monolith**: one Go executable that serves a versioned JSON API, the embedded
React SPA and health endpoints, backed by a single PostgreSQL database.

```text
 Browser (React SPA, service worker, IndexedDB outbox)   tb CLI   Slack · Mattermost · Teams · CI
        │  HTTPS via Caddy / ingress (admin: TLS client cert    │ signed / token-authenticated POSTs
        │  or single sign-on headers)                           ▼
 ┌───────────────────────────── turnbacks (Go) ─────────────────────────────┐
 │ middleware: request ID · access log · recover · security headers ·       │
 │             trusted proxies · timeout · no-store · actor · CSRF guard ·  │
 │             audit log                                                    │
 │                                                                          │
 │ internal/api  ──►  internal/service  ──►  internal/store (pgx + sqlc)    │
 │      │                  │    └──►  internal/reporting (pure functions)   │
 │ internal/auth     internal/domain (types, validation, money, errors)     │
 │ internal/integrations/{slack,chatcmd} ──► internal/service               │
 │ internal/webhook (outbound formats, signing, guarded HTTP client)        │
 │ web/embed.go (embed.FS SPA)     db/embed.go (embed.FS migrations)        │
 └──────────────────────────────────────────────────────────────────────────┘
        │                                    outbound webhooks ──► chat, your services
        ▼
   PostgreSQL (source of truth; all aggregation in SQL; webhook queue)
```

## Module boundaries

| Package | Responsibility | Depends on |
| --- | --- | --- |
| `internal/api` | HTTP routing (chi), decoding/validation of transport input, mapping domain errors to status codes, **API contract types** (separate from DB models), CSV exports, audit-log middleware, inbound webhook receiver (JSON, Mattermost, Teams), SPA serving | service, auth, domain, chatcmd |
| `internal/auth` | Request identity through a pluggable `Identifier`: the client address (after trusted-proxy handling) or a single sign-on proxy header; admin identity from the forwarded client certificate (verified against the admin CA) or single sign-on users/groups | domain |
| `internal/ca` | The tiny admin CA behind `turnbacks ca init/issue` | — |
| `internal/service` | All business rules: permissions, idempotent submission, problem merging, retention, analytics orchestration, business cases, change markers and their comparisons, webhook queue and delivery worker, inbound hook credentials, audit log, full exports | store, domain, reporting, webhook |
| `internal/webhook` | Outbound webhook formats (JSON, Slack, Markdown, Teams), HMAC signing, retry policy, and an HTTP client that refuses link-local addresses | — |
| `internal/reporting` | Pure, unit-tested logic: reporting periods in a timezone, previous-period resolution, projections, before/after windows | domain |
| `internal/domain` | Shared types, validation, exact money arithmetic (`math/big.Rat`), error kinds | — |
| `internal/store` | pgx pool, goose migrations (embedded), transactions, PostgreSQL error helpers | db/sqlc |
| `internal/integrations/slack` | Slack slash-command transport (signature and replay checks) | chatcmd, service, store |
| `internal/integrations/chatcmd` | The chat command shared by Slack, Mattermost and Teams (`vpn 10 x3 note`, `list`, `help`) | service, domain |
| `db/sqlc` | **Generated** by sqlc from `db/queries/*.sql` — never edited by hand | — |

Business logic lives in Go only. The React app formats numbers and validates forms for fast feedback,
but every rule (durations, permissions, idempotency, money) is enforced again on the server.

Interfaces are used sparingly: `auth.Identifier` (where identity comes from) and the outbox `Sender`
in the frontend (testability). The service uses concrete store types; integration tests run against real
PostgreSQL rather than mocks.

## Request flow: recording a turnback

1. The SPA generates a UUID for the report, writes it to the IndexedDB **outbox**, then POSTs it.
2. `POST /api/v1/turnbacks` → session middleware resolves the actor → CSRF guard → handler decodes
   strictly (unknown fields rejected) → `service.SubmitTurnback`.
3. In a transaction: follow merge redirects, reject archived problems, `INSERT … ON CONFLICT (id) DO NOTHING`.
   If the ID already exists with identical content the existing row is returned (`200`, `replayed: true`);
   different content → `409 idempotency_conflict`.
4. On success the SPA removes the outbox entry and invalidates cached analytics.

## Key decisions

| # | Decision | Rationale |
| --- | --- | --- |
| 1 | Each turnback is an immutable-by-default **occurrence row**; counts are derived with SQL | Spec principle; avoids counter drift; enables any aggregation |
| 2 | **Client-generated UUID as primary key** doubles as idempotency key | Offline retries cannot duplicate; no separate idempotency table |
| 3 | Money as **integer minor units**; cost aggregated as exact `NUMERIC` “minute-minor-units” and divided by 60 once | No floating point; totals never accumulate per-row rounding |
| 4 | Cost uses the **current** configured rate (team rate, else org default) | Rates are assumptions, not historical facts; changing them re-prices history transparently. Documented in the financial model |
| 5 | Reporting periods are calendar days in the **organisation timezone**, half-open `[start, end)` in UTC | DST-safe; consistent bucketing in SQL via `timezone()` |
| 6 | **No user accounts.** The reporter is a pseudonymous key: the client address (`ip:…`, one VPN address per person) by default, or a single sign-on user from a trusted proxy header (`user:…`); chat users and webhooks have their own keys. Used for "people affected" and shown to admins only | Trusted internal audience; zero sign-in friction; works with whatever SSO proxy already exists, without Turnbacks speaking OIDC; see [identity.md](identity.md), [privacy.md](privacy.md) |
| 7 | Administration is a **TLS client certificate** from a dedicated CA, checked by the TLS proxy and verified again by the app (admin name = certificate CN), or membership of an admin group/user list from single sign-on | No shared secret, per-person audit, nothing to brute-force; no server-side session state |
| 8 | CSRF: SameSite + Fetch Metadata/Origin check + JSON-only bodies | No token plumbing; cross-site forms cannot produce JSON without a CORS preflight, which is never granted |
| 9 | The proxy reaches the app over a Unix socket in the Docker stack; socket peers are trusted proxies. Undo uses a one-time token returned at submission | No IP ranges to configure; the app has no web-facing network |
| 10 | Migrations and SPA **embedded** in the binary; migrate on start (configurable) | One artefact; Node.js not needed at runtime |
| 11 | OpenAPI 3.1 is hand-written; a test asserts every router route is documented and vice versa; TS types generated with `openapi-typescript` | Contract stays in sync without a code-generation framework |
| 12 | Merged problems keep their row (`merged_into_id`, archived); late offline submissions are redirected | History preserved; queued reports never fail because of a merge |
| 13 | Service worker precaches only the app shell; API responses are never cached by it | Avoids caching private data; offline needs only non-personal reference data, cached by the app |
| 14 | No Redis, queues or materialised views | Indexed queries are sufficient at the expected scale |
| 15 | Outbound webhooks are queued in a PostgreSQL table (in the same transaction as a recorded turnback) and sent by a worker that leases rows with `FOR UPDATE SKIP LOCKED` | At-least-once delivery with retries, safe on several replicas, without a message broker |
| 16 | Change markers are timestamps, compared over equal windows immediately either side of the exact time (capped at the time elapsed) | "When did it change?" is all most fixes need; works for a change made an hour ago as well as a month ago. Remediations remain for costed improvements |
| 17 | The admin audit log is written by HTTP middleware from the route pattern and parameters, not by each service method | Every admin action is covered automatically, including future endpoints; request bodies (which may hold secrets) are never stored |

## Scale expectations

Indexed aggregate queries handle hundreds of thousands of occurrences comfortably (the seed creates
~3,000 for six months of a 32-person organisation; queries run in single-digit milliseconds). If an
organisation reaches tens of millions of rows, add a daily rollup table refreshed by the maintenance
loop — not needed for the MVP.

## Background work

Two goroutines, both idempotent and safe to run on several replicas at once:

- `service.MaintenanceLoop` (hourly) applies retention settings, removes old Slack replay records and
  deletes delivered or failed webhook deliveries older than 30 days.
- `service.WebhookLoop` sends queued webhook deliveries immediately after an event is queued on this
  replica, and every 15 seconds for retries and events queued by other replicas
  ([webhooks.md](webhooks.md)).

## Deliberately deferred

| Feature | Status / reason |
| --- | --- |
| User accounts / built-in OIDC | Not planned: trusted internal use. Identity is the network address or, optionally, a single sign-on user from a trusted proxy header ([identity.md](identity.md)) |
| Slack interactive modals | Slash command only; large durations confirmed with a `!` suffix |
| Per-period historical rate snapshots | Current rates re-price history (decision 4) |
| Small-group suppression in team breakdowns | Team breakdown shows participation (distinct reporters ÷ headcount) instead |
| Multi-organisation tenancy | One organisation per deployment |
| Daily rollup tables | Not needed at expected scale |
| Background Sync API | The app syncs on `online`, visibility change, a 30-second timer and manual retry, which works in all browsers |
