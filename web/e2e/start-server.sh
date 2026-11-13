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

# Recreates the E2E database and starts the production binary.
set -eu
BIN="${E2E_BINARY:-../bin/turnbacks}"
if [ ! -x "$BIN" ]; then
  echo "missing $BIN - run 'make build' first" >&2
  exit 1
fi
DB_NAME=$(printf '%s' "$E2E_DATABASE_URL" | sed -E 's#^.*/([^/?]+)(\?.*)?$#\1#')
ADMIN_URL=$(printf '%s' "$E2E_DATABASE_URL" | sed -E "s#/${DB_NAME}#/postgres#")
if command -v psql >/dev/null 2>&1; then
  psql "$ADMIN_URL" -q -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS ${DB_NAME} WITH (FORCE)" -c "CREATE DATABASE ${DB_NAME}"
else
  docker compose -f ../compose.yaml exec -T db psql -U turnbacks -d postgres -q \
    -c "DROP DATABASE IF EXISTS ${DB_NAME} WITH (FORCE)" -c "CREATE DATABASE ${DB_NAME}"
fi

# A throwaway admin CA. Tests act as administrators by sending the certificate
# in X-Client-Cert, as Caddy would after verifying the TLS client certificate;
# the server trusts that header only from loopback (the "proxy").
CA_DIR="${E2E_CA_DIR:-../bin/e2e-ca}"
rm -rf "$CA_DIR"
"$BIN" ca init --dir "$CA_DIR" --years 1 >/dev/null
"$BIN" ca issue --dir "$CA_DIR" --days 2 e2e-admin >/dev/null
export TURNBACKS_ADMIN_CLIENT_CA="$CA_DIR/ca.crt"
export TURNBACKS_TRUSTED_PROXIES="127.0.0.1,::1"
exec "$BIN" serve
