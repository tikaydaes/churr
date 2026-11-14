# gif-bot

`/gif <what to search for>` posts a GIF for everyone, with buttons
"Another" and "Remove" that only the person who asked can use.

**The search is a stub.** A real gif-bot calls an outside GIF service; the
server never does, and neither does this example. `search()` in `main.go`
draws small coloured GIFs from the words searched for, so the bot runs
with nothing but a Churr server. It finds three results for anything
except the word `nothing`. Replace that one function to make it real.

Run it:

    CHURR_URL=http://localhost:8080 CHURR_BOT_TOKEN=churr_bot_… go run ./examples/gif-bot

What to read in `main.go`:

- Uploading a file to the conversation and attaching it to a `message`
  response.
- A row of buttons: a form with no fields and the `bot` action. A press
  comes to the bot as `form.answered` and is stored nowhere.
- Remembering who asked, and answering anyone else privately.
- "Another" edits the message in place; "Remove" deletes it; both end with
  a `none` response so the presser's client stops waiting.
- Finding nothing is answered privately, so the channel is not bothered.

It needs level `member` in the channel. What it remembers is kept in
memory, so the buttons of GIFs posted before a restart say "ask again".
