-- Message marks (ruling R67).
--
-- A message typed with /me is an action, drawn after its author's name, and a
-- message may carry a chance result the server drew (/roll, /flip, /pick,
-- /shuffle, /draw), which no edit changes.

ALTER TABLE messages ADD COLUMN action boolean NOT NULL DEFAULT false;
ALTER TABLE messages ADD COLUMN chance jsonb;
