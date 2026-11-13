# Security policy

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report them privately through GitHub: open the repository's **Security** tab and choose
**Report a vulnerability**. This opens a private advisory that only the maintainers can see.

Please include:
- what an attacker can do, and what they need first (network access, a certificate, a proxy position…);
- steps to reproduce, or a proof of concept;
- the version (`turnbacks version`) and how it is deployed (Docker with Caddy, Kubernetes with
  ingress-nginx, something else).

Turnbacks is maintained by volunteers. We aim to acknowledge a report within a week and to agree a
disclosure date with you once a fix is ready. Reporters are credited in the advisory unless they
prefer not to be.

## Supported versions

Security fixes go into the latest release only. Please upgrade before reporting, if you can.

## What counts as a vulnerability

Turnbacks deliberately has **no user accounts**: anyone who can reach it can record and read
reports. It is meant for an internal network or VPN. Administration needs a TLS client certificate.
The threat model and the controls are described in [docs/security.md](docs/security.md).

These are vulnerabilities, for example:
- becoming an administrator without a valid admin client certificate, or with a revoked name;
- reading reporter addresses without being an administrator;
- spoofing another reporter's address through a correctly configured proxy;
- deleting or changing someone else's report without being an administrator;
- injection, cross-site scripting, cross-site request forgery or server-side request forgery;
- weaknesses in the `turnbacks ca` certificates or keys.

These are not, because they follow from the design or the deployment:
- anyone on the network being able to record and read reports;
- problems that need a misconfiguration the documentation warns against, such as exposing Turnbacks
  to the internet, a `TURNBACKS_TRUSTED_PROXIES` range that includes clients, or `ca.key` on the
  server;
- denial of service through ordinary high request volumes (Turnbacks limits each person's writes;
  anything beyond that belongs at the proxy);
- missing security headers that have no demonstrable impact.

If you are not sure, report it privately anyway.
