#!/bin/sh
# Posts to a Churr webhook with curl, in each of the ways docs/BOTS.md
# sections 9 to 14 describe.
#
#   CHURR_HOOK=http://localhost:8080/hooks/churr_hook_… examples/webhook.sh [what]
#
# what is one of: text (the default), card, file, slack, discord, github.
# For github, also set CHURR_HOOK_SECRET to the webhook's signing secret,
# which was shown beside the address when it was collected.
#
# To get an address: in a named channel you are in, ask for a webhook from
# the channel's menu, have an admin approve it, then collect the address.
# It is shown once.
set -eu

: "${CHURR_HOOK:?set CHURR_HOOK to the webhook address (…/hooks/churr_hook_…)}"
what=${1:-text}

case "$what" in
text)
	# Churr's own shape: the same body as posting a message. Mentions in
	# the text notify as they would from a member.
	curl -sS -X POST "$CHURR_HOOK" -H 'Content-Type: application/json' -d '{
	  "text": "Build **#482** failed on `main`."
	}'
	;;
card)
	# A card, and "via" to say which of several sources this came from.
	curl -sS -X POST "$CHURR_HOOK" -H 'Content-Type: application/json' -d '{
	  "cards": [{
	    "color": "red",
	    "title": "Build #482 failed",
	    "url": "https://ci.example/builds/482",
	    "text": "3 tests failing",
	    "fields": [{"label": "Branch", "value": "main", "short": true}],
	    "footer": "ci.example"
	  }],
	  "via": {"name": "CI"}
	}'
	;;
file)
	# A webhook cannot upload, so file bytes travel in the same request:
	# the JSON in a part named payload, files in parts file1 to file10.
	# The whole request is at most 1 MB.
	tmp=$(mktemp)
	trap 'rm -f "$tmp"' EXIT
	printf 'line one\nline two\n' >"$tmp"
	curl -sS -X POST "$CHURR_HOOK" \
		-F 'payload={"text": "The log is attached.", "files": [{"part": "file1", "name": "build.log"}]};type=application/json' \
		-F "file1=@$tmp;filename=build.log"
	;;
slack)
	# What a Slack incoming webhook accepts, on the address with /slack.
	# The answer is the plain text "ok", as Slack's is.
	curl -sS -X POST "$CHURR_HOOK/slack" -H 'Content-Type: application/json' -d '{
	  "text": "Deploy finished",
	  "username": "Jenkins",
	  "attachments": [{"color": "good", "title": "api 2.4.1", "title_link": "https://ci.example/7", "text": "*Passed* in 4m"}]
	}'
	;;
discord)
	# What a Discord webhook accepts, on the address with /discord.
	# ?wait=true asks for the message's id; without it the answer is 204.
	curl -sS -X POST "$CHURR_HOOK/discord?wait=true" -H 'Content-Type: application/json' -d '{
	  "content": "Build failed",
	  "username": "CI",
	  "embeds": [{"title": "Build #482", "url": "https://ci.example/482", "description": "3 tests failing", "color": 15158332}]
	}'
	;;
github)
	# A GitHub event, signed the way GitHub signs it: HMAC-SHA-256 over
	# the raw body with the webhook's signing secret as the key.
	: "${CHURR_HOOK_SECRET:?set CHURR_HOOK_SECRET to the signing secret}"
	body='{"zen": "Keep it logically awesome.", "repository": {"full_name": "acme/api"}, "sender": {"login": "sam"}}'
	sig=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$CHURR_HOOK_SECRET" | sed 's/^.*= *//')
	curl -sS -X POST "$CHURR_HOOK/github" -H 'Content-Type: application/json' \
		-H 'X-GitHub-Event: ping' -H "X-Hub-Signature-256: sha256=$sig" --data-binary "$body"
	;;
*)
	echo "usage: $0 [text|card|file|slack|discord|github]" >&2
	exit 2
	;;
esac
echo
