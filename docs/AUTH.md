# Authentication

How an account gets in and stays in: first-run setup, registration and
invites, passkeys, access keys, recovery, company sign-in over OIDC and SAML, the
second factor, sessions, sign-in for the apps, and the tokens used by
scripts, bots, webhooks and integrations.

Nothing here is built yet. `DESIGN.md` section 9 is the outline this
document fills in, and `CONVENTIONS.md` binds it. Tables and columns
are the ones in `schema.sql`. Operations are named in words; their
paths are in `openapi.yaml`. Section 18 lists every choice this
document made where the design was silent, and section 19 lists facts
that still need checking.

## 1. Rules that hold everywhere

- **No passwords and no email.** Churr never asks for a password, never
  sends email and never stores an email address. An access key
  (section 5.6) is not a password: nobody chooses it, it is 256 random
  bits made for the person, and the server never sees it.
- **Credentials are random and stored hashed.** Every token is 256
  random bits from the operating system, written in base64url without
  padding after its prefix from `CONVENTIONS.md`. The server stores
  only the SHA-256 hash of the whole string, prefix included. A token
  is shown once, when it is made.
- **One-time values in links** (the setup link, a recovery link, an
  OAuth authorization code, a pairing code) are also 256 random bits in
  base64url, stored only as SHA-256 hashes. They carry no prefix: they
  are not API credentials and work in one place only.
- `churr_link_` strings, which invite one server to link with
  another, are not sign-in credentials and are in
  `LINK-PROTOCOL.md`.
- **A prefix names the only place a token works.** `churr_ses_` is
  accepted only in the `churr_session` cookie, never as a bearer token.
  `churr_app_`, `churr_pat_` and `churr_bot_` are accepted only in the
  `Authorization` header. `churr_ref_` is accepted only when exchanging
  it for new app tokens. `churr_hook_` is accepted only in a webhook
  address. `churr_int_` is accepted only on integration operations.
- **Secrets the server must use are sealed.** TOTP secrets, OIDC client
  secrets, the SAML signing key and webhook signing secrets are
  encrypted with AES-256-GCM under the key file in `keys/`, with a
  random 96-bit nonce stored in front of the ciphertext and the table
  name, column name and row identifier as associated data, so a
  sealed value cannot be moved to another row.
- **Short-lived sign-in state lives in memory.** Passkey ceremonies,
  company sign-in requests waiting for their answer, used SAML
  assertion identifiers and rate-limit counters are kept in the
  process, not in the database. A server is one process (`DESIGN.md`
  section 1), so nothing is lost by this except that a restart makes
  people who were halfway through signing in start again. Everything
  in memory has an expiry and a size cap, and the oldest entries are
  dropped first.
- **Links are safe to preview.** Opening a setup, invite, claim or
  recovery link only shows a page. The one-time value is used up only
  when the person presses the button on that page, so a chat client or
  mail scanner that fetches the link does not consume it.
- **Errors do not say which part was wrong.** A failed recovery says
  "that username and code do not match" whether or not the username
  exists. A failed passkey sign-in says the passkey is not registered
  here. A failed access key sign-in says "that username and access key
  do not match", whether or not the username exists.

## 2. Who signs in, and the settings that decide how

| Role     | Signs in with                                              |
|----------|------------------------------------------------------------|
| `owner`, `admin` | A passkey or their access key, always. Company sign-in too, when the policy offers it and they have linked an identity. |
| `member`, `guest` | Whatever the member sign-in policy allows (section 8). |
| `bot`    | A bot token and nothing else.                               |
| `remote` | Nothing. A shadow of an account on another server cannot sign in here. |

An account that is not yet claimed (`accounts.claimed_at` is null) or
has been removed (`accounts.removed_at` is set) cannot sign in by any
method, and none of its tokens work.

The policy is held in `settings`. The values are from `DESIGN.md`
section 9. The first two keys are named in `SCHEMA.md`; the `auth.*`
keys are named here, and the other documents use these names.

| Key                           | Values                        | Default   |
|-------------------------------|-------------------------------|-----------|
| `registration.mode`           | `open`, `invite`, `closed`    | `invite`  |
| `invites.members_may_invite`  | `true`, `false`               | `false`   |
| `auth.member_sign_in`         | `passkey`, `either`, `sso`    | `passkey` |
| `auth.second_factor`          | `off`, `required`             | `off`     |
| `auth.second_factor_accepted` | A list from `passkey`, `totp`, `provider` | all three |
| `auth.allow_access_keys`      | `true`, `false` ("Allow accounts without a passkey") | `true` |

Changing any of these is an owner or admin action, needs a fresh check
(section 10.4) and is written to the audit log with the old and new
value.

## 3. First-run setup and the owner

The first account is the owner. It is made through a one-time link.

1. `churr serve` starts and finds no account with role `owner`. It
   makes a setup value, stores its hash in `settings` under
   `setup.token_hash`, and prints the setup link to its standard
   output and log. The link keeps working across restarts until it is
   used: a later start without an owner finds the stored hash, keeps
   it, and logs that the earlier link still works (it cannot print it
   again, having only the hash). `churr setup-link` makes a new link
   for an operator who lost it, and the old one stops working.
2. The operator opens the link. The page checks the value against the
   stored hash and shows the setup form. Until setup is done, every
   other page and API operation answers `unavailable`.
3. The operator chooses a username and display name and creates a
   passkey (section 5.2, with the setup value in place of an invite),
   or, if they cannot use a passkey, takes an access key (section 5.6).
4. On success the server, in one transaction:
   - inserts the `accounts` row with `role = 'owner'`, a fresh
     `webauthn_handle`, and `claimed_at` set;
   - inserts the `passkeys` row and eight `recovery_codes` rows
     (section 6.1), or the `access_keys` row and no recovery codes;
   - inserts a `sessions` row of kind `web`, `signed_in_by =
     'passkey'` or `'access_key'`;
   - writes `owner.created` to `audit_log` with `actor_kind = 'server'`
     and how the owner signs in.
   From this moment the setup value is refused, because an owner
   exists. Its hash is deleted when setup finishes.
5. The page shows the recovery codes once (an access key was shown
   and confirmed before the account was made). The rest of setup (the
   outbound connections table with its switches, the registration
   mode, `#general`) is in `FEATURES.md` section 26. It ends by
   deleting `setup.token_hash` and setting `setup.done_at`. An owner
   who closes the page early signs in with their passkey or access key
   and is taken back to where they stopped.

There is exactly one owner. If the owner loses every passkey and every
recovery code, or their access key, the operator runs `churr recovery
<username>` on the server (section 6.2). Anyone who can read the server's output before
setup is finished can become the owner; the operator should finish
setup straight after the first start.

## 4. Registration and invites

### 4.1 Registration modes

| Mode     | A new account can be made                                   |
|----------|-------------------------------------------------------------|
| `open`   | By anyone who can reach the server and passes the sign-in policy: with a passkey (or an access key) if the policy is `passkey` or `either`, through company sign-in if it is `either` or `sso`. |
| `invite` | Only with a usable invite, by passkey, access key or company sign-in as the policy allows. |
| `closed` | By nobody. Existing accounts sign in as before. Claim links for imported accounts still work, because they create no account. |

With `open` and `sso` together, "anyone" means anyone the identity
provider lets through, which is the usual setting for a company.

### 4.2 Invites

An invite is a row in `invites`.

| Column              | Meaning                                              |
|---------------------|------------------------------------------------------|
| `code`              | 128 random bits in base64url (22 characters). Stored as it is, not hashed, so an admin can copy the link again from the invite list. |
| `role`              | `member` or `guest`. An invite never makes an admin. |
| `group_ids`         | Groups the new account is added to (`group_members` rows, `added_by` the invite's creator). Groups filled from a provider cannot be listed. |
| `conversation_ids`  | For a guest: the named channels the guest is added to, at each channel's default level. |
| `claims_account_id` | Set on a claim link (section 4.4).                   |
| `max_uses`, `uses`  | `max_uses` defaults to 1. 0 means no limit.          |
| `expires_at`        | Defaults to seven days after creation. May be null for no expiry. |
| `created_by`        | The inviting account, or null for links printed by the importer. |
| `revoked_at`        | Set when the invite is withdrawn.                    |

Who may create one:

- An admin may create any invite.
- If `invites.members_may_invite` is true, an account with role `member`
  may create invites with `role = 'member'`, `max_uses = 1`, an expiry
  of at most seven days, and no groups or conversations. The new
  account lands in the channels `@everyone` is in and nowhere else.
- Guest invites are made by admins only. A guest invite must name at
  least one conversation, since a guest sees nothing else.
- Guests and bots cannot invite.

An invite is usable when `revoked_at` is null, `expires_at` is null or
in the future, and `max_uses` is 0 or `uses < max_uses`. A use is
counted at the end of registration, in the same transaction that
inserts the account, with an update that only succeeds while the invite
is still usable. Two people racing for the last use cannot both win.

Creating and revoking invites is written to the audit log. So is each
registration, with the invite's `id`.

### 4.3 Registering with a passkey

Allowed when the sign-in policy lets the invite's role use passkeys
(section 8).

1. The person opens the invite link (or the registration page, when
   registration is `open`). The page asks for a username and a display
   name.
2. The page asks to begin a passkey registration, sending the username
   and the invite code. The server checks the registration mode, the
   invite, the username rule and that the name is free among local
   usernames and group handles, and that the address has not been rate
   limited (section 14). It reserves nothing.
3. The passkey ceremony runs (section 5.2).
4. On success the server, in one transaction: counts the invite use;
   inserts `accounts` (`role` from the invite, `claimed_at` now, a
   fresh `webauthn_handle`); inserts `passkeys`; inserts eight
   `recovery_codes`; inserts `group_members` and `members` rows named
   by the invite, and `members` rows for every channel `@everyone` is
   in if the role is `member`; inserts a `sessions` row of kind `web`
   with `signed_in_by = 'passkey'`; writes `account.registered` to the
   audit log. If the username was taken in the meantime the transaction
   fails with `name_taken` and the passkey just made is orphaned on the
   device; the page says so and asks the person to delete it.
5. The page shows the recovery codes once.

**With an access key in place of a passkey.** The same page offers "I
can't use a passkey — give me an access key" beside "Create a passkey
and join", and leads with it in a browser that has no passkey API.
The page makes the key, shows it once, and has the person type its
last group again (section 5.6). Only then does it send the invite
code, the names and the value derived from the key in one request,
and the server does step 4 with an `access_keys` row in place of the
`passkeys` row, no recovery codes and no `webauthn_handle` (the
account gets one with its first passkey), and `how` in the audit
detail set to `access_key`. A refused username sends the person back
to the form with the same key. Allowed while `auth.allow_access_keys`
is on; under `open` registration without an invite, the address is
also held to the day's count of new accounts (section 14).

### 4.4 Claim links for imported accounts

The Slack importer creates accounts with `claimed_at` null and prints
one claim link per person. A claim link is an invite whose
`claims_account_id` names that account, with `max_uses = 1`,
`created_by` null and an expiry 30 days after the import. An admin can
make a fresh claim link for an unclaimed account at any time, which
revokes the earlier one.

Claiming follows section 4.3, by passkey or by access key, with these
differences: no account is inserted; the existing row gets
`claimed_at`, a `webauthn_handle` when a passkey is made and, if the
person chose one that is free, a new username. The role is the
one the importer gave the account. Claim links work in every
registration mode. Whoever holds a claim link takes over that account
and its imported history, so the operator must hand each link to the
right person.

With company sign-in on, an imported account is also claimed without a
link when the provider's verified email matches (section 7.4).

## 5. Passkeys and access keys

Churr uses the `go-webauthn` library, as Codemail does. Sections 5.1
to 5.5 are passkeys; section 5.6 is the access key, for someone who
cannot use one.

### 5.1 Configuration

| Setting                 | Value                                         |
|-------------------------|-----------------------------------------------|
| Relying-party ID        | The server's host name, exactly: `chat.example.org`, never a parent domain. For a local trial, `localhost`. |
| Allowed origin          | `https://<host name>` with the port if it is not 443. For a local trial, `http://localhost:<port>`. No other origin is accepted, and none can be added in settings. |
| Relying-party name      | The server's display name.                    |
| Resident key            | `required`. Every passkey is a discoverable credential, so sign-in needs no username. |
| User verification       | `required`, at creation and at every use. A response without the user-verified flag is refused. |
| Attestation             | `none`. Churr does not restrict which authenticators may be used. |
| Algorithms              | ES256, EdDSA and RS256, in that order of preference. |
| User handle             | `accounts.webauthn_handle`: 32 random bytes made when the account's first passkey is registered. It is not the account identifier and says nothing about the person. |
| Ceremony lifetime       | Five minutes, held in memory, usable once.    |

The relying-party ID ties every passkey to the host name. If the
server moves to another host name, no existing passkey works there and
every person must come back in through a recovery code or link.

### 5.2 Creating a passkey

The same steps serve setup, registration, claiming, adding another
passkey, and enrolling one as a second factor.

1. **Begin.** The server builds creation options with a fresh random
   challenge, the settings above, the account's user handle (a new one
   for a new account), the account's existing credentials as the
   exclude list, and the extension input `prf`. It keeps the challenge
   and what the ceremony is for (new account with which invite, or
   which signed-in account) in memory under a random ceremony
   identifier, and returns the options and that identifier.
2. **The browser** calls `navigator.credentials.create`. The
   authenticator checks a PIN or biometric and makes the key pair.
3. **Finish.** The page sends the ceremony identifier, the response,
   a name for the passkey, and the PRF result reported by the browser.
   The server takes the ceremony out of memory (so it cannot be used
   twice) and has the library verify the response: the challenge, the
   origin, the relying-party ID hash, the user-present and
   user-verified flags, and the signature format.
4. **Stored.** One `passkeys` row: `credential_id`; `credential` (the
   library's credential record as JSON: public key, signature counter,
   transports, and the backup-eligible and backed-up flags); `name`
   (the person's, or "Passkey added 13 Nov 2026"); `has_prf`;
   `created_at`.

**PRF is requested on every creation.** The PRF extension lets a
passkey produce a secret that only that passkey can produce. Churr
uses it to unlock sealed-chat history (`SEALED.md` section 13). An
authenticator decides at creation whether a credential supports PRF,
and it cannot be switched on afterwards. So every creation asks for
it, whether or not the person uses sealed chats yet, and whether or
not sealed chats are on.

`has_prf` records what the browser reported: `true` when the
creation's extension results contain `prf.enabled: true`. It is a
hint for the account page, which marks other passkeys as "signs in,
cannot unlock sealed chats". It is not a security decision: extension
results are not signed by the authenticator, and a browser that lies
about them only misleads its own user. When a later use of the passkey
asks for a PRF value and gets none, the client reports that and the
server sets `has_prf` to false. The server never sees a PRF value.

Which passkey stores support PRF is in section 19.

### 5.3 Signing in with a passkey

1. **Begin.** The server builds request options with a fresh challenge,
   user verification required and an empty allow list, keeps the
   challenge in memory, and returns the options and a ceremony
   identifier. No username is asked for and nothing in the reply
   depends on who is signing in.
2. **The browser** calls `navigator.credentials.get`. When the page is
   the web chat or the app sign-in page, it adds the PRF evaluation
   input from `SEALED.md` section 13, so the same touch that signs in
   also yields the unlocking secret. That secret stays in the page.
3. **Finish.** The server takes the ceremony out of memory. The
   library finds the account by the user handle in the response and
   verifies the challenge, origin, relying-party ID hash, flags and
   signature against the stored public key.
4. **Checks after the library.** In this order, each ending the
   attempt:
   - the account is local, claimed and not removed;
   - the account's role may sign in with a passkey under the policy
     (section 8);
   - the signature counter did not go backwards. A stored counter
     above zero followed by a lower or equal one suggests a copied
     authenticator: the sign-in is refused and `passkey.clone_suspected`
     is written to the audit log. Passkeys that always report zero
     (synced ones do) are not affected.
5. **Stored.** The `passkeys` row is updated: the new counter and
   flags in `credential`, and `last_used_at`. A `sessions` row is
   inserted: kind `web`, `signed_in_by = 'passkey'`, `second_factor`
   null, `pending_second_factor` false (a passkey sign-in checked a
   PIN or biometric on a device the person holds, so it already
   counts as two factors), `expires_at` 30 days ahead. The cookie is
   set (section 10.2).

### 5.4 Adding and removing passkeys

- Adding one needs a signed-in web session with a fresh check
  (section 10.4), then section 5.2. A passkey added this way cannot
  unlock sealed chats until a device that is already unlocked
  approves it (`SEALED.md` section 13.4).
- Removing one needs a fresh check. The last passkey of an account
  that has no access key and no linked company identity cannot be
  removed. The owner and admins are always asked to keep at least two.
- Removing a passkey deletes its `passkeys` row and, by cascade, its
  locked copy of the backup key in `backup_keys`. The clients then
  rotate the backup key (`SEALED.md` section 13.6).
- Both are written to the audit log with the passkey's name, never its
  key.
- An account whose role must use company sign-in may still hold
  passkeys. They serve as a second factor and to unlock sealed chats,
  and are refused at step 4 of section 5.3.

### 5.6 Access keys

Ruling R80. Someone who cannot use a passkey (an old browser, a
device with no authenticator, a passkey manager they do not want)
gets an **access key** instead: a long random code made for them,
which they cannot choose. They keep it secret, in a password manager,
and type it on each device. It does not stop them adding a passkey
later. It is one way in alongside passkeys: it signs in, it is a fresh
check (section 10.4) and it unlocks a locked session (section 10.6).

**Format.** 35 bytes: a version byte (1), 32 random bytes from the
browser's `crypto.getRandomValues`, and the first two bytes of the
SHA-256 of those 33 as a checksum. The 35 bytes are read as one
big-endian number and written in base 58 with the alphabet
`23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz`: the
digits and letters without `0`, `1`, `O` and `I` (small `l` is kept).
It is left-padded with the alphabet's zero digit to exactly 48
characters and shown as 12 groups of 4 separated by spaces, which is
the strength of the Matrix security key. The version shows through
as the first character: every key begins with `2`. An example:

    2BKQ ZQ55 Qlmj wU4a rXb9 9EcA zlfo dzTn 7det HP7g XDdo yErH

**Reading typed text.** Spaces, other whitespace and hyphens anywhere
are ignored; case matters. A `0`, `1`, `O` or `I` is refused with a
message that says those are never in a key; a wrong length says how
many characters were typed; a checksum that does not match, which is
what a wrong letter or a wrong case gives, is reported as "that key
has a typing mistake". All of this happens in the page, before
anything is sent. The grammar is implemented three times
(`internal/secret/accesskey.go`, `static/shared/accesskey.js`,
`app/lib/core/access_key.dart`), and all three run the vectors of
`testdata/accesskey/vectors.json`.

**Derivation.** The key never leaves the page that made or reads it.
What is sent is

    auth = HKDF-SHA-256(ikm = the 32 random bytes,
                        salt = "churr access key v1",
                        info = "churr access key auth v1", n = 32)

as `access_key_auth`, in base64url. A second derivation, with info
`"churr access key wrap v1"`, is reserved for locking device or
sealed-chat keys under an access key; nothing uses it yet, and the
vectors fix its output.

**Storage.** One `access_keys` row per account at most:
`auth_hash` is the SHA-256 of `auth`, with `created_at` and
`last_used_at`. A slow password hash is not used: it protects
secrets people choose, which have few possibilities, and this one has
2^256. A copy of the database does not let anyone find `auth`, let
alone the key.

**Who makes it.** The page, never the server. In first-run setup, at
registration and on a claim link, the page shows the key once, with
"Copy the key" and "Download as a text file", and the warning that
anyone with it can sign in as the person, that nobody (the server
included) can show it again, and that it belongs in a password
manager. The person types its last group again before the account is
made, so a key that was never kept never makes an account. The
account page makes a new one the same way.

**Signing in.** The person types their username and the key on the
sign-in page (or the app's authorization page). The server applies
the limits of section 14, looks up the account, and compares the
SHA-256 of `auth` with the stored hash in constant time; a username
that does not exist, or an account with no key, is compared with a
dummy hash, so the work is the same. Any failure answers "That
username and access key do not match." The account must be local,
claimed and not removed, and its role must be allowed to sign in by
itself under the policy (section 8), as for a passkey. On success the
session is made as for a passkey (section 5.3 step 5) with
`signed_in_by = 'access_key'`, and `last_used_at` is set.

**On the account page.** The page says whether the account has an
access key, when it was made and last used, never the key. "Make a
new access key" needs a fresh check: the current access key or a
passkey with user verification (or an authenticator code). The old
key stops working the moment the new one is stored. "Remove" needs a
fresh check and is refused unless the account has a passkey or a
linked company identity: an account always keeps a way in. Adding a
passkey works as in section 5.4; an account's first passkey brings
its recovery codes.

**The setting.** `auth.allow_access_keys` ("Allow accounts without a
passkey", on by default). Off, no account gets a first access key:
setup, registration and claiming refuse it and the pages stop offering
it, and an account without one cannot make one. Access keys that
exist keep working, and their accounts can replace them. The admin
console says so beside the switch, and its people list and overview
show which accounts have no passkey.

**What it gives up.** An access key is typed, so it can be typed into
the wrong page. A passkey answers only for this server's host name;
an access key is accepted by whatever page asks for it, and a
look-alike site can collect it and sign in as the person. That
phishing resistance is lost, plainly, and is the price of working
without a passkey. A stolen key signs in until the person makes a new
one. It is also only one factor: an access key sign-in is complete,
as a passkey's is, and is never asked for a second step, while a
passkey counts as two. An admin who wants everyone on passkeys turns
the setting off and asks the people listed without one to add a
passkey and remove their key.

**Sealed chats.** An access key does not unlock sealed history: it is
not a way of section 13.3 of `SEALED.md`. An account with only an
access key unlocks sealed chats with its recovery phrase or from
another of its devices, as an account whose passkeys have no PRF does
(`SEALED.md` section 13.5), and the lock's copy for the device
(section 10.6) brings a locked device back when the access key
unlocks the session.

## 6. Recovery

Recovery is for an account that signs in by itself and has lost its
way in. It leads to one thing only: a new passkey, or a new access
key.

### 6.1 Recovery codes

- **Issued** in a set of eight when an account's first passkey is
  created, and shown once. An account that has only an access key has
  none. A signed-in person can replace the whole
  set from the account page after a fresh check; the old set is
  deleted.
- **Format.** 16 characters from the 32-character alphabet
  `0123456789ABCDEFGHJKMNPQRSTVWXYZ` (Crockford base32), shown in
  four groups: `7K2M-9QXD-04TV-HB3N`. Each character comes from five
  random bits, so a code holds 80 bits.
- **Hashing.** The typed code is upper-cased, has spaces and hyphens
  removed, and has `O` read as `0` and `I` or `L` read as `1`. The
  SHA-256 hash of the resulting 16 ASCII characters is stored in
  `recovery_codes.code_hash`. With 80 random bits a plain hash is
  enough: a copy of the database does not let anyone search for the
  code in useful time.
- **Single use.** Using a code sets `used_at` with an update that only
  succeeds while `used_at` is null. Used rows are kept, so the account
  page can show "5 of 8 left".

Signing in with one:

1. The person enters their username and a code.
2. The server applies the rate limits (section 14), looks up the
   account, checks that its role may use passkeys under the policy,
   hashes the code and marks the matching row used. Any failure gives
   the same answer after the same work: a missing account is checked
   against a dummy hash.
3. The server inserts a **recovery session**: a `sessions` row of kind
   `web` with `signed_in_by = 'recovery_code'` and `expires_at` one
   hour ahead, never extended. It writes `recovery.code_used` to the
   audit log.
4. A recovery session can do these things: read the account's own
   profile, list and remove its passkeys, list and revoke its
   sessions, create a passkey, and make a new access key (section
   5.6; it replaces any key the account had). Every other operation
   answers `forbidden`.
5. When the passkey or access key is made, the server revokes the
   recovery session and inserts an ordinary one with `signed_in_by =
   'passkey'` or `'access_key'`, in the same transaction as the new
   row. The person is then shown their remaining code count and asked
   to remove passkeys they no longer have.

So every full session begins with a passkey ceremony, an access key
or a company sign-in. A recovery code is one factor; the passkey it leads to
restores the second.

### 6.2 Recovery links

A recovery link is the last resort. It is a row in `recovery_links`:
`token_hash`, `account_id`, `created_by`, `expires_at`, `used_at`.

| Made by                    | For                         | Valid for |
|----------------------------|-----------------------------|-----------|
| An admin, in the console   | A member or guest           | 24 hours  |
| The owner, in the console  | An admin, member or guest   | 24 hours  |
| `churr recovery <username>` on the server | Any local account that is a person, the owner included | 1 hour |

An admin cannot make a link for another admin or for the owner, so
the link cannot be used to climb to a higher role. The command line
needs access to the data directory, which already means full control.
Making a link revokes any earlier unused link for that account and is
written to the audit log (`actor_kind = 'cli'` and `created_by` null
for the command). Links are not made for bots, remote accounts or
unclaimed accounts.

Using one:

1. The person opens the link and presses the button on the page.
2. The server hashes the value, and marks the row used with an update
   that only succeeds while `used_at` is null and `expires_at` is in
   the future.
3. It inserts a recovery session as in section 6.1, with
   `signed_in_by = 'recovery_link'`, and writes `recovery.link_used`.
4. What follows depends on how the account's role signs in:
   - if it may use passkeys, the person creates one or makes a new
     access key, as in section 6.1 step 5. A new access key from a
     recovery session is allowed when `auth.allow_access_keys` is on
     or the account already has one;
   - if it must use company sign-in, the only thing the session can do
     is link a company identity (section 7.5). This is how a member
     who never linked one gets back in after the policy changes to
     `sso`.

A recovery link lets its holder become the account. An admin who makes
one and opens it themselves takes over that member's account,
including channels the admin could not otherwise read. Churr does not
prevent this, because the operator holds the database anyway
(`DESIGN.md` section 4). It makes it visible: both the making and the
use are in the audit log, the account's other sessions receive an
account event when a recovery session starts, and the new session is
listed on the account page with how it signed in. A takeover does not
open sealed chats: without one of the person's passkeys, their phrase
or one of their devices, sealed history stays locked, and the other
person in each sealed chat is told the account's devices were reset
(`SEALED.md` section 4.4).

## 7. Company sign-in

An admin adds one or more providers. Each is a row in `sso_providers`:
`kind` (`oidc` or `saml`), `name` (the text on the sign-in button),
`config` (below), `secret` (sealed), `group_claim`, `admin_group`,
`enabled`. Adding, changing, disabling and removing a provider needs a
fresh check and is written to the audit log without the secret.

Company sign-in is offered only when `auth.member_sign_in` is `either`
or `sso`. The server contacts a provider only once one is configured,
which is the "identity provider" row of the outbound connections table.

### 7.1 What both protocols share

**The waiting request.** Starting a sign-in creates an entry in memory,
kept ten minutes and usable once:

| Field           | Purpose                                             |
|-----------------|-----------------------------------------------------|
| Request handle  | 256 random bits. The OIDC `state`, or the SAML `RelayState`. |
| Provider        | The `sso_providers.id` the request was made for.    |
| Browser binding | The SHA-256 hash of a second 256-bit random value that is set in a cookie on the browser that started the request. |
| Protocol values | OIDC: `nonce` and the PKCE verifier. SAML: the `AuthnRequest` ID. |
| Purpose         | Sign in; sign in with an invite code; link an identity to the signed-in or recovering account; or continue an app authorization (section 11). |

**The binding cookie.** `churr_sso`, HttpOnly, Secure, ten minutes,
with its path limited to the provider callback. For OIDC it is
`SameSite=Lax`, because the provider sends the browser back with a
top-level GET. For SAML it is `SameSite=None`, because the provider
sends the browser back with a cross-site POST, which carries no Lax
cookie. The callback refuses an answer whose cookie does not hash to
the stored binding. Without this, an attacker could finish a sign-in
with their own identity and feed the answer to someone else's browser,
signing the victim in as the attacker.

**After the answer is validated** (sections 7.2 and 7.3), with a
provider and a subject in hand:

1. **Ban check.** If `bans` has a row for this `provider_id` and
   `subject` with `lifted_at` null, stop.
2. **Find the account.** Look up `identities` by `(provider_id,
   subject)`.
   - Found: the account must be local, claimed and not removed.
   - Not found, and the purpose is linking: section 7.5.
   - Not found otherwise: section 7.4.
3. **Groups and role.** Section 7.6.
4. **Update** `identities.last_used_at`.
5. **Second factor.** Section 9 decides whether the session starts
   complete or pending.
6. **Session.** Insert a `sessions` row of kind `web`, `signed_in_by`
   `oidc` or `saml`, `expires_at` 24 hours ahead and never extended
   (section 10.1).
7. **Reply with a page, not a redirect.** The session cookie is
   `SameSite=Strict`, and a browser does not send a Strict cookie along
   a redirect chain that began at another site. So the callback answers
   with a small page from Churr's own origin that then loads the next
   page itself.

A failed validation is written to the server log with the reason and
shown to the person as "sign-in through <name> failed". The details
are not shown, and nothing is written to the audit log, so an outsider
cannot fill it.

### 7.2 OIDC

Authorization code flow with PKCE, with Churr as a confidential client.

`config` holds: `issuer`, `client_id`, extra scopes, the claim names
for username, display name and groups (defaults `preferred_username`,
`name`, `groups`), and the `acr` values that count as multi-factor.
`secret` holds the client secret.

**Discovery.** When the provider is added the server fetches
`<issuer>/.well-known/openid-configuration` over HTTPS. The `issuer`
in the document must equal the configured one exactly. The server
keeps the authorization endpoint, token endpoint, key-set address and
supported signing algorithms. It fetches the document again every 24
hours, and the key set again when a token names a key it does not
have, at most once a minute. All endpoints must be HTTPS. An issuer
that serves many tenants under one name is not supported.

**Starting.**

1. Create the waiting request with a `state`, a `nonce` and a PKCE
   verifier, each 256 random bits, and set the binding cookie.
2. Send the browser to the authorization endpoint with
   `response_type=code`, `client_id`, the provider's fixed callback
   address on this server as `redirect_uri`, `scope=openid profile`
   plus the configured extra scopes, `state`, `nonce`,
   `code_challenge` (the base64url SHA-256 of the verifier) and
   `code_challenge_method=S256`. If `acr` values are configured and a
   second factor is required, they are sent as `acr_values`. The
   `email` scope is added only while unclaimed imported accounts exist
   (section 7.4).

**The callback.**

1. Take the waiting request by `state`. Unknown, expired or already
   used: stop. Check the binding cookie and delete it.
2. If the answer carries `error`, stop. If it carries `iss`, it must
   equal the configured issuer.
3. Exchange the code at the token endpoint with the verifier, the same
   `redirect_uri`, and the client secret by the method the provider
   advertises (HTTP Basic preferred).
4. Validate the ID token. Every check must pass:
   - it is a signed JWT whose `alg` is one of RS256, PS256, ES256 or
     EdDSA and is among the provider's advertised algorithms. `none`
     and the HMAC algorithms are refused;
   - the signature verifies under the key from the provider's key set
     named by `kid`;
   - `iss` equals the configured issuer exactly;
   - `aud` contains `client_id`, and if it names more than one
     audience, `azp` equals `client_id`;
   - `exp` is in the future and `iat` is not in the future, each with
     60 seconds of allowance for clock difference;
   - `nonce` equals the one in the waiting request;
   - `sub` is present and at most 255 characters.
5. Read the claims from the ID token only. The UserInfo endpoint is
   not called.

| Claim                 | Used for                                       |
|-----------------------|------------------------------------------------|
| `sub`                 | The subject. Stored in `identities.subject`.   |
| `preferred_username`  | The suggested username when an account is created. |
| `name`                | The display name when an account is created.   |
| The group claim       | Section 7.6.                                   |
| `amr`, `acr`          | The provider's statement about multi-factor (section 9.2). |
| `email`, `email_verified` | Claiming an imported account (section 7.4). Compared, then dropped. Never stored or logged. |

Access and refresh tokens from the provider are discarded as soon as
the ID token has been read. Churr holds no standing access to the
provider.

### 7.3 SAML 2.0

SAML has a history of signature-handling flaws in every language
(`DESIGN.md` section 21). Churr therefore supports the narrowest
useful profile, uses a maintained library for XML signature
verification, and repeats in its own code every check below that the
library does not make itself.

`config` holds: the provider's entity ID, its single sign-on address
(HTTP-Redirect binding), its signing certificates, the attribute names
for the subject (optional), username, display name, email and groups,
and the authentication-context values that count as multi-factor. The
admin pastes the provider's metadata or gives an address the server
fetches once. Metadata is not refreshed: when the provider changes its
certificate, the admin loads the metadata again. Certificates are
trusted because the admin put them there; their expiry dates and
issuers are not checked. `secret` holds Churr's own signing key, used
to sign requests when the provider wants that.

**Only sign-in started from Churr.** The server sends an
`AuthnRequest` by HTTP-Redirect and takes the `Response` by HTTP-POST
at the provider's fixed callback on this server. A response nobody
asked for is refused. Single logout, artifact binding and encrypted
assertions are not supported.

**Starting.** Create the waiting request, set the binding cookie, and
send an `AuthnRequest` with a random ID of 160 bits or more, the
callback as `AssertionConsumerServiceURL`, the provider's address as
`Destination`, Churr's entity ID as `Issuer`, a persistent `NameID`
format requested, and the request handle as `RelayState`.

**Validating the response.** In this order. Any failure refuses the
whole response.

1. **Size and parsing.** The decoded response is at most 256 KB. It is
   parsed with document type declarations and entities refused.
2. **Asked for.** `RelayState` names a waiting request for this
   provider, and the binding cookie matches. The request is taken out
   of memory here, so it cannot be answered twice.
3. **Response envelope.** `Status` is `Success`. `Destination` equals
   the callback address. `InResponseTo` equals the stored request ID.
   `Issuer`, if present, equals the provider's entity ID.
4. **One assertion.** The response has exactly one `Assertion`
   element, as a direct child of `Response`, and no
   `EncryptedAssertion`. No two elements in the document share an ID.
5. **Signature over the assertion.** The `Assertion` element itself
   carries a `Signature` that is its direct child. That signature has
   exactly one `Reference`, whose URI is `#` followed by that
   assertion's ID, with only the enveloped-signature and exclusive
   canonicalisation transforms. It verifies under one of the
   provider's configured certificates. The key in the document's own
   `KeyInfo` is never trusted. Signature algorithms accepted: RSA or
   ECDSA with SHA-256, SHA-384 or SHA-512. Digests: SHA-256 or
   stronger. SHA-1 is refused. A signature over the `Response` alone
   is not enough.
6. **Use what was signed.** Every value read from here on is read
   from the canonical bytes the signature covered, parsed afresh, and
   not from the original document. This is what defeats signature
   wrapping: an attacker cannot have the signature checked on one
   assertion and the values read from another.
7. **Assertion issuer.** `Issuer` equals the provider's entity ID.
8. **Subject confirmation.** There is a `SubjectConfirmation` with
   method `bearer` whose data has `Recipient` equal to the callback
   address, `InResponseTo` equal to the stored request ID, and
   `NotOnOrAfter` in the future.
9. **Conditions.** `NotBefore`, if present, is not in the future and
   `NotOnOrAfter` is in the future. An `AudienceRestriction` is
   present and names Churr's entity ID.
10. **Clock allowance.** Every time comparison in steps 8 and 9 allows
    60 seconds either way, and no more.
11. **Once only.** The assertion's ID has not been seen. It is then
    remembered until its `NotOnOrAfter` plus the allowance.
12. **Authentication statement.** An `AuthnStatement` is present. If
    it has `SessionNotOnOrAfter` earlier than 24 hours ahead, the
    Churr session ends then.

**The subject** is the text of `NameID`, or of the configured subject
attribute when one is set. Text is taken as the whole character
content of the element with comments ignored, so a comment placed
inside a name cannot cut it short. A transient `NameID` is refused,
because it changes at every sign-in. If the `NameID` format is an
email address, the subject that Churr stores is an email address; the
admin console warns about this when the provider is added and
suggests a persistent format or a subject attribute instead.

Attributes are used as the OIDC claims are: username and display name
when an account is created, groups for section 7.6, email only for
section 7.4.

**Tests.** The test suite keeps a set of hostile responses and proves
each is refused: unsigned; signed response with unsigned assertion;
signature removed; a second assertion wrapped before, after and
inside the signed one; duplicate IDs; a reference to another element;
a comment inside the `NameID`; a document type declaration; wrong
audience, recipient, destination and `InResponseTo`; expired and
not-yet-valid; a replay of a good response.

### 7.4 First sign-in: creating or claiming an account

When the subject matches no `identities` row:

1. **Imported account.** If `import_claims` has rows and the provider
   supplied an email address it vouches for (OIDC: `email_verified` is
   true; SAML: the configured email attribute), the server computes
   the keyed hash of the lower-cased address and looks for a match.
   The hash is HMAC-SHA-256 under a key derived from the key file, so
   a copy of the database alone cannot be tested against a list of
   addresses. On a match the imported account is claimed: `claimed_at`
   set, an `identities` row inserted, the `import_claims` row deleted.
   The address is then dropped.
2. **New account.** Otherwise registration must allow it: `open`, or
   `invite` with a usable invite code carried in the waiting request.
   The person confirms or chooses a username on a page (the
   provider's suggestion, lower-cased and cut to the username rule, is
   filled in if it is free). The server then inserts `accounts` (role
   from the invite, or `member`), `identities` (`provider_id`,
   `subject`, `account_id`, `linked_at`), the invite's and
   `@everyone`'s memberships as in section 4.3, and writes
   `account.registered` to the audit log with the provider's name.
3. **Otherwise** the person is told that this server is not taking
   new accounts.

No `webauthn_handle`, passkey or recovery code is made for an account
created this way. It gets them only if it later adds a passkey.

### 7.5 Linking an identity to an existing account

A signed-in person with a fresh check, or a person in a recovery
session from a link (section 6.2), can start a company sign-in with
the purpose "link". When the validated subject is not already in
`identities`, a row is inserted for the current account and
`identity.linked` is written to the audit log. A subject already
linked to another account is refused. An account may hold several
identities; a subject belongs to one account.

Before an admin changes the policy to `sso`, the console shows how
many members and guests have no linked identity. Those people cannot
sign in afterwards until they link one, which they do while the policy
is still `either`, or later through a recovery link.

### 7.6 Groups and the admin role from the provider

At every company sign-in the server reads the provider's group names:
the claim or attribute named by `sso_providers.group_claim`, taken as
a list of strings (a single string is a list of one). Names are
compared exactly.

- **Churr groups.** For each `groups` row with this `provider_id` and
  a `provider_group`, the account is added to or removed from
  `group_members` so that it is in the group exactly when the
  provider lists that name. Such a group takes its members only this
  way.
- **The admin role.** If `sso_providers.admin_group` is set, the
  provider decides the admin role for accounts that sign in through
  it: listed means `admin`, not listed means `member`. The change is
  applied at sign-in and written to the audit log. The owner's role is
  never changed, and a guest is never promoted.
- If the group claim is absent from an answer, nothing is changed and
  a warning is logged. An absent claim is treated as a
  misconfiguration, not as "in no groups", so one bad answer cannot
  demote every admin.

Membership is brought up to date only when the person signs in. With
24-hour sessions that is at most a day late for someone who keeps
using Churr. A person removed at the provider stays in their groups,
and keeps the admin role, until an admin changes it by hand: Churr
has no automatic removal (`BRIEF.md`, "Not in version 1").

## 8. The sign-in policy

`auth.member_sign_in` decides how accounts with role `member` and
`guest` get in. Guests follow the same rule as members.

| Value     | Members and guests                | Owner and admins        |
|-----------|-----------------------------------|-------------------------|
| `passkey` | Passkey or access key. Company sign-in is not offered to anyone. | Passkey or access key |
| `either`  | Passkey, access key or company sign-in | Passkey, access key or company sign-in |
| `sso`     | Company sign-in only. They cannot register with a passkey or access key, sign in with one, or use a recovery code. | Passkey, access key or company sign-in |

- The owner and admins can always use a passkey or their access key,
  so a broken or misconfigured provider cannot lock the server. The check is made on
  the account's role at the moment of sign-in.
- The console will not let the policy be set to `sso` while no enabled
  provider exists, and asks the admin making the change to have a
  passkey or an access key.
- Under `sso`, removing someone at the provider stops their next
  sign-in, and their sessions end within 24 hours. Under `either`
  that is not so: a member who has a passkey keeps getting in after
  the provider has dropped them, until an admin removes the account.
  The same holds for an admin under any policy.
- Changing the policy does not end existing sessions. An admin who
  wants the new rule to bite at once uses "sign everyone out", which
  revokes every `web` and `app` session but their own.

## 9. The second factor

### 9.1 When it is asked for

`auth.second_factor` is `off` or `required`. When it is `required`:

| Sign-in             | Second step                                      |
|---------------------|--------------------------------------------------|
| Passkey             | None. It already counts; `sessions.second_factor` stays null. |
| Access key          | None. It is one factor, and is not asked for a second (section 5.6). |
| Recovery code or link | None as such. The recovery session can only lead to a new passkey or a company sign-in, and those are subject to this table. |
| OIDC or SAML        | One of the accepted proofs below, before a session exists. |

### 9.2 Accepted proofs

The admin ticks which are accepted (`auth.second_factor_accepted`).
At least one must be ticked while a second factor is required.

| Proof      | What counts                                    | Recorded as |
|------------|------------------------------------------------|-------------|
| `provider` | The provider's own statement that it used more than one factor. OIDC: the ID token's `amr` list contains `mfa`, or its `acr` equals one of the values in the provider's `config`. SAML: the assertion's `AuthnContextClassRef` equals one of the configured values, or a configured attribute holds a configured value. | `provider` |
| `passkey`  | A WebAuthn assertion by one of the account's own passkeys, with user verification, against a fresh challenge. | `passkey` |
| `totp`     | A current code from the account's confirmed authenticator app (section 9.4). | `totp` |

The provider's statement is read from the same validated ID token or
signed assertion as the subject. A request for `acr_values` is only a
request, so the answer is always checked. If the provider's statement
is accepted and present, the session starts complete. If it is absent,
the person is asked for one of the other ticked proofs; if no other is
ticked, the sign-in is refused with an explanation for the admin.

### 9.3 The pending state

1. After section 7.1 step 4, when a second step is still owed, the
   server inserts the `sessions` row with `pending_second_factor =
   true`, `second_factor` null and `expires_at` ten minutes ahead, and
   sets the cookie.
2. A pending session can do only these things: read which proofs it
   may give; answer a passkey challenge limited to the account's own
   credentials; submit a TOTP code; enrol a first proof (below); and
   sign out. Every other operation, the live connection included,
   answers 401 `second_factor_required`.
3. **Enrolment on first sign-in.** If the account has no passkey and
   no confirmed TOTP, the pending session may set one up: create a
   passkey (section 5.2) if `passkey` is ticked, or enrol TOTP
   (section 9.4) if `totp` is ticked. Finishing the enrolment counts
   as the proof. This trusts the first company sign-in: whoever
   passes the provider first chooses the second factor.
4. On a good proof the server, in one transaction, sets
   `pending_second_factor = false`, sets `second_factor`, sets
   `expires_at` to 24 hours after `created_at`, and replaces
   `token_hash` with the hash of a new token, which it sends in a new
   cookie. The pending token stops working.
5. Five wrong proofs end the pending session. The person starts again
   at the provider.

App sign-in never sees a pending session: the app receives tokens only
after the browser holds a complete one (section 11).

### 9.4 TOTP

- **Algorithm.** RFC 6238 with HMAC-SHA-1, six digits and a 30-second
  step. This is the one combination every authenticator app accepts.
- **Secret.** 160 random bits, sealed in `totp.secret` (section 1).
  Shown once at enrolment as a QR code drawn by the server's own page
  and as base32 text, in the form
  `otpauth://totp/<server name>:<username>?secret=<base32>&issuer=<server name>&algorithm=SHA1&digits=6&period=30`.
- **Enrolment.** Beginning inserts or replaces the `totp` row with
  `confirmed_at` null. Submitting a valid code sets `confirmed_at` and
  `last_step`. An unconfirmed row is never accepted as a proof and is
  deleted after an hour.
- **Checking a code.** The server computes the codes for the current
  step and the one before and after it, and compares in constant
  time. A match is accepted only if its step is greater than
  `totp.last_step`, and `last_step` is then set to it with an update
  that only succeeds while it is smaller. A code can therefore be
  used once, and a code seen over someone's shoulder is dead once its
  owner has used it or a later one.
- **Limits.** Five wrong codes in 15 minutes per account, then
  refusals until the window passes (section 14).
- **Removing or replacing** TOTP needs a fresh check, and is refused
  when it would leave an account that needs a second factor without
  any. An admin can reset a member's TOTP (the row is deleted, the
  action is audited); the member enrols again at their next sign-in,
  on the same trust as a first enrolment.
- There are no TOTP backup codes. A lost authenticator app is handled
  by another proof or by an admin reset.

## 10. Sessions

Everything that authenticates a request is a row in `sessions`, except
webhook secrets and integration tokens, which have their own tables
(section 12).

### 10.1 Kinds and lifetimes

| `kind`     | Credential                      | Made by                    | Ends                          |
|------------|---------------------------------|----------------------------|-------------------------------|
| `web`      | `churr_ses_` in the cookie      | A sign-in in a browser     | See below                     |
| `app`      | `churr_app_` access token, `churr_ref_` refresh token | Section 11 | See below and section 11.3 |
| `personal` | `churr_pat_`                    | The person, section 12.1   | When revoked, or at the expiry the person chose |
| `bot`      | `churr_bot_`                    | An admin, section 12.2     | When revoked                  |

How long a `web` or `app` session lasts depends on how the person
proved who they are:

| Signed in by            | Lifetime                                        |
|-------------------------|-------------------------------------------------|
| Passkey or access key   | 30 days, sliding: each use moves `expires_at` to 30 days ahead. To spare the database, the row is written at most once an hour. |
| OIDC or SAML            | 24 hours from the sign-in, fixed. Use does not extend it. An earlier `SessionNotOnOrAfter` from SAML shortens it. |
| Recovery code or link   | One hour, fixed, and limited as section 6 describes. |
| Pending a second factor | Ten minutes, fixed.                             |

The 24 hours stand in for automatic account removal (`BRIEF.md`, open
decisions): a person dropped by the provider is out within a day.

The two lengths are the settings `sessions.passkey_seconds` and
`sessions.sso_seconds` of `OPERATIONS.md`; 30 days and 24 hours are
their defaults.

`sessions` records, per row: `account_id`, `kind`, `token_hash`,
`refresh_hash`, `prev_refresh_hash`, `rotated_at` and
`access_expires_at` (apps, section 11.3), `label` (a browser and
system name worked out from the first request, or the device or token
name), `signed_in_by`, `second_factor`, `pending_second_factor`,
`fresh_at` (section 10.4), `created_at`, `last_seen_at` (written at
most once a minute), `expires_at`, `max_expires_at`, `revoked_at`.

`max_expires_at` is a hard end that use never moves. It is null for a
passkey session and set to 24 hours after sign-in (or the earlier
SAML time) for a company one, and `expires_at` never passes it.

A session is valid when `revoked_at` is null, `expires_at` is null or
in the future, and its account is local, claimed and not removed. The
role and the removal mark are read from `accounts` on every request,
so a demotion or removal takes effect at once.

### 10.2 The cookie

    Set-Cookie: churr_session=churr_ses_…; Path=/; HttpOnly; Secure;
                SameSite=Strict; Max-Age=<seconds until expires_at>

- No `Domain` attribute, so the cookie belongs to the exact host.
- `Secure` is always set, except on a local trial at
  `http://localhost`.
- Scripts cannot read it. The web chat authenticates its WebSocket
  with the cookie too, never with a token in the address.
- A new sign-in always gets a new row and a new token. A token is
  never reused across a change of state: the pending token is replaced
  when the second factor is given, and a recovery session is replaced
  when its passkey is made.

### 10.3 Cross-site request forgery

The defence is an origin check, with `SameSite=Strict` as a second
layer.

- A request that carries the cookie and uses any method but `GET` or
  `HEAD` must have an `Origin` header equal to the server's own
  origin. If the header is missing, `Sec-Fetch-Site` must be
  `same-origin`. If both are missing, the request is refused.
- The WebSocket upgrade is a `GET`, so it gets the same check
  explicitly: with a cookie, its `Origin` must be the server's own.
- `GET` and `HEAD` never change anything.
- Requests authenticated by a bearer token skip the check: a browser
  does not attach those by itself.
- The SAML callback is the one cross-site POST the server accepts. It
  carries no session cookie, changes nothing until the signed
  assertion has been validated, and is bound to the browser by the
  `churr_sso` cookie (section 7.1).

The server's own origin is fixed by its configured host name. It is
never taken from the request's `Host` header.

### 10.4 The fresh check

Some actions are worth more to a thief than a session is: adding or
removing a passkey, replacing recovery codes, removing TOTP, creating
a personal token, linking a company identity, and for admins changing
sign-in settings or providers, making recovery links and creating bot
or integration tokens.

These need the session to be freshly checked: within the last ten
minutes it was created by a sign-in, or its holder passed a passkey
assertion, gave the account's access key, or gave a TOTP code. An
account with none of these gets the mark by signing in again through
its provider. The access key check is limited to five wrong keys per
account in fifteen minutes, and is not accepted as the second factor
of a pending session. The time of
the last check is `sessions.fresh_at`, set at sign-in and at each
check. A session whose `fresh_at` is older than ten minutes gets 403
`fresh_check_required`; the client runs the check and tries again.

### 10.5 Listing and revoking

- A person can list all their sessions of every kind: label, kind,
  how it signed in, when it was made and last seen, and which one is
  making the request. Token hashes are never returned.
- A person can revoke any of their own sessions. Signing out revokes
  the current one and clears the cookie. Revoking sets `revoked_at`;
  the live connections of that session are closed at once.
- Revoking an `app` or `web` session also retires its row in
  `devices` (`retired_at`), which for sealed chats means the device
  must be taken out of the account's device list by a device that
  remains (`SEALED.md` section 7.3). A browser that signs out wipes
  its own keys first.
- An admin can revoke all sessions and tokens of an account ("sign
  out everywhere"), which is audited. An admin cannot see a member's
  session list.
- The janitor deletes `sessions` rows 30 days after they expired or
  were revoked.

### 10.6 The lock

A person at a shared or unattended computer or phone can lock Churr
there (ruling R75) instead of signing out, which would throw away the
device's sealed-chat set-up. The lock is real, not only a screen: it
is a state of the **session**, kept in `sessions.locked_at` (migration
0008).

- **Who.** A `web` or `app` session locks itself (`lockSession`). A
  pending or recovery session cannot; a personal or bot token cannot
  (it has no screen). Each session locks on its own: the person's
  other browsers, apps and tokens are untouched. Every tab of one
  browser shares its cookie session, so they lock together.
- **While locked** the session is answered only by
  `beginSessionUnlock` and `logout`, and an app may still refresh its
  tokens (the refreshed session stays locked). Every other operation,
  the live connection's upgrade and the wake-up fetch included,
  answers 401 `session_locked`. Locking closes the session's live
  connections with 4002 (`EVENTS.md` section 6). To an operation that
  also works signed out (`getServer`, the authorization page), a
  locked session is nobody, as a pending one is. The calls server
  drops a locked session from any call at its next sweep; the clients
  leave the call as they lock.
- **Unlocking** is the passkey check of section 5.3 under its own
  ceremony kind, so a check cannot be spent as an unlock nor the
  other way round: `beginSessionUnlock` builds request options limited
  to the account's own passkeys with user verification required, and
  `finishSessionUnlock` has the library verify the challenge, origin,
  relying-party ID hash, flags and signature, refuses a response
  without the user-verified flag, refuses a counter that went
  backwards (`passkey.clone_suspected`), records the passkey's use,
  clears `locked_at` and marks the session freshly checked (section
  10.4). Nothing weaker unlocks: no TOTP, no recovery code, no
  company sign-in.
- **With the access key.** `beginSessionUnlock` says whether the
  account has an access key, and leaves out the passkey options when
  it has no passkey. `finishSessionUnlockWithAccessKey` takes the
  ceremony and the value derived from the key (section 5.6), compares
  it in constant time, and unlocks as the passkey does. A wrong key
  leaves the ceremony in place, so the person can correct it, and
  counts in the same limit as wrong assertions. An account with
  neither a passkey nor an access key cannot unlock; it signs out and
  in again. Where an account has both, the lock screen offers the
  passkey first and "Use your access key instead" under it; a browser
  without passkeys is offered the key alone.
- **In the web chat** the lock screen runs the assertion itself, with
  the PRF input of `SEALED.md` section 13.3, so the same touch can
  unlock sealed chats (below).
- **In the apps** the assertion runs on the server's own page
  (`/locked/#ceremony=…`), opened in the system browser, because a
  passkey works only at the server's own web address (ruling R43).
  The page needs no session: the ceremony names the locked session,
  and only a user-verified assertion by one of that account's passkeys,
  or its access key, answers it, which is why `getSessionUnlockOptions`,
  `finishSessionUnlock` and `finishSessionUnlockWithAccessKey` take no
  credential. The access key is typed on that page, so the app never
  handles it. The identifier travels
  after the `#`, which a browser never sends to a server or in a
  Referer. The app asks the server until the session answers again.
- **Limits.** Five wrong assertions or access keys for one account in
  fifteen minutes, then `beginSessionUnlock` and both finishing
  operations answer `rate_limited` until the window has passed
  (section 14). The count
  is per account, so many locked sessions do not multiply it.
- **Sealed chats.** Before the session is locked the client leaves on
  the server a copy of the backup key sealed to its own box key (kind
  `device`, `SEALED.md` section 13.3) if there is none for the current
  key version, and then forgets the backup key and account key in
  memory and in its store: the device is a locked device. The server
  gives the copy back only to an unlocked session, and the box key
  opens it, so unlocking the session unlocks sealed chats in the same
  touch, PRF or not. If the copy cannot be found, the web chat uses
  the same touch's PRF output; failing both, the sealed chats ask as
  they always do. An access key unlock has no PRF output, so the copy
  is what brings sealed chats back. Nothing sealed is lost by locking.
- **Signed out while locked.** A locked session that is revoked or
  ends meanwhile is simply signed out: its next request answers
  `unauthenticated`, and the client shows the sign-in page.
- **What it does not protect.** What the device keeps at rest (the
  app's cache database, the browser's encrypted sealed-chat store and
  its non-extractable device keys) stays on the device; the lock keeps
  it from being shown or brought up to date, and keeps the server from
  answering, until a passkey or the access key unlocks the session. Someone who can run
  code as the person on that device is out of its reach.

## 11. The apps

The apps never handle a passkey, an access key or a provider. They send the person to
the server's own pages in the system browser and receive tokens, by
OAuth 2.0 authorization code with PKCE, exactly as Codemail's apps do.
An app therefore gets every sign-in method, the sign-in policy and the
second factor without implementing any of them.

### 11.1 Clients, redirects and metadata

- **Public clients, no registration.** There is no client secret.
  `client_id` is a label (`churr-app`), not a credential, and the
  server gives it no trust.
- **Where a code may be sent.** Because clients are unregistered, the
  redirect address is what stops a web page from collecting codes.
  Exactly these forms are allowed, and no fragment in any of them:
  - a loopback address for desktop apps: `http://127.0.0.1:<port>/…`
    or `http://[::1]:<port>/…`, any port. The name `localhost` is not
    accepted;
  - the apps' own private address scheme for phones. Its exact name is
    fixed together with the apps' identifiers (`DESIGN.md` section 21,
    open questions).
  Nothing else is accepted, and in particular no `https` address.
- **PKCE with S256 is required.** A request without a
  `code_challenge` of at least 43 characters, or with any method but
  `S256`, is refused. PKCE is what protects the code if another app
  on a phone registers the same address scheme.
- **Metadata.** The server publishes the authorization server
  metadata document of RFC 8414 at its well-known address, so an app
  needs only the server's address. It states: `issuer` (the server's
  origin); the authorization, token and revocation endpoints;
  `response_types_supported: ["code"]`; `grant_types_supported:
  ["authorization_code", "refresh_token",
  "urn:churr:params:oauth:grant-type:pairing-code"]`;
  `code_challenge_methods_supported: ["S256"]`;
  `token_endpoint_auth_methods_supported: ["none"]`; and
  `authorization_response_iss_parameter_supported: true`.

### 11.2 Signing an app in

1. The person types the server's address into the app. The app
   fetches the metadata.
2. The app makes a PKCE verifier (256 random bits) and a `state`, and
   opens the authorization page in the system browser with
   `response_type=code`, `client_id`, `redirect_uri`,
   `code_challenge`, `code_challenge_method=S256`, `state`, a device
   name and a platform. It may add its box public key, for unlocking
   sealed chats (`SEALED.md` section 13.3).
3. The page checks the redirect form and the PKCE values before
   anything else, and shows an error itself if they are wrong. It
   never redirects to an address it has not accepted.
4. If the browser has no complete session, the person signs in there
   by whatever the policy allows, second factor included.
5. The page shows "Sign in <device name> (<platform>) as <username>?"
   The person approves. The approval is a same-origin POST with the
   session cookie, so section 10.3 applies.
6. The server inserts an `oauth_codes` row: `code_hash`, `account_id`,
   `kind = 'authorization'`, `client_name` (the device name, cut to 60
   characters), `redirect_uri`, `pkce_challenge`, `platform`,
   `box_key` if the app sent one, `handover` if the page made one
   (below), and `expires_at` two minutes ahead. It sends the browser
   to the redirect address with `code`, `state` and `iss`.
7. If the browser session was created in step 4, for this
   authorization only, it is revoked now. Signing an app in does not
   leave a browser signed in behind it.
8. The app checks `state` and `iss` and posts to the token endpoint:
   `grant_type=authorization_code`, `code`, `code_verifier`,
   `redirect_uri`.
9. The server finds the row by the code's hash and sets `used_at`
   with an update that only succeeds while it is null and unexpired.
   The code is spent even if the next checks fail. It then checks
   that the base64url SHA-256 of the verifier equals `pkce_challenge`
   and that `redirect_uri` is the same string.
10. It inserts a `sessions` row of kind `app` (`token_hash`,
    `refresh_hash`, `label` the device name, `signed_in_by` copied
    from the browser session that approved) and a `devices` row
    (`session_id`, `name`, `platform`), writes `device.added` to the
    audit log, and returns the access token, the refresh token,
    `token_type: "Bearer"`, `expires_in: 3600`, and the `handover`
    if there is one.

**The hand-over, and what it trusts.** When the person signs in on
that page with a passkey that can unlock sealed chats, the page opens
the account's backup key and seals it to the app's `box_key`. The
server stores the result in `oauth_codes.handover` and cannot open
it. But the page that did the opening is one the server supplied. A
passkey works only on the server's own web address, so unlocking with
a passkey passes through a server-supplied page for the apps exactly
as for the web chat, and a dishonest or compromised server could
capture the key at that moment. Unlocking an app with the recovery
phrase, or from another app, does not pass through any server page.

### 11.3 Access tokens, refresh tokens and rotation

- An access token works for one hour, until `access_expires_at`.
- A refresh token is used once. Presenting it returns a new access
  token and a new refresh token, and both hashes in the `sessions`
  row are replaced in one update that only succeeds while
  `refresh_hash` still holds the old value.
- **Reuse.** The row keeps the hash of the refresh token it last
  replaced in `prev_refresh_hash`, and when in `rotated_at`. If that
  one is presented again within 60 seconds, the
  server treats it as a retry after a lost answer and rotates again.
  If it is presented later than that, somebody is using a token that
  was already spent: the session is revoked and `session.reuse_revoked`
  is audited. An unknown refresh token is simply refused.
- **How long the app stays signed in** follows section 10.1. From a
  passkey sign-in, each refresh moves `expires_at` 30 days ahead, so
  an app in use stays signed in. From a company sign-in the session
  has a hard end (`max_expires_at`) 24 hours after that sign-in;
  when it is reached the
  refresh is refused and the app sends the person through the browser
  again.
- **Revocation** (RFC 7009). Either token identifies the session.
  The answer is always success.
- Token endpoint errors use the OAuth error shape (`invalid_grant`,
  `invalid_request`, `unsupported_grant_type`,
  `authorization_pending`), not the API's error shape, because OAuth
  libraries expect it.

### 11.4 QR pairing

The second way to sign an app in, for a phone beside a browser that
is already signed in.

1. On the web account page the person chooses "Add a device". This
   needs a fresh check. The server inserts an `oauth_codes` row with
   `kind = 'pairing'`, the person's `account_id` held back as null
   until they confirm, and `expires_at` two minutes ahead. The page
   shows a QR code holding the server's address and the one-time
   code, and remembers which code it is waiting on.
2. The app scans it and posts to the token endpoint with the pairing
   grant type, the code, a device name, a platform and its box public
   key. The server stores them in `client_name`, `platform` and
   `box_key`, and answers `authorization_pending`. The app asks again
   every two seconds.
3. The page shows "A device called <name> (<platform>) wants to sign
   in" and the person confirms. The server sets `account_id` on the
   row. If the page is an unlocked web chat it also stores a
   `handover` (`SEALED.md` section 13.7).
4. The app's next request finds the row confirmed. The server sets
   `used_at` and issues tokens as in section 11.2 step 10, with
   `signed_in_by = 'pairing'`. The new session takes the confirming
   web session's `max_expires_at`, so a device paired from a company
   session ends when that session would.

The code in the QR is a bearer secret for two minutes. Someone who
photographs the screen can post first, but they are then the device
whose name the person is asked to confirm, and the person's own phone
never gets tokens. The confirmation step is the defence, which is why
it cannot be skipped. The QR code also carries a secret that the app
never sends to the server. The hand-over is locked under it as well
as to the app's box key, so one scan can sign the app in and unlock
its sealed chats (`SEALED.md` section 13.7).

## 12. Tokens for scripts, bots, webhooks and integrations

### 12.1 Personal tokens

A token a person makes for their own scripts. It acts as them.

- Accounts with role `owner`, `admin` or `member` may make them.
  Guests and bots may not.
- Making one needs a fresh check. The person gives it a name and an
  expiry (30 days, 90 days, a year, or none; 90 days is offered
  first). The server inserts a `sessions` row with `kind =
  'personal'`, `label` the name, `signed_in_by` null, and returns the
  `churr_pat_` token once.
- It can do what the person can do through the API, with these
  exceptions: nothing that needs a fresh check, nothing to do with
  passkeys, recovery, TOTP, identities, sessions or other tokens, and
  no sealed chats (a script holds no device keys).
- It is listed and revoked like any session. Creation and revocation
  are audited.
- **When company sign-in is required for the account's role**, its
  personal tokens stop working 30 days after the account's last
  company sign-in (its newest `identities.last_used_at`), and work
  again once the person has signed in. Without this, a long-lived
  token would outlast the person's removal at the provider
  indefinitely. With it, the gap is at most 30 days, and an admin
  who needs it closed sooner removes the account.

### 12.2 Bot tokens

- An admin creates a bot and its tokens, after a fresh check. Each
  token is a `sessions` row for the bot's account with `kind = 'bot'`,
  a label, and no expiry. The `churr_bot_` token is shown once.
- A bot has no other way in: no passkey, no recovery, no company
  identity, no OAuth.
- A bot may hold several tokens, so one can be replaced without
  downtime. Each is revoked separately. Creation and revocation are
  audited.

### 12.3 Webhook secrets

- A webhook's secret is the `churr_hook_` value in its address. Its
  hash is in `webhooks.token_hash`. No `sessions` row is made.
- The secret is made when the requester collects the address of the
  approved webhook, shown to them once, and never again. Until then
  `token_hash` is null and the webhook accepts nothing. A requester
  who loses the address asks for the webhook to be reset, which makes
  a new secret.
- A second, separate secret is made at the same moment and shown with
  the address: the signing secret, 256 random bits, sealed in
  `webhooks.signing_secret`. A sender that signs its requests, as
  GitHub does with `X-Hub-Signature-256`, is given it, and the server
  checks the signature with it. It must be stored in a form the
  server can use, which is why it is sealed and not hashed. The
  `churr_hook_` value is never used for signing.
- The address authorises one thing: posting into the webhook's one
  named channel. It reads nothing.
- Because the secret is in the path, the server never writes webhook
  paths to its log. A reverse proxy in front of it may, and
  `OPERATIONS.md` should tell operators to turn that off for webhook
  addresses.
- Revoking sets `webhooks.state = 'revoked'` and clears `token_hash`
  and `signing_secret`.

### 12.4 Integration tokens

- An admin creates a server integration, after a fresh check, and
  receives its `churr_int_` token once. Its hash is in
  `integrations.token_hash`, and what it may do in
  `integrations.allows`.
- The token works only on the integration operations it was ticked
  for. It is not an account and opens no live connection.
- Every use is written to the audit log with `actor_kind =
  'integration'`. Revoking sets `revoked_at`.

### 12.5 Checking a token

For every request: read the prefix; refuse a prefix that does not
belong where the token was presented (section 1); hash the whole
string; look the hash up in the one column that prefix maps to; check
the row is live and, for `sessions`, that its `kind` matches the
prefix. Lookup is by hash, so it leaks nothing through timing about
tokens that almost match.

## 13. Removal and bans

**Removing an account** (`DESIGN.md` section 4) sets
`accounts.removed_at` and `removed_by` and, in the same transaction,
revokes every `sessions` row, deletes the account's `passkeys`,
`access_keys`, `recovery_codes`, `recovery_links`, `totp` and
`identities` rows,
revokes webhooks it stands behind, and removes it from every
conversation. Its live connections are closed. Its messages stay.

**A ban** is a removal plus rows in `bans` that stop the same person
coming back:

- one row with the `account_id`, which keeps the reason and who
  banned;
- one row per identity the account had, copying `provider_id` and
  `subject` before the `identities` rows are deleted.

A banned subject is refused at step 1 of section 7.1, before any
account is found or made, at every provider sign-in. Lifting a ban
sets `lifted_at`; it does not restore the account.

A ban can only recognise a person by a company identity. Someone who
registered with a passkey and is banned can register again with a new
invite, or freely if registration is `open`. For a server that needs
bans to hold, the answer is `invite` registration or `sso`.

Bans, lifts and refused sign-ins by a banned subject are audited.

## 14. Rate limits

Every step that can be reached without being signed in is limited.
Limits are counted in memory, per address unless stated, and answer
429 `rate_limited` with `retry_after_seconds`. The address is the
connection's, or, only when the server was started with
`-behind-proxy`, the last address the proxy added to its forwarding
header. These numbers are starting values to be tuned under test.

| Step                                         | Limit                              |
|----------------------------------------------|------------------------------------|
| Server information and well-known documents  | 60 a minute                        |
| Setup link: checking the value               | 5 failures an hour                 |
| Invite or claim link: checking a code        | 10 failures an hour                |
| Registration: beginning a passkey ceremony   | 10 an hour                         |
| Registration: accounts created               | 5 a day, when registration is `open` (by company sign-in, or with an access key and no invite) |
| Passkey sign-in: begin and finish            | 30 in 5 minutes                    |
| Access key sign-in                           | 10 failures an hour per address, and 30 failures an hour per account |
| Access key as a fresh check                  | 5 failures in 15 minutes per account |
| Recovery code                                | 5 failures an hour per address, and 10 failures a day per username |
| Recovery link: pressing the button           | 10 an hour                         |
| Company sign-in: starting                    | 20 in 5 minutes                    |
| Company sign-in: callbacks                   | 30 in 5 minutes, and a 256 KB body limit |
| Second factor in a pending session           | 5 failures, then the session ends  |
| TOTP codes                                   | 5 failures in 15 minutes per account |
| Unlocking a locked session                   | 5 failures in 15 minutes per account, passkey and access key together (section 10.6) |
| OAuth authorization page                     | 30 in 5 minutes                    |
| OAuth token endpoint                         | 30 a minute; a pairing code may be polled once every 2 seconds |
| A bearer token or cookie that matches nothing | 30 in 5 minutes                   |
| A webhook address that matches nothing       | 30 a minute                        |

- The per-username recovery limit lets an outsider block one person's
  recovery codes for a day by guessing wrongly. It cannot block their
  passkey or a recovery link, so the cost is small and the limit
  stays.
- The per-account access key limit does let an outsider who tries
  wrong keys from several addresses hold one person's access key
  sign-in for up to an hour; for an account with nothing else, that is
  an hour without signing in on a new device (its signed-in devices
  are not touched). Guessing a 256-bit key gains nothing, so the limit
  is there to keep the work bounded, and the first refusal is written
  to the audit log so an admin sees it.
- Apart from the case above, no limit locks an account. Limits slow an
  address or one method down; they never stop the rightful person
  using another method.
- Counters are lost on restart. That is accepted: guessing a 256-bit
  token or an 80-bit code is not helped by a restart.

Limits on signed-in actions (posting, uploads) are in `CONVENTIONS.md`
and the documents for those features.

## 15. What is written to the audit log

`audit_log` rows have `actor_id`, `actor_kind`, `action`,
`target_kind`, `target_id` and `detail`. No row ever holds a token, a
code, a secret, a public key or an email address.

| `action`                     | Written when                              | `detail`                    |
|------------------------------|-------------------------------------------|-----------------------------|
| `owner.created`              | Setup finishes                            |                             |
| `account.registered`         | An account is made                        | How (passkey, access key, provider name), invite id |
| `account.claimed`            | An imported account is claimed            | By link or by provider, and how |
| `account.removed`, `account.banned`, `ban.lifted` | Section 13           | Reason                      |
| `ban.sign_in_refused`        | A banned subject tried to sign in         | Provider name               |
| `account.role_changed`       | By hand or by the provider's group        | Old and new role, cause     |
| `invite.created`, `invite.revoked` |                                     | Role, uses, expiry          |
| `passkey.added`, `passkey.removed` | Any account changes its passkeys    | Passkey name                |
| `passkey.clone_suspected`    | A signature counter went backwards        | Passkey name                |
| `access_key.created`, `access_key.replaced`, `access_key.removed` | An account makes, replaces or removes its access key | |
| `access_key.limit_reached`   | Wrong access keys reached the per-account limit (written once per window) | |
| `recovery.codes_replaced`    | A person makes a new set                  |                             |
| `recovery.code_used`         | A recovery session starts from a code     | Codes left                  |
| `recovery.link_created`, `recovery.link_used` |                          | For whom                    |
| `identity.linked`            | Section 7.5                               | Provider name               |
| `totp.enrolled`, `totp.removed`, `totp.reset` |                          | By whom                     |
| `sso_provider.added`, `.changed`, `.removed` |                           | Names of the fields changed |
| `setting.changed`            | Any setting of section 2                  | Old and new value           |
| `sessions.revoked_all`       | An admin signs an account or everyone out | Target                      |
| `session.reuse_revoked`      | A spent refresh token came back           | Device name                 |
| `device.added`               | An app is signed in or paired             | Device name, platform       |
| `token.created`, `token.revoked` | Personal and bot tokens               | Kind, label                 |
| `integration.created`, `.revoked`, `.used` |                             | Name, what was done         |
| `webhook.approved`, `.declined`, `.revoked`, `.reset` |                  | Conversation                |

Not written: ordinary successful sign-ins and sign-outs (a person sees
their own in the session list), and failed attempts by anyone not
signed in, which go to the server log with the address and the
reason. The audit log is something admins read, so nothing an
outsider can trigger at will is allowed to fill it.

## 16. Threats

| Threat                                              | What stops it                                             |
|-----------------------------------------------------|-----------------------------------------------------------|
| Phishing: a look-alike site asks the person to sign in | A passkey answers only for the relying-party ID it was made for, and the server accepts one origin. An access key does not: it can be typed into the wrong page and used there. Accounts without a passkey give up phishing resistance (section 5.6), and the admin console lists them. |
| A stolen access key                                 | Not stopped: the username and the key sign in. The person makes a new key, which kills the old one at once; the key's use shows in `last_used_at` and the session list. |
| Guessing an access key                              | 256 random bits; limits per address and per account. |
| A stolen database copy                              | Tokens, codes and link values are stored as SHA-256 hashes of values with at least 80 random bits. An access key is stored as the SHA-256 of a value derived from 256 random bits. Passkeys are public keys. TOTP and provider secrets are sealed under the key file, which is not in the database. Invite codes are readable in it: an unused invite is usable by whoever has the copy. |
| A stolen database copy together with the key file   | Nothing. TOTP secrets and provider secrets are exposed and must be replaced. Passkeys and hashed tokens still are not. |
| Guessing a recovery code                            | 80 random bits, single use, rate limits per address and per username. |
| A stolen recovery code                              | Not stopped: a code plus the username lets its holder add a passkey. It is single use, audited, announced to the account's other sessions, and the person can replace the set. |
| A stolen session cookie                             | HttpOnly and Secure keep it from scripts and plain HTTP. It cannot add a passkey, make a token or change sign-in settings without a fresh check. The person can see and revoke the session. It still reads and posts as the person until it expires or is revoked. |
| Cross-site request forgery                          | The origin check on every changing request and on the WebSocket; `SameSite=Strict`. |
| Signing a victim's browser in as the attacker       | Company sign-in answers are bound to the browser that started them by the `churr_sso` cookie. |
| Script injection into Churr's pages                 | A strict content security policy, no third-party scripts, and messages drawn without parsing HTML (`DESIGN.md` section 17). |
| A forged or altered OIDC ID token                   | Signature against the issuer's published keys, with `none` and HMAC refused, and exact `iss`, `aud`, `exp` and `nonce` checks. |
| An OIDC code stolen or injected in the browser      | `state`, the browser binding, PKCE and `nonce`.            |
| A forged, unsigned or wrapped SAML assertion        | The assertion's own signature is required and verified under configured certificates only; values are read from the signed bytes; one assertion per response. |
| A replayed SAML response                            | `InResponseTo` must match a request that is taken out of memory on first use; assertion IDs are remembered; validity windows are short. |
| A SAML response made for another service or sent to the wrong place | Audience, recipient and destination checks. |
| An unasked-for (provider-initiated) SAML response   | Refused: there is no waiting request.                      |
| A compromised identity provider                     | Not stopped for members: the provider can sign in as any of its subjects, state any group, and assert multi-factor. Limited by: the owner is never changed by a provider, and with `passkey` or `totp` as the only ticked proofs the provider alone cannot finish a sign-in for an account that has one enrolled. |
| A provider user sets their email to a colleague's to claim an imported account | Only an address the provider marks verified is used. Beyond that the provider is trusted. |
| The provider is down or misconfigured               | The owner and admins sign in with passkeys under every policy. |
| A person removed at the provider                    | Under `sso`: no new sign-in, sessions end within 24 hours, personal tokens within 30 days. Under `either`, or for admins with passkeys: not stopped until an admin removes the account. |
| A second factor skipped by going around the main door | Every full session starts from a passkey ceremony, an access key, or a company sign-in that passed section 9. Recovery leads only to those. Apps receive tokens only from a complete browser session. An access key is one factor; where that is not enough, the admin turns off `auth.allow_access_keys`. |
| A TOTP code reused or observed                      | `totp.last_step` refuses any step already used.            |
| Guessing TOTP codes                                 | Five failures per 15 minutes per account; a pending session ends after five. |
| A copied hardware authenticator                     | A signature counter that goes backwards refuses the sign-in and is audited. Synced passkeys have no counter and no such check. |
| A malicious page collecting OAuth codes             | Only loopback and the apps' own scheme are accepted as redirects; the authorization page never redirects to an unaccepted address. |
| Another phone app claiming the apps' address scheme | PKCE: the code is useless without the verifier.            |
| A stolen refresh token                              | Rotation on every use. The thief and the app each spend the other's token, and a spent token coming back after 60 seconds revokes the session. |
| A photographed pairing QR code                      | The person must confirm the device by name on the signed-in page; two-minute life; single use. |
| An admin taking over a member's account with a recovery link | Not stopped. Audited, announced to the member's sessions, and unable to open sealed chats. |
| An admin taking over another admin or the owner     | Admins cannot make recovery links for admins or the owner, and only the owner changes roles. |
| A leaked personal, bot, webhook or integration token | The prefix lets scanners and people recognise it. Each is revoked singly. A webhook secret posts to one channel and reads nothing; an integration token does only what was ticked. |
| Someone reads the server's first-run output          | Not stopped until setup finishes. The link dies when the owner is made and changes at every start. |
| The host name changes, or is taken over by someone else | Passkeys are bound to the host name. A new holder of the name with a valid certificate could run their own server there and collect passkey assertions for it, but cannot use them against the old data without the data directory. |
| A banned person returns                             | Stopped only where a company identity recognises them (section 13). |
| A server that serves altered sign-in pages          | Not stopped. Sign-in itself is the server's to give, so nothing is lost there. What is lost is sealed history: unlocking it with a passkey happens on a page the server supplies, in the apps as well as the web chat, and an altered page can capture the backup key at that moment. Unlocking an app with the recovery phrase or from another app does not pass through a server page. |
| The operator                                        | Not stopped. Whoever holds the data directory can read everything unsealed, make a recovery link for anyone, and change any setting. `SEALED.md` covers what the operator still cannot read. |

## 17. What this uses in the schema and the API

**Schema.** Everything this document stores is in `schema.sql`:
`accounts`, `passkeys`, `access_keys` (migration 0010), `sso_providers`, `identities`, `totp`,
`recovery_codes`, `recovery_links`, `sessions`, `oauth_codes`,
`devices`, `invites`, `import_claims`, `bans`, `integrations`,
`webhooks`, `settings` and `audit_log`, with the columns named in the
sections above. Nothing further is needed.

Deliberately not in the schema: passkey ceremonies, waiting company
sign-in requests and used SAML assertion IDs, which are in memory
(section 1). If Churr ever ran as more than one process they would
need tables.

**API operations.** Begin and finish a passkey creation; begin and
finish a passkey sign-in; make the owner or an account with an access
key, sign in with one, give it as a fresh check, read, replace and
remove one's own, and unlock a session with it; a passkey check and a TOTP check for a
signed-in or pending session (second factor, fresh check, unlocking);
sign in with a recovery code; open a recovery link; start a company
sign-in for a provider and for a purpose (sign in, sign in with an
invite, link an identity); the two provider callbacks; list the
proofs a pending session may give; begin and confirm TOTP enrolment,
and remove it; list, rename and remove passkeys; replace recovery
codes; list and revoke sessions; sign out; lock the session, and begin,
fetch the options of and finish unlocking it (section 10.6); list identities; create,
list and revoke personal tokens; the OAuth authorization page and its
approval, the token endpoint with its three grants, revocation, and
the metadata document; create a pairing, read its state, confirm it;
and for admins: read and set the settings of section 2, manage
providers, manage invites, make recovery links, reset a member's
TOTP, sign an account or everyone out, manage bans, and create and
revoke bot tokens, integrations and webhooks. `openapi.yaml` is the
reference for their paths and shapes; where it and this document
disagree about what an operation must check, this document is right
(`CONVENTIONS.md` has the error codes, `fresh_check_required` among
them).

## 18. Choices this document made

Each fills a gap in the design with the simplest safe thing.

1. One-time values in links have no prefix; invite codes are 128
   bits and stored readable.
2. Sign-in state in flight is held in memory. The fresh-check time
   is in `sessions.fresh_at`.
3. The setup link lasts across restarts until it is used. A lost one
   is replaced with `churr setup-link`, not by restarting.
4. Recovery codes are 16 base32 characters (80 bits), not Codemail's
   eight.
5. A recovery code or link gives a one-hour session that can only add
   a passkey (or, under `sso`, link an identity).
6. Admins cannot make recovery links for admins or the owner. Admin
   links last 24 hours; command-line links one hour.
7. Members who may invite make single-use member invites with no
   groups. Claim links last 30 days and can be made again.
8. Guests follow the member sign-in policy.
9. Under `passkey`, company sign-in is offered to nobody.
10. Under `sso`, members' recovery codes are refused; a recovery link
    leads to linking an identity.
11. Company accounts get their username from the provider's
    suggestion, confirmed by the person.
12. Claims come from the ID token only; the `email` scope is asked
    for only while imported accounts wait to be claimed; the import
    email hash is keyed.
13. SAML: no encrypted assertions, no single logout, metadata loaded
    once, 60 seconds of clock allowance, transient `NameID` refused.
14. With `admin_group` set, the provider decides the admin role at
    each sign-in. An absent group claim changes nothing.
15. The provider's multi-factor statement is `amr` containing `mfa`,
    or a configured `acr`, context class or attribute value.
16. A pending session lasts ten minutes and its token is replaced on
    success.
17. TOTP is SHA-1, six digits, 30 seconds, one step either side, no
    backup codes.
18. Passkey sessions slide with no upper limit. Company sessions are
    fixed.
19. The fresh check, its ten minutes, and the list of actions that
    need it.
20. A browser session made only to approve an app is revoked once the
    code is issued.
21. Loopback redirects by address only, not `localhost`.
22. A spent refresh token is tolerated for 60 seconds, then revokes.
23. Personal tokens: not for guests, cannot manage credentials or
    read sealed chats, and stop 30 days after the last company
    sign-in where that is required.
24. A signature counter going backwards refuses the sign-in.
25. Ordinary sign-ins and anonymous failures are not in the audit
    log.
26. The rate-limit numbers in section 14.
27. Access keys (ruling R80): made in the page, never seen by the
    server; 35 bytes in 48 base-58 characters; HKDF for the sent
    value and a plain SHA-256 of it stored; no recovery codes with an
    access key; one factor, never asked for a second; not a way of
    unlocking sealed chats; removable only while a passkey or a
    company identity remains; existing keys keep working and can be
    replaced when the setting is off.

## 19. To verify

Facts this document depends on and could not confirm.

- **PRF support.** Checked on 13 November 2026: confirmed for Apple's
  and Google's passkey stores; partial for Firefox; unconfirmed for
  Windows Hello, hardware security keys and some third-party passkey
  managers. Test each before the account page promises anything.
- Whether `go-webauthn` passes the `prf` extension input through on
  creation and on sign-in without changes to the library, and whether
  it leaves unknown client extension results alone.
- How `go-webauthn` reports a signature counter that went backwards
  (a warning flag on the credential, or an error), and whether it
  rejects a change in the backup-eligible flag.
- Whether a passkey sign-in started from the system browser sheet the
  apps open on iPhone and Android returns PRF results the same way a
  normal browser tab does.
- Which Go SAML library to use, and for each numbered check in
  section 7.3 whether the library makes it. Candidates are
  `crewjam/saml` and `russellhaering/gosaml2` with `goxmldsig`. Both
  have had signature-handling advisories; read them before choosing.
- How the common providers state multi-factor: which put `mfa` in
  `amr`, which need an `acr` value, and for SAML which use the
  context class and which an attribute. Entra ID, Okta, Google
  Workspace and Keycloak should each be tried.
- That `SameSite=None` cookies set by Churr survive the SAML round
  trip in current Safari, Firefox and Chrome with their tracking
  protections on.
- That every authenticator app in common use accepts the `otpauth`
  address in section 9.4.
- The private address scheme the phone apps register, once the apps'
  identifiers are chosen.
