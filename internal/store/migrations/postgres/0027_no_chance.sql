-- Every chance outcome is a value of the catalogue drawn from the message's
-- draw (docs/FAIR.md): /flip, /pick, /shuffle, /draw, /tarot and /mahjongg
-- are modules, and a message's `chance` result is gone with them.

ALTER TABLE messages DROP COLUMN chance;
