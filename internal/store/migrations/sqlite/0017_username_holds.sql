-- A username someone gave up by renaming is held for them for 30 days
-- (review finding H8): nobody else may take it in that time, so a name a
-- person was known by cannot be picked up at once by someone else. The
-- person who gave it up may take it back. A removed account keeps its
-- username for good, as before; this is only for renames.

CREATE TABLE username_holds (
  username    text PRIMARY KEY,
  account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  released_at INTEGER NOT NULL
);
