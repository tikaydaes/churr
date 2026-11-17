-- The access key unlocks sealed chats (ruling R85, docs/SEALED.md section
-- 13.3): the public half of the unlock key the page derives from the key,
-- given by the page that made the key, together with it. The server never
-- sees the key or the unlock private key. Empty until a page gives it.

ALTER TABLE access_keys ADD COLUMN unlock_public BLOB;
