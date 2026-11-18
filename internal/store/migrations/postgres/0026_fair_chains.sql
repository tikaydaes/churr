-- Provably fair chance (docs/FAIR.md). The head of each chain of draws: the
-- number of its last draw and SHA-256 of that draw's proof, moved in the
-- same transaction as the message that uses the draw. A chain is named by
-- its conversation's id, or for a conversation shared over the link by the
-- stream's. And each message's proof of its draw, its `fair` block, kept as
-- the message carries it (JSON), also once the message is deleted.

CREATE TABLE fair_chains (
    chain text PRIMARY KEY,
    n     bigint NOT NULL,
    prev  bytea NOT NULL
);

ALTER TABLE messages ADD COLUMN fair jsonb;
