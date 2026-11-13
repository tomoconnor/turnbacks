# Admin client certificates

Recording reports and reading dashboards need nothing. **Administration** (settings and rates, teams,
categories, editing and merging problems, remediations, correcting reports, and the reporter audit
view) needs a **TLS client certificate** issued by the Turnbacks admin CA. There is no admin password.

- Each administrator has their own certificate, so admin actions are logged with their name.
- Nothing can be guessed or phished, and there is no sign-in form to attack.
- Removing one administrator does not affect the others.

If Turnbacks sits behind a single sign-on proxy, administrators can instead be an SSO group or user list
(`TURNBACKS_ADMIN_GROUPS`, `TURNBACKS_ADMIN_USERS`; see [identity.md](identity.md)). Certificates keep
working alongside it.

## How it works

```text
browser ──TLS (+ admin cert, if the browser has one)──► Caddy / ingress-nginx ──► Turnbacks
                                                         │ verifies the cert       │ trusts the header only from the proxy
                                                         │ against ca.crt          │ verifies the cert against ca.crt again
                                                         └ X-Client-Cert: <cert> ─►└ admin = certificate common name
```

1. The proxy terminates TLS and **asks for** a client certificate without requiring one. Browsers only
   offer certificates issued by the admin CA, so everyone else never sees a prompt.
2. If a certificate is presented, the proxy checks it against `ca.crt` and checks the TLS proof that
   the browser holds the private key. A certificate from any other CA ends the connection.
3. The proxy forwards the certificate in a header (`X-Client-Cert` for Caddy, `ssl-client-cert` for
   ingress-nginx), **always replacing** anything the client sent.
4. Turnbacks believes that header only from a trusted proxy: a peer on the Unix socket (Caddy in the
   Docker stack) or an address in `TURNBACKS_TRUSTED_PROXIES`. It then verifies the certificate again,
   checking the chain to `ca.crt`, the validity dates, client-authentication usage and the optional
   name allow-list. The administrator's name is the certificate's common name.

Certificates are public; only the private key proves identity. This is why the proxy must do the TLS
check, and why Turnbacks ignores the header from anything other than the proxy.

## The CA

`turnbacks ca` is a deliberately tiny CA:
- one ECDSA P-256 root, valid for 10 years;
- one certificate per administrator, valid for 1 year by default.

The root can **only** sign client-authentication certificates (its extended key usage says so). Even
if someone mistakenly installed it as a trusted root, it could not vouch for a website.

```sh
make build                            # or: go run ./cmd/turnbacks …
bin/turnbacks ca init --dir ca        # once: ca/ca.key (secret) and ca/ca.crt (public)
bin/turnbacks ca issue --dir ca jane  # → ca/issued/jane.{p12,crt,key} and a random bundle password
```

Make targets do the same: `make ca-init`, `make ca-issue NAME=jane [DAYS=365]`. Each issue is
appended to `ca/issued.log` (name, serial, SHA-256 fingerprint, expiry). `ca/` is git-ignored.

**Look after `ca/ca.key`.** Anyone with it can make themselves an administrator. Keep it on an
administrator's machine or in a password manager, back it up, and never copy it to the server or the
cluster: they only need `ca.crt`. When your organisation's PKI or MDM arrives, put its client CA
certificate in place of `ca.crt` and stop using `turnbacks ca`.

Send `jane.p12` and its password to Jane **by different channels**, for example the file by email and
the password in a chat message. Then delete your copies of `jane.p12` and `jane.key`.

## Installing a certificate (administrators)

| Platform | How |
| --- | --- |
| macOS (Safari, Chrome, Edge) | Double-click `jane.p12`, enter the password, and add it to the **login** keychain |
| Windows (Chrome, Edge) | Double-click `jane.p12` → Current User → enter the password → place automatically |
| Firefox (any OS) | Settings → Privacy & Security → Certificates → View Certificates → Your Certificates → Import |
| Linux (Chrome) | `chrome://certificate-manager` → Your certificates → Import |

Quit and reopen the browser, open Turnbacks and choose the certificate when asked. The header then
shows **Admin · jane**. Browsers remember the choice for the session; to stop being an admin, quit
the browser (or remove the certificate).

On macOS, Safari may ask once for permission to use the key: choose **Always Allow**.

## Revoking

- **Allow-list (recommended).** Set `TURNBACKS_ADMIN_NAMES=jane,raj` (Helm: `admin.names`). Remove a
  name and restart to revoke that person at once. To give someone a fresh certificate, issue it under a
  new name such as `jane-2`.
- **Expiry.** Certificates expire (1 year by default; choose with `--days`).
- **Everything.** Create a new CA, replace `ca.crt` and reissue certificates.

There is no CRL or OCSP: the allow-list is simpler and takes effect immediately.

## Docker stack (Caddy)

`compose.prod.yaml` is standalone: Caddy, Turnbacks and PostgreSQL.

```sh
cp .env.example .env     # set TURNBACKS_HOST, POSTGRES_PASSWORD (and TURNBACKS_TLS)
bin/turnbacks ca init --dir ca && bin/turnbacks ca issue --dir ca jane
docker compose -f compose.prod.yaml --env-file .env up -d --build
```

| Setting | Meaning |
| --- | --- |
| `TURNBACKS_HOST` | Host name people use |
| `TURNBACKS_TLS` | Server certificate: `internal` (Caddy's local CA; browsers must trust Caddy's root, in the `caddy-data` volume), an email address for ACME (public DNS), or `/certs/tls.crt /certs/tls.key` (mount them into the caddy service) |
| `TURNBACKS_ADMIN_CA` | Path on the host of `ca.crt` (default `./ca/ca.crt`) |
| `TURNBACKS_ADMIN_NAMES` | Optional allow-list |

Caddy talks to Turnbacks over a **Unix socket** on a shared volume (`app-socket`), not over a network.
Only Caddy mounts that volume, so whatever connects to the socket is Caddy and its headers can be
believed, with no IP ranges to configure. The app is attached only to the internal database network.
Caddy serves HTTP/1.1 and HTTP/2 only, because client certificates are unreliable over HTTP/3 in some
browsers.

Reporter addresses come from Caddy's `X-Forwarded-For`. On Linux, Docker's published ports preserve
the client address. Docker Desktop (macOS/Windows) shows its VM gateway instead, which is fine for
trying it out but not for production.

## Kubernetes (ingress-nginx)

```sh
kubectl -n turnbacks create secret generic turnbacks-admin-ca --from-file=ca.crt=ca/ca.crt
helm upgrade --install turnbacks deploy/helm/turnbacks … --set admin.clientCA.existingSecret=turnbacks-admin-ca
```

With `admin.ingressNginx=true` (the default), the chart adds the ingress-nginx annotations
`auth-tls-secret`, `auth-tls-verify-client: optional`, `auth-tls-verify-depth: 1` and
`auth-tls-pass-certificate-to-upstream: true`. ingress-nginx then forwards the certificate as
URL-encoded PEM in `ssl-client-cert` (`admin.certHeader`).

For another ingress or gateway, set `admin.ingressNginx=false` and configure the equivalent: request
and verify client certificates against the CA, and **always overwrite** the forwarded-certificate
header. `config.trustedProxies` must cover the ingress pods (and nothing else should be able to reach
the pods; see the NetworkPolicy).

## Development

`make dev` sets `TURNBACKS_DEV_ADMIN_LOOPBACK=true`, so requests from your own machine are
administrators without a certificate. Requests that reach the dev server from elsewhere are not, for
example through ngrok: the original address is still in `X-Forwarded-For`. Set it to `false` to see
the app as everyone else does. Turnbacks refuses this setting in production.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| No certificate prompt | Is the certificate imported into the store your browser uses? Did you restart the browser? Some browsers cache "no certificate" for the session: quit fully |
| The admin page says a certificate is needed although one was chosen | Server logs: `admin client certificate rejected` gives the reason (expired, wrong CA, name not allowed). If there is nothing, the proxy is not forwarding the header, or Turnbacks does not trust the proxy (`TURNBACKS_TRUSTED_PROXIES`, or the Unix socket) |
| Connection fails as soon as a certificate is chosen | The certificate is not from the configured CA (the proxy rejects it during the TLS handshake) |
| Who did what? | Admin → Audit log (or `GET /api/v1/audit/export`) lists every admin change, and every admin view of reporter data, with the administrator's name. The server log also has an `admin request` line for each change |
