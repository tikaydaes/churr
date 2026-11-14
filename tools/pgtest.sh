#!/bin/sh
# Starts a throwaway PostgreSQL cluster for tests and removes it afterwards.
#
#   tools/pgtest.sh go test ./...     run a command with CHURR_TEST_POSTGRES set
#   tools/pgtest.sh                   print the URL and wait until interrupted
#
# The cluster lives in a temporary directory, listens on a spare port on
# localhost only, and trusts local connections, so it needs no password and
# leaves nothing behind. It needs the PostgreSQL server programs (initdb,
# pg_ctl, postgres): they are looked for in PG_BIN, then in Homebrew's
# postgresql@17, then on PATH.
set -eu

find_bin() {
	for dir in "${PG_BIN:-}" /opt/homebrew/opt/postgresql@17/bin /usr/local/opt/postgresql@17/bin /usr/lib/postgresql/17/bin; do
		if [ -n "$dir" ] && [ -x "$dir/initdb" ]; then
			echo "$dir"
			return
		fi
	done
	if command -v initdb >/dev/null 2>&1; then
		dirname "$(command -v initdb)"
		return
	fi
	echo "pgtest: initdb not found; set PG_BIN to PostgreSQL's bin directory" >&2
	exit 1
}

BIN=$(find_bin)
DIR=$(mktemp -d "${TMPDIR:-/tmp}/churr-pgtest.XXXXXX")
# The socket directory must have a short path: a Unix socket name is limited
# to about a hundred characters and macOS temporary directories are long.
SOCK=$(mktemp -d /tmp/churr-pg.XXXXXX)

cleanup() {
	status=$?
	trap - EXIT INT TERM
	if [ -f "$DIR/data/postmaster.pid" ]; then
		"$BIN/pg_ctl" -D "$DIR/data" -m immediate -w stop >/dev/null 2>&1 || true
	fi
	rm -rf "$DIR" "$SOCK"
	exit "$status"
}
trap cleanup EXIT INT TERM

# Ask the system for a port nobody is using. There is a small window between
# this and the server binding it; for a test helper that is acceptable.
PORT=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()' 2>/dev/null || echo $((20000 + $$ % 20000)))

"$BIN/initdb" -D "$DIR/data" -U churr --auth=trust --encoding=UTF8 --locale=C --no-sync >"$DIR/initdb.log" 2>&1 || {
	cat "$DIR/initdb.log" >&2
	exit 1
}
# fsync and friends are off because the data is thrown away; it makes the
# storage tests several times faster.
"$BIN/pg_ctl" -D "$DIR/data" -l "$DIR/postgres.log" -w \
	-o "-p $PORT -c listen_addresses=127.0.0.1 -c unix_socket_directories=$SOCK -c fsync=off -c synchronous_commit=off -c full_page_writes=off -c max_connections=200" \
	start >/dev/null || {
	cat "$DIR/postgres.log" >&2
	exit 1
}
"$BIN/createdb" -h 127.0.0.1 -p "$PORT" -U churr churr_test

URL="postgres://churr@127.0.0.1:$PORT/churr_test?sslmode=disable"

if [ $# -eq 0 ]; then
	echo "$URL"
	echo "pgtest: running; press Ctrl-C to stop and remove it" >&2
	while :; do sleep 3600; done
fi

CHURR_TEST_POSTGRES="$URL" "$@"
