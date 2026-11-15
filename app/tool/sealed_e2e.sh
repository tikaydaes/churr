#!/bin/sh
# The sealed-chat end-to-end test (test/e2e/sealed_e2e_test.dart) against a
# fresh development server: builds the server and devseed, seeds a new
# data directory with five app sessions (kai three times, sam twice),
# serves it without calls on its own port, runs the test, and stops it.
#
#   tool/sealed_e2e.sh            # port 8491
#   PORT=8500 tool/sealed_e2e.sh
set -eu
cd "$(dirname "$0")/.."
APP=$(pwd)
PORT=${PORT:-8491}
WORK=$(mktemp -d "${TMPDIR:-/tmp}/churr-sealed-e2e.XXXXXX")
trap 'kill "$SERVER" 2>/dev/null || true; rm -rf "${WORK:?}"' EXIT INT TERM

# BIN=dir uses churr and devseed already built there (the tree may be
# mid-change elsewhere).
if [ -n "${BIN:-}" ]; then
  cp "$BIN/churr" "$BIN/devseed" "$WORK/"
else
  (cd .. && go build -o "$WORK/churr" ./cmd/churr && go build -o "$WORK/devseed" ./tools/devseed)
fi
"$WORK/devseed" -data "$WORK/data" -http "localhost:$PORT" -o "$WORK/seed.json" \
  -tokens kai,kai,kai,sam,sam -history 5
"$WORK/churr" serve -dev -data "$WORK/data" -http "localhost:$PORT" -media off \
  >"$WORK/server.log" 2>&1 &
SERVER=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
  curl -sf "http://localhost:$PORT/api/v1/server" >/dev/null && break
  sleep 1
done
cd "$APP"
CHURR_SEALED_E2E_SEED="$WORK/seed.json" CHURR_SEALED_E2E_DATA="$WORK/data" \
  flutter test test/e2e/sealed_e2e_test.dart "$@" || { tail -30 "$WORK/server.log"; exit 1; }
