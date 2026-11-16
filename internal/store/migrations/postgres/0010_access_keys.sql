-- Access keys (ruling R80, docs/AUTH.md section 5.6): an account whose
-- person cannot use a passkey signs in with a long random code instead.
-- The server never sees the code: it keeps the SHA-256 of a value the page
-- derives from it. At most one per account.

CREATE TABLE access_keys (
  account_id   uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  auth_hash    bytea NOT NULL,              -- SHA-256 of the derived sign-in value
  created_at   bigint NOT NULL,
  last_used_at bigint
);
