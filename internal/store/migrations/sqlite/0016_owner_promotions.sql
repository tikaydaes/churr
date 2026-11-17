-- An Owner made while `roles.new_owner_delay_hours` is above 0 waits here
-- until the delay has run (docs/PERMISSIONS.md section 9.1 note 12). Any
-- Owner withdraws it before then by giving the account any role; the
-- account stays what it was until `effective_at`. One row per account.

CREATE TABLE owner_promotions (
  account_id   TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  by_id        TEXT NOT NULL REFERENCES accounts(id),
  created_at   INTEGER NOT NULL,
  effective_at INTEGER NOT NULL
);
CREATE INDEX owner_promotions_due ON owner_promotions (effective_at);
