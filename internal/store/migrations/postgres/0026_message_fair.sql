-- Provably fair chance (docs/FAIR.md). Each message's proof of its draw, its
-- `fair` block, kept as the message carries it (JSON), also once the message
-- is deleted.

ALTER TABLE messages ADD COLUMN fair jsonb;
