# Test vectors for sealed chats

These files are the test vectors of `docs/SEALED.md` section 18. The Go
reference in `internal/sealed/ref` writes them from fixed inputs, and a
test fails if the committed files differ from what it writes:

    go test ./internal/sealed/ref -run TestVectors            check
    go test ./internal/sealed/ref -run TestVectors -update    rewrite

Every other implementation (Dart in the apps, JavaScript on Web Crypto in
the web chat and the server's pages, Swift in the iPhone notification
extension) is written against `docs/SEALED.md` and these files, and is not
finished until it passes every vector that covers what it does. Section
numbers below are sections of `docs/SEALED.md`.

Do not edit a file here by hand. A change to the document that changes
any byte comes with new vectors in the same change.

## 1. Format, for every file

- Each file is one JSON object in UTF-8, with a `description` saying what
  it holds. Clients ignore fields they do not know.
- **Bytes are lower-case hexadecimal** strings with no prefix and no
  separators. An empty byte string is `""`. (The API carries bytes as
  base64url; these files do not.)
- Identifiers are UUIDs in the usual lower-case hyphenated form. In a
  record they are the identifier's 16 bytes.
- Times are whole numbers of milliseconds since the Unix epoch.
- Whole numbers are JSON numbers. None exceeds 2^53.
- `fields` beside a record's bytes gives the record's fields by the names
  the document uses, for building the record; the bytes are what must be
  reproduced.
- A list called `refusals` holds inputs that **must be refused**. Each has
  a `why`. An implementation that accepts one is wrong, however it fails
  is its own business.
- Labels are ASCII strings exactly as written, with no terminator. Where
  the document says `label || 0x00 || …`, one zero byte follows the label.

### Random values

Seals, locks, epoch secrets, file keys, message salts and references use
random values. In these files each such value is **given**: wherever a
construction needs a random value the vector names it (`one_time_private`,
`salt`, `epoch_secret`, and so on). An implementation under test must have
an entry point, in test builds only, that takes these values in place of
its random generator.

The values were themselves made by a fixed stream (block `i` is
SHA-256(`"churr test stream v1"` || 0x00 || seed || `i` as a `u64`)). No
implementation needs to reproduce that stream.

### Signatures

Ed25519 as RFC 8032 specifies it is deterministic, and the Go reference
signs that way, so the signatures in these files are stable. **But a
signature is only ever verified, never compared** (section 2.4): not every
platform signs deterministically. So:

- where a vector gives a signature, verify it;
- where an implementation makes a record that ends in or contains a
  signature (a signed device list, a stored epoch record, a sealed
  message, a locked copy, a device proof), compare every byte except the
  signature with the vector, and check that the other implementation's
  signature verifies. An implementation whose signatures are deterministic
  will match the whole record.

## 2. The files

| File | Section | What it covers |
|------|---------|----------------|
| `primitives.json` | 2.2 | Published vectors for X25519, HKDF-SHA-256, AES-256-GCM, SHA-256 and Ed25519 |
| `encoding.json` | 2.3 | Each type of the record encoding; records a reader must refuse |
| `seal.json` | 2.5 | Seal to a public key |
| `lock.json` | 2.6 | Lock under a secret key |
| `signatures.json` | 2.4 | A signature under every label, refused under every other; hashes |
| `device_list.json` | 4 | A chain of four list versions; device proofs; one refusal per rule of 4.4 |
| `epoch_record.json` | 5 | An epoch record from its inputs; everything derived from it; one refusal per rule of 5.4 |
| `message.json` | 8 | A sealed message of each kind; what a receiving device makes of a run of rows |
| `file.json` | 9 | File chunks |
| `account_record.json` | 13.1 | The account record |
| `backup_record.json` | 13.2 | A backup record |
| `locked_copy.json` | 13.3 | A locked copy for each of the four ways |
| `unlock_keys.json` | 13.3 | Unlock keys from a PRF output, from the phrase and from an access key; the phrase itself |
| `handover.json` | 13.3, 13.7 | The hand-over to an app at sign-in, and the two-layer one of QR pairing |
| `unlock_request.json` | 13.7 | The unlock request, its 20-character code, the answer |
| `comparison_code.json` | 15 | Two account keys to 60 digits |
| `call_keys.json` | 5.2, 10 | The call secret and call sender keys |
| `transcript.json` | all | A whole conversation, byte for byte |

### primitives.json

- `x25519`: `private`, `public` → `shared`; an entry with `"refused":
  true` gives an all-zero result and must be refused.
- `x25519_public`: `private` → `public` (the function applied to the base
  point, 9).
- `hkdf_sha256`: `ikm`, `salt`, `info`, `length` → `okm`.
- `aes_256_gcm`: `key`, `nonce` (12 bytes), `plaintext`,
  `associated_data` → `ciphertext`, which is the ciphertext with its
  16-byte tag appended.
- `sha256`: `message` → `digest`.
- `ed25519`: verify `signature` over `message` under `public`; the answer
  is `valid`. Where `seed` is given, the public key is the one of that
  32-byte seed.

### encoding.json

- `types`: one entry per type of section 2.3, `value` (as text) → `bytes`.
- `accepted`: records that parse. `refusals`: records a reader must
  refuse, with `record` saying which reader to give them to
  (`unlock_request` of section 13.7 or `device_list` of section 4.3) and
  `why`: bytes left over, cut short, a choice out of range, a list out of
  order, a key twice, invalid UTF-8, longer than 64 KB.

### seal.json and lock.json

- `seal.json` `vectors`: `recipient_public` (or `recipient_private`, from
  which it comes), `one_time_private`, `label`, `context`, `plaintext` →
  `sealed`, and back with `recipient_private`. `refusals`: open `sealed`
  with `recipient_private`, `label` and `context`; it must not open.
- `lock.json` `vectors`: `key`, `salt`, `label`, `context`, `plaintext` →
  `locked`, and back. `refusals`: unlock `locked` with `key`, `label` and
  `context`; it must not open.
- `label` is text; `context` is bytes.

### signatures.json

- `signatures`: for each label, `signature` verifies over `label` || 0x00
  || `record` under `public` (`signed_bytes` is that concatenation), and
  must be refused when checked under each label in `refused_under`.
- `hashes`: `hash` is SHA-256(`label` || 0x00 || `record`).

### device_list.json

- `chain`: versions 1 to 4 of one account's list. Each has `fields`, the
  `list` record, `sig`, `prev_sig` when there is one, `signed_list` (the
  stored form of section 4.3), `hash` (of `list`, label `churr device list
  hash v1`), and `step`: `first`, `ordinary`, `key_change` or `reset`,
  which is what checking it against the version before must conclude.
  `account_key_seed` is the Ed25519 seed that signed it.
- `proofs`: device proofs, with the device's keys and the bytes signed.
- `refusals`: each `signed_list` is offered as the next version to a
  client whose pin is `pinned_list`, and must be refused. `rule` is the
  number of the rule of section 4.4 it breaks (1 the chain, 2 proofs and
  repeated keys, 3 signatures and the kind of step).
- `first_contact`: lists offered to a client with no pin; `accepted` says
  whether it pins them.

### epoch_record.json

- `devices`: the devices used, with their private keys. `lists_checked`:
  the device list versions the checking device has checked.
- `epoch_1.inputs` → `epoch_1.record`, `signature` and `stored` (record
  followed by signature); `hash` is of `stored`, label `churr epoch hash
  v1`. `inputs.one_time_private` gives, per recipient device, the one-time
  private key used to seal the epoch secret to it.
- `epoch_1.derived`: every value of section 5.2 for that epoch (the call
  keys are in `call_keys.json`).
- `epoch_2` follows `epoch_1` in the chain.
- `refusals`: each `stored` is offered as the record after
  `previous_stored` to `checking_device`, and must be refused under the
  rule of section 5.4 numbered `rule`.

### message.json

- `messages`: for each kind of section 8.3, `header` (fields and its 82
  `bytes`), `sender_key`, `salt` and `content` (the JSON text;
  `content_bytes` is its UTF-8) → `sealed`. `sign_key` verifies the
  signature.
- `receive`: rows given, in the order listed, to `receiving_device`, which
  holds `epoch_stored` as the chat's epoch 1 at sequence number
  `epoch_seq`. Each row has `seq`, `author_id`, `key_epoch`, `quiet` and
  `ciphertext`, as the server would give them, and the `outcome` the
  device must reach: `shown`, `dropped` (a replay), `refused` (shown as "A
  message could not be checked"), `shown_out_of_order`, or
  `shown_with_gap` ("A message may be missing here").
- `shown_after`: what the device draws once every row has been taken in.

### file.json

- The plaintext of a file of `size` bytes is the bytes `i mod 251` for
  `i` from 0 to `size` − 1.
- `files`: under `key`, the file encrypts to a blob of `blob_size` bytes
  whose SHA-256 is `blob_sha256`. Small blobs are given whole in `blob`;
  `blob_first_bytes` and `blob_last_bytes` are the first and last 32 bytes
  of any.
- A file of `n` bytes has max(1, ceil(n / 65536)) chunks: an empty file is
  one empty chunk, and a file whose length is an exact multiple of 65,536
  ends with a full chunk, not an empty one.
- `refusals`: blobs that must not decrypt. Each has `how` (how it was made
  from one of the files above), its size and SHA-256, and the blob itself
  when it is small.

### account_record.json, backup_record.json, locked_copy.json

- `account_record.json`: `fields` → `content`; `content` locked under
  `backup_key` with `salt` and `context` → `stored`.
- `backup_record.json`: the same for a backup record; `stored` is 97
  bytes. `opens_but_fails_the_epoch_check` is a record that opens and whose
  secret must then be refused because it does not match `epoch_check`.
- `locked_copy.json` `copies`: for each way (passkey, phrase, device and
  access key), the backup key sealed to
  `unlock_public` with `one_time_private` and `context` → `sealed` (80
  bytes); the account key's `signature` over `signed_record`; `stored` is
  the two together, 144 bytes. `opens_but_is_not_signed_by_the_account_key`
  opens with `unlock_private` and must be refused at the signature check.
- Each file's `refusals` are stored values opened under the wrong account,
  chat, key epoch, key version, revision or way, which must not open.

### unlock_keys.json

- `passkey`: `prf_output`, `account_id` and `credential_id` →
  `unlock_private` and `unlock_public`.
- `phrase`: `entropy` (16 bytes) ↔ `words`; `entropy` and `account_id` →
  `unlock_private` and `unlock_public`.
- `access_key`: the key's 32 `random` bytes → `wrap` (HKDF with
  `wrap_salt` and `wrap_info`, the derivation of `docs/AUTH.md` section
  5.6); `wrap` → `unlock_private` and `unlock_public` (the account is not
  mixed in).
- `phrases`: more `entropy` ↔ `words` pairs, the first four from BIP-39's
  own vectors.
- `reading`: what a person typed, and whether it is `accepted`; if so, the
  `entropy` it stands for. A typed word matches a list word if the two are
  equal, or if both have at least four letters and their first four
  letters are equal.

### handover.json, unlock_request.json, comparison_code.json, call_keys.json

- `handover.json`: `sign_in.handover` (80 bytes) opens with
  `app_box_private` and `code_challenge` to `backup_key`, and must not
  open with `refused_with_code_challenge`. `pairing.handover` (112 bytes)
  opens with `pairing_secret` and `app_box_private`, and must not open
  with `refused_with_pairing_secret`.
- `unlock_request.json`: `fields` → `request`; `code` is the first 100
  bits of `code_hash` as 20 characters of Crockford base32
  (`0123456789ABCDEFGHJKMNPQRSTVWXYZ`) in four groups of five joined by
  hyphens. Typed codes are compared after upper-casing, dropping spaces
  and hyphens, and reading `O` as `0` and `I` and `L` as `1`.
- `comparison_code.json`: each account's `half` (30 digits) and the
  `code`: the two halves, the smaller first as text, in twelve groups of
  five digits separated by single spaces.
- `call_keys.json`: `epoch_secret`, the conversation, the key epoch and
  the call → `call_secret`; that and each device → its call sender key.

## 3. The transcript

`transcript.json` is one conversation between two accounts, `ann` and
`bob`, on one server, played by seven devices (`devices`). It covers:
creating each account's backup; a device added by the recovery phrase, by
approval from another device, and by a passkey's PRF output; creating the
chat; messages from every device, a reaction, an edit, a checklist tick
and a delete; a device removed; an epoch made for age; two devices making
the same epoch at once; an account with no device for two epochs and the
history share that follows; a key replacement; a reset; and a record the
devices refuse, followed by its repair.

Each entry of `steps` is one step:

| Field | Meaning |
|-------|---------|
| `step` | Its number, from 1 |
| `what` | What happens, in words |
| `actor` | The device that acts, if one does |
| `clock` | Every device's clock during the step, in milliseconds |
| `random` | Every random value drawn during the step, in the order drawn: `device`, what it is `for`, and its `value` |
| `sent` | For a step that posts a message: its `content` (the JSON text that is encrypted), `reference`, `counter`, `key_epoch` and `quiet` |
| `stored` | What the server stored or deleted, in order (below) |
| `refused` | What the server or a device refused in this step, and `why` |
| `after` | For each device still in use: what it holds and shows once the step is over |

### What is drawn at random, and what for

| `for` | Bytes | Use |
|-------|-------|-----|
| `device_sign_seed` | 32 | The device's Ed25519 seed. Listed in the step the device first acts in |
| `device_box_private` | 32 | The device's X25519 private key |
| `backup_key` | 32 | A new backup key |
| `account_key_seed` | 32 | The Ed25519 seed of a new account key |
| `phrase_entropy` | 16 | The 16 bytes behind a new recovery phrase |
| `account_record_salt` | 16 | The salt the account record is locked with |
| `locked_copy_one_time:<way>:<id>` | 32 | The one-time private key a locked copy is sealed with; `<way>` is `passkey`, `phrase` or `device`, `<id>` the passkey, 16 zero bytes as a UUID, or the device |
| `epoch_secret:<key epoch>` | 32 | A new epoch secret |
| `epoch_seal_one_time:<key epoch>:<device>` | 32 | The one-time private key the epoch secret is sealed to that device with |
| `backup_record_salt:<conversation>:<key epoch>` | 16 | The salt a backup record is locked with |
| `message_reference` | 16 | The message reference. The reference is these 16 bytes made into a version 4 UUID: the high four bits of byte 6 are set to 0100 and the top two bits of byte 8 to 10 |
| `message_salt` | 16 | The salt of a sealed message |

A value drawn and then not used (the losing record of a race) is still
listed.

### `stored`

Each entry names a `table` of `docs/schema.sql` and gives the columns that
matter, with bytes in hexadecimal:

| `table` | Columns |
|---------|---------|
| `device_lists` | `account_id`, `version`, `signed_list` |
| `account_records` | `account_id`, `key_version`, `revision`, `record` |
| `backup_keys` | `account_id`, `kind`, `passkey_id` or `device_id`, `key_version`, `locked_key` |
| `key_backups` | `account_id`, `conversation_id`, `key_epoch`, `key_version`, `record` |
| `sealed_epochs` | `conversation_id`, `key_epoch`, `seq`, `record` |
| `messages` | `conversation_id`, `seq`, `author_id`, `client_id`, `key_epoch`, `quiet`, `ciphertext` |
| `unlock_requests` | `account_id`, `device_id`, `request`, `answer`, and `code_shown`, the code the asking device displays |

Three other kinds of entry appear: `{"deleted": [...], "account_id",
"because"}` when an account's backup rows are deleted for a replacement
or a reset; `{"erased": "messages.ciphertext", "seq"}` when a row's
ciphertext is erased because its author deleted it; and, in the step that
adds a passkey, an entry with the passkey's `prf_output` and
`credential_id`, which the server never holds and which are given so the
unlock key can be derived.

The server keeps the first backup record for an account, chat and key
epoch and ignores the rest, so a record a second device writes for the
same epoch is not in `stored`.

### `after`

For each device:

| Field | Meaning |
|-------|---------|
| `unlocked` | Whether it holds its account's backup key |
| `key_version` | The key version of the backup key it holds |
| `pins` | Per account, the newest device list version it has accepted |
| `epochs` | The chat's epoch records as it holds them: `key_epoch`, whether it `accepted` the record, and whether it `can_read` the epoch's messages (it has the history secret) |
| `shows` | The messages it draws, in sequence order: `reference`, `author_id`, `seq`, `text`, and `edited`, `deleted`, `out_of_order`, `reactions` (emoji → accounts) and `tasks` (item → checked) where they apply |
| `unchecked` | How many rows it shows as "A message could not be checked" |
| `notices` | Every notice it has shown so far: `kind` (`device_added`, `device_removed`, `key_change`, `reset`, `list_refused`, `out_of_step`), the `account_id` it is about, and for a device added its `platform` |

### Replaying it

An implementation can be checked against the transcript in two ways.

**As a reader.** Feed a device the `stored` entries in order (device
lists as they are published, then epoch records and messages by `seq`),
together with its own keys and, where the step says the device unlocks,
the means of unlocking named in `what`. After each step its state must
equal its entry in `after`. This needs no random values, and is what the
Swift decrypt and any read-only implementation should do with the
`messages` rows.

**As a writer.** Give the acting device the values of `random` for the
purposes named, and the step's `clock`. It must then produce the bytes in
`stored` (signatures apart; see above).

Two rules of the reference matter for a writer and change no byte another
implementation reads:

- A key replacement continues the account record's revision count (the
  new record's `revision` is the old one plus one); a reset starts it
  again at 1.
- A device makes a new epoch for age when the current record's `at` is
  more than seven days behind its clock, or more than five minutes ahead
  of it.
