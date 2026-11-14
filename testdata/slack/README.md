# Small Slack exports

Hand-made exports in the layout Slack writes, for the tests of
`internal/importer`. Each folder is one export, unzipped; a test zips it
before importing. `{{FILES}}` in an address is replaced with the address of
the test's own file server, which stands in for Slack's.

`small/` has a little of everything section 25 of `docs/FEATURES.md` maps:

- five users: a member with a title, time zone, email address and picture;
  a guest whose handle needs cleaning; a deleted user; a bot user; and one
  whose handle (`owner`) is already taken on the server;
- `#general` (Slack's general channel, with a topic and a pin), an archived
  `#random` with only a purpose, a private channel, a group direct message,
  a direct message and a direct message to oneself;
- messages with Slack's markup, a thread with a reply "also sent to
  channel", an edited message, reactions (standard, custom, and one by an
  unknown user), a bot that only appears as an author with an attachment, a
  message by a deleted user and one by a user the export does not list, and
  join and topic notices that must not be imported;
- files: one that downloads, an image, one whose link has stopped working,
  and one Slack hid behind its plan's limit;
- custom emoji: one image, one alias, one too large.
