// End to end against a real development server: two accounts, app-side
// devices driven in Dart (no UI) through the real encrypted store and the
// real API. Run by tool/sealed_e2e.sh, which builds the server, seeds it
// with five app sessions (kai three times, sam twice), serves it, and
// passes the seed and data directory here. Skipped without them.
//
// Stubbed: only the passkey's PRF output (32 fixed bytes), which only a
// real authenticator in a browser can give; the unlock page's work is
// done here by a throwaway browser device exactly as
// internal/web/static/unlock/unlock.js does it.

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:churr/core/api/client.dart';
import 'package:churr/core/api/messages.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/sealed/sealed.dart';
import 'package:churr/core/sealed_client/api.dart';
import 'package:churr/core/sealed_client/notify_keys.dart';
import 'package:churr/core/sealed_client/session.dart';
import 'package:churr/core/sealed_client/sqlite_store.dart';
import 'package:churr/core/session_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqlite3/sqlite3.dart';

final _seedPath = Platform.environment['CHURR_SEALED_E2E_SEED'];
final _dataDir = Platform.environment['CHURR_SEALED_E2E_DATA'];

class _App {
  _App(this.name, this.api, this.session, this.accountId, this.nse);
  final String name;
  final ApiClient api;
  final SealedSession session;
  final String accountId;
  final MemoryNotificationKeys nse;

  /// Fetches every sealed row of [conv] and hands it to the session, as
  /// the sync engine's writes to cache.db do in the app.
  Future<void> pull(String conv, String other, {ApiClient? via}) async {
    final rows = <Message>[];
    int? before;
    while (true) {
      final page = await (via ?? api).listMessages(
        conv,
        before: before,
        limit: 200,
      );
      rows.addAll(page.messages);
      if (!page.hasMore || page.messages.isEmpty) break;
      before = page.messages.map((m) => m.seq).reduce((a, b) => a < b ? a : b);
    }
    await session.takeRows(conv, other, rows);
  }

  List<String> texts(String conv) => [
    for (final (m, _, _) in session.view(conv))
      if (!m.isDeleted) m.text,
  ];
}

void main() {
  final skip = _seedPath == null || _dataDir == null
      ? 'needs tool/sealed_e2e.sh (a dev server)'
      : null;
  late Map<String, dynamic> seed;
  late Directory work;
  final secrets = MemorySecretStore();

  Future<_App> device(String name, int token) async {
    final a = (seed['accounts'] as List)[token] as Map<String, dynamic>;
    final api = ApiClient(
      base: Uri.parse(seed['origin'] as String),
      tokens: Tokens(
        access: a['access_token'] as String,
        refresh: a['refresh_token'] as String,
        expiresAt: DateTime.now().add(const Duration(minutes: 50)),
      ),
    );
    final store = await SqliteSealedStore.open(
      path: '${work.path}/$name.sealed.db',
      secrets: secrets,
      scope: name,
    );
    final nse = MemoryNotificationKeys();
    final s = SealedSession(
      api: api,
      store: store,
      platformName: 'macos',
      deviceName: name,
      nse: nse,
      delay: () => Duration.zero,
    );
    await s.start(myId: a['id'] as String);
    return _App(name, api, s, a['id'] as String, nse);
  }

  setUpAll(() {
    if (skip != null) return;
    seed =
        jsonDecode(File(_seedPath!).readAsStringSync()) as Map<String, dynamic>;
    work = Directory.systemTemp.createTempSync('sealed-e2e');
  });

  test(
    'two accounts, a chat, a file, three ways in, a removal, no plaintext on the server',
    () async {
      final kai1 = await device('kai1', 0);
      final sam1 = await device('sam1', 3);
      expect(kai1.session.status, SealedStatus.noBackup);

      // The first device of each account makes the backup.
      final kaiPhrase = await kai1.session.createBackup();
      final samPhrase = await sam1.session.createBackup();
      expect(kaiPhrase!.split(' '), hasLength(12));
      expect(samPhrase, isNotNull);
      expect(kai1.session.status, SealedStatus.unlocked);

      // kai starts the chat: the app makes the id and epoch record 1.
      final conv = await kai1.session.startChat(sam1.accountId);
      final id = conv['id'] as String;
      expect(conv['kind'], 'sealed');
      await sam1.session.setConversations({id: kai1.accountId});
      await kai1.session.setConversations({id: sam1.accountId});

      // Messages both ways, a reply, a reaction, an edit, a tick.
      final hello = await kai1.session.sendText(
        id,
        sam1.accountId,
        'Hello sam, the plan:\n- [ ] floor one\n- [ ] floor two',
      );
      await sam1.pull(id, kai1.accountId);
      expect(sam1.texts(id), contains(startsWith('Hello sam')));
      final ref = hello.clientId!;
      await sam1.session.sendText(
        id,
        kai1.accountId,
        'Got it, kai',
        parent: ref,
      );
      await sam1.session.react(id, kai1.accountId, ref, '👍', true);
      await sam1.session.tick(id, kai1.accountId, ref, 't1', true);
      await kai1.session.edit(
        id,
        sam1.accountId,
        ref,
        'Hello sam, the revised plan:\n- [ ] floor one\n- [ ] floor two',
      );
      await kai1.pull(id, sam1.accountId);
      await sam1.pull(id, kai1.accountId);
      final kaiView = kai1.session.view(id);
      final first = kaiView.firstWhere((x) => x.$1.id == ref).$1;
      expect(first.text, contains('revised plan'));
      expect(first.reactions.single.emoji, '👍');
      expect(first.tasks.single.checked, isTrue);
      expect(kai1.texts(id), contains('Got it, kai'));
      expect(sam1.texts(id).first, contains('revised plan'));

      // A file, encrypted on the device.
      final fileBytes = Uint8List.fromList(
        utf8.encode('FLOORPLAN-SECRET-CONTENT ' * 6000),
      );
      final entry = await kai1.session.upload(
        id,
        SealedUpload(
          name: 'floorplan-secret.txt',
          bytes: fileBytes,
          mediaType: 'text/plain',
          kind: 'file',
        ),
      );
      await kai1.session.sendText(
        id,
        sam1.accountId,
        'The floor plan',
        files: [entry],
      );
      await sam1.pull(id, kai1.accountId);
      final withFile = sam1.session.view(id).firstWhere((x) => x.$2.isNotEmpty);
      expect(withFile.$2.single.name, 'floorplan-secret.txt');
      final got = await sam1.session.download(
        withFile.$2.single.id,
        withFile.$2.single.key,
      );
      expect(got, fileBytes);

      // The notification extension's items: content key and senders only.
      final item =
          jsonDecode(sam1.nse.items.values.first) as Map<String, dynamic>;
      expect(item.keys.toSet(), {
        'v',
        'conversation_id',
        'key_epoch',
        'epoch_seq',
        'content_key',
        'senders',
      });

      // Way one: kai's second app types the recovery phrase.
      final kai2 = await device('kai2', 1);
      await kai2.session.setConversations({id: sam1.accountId});
      expect(kai2.session.status, SealedStatus.locked);
      await kai2.session.unlockWithPhrase(kaiPhrase);
      expect(kai2.session.status, SealedStatus.unlocked);
      await kai2.pull(id, sam1.accountId);
      expect(
        kai2.texts(id),
        contains('Got it, kai'),
        reason: 'history from the backup',
      );
      await sam1.session.refresh();
      await sam1.session.sendText(id, kai1.accountId, 'Welcome, second laptop');
      await kai2.pull(id, sam1.accountId);
      expect(kai2.texts(id), contains('Welcome, second laptop'));

      // Way two: kai's third app is approved from the first by its code.
      final kai3 = await device('kai3', 2);
      await kai3.session.setConversations({id: sam1.accountId});
      final (reqId, _, code) = await kai3.session.requestApproval();
      await kai1.session.refresh();
      final req = kai1.session.requests.firstWhere((r) => r.id == reqId);
      await expectLater(
        kai1.session.approve(req, 'AAAAA-AAAAA-AAAAA-AAAAA'),
        throwsA(isA<CodeError>()),
      );
      await kai1.session.approve(req, code);
      expect(await kai3.session.checkApproval(reqId), isTrue);
      await kai3.pull(id, sam1.accountId);
      expect(kai3.texts(id), contains('Welcome, second laptop'));

      // Way three: sam's second app, by a passkey on the unlock page.
      final sam2 = await device('sam2', 4);
      await sam2.session.setConversations({id: kai1.accountId});
      final prf = Uint8List.fromList(
        List.generate(32, (i) => 200 - i),
      ); // the stub
      final pk = (await sam1.api.passkeys()).firstWhere((p) => p.hasPrf);
      final credentialId = _credentialId(pk.id);
      final samId = Uuid.parse(sam1.accountId);
      // The page that held the passkey gives its unlock public key...
      await sam1.api.setPasskeyUnlockKey(
        pk.id,
        x25519Public(unlockKeyFromPrf(prf, samId, credentialId)),
      );
      // ...and sam's unlocked app is asked, and says yes.
      await sam1.session.refresh();
      await sam1.session.approvePasskey(
        sam1.session.passkeys.firstWhere((p) => p.id == pk.id),
      );
      // The unlock page, for sam2's device:
      final page = await Device.create(
        account: samId,
        server: sam1.session.serverName,
        platform: 6,
        random: SystemRandom(),
        now: () => DateTime.now().millisecondsSinceEpoch,
      );
      final backup = (await sam2.api.backup())!;
      await page.learnLists(samId, await sam2.api.deviceLists(sam1.accountId));
      final copy = backup.copyFor('passkey', passkeyId: pk.id)!;
      await page.unlockWithPrf(
        prf,
        credentialId,
        Uuid.parse(pk.id),
        copy.locked,
        backup.stored,
      );
      final sam2Device = sam2.session.device!;
      final forApp = await page.deviceCopyFor(
        sam2Device.id,
        sam2Device.boxPublic,
      );
      await sam2.api.addLockedCopy(
        Copy(wayDevice, sam2Device.id, forApp),
        backup.keyVersion,
      );
      expect(await sam2.session.checkUnlockPage(), isTrue);
      await sam2.pull(id, kai1.accountId);
      expect(sam2.texts(id), contains('Welcome, second laptop'));
      expect(sam2.texts(id), contains('The floor plan'));

      // Everyone catches up with the new devices.
      for (final a in [kai1, sam1, kai2, kai3]) {
        await a.session.refresh();
      }
      await kai3.session.sendText(id, sam1.accountId, 'From the third app');
      for (final a in [kai1, sam1, kai2, sam2]) {
        await a.pull(
          id,
          a.accountId == kai1.accountId ? sam1.accountId : kai1.accountId,
        );
        expect(a.texts(id), contains('From the third app'), reason: a.name);
      }

      // kai removes the second app from the first: its session ends, the
      // keys are replaced, a new epoch leaves it out.
      final kai2Id = '${kai2.session.device!.id}';
      await kai1.session.removeDevice(kai2Id);
      expect(
        kai1.session.listedDevices(kai1.accountId).map((d) => '${d.deviceId}'),
        isNot(contains(kai2Id)),
      );
      await sam1.session.refresh();
      await sam1.session.sendText(id, kai1.accountId, 'After the removal');
      await kai1.pull(id, sam1.accountId);
      expect(kai1.texts(id), contains('After the removal'));
      // The removed app is given the rows anyway (fetched with kai1's
      // session, since its own was ended) and cannot read the new one.
      await expectLater(kai2.api.listMessages(id), throwsA(anything));
      await kai2.pull(id, sam1.accountId, via: kai1.api);
      expect(kai2.texts(id), isNot(contains('After the removal')));
      expect(
        kai2.session.troubles(id).waiting + kai2.session.troubles(id).unchecked,
        greaterThan(0),
      );

      // No plaintext in the server's database.
      final server = <int>[];
      for (final f in Directory(_dataDir!).listSync()) {
        if (f is File && f.path.contains('churr.db')) {
          server.addAll(f.readAsBytesSync());
        }
      }
      final text = latin1.decode(server, allowInvalid: true);
      expect(text, contains('localhost')); // the file was read
      for (final secret in [
        'revised plan',
        'Got it, kai',
        'FLOORPLAN-SECRET-CONTENT',
        'floorplan-secret',
        'After the removal',
        'Welcome, second laptop',
        kaiPhrase,
      ]) {
        expect(
          text.contains(secret),
          isFalse,
          reason: 'the server holds "$secret"',
        );
      }
      // Nor in the devices' own stores, which are encrypted at rest.
      for (final f in work.listSync().whereType<File>()) {
        final raw = latin1.decode(f.readAsBytesSync(), allowInvalid: true);
        expect(raw.contains('revised plan'), isFalse, reason: f.path);
        expect(raw.startsWith('SQLite format 3'), isFalse, reason: f.path);
      }
    },
    skip: skip,
    timeout: const Timeout(Duration(minutes: 5)),
  );
}

/// The passkey's credential identifier, read from the server's database
/// (the API does not return it; the unlock page gets it from the browser).
Uint8List _credentialId(String passkeyId) {
  final db = sqlite3.open('$_dataDir/churr.db', mode: OpenMode.readOnly);
  try {
    final r = db.select('SELECT credential_id FROM passkeys WHERE id = ?', [
      passkeyId,
    ]);
    return r.first['credential_id'] as Uint8List;
  } finally {
    db.close();
  }
}
