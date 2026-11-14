# poll-bot

A command that posts a poll, and a dialog that asks for what is missing.

    /quickpoll Lunch? | Pizza | Sushi | Salad     posts the poll
    /quickpoll                                    opens a dialog that asks for the question and options

The poll is an ordinary form with the `tally` action, so the server keeps
the votes and shows the tally; the bot posts it and is done. (Churr has a
built-in `/poll` that every client carries out by itself. This bot exists
to show how a bot does the same with a command of its own.)

Run it:

    CHURR_URL=http://localhost:8080 CHURR_BOT_TOKEN=churr_bot_… go run ./examples/poll-bot

What to read in `main.go`: responding to one interaction with a `dialog`,
and receiving its answer as `form.answered` with `"dialog": true` and a new
`interaction_id`, which is answered in turn. A dialog is shown only to the
person who asked, and a bot can open one only in answer to something that
person just did.
