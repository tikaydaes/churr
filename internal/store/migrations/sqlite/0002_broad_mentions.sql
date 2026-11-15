-- Broad mentions in large channels (ruling R64).
--
-- A channel may say who uses @channel, @here and @everyone in it, and each
-- member may choose not to be notified by them.

ALTER TABLE conversations ADD COLUMN broad_mentions TEXT;   -- anyone | managers | null = follow the server
ALTER TABLE members ADD COLUMN ignore_broad INTEGER NOT NULL DEFAULT 0;
