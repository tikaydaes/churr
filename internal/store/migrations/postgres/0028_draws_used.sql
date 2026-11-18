-- The pairs of second and nonce this server's VRF key has drawn for, while
-- their second is inside the window (docs/FAIR.md section 6): written before
-- the key is applied, read back at start, and deleted once their second has
-- left the window. So a restart or a crash never lets a pair draw twice, and
-- an honest client is never refused after one. who names the request, so the
-- same request made again draws again, to the same result.

CREATE TABLE draws_used (
  second bigint NOT NULL,
  nonce  uuid NOT NULL,
  who    text NOT NULL,
  PRIMARY KEY (second, nonce)
);
