# Privacy and access

Turnbacks measures friction, not people. It is designed for a trusted internal audience: there are no
user accounts, anyone who can reach the server can record and read reports, and administration
requires a personal TLS client certificate (or, with single sign-on, membership of an admin group). This document describes exactly what is stored, who can see it, and
how it is deleted.

> **Deploy Turnbacks only on your internal network or VPN.** Anyone who can reach it can use it.

## What is recorded with each report

| Data | Purpose | Who can see it |
| --- | --- | --- |
| Problem, duration, quantity, time, optional note, optional team | The report itself | Everyone |
| Source (`web`, `pwa_offline`, `cli`, `api`, `slack`, `webhook`, `mattermost`, `teams`) | Data quality | Everyone |
| **Reporter**: the client network address (`ip:10.8.0.12`), the single sign-on user (`user:ann@example.com`, if enabled), a chat user (`slack:T…/U…`, `mattermost:…`, `teams:…`) or an inbound webhook (`hook:<name>[:<label>]`) | Counting distinct **people affected**; audit trail | **Administrators only** |
| One-time undo token (stored only as a SHA-256 hash) | Lets the submitter undo within 15 minutes | Nobody |

By default there are no names, email addresses, accounts or device fingerprints. Each person has their
own VPN address (no NAT), so one address is one person for the "people affected" count. Chat reports
arrive from the chat servers, so they are counted per chat user instead.

With `TURNBACKS_IDENTITY=header` ([identity.md](identity.md)), the reporter is the user name passed
by your single sign-on proxy, usually an **email address**. That is more identifying than an address,
though it is still shown to administrators only and still removed by reporter retention. Requests
without the header fall back to the address.

The address is taken from the TCP connection, or from `X-Forwarded-For` only when the connection comes
from a configured trusted proxy (`TURNBACKS_TRUSTED_PROXIES`). If the proxy setting is wrong, every
report appears to come from the proxy and "people affected" collapses to one, so check it after
deploying (Admin → Reports shows the addresses).

## Who can see what

| | Everyone | Administrators |
| --- | :-: | :-: |
| Record, undo own report within 15 minutes, create problems | ✓ | ✓ |
| Dashboards, rankings, business cases, team breakdowns, CSV exports | ✓ | ✓ |
| Occurrence list (without reporter) | ✓ | ✓ |
| Mark changes and see before/after comparisons; full CSV exports (without reporter) | ✓ | ✓ |
| Reporters (audit view, `reporter` column in exports, filtering by reporter, who marked a change) | | ✓ |
| Correct or delete any report; edit, merge or archive problems; remediations; edit or delete changes | | ✓ |
| Settings, rates, retention, teams, categories, webhooks, audit log | | ✓ |

The dashboards aggregate by problem, category, team and time. There are no per-person metrics,
rankings or histories. The administrators' audit view exists to investigate data-quality problems
(for example, one address submitting implausible numbers), not to monitor individuals.

## Administration

Each administrator has a personal TLS client certificate from the Turnbacks admin CA
([admin-certificates.md](admin-certificates.md)) or, with single sign-on, is in an admin group or user
list ([identity.md](identity.md)); there is no shared password and no session stored on the server.
If neither is configured, administration is disabled entirely.

The **audit log** (Admin → Audit log, CSV export) records every change an administrator makes, and
every time an administrator views or exports who reported what: when, the administrator's name, how
they were recognised (certificate or single sign-on), the route and its parameters, the result, and
the administrator's own reporter key. Request bodies are not stored. Entries are kept indefinitely;
there is no retention setting for them yet.

## Webhooks

Outbound webhooks ([webhooks.md](webhooks.md)) send events to chat channels or other services. Their
payloads **never include the reporter**. The `turnback.recorded` event includes the problem, duration,
team, source and note, so subscribe only destinations whose audience could already open Turnbacks.

## Notes

Notes are free text and visible to everyone, so people are reminded not to include personal details.
Business cases quote a few recent notes with email addresses and `@mentions` redacted.

## Retention and deletion

Configured in **Admin → Settings**:

| Setting | Effect |
| --- | --- |
| `note_retention_days` | Notes on reports older than N days are cleared |
| `reporter_retention_days` | Reporter addresses on reports older than N days are removed. Those reports then no longer count towards "people affected" |

The hourly maintenance loop applies these, and also prunes the Slack replay log and webhook deliveries
older than 30 days. Administrators can delete individual reports or whole batches. Demonstration data is flagged
`is_demo` and removed with `turnbacks seed --purge`. Apply equivalent retention to backups
([backup-restore.md](backup-restore.md)).

## Logs

Access logs contain method, path, status, size, duration and request ID. They do not contain IP
addresses, user names, user agents or query strings. Every state-changing admin request is also logged
with the administrator's name and how they were recognised (`admin request`), and rejected admin
certificates are logged with their subject and the reason. The audit log above is the durable record.

## On the device

The browser stores the queued (unsent) reports, a cache of non-personal reference data (problem
names, categories, teams, settings), the chosen team, and a short list of recent problems for "Happened
again". The `tb` CLI stores its server URL and team (`~/.config/turnbacks/config.json`), and its
last report, offline queue and reference cache (`~/.local/state/turnbacks/`), all with user-only file
permissions.
