# Examples

Small programs that take part in a Churr server, each using only what
`docs/BOTS.md` describes and nothing but Go's standard library (or, for
`webhook.sh`, `curl`).

| Example | What it shows |
|---------|---------------|
| [`echo-bot`](echo-bot) | The smallest bot: one command, answered for everyone; a reply in a thread when mentioned |
| [`poll-bot`](poll-bot) | A command that posts a poll, and a dialog that asks one person for more |
| [`gif-bot`](gif-bot) | Uploading a file, buttons on a message, and answering only the person who pressed. Its GIF search is a stub |
| [`webhook.sh`](webhook.sh) | Posting through a webhook with `curl`: Churr's own shape, a file, and Slack, Discord and GitHub payloads |
| [`calendar-integration`](calendar-integration) | A server integration that sets "in a meeting" and "out of office". Its calendar is a stub |

`botkit` is the little the three bots share: calling the API with a bot
token, and holding the live connection open with catch-up and reconnection.
It is there to keep the examples short, not to be a client library.

## Trying them

Start a server on this computer and open the setup link it prints:

    go run ./cmd/churr serve -dev

**A bot.** As an admin, create a bot (the admin console, or `POST
/api/v1/admin/bots`). Its token, which starts `churr_bot_`, is shown once.
Add the bot to a channel as you would add a person, then:

    CHURR_URL=http://localhost:8080 CHURR_BOT_TOKEN=churr_bot_… go run ./examples/echo-bot

Type `/echo hello` in that channel. A bot connects out to the server, so it
runs anywhere that can reach it. Stop it and the command answers "the bot
is not connected": nothing is queued for a bot that is away.

**A webhook.** In a named channel you are in, ask for a webhook from the
channel's menu, have an admin approve it, and collect the address, which is
shown once together with a signing secret. Then:

    CHURR_HOOK=http://localhost:8080/hooks/churr_hook_… examples/webhook.sh card

**An integration.** As an admin, create a server integration that may set
availability. Its token starts `churr_int_`. See `calendar-integration`.

Use the address your server printed if it is not `localhost:8080`.
