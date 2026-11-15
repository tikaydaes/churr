-- Channel bans (ruling R72): IRC's ban beside the server-wide ban, which is
-- IRC's kline. A banned account is out of one channel and cannot come back
-- to it, by joining, being added or through a group, until the ban is
-- lifted or reaches expires_at.

CREATE TABLE channel_bans (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  reason          text NOT NULL DEFAULT '',
  created_by      TEXT NOT NULL,
  created_at      INTEGER NOT NULL,
  expires_at      INTEGER,                -- null = until lifted
  lifted_at       INTEGER
);
CREATE INDEX channel_bans_member ON channel_bans (conversation_id, account_id);
