// What the app writes for the iPhone notification extension
// (ios/NotificationService) to read: the list of servers with their wake
// routes, and for sealed chats the content key and senders' signing keys
// of each key epoch (docs/PUSH.md section 8, docs/SEALED.md section 11,
// docs/CLIENTS.md section 5). The format is set out in
// app/docs/NOTIFY-SHARED.md; change both together, and the Swift reader
// (ios/NotificationService/Sources/ChurrNotify/SharedItems.swift).
//
// Items go through the app's [SecretStore], which on iPhone is the
// keychain group the app shares with the extension, readable after the
// first unlock (lib/platform/keychain_store.dart). Elsewhere they are
// harmless: Android's push handler is Dart and reads the app's own state.

import 'dart:convert';

import '../session_store.dart';

/// The item holding the server list.
const serversItem = 'churr.notify.servers';

/// The item listing every epoch item written, so they can be removed.
const epochIndexItem = 'churr.notify.epochs';

/// The item of one key epoch of one sealed chat on one server.
String epochItem(String serverLocalId, String conversationId, int keyEpoch) =>
    'churr.notify.epoch.$serverLocalId.$conversationId.$keyEpoch';

/// The item of a server's tokens (`SessionStore`).
String tokensItem(String serverLocalId) => 'churr.tokens.$serverLocalId';

/// One server as the extension needs it.
class SharedServer {
  const SharedServer({
    required this.localId,
    required this.address,
    required this.name,
    required this.route,
  });
  final String localId;
  final String address;
  final String name;
  final String route;

  Map<String, Object?> toJson() => {
    'id': localId,
    'address': address,
    'name': name,
    'route': route,
    'token_key': tokensItem(localId),
  };
}

/// Writes the server list. [showText] is the "show the message text"
/// setting; [multi] whether the server's name goes in the notification.
Future<void> writeSharedServers(
  SecretStore store,
  List<SharedServer> servers, {
  required bool showText,
}) => store.write(
  serversItem,
  jsonEncode({
    'v': 1,
    'show_text': showText,
    'multi': servers.length > 1,
    'servers': [for (final s in servers) s.toJson()],
  }),
);

/// A device that may send in an epoch, with its Ed25519 public key.
class SharedSender {
  const SharedSender({
    required this.accountId,
    required this.deviceId,
    required this.signKey,
  });
  final String accountId;
  final String deviceId;
  final List<int> signKey;
}

String _b64(List<int> b) => base64Url.encode(b).replaceAll('=', '');

/// Writes what the extension needs to read messages of one key epoch:
/// called by the sealed-chat code whenever it accepts an epoch record
/// (docs/SEALED.md section 11, step 3). [contentKey] is the epoch's
/// content key (section 5.2), never the epoch or history secret;
/// [epochSeq] the sequence number of the epoch record in the chat, so a
/// message from before it is refused; [senders] every recipient device of
/// the epoch with its `sign_key` from the device-list version the record
/// names.
Future<void> shareEpochForNotifications(
  SecretStore store, {
  required String serverLocalId,
  required String conversationId,
  required int keyEpoch,
  required int epochSeq,
  required List<int> contentKey,
  required List<SharedSender> senders,
}) async {
  final key = epochItem(serverLocalId, conversationId, keyEpoch);
  await store.write(
    key,
    jsonEncode({
      'v': 1,
      'conversation_id': conversationId,
      'key_epoch': keyEpoch,
      'epoch_seq': epochSeq,
      'content_key': _b64(contentKey),
      'senders': [
        for (final s in senders)
          {
            'account_id': s.accountId,
            'device_id': s.deviceId,
            'sign_key': _b64(s.signKey),
          },
      ],
    }),
  );
  final index = await _index(store);
  if (index.add(key)) await store.write(epochIndexItem, jsonEncode([...index]));
}

Future<Set<String>> _index(SecretStore store) async {
  try {
    return {
      ...(jsonDecode(await store.read(epochIndexItem) ?? '[]') as List)
          .cast<String>(),
    };
  } on Object {
    return {};
  }
}

/// Removes every epoch item of a server (signing out of it), or of one
/// conversation on it.
Future<void> forgetSharedEpochs(
  SecretStore store,
  String serverLocalId, {
  String? conversationId,
}) async {
  final prefix = conversationId == null
      ? 'churr.notify.epoch.$serverLocalId.'
      : 'churr.notify.epoch.$serverLocalId.$conversationId.';
  final index = await _index(store);
  final gone = {
    for (final k in index)
      if (k.startsWith(prefix)) k,
  };
  for (final k in gone) {
    await store.delete(k);
  }
  if (gone.isNotEmpty) {
    await store.write(epochIndexItem, jsonEncode([...index.difference(gone)]));
  }
}
