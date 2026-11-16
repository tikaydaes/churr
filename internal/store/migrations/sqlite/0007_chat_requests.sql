-- Chat requests, closing a chat, and a block as deaf ears (rulings R77,
-- R78, R79). Planned as migration 0009, after 0007 (irc2) and 0008 (lock);
-- renumber it to follow whatever has landed.
--
-- A direct chat or sealed chat with someone who does not approve new chats
-- automatically starts as a request. Its conversation holds only the person
-- who asked until the request is accepted; the person asked is shown to
-- them through ghost_members. A block keeps the blocked person's chats and
-- additions looking normal to them, through the same table.

ALTER TABLE conversations ADD COLUMN closed_at INTEGER;      -- a direct or sealed chat that was closed: read-only for good, no member key
ALTER TABLE conversations ADD COLUMN request_state text;    -- waiting | declined | null = open

CREATE TABLE chat_requests (
  id              TEXT PRIMARY KEY,
  requester_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  recipient_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  kind            text NOT NULL,          -- direct | sealed
  conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,
  state           text NOT NULL,          -- pending | accepted | declined | dismissed | withdrawn
  shown_open      INTEGER NOT NULL DEFAULT 0,  -- the requester is shown the chat open (the person asked approves automatically but has blocked them)
  created_at      INTEGER NOT NULL,
  answered_at     INTEGER,
  closed_at       INTEGER                  -- when the chat it opened was closed
);
CREATE UNIQUE INDEX chat_requests_open ON chat_requests (requester_id, recipient_id, kind) WHERE state IN ('pending', 'dismissed');
CREATE INDEX chat_requests_requester ON chat_requests (requester_id, created_at);
CREATE INDEX chat_requests_recipient ON chat_requests (recipient_id, state);
CREATE INDEX chat_requests_conversation ON chat_requests (conversation_id);

CREATE TABLE ghost_members (
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  shown_to        TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  created_at      INTEGER NOT NULL,
  PRIMARY KEY (conversation_id, account_id, shown_to)
);
