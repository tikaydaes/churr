-- Provably fair chance (docs/FAIR.md section 6): a chain's head is the
-- number of its last draw alone. Each draw's alpha is fixed by the owner,
-- the chain and n; the hash of the previous proof no longer goes into it,
-- since an ECVRF proof is not unique (its output is) and a server holding
-- the key could have chosen among proofs of one draw to steer the next.

ALTER TABLE fair_chains DROP COLUMN prev;
