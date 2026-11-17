-- Notices kept for one account: something done that concerns it, which it
-- should hear of even if it was not connected at the time and which is not
-- a message in any conversation (a recovery link used on it; for Owners, an
-- Owner made or stepping down, the export started or downloaded, an
-- override used). Each is kept until the account is removed; `seen_at` is
-- when its holder marked it seen.

CREATE TABLE notices (
  id              TEXT PRIMARY KEY,
  account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  kind            text NOT NULL,
  by_id           TEXT,                -- who did it; null when unknown, such as the command line
  subject_id      TEXT,                -- the account it is about, if not the holder
  conversation_id TEXT,                -- the conversation it is about, if any
  created_at      INTEGER NOT NULL,
  seen_at         INTEGER
);
CREATE INDEX notices_account ON notices (account_id, created_at);
