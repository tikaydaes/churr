-- Reference patterns (ruling R73): text such as PROJ-123 or #4521 that the
-- server's admins, or a channel's managers, have said is a reference to
-- something kept elsewhere.
--
-- A row with no conversation is the server's and applies everywhere; one
-- with a conversation applies in that channel, ahead of the server's.

CREATE TABLE reference_patterns (
  id              uuid PRIMARY KEY,
  conversation_id uuid REFERENCES conversations(id) ON DELETE CASCADE,  -- null = the server's
  name            text NOT NULL,
  prefix          text NOT NULL,            -- what a reference starts with: PROJ- or #
  expression      text NOT NULL DEFAULT '', -- what the value after it matches; '' = digits
  url_template    text NOT NULL,            -- the address, with {value} where the value goes
  created_by      uuid,
  created_at      bigint NOT NULL
);

CREATE INDEX reference_patterns_conversation ON reference_patterns (conversation_id);
