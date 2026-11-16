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
  applied_at  INTEGER NOT NULL
);

CREATE TABLE settings (
  key         text PRIMARY KEY,
  value       TEXT NOT NULL,
  updated_at  INTEGER NOT NULL,
  updated_by  TEXT
);

CREATE TABLE audit_log (
  id          TEXT PRIMARY KEY,
  at          INTEGER NOT NULL,
  actor_id    TEXT,                 -- account; null for the server itself
  actor_kind  text NOT NULL,        -- account | integration | server | cli
  action      text NOT NULL,        -- e.g. account.removed, setting.changed
  target_kind text,                 -- account | conversation | message | ...
  target_id   text,
  detail      TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_at ON audit_log (at DESC);

-- -------------------------------------------------------------- accounts

CREATE TABLE servers (              -- other Churr servers this one knows
  id            TEXT PRIMARY KEY,
  name          text NOT NULL UNIQUE,   -- host name
  sign_key      BLOB NOT NULL,         -- Ed25519 public key
  box_key       BLOB NOT NULL,         -- X25519 public key
  first_seen_at INTEGER NOT NULL,
  blocked_at    INTEGER
);

CREATE TABLE accounts (
  id               TEXT PRIMARY KEY,
  username         text NOT NULL,
  server_id        TEXT REFERENCES servers(id),   -- null = local account
  role             text NOT NULL,       -- owner | admin | member | guest | bot | remote
  display_name     text NOT NULL,
  avatar_file_id   TEXT,
  title            text,
  pronouns         text,
  time_zone        text,                -- IANA name
  webauthn_handle  BLOB UNIQUE,        -- random user handle for passkeys
  created_at       INTEGER NOT NULL,
  claimed_at       INTEGER,              -- null = imported and not yet claimed
  removed_at       INTEGER,
  removed_by       TEXT,
  profile_version  INTEGER NOT NULL DEFAULT 0,   -- shadow accounts: newest profile applied

  -- status, availability, do not disturb
  status_emoji       text,
  status_text        text,
  status_until       INTEGER,
  status_dnd         INTEGER NOT NULL DEFAULT 0,  -- status pauses notifications
  status_away        INTEGER NOT NULL DEFAULT 0,  -- status appears away
  availability       text,              -- meeting | out_of_office | null
  availability_until INTEGER,
  availability_by    text,              -- self | integration:<id>
  away_until         INTEGER,            -- appear away; 0 = until turned off
  dnd_until          INTEGER,            -- manual do not disturb; 0 = until turned off
  quiet_start        text,              -- HH:MM
  quiet_end          text,
  quiet_days         text,              -- seven characters, Monday first: "1111100"
  quiet_skip_until   INTEGER,
  allow_notify_anyway INTEGER NOT NULL DEFAULT 1,

  prefs            TEXT NOT NULL DEFAULT '{}'   -- thread view, meeting pauses notifications, ...
);
CREATE UNIQUE INDEX accounts_local_username ON accounts (username) WHERE server_id IS NULL;
CREATE UNIQUE INDEX accounts_remote_username ON accounts (server_id, username) WHERE server_id IS NOT NULL;

CREATE TABLE bots (
  account_id   TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  description  text NOT NULL DEFAULT '',
  created_by   TEXT NOT NULL REFERENCES accounts(id),
  webhook_only INTEGER NOT NULL DEFAULT 0   -- the account behind a webhook
);

CREATE TABLE passkeys (
  id            TEXT PRIMARY KEY,
  account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  credential_id BLOB NOT NULL UNIQUE,
  credential    TEXT NOT NULL,        -- public key, counters, flags
  name          text NOT NULL,
  has_prf       INTEGER NOT NULL DEFAULT 0,
  unlock_public BLOB,                 -- X25519 public key derived from this passkey's PRF output
  created_at    INTEGER NOT NULL,
  last_used_at  INTEGER
);
CREATE INDEX passkeys_account ON passkeys (account_id);

CREATE TABLE sso_providers (
  id            TEXT PRIMARY KEY,
  kind          text NOT NULL,         -- oidc | saml
  name          text NOT NULL,         -- shown on the sign-in button
  config        TEXT NOT NULL,        -- issuer, client id, metadata, claim names
  secret        BLOB,                 -- sealed: OIDC client secret, SAML key
  group_claim   text,
  admin_group   text,                  -- provider group that grants the admin role
  enabled       INTEGER NOT NULL DEFAULT 1,
  created_at    INTEGER NOT NULL
);

CREATE TABLE identities (               -- an account's identity at a provider
  provider_id  TEXT NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
  subject      text NOT NULL,           -- OIDC sub or SAML NameID
  account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  linked_at    INTEGER NOT NULL,
  last_used_at INTEGER,
  PRIMARY KEY (provider_id, subject)
);
CREATE INDEX identities_account ON identities (account_id);

CREATE TABLE totp (
  account_id   TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  secret       BLOB NOT NULL,          -- sealed
  confirmed_at INTEGER,
  last_step    INTEGER,                  -- last accepted time step, to refuse replays
  created_at   INTEGER NOT NULL          -- when set-up began; an unconfirmed row is removed an hour after
);

CREATE TABLE recovery_codes (
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  code_hash  BLOB NOT NULL,
  used_at    INTEGER,
  PRIMARY KEY (account_id, code_hash)
);

CREATE TABLE recovery_links (
  token_hash BLOB PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  created_by TEXT,                      -- null = command line
  expires_at INTEGER NOT NULL,
  used_at    INTEGER
);

CREATE TABLE sessions (
  id            TEXT PRIMARY KEY,
  account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  kind          text NOT NULL,          -- web | app | personal | bot
  token_hash    BLOB NOT NULL UNIQUE,
  refresh_hash  BLOB UNIQUE,           -- app sessions
  prev_refresh_hash BLOB,              -- tolerated for 60 seconds after rotation
  rotated_at    INTEGER,
  access_expires_at INTEGER,             -- app access tokens
  max_expires_at INTEGER,                -- a sliding session never passes this
  fresh_at      INTEGER,                 -- last passkey or TOTP check, for changes that need a fresh one
  label         text NOT NULL DEFAULT '',
  signed_in_by  text,                   -- passkey | oidc | saml | recovery_code | recovery_link | pairing
  second_factor text,                   -- passkey | totp | provider | null
  pending_second_factor INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL,
  last_seen_at  INTEGER,
  expires_at    INTEGER,                 -- null = until revoked
  revoked_at    INTEGER
  -- locked_at bigint                 -- added by migration 0008 (ruling R75): the holder locked this session; null = not locked
);
CREATE INDEX sessions_account ON sessions (account_id);

CREATE TABLE oauth_codes (
  code_hash      BLOB PRIMARY KEY,
  account_id     TEXT REFERENCES accounts(id) ON DELETE CASCADE,  -- null until a pairing code is confirmed
  kind           text NOT NULL,         -- authorization | pairing
  client_name    text NOT NULL,
  redirect_uri   text,
  pkce_challenge text,
  platform       text,
  box_key        BLOB,                 -- the app's public key, for the sealed hand-over
  handover       BLOB,                 -- backup key sealed to box_key, at app sign-in and QR pairing
  expires_at     INTEGER NOT NULL,
  used_at        INTEGER
);

CREATE TABLE devices (
  id            TEXT PRIMARY KEY,
  account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  session_id    TEXT UNIQUE REFERENCES sessions(id) ON DELETE SET NULL,  -- a device outlives its session by up to 30 days
  name          text NOT NULL,
  platform      text NOT NULL,          -- android | ios | macos | windows | linux | web
  push_kind     text,                   -- apns | fcm | unifiedpush | none
  push_address  text,                   -- device token or UnifiedPush endpoint
  push_failed_at INTEGER,
  push_public_key BLOB,                -- UnifiedPush / Web Push encryption, if the distributor needs it
  push_auth     BLOB,
  sign_key      BLOB,                  -- this device's Ed25519 public key, for sealed chats
  box_key       BLOB,                  -- this device's X25519 public key
  retired_at    INTEGER,
  created_at    INTEGER NOT NULL,
  last_seen_at  INTEGER
);
CREATE INDEX devices_account ON devices (account_id);

CREATE TABLE invites (
  id          TEXT PRIMARY KEY,
  code        text NOT NULL UNIQUE,
  role        text NOT NULL DEFAULT 'member',   -- member | guest
  group_ids   TEXT NOT NULL DEFAULT '[]',
  conversation_ids TEXT NOT NULL DEFAULT '[]', -- for guests
  claims_account_id TEXT REFERENCES accounts(id) ON DELETE CASCADE,  -- imported account this link claims
  max_uses    integer NOT NULL DEFAULT 1,       -- 0 = no limit
  uses        integer NOT NULL DEFAULT 0,
  expires_at  INTEGER,
  created_by  TEXT REFERENCES accounts(id),
  created_at  INTEGER NOT NULL,
  revoked_at  INTEGER
);

CREATE TABLE import_claims (             -- lets company sign-in claim an imported account
  account_id  TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  email_hash  BLOB NOT NULL
);

CREATE TABLE bans (
  id          TEXT PRIMARY KEY,
  account_id  TEXT REFERENCES accounts(id),
  provider_id TEXT,
  subject     text,                     -- banned sign-in identity
  reason      text NOT NULL DEFAULT '',
  created_by  TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  lifted_at   INTEGER
);

CREATE TABLE blocks (
  account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  blocked_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (account_id, blocked_id)
);

CREATE TABLE status_presets (
  id            TEXT PRIMARY KEY,
  owner_id      TEXT REFERENCES accounts(id) ON DELETE CASCADE,  -- null = community preset
  emoji         text,
  text          text NOT NULL,
  duration_seconds integer,             -- null = until cleared
  dnd           INTEGER NOT NULL DEFAULT 0,
  away          INTEGER NOT NULL DEFAULT 0,
  availability  text,
  position      integer NOT NULL DEFAULT 0
);

CREATE TABLE themes (                   -- ruling R61: an account's colour themes
  id            TEXT PRIMARY KEY,
  owner_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name          text NOT NULL,          -- 1 to 64 characters
  kind          text NOT NULL,          -- dark | light
  colours       TEXT NOT NULL,         -- {"bg","panel","raise","ink","dim","lav","cyan","pink","yel"}: "#rrggbb" each
  published_at  INTEGER,                 -- null = not in the server's theme library
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE INDEX themes_owner ON themes (owner_id, created_at);
CREATE INDEX themes_library ON themes (published_at, id) WHERE published_at IS NOT NULL;

CREATE TABLE integrations (
  id          TEXT PRIMARY KEY,
  name        text NOT NULL,
  token_hash  BLOB NOT NULL UNIQUE,
  allows      TEXT NOT NULL,           -- ["availability"]
  created_by  TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  last_used_at INTEGER,
  revoked_at  INTEGER
);

-- ---------------------------------------------------------------- groups

CREATE TABLE groups (
  id          TEXT PRIMARY KEY,
  handle      text NOT NULL UNIQUE,     -- shares a namespace with usernames
  name        text NOT NULL,
  description text NOT NULL DEFAULT '',
  color       text,
  open        INTEGER NOT NULL DEFAULT 0,
  builtin     text UNIQUE,              -- everyone | admins | null
  provider_id TEXT REFERENCES sso_providers(id) ON DELETE SET NULL,
  provider_group text,                  -- membership comes from this provider group
  created_at  INTEGER NOT NULL
);

CREATE TABLE group_members (            -- not used for built-in groups, which follow roles
  group_id   TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  added_by   TEXT,
  added_at   INTEGER NOT NULL,
  PRIMARY KEY (group_id, account_id)
);
CREATE INDEX group_members_account ON group_members (account_id);

-- --------------------------------------------------------- conversations

CREATE TABLE sections (
  id        TEXT PRIMARY KEY,
  name      text NOT NULL,
  position  integer NOT NULL
);

CREATE TABLE conversations (
  id            TEXT PRIMARY KEY,
  kind          text NOT NULL,          -- channel | sealed
  private       INTEGER NOT NULL,
  name          text,                   -- null = ad-hoc channel or sealed chat
  topic         text NOT NULL DEFAULT '',
  welcome       text NOT NULL DEFAULT '',
  section_id    TEXT REFERENCES sections(id) ON DELETE SET NULL,
  parent_id     TEXT REFERENCES conversations(id) ON DELETE SET NULL,  -- sub-channel of this channel; one level only
  default_level text NOT NULL DEFAULT 'member',   -- reader | member
  retention_seconds INTEGER,             -- null = server default
  slow_mode_seconds integer NOT NULL DEFAULT 0,
  locked_at     INTEGER,                 -- private channel closed to new members for good
  locked_by     TEXT,
  member_key    text UNIQUE,            -- ad-hoc: hash of sorted member ids; sealed: the pair
  created_by    TEXT REFERENCES accounts(id),
  created_at    INTEGER NOT NULL,
  archived_at   INTEGER,
  next_seq      INTEGER NOT NULL DEFAULT 1,
  last_message_at INTEGER,
  moved_from_message_id TEXT,           -- thread this channel was made from
  ticket_number integer,
  stream_id     TEXT,                   -- shared with other servers; see streams
  key_epoch     INTEGER,                 -- sealed chats: current key epoch
  -- broad_mentions text              -- added by migration 0002: who may use @channel, @here, @everyone here: anyone | managers | null = follow the server
  -- lazy_delivery text               -- added by migration 0004: on | off | null = follow the server's large_channel_limit (ruling R66)
  -- announcement boolean NOT NULL DEFAULT false          -- added by migration 0004: only managers and members with may_post post
  -- announcement_replies boolean NOT NULL DEFAULT false  -- added by migration 0004: members may reply in threads of an announcement channel
  -- closed_at bigint                 -- added by migration 0009: a direct or sealed chat that was closed (ruling R78)
  -- request_state text               -- added by migration 0009: waiting | declined | null = open
  order_floor   INTEGER NOT NULL DEFAULT 0  -- 0 unless the conversation was shared once and no longer is: then one more than the highest `at` it held, and messages posted since are ordered after everything from that time
);
CREATE INDEX conversations_name ON conversations (name) WHERE name IS NOT NULL;   -- names are not unique
CREATE INDEX conversations_parent ON conversations (parent_id) WHERE parent_id IS NOT NULL;

CREATE TABLE members (
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  level           text NOT NULL,        -- reader | member | manager (highest of direct and group)
  direct_level    text,                 -- level held directly; null = only through groups
  via_group       INTEGER NOT NULL DEFAULT 0,
  joined_at       INTEGER NOT NULL,
  read_seq        INTEGER NOT NULL DEFAULT 0,
  mode            text NOT NULL,        -- what notifies: any | mentions | nothing
  show            text,                 -- sidebar: always | activity | notification | hidden; null = the app's own default
  mode_before_mute text,
  muted_until     INTEGER,
  starred         INTEGER NOT NULL DEFAULT 0,
  thread_view     text,                 -- threaded | flat | null = account preference
  last_post_at    INTEGER,               -- for slow mode
  -- ignore_broad boolean NOT NULL DEFAULT false   -- added by migration 0002: broad mentions write no mention row for this member
  -- may_post boolean NOT NULL DEFAULT false       -- added by migration 0004: may post in an announcement channel without being a manager
  PRIMARY KEY (conversation_id, account_id)
);
CREATE INDEX members_account ON members (account_id);

CREATE TABLE channel_groups (
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  group_id        TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  level           text NOT NULL,
  added_by        TEXT,
  added_at        INTEGER NOT NULL,
  PRIMARY KEY (conversation_id, group_id)
);

CREATE TABLE timeouts (
  id              TEXT PRIMARY KEY,
  account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  conversation_id TEXT REFERENCES conversations(id) ON DELETE CASCADE,  -- null = whole server
  until           INTEGER NOT NULL,
  reason          text NOT NULL DEFAULT '',
  created_by      TEXT NOT NULL,
  created_at      INTEGER NOT NULL,
  lifted_at       INTEGER
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

-- reference_patterns: added by migration 0006 (ruling R73). Text such as
-- PROJ-123 that becomes a link: the server's (no conversation) or one
-- channel's. Kept whole in memory by the server.
-- CREATE TABLE reference_patterns (
--   id              uuid PRIMARY KEY,
--   conversation_id uuid REFERENCES conversations(id) ON DELETE CASCADE,  -- null = the server's
--   name            text NOT NULL,
--   prefix          text NOT NULL,
--   expression      text NOT NULL DEFAULT '',  -- '' = 1 to 12 digits
--   url_template    text NOT NULL,             -- with {value}
--   created_by      uuid,
--   created_at      bigint NOT NULL
-- );
-- CREATE INDEX reference_patterns_conversation ON reference_patterns (conversation_id);

-- conversations.knock: added by migration 0007 (ruling R79), boolean NOT NULL
-- DEFAULT false. With private, a knock channel: seen by owner, admin and
-- member accounts outside it (name, topic, member count, managers), never read
-- by them, joined only when a manager accepts their knock or adds them.
-- knocks: added by migration 0007 (rulings R74, R79). Someone asks the managers
-- of a knock channel to add them; one waits per account and channel at a time.
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

-- chat_requests and ghost_members: added by migration 0009 (rulings R77,
-- R78). A new
-- direct or sealed chat that waits for the person asked; someone a viewer is
-- shown as a member who is not one.
-- CREATE TABLE chat_requests (
--   id              uuid PRIMARY KEY,
--   requester_id    uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
--   recipient_id    uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
--   kind            text NOT NULL,          -- direct | sealed
--   conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL,
--   state           text NOT NULL,          -- pending | accepted | declined | dismissed | withdrawn
--   shown_open      boolean NOT NULL DEFAULT false,
--   created_at      bigint NOT NULL,
--   answered_at     bigint,
--   closed_at       bigint
-- );
-- CREATE UNIQUE INDEX chat_requests_open ON chat_requests (requester_id, recipient_id, kind) WHERE state IN ('pending', 'dismissed');
-- CREATE TABLE ghost_members (
--   conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
--   account_id      uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
--   shown_to        uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
--   created_at      bigint NOT NULL,
--   PRIMARY KEY (conversation_id, account_id, shown_to)
-- );

-- -------------------------------------------------------------- messages

CREATE TABLE messages (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  seq             INTEGER NOT NULL,      -- the arrival number on this server: the seq of the event that created the message
  author_id       TEXT NOT NULL REFERENCES accounts(id),
  parent_id       TEXT REFERENCES messages(id) ON DELETE CASCADE,
  quote_message_id TEXT,
  quote_start     integer,
  quote_end       integer,
  client_id       TEXT,
  text            text NOT NULL DEFAULT '',
  entities        TEXT NOT NULL DEFAULT '[]',
  cards           TEXT NOT NULL DEFAULT '[]',
  fallback        text NOT NULL DEFAULT '',
  search_text     text NOT NULL DEFAULT '',  -- plain text for the search index: names in place of links, plus the fallback; empty for sealed and deleted messages
  system          TEXT,                -- server-posted notices
  via_kind        text,
  via_name        text,
  via_account_id  TEXT,
  via_icon_file_id TEXT,
  quiet           INTEGER NOT NULL DEFAULT 0,  -- sealed chats: does not notify or count as unread
  ciphertext      BLOB,                -- sealed chats: the whole content
  key_epoch       INTEGER,               -- sealed chats: the key epoch this was encrypted in
  notified_anyway INTEGER NOT NULL DEFAULT 0,
  version         integer NOT NULL DEFAULT 1,
  created_at      INTEGER NOT NULL,
  edited_at       INTEGER,
  deleted_at      INTEGER,
  deleted_by      TEXT,
  history_hidden_at INTEGER,             -- a purge was asked for; history hidden from non-admins
  purged_at       INTEGER,
  -- thread summary, kept on the parent
  reply_count     integer NOT NULL DEFAULT 0,
  last_reply_seq  INTEGER,
  last_reply_at   INTEGER,
  last_reply_ord  text,                 -- the order string of the reply at last_reply_seq ('' = by that seq)
  -- where the message is shown (ruling R57)
  ord             text NOT NULL DEFAULT '',  -- the order string; '' = by seq, in a conversation that was never shared
  late            INTEGER NOT NULL DEFAULT 0,  -- arrived after messages ordered later than it had been delivered here
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
  message_id  TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  version     integer NOT NULL,
  text        text NOT NULL,
  entities    TEXT NOT NULL,
  cards       TEXT NOT NULL,
  file_ids    TEXT NOT NULL,
  replaced_at INTEGER NOT NULL,
  replaced_by TEXT NOT NULL,
  reason      text NOT NULL,            -- edit | delete
  PRIMARY KEY (message_id, version)
);

CREATE TABLE files (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  message_id      TEXT REFERENCES messages(id) ON DELETE SET NULL,
  uploader_id     TEXT NOT NULL REFERENCES accounts(id),
  purpose         text NOT NULL DEFAULT 'attachment',  -- attachment | card | avatar | emoji
  position        integer NOT NULL DEFAULT 0,
  name            text NOT NULL,
  caption         text NOT NULL DEFAULT '',
  size            INTEGER NOT NULL,
  media_type      text NOT NULL,
  kind            text NOT NULL,        -- image | video | audio | text | file
  width           integer,
  height          integer,
  duration_seconds integer,
  preview_text    text,
  sha256          BLOB NOT NULL,
  state           text NOT NULL DEFAULT 'ready',  -- ready | arriving | unavailable | too_large (files from other servers)
  blob_key        text,                 -- 'disk:<path>' or 's3:<key>'; null until a remote file has arrived
  poster_key      text,                 -- video poster image sent by the uploading client
  thumb_key       text,
  detached_at     INTEGER,               -- removed from its message by an edit or delete; kept for history
  created_at      INTEGER NOT NULL
);
CREATE INDEX files_message ON files (message_id);
CREATE INDEX files_orphans ON files (created_at) WHERE message_id IS NULL;
-- files.conversation_id is null-able in practice for avatars and emoji: those rows
-- use the server's own "library" conversation, created at setup and visible to nobody.

CREATE TABLE checklist_items (
  message_id  TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  item_id     text NOT NULL,
  position    integer NOT NULL,
  text        text NOT NULL,
  checked     INTEGER NOT NULL DEFAULT 0,
  checked_by  TEXT,
  checked_at  INTEGER,
  removed_at  INTEGER,                   -- removed by an edit; kept for history
  PRIMARY KEY (message_id, item_id)
);

CREATE TABLE checklist_history (
  id          TEXT PRIMARY KEY,
  message_id  TEXT NOT NULL,
  item_id     text NOT NULL,
  account_id  TEXT NOT NULL,
  checked     INTEGER NOT NULL,
  at          INTEGER NOT NULL,
  FOREIGN KEY (message_id, item_id) REFERENCES checklist_items(message_id, item_id) ON DELETE CASCADE
);
CREATE INDEX checklist_history_item ON checklist_history (message_id, item_id, at);

CREATE TABLE forms (
  id            TEXT PRIMARY KEY,
  message_id    TEXT UNIQUE REFERENCES messages(id) ON DELETE CASCADE,  -- null = a bot's dialog
  dialog_for    TEXT REFERENCES accounts(id) ON DELETE CASCADE,         -- the one person a dialog is shown to
  owner_id      TEXT NOT NULL REFERENCES accounts(id),
  preset        text,
  title         text NOT NULL DEFAULT '',
  fields        TEXT NOT NULL,
  buttons       TEXT NOT NULL DEFAULT '[]',
  action        TEXT NOT NULL,
  anonymous     INTEGER NOT NULL DEFAULT 0,
  results       text NOT NULL DEFAULT 'live',
  allow_added_options INTEGER NOT NULL DEFAULT 0,
  answerer_group_id TEXT,               -- only this group may answer
  one_answer    INTEGER NOT NULL DEFAULT 1,
  closes_at     INTEGER,
  closed_at     INTEGER,
  closed_by     TEXT,
  answer_count  integer NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL
);

CREATE TABLE form_answers (
  id          TEXT PRIMARY KEY,
  form_id     TEXT NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
  account_id  TEXT NOT NULL REFERENCES accounts(id),
  button_id   text,
  answers     TEXT NOT NULL,
  result_message_id TEXT,               -- the reply or channel message a thread or channel action produced
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER
);
CREATE INDEX form_answers_form ON form_answers (form_id, account_id);

CREATE TABLE form_tallies (             -- counts per option, kept in step with form_answers
  form_id    TEXT NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
  field_id   text NOT NULL,
  option_id  text NOT NULL,
  count      integer NOT NULL DEFAULT 0,
  PRIMARY KEY (form_id, field_id, option_id)
);

CREATE TABLE reactions (
  message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  emoji      text NOT NULL,             -- the emoji itself, :name: for a custom one, :name@server: for another server's
  at         INTEGER NOT NULL,
  PRIMARY KEY (message_id, emoji, account_id)
);

CREATE TABLE mentions (
  message_id      TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  conversation_id TEXT NOT NULL,
  seq             INTEGER NOT NULL,
  parent_id       TEXT,
  kind            text NOT NULL,        -- account | group | channel | here
  PRIMARY KEY (message_id, account_id)
);
CREATE INDEX mentions_unread ON mentions (account_id, conversation_id, seq);

CREATE TABLE thread_states (
  parent_id   TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  state       text NOT NULL,            -- following | ignored
  read_seq    INTEGER NOT NULL DEFAULT 0,
  updated_at  INTEGER NOT NULL,
  PRIMARY KEY (parent_id, account_id)
);
CREATE INDEX thread_states_account ON thread_states (account_id, state);

CREATE TABLE pins (
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  message_id      TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  pinned_by       TEXT NOT NULL,
  pinned_at       INTEGER NOT NULL,
  PRIMARY KEY (conversation_id, message_id)
);

CREATE TABLE bookmarks (
  account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  message_id  TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (account_id, message_id)
);

CREATE TABLE emoji (
  id          TEXT PRIMARY KEY,
  name        text NOT NULL,
  file_id     TEXT NOT NULL REFERENCES files(id),
  created_by  TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  server_id   TEXT REFERENCES servers(id)   -- came with a shared channel
);
CREATE UNIQUE INDEX emoji_local_name ON emoji (name) WHERE server_id IS NULL;
CREATE UNIQUE INDEX emoji_remote_name ON emoji (server_id, name) WHERE server_id IS NOT NULL;

CREATE TABLE preview_cache (
  url_hash    BLOB PRIMARY KEY,
  url         text NOT NULL,
  card        TEXT,                    -- null = nothing usable at that address
  fetched_at  INTEGER NOT NULL
);

CREATE TABLE events (                   -- the catch-up log
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  seq             INTEGER NOT NULL,
  type            text NOT NULL,
  message_id      TEXT,                 -- the message this event carries, so erasing it can find the rows
  payload         TEXT NOT NULL,
  at              INTEGER NOT NULL,
  PRIMARY KEY (conversation_id, seq)
);
CREATE INDEX events_at ON events (at);
CREATE INDEX events_message ON events (message_id) WHERE message_id IS NOT NULL;

CREATE TABLE notify_anyway (            -- who a message was pushed through do not disturb to
  message_id  TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  by_id       TEXT NOT NULL,
  at          INTEGER NOT NULL,
  delivered   INTEGER NOT NULL,         -- false = quietly not applied; never revealed
  PRIMARY KEY (message_id, account_id)
);

-- ------------------------------------------------------------ moderation

CREATE TABLE purge_requests (
  id            TEXT PRIMARY KEY,
  message_id    TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  requested_by  TEXT NOT NULL,
  requested_at  INTEGER NOT NULL,
  decided_by    TEXT,
  decided_at    INTEGER,
  outcome       text                    -- shown | hidden | erased | null = waiting
);
CREATE INDEX purge_requests_waiting ON purge_requests (requested_at) WHERE outcome IS NULL;

CREATE TABLE reports (
  id            TEXT PRIMARY KEY,
  message_id    TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  conversation_id TEXT NOT NULL,
  reported_by   TEXT,                   -- null when the word filter flagged it
  source        text NOT NULL DEFAULT 'person',  -- person | filter
  reason        text NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL,
  handled_by    TEXT,
  handled_at    INTEGER,
  outcome       text                    -- dismissed | deleted | timed_out | null = waiting
);

CREATE TABLE word_filters (
  id          TEXT PRIMARY KEY,
  pattern     text NOT NULL,
  is_regex    INTEGER NOT NULL DEFAULT 0,
  action      text NOT NULL,            -- block | flag
  created_by  TEXT NOT NULL,
  created_at  INTEGER NOT NULL
);

-- ------------------------------------------------- scheduling and replies

CREATE TABLE scheduled (
  id              TEXT PRIMARY KEY,
  account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  kind            text NOT NULL,        -- message | reminder | form
  content         TEXT NOT NULL,       -- a message to post, as the API accepts it
  about_message_id TEXT,                -- "remind me about this"
  next_run_at     INTEGER,               -- null = finished
  repeat          TEXT,                -- {"every": "day"|"week"|"month", "days": "1111100", "at": "09:30", "time_zone": "..."}
  last_run_at     INTEGER,
  failed_at       INTEGER,
  failure         text,
  created_at      INTEGER NOT NULL,
  cancelled_at    INTEGER
);
CREATE INDEX scheduled_due ON scheduled (next_run_at) WHERE next_run_at IS NOT NULL;

CREATE TABLE saved_replies (
  id         TEXT PRIMARY KEY,
  owner_id   TEXT REFERENCES accounts(id) ON DELETE CASCADE,  -- null = community command
  name       text NOT NULL,             -- typed as /name
  text       text NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX saved_replies_community ON saved_replies (name) WHERE owner_id IS NULL;
CREATE UNIQUE INDEX saved_replies_personal ON saved_replies (owner_id, name) WHERE owner_id IS NOT NULL;

-- ------------------------------------------------------ bots and webhooks

CREATE TABLE webhooks (
  id              TEXT PRIMARY KEY,
  bot_id          TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  token_hash      BLOB UNIQUE,         -- null until the requester collects the address
  signing_secret  BLOB,                -- sealed; verifies GitHub's X-Hub-Signature-256
  name            text NOT NULL,
  purpose         text NOT NULL DEFAULT '',
  requested_by    TEXT NOT NULL,
  requested_at    INTEGER NOT NULL,
  decided_by      TEXT,
  decided_at      INTEGER,
  state           text NOT NULL,        -- waiting | approved | declined | revoked
  last_used_at    INTEGER
);

CREATE TABLE commands (
  id          TEXT PRIMARY KEY,
  bot_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name        text NOT NULL UNIQUE,
  description text NOT NULL,
  hint        text NOT NULL DEFAULT ''
);

-- ----------------------------------------------------------------- calls

CREATE TABLE calls (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  message_id      TEXT,
  started_by      TEXT NOT NULL,
  started_at      INTEGER NOT NULL,
  ended_at        INTEGER,
  host_server_id  TEXT REFERENCES servers(id)   -- null = this server carries the media
);
CREATE UNIQUE INDEX calls_live ON calls (conversation_id) WHERE ended_at IS NULL;

CREATE TABLE call_participants (
  call_id     TEXT NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
  account_id  TEXT NOT NULL,
  joined_at   INTEGER NOT NULL,
  left_at     INTEGER,
  PRIMARY KEY (call_id, account_id, joined_at)
);

-- ------------------------------------------------- links to other servers

CREATE TABLE peers (
  id            TEXT PRIMARY KEY,
  kind          text NOT NULL,          -- server | hub
  server_id     TEXT REFERENCES servers(id),   -- for kind = server
  hub_name      text,
  hub_sign_key  BLOB,
  url           text,                   -- where this server dials; null if the peer dials us
  dials         INTEGER NOT NULL,
  state         text NOT NULL,          -- invited | active | paused | removed
  invite_hash   BLOB,
  invite_expires_at INTEGER,
  created_by    TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  connected_at  INTEGER,
  last_error    text,
  clock_told    TEXT,                  -- what the peer, as a time reference, last told this server about this server's clock; null until told
  clock_measured TEXT,                 -- what this server, as a time reference, last measured of the peer's clock (direct links)
  away          TEXT                   -- direct links: the last few stretches the peer was disconnected, as [from, to] pairs by this server's clock
);

CREATE TABLE peer_servers (             -- servers reachable through a hub
  peer_id    TEXT NOT NULL REFERENCES peers(id) ON DELETE CASCADE,
  server_id  TEXT NOT NULL REFERENCES servers(id),
  PRIMARY KEY (peer_id, server_id)
);

CREATE TABLE streams (
  id              TEXT PRIMARY KEY,
  peer_id         TEXT NOT NULL REFERENCES peers(id),
  conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,  -- null until accepted locally
  owner_server_id TEXT REFERENCES servers(id),   -- null = this server owns it
  sequencer       text NOT NULL,        -- self | owner | hub: who is the stream's time reference and keeps its log. The column keeps its old name; nobody numbers events any more
  kind            text NOT NULL,        -- channel | adhoc | sealed
  state           text NOT NULL,        -- offered | active | left | removed
  key_epoch       integer NOT NULL DEFAULT 0,
  last_seq        INTEGER NOT NULL DEFAULT 0,   -- a delivery cursor: for sequencer = self, the last position written to stream_log; otherwise the log position of the last envelope received from whoever delivers. It orders nothing
  first_seq       INTEGER NOT NULL DEFAULT 0,   -- not used
  servers_version integer NOT NULL DEFAULT 0,
  max_blob_bytes  INTEGER,
  history_days    integer NOT NULL DEFAULT 0,
  offered_name    text,
  offered_at      INTEGER,
  accepted_by     TEXT,
  created_at      INTEGER NOT NULL,
  home_hub        text,                 -- through a hub: the name of the hub that is the stream's home and its time reference. Today always the hub of peer_id
  max_at          INTEGER NOT NULL DEFAULT 0,   -- the highest `at` of any event this server has applied or made in the stream
  acked_at        INTEGER NOT NULL DEFAULT 0,   -- the `at` of the last of this server's own events the time reference acknowledged
  top_ord         text NOT NULL DEFAULT ''     -- the highest order string of any message delivered here; a message below it arrives late
);

CREATE TABLE stream_servers (
  stream_id  TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  server_id  TEXT NOT NULL REFERENCES servers(id),
  state      text NOT NULL,             -- invited | active | removed
  last_n     INTEGER NOT NULL DEFAULT 0, -- highest origin counter applied from this server
  last_at    INTEGER NOT NULL DEFAULT 0, -- the `at` of that event: an origin's `at` only grows
  logged_n   INTEGER NOT NULL DEFAULT 0, -- for a stream this server is the time reference of: the counter of this server's last envelope in stream_log
  joined_at  INTEGER,
  PRIMARY KEY (stream_id, server_id)
);

CREATE TABLE stream_keys (
  stream_id  TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  epoch      integer NOT NULL,
  key        BLOB NOT NULL,            -- sealed
  created_at INTEGER NOT NULL,
  PRIMARY KEY (stream_id, epoch)
);

CREATE TABLE outbox (                   -- this server's events that the stream's time reference has not acknowledged yet
  id          TEXT PRIMARY KEY,         -- the origin event id
  stream_id   TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  envelope    BLOB NOT NULL,
  created_at  INTEGER NOT NULL,
  attempts    integer NOT NULL DEFAULT 0,
  next_try_at INTEGER NOT NULL
);
CREATE INDEX outbox_due ON outbox (next_try_at);

CREATE TABLE stream_seen (              -- origin event ids already applied, to drop duplicates
  stream_id       TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  origin_event_id TEXT NOT NULL,
  seq             INTEGER NOT NULL,
  at              INTEGER NOT NULL,      -- rows older than 35 days are pruned
  PRIMARY KEY (stream_id, origin_event_id)
);

CREATE TABLE stream_log (               -- for streams this server is the time reference of: every envelope it took, in the order it took them; kept 30 days
  stream_id  TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  seq        INTEGER NOT NULL,           -- the position in this log: a delivery cursor, not an order
  origin     text NOT NULL DEFAULT '',  -- the name of the server that made the event
  n          INTEGER NOT NULL DEFAULT 0, -- that server's counter, as signed in the envelope
  prev_n     INTEGER NOT NULL DEFAULT 0, -- the counter of the same origin's envelope before this one in the log
  origin_event_id TEXT,
  envelope   BLOB NOT NULL,
  at         INTEGER NOT NULL,           -- when it was taken, by this server's clock
  PRIMARY KEY (stream_id, seq)
);
CREATE INDEX stream_log_origin ON stream_log (stream_id, origin, n);
CREATE INDEX stream_log_event ON stream_log (stream_id, origin_event_id);


CREATE TABLE stream_channels (          -- the hub-signed record of each hub-owned channel this server is in or invited to
  stream_id  TEXT PRIMARY KEY REFERENCES streams(id) ON DELETE CASCADE,
  version    INTEGER NOT NULL,
  home_hub   text NOT NULL,             -- the hub that is the stream's home
  record     BLOB NOT NULL,            -- the signed bytes, as received
  sig        BLOB NOT NULL,
  makers     text NOT NULL DEFAULT '{}', -- key epoch -> the server the hub named to make its key
  moderators text NOT NULL DEFAULT '[]'  -- the account ids on the record's moderator list that are accounts of this server; internal/perm reads it
);

CREATE TABLE stream_moves (             -- moves of a direct-link stream onto a hub, one row per attempt, on both servers
  id              TEXT PRIMARY KEY,     -- the move_id; the owner server makes it
  stream_id       TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
  from_peer_id    TEXT NOT NULL REFERENCES peers(id),   -- the direct link the stream ran over
  hub_peer_id     TEXT REFERENCES peers(id),            -- this server's link to the hub named
  hub_name        text NOT NULL,
  hub_fingerprint text NOT NULL,
  state           text NOT NULL,        -- proposed | accepted | committed | done | declined | abandoned
  reason          text,                 -- why it was declined or abandoned
  commit_seq      INTEGER,               -- on the owner server: the position of stream.moved in the direct log; the move is done when the other server has acknowledged it
  commit_event_id TEXT,                 -- the origin event id of stream.moved
  accept          BLOB,                -- the other server's signed agreement (a move_accept block), and its signature
  accept_sig      BLOB,
  commit_block    BLOB,                -- the owner server's signed commit (a channel_commit block), and its signature; set in the transaction that commits
  commit_sig      BLOB,
  proposed_by     TEXT,                 -- the local owner who proposed it
  answered_by     TEXT,                 -- the local owner who accepted or declined it
  created_at      INTEGER NOT NULL,
  expires_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);
CREATE INDEX stream_moves_stream ON stream_moves (stream_id, created_at);

-- ---------------------------------------------------------- sealed chats

CREATE TABLE device_lists (             -- every version of each account's signed list of its devices
  account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  version     INTEGER NOT NULL,
  signed_list BLOB NOT NULL,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (account_id, version)
);

CREATE TABLE sealed_epochs (            -- one record per key epoch per sealed chat
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  key_epoch       INTEGER NOT NULL,
  seq             INTEGER NOT NULL,
  device_id       TEXT NOT NULL,        -- the device that made it
  record          BLOB NOT NULL,       -- signed; holds the epoch secret sealed to each device
  created_at      INTEGER NOT NULL,
  ord             text NOT NULL DEFAULT '',  -- in a chat shared with another server: the order string of the stamp of the event that carried this record. Of two records made for one key epoch the one with the lower stands
  PRIMARY KEY (conversation_id, key_epoch)
);

CREATE TABLE backup_keys (              -- the account's backup key, locked once per way of opening it
  id          TEXT PRIMARY KEY,
  account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  kind        text NOT NULL,            -- passkey | phrase | device
  passkey_id  TEXT REFERENCES passkeys(id) ON DELETE CASCADE,
  device_id   TEXT REFERENCES devices(id) ON DELETE CASCADE,
  key_version integer NOT NULL,
  locked_key  BLOB NOT NULL,
  created_at  INTEGER NOT NULL
);
CREATE INDEX backup_keys_account ON backup_keys (account_id);

CREATE TABLE key_backups (
  account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  key_epoch       INTEGER NOT NULL,
  key_version     integer NOT NULL,
  record          BLOB NOT NULL,       -- encrypted under the backup key
  created_at      INTEGER NOT NULL,
  PRIMARY KEY (account_id, conversation_id, key_epoch)
);

CREATE TABLE account_records (          -- the account key and backup settings, encrypted under the backup key
  account_id      TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  key_version     integer NOT NULL,
  revision        INTEGER NOT NULL,
  record          BLOB NOT NULL,
  replacement_due INTEGER NOT NULL DEFAULT 0,  -- the backup key must be replaced at next unlock
  updated_at      INTEGER NOT NULL
);

CREATE TABLE unlock_requests (          -- a new device asking an unlocked one for the backup key
  id           TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  device_id    TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  request      BLOB NOT NULL,
  answer       BLOB,                   -- sealed to the asking device
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  answered_at  INTEGER
);

-- ---------------------------------------------------------------- search
--
-- SQLite:

CREATE VIRTUAL TABLE messages_fts USING fts5(
  body, message_id UNINDEXED, conversation_id UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);
INSERT INTO messages_fts(messages_fts, rank) VALUES ('secure-delete', 1);
--
-- The store keeps messages_fts in step with messages in the same transaction:
-- insert on create, replace on edit, delete on delete, purge and retention.
-- Sealed-chat messages and deleted messages are never indexed.
-- The database is opened with PRAGMA secure_delete = ON and journal_mode = WAL.
