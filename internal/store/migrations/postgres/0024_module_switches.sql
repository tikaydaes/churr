-- Built-in modules switched off (docs/DESIGN-MODULES.md section 6.5): by an
-- admin for the whole instance (no conversation), or by a channel's managers
-- for that channel. A module with no row is on. A channel cannot switch on
-- what the instance has off; its own row stays, so that switching the
-- instance on again leaves the channel as its managers set it.

CREATE TABLE module_switches (
  module          text NOT NULL,
  conversation_id uuid REFERENCES conversations(id) ON DELETE CASCADE   -- null = the whole instance
);
CREATE UNIQUE INDEX module_switches_instance ON module_switches (module) WHERE conversation_id IS NULL;
CREATE UNIQUE INDEX module_switches_conversation ON module_switches (conversation_id, module) WHERE conversation_id IS NOT NULL;
