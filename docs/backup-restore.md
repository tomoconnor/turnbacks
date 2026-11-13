# Backup and restore

All state lives in PostgreSQL. The application container holds nothing that needs backing up.

## What to back up

The whole Turnbacks database. Slack replay records are ephemeral; restoring them is harmless (old rows
are cleaned up hourly), but you may exclude them with `--exclude-table-data`.

Also back up, separately and securely, the **admin CA** (`ca/ca.key` and `ca/ca.crt`; see
[admin-certificates.md](admin-certificates.md)). With the Docker stack, the `caddy-data` volume holds
Caddy's server certificates; losing it only means Caddy issues new ones (with `TURNBACKS_TLS=internal`,
browsers would need to trust Caddy's new root).

## Logical backups with `pg_dump`

```sh
# Docker Compose (production file): dump from inside the db container
docker compose -f compose.prod.yaml exec -T db \
  pg_dump -U turnbacks -d turnbacks --format=custom --no-owner \
  > turnbacks-$(date +%Y%m%d-%H%M).dump

# Any PostgreSQL
pg_dump "$TURNBACKS_DATABASE_URL" --format=custom --no-owner \
  --exclude-table-data=slack_request_log \
  -f turnbacks.dump
```

Schedule this daily (cron, a Kubernetes CronJob, or your platform's scheduler), encrypt the files at rest,
copy them off-host, and keep a rotation that matches your retention policy (for example 7 daily, 4 weekly,
6 monthly). Managed PostgreSQL services usually offer automated snapshots and point-in-time recovery;
enable them, as they complement logical dumps.

## Restore

```sh
# 1. Stop the application so nothing writes during the restore
docker compose -f compose.prod.yaml stop app

# 2. Recreate an empty database
docker compose -f compose.prod.yaml exec -T db psql -U turnbacks -d postgres \
  -c "DROP DATABASE IF EXISTS turnbacks WITH (FORCE)" -c "CREATE DATABASE turnbacks"

# 3. Restore
docker compose -f compose.prod.yaml exec -T db \
  pg_restore -U turnbacks -d turnbacks --no-owner --exit-on-error < turnbacks-20261111-0200.dump

# 4. Start the app (pending migrations from a newer version are applied automatically)
docker compose -f compose.prod.yaml start app
```

## Test your restores

At least quarterly, restore the latest backup into a scratch database and run the app against it:

```sh
createdb turnbacks_restore_test
pg_restore -d turnbacks_restore_test --no-owner turnbacks.dump
TURNBACKS_DATABASE_URL=postgres://…/turnbacks_restore_test turnbacks migrate status
```

Then check the dashboard totals for a known period match production.

## Retention and backups

Retention settings (note and reporter-link expiry) and user deletions apply to the live database only.
Make sure backup retention is no longer than your privacy policy allows, or apply the same clean-up after
restoring an older backup (the maintenance loop does this within an hour of start-up).
