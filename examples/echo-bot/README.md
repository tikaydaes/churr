# echo-bot

The smallest useful bot, in about seventy lines.

- Offers `/echo <text>` and answers with the text, for everyone. The
  message is marked with who asked.
- With no text, tells only the asker what to type (a `private` response).
- When mentioned in a message, replies in that message's thread.

Run it:

    CHURR_URL=http://localhost:8080 CHURR_BOT_TOKEN=churr_bot_… go run ./examples/echo-bot

It needs level `member` in a channel to answer there. As a `reader` it can
still be asked, and can only answer privately.

What to read in `main.go`: the three steps every bot takes when it starts
(who am I, set my commands, open the live connection), and the two events
it handles, `command.invoked` and `message.created`.
