-- What a newcomer to a private channel may read (ruling R87). A private
-- channel promises its members that what they said will not be opened up
-- to people who come later: a member reads only what was posted at or
-- after their first join. first_joins keeps that time per account and
-- channel, and when they last left: coming back within three hours keeps
-- it, later starts it again at the new join;
-- members.visible_from is a copy of it on the member row, 0 for every
-- member of any other kind of conversation and for the members a public
-- channel had when it was made private.

ALTER TABLE members ADD COLUMN visible_from bigint NOT NULL DEFAULT 0;

CREATE TABLE first_joins (
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  account_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  at              bigint NOT NULL,
  left_at         bigint,                   -- when they last went; null while a member
  PRIMARY KEY (conversation_id, account_id)
);
