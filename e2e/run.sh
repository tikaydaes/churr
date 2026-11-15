#!/bin/sh
# Builds the server, starts it on a fresh data directory in -dev mode and
# drives the embedded pages in headless Chromium with a virtual passkey.
#
# First use: (cd e2e && npm install && npx playwright install chromium)
#
#   e2e/run.sh              the whole run
#   e2e/run.sh markdown     only the Markdown corpus (needs no server)
#   e2e/run.sh contrast     only the colour contrast of the token file
#   e2e/run.sh order        only where messages are shown (needs no server)
#   e2e/run.sh presence    only what the store does with presence events (needs no server)
#   e2e/run.sh broad       only what the composer does about @channel, @here and @everyone (needs no server)
#   e2e/run.sh commands    only the built-in commands' table, grammar, decks and reels (needs no server)
#   e2e/run.sh reconnect   only when the web chat reconnects, and its countdown (needs no server)
#   e2e/run.sh watch       only how the web chat says whose presence it is showing (needs no server)
#   e2e/run.sh open        only how the web chat says which conversations are on screen (needs no server)
#   e2e/run.sh palette      only the theme arithmetic against the Go table
#   e2e/run.sh hub-logic    only the logic of the hub screens (needs no server)
#   e2e/run.sh calls-logic  only the logic of calls in the web chat (needs no server)
#   e2e/run.sh calls        setup, then only calls in the browser
#   e2e/run.sh themes       setup, then only the theme flows in the browser
#   e2e/run.sh hub          setup, then only the hub screens against stubs
#   e2e/run.sh sealed-lib   only the sealed-chat library under Node: every
#                           vector, the transcript, the refusals (no server)
#   e2e/run.sh sealed       setup, then the library and sealed chats in the browser
#   e2e/run.sh call-frames  only call frame encryption against the shared vectors
#                           (testdata/call-frames; needs no server)
#   e2e/run.sh sealed-call  setup, then a call in a sealed chat between two
#                           browsers: media decrypts, a new epoch re-keys it,
#                           a device without the keys gets nothing
set -e
cd "$(dirname "$0")"
if [ "$1" = "markdown" ]; then
  exec node markdown.mjs
fi
if [ "$1" = "contrast" ]; then
  exec node contrast.mjs
fi
if [ "$1" = "order" ]; then
  exec node order.mjs
fi
if [ "$1" = "presence" ]; then
  exec node presence.mjs
fi
if [ "$1" = "commands" ]; then
  exec node commands.mjs
fi
if [ "$1" = "broad" ]; then
  exec node broad.mjs
fi
if [ "$1" = "reconnect" ]; then
  exec node reconnect.mjs
fi
if [ "$1" = "watch" ]; then
  exec node watch.mjs
fi
if [ "$1" = "open" ]; then
  exec node open.mjs
fi
if [ "$1" = "palette" ]; then
  exec node palette.mjs
fi
if [ "$1" = "hub-logic" ]; then
  exec node hub.mjs
fi
if [ "$1" = "sealed-lib" ]; then
  node sealed-vectors.mjs && node sealed-transcript.mjs && exec node sealed-refusals.mjs
fi
if [ "$1" = "calls-logic" ]; then
  exec node calls.mjs
fi
if [ "$1" = "call-frames" ]; then
  exec node call-frames.mjs
fi
tmp=$(mktemp -d)
trap 'kill $pid 2>/dev/null; rm -rf "$tmp"' EXIT
# The binary is given a name of its own so that somebody else's
# `pkill -f "churr serve"` on this machine does not stop this server too.
(cd .. && CGO_ENABLED=0 go build -o "$tmp/churr-e2e" ./cmd/churr)
port=${E2E_PORT:-8473}
# Calls need the media port. It listens on the loopback address only, and
# clients are told to send media there; Chromium is started with
# --allow-loopback-in-peer-connection for it (calls-web.mjs).
media=${E2E_MEDIA_PORT:-24977}
"$tmp/churr-e2e" serve -dev -http "localhost:$port" -media "127.0.0.1:$media" -media-address 127.0.0.1 -data "$tmp/data" >"$tmp/serve.out" 2>"$tmp/serve.log" &
pid=$!
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  url=$(grep -o "http://localhost:$port/setup/[A-Za-z0-9_-]*" "$tmp/serve.out" "$tmp/serve.log" 2>/dev/null | head -1 | sed 's/^[^:]*://')
  [ -n "$url" ] && break
  sleep 0.25
done
if [ -z "$url" ]; then
  echo "the server did not print a setup link:" >&2
  cat "$tmp/serve.out" "$tmp/serve.log" >&2
  exit 1
fi
rm -rf shots && mkdir shots
node contrast.mjs | tee "$tmp/out.log"
node markdown.mjs | tee -a "$tmp/out.log"
node order.mjs | tee -a "$tmp/out.log"
node presence.mjs | tee -a "$tmp/out.log"
node reconnect.mjs | tee -a "$tmp/out.log"
node broad.mjs | tee -a "$tmp/out.log"
node commands.mjs | tee -a "$tmp/out.log"
node watch.mjs | tee -a "$tmp/out.log"
node open.mjs | tee -a "$tmp/out.log"
node palette.mjs | tee -a "$tmp/out.log"
node hub.mjs | tee -a "$tmp/out.log"
node sealed-vectors.mjs | tee -a "$tmp/out.log"
node sealed-transcript.mjs | tee -a "$tmp/out.log"
node sealed-refusals.mjs | tee -a "$tmp/out.log"
node calls.mjs | tee -a "$tmp/out.log"
node call-frames.mjs | tee -a "$tmp/out.log"
node run.mjs "$url" "$@" | tee -a "$tmp/out.log"
if grep -q '^FAIL' "$tmp/out.log"; then
  # The server's log is kept beside the screenshots, for reading afterwards.
  cp "$tmp/serve.log" shots/serve.log
  kill -0 $pid 2>/dev/null || echo "the server is no longer running" >&2
  echo "--- server log (last 40 lines; all of it is in e2e/shots/serve.log) ---" >&2
  tail -40 "$tmp/serve.log" >&2
  exit 1
fi
