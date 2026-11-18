-- A message's view (docs/DESIGN-MODULES.md section 4.1): the message as a
-- value of the catalogue, such as a roll of dice, stored as it is. The
-- message's text is the message; the view is how a client that knows the
-- kind draws it.

ALTER TABLE messages ADD COLUMN view jsonb;
