-- Knock channels and knocks (rulings R74 and R79). A knock channel is a
-- private channel that owner, admin and member accounts can see (its name,
-- topic, member count and managers, never its content) and ask to join;
-- its managers accept or decline. At most one request waits per account
-- and channel; an answered one is kept as the record of who was let in or
-- turned away.

ALTER TABLE conversations ADD COLUMN knock INTEGER NOT NULL DEFAULT 0;

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
