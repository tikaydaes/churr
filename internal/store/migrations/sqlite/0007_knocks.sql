-- Knocks (ruling R74): someone asks the managers of a private channel to add
-- them. At most one waits per account and channel; an answered one is kept
-- as the record of who was let in or turned away.

CREATE TABLE knocks (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  created_at      INTEGER NOT NULL,
  outcome         text NOT NULL DEFAULT 'pending',  -- pending | accepted | declined
  answered_by     TEXT,
  answered_at     INTEGER
);
CREATE UNIQUE INDEX knocks_waiting ON knocks (conversation_id, account_id) WHERE outcome = 'pending';
