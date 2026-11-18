-- The VRF key history of each server this one has linked with directly
-- (docs/DESIGN-FAIR.md section 1): pinned when the link is made, and taken
-- again only when the server shows the pinned history with keys added after
-- it, each signed by its pinned signing key. Null until pinned. Kept as the
-- link carries it: a JSON list of {key, began, sig}.

ALTER TABLE servers ADD COLUMN vrf_keys jsonb;
