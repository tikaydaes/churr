# Sealed chats

A sealed chat is a conversation between exactly two accounts,
encrypted end to end from its first message. There is one per pair of
people, nobody can be added, and the other person may be on a linked
server. The server stores and delivers ciphertext it cannot read.

Nothing here is built yet. This document is the authority on sealed
chats. `DESIGN.md` section 14 is its outline; `MESSAGE-FORMAT.md`
section 9 gives the message shape; `AUTH.md` covers passkeys and
sessions. Tables are those of `schema.sql`. Operations are named in
words; their paths are in `openapi.yaml`.

**Read this first.** The scheme here is Churr's own. It is built from
five standard primitives, but the way they are put together has not
been published or analysed by anyone outside. Section 21 lists what
must be reviewed before anyone relies on a sealed chat for something
that matters, section 20 lists what is not promised, and section 22
lists facts about libraries and platforms that still need checking.

The document is meant to be exact enough to implement three times
and get identical bytes. Where it is not, that is a fault in the
document.

## 1. What a sealed chat has and lacks

| Has                                             | Lacks                         |
|-------------------------------------------------|-------------------------------|
| Text in Churr Markdown, files, reactions, replies, edits, deletes, checklist ticks, calls | Cards, forms, link previews |
| History on a newly added device, through the key backup | Search, export        |
| A code two people can compare                   | Bots, webhooks, a third person |

The server sees who is in the chat, which account and device posted,
when, and how large each item is. Section 16 has the full list.

## 2. Building blocks

### 2.1 The idea

A sealed chat is always two accounts with a handful of devices each.
For that, one idea is enough: **make a fresh secret and encrypt it
separately to every device that should have it.** A secret made this
way is a key epoch. A new one is made whenever the set of devices
changes, and at least weekly while the chat is in use. Messages are
encrypted under keys derived from the current epoch's secret.

Everything else in this document is bookkeeping around that idea:
how devices are listed and by whom, how a new device gets old
secrets, and what the server is allowed to decide.

### 2.2 Primitives

These five, and no others.

| Primitive     | Use                                    | Reference   |
|---------------|----------------------------------------|-------------|
| X25519        | Key agreement, for sealing to a device or an unlock key | RFC 7748 |
| Ed25519       | Signatures                             | RFC 8032    |
| HKDF-SHA-256  | Every key derivation                   | RFC 5869    |
| AES-256-GCM   | Every encryption (call frames alone use AES-128-GCM, section 10). 12-byte nonce, 16-byte tag appended | NIST SP 800-38D |
| SHA-256       | Hashes                                 | FIPS 180-4  |

All five exist in Dart, in Web Crypto in every current browser
engine, in Apple's CryptoKit and in Go's standard library. So the
apps, the web chat, the iPhone notification extension and the
server's test code each use what their platform already has, with no
second toolchain, no native library to build for five platforms and
no compiled module to ship to browsers (section 18).

Rules that hold for every construction below:

- `HKDF(ikm, salt, info, n)` is HKDF-SHA-256 extract-then-expand with
  those inputs and `n` bytes of output.
- Every label is ASCII, begins `churr ` and contains no zero byte.
  No value made under one label is accepted under another.
- `||` joins bytes. `0x00` is one zero byte.
- Random values come from the operating system's generator; in the
  browser from `crypto.getRandomValues` or Web Crypto key generation.
- An X25519 result that is 32 zero bytes is refused.
- An X25519 public key is computed from a private key as
  `X25519(private, 9)`, the function applied to the base point.
- In JSON, bytes are base64url without padding.

### 2.3 The record encoding

Every record that is signed or hashed is written in one binary
encoding with exactly one valid form. JSON was the alternative. A
"canonical" JSON needs three implementations to agree on key order,
number formatting, string escaping and Unicode handling, and each of
those has caught other projects out. A fixed binary layout has
nothing to agree on.

| Type        | Bytes                                                  |
|-------------|--------------------------------------------------------|
| `u8`, `u32`, `u64` | 1, 4 or 8 bytes, unsigned, big-endian           |
| `bytes(n)`  | Exactly `n` bytes                                      |
| `uuid`      | The identifier's 16 bytes                              |
| `time`      | `u64` milliseconds since the Unix epoch                |
| `blob`      | `u32` length, then that many bytes                     |
| `text`      | A `blob` holding UTF-8. Compared byte for byte; never normalised. |
| `list of T` | `u32` count, then that many `T`                        |

- A record is its fields in the order given, with nothing between
  them and nothing after the last.
- A reader refuses a record that is too short, has bytes left over,
  holds a `u8` choice outside its listed values, or holds text that
  is not valid UTF-8.
- A list that stands for a set is sorted by the stated key, compared
  as bytes, ascending, with no key twice. A reader refuses any other
  order.
- No record is longer than 64 KB.
- The content *inside* a sealed message is the JSON of
  `MESSAGE-FORMAT.md` section 9. It is encrypted and signed as bytes
  by its one author and only ever parsed by others, so it needs no
  canonical form.

### 2.4 Signatures and hashes

- A signature is Ed25519 over `label || 0x00 || record bytes`, with
  the label this document gives for that kind of record.
- A hash of a record is `SHA-256(label || 0x00 || record bytes)`.
- A signature is only ever verified, never compared with another
  signature. Not every platform signs deterministically.
- A verifier checks the signature over the bytes it received, then
  parses those same bytes.

### 2.5 Seal: encrypting to a public key

`seal(R, label, context, plaintext)`, where `R` is the recipient's
32-byte X25519 public key:

1. Make a fresh X25519 key pair: private `e`, public `E`. It is used
   for this one seal and then discarded.
2. `shared = X25519(e, R)`.
3. `okm = HKDF(ikm = shared, salt = E || R, info = label || 0x00 ||
   context, n = 44)`. The first 32 bytes are the key, the last 12 the
   nonce.
4. `ct = AES-256-GCM(key, nonce, plaintext, associated data = label
   || 0x00 || context)`.
5. The result is `E || ct`: 32 bytes, then the ciphertext with its
   tag. Sealing 32 bytes gives 80.

To open with private key `r`: split off `E`, compute `shared =
X25519(r, E)`, derive the same key and nonce with `R = X25519(r, 9)`,
and decrypt. Any failure refuses the whole value.

A seal says nothing about who made it. Wherever that matters, a
signature covers the sealed bytes.

### 2.6 Lock: encrypting under a secret key

`lock(K, label, context, plaintext)`, where `K` is 32 secret bytes:

1. `salt` = 16 random bytes.
2. `okm = HKDF(ikm = K, salt = salt, info = label || 0x00 || context,
   n = 44)`: a 32-byte key and a 12-byte nonce.
3. `ct = AES-256-GCM(key, nonce, plaintext, associated data = label
   || 0x00 || context)`.
4. The result is `salt || ct`.

Every use of AES-GCM in this document gets a key that is used for
exactly one encryption: a seal's key comes from a one-time key pair,
and a lock's key from a fresh random salt. So the usual limits on how
much one AES-GCM key may encrypt, and the disaster of a repeated
nonce, do not arise as long as either the random generator works or,
for messages, the sender's counter has not been wound back
(section 8.1).

## 3. The keys

| Key                | What it is                    | Made                         | Held by                         |
|--------------------|-------------------------------|------------------------------|---------------------------------|
| Device signing key | Ed25519 pair                  | On the device, before it unlocks | That device only. Never leaves it, never backed up. |
| Device box key     | X25519 pair. Epoch secrets and the backup key are sealed to it. | With the signing key | That device only. |
| Account key        | Ed25519 pair. Signs the account's device list. | With the backup key | Every unlocked device of the account. The private half is stored inside the backup. |
| Backup key         | 32 random bytes per account.  | On the device that first joins a sealed chat | Every unlocked device. On the server only in locked copies. |
| Unlock keys        | X25519 pairs, one per passkey and one for the phrase. | Derived when needed, never stored | Whoever holds the passkey or the phrase. The public halves are on the server and in the backup. |
| Epoch secret       | 32 random bytes per sealed chat per key epoch. | By the device that makes the epoch | The devices it was sealed to. |
| History secret     | 32 bytes, derived from the epoch secret (section 5.2). | By each device | Devices of the epoch; and the backups of both accounts. |
| File key           | 32 random bytes per file.     | By the sending device        | Inside the message that carries the file. |
| Call secret        | Per call, per key epoch, derived from the epoch secret. | By each device | Devices of the epoch, in memory, for the call only. |

A device is **unlocked** when it holds the account's backup key. Only
an unlocked device is in the device list, and only a listed device is
given epoch secrets. A device that is signed in but locked shows the
sealed chats' names and an "Unlock" button, and nothing else.

## 4. Devices and the signed device list

### 4.1 Devices

A device is one installed app, or one browser profile, signed in to
one account. It is a row in `devices`. Its Ed25519 public key is in
`devices.sign_key` and its X25519 public key in `devices.box_key`.

A device outlives its session. A session from company sign-in ends
after 24 hours (`AUTH.md` section 10.1), and the device must not lose
its place in every sealed chat each day. When the same app or browser
signs in to the same account again, it proves it holds the signing
key by signing a challenge from the server
(`"churr device attach v1" || 0x00 || challenge`), and the server
attaches the existing `devices` row to the new session
(`devices.session_id`, which is empty in between). A device with no
session for 30 days is retired (`devices.retired_at`), and its
owner's other devices remove it from the list (section 7.3). If a
different account signs in on that app or browser, the old keys are
wiped first.

### 4.2 The account key

Each account has one Ed25519 key pair that signs its device list. It
is made together with the backup key (section 13.1), and its private
half is kept in the backup, so every unlocked device can sign with it
and the server never can.

Without it, a new device could only be listed if an older device were
at hand to sign for it, and a browser, which is a new device after
every sign-out, would be useless on its own. With it, any way of
unlocking (passkey, phrase, or another device) also lets the new
device list itself. The cost is that the backup key becomes the root
of the account's identity as well as of its history (section 20).

The account key has a version number, shared with the backup key
(`key_version`). Both are replaced together (section 13.6).

### 4.3 The list

A device list is one record (section 2.3):

| Field         | Type        | Meaning                                  |
|---------------|-------------|------------------------------------------|
| `format`      | `u8`        | 1                                        |
| `account_id`  | `uuid`      |                                          |
| `server`      | `text`      | The account's home server name           |
| `version`     | `u64`       | Starts at 1, goes up by exactly one      |
| `prev`        | `bytes(32)` | Hash of the previous version's list; 32 zero bytes in version 1 |
| `reset`       | `u8`        | 0 or 1                                   |
| `key_version` | `u32`       |                                          |
| `account_key` | `bytes(32)` | Ed25519 public key                       |
| `devices`     | list of device | Sorted by `device_id`; at most 50     |
| `at`          | `time`      |                                          |

Each device in it:

| Field       | Type        | Meaning                                    |
|-------------|-------------|--------------------------------------------|
| `device_id` | `uuid`      |                                            |
| `sign_key`  | `bytes(32)` | Ed25519 public key                         |
| `box_key`   | `bytes(32)` | X25519 public key                          |
| `platform`  | `u8`        | 1 android, 2 ios, 3 macos, 4 windows, 5 linux, 6 web |
| `added_at`  | `time`      |                                            |
| `proof`     | `bytes(64)` | See below                                  |

- A device's name is not in the list: the other person sees what
  kind of device it is, not what its owner called it. Platform 6 is a
  browser, and is labelled as one wherever the list is shown.
- `proof` is the device's own signature, with the label
  `churr device proof v1`, over `account_id || device_id || sign_key
  || box_key`. It shows the device holds its signing key and belongs
  to this account, and it stays valid when the account key changes.
- The hash of a list uses the label `churr device list hash v1`.

What is stored (`device_lists.signed_list`) and sent is:

| Field         | Type        | Meaning                                  |
|---------------|-------------|------------------------------------------|
| `list`        | `blob`      | The list record                          |
| `sig`         | `bytes(64)` | By the list's own `account_key`, label `churr device list v1`, over the list record |
| `has_prev_sig`| `u8`        | 0 or 1                                   |
| `prev_sig`    | `bytes(64)` | Present only if `has_prev_sig` is 1. By the previous version's account key, label `churr device list rollover v1`, over the list record |

`has_prev_sig` is 1 exactly when `account_key` differs from the
previous version's and the list is not a reset.

### 4.4 Checking a list, and what people are shown

Every client keeps, for its own account and for each person it has a
sealed chat with, the last list it accepted. This is its **pin**.

**First contact.** With no pin, the client takes the newest list the
server gives, checks `sig` under the list's own `account_key` and
every `proof`, and pins it. Nothing proves that list is really the
other person's. This is trust on first use, and comparing codes
(section 15) is what closes it.

**Later.** The client fetches every version after its pin and checks
each step from one version to the next:

1. `account_id` and `server` are unchanged, `version` is one higher,
   and `prev` is the hash of the previous version's list.
2. Every `proof` verifies, and no device identifier or key repeats.
3. One of these holds:
   - **Ordinary change.** `account_key` and `key_version` are
     unchanged, `reset` is 0, `sig` verifies, and there is no
     `prev_sig` (an ordinary change carrying a rollover signature is
     refused).
   - **Key change.** `key_version` is one higher, `reset` is 0, `sig`
     verifies under the new key and `prev_sig` under the old one.
   - **Reset.** `reset` is 1, `key_version` is one higher and `sig`
     verifies under the new key. It carries no `prev_sig`. Nothing
     connects it to the old key.
4. Anything else is refused: the client keeps its pin, shows "The
   server sent a device list that does not check out", and makes no
   epoch because of it.

A version lower than the pin does not move the pin. It is accepted
as history only if it chains into what the device already holds
(each older version steps by the rules above into the one above it,
which a device needs to read old epochs); otherwise it is refused. A
client cannot tell when
the server is holding a newer version back (section 16).

**What is shown.** Notices are drawn by the client in the sealed chat
with that person. They are not messages and the server does not know
they were shown.

| Change            | Notice                                              | Effect |
|-------------------|-----------------------------------------------------|--------|
| Device added      | "Sam added a device: Android app." or "…: a browser." | None |
| Device removed    | "Sam removed a device."                             | None   |
| Key change        | "Sam's security key changed. Their old key signed the change." | The comparison code changes. A verified mark stays. |
| Reset             | "Sam's sealed-chat identity was reset. Messages from now on go to new devices. If you did not expect this, compare codes before you send anything private." | A verified mark is removed. If there was one, sending is held until the person acknowledges the notice. |

Each device shows the same notices for changes to its **own**
account's list, worded as "A device was added to your account". That
is how a person learns of a device they did not add.

A reset is what an account does when no way of unlocking is left
(section 13.8). It is also the only thing a server or an account
thief can do to get a device of theirs listed without the backup key,
so the notice is the same in both cases and is deliberately loud.

### 4.5 What the server does with lists

The server stores every version in `device_lists`, keyed by
`account_id` and `version`, and serves any range of them, at most 500
versions in one answer. It serves them to the account itself, to the
other person in each of its sealed chats, and to anyone who may start
a sealed chat with it. It accepts
a new version only from a session of that account, only when its
`version` is exactly one above the stored newest (compare and set),
and only when every device in it is a live `devices` row of that
account whose `sign_key` and `box_key` match. It makes the structural
checks of section 4.4 too. These checks keep honest clients from
tripping each other up; they are not what security rests on, since a
dishonest server can skip them.

A new version is announced to the account's own sessions, to the
other account of each of its sealed chats, and over the link to the
home server of a remote one (section 17).

## 5. Key epochs

### 5.1 The epoch record

A key epoch is one secret shared by every device of both accounts.
It is announced by an epoch record, made by one device.

| Field              | Type        | Meaning                             |
|--------------------|-------------|-------------------------------------|
| `format`           | `u8`        | 1                                   |
| `conversation_id`  | `uuid`      |                                     |
| `key_epoch`        | `u64`       | A plain counter. A chat's first is 1 |
| `prev`             | `bytes(32)` | Hash of the previous epoch's stored record; 32 zero bytes in epoch 1 |
| `maker_account_id` | `uuid`      |                                     |
| `maker_device_id`  | `uuid`      |                                     |
| `accounts`         | list of account reference | Exactly two, sorted by `account_id` |
| `check`            | `bytes(32)` | Section 5.2                         |
| `recipients`       | list of recipient | Sorted by `device_id`         |
| `at`               | `time`      | The maker's clock                   |

An account reference says which device list the maker used:

| Field          | Type        | Meaning                                 |
|----------------|-------------|-----------------------------------------|
| `account_id`   | `uuid`      |                                         |
| `list_version` | `u64`       | 0 if the account has no list yet        |
| `list_hash`    | `bytes(32)` | Hash of that list (section 4.3); zero bytes if `list_version` is 0 |

A recipient:

| Field        | Type        | Meaning                                   |
|--------------|-------------|-------------------------------------------|
| `account_id` | `uuid`      |                                           |
| `device_id`  | `uuid`      |                                           |
| `sealed`     | `bytes(80)` | `seal(that device's box_key, "churr seal epoch v1", conversation_id \|\| key_epoch \|\| device_id, epoch secret)` |

- **The device set is not chosen.** `recipients` is every device in
  the two lists named by `accounts`, no more and no fewer. The maker
  is one of them.
- The epoch secret is 32 fresh random bytes, sealed once per
  recipient, each with its own one-time key pair (section 2.5).
- What is stored (`sealed_epochs.record`) and sent is the record
  followed by 64 bytes: the maker device's signature over the record,
  label `churr epoch v1`.
- The hash of an epoch, used as the next one's `prev`, is over the
  stored bytes, signature included, with the label
  `churr epoch hash v1`.

Only the 80-byte `sealed` values are secret. The rest of a record is
readable by the server, which is what lets it make the checks of
section 5.5.

### 5.2 What is derived from an epoch secret

Every derivation uses the same salt, `conversation_id || key_epoch`
(24 bytes), and gives 32 bytes.

    history secret = HKDF(epoch secret,   salt, "churr history v1")
    content key    = HKDF(history secret, salt, "churr content v1")
    check          = HKDF(history secret, salt, "churr epoch check v1")
    sender key     = HKDF(content key,    salt,
                          "churr sender v1" || 0x00 || device_id)

    call secret    = HKDF(epoch secret,   salt,
                          "churr call v1" || 0x00 || call_id)
    call sender key = HKDF(call secret,   salt,
                          "churr call sender v1" || 0x00 || device_id)

- The **history secret** is what a device keeps for an epoch, and
  what goes in the backup (section 13.2). Everything needed to read
  the epoch's messages comes from it.
- The **content key** is never used to encrypt directly. Each device
  has its own **sender key** under it, and each message its own key
  under that (section 8.1).
- **`check`** is published in the epoch record. It lets a device
  confirm that the secret it opened, or was given later from a backup
  or by the other person, is the one the record announced, and that
  every recipient was given the same one. It reveals nothing useful
  about a 32-byte random secret.
- The **call secret** comes from the epoch secret, not from the
  history secret. A device keeps the epoch secret itself only while
  that epoch is the current one, in memory or protected storage, and
  it is never written to the backup. So the backup cannot yield call
  keys (section 10).

### 5.3 Making an epoch

A device that decides to make one (section 7 says when):

1. Fetches and checks the newest device list of both accounts
   (section 4.4).
2. Builds the record: `key_epoch` one above the chat's current,
   `prev` the hash of the current record, both list versions and
   hashes, and one `sealed` value for every device in those lists.
3. Derives `check`, signs, and posts the record to the chat.
4. On success, derives the keys of section 5.2, writes a backup
   record (section 13.2), and discards the previous epoch's epoch
   secret.

### 5.4 Checking an epoch record

A device accepts a record only if all of these hold. It checks them
itself and does not rely on the server having done so.

1. The record parses, with no bytes left over, and
   `conversation_id` is this chat.
2. `key_epoch` is one above the previous record stored for this
   chat, and `prev` is the hash of that record's stored bytes,
   whether or not the device accepted it. For the oldest record a
   device is given, there is nothing to compare `prev` with, and it
   is taken as the starting point, except that a record with
   `key_epoch` 0, or with `key_epoch` 1 and a `prev` that is not all
   zero bytes, is refused.
3. `accounts` names exactly the chat's two accounts.
4. For each account, the device has checked that account's list chain
   up to `list_version` (fetching versions if it must), the hash of
   that version equals `list_hash`, and `list_version` is not lower
   than in the previous epoch record. Lists never go backwards.
5. `recipients` is exactly the devices of those two list versions,
   each under its right account.
6. The maker is one of the recipients, and the signature verifies
   under that device's `sign_key` from the list.
7. If the device is a recipient: its `sealed` value opens, and the
   `check` derived from what it opened equals the record's.

A record that fails is not accepted. The device uses no key from
it, draws no message sent in it, shows "This chat's keys are out of
step", and makes the next epoch itself (section 7.4). The refused
record keeps its place in the chain, so every device still agrees on
what `prev` must be.

A record that passes but names a list version older than the newest
the device knows is still accepted. It is valid for what it says. The
device then makes a newer epoch at once (section 7.1).

### 5.5 What the server does, and two devices at once

The server stores one record per chat per `key_epoch` in
`sealed_epochs` (`conversation_id`, `key_epoch`, `seq`, `device_id`,
`record`, `created_at`), gives each the conversation's next sequence
number, sets `conversations.key_epoch`, and delivers it to both
accounts' devices as `sealed.epoch_started`. Records are kept as long
as their epoch has messages (section 13.2).

Before storing, it checks what it can read:

- the poster is a device of one of the two accounts, and the chat is
  not read-only;
- `key_epoch` is exactly one above `conversations.key_epoch`;
- the two accounts are the chat's;
- each `list_version` exists in `device_lists`, has that hash, and is
  not lower than in the previous record;
- `recipients` is exactly those lists' devices;
- the maker's signature verifies.

The poster need not be the maker: a record proves itself, which
matters in section 7.4.

**A race.** Two devices may make epoch `n + 1` at once. The primary
key of `sealed_epochs` lets one in. The other is answered `conflict`,
fetches the winner, checks it, and makes epoch `n + 2` only if the
winner does not already cover what it wanted (the newest lists, a
fresh enough date). Across servers no single server sees both
records first (section 17), so the rule is applied by every server:
of two records for one `key_epoch`, the one with the earlier
shared-clock stamp stands and replaces the other wherever the other
was stored first; devices are told with `sealed.epoch_started`
carrying `replaced`.

**Messages follow epochs.** A message is valid only if its key epoch
is the chat's current one at its place in the sequence. The server
refuses a stale one with `conflict`, and the device encrypts again in
the new epoch. So everything after an epoch record in the sequence is
in that epoch, and a device left out of it can read nothing that
follows.

**A stale epoch.** If the chat's current record names a list version
older than the newest in `device_lists`, the server refuses messages
with `conflict` until someone has made a newer epoch. An honest
server therefore lets nothing be sent to a device after its removal
has been published, even by a sender that has not heard of it.

The server is not trusted for any of this. A dishonest server can
store a bad record or order two; devices then refuse it under
section 5.4 and the chat is disrupted, not read.

## 6. Creating a sealed chat

1. **The row.** A device asks the server to start a sealed chat with
   an account. `perm` decides (`DESIGN.md` section 4): sealed chats
   are on, neither is a bot, neither has blocked the other, and a
   remote account shares a channel with the asker. The server inserts
   the `conversations` row (`kind = 'sealed'`, `private`, no name,
   `member_key` the pair) and two `members` rows at level `member`.
   If the pair already has one, it returns that one. The chat exists
   and is empty, and its `key_epoch` is null.
2. **Lists.** The creating device fetches and checks the other
   account's device list and its own account's (section 4.4).
3. **Epoch 1.** It makes the first epoch record (section 5.3) with
   `key_epoch` 1 and `prev` of 32 zero bytes, sealed to every device
   in both lists, itself included.
4. **Upload.** The server accepts it only while the chat's
   `key_epoch` is null (compare and set), stores it, and delivers
   `sealed.epoch_started` to both accounts' devices.
5. **A race.** If both people create at once, one record wins. The
   loser's device checks the winner and uses it.
6. **The other devices** check the record (section 5.4), open their
   `sealed` value, derive the keys and write a backup record. Nothing
   has to be fetched from them or prepared by them beforehand: a
   device that is switched off is sealed to all the same, and opens
   the record when it next connects.

If the other account has no device list yet, because that person has
never used a sealed chat, its account reference has `list_version` 0
and the epoch has only the creator's devices in it. The creator may
send at once. The other person's first device makes a new epoch that
includes itself, and is given what it missed (section 12.5).

## 7. When a new epoch is made

### 7.1 The three reasons

| Reason                                   | Who makes it                          |
|------------------------------------------|---------------------------------------|
| The chat is created                      | The creating device. This is epoch 1. |
| Either account's device list changed     | Any listed device of either account that knows of the change. The device that changed the list does it straight away. |
| The current epoch is more than 7 days old when someone sends | The device about to send, before it sends. |

**Age.** A device about to send compares its own clock with the `at`
of the current epoch record. If `at` is more than 7 days ago, or is
in the future by more than 5 minutes, it makes a new epoch first.
The maker's clock is trusted only this far: a wrong one causes extra
epochs or a late one, never a missing check. A chat nobody writes in
makes no epochs.

**A list change the device has no epoch for.** When a device learns
of a device list version newer than the current epoch record names,
for either account:

1. It checks the new version (section 4.4) and shows the notice.
2. It holds its own outgoing messages for this chat.
3. If it made the change itself, it makes the new epoch now.
   Otherwise it waits a random time up to two seconds, to let the
   device that made the change go first.
4. If no record naming the new version has arrived, it makes one.
   If several devices do, one wins (section 5.5).
5. It sends its held messages in the new epoch.

A device that is not running does nothing. The next device to send
does the work, and the server's stale-epoch rule (section 5.5) stops
a sender that has not yet heard of the change.

There is no other reason. In particular a device does not make an
epoch because it was asked to by the server or by a message.

### 7.2 A device joins

A device that has just been unlocked (section 13) needs no other
device to be online.

1. It reads its account's backup records and so has the history
   secret of every epoch its account was part of (section 13.2).
2. It publishes the next version of its account's device list, with
   itself in it (section 4).
3. For each of the account's sealed chats it fetches the epoch
   records, checks the chain from the oldest it is given
   (section 5.4, all but step 7), and checks each history secret from
   the backup against that epoch's `check`.
4. It makes a new epoch that includes itself (section 5.3).
5. The other devices accept that record under section 5.4. They show
   nothing more: the notice was shown when the list changed.

From then on it is a recipient like any other. Epochs before it
joined are read from the backup; epochs its whole account missed are
handled by section 12.5.

### 7.3 A device is removed

A device leaves in two steps: it leaves the list, then a new epoch is
made without it.

| Cause                         | Who changes the list                          |
|-------------------------------|-----------------------------------------------|
| The person signs out on it    | The device itself, as its last act, then it wipes its keys. |
| The person revokes it from another device ("lost phone") | That other device, if it is unlocked. It also replaces the backup key and account key (section 13.6), because the lost device held both. |
| It had no session for 30 days | Any unlocked device of the account, when the server reports it retired. |
| The session was revoked by an admin, or from a device that is not unlocked | The next unlocked device of the account to come online. Until then the device stays listed, though the server no longer serves it. |

Then section 7.1 applies: a remaining device of either account makes
an epoch from the new list. Its secret is fresh and is sealed only to
the devices still listed, so the removed device cannot learn it.

If the device removed was the last one of both accounts, nobody is
left to make an epoch. Nothing can be sent either, so nothing is
exposed. The next device to join makes one (section 7.2).

When an account is removed from the server or banned, its devices
can no longer fetch anything, and the chat becomes read-only for the
other person. No epoch is made.

### 7.4 When the server and the devices disagree

**A refused record.** A device that refuses the newest stored record
(section 5.4) makes the next epoch itself, with `prev` the hash of
the refused record's stored bytes. Other devices check that hash the
same way, so they accept the repair whether or not they had refused
the bad record too. Messages sent in a refused epoch are not drawn.

**A server restored from an older backup.** The server may then hold
fewer epoch records, device list versions or backup records than the
devices do. All three kinds prove themselves, so a device that holds
ones the server lacks posts them again, in order: its own account's
list versions, then the chat's epoch records (the poster need not be
the maker, section 5.5), then its backup records. The chat then
carries on. Messages the server lost are gone from the server; apps
that had them keep their local copies. A device that finds the
server's `key_version` behind its own repeats the key replacement
(section 13.6) from what it holds.

Neither case gives a device anything it could not already do as a
listed device.

## 8. Messages

### 8.1 The sealed message

The `ciphertext` of `MESSAGE-FORMAT.md` section 9 is these bytes:

| Offset | Length | Field                                           |
|--------|--------|-------------------------------------------------|
| 0      | 1      | Format, 1                                       |
| 1      | 1      | Flags. Bit 0: quiet (section 8.4). Other bits zero. |
| 2      | 16     | Conversation identifier                         |
| 18     | 8      | Key epoch                                       |
| 26     | 16     | Sender's account identifier                     |
| 42     | 16     | Sender's device identifier                      |
| 58     | 8      | Counter                                         |
| 66     | 16     | Message reference                               |
| 82     | 16     | Salt, random                                    |
| 98     | n      | Encrypted content, with its 16-byte tag         |
| 98+n   | 64     | Signature                                       |

Bytes 0 to 81 are the **header**.

- **Salt and encrypted content** together are
  `lock(sender key, "churr lock message v1", header, content)`
  (section 2.6), where the sender key is the sending device's for
  this epoch (section 5.2) and the content is the UTF-8 JSON object
  of section 8.3. Written out: the message key and nonce are
  `HKDF(sender key, salt, "churr lock message v1" || 0x00 || header,
  44)`, and the associated data is `"churr lock message v1" || 0x00
  || header`.
- **Signature** is by the sending device's signing key, label
  `churr sealed message v1`, over bytes 0 to 97+n.
- **Counter** starts at 1 for each device in each sealed chat and
  goes up by one for each message that device sends there. It carries
  on across epochs and is kept in the device's storage. A device
  sends one message at a time in a chat, so its counters rise with
  the sequence numbers.
- **Message reference** is a UUID the sending device makes. It is
  also sent to the server as the message's `client_id`
  (`CONVENTIONS.md`). Inside sealed content, every reference to
  another message (`message_id`, `parent_id`, the quote) holds that
  message's reference, not the identifier the server gave its row.
  A device cannot trust the server to say which row is which, and it
  can trust a value under the sender's signature.
- Content is not padded. The server sees its length.

**Why keys are made this way.** Several devices write under one
epoch, and AES-GCM fails badly if one key ever encrypts two messages
with the same nonce: the two plaintexts leak into each other and
forgery becomes possible. Three ways to avoid that were weighed.

| Scheme                                   | Fails when                         |
|------------------------------------------|------------------------------------|
| One key per device, the counter as nonce | A device's storage is wound back (a restored phone backup, a browser profile copied) and the counter repeats. |
| One key per device, a random 12-byte nonce | The random generator misbehaves; and it is limited to about 2^32 messages per key. |
| **A key per message**, derived from the device's sender key with a random 16-byte salt and the header, which holds the counter | Only when the salt repeats **and** the counter repeats: a bad generator and wound-back storage together. |

The third is used. Its cost is one HKDF per message. Its limits: a
message's content is at most 128 KB, a counter is below 2^64, and no
AES-GCM key ever encrypts more than one message, so no volume limit
applies.

### 8.2 Sending and receiving

**Sending.**

1. If the chat needs a new epoch (section 7.1), the device makes it
   first.
2. The device builds the content object and the header, encrypts and
   signs.
3. It posts to the chat: the sealed bytes, the key epoch, the
   `client_id`, whether the message is quiet, and the identifiers of
   any uploaded files it carries (section 9).
4. The server checks what it can see: the sender is a member and the
   chat is not read-only; the bytes are at most 128 KB; the header's
   conversation, account and device are the sender's own; the key
   epoch is the chat's current one and that epoch is not stale
   (section 5.5); the quiet flag matches. It inserts the `messages`
   row with `ciphertext`, `key_epoch`, `quiet`, `author_id`,
   `client_id` and empty text, gives it a sequence number, and
   delivers it. The row is never put in the search index.

**Receiving.** A device accepts a sealed message only if all of these
hold. A message that fails is shown as "A message could not be
checked" and its content is never drawn.

1. The format is 1, and the conversation, key epoch and sender
   account in the header equal those of the row it arrived in.
2. The device accepted that epoch's record, the row's sequence
   number is after that record's and not after the next epoch's
   record (if the device holds one), and the sending device is one of
   its recipients.
3. The signature verifies under that device's `sign_key`, taken from
   the device list version the epoch record names, and the content
   decrypts.
4. The pair (device, counter) has not been seen at another sequence
   number. If it has, the lower sequence number is the message and
   the other is dropped: the server replayed it. If the copy at the
   higher number was taken in first (a device reads rows out of
   order when an old epoch's secret arrives late), it was shown until
   then; the original takes its place, and the message is shown once,
   at the original's place.
5. The counter is higher than the last one seen from that device at
   a lower sequence number. If not, the message is still shown, with
   a mark that the server delivered it out of order.

A jump in a device's counters that is not explained by a deletion
marker or by retention having removed old rows is shown as "A message
may be missing here." This is a hint, not a proof: the last message
in a chat can always be held back without leaving a gap.

### 8.3 What is inside

The kinds of `MESSAGE-FORMAT.md` section 9.

| `kind`     | Fields                                    | Rule the receiving device applies                  |
|------------|-------------------------------------------|----------------------------------------------------|
| `message`  | `text`, `entities`, `files`, `parent_id`, `quote`, and `action` (true only, written only when set) | Drawn as any message. Entities are worked out by the sending device; the receiver draws a mention only for one of the two accounts. Checklist items get their `item_id` from the sender. `action` marks text typed with `/me` (ruling R67): it is drawn after the author's name; a device that does not know it shows the text plainly. Since it is left out when not set, no other message's bytes change and the vectors stand. |
| `edit`     | `message_id`, `text`, `entities`          | Applied only if the sender's account wrote the target. Replaces its text and entities; its files stay. |
| `delete`   | `message_id`                              | Applied only if the sender's account wrote the target. The target is no longer shown. |
| `reaction` | `message_id`, `emoji`, `on`               | One of each emoji per account per message; at most twenty different emoji on a message. |
| `task`     | `message_id`, `item_id`, `checked`        | Either person may tick. The state is the latest by sequence number. |
| `history`  | `secrets`                                 | Section 12.5. Never drawn.                         |

- A kind the device does not know is ignored, and so is anything that
  refers to a message it does not have.
- **Edits and deletes.** The server keeps no versions. An `edit`
  travels as a new sealed message and devices show only the latest
  text. The earlier ciphertext stays on the server until retention
  removes it, and a device that holds the keys could still decrypt
  it; clients do not offer to. For a `delete`, the device also asks
  the server to delete the target row, as an author may for any
  message. The server erases that row's ciphertext and file blobs at
  once and leaves the marker. It learns that a deletion happened and
  of which row.
- **Reactions and checklist ticks** are sealed messages like any
  other. Who ticked what and when is known only to the two people's
  devices, and the per-item history is whatever those devices and
  the stored messages hold.
- **Threads.** `parent_id` is inside, so the server sees a flat
  stream (`DESIGN.md` section 5).

### 8.4 Quiet messages

The server cannot tell a reaction from a message, yet a reaction must
not notify anyone or count as unread. So the sender marks every kind
except `message` as quiet, in the header flag and in the request
(`messages.quiet`). The server does not notify, wake a phone or raise
the unread count for a quiet message. This tells the server that a
given item is not an ordinary message, which is accepted. The flag is
under the signature, so a receiving device sees if the server changed
it.

## 9. Files

A file in a sealed chat is encrypted on the device before upload.

1. The device makes a random 32-byte file key.
2. It cuts the file into chunks of 65,536 bytes, the last one
   shorter (an empty file is one empty chunk; a file whose length is
   an exact multiple of 65,536 ends with a full chunk, not an empty
   one), and encrypts each with AES-256-GCM under the file key, using
   the platform's own AES-256-GCM where it has one and the pure-Dart
   (or, in a browser, Web Crypto) implementation otherwise. The bytes
   are the same either way. The 12-byte nonce of chunk `i`,
   counting from 0, is 3 zero bytes, then `i` as a `u64`, then one
   byte that is 1 for the last chunk and 0 otherwise. The associated
   data is `"churr file v1"`. A fixed nonce pattern is safe here
   because the key is used for this one file only and no two chunks
   share a nonce.
3. It uploads the chunks, each followed by its tag, joined end to
   end, as one blob to the conversation. The server stores a `files`
   row with an empty name, the media type `application/octet-stream`,
   kind `file`, and no thumbnail or text preview.
4. For an image or video the device also makes the thumbnail itself
   (at most 720 pixels on the long edge, re-encoded, with location
   and camera details removed), and encrypts and uploads it the same
   way under a second random key.
5. The file entry inside the sealed message carries what the server
   was not given:

       {"id": "<file id>", "key": "<32 bytes>", "name": "floorplan.png",
        "media_type": "image/png", "kind": "image", "size": 482113,
        "width": 1600, "height": 900, "caption": "Second floor",
        "thumb": {"id": "<file id>", "key": "<32 bytes>", "width": 720, "height": 405}}

6. The message is posted with the blobs' identifiers named outside
   the ciphertext, so the server can tie them to the message's row
   for retention, deletion and the clean-up of uploads never used.

**Downloading.** The device fetches the blob through the server,
which checks membership as for any file, and decrypts chunk by chunk.
The chunk number in the nonce stops chunks being reordered and the
last-chunk byte stops the file being cut short. A file is saved or
opened only after its last chunk has checked out. Media may start
playing earlier, from chunks that have each been checked. Chunks let
a large file be encrypted, decrypted and seeked in without holding
it in memory, which matters because Web Crypto encrypts a whole
buffer at a time.

The receiving device decides how to draw the file from its content,
by the rules of `MESSAGE-FORMAT.md` section 5, and treats the
sender's `media_type` and `kind` as hints. SVG is always a download.

**What the server sees:** who uploaded, to which sealed chat, when,
the blob's length (the file's length plus 16 bytes per chunk), which
message it belongs to, and who downloads it when. Not the name, the
type or the content.

The file size limit applies to the blob, which bounds a file at far
fewer chunks than a `u64` can count. Up to ten files per message, as
elsewhere.

## 10. Calls

A call in a sealed chat runs through the server's media forwarder
like any call (`DESIGN.md` section 13). The difference is that each
audio and video frame is encrypted by the sending device before
WebRTC's own transport encryption, and decrypted after it on the
receiving device, so the forwarder passes on frames it cannot decode.

**Keys.** For the call and the current key epoch, each device derives
the call secret and, for every device in the epoch, that device's
call sender key (section 5.2). A device encrypts its frames under its
own call sender key and decrypts each other device's frames under
theirs. The key index carried in each frame is `key_epoch mod 16`.

**Not in the backup.** The call secret comes from the epoch secret,
which is never backed up (section 5.2). Call keys are held in memory
only and dropped when the call ends. Someone who later gets the
backup key cannot decrypt a recording of the call's packets.

**A device joins or leaves during a call.** A new epoch is made
(section 7), every device derives new call keys and switches to the
new key index. Receivers keep the previous epoch's keys for ten
seconds to cover frames in flight.

**Which device.** Each device sends under its own key, so a receiver
must know which device a track comes from. The server says so in the
call's signalling (`device_id`, `docs/CALLS.md` section 10), and the
receiver uses that device's call sender key if the device is in the
current epoch. A device that is not has no key here, and nothing from
it decrypts. The server is not trusted for this: a wrong answer can
only make frames fail to decrypt.

**Frame format.** Churr does not define one. It uses the format of
the frame encryption built into the WebRTC library the apps ship
(libwebrtc's FrameCryptor), so the Flutter WebRTC plugin encrypts and
decrypts natively, and the web chat implements the same format in
JavaScript on encoded frames, using Web Crypto, off the main thread.
**`docs/CALL-FRAMES.md` is the definition**, measured on the library
and checked byte for byte: the layout of a frame, the IV, which
leading bytes stay readable and are the associated data, the key
index and the ring of 16, what is done with a frame that does not
decrypt, and the settings the app gives the plugin. It is not repeated
here. Two things in it bear on the rest of this document:

- The plugin takes no AES key. It derives one from what it is given,
  so the call sender key is its input, not the frame key:
  `HKDF(call sender key, "churr call frames v1", 128 zero bytes, 16)`.
  Frames are therefore **AES-128-GCM**, where everything else here is
  AES-256-GCM. This is the one exception to section 2.2, for call
  frames only. The derivation is HKDF-SHA-256, as everywhere.
- Video in a sealed chat's call is VP8. The readable bytes of other
  codecs under the library are not settled.

The format has test vectors of its own, from an epoch secret to
encrypted frames, with frames that must be refused:
`testdata/call-frames/vectors.json`, written by the Go reference and
passed by the web chat's code and the app's (section 18 applies to it
as to the others). The points of `CALL-FRAMES.md` not marked as seen
there are listed in section 22.

**What the forwarder sees:** who is in the call, when, packet sizes
and timing, the packet headers WebRTC needs to route media (which can
include whether someone is speaking), and the readable leading bytes
of each frame.

**Limit.** Every device of the epoch can derive every other device's
call sender key. Frames prove "sent by a device in this chat", not
which one. In a chat of two people that is enough.

The call's start and end are ordinary system messages the server
posts in the clear, as for any conversation.

## 11. Notifications

A wake-up carries no content (`DESIGN.md` section 12). For a sealed
chat the phone must also decrypt.

1. The server sends a wake-up for a message that is not quiet, when
   the person's mode for the chat would notify.
2. The app's notification extension (iPhone) or background handler
   (Android) fetches the message's row from the person's own server
   with the app's token.
3. It reads, from storage the main app shares with it, the content
   key for that chat and key epoch and the `sign_key` of each device
   in that epoch. On iPhone that storage is the app group's keychain;
   on Android a keystore-wrapped file. The main app writes these
   whenever it accepts an epoch.
4. It derives the sender key, checks the signature, decrypts, and
   shows the sender's name and the text, or just "New message" if
   the person has turned previews off.
5. If it lacks the key (the message is in an epoch the app has not
   opened yet), it shows "New message". The app catches up when it
   is next opened.

On iPhone this is a small piece of Swift using CryptoKit: HKDF, an
Ed25519 verification and one AES-GCM decryption. It never makes an
epoch, never signs, and changes no stored key, so it and the app
cannot trip over each other. It applies the checks of section 8.2
except the counter checks, which need the full history and are left
to the app. On Android the app's own Dart code does the same in the
background.

In the web chat, notifications appear only while the page is open,
and the page decrypts as it does for the screen.

## 12. History

### 12.1 Why history secrets are kept

An epoch secret is sealed to the devices that exist when it is made.
A device added later was never sent it. Left there, a phone added
today could not read yesterday's messages, and `DESIGN.md` asks for
the opposite: a newly added device or browser reads the earlier
messages of its account's sealed chats.

So each device keeps the history secret of every epoch it was in, and
puts it in an encrypted backup that the account's later devices can
open. This buys the feature and costs forward secrecy. Section 12.4
says exactly how much.

### 12.2 What is kept where

| Value          | On a device                              | In the backup |
|----------------|------------------------------------------|---------------|
| Epoch secret   | Only while its epoch is the current one  | Never         |
| History secret | For as long as the epoch has messages    | Yes, one record per chat per epoch (section 13.2) |
| Content, sender and message keys | Derived when needed          | Never         |
| Call secret and call keys | In memory during the call           | Never         |

### 12.3 Reading earlier messages

A device reads a message of key epoch `e` if it has the history
secret of `e`. It has it:

- by having been a recipient of epoch `e`; or
- from its account's backup, where a device of the same account that
  was a recipient put it; or
- from the other person's device, for epochs in which its own account
  had no device at all (section 12.5).

In the second and third cases the device checks the secret against
the `check` in that epoch's record before using it (section 5.2), and
takes the signing keys for that epoch's messages from the device list
versions the record names.

### 12.4 What is lost, and what is kept

**Lost.**

- **Forward secrecy inside an epoch.** Every message of an epoch can
  be read with that epoch's history secret. An epoch lasts up to
  seven days of use, or longer in a quiet chat.
- **Forward secrecy across epochs, against anyone who gets the backup
  key.** The backup holds every history secret the account still
  has. The backup key opens them all.
- **Forward secrecy against a copied device.** An unlocked device
  holds the backup key, so taking over a device or its storage gives
  the whole readable history. Separately, every epoch record is
  sealed to devices' long-lived box keys, so a copied box key opens
  every epoch that device was sealed to, from the stored records.
- **Healing that needs no one to act.** A new epoch shuts out someone
  who learned an earlier epoch's secret and nothing else. It does not
  shut out someone who holds a device's box key, since the new secret
  is sealed to that key again, or someone who holds the backup key,
  since the new history secret is written to the same backup. Those
  take removing the device or replacing the backup key and account
  key (section 13.6), which a person must set off.
- **Deniability.** Every message is signed by its device. A recipient
  can show a third party who wrote what.

**Kept.**

- **Secrecy from the server, the hub and the network.** Reading a
  message needs a history secret, which comes only from a device's
  box key or from a backup key. The server holds neither.
- **A removed device is shut out of later epochs.** Their secrets are
  not sealed to it. This holds fully when the removal also replaced
  the backup key (section 13.6), or when the removed device can no
  longer fetch from the server.
- **One leaked history secret exposes one epoch of one chat,** at
  most a week of active use, and nothing before, after or elsewhere.
- **Retention shrinks what a later leak exposes.** Once retention has
  deleted every message of a past epoch, the server deletes that
  epoch's record and backup records, and devices delete its secret
  (section 13.2). A device that has been offline, or a server that
  does not do its job, keeps them longer.
- **Call media.** Not readable from the backup at all (section 10).
- **Authenticity.** Each message is signed by a device whose key is in
  a signed list, and is bound to its chat, epoch, sender and counter.

### 12.5 Epochs an account missed

If an account has no device for a while (the person was signed out
everywhere), epochs go by that its backup has no record for. The
messages of those epochs were written to that person and they should
be able to read them.

Which epochs those are is plain from the epoch records: the ones with
no recipient belonging to that account. When a device accepts an
epoch record in which the **other** account has a device again, after
one or more epochs in which it had none, it sends a `history` message
(section 8.3) in the new epoch:

    {"kind": "history", "secrets": [
       {"key_epoch": 5, "secret": "<32 bytes, the history secret>"}
    ]}

- Only epochs the other account had no device in, and only those that
  still have messages on the server.
- The receiving device checks the message like any other, then checks
  each secret against the `check` of that epoch's record, so it does
  not have to take the sender's word. It writes each into its own
  account's backup as an ordinary record, and can then read those
  epochs. Several devices may send the same secrets; the copies are
  harmless.
- This needs one of the sender's devices to be online after the other
  person's device joins. Until then the joiner sees "Earlier messages
  will appear when Sam's device is next online."
- **Nothing is shared across a reset.** If the other account's list
  was reset (section 4.4), no secret from before the reset is sent,
  ever. Otherwise someone who took over the account and reset it
  would be handed the messages it missed. The cost is that a person
  who truly lost everything also loses what was sent while they had
  no device.

## 13. The key backup

One random backup key per account; small records, one per sealed chat
per key epoch, encrypted under it; one account record; and the backup
key itself stored on the server in several locked copies, any one of
which opens it.

### 13.1 The backup key and the account record

The **backup key** is 32 random bytes. It is made by the first device
of an account that creates or is brought into a sealed chat and finds
that the account has none. That device also makes the account key
pair (section 4.2) and the recovery phrase (section 13.3), and
publishes version 1 of the device list. The backup key's
`key_version` starts at 1. If two devices of one account do this at
once, the server keeps the first and the other unlocks in the
ordinary way.

The **account record** is the one backup item that is not about a
chat. Its content is a record (section 2.3):

| Field                 | Type        | Meaning                          |
|-----------------------|-------------|----------------------------------|
| `format`              | `u8`        | 1                                |
| `account_id`          | `uuid`      |                                  |
| `key_version`         | `u32`       |                                  |
| `revision`            | `u64`       | Goes up by one each time the ways change |
| `account_key`         | `bytes(32)` | Ed25519 public key               |
| `account_key_private` | `bytes(32)` | Its Ed25519 seed                 |
| `ways`                | list of way | Sorted by `kind`, then `id`      |
| `at`                  | `time`      |                                  |

A way:

| Field           | Type        | Meaning                              |
|-----------------|-------------|--------------------------------------|
| `kind`          | `u8`        | 1 passkey, 2 phrase                  |
| `id`            | `uuid`      | The passkey's identifier; 16 zero bytes for the phrase |
| `unlock_public` | `bytes(32)` | X25519 public key                    |

It is stored in `account_records.record` as

    lock(backup key, "churr lock account record v1",
         account_id || key_version || revision, content)

with `key_version` as a `u32` and `revision` as a `u64`. The server
keeps the newest (`account_records`: `key_version`, `revision`,
`record`). A device remembers the newest record it has seen and
refuses an older one, comparing `key_version` first and then
`revision`. A key replacement (section 13.6) continues the revision
count; a reset (section 13.8) starts it again at 1.

`ways` is the account's own, authenticated list of the public unlock
keys the backup key may be locked to. It exists because the copy of
those keys on the server cannot be trusted (section 13.6).

### 13.2 Records

One record per sealed chat per key epoch, per account. Its content:

| Field             | Type        | Meaning                |
|-------------------|-------------|------------------------|
| `format`          | `u8`        | 1                      |
| `conversation_id` | `uuid`      |                        |
| `key_epoch`       | `u64`       |                        |
| `secret`          | `bytes(32)` | The history secret     |
| `at`              | `time`      |                        |

It is stored in `key_backups.record` as

    lock(backup key, "churr lock backup record v1",
         account_id || conversation_id || key_epoch || key_version,
         content)

in the row keyed by `account_id`, `conversation_id` and `key_epoch`,
with `key_version` beside it. The stored value is 16 bytes of salt
and 81 bytes of ciphertext.

A device's own private keys are never in a record, and neither is an
epoch secret.

**Writing.** A device that accepts an epoch, or receives a secret by
section 12.5, uploads a record. The server keeps the first one for
each account, chat and epoch and ignores the rest, so a device with
new keys adds a row and rewrites nothing.

**Reading.** An unlocked device lists its account's records for a
chat, opens those it lacks, checks that the chat and epoch inside
match the row, and checks the secret against the epoch record's
`check` (section 5.2).

**What the server can do with records.** It cannot open one: that
needs the backup key. It cannot make one: the encryption is
authenticated under a key it lacks, and a made-up secret would not
match the epoch's `check`. It cannot move one to another chat, epoch
or account: the context would not match. It can delete records,
refuse to return them, or return records from before a key
replacement, which no longer open. Each of those loses history for
the account and reveals nothing.

**Clean-up.** When retention has removed every message of a chat's
oldest epochs, the janitor deletes those epochs' `sealed_epochs` rows
and both accounts' `key_backups` rows for them, oldest first and
never the current epoch, so what remains is always an unbroken run.
Devices delete their copies of those secrets when they next sync.

### 13.3 Locked copies

Each way of unlocking is an X25519 key pair, the **unlock key**. A
locked copy is the backup key sealed to an unlock key's public half,
and signed:

    way tag = kind (u8: 1 passkey, 2 phrase, 3 device)
              || id (uuid: passkey, 16 zero bytes, or device)

    sealed  = seal(unlock public key, "churr seal backup key v1",
                   account_id || key_version || way tag, backup key)

    sig     = account key signature, label "churr locked copy v1",
              over account_id || key_version || way tag || sealed

The stored value is `sealed` (80 bytes) then `sig` (64 bytes): 144
bytes in `backup_keys.locked_key`, in a row with `kind`, `passkey_id`
or `device_id`, and `key_version`.

**Why a public key and not a plain symmetric lock.** A device must be
able to make a copy for a passkey that is not in front of it: when it
approves a passkey added elsewhere (section 13.4) and when it
replaces the backup key (section 13.6). With a symmetric lock only
the holder of the passkey could make the copy. Only the passkey can
still open it.

**Why the signature.** Anyone who knows an unlock public key, and
the server does, could seal a key of their own choosing to it. So a
device that opens a copy does not trust it yet. It opens the account
record with the key it got, takes the account key from it, checks
`sig`, and checks that this account key is the one in the account's
device list (its pin, if it has one). A copy that fails any of this
is refused. Section 16 says what is left for a device with no pin.

**The passkey way.**

- PRF input: the 32 bytes `SHA-256("churr sealed unlock v1")`, given
  as the first evaluation input of the WebAuthn PRF extension. It is
  the same for every account; the authenticator mixes it with a
  secret that belongs to the one credential.
- PRF output: 32 bytes. It never leaves the page that asked for it.
- Unlock private key:

      HKDF(ikm  = PRF output,
           salt = 32 zero bytes,
           info = "churr unlock passkey v1" || 0x00 || account_id
                  || credential identifier,
           n    = 32)

- Unlock public key: `X25519(private, 9)`. It is stored in
  `passkeys.unlock_public`.

**The phrase way.**

- The phrase is 12 words from the BIP-39 English list of 2,048
  words: 128 random bits and BIP-39's 4-bit checksum, which is the
  first four bits of the SHA-256 of those 16 bytes. It is shown once,
  when the backup key is made, with the instruction to write it down.
  It is never sent to the server.
- Reading a typed phrase: lower-case it, split on any run of
  whitespace, match each word to the list on its first four letters
  (they identify a word, so a word typed in full or cut to four
  letters both match), check
  the checksum, and recover the 16 bytes.
- Unlock private key:

      HKDF(ikm  = the 16 bytes,
           salt = 32 zero bytes,
           info = "churr unlock phrase v1" || 0x00 || account_id,
           n    = 32)

- Unlock public key: `X25519(private, 9)`.
- **Why no slow function.** A slow, memory-hungry derivation exists
  to protect a secret people choose, which has few possibilities:
  it makes each guess expensive. This phrase is not chosen. It is
  128 random bits, so there are 2^128 guesses to make, and no
  attacker gets through them however cheap each one is. Slowing the
  derivation would add nothing, and would need a sixth primitive that
  Web Crypto and CryptoKit do not all have. BIP-39's own derivation
  is not used. The one case this does not cover is a phrase most of
  whose words someone has seen; such a phrase must be replaced
  (section 13.6).

**The device way.** The unlock key is the device's box key from the
device list. Used in sections 13.6 and 13.7.

**Unlocking in the web chat.** The sign-in ceremony carries the PRF
input (`AUTH.md` section 5.3). The page derives the unlock key,
fetches the account's locked copies and account record (the server
gives them to any complete session of the account), opens the copy
tagged with that passkey, runs the checks above, and stores the
backup key (section 14). The browser then makes its signing key and
box key and joins (section 7.2).

**Unlocking an app.** An app cannot use a passkey itself (`AUTH.md`
section 11). It makes its signing key and box key before it signs in
and adds the box public key to the authorization request. The
server's sign-in page, having signed the person in with a passkey
that has a locked copy, opens the backup key as above and seals it
again for the app:

    seal(the app's box key, "churr seal handover v1",
         account_id || the request's PKCE code challenge (its ASCII
         bytes), backup key)

The page gives the result to the server, which holds it with the
authorization code (`oauth_codes.box_key` and `oauth_codes.handover`)
and returns it with the tokens. The page keeps nothing. The app opens
it, checks the backup key against the account record and device list
as above, and proceeds as a browser does. If the person signed in
another way, the app offers the phrase, approval from another device
(section 13.7), and "use a passkey". That last choice posts an unlock
request as in section 13.7 and opens a server page, where the person
touches a passkey that can unlock; the page opens the backup key and
answers the request.

**Where the server's pages are trusted.** A passkey works only on a
page served from the server's own web address. So the passkey way
always passes the PRF output, and at the moment of unlocking the
backup key, through a page the server supplies, in the apps exactly
as in the web chat. A dishonest or compromised server could serve a
page that keeps the key. In the apps, the phrase and approval from
another app do not pass through any server page: the phrase is typed
into the app, and approval runs between two apps. Section 20 says
what follows.

### 13.4 Adding a passkey

Every passkey has a different PRF secret, so each gets its own copy,
and a copy can only be made by a device that is already unlocked.

1. A passkey is created (`AUTH.md` section 5.2). If it supports PRF,
   the page that created it obtains the PRF output (at creation where
   the browser returns it, otherwise by one more touch), derives the
   unlock **public** key and gives it to the server, which keeps it
   in `passkeys.unlock_public`.
2. If that page is an unlocked web chat, it makes the locked copy
   there and then. Otherwise the passkey signs in but does not
   unlock.
3. Any unlocked device of the account that sees a passkey with an
   unlock public key and no copy asks its owner: "Let the passkey
   'Work laptop' unlock sealed chats?" On yes, it seals and signs a
   copy (a `backup_keys` row with `kind = 'passkey'`, `passkey_id`,
   `key_version`, `locked_key`), and adds the way to the account
   record with the next revision.

The question in step 3 matters. The public key came through the
server, and a dishonest server could offer a key of its own under a
passkey's name. A person should say yes only for a passkey they have
just added.

### 13.5 Passkeys without PRF

A passkey from a store without PRF signs in normally. `has_prf` is
false, it has no unlock key and no copy, and the account page marks
it "cannot unlock sealed chats". Its owner unlocks with the phrase or
from another device. An account that only uses company sign-in adds a
passkey for unlocking (`AUTH.md` section 5.4) or uses the phrase.
Which stores support PRF is in section 22.

### 13.6 Replacing the backup key

Deleting a passkey deletes its locked copy. That is enough against
someone who has the passkey and nothing else, because an honest
server no longer has anything to give them. It is not enough against
someone who has the passkey, or a lost device, **and** a copy of the
server's data, old or current. So the backup key and the account key
are replaced when:

- a passkey that had a locked copy is removed;
- a device is revoked from another device;
- the phrase is replaced with a new one;
- the person asks for it.

Signing out on a device itself replaces nothing.

An unlocked device does this:

1. Makes a new backup key and a new account key pair, with
   `key_version` one higher.
2. Builds the new account record. Its `ways` are taken from the old
   **account record**, less whatever is being removed. They are not
   taken from the server's rows.
3. Makes a locked copy of the new backup key for each way, and one
   for each remaining device in the list (the device way), all
   signed by the new account key. The phrase stays the same: its
   public key is known, so nobody has to write down new words.
4. Downloads every backup record, opens it, and locks it again under
   the new backup key and `key_version`.
5. Signs the next version of the device list with the new account
   key and, as `prev_sig`, with the old one (a key change,
   section 4.4), leaving out any device being revoked.
6. Hands all of it to the server in one operation that names the
   `key_version` it replaces. The server swaps the account record,
   locked copies, records and list together, deletes the old rows,
   and refuses a second replacement of the same version.
7. Makes a new epoch in each sealed chat, since the list changed
   (section 7.1).
8. The account's other devices see the list's new `key_version`,
   open their device copy with their box key, check it, and carry
   on. The other person's devices show the key-change notice.

If the removal was made where no unlocked device was at hand, the
server sets `account_records.replacement_due`, and the next unlocked
device to connect does the steps above before anything else.

**What this achieves.** After it, the old backup key opens nothing an
honest server still holds, and the old account key signs nothing a
client that has seen the change will accept. Someone who had the old
backup key and a copy of the old records keeps what those records
open: history up to the replacement. They get nothing later.

### 13.7 Approval from another device

The third way to unlock, and the only one for a person with no PRF
passkey and no phrase at hand.

1. The new device, signed in and locked, makes its signing key and
   box key and posts an **unlock request**, a record:

   | Field        | Type        | Meaning                           |
   |--------------|-------------|-----------------------------------|
   | `format`     | `u8`        | 1                                 |
   | `account_id` | `uuid`      |                                   |
   | `device_id`  | `uuid`      |                                   |
   | `sign_key`   | `bytes(32)` |                                   |
   | `box_key`    | `bytes(32)` |                                   |
   | `platform`   | `u8`        | As in section 4.3                 |
   | `at`         | `time`      |                                   |
   | `proof`      | `bytes(64)` | The device proof of section 4.3   |

   The server stores it in `unlock_requests.request` and tells the
   account's other devices. The two devices need not be connected at
   the same moment.
2. It shows a code: the first 100 bits of
   `SHA-256("churr device approval v1" || 0x00 || request record)`,
   as 20 characters of Crockford base32 in four groups of five, and
   as a QR code.
3. An unlocked device of the same account shows "Unlock a new device
   (Android app)?" The person scans the QR code with it, or types
   the 20 characters into it. The unlocked device computes the code
   from the request it received through the server and proceeds only
   if they are equal.
4. It makes a locked copy with the device way for the new device's
   box key (section 13.3). The server stores it in
   `unlock_requests.answer` and passes it on.
5. The new device opens it, checks it, and joins (section 7.2).

The code is long because it is the only thing that stops the server
from swapping the new device's keys for its own. A six-digit code
could be matched by trying keys; 100 bits cannot. Naming the device
and pressing "approve" is not enough. Requests expire after ten
minutes.

**With QR pairing.** When an app is signed in by scanning a QR code
from a web chat that is unlocked (`AUTH.md` section 11.4), the QR
code also holds 32 random bytes, the pairing secret, which the app
never sends to the server. The app sends its box public key with its
pairing request (`oauth_codes.box_key`). When the person confirms the
device, the page makes the hand-over in two layers:

    inner    = seal(the app's box key, "churr seal pairing v1",
                    account_id, backup key)
    handover = lock(pairing secret, "churr lock pairing v1",
                    account_id, inner)

It is 112 bytes, stored in `oauth_codes.handover` and returned with
the tokens. The server cannot open it without the pairing secret, so
swapping the box key for its own gains it nothing. Someone who
photographed the QR code has the secret but not the app's box private
key. One scan signs the app in and unlocks it. The page doing this is
a web chat page, with the trust that implies (section 14).

### 13.8 With no way left

With no passkey that has a copy, no phrase and no unlocked device,
the backup key cannot be recovered, and earlier sealed messages
cannot be read again on any device of that account. The server has
nothing that helps.

The person can start again. After a clear warning, the device makes
a new backup key, account key and phrase, with the next
`key_version`, and publishes a device list marked `reset` holding
only itself. The server deletes the account's old locked copies,
account record and backup records. The device makes a new epoch in
each of the account's sealed chats (section 7.2) and reads from then
on. The other person keeps their own history, is shown the reset
notice (section 4.4), and their devices share nothing from before it
(section 12.5).

## 14. In the web chat

**The code.** The web chat's sealed-chat code is plain JavaScript
modules embedded in the server like the rest of the web chat, using
the browser's own Web Crypto for all five primitives. Nothing is
compiled and nothing is fetched from anywhere but the server's own
origin.

**The browser is a device.** It has its own signing key and box key,
is in the device list with platform 6, and is a recipient of each
epoch. Every person in a sealed chat with it sees that a browser is
among the other's devices.

**Where keys are kept.** In an IndexedDB database belonging to the
server's origin.

- The device's signing key and box key are made by Web Crypto as
  keys that cannot be exported, and are stored as such. A script can
  use them; it cannot read their bytes.
- Everything else (the backup key, the account key, history secrets,
  the current epoch secrets, pins, verified marks, send counters) is
  encrypted before it is written, under an AES-256-GCM key that the
  browser generated as non-exportable and that is kept in the same
  database. These values cannot be made non-exportable themselves,
  because the page has to seal them to other devices or write them
  to the backup.

This keeps raw key bytes out of the stored files. It does not stop a
script running in the page from using the keys. It is a small
protection and is not counted on. Decrypted messages are not stored:
the web chat decrypts what it shows each time it loads.

The page asks the browser to keep the storage (`storage.persist`). A
browser may still clear it, and some clear storage for sites not
visited for a week. When that happens the browser is simply a new
device at its next sign-in, and the old entry is retired
(section 7.3).

**Signing out wipes.** The page removes this device from the device
list, deletes the database, and then revokes the session. If the
first step cannot reach the server, it wipes anyway and the stale
entry is cleaned up by the account's other devices.

**A session that ends by itself** (a 24-hour company session, or 30
idle days) is not a sign-out. The keys stay. When the same account
signs in again in that browser, the page proves the signing key to
the server and carries on as the same device (section 4.1). If a
different account signs in, the database is deleted first.

**Signing in again restores through PRF.** After a sign-out the
browser has nothing. Signing in with a passkey that has a locked copy
gives it the backup key in the same touch (section 13.3); it becomes
a new device and reads history from the backup. With a passkey
without PRF, or company sign-in, the person unlocks with the phrase,
from another device, or with one more touch of a passkey that can
unlock.

**The stated weakness.** A browser gets its code from the server
every time the page loads. A dishonest server, a compromised one, or
anyone able to change what it sends, can serve a page that copies the
backup key somewhere else and uses the device keys as it likes. The
person would see nothing. From then on that party reads the account's
sealed chats, past and future, until every key is replaced from a
device that was not affected. A browser extension with access to the
page can do the same.

**Compared with the apps.** The apps do their encryption in code that
came from the app store or a release download, not from the server.
But unlocking with a passkey passes through a page the server
supplies in the apps too, because a passkey only works for the
server's own web address (section 13.3). So a dishonest server can
capture a backup key at unlock time on any client, and can do more
against a browser, whose every load it supplies. In the apps, the
phrase and approval from another app do not pass through a server
page. The README says this, and the browser label in the device list
is there so the other person can take it into account.

## 15. Comparing codes

Two people can check that nobody stands between them by comparing a
code, in person or on a call they trust.

**Each account's half.** From the account key a device has pinned for
an account:

    h = SHA-256("churr compare v1" || 0x00 || account_key
                || account_id || server name (UTF-8))
    repeat 5,200 times:  h = SHA-256(h || account_key)

Take the first 30 bytes of the result in six groups of five bytes.
Read each group as a big-endian number, take it modulo 100,000, and
write it as five digits. That is 30 digits.

**The code** is the two halves, the smaller first when compared as
text, shown as twelve groups of five digits, and as a QR code holding
the same. Both people's devices show the same 60 digits if and only
if each has pinned the other's true account key.

It is not shorter because it must not be. Each half depends on one
account only, so an attacker has to match a 30-digit half for a key
they do not control, which is out of reach. A single short code
computed over both keys together could be matched by trying many
keys on both sides at once.

**Marking.** When the codes match, each person marks the other as
verified on that device. The mark is kept on the device. It is not
synced, and a new device starts without it.

**What a match proves.** That the two devices hold the same account
keys for each other. Because every device in a list was put there by
a holder of that account's backup key, and every epoch names the
lists it was sealed to, it follows that the devices reading the chat
are ones the two accounts added. It proves nothing about the code a
browser is running or about who is holding the other phone.

**When the code changes.** After a key change (section 13.6) the
code is different and the mark stays, because the old key signed the
new one. After a reset the mark is removed and the people should
compare again.

## 16. What the server does, sees, and can do

**Its job.** It stores and serves: device lists, epoch records,
sealed messages, encrypted blobs, locked copies, the account record,
backup records and unlock requests. It assigns sequence numbers,
enforces membership and one record per key epoch, applies retention,
and sends wake-ups. It never holds a key that opens any of it.

**What it sees.**

- Which two accounts have a sealed chat, and when it was made.
- For every item: the sending account and device, the time, the
  length, the key epoch, and whether it is quiet.
- That a row was deleted by its author, and which blobs belong to
  which message.
- Each account's devices: how many, their kinds, when each was added
  and removed, and their public keys.
- Every epoch record apart from the sealed secrets: when it was
  made, by which device, and for which devices.
- That an account has a backup, how many ways unlock it, how many
  records it has per chat, and when each was written.
- Who downloads which blob and when; who is in a call and for how
  long.

**What it cannot do** without breaking one of the primitives: read
text, files, names, reactions, edits, ticks or thread links; decode
call media; make a message appear to come from a device; change a
message; open or make a backup record; sign a device list; make an
epoch record.

**What it can do, and what comes of it.**

| The server…                              | Result                                               |
|------------------------------------------|------------------------------------------------------|
| Drops or delays messages                 | They do not arrive, or arrive late. A gap in a device's counters shows a drop in the middle; a drop at the end shows nothing. |
| Replays a message                        | Refused: the same device and counter at a second sequence number (section 8.2). |
| Delivers out of order                    | Shown with a mark (section 8.2).                     |
| Replays, reorders or drops epoch records | A record must have the next number and the hash of the one before, so a replayed or reordered one is refused. A dropped one stalls the chat for devices that did not get it. |
| Stores two records for one epoch, or a bad one | Devices refuse and make the next epoch (section 7.4). Service is disrupted; nothing is read. |
| Withholds a device list change           | Not detected at once. A device its owner removed keeps receiving until an epoch is made without it, which the owner's own devices do themselves. An honest server refuses messages in the stale epoch (section 5.5); a dishonest one does not. |
| Gives a fake device list at first contact | **Works**, until the two people compare codes. This is trust on first use. |
| Gives a fake list later                  | Refused, unless it is a reset, which is shown loudly and removes a verified mark. A person who ignores the notice is then talking to the server. |
| Adds a device of its own to an epoch     | It cannot. The device set is the signed lists', and the record is signed by a listed device. |
| Serves old or no backup records          | History is lost on new devices. Nothing is revealed. |
| Serves an old account record or locked copy | Refused if the device has seen a higher revision or version. A device with no memory can be given the state from before a key replacement, which helps only someone who already held the old key. |
| Seals a key of its own into a locked copy | A device that has a pin refuses it. A device with no pin can be led into a made-up account that the other person's devices see as a reset. |
| Offers its own unlock public key under a passkey's name | **Works** if the person approves it (section 13.4). |
| Serves altered pages                     | **Works** against the web chat, and against passkey unlocking on any client (sections 13.3 and 14). |
| Refuses service                          | The chat stops. Messages already on devices stay readable there. |

An admin can turn sealed chats off for the server. Existing ones
become read-only, retention still deletes their ciphertext, and the
export leaves them out (`DESIGN.md` section 14).

## 17. Sealed chats across servers

A sealed chat between accounts on two linked servers is allowed
while the two share a shared channel (`DESIGN.md` section 11). It is
a stream of kind `sealed` over the same peer, carried in the same
envelopes as any stream.

**What stays at home.** Each account's devices, locked copies,
account record, backup records and unlock requests exist only on its
own home server. They never cross the link.

**Device lists.** When the stream is made, and at every change after,
an account's home server sends the list's new versions to the other
server, which stores them against the shadow account in
`device_lists` and serves them to its own member's devices. A server
can also ask the other for a range of versions it lacks. The lists
are signed by the account key, so neither server and no hub can
change one without the checks of section 4.4 failing. First contact
is still trust on first use, now with two servers and perhaps a hub
in a position to substitute a list, and comparing codes closes it in
the same way.

That is all a device needs from the other side. An epoch is sealed
to the box keys in the lists, so nothing is requested from the other
person's devices or their server when a chat is created or an epoch
is made.

**Epoch records, messages, blobs.** Epoch records and messages each
travel as a stream event, and every server in the stream stores the
epoch records. Encrypted blobs do not travel in the stream: the
receiving server fetches each from the origin server, as any shared
file is fetched, and checks it against the hash in the signed event
(`docs/LINK-PROTOCOL.md`). Each server stores its copy.

**Ordering.** Like every shared conversation, a sealed chat across
servers is ordered by the shared clock (`DESIGN.md` section 11): each
event carries its origin's stamp, and nobody hands out positions.
Each server applies the rules of section 5.5 when it stores an event,
without being able to read it: a message whose key epoch is not the
chat's current one is dropped, and if it came from that server's own
member the device is told to send again. If devices on the two
servers each made the same key epoch before either saw the other's
record, each server first stores its own; the record with the earlier
stamp then stands on both and replaces the later one, and devices are
told with `sealed.epoch_started` carrying `replaced`
(`docs/LINK-PROTOCOL.md` section 11). The devices apply the same
rules and do not depend on the servers having done so. Each server's
own `seq` still numbers the rows it stores, and a device checks rows
against those numbers as section 8.2 says.

**What each party sees.**

| Party                      | Sees                                                    |
|----------------------------|---------------------------------------------------------|
| Your home server           | Everything in section 16 about you. About the other person: their name and server, their device list, and the time, length and device of each item they send. |
| The other person's server  | The same, the other way round.                          |
| A hub                      | Which servers exchange how much and when. The envelope is encrypted between the servers, so not even the account names. |

Neither server and no hub can read content: it is encrypted to the
two people's devices before any server sees it.

**Calls.** A call lives on the server of whoever started it
(`DESIGN.md` section 13). Frames are encrypted as in section 10, so
that server forwards what it cannot decode.

**When the shared channel goes.** When the two people no longer share
a channel, or the peer is removed, the servers refuse new items in
the chat. Devices keep their keys and what they have stays readable.

**Retention.** Each server deletes its own copy on its own schedule,
and cleans up its own member's backup records to match
(section 13.2).

## 18. Implementations and test vectors

| Implementation            | Where                         | Does                         |
|---------------------------|-------------------------------|------------------------------|
| Dart                      | The apps                      | Everything in this document  |
| JavaScript on Web Crypto  | The web chat, and the server's sign-in and unlock pages | Everything |
| Swift on CryptoKit        | The iPhone notification extension | Opening one message: HKDF, one Ed25519 verification, one AES-GCM decryption |
| Go, standard library only | The server repository, used only by tests | Everything. It is the reference, and it writes the test vectors |

The running server contains none of the code that handles secrets.
It has only a reader for the parts of records it is allowed to see
(sections 4.5, 5.5 and 8.2).

**Test vectors** live in `testdata/sealed/` as JSON files with bytes
in hexadecimal. The Go reference writes them; a check on every push
writes them again and fails if any byte differs. Every implementation
must pass every vector that covers what it does, and none is finished
until it does. A change to this document that changes any byte comes
with new vectors in the same change.

**Making output repeatable.** Seals, locks, epoch secrets and file
keys use random values. For tests, each implementation can be handed
those values (one-time private keys, salts, secrets) in place of the
random generator, through an entry point that exists only in test
builds. Signatures are the exception: vectors give signatures to
verify, and a signature an implementation makes is checked by the
others, never compared byte for byte (section 2.4).

Vectors that must exist:

1. **Primitives.** X25519, including a public key from a private key
   through the base point and the refusal of an all-zero result;
   HKDF-SHA-256; AES-256-GCM; SHA-256; Ed25519 verification. Known
   published vectors, to prove each platform's library is being
   called correctly.
2. **The record encoding.** Each type and each record of this
   document, fields to bytes and back. Refusals: bytes left over, a
   record cut short, an unsorted list, a repeated key, a choice out
   of range, invalid UTF-8, a record over 64 KB.
3. **Seal.** Recipient key, one-time key, label, context and
   plaintext to output, and back. Refusals: any changed byte, a
   different label or context, an all-zero shared secret.
4. **Lock.** Key, salt, label, context and plaintext to output, and
   back. The same refusals.
5. **Signatures.** For every label in this document: bytes, public
   key and a valid signature; and proof that the same signature is
   refused under every other label.
6. **Device lists.** A chain with an ordinary change, a key change
   and a reset; each list's hash; every proof. One refusal for each
   rule of section 4.4.
7. **Epoch records.** From given lists, secret and one-time keys to
   exact bytes; every value of section 5.2 derived from it. One
   refusal for each numbered rule of section 5.4.
8. **Sealed messages.** Header, salt and content to bytes, for each
   kind of section 8.3. Refusals: wrong epoch, a device that is not a
   recipient, a bad signature, a changed flag, a replay, and the
   out-of-order mark.
9. **Files.** Files of 0, 1, 65,535, 65,536, 65,537 and 200,000
   bytes. Refusals: chunks reordered, a file cut short, a missing
   last-chunk mark.
10. **The backup.** An account record; a backup record; a locked
    copy for each of the three ways; an unlock key pair from a given
    PRF output; twelve words to 16 bytes to a key pair, and a phrase
    with a bad checksum; the hand-over to an app; the two-layer
    pairing hand-over; an unlock request and its 20-character code.
11. **The comparison code.** Two account keys to 60 digits.
12. **Call keys.** The call secret and call sender keys. Once
    section 10's list has been verified: sample encrypted frames for
    each codec.
13. **A whole conversation.** Two accounts with several devices
    each, as every byte the server would store, in order, with what
    each device must be able to read and must show after each step.
    It includes: creation; messages from every device; a device
    added, reading the backup and making an epoch; a device removed;
    an epoch made for age; two devices making the same epoch at once;
    an account with no device for two epochs, then a history share;
    a key replacement; a reset; and a refused record followed by its
    repair.

## 19. Properties claimed

Each holds only if every implementation matches this document and
the test vectors, the client code is the published code, and the
review of section 21 finds no flaw.

1. **Content is secret from the servers, hubs and network.** Text,
   entities, file contents, file names and types, captions,
   reactions, edits, deletes, checklist ticks and thread links are
   readable only with a history secret, which only the two accounts'
   listed devices and backup keys give.
2. **Messages are authentic and whole.** Each is signed by a device
   whose key is in a signed list, and is bound to its chat, key
   epoch, sender, counter and reference. The server cannot alter
   one, move one to another chat or epoch, or point an edit,
   deletion or reaction at a different message.
3. **Replays are refused and reordering is shown.**
4. **The server cannot choose who an epoch is sealed to.** The device
   set is the two signed lists', the record is signed by a listed
   device, and the records form a chain.
5. **After first contact, no device joins an account quietly.** A
   device gets into a signed list only by holding the account's
   backup key. Anything else is a reset, which both people are told
   about.
6. **With codes compared, first contact is covered too.**
7. **A removed device reads nothing sent after the next epoch,**
   provided the removal replaced the backup key or the device can no
   longer fetch from the server. On an honest server nothing is sent
   between the removal and that epoch.
8. **The server cannot open, forge or move backup records.**
9. **One leaked history secret exposes one epoch of one chat.**
10. **The server has nothing to search or export** from a sealed
    chat.
11. **Taking over an account through recovery does not open its
    sealed history,** and shows as a reset to the other person.

About call media this section claims nothing beyond what section 10
states. The intent is that the forwarding server cannot decode it and
that it cannot be recovered from the backup; the frame format this
rests on is `docs/CALL-FRAMES.md`, and it is among the things for
outside review (section 21).

## 20. Properties not claimed

- **Forward secrecy.** Not within an epoch, not across epochs against
  a holder of the backup key, and not against a copied device, whose
  box key opens every stored epoch record sealed to it.
  Section 12.4.
- **Recovery after a compromise without anyone acting.** A new epoch
  shuts out only someone who learned an epoch's secret and nothing
  else. A copied device key or backup key stays good until a person
  removes the device or replaces the keys (section 13.6).
- **A published analysis.** This exact protocol has none.
- **Safety from a server that serves altered pages.** This covers the
  web chat entirely (section 14). It also covers passkey unlocking
  everywhere: a passkey can only be used on a page the server
  supplies, so the PRF output, the unlock key and, at the moment of
  unlocking, the backup key pass through server-supplied code, for
  the apps as well. An app whose owner unlocks only with the phrase
  or from another app never exposes its keys to such a page.
- **Independence from the passkey store.** A synced passkey's PRF
  secret is held by whoever syncs it. Someone who can obtain the
  passkey from that store, and the locked copy from the server, has
  the backup key.
- **A separate root for identity.** The backup key opens the history
  and holds the account key. Whoever has it can read everything and
  add devices. The two are not kept apart.
- **Hiding who talks to whom, when, how often or how much.** Nor
  message lengths, file sizes, device counts and kinds, when epochs
  change, which items are quiet, or that a deletion happened.
- **Protection from the other person.** They can keep, copy and show
  what they received. Signatures let them prove who wrote it.
- **Erasure.** A deleted message's ciphertext is erased on an honest
  server. An edited message's earlier ciphertext stays until
  retention. Copies on the other person's devices are theirs.
- **First contact without comparing codes.** It is trust on first
  use.
- **Freshness.** A server can hold back a newer device list or the
  last messages of a chat without being noticed at once.
- **Availability.** A server can stop a sealed chat working.
- **Per-device proof of who sent a call frame.**
- **Verified marks that follow a person** from device to device.
- **Resistance to a quantum computer.** X25519 and Ed25519 have none.

**Compared with MLS.** The standard protocol for this job is MLS
(RFC 9420), and an earlier version of this design used it. It was
dropped for three reasons: a sealed chat is always two accounts with
a handful of devices, which is far from the large groups MLS is built
for; the earlier design already encrypted content outside MLS, so
that a later device could read earlier messages, and therefore
already went without MLS's per-message keys; and a full MLS stack
cost a second toolchain, native builds on five platforms and a
compiled module for browsers. What MLS would have given and this
scheme does not: a key per message that is deleted after use, so
that a later leak cannot open earlier messages of the same epoch;
members that replace their own encryption keys as they go, so that a
copied device key stops working once its owner next updates, and
cannot open earlier epochs; a design with published security
analyses and several independent implementations. Here forward
secrecy inside an epoch is absent, healing after a compromise happens
only at the next epoch and only against a leaked epoch secret, and
the protocol has been examined by nobody but its authors. What this
scheme gives in return is that it is small enough to be read in one
sitting, implemented on each platform with that platform's own
library, and tested byte for byte against one reference.

## 21. Needs outside review

This matters more for a scheme of Churr's own than it would for a
standard one. Nobody should rely on a sealed chat for something that
matters until a cryptographer who did not write this has examined at
least these.

1. **The scheme as a whole**: whether sealing a fresh secret to
   long-lived device keys, with weekly epochs, is an acceptable
   design for its purpose, and whether a cheap addition (devices
   replacing their box keys on a schedule, for one) would recover
   something worth having.
2. **Seal** (section 2.5): the key derivation, its salt and info, a
   nonce taken from the same derivation, and the absence of sender
   authentication.
3. **Lock** (section 2.6): a random salt in place of a nonce, a key
   per encryption, and the claim that this removes AES-GCM's usual
   limits.
4. **The record encoding and domain separation** (sections 2.3 and
   2.4): that no two records can be confused, and that labels and
   contexts cannot run into each other.
5. **Epoch records** (section 5): the chain, the device-set rule, the
   `check` value and what publishing it gives away, the checks a
   device makes, and the treatment of refused records.
6. **Races and ordering** (sections 5.5, 7 and 17): one record per
   epoch enforced by the server on one server and by sequence order
   across servers; the stale-epoch rule; the seven-day rule and its
   reliance on device clocks.
7. **The key tree** (section 5.2): the labels, the shared salt, and
   the separation of call keys from the backup.
8. **Message encryption** (section 8.1): a key per message from the
   sender key, salt and header; signing over the ciphertext; counters
   and replay.
9. **The device list** (section 4): an account key kept in the
   backup, the three kinds of step, pins, and what a reset allows.
10. **Locked copies** (section 13.3): a key pair derived from a PRF
    output, and the signature check that depends on a record opened
    with the key being checked.
11. **PRF use**: a fixed input for every account, behaviour of synced
    and cross-device passkeys, and what the hand-over to an app
    through a page exposes.
12. **The phrase**: 12 words and no slow derivation.
13. **Replacing the backup key** (section 13.6): the order of steps,
    what happens if it stops halfway, and what is claimed after it.
14. **Approval between devices** (section 13.7): the 100-bit code and
    the two-layer hand-over used with QR pairing.
15. **Sharing missed epochs** (section 12.5), and the rule that
    nothing crosses a reset.
16. **File encryption** (section 9): the nonce layout and the
    last-chunk mark.
17. **Call frames** (section 10, `CALL-FRAMES.md`): how Churr's keys
    are fed to the library's format, AES-128-GCM there, nonce
    uniqueness, the key index not being authenticated, and the change
    of keys mid-call.
18. **The comparison code** (section 15).
19. **Keys shared with the notification extension** (section 11), and
    browser key storage (section 14).
20. **The implementations**: four of them, in four languages, each a
    chance to differ. The test vectors, whether they cover enough,
    and the server's reader for untrusted records.

## 22. To verify

Facts this design leans on that have not been confirmed.

**Dart.**

- Which of the `cryptography` package (2.9.0) and its fork
  `cryptography_plus` (3.0.0) is maintained, and which to depend on.
- That the chosen one provides X25519, Ed25519, HKDF-SHA-256 and
  AES-256-GCM on Android, iPhone, macOS, Windows and Linux, whether
  it uses the platform's own implementations there or pure Dart, and
  how fast the pure Dart ones are for a 100 MB file.
- That it can take a given private key and nonce, for the test
  vectors.

**Web Crypto.**

- X25519 and Ed25519. A search on 13 November 2026 found that
  current Chrome, Firefox and Safari all ship both, without a
  confirmed Baseline date. The oldest version of each that the web
  chat can support has to be found and stated.
- That an X25519 private key can be imported from 32 derived bytes
  (through a PKCS#8 wrapper, since raw import is for public keys),
  and that a public key can be computed from it, in all three.
- That each engine refuses an all-zero X25519 result, or lets the
  page check for it.
- That HKDF gives 44 bytes of output with the salts used here.
- That keys made non-exportable can be stored in IndexedDB in all
  three, and what each does to stored data for a site left unvisited.
- How the web chat keeps several open tabs from making epochs or
  spending counters at once.

**CryptoKit and Go.**

- That CryptoKit verifies Ed25519 signatures made by the other
  implementations, and whether its own are deterministic.
- The memory and time limits of an iPhone notification extension.
- That HKDF is in the Go standard library version the server builds
  with, so the reference needs nothing outside it.

**The call frame format.** Settled in `docs/CALL-FRAMES.md`. What it
marks as the library's documented behaviour and not observed: that a
frame which fails to decrypt is dropped before the decoder in the
apps, the readable bytes of H.264 (not used), and the format on
platforms other than macOS. That the apps forget a replaced epoch's
keys after ten seconds (they overwrite them, the key provider having
no way to remove one) has not been observed on the library either.

**Passkeys and PRF.** Checked on 13 November 2026: PRF is confirmed
for Apple's and Google's passkey stores, partial for Firefox, and
unconfirmed for Windows Hello, hardware security keys and some
third-party passkey managers. Still to check:

- whether each store returns PRF output at creation, or only at a
  later use;
- whether PRF works when a phone's passkey is used on another
  computer through the QR-code route, and gives the same output as
  on the phone;
- whether PRF works in the system browser sheets the apps open on
  iPhone and Android;
- whether a synced passkey gives the same PRF output on every device
  it syncs to.

**The server.**

- That the SQLite and PostgreSQL stores can swap an account's backup
  rows in one transaction of the size section 13.6 needs.

## 23. What this uses, and the choices made

**In the schema.** Everything here is stored in tables `schema.sql`
has:

| Thing                                  | Where                              |
|----------------------------------------|------------------------------------|
| Device keys                            | `devices.sign_key`, `devices.box_key`; `session_id` (empty between sessions), `retired_at` |
| Every version of each device list      | `device_lists` (`account_id`, `version`, `signed_list`) |
| Epoch records, one per key epoch       | `sealed_epochs` (`conversation_id`, `key_epoch`, `seq`, `device_id`, `record`) |
| A chat's current key epoch             | `conversations.key_epoch`          |
| Sealed messages                        | `messages.ciphertext`, `key_epoch`, `client_id`, `quiet` |
| Encrypted blobs                        | `files`, tied to their message by `message_id` |
| Locked copies                          | `backup_keys` (`kind` of `passkey`, `phrase` or `device`; `passkey_id`, `device_id`, `key_version`, `locked_key`) |
| A passkey's unlock public key          | `passkeys.unlock_public`           |
| The account record                     | `account_records` (`key_version`, `revision`, `record`, `replacement_due`) |
| Backup records                         | `key_backups` (`conversation_id`, `key_epoch`, `key_version`, `record`) |
| Unlock requests and their answers      | `unlock_requests` (`request`, `answer`, `expires_at`) |
| The hand-over to an app                | `oauth_codes.box_key`, `oauth_codes.handover` |

**From the API.**

- Start a sealed chat.
- Publish a device list version; fetch a range of versions for an
  account.
- Post an epoch record; list a chat's epoch records after a given
  key epoch.
- Post a sealed message with `client_id`, key epoch, quiet flag and
  blob identifiers; delete one's own message.
- Upload and download blobs.
- Register a device's keys; reattach a device to a new session by
  signing a challenge; remove a device.
- Fetch the account's locked copies and account record; create the
  backup; add a locked copy; update the account record; give a
  passkey's unlock public key.
- Write a backup record; list an account's backup records for a
  chat.
- Replace the backup key in one step; reset.
- Post an unlock request; list waiting ones; answer one; fetch the
  answer.
- The hand-over with the tokens at app sign-in and at pairing.
- Over the link: device list versions, pushed and asked for, and the
  stream events of section 17.

**Events.** `sealed.epoch_started`, a stored conversation event
carrying the record; `message.created` and `message.deleted` as for
any conversation; `device_list.changed`, to the account's own
sessions and to the other account of each sealed chat; an event to
the account's own devices when an unlock request arrives and one to
the asking device when it is answered; and an event to the account's
own devices when a passkey gains an unlock public key or
`replacement_due` is set. Nothing in this design needs a frame from
client to server on the live connection: every change is an API
operation.

**Choices this document made** where the design was silent:

1. Signed and hashed records use a fixed binary encoding, not JSON.
2. One seal and one lock construction, each giving AES-GCM a key
   used once.
3. An account key, kept in the backup, signs the device list.
4. A device is either unlocked and listed, or neither.
5. An epoch's device set is exactly the two named list versions.
6. Epoch records carry a `check` value, and form a hash chain that
   includes refused records.
7. The backup holds the history secret, one step below the epoch
   secret, so that call keys cannot be derived from it.
8. A key per message, from a per-device sender key, a random salt
   and the header.
9. Message references made by the sender, and per-device counters,
   for integrity and replay.
10. The server refuses messages in an epoch whose lists are out of
    date.
11. A poster of an epoch record need not be its maker, so devices
    can repair a restored server.
12. A delete also erases the target row on the server; an edit does
    not.
13. Thumbnails are made and encrypted by the sending device.
14. Locked copies are sealed to public keys and signed.
15. The phrase is 12 BIP-39 words, stretched with HKDF only, and
    survives a key replacement.
16. The backup key and account key are replaced on passkey removal,
    device revocation and phrase replacement.
17. Device approval uses a 100-bit code or QR; QR pairing can unlock
    in the same scan.
18. Missed epochs are shared by the other person's device, checked
    against the epoch record, and never across a reset.
19. Epoch records and backup records are deleted once retention has
    emptied their epochs, oldest first.
20. A device outlives its session by up to 30 days.
21. The comparison code is 60 digits, built from each account's key
    separately, with SHA-256.
22. Verified marks stay on the device, and the web chat stores no
    decrypted messages.
