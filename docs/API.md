# Using the Churr API

A guide to the HTTP API of a Churr server. The apps, the web chat,
bots and scripts all use this one API; nothing is reachable only
through one client.

- `openapi.yaml` lists every endpoint with its request, response,
  who may call it and the errors it returns. This guide names
  operations by their `operationId`, for example `postMessage`.
- `CONVENTIONS.md` has the rules for identifiers, times, names,
  errors, paging and limits.
- `MESSAGE-FORMAT.md` defines the message object and its parts.
- `EVENTS.md` describes the live connection and every event.

The examples use a server at `chat.example.org`.

## 1. Authenticating

Every request is authenticated one of two ways: the `churr_session`
cookie, or `Authorization: Bearer <token>`. Which one a client uses
depends on what it is.

| Client             | Credential                          | How it gets it                          |
|--------------------|-------------------------------------|-----------------------------------------|
| Web pages and web chat | Cookie, `churr_ses_…`           | Signing in on the server's own pages    |
| Apps               | Access token, `churr_app_…`, with a refresh token, `churr_ref_…` | OAuth in the system browser, or QR pairing |
| A person's script  | Personal token, `churr_pat_…`       | Made on the account page (`createToken`) |
| Bot                | Bot token, `churr_bot_…`            | Made by an admin (`createBot`)          |
| Webhook            | Secret in the address, `churr_hook_…` | Requested by a member, approved by an admin |
| Server integration | Integration token, `churr_int_…`    | Made by an admin (`createIntegration`)  |

The prefix says what a token is, so a leaked one can be recognised.
The server stores only a hash of each.

### Web pages

`AUTH.md` is the authority on signing in. This is the outline.

The sign-in operations set the cookie. It is HttpOnly, Secure and
SameSite=Strict. A request that carries the cookie and is not a
`GET` or `HEAD` must come from the server's own origin, and so must
the live connection. A session gets a new token each time it changes
state.

A passkey sign-in is two calls:

1. `beginPasskeyLogin` returns `ceremony_id` and `public_key`, the
   WebAuthn request options. Pass `public_key` to the browser's
   passkey API as it is. No username is asked for.
2. `finishPasskeyLogin` takes `ceremony_id` and the credential the
   browser returned.

Registration is the same pair, `beginPasskeyRegistration` and
`finishPasskeyRegistration`, with an invite code unless registration
is open. `checkInvite` looks at a code without using it up. The
answer to the first passkey of an account includes eight recovery
codes, shown once.

Every passkey creation asks for the PRF extension, because that is
what later unlocks sealed chats and it cannot be switched on
afterwards. The server never sees a PRF value: the page removes it
from the credential before sending, and reports only whether the
browser said PRF is enabled (`prf_enabled`). The page then gives the
passkey's unlock public key with `setPasskeyUnlockKey`
(SEALED.md).

The other ways in:

- **Company sign-in.** Send the browser to `startSso`. The provider
  sends it back to `finishOidcSso` or `finishSamlSso`, which answer
  with a small page of the server's own that carries the cookie on.
- **Recovery.** `recoverWithCode` takes a username and a recovery
  code; `recoverWithLink` takes the value from a recovery link. Both
  give a recovery session: one hour, and able to do little but make
  a passkey with `beginAddPasskey` and `finishAddPasskey`, which
  replaces it with an ordinary session. Where the person's role must
  use company sign-in, the recovery session links an identity
  instead, with `startSso` and `purpose=link`.
- **A second factor.** When the policy requires one, a company
  sign-in leaves the session pending for ten minutes. Until the
  second step is done every other call, the live connection
  included, answers 401 `second_factor_required`. `getSecondFactor`
  lists the proofs the session may give. Give one with
  `beginPasskeyCheck` and `finishPasskeyCheck`, or with `checkTotp`.
  A person with nothing set up enrols a passkey or an authenticator
  app (`beginTotp`, `confirmTotp`) from the pending session, and
  that counts as the proof. A passkey sign-in is never asked for a
  second factor.
- **First run.** The setup link printed when the server starts
  carries a value for `beginSetup` and `finishSetup`, which make the
  owner. Until the owner exists every other operation but
  `getServer` answers `unavailable`.

`getServer` needs no sign-in and tells a client which of these the
server offers.

**The fresh check.** Some actions are worth more to a thief than a
session is: adding or removing a passkey, replacing recovery codes,
removing the authenticator app, making a personal token, linking a
company identity, starting a QR pairing, and for admins changing
sign-in settings or providers, making recovery links and making bot
or integration tokens. They answer 403 `fresh_check_required` unless
the session signed in, or passed a check, in the last ten minutes.
The client runs `beginPasskeyCheck` and `finishPasskeyCheck`, or
`checkTotp`, and tries again. Each operation that needs one says so.

### Apps

Apps never handle passkeys or company sign-in themselves. They use
OAuth 2.0 authorization code with PKCE against the server's own
address, in the system browser, so every sign-in method works
without the app knowing about it.

1. Make a random `code_verifier` and its S256 `code_challenge`.
2. Open the system browser at:

       https://chat.example.org/oauth/authorize?response_type=code
         &client_id=churr-app
         &redirect_uri=http%3A%2F%2F127.0.0.1%3A49152%2Fcallback
         &code_challenge=…&code_challenge_method=S256&state=…
         &device_name=Kai%27s%20laptop&platform=macos

   `client_id` is a label and is given no trust: apps are not
   registered. What protects the code is the redirect address, which
   must be a loopback address by number (`127.0.0.1` or `[::1]`, any
   port) or the apps' own address scheme on phones, and PKCE. An app
   that will use sealed chats also sends `box_key`, its device box
   public key.
3. The person signs in and approves the device by name. The browser
   returns to the redirect address with `code`, `state` and `iss`.
4. Exchange the code:

       curl https://chat.example.org/oauth/token \
         -d grant_type=authorization_code \
         -d code=… -d code_verifier=… \
         -d redirect_uri=http://127.0.0.1:49152/callback

   The answer is `access_token`, `refresh_token` and `expires_in`.
   If the person signed in with a passkey that can unlock sealed
   chats and the app sent `box_key`, it also carries `handover`: the
   backup key sealed to the app, which the server cannot read.

An access token lasts one hour. Use `grant_type=refresh_token` to
get a new pair; each refresh token works once. After a passkey
sign-in the session slides 30 days ahead with each use. After a
company sign-in it ends 24 hours after that sign-in, and the person
goes through the browser again.

`/oauth/token` and `/oauth/revoke` use the standard OAuth error
shape (`{"error": "invalid_grant"}`), not Churr's. They are the only
endpoints that do. `getOAuthMetadata` describes the endpoints.

**QR pairing** is the second way in, for a phone beside a browser
that is already signed in.

1. The web page calls `startPairing`, which needs a fresh check, and
   shows the server's address and the code as a QR code.
2. The app scans it and posts to `/oauth/token` with
   `grant_type=urn:churr:params:oauth:grant-type:pairing-code`, the
   `pairing_code`, a `device_name` and a `platform`. The answer is
   `authorization_pending`. The app asks again every two seconds.
3. The web page sees the claim with `getPairing`, shows "A device
   called … wants to sign in" and asks the person. `confirmPairing`
   approves it. This step cannot be skipped.
4. The app's next request returns tokens.

After signing in, and again on every start, an app registers its
device with `registerDevice`: the push address for wake-ups and, for
sealed chats, its `sign_key` and `box_key`. The answer carries the
`wake_route` the phone uses to tell which server a wake-up came
from. When woken, the phone calls `fetchWakeUp` with the identifier
the wake-up carried (PUSH.md). An app whose session ended by itself
signs in again and takes its device back with `beginReattachDevice`
and `finishReattachDevice`, so it keeps its place in sealed chats.

### Personal tokens

An owner, admin or member makes a token on their account page or
with `createToken`, after a fresh check. Guests cannot. It acts as
them: the same conversations, the same level in each. Use it for
scripts, for example one that sets your own status.

A personal token cannot do anything that needs a fresh check, cannot
touch passkeys, recovery codes, the authenticator app, identities,
sessions or other tokens, and cannot use sealed chats, since a
script holds no device keys. Where company sign-in is required for
the account's role, the token stops working 30 days after the
account's last company sign-in.

### Bots, webhooks and integrations

- A **bot** is an account with role `bot` and one or more tokens. It
  uses this API and the live connection like any client. Its reach
  is the channels it has been added to, at its level in each.
  Section 8 walks through starting one.
- A **webhook** is an address that posts into one channel and can
  read nothing. Whoever holds the address may post to it. Section 9.
- A **server integration** token works only on the endpoints under
  `/api/v1/integration/`, and no other token works there. It is in
  no conversation and can read no messages. The one thing it can be
  allowed to do is set availability, for a calendar integration:

      curl -X PUT https://chat.example.org/api/v1/integration/accounts/$ACCOUNT/availability \
        -H "Authorization: Bearer churr_int_…" \
        -H "Content-Type: application/json" \
        -d '{"availability": "meeting", "until": "2026-11-13T11:00:00.000Z"}'

  Sending `"availability": null` clears it, but only if this same
  integration set it.

  `findAccountForIntegration` finds `$ACCOUNT` by username or by the
  person's identifier at a company sign-in provider.

## 2. A first request

With a personal token in `$TOKEN`:

    curl https://chat.example.org/api/v1/me \
      -H "Authorization: Bearer $TOKEN"

```json
{
  "account": {
    "id": "0193a000-7c1e-7b3a-9f0e-2a4d5c6b7e8f",
    "username": "kai",
    "role": "member",
    "display_name": "Kai",
    "presence": "online",
    "created_at": "2026-11-13T09:00:00.000Z",
    "prefs": {"thread_view": "threaded", "meeting_pauses_notifications": false, "allow_notify_anyway": true},
    "dnd_state": {"on": false, "manual": false, "quiet": false, "from_status": false, "from_meeting": false},
    "passkey_count": 1,
    "has_totp": false
  }
}
```

List the conversations you are in, then read one:

    curl "https://chat.example.org/api/v1/conversations?limit=20" \
      -H "Authorization: Bearer $TOKEN"

    curl "https://chat.example.org/api/v1/conversations/$CONVERSATION/messages?limit=20" \
      -H "Authorization: Bearer $TOKEN"

A failure always has the same shape:

    HTTP/1.1 403 Forbidden
    {"error": {"code": "forbidden", "message": "You are not in this channel."}}

Act on `code`. Show `message` if you like. A thing that exists but
that you may not know exists answers 404 `not_found`, the same as a
thing that does not exist.

## 3. Conventions in brief

`CONVENTIONS.md` is the rule book and this is only a reminder of
what trips people up.

- Field names are `snake_case`. Identifiers are UUIDv7 strings.
  Times are RFC 3339 in UTC with milliseconds. Durations are whole
  seconds in fields ending `_seconds`.
- Absent and `null` mean the same, and the server omits `null`
  fields. Do not rely on a key being present.
- Ignore fields you do not know. The server does the opposite: an
  unknown field in a request is refused with `invalid_request`.
- A response is an object naming what it holds (`{"message": {…}}`),
  never a bare list.
- `GET` reads, `POST` creates or acts, `PATCH` changes some fields,
  `PUT` sets a whole value or turns something on, `DELETE` removes
  or turns off.
- Order inside a conversation is by `seq`, never by identifier or
  time.
- "Admin" includes the owner.

### Protocol Buffers instead of JSON

Every JSON body of this API can travel as Protocol Buffers, for any
client that asks: an app, a bot, a script. JSON is the default and
nothing about it changes.

    Accept: application/x-protobuf; v=1
    Content-Type: application/x-protobuf; v=1

- `Accept` gets the answer, and any error, as the operation's message
  from `proto/churr/v1/churr.proto`: `GetMeResponse`, `Error`. An
  `Accept` the server does not know gets JSON.
- `Content-Type` sends the body as the operation's request message:
  `PostMessageRequest`. Either header works without the other.
- Read an answer by its `Content-Type`. The server answers in JSON
  when a body has no exact protobuf form, which should be rare and is
  never an error.
- Identifiers are 16 bytes in protobuf and hyphenated hex text in
  JSON, paths and query parameters. Times are microseconds.
- `v` is the schema version the client speaks; the answer names the
  one used, the lower of the two.
- **Protobuf is never sent uncompressed.** A request that asks for a
  protobuf answer must also say `Accept-Encoding: gzip` (most HTTP
  libraries do unasked), and every protobuf answer, errors and the
  smallest ones included, comes with `Content-Encoding: gzip`. gzip is
  the one compression the server has. A request for protobuf that does
  not accept gzip is answered 406 with a JSON error and is not carried
  out. A protobuf request body may be sent with
  `Content-Encoding: gzip`; the body limit is on what it unpacks to.
  JSON answers are not compressed by the server, as before.

`openapi.yaml` names the message beside each body
(`x-protobuf-message`). `WIRE-PROTOBUF.md` has the mapping rules and
how the schema may change.

## 4. Paging

There are two kinds, and an endpoint uses one or the other.

**By sequence number**, for messages and events. Send `before` or
`after` (a `seq`) and `limit`. The answer has `has_more`.

    # the newest 50
    GET /api/v1/conversations/{id}/messages

    # the 50 before what you have
    GET /api/v1/conversations/{id}/messages?before=4771

    # everything since you last looked
    GET /api/v1/conversations/{id}/messages?after=4821

`listMessages` also takes `around` to open a conversation at one
message, as when following a search result or a link.

**By cursor**, for everything else. Send `limit`, and the
`next_cursor` of the page before as `cursor`. When there is no
`next_cursor` you have everything. A cursor is opaque; do not build
or keep one.

`limit` defaults to 50 and is capped at 200.

Filters by time are always `since` and `until`, as in `search`,
which pages by cursor. `listEvents` takes `after` and `until` as
sequence numbers, so a client can fetch exactly the gap it is
missing.

## 5. Sending a message once

Networks fail after the server has acted and before the client
hears. To make a retry safe, give every new message a `client_id`,
a UUID the client makes:

    curl https://chat.example.org/api/v1/conversations/$CONVERSATION/messages \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"client_id": "0193c000-1111-7222-8333-444455556666", "text": "Deploy at 5. @sam can you check <#0193a0c7-5b2e-7c11-8d04-6f1a2b3c4d5e>?"}'

The first time, the answer is 201 with the new message. Sending the
same `client_id` again in the same conversation within 24 hours
answers 200 with that same message and posts nothing.

A client that drew the message before the answer came (its own
outgoing copy, not a field of the message) can match it two ways: by
the response, or by `client_id` on the `message.created` event its
own connections receive.

The answer may carry `cannot_read_ids`: accounts the text mentions
that cannot read this conversation, so the client can tell the
author they will not be notified. It says nothing else about anyone.

A sender who sees the do not disturb mark on someone may push that
one message through with `notifyAnyway`, one person at a time. It
always answers as if it worked. If the person would not have been
notified anyway (their mode for the channel is `nothing`, they blocked the sender,
turned the feature off), the server quietly does nothing, and
nothing in the answer says so.

A channel is linked by its identifier, never by its name: write
`<#conversation-id>` in the text, as above. The server turns each
one that names a channel the author can see into a `conversation`
entity covering the whole `<#…>`, and clients draw it as the
channel's current name. `#ops` typed as plain characters is plain
text and is never resolved. Bots, webhooks and scripts link a
channel the same way.

To reply in a thread, add `parent_id`. To quote, add `quote` with
the message quoted and, for part of it, `start` and `end`. Text is
Churr Markdown as typed; the server works out the entities and
returns them.

Things that can stop a post: `forbidden` (not a member, or a
reader), `archived`, `timed_out`, `slow_mode` (with
`retry_after_seconds`), `blocked_by_filter`, `too_large`, and
`conflict` for a reply to a thread that was moved to a channel.

## 6. Uploading and attaching files

A file is uploaded to a conversation first and attached by its
identifier when the message is posted.

    curl https://chat.example.org/api/v1/conversations/$CONVERSATION/files \
      -H "Authorization: Bearer $TOKEN" \
      -F file=@floorplan.png \
      -F caption="Second floor"

```json
{"file": {"id": "0193a700-…", "name": "floorplan.png", "size": 482113,
          "media_type": "image/png", "kind": "image", "width": 1600, "height": 900,
          "caption": "Second floor",
          "url": "/api/v1/files/0193a700-…/floorplan.png",
          "thumb_url": "/api/v1/files/0193a700-…/thumb"}}
```

Then post, naming up to ten files in the order they should show:

    curl https://chat.example.org/api/v1/conversations/$CONVERSATION/messages \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"client_id": "…", "text": "Here is the plan", "file_ids": ["0193a700-…"]}'

- The server decides `kind` and `media_type` from the content, not
  from the name or what the client claimed.
- An upload that is never attached is deleted after 24 hours.
- The size limit is in `getServer` under `limits.file_size`. Past it
  the upload answers 413 `too_large`.
- Download from `url` with the same credential. Permission is
  checked each time, so a link to a file works only for someone who
  can read its conversation.
- The server makes no poster frame for a video. Send one image as
  the `poster` part of the upload and it comes back as `poster_url`.
- In a conversation shared with other servers, a file from another
  server has a `state` and only gets a `url` once it is `ready`.
- In a sealed chat the client encrypts the file before uploading it,
  and the key and real name travel inside the encrypted message.

## 7. Posting a poll

A poll is a message with a form: one `choice` field and the `tally`
action (MESSAGE-FORMAT.md section 7).

    curl https://chat.example.org/api/v1/conversations/$CONVERSATION/messages \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{
        "client_id": "…",
        "form": {
          "preset": "poll",
          "title": "Lunch on Friday?",
          "fields": [{
            "id": "choice", "type": "choice", "label": "Where", "required": true,
            "options": [{"id": "a", "label": "Noodles"}, {"id": "b", "label": "Tacos"}]
          }],
          "action": {"type": "tally"},
          "anonymous": false,
          "results": "live",
          "closes_at": "2026-11-14T02:00:00.000Z"
        }
      }'

The answer is the message, with the form given an `id`,
`answer_count` of 0 and a `fallback` the server wrote for clients
that cannot draw forms.

To vote:

    curl -X PUT https://chat.example.org/api/v1/messages/$MESSAGE/form/answer \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"answers": {"choice": ["a"]}}'

- Voting again changes the answer. `withdrawFormAnswer` takes it
  back.
- Any member of the conversation may vote, readers included. Someone
  reading a public channel they have not joined joins first. A form
  with `answerer_group_id` takes answers only from that group.
- Every member receives `form.answered` with the new counts, when
  `results` lets them see counts.
- On an anonymous poll nobody is shown who chose what, through any
  endpoint, admins included.
- `closeForm` ends it: the author, a channel manager or an admin.
- In a conversation shared with other servers, polls (`tally`) are
  the only forms allowed.
- The other presets (`rsvp`, `signup`, `approve`) are forms too and
  work the same way. `listFormResults` and `downloadFormResults`
  read the answers of a `collect` form.

## 8. How a bot gets started

1. **An admin creates it** with `createBot` (or in the admin
   console) and receives its token, once.
2. **Someone adds it to channels.** A bot cannot browse or join by
   itself. A channel's managers add it with `addMembers`, at
   `reader`, `member` or `manager`. That is all the permission there
   is: to let a bot post somewhere, add it; to stop it, remove it.
3. **The bot checks who it is**:

       curl https://chat.example.org/api/v1/me \
         -H "Authorization: Bearer churr_bot_…"

4. **It opens the live connection** at `/api/v1/events` with the
   same header and the subprotocol `churr.v1` (or `churr.v1+proto.1`
   with compression, for protobuf frames), answers `hello` with
   `resume`, and from then on receives the events of its channels
   (EVENTS.md). It needs no public address; the server never calls
   out to a bot.
5. **It posts** with `postMessage` like anyone. A bot may also send
   up to five cards, and forms with the `bot` action.
6. **It sets its commands** with `setCommands`. The list replaces
   the one it had, so a bot can send the same list every time it
   starts:

       curl -X PUT https://chat.example.org/api/v1/commands \
         -H "Authorization: Bearer churr_bot_…" \
         -H "Content-Type: application/json" \
         -d '{"commands": [{"name": "deploy", "description": "Deploy a service", "hint": "<service> to <environment>"}]}'

   In channels the bot is in, people now see `/deploy`. Running it
   posts nothing. The bot receives `command.invoked` with an
   `interaction_id`.
7. **It responds** with `respondToInteraction`, for everyone
   (`message`) or only for the person who asked (`private`):

       curl https://chat.example.org/api/v1/interactions/$INTERACTION/responses \
         -H "Authorization: Bearer churr_bot_…" \
         -H "Content-Type: application/json" \
         -d '{"type": "private", "text": "Deploying `api` to staging."}'

   Or it asks for more with a `dialog` response, which shows that one
   person a form and sends the bot the answer. `none` shows nothing
   and tells the person's client to stop waiting.

A bot that is not connected cannot receive a command or a `bot`
form answer. The person's call fails with `unavailable` and nothing
is queued, so a bot should reconnect promptly.

What a bot cannot do: be an admin, be in a sealed chat, answer a
form, use notify anyway, or see anything outside its channels.
`BOTS.md` has the whole of it.

## 9. Webhooks

The smallest bot, and only for named channels. A member asks for one
with `requestWebhook`; an admin approves it with `approveWebhook`;
the member collects the address, once, with `collectWebhookAddress`.
The answer holds the address and a signing secret, and neither is
shown again. `resetWebhookAddress` replaces both.

    curl https://chat.example.org/hooks/churr_hook_… \
      -H "Content-Type: application/json" \
      -d '{"text": "Build #482 failed", "cards": [{"color": "red", "title": "Build #482 failed", "url": "https://ci.example/builds/482"}]}'

Put `/slack`, `/discord` or `/github` on the end and the same
address takes the payload those services send, so a tool that can
post to Slack or Discord can post to Churr by changing the address.
Attachments and embeds become cards. Layout with no equivalent
becomes text. GitHub is given the signing secret, and each delivery
is checked against it. The token in the address is never the
signing secret.

The answer names the new message (`id`, `seq`, `created_at`) and
says nothing else about the channel. Bodies are limited to 1 MB. To
send files, post multipart with the JSON in a part named `payload`
and the files in parts named `file1` to `file10`. `BOTS.md` has the
payload rules for all four addresses.

## 10. Rate limits

Every response to an authenticated request carries three headers:

    RateLimit-Limit: 120
    RateLimit-Remaining: 117
    RateLimit-Reset: 42

`RateLimit-Reset` is seconds until the window resets. The numbers
are the server's and may differ by endpoint; read them, do not
assume them.

| Who                         | Counted per |
|-----------------------------|-------------|
| People and bots             | Account     |
| Webhooks and integrations   | Token       |
| Sign-up, sign-in, recovery  | Address     |

Past a limit the answer is 429:

    {"error": {"code": "rate_limited", "message": "Slow down.", "retry_after_seconds": 12}}

Wait `retry_after_seconds` and try again. `slow_mode` is the same
status and shape, and means the channel lets each person post once
per interval.

Posting, uploads, webhook posts and sign-in each have a tighter
limit of their own. The defaults for bots and webhooks are in
`BOTS.md` section 8. `getStatusSummary` is made to be called often:
it carries an `ETag`, and `If-None-Match` gets a 304 when nothing
changed.

## 11. Areas of the API

Every operation, by area. The names are the `operationId`s in
`openapi.yaml`. Both that file and this table are written by
`tools/openapi-gen/build.py`.

<!-- areas:begin -->
| Area | Operations |
|------|------------|
| Server | `getServer`, `getWellKnown`, `beginSetup`, `finishSetup`, `setupWithAccessKey` |
| Sign-in | `checkInvite`, `beginPasskeyRegistration`, `finishPasskeyRegistration`, `registerWithAccessKey`, `signInWithAccessKey`, `beginPasskeyLogin`, `finishPasskeyLogin`, `startSso`, `finishOidcSso`, `finishSamlSso`, `getSamlMetadata`, `getSecondFactor`, `logout`, `recoverWithCode`, `recoverWithLink`, `beginPasskeyCheck`, `finishPasskeyCheck`, `checkAccessKey`, `checkTotp`, `lockSession`, `beginSessionUnlock`, `getSessionUnlockOptions`, `finishSessionUnlock`, `finishSessionUnlockWithAccessKey` |
| OAuth | `getOAuthMetadata`, `authorize`, `exchangeToken`, `revokeOAuthToken`, `startPairing`, `getPairing`, `confirmPairing`, `openUnlockPage` |
| Me | `getMe`, `updateMe`, `setMyAvatar`, `removeMyAvatar`, `getPrefs`, `updatePrefs`, `listPasskeys`, `beginAddPasskey`, `finishAddPasskey`, `renamePasskey`, `deletePasskey`, `getAccessKey`, `setAccessKey`, `removeAccessKey`, `getRecoveryCodes`, `regenerateRecoveryCodes`, `getTotp`, `beginTotp`, `confirmTotp`, `removeTotp`, `listIdentities`, `listSessions`, `endSession`, `endOtherSessions`, `listTokens`, `createToken`, `revokePersonalToken`, `listDevices`, `registerDevice`, `removeDevice`, `getStatusSummary`, `fetchWakeUp`, `setPasskeyUnlockKey`, `beginReattachDevice`, `finishReattachDevice` |
| Status | `setStatus`, `clearStatus`, `setAvailability`, `clearAvailability`, `setDnd`, `clearDnd`, `getQuietHours`, `setQuietHours`, `removeQuietHours`, `skipQuietHours`, `unskipQuietHours`, `setAway`, `clearAway`, `listStatusPresets`, `createStatusPreset`, `updateStatusPreset`, `deleteStatusPreset` |
| Accounts | `listAccounts`, `getAccount`, `listGroups`, `getGroup`, `listGroupMembers`, `joinGroup`, `leaveGroup`, `listBlocks`, `blockAccount`, `unblockAccount`, `listChatRequests`, `acceptChatRequest`, `declineChatRequest`, `dismissChatRequest`, `blockChatRequester`, `watchAccount`, `unwatchAccount` |
| Conversations | `listConversations`, `getConversation`, `browseChannels`, `listSections`, `listReferencePatterns`, `createReferencePattern`, `updateReferencePattern`, `deleteReferencePattern`, `createChannel`, `openChat`, `openSealedChat`, `updateConversation`, `joinConversation`, `leaveConversation`, `closeChat`, `withdrawChatRequest`, `archiveConversation`, `unarchiveConversation`, `listMembers`, `addMembers`, `setMemberLevel`, `setMemberMayPost`, `removeMember`, `listChannelBans`, `banFromChannel`, `liftChannelBan`, `knock`, `listKnocks`, `answerKnock`, `listChannelGroups`, `addChannelGroup`, `removeChannelGroup`, `setReadPosition`, `updateMembership`, `checkBroadMentions`, `sendTyping`, `listPins`, `pinMessage`, `unpinMessage`, `listChannelTimeouts`, `timeOutMember`, `liftChannelTimeout`, `handleReport`, `lockConversation`, `listReports` |
| Messages | `listMessages`, `postMessage`, `getMessage`, `lookupCodePoints`, `getThread`, `editMessage`, `deleteMessage`, `getMessageHistory`, `purgeMessage`, `listReactions`, `addReaction`, `removeReaction`, `setChecklistItem`, `getChecklistItemHistory`, `removeLinkPreview`, `notifyAnyway`, `reportMessage`, `addBookmark`, `removeBookmark`, `listBookmarks`, `moveThreadToChannel` |
| Forms | `answerForm`, `withdrawFormAnswer`, `pressFormButton`, `addFormOption`, `closeForm`, `listFormResults`, `downloadFormResults` |
| Threads | `listThreads`, `setThreadState`, `setThreadReadPosition` |
| Files | `uploadFile`, `downloadFile`, `downloadThumbnail`, `listEmoji`, `createEmoji`, `deleteEmoji`, `downloadPoster` |
| Search | `search` |
| Events | `openEvents`, `listEvents` |
| Scheduled | `listScheduled`, `createScheduled`, `getScheduled`, `updateScheduled`, `cancelScheduled`, `sendScheduledNow` |
| Commands | `listCommands`, `runCommand`, `listSavedReplies`, `createSavedReply`, `updateSavedReply`, `deleteSavedReply` |
| Bots | `submitDialog`, `dismissDialog`, `requestWebhook`, `listChannelWebhooks`, `approveWebhook`, `declineWebhook`, `revokeWebhook`, `setCommands`, `respondToInteraction`, `collectWebhookAddress`, `resetWebhookAddress` |
| Webhook intake | `postWebhook`, `postSlackWebhook`, `postDiscordWebhook`, `postGithubWebhook` |
| Calls | `getCall`, `joinCall`, `leaveCall` |
| Server integrations | `findAccountForIntegration`, `setAvailabilityForAccount` |
| Sealed chats | `listDeviceListVersions`, `publishDeviceList`, `postEpochRecord`, `listEpochRecords`, `getBackup`, `createBackup`, `addLockedCopy`, `putAccountRecord`, `deleteLockedCopy`, `listBackupRecords`, `putBackupRecord`, `replaceBackup`, `resetBackup`, `createUnlockRequest`, `listUnlockRequests`, `getUnlockRequest`, `answerUnlockRequest` |
| Admin: settings and sign-in | `getSettings`, `updateSettings`, `listSsoProviders`, `createSsoProvider`, `getSsoProvider`, `updateSsoProvider`, `deleteSsoProvider`, `testObjectStorage`, `setServerIcon`, `testSsoProvider` |
| Admin: people | `listInvites`, `createInvite`, `revokeInvite`, `adminListAccounts`, `adminGetAccount`, `setAccountRole`, `removeAccount`, `createRecoveryLink`, `adminRemoveTotp`, `listBans`, `createBan`, `liftBan`, `sendBroadcast`, `clearBroadcast`, `listAllChannelBans`, `listTimeouts`, `createTimeout`, `liftTimeout`, `createGroup`, `updateGroup`, `deleteGroup`, `addGroupMember`, `removeGroupMember`, `createClaimLink`, `signOutAccount`, `signOutEveryone` |
| Admin: channels and content | `createSection`, `updateSection`, `deleteSection`, `adminListChannels`, `adminGetChannel`, `adminUpdateChannel`, `appointManager`, `adminArchiveChannel`, `adminLockChannel`, `adminListEmoji`, `renameEmoji`, `adminDeleteEmoji`, `createCommunityReply`, `updateCommunityReply`, `deleteCommunityReply`, `createCommunityPreset`, `updateCommunityPreset`, `deleteCommunityPreset`, `listWordFilters`, `createWordFilter`, `updateWordFilter`, `deleteWordFilter`, `listPurgeRequests`, `decidePurgeRequest`, `adminUnarchiveChannel` |
| Admin: bots and integrations | `listBots`, `createBot`, `getBot`, `updateBot`, `setBotAvatar`, `deleteBot`, `listBotTokens`, `createBotToken`, `revokeBotToken`, `listWebhooks`, `listIntegrations`, `createIntegration`, `revokeIntegration` |
| Admin: linked servers | `listPeers`, `createPeerInvitation`, `acceptPeerInvitation`, `getPeer`, `updatePeer`, `removePeer`, `listKnownServers`, `blockServer`, `unblockServer`, `listStreams`, `shareChannel`, `acceptStream`, `declineStream`, `leaveStream`, `addStreamServer`, `removeStreamServer`, `transferStream`, `inspectPeerInvitation`, `updateStream`, `closeStream`, `listHubDirectory`, `joinHubChannel`, `proposeStreamMove`, `listStreamMoves`, `acceptStreamMove`, `declineStreamMove`, `cancelStreamMove` |
| Channels on a hub | `getHubChannel`, `moderateHubChannel` |
| Admin: records | `listAuditLog`, `startExport`, `getExport`, `downloadExport`, `getOverview`, `listLiveCalls`, `testCalls` |
| Themes | `listMyThemes`, `createTheme`, `getTheme`, `updateTheme`, `deleteTheme`, `publishTheme`, `unpublishTheme`, `listThemeLibrary`, `addTheme`, `removeLibraryTheme`, `setDefaultTheme` |
<!-- areas:end -->

## 12. Choices this reference made

`AUTH.md`, `SEALED.md`, `BOTS.md`, `CALLS.md`, `PUSH.md`,
`PERMISSIONS.md`, `FEATURES.md` and `LINK-PROTOCOL.md` decide what
happens in their areas, and this reference follows them. What is
left is where things live and what they are called.

- **Setup** has its own pair, `beginSetup` and `finishSetup`.
- **Checks.** One pair of passkey operations and one for an
  authenticator code serve both the second factor of a pending
  session and the fresh check of a signed-in one. They live at
  `/checks/…`; DESIGN.md section 7 wrote `totp`.
- **Recovery** is two operations, `/recovery/code` and
  `/recovery/link`.
- **Sign-in policy** is not a separate object. Its keys are
  settings: `registration.mode`, `invites.members_may_invite`,
  `auth.member_sign_in`, `auth.second_factor` and
  `auth.second_factor_accepted`. `getSettings` and `updateSettings`
  use the dotted keys of OPERATIONS.md section 5 as they are.
- **Pairing** is claimed through the token endpoint, as AUTH.md
  says; there is no separate claim operation.
- **Unlocking an app with a passkey after sign-in** goes through
  the page at `/oauth/unlock`, which leaves a locked copy of kind
  `device` for the app to fetch with `getBackup`.
- **Devices.** One per session, registered with `registerDevice`.
- **Mode and show.** `mode` (`any`, `mentions`, `nothing`) is what
  notifies; `show` (`always`, `activity`, `notification`, `hidden`,
  or null for the app's default) is whether the conversation is in
  the sidebar. They are independent: a hidden conversation still
  notifies and counts in badges and in `getStatusSummary`. The
  server does not work out sidebar visibility. Mute is `muted_until`
  beside `mode`, not a mode of its own.
- **Do not disturb** is reported as a list of the sources that are
  on.
- **Channel names** are free-form (1 to 64 characters, any script,
  spaces and capitals), are not unique and are never refused with
  `name_taken`. Channels are identified by `id` everywhere, and
  linked in text as `<#conversation-id>`.
- **Sub-channels** are a `parent_id` on the conversation, one level
  deep, set with `createChannel`, `updateConversation` or
  `adminUpdateChannel` and filtered on with `browseChannels`. A
  channel made from a thread or a ticket form is a sub-channel.
- **Locking** is its own operation, `lockConversation`. Making a
  public channel private is `updateConversation` with
  `private: true`.
- **Reports** are one list, `listReports`, filtered to what the
  caller may handle. The admin console uses the same one.
- **Invites** live at `/invites`, not under `/admin`, because
  members may make them when the setting allows.
- **Commands.** `setCommands` is a `PUT` at `/commands`, since it
  sets a whole list. DESIGN.md section 7 wrote `POST commands`.
- **Threads.** `listMessages` returns replies among top-level
  messages unless `top_level` is set. The reader's thread state
  comes beside the messages as `thread_states`.
- **Unsent messages.** A message this server posts in a shared
  conversation is stored at once with its `seq` and `order`, shown in
  its place, and announced by `message.created` like any other. Until
  the conversation's time reference acknowledges it, it carries
  `unsent: true`; `message.sent` (stored, so it also reaches a client
  on catch-up) says when that ends, and `message.placed` if its
  `order` had to change. There is no `pending` field and no
  `message.queued` or `message.unqueued` event.
- **Link previews** are removed by their position in `cards`.
- **Retention** sends no events. Clients drop old messages by the
  same period.
- **Sealed chats** have no frames on the live connection. Epoch
  records and unlock requests are operations.
- **Group colours** are the seven card colours.
- **Adding a group to a channel** sends one event, not one per
  person.
- **Roles.** Setting `owner` on another account hands the server
  over.
- **Export.** Started, polled and downloaded in three calls.
- **Test calls** takes the browser's offer and returns the server's
  answer; the browser reports what worked.
