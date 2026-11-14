#!/bin/sh
# Runs the app's integration test against a real server on this machine:
# builds the Go server and tools/devseed, fills a fresh data directory,
# starts the server on a spare port, runs integration_test/ on macOS with
# the seed passed in, and stops the server.
#
#   tool/integration.sh            run the test
#   KEEP=1 tool/integration.sh     also leave the work folder (screenshots)
set -eu
APP="$(cd "$(dirname "$0")/.." && pwd)"
ROOT="$(cd "$APP/.." && pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/churr-it.XXXXXX")"
PORT="${PORT:-$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')}"

cleanup() {
  [ -n "${SERVER_PID:-}" ] && kill "$SERVER_PID" 2>/dev/null || true
  [ -n "${KEEP:-}" ] || rm -rf "$WORK"
}
trap cleanup EXIT

echo "== building the server and devseed"
(cd "$ROOT" && go build -o "$WORK/churr" ./cmd/churr && go build -o "$WORK/devseed" ./tools/devseed)

echo "== seeding $WORK/data on localhost:$PORT"
"$WORK/devseed" -data "$WORK/data" -http "localhost:$PORT" -o "$WORK/seed.json"

echo "== starting the server"
"$WORK/churr" serve -dev -data "$WORK/data" -http "localhost:$PORT" >"$WORK/server.log" 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 50); do
  curl -sf "http://localhost:$PORT/api/v1/server" >/dev/null && break
  sleep 0.2
done
curl -sf "http://localhost:$PORT/api/v1/server" >/dev/null || { cat "$WORK/server.log"; exit 1; }

echo "== running the integration test on macOS"
cd "$APP"
# A macOS window that is not in front is not sent frames, and the test
# waits on frames, so the app is brought forward once it is up and the
# app that was in front gets its place back afterwards.
FRONT="$(osascript -e 'tell application "System Events" to get name of first process whose frontmost is true' 2>/dev/null || true)"
(
  for _ in $(seq 1 600); do
    if pgrep -f 'Products/Debug/Churr.app' >/dev/null; then
      sleep 1
      osascript -e 'tell application "System Events" to set frontmost of (first process whose name is "Churr") to true' >/dev/null 2>&1 || true
      break
    fi
    sleep 0.5
  done
) &
flutter test integration_test/app_test.dart -d macos \
  --dart-define="CHURR_SEED=$(base64 <"$WORK/seed.json" | tr -d '\n')" 2>&1 | tee "$WORK/test.log"
STATUS=$(grep -c 'All tests passed' "$WORK/test.log" || true)
[ -z "$FRONT" ] || osascript -e "tell application \"$FRONT\" to activate" >/dev/null 2>&1 || true

# The test writes its screenshots inside the app's sandbox; copy them out.
if [ -n "${SHOTS:-}" ]; then
  mkdir -p "$SHOTS"
  grep -o 'SCREENSHOT .*' "$WORK/test.log" | while read -r _ path; do cp "$path" "$SHOTS/" 2>/dev/null || true; done
  echo "== screenshots in $SHOTS"
fi
[ "$STATUS" -ge 1 ]
