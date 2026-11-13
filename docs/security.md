# Security review

This reviews the deployed design against common web-application risks (OWASP Top 10 / ASVS level 1–2).
✅ marks controls that are implemented and covered by tests. ⚠️ marks accepted risks or deployment
responsibilities.

## Threat model

Turnbacks is an internal tool for a trusted organisation. By design there are **no user accounts**:
anyone who can reach the server can record and read reports. The security boundary is therefore
**network reachability** (internal network / VPN, optionally behind a single sign-on proxy), plus
**personal TLS client certificates** or single sign-on admin groups for administration. The assets
worth protecting are:
- the integrity of cost assumptions and corrections (admin-only);
- the reporters (admin-only);
- webhook secrets and inbound tokens;
- availability.

## Access control

| | Control | Evidence |
| --- | --- | --- |
| ⚠️ | No end-user authentication (deliberate). Must be deployed on an internal network/VPN only | Deployment and privacy docs; Helm NOTES |
| ✅ | Administration requires a TLS client certificate from the admin CA. The TLS proxy (Caddy / ingress-nginx) requests it, rejects certificates from other CAs during the handshake, and checks proof of key possession | `deploy/caddy/Caddyfile`, Helm ingress annotations; verified against a real Caddy stack |
| ✅ | The forwarded certificate header is believed only from a trusted proxy (a Unix-socket peer or `TURNBACKS_TRUSTED_PROXIES`); Caddy strips any client-supplied copy. The app re-verifies chain, validity, client-auth EKU and the optional name allow-list | `TestAdminClientCertificate` (auth and API), `TestAdminAllowedNames` |
| ✅ | The CA root's extended key usage is restricted to client authentication, so it cannot vouch for servers; keys are ECDSA P-256; `ca.key` is created `0600` and never needed by the server | `TestInitIssue` |
| ✅ | No shared secret, no server-side admin sessions, nothing to brute-force; per-person revocation via `TURNBACKS_ADMIN_NAMES` | `docs/admin-certificates.md` |
| ✅ | Single sign-on identity (`TURNBACKS_IDENTITY=header`): user and group headers are believed only from a trusted proxy; elsewhere they are ignored and the address is used. Admin groups/users require header mode | `TestHeaderIdentity`, `TestSingleSignOnIdentity`, `TestIdentityConfig` |
| ⚠️ | In header mode the proxy **must** strip client-supplied identity headers before setting its own; otherwise users can claim any identity, including an admin. The reference Caddyfile strips the default header names | [identity.md](identity.md) |
| ✅ | Development-only loopback admin (`TURNBACKS_DEV_ADMIN_LOOPBACK`) is refused in production and never applies to remote clients behind a local proxy | `TestDevAdminRefusedInProduction`, `TestDevLoopbackAdmin` |
| ✅ | Every admin operation is authorised in the service layer, not only in the UI | `TestAdminOnlyOperations`, Playwright admin test |
| ✅ | Reporters are returned only to admins (list, filter, CSV columns, change markers' `created_by`) and never sent in outbound webhooks | `TestReporterVisibleToAdminsOnly`, `TestAuditViewAndExports`, `TestFullExports`, `TestOutboundWebhooks` |
| ✅ | Admin audit log: every admin state change, and admin reads of reporter data, recorded with name, method, route, parameters, status and request ID; bodies (which may contain secrets) are not stored | `TestAdminAuditLog` |
| ✅ | Undo is a capability: a random one-time token (hash stored) valid for 15 minutes, which non-admins need to delete a report | `TestUndoAndCorrection` |
| ✅ | Idempotency keys cannot be used to overwrite or read another report's content (`409`) | `TestIdempotentSubmission` |

## Reporter attribution

| | Control |
| --- | --- |
| ✅ | The reporter address comes from the TCP peer; `X-Forwarded-For` is honoured only from `TURNBACKS_TRUSTED_PROXIES`, walking right-to-left past trusted hops, so clients cannot spoof their address unless they are themselves inside a trusted range |
| ⚠️ | The trusted-proxy range must cover the ingress/reverse proxy but **not** the VPN client range. If it is too broad, clients could inflate "people affected" with forged headers. If it is too narrow, every report is attributed to the proxy |

## Input handling and injection

| | Control |
| --- | --- |
| ✅ | All SQL is parameterised (sqlc); `LIKE` wildcards in search are escaped |
| ✅ | Strict JSON decoding: unknown fields rejected, 1 MiB body limit, single object |
| ✅ | Server-side validation of every field (durations, quantities, lengths, enums, http(s)-only URLs, aliases, timezones, currencies, team existence) |
| ✅ | CSV exports neutralise spreadsheet formulas (`TestAuditViewAndExports`) |
| ✅ | React escapes output; no `dangerouslySetInnerHTML`; external links use `rel="noreferrer noopener"` |

## CSRF, XSS and browser hardening

| | Control |
| --- | --- |
| ✅ | CSRF: Fetch Metadata (`Sec-Fetch-Site`) and `Origin` checks plus JSON-only bodies for state-changing requests; browsers send client certificates automatically (like cookies), so these checks still apply to admins; no CORS headers (`TestCSRFProtection`) |
| ✅ | Strict CSP (`script-src 'self'`, `frame-ancestors 'none'`, `object-src 'none'`…), `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`, COOP/CORP, `Permissions-Policy`, HSTS over HTTPS (`TestSPAAndSecurityHeaders`) |
| ✅ | API responses `Cache-Control: no-store`; the service worker never caches `/api/` |

## Integrations

| | Control |
| --- | --- |
| ✅ | Slack: HMAC-SHA256 signatures compared in constant time, 5-minute timestamp window, replay log (`TestVerify`, `TestSlackCommand`) |
| ✅ | Inbound webhooks: JSON hooks use random 256-bit bearer tokens stored as SHA-256 and compared in constant time; Mattermost tokens likewise; Teams requests are HMAC-verified. Unknown, disabled and unauthenticated hooks all return the same `401`; bodies are limited to 64 KiB (`TestInboundWebhooks`, `TestTeamsHelpers`) |
| ⚠️ | Mattermost and Teams send no timestamp, so there is no replay window: a captured request could be replayed. Teams retries are idempotent per message ID; keep tokens secret and traffic on HTTPS |
| ⚠️ | The Teams HMAC key and outbound signing secrets must be usable by the server, so they are stored in the database in clear; protect database backups accordingly |
| ✅ | Outbound webhooks: the address actually dialled is checked after DNS resolution; link-local and other cloud metadata addresses (`100.100.100.200`, `fd00:ec2::254`), unspecified and multicast addresses are always refused, loopback unless `TURNBACKS_WEBHOOK_ALLOW_LOOPBACK`; no redirects, no HTTP proxy, 10-second timeout; JSON payloads are HMAC-signed; webhook URLs never appear in logs or delivery errors (Slack and Teams URLs are credentials); user-entered text is escaped for each chat format, so titles cannot ping `@channel` or carry disguised links (`TestAddressPolicy`, `TestRenderEscapesUserText`, `TestSendRedactsURL`, `TestOutboundWebhooks`) |
| ⚠️ | Private addresses are allowed on purpose (chat servers are usually internal), so an administrator can point a webhook at any internal HTTP service. Webhook configuration is admin-only and audited |

## Operations and supply chain

| | Control |
| --- | --- |
| ✅ | Container: distroless, non-root, read-only filesystem, no capabilities; database never published in `compose.prod.yaml`; Helm chart applies the same settings plus a NetworkPolicy, on by default, that admits only the ingress controller (the app trusts identity headers from `trustedProxies`), with optional egress restriction to DNS, PostgreSQL and `networkPolicy.webhookEgress`; the build context excludes `ca/`, `*.key` and `*.p12` |
| ✅ | Each person's state-changing requests are rate-limited (`TURNBACKS_WRITE_RATE_LIMIT`, default 60 a minute), so one client cannot flood the analytics or the organisation's webhooks (`TestWriteLimiter`) |
| ✅ | Request size, header and handler timeouts; panics recovered with generic `500`s |
| ✅ | `golangci-lint` with `gosec` clean (reviewed suppressions are annotated) |
| ✅ | Dependency scan at review time: `govulncheck` reported 0 reachable vulnerabilities (toolchain pinned to go1.27.4) and one unreachable advisory in indirect `golang.org/x/crypto`; `npm audit --omit=dev` reported 0 |
| ⚠️ | TLS termination, a WAF and rate limiting of reads belong to the reverse proxy; keep running `govulncheck` and `npm audit` in CI |

## Reporting vulnerabilities

Report suspected vulnerabilities privately to the maintainers, not in a public issue.
