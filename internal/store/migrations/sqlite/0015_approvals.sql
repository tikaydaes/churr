-- The instance's approvals queue (ruling R97 and the owner's item on
-- approvals). Each row is one request that waits for a person who may
-- approve that kind: a registration that waits before its account may sign
-- in, a channel a member asked to have made. An answered row is kept as the
-- record of who decided. `approvers` holds the accounts given the
-- instance-level flag "may approve requests": a flag, not a role.

CREATE TABLE approvals (
  id           TEXT PRIMARY KEY,
  kind         text NOT NULL,                     -- registration | channel
  requester_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  note         text NOT NULL DEFAULT '',          -- what the requester said, at most 500 characters
  payload      text NOT NULL DEFAULT '{}',        -- what the kind needs to carry it out, as JSON
  state        text NOT NULL DEFAULT 'waiting',   -- waiting | approved | declined | lapsed
  reason       text NOT NULL DEFAULT '',          -- the decider's reason for declining, if any
  decided_by   TEXT REFERENCES accounts(id),
  decided_at   INTEGER,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL
);
CREATE INDEX approvals_waiting ON approvals (state, kind, created_at);
CREATE INDEX approvals_requester ON approvals (requester_id, created_at);

CREATE TABLE approvers (
  account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  granted_by TEXT REFERENCES accounts(id),
  granted_at INTEGER NOT NULL
);
