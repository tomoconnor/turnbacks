#!/bin/sh
# Copyright (c) 2026 Tom O'Connor <tom@twinhelix.org>
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in
# all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

# Development orchestration. Makes no assumptions about which ports are free:
# every port is discovered at start-up, and if a service still fails to bind
# (another process grabbed the port in the meantime) it is retried on the
# next free port.
#
#   scripts/dev.sh run                 PostgreSQL + API + Vite dev server
#   scripts/dev.sh db                  ensure PostgreSQL is running; print its port
#   scripts/dev.sh turnbacks <args>    run the CLI against the dev database (seed, migrate …)
#   scripts/dev.sh e2e [args]          Playwright tests against bin/turnbacks
#
# Preferred starting ports can be overridden: DB_PORT (5432), API_PORT (8080),
# WEB_PORT (5173), E2E_PORT (18181).
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
BIN_DIR="$ROOT/bin"
DEVPORT="$BIN_DIR/devport"
ATTEMPTS=10

say() { printf '\033[36m[dev]\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31m[dev] error:\033[0m %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "'$1' is required but was not found on PATH ($2)"; }

check_go() {
	need go "install from https://go.dev/dl"
	mkdir -p "$BIN_DIR"
	if [ ! -x "$DEVPORT" ] || [ tools/devport/main.go -nt "$DEVPORT" ]; then
		go build -o "$DEVPORT" ./tools/devport || die "could not build the devport helper"
	fi
}

check_docker() {
	need docker "install Docker Desktop or Docker Engine"
	docker info >/dev/null 2>&1 || die "Docker is installed but the daemon is not reachable — start Docker and retry"
	docker compose version >/dev/null 2>&1 || die "the Docker Compose v2 plugin ('docker compose') is required"
}

check_node() {
	need node "install Node.js 22.14+ or 24"
	need npm "it ships with Node.js"
	if [ ! -x web/node_modules/.bin/vite ]; then
		say "frontend dependencies missing — running npm ci"
		(cd web && npm ci --no-audit --no-fund) || die "npm ci failed"
	fi
}

free_port() { "$DEVPORT" free "$1"; }

# Prints the host port of an already-running dev PostgreSQL, if any.
running_db_port() {
	[ -n "$(docker compose ps -q --status running db 2>/dev/null)" ] || return 0
	docker compose port db 5432 2>/dev/null | sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | head -n 1
}

ensure_db() {
	existing=$(running_db_port)
	if [ -n "$existing" ]; then
		TURNBACKS_DB_PORT=$existing docker compose up -d --wait db >/dev/null 2>&1 ||
			die "PostgreSQL container is running on port $existing but not healthy (see: docker compose logs db)"
		DB_PORT=$existing
		say "using running PostgreSQL on 127.0.0.1:$DB_PORT"
		return
	fi
	start=${DB_PORT:-5432}
	i=0
	while [ $i -lt $ATTEMPTS ]; do
		i=$((i + 1))
		p=$(free_port "$start")
		say "starting PostgreSQL on 127.0.0.1:$p"
		if out=$(TURNBACKS_DB_PORT=$p docker compose up -d --wait db 2>&1); then
			DB_PORT=$p
			return
		fi
		case "$out" in
		*"already allocated"* | *"already in use"* | *"bind"*)
			say "port $p was taken while starting — retrying on the next free port"
			start=$((p + 1))
			;;
		*)
			printf '%s\n' "$out" >&2
			die "could not start PostgreSQL"
			;;
		esac
	done
	die "could not find a usable port for PostgreSQL after $ATTEMPTS attempts"
}

dev_env() {
	export TURNBACKS_ENV=development
	# Development only: requests from this machine are administrators (no client
	# certificate needed). Remote clients, e.g. through ngrok, are not. Set
	# TURNBACKS_DEV_ADMIN_LOOPBACK=false to see the app as everyone else does.
	export TURNBACKS_DEV_ADMIN_LOOPBACK="${TURNBACKS_DEV_ADMIN_LOOPBACK:-true}"
	# Let outbound webhooks reach receivers on this machine while developing.
	export TURNBACKS_WEBHOOK_ALLOW_LOOPBACK="${TURNBACKS_WEBHOOK_ALLOW_LOOPBACK:-true}"
	# The Vite dev server proxies the API from loopback and forwards the
	# browser's address, so reporters are recorded correctly in development.
	export TURNBACKS_TRUSTED_PROXIES="${TURNBACKS_TRUSTED_PROXIES:-127.0.0.1,::1}"
	export TURNBACKS_LOG_FORMAT=text
	export TURNBACKS_DATABASE_URL="postgres://turnbacks:turnbacks@127.0.0.1:$DB_PORT/turnbacks?sslmode=disable"
}

# alive_and_listening <pid> <port> <seconds>: succeed once the process is
# listening on the port; fail if it exits first or the time runs out.
alive_and_listening() {
	pid=$1 port=$2 secs=$3
	n=0
	while [ $n -lt $((secs * 5)) ]; do
		kill -0 "$pid" 2>/dev/null || return 1
		if "$DEVPORT" wait "$port" 1 2>/dev/null; then
			sleep 1 # a process that lost the bind race exits almost immediately
			kill -0 "$pid" 2>/dev/null && return 0
			return 1
		fi
		n=$((n + 5))
	done
	return 1
}

stop_pid() {
	if [ -n "${1:-}" ] && kill -0 "$1" 2>/dev/null; then
		kill "$1" 2>/dev/null || true
		wait "$1" 2>/dev/null || true
	fi
}

cmd_run() {
	check_go
	check_docker
	check_node
	ensure_db
	dev_env

	say "building API"
	go build -o "$BIN_DIR/turnbacks-dev" ./cmd/turnbacks || die "API build failed"

	API_PID="" WEB_PID=""
	trap 'stop_pid "$WEB_PID"; stop_pid "$API_PID"' EXIT
	trap 'exit 130' INT TERM

	start=${API_PORT:-8080}
	i=0
	while :; do
		i=$((i + 1))
		[ $i -le $ATTEMPTS ] || die "API could not start after $ATTEMPTS attempts"
		p=$(free_port "$start")
		say "starting API on 127.0.0.1:$p"
		TURNBACKS_HTTP_ADDR="127.0.0.1:$p" "$BIN_DIR/turnbacks-dev" serve &
		API_PID=$!
		if alive_and_listening "$API_PID" "$p" 60; then
			API_PORT=$p
			break
		fi
		stop_pid "$API_PID"
		say "API failed to start on port $p — retrying on the next free port"
		start=$((p + 1))
	done

	start=${WEB_PORT:-5173}
	i=0
	while :; do
		i=$((i + 1))
		[ $i -le $ATTEMPTS ] || die "Vite could not start after $ATTEMPTS attempts"
		p=$(free_port "$start")
		say "starting Vite on 127.0.0.1:$p"
		(cd web && TURNBACKS_API_URL="http://127.0.0.1:$API_PORT" exec ./node_modules/.bin/vite --host 127.0.0.1 --port "$p" --strictPort) &
		WEB_PID=$!
		if alive_and_listening "$WEB_PID" "$p" 60; then
			WEB_PORT=$p
			break
		fi
		stop_pid "$WEB_PID"
		say "Vite failed to start on port $p — retrying on the next free port"
		start=$((p + 1))
	done

	printf '\n\033[32m  Turnbacks dev is running\033[0m\n'
	printf '  App        http://127.0.0.1:%s\n' "$WEB_PORT"
	if [ "$TURNBACKS_DEV_ADMIN_LOOPBACK" = "true" ]; then
		printf '  Admin      you, from this machine (TURNBACKS_DEV_ADMIN_LOOPBACK=false for the public view)\n'
	fi
	printf '  API        http://127.0.0.1:%s\n' "$API_PORT"
	printf '  PostgreSQL 127.0.0.1:%s  (turnbacks/turnbacks)\n' "$DB_PORT"
	printf '  Ctrl-C to stop. Demo data: make seed\n\n'

	# Stop everything if either process exits.
	while kill -0 "$API_PID" 2>/dev/null && kill -0 "$WEB_PID" 2>/dev/null; do
		sleep 1
	done
	say "a dev process exited — shutting down"
	exit 1
}

case "${1:-}" in
run) cmd_run ;;
db)
	check_go
	check_docker
	ensure_db
	echo "$DB_PORT"
	;;
turnbacks)
	shift
	check_go
	check_docker
	ensure_db
	dev_env
	exec go run ./cmd/turnbacks "$@"
	;;
e2e)
	shift
	check_go
	check_docker
	check_node
	ensure_db
	[ -x "$BIN_DIR/turnbacks" ] || die "bin/turnbacks not found — run 'make build' first"
	E2E_PORT=$(free_port "${E2E_PORT:-18181}")
	export E2E_PORT
	export E2E_DATABASE_URL="postgres://turnbacks:turnbacks@127.0.0.1:$DB_PORT/turnbacks_e2e?sslmode=disable"
	say "E2E server on 127.0.0.1:$E2E_PORT, database 127.0.0.1:$DB_PORT/turnbacks_e2e"
	cd web && exec npx playwright test "$@"
	;;
*)
	die "usage: scripts/dev.sh run | db | turnbacks <args> | e2e [args]"
	;;
esac
