-- The hub that owns a channel makes every key epoch's stream key itself
-- (ruling R109; LINK-PROTOCOL.md section 10). An instance takes such a
-- channel's key from the home hub and from no member instance, so it no
-- longer keeps, per key epoch, which instance the hub named to make the key.

ALTER TABLE stream_channels DROP COLUMN makers;
