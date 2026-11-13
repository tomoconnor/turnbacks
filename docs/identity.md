# Identity: network address or single sign-on

Turnbacks has no user accounts of its own. Each report records a pseudonymous **reporter**, used to
count distinct "people affected" and shown to administrators only ([privacy.md](privacy.md)). Where
the reporter comes from is configurable:

| `TURNBACKS_IDENTITY` | Reporter | Suits |
| --- | --- | --- |
| `ip` *(default)* | The client address, `ip:10.8.0.12` | A VPN where every person has their own address (no NAT) |
| `header` | The user named by a single sign-on proxy, `user:ann@example.com` | NAT, shared offices, or anywhere an SSO proxy already sits in front of internal tools |

Integrations always use their own identities: `slack:<team>/<user>`, `mattermost:<team>/<user>`,
`teams:<aadObjectId>` and `hook:<name>[:<reporter>]` ([webhooks.md](webhooks.md)).

## Header mode

Turnbacks never talks to an identity provider. A proxy in front of it (oauth2-proxy, Pomerium,
Authelia, Authentik, Cloudflare Access…) signs people in and passes the result in request headers:

| Variable | Default | Meaning |
| --- | --- | --- |
| `TURNBACKS_IDENTITY` | `ip` | `header` turns this mode on |
| `TURNBACKS_IDENTITY_USER_HEADER` | `X-Forwarded-Email` | The signed-in user (usually an email address) |
| `TURNBACKS_IDENTITY_GROUPS_HEADER` | `X-Forwarded-Groups` | The user's groups, comma-separated (optional) |
| `TURNBACKS_ADMIN_GROUPS` | | Members of any of these groups are administrators (group names are compared exactly, including case; user names are lower-cased) |
| `TURNBACKS_ADMIN_USERS` | | These users are administrators |

Rules:

- The headers are believed **only from a trusted proxy**: a peer on the Unix socket (the Caddy stack)
  or an address in `TURNBACKS_TRUSTED_PROXIES`. From anyone else they are ignored.
- The user is trimmed and lower-cased; values over 190 characters or containing control characters
  are ignored. The reporter key is `user:<value>`.
- A request **without** the user header falls back to the client address (`ip:…`). This covers the
  `tb` CLI or other clients on a route that bypasses single sign-on; they still count, by address.
- `TURNBACKS_ADMIN_GROUPS` / `TURNBACKS_ADMIN_USERS` require `TURNBACKS_IDENTITY=header`; the server
  refuses to start otherwise.
- Admin client certificates ([admin-certificates.md](admin-certificates.md)) keep working alongside
  single sign-on administrators. A valid certificate wins; the audit log records which was used
  (`via` = `certificate` or `sso`).

> **The proxy must remove any copy of these headers sent by the client** and set them itself.
> Otherwise anyone could claim to be anyone, including an administrator. The reference Caddyfile
> strips `X-Forwarded-Email` and `X-Forwarded-Groups`; if you change the header names, strip those
> instead.

The page header shows the signed-in user, and the Admin page explains when someone is signed in but not an
administrator.

## Proxy examples

### oauth2-proxy with Caddy

```caddyfile
{$TURNBACKS_HOST} {
	request_header -X-Forwarded-Email
	request_header -X-Forwarded-Groups

	handle /oauth2/* {
		reverse_proxy oauth2-proxy:4180
	}
	# Inbound webhooks authenticate themselves; keep them outside single sign-on.
	handle /api/v1/hooks/* {
		reverse_proxy unix//run/turnbacks/turnbacks.sock
	}
	handle {
		forward_auth oauth2-proxy:4180 {
			uri /oauth2/auth
			copy_headers X-Auth-Request-Email>X-Forwarded-Email X-Auth-Request-Groups>X-Forwarded-Groups
			@unauthorised status 401
			handle_response @unauthorised {
				redir * /oauth2/sign_in?rd={scheme}://{host}{uri}
			}
		}
		reverse_proxy unix//run/turnbacks/turnbacks.sock
	}
}
```

Run oauth2-proxy with `--set-xauthrequest` (and `--reverse-proxy`) so it returns the
`X-Auth-Request-*` headers. Keep the `client_auth` block from `deploy/caddy/Caddyfile` if you also use
admin certificates.

### Authelia or Authentik

Both return `Remote-User`, `Remote-Email` and `Remote-Groups` from their forward-auth endpoints:

```sh
TURNBACKS_IDENTITY=header
TURNBACKS_IDENTITY_USER_HEADER=Remote-Email
TURNBACKS_IDENTITY_GROUPS_HEADER=Remote-Groups
TURNBACKS_ADMIN_GROUPS=turnbacks-admins
```

In Caddy, strip the client's copies with `request_header -Remote-Email -Remote-Groups` and copy them
from the auth response with `copy_headers Remote-Email Remote-Groups`.

### Cloudflare Access

```sh
TURNBACKS_IDENTITY=header
TURNBACKS_IDENTITY_USER_HEADER=Cf-Access-Authenticated-User-Email
TURNBACKS_ADMIN_USERS=ann@example.com,raj@example.com
```

Cloudflare sets this header on every request through the tunnel. Groups are not sent as a header, so
use `TURNBACKS_ADMIN_USERS`. Make sure the origin can only be reached through the tunnel (or through a
trusted proxy that strips the header).

### Pomerium

Pomerium passes claims as `X-Pomerium-Claim-<name>` headers when configured with
`pass_identity_headers` / `jwt_claims_headers`:

```sh
TURNBACKS_IDENTITY=header
TURNBACKS_IDENTITY_USER_HEADER=X-Pomerium-Claim-Email
TURNBACKS_IDENTITY_GROUPS_HEADER=X-Pomerium-Claim-Groups
```

### Kubernetes (ingress-nginx)

```yaml
nginx.ingress.kubernetes.io/auth-url: "http://oauth2-proxy.auth.svc.cluster.local/oauth2/auth"
nginx.ingress.kubernetes.io/auth-signin: "https://$host/oauth2/start?rd=$escaped_request_uri"
nginx.ingress.kubernetes.io/auth-response-headers: "X-Auth-Request-Email,X-Auth-Request-Groups"
```

ingress-nginx forwards the listed response headers, overwriting client copies. Set
`TURNBACKS_IDENTITY_USER_HEADER=X-Auth-Request-Email` and
`TURNBACKS_IDENTITY_GROUPS_HEADER=X-Auth-Request-Groups`, and make sure `TURNBACKS_TRUSTED_PROXIES`
covers the ingress pods only. Exclude `/api/v1/hooks/` from authentication (a separate Ingress without
the `auth-url` annotation) if you use inbound webhooks.

## Checking it works

Open the app: the page header shows the signed-in user. `GET /api/v1/session` returns `identity_mode`,
`user` and, for administrators, `admin_name` and `admin_via`. In Admin → Reports, new reports show
`user:…` reporters. If they show `ip:…`, the header is not arriving or the proxy is not trusted; the
server logs a warning at start-up when `TURNBACKS_IDENTITY=header` is set without trusted proxies.

## Switching modes

Switching changes how new reports are attributed; existing reports keep their `ip:…` reporters. For
a period, "people affected" may count someone twice (once by address, once by name).
