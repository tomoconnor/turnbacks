# Turnbacks

**Turnbacks** records, analyses and quantifies the everyday operational friction that stops people
working: VPNs that drop, CI runners that vanish, environments that need rebuilding, approvals that
stall. In aerospace manufacturing a *turnback* is logged whenever work has to stop, repeat or be redone
because something in the system prevents normal progress. This application brings that idea to
engineering organisations.

It answers four questions:

1. What repeatedly gets in people's way?
2. How often does it happen?
3. How much productive time — and estimated money — does it consume?
4. Which improvements would deliver the greatest measurable benefit?

Recording a recurring turnback takes a few seconds: press `1` (the problem), `3` (10 minutes), `Enter`.

> Turnbacks is **not** a ticketing system, incident manager, project tracker or employee-monitoring
> tool. Reporting is aggregated and there are no individual rankings. There are no user accounts:
> it's for trusted internal use, so deploy it on your internal network / VPN.

## Features

- **Fast recording** — one compact form: quick-action buttons for common problems, duration presets,
  a quantity for repeats (“VPN dropped, 10 min, 15 times today”), “Happened again” one-tap repeats,
  keyboard shortcuts, searchable problems with aliases; notes and timing tucked under “More options”.
- **Offline-capable PWA** — installable; reports made while the network (or VPN!) is down are queued
  in IndexedDB and synchronised idempotently when connectivity returns. Nothing is silently discarded.
- **Problem discovery** — search, recent and frequent problems, category filters, duplicate
  suggestions when creating a problem, admin merging without losing history.
- **Analytics** — headline metrics with period-over-period change, time series, rankings by
  frequency/time/cost, Pareto analysis, category and team breakdowns (with reporting participation),
  CSV exports. All aggregation is done in PostgreSQL.
- **Financial modelling** — transparent, exact decimal arithmetic; organisation and team hourly rates;
  business cases with adjustable assumptions, payback period, print/PDF and CSV output.
- **Remediation tracking** — record improvements and compare equal before/after windows
  (observational, never claimed as causal).
- **Change markers** — anyone can flag the moment something changed (“VPN client 2.3 rolled out”), for
  one problem or everything; Turnbacks compares equal periods either side and draws the change on the
  charts. CI pipelines can mark changes through a webhook.
- **Webhooks** — outbound events to Slack, Mattermost, Rocket.Chat, Microsoft Teams or signed JSON, with
  retries; inbound webhooks so scripts, Mattermost slash commands and Teams can record turnbacks.
- **No accounts, no sign-in** — everyone on the network can record and read. Each report keeps its
  source address (one VPN address = one person) to count people affected; only administrators see it.
  Administration (settings, rates, merging, corrections, audit) needs a TLS client certificate from a
  tiny built-in CA (`turnbacks ca`); there is no admin password.
- **Single sign-on identity (optional)** — behind oauth2-proxy, Pomerium, Authelia, Authentik or
  Cloudflare Access, people can be identified by their SSO user instead of their address, and an SSO
  group can grant administration.
- **Full exports and audit log** — all-time CSV exports of reports, problems, remediations and changes;
  every administrative action (and every view of who reported what) is recorded in an audit log with
  CSV export.
- **`tb` CLI** — `tb vpn 10` from the terminal, with bash/zsh completion and an offline queue.
- **Slack** — `/turnback vpn 10 disconnected again`, with signature verification and replay protection.
- **Light and dark themes** — follows the system setting, or choose Light or Dark from the header.
- **One binary + PostgreSQL** — the React SPA is embedded in the Go executable. No Node.js at runtime.

## Quick start (Docker, about two minutes)

```sh
docker compose up --build            # PostgreSQL + Turnbacks in development mode
docker compose exec app /turnbacks seed   # optional: six months of demo data
open http://localhost:8080           # administration: use `make dev` or the Caddy stack
```

For production see [docs/deployment.md](docs/deployment.md): `compose.prod.yaml` is a standalone stack
with Caddy (TLS and admin client certificates). Or see [Kubernetes](docs/kubernetes.md).

## Local development

Requirements: Go 1.27+, Node.js 22.14+ (or 24), Docker (for PostgreSQL and integration tests),
[sqlc](https://sqlc.dev) (only to regenerate queries).

```sh
make setup        # npm ci + Playwright browser
make dev          # PostgreSQL in Docker + Go API + Vite (hot reload), each on a free port
make seed         # demo data (marked is_demo; remove with make seed-purge)
```

`make dev` assumes no port is free. It reuses an already-running dev PostgreSQL container, otherwise
probes upwards from 5432 (database), 8080 (API) and 5173 (Vite), and retries on the next free port if a
service still fails to bind. It prints the URLs it chose — open the **App** URL. In development you are
an administrator from your own machine without a certificate. Preferred starting ports can be set, e.g. `make dev API_PORT=9000 WEB_PORT=5300`.
`make seed`, `make migrate-*` and `make e2e` discover the database the same way.

To open the dev server through a tunnel (e.g. on a phone), point ngrok or `cloudflared` at the **App**
port: `*.ngrok-free.app`, `*.ngrok.app`, `*.ngrok.io` and `*.trycloudflare.com` hosts are accepted. Add
others with `TURNBACKS_DEV_ALLOWED_HOSTS=dev.example.com,.mytunnel.dev make dev`. Leave the tunnel's
Host header unchanged (don't use ngrok's `--host-header=rewrite`), or writes are rejected as cross-origin.

| Command | What it does |
| --- | --- |
| `make dev` | Run PostgreSQL, API and Vite on free ports (you are admin from this machine) |
| `make ca-init` / `make ca-issue NAME=jane` | Create the admin CA / issue an admin client certificate |
| `make cli` / `make install-cli` | Build the `tb` CLI / install it with shell completions |
| `make build` | Build the frontend and the production binary `bin/turnbacks` |
| `make test` | Go unit + PostgreSQL integration tests (testcontainers) and Vitest |
| `make e2e` | Playwright journeys against the production binary |
| `make lint` / `make fmt` | golangci-lint, tsc, ESLint, Prettier |
| `make migrate-up` / `migrate-status` | Database migrations (goose, embedded) |
| `make sqlc` / `make api-types` | Regenerate Go queries / TypeScript API types |
| `make docker` | Build the production container image |

The binary has subcommands: `turnbacks serve | migrate up|down|status | seed [--purge] | healthcheck | version`.

## Repository layout

```text
cmd/turnbacks/        server entry point and subcommands
cmd/tb/               the tb command-line client (+ bash/zsh completions)
scripts/dev.sh        development orchestration (port discovery, retries)
tools/devport/        free-port / wait-for-port helper used by scripts/dev.sh
internal/
  api/                HTTP handlers, middleware, API contract types, SPA serving
  auth/               request identity (reporter address), admin client-certificate checks
  ca/                 tiny CA for admin client certificates (`turnbacks ca`)
  config/             environment configuration and safety rails
  domain/             core types, validation, money arithmetic, errors
  service/            business rules (idempotency, merging, permissions, analytics, business cases)
  reporting/          pure reporting logic: periods/timezones, projections, comparison windows
  store/              pgx pool, embedded goose migrations, transactions
  integrations/slack/ /turnback slash command
  seed/               demonstration data generator
db/migrations/        goose SQL migrations (embedded)
db/queries/           hand-written SQL for sqlc
db/sqlc/              generated Go (do not edit)
api/openapi.yaml      OpenAPI 3.1 contract (source of truth for TypeScript types)
web/                  React + TypeScript + Vite + Tailwind + shadcn/ui SPA (embedded via web/embed.go)
deploy/               Helm chart, Argo CD manifests, environment values
docs/                 documentation
```

## Documentation

- [Architecture and decisions](docs/architecture.md) (including deferred features)
- [Database schema](docs/database.md)
- [REST API](docs/api.md) — machine-readable at `/api/v1/openapi.yaml`
- [Financial calculations](docs/financial-model.md)
- [Privacy, access and retention](docs/privacy.md)
- [Admin client certificates](docs/admin-certificates.md)
- [Identity: network address or single sign-on](docs/identity.md)
- [Webhooks (outbound and inbound) and change markers](docs/webhooks.md)
- [Offline behaviour (PWA)](docs/offline.md)
- [Command-line client `tb`](docs/cli.md)
- [Slack integration](docs/slack.md) (Mattermost and Teams: see [webhooks](docs/webhooks.md))
- [Deployment](docs/deployment.md) and [Kubernetes: Helm + Argo CD + CloudNativePG](docs/kubernetes.md)
- [Backup and restore](docs/backup-restore.md)
- [Security review](docs/security.md) and [accessibility review](docs/accessibility.md)

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md). Please report vulnerabilities privately, as described in
[SECURITY.md](SECURITY.md).

## Licence

[MIT](LICENSE)
