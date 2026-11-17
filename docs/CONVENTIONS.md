# Conventions

Rules every other document in `docs/` follows. If another document
disagrees with this one, this one is right and the other needs fixing.

## Identifiers

- Accounts, conversations, messages, files, groups, forms and every
  other stored thing with an `id` use a UUIDv7, written lowercase with
  hyphens: `0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f`.
- UUIDv7 sorts by creation time. Nothing relies on that for message
  order; order inside a conversation is by `seq`.
- The server makes every identifier, with two exceptions: a client may
  supply `client_id` on a new message (below), and events from another
  server keep the identifiers that server gave them.
- Small things owned by a parent use short identifiers unique within
  the parent: form fields, form options, form buttons and checklist
  items have an `id` of 1 to 32 characters from `[a-z0-9_-]`.

## Sequence numbers

- Each conversation numbers its events from 1 in a 64-bit integer
  `seq`. A message's `seq` is the `seq` of the event that created it.
- Read positions, catch-up and paging all use `seq`.
- An event that does not belong to a conversation (a change to an
  account, a group, a setting) has no `seq`.

## Times

- JSON: RFC 3339 in UTC with milliseconds, `2026-11-13T09:30:00.000Z`.
- Database: `BIGINT` milliseconds since the Unix epoch, in columns named
  `…_at` (a moment) or `…_until` (a moment something ends).
- Durations in JSON are whole seconds in fields named `…_seconds`.
- Times of day (quiet hours, schedules) are `HH:MM` strings with an
  IANA time zone name beside them.

## Names

| Thing          | Rule                                                        |
|----------------|-------------------------------------------------------------|
| Username       | 2 to 32 characters, `[a-z0-9]` first, then `[a-z0-9._-]`. Unique among local accounts and group handles together. |
| Group handle   | Same rule and the same namespace as usernames, so `@name` is never ambiguous. |
| Reserved       | `everyone`, `admins`, `channel`, `here`, `churr`, `system`, `me` cannot be usernames or handles. |
| Channel name   | 1 to 64 characters of any script; spaces and capitals allowed; no control characters; no leading or trailing space. Not unique: any number of channels may share a name, and channels are identified by id. |
| Display name   | 1 to 64 characters of any script, no control characters.    |
| Custom emoji   | 2 to 32 characters, `[a-z0-9_]`. Unique among the server's own emoji. |
| Command name   | 1 to 32 characters, `[a-z0-9_-]`. The names held back (`help`, `later`, `status`) cannot be taken; a built-in's name can, and the community's command then hides the built-in (ruling R72). |
| Remote account | Shown and typed as `username@server.name`. The local part follows the username rule of its home server. |
| Remote emoji   | Written `:name@server.name:`.                               |

## JSON

- Field names are `snake_case`.
- A field that refers to another thing ends in `_id` and holds its
  identifier, including fields that say who did something:
  `checked_by_id`, `deleted_by_id`, `started_by_id`. Lists of
  identifiers end in `_ids`.
- Absent and `null` mean the same thing. Servers omit `null` fields.
- Clients ignore fields they do not know. Servers reject unknown fields
  in requests with `invalid_request`.
- Enumerations are lowercase strings with underscores.

## The API

- Base path `/api/v1`. Request and response bodies are JSON in UTF-8,
  except file uploads (multipart) and downloads.
- Authentication is a session cookie named `churr_session`, or
  `Authorization: Bearer <token>`.
- Tokens are 256 random bits in base64url with a prefix that says what
  they are, so a leaked one can be recognised:

  | Prefix        | Kind                         |
  |---------------|------------------------------|
  | `churr_ses_`  | Web session (cookie value)   |
  | `churr_app_`  | App access token             |
  | `churr_ref_`  | App refresh token            |
  | `churr_pat_`  | Personal token               |
  | `churr_bot_`  | Bot token                    |
  | `churr_hook_` | Webhook secret (in the path) |
  | `churr_int_`  | Server integration token     |
  | `churr_link_` | Invitation to link two servers or join a hub (a string, not a bearer token) |

  Only the SHA-256 hash of a token is stored.
- Methods: `GET` reads, `POST` creates or performs an action, `PATCH`
  changes some fields, `PUT` sets a whole value or turns something on,
  `DELETE` removes or turns off.
- A successful response is the thing itself or an object naming it
  (`{"message": {…}}`), never a bare list. Lists are in a named field
  with paging beside them.

### Errors

    HTTP/1.1 403 Forbidden
    {"error": {"code": "forbidden", "message": "You are not in this channel."}}

| Status | Codes                                                         |
|--------|---------------------------------------------------------------|
| 400    | `invalid_request` (malformed), `invalid_value` (with `field`) |
| 401    | `unauthenticated`, `second_factor_required`, `session_locked` (the session is locked: only unlocking and signing out are answered, `AUTH.md` section 10.6) |
| 403    | `forbidden`, `timed_out`, `archived`, `locked`, `feature_off`, `fresh_check_required` (a passkey or authenticator check is needed first), `approval_required` (this is approved first: ask for it in the approvals queue instead, `FEATURES.md` section 31), `approval_pending` (the account waits for its registration to be approved and cannot sign in yet) |
| 404    | `not_found`. Also returned for things that exist and that the caller may not know exist |
| 409    | `conflict`, `name_taken` (usernames, group handles, emoji and command names, and reserved names; never for a channel name, since those need not be unique), `already_exists` |
| 413    | `too_large`                                                   |
| 422    | `blocked_by_filter`                                           |
| 429    | `rate_limited`, `slow_mode`, with `retry_after_seconds`       |
| 5xx    | `internal`, `unavailable`                                     |

`message` is a plain sentence a client may show. `code` is what a
client acts on.

### Paging

- Things ordered by `seq` (messages, events) take `before`, `after`
  and `limit`, and return `has_more`.
- Filters by time are named `since` and `until`.
- Everything else takes `limit` and `cursor`, and returns `next_cursor`
  when there is more. A cursor is opaque.
- `limit` defaults to 50 and is capped at 200.

### Sending something once

`POST …/messages` accepts `client_id`, a UUID the client makes. Sending
the same `client_id` again in the same conversation within 24 hours
returns the message that was already created.

### Rate limits

Every response to an authenticated request carries
`RateLimit-Limit`, `RateLimit-Remaining` and `RateLimit-Reset`
(seconds). Limits are per account for people and bots, per token for
webhooks and integrations, and per address for sign-up and sign-in.

## Events

- Event names are `thing.happened`, lowercase, past tense:
  `message.created`, `member.joined`, `form.answered`.
- Every event is a JSON object with `type`, `at`, and, when it belongs
  to a conversation, `conversation_id` and `seq`.
- An event carries the changed thing in full, in the same shape the API
  returns it, so a client can apply it without another request.

## Limits

Defaults. Those marked * are server settings an admin can change.

| Thing                              | Limit            |
|------------------------------------|------------------|
| Message text                       | 16,000 characters |
| Files per message                  | 10               |
| File size *                        | 100 MB           |
| Cards per message                  | 5                |
| Fields per card                    | 10               |
| Fields per form                    | 20               |
| Options per choice field           | 50               |
| Buttons per form                   | 5                |
| Checklist items per message        | 100              |
| Different emoji reacting to a message | 20            |
| Pins per conversation              | 100              |
| Members of an ad-hoc channel       | 50, then it must be named |
| Custom emoji image                 | 256 KB, 256 px square |
| Topic                              | 250 characters   |
| Status text                        | 100 characters   |
| Quick-correction window *          | 30 seconds       |
| Edit window *                      | No limit         |
| Events kept for catch-up           | 30 days          |
| Webhook request body               | 1 MB             |

## Wording in documents

- "Account" is the stored thing. "Person" is a human. "Member" is an
  account in a conversation, or an account with the server role
  `member`; the text says which when it matters.
- "Channel" is a conversation of kind `channel`, named or ad-hoc.
  "Sealed chat" is a conversation of kind `sealed`. "Conversation" is
  either.
- "Admin" includes the owner unless the text says otherwise.

## Git hooks

The repository carries hooks in `.githooks/`. Turn them on once per clone:

    git config core.hooksPath .githooks

- **pre-commit** refuses staged Go files that are not formatted with `gofmt`,
  and refuses `e2e/node_modules`.
- **pre-push** runs the quick steps of the build: formatting, the build
  without cgo, and `go vet`.

The build on GitHub (`.github/workflows/build.yml`) runs the same checks and
the full tests.
