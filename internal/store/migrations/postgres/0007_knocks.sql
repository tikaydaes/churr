-- Knock channels and knocks (rulings R74 and R79). A knock channel is a
-- private channel that owner, admin and member accounts can see (its name,
-- topic, member count and managers, never its content) and ask to join;
-- its managers accept or decline. At most one request waits per account
-- and channel; an answered one is kept as the record of who was let in or
-- turned away.

ALTER TABLE conversations ADD COLUMN knock boolean NOT NULL DEFAULT false;

CREATE TABLE knocks (
  id              uuid PRIMARY KEY,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  account_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  created_at      bigint NOT NULL,
  outcome         text NOT NULL DEFAULT 'pending',  -- pending | accepted | declined
  answered_by     uuid,
  answered_at     bigint
);
CREATE UNIQUE INDEX knocks_waiting ON knocks (conversation_id, account_id) WHERE outcome = 'pending';
