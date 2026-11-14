// The real check of calls (README.md, "Calls"): this macOS app joins a call
// on a development server with calls on, beside a second participant (a
// Pion client, tools/callpeer), and confirms media flows both ways.
//
// With CALL_CRYPTO=1 both sides encrypt frames as a sealed chat's call does
// (docs/CALL-FRAMES.md), with fixed keys: this device 0x11 × 32, the other
// 0x22 × 32, key index 5. callpeer finds which reading of the format opens
// the app's frames; the app reports whether it opens callpeer's.
//
// Run by hand (it needs a server and callpeer started first):
//
//   flutter test integration_test/call_real_test.dart -d macos \
//     --dart-define=CHURR_SEED=$(base64 < seed.json | tr -d '\n') \
//     [--dart-define=CALL_CRYPTO=1] [--dart-define=CALL_CAMERA=1]
import 'dart:async';
import 'dart:convert';

import 'package:churr/core/api/calls.dart';
import 'package:churr/core/api/client.dart';
import 'package:churr/core/calls/call_layout.dart';
import 'package:churr/core/calls/call_media.dart';
import 'package:churr/core/calls/call_model.dart';
import 'package:churr/core/calls/call_session.dart';
import 'package:churr/core/calls/live_signal.dart';
import 'package:churr/core/live.dart';
import 'package:churr/core/models/json.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/platform/calls/webrtc_media.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';

const _seed = String.fromEnvironment('CHURR_SEED');
const _crypto = String.fromEnvironment('CALL_CRYPTO') == '1';
const _camera = String.fromEnvironment('CALL_CAMERA') == '1';

class _Delegate implements LiveDelegate {
  final events = StreamController<Json>.broadcast();
  @override
  Future<Map<String, int>> positions() async => {};
  @override
  Future<void> onEvent(Json event, {required bool replay}) async =>
      events.add(event);
  @override
  Future<void> onReload(Json frame) async {}
  @override
  Future<void> onReady(List<String> ids) async {}
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('a call with a second participant carries media both ways', (
    tester,
  ) async {
    expect(_seed, isNotEmpty, reason: 'pass CHURR_SEED');
    final seed =
        jsonDecode(utf8.decode(base64.decode(_seed))) as Map<String, dynamic>;
    final accounts = (seed['accounts'] as List).cast<Map<String, dynamic>>();
    final kai = accounts.firstWhere((a) => a['username'] == 'kai');
    final sam = accounts.firstWhere((a) => a['username'] == 'sam');
    final conv = (seed['conversations'] as Map)['chat_sam'] as String;
    final api = ApiClient(
      base: Uri.parse(seed['origin'] as String),
      tokens: Tokens(
        access: kai['access_token'] as String,
        refresh: kai['refresh_token'] as String,
        expiresAt: DateTime.now().add(const Duration(minutes: 30)),
      ),
    );
    final delegate = _Delegate();
    final live = LiveConnection(api: api, delegate: delegate)..start();
    await tester.runAsync(() async {
      final end = DateTime.now().add(const Duration(seconds: 10));
      while (live.state != LiveState.ready && DateTime.now().isBefore(end)) {
        await Future<void>.delayed(const Duration(milliseconds: 100));
      }
    });
    expect(live.state, LiveState.ready);

    final media = WebRtcMedia(selfId: kai['id'] as String);
    await tester.runAsync(() async {
      try {
        await media.openMicrophone().timeout(const Duration(seconds: 20));
      } on TimeoutException {
        // ignore: avoid_print
        print('CALLCHECK microphone permission prompt is blocking');
        rethrow;
      }
    });
    final log = <String>[];
    final session = CallSession(
      api: ClientCallApi(api),
      signal: LiveCallSignal(live, delegate.events.stream),
      media: media,
      conversationId: conv,
      selfId: kai['id'] as String,
      camera: _camera,
      trace: (dir, f) {
        if (f['type'] != 'call.candidate') {
          final copy = {...f};
          if (copy['sdp'] is String) {
            // Only the media lines and their first codec, for checking
            // what was negotiated.
            final sdp = copy['sdp'] as String;
            copy['sdp'] = [
              for (final l in sdp.split('\r\n'))
                if (l.startsWith('m=')) l,
            ].join(' | ');
          }
          log.add('$dir ${jsonEncode(copy)}');
        }
      },
    );
    final decrypts = <String>[];
    final onDecrypt = media.onDecrypt;
    media.onDecrypt = (who, ok) {
      decrypts.add('$who ${ok ? 'ok' : 'failed'}');
      onDecrypt?.call(who, ok);
    };
    if (_crypto) {
      await tester.runAsync(
        () => media.setFrameKeys(
          // callpeer's session has no device, so the server names none
          // for its tracks: the account's one device stands in.
          FrameKeys(
            ownDevice: 'own',
            keys: {'own': List.filled(32, 0x11), 'peer': List.filled(32, 0x22)},
            keyIndex: 5,
            soleDevice: {sam['id'] as String: 'peer'},
          ),
        ),
      );
    }

    Map<String, int> counters = {};
    await tester.runAsync(() async {
      await session.start();
      final end = DateTime.now().add(const Duration(seconds: 40));
      var subscribed = <String>{};
      while (DateTime.now().isBefore(end) && session.ended == null) {
        await Future<void>.delayed(const Duration(milliseconds: 250));
        // Show (subscribe to) every camera of the others, full size.
        final videos = [
          for (final p in session.call?.participants ?? <CallParticipant>[])
            if (p.accountId != kai['id'])
              for (final t in p.tracks)
                if (t.source.isVideo) t.id,
        ];
        if (videos.toSet().difference(subscribed).isNotEmpty) {
          subscribed = videos.toSet();
          session.setSubscriptions([
            for (final id in videos) Subscription(id, 1280, 720),
          ]);
        }
        counters = await media.counters();
        final inAudio = counters['audio_in'] ?? 0;
        final outAudio = counters['audio_out'] ?? 0;
        if (session.phase == CallPhase.live &&
            inAudio > 20000 &&
            outAudio > 20000 &&
            DateTime.now().isAfter(end.subtract(const Duration(seconds: 20)))) {
          break;
        }
      }
    });
    // ignore: avoid_print
    print(
      'CALLCHECK phase ${session.phase} ended ${session.ended} '
      '${session.endCode} counters $counters tracks ${session.myTracks} '
      'forwarding ${{for (final e in session.forwarding.entries) e.key: '${e.value.state.name}/${e.value.layer}'}} '
      'decrypts ${decrypts.toSet()}',
    );
    for (final l in log) {
      // ignore: avoid_print
      print('CALLFRAME $l');
    }
    await tester.runAsync(() async {
      await session.leave();
      await session.dispose();
      await live.dispose();
    });
    expect(counters['audio_out'] ?? 0, greaterThan(0));
    expect(counters['audio_in'] ?? 0, greaterThan(0));
  });
}
