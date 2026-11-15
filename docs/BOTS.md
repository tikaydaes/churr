# Bots, webhooks, commands and integrations

How a program takes part in a Churr server. `CONVENTIONS.md` gives the
rules for identifiers, times, names, errors and limits used here, and
`MESSAGE-FORMAT.md` gives the message, card and form shapes that every
payload in this document ends up as.

This document names no `/api/v1` paths. Operations are described in
words and `docs/openapi.yaml` gives each one its path. The paths outside
`/api/v1` that belong to this document are the webhook addresses:
`/hooks/{token}`, `/hooks/{token}/slack`, `/hooks/{token}/discord` and
`/hooks/{token}/github`.

There are three kinds of program:

| Kind               | What it is                                   | Where it works            |
|--------------------|----------------------------------------------|---------------------------|
| Bot                | An account with role `bot` and a token       | The channels it is in     |
| Webhook            | A secret address that posts into one channel | That channel, write only  |
| Server integration | A credential that is not an account          | The server, in no channel |

A fourth thing needs nothing from this document: a person's own script
uses a personal token (`churr_pat_…`) and acts as that person.

## 1. What a bot is

A bot is a member with a token. It uses the same HTTP API and the same
WebSocket as a person's client. Nothing in the API is bot-only, with two
narrow exceptions that are data, not endpoints: only a bot may post a
card through the API, and only a bot may post a form with the `bot`
action.

A bot's reach is the channels it is in. What it can do in a channel is
its level there: `reader`, `member` or `manager`, the same three levels
people have. There are no scopes and no intents.

| A bot can                                             | A bot cannot                                       |
|-------------------------------------------------------|----------------------------------------------------|
| Read, search and receive events in channels it is in  | See or browse any other channel, public ones included |
| See the accounts in those channels                    | List other accounts                                |
| Post, upload, edit and delete its own messages (level `member`) | Join a channel by itself; it must be added |
| React and tick checklists                             | Create a named channel                             |
| Post cards and forms                                  | Answer a form or vote in a poll                    |
| Offer commands                                        | Be in a sealed chat                                |
| Use manager powers where it is `manager`              | Be an owner or admin, or hold any server power     |
| Set its own status                                    | Use notify anyway                                  |
| Be in a group, and so be in that group's channels     | Sign in any way other than its token               |
| Leave a channel                                       | Be in `@everyone`                                  |
| Start an ad-hoc channel with accounts it can already see | Start one with anyone else                      |

- `perm` treats a bot exactly as it treats a guest for reading: the
  conversations it is in and the accounts in them, and nothing else.
- A bot in a channel receives every event of that channel, as a person
  does. A mention of the bot is an `account` entity with the bot's
  identifier in the message's `entities`.
- A bot added to a channel sees all of the channel's history, the same
  as any newcomer.
- Other accounts see a bot marked as a bot wherever its name is shown.
- A bot's presence is `online` while it has a live connection.
- A bot may start an ad-hoc channel, and add people to one it is in,
  only with accounts it can already see: those in a conversation it
  is in. That is how a bot messages one person. It cannot reach
  anyone it does not already share a conversation with.
- Slow mode does not apply to bots. The word filter and time-outs do.
- The edit time limit set by the admin does not apply to a bot editing
  its own messages. Earlier versions are kept as for anyone.

### Bots in shared channels

A bot is a local account, so in a channel shared with other servers its
messages and cards travel like any member's (`LINK-PROTOCOL.md`).
Its interactive side stays on its own server:

- Its commands are offered only to accounts of its own server.
- Dialogs can be shown only to accounts of its own server.
- In a shared conversation only `tally` forms are allowed, for bots
  as for everyone. A form with the `bot` action is refused there with
  `invalid_value`, so a bot has no buttons in a shared conversation.
  It can post a poll, and it can still answer commands with messages
  and private replies.

## 2. Creating a bot

An admin creates a bot in the admin console, or with the admin
operation that creates a bot. They give:

| Field          | Rule                                                   |
|----------------|--------------------------------------------------------|
| `username`     | The username rule, in the same namespace as people and groups |
| `display_name` | 1 to 64 characters                                     |
| `avatar`       | Optional image                                         |
| `description`  | Optional, up to 250 characters, shown on the bot's profile |

The result is an account with role `bot`, a row in `bots`, and the
bot's first token.

### Tokens

- A bot token is `churr_bot_` followed by 256 random bits in base64url.
  It is shown once, when made. The server stores its SHA-256 hash as a
  row in `sessions` with kind `bot`.
- A bot may have several tokens, each with a label, so a token can be
  replaced without downtime: make the new one, deploy it, revoke the
  old one. At most ten live tokens per bot.
- A token does not expire. An admin revokes it. Revoking closes any
  live connection opened with it at once.
- A bot cannot make or revoke its own tokens.
- Removing the bot (the ordinary remove-an-account operation) revokes
  every token, removes it from every channel and deletes its commands.
  Its messages stay, shown under its name as a former member.

Every one of these actions is written to the audit log.

## 3. Adding a bot to a channel

A bot gets into a channel the way a guest does: someone who may add
people there adds it.

- In a named channel: a manager of the channel, or an admin where
  admins may add people (public channels, and private channels the
  admin is in).
- In an ad-hoc channel: any member of it.
- Through a group: a bot that is in a group is in every channel that
  group has been added to, at the group's level.

The person adding the bot picks its level, `member` if they do not say.
Add a bot as `reader` if it should only watch, and as `manager` only if
it is meant to moderate. The channel shows the usual "joined" notice,
so nobody is surprised to find a bot reading. Removing the bot from the
channel ends its access to that channel at once, live events included.

A bot cannot be added to a sealed chat, and the operation that starts
a sealed chat refuses a bot. A locked private channel takes no new
members, bots included (`locked`).

## 4. Connecting

A bot is a client. It sends `Authorization: Bearer churr_bot_…` on
every HTTP request and when opening the WebSocket. The live connection
is described in DESIGN.md section 8 and exactly in `EVENTS.md`; nothing
about it differs for a bot. A bot connects out to the server, so it can
run on a laptop or behind a firewall, and the server never calls an
address a bot supplies.

A bot is sent every event of every channel it is in, however large the
channel: large channels are sent in full only to the connections that
have them open (`EVENTS.md` section 4.1), and a connection that never
says what it has open, as a bot does not, is sent everything. A bot
that wants less in a very large channel may send `open` in `resume`
and `conversations.open` as an app does; a bot in an announcement
channel posts there only if one of its managers allows it
(`FEATURES.md` section 21.9).

What a bot normally does when it starts:

1. Ask who it is (the operation that returns the caller's own
   account).
2. List its conversations.
3. Set its commands (section 5).
4. Open the WebSocket and tell the server the last `seq` it has for
   each conversation, so it is sent what it missed.
5. Handle events until disconnected, then reconnect with a delay that
   doubles from 1 second to 60 seconds.

Events that belong to a conversation carry `seq` and can be caught up
after a disconnection. The two kinds of event addressed to a bot alone,
`command.invoked` and `form.answered` for a `bot` form, carry no `seq`
and are never stored: if the bot is not connected when one happens, the
person is told the bot is not connected and nothing is queued. A bot
with several connections receives each on all of them.

### A worked session

`deploybot` has been created by an admin and added to `#ops` at level
`member`. Requests are shown by what they do; `→` is sent by the bot
and `←` is received. Identifiers are shortened.

Who am I:

    → the operation that returns the caller's account
    ← {"account": {"id": "0193b000-…", "username": "deploybot",
                   "display_name": "Deploy bot", "role": "bot"}}

Where am I:

    → list my conversations
    ← {"conversations": [{"id": "0193a0c7-…", "kind": "channel",
                          "name": "ops", "level": "member",
                          "last_seq": 4821}]}

Offer a command:

    → set my commands
      {"commands": [{"name": "deploy",
                     "description": "Deploy a service",
                     "hint": "<service> to <environment>"}]}
    ← {"commands": [{"name": "deploy", …}]}

Open the WebSocket and resume from `seq` 4821 in `#ops`. From here on
the server sends events. Sam types `/deploy api to staging`:

    ← {"type": "command.invoked",
       "at": "2026-11-13T09:30:00.000Z",
       "interaction_id": "0193c100-…",
       "conversation_id": "0193a0c7-…",
       "parent_id": null,
       "account_id": "0193a001-…",
       "command": "deploy",
       "text": "api to staging",
       "entities": []}

The bot answers for everyone, with a card and two buttons:

    → respond to the interaction
      {"interaction_id": "0193c100-…",
       "type": "message",
       "message": {
         "text": "",
         "cards": [{"color": "yellow",
                    "title": "Deploy api to staging?",
                    "text": "Asked by @sam",
                    "fields": [{"label": "Version", "value": "`4f2c91a`",
                                "short": true}]}],
         "form": {"fields": [],
                  "buttons": [{"id": "go", "label": "Deploy",
                               "style": "primary"},
                              {"id": "cancel", "label": "Cancel"}],
                  "action": {"type": "bot"}}}}
    ← {"message": {"id": "0193c200-…", "seq": 4822, …}}

The bot, like every member of `#ops`, then receives that message as an
event:

    ← {"type": "message.created", "at": "…",
       "conversation_id": "0193a0c7-…", "seq": 4822,
       "message": {"id": "0193c200-…", "author_id": "0193b000-…",
                   "via": {"kind": "command", "name": "deploy",
                           "account_id": "0193a001-…"}, …}}

Lee presses "Deploy":

    ← {"type": "form.answered",
       "at": "2026-11-13T09:30:41.000Z",
       "interaction_id": "0193c300-…",
       "conversation_id": "0193a0c7-…",
       "message_id": "0193c200-…",
       "form_id": "0193c201-…",
       "account_id": "0193a002-…",
       "button_id": "go",
       "answers": {}}

The bot starts the deploy, edits its message to say so (the ordinary
edit operation, with no form in the new content, so the buttons go),
and acknowledges the press so Lee's client stops waiting:

    → edit message 0193c200-…
      {"text": "", "cards": [{"color": "cyan",
                              "title": "Deploying api to staging",
                              "text": "Started by @lee"}]}
    → respond to the interaction
      {"interaction_id": "0193c300-…", "type": "none"}

Later it reports the result in the message's thread, with the ordinary
post operation and `parent_id` set to `0193c200-…`.

Had Lee not been allowed to deploy, the bot would have answered only
Lee:

    → respond to the interaction
      {"interaction_id": "0193c300-…", "type": "private",
       "text": "Only people in @deployers can deploy."}

## 5. Commands

A command is a word a bot offers, typed as `/name` in the message box.
Sending one posts nothing. The bot receives an event and decides what
to do.

### Registering

A bot sets its whole list of commands in one operation. The list it
sends replaces the list it had, so a bot can send the same list every
time it starts. An empty list removes them all.

| Field         | Rule                                                    |
|---------------|---------------------------------------------------------|
| `name`        | 1 to 32 characters, `[a-z0-9_-]`                        |
| `description` | 1 to 100 characters, shown beside the name              |
| `hint`        | Up to 100 characters describing the arguments, such as `<service> to <environment>`. Shown greyed after the name while typing. Not parsed. |

- At most 50 commands per bot.
- A name is unique on the server. Registration is refused with
  `name_taken`, naming the command, if the name is a built-in command,
  a community saved reply, or a command of another bot. Built-in names
  are listed in `FEATURES.md`; they include `poll`, `rsvp`, `signup`,
  `approve`, `form` and `remind`. An admin cannot create a community
  saved reply with the name of a bot's command either.
- Only a bot can register commands. A webhook cannot.

### Where commands appear

In a conversation, typing `/` lists, in this order: the built-in
commands, the community saved replies, the commands of every bot that
is a member of this conversation (each labelled with the bot's name),
and the person's own saved replies. A bot's commands are not listed,
and cannot be sent, in a conversation the bot is not in.

A person's own saved reply may have the same name as a bot's command.
For that person, typing the name runs their saved reply; the bot's
command is still in the list and can be picked from it.

### Sending a command

The client sends a command with the send-a-command operation, giving
the conversation, the command name, the text after the name (up to
4,000 characters) and, when typed in a thread, the thread's parent.

- The sender must be a local account that is a member of the
  conversation at any level, and not timed out there.
- The bot must still be a member of the conversation. Otherwise the
  answer is `not_found`.
- If the bot has no live connection the answer is `unavailable`, and
  the client says the bot is not connected.
- Otherwise the server makes an interaction (below), sends the bot the
  event, and returns the `interaction_id` to the client at once.

### The command event

    {"type": "command.invoked",
     "at": "2026-11-13T09:30:00.000Z",
     "interaction_id": "0193c100-…",
     "conversation_id": "0193a0c7-…",
     "parent_id": null,
     "account_id": "0193a001-…",
     "command": "deploy",
     "text": "api to staging",
     "entities": []}

| Field            | Meaning                                              |
|------------------|------------------------------------------------------|
| `interaction_id` | Names this request when the bot responds             |
| `conversation_id`| Where it was typed                                   |
| `parent_id`      | The thread it was typed in, if any                   |
| `account_id`     | Who asked                                            |
| `command`        | The name, without the slash                          |
| `text`           | Everything after the name, trimmed                   |
| `entities`       | What the server resolved in `text`, as for a message: accounts, groups, channels the asker can see, emoji and links, with positions in `text` |

The event has no `seq`. It goes to the bot only; other members of the
conversation see nothing.

### Interactions and responding

An **interaction** is one request from one person to one bot: a
command, a button press, or an answer to a `bot` form or dialog. The
event that tells the bot about it carries an `interaction_id`. The bot
answers with the respond-to-an-interaction operation, giving that
identifier and one of four types:

| `type`    | Other fields       | What happens                           |
|-----------|--------------------|----------------------------------------|
| `message` | `message`: the same body the post-a-message operation takes | The message is posted in the interaction's conversation, as the bot. This is the answer for everyone. The bot must be level `member` or above. If the interaction was in a thread and `message.parent_id` is absent, it goes into that thread. The message is marked `via: {"kind": "command", "name": "<command>", "account_id": "<who asked>"}` when the interaction was a command, so everyone can see who made the bot speak. |
| `private` | `text`, `cards`    | Shown to the asker only, in the conversation where they asked, marked "only you can see this". Text and up to five cards; no files and no form. It is not a message: it has no identifier and no `seq`, is not stored, is not searchable, and is gone when the asker's client restarts. It is sent to every client the asker has connected. |
| `dialog`  | `form`             | The asker is shown a form in a dialog (section 6). |
| `none`    |                    | Nothing is shown. Tells the asker's client to stop waiting. |

- An interaction lasts 15 minutes and accepts at most five responses.
  After that the operation answers `not_found`.
- Interactions are held in memory. A server restart ends them.
- A bot may also act without responding, using ordinary operations:
  post, edit, react. It should still send `none`, or the asker's client
  will show that the bot did not answer.
- The asker's clients learn of each response through an event
  addressed to that account alone (`interaction.responded`, carrying
  the `interaction_id`, the bot's identifier and the response).

### Timeouts

- The asker's client shows "Waiting for *bot*…" from the moment it
  sends a command or presses a button until the first response.
- If no response has arrived after 10 seconds the client shows "*bot*
  did not answer". This is display only: a response that arrives
  later, within the 15 minutes, is still delivered and shown.
- A bot that needs longer should respond `private` with "Working on
  it…" at once and follow up with a second response or an ordinary
  message.

## 6. Forms from bots

A bot posts forms like anyone else (`MESSAGE-FORMAT.md` section 7),
through the ordinary post operation or as a `message` response. Any of
the actions a member may use are open to it. One action is for bots
alone.

### The `bot` action

A form with `"action": {"type": "bot"}` sends each answer and each
button press to the bot that posted it, and stores it nowhere.

    {"type": "form.answered",
     "at": "2026-11-13T09:30:41.000Z",
     "interaction_id": "0193c300-…",
     "conversation_id": "0193a0c7-…",
     "message_id": "0193c200-…",
     "form_id": "0193c201-…",
     "account_id": "0193a002-…",
     "button_id": "go",
     "answers": {"reason": "Hotfix for the login bug"}}

| Field        | Meaning                                                  |
|--------------|----------------------------------------------------------|
| `account_id` | Who answered. A `bot` form cannot be `anonymous`. |
| `button_id`  | The button pressed, or absent if the form was submitted with its "Answer" button |
| `answers`    | Field identifier to value, in the value shapes of `MESSAGE-FORMAT.md` section 7. Empty for a form with no fields. |

- The event has no `seq` and goes to the bot only.
- Any member of the conversation can answer, readers included. Other
  bots cannot. Someone reading a public channel without having joined
  it joins first.
- A bot that is a manager of the conversation may set
  `answerer_group_id` on its form, as any manager may. The server
  then refuses answers and presses from accounts outside that group,
  and the bot never receives them.
- A `bot` form is refused in a conversation shared with other servers
  (section 1).
- Because nothing is stored, a `bot` form has no tally, no
  `answer_count` and no `my_answer`, and a person may answer or press
  any number of times. A bot that wants one answer per person keeps
  track itself.
- The server checks an answer against the form before sending it on:
  required fields, types, ranges, option identifiers. The bot never
  receives an answer that does not fit the form.
- If the bot has no live connection, the person answering gets
  `unavailable` and is told the bot is not connected.
- A closed form accepts nothing. The bot closes its form with the
  ordinary close operation, or removes it by editing the message.
- The rule that a form's fields cannot be edited once someone has
  answered does not bind a `bot` form, since no answers are held. A
  bot may replace the form, the cards and the text in one edit. That
  is how a bot shows the next step of a flow in place.

### Buttons

A form with no fields and up to five buttons is a row of buttons. A
press is a `form.answered` event with `button_id` set and empty
`answers`. A form with fields and buttons submits the fields together
with the button pressed. There is no other kind of button, and no menu
or picker outside a form's fields.

### Dialogs

When a bot needs more from one person it responds to that person's
interaction with a dialog.

    → respond to the interaction
      {"interaction_id": "0193c300-…",
       "type": "dialog",
       "form": {"title": "Roll back api",
                "fields": [{"id": "reason", "type": "longtext",
                            "label": "Why?", "required": true}],
                "buttons": [{"id": "ok", "label": "Roll back",
                             "style": "danger"}]}}

- A dialog form has the same fields and buttons as any form. Its
  action is always `bot` and need not be given. The settings
  `anonymous`, `results`, `allow_added_options` and `closes_at` are
  refused, and so is `answerer_group_id`.
- It is not a message. Only the asker sees it, on the client where
  they acted. A dialog is always the direct result of something that
  person just did; a bot cannot open one unprompted.
- Submitting it sends the bot a `form.answered` event with a new
  `interaction_id`, the dialog's `form_id`, `"dialog": true`, and no
  `message_id`. The bot can respond to that in turn, with another
  dialog if it needs a second step.
- Dismissing it sends the bot nothing. A bot must not wait for a
  dialog to come back.
- A dialog is gone once answered or dismissed, and in any case when
  its interaction ends, 15 minutes after the action that led to it.
  One dialog per interaction.
- Dialogs can be shown only to accounts of the bot's own server.

## 7. Reactions as input

A bot receives `reaction.added` and `reaction.removed` for every
message in its channels, like any member. Each names the message, the
emoji and the account. That is enough for light interaction without a
form: a bot can post a message, add one or two reactions itself as
ready-made choices, and act when others add theirs.

- A bot may add and remove its own reactions at any level.
- Reactions do not notify, so a bot that is not connected learns of
  them from catch-up, by `seq`, when it returns.
- The limit of twenty different emoji on a message applies.

## 8. Rate limits

Limits are per bot account, whichever token is used. A refused request
gets 429 `rate_limited` with `retry_after_seconds`, and every response
carries the `RateLimit-*` headers. The numbers are defaults an admin
can change.

| What                                   | Default                         |
|----------------------------------------|---------------------------------|
| Requests of any kind                   | 600 per minute                  |
| Messages posted or edited              | 60 per minute in one conversation, 300 per minute in all |
| Files uploaded                         | 30 per minute                   |
| Reactions added or removed             | 120 per minute                  |
| Setting the command list               | 10 per hour                     |
| Responses to one interaction           | 5                               |
| Live connections at once               | 5                               |
| Webhook posts (section 9)              | 60 per minute per webhook       |
| Image fetches for one webhook (section 11) | 30 per minute               |
| Server integration requests (section 15) | 600 per minute per integration |

## 9. Webhooks

A webhook is the smallest bot: a secret address that posts into one
channel and can read nothing. Behind it is an account with role `bot`
marked `webhook_only`, which is a member of that one channel at level
`member`. That account has no token for the API or the WebSocket. It
cannot be added to another channel, register commands, or be given
manager level.

A webhook belongs to a named channel, public or private. It cannot be
requested for an ad-hoc channel (name the channel first) or for a
sealed chat.

### Asking for one

Any account with server role `owner`, `admin` or `member` can ask for a
webhook for a named channel it is in, from the channel's menu. Guests
and bots cannot. The request gives:

| Field     | Rule                                                        |
|-----------|-------------------------------------------------------------|
| `name`    | 1 to 64 characters. The display name its messages appear under. |
| `purpose` | Up to 250 characters, for the person who approves it        |

- The request is stored in `webhooks` with state `waiting`, together
  with the account that will stand behind it. That account has a
  generated username, `hook-` followed by eight random characters from
  `[a-z0-9]`, and is in no channel yet.
- A person may have at most three requests waiting.
- The requester can withdraw a waiting request.

### Approving or declining

Waiting requests are listed in the admin console with the channel's
name, the requester, the name and the purpose.

- An admin approves or declines. A server setting lets admins hand the
  same decision to the managers of the channel concerned; it is off by
  default.
- An approver who is also the requester approves their own request in
  the same step.
- Declining sets the state to `declined` and removes the waiting
  account. No reason is recorded beyond the audit log entry.
- Approving sets the state to `approved` and adds the webhook's
  account to the channel at level `member`, with the usual "joined"
  notice. For a private channel the approving admin need not be in it:
  the request came from a member who is. A locked private channel
  takes no new members, so a request for one cannot be made or
  approved (`locked`); webhooks it already has keep working.
- The requester's clients are told of the decision by an event
  addressed to that account.

### Collecting the address

The address is shown to the requester once.

- After approval the requester opens the request and collects the
  address. Only then does the server make the token, store its
  SHA-256 hash in `webhooks.token_hash`, and return the full address.
  It can never be shown again.
- The token is `churr_hook_` followed by 256 random bits in base64url.
  The address is `https://<server>/hooks/<token>`.
- A **signing secret** is made at the same moment and shown beside the
  address, once. It is 256 random bits in base64url with no prefix.
  The server keeps it sealed in `webhooks.signing_secret`, because it
  must use the value itself to check signatures. It is for senders
  that sign what they send; in version 1 that is GitHub (section 14).
  It is not the token and is never part of an address.
- Until it is collected the webhook has no token and cannot post.
- If the requester leaves the channel or is removed before collecting,
  the webhook is revoked.
- A lost or leaked address is replaced, not recovered: the requester,
  a manager of the channel or an admin resets it. The old token and
  the old signing secret stop working at once, and the new address
  and new signing secret are shown once to whoever reset it.

### Revoking

The requester, a manager of the channel or an admin can revoke a
webhook. Its state becomes `revoked`, its token hash and signing
secret are cleared, and its account is removed. Its messages stay, shown under its name as a
former member. Removing the webhook's account from the channel by the
ordinary remove-a-member operation is the same as revoking it.

Members of a channel can see which webhooks it has: name, who asked,
who approved, and when each was last used. Nobody can see an address
or a signing secret.

Requests, decisions, collections, resets and revocations are written
to the audit log.

### The addresses

| Address                  | Accepts                                      |
|--------------------------|----------------------------------------------|
| `/hooks/{token}`         | Churr's own shape (section 10)               |
| `/hooks/{token}/slack`   | A Slack incoming-webhook payload (section 12) |
| `/hooks/{token}/discord` | A Discord webhook payload (section 13)       |
| `/hooks/{token}/github`  | A GitHub repository webhook event (section 14) |

Rules common to all four:

- `POST` only. The token in the path is the only credential. A
  `churr_hook_` token is not accepted in an `Authorization` header, and
  no other token works at these addresses.
- The request body is at most 1 MB, files included. Larger gets 413
  `too_large`. A program that must send more needs a bot.
- An unknown, revoked or uncollected token gets 404 `not_found`.
- A webhook in an archived channel gets 403 `archived`.
- The word filter applies: 422 `blocked_by_filter`. Slow mode does
  not.
- Over the rate limit gets 429 `rate_limited` with
  `retry_after_seconds` and a `Retry-After` header.
- Errors use Churr's error shape on all four addresses.
- The server's request log records at most the first eight characters
  of a token.
- Text in a webhook's message is resolved like anyone's: `@username`
  becomes an entity where it resolves, and a channel link written
  `<#conversation-id>` becomes one if it names a channel the
  webhook's account can see, as for any author (`#name` typed as
  plain characters stays plain text). Mentions notify
  under the same rules as for a member, including the admin settings
  and the channel's own setting that limit `@channel`, `@here` and
  `@everyone` to managers (`FEATURES.md` section 2.5): where the
  webhook's or bot's account may not use them they are posted as plain
  text and notify nobody. Mentions inside
  cards never notify.
- Every message posted through a webhook carries
  `via: {"kind": "webhook", "name": …}` (section 10).

## 10. The native payload

`POST /hooks/{token}` takes the same body as the post-a-message
operation, with the differences listed below it.

    POST /hooks/churr_hook_k3… HTTP/1.1
    Content-Type: application/json

    {
      "text": "Build **#482** failed on `main`. @sam",
      "cards": [
        {
          "color": "red",
          "title": "Build #482 failed",
          "url": "https://ci.example/builds/482",
          "text": "3 tests failing",
          "fields": [{"label": "Branch", "value": "main", "short": true}],
          "image_url": "https://ci.example/builds/482/graph.png",
          "footer": "ci.example",
          "at": "2026-11-13T09:28:10.000Z"
        }
      ],
      "parent_id": "0193c200-7c1e-7b3a-9f0e-2a4d5c6b7e8f",
      "via": {"name": "CI", "icon_url": "https://ci.example/icon.png"}
    }

| Field       | Rule                                                      |
|-------------|-----------------------------------------------------------|
| `text`      | Churr Markdown, up to 16,000 characters                   |
| `cards`     | Up to five cards (`MESSAGE-FORMAT.md` section 6). In place of `image_file_id` a card gives `image_url` or `image_part`; in place of `author.icon_file_id`, `author.icon_url` or `author.icon_part`. |
| `form`      | One form (`MESSAGE-FORMAT.md` section 7). Actions allowed: `tally`, `collect` and `thread`. A webhook has no connection to receive answers on, so `bot` is refused; `channel` and `group` are refused, and so is `answerer_group_id`, because a webhook is never a manager or admin. Answers to a `collect` form are read by the channel's managers. In a channel shared with other servers only `tally` is allowed. |
| `files`     | Up to ten entries `{"part": "file1", "name": "…", "caption": "…"}`, each naming a part of a multipart request (below). `name` defaults to the part's file name. |
| `parent_id` | A top-level message in the webhook's channel. The new message is a reply in its thread. A webhook learns identifiers from the responses to its own posts. |
| `client_id` | A UUID. Posting the same one again within 24 hours returns the first message and posts nothing (CONVENTIONS.md). |
| `via`       | Optional override of how the message is labelled: `name`, 1 to 64 characters, and `icon_url` or `icon_part`. |

At least one of `text`, `cards`, `form` and `files` must be present.
An unknown field is refused with `invalid_request`. Not accepted from
a webhook: `file_ids`, a quote, and notify anyway.

**Shown as "via".** The message's author is always the webhook's own
account, under the name the requester gave. When `via.name` is given,
clients add "via *name*" beside the author with the icon, so one
address can carry several sources and still show where each message
came from, and nobody can make a webhook look like a person. Without
an override, `via.name` is the webhook's name and there is no icon.

**Attaching files and images.** Files are normally uploaded to a
conversation first and attached by identifier. A webhook cannot
upload, so it has two other ways:

1. *Send the bytes in the same request.* Use
   `Content-Type: multipart/form-data` with one part named `payload`
   holding the JSON above, and up to ten file parts named `file1` to
   `file10`. A part is used where the JSON names it: in `files` to
   attach it to the message, or as `image_part` or `icon_part` to make
   it a card's image, a card author's icon or the via icon. A part the
   JSON does not name is refused. The whole request is still limited
   to 1 MB. Card images and icons must be PNG, JPEG, GIF or WebP by
   content.
2. *Give an address.* `image_url` and `icon_url` are fetched by the
   server (section 11). This only works when link previews are on.

A file part attached through `files` becomes an ordinary file of the
message: kind and media type are decided from its content, and images
get a thumbnail.

**Response.** 200 with the new message named, and nothing else about
the channel:

    {"message": {"id": "0193c400-…", "seq": 4830,
                 "created_at": "2026-11-13T09:30:00.000Z"}}

## 11. Image addresses in payloads

A client never loads anything from another site, so an image address
in a payload cannot be passed through. This applies to `image_url`,
`icon_url` and `via.icon_url` in the native shape, and to every image
address in a Slack, Discord or GitHub payload.

- **Link previews on.** The message is posted at once, without the
  images. The server then fetches each address once under the
  link-preview rules: public addresses only, ports 80 and 443, the
  address checked again after every redirect, and limits on time, size
  and redirects. A fetched image must be PNG, JPEG, GIF or WebP by
  content. It is re-encoded, stored as a file with purpose `card`, and
  added to the message with the same event that adds a link preview.
  This is not an edit and leaves no history.
- **Link previews off, or the fetch fails.** The image is dropped. A
  card that lost its image gets the address appended to its `footer`.
  A dropped icon leaves nothing behind.
- A fetched address is remembered for 24 hours by the hash of the
  address, so a webhook that posts often with the same icon causes one
  fetch a day.
- Fetches are counted against the webhook (section 8) as well as the
  server's own limit on preview fetches.
- A card left with no title, text or image after its image is dropped
  is removed from the message. A message left with nothing at all is
  not posted, and the request is refused with `invalid_request`
  before anything is stored when that can be known in advance (link
  previews are off).
- Image addresses are never fetched from the device of someone reading
  the message, and are never fetched a second time when the message is
  read, edited or sent to another server.

## 12. Slack payloads

`POST /hooks/{token}/slack` accepts what a Slack incoming webhook
accepts: a JSON body, or a form-encoded body with the JSON in a field
named `payload`. Unknown fields are ignored on this address. The
response to a success is 200 with the plain-text body `ok`, which is
what Slack sends and what some tools check for.

### Top-level fields

| Slack field   | Becomes                                                  |
|---------------|----------------------------------------------------------|
| `text`        | The message text, converted (below). Ignored when `blocks` is present, as Slack then uses it only for notifications. |
| `blocks`      | Message text and cards (below)                           |
| `attachments` | Cards (below)                                            |
| `username`    | `via.name`                                               |
| `icon_url`    | The via icon (section 11)                                |
| `icon_emoji`  | Ignored                                                  |
| `mrkdwn`      | If `false`, `text` is treated as plain: everything is escaped except the angle-bracket forms |
| `channel`, `thread_ts`, `link_names`, `unfurl_links`, `unfurl_media`, `response_type`, `replace_original`, `delete_original`, `metadata` | Ignored. A webhook posts to its own channel, and a Slack timestamp names no Churr message. |

If nothing is left to post after conversion the request is refused
with `invalid_request`.

### Text: mrkdwn to Churr Markdown

| Slack                                 | Churr                           |
|---------------------------------------|---------------------------------|
| `*bold*`                              | `**bold**`                      |
| `_italic_`                            | `_italic_`                      |
| `~strike~`                            | `~~strike~~`                    |
| `` `code` ``                          | `` `code` ``                    |
| Three backticks around text           | A fenced code block with no language, with the fences moved onto their own lines |
| `> quote` at the start of a line      | `> quote`                       |
| `&amp;`, `&lt;`, `&gt;`               | `&`, `<`, `>`                   |
| `<https://x.example\|text>`           | `[text](https://x.example)`     |
| `<https://x.example>`                 | `https://x.example`             |
| `<mailto:a@x.example\|text>`          | `[text](mailto:a@x.example)`    |
| `<@U024BE7LH>` or `<@W…>`             | `` `@U024BE7LH` `` in inline code. A Slack user identifier names nobody here. |
| `<@U024BE7LH\|sam>`                   | `@sam`                          |
| `<#C024BE7LR\|general>`               | `#general`, as plain text       |
| `<#C024BE7LR>`                        | Nothing: a Slack channel names no channel here |
| `<!channel>`, `<!everyone>`           | `@channel`                      |
| `<!here>`                             | `@here`                         |
| `<!subteam^S123\|@design>`            | `@design`                       |
| `<!subteam^S123>`                     | `` `@S123` ``                   |
| `<!date^1392734382^{date_short}\|Feb 18>` | `Feb 18` (the fallback text) |
| Any other `<!word>` or `<!word\|label>` | `label` if there is one, otherwise `word`, as plain text |
| `:emoji_name:`                        | Unchanged. Churr resolves it if it knows the name. |
| A line break                          | A line break                    |

- A link whose scheme is not `http`, `https`, `mailto` or `tel` is
  written as its text.
- Text that Slack would show plainly but that means something in
  Churr Markdown is escaped with a backslash so it stays plain: `#` at
  the start of a line, `[` and `]`, `|`, `**`, `~~`, a line of `---`,
  and `1.` at the start of a line.
- `@sam` produced by the conversion becomes an entity only if it
  resolves on this server, like any typed text. `#general` is plain
  text and is never resolved (a Churr channel is linked only by its
  identifier).
- Text longer than 16,000 characters after conversion is cut there
  and ends with `…`.

### Attachments to cards

Each of the first five attachments becomes one card.

| Attachment field | Card                                                  |
|------------------|-------------------------------------------------------|
| `color`          | `color`. `good` is `green`, `warning` is `yellow`, `danger` is `red`. A hex value is mapped to the nearest (section 12.1). Absent gives `grey`. |
| `pretext`        | Not part of the card. Converted and added to the message text as its own paragraph, in attachment order, after any top-level text. |
| `author_name`    | `author.name`                                         |
| `author_icon`    | `author` icon (section 11)                            |
| `author_link`    | Dropped                                               |
| `title`          | `title`, as plain text with mrkdwn marks removed, cut to 200 characters |
| `title_link`     | `url`                                                 |
| `text`           | `text`, converted, then reduced by the card text rule (section 12.2) |
| `fields[]`       | `fields[]`: `title` to `label`, `value` to `value` (converted, inline), `short` to `short`. Fields after the tenth are appended to the card's `text` as lines of `**label:** value`. |
| `image_url`      | The card's image (section 11)                         |
| `thumb_url`      | The card's image if there is no `image_url`; otherwise dropped. A card has one image. |
| `footer`         | `footer`, as plain text                               |
| `footer_icon`    | Dropped                                               |
| `ts`             | `at`. Seconds since the Unix epoch, as a number or a string of digits; a fraction is cut off. |
| `fallback`       | Ignored. The server writes its own fallback.          |
| `blocks`         | Converted as in the next section and appended to the card's `text`; the first image block becomes the card's image if it has none |
| `actions[]`      | Each element with a `url` becomes a link `[text](url)`. The links are appended to the card's `text` on one line, separated by ` · `. Elements without a `url` are dropped. |
| `mrkdwn_in`, `callback_id`, `attachment_type`, `id` | Ignored. `text`, `pretext` and field values are always converted. |

- An attachment with fields but no title, text or image has its
  fields written into `text` as `**label:** value` lines, since a card
  needs one of the three.
- An attachment with nothing in it is dropped.
- Each attachment after the fifth becomes a paragraph of the message
  text: its `fallback` if it has one, otherwise its title (as a link
  if it has `title_link`) and its text.

### 12.1 Colours

A colour that is not one of Churr's seven names is mapped by hue.
Convert the RGB value to hue (0 to 360), saturation and lightness
(0 to 1):

| Condition                                  | Colour     |
|--------------------------------------------|------------|
| Saturation below 0.15, or lightness below 0.10 or above 0.95 | `grey` |
| Hue from 345 up to 360, or from 0 up to 20 | `red`      |
| Hue from 20 up to 70                       | `yellow`   |
| Hue from 70 up to 160                      | `green`    |
| Hue from 160 up to 215                     | `cyan`     |
| Hue from 215 up to 290                     | `lavender` |
| Hue from 290 up to 345                     | `pink`     |

A value that cannot be parsed gives `grey`. The same table is used for
Discord colours.

### 12.2 The card text rule

A card's `text` holds inline constructs and lists only. When converted
text has more than that, it is reduced:

- A heading loses its `#` marks and becomes a bold line.
- A quote loses its `>` marks.
- A fenced code block becomes one inline-code span per line.
- A table and a rule are left as typed.
- The result is cut at 4,000 characters and then ends with `…`.

Field values are reduced the same way and then joined onto one line.

### Blocks

Top-level blocks are converted in order. Most become paragraphs of the
message text, separated by blank lines. An image block becomes a card,
because text cannot hold an image; cards are drawn after the text, so
an image in the middle of a Slack message moves to the end.

| Block type | Becomes                                                    |
|------------|------------------------------------------------------------|
| `header`   | `## ` followed by its `text.text`, as plain text           |
| `section`  | Its `text` as a paragraph (converted if `type` is `mrkdwn`, escaped if `plain_text`). Then each entry of `fields` as a paragraph of its own, in order. Then its `accessory`: an `image` is treated as an image block; a `button` with a `url` becomes a line with the link `[text](url)`; any other accessory is dropped. |
| `divider`  | `---`                                                      |
| `context`  | One paragraph: the text of its `mrkdwn` and `plain_text` elements joined with ` · `. Image elements are dropped. |
| `image`    | A card with the image from `image_url` (section 11), `title` from the block's `title.text`, and `text` from `alt_text` when there is no title. If the message already has five cards, a line `[alt_text](image_url)` in the text instead. An image block that gives a `slack_file` instead of `image_url` is dropped. |
| `actions`  | One line holding a link `[text](url)` for each `button` element that has a `url`, separated by ` · `. Buttons without a `url` are dropped: pressing them would have to call Slack. Every other element (menus, pickers, overflow, tick-boxes, radio buttons) is dropped. If nothing is left the block produces nothing. |
| `rich_text` | Flattened to Markdown: a `rich_text_section` is a paragraph; a `rich_text_list` is a bullet or numbered list by its `style`, nested by `indent`; a `rich_text_preformatted` is a fenced code block; a `rich_text_quote` is a quote. Inside them: `text` with `style.bold`, `italic`, `strike` and `code` as the matching marks; `link` as `[text](url)`; `emoji` as `:name:`; `user`, `channel` and `usergroup` as their identifier in inline code; `broadcast` as `@channel` or `@here` by its `range`. |
| `markdown` | Its `text` unchanged                                       |
| `video`    | A line `[title](video_url)`                                |
| `input`, `file`, `call` | Dropped                                       |
| Anything else | Flattened: every object inside it with `type` of `mrkdwn` or `plain_text` contributes its `text`, in document order, one paragraph each. Nothing else in it is kept. |

Block identifiers (`block_id`, `action_id`) are ignored everywhere.

### Example

    {"text": "Deploy finished",
     "username": "Jenkins",
     "attachments": [{"color": "#36a64f",
                      "pretext": "Pipeline <https://ci.example/7|#7>",
                      "title": "api 2.4.1",
                      "title_link": "https://ci.example/7",
                      "text": "*Passed* in 4m",
                      "fields": [{"title": "Env", "value": "staging",
                                  "short": true}],
                      "footer": "ci.example", "ts": 1794562090}]}

becomes

    {"text": "Deploy finished\n\nPipeline [#7](https://ci.example/7)",
     "cards": [{"color": "green",
                "title": "api 2.4.1",
                "url": "https://ci.example/7",
                "text": "**Passed** in 4m",
                "fields": [{"label": "Env", "value": "staging",
                            "short": true}],
                "footer": "ci.example",
                "at": "2026-11-13T09:28:10.000Z"}],
     "via": {"kind": "webhook", "name": "Jenkins"}}

## 13. Discord payloads

`POST /hooks/{token}/discord` accepts what a Discord webhook accepts:
a JSON body, or `multipart/form-data` with the JSON in a part named
`payload_json` and files in parts named `files[0]`, `files[1]` and so
on. Unknown fields are ignored on this address. A success is answered
204 with no body, as Discord does; with `?wait=true` it is answered
200 with `{"id": "<message id>"}`. The other query parameters Discord
defines (`thread_id`, `with_components`) are ignored.

### Top-level fields

| Discord field      | Becomes                                             |
|--------------------|-----------------------------------------------------|
| `content`          | The message text, converted (below)                 |
| `username`         | `via.name`                                          |
| `avatar_url`       | The via icon (section 11)                           |
| `embeds`           | Cards (below)                                       |
| `files[n]` parts   | Files on the message, up to ten, in order. A part that an embed names with `attachment://` is used there instead. |
| `attachments`      | For each entry, `description` becomes the caption of the file part with the same index as its `id`, and `filename` its name |
| `allowed_mentions` | If present and its `parse` list lacks `"everyone"`, `@everyone` and `@here` in the text are written in inline code so they notify nobody. Nothing else in it is used. |
| `components`       | Dropped                                             |
| `tts`, `flags`, `thread_name`, `applied_tags`, `poll` | Ignored             |

If nothing is left to post after conversion the request is refused
with `invalid_request`.

### Text: Discord Markdown to Churr Markdown

Discord's Markdown is close to Churr's. These pass through unchanged:
`**bold**`, `*italic*`, `_italic_`, `~~strike~~`, `||spoiler||`, inline
code, fenced code blocks with their language, `> quote`, `#`, `##` and
`###` headings, bullet and numbered lists, and `[text](url)` links.
The differences:

| Discord                         | Churr                               |
|---------------------------------|-------------------------------------|
| `__underline__`                 | `underline`, with the marks removed |
| `>>> ` and everything after it  | Every following line prefixed with `> ` |
| `-# small text`                 | `small text`                        |
| `<https://x.example>`           | `https://x.example`                 |
| `<@80351110224678912>` or `<@!…>` | `` `@80351110224678912` ``        |
| `<@&165511591545143296>`        | `` `@&165511591545143296` ``        |
| `<#103735883630395392>`         | `` `#103735883630395392` ``         |
| `@everyone`                     | `@channel`                          |
| `@here`                         | `@here`                             |
| `<:name:id>` and `<a:name:id>`  | `:name:`                            |
| `<t:1794562090>` with or without a style suffix | The time in UTC, `2026-11-13 09:28 UTC` |
| `- [ ] text` and `- [x] text`   | The `[` is escaped, so a webhook does not create a checklist |

Text longer than 16,000 characters after conversion is cut there and
ends with `…`.

### Embeds to cards

Each of the first five embeds becomes one card.

| Embed field        | Card                                                |
|--------------------|-----------------------------------------------------|
| `title`            | `title`, as plain text, cut to 200 characters       |
| `description`      | `text`, converted, then reduced by the card text rule (section 12.2) |
| `url`              | `url`                                               |
| `color`            | `color`. An integer holding RGB as `0xRRGGBB`, mapped by section 12.1. Absent gives `grey`. |
| `author.name`      | `author.name`                                       |
| `author.icon_url`  | `author` icon (section 11)                          |
| `author.url`       | Dropped                                             |
| `fields[]`         | `fields[]`: `name` to `label`, `value` to `value` (converted, inline), `inline` to `short`. Fields after the tenth are appended to the card's `text` as lines of `**label:** value`. |
| `image.url`        | The card's image (section 11)                       |
| `thumbnail.url`    | The card's image if there is no `image.url`; otherwise dropped |
| `footer.text`      | `footer`                                            |
| `footer.icon_url`  | Dropped                                             |
| `timestamp`        | `at`. An ISO 8601 time, converted to UTC.           |
| `type`, `video`, `provider`, and the `proxy_url`, `height` and `width` members of images | Ignored |

- An image address of the form `attachment://name` names the file
  part with that file name. The part's bytes become the card's image
  and nothing is fetched, so this works with link previews off.
- An embed with fields but no title, description or image has its
  fields written into `text`, as for Slack.
- An embed with nothing in it is dropped.
- Each embed after the fifth becomes a paragraph of the message text:
  its title (as a link if it has `url`) and its description.

### Example

    {"content": "Build failed <@&165511591545143296>",
     "username": "CI",
     "embeds": [{"title": "Build #482", "url": "https://ci.example/482",
                 "description": "3 tests failing", "color": 15158332,
                 "fields": [{"name": "Branch", "value": "main",
                             "inline": true}],
                 "timestamp": "2026-11-13T09:28:10.000Z"}]}

becomes

    {"text": "Build failed `@&165511591545143296`",
     "cards": [{"color": "red",
                "title": "Build #482",
                "url": "https://ci.example/482",
                "text": "3 tests failing",
                "fields": [{"label": "Branch", "value": "main",
                            "short": true}],
                "at": "2026-11-13T09:28:10.000Z"}],
     "via": {"kind": "webhook", "name": "CI"}}

## 14. GitHub events

`POST /hooks/{token}/github` accepts the events GitHub sends from a
repository or organisation webhook, as `application/json` or as
`application/x-www-form-urlencoded` with the JSON in a field named
`payload`. Each event that is listed below becomes one message with
one card and no text.

### Setting it up at GitHub

- Payload URL: the webhook address with `/github` on the end.
- Secret: the webhook's signing secret, exactly as it was shown.
- Events: whichever are wanted. Events not chosen are never sent.

### Checking the signature

GitHub signs each delivery with the secret and sends
`X-Hub-Signature-256: sha256=<hex>`. The server:

1. Takes the token from the path and finds the webhook by its hash.
2. Opens the webhook's stored signing secret and computes
   HMAC-SHA-256 over the raw request body, exactly as received and
   before any decoding, with the bytes of the secret's base64url
   string as the key. That string is what was typed into GitHub.
3. Compares the result with the hex value in the header in constant
   time.

A missing header, a header in another form, or a value that does not
match gets 401 `unauthenticated` and nothing is posted. The older
`X-Hub-Signature` (SHA-1) header is ignored.

The signing secret is separate from the token and never appears in an
address, so someone who learns the address alone, from a proxy log
for instance, cannot post to `/github`. The other three addresses do
not check a signature and are protected by the address only. Reset a
webhook whose address or secret has leaked; both are replaced
together.

### Headers used

| Header                | Use                                              |
|-----------------------|--------------------------------------------------|
| `X-GitHub-Event`      | The event name. Required.                        |
| `X-Hub-Signature-256` | The signature. Required.                         |
| `X-GitHub-Delivery`   | If it is a UUID it is used as the message's `client_id`, so a delivery GitHub sends again within 24 hours is not posted twice |

### What every card has

| Card field    | From                                                   |
|---------------|--------------------------------------------------------|
| `author.name` | `sender.login`                                         |
| `author` icon | `sender.avatar_url` (section 11)                       |
| `footer`      | `repository.full_name`                                 |
| `at`          | The time the request arrived, unless the row says otherwise |

"Body excerpt" below means: the text with HTML comments removed,
reduced by the card text rule (section 12.2), and cut to 500
characters at a word, ending with `…` if cut. GitHub's Markdown is
otherwise passed through; what Churr Markdown lacks is shown as typed.
`#n` is the item's `number`.

### Events

**`ping`**, sent when the webhook is created. Card: title "GitHub is
connected", text `repository.full_name` (or `organization.login`),
colour `cyan`.

**`push`**. The ref is `ref` with `refs/heads/` or `refs/tags/`
removed, and is a branch or a tag accordingly.

| Case                               | `title`                              | `color`    |
|------------------------------------|--------------------------------------|------------|
| `deleted` is true                  | "Branch *ref* deleted" or "Tag *ref* deleted" | `grey` |
| `created` is true and it is a tag  | "Tag *ref* created"                  | `cyan`     |
| `created` is true, a branch, and `commits` is empty | "Branch *ref* created" | `cyan`    |
| `forced` is true                   | "*N* commits force-pushed to *ref*"  | `yellow`   |
| Otherwise                          | "*N* commits pushed to *ref*" ("1 commit" for one) | `lavender` |

- `url` is `compare`.
- `text` is a bullet list of the first five entries of `commits`, each
  `` [`<first 7 of id>`](<url>) <first line of message, cut to 72> — <author.username, or author.name> ``,
  then "and *N* more" when there are more. *N* in the title is the
  length of `commits`.
- A push with no commits that neither creates nor deletes posts
  nothing.

**`pull_request`**. Posts for these values of `action`; any other
value posts nothing.

| `action`                       | `title`                                  | `color`    |
|--------------------------------|------------------------------------------|------------|
| `opened`                       | "Pull request #n opened: *title*" ("Draft pull request" if `pull_request.draft`) | `green` |
| `ready_for_review`             | "Pull request #n ready for review: *title*" | `green` |
| `reopened`                     | "Pull request #n reopened: *title*"      | `green`    |
| `closed`, `pull_request.merged` true  | "Pull request #n merged: *title*" | `lavender` |
| `closed`, `pull_request.merged` false | "Pull request #n closed: *title*" | `red`      |

- `url` is `pull_request.html_url`. *title* is `pull_request.title`.
- Fields: "Branch", `head.ref → base.ref`, short. For `opened` only,
  "Changes", `+additions −deletions in changed_files files`, short.
- `text`, for `opened` only, is the body excerpt of
  `pull_request.body`.

**`issues`**. Posts for these values of `action`; any other posts
nothing.

| `action`   | `title`                         | `color`                         |
|------------|---------------------------------|---------------------------------|
| `opened`   | "Issue #n opened: *title*"      | `green`                         |
| `closed`   | "Issue #n closed: *title*"      | `lavender`, or `grey` when `issue.state_reason` is `not_planned` |
| `reopened` | "Issue #n reopened: *title*"    | `green`                         |

- `url` is `issue.html_url`. *title* is `issue.title`.
- For `opened`: `text` is the body excerpt of `issue.body`, and a
  field "Labels" lists `issue.labels[].name` separated by commas, if
  there are any.

**`issue_comment`**. Posts for `action` of `created` only.

- `title` is "Comment on issue #n: *title*", or "Comment on pull
  request #n: *title*" when `issue.pull_request` is present.
- `url` is `comment.html_url`. `text` is the body excerpt of
  `comment.body`. `color` is `grey`.

**`release`**. Posts for `action` of `published` only.

- `title` is "Release *name* published", using `release.name`, or
  `release.tag_name` when the name is empty; "Pre-release" when
  `release.prerelease` is true.
- `url` is `release.html_url`. `text` is the body excerpt of
  `release.body`. A field "Tag" holds `release.tag_name`. `color` is
  `cyan`.

**`workflow_run`**. Posts for `action` of `completed` only.

- `title` is "*name*: *conclusion* on *branch*", from
  `workflow_run.name`, `workflow_run.conclusion` with underscores as
  spaces, and `workflow_run.head_branch`.
- `url` is `workflow_run.html_url`.
- Fields, all short: "Commit", the first seven characters of
  `workflow_run.head_sha` in inline code; "Run", `#run_number`, with
  "attempt *n*" when `run_attempt` is above 1; "Trigger",
  `workflow_run.event`.
- `at` is `workflow_run.updated_at`.
- `color` by `conclusion`: `success` is `green`; `failure`,
  `timed_out` and `startup_failure` are `red`; `action_required` is
  `yellow`; anything else is `grey`.

**Any other event.** One plain card, so that nothing GitHub was asked
to send vanishes:

- `title` is the event name with underscores as spaces, followed by
  `action` if the payload has one: "star created", "fork".
- `url` is `html_url` of the payload member named like the event if
  there is one, otherwise `repository.html_url`.
- `color` is `grey`.

### Responses

| Case                                           | Response               |
|------------------------------------------------|------------------------|
| A message was posted                           | 200, the same body as the native address |
| A listed event with an action that posts nothing | 204                  |
| `X-GitHub-Event` missing                       | 400 `invalid_request`  |
| Signature missing or wrong                     | 401 `unauthenticated`  |

## 15. Server integrations

A server integration is a credential for a program that works on the
server as a whole. It is not a bot and not an account.

| A server integration                              |
|---------------------------------------------------|
| Has a name and a token, and nothing else          |
| Appears in no member list and has no profile      |
| Is in no conversation and cannot be added to one  |
| Cannot read any message, file or event, post anything, or open the WebSocket |
| Can do only what an admin ticked when creating it |
| Has every action written to the audit log, with `actor_kind` `integration` |

### Creating one

An admin creates an integration in the admin console: a name, and the
things it may do, stored as a list in `integrations.allows`. The token
is `churr_int_` followed by 256 random bits in base64url. It is shown
once and stored as a hash. An admin can revoke it; there is nothing
else to edit, and a lost token is replaced by making a new integration.
An integration token is accepted only by the integration operations.
Anywhere else it gets 401 `unauthenticated`.

### The one capability: `availability`

There is one thing to tick. It lets the integration set and clear the
two availability values, in a meeting and out of office, for accounts.

**Finding an account.** The integration looks an account up by one of:

- `username`: a local username.
- `provider_id` and `subject`: a company sign-in provider configured
  on this server and the person's identifier there (the OIDC `sub` or
  the SAML NameID), matched against `identities`.

The answer is the account's `id` and `username` and nothing more, or
`not_found`. Only local accounts with role `owner`, `admin`, `member`
or `guest` can be found; bots, removed accounts and accounts from
other servers cannot.

**Setting.** The set-availability operation names an account by `id`
and takes:

    {"availability": "meeting", "until": "2026-11-13T10:30:00.000Z"}

| Field          | Rule                                                   |
|----------------|--------------------------------------------------------|
| `availability` | `meeting`, `out_of_office`, or `null` to clear         |
| `until`        | When it ends. Required with `meeting`, and at most 24 hours ahead. Optional with `out_of_office`. Must be in the future. |

- The server records who set the value in `accounts.availability_by`
  as `integration:<id>`.
- An integration may replace any value, including one the person set.
- Clearing only clears a value that this same integration set. A value
  the person set themselves, or another integration set, is left
  alone, and the answer says so with `conflict`. That lets a sync
  program clear at the end of every meeting without wiping an
  out-of-office the person set by hand.
- The person can always change or clear their availability, whoever
  set it.
- What the value does to notifications follows the person's own
  settings (DESIGN.md section 3). An integration cannot set a status,
  do not disturb, or anything else.
- The response is the account's `id` and its availability as now
  stored. It carries nothing else about the account.

### Outline: a calendar sync program

The server never reads a calendar. A program beside it does, and tells
the server only "in a meeting until 10:30".

    configuration:
      Churr address, integration token (churr_int_…)
      calendar service credentials
      how to name a person at Churr: the sign-in provider's id, with
        the calendar's user identifier mapped to the provider subject
        (or a table of calendar user -> Churr username)

    at start:
      for each calendar user:
        look up the Churr account by provider_id + subject
        remember calendar user -> account id; skip those not found

    every minute, for each remembered person:
      read their calendar for now .. now + 1 minute
      if an all-day "out of office" entry covers now:
        want = out_of_office until the entry's end
      else if a busy, accepted event covers now:
        want = meeting until the event's end
      else:
        want = nothing
      if want differs from what this program last set for them:
        set availability (or clear it; ignore a `conflict` answer)
        remember what was set

    on 429: wait retry_after_seconds
    on 404 for an account: drop it and look it up again tomorrow

The program holds calendar credentials and the map of people; the
Churr server holds neither. With 600 requests a minute and one request
per change, this is far inside the limit for 5,000 people.

## 16. Left to bots

These need an outside service or are a matter of taste, so the server
does not do them. `examples/` holds a small bot for each. Each sketch
uses only what this document describes.

### GIF search (`examples/gifbot`)

- Registers `gif` with hint `<what to search for>`.
- On `command.invoked`: searches an outside GIF service for `text`
  (the bot makes that connection, not the server), downloads the first
  result, uploads it to the conversation as a file, and responds with
  type `message`: the file, plus a `bot` form with buttons "Another"
  and "Remove". The message shows who asked through `via`.
- On `form.answered`: if `account_id` is not the asker (the bot keeps
  message identifier to asker in memory), it responds `private` with
  "Only the person who asked can change this". Otherwise "Another"
  uploads the next result and edits the message to carry it, and
  "Remove" deletes the message. It responds `none`.
- If it finds nothing it responds `private`, so the channel is not
  bothered.
- In a channel shared with other servers it posts the file without
  the buttons, since `bot` forms are refused there.
- Needs level `member` in the channel. Readers of the channel can
  still use the command; the bot does the posting.

### Kudos (`examples/kudosbot`)

- Counts thanks. Two inputs, both already there:
  - `reaction.added` with a chosen emoji (say `:star:`) on any message
    in its channels: one point to the message's author from the
    reacting account, unless they are the same account.
    `reaction.removed` takes it back.
  - The command `kudos`, hint `@person for <reason>`: the account
    entities in the event's `entities` say who is thanked, with no
    name parsing.
- Keeps its own table of points in its own storage. The server keeps
  nothing for it.
- Responds to `kudos` with type `message`: "@sam thanked @lee for …"
  and the new total. Responds to `kudos top` with a Markdown table of
  the top ten, as `private` unless asked with `kudos top here`.
- After a disconnection it catches up on reactions by `seq`, so none
  are missed; commands sent while it was away were refused at the
  time, so there is nothing to recover.

### Jira notifier (`examples/jirabot`)

- One direction needs no bot at all: Jira's automation can send a web
  request, so point it at a webhook's native address with a card
  (title, link, status, assignee as fields). Where Jira can only send
  its own shape, it goes to a bot instead.
- The bot covers the other direction and richer notices:
  - It connects out to Jira (polling, or Jira calling the bot's own
    address, which is the bot operator's business and not the
    server's) and posts a card per issue event in the channel mapped
    to that project.
  - Each card's message carries a `bot` form with buttons "Assign to
    me" and "Comment".
  - "Assign to me" needs to know who the presser is at Jira. The
    first time, the bot responds with a `dialog` asking for their Jira
    account, and remembers the answer against their account
    identifier.
  - "Comment" responds with a `dialog` holding one `longtext` field.
    On its answer the bot posts the comment to Jira and responds
    `private` with "Comment added".
  - When the issue changes, the bot edits its card in place and posts
    the change as a reply in the card's thread, so the channel keeps
    one message per issue.
- Registers `jira` with hint `<issue key>` to post an issue's card on
  request.
- In a channel shared with other servers the buttons are not
  available, so there the bot posts the cards without them.

## 17. Choices made in this document

Where DESIGN.md left a gap, the simplest thing consistent with the
rest was chosen. Each is listed so it can be changed in one place.

1. The command event is named `command.invoked`, to follow the
   `thing.happened` rule.
2. Commands, button presses, `bot` form answers and dialog answers
   are all "interactions" with one way to respond and four response
   types.
3. Events addressed to a bot alone are not stored. If the bot is not
   connected the person is told at once.
4. A private response is not a message and is not stored.
5. An interaction lasts 15 minutes and takes five responses; clients
   show "did not answer" after 10 seconds.
6. A bot sets its whole command list at once. Names are unique on the
   server, across built-ins, community saved replies and all bots.
7. A command may be sent by any member of the conversation, readers
   included, on the bot's own server only.
8. A message posted in response to a command names the asker in
   `via.account_id`.
9. Bots cannot create named channels. They may start ad-hoc
   channels only with accounts they can already see.
10. Slow mode and the edit time limit do not apply to bots.
11. Webhooks are for named channels only, so that a request never
    shows an admin an ad-hoc channel.
12. The account behind a webhook is created with the request, under a
    generated username.
13. The webhook token is made when the requester collects the
    address, not when the request is approved.
14. A webhook sends file bytes in a multipart request, inside the
    1 MB limit, or gives image addresses for the server to fetch.
15. A webhook's form may use `tally`, `collect` or `thread`; in a
    shared channel, like every form there, only `tally`.
16. The via override has a name and an icon.
17. Unknown fields are refused on the native address and ignored on
    the Slack, Discord and GitHub addresses.
18. Each webhook has its own signing secret, stored sealed, shown
    with the address and replaced with it. Only `/github` checks a
    signature.
19. GitHub events with unlisted actions post nothing; unlisted events
    post a plain card.
20. Colours are mapped by hue with the table in section 12.1.
21. An integration clears only values it set itself.
22. The rate limits in section 8.

## 18. To verify

Field names and behaviour of the three outside formats were written
from memory. Check these against the current documentation before the
converters are built.

**Slack**

- That incoming webhooks accept a form-encoded `payload` field, and
  answer a success with the body `ok`.
- Which of `username`, `icon_url`, `icon_emoji` and `channel` current
  app-created webhooks still honour (legacy ones do).
- `mrkdwn_in`: which attachment fields Slack formats when it is
  absent.
- The exact forms `<!subteam^ID|@handle>` and
  `<!date^unix^{format}|fallback>` (and its optional link part).
- Whether `ts` in an attachment may be a string.
- The `image` block's `slack_file` alternative to `image_url`.
- Element type names inside `rich_text`: `rich_text_section`,
  `rich_text_list` (`style`, `indent`), `rich_text_preformatted`,
  `rich_text_quote`, and the inline types `text`, `link`, `emoji`,
  `user`, `channel`, `usergroup`, `broadcast` (`range`).
- The newer `markdown` and `table` block types and their fields.
- The legacy attachment `actions[]` element fields (`type`, `text`,
  `url`).

**Discord**

- 204 as the default response, and what `?wait=true` returns.
- The multipart part names `payload_json` and `files[n]`, and how
  `attachments[].id` refers to a file part.
- `attachment://filename` in embed image and thumbnail addresses.
- `allowed_mentions.parse` values (`everyone`, `roles`, `users`).
- Embed limits: title 256, description 4,096, 25 fields, 10 embeds.
- The `-# ` small-text line and `>>> ` multi-line quote syntax.
- Timestamp tags `<t:unix:style>` and the animated emoji form
  `<a:name:id>`.
- Whether webhooks may send `poll`, `thread_name`, `applied_tags` and
  link-button `components`, and the meaning of `flags` bits.
- That `color` is always a decimal integer.

**GitHub**

- Header names `X-GitHub-Event`, `X-GitHub-Delivery`,
  `X-Hub-Signature-256`, and that the delivery identifier is a UUID.
- That the form-encoded content type puts the JSON in `payload`, and
  that the signature is over the raw body in both content types.
- `push`: `created`, `deleted`, `forced`, `compare`, and
  `commits[].author.username`; the largest number of commits a
  payload carries.
- `pull_request`: the `action` values `opened`, `ready_for_review`,
  `reopened`, `closed`; `merged`, `draft`, `additions`, `deletions`,
  `changed_files`.
- `issues`: `issue.state_reason` values `completed` and
  `not_planned`.
- `issue_comment`: `issue.pull_request` being present for comments on
  pull requests.
- `release`: the difference between the `published`, `released` and
  `prereleased` actions, to be sure `published` alone posts once per
  release.
- `workflow_run`: the full list of `conclusion` values, and
  `run_attempt`.
- `ping`: which of `repository` and `organization` is present.
- GitHub's own payload size limit (25 MB) against Churr's 1 MB: a
  very large push would be refused with 413.
