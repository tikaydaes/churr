import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/api/calls.dart';
import '../core/calls/call_media.dart';
import '../core/calls/call_model.dart';
import '../core/calls/call_session.dart';
import '../core/calls/live_signal.dart';
import '../core/calls/ringing.dart';
import '../core/calls/sealed_call_keys.dart';
import '../core/models/json.dart';
import '../platform/calls/webrtc_media.dart';
import '../platform/sounds.dart';
import 'account_hub.dart';
import 'app_state.dart';
import 'sealed_state.dart';
import 'server_state.dart';

/// The call this device is in.
class ActiveCall {
  ActiveCall(this.server, this.conversationId, this.session, this.media);
  final ServerState server;
  final String conversationId;
  final CallSession session;
  final CallMedia media;
}

/// A ring being shown.
class IncomingCall {
  IncomingCall({
    required this.server,
    required this.conversationId,
    required this.callId,
    this.fromAccountId,
    this.until,
    this.silent = false,
  });
  final ServerState server;
  final String conversationId;
  final String callId;
  final String? fromAccountId;
  final DateTime? until;

  /// A banner without the ring sound: the person is in another call.
  final bool silent;
}

/// Why the app would not join, said before anything is sent.
enum JoinRefusal {
  /// A sealed chat's call, and this device holds no call keys for it.
  sealedNoKeys,
}

/// Calls across every signed-in server: which conversations have a call
/// on (for headers and system messages), the one call this device is in,
/// and rings. One per [AppState] ([CallService.of]).
class CallService extends ChangeNotifier {
  CallService(this.app);

  static final _all = Expando<CallService>('calls');

  /// The service of an app, made on first use.
  static CallService of(AppState app) => _all[app] ??= CallService(app);

  /// Makes the media side of a call. Tests put a fake here.
  static CallMedia Function(String selfId) mediaFactory = (selfId) =>
      WebRtcMedia(selfId: selfId);

  /// The signalling connection of a server. Tests put a fake here.
  static CallSignal Function(ServerState server) signalFactory = (s) =>
      LiveCallSignal(s.live, s.sync.localEvents);

  /// Frame keys for a sealed chat's call, from the sealed-chat library
  /// (`Chat.callKeys`, through [SealedHub]). Tests put their own here.
  static Future<FrameKeys?> Function(
    ServerState server,
    String conversationId,
    CallInfo call,
  )?
  frameKeys = _sealedKeys;

  static Future<FrameKeys?> _sealedKeys(
    ServerState s,
    String conversationId,
    CallInfo call,
  ) async {
    final session = SealedHub.instance.of(s)?.session;
    final chat = session?.chat(conversationId);
    final device = session?.device;
    if (chat == null || device == null) return null;
    return sealedFrameKeys(chat, device.id, call.id);
  }

  /// What tells that a server's sealed chats changed (a new key epoch
  /// among other things): a call in a sealed chat then asks for its frame
  /// keys again. Tests put their own here.
  static Listenable? Function(ServerState server) sealedChanges = (s) =>
      SealedHub.instance.of(s);

  /// Whether this device holds a sealed chat at all (checked before
  /// joining its call).
  static bool Function(ServerState server, String conversationId)
  holdsSealedChat = (s, id) =>
      SealedHub.instance.of(s)?.session?.chat(id) != null;

  /// Starts the ring and returns something to stop it with. Tests put a
  /// silent one here.
  static Future<Future<void> Function()?> Function() ring = () async {
    final p = await Sounds.startRing();
    if (p == null) return null;
    return () async {
      await p.stop();
      await p.dispose();
    };
  };

  final AppState app;

  final _attached = <String, StreamSubscription<Object?>>{};
  final _live = <String, Map<String, CallInfo?>>{};
  final _fetched = <String>{};

  ActiveCall? active;
  IncomingCall? incoming;

  /// The call screen fills the window; false shows the compact bar.
  bool expanded = true;

  StreamSubscription<void>? _activeSub;
  Future<void> Function()? _stopRing;
  Timer? _ringTimeout;
  int _nextId = 0;

  /// Listens to every signed-in server's call events. Cheap to call on
  /// every build.
  void attachAll() {
    for (final s in app.servers) {
      if (_attached.containsKey(s.localId)) continue;
      _attached[s.localId] = s.sync.localEvents.listen((e) => _onEvent(s, e));
    }
  }

  /// The call in progress in a conversation, if any: from events since
  /// start, else from the conversation as last fetched.
  CallInfo? liveCall(ServerState s, String conversationId) {
    final m = _live[s.localId];
    if (m != null && m.containsKey(conversationId)) return m[conversationId];
    return CallInfo.fromJson(
      object(s.conversation(conversationId)?.raw['call']),
    );
  }

  /// Fetches a conversation's call once per run, so a call that started
  /// before this device connected (call events are not replayed) shows.
  Future<void> refresh(ServerState s, String conversationId) async {
    if (!_fetched.add('${s.localId}/$conversationId')) return;
    try {
      final c = await s.api.getCall(conversationId);
      _put(s, conversationId, c);
    } on Object {
      // Offline: the header goes by what it has.
    }
  }

  void _put(ServerState s, String conversationId, CallInfo? c) {
    (_live[s.localId] ??= {})[conversationId] = c;
    notifyListeners();
  }

  bool isActive(ServerState s, String conversationId) =>
      active != null &&
      active!.server == s &&
      active!.conversationId == conversationId &&
      active!.session.ended == null;

  // ---- joining and leaving ----

  /// Joins (or starts) the call in a conversation. One call at a time: a
  /// call this device is in elsewhere is left first. Returns why it would
  /// not, or null.
  Future<JoinRefusal?> join(
    ServerState s,
    String conversationId, {
    bool microphone = true,
    bool camera = false,
  }) async {
    if (isActive(s, conversationId)) {
      expanded = true;
      notifyListeners();
      return null;
    }
    final conv = s.conversation(conversationId);
    if ((conv?.isSealed ?? false) &&
        (frameKeys == null || !holdsSealedChat(s, conversationId))) {
      return JoinRefusal.sealedNoKeys;
    }
    await leave();
    _clearRing();
    final me = s.me?.id ?? '';
    final media = mediaFactory(me);
    final session = CallSession(
      api: ClientCallApi(s.api),
      signal: signalFactory(s),
      media: media,
      conversationId: conversationId,
      selfId: me,
      microphone: microphone,
      camera: camera,
      frameKeys: frameKeys == null
          ? null
          : (call) => frameKeys!(s, conversationId, call),
    );
    active = ActiveCall(s, conversationId, session, media);
    expanded = true;
    _activeSub = session.changes.listen((_) {
      if (session.call != null && session.ended == null) {
        _put(s, conversationId, session.call);
      }
      notifyListeners();
    });
    if (conv?.isSealed ?? false) {
      // A new key epoch during the call re-keys it (SEALED.md section 10).
      final changes = sealedChanges(s);
      void rekey() => unawaited(session.refreshKeys());
      changes?.addListener(rekey);
      _stopRekey = () => changes?.removeListener(rekey);
    }
    notifyListeners();
    await session.start();
    return null;
  }

  void Function()? _stopRekey;

  /// Leaves the call this device is in.
  Future<void> leave() async {
    final a = active;
    if (a == null) return;
    await a.session.leave();
    await dismiss();
  }

  /// Forgets an ended call (the screen showed why it ended).
  Future<void> dismiss() async {
    final a = active;
    if (a == null) return;
    active = null;
    _stopRekey?.call();
    _stopRekey = null;
    await _activeSub?.cancel();
    _activeSub = null;
    await a.session.dispose();
    if (a.media is ChangeNotifier) (a.media as ChangeNotifier).dispose();
    notifyListeners();
  }

  void setExpanded(bool v) {
    if (expanded == v) return;
    expanded = v;
    notifyListeners();
  }

  // ---- rings ----

  /// Opens the in-app incoming-call prompt. This is the interface for the
  /// system notification of a call (the notifications code calls it when
  /// the person taps a call notification, or when the app is brought
  /// forward by one). [serverId] is the local id of the server
  /// (`ServerState.localId`). It rings unless [silent], and unless the
  /// rules of docs/CALLS.md section 15 say not to.
  void showIncomingCall({
    required String serverId,
    required String conversationId,
    required String callId,
    String? fromAccountId,
    DateTime? until,
    bool silent = false,
  }) {
    final s = app.server(serverId);
    if (s == null) return;
    final action = ringAction(
      inThisCall:
          active?.session.call?.id == callId && active?.session.ended == null,
      inAnotherCall: active != null && active!.session.ended == null,
      doNotDisturb: s.me?.dndOn ?? false,
      serverMuted: AccountHub.of(app).isMuted(s),
    );
    if (action == RingAction.none) return;
    _clearRing();
    incoming = IncomingCall(
      server: s,
      conversationId: conversationId,
      callId: callId,
      fromAccountId: fromAccountId,
      until: until,
      silent: silent || action == RingAction.banner,
    );
    final left = until == null
        ? const Duration(seconds: 45)
        : until.difference(DateTime.now());
    _ringTimeout = Timer(left.isNegative ? Duration.zero : left, _clearRing);
    if (!incoming!.silent) {
      unawaited(
        ring().then((stop) {
          if (incoming?.callId == callId) {
            _stopRing = stop;
          } else {
            unawaited(stop?.call());
          }
        }),
      );
    }
    notifyListeners();
  }

  /// Declines the ring being shown (`call.decline`).
  void decline() {
    final i = incoming;
    if (i == null) return;
    i.server.live.sendFrame({
      'type': 'call.decline',
      'id': 'd${++_nextId}',
      'call_id': i.callId,
    });
    _clearRing();
  }

  /// Answers the ring being shown.
  Future<JoinRefusal?> answer({bool camera = false}) async {
    final i = incoming;
    if (i == null) return null;
    _clearRing();
    return join(i.server, i.conversationId, camera: camera);
  }

  void _clearRing() {
    _ringTimeout?.cancel();
    _ringTimeout = null;
    final stop = _stopRing;
    _stopRing = null;
    if (stop != null) unawaited(stop());
    if (incoming != null) {
      incoming = null;
      notifyListeners();
    }
  }

  // ---- events ----

  void _onEvent(ServerState s, Json e) {
    final type = e['type'] as String? ?? '';
    if (!type.startsWith('call.')) return;
    final conv = e['conversation_id'] as String?;
    final callId = e['call_id'] as String? ?? object(e['call'])?['id'];
    switch (type) {
      case 'call.started':
        final c = CallInfo.fromJson(object(e['call']));
        if (conv != null && c != null) _put(s, conv, c);
      case 'call.ended':
        if (conv != null) _put(s, conv, null);
        if (incoming?.callId == callId) _clearRing();
      case 'call.participant_joined':
        final c = conv == null ? null : liveCall(s, conv);
        final p = CallParticipant.fromJson(object(e['participant']) ?? {});
        if (c != null && c.id == callId) _put(s, conv!, c.joined(p));
      case 'call.participant_left':
        final c = conv == null ? null : liveCall(s, conv);
        final who = e['account_id'] as String?;
        if (c != null && c.id == callId && who != null) {
          _put(s, conv!, c.left(who));
        }
      case 'call.ring_started':
        if (conv != null && callId is String) {
          showIncomingCall(
            serverId: s.localId,
            conversationId: conv,
            callId: callId,
            fromAccountId: e['from_account_id'] as String?,
            until: parseTime(e['until']),
          );
        }
      case 'call.ring_stopped':
        if (incoming?.callId == callId) _clearRing();
    }
  }

  @override
  void dispose() {
    for (final s in _attached.values) {
      s.cancel();
    }
    _clearRing();
    super.dispose();
  }
}
