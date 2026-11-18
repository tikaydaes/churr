-- What the built-in modules keep (docs/DESIGN-MODULES.md section 6.2): one
-- namespaced store of keys and bytes, with no migrations of a module's own.
-- A value is kept for the whole instance, for one conversation (it goes
-- with the conversation) or for one person (it goes when the account is
-- removed). The host of the modules caps a value at 64 KB, a module's
-- values in one conversation at 1 MB and all of a module's at 64 MB.

CREATE TABLE module_values (
  module          text NOT NULL,
  scope           text NOT NULL,        -- instance | conversation | account
  conversation_id uuid REFERENCES conversations(id) ON DELETE CASCADE,  -- scope conversation only
  account_id      uuid REFERENCES accounts(id) ON DELETE CASCADE,       -- scope account only
  key             text NOT NULL,
  value           bytea NOT NULL
);
CREATE UNIQUE INDEX module_values_instance ON module_values (module, key) WHERE scope = 'instance';
CREATE UNIQUE INDEX module_values_conversation ON module_values (module, conversation_id, key) WHERE scope = 'conversation';
CREATE UNIQUE INDEX module_values_account ON module_values (module, account_id, key) WHERE scope = 'account';
CREATE INDEX module_values_conversation_id ON module_values (conversation_id) WHERE conversation_id IS NOT NULL;
CREATE INDEX module_values_account_id ON module_values (account_id) WHERE account_id IS NOT NULL;
