# Offline behaviour (PWA)

Network problems are among the most common turnbacks, and they strike exactly when someone wants to
report one. Turnbacks is therefore an installable Progressive Web App that keeps recording when the
server is unreachable.

## What works offline

- Opening the app (the service worker precaches the application shell: HTML, JS, CSS, icons).
- Recording turnbacks against known problems: quick buttons, search over a cached list of active
  problems, presets, notes, anonymity, “Happened again”.
- Viewing and managing unsent reports (**Unsent** badge → Outbox page).

Not available offline: dashboards, business cases, creating problems, administration.

## How submissions are kept safe

1. Each report gets a client-generated UUID and its occurrence time **when it is recorded**.
2. It is written to an IndexedDB **outbox** before any network request.
3. If the request succeeds (`201`, or `200` for an idempotent replay) the entry is removed.
4. If the network fails, or the server returns 5xx/408/429, the entry stays as *Waiting to send* and is
   retried automatically when the browser comes back online, when the app becomes visible, every 30
   seconds, or when the user presses **Retry now**. Retries are sent with `source: "pwa_offline"`.
5. Because the UUID is the idempotency key, a retry after a lost response cannot create a duplicate:
   the server returns the original record.
6. If the server rejects a queued report (e.g. validation), it is marked *Rejected* and kept, visibly,
   until the user discards it. Reports are never silently dropped.


## No sign-in required

There are no accounts, so there are no sessions to expire. Queued reports are sent as soon as the
server is reachable again. The server records the address the report is finally sent from: one VPN
address per person, so that is normally the same person. The original occurrence time is always
preserved. The `tb` CLI has the same offline queue (see [cli.md](cli.md)).

## Installing

Chrome, Edge and Android: use the browser's *Install app* option. iOS/iPadOS Safari: *Share → Add to
Home Screen*. The app must be served over HTTPS (or `localhost`) for the service worker to register.

## Caching rules

- The service worker precaches build assets only and uses `index.html` as the navigation fallback for
  client-side routes. Requests under `/api/` are never handled or cached by the service worker.
- The app itself caches non-personal reference data (problem names, categories, organisation settings)
  in IndexedDB to make search work offline.
- Fingerprinted assets are served with `Cache-Control: immutable`; `index.html`, `sw.js` and the
  manifest are always revalidated, so new releases are picked up on the next load.
