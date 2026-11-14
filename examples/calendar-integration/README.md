# calendar-integration

Sets "in a meeting" and "out of office" for people from a calendar. The
server never reads a calendar; this program does, and tells the server
only "in a meeting until 10:30".

**The calendar is a stub.** A real one would ask a calendar service.
`readCalendar()` in `main.go` reads a JSON file instead, again on every
pass, so you can edit it while the program runs. Replace that one function
to make it real.

It uses a **server integration** token (`churr_int_…`), which an admin
makes with "may set availability" ticked. An integration is not a bot and
not an account: it is in no conversation, reads nothing, and every use is
written to the audit log.

Run it:

    cp examples/calendar-integration/calendar.example.json /tmp/calendar.json
    # edit the usernames and times in /tmp/calendar.json
    CHURR_URL=http://localhost:8080 CHURR_INTEGRATION_TOKEN=churr_int_… \
      go run ./examples/calendar-integration /tmp/calendar.json

What it does, once a minute:

1. Works out what each person's entries mean now. Out of office wins over
   a meeting; a meeting is set at most 24 hours ahead.
2. Sends one request per change, not per minute, so it stays far inside
   the limit of 600 requests a minute.
3. Clears only what it set itself. If the person has set something by
   hand since, the server answers `conflict` and the program leaves it.
4. Waits as long as it is told on `rate_limited`, and looks an account up
   again tomorrow when it is not found.
