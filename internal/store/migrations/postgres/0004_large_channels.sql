-- Large channels and announcement channels (ruling R66).
--
-- A channel may say whether it is delivered lazily (in full only to the
-- connections that have it open) whatever its size, and may be an
-- announcement channel, where only managers and the members they allow
-- post.

ALTER TABLE conversations ADD COLUMN lazy_delivery text;   -- on | off | null = follow the server
ALTER TABLE conversations ADD COLUMN announcement boolean NOT NULL DEFAULT false;
ALTER TABLE conversations ADD COLUMN announcement_replies boolean NOT NULL DEFAULT false;
ALTER TABLE members ADD COLUMN may_post boolean NOT NULL DEFAULT false;
