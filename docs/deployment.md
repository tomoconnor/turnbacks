# Deployment

Production needs exactly two things: **one Turnbacks executable (or container)** and **one PostgreSQL
database** (17 or 18). The React app, migrations and OpenAPI document are embedded in the binary;
Node.js is not used at runtime.

## Option A — Docker Compose with Caddy (single host, standalone)

```sh
cp .env.example .env        # set TURNBACKS_HOST, POSTGRES_PASSWORD, TURNBACKS_TLS
make build                  # for bin/turnbacks (the CA tool)
bin/turnbacks ca init --dir ca && bin/turnbacks ca issue --dir ca jane
docker compose -f compose.prod.yaml --env-file .env up -d --build
docker compose -f compose.prod.yaml ps      # all three services should become "healthy"
```

`compose.prod.yaml`:

- runs **Caddy** as the only published service (ports 80 and 443). It terminates TLS, redirects HTTP
  to HTTPS, and asks for (but does not require) admin client certificates
  ([admin-certificates.md](admin-certificates.md));
- connects Caddy to the app over a **Unix socket** on a shared volume, so the app has no web-facing
  network at all and needs no trusted-proxy IP ranges;
- runs PostgreSQL on an **internal network with no published port** (not reachable from the host or
  the internet);
- runs the app as a non-root user in a distroless image with a read-only root filesystem, all Linux
  capabilities dropped and `no-new-privileges`;
- uses `turnbacks healthcheck` (probes `/readyz`, over the socket) as the container health check.

`TURNBACKS_TLS` picks Caddy's server certificate: `internal` (Caddy's own CA; install its root from
the `caddy-data` volume on clients, or push it with MDM), an email address (ACME, needs public DNS
and inbound port 80/443 from the CA), or your own `cert key` files.

## Option B — Container platform / Kubernetes

Run the image built by `make docker` (or the `Dockerfile`) with environment variables from
[`.env.example`](../.env.example). Point `TURNBACKS_DATABASE_URL` at a managed PostgreSQL
(`sslmode=require` or `verify-full`). Suggested probes:

```yaml
livenessProbe:  { httpGet: { path: /healthz, port: 8080 }, periodSeconds: 15 }
readinessProbe: { httpGet: { path: /readyz,  port: 8080 }, periodSeconds: 5 }
```

The app is stateless, so it can run multiple replicas. Migrations run on
start-up inside a goose lock; for strict change control set `TURNBACKS_MIGRATE_ON_START=false` and run
`turnbacks migrate up` as a pre-deploy job.

## Option C — Plain binary (systemd)

```sh
make build                  # → bin/turnbacks (frontend embedded)
```

```ini
# /etc/systemd/system/turnbacks.service
[Unit]
Description=Turnbacks
After=network-online.target

[Service]
EnvironmentFile=/etc/turnbacks/env
ExecStart=/usr/local/bin/turnbacks serve
User=turnbacks
Restart=on-failure
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

## Reverse proxy and TLS

Serve Turnbacks over HTTPS (required for the service worker, PWA installation and admin client
certificates). `deploy/caddy/Caddyfile` is the reference configuration. With your own proxy, it must:

- request (optionally) and verify client certificates against the admin CA;
- forward the verified certificate in `TURNBACKS_ADMIN_CERT_HEADER`, **always overwriting** any
  client-supplied value;
- set `X-Forwarded-For`;
- with single sign-on (`TURNBACKS_IDENTITY=header`), remove client-supplied identity headers and set
  them from the authentication response ([identity.md](identity.md)), and leave `/api/v1/hooks/`
  outside single sign-on if you use inbound webhooks.

Set `TURNBACKS_TRUSTED_PROXIES` to the proxy's address (e.g. `127.0.0.1` or the ingress CIDR), or
listen on a Unix socket (`TURNBACKS_HTTP_ADDR=unix:/run/turnbacks/turnbacks.sock`) that only the
proxy can reach. Then `X-Forwarded-For` and the certificate header are honoured only from it. The resulting client address is recorded as each report's
reporter (admin-only; see [privacy.md](privacy.md)); it is not written to access logs. HSTS is sent automatically when `TURNBACKS_PUBLIC_URL` is `https://`.

## Configuration checklist

| | |
| --- | --- |
| Network | **Internal network / VPN only.** There are no user accounts |
| `TURNBACKS_ENV=production` | The default; refuses demo seeding and the development loopback admin |
| `TURNBACKS_PUBLIC_URL` | Exact external origin, e.g. `https://turnbacks.example.com` |
| `TURNBACKS_DATABASE_URL` | Use a dedicated database user; TLS to managed databases |
| `TURNBACKS_ADMIN_CLIENT_CA` | Admin CA certificate (PEM); empty disables administration. Optional `TURNBACKS_ADMIN_NAMES` allow-list. See [admin-certificates.md](admin-certificates.md) |
| `TURNBACKS_TRUSTED_PROXIES` | Your reverse proxy / ingress addresses, **not** the VPN client range (not needed on a Unix socket). Reports are attributed to the resulting client address; verify in Admin → Reports |
| `TURNBACKS_IDENTITY` | `ip` (default) or `header` with `TURNBACKS_IDENTITY_USER_HEADER`, `TURNBACKS_IDENTITY_GROUPS_HEADER` (split by `TURNBACKS_IDENTITY_GROUPS_SEPARATOR`), and optionally `TURNBACKS_ADMIN_GROUPS` / `TURNBACKS_ADMIN_USERS` for single sign-on administrators. See [identity.md](identity.md) |
| Webhooks | Outbound webhooks need egress from the app to their destinations, and `TURNBACKS_PUBLIC_URL` for links in messages. `TURNBACKS_WEBHOOK_ALLOW_LOOPBACK` (default `false`) is for development. Inbound webhooks need `/api/v1/hooks/` reachable from Mattermost or Teams. See [webhooks.md](webhooks.md) |
| `TURNBACKS_WRITE_RATE_LIMIT` | State-changing requests per person per minute (default `60`, per replica; `429` with `Retry-After` beyond it). Administrators, Slack and inbound webhooks are exempt; `0` disables it. Rate-limit reads at your proxy if you need to |
| Secrets | Prefer `*_FILE` variables with Docker/Kubernetes secrets |
| Settings | In Admin → Settings, set organisation name, currency, hourly rate, timezone and retention; in Teams, set headcounts |

## Operations

- **Logs**: structured JSON on stdout; every line for a request carries `request_id`, which is also
  returned in the `X-Request-ID` header and in error responses.
- **Health**: `/healthz` (process alive), `/readyz` (database reachable).
- **Timeouts**: read header 10 s, read 30 s, handler `TURNBACKS_REQUEST_TIMEOUT` (default 30 s),
  idle 120 s.
- **Graceful shutdown**: on `SIGTERM`/`SIGINT` the server stops accepting connections and drains
  in-flight requests for up to `TURNBACKS_SHUTDOWN_TIMEOUT` (default 20 s).
- **Connection pool**: `TURNBACKS_DB_MAX_CONNS` (default 10) per instance.
- **Upgrades**: deploy the new image; migrations apply on start. Roll back with the previous image and,
  if required, `turnbacks migrate down`. Take a backup before upgrading.
- **Demo data** is never loaded automatically and `turnbacks seed` refuses to run in production.
- **Webhooks**: each replica runs the delivery worker; Admin → Webhooks shows the last delivery of
  each webhook and failures are logged as `webhook delivery failed`.
- **Audit log**: Admin → Audit log, or `GET /api/v1/audit/export` for CSV. It has no retention setting
  yet; prune `admin_audit` by hand if you need to.

## Local quick start (not for production)

`docker compose up --build` starts PostgreSQL and the app in development mode on
<http://localhost:8080>. Administration is not available there (no TLS proxy); use `make dev`, where
your own machine is admin. PostgreSQL is published on `127.0.0.1` only.
