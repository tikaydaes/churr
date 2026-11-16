-- Generated from docs/schema.sql by internal/store/migrations. Do not edit.
-- Change docs/schema.sql and run: go test ./internal/store/migrations -update

-- Churr database schema.
--
-- Written in the PostgreSQL dialect. The SQLite schema is the same tables
-- and columns with these substitutions, applied by the migration runner:
--
--   uuid      -> TEXT   (36 characters, lowercase, hyphenated)
--   jsonb     -> TEXT   (JSON)
--   boolean   -> INTEGER (0 or 1)
--   bytea     -> BLOB
--   bigint    -> INTEGER
--
-- Times are bigint milliseconds since the Unix epoch (docs/CONVENTIONS.md).
-- "sealed" in a comment on a bytea column means encrypted with AES-256-GCM
-- under the key file in the data directory, so the server can use the value
-- and a copy of the database alone does not reveal it.
--
-- Search differs per backend and is at the end of this file.
-- docs/SCHEMA.md explains each table.

-- ---------------------------------------------------------------- server

CREATE TABLE schema_migrations (
  version     integer PRIMARY KEY,
  applied_at  bigint NOT NULL
);

CREATE TABLE settings (
  key         text PRIMARY KEY,
  value       jsonb NOT NULL,
  updated_at  bigint NOT NULL,
  updated_by  uuid
);

CREATE TABLE audit_log (
  id          uuid PRIMARY KEY,
  at          bigint NOT NULL,
  actor_id    uuid,                 -- account; null for the server itself
  actor_kind  text NOT NULL,        -- account | integration | server | cli
  action      text NOT NULL,        -- e.g. account.removed, setting.changed
  target_kind text,                 -- account | conversation | message | ...
  target_id   text,
  detail      jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_at ON audit_log (at DESC);

-- -------------------------------------------------------------- accounts

CREATE TABLE servers (              -- other Churr servers this one knows
  id            uuid PRIMARY KEY,
  name          text NOT NULL UNIQUE,   -- host name
  sign_key      bytea NOT NULL,         -- Ed25519 public key
  box_key       bytea NOT NULL,         -- X25519 public key
  first_seen_at bigint NOT NULL,
  blocked_at    bigint
);

CREATE TABLE accounts (
  id               uuid PRIMARY KEY,
  username         text NOT NULL,
  server_id        uuid REFERENCES servers(id),   -- null = local account
  role             text NOT NULL,       -- owner | admin | member | guest | bot | remote
  display_name     text NOT NULL,
  avatar_file_id   uuid,
  title            text,
  pronouns         text,
  time_zone        text,                -- IANA name
  webauthn_handle  bytea UNIQUE,        -- random user handle for passkeys
  created_at       bigint NOT NULL,
  claimed_at       bigint,              -- null = imported and not yet claimed
  removed_at       bigint,
  removed_by       uuid,
  profile_version  bigint NOT NULL DEFAULT 0,   -- shadow accounts: newest profile applied

  -- status, availability, do not disturb
  status_emoji       text,
  status_text        text,
  status_until       bigint,
  status_dnd         boolean NOT NULL DEFAULT false,  -- status pauses notifications
  status_away        boolean NOT NULL DEFAULT false,  -- status appears away
  availability       text,              -- meeting | out_of_office | null
  availability_until bigint,
  availability_by    text,              -- self | integration:<id>
  away_until         bigint,            -- appear away; 0 = until turned off
  dnd_until          bigint,            -- manual do not disturb; 0 = until turned off
  quiet_start        text,              -- HH:MM
  quiet_end          text,
  quiet_days         text,              -- seven characters, Monday first: "1111100"
  quiet_skip_until   bigint,
  allow_notify_anyway boolean NOT NULL DEFAULT true,

  prefs            jsonb NOT NULL DEFAULT '{}'   -- thread view, meeting pauses notifications, ...
);
CREATE UNIQUE INDEX accounts_local_username ON accounts (username) WHERE server_id IS NULL;
CREATE UNIQUE INDEX accounts_remote_username ON accounts (server_id, username) WHERE server_id IS NOT NULL;

CREATE TABLE bots (
  account_id   uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  description  text NOT NULL DEFAULT '',
  created_by   uuid NOT NULL REFERENCES accounts(id),
  webhook_only boolean NOT NULL DEFAULT false   -- the account behind a webhook
);

CREATE TABLE passkeys (
  id            uuid PRIMARY KEY,
  account_id    uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  credential_id bytea NOT NULL UNIQUE,
  credential    jsonb NOT NULL,        -- public key, counters, flags
  name          text NOT NULL,
  has_prf       boolean NOT NULL DEFAULT false,
  unlock_public bytea,                 -- X25519 public key derived from this passkey's PRF output
  created_at    bigint NOT NULL,
  last_used_at  bigint
);
CREATE INDEX passkeys_account ON passkeys (account_id);

CREATE TABLE sso_providers (
  id            uuid PRIMARY KEY,
  kind          text NOT NULL,         -- oidc | saml
  name          text NOT NULL,         -- shown on the sign-in button
  config        jsonb NOT NULL,        -- issuer, client id, metadata, claim names
  secret        bytea,                 -- sealed: OIDC client secret, SAML key
  group_claim   text,
  admin_group   text,                  -- provider group that grants the admin role
  enabled       boolean NOT NULL DEFAULT true,
  created_at    bigint NOT NULL
);

CREATE TABLE identities (               -- an account's identity at a provider
  provider_id  uuid NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
  subject      text NOT NULL,           -- OIDC sub or SAML NameID
  account_id   uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  linked_at    bigint NOT NULL,
  last_used_at bigint,
  PRIMARY KEY (provider_id, subject)
);
CREATE INDEX identities_account ON identities (account_id);

CREATE TABLE totp (
  account_id   uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  secret       bytea NOT NULL,          -- sealed
  confirmed_at bigint,
  last_step    bigint,                  -- last accepted time step, to refuse replays
  created_at   bigint NOT NULL          -- when set-up began; an unconfirmed row is removed an hour after
);

CREATE TABLE recovery_codes (
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  code_hash  bytea NOT NULL,
  used_at    bigint,
  PRIMARY KEY (account_id, code_hash)
);

CREATE TABLE recovery_links (
  token_hash bytea PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  created_by uuid,                      -- null = command line
  expires_at bigint NOT NULL,
  used_at    bigint
);

CREATE TABLE sessions (
  id            uuid PRIMARY KEY,
  account_id    uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  kind          text NOT NULL,          -- web | app | personal | bot
  token_hash    bytea NOT NULL UNIQUE,
  refresh_hash  bytea UNIQUE,           -- app sessions
  prev_refresh_hash bytea,              -- tolerated for 60 seconds after rotation
  rotated_at    bigint,
  access_expires_at bigint,             -- app access tokens
  max_expires_at bigint,                -- a sliding session never passes this
  fresh_at      bigint,                 -- last passkey or TOTP check, for changes that need a fresh one
  label         text NOT NULL DEFAULT '',
  signed_in_by  text,                   -- passkey | oidc | saml | recovery_code | recovery_link | pairing
  second_factor text,                   -- passkey | totp | provider | null
  pending_second_factor boolean NOT NULL DEFAULT false,
  created_at    bigint NOT NULL,
  last_seen_at  bigint,
  expires_at    bigint,                 -- null = until revoked
  revoked_at    bigint
);
CREATE INDEX sessions_account ON sessions (account_id);

CREATE TABLE oauth_codes (
  code_hash      bytea PRIMARY KEY,
  account_id     uuid REFERENCES accounts(id) ON DELETE CASCADE,  -- null until a pairing code is confirmed
  kind           text NOT NULL,         -- authorization | pairing
  client_name    text NOT NULL,
  redirect_uri   text,
  pkce_challenge text,
  platform       text,
  box_key        bytea,                 -- the app's public key, for the sealed hand-over
  handover       bytea,                 -- backup key sealed to box_key, at app sign-in and QR pairing
  expires_at     bigint NOT NULL,
  used_at        bigint
);

CREATE TABLE devices (
  id            uuid PRIMARY KEY,
  account_id    uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  session_id    uuid UNIQUE REFERENCES sessions(id) ON DELETE SET NULL,  -- a device outlives its session by up to 30 days
  name          text NOT NULL,
  platform      text NOT NULL,          -- android | ios | macos | windows | linux | web
  push_kind     text,                   -- apns | fcm | unifiedpush | none
  push_address  text,                   -- device token or UnifiedPush endpoint
  push_failed_at bigint,
  push_public_key bytea,                -- UnifiedPush / Web Push encryption, if the distributor needs it
  push_auth     bytea,
  sign_key      bytea,                  -- this device's Ed25519 public key, for sealed chats
  box_key       bytea,                  -- this device's X25519 public key
  retired_at    bigint,
  created_at    bigint NOT NULL,
  last_seen_at  bigint
);
CREATE INDEX devices_account ON devices (account_id);

CREATE TABLE invites (
  id          uuid PRIMARY KEY,
  code        text NOT NULL UNIQUE,
  role        text NOT NULL DEFAULT 'member',   -- member | guest
  group_ids   jsonb NOT NULL DEFAULT '[]',
  conversation_ids jsonb NOT NULL DEFAULT '[]', -- for guests
  claims_account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,  -- imported account this link claims
  max_uses    integer NOT NULL DEFAULT 1,       -- 0 = no limit
  uses        integer NOT NULL DEFAULT 0,
  expires_at  bigint,
  created_by  uuid REFERENCES accounts(id),
  created_at  bigint NOT NULL,
  revoked_at  bigint
);

CREATE TABLE import_claims (             -- lets company sign-in claim an imported account
  account_id  uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  email_hash  bytea NOT NULL
);

CREATE TABLE bans (
  id          uuid PRIMARY KEY,
  account_id  uuid REFERENCES accounts(id),
  provider_id uuid,
  subject     text,                     -- banned sign-in identity
  reason      text NOT NULL DEFAULT '',
  created_by  uuid NOT NULL,
  created_at  bigint NOT NULL,
  lifted_at   bigint
);

CREATE TABLE blocks (
  account_id  uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  blocked_id  uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  created_at  bigint NOT NULL,
  PRIMARY KEY (account_id, blocked_id)
);

CREATE TABLE status_presets (
  id            uuid PRIMARY KEY,
  owner_id      uuid REFERENCES accounts(id) ON DELETE CASCADE,  -- null = community preset
  emoji         text,
  text          text NOT NULL,
  duration_seconds integer,             -- null = until cleared
  dnd           boolean NOT NULL DEFAULT false,
  away          boolean NOT NULL DEFAULT false,
  availability  text,
  position      integer NOT NULL DEFAULT 0
);

CREATE TABLE themes (                   -- ruling R61: an account's colour themes
  id            uuid PRIMARY KEY,
  owner_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name          text NOT NULL,          -- 1 to 64 characters
  kind          text NOT NULL,          -- dark | light
  colours       jsonb NOT NULL,         -- {"bg","panel","raise","ink","dim","lav","cyan","pink","yel"}: "#rrggbb" each
  published_at  bigint,                 -- null = not in the server's theme library
  created_at    bigint NOT NULL,
  updated_at    bigint NOT NULL
);
CREATE INDEX themes_owner ON themes (owner_id, created_at);
CREATE INDEX themes_library ON themes (published_at, id) WHERE published_at IS NOT NULL;

CREATE TABLE integrations (
  id          uuid PRIMARY KEY,
  name        text NOT NULL,
  token_hash  bytea NOT NULL UNIQUE,
  allows      jsonb NOT NULL,           -- ["availability"]
  created_by  uuid NOT NULL,
  created_at  bigint NOT NULL,
  last_used_at bigint,
  revoked_at  bigint
);

-- ---------------------------------------------------------------- groups

CREATE TABLE groups (
  id          uuid PRIMARY KEY,
  handle      text NOT NULL UNIQUE,     -- shares a namespace with usernames
  name        text NOT NULL,
  description text NOT NULL DEFAULT '',
  color       text,
  open        boolean NOT NULL DEFAULT false,
  builtin     text UNIQUE,              -- everyone | admins | null
  provider_id uuid REFERENCES sso_providers(id) ON DELETE SET NULL,
  provider_group text,                  -- membership comes from this provider group
  created_at  bigint NOT NULL
);

CREATE TABLE group_members (            -- not used for built-in groups, which follow roles
  group_id   uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  added_by   uuid,
  added_at   bigint NOT NULL,
  PRIMARY KEY (group_id, account_id)
);
CREATE INDEX group_members_account ON group_members (account_id);

-- --------------------------------------------------------- conversations

CREATE TABLE sections (
  id        uuid PRIMARY KEY,
  name      text NOT NULL,
  position  integer NOT NULL
);

CREATE TABLE conversations (
  id            uuid PRIMARY KEY,
  kind          text NOT NULL,          -- channel | sealed
  private       boolean NOT NULL,
  name          text,                   -- null = ad-hoc channel or sealed chat
  topic         text NOT NULL DEFAULT '',
  welcome       text NOT NULL DEFAULT '',
  section_id    uuid REFERENCES sections(id) ON DELETE SET NULL,
  parent_id     uuid REFERENCES conversations(id) ON DELETE SET NULL,  -- sub-channel of this channel; one level only
  default_level text NOT NULL DEFAULT 'member',   -- reader | member
  retention_seconds bigint,             -- null = server default
  slow_mode_seconds integer NOT NULL DEFAULT 0,
  locked_at     bigint,                 -- private channel closed to new members for good
  locked_by     uuid,
  member_key    text UNIQUE,            -- ad-hoc: hash of sorted member ids; sealed: the pair
  created_by    uuid REFERENCES accounts(id),
  created_at    bigint NOT NULL,
  archived_at   bigint,
  next_seq      bigint NOT NULL DEFAULT 1,
  last_message_at bigint,
  moved_from_message_id uuid,           -- thread this channel was made from
  ticket_number integer,
  stream_id     uuid,                   -- shared with other servers; see streams
  key_epoch     bigint,                 -- sealed chats: current key epoch
  -- broad_mentions text              -- added by migration 0002: who may use @channel, @here, @everyone here: anyone | managers | null = follow the server
  -- lazy_delivery text               -- added by migration 0004: on | off | null = follow the server's large_channel_limit (ruling R66)
  -- announcement boolean NOT NULL DEFAULT false          -- added by migration 0004: only managers and members with may_post post
  -- announcement_replies boolean NOT NULL DEFAULT false  -- added by migration 0004: members may reply in threads of an announcement channel
  order_floor   bigint NOT NULL DEFAULT 0  -- 0 unless the conversation was shared once and no longer is: then one more than the highest `at` it held, and messages posted since are ordered after everything from that time
);
CREATE INDEX conversations_name ON conversations (name) WHERE name IS NOT NULL;   -- names are not unique
CREATE INDEX conversations_parent ON conversations (parent_id) WHERE parent_id IS NOT NULL;

CREATE TABLE members (
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  account_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  level           text NOT NULL,        -- reader | member | manager (highest of direct and group)
  direct_level    text,                 -- level held directly; null = only through groups
  via_group       boolean NOT NULL DEFAULT false,
  joined_at       bigint NOT NULL,
  read_seq        bigint NOT NULL DEFAULT 0,
  mode            text NOT NULL,        -- what notifies: any | mentions | nothing
  show            text,                 -- sidebar: always | activity | notification | hidden; null = the app's own default
  mode_before_mute text,
  muted_until     bigint,
  starred         boolean NOT NULL DEFAULT false,
  thread_view     text,                 -- threaded | flat | null = account preference
  last_post_at    bigint,               -- for slow mode
  -- ignore_broad boolean NOT NULL DEFAULT false   -- added by migration 0002: broad mentions write no mention row for this member
  -- may_post boolean NOT NULL DEFAULT false       -- added by migration 0004: may post in an announcement channel without being a manager
  PRIMARY KEY (conversation_id, account_id)
);
CREATE INDEX members_account ON members (account_id);

CREATE TABLE channel_groups (
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  group_id        uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  level           text NOT NULL,
  added_by        uuid,
  added_at        bigint NOT NULL,
  PRIMARY KEY (conversation_id, group_id)
);

CREATE TABLE timeouts (
  id              uuid PRIMARY KEY,
  account_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  conversation_id uuid REFERENCES conversations(id) ON DELETE CASCADE,  -- null = whole server
  until           bigint NOT NULL,
  reason          text NOT NULL DEFAULT '',
  created_by      uuid NOT NULL,
  created_at      bigint NOT NULL,
  lifted_at       bigint
);
CREATE INDEX timeouts_account ON timeouts (account_id, until);

-- channel_bans: added by migration 0005 (ruling R72). One account banned from
-- one channel: out of it, and kept out (joining, being added, groups) while
-- lifted_at is null and expires_at is null or ahead.
-- CREATE TABLE channel_bans (
--   id              uuid PRIMARY KEY,
--   conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
--   account_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
--   reason          text NOT NULL DEFAULT '',
--   created_by      uuid NOT NULL,
--   created_at      bigint NOT NULL,
--   expires_at      bigint,                -- null = until lifted
--   lifted_at       bigint
-- );
-- CREATE INDEX channel_bans_member ON channel_bans (conversation_id, account_id);

-- knocks: added by migration 0006 (ruling R74). Someone asks the managers of a
-- private channel to add them; one waits per account and channel at a time.
-- CREATE TABLE knocks (
--   id              uuid PRIMARY KEY,
--   conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
--   account_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
--   created_at      bigint NOT NULL,
--   outcome         text NOT NULL DEFAULT 'pending',  -- pending | accepted | declined
--   answered_by     uuid,
--   answered_at     bigint
-- );
-- CREATE UNIQUE INDEX knocks_waiting ON knocks (conversation_id, account_id) WHERE outcome = 'pending';

-- -------------------------------------------------------------- messages

CREATE TABLE messages (
  id              uuid PRIMARY KEY,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  seq             bigint NOT NULL,      -- the arrival number on this server: the seq of the event that created the message
  author_id       uuid NOT NULL REFERENCES accounts(id),
  parent_id       uuid REFERENCES messages(id) ON DELETE CASCADE,
  quote_message_id uuid,
  quote_start     integer,
  quote_end       integer,
  client_id       uuid,
  text            text NOT NULL DEFAULT '',
  entities        jsonb NOT NULL DEFAULT '[]',
  cards           jsonb NOT NULL DEFAULT '[]',
  fallback        text NOT NULL DEFAULT '',
  search_text     text NOT NULL DEFAULT '',  -- plain text for the search index: names in place of links, plus the fallback; empty for sealed and deleted messages
  system          jsonb,                -- server-posted notices
  via_kind        text,
  via_name        text,
  via_account_id  uuid,
  via_icon_file_id uuid,
  quiet           boolean NOT NULL DEFAULT false,  -- sealed chats: does not notify or count as unread
  ciphertext      bytea,                -- sealed chats: the whole content
  key_epoch       bigint,               -- sealed chats: the key epoch this was encrypted in
  notified_anyway boolean NOT NULL DEFAULT false,
  version         integer NOT NULL DEFAULT 1,
  created_at      bigint NOT NULL,
  edited_at       bigint,
  deleted_at      bigint,
  deleted_by      uuid,
  history_hidden_at bigint,             -- a purge was asked for; history hidden from non-admins
  purged_at       bigint,
  -- thread summary, kept on the parent
  reply_count     integer NOT NULL DEFAULT 0,
  last_reply_seq  bigint,
  last_reply_at   bigint,
  last_reply_ord  text,                 -- the order string of the reply at last_reply_seq ('' = by that seq)
  -- where the message is shown (ruling R57)
  ord             text NOT NULL DEFAULT '',  -- the order string; '' = by seq, in a conversation that was never shared
  late            boolean NOT NULL DEFAULT false,  -- arrived after messages ordered later than it had been delivered here
  edit_ord        text,                 -- shared conversations: the order string of the event that last set the content
  -- action boolean NOT NULL DEFAULT false   -- added by migration 0003: typed with /me (ruling R67)
  -- chance jsonb                    -- added by migration 0003: a result the server drew for /roll, /flip, /pick, /shuffle, /draw (ruling R67)
  UNIQUE (conversation_id, seq)
);
CREATE INDEX messages_parent ON messages (parent_id, seq) WHERE parent_id IS NOT NULL;
CREATE INDEX messages_author ON messages (author_id, created_at);
CREATE UNIQUE INDEX messages_client ON messages (conversation_id, author_id, client_id) WHERE client_id IS NOT NULL;
CREATE INDEX messages_retention ON messages (conversation_id, created_at);

CREATE TABLE message_versions (         -- earlier versions, and the last content of a deleted message
  message_id  uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  version     integer NOT NULL,
  text        text NOT NULL,
  entities    jsonb NOT NULL,
  cards       jsonb NOT NULL,
  file_ids    jsonb NOT NULL,
  replaced_at bigint NOT NULL,
  replaced_by uuid NOT NULL,
  reason      text NOT NULL,            -- edit | delete
  PRIMARY KEY (message_id, version)
);

CREATE TABLE files (
  id              uuid PRIMARY KEY,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  message_id      uuid REFERENCES messages(id) ON DELETE SET NULL,
  uploader_id     uuid NOT NULL REFERENCES accounts(id),
  purpose         text NOT NULL DEFAULT 'attachment',  -- attachment | card | avatar | emoji
  position        integer NOT NULL DEFAULT 0,
  name            text NOT NULL,
  caption         text NOT NULL DEFAULT '',
  size            bigint NOT NULL,
  media_type      text NOT NULL,
  kind            text NOT NULL,        -- image | video | audio | text | file
  width           integer,
  height          integer,
  duration_seconds integer,
  preview_text    text,
  sha256          bytea NOT NULL,
  state           text NOT NULL DEFAULT 'ready',  -- ready | arriving | unavailable | too_large (files from other servers)
  blob_key        text,                 -- 'disk:<path>' or 's3:<key>'; null until a remote file has arrived
  poster_key      text,                 -- video poster image sent by the uploading client
  thumb_key       text,
  detached_at     bigint,               -- removed from its message by an edit or delete; kept for history
  created_at      bigint NOT NULL
);
CREATE INDEX files_message ON files (message_id);
CREATE INDEX files_orphans ON files (created_at) WHERE message_id IS NULL;
-- files.conversation_id is null-able in practice for avatars and emoji: those rows
-- use the server's own "library" conversation, created at setup and visible to nobody.

CREATE TABLE checklist_items (
  message_id  uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  item_id     text NOT NULL,
  position    integer NOT NULL,
  text        text NOT NULL,
  checked     boolean NOT NULL DEFAULT false,
  checked_by  uuid,
  checked_at  bigint,
  removed_at  bigint,                   -- removed by an edit; kept for history
  PRIMARY KEY (message_id, item_id)
);

CREATE TABLE checklist_history (
  id          uuid PRIMARY KEY,
  message_id  uuid NOT NULL,
  item_id     text NOT NULL,
  account_id  uuid NOT NULL,
  checked     boolean NOT NULL,
  at          bigint NOT NULL,
  FOREIGN KEY (message_id, item_id) REFERENCES checklist_items(message_id, item_id) ON DELETE CASCADE
);
CREATE INDEX checklist_history_item ON checklist_history (message_id, item_id, at);

CREATE TABLE forms (
  id            uuid PRIMARY KEY,
  message_id    uuid UNIQUE REFERENCES messages(id) ON DELETE CASCADE,  -- null = a bot's dialog
  dialog_for    uuid REFERENCES accounts(id) ON DELETE CASCADE,         -- the one person a dialog is shown to
  owner_id      uuid NOT NULL REFERENCES accounts(id),
  preset        text,
  title         text NOT NULL DEFAULT '',
  fields        jsonb NOT NULL,
  buttons       jsonb NOT NULL DEFAULT '[]',
  action        jsonb NOT NULL,
  anonymous     boolean NOT NULL DEFAULT false,
  results       text NOT NULL DEFAULT 'live',
  allow_added_options boolean NOT NULL DEFAULT false,
  answerer_group_id uuid,               -- only this group may answer
  one_answer    boolean NOT NULL DEFAULT true,
  closes_at     bigint,
  closed_at     bigint,
  closed_by     uuid,
  answer_count  integer NOT NULL DEFAULT 0,
  created_at    bigint NOT NULL
);

CREATE TABLE form_answers (
  id          uuid PRIMARY KEY,
  form_id     uuid NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
  account_id  uuid NOT NULL REFERENCES accounts(id),
  button_id   text,
  answers     jsonb NOT NULL,
  result_message_id uuid,               -- the reply or channel message a thread or channel action produced
  created_at  bigint NOT NULL,
  updated_at  bigint
);
CREATE INDEX form_answers_form ON form_answers (form_id, account_id);

CREATE TABLE form_tallies (             -- counts per option, kept in step with form_answers
  form_id    uuid NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
  field_id   text NOT NULL,
  option_id  text NOT NULL,
  count      integer NOT NULL DEFAULT 0,
  PRIMARY KEY (form_id, field_id, option_id)
);

CREATE TABLE reactions (
  message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  emoji      text NOT NULL,             -- the emoji itself, :name: for a custom one, :name@server: for another server's
  at         bigint NOT NULL,
  PRIMARY KEY (message_id, emoji, account_id)
);

CREATE TABLE mentions (
  message_id      uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  account_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  conversation_id uuid NOT NULL,
  seq             bigint NOT NULL,
  parent_id       uuid,
  kind            text NOT NULL,        -- account | group | channel | here
  PRIMARY KEY (message_id, account_id)
);
CREATE INDEX mentions_unread ON mentions (account_id, conversation_id, seq);

CREATE TABLE thread_states (
  parent_id   uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  account_id  uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  state       text NOT NULL,            -- following | ignored
  read_seq    bigint NOT NULL DEFAULT 0,
  updated_at  bigint NOT NULL,
  PRIMARY KEY (parent_id, account_id)
);
CREATE INDEX thread_states_account ON thread_states (account_id, state);

CREATE TABLE pins (
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  message_id      uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  pinned_by       uuid NOT NULL,
  pinned_at       bigint NOT NULL,
  PRIMARY KEY (conversation_id, message_id)
);

CREATE TABLE bookmarks (
  account_id  uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  message_id  uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  created_at  bigint NOT NULL,
  PRIMARY KEY (account_id, message_id)
);

CREATE TABLE emoji (
  id          uuid PRIMARY KEY,
  name        text NOT NULL,
  file_id     uuid NOT NULL REFERENCES files(id),
  created_by  uuid NOT NULL,
  created_at  bigint NOT NULL,
  server_id   uuid REFERENCES servers(id)   -- came with a shared channel
);
CREATE UNIQUE INDEX emoji_local_name ON emoji (name) WHERE server_id IS NULL;
CREATE UNIQUE INDEX emoji_remote_name ON emoji (server_id, name) WHERE server_id IS NOT NULL;

CREATE TABLE preview_cache (
  url_hash    bytea PRIMARY KEY,
  url         text NOT NULL,
  card        jsonb,                    -- null = nothing usable at that address
  fetched_at  bigint NOT NULL
);

CREATE TABLE events (                   -- the catch-up log
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  seq             bigint NOT NULL,
  type            text NOT NULL,
  message_id      uuid,                 -- the message this event carries, so erasing it can find the rows
  payload         jsonb NOT NULL,
  at              bigint NOT NULL,
  PRIMARY KEY (conversation_id, seq)
);
CREATE INDEX events_at ON events (at);
CREATE INDEX events_message ON events (message_id) WHERE message_id IS NOT NULL;

CREATE TABLE notify_anyway (            -- who a message was pushed through do not disturb to
  message_id  uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  account_id  uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  by_id       uuid NOT NULL,
  at          bigint NOT NULL,
  delivered   boolean NOT NULL,         -- false = quietly not applied; never revealed
  PRIMARY KEY (message_id, account_id)
);

-- ------------------------------------------------------------ moderation

CREATE TABLE purge_requests (
  id            uuid PRIMARY KEY,
  message_id    uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  requested_by  uuid NOT NULL,
  requested_at  bigint NOT NULL,
  decided_by    uuid,
  decided_at    bigint,
  outcome       text                    -- shown | hidden | erased | null = waiting
);
CREATE INDEX purge_requests_waiting ON purge_requests (requested_at) WHERE outcome IS NULL;

CREATE TABLE reports (
  id            uuid PRIMARY KEY,
  message_id    uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  conversation_id uuid NOT NULL,
  reported_by   uuid,                   -- null when the word filter flagged it
  source        text NOT NULL DEFAULT 'person',  -- person | filter
  reason        text NOT NULL DEFAULT '',
  created_at    bigint NOT NULL,
  handled_by    uuid,
  handled_at    bigint,
  outcome       text                    -- dismissed | deleted | timed_out | null = waiting
);

CREATE TABLE word_filters (
  id          uuid PRIMARY KEY,
  pattern     text NOT NULL,
  is_regex    boolean NOT NULL DEFAULT false,
  action      text NOT NULL,            -- block | flag
  created_by  uuid NOT NULL,
  created_at  bigint NOT NULL
);

-- ------------------------------------------------- scheduling and replies

CREATE TABLE scheduled (
  id              uuid PRIMARY KEY,
  account_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  kind            text NOT NULL,        -- message | reminder | form
  content         jsonb NOT NULL,       -- a message to post, as the API accepts it
  about_message_id uuid,                -- "remind me about this"
  next_run_at     bigint,               -- null = finished
  repeat          jsonb,                -- {"every": "day"|"week"|"month", "days": "1111100", "at": "09:30", "time_zone": "..."}
  last_run_at     bigint,
  failed_at       bigint,
  failure         text,
  created_at      bigint NOT NULL,
  cancelled_at    bigint
);
CREATE INDEX scheduled_due ON scheduled (next_run_at) WHERE next_run_at IS NOT NULL;

CREATE TABLE saved_replies (
  id         uuid PRIMARY KEY,
  owner_id   uuid REFERENCES accounts(id) ON DELETE CASCADE,  -- null = community command
  name       text NOT NULL,             -- typed as /name
  text       text NOT NULL,
  created_at bigint NOT NULL
);
CREATE UNIQUE INDEX saved_replies_community ON saved_replies (name) WHERE owner_id IS NULL;
CREATE UNIQUE INDEX saved_replies_personal ON saved_replies (owner_id, name) WHERE owner_id IS NOT NULL;

-- ------------------------------------------------------ bots and webhooks

CREATE TABLE webhooks (
  id              uuid PRIMARY KEY,
  bot_id          uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  token_hash      bytea UNIQUE,         -- null until the requester collects the address
  signing_secret  bytea,                -- sealed; verifies GitHub's X-Hub-Signature-256
  name            text NOT NULL,
  purpose         text NOT NULL DEFAULT '',
  requested_by    uuid NOT NULL,
  requested_at    bigint NOT NULL,
  decided_by      uuid,
  decided_at      bigint,
  state           text NOT NULL,        -- waiting | approved | declined | revoked
  last_used_at    bigint
);

CREATE TABLE commands (
  id          uuid PRIMARY KEY,
  bot_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name        text NOT NULL UNIQUE,
  description text NOT NULL,
  hint        text NOT NULL DEFAULT ''
);

-- ----------------------------------------------------------------- calls

CREATE TABLE calls (
  id              uuid PRIMARY KEY,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  message_id      uuid,
  started_by      uuid NOT NULL,
  started_at      bigint NOT NULL,
  ended_at        bigint,
  host_server_id  uuid REFERENCES servers(id)   -- null = this server carries the media
);
CREATE UNIQUE INDEX calls_live ON calls (conversation_id) WHERE ended_at IS NULL;

CREATE TABLE call_participants (
  call_id     uuid NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
  account_id  uuid NOT NULL,
  joined_at   bigint NOT NULL,
  left_at     bigint,
  PRIMARY KEY (call_id, account_id, joined_at)
);

-- ------------------------------------------------- links to other servers

CREATE TABLE peers (
  id            uuid PRIMARY KEY,
  kind          text NOT NULL,          -- server | hub
  server_id     uuid REFERENCES servers(id),   -- for kind = server
  hub_name      text,
  hub_sign_key  bytea,
  url           text,                   -- where this server dials; null if the peer dials us
  dials         boolean NOT NULL,
  state         text NOT NULL,          -- invited | active | paused | removed
  invite_hash   bytea,
  invite_expires_at bigint,
  created_by    uuid NOT NULL,
  created_at    bigint NOT NULL,
  connected_at  bigint,
  last_error    text,
  clock_told    jsonb,                  -- what the peer, as a time reference, last told this server about this server's clock; null until told
  clock_measured jsonb,                 -- what this server, as a time reference, last measured of the peer's clock (direct links)
  away          jsonb                   -- direct links: the last few stretches the peer was disconnected, as [from, to] pairs by this server's clock
);

CREATE TABLE peer_servers (             -- servers reachable through a hub
  peer_id    uuid NOT NULL REFERENCES peers(id) ON DELETE CASCADE,
  server_id  uuid NOT NULL REFERENCES servers(id),
  PRIMARY KEY (peer_id, server_id)
);

CREATE TABLE streams (
  id              uuid PRIMARY KEY,
  peer_id         uuid NOT NULL REFERENCES peers(id),
  conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL,  -- null until accepted locally
  owner_server_id uuid REFERENCES servers(id),   -- null = this server owns it
  sequencer       text NOT NULL,        -- self | owner | hub: who is the stream's time reference and keeps its log. The column keeps its old name; nobody numbers events any more
  kind            text NOT NULL,        -- channel | adhoc | sealed
  state           text NOT NULL,        -- offered | active | left | removed
  key_epoch       integer NOT NULL DEFAULT 0,
  last_seq        bigint NOT NULL DEFAULT 0,   -- a delivery cursor: for sequencer = self, the last position written to stream_log; otherwise the log position of the last envelope received from whoever delivers. It orders nothing
  first_seq       bigint NOT NULL DEFAULT 0,   -- not used
  servers_version integer NOT NULL DEFAULT 0,
  max_blob_bytes  bigint,
  history_days    integer NOT NULL DEFAULT 0,
  offered_name    text,
  offered_at      bigint,
  accepted_by     uuid,
  created_at      bigint NOT NULL,
  home_hub        text,                 -- through a hub: the name of the hub that is the stream's home and its time reference. Today always the hub of peer_id
  max_at          bigint NOT NULL DEFAULT 0,   -- the highest `at` of any event this server has applied or made in the stream
  acked_at        bigint NOT NULL DEFAULT 0,   -- the `at` of the last of this server's own events the time reference acknowledged
  top_ord         text NOT NULL DEFAULT ''     -- the highest order string of any message delivered here; a message below it arrives late
);

CREATE TABLE stream_servers (
  stream_id  uuid NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  server_id  uuid NOT NULL REFERENCES servers(id),
  state      text NOT NULL,             -- invited | active | removed
  last_n     bigint NOT NULL DEFAULT 0, -- highest origin counter applied from this server
  last_at    bigint NOT NULL DEFAULT 0, -- the `at` of that event: an origin's `at` only grows
  logged_n   bigint NOT NULL DEFAULT 0, -- for a stream this server is the time reference of: the counter of this server's last envelope in stream_log
  joined_at  bigint,
  PRIMARY KEY (stream_id, server_id)
);

CREATE TABLE stream_keys (
  stream_id  uuid NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  epoch      integer NOT NULL,
  key        bytea NOT NULL,            -- sealed
  created_at bigint NOT NULL,
  PRIMARY KEY (stream_id, epoch)
);

CREATE TABLE outbox (                   -- this server's events that the stream's time reference has not acknowledged yet
  id          uuid PRIMARY KEY,         -- the origin event id
  stream_id   uuid NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  envelope    bytea NOT NULL,
  created_at  bigint NOT NULL,
  attempts    integer NOT NULL DEFAULT 0,
  next_try_at bigint NOT NULL
);
CREATE INDEX outbox_due ON outbox (next_try_at);

CREATE TABLE stream_seen (              -- origin event ids already applied, to drop duplicates
  stream_id       uuid NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  origin_event_id uuid NOT NULL,
  seq             bigint NOT NULL,
  at              bigint NOT NULL,      -- rows older than 35 days are pruned
  PRIMARY KEY (stream_id, origin_event_id)
);

CREATE TABLE stream_log (               -- for streams this server is the time reference of: every envelope it took, in the order it took them; kept 30 days
  stream_id  uuid NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  seq        bigint NOT NULL,           -- the position in this log: a delivery cursor, not an order
  origin     text NOT NULL DEFAULT '',  -- the name of the server that made the event
  n          bigint NOT NULL DEFAULT 0, -- that server's counter, as signed in the envelope
  prev_n     bigint NOT NULL DEFAULT 0, -- the counter of the same origin's envelope before this one in the log
  origin_event_id uuid,
  envelope   bytea NOT NULL,
  at         bigint NOT NULL,           -- when it was taken, by this server's clock
  PRIMARY KEY (stream_id, seq)
);
CREATE INDEX stream_log_origin ON stream_log (stream_id, origin, n);
CREATE INDEX stream_log_event ON stream_log (stream_id, origin_event_id);


CREATE TABLE stream_channels (          -- the hub-signed record of each hub-owned channel this server is in or invited to
  stream_id  uuid PRIMARY KEY REFERENCES streams(id) ON DELETE CASCADE,
  version    bigint NOT NULL,
  home_hub   text NOT NULL,             -- the hub that is the stream's home
  record     bytea NOT NULL,            -- the signed bytes, as received
  sig        bytea NOT NULL,
  makers     text NOT NULL DEFAULT '{}', -- key epoch -> the server the hub named to make its key
  moderators text NOT NULL DEFAULT '[]'  -- the account ids on the record's moderator list that are accounts of this server; internal/perm reads it
);

CREATE TABLE stream_moves (             -- moves of a direct-link stream onto a hub, one row per attempt, on both servers
  id              uuid PRIMARY KEY,     -- the move_id; the owner server makes it
  stream_id       uuid NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  from_peer_id    uuid NOT NULL REFERENCES peers(id),   -- the direct link the stream ran over
  hub_peer_id     uuid REFERENCES peers(id),            -- this server's link to the hub named
  hub_name        text NOT NULL,
  hub_fingerprint text NOT NULL,
  state           text NOT NULL,        -- proposed | accepted | committed | done | declined | abandoned
  reason          text,                 -- why it was declined or abandoned
  commit_seq      bigint,               -- on the owner server: the position of stream.moved in the direct log; the move is done when the other server has acknowledged it
  commit_event_id uuid,                 -- the origin event id of stream.moved
  accept          bytea,                -- the other server's signed agreement (a move_accept block), and its signature
  accept_sig      bytea,
  commit_block    bytea,                -- the owner server's signed commit (a channel_commit block), and its signature; set in the transaction that commits
  commit_sig      bytea,
  proposed_by     uuid,                 -- the local owner who proposed it
  answered_by     uuid,                 -- the local owner who accepted or declined it
  created_at      bigint NOT NULL,
  expires_at      bigint NOT NULL,
  updated_at      bigint NOT NULL
);
CREATE INDEX stream_moves_stream ON stream_moves (stream_id, created_at);

-- ---------------------------------------------------------- sealed chats

CREATE TABLE device_lists (             -- every version of each account's signed list of its devices
  account_id  uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  version     bigint NOT NULL,
  signed_list bytea NOT NULL,
  created_at  bigint NOT NULL,
  PRIMARY KEY (account_id, version)
);

CREATE TABLE sealed_epochs (            -- one record per key epoch per sealed chat
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  key_epoch       bigint NOT NULL,
  seq             bigint NOT NULL,
  device_id       uuid NOT NULL,        -- the device that made it
  record          bytea NOT NULL,       -- signed; holds the epoch secret sealed to each device
  created_at      bigint NOT NULL,
  ord             text NOT NULL DEFAULT '',  -- in a chat shared with another server: the order string of the stamp of the event that carried this record. Of two records made for one key epoch the one with the lower stands
  PRIMARY KEY (conversation_id, key_epoch)
);

CREATE TABLE backup_keys (              -- the account's backup key, locked once per way of opening it
  id          uuid PRIMARY KEY,
  account_id  uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  kind        text NOT NULL,            -- passkey | phrase | device
  passkey_id  uuid REFERENCES passkeys(id) ON DELETE CASCADE,
  device_id   uuid REFERENCES devices(id) ON DELETE CASCADE,
  key_version integer NOT NULL,
  locked_key  bytea NOT NULL,
  created_at  bigint NOT NULL
);
CREATE INDEX backup_keys_account ON backup_keys (account_id);

CREATE TABLE key_backups (
  account_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  key_epoch       bigint NOT NULL,
  key_version     integer NOT NULL,
  record          bytea NOT NULL,       -- encrypted under the backup key
  created_at      bigint NOT NULL,
  PRIMARY KEY (account_id, conversation_id, key_epoch)
);

CREATE TABLE account_records (          -- the account key and backup settings, encrypted under the backup key
  account_id      uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  key_version     integer NOT NULL,
  revision        bigint NOT NULL,
  record          bytea NOT NULL,
  replacement_due boolean NOT NULL DEFAULT false,  -- the backup key must be replaced at next unlock
  updated_at      bigint NOT NULL
);

CREATE TABLE unlock_requests (          -- a new device asking an unlocked one for the backup key
  id           uuid PRIMARY KEY,
  account_id   uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  device_id    uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  request      bytea NOT NULL,
  answer       bytea,                   -- sealed to the asking device
  created_at   bigint NOT NULL,
  expires_at   bigint NOT NULL,
  answered_at  bigint
);

-- ---------------------------------------------------------------- search
--
-- PostgreSQL:

ALTER TABLE messages ADD COLUMN tsv tsvector
  GENERATED ALWAYS AS (to_tsvector('simple', search_text)) STORED;
CREATE INDEX messages_tsv ON messages USING gin (tsv);

-- SQLite (in place of the two statements above):
--
--   CREATE VIRTUAL TABLE messages_fts USING fts5(
--     body, message_id UNINDEXED, conversation_id UNINDEXED,
--     tokenize = 'unicode61 remove_diacritics 2'
--   );
--   INSERT INTO messages_fts(messages_fts, rank) VALUES ('secure-delete', 1);
--
-- The store keeps messages_fts in step with messages in the same transaction:
-- insert on create, replace on edit, delete on delete, purge and retention.
-- Sealed-chat messages and deleted messages are never indexed.
-- The database is opened with PRAGMA secure_delete = ON and journal_mode = WAL.
