-- The lock (ruling R75): a session its holder has locked. While locked_at
-- is set the session may only unlock (a passkey assertion with user
-- verification) or sign out; every other operation, the live connection
-- included, is refused with `session_locked`.

ALTER TABLE sessions ADD COLUMN locked_at bigint;   -- null = not locked
