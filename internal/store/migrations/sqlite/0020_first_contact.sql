-- First contact (ruling R103): nobody is put anywhere they did not accept.
--
-- A chat request now has four kinds. `direct` and `sealed` are as before:
-- one open request per pair and kind. `group` is being included in a group
-- chat (an ad-hoc channel of three or more) and `channel` is being invited
-- to a named channel: one open invitation per person and conversation,
-- whoever sent it. `level` is the level a channel invitation brings,
-- `override` marks one an Owner sent from the admin console.
--
-- channel_offers holds, per person and named channel, the answer to a
-- channel offered to them: through a group added to the channel (source
-- `group`), or by the channel becoming one of the instance's default
-- channels after they signed up (source `default`). A group gives its
-- people access only once they accept; `offered` waits in their sidebar,
-- `declined` closed it and they may still join while the access lasts.

ALTER TABLE chat_requests ADD COLUMN level text;                       -- channel invitations: the level it brings
ALTER TABLE chat_requests ADD COLUMN override INTEGER NOT NULL DEFAULT 0; -- channel invitations: sent by an Owner's override

DROP INDEX chat_requests_open;
CREATE UNIQUE INDEX chat_requests_open ON chat_requests (requester_id, recipient_id, kind)
  WHERE state IN ('pending', 'dismissed') AND kind IN ('direct', 'sealed');
CREATE UNIQUE INDEX chat_requests_invited ON chat_requests (recipient_id, conversation_id)
  WHERE state IN ('pending', 'dismissed') AND kind IN ('group', 'channel');

CREATE TABLE channel_offers (
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  source          text NOT NULL,             -- group | default
  state           text NOT NULL,             -- offered | accepted | declined
  created_at      INTEGER NOT NULL,
  answered_at     INTEGER,
  PRIMARY KEY (conversation_id, account_id)
);
CREATE INDEX channel_offers_account ON channel_offers (account_id, state);
