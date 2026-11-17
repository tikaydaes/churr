-- A report keeps what it names (review findings H1 and H5). A report holds
-- a copy of the reported message as it was when reported: its text, its
-- author, its time and the names of its files. Deleting or purging the
-- message, or the chat it was in, no longer takes the report or the
-- evidence with it: message_id is cleared instead. A report may also be of
-- a person (kind `account`): their name and picture as they were. to_admins
-- says the report went to the instance's admins: every report made in a
-- direct or group chat, every report of a person, and a report in a private
-- channel about one of its moderators or managers or in a channel with
-- none. account_id is the person reported: the message's author, or the
-- account. message_at is the reported message's time, so the list of a
-- channel's moderators can leave out what is from before their first join.

CREATE TABLE reports_new (
  id              TEXT PRIMARY KEY,
  kind            text NOT NULL DEFAULT 'message',  -- message | account
  message_id      TEXT REFERENCES messages(id) ON DELETE SET NULL,
  conversation_id TEXT,                   -- null for a report of a person
  account_id      TEXT,                   -- the person reported: the author, or the account
  message_at      INTEGER NOT NULL DEFAULT 0,
  snapshot        TEXT NOT NULL DEFAULT '{}',
  to_admins       INTEGER NOT NULL DEFAULT 0,
  reported_by     TEXT,                   -- null when the word filter flagged it
  source          text NOT NULL DEFAULT 'person',  -- person | filter
  reason          text NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL,
  handled_by      TEXT,
  handled_at      INTEGER,
  outcome         text                    -- dismissed | deleted | timed_out | null = waiting
);

INSERT INTO reports_new (id, kind, message_id, conversation_id, account_id, message_at, reported_by, source, reason, created_at,
    handled_by, handled_at, outcome)
  SELECT r.id, 'message', r.message_id, r.conversation_id, m.author_id, m.created_at, r.reported_by, r.source, r.reason,
    r.created_at, r.handled_by, r.handled_at, r.outcome
  FROM reports r JOIN messages m ON m.id = r.message_id;

DROP TABLE reports;
ALTER TABLE reports_new RENAME TO reports;
CREATE INDEX reports_conversation ON reports (conversation_id, created_at);
