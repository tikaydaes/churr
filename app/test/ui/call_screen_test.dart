import 'package:churr/core/calls/call_media.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/state/call_service.dart';
import 'package:churr/ui/scopes.dart';
import 'package:churr/ui/screens/call/call_overlay.dart';
import 'package:churr/ui/screens/call/call_video.dart';
import 'package:churr/ui/screens/conversation/conversation_screen.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../conversation_support.dart';
import '../core/calls/call_fakes.dart' show FakeMedia, FakeSignal;

const callId = '0193b100-2c1e-7b3a-9f0e-2a4d5c6b7e8f';

Map<String, Object?> callJson(List<String> people) => {
  'id': callId,
  'conversation_id': general,
  'message_id': 'msg-call',
  'started_by_id': 'me',
  'started_at': '2026-11-13T09:30:00.000Z',
  'sealed': false,
  'participants': [
    for (final p in people)
      {'account_id': p, 'joined_at': '2026-11-13T09:30:00.000Z'},
  ],
};

class Rig {
  final signal = FakeSignal();
  final media = FakeMedia();
  late AppState app;
  late FakeServer fake;

  Future<void> setUp(WidgetTester tester) async {
    fake = FakeServer()
      ..on('GET /api/v1/conversations/$general/call', (_) => {})
      ..on(
        'POST /api/v1/conversations/$general/call/join',
        (_) => {'call': callJson([]), 'ticket': 'tk'},
      );
    signal.auto = (f) => switch (f['type']) {
      'call.join' || 'call.resume' => {
        'type': 'call.ok',
        'resume_token': 'rt',
        'call': callJson(['me']),
        'limits': {'max_subscriptions': 16},
      },
      'call.offer' => {
        'type': 'call.answer',
        'pc': 'send',
        'sdp': 'x',
        'tracks': [
          for (final t in (f['tracks'] as List).cast<Map<String, Object?>>())
            {'mid': t['mid'], 'track_id': 'my${t['mid']}'},
        ],
      },
      'call.candidate' => null,
      _ => {'type': 'call.ok'},
    };
    CallService.mediaFactory = (_) => media;
    CallService.signalFactory = (_) => signal;
    CallService.ring = () async => null;
    CallVideo.remote = (m, id, {cover = true}) =>
        ColoredBox(key: Key('video-$id'), color: Colors.teal);
    CallVideo.local = (m, {screen = false}) => ColoredBox(
      key: Key(screen ? 'my-screen' : 'my-camera'),
      color: Colors.teal,
    );
    app = await conversationApp(tester, await conversationCache(), fake);
  }

  Future<void> pump(WidgetTester tester) async {
    final server = serverOf(app);
    await tester.pumpWidget(
      AppScope(
        state: app,
        child: MaterialApp(
          localizationsDelegates: L10n.localizationsDelegates,
          supportedLocales: L10n.supportedLocales,
          theme: buildTheme(dark: false),
          home: Scaffold(
            body: CallOverlay(
              app: app,
              child: ConversationScreen(
                server: server,
                conversationId: general,
              ),
            ),
          ),
        ),
      ),
    );
    await settle(tester);
  }

  void event(String type, Map<String, Object?> fields) => signal.deliver({
    'type': type,
    'at': '2026-11-13T09:31:00.000Z',
    'conversation_id': general,
    'call_id': callId,
    ...fields,
  });

  Future<void> joinFromHeader(WidgetTester tester) async {
    await tester.tap(find.byKey(const Key('header-call')));
    await settle(tester, 2);
    expect(find.byKey(const Key('prejoin-join')), findsOneWidget);
    await tester.tap(find.byKey(const Key('prejoin-join')));
    await settle(tester);
  }

  Future<void> samJoinsWithCamera(WidgetTester tester) async {
    event('call.participant_joined', {
      'participant': {
        'account_id': 'sam',
        'joined_at': '2026-11-13T09:31:00.000Z',
      },
    });
    event('call.track_published', {
      'account_id': 'sam',
      'track': {
        'id': 't8',
        'source': 'camera',
        'kind': 'video',
        'muted': false,
        'codec': 'vp8',
        'layers': ['q', 'h', 'f'],
        'width': 1280,
        'height': 720,
      },
    });
    event('call.track_published', {
      'account_id': 'sam',
      'track': {
        'id': 't7',
        'source': 'microphone',
        'kind': 'audio',
        'muted': false,
      },
    });
    await settle(tester);
    // The subscription goes out 200 ms after the layout settles.
    await tester.pump(const Duration(milliseconds: 250));
    await settle(tester, 2);
  }

  Future<void> tearDown(WidgetTester tester) async {
    await CallService.of(app).dismiss();
    await tester.pumpWidget(const SizedBox());
    await tester.runAsync(() async {
      for (final s in app.servers) {
        await s.shutDown();
      }
    });
  }
}

void main() {
  for (final size in [desktop, phone]) {
    final name = size == desktop ? 'desktop' : 'phone';

    testWidgets('$name: start from the header, see the other person, '
        'speaking and muted marks, subscription, shrink to the bar, leave', (
      tester,
    ) async {
      await setSize(tester, size);
      final r = Rig();
      await r.setUp(tester);
      await r.pump(tester);
      expect(find.byKey(const Key('header-call')), findsOneWidget);
      await r.joinFromHeader(tester);

      expect(r.signal.types.first, 'call.join');
      expect(find.byKey(const Key('call-mic')), findsOneWidget);
      expect(find.byKey(const Key('call-leave')), findsOneWidget);
      expect(find.text('You'), findsWidgets);

      await r.samJoinsWithCamera(tester);
      expect(find.byKey(const Key('video-t8')), findsOneWidget);
      final sub = r.signal.last('call.subscribe');
      final video = (sub['video'] as List).single as Map;
      expect(video['track_id'], 't8');
      expect(video['width'], greaterThan(100));

      r.event('call.speakers_changed', {
        'account_ids': ['sam'],
      });
      r.event('call.mute_changed', {
        'account_id': 'sam',
        'track_id': 't7',
        'source': 'microphone',
        'muted': true,
      });
      await settle(tester, 2);
      expect(find.byKey(const Key('speaking-sam')), findsOneWidget);
      expect(find.byKey(const Key('muted-sam')), findsOneWidget);

      // Microphone off: the control and the call.mute frame.
      await tester.tap(find.byKey(const Key('call-mic')));
      await settle(tester, 2);
      expect(r.signal.last('call.mute')['muted'], true);
      expect(find.byKey(const Key('muted-me')), findsOneWidget);

      // Shrink: the conversation is back, with the bar above it.
      await tester.tap(find.byKey(const Key('call-shrink')));
      await settle(tester, 2);
      expect(find.byKey(const Key('call-bar')), findsOneWidget);
      expect(find.byKey(const Key('composer')), findsOneWidget);
      if (size == desktop) {
        expect(find.byKey(const Key('call-floating')), findsOneWidget);
      }
      await tester.tap(find.byKey(const Key('call-bar-leave')));
      await settle(tester, 2);
      expect(r.signal.types, contains('call.leave'));
      expect(find.byKey(const Key('call-bar')), findsNothing);
      await r.tearDown(tester);
    });

    testWidgets('$name: the call moving to another device says so', (
      tester,
    ) async {
      await setSize(tester, size);
      final r = Rig();
      await r.setUp(tester);
      await r.pump(tester);
      await r.joinFromHeader(tester);
      r.event('call.participant_left', {
        'account_id': 'me',
        'reason': 'joined_elsewhere',
      });
      await settle(tester, 2);
      expect(
        find.text('You joined this call on another device.'),
        findsOneWidget,
      );
      await tester.tap(find.byKey(const Key('call-close')));
      await settle(tester, 2);
      expect(find.byKey(const Key('call-ended')), findsNothing);
      await r.tearDown(tester);
    });

    testWidgets('$name: an incoming call can be declined', (tester) async {
      await setSize(tester, size);
      final r = Rig();
      await r.setUp(tester);
      await r.pump(tester);
      CallService.of(r.app).showIncomingCall(
        serverId: 's',
        conversationId: general,
        callId: callId,
        fromAccountId: 'sam',
      );
      await settle(tester, 2);
      expect(find.byKey(const Key('incoming-call')), findsOneWidget);
      expect(find.text('Sam Okafor is calling'), findsOneWidget);
      await tester.tap(find.byKey(const Key('incoming-decline')));
      await settle(tester, 2);
      expect(find.byKey(const Key('incoming-call')), findsNothing);
      await r.tearDown(tester);
    });
  }

  testWidgets('a call\'s system message: started with Join, then the summary', (
    tester,
  ) async {
    await setSize(tester, desktop);
    final r = Rig();
    await r.setUp(tester);
    final server = serverOf(r.app);
    Message m(Map<String, Object?> system) =>
        msg('msg-call', 30, '', author: 'sam', extra: {'system': system});
    Widget host(Message message) => AppScope(
      state: r.app,
      child: MaterialApp(
        localizationsDelegates: L10n.localizationsDelegates,
        supportedLocales: L10n.supportedLocales,
        theme: buildTheme(dark: false),
        home: Scaffold(
          body: CallSystemLine(server: server, message: message),
        ),
      ),
    );
    await tester.pumpWidget(
      host(m({'kind': 'call_started', 'call_id': callId})),
    );
    await settle(tester, 2);
    expect(find.text('Sam Okafor started a call'), findsOneWidget);
    await tester.pumpWidget(
      host(
        m({
          'kind': 'call_ended',
          'call_id': callId,
          'duration_seconds': 1284,
          'participant_ids': ['sam', 'me', 'lee', 'x'],
        }),
      ),
    );
    await settle(tester, 2);
    expect(find.text('Call, 21 minutes, 4 people'), findsOneWidget);
    await r.tearDown(tester);
  });

  test('FrameKeys is plain data', () {
    const k = FrameKeys(
      ownDevice: 'd',
      keys: {
        'd': [1],
      },
      keyIndex: 0,
    );
    expect(k.keyIndex, 0);
    expect(k.ownKey, [1]);
  });
}
