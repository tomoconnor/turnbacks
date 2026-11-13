# Contributing to Turnbacks

Thanks for helping. Bug reports, documentation fixes and pull requests are all welcome.

- **Bugs:** open an issue with what you did, what you expected, what happened, and your version and
  deployment (Docker or Kubernetes).
- **Features or larger changes:** open an issue first so we can agree the approach before you spend
  time on it. Turnbacks aims to stay small and quick to record with.
- **Security problems:** do not open an issue. See [SECURITY.md](SECURITY.md).

## Getting set up

You need Go 1.27+, Node.js 22.14+ (or 24) and Docker. [sqlc](https://sqlc.dev) is only needed if you
change SQL queries.

```sh
make setup   # frontend dependencies and Playwright's browser
make build   # build the frontend (embedded in the Go server) and the binaries
make dev     # PostgreSQL, API and Vite on free ports; you are admin from your own machine
make seed    # optional: six months of demonstration data
```

`make help` lists every command. The [README](README.md) explains the repository layout, and
[docs/architecture.md](docs/architecture.md) explains the design decisions.

## Making a change

| If you change… | Also… |
| --- | --- |
| The database schema | Add a new migration in `db/migrations/` (`NNNNN_description.sql`, with `-- +goose Down`). Never edit a migration that has been released |
| A query in `db/queries/` | Run `make sqlc` and commit the generated `db/sqlc/` files. Don't edit them by hand |
| The API | Update `api/openapi.yaml` first, then run `make api-types` and commit `web/src/api/schema.d.ts` |
| Configuration | Update `.env.example`, the Helm chart values and the relevant docs |
| Anything user-facing | Keep it keyboard-usable and accessible ([docs/accessibility.md](docs/accessibility.md)) |
| Behaviour, settings or deployment | Update the docs in the same pull request |

Business rules belong in `internal/service`, not in HTTP handlers or the frontend. Admin checks must be
enforced there too, not only hidden in the UI.

## Before opening a pull request

```sh
make fmt     # gofmt and Prettier
make check   # lint plus Go (unit and PostgreSQL integration) and frontend tests
make e2e     # Playwright journeys against the production binary, if you touched the UI or the API
```

Integration tests start PostgreSQL with testcontainers, so Docker must be running.

- Add or update tests for what you changed. Bug fixes should come with a test that fails without the fix.
- Keep pull requests focused: one change per pull request is easier to review.
- Write commit messages that say what changed and why, with a short first line.
- New source files start with the MIT licence header used in the existing files.

## Licence

Turnbacks is [MIT licensed](LICENSE). By contributing, you agree that your contributions are licensed
under the same terms.
