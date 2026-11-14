// What the iPhone notification extension may read (docs/SEALED.md section
// 11, docs/CLIENTS.md section 5): for each key epoch of each sealed chat
// this device can read, the epoch's content key and the Ed25519 public
// key of every device in it. Nothing else: not the history or epoch
// secret, not the backup key, not this device's private keys.
//
// The items and their format are the notifications area's
// (app/docs/NOTIFY-SHARED.md section 1, lib/core/notify/shared_items.dart);
// this file only says when to write them: whenever the device accepts an
// epoch or learns its history secret, and gone when the secret is gone or
// on sign-out. The app writes them on iPhone only
// (lib/platform/sealed_store_file.dart); elsewhere [NoNotificationKeys].

import '../notify/shared_items.dart';
import '../sealed/sealed.dart';
import '../session_store.dart';

/// Where the items go.
abstract class NotificationKeyStore {
  /// Writes the item for epoch [e] of [chat] (which the device can read).
  Future<void> share(Chat chat, Epoch e);

  /// Removes one epoch's item.
  Future<void> forget(Chat chat, int keyEpoch);

  /// Removes every item of this server (signing out, or this device
  /// removed).
  Future<void> forgetAll();
}

/// For platforms with no extension.
class NoNotificationKeys implements NotificationKeyStore {
  const NoNotificationKeys();
  @override
  Future<void> share(Chat chat, Epoch e) async {}
  @override
  Future<void> forget(Chat chat, int keyEpoch) async {}
  @override
  Future<void> forgetAll() async {}
}

/// The epoch's item as shared_items.dart writes it.
List<SharedSender> sendersOf(Epoch e) => [
  for (final r in e.record?.recipients ?? const <Recipient>[])
    if (e.devices[r.deviceId] case final d?)
      SharedSender(
        accountId: '${r.accountId}',
        deviceId: '${r.deviceId}',
        signKey: d.signKey,
      ),
];

/// Writes through the app's [SecretStore] (on iPhone, the keychain group
/// shared with the extension).
class SharedNotificationKeys implements NotificationKeyStore {
  SharedNotificationKeys(this.store, this.serverLocalId);
  final SecretStore store;
  final String serverLocalId;

  @override
  Future<void> share(Chat chat, Epoch e) => shareEpochForNotifications(
    store,
    serverLocalId: serverLocalId,
    conversationId: '${chat.id}',
    keyEpoch: e.keyEpoch,
    epochSeq: e.seq,
    contentKey: contentKey(e.history!, chat.id, e.keyEpoch),
    senders: sendersOf(e),
  );

  @override
  Future<void> forget(Chat chat, int keyEpoch) =>
      store.delete(epochItem(serverLocalId, '${chat.id}', keyEpoch));

  @override
  Future<void> forgetAll() => forgetSharedEpochs(store, serverLocalId);
}

/// What the extension would be given, kept in memory (tests).
class MemoryNotificationKeys implements NotificationKeyStore {
  final store = MemorySecretStore();
  late final _shared = SharedNotificationKeys(store, 'test');
  Map<String, String> get items => {
    for (final e in store.values.entries)
      if (e.key.startsWith('churr.notify.epoch.')) e.key: e.value,
  };
  @override
  Future<void> share(Chat chat, Epoch e) => _shared.share(chat, e);
  @override
  Future<void> forget(Chat chat, int keyEpoch) =>
      _shared.forget(chat, keyEpoch);
  @override
  Future<void> forgetAll() => _shared.forgetAll();
}

/// The name this session tracks an epoch's item by.
String notificationKeyAccount(Uuid conversation, int keyEpoch) =>
    '$conversation.$keyEpoch';
