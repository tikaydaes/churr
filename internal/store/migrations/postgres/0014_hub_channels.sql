-- Channels a hub owns (rulings R56, R94). The record the hub signs names
-- the channel's managers, who hold it as an owner would, and its
-- moderators, who keep the peace on every instance in it; and it says
-- whether the channel is private for good. stream_channels keeps, with the
-- record, which of its managers and moderators are accounts of this server
-- (the column that held the managers under the name moderators is renamed)
-- and a copy of the private flag, which internal/perm reads: in this
-- server's copy of a private channel nobody but its moderators and
-- managers admits people. Requests to join one wait in knocks, as a knock channel's do.

ALTER TABLE stream_channels RENAME COLUMN moderators TO managers;
ALTER TABLE stream_channels ADD COLUMN moderators text NOT NULL DEFAULT '[]';
ALTER TABLE stream_channels ADD COLUMN private boolean NOT NULL DEFAULT false;
