# Interfaces the notifications code shares with sealed chats and calls

Written by the notifications area (app/STATUS-notify.md while it lasts).
Two other areas plug into it; this file says how. Change it only
together with the code it names.

## 1. Keys the iPhone notification extension reads (sealed chats)

The extension (`ios/NotificationService`) decrypts a sealed message for
its notification (docs/SEALED.md section 11, docs/PUSH.md section 8). It
cannot reach the app's sealed store, so **the sealed-chat code writes,
whenever it accepts an epoch record, what the extension needs for that
epoch**, by calling one Dart function:

```dart
import 'package:churr/core/notify/shared_items.dart';

await shareEpochForNotifications(
  app.sessions.secrets,          // the app's SecretStore (KeychainStore)
  serverLocalId: server.localId,
  conversationId: chatId,
  keyEpoch: epoch,               // the record's key_epoch
  epochSeq: recordSeq,           // seq of the epoch record in the chat
  contentKey: contentKey,        // SEALED.md 5.2 content key, 32 bytes
  senders: [                     // every recipient device of the epoch
    SharedSender(accountId: a, deviceId: d, signKey: signKey32),
  ],
);
```

and `forgetSharedEpochs(store, serverLocalId, conversationId: …)` when a
chat is left (signing out of a server removes them all; the
notifications code does that).

Only the **content key** is shared: never the epoch secret, the history
secret, a private key or the backup key. The extension derives each
sender key from it (`HKDF(content key, conversation_id || key_epoch,
"churr sender v1" || 0x00 || device_id)`), checks the signature with
the sender's `sign_key`, and decrypts. It changes no key.

### The items

Keychain generic-password items, written through `flutter_secure_storage`
(service `flutter_secure_storage_service`, account = the item name,
value = UTF-8 JSON), accessible after first unlock, in the keychain
access group `$(AppIdentifierPrefix)example.notfinal.churr.shared`
(the first group of both targets' entitlements, so the app writes there
without naming it). Bytes are base64url without padding; identifiers are
lower-case hyphenated UUIDs.

| Item | Value |
|------|-------|
| `churr.notify.epoch.<server local id>.<conversation id>.<key epoch>` | `{"v": 1, "conversation_id", "key_epoch", "epoch_seq", "content_key", "senders": [{"account_id", "device_id", "sign_key"}]}` |
| `churr.notify.epochs` | JSON list of the epoch item names written (for removal) |
| `churr.notify.servers` | `{"v": 1, "show_text", "multi", "servers": [{"id", "address", "name", "route", "token_key"}]}`, written by the notifications code |
| `churr.tokens.<server local id>` | The app's tokens (`SessionStore`), `{"access", "refresh", "expires"}` |

The extension refuses a message unless: the format is 1; the header's
conversation, key epoch and sender account equal the row's; the sender
device is in `senders` with that account; the row's `seq` is after
`epoch_seq`; the signature verifies; the content decrypts; the row's
`quiet` matches the header's flag. It shows only `kind: "message"`
content. Counter checks are left to the app (SEALED.md section 11).

## 2. Incoming calls (calls)

The calls code owns the incoming-call prompt and the ring while the app
is in front (`CallService` in `lib/state/call_service.dart`). The
notifications code owns the system notification when the window is not
in front, and wake-ups on phones. It joins the two in
`lib/platform/notify_calls.dart`, using only these names of the calls
code, so the calls code has nothing to call:

- `CallService.of(app).showIncomingCall(serverId:, conversationId:,
  callId:)`: called when a ring notification is tapped, or a phone is
  woken for a ring that is still ringing;
- `CallService.incoming` (`IncomingCall`: `server`, `conversationId`,
  `callId`, `fromAccountId`, `silent`) and `CallService.active`
  (`conversationId`), listened to: a ring shown while the window is not
  in front also gets a system notification with the ring sound
  (`NotifyCenter.showRing`), which becomes "Missed call from …" when the
  ring stops unless this device joined (`NotifyCenter.stopRing`).

Rename any of these only together with that file.

## 3. Device registration

Both areas call `registerDevice`. The server leaves a field it is not
sent as it is, so each sends only its own: the notifications code sends
`name`, `platform`, `push_kind` and `push_address` (and the Web Push
keys); the sealed-chat code sends `sign_key` and `box_key`.
