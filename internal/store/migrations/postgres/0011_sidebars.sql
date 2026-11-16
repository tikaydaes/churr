-- Each person's own sidebar (ruling R83, docs/FEATURES.md section 21.4):
-- their sections, where each conversation sits, how each heading sorts,
-- custom orders and the order of starred conversations. Kept on the
-- server so it follows the person to every device and both clients. One
-- row per account that has changed anything; none means the server's
-- default. `version` goes up by one with every change, so two devices
-- cannot overwrite each other unseen.

CREATE TABLE sidebars (
  account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  version    bigint  NOT NULL,
  layout     jsonb NOT NULL             -- JSON: the Sidebar object without its version
);
