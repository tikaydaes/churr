#!/bin/sh
# Starts a throwaway server for looking at the pages by hand, and prints its
# setup link. The data directory is removed when the script is stopped.
#   e2e/dev.sh [port]
set -e
cd "$(dirname "$0")"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
# The binary is given a name of its own so that somebody else's
# `pkill -f "churr serve"` on this machine does not stop this server too.
(cd .. && CGO_ENABLED=0 go build -o "$tmp/churr-e2e" ./cmd/churr)
exec "$tmp/churr-e2e" serve -dev -http "localhost:${1:-8474}" -media off -data "$tmp/data"
