# Schema

A table-by-table explanation of `docs/schema.sql` as it stands. That
file is the source of truth for names and types; this document says
what each table is for, when its rows are deleted, and what the
`store` package must keep true.

`CONVENTIONS.md` gives the rules for identifiers and times. Where the
schema leaves something open, the simplest choice was made; those are
marked "(decision Sn)" and listed in section 19.

## 1. Rules that apply to every table

- **Identifiers** are UUIDv7. PostgreSQL stores them as `uuid`,
  SQLite as 36 characters of text.
- **Times** are `bigint` milliseconds since the Unix epoch, in
  columns ending `_at` (a moment) or `_until` (a moment something
  ends). Two `_until` columns use `0` to mean "until turned off"; the
  column notes say which.
- **JSON columns** hold the same shapes the API returns. The store
  does not query inside them in SQL, so both databases behave alike
  (decision S1).
- **"Sealed"** on a `bytea` column means encrypted with AES-256-GCM
  under the key file in `keys/`. It has nothing to do with sealed
  chats.
- **Hashes.** Tokens and codes are stored only as SHA-256 hashes.
- **Foreign keys** are enforced on both databases. SQLite is opened
  with `PRAGMA foreign_keys = ON`.
- **Writes that belong together share a transaction.** In particular
  a message, its event, its mentions, its search entry and the
  conversation's `next_seq` are written together or not at all.

### Four ways a row is deleted

| Name            | What triggers it                                   |
|-----------------|----------------------------------------------------|
| Retention       | The janitor finds a message older than its conversation's period (section 14.1). |
| Purge           | An author's request in a private or ad-hoc channel, an admin's decision, or an admin's direct purge (section 14.2). |
| Account removal | An admin removes or bans an account (section 14.3). |
| Janitor         | Housekeeping by age: expired sessions, old events, orphaned uploads and so on (section 14.4). |

An `accounts` row itself is never deleted, because messages name
their author (decision S2). Many tables say `ON DELETE CASCADE` on
`account_id`; that is a safety net. On removal the store deletes the
rows listed in section 14.3 itself.

## 2. Server

### `schema_migrations`

One row per migration that has been applied. The migration runner
reads and writes it and nothing else does (section 16). Rows are never
deleted.

| Column       | Meaning |
|--------------|---------|
| `version`    | The migration's number. Primary key. |
| `name`       | The migration's file name without `.sql`, such as `0007_knocks`. Added by the runner, not by a migration (section 16); null only until the runner first starts on a database created before names were recorded, when it fills each row in from the migration of that number. |
| `applied_at` | When it was applied. |

### `settings`

One row per server setting. `key` is a dotted name, `value` is JSON,
`updated_by` is the admin who last changed it (null for the server or
the command line).

The keys are these. Sign-in policy uses the `auth.*` names of
`AUTH.md`; the rest are also listed in `OPERATIONS.md`, with what
each does.

| Key                                 | Value and default                          |
|-------------------------------------|--------------------------------------------|
| `server.name`                       | The community's display name               |
| `server.icon_file_id`               | `null`                                     |
| `server.time_zone`                  | IANA name used when an account has none; `"UTC"` |
| `setup.token_hash`, `setup.done_at` | First-run setup (FEATURES section 26)      |
| `welcome.text`                      | `""`, Churr Markdown                       |
| `registration.mode`                 | `open`, `invite` (default), `closed`       |
| `invites.members_may_invite`           | `false`                                    |
| `auth.member_sign_in`               | `passkey` (default), `either`, `sso`       |
| `auth.second_factor`                | `off` (default), `required`                |
| `auth.second_factor_accepted`       | A list from `passkey`, `totp`, `provider`  |
| `sessions.passkey_seconds`, `sessions.sso_seconds` | `2592000`, `86400`          |
| `messages.quick_correction_seconds` | `30`                                       |
| `messages.edit_window_seconds`      | `null` = no limit                          |
| `retention.seconds`                 | `null` = keep until deleted                |
| `mentions.broadcast_managers_only`  | `false`                                    |
| `mentions.broad_mention_limit`      | `250`. Above this many members only a channel's managers and the admins use `@channel`, `@here` and `@everyone` there. `0` = no limit (ruling R64) |
| `roles.new_owner_delay_hours`      | `0`. Hours someone made an Owner waits before becoming one, in `owner_promotions` (one row per account: who asked, when, when it takes effect); any Owner withdraws it meanwhile. `0` = at once |
| `emoji.members_may_add`             | `false`                                    |
| `blocking.enabled`                  | `true`                                     |
| `webhooks.managers_may_approve`     | `false`                                    |
| `files.max_bytes`                   | `104857600`                                |
| `files.store`, `files.s3`, `files.s3_secret` | `"disk"`; object-store details; its secret, sealed |
| `push.relay_enabled`, `push.relay_url`, `push.unifiedpush_enabled`, `push.send_badge` | `false`; `""` (no relay); `false`; `true`. The relay is used only when it is switched on and has an address |
| `previews.enabled`                  | `false`                                    |
| `sealed.enabled`                    | `true`                                     |
| `calls.enabled`, `calls.max_participants`, `calls.max_cameras` | `true`, `50`, `12` |
| `links.send_presence`, `links.default_max_file_bytes` | `true`, `26214400`        |
| `tickets.next.<prefix>`             | Next ticket number for a name prefix; starts at `1` |
| `themes.default_dark`, `themes.default_light` | `null`; or a copy of a theme (`id`, `name`, `kind`, `colours`, `author_id`) set with `setDefaultTheme` (FEATURES section 29) |
| `link.home_clocks.<peer>`           | Written by the server, not an admin setting. For a hub peer (`<peer>` is its `peers.id`): what that hub last told this server of its clock against each other hub of the hub's cluster, as an object from hub name to `offset_ms`, `uncertainty_ms` and `measured_at`. Absent until the hub has told any. Read after a restart, so the server can stamp events in a channel homed on another hub before its hub's first ping |

Rows are never deleted. A setting returns to its default by writing
the default.

### `audit_log`

One row per admin, owner, integration or command-line action.

| Column        | Meaning                                              |
|---------------|------------------------------------------------------|
| `actor_id`    | The account, or the integration's id. Null for the server and the command line. |
| `actor_kind`  | `account`, `integration`, `server` or `cli`.         |
| `action`      | A name in the same `thing.happened` style as events. |
| `target_kind`, `target_id` | What it was done to. `target_id` is text because some targets are not UUIDs (a setting key). |
| `detail`      | Before and after values, a reason, a duration. Never message text or file contents. |

Rows are never deleted or changed (decision S4). Nothing done inside
an ad-hoc channel is written here, and an author's own purge in a
private channel is not written here, because the log is read by
admins who must not learn of either (PERMISSIONS sections 8.2 and
9.2).

## 3. Accounts and sign-in

### `servers`

Other Churr servers this one has met, through a direct link or a hub.
`name` is the host name. `sign_key` and `box_key` are the pinned
public keys. `blocked_at` set means this server accepts nothing from
it.

Rows are kept for as long as any remote account, stream or emoji
refers to them, which in practice is for good.

### `accounts`

Every person, bot, webhook account and remote shadow.

| Column             | Meaning                                         |
|--------------------|-------------------------------------------------|
| `server_id`        | Null for a local account. Set for a shadow of an account on that server. |
| `role`             | `owner`, `admin`, `member`, `guest`, `bot`, or `remote` for a shadow. |
| `avatar_file_id`   | A `files` row with purpose `avatar`. No foreign key. |
| `time_zone`        | IANA name, kept current by the person's client. Used for quiet hours and reminders, and shown to others as local time. |
| `webauthn_handle`  | The random user handle given to passkeys. Not the account id, so a passkey does not reveal it. |
| `claimed_at`       | Null for an account made by the Slack importer that nobody has claimed. Set at creation for every other account. |
| `removed_at`, `removed_by` | Set on removal.                         |
| `profile_version`  | For a shadow of a remote account: the version of the newest profile its home server sent and this server applied. An older one arriving later is ignored. `0` for local accounts. |
| `status_emoji`, `status_text`, `status_until` | The personal status. `status_until` null means until cleared. |
| `status_dnd`, `status_away` | The status's two switches: pause notifications, appear away. They mean nothing once the status has ended. |
| `availability`, `availability_until` | `meeting`, `out_of_office` or null, and when it ends. |
| `availability_by`  | `self`, or `integration:<id>` for the integration that set it. |
| `away_until`       | Appear away. Null = off, `0` = until turned off, otherwise the end. |
| `dnd_until`        | Manual do not disturb. Null = off, `0` = until turned off, otherwise the end. |
| `quiet_start`, `quiet_end` | Quiet hours as `HH:MM` in `time_zone`. Null = no schedule. Switching quiet hours off clears these and `quiet_days`. |
| `quiet_days`       | Seven characters, Monday first. A `1` means a quiet period *starts* on that day. |
| `quiet_skip_until` | Quiet periods that end at or before this moment are skipped. |
| `allow_notify_anyway` | Whether others may push a message through this account's do not disturb. |
| `prefs`            | The account's own preferences, below.           |

Keys in `prefs` (decision S5):

| Key                   | Value                                           |
|-----------------------|-------------------------------------------------|
| `thread_view`         | `threaded` (default) or `flat`                  |
| `meeting_pauses`      | `true` if "in a meeting" also pauses notifications; default `false` |
| `welcome_seen_at`     | When the server welcome text was dismissed      |
| `meeting_pause_skip_until` | Set when the person turns do not disturb off during a meeting that pauses notifications: that meeting's end time, or `0` if it has none. The pause from that meeting is ignored until then. Removed whenever availability is set or cleared. |

Clients may keep other keys there; the server ignores keys it does
not know.

Rows go when: never.

Must hold:

- Exactly one row has role `owner`.
- `server_id` is null if and only if `role` is not `remote`. The
  role `remote` carries no powers here.
- Local usernames are unique (first index), remote ones are unique
  per server (second index), and a local username is never equal to a
  `groups.handle`. No database constraint spans the two tables; the
  store checks both in the transaction that creates either.
- A username an account gave up by renaming is held for that account
  for 30 days (`username_holds`: `username`, `account_id`,
  `released_at`): the store refuses it to any other account, by
  rename or new account, as taken (`name_taken`). The account that
  gave it up may take it back. Renames are audited
  (`account.renamed`, with the old and new names).
- A removed account keeps its row, username, display name, role and
  `created_at`. Its avatar, title, pronouns, time zone, status,
  availability, do not disturb, quiet hours and `prefs` are cleared.
- Expired values (`status_until`, `availability_until`, `away_until`,
  `dnd_until` in the past) are treated as unset by every reader. The
  scheduler clears the columns soon after, and correctness never
  depends on it having done so.

### `bots`

One row for each account with role `bot`: a description, the admin
who made it, and `webhook_only` for the account that stands behind a
webhook. Removed with the bot (the row stays with the soft-removed
account).

Must hold: a row exists if and only if the account's role is `bot`.

### `passkeys`

One row per passkey. `credential` holds what WebAuthn needs (public
key, sign counter, flags). `has_prf` says the passkey can unlock
sealed history. `unlock_public` is the public half of the key pair
derived from that passkey's PRF output, to which the passkey's
locked copy of the backup key is sealed; null for a passkey without
PRF or one no unlocked device has yet made a copy for. Deleted when
the person deletes the passkey and on account removal. Deleting one
deletes its `backup_keys` copy by cascade.

### `access_keys`

Added by migration 0010 (ruling R80). An account's access key
(`AUTH.md` section 5.6): the long random code its person signs in
with when they cannot use a passkey. The server never sees the key.
`auth_hash` is the SHA-256 of the value the page derives from it and
sends. One row per account at most: making a new key replaces the
row. `last_used_at` is set at each sign-in, check or unlock with it.
`unlock_public` (migration 0012, ruling R85) is the public half of the
key pair derived from the key's wrap value, to which the access key's
locked copy of the backup key is sealed (`SEALED.md` section 13.3); it
is given by the page that made the key, in the same call, and is null
for a key made without it. Deleted when the person removes the key and
on account removal; the `backup_keys` copy of kind `access_key` goes
with it, and with a replaced key.

### `sso_providers`

One row per OIDC or SAML provider. `secret` is sealed. `group_claim`
names the claim that lists a person's provider groups; `admin_group`
is the provider group that grants the admin role (`AUTH.md`).
Deleting a provider deletes its `identities` and unmaps its groups.

### `identities`

Links an account to its subject at a provider. The email address is
never stored. Deleted on account removal, after any ban has copied
the provider and subject it needs.

### `totp`

One authenticator-app secret per account, sealed. `confirmed_at` is
null until the first code is accepted. `last_step` refuses a code
being used twice. Deleted when the person removes it and on account
removal.

### `recovery_codes`

Eight per passkey account, as hashes. `used_at` marks a spent code.
Replaced as a set when the person makes new ones. Deleted on account
removal.

### `recovery_links`

One-time links made by an admin, or by `churr recovery`
(`created_by` null). The janitor deletes a row one day after it was
used or expired.

### `sessions`

Every credential that acts as an account. `AUTH.md` is the authority
on how they behave.

| Column          | Meaning                                            |
|-----------------|----------------------------------------------------|
| `kind`          | `web` (cookie), `app` (access and refresh token), `personal` (personal token), `bot` (bot token). There is no webhook kind: webhook secrets are in `webhooks.token_hash`. |
| `token_hash`    | The cookie value, the app's current access token, or the token. |
| `refresh_hash`  | App sessions: the current refresh token.           |
| `prev_refresh_hash`, `rotated_at` | The refresh token just replaced and when. It is tolerated for 60 seconds after the rotation; coming back later than that means it was copied, and the session is revoked. |
| `access_expires_at` | When the app's current access token stops working, which is sooner than the session ends. |
| `expires_at`    | When the session ends. A passkey session slides: each use moves it ahead. Null for personal and bot tokens with no expiry. |
| `max_expires_at`| A hard end that use never moves, such as the 24 hours of a company sign-in. Null if there is none. |
| `fresh_at`      | The last passkey or authenticator check in this session. Changes that need a recent one compare against it and answer `fresh_check_required` if it is too old. |
| `signed_in_by`, `second_factor` | How the session came to exist.     |
| `pending_second_factor` | True between a company sign-in and its second step. Such a session can do nothing else. |
| `revoked_at`    | Set when it is ended by hand, by removal, or by a reuse check. |
| `locked_at`     | Added by migration 0008 (ruling R75). Set while its holder has locked it (`AUTH.md` section 10.6); such a session can only unlock or sign out. Null otherwise. |

Rows go when: account removal deletes them all. The janitor deletes a
row 30 days after it expired or was revoked. Deleting a session does
not delete its device (below).

### `oauth_codes`

Short-lived codes for app sign-in and QR pairing. `account_id` is
null on a pairing code until a signed-in page confirms it. `box_key`
is the public key the app made for the hand-over, and `handover` is
the backup key sealed to it by the unlocked web chat during QR
pairing, so the paired app can read sealed history at once; the
server cannot open it. The janitor deletes a row one hour after it
expired or was used.

### `devices`

One row per installed app or browser profile signed in to one
account.

| Column          | Meaning                                            |
|-----------------|----------------------------------------------------|
| `session_id`    | The session the device is attached to now, at most one device per session. Null after that session ended. A device outlives its session: when the same app signs in again and proves it holds the device key, the row is attached to the new session. |
| `push_kind`, `push_address` | How to wake it: `apns`, `fcm`, `unifiedpush` or `none`, and the device token or UnifiedPush endpoint. |
| `push_failed_at`| Set when the address is reported dead. The server stops using it until the device registers again. |
| `push_public_key`, `push_auth` | The encryption keys a UnifiedPush or Web Push distributor needs, where it needs them. |
| `sign_key`      | The device's Ed25519 public key for sealed chats.  |
| `box_key`       | The device's X25519 public key, to which things meant for this device alone are sealed, epoch secrets among them. |
| `retired_at`    | Set when the device has had no session for 30 days, or was removed by its owner. A retired device is no longer accepted in a device list, and the account's other devices remove it from the sealed chats. |

Rows go on account removal, and the janitor deletes a retired row
once no device list version in force names it. Unlock requests and
device-way `backup_keys` rows go with the device.

### `invites`

| Column              | Meaning                                         |
|---------------------|-------------------------------------------------|
| `role`              | `member` or `guest`.                            |
| `group_ids`         | Groups the new account is put in.               |
| `conversation_ids`  | Channels a guest is added to, at each channel's default level. |
| `claims_account_id` | Set on a claim link for an imported account. Using it takes over that account instead of making a new one. |
| `max_uses`, `uses`  | `max_uses = 0` means no limit.                  |

Must hold: `uses` never exceeds `max_uses` when that is not 0; the
increment and the account creation share a transaction.

Rows go when: the janitor deletes a row 30 days after it expired, was
revoked or was used up. Removing an account revokes its unused
invites. A claim link made by the importer expires 30 days after the
import and can be made again.

### `import_claims`

A keyed hash of the email address an imported account had in Slack,
so company sign-in can claim it. Deleted the moment the account is
claimed by any route.

### `bans`

A ban on an account and, where it had one, on its sign-in identity
(`provider_id`, `subject`, copied from `identities` at the time). One
row per identity; an account with none gets one row with those
columns null. `lifted_at` ends it. Never deleted.

### `blocks`

`account_id` has blocked `blocked_id`. Deleted when unblocked, and in
both directions on account removal.

### `status_presets`

Saved statuses. `owner_id` null is a community preset made by an
admin. `duration_seconds` null means until cleared. `availability`
lets a preset also set in-a-meeting or out-of-office. `position`
orders the list. Personal ones are deleted on account removal.

### `themes`

Colour themes (ruling R61, FEATURES section 29). `owner_id` is the
account that made or added it; `kind` is `dark` or `light`; `colours`
holds exactly the nine base colours as `#rrggbb` in lower case, which
the themes service checked for shape and contrast before storing;
`published_at` is set while the theme is in the server's theme
library and null otherwise.

Must hold:

- An account has at most 50 rows. The store counts them with the
  owner's `accounts` row locked, in the transaction that inserts.
- Deleted on account removal (14.3). Theme cards in messages and the
  server defaults are copies and do not point at a row.

### `integrations`

Server integrations. `allows` lists what was ticked; the only value
is `"availability"`. `revoked_at` ends it. Never deleted, so the
audit log can still name it.

## 4. Groups

### `groups`

| Column        | Meaning                                              |
|---------------|------------------------------------------------------|
| `handle`      | What is typed after `@`. Shares a namespace with local usernames. |
| `open`        | People may join and leave by themselves.             |
| `builtin`     | `everyone` or `admins` for the two built-in groups, else null. |
| `provider_id`, `provider_group` | Set when membership comes from a group at the identity provider. |

Must hold:

- The two built-in rows always exist, with handles `everyone` and
  `admins`, and cannot be edited or deleted.
- A built-in or provider-mapped group is never `open`.

Deleted by an admin. Its `group_members` and `channel_groups` rows go
by cascade, and the store then works out `members` again for every
channel it was in (section 5, `members`).

### `group_members`

Who is in each ordinary group. Not used for built-in groups: the
store works their membership out from `accounts.role`.

- `@everyone` = local accounts with role `owner`, `admin` or
  `member`, claimed and not removed.
- `@admins` = the owner and the admins.

Must hold: no row names a remote or removed account.

Rows go when: the account leaves or is taken out, on account removal,
and when a company sign-in shows the person is no longer in the
mapped provider group.

## 5. Conversations

### `sections`

The headings admins define for the channel list: the default
grouping, which each person can rearrange for themselves
(`sidebars`). `position` is the default order of the sections; nothing
orders channels within one for everyone. Deleting one sets `conversations.section_id` to null. A
sub-channel is shown in its parent's section; its own `section_id`
is kept and ignored while it has a parent.

### `sidebars`

Added by migration 0011 (ruling R83). One person's own sidebar
(`FEATURES.md` section 21.4): one row per account that has changed
anything; no row means the server's default. `layout` is the
`Sidebar` object of the API without its version: own sections,
heading order, sorts and custom orders, placements (each a
conversation and the key of the section it was put in: own, admin's
or Channels), and the order of stars. `version` goes up by one with each change; a write that finds
it moved is refused. Conversations the person has left are dropped
at the next change. Deleted on account removal.

### `conversations`

| Column              | Meaning                                         |
|---------------------|-------------------------------------------------|
| `kind`              | `channel` or `sealed`.                          |
| `private`           | Fixed at creation.                              |
| `name`              | Null for an ad-hoc channel and a sealed chat. Free-form, 1 to 64 characters of any script. Not unique: any number of channels may share a name. The index on it is an ordinary one, for lookups by name. |
| `parent_id`         | For a sub-channel: the named channel it sits under. Local to this server and never sent to linked ones. Deleting the parent sets it to null. |
| `welcome`           | Text shown to a person when they join.          |
| `default_level`     | The level a newcomer gets: `member`, or `reader` for an announcement channel. |
| `retention_seconds` | Null = follow the server. Otherwise the channel's own period. Setting `0` through the API clears it to null (decision S7). |
| `slow_mode_seconds` | `0` = off.                                      |
| `broad_mentions`    | Who may use `@channel`, `@here` and `@everyone` here: `anyone`, `managers`, or null to follow the server's settings (ruling R64; FEATURES section 2.5). The API writes null as `server`. Added by migration 0002. |
| `lazy_delivery`     | Whether the channel is delivered in full only to the connections that have it open: `on`, `off`, or null to follow the server's `channels.large_channel_limit` (ruling R66; EVENTS section 4.1). The API writes null as `server`. Added by migration 0004. |
| `announcement`      | True: an announcement channel, where only its managers and the members with `may_post` post (FEATURES section 21.9). Added by migration 0004. |
| `announcement_replies` | True: in an announcement channel, members who may not post may reply in threads. Always false while `announcement` is. Added by migration 0004. |
| `locked_at`, `locked_by` | Set when a manager locks a private channel. From then on nobody can be added or join. Never cleared. |
| `member_key`        | Finds an existing conversation by who is in it; see below. |
| `next_seq`          | The number the next event will take.            |
| `last_message_at`   | When the newest message was posted. Used to order lists. Not lowered when that message is deleted. |
| `moved_from_message_id` | For a channel made by moving a thread: that thread's parent, which stays in the old channel (FEATURES section 1.6). No foreign key. |
| `ticket_number`     | For a channel opened by a ticket form.          |
| `stream_id`         | Set while the conversation is shared with other servers. No foreign key, because `streams` is defined later and points back. |
| `closed_at`         | A direct or sealed chat that either person closed (ruling R78): read-only for good, and its `member_key` is null so a new chat between the same people is a new conversation. Never says who closed it. Added by migration 0009. |
| `request_state`     | `waiting` while the chat request that started the chat waits, `declined` once the person asked declined it (the member key is then null), null while open. Added by migration 0009. |
| `key_epoch`         | Sealed chats: the current key epoch, a plain counter starting at 1 when the chat is made. A sealed message for any other epoch is refused. Null for channels. |

`member_key` (decision S8):

- Sealed chat: `s:` followed by the two account ids in sorted order,
  joined by `:`. Always set, which is what keeps one sealed chat per
  pair.
- Ad-hoc channel: `a:` followed by the SHA-256, in hex, of the member
  account ids sorted and joined by `,`. Worked out again whenever its
  members change. If the new value is already held by another ad-hoc
  channel, this one's is set to null and the older channel keeps the
  key. Starting a chat with a set of people looks the key up and
  opens that channel, or makes a new one.
- Named channel: null. Naming an ad-hoc channel clears it.
- The library conversation: `library`.

Must hold:

- `kind = sealed` implies `private`, a null `name`, no section, no
  topic, no welcome text, no slow mode, a null `retention_seconds`
  and exactly two `members` rows.
- A null `name` implies `private`.
- Sub-channels are one level deep: a row with a `parent_id` has a
  `name`, is of kind `channel`, points at a row that has a `name`
  and a null `parent_id`, and is itself the parent of no row. The
  store checks this in the transaction that sets `parent_id`;
  nothing in the database enforces it.
- Nothing may rely on `name` to identify a channel.
- `private` may change from false to true and never from true to
  false.
- `locked_at` set implies `private`, a null `stream_id`, and no
  `channel_groups` rows for the conversation. In the transaction
  that sets it, every `members` row with `via_group` true gets
  `direct_level` set to its `level` and `via_group` set to false,
  and the conversation's `channel_groups` rows are deleted.
- `key_epoch` is set if and only if `kind` is `sealed`, and equals
  the highest `sealed_epochs.key_epoch` for the conversation.
- `retention_seconds` is null for ad-hoc channels and sealed chats.
- `next_seq` is greater than every `seq` in `events` and `messages`
  for the conversation, and never goes down. Numbers freed by
  deleted rows are not used again. A writer takes the number by
  locking the row, reading `next_seq` and adding one in the same
  transaction that writes the event. This is so in a conversation
  shared with other servers too: there `seq` is this server's own
  arrival number, an event from another server takes the next one
  when it is applied, and no number means anything on another server
  (ruling R57).
- `order_floor` is 0 unless the conversation was shared with other
  servers once and no longer is. It is then one more than the
  highest `at` of any event the conversation held while it was
  shared (`streams.max_at` at the moment the stream ended), and a
  message stored from then on is ordered after all of them
  (`messages.ord`).
- Every channel that has members has at least one manager.
- Exactly one library conversation exists: `kind = channel`,
  private, no name, no members. It is made at first start and holds
  avatar and emoji files (`files.conversation_id` is `NOT NULL`).

Rows go when:

- The last member of a private or ad-hoc channel leaves or is
  removed: the conversation is deleted, with everything that hangs
  from it, and its files are removed from the file store first.
- A sealed chat's row stays until both accounts are removed.
- Nothing else deletes a conversation. Archiving sets `archived_at`.

### `members`

One row per account per conversation it is in.

| Column             | Meaning                                         |
|--------------------|-------------------------------------------------|
| `level`            | The level in force.                             |
| `direct_level`     | The level given to the account itself, by joining, being added or having its level set. Null if it is in only through groups. |
| `via_group`        | True if at least one group in `channel_groups` for this conversation contains the account and counts for it: the account accepted the channel's offer (`channel_offers`) or holds a direct level (ruling R103). |
| `read_seq`         | The read position: everything up to this `seq` has been seen. |
| `mode`             | What notifies: `any`, `mentions` or `nothing`.  |
| `mode_before_mute`, `muted_until` | Set together by "mute until". While set, `mode` is `nothing`. |
| `show`             | Whether the conversation has a row in the person's sidebar: `always`, `activity` (only while it has unread messages), `notification` (only while it has a badge), `hidden` (never), or null for the app's own default, which is kept on the device. The server stores and returns it and uses it in no rule. |
| `starred`          | Shown at the top of its heading in the person's sidebar, in the order of `sidebars` stars. |
| `thread_view`      | `threaded`, `flat`, or null to follow `accounts.prefs.thread_view`. |
| `last_post_at`     | When the account last posted here, for slow mode. |
| `ignore_broad`     | True: `@channel`, `@here` and `@everyone` write no `mentions` row for this account here. A mention by name or of another group still does. Added by migration 0002. |
| `may_post`         | True: a manager has allowed this member to post while the conversation is an announcement channel. Kept when it is not one. Added by migration 0004. |
| `visible_from`     | The earliest `messages.created_at` the member may read: in a named private channel that is not a knock channel, the account's `first_joins.at` there; for someone who joined a group chat (an ad-hoc channel) two people were already in, their join (ruling R103); 0 everywhere else, and for the members a channel had when it was made private (ruling R87, PERMISSIONS.md section 4.4). Added by migration 0013. |

Must hold:

- `level` is the highest of `direct_level` and the `level` of every
  `channel_groups` row for this conversation whose group contains
  the account and counts for it, in the order `reader` < `member` <
  `moderator` < `manager`.
- An account a group gives access to, not a member and with no
  `channel_offers` row, gets one `offered`; one that was a member and
  is not is given one `declined`. A `channel_offers` row of source
  `group` goes when no group gives the account access.
- A row exists if and only if `direct_level` is not null or
  `via_group` is true.
- The store works both out again, in the same transaction, whenever
  any input changes: a direct add, leave or level change; a row
  added to or removed from `group_members`; a row added to, changed
  in or removed from `channel_groups`; a role change (for the
  built-in groups); an account claimed or removed.
- `read_seq` is never greater than `next_seq - 1`.
- `muted_until` not null implies `mode = 'nothing'` and
  `mode_before_mute` not null. A mute whose time has passed is read
  as `mode_before_mute`; the scheduler rewrites the row soon after.
- In a locked channel no row has `via_group` true, and no row is
  inserted.
- In a sealed chat both rows have `level = 'member'`,
  `direct_level = 'member'` and `via_group` false, and `mode` is
  never `mentions`.
- A new row starts with `mode = 'mentions'` in a named channel and
  `mode = 'any'` in an ad-hoc channel or sealed chat, with a null
  `show`, and with
  `read_seq` at the conversation's latest `seq`, so a newcomer is
  not handed the whole history as unread. A thread move sets its
  own starting position (FEATURES section 1.6).

Rows go when the account leaves, is removed from the conversation,
loses its last route in, or is removed from the server. The same
transaction deletes the account's `thread_states` and `mentions` in
that conversation, and its `scheduled` rows there.

### `channel_groups`

A group added to a channel at a level. `added_by` is who did it.

Must hold: no row for an ad-hoc channel, a sealed chat, a locked
channel or the library. Adding, changing or deleting a row is followed
by working out `members` for every account in the group.

### `timeouts`

A time-out on an account, in one conversation or (null
`conversation_id`) the whole server. It is in force while `until` is
in the future and `lifted_at` is null. The janitor deletes a row 30
days after it ended or was lifted; the audit log keeps the record.

### `channel_bans`

Added by migration 0005 (ruling R72). An account banned from one
channel by someone acting as its manager (FEATURES.md section 19.1).
It is in force while `lifted_at` is null and `expires_at` is null or in
the future. While it is, working out `members` for that account in that
conversation gives no row, whatever its direct level or groups, so a
ban takes the account out and nothing brings it back. Rows are kept
after they end, as the record of who banned whom and when; they go with
the conversation or the account row.

### `first_joins`

Added by migration 0013 (ruling R87). An account's first join of a
named private channel that is not a knock channel: `at`, the time
`members.visible_from` is copied from whenever the account comes in,
and `left_at`, when it last left or was removed (null while it is a
member). Coming back within `model.RejoinWindow` (three hours) of
`left_at` keeps `at`; coming back later sets `at` to the new join. 0
when the account came in before anything was posted, or with a thread
moved out to a channel. Written by the store as the member row is made
and when it goes, and kept when the account leaves. Rows go with the
conversation or the account.

### `chat_requests`

Added by migration 0009 (rulings R77, R78;
FEATURES.md section 20.2). One request for a new direct or sealed
chat. At most one per requester, recipient and kind is open
(`pending` or `dismissed`), which a partial unique index holds.
`conversation_id` is the chat it started, which holds only the
requester until it is accepted. `shown_open` is set when the
recipient approves chats automatically but had blocked the
requester: the requester is shown the chat open and it reaches
nobody. `answered_at` is when it was accepted, declined, dismissed or
withdrawn (for one that opened at once, when it was made);
`closed_at` when its chat was closed. The caps read the requester's
rows of the last day; the 24-hour wait reads `answered_at` of
declined and withdrawn rows. Rows go with either account.

Migration 0020 (ruling R103) adds the kinds `group` (being included
in a group chat) and `channel` (an invitation to a named channel),
with `level` (what a channel invitation brings) and `override` (an
owner's override). For those kinds at most one per recipient and
conversation is open, whoever sent it; an open `group` request counts
towards its ad-hoc channel's `member_key`. Accepting makes the
recipient a member; until then they are not one.

### `channel_offers`

Added by migration 0020 (ruling R103, FEATURES.md 14.4). One
account's answer to a named channel offered to it: through a group
added to the channel (`source` `group`) or by its becoming a default
channel after the account signed up (`default`). `state` is `offered`
(waiting in the account's sidebar), `accepted` or `declined`. An
account with a `group` or `default` row, or that a group added to the
channel contains, may join it without an invitation or knocking.
Rows go with either the conversation or the account.

### `ghost_members`

Added by migration 0009. Someone `shown_to` is shown as a member of a
conversation who is not one: the person asked in a chat that waits,
or someone who has blocked `shown_to` and was "added" by them (ruling
R77). An ad-hoc channel's `member_key` counts the ghosts shown to its
members, so it is found again as the chat with those people. A ghost
goes when the person accepts and becomes a member.

### `reference_patterns`

Added by migration 0006 (ruling R73). A reference pattern
(`MESSAGE-FORMAT.md` section 3.2): with no `conversation_id` the
server's, with one that channel's, going with the channel. `expression`
is empty for the default (1 to 12 digits). The store reads the whole
table once and keeps it in memory, replacing its copy on every change,
so resolving a post costs no statement. At most 50 rows per scope, which
the service enforces. Messages keep the address they resolved, so a row
may be changed or removed freely.

### `knocks`

Added by migration 0007 (rulings R74, R79), with
`conversations.knock`: a private named channel with `knock` set is a
knock channel, seen by owner, admin and member accounts outside it and
read only by its members (FEATURES.md section 12.5). A `knocks` row is
a request by an account to be added to a knock channel, or to this
server's copy of a private channel a hub owns (migration 0014, ruling
R94; such a request is made on every instance in the channel, so its
moderators and managers everywhere see it). `outcome` is
`pending`, `accepted` or `declined`; a partial unique index keeps one
`pending` row per account and channel. An answered row keeps who
answered and when, as the record; rows go with the conversation or the
account row.

## 6. Messages

### `messages`

| Column              | Meaning                                         |
|---------------------|-------------------------------------------------|
| `seq`               | The `seq` of the event that created the message: its arrival number on this server. Never null. It serves catch-up, read positions and unread counts, and is what the store's listings page by. It does not say where the message is shown. |
| `ord`               | The message's order string, which clients sort by (the `order` of the API). Empty means "by `seq`": the string is then worked out from `seq` when the row is read, and that is every message of a conversation that was never shared. A message whose event carried a stamp (LINK-PROTOCOL.md section 13) holds `hex16(at)!origin!hex16(n)`; one stored in a conversation whose `order_floor` is F > 0 holds `hex16(F)!!hex16(seq)`. Strings made this way compare, byte by byte, as (`at`, origin, `n`) compare in turn. It changes only when a refused stamp is replaced. |
| `late`              | Shared conversations: when the message arrived here, messages with a higher `ord` had already been delivered (`streams.top_ord`). |
| `edit_ord`          | Shared conversations: the order string of the event the message's current content came from. An edit with a lower one is ignored. Null until the message is edited. |
| `parent_id`         | For a reply, the top-level message it hangs under. |
| `quote_message_id`, `quote_start`, `quote_end` | What a reply quotes: a message and a range of its text in UTF-16 code units, or no range for the whole message. No foreign key: the quoted message may be deleted later. |
| `client_id`         | The sender's own identifier, for sending once.  |
| `entities`, `cards` | As in `MESSAGE-FORMAT.md`. Link previews are cards here; a preview card carries `preview_url`, the address it previews, and a card without it was posted by a bot or webhook. |
| `fallback`          | Plain text standing in for the cards and form.  |
| `system`            | Set on a notice the server posted; `text` is then empty. |
| `via_kind`, `via_name` | How it was posted if not typed by its author. |
| `via_account_id`    | Who caused it, when that is not the author: the person whose command a bot answered. |
| `via_icon_file_id`  | A webhook's icon for this one message.          |
| `quiet`             | Sealed chats: the sender marked this item as not an ordinary message (a reaction, an edit, a tick). The server does not notify, wake a phone or count it as unread. |
| `ciphertext`, `key_epoch` | Sealed chats only: the whole content, and the key epoch it was encrypted in. |
| `notified_anyway`   | The author used notify anyway on it. Says nothing about whether anyone was reached; that is in `notify_anyway`. |
| `version`           | Starts at 1 and goes up by one for each edit that kept history. |
| `edited_at`         | Set by the first edit that kept history, and by each one after. |
| `deleted_at`, `deleted_by` | Set on a delete.                         |
| `history_hidden_at` | Set when the author asks for a purge in a public channel; cleared if an admin shows the history again. |
| `purged_at`         | When a purge last erased something of this message. |
| `reply_count`, `last_reply_seq`, `last_reply_at`, `last_reply_ord` | The thread summary, on the parent. `last_reply_ord` is the `ord` of the reply at `last_reply_seq` (empty: ordered by that seq). |

Must hold:

- `(conversation_id, seq)` is unique among rows that have a `seq`.
  A row with a null `seq` exists only in a shared conversation, has
  an `outbox` envelope waiting, has no `mentions` rows and no
  `events` row yet, and gets all three when its number arrives.
- A row with a `parent_id` points at a row in the same conversation
  whose own `parent_id` is null.
- On a parent, `reply_count` is the number of its replies that are
  not deleted, and `last_reply_seq`, `last_reply_at` and
  `last_reply_ord` are those of the newest such reply (the highest
  `seq`), or null. The store updates them in the transaction that
  posts, deletes, moves or re-stamps a reply.
- In a sealed chat `ciphertext` is set and `text`, `entities`,
  `cards` and `fallback` are empty. Everywhere else `ciphertext` is
  null.
- A deleted message has empty `text`, `entities`, `cards` and
  `fallback`. What it held is in `message_versions` until purged.
- A message with both `deleted_at` and `purged_at` is a bare record:
  who, where, when, and nothing else.
- The index on `client_id` makes a repeated send return the first
  message. The API honours it for 24 hours; the janitor sets
  `client_id` to null on older rows.
- The search index holds a message if and only if it is in a
  channel, is not deleted and has text or fallback (section 13).

Rows go when:

- Retention deletes the row (section 14.1).
- The author deletes it inside the quick-correction window and it
  has no replies: the row is deleted outright.
- Its conversation is deleted.
- A purge does **not** delete the row; it empties it.

Deleting a parent row deletes its replies by cascade, so retention
never deletes a parent that still has replies (section 14.1).

### `message_versions`

Earlier versions of a message. One row per edit that kept history,
holding what the message was before that edit, and one row with
`reason = 'delete'` holding its last content when it is deleted.
`version` is the value `messages.version` had while that content was
current. `file_ids` lists the files it carried.

Rows go on purge, on retention, and with the message.

Must hold: no row is written for a quick correction, for a sealed
chat, or for a change that is not an edit (a link preview arriving,
a reaction, a tick).

### `files`

One row per stored file.

| Column           | Meaning                                            |
|------------------|----------------------------------------------------|
| `conversation_id`| Where it was uploaded, which decides who may fetch it. The library conversation for avatars and emoji. |
| `message_id`     | Null until the upload is attached to a message.    |
| `purpose`        | `attachment`, `card` (a card's image or icon, fetched or uploaded), `avatar`, `emoji`. |
| `position`       | Order among a message's files.                     |
| `media_type`, `kind`, `width`, `height`, `duration_seconds`, `preview_text` | Worked out from the content by the server. |
| `sha256`         | Of the content, for integrity checks, for the importer to skip a download it already has, and to check a file fetched from another server against the hash in the signed event. |
| `state`          | `ready` for every file uploaded here. For a file that belongs to a message from another server: `arriving` until this server has fetched it from the origin server, then `ready`; `unavailable` if it could not be fetched; `too_large` if it is over this server's limit. |
| `blob_key`       | Where the content is: `disk:<path under files/>` or `s3:<object key>`. Null until a file from another server has arrived. The prefix lets a server that changed stores still find older files. |
| `poster_key`     | For a video: the one poster image the uploading client may send with the upload, stored the same way. The server makes none; without it the player shows a plain placeholder. |
| `thumb_key`      | The server-made thumbnail of an image.          |
| `detached_at`    | Set when an edit or delete took the file off its message. `message_id` still names the message, so history can show it. |

Must hold:

- Each row owns its blob, thumbnail and poster. No two rows share a
  `blob_key`, so deleting a row always means deleting its blob
  (decision S9).
- `blob_key` is null if and only if `state` is not `ready`.
- A file's row is deleted only after its blob has been removed from
  the file store, or in a transaction that records the blob for the
  janitor to remove. A blob with no row is found and removed by the
  janitor's sweep of the file store.
- In a sealed chat `name` is empty, `media_type` is
  `application/octet-stream` and `kind` is `file`; the real values
  are inside the ciphertext.

Rows go when:

- An upload is still unattached 24 hours after `created_at`
  (janitor; this is what the `files_orphans` index is for).
- The message is purged or removed by retention. The foreign key
  only sets `message_id` to null, so the store deletes these rows
  itself, before the message.
- A quick correction removes the file from its message.
- The avatar or emoji that used it is replaced or removed.

### `checklist_items`

One row per tick-box line of a message. `item_id` is the short
identifier in the message's entities. `text` is the item's wording,
kept so history can show an item that an edit removed
(`removed_at`). `checked`, `checked_by` and `checked_at` are the
current state.

### `checklist_history`

Every tick and untick: who, which way, when.

Must hold: the newest history row for an item agrees with
`checklist_items.checked`, except for an item nobody has touched,
whose state is the one its author typed.

Both tables' rows go when the message is deleted and purged, when
retention removes it, and with the message row. A purge of a message
that is not deleted removes only the items with `removed_at` set.

### `forms`

One row per form: the form on a message, or a dialog a bot is
showing one person.

| Column        | Meaning                                              |
|---------------|------------------------------------------------------|
| `message_id`  | The message carrying the form. Null for a dialog.    |
| `dialog_for`  | The one account a dialog is shown to.                |
| `owner_id`    | The message's author, or the bot behind a dialog.    |
| `fields`, `buttons`, `action` | As in `MESSAGE-FORMAT.md`. In a `group` form each button carries its `group_action` and `group_id`. |
| `answerer_group_id` | If set, only members of that group may answer or press. No foreign key: if the group is deleted, nobody can answer. |
| `answer_count`| How many accounts have a current answer.             |
| `closed_at`, `closed_by` | Set once. `closed_by` null means it closed itself at `closes_at`. |

Must hold:

- Exactly one of `message_id` and `dialog_for` is set.
- `fields` does not change after the first answer, except by an
  answerer adding an option where the form allows it.
- `anonymous` never changes.
- `closed_at`, once set, is never cleared.
- `answer_count` equals the number of distinct accounts in
  `form_answers` for the form.

Rows go with the message (retention, purge of a deleted message). A
dialog's row is deleted when it is answered or dismissed, and by the
janitor after an hour otherwise.

### `form_answers`

One row per answer. With `one_answer` true there is at most one row
per account and a changed answer updates it (`updated_at`). With
`one_answer` false each answer is a new row. `button_id` is the
button that submitted it. `answers` maps field ids to values.

`result_message_id` is the message a `thread` or `channel` answer
produced: the reply in the form's thread, or the first message of
the ticket channel. No foreign key; the message may be deleted
later.

The account is stored on anonymous forms too, to hold each person to
one answer. No API operation and no export gives it out.

For a form in a shared conversation, answer rows exist only on the
server the form was posted on. Other servers hold the tally they
were sent in `form_tallies` and no answers.

Nothing is stored here for the `bot` and `group` actions.

### `form_tallies`

Counts per option for forms with the `tally` action.

Must hold, on the server the form was posted on: for every form, field
and option, `count` equals the number of `form_answers` rows for that
form whose value for that field contains that option. A missing row
means zero. Button presses on a form with no choice field are counted
under the field id `_button`, with the button id as the option
(decision S10). The store changes the answer and the counts in one
transaction, and the shared storage tests check the equality by
recounting.

### `reactions`

One emoji on one message by one account. `emoji` is the emoji
itself, `:name:` for one of this server's custom emoji, or
`:name@server.name:` for another server's. Deleted when taken back,
when the message is deleted, and on retention. They stay when the
reacting account is removed (decision S11).

Must hold: at most twenty different `emoji` values per message.

### `mentions`

One row per account a message mentioned and that could be told.

| Column            | Meaning                                          |
|-------------------|--------------------------------------------------|
| `conversation_id`, `seq`, `parent_id` | Copies of the message's, so unread mentions are counted from this table alone. |
| `kind`            | `account` (by name), `group`, `channel` or `here`. If more than one applies, the first in that order. |

Must hold:

- No row for the message's author, for an account that could not
  read the conversation when the message was posted, for an account
  that had blocked the author, or for an unclaimed or removed
  account.
- The copies follow the message when a thread is moved.

Rows are not removed when read; read state comes from positions.
They go when the message is deleted or edited so the mention is
gone, on retention, and when the account can no longer read the
conversation.

### `thread_states`

One row per account per thread it follows or ignores. No row means
"neither". `read_seq` is how far the account has read in that
thread.

Must hold: rows exist only for threads in conversations the account
is a member of. Rows go when the account goes back to "neither",
leaves the conversation, or is removed; when the thread is moved to
a channel; and with the parent.

### `pins`

A pinned message, who pinned it and when. At most 100 per
conversation. Deleted on unpin, when the message is deleted, and
when a thread move takes the message elsewhere.

### `bookmarks`

A person's saved messages. Deleted when unsaved, with the message,
when the message becomes a bare record, and on account removal. A
bookmark whose message the person can no longer read is kept and not
shown.

### `emoji`

Custom emoji. `file_id` is a `files` row with purpose `emoji`.
`server_id` is null for this server's own and set on one that
arrived with a shared channel. Names are unique among the server's
own (first index) and per other server (second index), so a remote
`:party:` and a local one can both exist; the remote one is written
`:party@server.name:`. Deleting one deletes its file. Messages and
reactions that used it keep the written name.

### `preview_cache`

What the link-preview fetcher found at an address, keyed by the
SHA-256 of the normalised address. `card` is the card it made, or
null if there was nothing usable. A cached card's image is a file in
the library conversation, which the cache entry owns; each message
that uses the card gets its own copy.

The janitor deletes a row, and its image, 24 hours after
`fetched_at` (one hour for a null card).

### `events`

The catch-up log: one row per event that belongs to a conversation,
under the `seq` it was given. `payload` is the event exactly as it
was sent. `message_id` is the message the event carries, if it
carries one: set on message, edit, delete, preview, reaction, task
and form events. Events with no `seq` (account, group and setting
changes, typing, presence) are not stored.

Must hold:

- The row is written in the transaction that makes the change.
- A row never outlives the content it carries. When a message is
  purged, removed by retention, deleted in the quick-correction
  window or moved out of the conversation, every row with that
  `message_id` whose payload holds content is deleted in the same
  transaction, found through the `events_message` index.
- Readers must accept gaps in `seq`.

Rows go when older than 30 days (janitor), as above, and with the
conversation.

### `notify_anyway`

One row per person a message was pushed through do not disturb to.
`by_id` is the author, `at` when. `delivered` is true if the server
then notified that person and false if it quietly did nothing
(they had the conversation on `nothing` or muted, blocked the sender,
turned the feature off, could not read the conversation, or were
not in do not disturb). `delivered` is for the server's own use:
no API operation, event or export gives it out, and neither does
the list of targets.

Must hold: a row exists only for a message whose `notified_anyway`
is true. Rows go with the message and when the target account is
removed.

## 7. Moderation

### `purge_requests`

An author's request to purge their own message in a public channel.
`outcome` is null while it waits, then `shown`, `hidden` or
`erased`. No row is written for a purge in a private or ad-hoc
channel, or for an admin's direct purge.

Must hold: at most one waiting row per message. While a row waits,
or was decided `hidden`, the message's `history_hidden_at` is set.

Rows stay after a decision, as the record, and go with the message
row.

### `reports`

A report of a message (`kind` `message`) or of a person (`account`).
`conversation_id` is a copy, so reports can be listed per channel;
`account_id` is the person reported, the message's author or the
account. `snapshot` is what was reported, as it was: for a message
where it was, its text, author, times and file names; for a person
their username, display name, status text and picture address.
`message_at` is the message's time, which the moderators' list
compares with their first join. `to_admins` says the report went to
the instance's admins (PERMISSIONS 8.4). `source` is `person` for a
report someone made, with `reported_by` set, and `filter` for a
message the word filter flagged, with `reported_by` null. `outcome`
is null while it waits, then `dismissed`, `deleted` or `timed_out`.

Must hold: `reported_by` is null if and only if `source` is
`filter`; `message_id` is set when a `message` report is made. At
most one waiting row per message (or person) and reporter, and at
most one waiting `filter` row per message. A waiting row stays when
its message goes (`message_id` is cleared, the snapshot stays), and a
private or ad-hoc channel with no members is kept while a row about
it waits. A handled row goes with its message under retention, and
with its conversation.

### `word_filters`

The admins' list. `pattern` is a word or phrase, or a regular
expression in RE2 syntax when `is_regex` is true. `action` is
`block` or `flag`. Deleted by an admin.

## 8. Scheduling and saved replies

### `scheduled`

Messages to send later, reminders, recurring forms and watches.

| Column             | Meaning                                         |
|--------------------|-------------------------------------------------|
| `account_id`       | Who it belongs to and who it will be posted as. |
| `conversation_id`  | Where it will be posted.                        |
| `kind`             | `message`, `reminder`, `form` or `watch`.       |
| `content`          | The message to post, in the shape the request to post a message accepts. For a `watch` (FEATURES section 9.7): `{"watch_account_id": …}`, the person it waits for, kept here so that it needed no migration; the server holds the waiting watches in memory by that person, so nothing looks a row up by it. Its `next_run_at` is when it expires unfired. |
| `about_message_id` | For "remind me about this message".             |
| `next_run_at`      | When it next runs. Null once it will not run again. |
| `repeat`           | Null for a single run. Otherwise how it repeats (FEATURES section 9). |
| `last_run_at`      | When it last ran.                               |
| `failed_at`, `failure` | When a run failed and why, as an error code and sentence. A failed item does not run again until its owner changes it. |

State is read from the columns:

| State     | Columns                                                  |
|-----------|----------------------------------------------------------|
| Cancelled | `cancelled_at` set                                       |
| Failed    | `failed_at` set, `next_run_at` null                      |
| Waiting   | `next_run_at` set                                        |
| Done      | none of the above, `last_run_at` set                     |

Rows go when the account is removed or leaves the conversation, with
the conversation, and by the janitor 30 days after reaching done or
cancelled. A failed row stays until its owner deletes it.

### `saved_replies`

Preset texts posted by typing `/name`. `owner_id` null is a community
command made by an admin. The two indexes make names unique among
community commands and among one person's own. Personal ones go on
account removal.

## 9. Bots and webhooks

### `webhooks`

A webhook and the request that led to it. `bot_id` is the
webhook-only bot account that posts. `state` is `waiting`,
`approved`, `declined` or `revoked`. `token_hash` is the hash of the
`churr_hook_` secret in the address; it is null until the requester
collects the address after approval, which is when the secret is
made and shown once. `signing_secret` is a separate secret, sealed,
made at the same moment and shown with the address; it verifies
GitHub's `X-Hub-Signature-256`. The address secret is never used
for that. The janitor deletes declined rows after 30 days. Revoking
sets the state, clears `token_hash` and `signing_secret`, and
removes the bot account.

### `commands`

Commands a bot has registered. Names are unique across all bots and
cannot be a built-in name or a community saved reply. Deleted when
the bot unregisters them or is removed.

## 10. Calls

### `calls`

One row per call. `message_id` is the notice that announced it.
`ended_at` null means it is live; the `calls_live` index allows one
live call per conversation. `host_server_id` is set when another
server carries the media.

### `call_participants`

Who was in a call and when. The key includes `joined_at` because a
person can leave and come back.

Rows of both go with the conversation, and when retention removes
the call's notice message (decision S14). Nothing else about a call
is stored.

## 11. Links to other servers

`LINK-PROTOCOL.md` explains how these are used. In brief:

| Table            | Holds                                                 |
|------------------|-------------------------------------------------------|
| `peers`          | A direct link to one other server, or a connection to a hub. `dials` says which side opens the connection. `invite_hash` and `invite_expires_at` belong to an invitation not yet accepted. `state` is `invited`, `active`, `paused` or `removed`. `clock_told` is what the peer, as a time reference, last told this server about this server's clock: offset, latency, jitter, uncertainty, each direction's lowest one-way time and jitter, and any asymmetry estimate, as JSON in the shape of the API's `PeerClock`; null until the peer has told one. This server stamps its events with its offset in every stream the peer is the reference of. `clock_measured` is the same figures as this server, the reference of the streams it owns on a direct link, last measured them of the peer; null for a hub. Both are kept while disconnected and across restarts. `away` is, for a direct link, the last 8 stretches the peer was disconnected, as `[from, to]` pairs in milliseconds by this server's clock: an event stamped inside one is not refused for being old. |
| `peer_servers`   | The servers reachable through a hub.                  |
| `streams`        | One shared conversation over one peer; columns below. |
| `stream_servers` | Which servers are in a stream. `last_n` is the counter up to which this server holds everything that server made in the stream, as far as whoever delivers has it, and `last_at` the `at` of that event; on this server's own row they are the counter and `at` of the last event it stamped. Catching up is by `last_n`. `logged_n` is, for a stream this server is the time reference of, the counter of that server's last envelope in `stream_log`. |
| `stream_keys`    | The stream key for each epoch, sealed. Old epochs are kept so history can be read while a peer catches up. |
| `outbox`         | This server's own events that the stream's time reference has not acknowledged yet: the event unencrypted, with the stamp it was given when it was made. `id` is the origin event id, which for a new message is the message's id. Deleted when the reference acknowledges the event. A message whose id is here is `unsent`. |
| `stream_seen`    | Origin event ids already applied, so a duplicate is dropped. `at` is when; the janitor deletes rows older than 35 days, and an event older than that is refused for its age instead. |
| `stream_log`     | For streams this server is the time reference of: every envelope it took, in the order it took them, so a server that was offline can catch up. `seq` is the position in the log, a delivery cursor and nothing more. `origin` and `n` are the server that made the event and its counter, as signed in the envelope; `prev_n` is the counter of the same origin's row before it, kept even when that row has been pruned. Kept 30 days. |
| `stream_channels` | The record of each channel a hub owns that this server is in, invited to or moving onto the hub (ruling R56; `LINK-PROTOCOL.md` section 23a): the newest one the hub signed that this server has verified. `version` is the record's; `home_hub` the hub that owns the channel; `record` and `sig` the signed bytes and signature exactly as received, so the record can be shown to another server (a move carries it in `stream.moved`); `managers` lists the account ids on the record's manager list that are accounts of this server, `moderators` those on its moderator list, and `private` copies the record's flag (the channel is private for good, ruling R94); `internal/perm` reads those three. The server list, the managers and the moderators of such a channel come from here and from nowhere else. `managers`, `moderators` and `private` are added by migration 0014. Migration 0021 drops `makers`: the home hub makes every key of such a channel, and a key is taken only from it. Deleted when a move that had the hub prepare the channel is abandoned. |
| `stream_moves`   | Moves of a direct-link stream onto a hub (ruling R55; `LINK-PROTOCOL.md` section 21a), one row per attempt on each of the two servers, under the identifier the owner server made. `from_peer_id` is the direct link the stream ran over and `hub_peer_id` this server's own link to the hub named by `hub_name` and `hub_fingerprint`. `proposed_by` is set on the server that proposed (the owner server) and null on the other; `answered_by` is the local owner who accepted or declined. `state` is `proposed`, `accepted`, `committed` (owner server only: the hand-over has happened and is being finished), `done`, `declined` or `abandoned`, with `reason` for the last two. `accept` and `accept_sig` are the other server's signed agreement. `commit_block` and `commit_sig` are the owner server's signed commit, `commit_event_id` the origin event id of `stream.moved`, and `commit_seq` its position in the direct log (owner server only). `expires_at` is when the proposal lapses. |

`streams` columns:

| Column            | Meaning                                            |
|-------------------|----------------------------------------------------|
| `owner_server_id` | Null means this server owns the stream. For a channel a hub owns it is the row in `servers` that stands for the hub (the hub's name and signing key, no box key): the hub is no server in the stream, so no server is its owner. |
| `sequencer`       | `self`, `owner` or `hub`: who the stream's time reference is, that is, whose clock its events are stamped by and who keeps its log. The column keeps the name it had when that party numbered events; nobody does now (ruling R57). The API calls it `reference`. |
| `kind`            | `channel`, `adhoc` or `sealed`.                    |
| `state`           | `offered`, `active`, `left` or `removed`.          |
| `key_epoch`       | The stream key epoch in use.                       |
| `last_seq`        | A delivery cursor. For `sequencer = self`: the last position written to `stream_log`. Otherwise: the position, in the log of whoever delivers, of the last envelope received. It is acknowledged to the deliverer and orders nothing. |
| `first_seq`       | Not used.                                          |
| `home_hub`        | Through a hub: the name of the hub that is the stream's home and its time reference. Null on a direct link. The hub of `peer_id`, or for a channel another hub of that hub's cluster (`LINK-PROTOCOL.md` section 23b); it changes when a record the home signed names a new home. `owner_server_id` is then the row that stands for that hub. The server's clock against such a hub is told by the peer with every ping; no column holds it, and it is written down in the setting `link.home_clocks.<peer>` (section 2). |
| `max_at`          | The highest `at` of any event this server has applied or made in the stream. Its next event is stamped above it, so a reply sorts after what it answers. |
| `acked_at`        | The `at` of the last of this server's own events the time reference acknowledged. When a stamp is refused, `max_at` goes back to this or to the highest `last_at` of another server, whichever is higher. |
| `top_ord`         | The highest `messages.ord` of any message delivered here in the stream. A message that arrives below it is `late`. |
| `servers_version` | The version of the stream's server list last applied. |
| `max_blob_bytes`  | The largest file the stream carries, set by its owner. |
| `history_days`    | How many days of earlier history came with the offer, 0 to 90. |
| `offered_name`, `offered_at`, `accepted_by` | The name an offer arrived with, when, and which local owner accepted it. |

Must hold:

- `conversations.stream_id` names a stream whose `conversation_id`
  names that conversation, and the other way round, while the stream
  is `active`.
- An `outbox` row is written in the transaction that makes the
  change it carries, with the stamp taken in that transaction.
- `stream_log` rows are written only for streams with
  `sequencer = self`, and the row is written in the transaction that
  takes the envelope: for this server's own events, the transaction
  that makes the change. The rows of a stream this server has moved
  onto a hub stay (the other server may still be reading them) until
  they are 30 days old; their last one is `stream.moved`.
- A stream has at most one `stream_moves` row that is neither `done`,
  `declined` nor `abandoned`. A change of a move's state is made in a
  transaction that holds the stream's row.
- On the owner server a move becomes `committed` in the one
  transaction that also appends `stream.moved` to `stream_log`, sets
  `commit_seq`, `commit_event_id`, `commit_block` and `commit_sig`,
  and changes the stream's `peer_id`, `owner_server_id`, `sequencer`
  and `home_hub` to the hub's. On the other server a move becomes
  `done` in the one transaction that applies `stream.moved`, deletes
  the `outbox` rows the event says the owner server took, and makes
  the same change to the stream's row. No other transaction changes
  those four columns of an `active` stream.
- A `stream_channels` row holds a record only after its signature has
  been verified under a hub key this server accepts for the peer it
  came over, or, on the other server of a move, for the hub it agreed
  to. A row's `version` only grows.
- In a stream's `stream_log`, one origin's rows have rising `n` and
  rising `at`.
- `streams.max_at` is at least every `stream_servers.last_at` of the
  stream, except in the moment between a refusal and the stamping
  again that follows it.
- File contents are never in `outbox` or `stream_log`. A file is
  fetched by the receiving server from the origin server and checked
  against the hash in the signed event (`files.state`).
- Removing a peer sets its streams to `removed`, clears
  `conversations.stream_id`, deletes the stream's `outbox`,
  `stream_seen` and `stream_log` rows, and leaves the local copy of
  each conversation in place as an ordinary local channel.

### The hub's database

`churr-hub` keeps its own small SQLite database, `hub.db` in its data
directory, apart from any server's. Its tables are made, and columns
added to them, by the hub itself when it opens the database
(`internal/hub/hub.go`); there are no migration files. It holds names
of servers and hubs, public keys, stream identifiers, channel records,
envelopes as they travel (ciphertext), and the stream keys of the
channels it is the home of, sealed at rest. Most of it follows from
`LINK-PROTOCOL.md` sections 23 to 23b. Three parts serve trunks and
the moving of a channel's home:

| Table or column     | Holds                                              |
|---------------------|----------------------------------------------------|
| `stream_ctl`        | For a stream a server owns that crosses a trunk: the owner's last control frame as it was relayed (`frame`), and `floors`, per member server, what the home hub's log held of each origin when that server joined. Deleted with the stream's other rows when the stream has been closed long enough. |
| `channels.offer`    | A channel's home offered to another hub (`churr-hub rehome`) and not yet settled, as JSON: the signed record that hands it over, the hub it goes to, when the offer was made, and whether it is being called off. Empty otherwise. While it is set the hub signs nothing more for that channel. |
| `trunks.farewell`   | On a hub that has left its cluster: the roster that says so, owed to the neighbour it left through, kept until it has been sent over that trunk. Null otherwise. A trunk's `state` is `pending`, `active`, `leaving` or `removed`. |

### The hub's holder database

The hub holds every channel it is the home of whole (`LINK-PROTOCOL.md`
section 23a, "The hub holds the whole channel") in a second database,
`holder/churr.db` under its data directory, with the instance's schema
and the instance's migrations (section 16), run when the hub starts.
Its files are in `holder/files/`, as a server keeps its own. It is the
database of a server with no accounts of its own: one row in `peers`
(the hub itself, dialled on the hub's internal connection), the
servers of the hub's roster in `servers`, and for each channel a
`streams` row, its `stream_channels` record, its `stream_keys`, and a
`conversations` row of kind `channel` with no creator, holding the
channel's messages, reactions, ticks, forms, members and the shadow
accounts of everyone who wrote in it or is in it, as any instance's
copy holds them. A closed channel's rows are removed when the hub
sweeps its stream, 30 days after it closed. Nothing else in it ages:
no retention runs there, and `conversations.retention_seconds` is
null.

## 12. Sealed chats

`SEALED.md` is the authority and explains the design. The server
can read none of the `bytea` values in these tables except public
keys, and cannot forge the signed ones.

| Table              | Holds                                               |
|--------------------|-----------------------------------------------------|
| `device_lists`     | Every version of each account's list of its devices, signed with the account key. The server keeps them all and serves any range, so the other person's devices can check the chain. A new version is accepted only if its `version` is exactly one above the newest and every device in it is a live `devices` row with a matching `sign_key`. |
| `sealed_epochs`    | One record per key epoch per sealed chat, kept for as long as the chat exists. `record` is made and signed by one device (`device_id`): a fresh epoch secret sealed separately to every device on both accounts' current device lists, with the device set and the hash of the previous record. `seq` is its place in the conversation's numbering. The primary key makes "one record per epoch" atomic: a second record for the same epoch fails. A new epoch is made when either account's device list changes, and when the current one is more than seven days old at the time someone sends. |
| `backup_keys`      | The account's backup key, once per way of opening it. `kind = passkey` with `passkey_id`: sealed to that passkey's `unlock_public`. `kind = phrase`: sealed to the key pair derived from the recovery phrase. `kind = device` with `device_id`: sealed to one device's `box_key`, made when an unlocked device approves a new one or when the backup key is replaced. `kind = access_key`: sealed to `access_keys.unlock_public`; at most one per account. |
| `key_backups`      | The history keys: one record per sealed chat per key epoch, each encrypted under a key derived from the backup key. |
| `account_records`  | One per account: the account key and the list of ways to unlock, encrypted under the backup key. `revision` goes up by one each time the ways change, and a device refuses a lower one than it has seen. `replacement_due` is set when a way was removed with no unlocked device at hand; the next unlocked device to connect replaces the backup key before doing anything else. |
| `unlock_requests`  | A new, locked device asking an unlocked one for the backup key. `request` holds its device identifier, public keys and proof. `answer` is the backup key sealed to the asking device, set with `answered_at` when an unlocked device approves. A request expires ten minutes after it is made. |

`key_version` appears in `backup_keys`, `key_backups` and
`account_records`. It is the version of the backup key (and of the
account key, which shares it). It starts at 1 and goes up by one
when the backup key is replaced.

Must hold:

- At most one `account_records` row per account, and every
  `backup_keys` and `key_backups` row of that account has its
  `key_version`. Replacing the backup key swaps the account record,
  the locked copies, the records and a new device list version in
  one transaction and deletes the old rows.
- A new `sealed_epochs` row has `key_epoch` exactly one above the
  chat's `conversations.key_epoch` (1 for a new chat), and the same
  transaction sets `conversations.key_epoch` to it and writes the
  `sealed.epoch_started` event under the row's `seq`.
- In a chat shared with another server, `sealed_epochs.ord` is the
  order string of the stamp of the event that carried the record
  (empty for a chat's first record, and for chats that are not
  shared). A row is replaced, keeping its `key_epoch` and leaving
  `conversations.key_epoch` alone, only by a record made for the same
  key epoch on the other server whose `ord` is lower
  (`LINK-PROTOCOL.md` section 11).
- A sealed message's `key_epoch` equals `conversations.key_epoch` at
  the moment it is numbered.

All go on account removal, except `sealed_epochs`, which belongs to
the chat. `sealed_epochs` and `key_backups` rows go with their
conversation. The janitor
deletes an `unlock_requests` row a day after it expired.

## 13. Search

Only channel messages that are not deleted are indexed. The indexed
text is the message's `text` followed by its `fallback`. Sealed
chats, message history, file names and file contents are never
indexed.

- **PostgreSQL**: a generated column `messages.tsv` and a GIN index.
  Because the column is generated from `text` and `fallback`, it
  follows every insert, edit, delete (which empties both) and row
  deletion with no extra work.
- **SQLite**: the virtual table `messages_fts`, which the store
  writes in the same transaction as `messages`: insert on create,
  replace on edit, delete on delete, purge, retention and quick
  delete, and an update of `conversation_id` when a thread is moved.

Must hold on SQLite: `messages_fts` has exactly one row for each
indexed message and none for any other. The storage tests check
this by comparing counts after each kind of change.

## 14. What each kind of deletion removes

### 14.1 Retention

A message is past its period when `created_at` is older than the
conversation's period: `conversations.retention_seconds` if set,
otherwise the `retention.seconds` setting (null meaning never). Edits do not make a message younger. The janitor
runs hourly and, for each expired message, in one transaction per
batch:

1. Removes the blobs of its `files` rows and deletes the rows.
2. Deletes the `events` rows that carry it (`events.message_id`).
3. Deletes its search entry (SQLite).
4. Deletes the `calls` row it announced, if any.
5. If it is a parent that still has replies, empties it to a bare
   record (as a purge of a deleted message does, with `deleted_by`
   null) and stops. The row is deleted in a later run, once its last
   reply has gone.
6. Otherwise deletes the `messages` row. The cascade removes its
   `message_versions`, `checklist_items`, `checklist_history`,
   `forms`, `form_answers`, `form_tallies`, `reactions`, `mentions`,
   `thread_states`, `pins`, `bookmarks`, `notify_anyway` and
   `purge_requests`, and its handled `reports`; a waiting report stays
   with its snapshot until it is handled.

Sealed chats are treated the same; the ciphertext rows are deleted.
Their `sealed_epochs` rows are not messages and stay.

### 14.2 Purge

| What is purged                 | What is removed                         | What stays |
|--------------------------------|------------------------------------------|-----------|
| A message that is not deleted  | Its `message_versions` rows; files with `detached_at` set; `checklist_items` with `removed_at` set; `events` rows carrying earlier versions | The message as it now reads, its reactions, its form, its ticks |
| A deleted message              | All of the above, and every remaining `files`, `checklist_items`, `checklist_history`, `forms` (with answers and tallies), `mentions`, `pins` and `bookmarks` row, and every `events` row carrying it | A bare `messages` row; `purge_requests` rows; `reports` rows with their snapshots |

In both cases `purged_at` is set, `client_id` and the quote columns
are cleared on a deleted message, and blobs are removed from the
file store.

### 14.3 Account removal

In one transaction the store sets `removed_at` and `removed_by`,
clears the profile columns listed under `accounts`, and deletes the
account's rows in: `sessions`, `devices`, `passkeys`, `access_keys`, `sidebars`, `totp`,
`recovery_codes`, `recovery_links`, `oauth_codes`, `identities`,
`import_claims`, `device_lists`, `backup_keys`, `key_backups`,
`account_records`, `unlock_requests`, `notify_anyway` (as target),
`group_members`, `members`, `thread_states`, `mentions`,
`bookmarks`, `blocks` (both directions), `timeouts`, `scheduled`,
personal `status_presets`, personal `saved_replies` and `themes`. Unused
invites it made are revoked. For a bot, its `commands` go and its
`webhooks` are set to `revoked`.

It then applies the last-manager and last-member rules to each
conversation the account was in.

What stays: the `accounts` row, its messages and their history, its
files on those messages, its reactions, its form answers, its ticks,
and every record that names it as the one who acted (`pinned_by`,
`created_by` and so on).

### 14.4 The janitor's other work

| Rows                                   | Deleted                              |
|----------------------------------------|--------------------------------------|
| `events`                               | Older than 30 days                   |
| `files` never attached to a message    | 24 hours after upload                |
| Blobs in the file store with no row    | When the sweep finds them            |
| `sessions`                             | 30 days after expiry or revocation   |
| `devices` retired                      | Once no current device list names them |
| `unlock_requests`                      | 1 day after expiry                   |
| `stream_log`                           | Older than 30 days                   |
| `oauth_codes`                          | 1 hour after expiry or use           |
| `recovery_links`                       | 1 day after expiry or use            |
| `invites`                              | 30 days after expiry, revocation or last use |
| `preview_cache` and its images         | 24 hours after the fetch; 1 hour for a failure |
| `forms` that are dialogs               | 1 hour after creation                |
| `timeouts`                             | 30 days after ending                 |
| `scheduled` done or cancelled          | 30 days after                        |
| `webhooks` declined                    | 30 days after                        |
| `stream_seen`                          | Older than 35 days                   |
| `messages.client_id`                   | Set to null after 24 hours           |

## 15. SQLite and PostgreSQL

`store.Store` hides these differences. One shared test suite runs
against both.

### Types

| In `schema.sql` | PostgreSQL | SQLite                              |
|-----------------|------------|-------------------------------------|
| `uuid`          | `uuid`     | `TEXT`, 36 characters, lowercase    |
| `jsonb`         | `jsonb`    | `TEXT` holding JSON                 |
| `boolean`       | `boolean`  | `INTEGER`, 0 or 1                   |
| `bytea`         | `bytea`    | `BLOB`                              |
| `bigint`, `integer` | as written | `INTEGER`                       |

The store reads and writes Go values (`uuid.UUID`, `bool`,
`[]byte`, `int64`, `json.RawMessage`) and each backend converts.
Partial indexes (`WHERE …`) and partial unique indexes work the same
on both. Text comparison is by byte on both: usernames and handles
are lower-cased by the API before they are stored. Channel names
are stored as written, with capitals and spaces, after trimming and
collapsing runs of spaces; anything that matches or sorts them
ignores case in Go, not in SQL.

### Search

| | PostgreSQL | SQLite |
|-|------------|--------|
| Index | `tsvector` column, `simple` configuration, GIN | FTS5 table, `unicode61` tokenizer |
| Kept in step by | The database | The store, in the same transaction |
| Accents | Kept: `café` and `cafe` differ | Removed: they match |
| Prefix (`word*`) | `word:*` | `word*` |
| Phrase | `phraseto_tsquery` | A quoted FTS5 phrase |

Neither stems words. The accent difference is real and is recorded
in section 20. The query syntax people type is the same on both
(FEATURES section 24); `search` turns it into each database's form
and never passes typed text through as query syntax.

### Erasing for good

Erased means deleted from the database, the search index and the
file store, on both backends.

- **PostgreSQL**: the rows are deleted, and the generated search
  column goes with the row or is emptied with the text.
- **SQLite** is opened with `PRAGMA secure_delete = ON`, and
  `messages_fts` has its own `secure-delete` option turned on, so
  removed text is not left behind in freed pages or in the index.
- File contents are removed from disk or from the object store by
  the key in `files.blob_key`.

### Write-ahead log and concurrency

- **SQLite** runs in WAL mode with one writer at a time. The store
  keeps a single write connection and a pool of read connections,
  and sets a busy timeout. `churr backup` uses SQLite's online
  backup so the copy is consistent while the server runs.
- **PostgreSQL** uses a connection pool at the default isolation
  level. The one place order matters is `next_seq`: the writer locks
  the `conversations` row (`SELECT … FOR UPDATE`) before reading it.
  On SQLite the single writer gives the same guarantee.
- Transactions are short and never span a network call. A link
  preview fetch, a push or a file upload happens outside one.

## 16. Migrations

- **One source, two checked-in files.** Migrations are files
  `NNNN_name.sql` in `internal/store/migrations`, one per dialect
  (`postgres/` and `sqlite/`), embedded in the binary. `0001` is
  `docs/schema.sql`: the PostgreSQL file is that document as it
  stands, and the SQLite file is generated from it by applying the
  type substitutions in section 15 and swapping the search block.
  Both generated files are checked in; nothing is derived at run
  time. A test regenerates them and fails if they differ from what
  is checked in, so the document and the migrations cannot drift.
  `go test ./internal/store/migrations -update` rewrites them. Later
  migrations are written by hand as a pair of files with the same
  number. `docs/schema.sql` no longer changes, since that would
  change `0001` under databases that have already run it: the
  columns and tables of later migrations are described in this
  document, and the comments in `docs/schema.sql` that name
  migrations up to `0010` stay as they are. `0002_broad_mentions`
  adds `conversations.broad_mentions` and `members.ignore_broad`;
  `0004_large_channels` adds `conversations.lazy_delivery`,
  `announcement` and `announcement_replies`, and `members.may_post`;
  `0005_channel_bans` adds `channel_bans`; `0006_reference_patterns`
  adds `reference_patterns`; `0007_knocks` adds `conversations.knock`
  and `knocks`; `0008_session_lock` adds `sessions.locked_at`;
  `0009_chat_requests` adds `conversations.closed_at` and
  `request_state`, `chat_requests` and `ghost_members`;
  `0010_access_keys` adds `access_keys`; `0011_sidebars` adds
  `sidebars`; `0012_access_key_unlock` adds
  `access_keys.unlock_public`; `0013_first_joins` adds `first_joins`
  and `members.visible_from`; `0014_hub_channels` adds
  `stream_channels.managers`, `moderators` and `private`;
  `0015_approvals` adds `approvals` and `approvers`;
  `0020_first_contact` adds `chat_requests.level` and `override` and
  `channel_offers`.
- **Fixed once released.** `internal/store/migrations/manifest.txt`
  lists every migration with the SHA-256 of both its files. A test
  fails if a listed file changes, is renamed or is removed, and
  prints the line to add for a migration that is not listed yet. The
  runner checks names only, never content.
- **Numbered.** Four digits, no gaps, the same numbers and names in
  both dialects; a test holds all three. The runner records each
  migration it applies in `schema_migrations` (`version`, `name`,
  `applied_at`). That table is the first statement of `0001`, which
  creates it without `name`; the runner adds that column in the
  transaction of the first migration it applies, or, on a database
  created before names were recorded, in a transaction of its own
  that also fills each row in with the name of this build's migration
  of that number. Before the table exists the runner treats the
  database as empty.
- **Checked before anything runs.** At every start the runner reads
  `schema_migrations` and refuses the database, changing nothing, if
  a recorded number has a different name in this build, if a
  migration of this build is missing below the highest recorded one
  (an unapplied migration is never skipped), or if the database has
  migrations this build does not. The refusal names each migration
  concerned and says to restore a backup or run the matching build.
- **Forward-only.** There are no down migrations. Going back means
  restoring a backup.
- **Run at start.** `churr serve` applies whatever is pending before
  it listens, each migration and its row in `schema_migrations` in
  one transaction, and stops with the error if one fails. Both
  databases run DDL inside transactions, so a failed migration
  leaves neither its changes nor its record behind.
- **SQLite's limits.** SQLite cannot alter or drop a constraint, so
  a migration that needs to makes a new table, copies the rows and
  renames. The runner does this from the same source statement where
  it can, and otherwise the migration carries a SQLite block.
- **Data changes** that cannot be written in SQL common to both are
  Go functions registered under a migration number and run in that
  migration's transaction.

## 17. What `churr migrate-db` copies

`churr migrate-db` moves a community from one database to the other.
The server must be stopped.

1. It opens the source read-only and checks it as the runner does at
   start (section 16), and that nothing is pending: every migration
   of this binary applied and named.
2. It creates the schema in the target by running every migration.
   The target must be empty beforehand.
3. It copies **every row of every table** in section 2 to 12, in
   foreign-key order, in batches, converting types as in section 15.
   That includes sealed-chat ciphertext, key backups, the event log,
   the outbox and the audit log. Encrypted ("sealed") columns are
   copied as bytes; the same key file opens them afterwards.
4. It does not copy the search index. PostgreSQL builds `tsv` as
   rows arrive. For SQLite it fills `messages_fts` from `messages`
   at the end, under the rule in section 13.
5. It does not copy `schema_migrations`; the target has its own from
   step 2.
6. It compares row counts per table and stops with a report if any
   differ.
7. It does not touch the data directory. `files/` and `keys/` serve
   both databases unchanged.

The source is left as it was, so a failed or unwanted migration is
undone by pointing the server back at it.

## 18. What the export contains and leaves out

The export is for taking a community's data elsewhere and for
answering "what does this server hold". It is not a backup and
cannot be loaded back; the backup is the data directory. Only the
owner can run it, from the admin console or with `churr export`, and
each run is written to the audit log.

It is one zip archive (decision S15):

| Path                               | Contents                           |
|------------------------------------|------------------------------------|
| `export.json`                      | Server name, time of export, schema version, counts |
| `accounts.json`                    | Every account: id, username, display name, role, home server, title, pronouns, created, claimed, removed |
| `groups.json`                      | Groups and who is in them          |
| `sections.json`, `emoji.json`      | Sections; custom emoji names       |
| `conversations.json`               | Every channel: public, private and ad-hoc, with its settings, members, levels and groups |
| `messages/<conversation id>.jsonl` | One message per line, in `seq` order, in the shape of `MESSAGE-FORMAT.md` section 1, followed by its earlier versions where they exist |
| `files/<file id>/<name>`           | The content of every file on an exported message, and of every custom emoji and avatar |
| `moderation.json`                  | Purge-request records, bans, time-outs, word filters. `reports` is always empty: a report's snapshot is what one person chose to show a channel's moderators or the admins |
| `settings.json`                    | Settings, without secrets          |
| `audit.jsonl`                      | The audit log                      |

Left out:

| Left out                                   | Why                              |
|--------------------------------------------|----------------------------------|
| Sealed chats, entirely: the conversation, who is in it, its messages, its files, its calls, key backups, epoch records and device lists | The design's rule. The server has nothing readable, and even the fact that two people have a sealed chat is not exported. |
| Anything a purge or retention removed      | It is gone.                      |
| Who answered what on an anonymous form     | Exported as counts only.         |
| Sign-in material: passkeys, authenticator secrets, recovery codes and links, sessions and tokens, provider subjects, devices and push addresses | Credentials. |
| The server's own keys, stream keys, provider secrets, webhook and integration tokens | Secrets. |
| Each person's private state: read positions, modes, stars, thread states, bookmarks, blocks, personal saved replies and presets, scheduled items, do not disturb and quiet hours, preferences | Personal, and not the community's content. |
| Who a message was pushed through do not disturb to, and whether it got through (`notify_anyway`) | It would reveal who has muted or blocked whom. The public mark on the message is exported. |
| The event log, the outbox, the preview cache, the search index | Derived or passing data. |
| Peers and streams                          | A shared channel's local copy is exported as a channel; the link is not. |

Private and ad-hoc channels, locked ones included, are in the export
even though the owner cannot read them through the API. That is the
catch the README prints: whoever runs the server holds the database.

## 19. Decisions recorded here

| No. | Gap                                              | Choice                                   |
|-----|--------------------------------------------------|------------------------------------------|
| S1  | Whether SQL may look inside JSON columns         | It does not, so both databases behave alike. |
| S2  | Whether an `accounts` row is ever deleted        | Never. Removal is a state.               |
| S4  | How long the audit log is kept                   | For good.                                |
| S5  | What `accounts.prefs` holds                      | The four keys listed. The skip of a meeting pause lives here because no column holds it. |
| S7  | How "keep for ever" is stored for one channel    | It is not: keeping for ever is the server's period being unset. `retention_seconds` is null (follow the server) or a period; `0` in a request clears it. |
| S8  | What `member_key` is and what happens when two ad-hoc channels come to have the same members | The formulas in section 5; the older channel keeps the key. |
| S9  | Whether two `files` rows may share a blob        | No. Each row owns its blob.              |
| S10 | How button presses are tallied                   | Under field id `_button`.                |
| S11 | Reactions of a removed account                   | Kept.                                    |
| S14 | How long call records are kept                   | As long as the notice message.           |
| S15 | The shape of the export                          | The archive in section 18.               |
| S16 | What locking does to rows                        | Group members become direct members and the channel's `channel_groups` rows are deleted. |

S3, S6, S12 and S13 were settled by the rulings and the new columns
(setting keys, no webhook session kind, `reports.source`,
`scheduled.failed_at`) and are no longer decisions of this document.

## 20. Where the schema falls short

Things these documents need that `schema.sql` does not have. None
has been changed.

| What                                          | Why                                         |
|-----------------------------------------------|---------------------------------------------|
| No column for "the pause from the current meeting was turned off" | Ruling R37 has turning do not disturb off clear it. It is kept in `accounts.prefs.meeting_pause_skip_until`; a column beside `quiet_skip_until` would be the natural home. |
| The search text is the raw message text       | A channel link is stored in `messages.text` as `<#conversation-id>`, and both search indexes are built from `text` as it is. Ruling R52 has the index hold the channel's name in place of the link. That needs a separate search-text column (written by the store) for the PostgreSQL generated column and the SQLite insert to read. |
| Search differs on accents                     | PostgreSQL's `simple` configuration keeps accents and SQLite's tokenizer removes them, so `cafe` finds `café` on one and not the other. |
