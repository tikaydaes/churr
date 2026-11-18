# Operations

How to run a Churr server, a hub and a relay: commands, settings,
ports, proxies, containers, databases, backup and upgrade. `DESIGN.md`
section 18 is the outline this document fills in.

Nothing here is built yet. Every command, flag and file below is the
design to build to, and every figure about capacity is untested.

`<RELAY-ADDRESS-NOT-CHOSEN>` stands for the address of the relay
Tikaydaes runs, and `<APP-ID-NOT-CHOSEN>` for the apps' identifier.
Neither has been chosen.

## 1. The three programs

| Program | What it is | Who runs it |
|---------|------------|-------------|
| `churr` | The server | Each community |
| `churr-hub` | A relay where several servers meet to share channels | Whoever convenes a group of servers |
| `churr-relay` | The push relay | Tikaydaes, or someone who builds and signs their own apps |

Each is one static binary with no other files. Most people run only
`churr`. Building any of them needs the Go toolchain and nothing
else.

**How configuration works, for all three.**

- A command is `program command [flags] [arguments]`.
- Every flag has an environment variable. For `churr` it is `CHURR_`
  followed by the flag's name in capitals with dashes turned into
  underscores: `-behind-proxy` is `CHURR_BEHIND_PROXY`. The hub uses
  `CHURR_HUB_` and the relay `CHURR_RELAY_`.
- A flag given on the command line wins over the environment.
- Switches take `true` or `false` in the environment.
- Flags and environment cover only what the program needs before it
  can open its database: where to listen, its host name, its data
  directory, its database. Everything else about a `churr` server is a
  setting in the admin console, stored in the database (section 5).
- There is no configuration file.

## 2. `churr`

    churr serve       run the server
    churr backup      write a consistent copy of the data directory
    churr recovery    print a one-time recovery link for an account
    churr owner       make an account an owner, or an owner an admin
    churr setup-link  print a new setup link for a server with no owner yet
    churr migrate-db  copy a community between SQLite and PostgreSQL
    churr import slack   read a Slack export into the community
    churr export      write the community's data to a file
    churr health      ask the server on this machine whether it is healthy
    churr version     print the version

`-data` and `-db` are accepted by every command that touches the
community, with the same meaning as for `serve`.

### `churr serve`

| Flag | Environment | Default | Meaning |
|------|-------------|---------|---------|
| `-data` | `CHURR_DATA` | `./data` | The data directory. Made if missing |
| `-hostname` | `CHURR_HOSTNAME` | None; required | The server's public host name, such as `chat.example.com`. It is the server's name to other servers and what passkeys are tied to. Do not change it later (section 17) |
| `-db` | `CHURR_DB` | Empty | Empty means SQLite at `churr.db` in the data directory. A `postgres://` address means PostgreSQL |
| `-behind-proxy` | `CHURR_BEHIND_PROXY` | `false` | A reverse proxy provides HTTPS. Serve plain HTTP, fetch no certificate, and believe the proxy's forwarding headers |
| `-http` | `CHURR_HTTP` | `:80`, or `127.0.0.1:8472` behind a proxy | Where to listen for HTTP. Alone, this serves certificate challenges and redirects to HTTPS. Behind a proxy, this is the whole service |
| `-https` | `CHURR_HTTPS` | `:443` | Where to listen for HTTPS. Not used behind a proxy |
| `-trusted-proxies` | `CHURR_TRUSTED_PROXIES` | `127.0.0.1/8,::1/128` | Addresses or ranges whose `X-Forwarded-For` and `X-Forwarded-Proto` are believed. Only with `-behind-proxy`. A forwarding header from a private address outside it is ignored and warned about (section 7) |
| `-media` | `CHURR_MEDIA` | `:24877` | Where to listen for call media, on UDP and TCP both. `off` runs without calls |
| `-media-address` | `CHURR_MEDIA_ADDRESS` | The addresses `-hostname` resolves to | The public IP addresses, or a host name, that clients send call media to. Comma-separated. See `CALLS.md` section 5 |
| `-tls-cert`, `-tls-key` | `CHURR_TLS_CERT`, `CHURR_TLS_KEY` | None | Use this certificate and key instead of fetching one. Read again when the files change |
| `-acme-directory` | `CHURR_ACME_DIRECTORY` | Let's Encrypt | The certificate authority's ACME address |
| `-unifiedpush-allow-private` | `CHURR_UNIFIEDPUSH_ALLOW_PRIVATE` | `false` | Allow UnifiedPush endpoints on private addresses (`PUSH.md` section 10) |
| `-clock-synced` | `CHURR_CLOCK_SYNCED` | `false` | Declare that this server's clock and the clocks of the servers linked to it directly are synchronised (by NTP, say). A measured clock offset beyond 10 ms is then put down to the network path and reported, not applied (`LINK-PROTOCOL.md` section 5). Never assumed: leave it out unless you know |
| `-log-level` | `CHURR_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `-log-format` | `CHURR_LOG_FORMAT` | `text` | `text` or `json` |
| `-dev` | `CHURR_DEV` | `false` | For trying it out: plain HTTP on `localhost:8472`, host name `localhost`, no certificate, data in `./data`. Passkeys work because browsers treat localhost as secure. It also raises the per-account request limit to 20 a second with a burst of 1,200, against the default 2 a second with a burst of 120. Never use it on a network |

`churr serve` runs migrations, starts the listeners, and prints the
setup link if the server has no owner yet (section 13). It stops
cleanly on SIGINT or SIGTERM: it stops accepting requests, ends live
calls, closes WebSocket connections with a "restarting" code so
clients reconnect promptly, and closes the database.

### `churr backup`

    churr backup [-data DIR] [-db URL] [-no-files] <destination directory>

Writes a complete, consistent data directory to the destination while
the server keeps running (section 11).

| Flag | Environment | Meaning |
|------|-------------|---------|
| `-no-files` | `CHURR_NO_FILES` | Leave out `files/`, for when uploads are backed up some other way |

### `churr recovery`

    churr recovery [-data DIR] [-db URL] <username>

Prints a one-time link at which that account can make a new passkey
or a new access key.
It is for an owner who is locked out; it works for any local account
that is a person. The link lasts one hour (`docs/AUTH.md` section 6),
and making it is written to the audit log with the actor `cli`. The
server may be running or stopped.

### `churr owner`

    churr owner grant <username> [the flags churr serve is run with]
    churr owner revoke <username> [the flags churr serve is run with]

For the extreme case an instance cannot settle itself: its owners are
gone, or one must be stopped. Inside Churr an owner makes other people
owners and nobody demotes an owner (`PERMISSIONS.md` section 9.1); this
is the operator's way round that. `grant` makes an active person's
account an owner. `revoke` makes an owner an admin, and refuses the
instance's last owner. It takes the same flags and environment as
`churr serve`, to find the data directory and the database. Stop the
server first: it keeps roles in memory and would not see the change.
The account moves in or out of the built-in groups as for any role
change, and the change is written to the audit log as `owner.granted`
or `owner.revoked` with `actor_kind` `cli`.

### `churr health`

    churr health [-http ADDRESS]

Asks the server on this machine for `GET /healthz` over plain HTTP and
exits 0 when it answers `200`, 1 otherwise, within five seconds. It is
the container health check: the image has no shell and no curl.
`-http` (`CHURR_HTTP`) is the address `churr serve` listens on for
plain HTTP, `127.0.0.1:8472` unless given; a missing or wildcard host,
as in `:8080`, means this machine. It is for a server behind a proxy
or a local trial. A server that answers HTTPS itself only redirects on
plain HTTP; check it at `https://<host name>/healthz` instead.

### `churr setup-link`

    churr setup-link [the flags churr serve is run with]

For a server that has no owner yet: makes a new one-time setup link,
prints it, and the link made before stops working. It is for an
operator who lost the link printed at the first start, which is not
printed again (section 13). It takes the same flags and environment
as `churr serve`, to find the data directory, the database and the
host name the link is for. The server may be running or stopped. With
an owner it refuses; a locked-out owner uses `churr recovery`.

### `churr migrate-db`

    churr migrate-db -from <path or URL> -to <path or URL>

Copies every table from one database to the other and rebuilds the
search index there. A path means a SQLite file; a `postgres://`
address means PostgreSQL. The destination must be empty. The server
must be stopped. The source is not changed. The command ends by
comparing row counts and says so. Environment: `CHURR_FROM`,
`CHURR_TO`.

### `churr import slack`

    churr import slack [-data DIR] [-db URL] [-hostname NAME] [-no-files]
                       [-dry-run] [-claims FILE] <export.zip>

Reads a Slack export and creates its channels, people, messages,
threads, reactions, pins, custom emoji and files (`DESIGN.md` section
15). Stop the server first.

| Flag | Environment | Meaning |
|------|-------------|---------|
| `-hostname` | `CHURR_HOSTNAME` | The server's public host name, for the claim links. Without it they are printed without a host |
| `-no-files` | `CHURR_NO_FILES` | Do not download the files the export links to. Without this, the command contacts Slack's file addresses |
| `-dry-run` | `CHURR_DRY_RUN` | Read the export and report what would be created, and change nothing |
| `-claims` | `CHURR_CLAIMS` | Write the claim links to this file, one line per person, instead of printing them |

Running it twice on the same export does not import anything twice.

### `churr export`

    churr export [-data DIR] [-db URL] [-no-files] -o <file.zip>

Writes the community's data to one zip file: accounts (profiles and
roles, no credentials), groups, sections, channels with their members,
messages with their history, reactions, forms and answers, pins, custom
emoji, uploaded files, settings and the audit log, in the files
`docs/SCHEMA.md` lists, in the shapes the API uses.

- Sealed chats are not in it. The server has nothing readable to put
  there.
- Tokens, passkeys, recovery codes and keys are not in it.
- It includes private and ad-hoc channels. Whoever can run this
  command already holds the database.
- It is the same export an owner can start from the admin console,
  and it is written to the audit log either way.
- The server may be running.

| Flag | Environment | Meaning |
|------|-------------|---------|
| `-o` | `CHURR_O` | The file to write. Required |
| `-no-files` | `CHURR_NO_FILES` | Leave out uploaded files |

### `churr version`

Prints the version, with the short commit it was built from in
brackets when that is known. Nothing else. `churr-hub version` and
`churr-relay version` print the same.

## 3. `churr-hub` and `churr-relay`

### `churr-hub`

    churr-hub serve           run the hub
    churr-hub fingerprint     print its name and fingerprint
    churr-hub invite          print an invitation for a server's owner
    churr-hub invites         list invitations not yet used
    churr-hub revoke-invite   cancel one
    churr-hub servers         list the servers on the hub, each with its measured clock
    churr-hub remove          remove a server
    churr-hub streams         list the streams it carries, each with its home hub
    churr-hub channels        list the channels it owns
    churr-hub appoint         make an account a manager of a channel
    churr-hub dismiss         take an account off a channel's managers
    churr-hub expel           remove a server from one channel
    churr-hub limits          show or set limits, per server or as the default
    churr-hub status          print what the status page shows
    churr-hub trunk           join this hub to other hubs (below)
    churr-hub cluster         list the cluster; leave it; remove a hub from it
    churr-hub rehome          move a channel's home to another hub of the cluster
    churr-hub version         print the version

`churr-hub serve`:

| Flag | Environment | Default | Meaning |
|------|-------------|---------|---------|
| `-data` | `CHURR_HUB_DATA` | `./hub-data` | Data directory: `hub.db`, `keys/`, `certs/` |
| `-hostname` | `CHURR_HUB_HOSTNAME` | Required | The hub's public host name |
| `-behind-proxy` | `CHURR_HUB_BEHIND_PROXY` | `false` | As for `churr` |
| `-http` | `CHURR_HUB_HTTP` | `:80`, or `127.0.0.1:8473` behind a proxy | |
| `-https` | `CHURR_HUB_HTTPS` | `:443` | |
| `-trusted-proxies` | `CHURR_HUB_TRUSTED_PROXIES` | Loopback | |
| `-status` | `CHURR_HUB_STATUS` | `127.0.0.1:8475` | Where to serve the status page. It shows totals only: servers joined and connected, streams open, channels by listing mode, the cluster, envelopes carried and the clocks measured. It names no server, stream or channel. It listens on localhost unless told otherwise. `off` disables it |
| `-keep-days` | `CHURR_HUB_KEEP_DAYS` | `30` | How long envelopes are kept for servers that were offline |
| `-server-rate` | `CHURR_HUB_SERVER_RATE` | `50` | Envelopes a second accepted from one server |
| `-max-envelope` | `CHURR_HUB_MAX_ENVELOPE` | `1048576` | Largest envelope, in bytes |
| `-clock-synced` | `CHURR_HUB_CLOCK_SYNCED` | `false` | Declare that the hub's clock and its servers' are synchronised by other means (NTP, say). A measured offset beyond 10 ms is then put down to the network path and reported, not applied (`LINK-PROTOCOL.md` section 5). Never assumed |
| `-tls-cert`, `-tls-key`, `-acme-directory`, `-log-level`, `-log-format` | `CHURR_HUB_…` | | As for `churr` |

The other commands take `-data` and `-hostname` as `churr-hub serve`
does:

- `churr-hub invite [--for NAME] [--expires DURATION]` prints an
  invitation an owner pastes into their admin console. `invites`
  lists those not yet used and `revoke-invite ID` cancels one.
- `churr-hub servers` lists each server's name, key fingerprint, when
  it joined, when it was last connected and its measured clock.
- `churr-hub remove <server name>` disconnects a server and refuses it
  from then on. The owners of the streams it was in are told, and
  start new key epochs.
- `churr-hub streams` lists the streams the hub carries, each with
  its home hub. `churr-hub limits` shows or sets the limits on what a
  server may submit, for one server or as the default. `churr-hub
  status` prints what the status page shows.
- There is no `churr-hub backup`. Stop the hub and copy its data
  directory.
- `churr-hub channels` lists the channels the hub owns, and those of
  other hubs of its cluster that it carries or lists, each with its
  home hub, its managers and moderators, and "(private)" after the
  listing mode of a private one. `churr-hub appoint STREAM ACCOUNT-ID USERNAME@SERVER`,
  `churr-hub dismiss STREAM ACCOUNT-ID` and `churr-hub expel STREAM
  SERVER` are the operator's last resort over a channel the hub owns
  (`LINK-PROTOCOL.md` section 23). They work only on the channel's
  home hub.

A hub's data directory holds its keys, thirty days of envelopes, and
the stream keys of every channel it is the home of, sealed under the
key file in `keys/`. With both, the envelopes can be read: the hub owns
those channels, and its operator can read them. Losing the directory
loses the hub's identity; the servers would have to join again.

#### Several hubs: a cluster

Hubs can be joined by trunks so that a channel reaches servers on
any of them (`LINK-PROTOCOL.md` section 23b). A cluster is a tree: a
hub joins through one member, and that is its only trunk into the
cluster until others join through it.

    churr-hub trunk invite [--for NAME] [--expires DURATION]
    churr-hub trunk join INVITATION
    churr-hub trunk cancel
    churr-hub cluster
    churr-hub cluster leave
    churr-hub cluster remove HUB
    churr-hub rehome STREAM HUB

- On a hub already in the cluster, `trunk invite` prints an
  invitation. On the hub that is to join, `trunk join` takes it; the
  running hub dials within a second. `churr-hub cluster` on either
  shows when it has been let in. A hub that is in a cluster with
  other hubs cannot join another, or the same one a second time (that
  would close a loop): it leaves first.
- `churr-hub cluster` lists the hubs with their fingerprints, which
  hub each joined through, this hub's clock against each, and the
  state of this hub's own trunks. Compare fingerprints with the other
  operators as servers' owners do.
- `cluster leave` takes this hub out. It is refused while another hub
  is joined through this one, or while a channel whose home is here
  has a server on another hub. It takes effect at once, whether or
  not `churr-hub serve` is running. If the hub is not serving, or its
  trunk is down, it keeps the word that it has left, goes on dialling
  the hub it joined through, and delivers that word as soon as it
  reaches it; until then that hub still lists it, and nothing more
  need be run. `cluster remove HUB`, run on the hub
  that HUB joined through, takes out a hub that is gone for good.
- Every channel has a home hub: the hub it was shared to. **If a home
  hub is down or cut off, its channels are read-and-post only among
  the servers that can still reach each other, until it returns.**
  Messages keep flowing between those servers; nobody can join,
  leave, be removed, become or stop being a manager or moderator, or change the
  channel's name, description or listing. Nothing moves the channel
  to another hub by itself, however long the home hub is gone.
- `rehome STREAM HUB`, run on the channel's home hub, moves the home
  to another hub of the cluster. Both hubs must be up and able to
  reach each other, with clocks within 15 seconds. The running hub
  does the move; the command waits up to about ten seconds and says
  either that it is done (the channel's home is now HUB) or that it
  failed, with the reason, and the home is still this hub. If the
  other hub has not answered within five seconds, the move is called
  off: the hub logs a warning, and the home stays here unless the
  other hub had already taken it, which the two hubs settle when they
  next reach each other. A command that gets neither answer in time
  says the running hub has not answered and asks whether `churr-hub
  serve` is running. A hub that is to be shut down for good is
  emptied of its channels this way first.
- A trunk that drops splits the cluster in two until it returns. Each
  part goes on exchanging messages among its own servers; when the
  trunk returns each part is sent what it missed, and people see the
  other part's messages arrive marked late, in their place by time.
- The status page adds three totals: hubs in the cluster, trunks
  connected, and channels of other hubs carried or listed.

### `churr-relay`

    churr-relay serve
    churr-relay version

| Flag | Environment | Default | Meaning |
|------|-------------|---------|---------|
| `-hostname` | `CHURR_RELAY_HOSTNAME` | Required unless behind a proxy | |
| `-behind-proxy` | `CHURR_RELAY_BEHIND_PROXY` | `false` | |
| `-http` | `CHURR_RELAY_HTTP` | `:80`, or `127.0.0.1:8474` behind a proxy | |
| `-https` | `CHURR_RELAY_HTTPS` | `:443` | |
| `-trusted-proxies` | `CHURR_RELAY_TRUSTED_PROXIES` | Loopback | |
| `-data` | `CHURR_RELAY_DATA` | `./relay-data` | Holds `certs/` only, and only when the relay fetches its own certificate |
| `-apns-key` | `CHURR_RELAY_APNS_KEY` | None | Path to Apple's `.p8` signing key |
| `-apns-key-id` | `CHURR_RELAY_APNS_KEY_ID` | None | That key's identifier |
| `-apns-team-id` | `CHURR_RELAY_APNS_TEAM_ID` | None | The Apple developer team identifier |
| `-apns-topic` | `CHURR_RELAY_APNS_TOPIC` | `<APP-ID-NOT-CHOSEN>` | The iPhone app's bundle identifier |
| `-apns-sandbox` | `CHURR_RELAY_APNS_SANDBOX` | `false` | Use Apple's development push service |
| `-fcm-credentials` | `CHURR_RELAY_FCM_CREDENTIALS` | None | Path to the Google service account file |
| `-device-rate` | `CHURR_RELAY_DEVICE_RATE` | `60` | Wake-ups a minute per push address |
| `-device-daily` | `CHURR_RELAY_DEVICE_DAILY` | `2000` | Wake-ups a day per push address |
| `-address-rate` | `CHURR_RELAY_ADDRESS_RATE` | `600` | Requests a minute per calling address |
| `-max-inflight` | `CHURR_RELAY_MAX_INFLIGHT` | `2000` | Requests in progress at once |
| `-dry-run` | `CHURR_RELAY_DRY_RUN` | `false` | For development: accept and count wake-ups, and send nothing to Apple or Google |
| `-tls-cert`, `-tls-key`, `-acme-directory`, `-log-level`, `-log-format` | `CHURR_RELAY_…` | | As for `churr` |

A relay started without the Apple flags answers iPhone wake-ups with
"platform off", and the same for Android without the Google file. It
needs no database and no accounts. What it does with a request, and
what it keeps, is in `PUSH.md` sections 5 and 7.

Running a relay is only useful together with apps signed by the same
developer accounts as its keys. A self-hoster using the published apps
uses the Tikaydaes relay and runs nothing.

## 4. The data directory

    data/
      churr.db           the SQLite database (not present with PostgreSQL)
      churr.db-wal       SQLite's write-ahead log, while running
      churr.db-shm       SQLite's shared-memory file, while running
      files/             uploaded files, thumbnails, avatars and emoji
        3f/a1/3fa1c0…    random names, two levels of folders
      keys/
        seal.key         AES-256 key that seals secrets stored in the database
        sign.key         the server's Ed25519 signing key
        box.key          the server's X25519 encryption key
      certs/             the certificate and the ACME account, when Churr fetches its own
      tmp/               uploads in progress; emptied at start

- Everything a community is lives here, plus the PostgreSQL database
  if one is used. Copying this directory to another machine and
  starting the binary there with the same host name restores the
  community.
- The directory and everything in it should be readable only by the
  user the server runs as. The server makes `keys/` with mode 0700 and
  refuses to start if the keys are readable by others.
- The keys are made at first start. **Keep them with every backup.**
  - Without `seal.key`, the secrets sealed in the database cannot be
    read: authenticator-app secrets, company sign-in client secrets,
    object storage credentials, and the keys of shared channels.
  - Without `sign.key` and `box.key` the server has a new identity,
    and every linked server and hub must be linked again.
- `certs/` can be lost without harm. A new certificate is fetched.
- With object storage on, new uploads are not in `files/` (section
  10).
- Use a local disk. SQLite does not work safely on network file
  systems.
- Nothing here is encrypted at rest except sealed secrets and the
  ciphertext of sealed chats. Use disk encryption (`DESIGN.md` section
  17).

## 5. Server settings

Stored in the `settings` table as a key and a JSON value, changed in
the admin console (`CLIENTS.md` section 16) or through the admin
settings operation, and effective at once without a restart. Every
change is written to the audit log.

Durations are in seconds. `null` means "none" or "no limit".

### General

| Key | Default | Meaning |
|-----|---------|---------|
| `server.name` | Asked at setup | The community's name, shown in the apps and on the sign-in page |
| `server.icon_file_id` | `null` | The community's icon |
| `server.time_zone` | `"UTC"` | The time zone used for an account that has none |
| `welcome.text` | `""` | Text shown to a new member on first sign-in, in Churr Markdown |

### Accounts and sign-in

| Key | Default | Values and meaning |
|-----|---------|--------------------|
| `registration.mode` | `"invite"` | `open`, `invite` or `closed` |
| `registration.approval` | `false` | "New registrations need approval". On, someone who registers without an invite in `open` registration makes their account and then waits until an owner, an admin or someone who may approve requests approves it in the approvals queue (`docs/FEATURES.md` section 31). Invites go straight in, as do company sign-in registrations |
| `invites.members_may_invite` | `false` | Whether members, not only admins, can make invite links. Guest invites are always admin-only |
| `auth.member_sign_in` | `"passkey"` | `passkey`, `either` or `sso`. `passkey` includes access keys. Owners and admins can always use a passkey or their access key |
| `auth.second_factor` | `"off"` | `off` or `required` |
| `auth.second_factor_accepted` | `["passkey", "totp", "provider"]` | Which second steps count, when one is required |
| `auth.allow_access_keys` | `true` | "Allow accounts without a passkey". On: someone who cannot use a passkey takes an access key (a long random code made for them, `docs/AUTH.md` section 5.6) when the account is made. Off: no account gets a new access key and the pages stop offering one; access keys that exist keep working and can be replaced. The admin console's people list and overview show who has no passkey |
| `sessions.passkey_seconds` | `2592000` (30 days) | How long a session from a passkey or access key sign-in lasts. Sliding: each use moves the end that far ahead |
| `sessions.sso_seconds` | `86400` (24 hours) | How long a session from company sign-in lasts. Fixed from the sign-in |

Changing one of these needs a fresh check: the admin console asks for
a passkey touch, the access key or an authenticator code first.

A session from a recovery code or link lasts one hour, which is not a
setting. A personal token under required company sign-in stops
working 30 days after the account's last company sign-in.

Company sign-in providers are not settings. They have their own table
and their own admin section.

### Messages and channels

| Key | Default | Values and meaning |
|-----|---------|--------------------|
| `messages.quick_correction_seconds` | `30` | The window for corrections that leave no trace |
| `messages.edit_window_seconds` | `null` | How long after posting an author may still edit. `null` is no limit |
| `retention.seconds` | `null` | The server-wide retention period. `null` keeps messages until deleted |
| `mentions.broadcast_managers_only` | `false` | Limit `@channel`, `@here` and `@everyone` to channel managers |
| `mentions.broad_mention_limit` | `250` | In a channel with more members than this, only its managers may use `@channel`, `@here` and `@everyone`; anyone else's is posted as plain text and notifies nobody. A channel's managers can set the channel to allow anyone or managers only instead. `0` is no limit |
| `mentions.broad_per_hour` | `20` | How many broad mentions one person may make in an hour, across the instance, managers and admins included; past it they are posted as plain text and notify nobody. Bots are not held to it. `0` is no limit |
| `members.adds_per_hour` | `60` | How many people one person may add to conversations in an hour, across the instance (adding members, and the people a channel is made with); past it the request answers `rate_limited`. Bots are not held to it. `0` is no limit |
| `channels.large_channel_limit` | `1000` | A channel with more members than this is delivered lazily: in full only to the live connections that have it open, with `conversation.changed` to the others at most once a second (`docs/EVENTS.md` section 4.1). A channel's managers can set the channel to `on` or `off` instead. `0` delivers every channel in full to everyone |
| `channels.large_feature_limit` | `50` | In a conversation with more members than this nobody is shown typing, and clients show an online count in place of each member's presence. `0` is no limit |
| `emoji.members_may_add` | `false` | Whether members, not only admins, can add custom emoji |
| `blocking.enabled` | `true` | Whether people can block each other |
| `webhooks.managers_may_approve` | `false` | Whether a channel's managers, not only admins, can approve webhook requests for it |
| `channels.default` | the first `#general` | "Default channels". The named channels a new owner, admin or member joins at sign-up, at most 50. A channel added later is offered to the people already here; it is not joined for them (`docs/FEATURES.md` section 14) |
| `roles.new_owner_delay_hours` | `0` | How many hours someone made an Owner waits before becoming one; any Owner withdraws it meanwhile by giving the account any role, and every Owner is told. 0 is at once. Changing it needs a fresh check (`docs/PERMISSIONS.md` section 9.1 note 12) |
| `channels.creation_approval` | `false` | "Creating a channel needs approval". On, a member who is not an owner, an admin or someone who may approve requests asks for a new channel, and it is made in their name when approved (`docs/FEATURES.md` section 31) |

### Files

| Key | Default | Values and meaning |
|-----|---------|--------------------|
| `files.max_bytes` | `104857600` (100 MB) | The largest upload |
| `files.store` | `"disk"` | `disk` or `s3` |
| `files.s3` | `null` | When the store is `s3`: `endpoint`, `region`, `bucket`, `prefix`, `path_style`, `access_key_id` |
| `files.s3_secret` | `null` | The secret access key, sealed under `seal.key`. Never shown again after it is entered |

### Features and outside connections

| Key | Default | Values and meaning |
|-----|---------|--------------------|
| `push.relay_enabled` | `false` | Send phone wake-ups through the relay. Only with an address in `push.relay_url` |
| `push.relay_url` | `""` | Which relay. There is none by default: the push relay stays off, and is reported off, until it has an address and is switched on |
| `push.unifiedpush_enabled` | `false` | Post wake-ups to UnifiedPush endpoints |
| `push.send_badge` | `true` | Include the badge number in wake-ups |
| `previews.enabled` | `false` | Fetch link previews |
| `sealed.enabled` | `true` | Allow sealed chats. Off: none can be started and existing ones become read-only |
| `calls.enabled` | `true` | Allow calls |
| `calls.max_participants` | `50` | Most people in one call |
| `calls.max_cameras` | `12` | Most cameras on at once in one call |
| `links.send_presence` | `true` | Send members' presence to linked servers that share a channel with them |
| `links.default_max_file_bytes` | `26214400` (25 MB) | The file size limit a newly shared channel starts with. An owner can change it per shared channel |

With these defaults a new server makes one kind of outbound
connection: wake-ups to the relay. First-run setup shows the switches
before the server is used.

Key names are the ones `docs/SCHEMA.md`, `docs/AUTH.md` and the API
use. The server keeps a few rows of its own in the same table that
are not for admins to change: `setup.token_hash`, `setup.done_at` and
the ticket counters.

### Not settings

- **Listeners, host name, data directory, database:** flags
  (section 2).
- **Rate limits** are fixed in the program (`CONVENTIONS.md`).
- **The format limits** in `CONVENTIONS.md` that are not marked as
  settings there.
- **Per-channel things** (a channel's retention, slow mode, welcome
  text) belong to the channel.
- **Per-person things** (modes, do not disturb, quiet hours, notify
  anyway) belong to the account.

## 6. Ports and firewall

### Inbound

| Port | Protocol | Running alone | Behind a proxy |
|------|----------|---------------|----------------|
| 443 | TCP | HTTPS: pages, API, WebSocket, other servers linking in | The proxy's |
| 80 | TCP | Certificate challenges, and a redirect to HTTPS | The proxy's |
| 24877 | UDP and TCP | Call media | Call media. Published directly, not through the proxy |
| 8472 | TCP, localhost only | Not used | Plain HTTP for the proxy |

Open 443 and 80 to the internet, and 24877 on both UDP and TCP.
Opening only UDP works for most people; TCP is the fallback for
networks that block UDP.

A server with calls switched off (`-media off`) needs only 443 and 80.

### Outbound

All are HTTPS on port 443 unless noted, and each happens only when
its feature is on (`DESIGN.md` section 17).

| To | When |
|----|------|
| The certificate authority | Running alone |
| The push relay | `push.relay_enabled` and `push.relay_url` set |
| UnifiedPush endpoints | `push.unifiedpush_enabled` |
| Pages members link to, ports 80 and 443 | `previews.enabled` |
| The object store | `files.store` is `s3` |
| The identity provider | Company sign-in is configured |
| Linked servers and hubs (WebSocket over HTTPS, and fetching files of shared conversations from the server they came from) | A peer exists |
| PostgreSQL, usually port 5432 | `-db` names one |
| DNS | Looking up any of the above, and the server's own name for the media address |

A server with every switch off and behind a proxy with a fixed
`-media-address` makes no outbound connection at all.

## 7. Alone, or behind a reverse proxy

### Alone

    churr serve -hostname chat.example.com -data /var/lib/churr

- Churr listens on 80, 443 and 24877, fetches a certificate for the
  host name from Let's Encrypt, and renews it. The host name's DNS
  record must point at the machine first.
- Binding ports under 1024 needs permission. With systemd:

      [Unit]
      Description=Churr
      After=network-online.target
      Wants=network-online.target

      [Service]
      User=churr
      ExecStart=/usr/local/bin/churr serve -hostname chat.example.com -data /var/lib/churr
      AmbientCapabilities=CAP_NET_BIND_SERVICE
      NoNewPrivileges=true
      ProtectSystem=strict
      ReadWritePaths=/var/lib/churr
      LimitNOFILE=65536
      Restart=on-failure

      [Install]
      WantedBy=multi-user.target

- To use a certificate from somewhere else, give `-tls-cert` and
  `-tls-key`. Port 80 then only redirects.

### Behind a reverse proxy

    churr serve -behind-proxy -hostname chat.example.com -data /var/lib/churr

Churr serves plain HTTP on `127.0.0.1:8472` and expects the proxy to:

- terminate HTTPS for the host name;
- pass WebSocket upgrades through, and not close a quiet connection
  for at least two minutes (clients send a heartbeat every 30
  seconds);
- pass the `Host` header unchanged;
- set `X-Forwarded-For` and `X-Forwarded-Proto`;
- allow request bodies as large as the upload limit, and not buffer
  them to disk first;
- not cache anything.

The proxy need not compress anything. Churr gzips the pages, scripts,
stylesheets and SVG pictures it serves itself, for any browser that
accepts gzip, with `Vary: Accept-Encoding` and an ETag for each form;
files under 1 KB, PNG pictures, fonts and sounds go as they are, and so
does any request for a byte range. A proxy with compression switched on
(Caddy's `encode`, nginx's `gzip on`) leaves those responses alone,
because they already carry `Content-Encoding`, and compresses only what
Churr sent plain. API responses and the live connection follow their
own rules for compression (docs/WIRE-PROTOBUF.md and docs/EVENTS.md).

If the proxy is on another machine, set `-http` to an address it can
reach and name the proxy in `-trusted-proxies`.

**Trusted proxies decide who the server thinks it is talking to.**
Churr believes `X-Forwarded-For` only from an address in
`-trusted-proxies` (`CHURR_TRUSTED_PROXIES`). From anyone else the
header is ignored, and every client behind that proxy looks like the
proxy's own address: sign-in limits and the log then count everyone as
one visitor. Nothing is trusted automatically.

- The setting must cover the address the proxy's connections arrive
  from *as Churr sees it*. For a proxy on the same machine that is
  loopback, the default. For Churr in Docker it is the gateway of the
  container's Docker network, whatever range Docker gave that network.
  The sample compose file assumes Docker's usual `172.16.0.0/12`. A
  host with its own address pools (`default-address-pools` in Docker's
  `daemon.json`, such as `10.240.0.0/12`) needs that range instead.
  To see it: `docker network inspect <project>_default` and read
  `Subnet` and `Gateway`.
- When a request with `X-Forwarded-For` arrives from a private or
  loopback address that is not trusted, Churr logs one warning for
  that network, naming the address and this setting, and the admin
  Overview shows the same warning until the server restarts:

      WARN a forwarding header came from an address that is not a trusted proxy and was ignored ... peer=10.240.0.1 network=10.240.0.0/24

  Add that network (or the whole Docker range) to
  `CHURR_TRUSTED_PROXIES` and restart. A header from a public address
  that is not trusted is ignored without a warning: anyone on the
  internet can send one.
- With two proxies in a row, such as a front proxy on the internet
  and Caddy on the host, trust both: the network Caddy connects from
  and the front proxy's address. Churr reads `X-Forwarded-For` from
  the right, past every trusted proxy, and takes the first address
  that is not one. Caddy must keep the header the front proxy sent
  (`trusted_proxies static <front proxy address>` in its global
  options); otherwise it replaces it and the front proxy is all
  Churr sees.

**The media port does not go through the proxy.** An HTTP proxy
cannot carry it. Open 24877 on UDP and TCP in the firewall, straight
to the machine Churr runs on. If the host name resolves to the proxy
and the proxy is on a different address from Churr, tell Churr its own
public address:

    churr serve -behind-proxy -hostname chat.example.com \
                -media-address 203.0.113.20

The same applies when a content-delivery network sits in front of the
host name: media must go to an address that reaches Churr itself.

### Behind NAT, or with a front proxy

A common home or office set-up: Churr runs on a machine with a LAN
address (say `192.168.1.20`) behind a router whose public address is
`203.0.113.7`, perhaps with a front proxy on the internet that
forwards the web traffic, and with split DNS, so the host name
resolves to the LAN address inside and to the router or front proxy
outside.

The web side works through the proxies. Calls do not, unless:

1. **The router forwards the media port** (24877 unless `-media` says
   otherwise) to Churr's machine, **UDP and TCP both**, with the same
   port number outside and inside. A front proxy cannot carry it: the
   port must reach Churr's machine through the router.
2. **`CHURR_MEDIA_ADDRESS` (`-media-address`) lists the public address
   and the LAN address**, public first:

       CHURR_MEDIA_ADDRESS=203.0.113.7,192.168.1.20

   Clients are told both and use whichever answers: people on the
   internet reach the public one, people on the LAN the LAN one (many
   routers do not loop traffic for their own public address back
   inside). With a front proxy, the public address is the router's,
   not the front proxy's.

Left empty, Churr tells clients what the host name resolves to on the
server's own machine. With split DNS that is the LAN address, so calls
quietly work on the LAN and nowhere else. Churr notices this case
without asking anything outside: when every media address is a private
one (LAN ranges, `fd00::/8`, `100.64.0.0/10`), it logs a warning once,
at start when the addresses are given as IP addresses or at the first
lookup of the host name, and the admin Overview shows it:

    WARN the media address is only private addresses: calls will connect on this network only ... addresses=192.168.1.20 port=24877

Churr cannot tell from inside whether the port is really forwarded.
The admin console's "Test calls" checks from the admin's browser; to
be sure, try a call from a phone on mobile data, off the LAN.

### Caddy

    chat.example.com {
        reverse_proxy 127.0.0.1:8472
    }

Caddy fetches the certificate, passes WebSocket upgrades, sets the
forwarding headers and streams uploads without further configuration.
Nothing is added for the media port; it is not Caddy's.

### nginx

    map $http_upgrade $connection_upgrade {
        default upgrade;
        ''      close;
    }

    server {
        listen 443 ssl;
        listen [::]:443 ssl;
        http2 on;
        server_name chat.example.com;

        ssl_certificate     /etc/letsencrypt/live/chat.example.com/fullchain.pem;
        ssl_certificate_key /etc/letsencrypt/live/chat.example.com/privkey.pem;

        # As large as files.max_bytes, with room for the form around it.
        client_max_body_size 110m;

        location / {
            proxy_pass http://127.0.0.1:8472;
            proxy_http_version 1.1;

            proxy_set_header Host              $host;
            proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            # WebSocket
            proxy_set_header Upgrade    $http_upgrade;
            proxy_set_header Connection $connection_upgrade;
            proxy_read_timeout  3600s;
            proxy_send_timeout  3600s;

            # Stream uploads and downloads; do not spool them.
            proxy_request_buffering off;
            proxy_buffering         off;
        }
    }

    server {
        listen 80;
        listen [::]:80;
        server_name chat.example.com;
        return 301 https://$host$request_uri;
    }

The media port is opened in the firewall, not in nginx:

    # example with ufw
    ufw allow 24877/udp
    ufw allow 24877/tcp

## 8. Containers

These three files are in the repository root. They follow the other
Tikaydaes projects: a static binary on a base image with no shell, and
a compose file that publishes the web port on localhost only for the
host's reverse proxy.

### Dockerfile

    # Builds the three static binaries and ships each on a minimal base
    # that has CA certificates and time zone data but no shell.
    #
    #   docker build -t churr .                         the server
    #   docker build -t churr-hub --target hub .        the hub
    #   docker build -t churr-relay --target relay .    the push relay
    FROM golang:1.27 AS build
    WORKDIR /src
    COPY go.mod go.sum ./
    RUN go mod download
    COPY . .
    ARG VERSION=dev
    RUN CGO_ENABLED=0 go build -trimpath \
        -ldflags="-s -w -X github.com/tikaydaes/churr/internal/version.Version=${VERSION}" \
        -o /out/ ./cmd/churr ./cmd/churr-hub ./cmd/churr-relay

    FROM gcr.io/distroless/static-debian12:nonroot AS hub
    COPY --from=build /out/churr-hub /churr-hub
    VOLUME /data
    EXPOSE 8080
    ENTRYPOINT ["/churr-hub"]
    CMD ["serve", "-data", "/data", "-behind-proxy", "-http", ":8080"]

    FROM gcr.io/distroless/static-debian12:nonroot AS relay
    COPY --from=build /out/churr-relay /churr-relay
    EXPOSE 8080
    ENTRYPOINT ["/churr-relay"]
    CMD ["serve", "-behind-proxy", "-http", ":8080"]

    # Last, so a plain "docker build ." makes the server.
    FROM gcr.io/distroless/static-debian12:nonroot
    COPY --from=build /out/churr /churr
    VOLUME /data
    EXPOSE 8080 24877/udp 24877/tcp
    # No shell or curl in the image: the server checks itself.
    HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
        CMD ["/churr", "health", "-http", ":8080"]
    ENTRYPOINT ["/churr"]
    CMD ["serve", "-data", "/data", "-behind-proxy", "-http", ":8080"]

A `.dockerignore` keeps `app/`, `e2e/`, `data/` and `.git` out of the
build. Images are published to `ghcr.io/tikaydaes/churr`,
`ghcr.io/tikaydaes/churr-hub` and `ghcr.io/tikaydaes/churr-relay`.

### compose.yaml

    # Churr behind the host's reverse proxy.
    #
    # The web port is published on localhost only; point the proxy at it.
    # The media port is the one exception: call audio and video cannot go
    # through an HTTP proxy, so it is published to the network directly.
    # Open it in the firewall on UDP and TCP.
    #
    # CHURR_TRUSTED_PROXIES must cover the Docker network the host's proxy
    # connects from. The default, 172.16.0.0/12, is Docker's usual range;
    # a daemon with its own address pools needs its range in .env. If it
    # is wrong, the log and the admin Overview warn and name the address.
    #
    # Copy .env.example to .env and fill it in first.
    services:
      churr:
        build: .
        # image: ghcr.io/tikaydaes/churr:latest
        restart: unless-stopped
        environment:
          CHURR_HOSTNAME: ${CHURR_HOSTNAME:?set CHURR_HOSTNAME in .env}
          CHURR_TRUSTED_PROXIES: ${CHURR_TRUSTED_PROXIES:-172.16.0.0/12}
          CHURR_MEDIA: ":${MEDIA_PORT:-24877}"
          CHURR_MEDIA_ADDRESS: ${CHURR_MEDIA_ADDRESS:-}
          CHURR_DB: ${CHURR_DB:-}
        ports:
          - "127.0.0.1:${HOST_PORT:-8472}:8080"
          - "${MEDIA_PORT:-24877}:${MEDIA_PORT:-24877}/udp"
          - "${MEDIA_PORT:-24877}:${MEDIA_PORT:-24877}/tcp"
        volumes:
          - ./data:/data
        healthcheck:
          test: ["CMD", "/churr", "health", "-http", ":8080"]
          interval: 30s
          timeout: 10s
          start_period: 30s
          retries: 3
        read_only: true
        tmpfs: [/tmp]
        cap_drop: [ALL]
        security_opt: ["no-new-privileges:true"]

      # Optional PostgreSQL. To use it: uncomment this service, set
      # POSTGRES_PASSWORD in .env, and set
      #   CHURR_DB=postgres://churr:<password>@db:5432/churr
      # The database is reachable from the compose network and nowhere else.
      #
      # db:
      #   image: postgres:17-alpine
      #   restart: unless-stopped
      #   environment:
      #     POSTGRES_USER: churr
      #     POSTGRES_DB: churr
      #     POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD in .env}
      #   volumes:
      #     - ./data/postgres:/var/lib/postgresql/data
      #   healthcheck:
      #     test: ["CMD-SHELL", "pg_isready -U churr -d churr"]
      #     interval: 5s
      #     timeout: 3s
      #     retries: 20

Notes on it:

- The media port has the same number inside and outside the
  container, because that number is what the server tells clients.
- The image runs as a user without privileges (uid 65532). Make the
  data folder writable by it before the first start:
  `mkdir data && sudo chown 65532:65532 data`.
- The server cannot see the host's public address from inside a
  container. It uses what `CHURR_HOSTNAME` resolves to. If that is not
  the address media should go to, set `CHURR_MEDIA_ADDRESS`; behind
  NAT, see "Behind NAT, or with a front proxy" in section 7.
- The first start prints the setup link in the container's log:
  `docker compose logs churr`.
- The health check runs `churr health` inside the container every
  30 seconds; `docker compose ps` shows the result.
- Commands run in the same image:
  `docker compose run --rm churr recovery -data /data <username>`.

### .env.example

    # Copy to .env and fill in. Never commit .env.

    # The public host name people will use. It cannot be changed later
    # without everyone making new passkeys.
    CHURR_HOSTNAME=chat.example.com

    # The localhost port your reverse proxy forwards to.
    HOST_PORT=8472

    # Reverse proxies whose X-Forwarded-For is believed (addresses or
    # ranges, separated by commas): the Docker network the host's proxy
    # connects from, and a front proxy if there is one. 172.16.0.0/12 is
    # Docker's usual range; if your Docker daemon has its own address
    # pools, use that range instead (docker network inspect
    # <project>_default shows it). A wrong value is logged as a warning
    # naming the address to add.
    CHURR_TRUSTED_PROXIES=172.16.0.0/12

    # Call media, UDP and TCP. Open this port in your firewall. It is
    # published to the network directly, not through the proxy.
    MEDIA_PORT=24877

    # The IP addresses clients send call media to, comma-separated. Leave
    # empty if CHURR_HOSTNAME resolves to this machine's public address.
    # Set it if the host name points at a proxy or CDN on another address,
    # or if this machine is behind NAT: then give the router's public
    # address and this machine's LAN address, public first, such as
    # 203.0.113.7,192.168.1.20, and forward MEDIA_PORT (UDP and TCP) to
    # this machine. Otherwise calls work on the LAN only.
    CHURR_MEDIA_ADDRESS=

    # Leave empty for the built-in SQLite database. For PostgreSQL:
    #   CHURR_DB=postgres://churr:<password>@db:5432/churr
    CHURR_DB=
    POSTGRES_PASSWORD=

### A local trial in two commands

    go build -o churr ./cmd/churr
    ./churr serve -dev

Open the setup link printed in the log. The web chat is then at
`http://localhost:8472/chat/`.

## 9. PostgreSQL

SQLite is the default and needs nothing. PostgreSQL is for larger
communities and for operators who already run it.

**Set-up.**

1. Use PostgreSQL 15 or newer.
2. Make a role and an empty database owned by it, in UTF-8:

       CREATE ROLE churr LOGIN PASSWORD 'choose-one';
       CREATE DATABASE churr OWNER churr ENCODING 'UTF8';

3. Start Churr with the address:

       churr serve -hostname chat.example.com \
             -db 'postgres://churr:choose-one@localhost:5432/churr'

   Put the address in `CHURR_DB` instead of on the command line to
   keep the password out of the process list.
4. Churr makes its tables at first start. It needs no extension and no
   superuser rights. Search uses PostgreSQL's built-in full-text
   index.

**Notes.**

- The data directory is still needed, for keys, certificates and
  uploaded files.
- Use `sslmode=verify-full` in the address when the database is on
  another machine.
- Churr opens a pool of up to 20 connections. A different number goes
  in the address as `pool_max_conns`.
- One database serves one community. Do not point two Churr servers
  at the same database.

**Moving an existing community.** Stop the server, then:

    churr migrate-db -from /var/lib/churr/churr.db \
                     -to 'postgres://churr:choose-one@localhost:5432/churr'

and start it again with `-db`. The same command with the arguments
swapped goes back. Keep the old database until the new one has been
checked.

## 10. S3-compatible object storage

By default uploads are files in `files/`. An admin can send new
uploads to an S3-compatible store instead: Amazon S3, or anything that
speaks the same protocol, such as MinIO, Garage or a hosting company's
object storage.

**Set-up.**

1. Make a private bucket. No public access, no website hosting.
2. Make an access key that can get, put, delete and list objects in
   that bucket and nothing else.
3. In the admin console, under Settings → Outside connections → Object
   storage, enter the endpoint, region, bucket, an optional prefix,
   whether to use path-style addresses (most stores that are not
   Amazon need it), the access key identifier and the secret.
4. "Test" writes, reads and deletes a small object. Saving is refused
   until the test passes.

**How it behaves.**

- Only file contents go to the store: uploads, thumbnails, avatars and
  emoji images, under random names. Names, sizes, types and who
  uploaded what stay in the database.
- Clients never talk to the store. They download through the Churr
  server, which checks permission each time and sets the content type,
  exactly as with files on disk. The bucket stays private and no
  signed addresses are handed out.
- Switching the store on affects new uploads. Files already on disk
  stay on disk and keep working: each file's record says where it is.
  Switching back does the same in reverse. There is no command to move
  existing files between the two in version 1.
- Retention, purges and deleted uploads delete the object from the
  store. Set the bucket not to keep old versions of objects, so that
  a delete there is a delete.
- Files in a sealed chat are encrypted on the device before upload,
  so the store holds ciphertext for those. Every other file is stored
  as it was uploaded: the store's operator can read it. Use the
  store's own encryption at rest.
- If the store cannot be reached, uploads fail with `unavailable` and
  downloads of files held there fail. Everything else keeps working.

## 11. Backup and restore

### With SQLite

**Back up** while the server runs:

    churr backup -data /var/lib/churr /backups/churr-$(date +%F)

The destination becomes a complete data directory:

- `churr.db`, a consistent snapshot taken in one step;
- `keys/`;
- `certs/`;
- `files/`, copied after the snapshot, so every file the database
  mentions is there. Files are never changed once written, so the copy
  is safe while uploads continue.

Copying the live data directory by hand also works if the server is
stopped first. Copying it while the server runs can catch the database
mid-write; use the command.

For large `files/`, run `churr backup -no-files` for the database and
keys and copy `files/` with a tool that only sends what is new, such
as `rsync`. Take the database snapshot first and the files second.

**Restore.**

1. Stop the server if it is running.
2. Put the backup where the data directory should be.
3. Start the server with the same `-hostname` as before.

Members are signed in as they were, unless their sessions ended in
the meantime. Anything posted after the backup is gone from this
server; linked servers still have their copies of shared channels and
this server catches up on those from each stream's time reference
(the owner server or the hub), as far back as it keeps (thirty days
on a hub).

### With PostgreSQL

Two things to back up, as close together in time as possible, database
first:

    pg_dump --format=custom --file=/backups/churr-$(date +%F).dump \
            'postgres://churr:…@localhost:5432/churr'
    churr backup -data /var/lib/churr -db 'postgres://…' /backups/churr-$(date +%F)

With `-db` set to PostgreSQL, `churr backup` copies `keys/`, `certs/`
and `files/` and leaves the database to `pg_dump`.

**Restore.**

1. Stop the server.
2. Restore the dump into an empty database:

       pg_restore --no-owner --dbname='postgres://churr:…@localhost:5432/churr' \
                  /backups/churr-2026-11-13.dump

3. Put the backed-up directory in place as the data directory.
4. Start the server with the same `-hostname` and `-db`.

### With object storage

`churr backup` copies only what is in `files/`. Objects in the bucket
are backed up with the store's own tools. Restore the bucket's
contents under the same names.

### What a backup contains

- Everything the operator can already read: all channels, private and
  ad-hoc ones included.
- Sealed chats as ciphertext only, and the locked copies of members'
  backup keys, which the server cannot open.
- The server's keys. **A backup is as sensitive as the server.**
  Encrypt it and control who can reach it.

A backup that restores is the only kind worth having. Restore one to a
spare machine, with `-dev` and the same data directory, before relying
on the routine.

## 12. Upgrading

1. Read the release notes.
2. Back up (section 11).
3. Replace the binary, or pull the new image.
4. Start it. Before the listeners open, the server checks which
   migrations the database has had against the ones this build
   carries, then applies the new ones, each in a transaction with its
   record. A failed migration leaves the database as it was and the
   server exits saying why.

       docker compose pull && docker compose up -d      # or
       git pull && docker compose up -d --build

- There is no downgrade. To go back, restore the backup taken in
  step 2.
- The database keeps the number and name of every migration applied
  to it. The server refuses to start, and changes nothing, when that
  record does not fit the build:
  - a migration is recorded under a name this build has a different
    migration for (the database ran a build whose migrations differ
    from this one's);
  - a migration this build has is missing below the last one
    recorded (it is never skipped);
  - the database has migrations this build does not (a newer build
    ran on it).

  The message names each migration concerned. Restore the backup
  taken before the upgrade, or run the build that matches the
  database: for the third case, the newer build that last ran on it.
  Do not edit `schema_migrations` by hand to get past it; the
  database would then claim a schema it does not have.
- The first start of a build that records names fills them in for a
  database created before names were recorded, taking each from this
  build's migration of that number. That is an assumption, and the
  limit of the check: on such a database, a migration an older build
  skipped or ran with different SQL under the same number cannot be
  seen in the record. To narrow the gap, before filling in the names
  the server works out every table and column those migrations create
  (by running them on a scratch database of the same kind: an
  in-memory SQLite database, or PostgreSQL's temporary schema in a
  transaction that is rolled back) and refuses to start, changing
  nothing, if any is missing from the database; the message lists
  each one. Only names are compared, not types, indexes or
  constraints. On PostgreSQL the database user needs the `TEMPORARY`
  privilege, which every user has unless it was revoked.
- Every start logs one line saying what was checked and done:

      INFO migrations: database verified against this build (10 migrations, names match); applied 0011_example dialect=sqlite schema=11
- A restart ends live calls and drops connections. Clients reconnect
  by themselves and catch up by sequence number. Pick a quiet moment.
- The web pages are part of the binary, so they upgrade with it.
  Browsers pick up the new files on their next load.
- The apps update through their stores. A server tells apps the oldest
  app version it supports; an older app says it needs updating.
  Releases keep at least the previous app version working.
- Linked servers do not have to upgrade together. The link protocol
  carries a version, and two servers use the newest they share.
- `churr-hub` upgrades the same way. `churr-relay` has no state:
  replace and restart.

## 13. First-run setup

When `churr serve` first starts and the server has no owner, it
prints:

    Churr has no owner yet. Open this link to set it up:
      https://chat.example.com/setup/Zk3…43 characters…
    The link works once, and keeps working across restarts until it is used.

A later start with no owner yet prints instead:

    Churr has no owner yet. The setup link printed when it first started still works.
    It is not printed again. If it is lost, run churr setup-link with the flags churr serve is run with: it prints a new link and the old one stops working.

- Opening it starts first-run setup (`CLIENTS.md` section 15): the
  owner's account and passkey, recovery codes, the community's name,
  the outside-connection switches, and the registration mode.
- The link is the only way to become owner. Anyone who has it can, so
  treat the first log like a password until setup is done.
- The token is stored as a hash. The link keeps working across
  restarts until it is used. A later start does not print it again
  (it has only the hash); it logs that the link printed earlier still
  works.
- If the link is lost before it is used, `churr setup-link` with the
  flags `churr serve` is run with prints a new one, and the old one
  stops working (section 2).
- After setup, `/setup/…` answers `not_found`.
- Until setup is done the server serves only the setup page and the
  health endpoint.

## 14. Health and status

| What | Where | Answers |
|------|-------|---------|
| Health | `GET /healthz` | `200` and `ok` when the process is serving and the database answered a trivial query within two seconds. `503` otherwise. No sign-in, no detail. For load balancers and container health checks; `churr health` asks it from inside the container |
| The server's name and public keys | `GET /.well-known/churr` | For other servers and for checking a key fingerprint by hand |
| What the server is and which features are on | The "server" operation of the API | Public. The apps read it before sign-in |
| Per-account totals | The status endpoint (`PUSH.md` section 11) | For a signed-in client |
| The operator's view | The admin console's Overview (`CLIENTS.md` section 16) | Version, uptime, database and file store size, connections, live calls, the state of each outside connection, warnings |
| Hub | `GET /healthz`, and the status page on its `-status` address | Totals: servers joined and connected, channels by listing mode, envelopes carried. No server or channel is named |
| Relay | `GET /healthz` | `ok` |

There is no metrics endpoint in version 1. The Overview page shows the
numbers an operator needs, and the same numbers are available through
the admin API for anyone who wants to scrape them.

## 15. Logs

All three programs write to standard error and keep no log files.
Rotation and storage are left to systemd, Docker or whatever runs
them.

    2026-11-13T09:30:00.412Z INFO  listening https=:443 http=:80 media=:24877
    2026-11-13T09:30:00.415Z INFO  database kind=sqlite schema=42
    2026-11-13T09:31:12.008Z WARN  sign-in failed method=passkey address=203.0.113.9
    2026-11-13T09:40:00.000Z INFO  janitor messages=312 files=18 events=20744 took=1.2s

`-log-format json` writes the same lines as JSON objects.

**Logged at `info`:** start-up (version, listeners, database kind,
which outside connections are on), the setup link, migrations, failed
sign-ins and recovery attempts with the address they came from, rate
limits being hit, failures reaching the relay, a peer, the object
store, the identity provider or the certificate authority, a call
starting and ending with its participant count, each janitor run,
import and export progress, shutdown.

**Logged at `debug` as well:** one line per request, with method,
path, status, duration and address. Secrets in a path (a webhook
address, a setup or recovery link) are replaced by `…`. Signalling and
link-protocol traffic are summarised.

**Never logged, at any level:** message text, file names and contents,
topics, tokens and cookies, passkey data, recovery codes, push
addresses, and anything from a sealed chat.

The log is for the operator. What admins did is in the audit log, in
the database, where admins can see it.

## 16. Sizing

How to choose a structure and a machine from what a community needs.
The figures come from `BENCHMARKS.md`, at the top of the repository,
which has the full tables and their conditions: synthetic text chat from
a load tool acting as every member, rented virtual machines, one run per
row. **Every threshold below is marked "measured" (it was run) or
"inference" (it was not, and may be wrong).**

### What decides the size

- **People online at once**, not members. Each live connection costs
  memory, and each message is sent to every connected member of its
  channel. Every measurement counted people online; how many members
  who are not online a server carries is not measured. Size for the
  number who may be online together, and for everyone if that is not
  known (inference: members who are not connected hold no connection).
- **How busy**: messages posted a second across the whole server. The
  work is the deliveries: a post to a channel of 750 with everyone
  online is 750 deliveries. In the tests one post in ten went to a
  channel everyone is in, so 50 posts a second with 10,000 online was
  83,500 deliveries a second.
- **Calls**: measured on one machine only (below).
- **Growth**: disk, below.

### Step 1: the machine

| Machine | People online, quiet (5 posts/s) | Busy | Basis |
|---|---|---|---|
| 1 vCPU, 1 GB | 12,000 healthy; refuses new connections from about 14,000 | 5,000 online at 50 posts/s with SQLite, 100 with a dedicated PostgreSQL | measured |
| 2 vCPU, 4 GB | 20,000 healthy; 40,000 delivered, but a message to everyone takes over a second; refuses from about 56,000 | 10,000 online at 50 posts/s with SQLite, 100 with a dedicated PostgreSQL | measured |
| 8 vCPU, 16 GB | 20,000 healthy; 60,000 delivered with no refusal, but a message to everyone takes about 1.2 s at the slowest | 20,000 online at 200 posts/s with SQLite; at 400 posts/s the SQLite run was over and the PostgreSQL-on-the-box run healthy (Step 2) | measured |
| Between two rows | The larger row's machine | | inference |
| 4 vCPU, 8 GB and 16 vCPU, 64 GB | 5,000 and 50,000 members ran well within these machines | | measured on an earlier build |
| More than 60,000 online, or more than 400 posts/s with 20,000 online | Not measured. The network card is the first limit met on 8 vCPU (the transmit-queue warning appeared from 50 posts/s with 20,000 online) | | inference |

"Healthy" means everything arrived, and 99 in 100 posts were answered
and 99 in 100 deliveries arrived within one second. Rules of thumb from
those rows:

- **Memory**: about 50 KiB of server memory per person online (measured
  from 5,000 to 40,000 on 1 and 2 vCPU), plus the system and, on the same
  box, the database. Planning beyond the measured sizes with that figure
  is inference.
- **CPU**: about half goes to Churr and half to the kernel sending
  packets. One CPU sent about 59,000 deliveries a second with SQLite at
  its largest healthy rate; two CPUs about 83,500, so a second CPU does
  not double it (measured). Eight CPUs sent 530,000 a second at 200
  posts/s with half the machine idle (measured).
- **Calls**: one call of 25 on audio and 8 on video, on 4 vCPU and 8 GB,
  took about 1.2 CPUs and 113 Mbit/s out (measured, on an earlier
  build). That build held 860 MB during the call; the current one keeps
  far less for a call, and its memory in a call has not been measured
  on a rented machine. Calls on smaller machines, or several calls at
  once: not measured. A server on a home connection should expect small
  calls (inference from the bandwidth).

### Step 2: SQLite or PostgreSQL

Choose on what the database is for, not on the size of the community. No
number of members or people online has been found at which SQLite fails
to carry a community that the same machine otherwise carries. The one
measured place where SQLite stops first is a sustained posting rate of
more than 800 posts a second (below), far beyond the busiest community
in the tests with ordinary channels.

| | SQLite | PostgreSQL on its own machine | PostgreSQL on the same machine |
|---|---|---|---|
| A post's answer, at moderate load | Fastest: 7 to 24 ms at half (measured) | A few milliseconds more for the round trips: 10 to 77 ms (measured) | 16 to 68 ms on 2 vCPU; 6 to 7 ms on 8 vCPU (measured) |
| Largest healthy rate, 1 and 2 vCPU | 50 posts/s | 100 posts/s: the database's work has left the box, so the server has CPU to spare (measured) | 50 posts/s on 2 vCPU; the worst of the three past it (measured) |
| Largest healthy rate, 8 vCPU, 20,000 online | 200 posts/s; the 400 posts/s run was over, with the network card dropping packets and no database error (measured, one run) | not measured | 400 posts/s, the highest tried (measured, one run) |
| Writing alone, 8 vCPU, small channels | Between 800 and 1,600 posts/s (measured) | not measured | 1,600 posts/s and not at its limit (measured) |
| Memory | Inside Churr | None on the server | 1.1 to 2.9 GB of a 4 GB machine; 2.0 to 3.3 GB on 8 vCPU (measured) |
| What it gives | Nothing else to run; one folder to back up | A database that outlives the chat machine, replication, the backup and monitoring an operator already runs for PostgreSQL, its load off the box | PostgreSQL's tools, without the separate machine |

- **SQLite** is the default and right for most servers. It logged no
  busy or locked error at any rate tested (measured). Its own write
  ceiling, on 8 vCPU with small channels, lies between 800 and 1,600
  posts a second (measured); with channels of 750 the delivery work
  stops the server well before that. PostgreSQL on the same machine
  carried 1,600 posts a second in that run without slowing (measured,
  one run).
- **A dedicated PostgreSQL** is for an operator who wants the database to
  survive the chat machine, to replicate it, or to take its load off the
  box. It costs a few milliseconds per post and frees CPU on the server;
  on 1 and 2 vCPU that carried one rate step more (measured). The
  database machine was never above 4% busy in any run (measured), so a
  small one will do (inference: no run used a small database machine).
- **PostgreSQL on the same machine** pays the round trips and also takes
  CPU and memory from the server. Choose it only to use PostgreSQL's
  tools and plan to move it to its own machine (inference).

`churr migrate-db` moves a community either way, so the choice is not
permanent.

### Step 3: one server, or several through a hub

One server serves one community; there is no clustering and no
failover, and recovery from a dead machine is a restore. A hub does not
spread one community over several machines: it links separate servers,
each with its own members and database, so that channels can be shared
between them.

- **One server** for one community, at any size in the table above.
- **Several servers through a hub** when the people belong to separate
  servers anyway (several organisations, or one per site) and share some
  channels. Size each server by its own members, as in Step 1: a
  shared channel is delivered by every server to its own members, so a
  server carries its share of a shared channel's deliveries as if the
  channel were local. Measured: three 1-vCPU servers with 1,000 members
  each in one shared channel were healthy at 40 posts/s and over at 80;
  with 2,000 each, healthy at 20 and over at 40. Past that, members on
  the poster's server still get messages quickly and deliveries from
  the other servers arrive seconds late.
- **The hub** can be the smallest machine. A 1 vCPU / 1 GB hub never used
  more than 0.11 of its CPU and 46 MiB while three servers ran full
  (measured); its own limit was not reached. Raise each server's submit
  limit (`churr-hub limits`, default 3,000 envelopes a minute) for busy
  shared channels: every post to a shared channel is one envelope.
- Splitting one large community into several servers that share their
  busy channels through a hub is possible, but whether that carries more
  than one larger server is not measured.

### Step 4: disk

| | SQLite | PostgreSQL | Basis |
|---|---|---|---|
| Per message (200-byte text, search index and events log included) | about 1.5 KB | about 1.6 KB | measured |
| Per member (each in 4 channels) | about 1.6 KB | about 1.3 KB | measured |

So a million messages take about 1.5 GB, and 10,000 members about
16 MB (arithmetic on the measured figures). SQLite's write-ahead log
adds tens of megabytes (16 MiB through 126,000 messages; up to 80 MiB
after a reconnect storm, measured). Longer messages, reactions and
edits take more (inference).

Uploaded files are not in these figures and usually dominate (not
measured). Set a retention period or use object storage if uploads will
outgrow the disk.

### Approaching a limit: what you see and what to do

| Limit | What you see | What to do |
|---|---|---|
| Memory, from people online | The log says `memory guard: new live connections are being refused` and, when it gets tighter, `memory guard: memory is tight`; the admin console's Overview shows a warning with the numbers; clients are told 4029 "the server is short of memory; try again shortly" and retry by themselves about 30 seconds later. Everyone already connected carries on. Measured: from about 14,000 online on 1 GB and about 56,000 on 4 GB with SQLite | A machine with more memory (the next row). See [At the memory limit](#at-the-memory-limit) |
| CPU, from how busy it is | Messages arrive late: posts are answered more slowly and deliveries take seconds instead of a fraction of one, while the machine's CPU is above about 90% busy. There is no log line for it. Measured past the limit: everything still arrives, later; at four times the healthy rate some posts time out at the client's 30 seconds; it is healthy again as soon as the load falls | More CPUs, or move the database to its own machine (one rate step more on 1 and 2 vCPU, measured) |
| The network card | The log says `TCP segments are being sent again ... this machine's transmit queues are dropping packets`, and `live: the pace of writes to connections was set` when the server starts pacing itself. Measured on 8 vCPU at 400 posts/s with 20,000 online | The optional tuning in [Large servers](#large-servers-the-network-cards-transmit-queue), or a machine with a faster network |
| A crowd reconnecting | After a restart or a network cut, clients are told 4029 "too many clients are reconnecting; try again shortly" and come back over the next minute. Measured: 10,000 coming back as the apps do were all back within 49 s on 1 vCPU | Nothing: it is the server spreading the crowd out |
| SQLite's write ceiling | Posts wait for seconds while the CPU is mostly idle and the log has no error. Measured on 8 vCPU between 800 and 1,600 posts/s with small channels | PostgreSQL: on the same 8-vCPU machine it carried 1,600 posts/s with posts answered within 9 ms (measured, one run); on its own machine it should do as well or better (inference) |

### What the numbers do not cover

- Members who are not online (members against people online was not
  run).
- Reconnect storms on 2 vCPU as the apps do them (only all at once),
  bursts, and real people: the load is steady and synthetic.
- Uploads, search and push.
- Any browser, Firefox and Safari included: the load tool is the client.
- A dedicated PostgreSQL behind the 8-vCPU server; PostgreSQL on the same
  box on 1 vCPU; the 4- and 16-vCPU machines on the current build.
- Hub arrangements other than the measured one.
- Variation between runs, other hardware and other providers.

### At the memory limit

A machine with no swap kills a process that takes more memory than
there is, with no warning: on 1 vCPU and 1 GB that happened to Churr
at about 9,500 live connections, before it had a guard. Now the server
watches its memory once a second and stops growing before the kernel
has to act. There is nothing to set.

What it may use is the cgroup's limit where there is one (`MemoryMax=`
or `MemoryHigh=` in a systemd unit, `--memory` in Docker; cgroup v2 and
v1, and the limit of any cgroup above its own), and otherwise the
machine's memory. What is left is the kernel's `MemAvailable`, or the
limit less the cgroup's working set. To that it adds the garbage the Go
runtime holds and could give back: the figure it acts on is what would
be left after a full collection. It works on Linux; elsewhere there is
no guard and the server runs as it did.

| State | When | What the server does |
|---|---|---|
| normal | a fifth of the limit or more would be left | Admits live connections, each second only as many as there is room for above the next line (at 128 KiB each), so a crowd arriving at once cannot run past it |
| tight | under a fifth (190 MB of 1 GB) | Refuses new live connections: the upgrade is answered and closed with 4029 and "the server is short of memory; try again shortly", which clients answer by waiting about 30 seconds, spread at random. Gives back what it holds only to be quick (collects, returns freed memory to the system, again every 30 seconds). Everything else, and everyone connected, carries on |
| critical | under a sixteenth (60 MB of 1 GB) | Also closes live connections with the same 4029, those with the most queued first and then those idle longest, each second as many as would bring it clear, never more than a fiftieth of them (or eight). They reconnect by themselves and miss nothing |

A state is left when memory has been a quarter clear of its line for
five seconds.

Memory that closed connections held counts as in use until the Go
runtime has collected it. So that people coming back after a network
blip are not turned away for memory that is theirs to have, the guard
collects first: when a connection finds no room, and when a crowd has
just left (500 connections and a fifth of those there were), at most
once every two seconds, and it gives the freed memory back to the
system. A connection that finds no room in the normal state is not
refused at once but kept waiting, before its upgrade is answered, for
that collection and the looks after it, up to five seconds and at most
2,000 at a time. A collection takes one processor for as long as it
takes to walk what is in use (tenths of a second for a few hundred
megabytes; not measured on a small machine).

Why those lines. The sixteenth is always kept free for the kernel, the
database's pages in the page cache and whatever else runs on the
machine; the Go runtime is given a memory limit of its own (as
`GOMEMLIMIT` would, recomputed as the rest of the machine changes) that
makes it collect harder to stay that far from the edge. The fifth
leaves the collector the difference to work in, so a server that has
stopped admitting is slower, not stuck.

At the start (and again if the limit is changed under it) the server
logs which limit it keeps to and where it read it:

    INFO memory guard: watching memory against this limit; ... limit="700 MiB" limit_from="the cgroup's memory limit (/sys/fs/cgroup/system.slice/churr.service)" left="694 MiB" left_after_collection="696 MiB" tight_under="140 MiB" critical_under="43 MiB"

Because garbage the runtime can give back is counted as free, the
system's own figure (`MemAvailable`, or the unit's `MemoryCurrent`
against `MemoryMax`) can be under the tight line while the server is
not tight. It says so once when that happens, with how much the
runtime can give back (`less is left than the tight line, but ...`).

Each change of state is one log line, `warn` on the way in and `info`
on the way out, with the numbers:

    WARN memory guard: memory is tight; new live connections are refused (4029, clients retry in about 30 seconds) until there is room from=normal to=tight limit="957 MiB" limit_from="the machine's memory" left="131 MiB" left_after_collection="188 MiB" tight_under="191 MiB" critical_under="59 MiB" ...
    INFO memory guard: memory is back to normal; live connections are admitted again ...

Connections are also refused in the normal state, when more arrive in
a second than there is room for above the tight line; a server that
fills slowly can sit just above that line and never be called tight.
So refusing has lines of its own, whatever the state: one `warn` when
the first connection is turned away, and one `info`, with the total,
when none has been for a minute.

    WARN memory guard: new live connections are being refused (4029, clients retry in about 30 seconds): more are arriving than there is memory for state=normal refused_so_far=226 ...
    INFO memory guard: no live connection has been refused for a minute refused=226 over=41s ...

While either lasts the admin console's Overview shows a warning with
the same numbers and how many connections were refused and closed.

`CHURR_MEMORY_LIMIT` in the environment overrides it: a size (`700M`,
`2G`, or a number of bytes) is the most the server's own resident
memory may reach, held against the same two lines, and can only
tighten what the system says; `off` removes the guard. If `GOMEMLIMIT`
is set the Go runtime's limit is left as the operator set it.

What a connection costs the server, measured with nothing being sent:
about 15 KiB of live memory with a proxy ending TLS and about 22 KiB
when the server ends TLS itself (it was 39 and 46 KiB), of which 8 KiB
is the stack of the one goroutine that reads it. The process's resident
size per connection is more than that: the collector's working room,
the accounts' state and the kernel's socket buffers come on top.

### Large servers: the network card's transmit queue

Every delivery to a connection is one small packet out and one
acknowledgement in. A message to a channel that fifty thousand people have
open is fifty thousand packets handed to the kernel within milliseconds,
and a busy server of that size sends some 300,000 a second. Two limits of
the machine, not of Churr, are met there:

- Linux gives a network interface a transmit queue of 10,240 packets
  (`fq_codel`). What does not fit is dropped on the machine itself and
  sent again by TCP's timer, 200 ms later, then 400, and so on.
- A virtual network card with one queue passes a fixed number of small
  packets a second (about 220,000 on the 16-vCPU machine in
  `BENCHMARKS.md`), and all of its packets are handled on one processor.

Past either, deliveries arrive late while the processors idle.

**The server handles this itself.** On Linux it reads, once a second,
what the machine's own transmit queues say (what `tc -s qdisc` shows:
packets dropped, packets waiting; no privilege is needed). When they drop
more than 100 packets a second, or stand full for three seconds, while it
is writing more than 20,000 frames a second, it sets a pace for its
writes to live connections, seeks the rate the card carries, lowers the
pace when the queues overflow again, and lets go of it when the load has
gone. Frames that wait for the pace wait in order in each connection's own
queue and leave together in one write, so fewer packets carry the same
messages. The log says when a pace is set, lowered or lifted (`live: the
pace of writes to connections was set`) and at what rate. On other
systems there is no pace unless one is pinned.

Only this machine's queues set a pace. Packets lost further along, or by
the receivers (a member on a poor network, a load generator whose own
network card is full), also make TCP send segments again, and no pace
cures that; slowing down for it only delays everyone. Once a minute, at
`warn`, the log says when over 1% of segments are being sent again and
which it is: `this machine's transmit queues are dropping packets`, or
`lost on the path or by the receivers ... nothing is slowed for it`.

`CHURR_LIVE_WRITES_PER_SECOND` pins the pace: a number of writes a second
for all live connections together (200,000 suited the machine above), or
`off` for never any pace. It is an environment variable only, and is not
needed in ordinary use.

Optional tuning of the machine, which raises what it can carry and is
lost at reboot unless made permanent (replace `enp7s0` with the interface
clients arrive on; run as root):

```
# How many queues the card has; if "Combined" can be raised, raise it.
ethtool -l enp7s0
# A deeper transmit queue, so a burst waits and is not dropped.
tc qdisc replace dev enp7s0 root pfifo limit 400000
# Spread the handling of incoming packets over all processors (here 16:
# the mask ffff) when the card has one queue.
echo ffff  > /sys/class/net/enp7s0/queues/rx-0/rps_cpus
echo 32768 > /proc/sys/net/core/rps_sock_flow_entries
echo 32768 > /sys/class/net/enp7s0/queues/rx-0/rps_flow_cnt
echo ffff  > /sys/class/net/enp7s0/queues/tx-0/xps_cpus
# To look: packets dropped by the queue, and segments sent again.
tc -s qdisc show dev enp7s0
nstat -az TcpOutSegs TcpRetransSegs TcpExtTCPTimeouts
# Back to the default:
tc qdisc replace dev enp7s0 root fq_codel
echo 0 > /sys/class/net/enp7s0/queues/rx-0/rps_cpus
```

The pace needs none of this. With the pace and the machine's default
settings, 50,000 connections all viewing one channel at 50 posts a
second receive half of their deliveries within 178 ms, 95 in 100 within
365 ms and 99 in 100 within 541 ms (`BENCHMARKS.md`).

### Load testing

`tools/loadtest` acts as a community's members against a running
server and measures it; `go run ./tools/loadtest -h` lists everything.
On one machine, `loadtest -data DIR -url ... -members 500` does it all.
For a larger run the steps are separate, so the load comes from other
machines than the one being measured:

1. **Prepare**, on the database's machine or beside it (for SQLite, on
   the server's machine, with the server running or stopped). It makes
   the members and channels and writes each member's token and channels
   to a file. An empty server gets its owner with `loadtest -bootstrap`
   first.

       loadtest -prepare -data /var/lib/churr/data -db "$CHURR_DB" \
           -members 50000 -channels 400 -per-member 6 -members-file members.json

2. **Sample**, on the server's machine, for as long as the run lasts:

       tools/loadtest/sample.sh -d /var/lib/churr/data -b "$CHURR_DB" server.csv

   Every two seconds it records the CPU seconds churr and PostgreSQL
   have used, their memory, idle CPU and network bytes, and at the end
   the size of the data directory and the database. The script's first
   lines describe the columns.

3. **Generate**, on each generator, with the members file copied to it.
   Every generator gets the same flags apart from `-shard` and `-out`:

       loadtest -members-file members.json -url https://chat.example.com \
           -shard 1/3 -start-at 2026-01-02T15:04:00Z \
           -online 0.3 -rate 50 -duration 5m -out g1.json

   `-online` is the share of members who hold a connection and post;
   the rest exist and are in their channels, as people who are away
   are. `-rate` is for the whole run. `-start-at` is when posting
   begins: leave time for every generator to connect first. `-storm`
   adds a reconnect storm, at the same instant on every generator.
   `-viewing` is the share of the connected members who have #general
   open, for a #general the server delivers lazily (more members than
   `channels.large_channel_limit`): below 1 every member says what it
   has open, that share is sent #general's messages in full, and the
   report counts the events that arrived in full and the
   `conversation.changed` signals that arrived in place of the rest.
   At 1, the default, nobody says what they have open, as clients from
   before lazy delivery do not, and everyone is sent everything.

4. **Merge**, anywhere: `loadtest -merge g1.json g2.json g3.json -out
   run.json` prints the report of the whole run, with percentiles
   worked out from every generator's samples together.

What to know:

- The generators' clocks must agree (NTP or chrony): phases are timed
  by the clock, and a delivery from one generator's post to another's
  connection is timed across two clocks. The report also gives delivery
  times within one generator, which no clock difference touches.
- One machine reaches one server address over about 28,000 connections
  at most with Linux's default port range, so 50,000 online members
  need two generators or more. A generator raises its own open-file
  limit as far as it is allowed; `ulimit -n` must allow one file for
  each connection.
- The report warns when a generator used more than 80% of its CPUs.
  Latencies from such a run are the generator's own; use more
  generators.
- The members file holds every member's token. A server that has been
  load-tested holds the test's members and channels for good: test on
  a server made for it.

**Calls.** `loadtest -call` holds a call in the fullest channel of the
members file in place of posting. Every person is a WebRTC client that
sends made-up media of the real weight over the real API, live
connection and media port; nothing is encoded or decoded.

    loadtest -call -members-file members.json -url https://chat.example.com \
        -call-audio 25 -call-video 8 -call-for 1m

| Flag | Meaning |
| --- | --- |
| `-call-audio N` | People in the call, each with a microphone. `-call-video 0` makes it a voice call |
| `-call-video N` | How many of them have a camera on, in three sizes. Everyone is shown every camera but their own at 640×360 |
| `-call-listeners N` | More people who publish nothing and are forwarded everything |
| `-call-talk SHARE` | The share of the time each person with a microphone talks; 1 is everyone at once |
| `-call-for D` | How long is measured, once everyone is in and the pictures have settled |
| `-call-sweep LIST` | One call after another and a table of them: `5,10,25,25/8,25/8/20` is people, people/cameras or people/cameras/listeners |
| `-pid N` | The server's process, when the generator runs on the server's machine: adds the server's CPUs and memory, in all and for each person |
| `-call-budget MIB` | With `-pid`: exit with an error if a call's memory for each person passes this |
| `-heap FILE`, `-diag URL` | Save the server's heap profile with everyone still in. The server must be a `-tags diag` build; `-diag` is where its profiles are (`CHURR_DIAG_ADDR`, by default `http://127.0.0.1:6060`) |

The report gives, for audio and for video: packets received and lost,
in all and for the person who lost most; the delay of every packet from
the client that sent it to the client that received it, at p50, p95, p99
and the worst; bit rates; which camera sizes arrived; time to join; and
who was dropped or left without a picture. The delay is timed on the
generator's clock at both ends, so it needs no agreement between
machines, and it includes the generator's own share.

What to know:

- A call takes no more people than `calls.max_participants` (50) and
  `calls.max_cameras` (12); listeners count as people. Raise the
  settings to test beyond them.
- Memory for each person is the server's growth since before the first
  call of the run, over the people in the call. A process does not hand
  memory back the moment it is freed, so start the server afresh for a
  run, and put the calls of a sweep in rising order: a call held after
  a larger one is marked `≤` in the table, its memory being at most
  what was read, and `-call-budget` does not judge it. For the memory
  of one size alone, start the server and hold only that call.
- With `-pid` the generator shares the server's machine and its CPUs:
  the memory figures stand, the CPU, loss and delay figures are of the
  two together. For those, run the generator elsewhere and
  `tools/loadtest/sample.sh` on the server.
- A generator sending and receiving a call with cameras is busy: the
  report gives its own CPUs. Near all of them, loss and delay are the
  generator's.

## 17. Troubleshooting

**The setup link is lost.** Run `churr setup-link` with the flags
`churr serve` is run with (in Docker:
`docker compose run --rm churr setup-link -data /data`). It prints a
new link and the old one stops working. Restarting does not replace
it.

**An owner is locked out.** On the server:
`churr recovery -data /var/lib/churr <username>` prints a one-time
link to make a new passkey. If no owner can be reached at all, stop
the server and run `churr owner grant <username>` for another person.

**No certificate is issued.** Running alone, the certificate authority
must reach the server on ports 80 and 443 at the host name. Check the
DNS record, the firewall, and that nothing else holds those ports. The
log says what the authority answered. Repeated failures can hit the
authority's rate limit; wait an hour.

**Passkeys fail, or sign-in says the address is wrong.** A passkey is
tied to the host name. `-hostname` must be exactly what people type in
the browser, and a proxy must pass the `Host` header unchanged.

**The host name has to change.** Passkeys made for the old name will
not work for the new one, and linked servers know this server by the
old name. Changing it means: start with the new `-hostname`, give
every person a recovery link to make a new passkey, and make every
link to another server again. Avoid it; choose the name with care.

**Apps and the web chat say "reconnecting" forever behind a proxy.**
The proxy is not passing WebSocket upgrades, or is closing idle
connections. See the nginx example in section 7.

**Everyone is rate-limited together, or the audit log shows every
address as the proxy's.** `-trusted-proxies` does not include the
proxy, or the proxy is not setting `X-Forwarded-For`.

**Uploads fail above a certain size.** The proxy's body limit is lower
than `files.max_bytes` (nginx: `client_max_body_size`).

**Calls join and there is no sound or picture, or "This network
blocks calls".** Media is not reaching port 24877.

1. Use "Test calls" in the admin console (`CALLS.md` section 12).
2. Check the firewall allows 24877 on UDP and TCP, to the machine
   Churr runs on, not to the proxy.
3. Check the address clients are sent to. If the host name points at a
   proxy or CDN on another address, set `-media-address`.
4. In a container, check the port is published with the same number
   on both sides, on UDP and TCP.
5. If only some people fail, their network blocks the port. TCP on
   the same number is tried automatically; nothing else can be done
   from the server.

**Phones are not notified when the app is closed.**

1. Admin console → Outside connections: is the relay on, and does the
   Overview show it reachable? The server needs outbound HTTPS to it.
2. On the person's account page, does the device show a push kind
   other than `none`?
3. Is the person in do not disturb, or is the conversation's mode one
   that does not notify?
4. If the phone shows only "New message", the wake-up arrived and the
   phone could not fetch the content. The phone must be able to reach
   the server from wherever it is; a server reachable only on a
   private network cannot fill in notifications for a phone outside
   it.

**"Database is locked", or everything is slow, with SQLite.** The
data directory is probably on a network file system, or the disk is
slow. Move it to a local disk. If the community has simply grown, move
to PostgreSQL.

**The disk is full.** Uploaded files are nearly always the cause. Set
a retention period, lower the upload limit, or switch new uploads to
object storage. The server stops accepting uploads and posts, and
keeps serving reads, when it cannot write.

**Authenticator codes or company sign-in fail for everyone.** Check
the server's clock. Both depend on it being right to within a minute.

**Company sign-in loops back to the sign-in page.** The return
address registered at the identity provider must match the host name
exactly. The admin console's "Test" on the provider shows what was
sent and received.

**A linked server shows as disconnected.** The admin console's Linked
servers section shows the last error. The side that accepted the
invitation dials out, so it needs outbound HTTPS to the other. Shared
channels keep working locally and catch up when the link returns.

**After a restore, linked servers refuse this one.** `keys/` was not
restored, so the server has a new identity. Restore the keys from the
backup. If they are gone, each link must be made again.

**After a restore, authenticator codes, company sign-in or object
storage stop working.** `seal.key` was not restored. Restore it. If it
is gone, those secrets must be entered again and members must set
their authenticator apps up again.

**Search finds nothing after moving databases.** `migrate-db` rebuilds
the index and reports it. If it was interrupted, run it again into an
empty database.

**The server will not start after an upgrade.** Read the last lines of
the log. If it says the database's applied migrations do not match
this build, nothing was changed; section 12 says what each case means.
A failed migration leaves the database unchanged; restoring the old
binary works as long as no migration completed. Otherwise restore the
backup.

## 18. Decisions recorded here

- Every flag has an environment variable made from its name. There is
  no configuration file.
- Object storage is configured in the admin console, with its secret
  sealed in the database, not by flags.
- Turning object storage on affects new uploads only. Nothing moves
  existing files.
- A backup directory is a complete data directory, files and
  certificates included.
- `churr recovery` prints a link, works for any local person's
  account, and lasts an hour. There is no flag for its length.
- `churr owner` needs the server stopped and never removes the last
  owner.
- The export from the command line includes private and ad-hoc
  channels and never sealed chats.
- The Slack import needs the server stopped.
- The setup link is printed once and lasts across restarts until it
  is used; `churr setup-link` replaces a lost one.
- The default ports: 8472 (server behind a proxy), 8473 (hub), 8474
  (relay), 8475 (hub status page), 24877 (media).
- The compose file publishes the media port to the network. That is
  the one exception to "localhost only".
- The hub's status page listens on localhost.
- No metrics endpoint.

## 19. Needs from other documents

- **API:** the admin settings operation reads and writes the keys in
  section 5, and tests object storage before saving. The admin
  overview operation returns what the Overview page shows.
- **Link protocol:** a protocol version, so servers on different
  releases can stay linked.
- **Schema:** nothing new. `files.blob_key` must say whether a file is
  on disk or in the object store; a prefix on the key is enough.

## 20. To verify before building

- That the `nonroot` distroless image and a read-only root file system
  work with everything the server writes (the data volume and `tmp/`).
- That Let's Encrypt still issues without a contact address, since
  Churr never asks for an email address.
- That UDP port publishing in Docker preserves what ICE needs
  (`CALLS.md` section 20).
- The lowest PostgreSQL version the schema's features need.
- The nginx directives above against the current nginx release.
