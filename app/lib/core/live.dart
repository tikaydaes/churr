import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:web_socket_channel/io.dart';

import 'api/client.dart';
import 'api/errors.dart';
import 'api/operations.dart';
import 'models/json.dart';

/// One open WebSocket, as little of it as the live connection needs, so
/// tests can put a fake in its place.
abstract class LiveSocket {
  /// Text frames from the server. Closes when the socket does.
  Stream<String> get incoming;
  void send(String text);
  Future<void> close();

  /// The close code, once closed.
  int? get closeCode;
}

/// Opens a socket to [url] with the upgrade [headers] and subprotocols.
typedef LiveConnector = Future<LiveSocket> Function(
  Uri url,
  Map<String, String> headers,
  List<String> protocols,
);

class _IoSocket implements LiveSocket {
  _IoSocket(this._channel);
  final IOWebSocketChannel _channel;

  @override
  Stream<String> get incoming =>
      _channel.stream.where((m) => m is String).cast<String>();
  @override
  void send(String text) => _channel.sink.add(text);
  @override
  Future<void> close() async => _channel.sink.close();
  @override
  int? get closeCode => _channel.closeCode;
}

/// The connector the app uses: `web_socket_channel` over `dart:io`, which
/// is what lets the token travel in the `Authorization` header of the
/// upgrade and never in the address (docs/EVENTS.md section 1).
Future<LiveSocket> ioLiveConnector(
  Uri url,
  Map<String, String> headers,
  List<String> protocols,
) async {
  final channel = IOWebSocketChannel.connect(
    url,
    headers: headers,
    protocols: protocols,
    connectTimeout: const Duration(seconds: 15),
  );
  await channel.ready;
  return _IoSocket(channel);
}

/// Where the live connection stands. The shell shows "catching up" until
/// [ready], and sends are held until then.
enum LiveState {
  /// Not connected; waiting to try again, or stopped.
  offline,
  connecting,

  /// `resume` sent; replayed events are arriving.
  catchingUp,

  /// `ready` received: events now arrive as they happen.
  ready,

  /// The refresh token was refused. The person signs in again.
  signedOut,

  /// Closed with 4003: the account was removed or banned. No reconnect.
  removed,
}

/// What the live connection needs from the sync engine.
abstract class LiveDelegate {
  /// The `positions` map of `resume`: every conversation held and the
  /// last `seq` applied to it.
  Future<Map<String, int>> positions();

  /// One event, replayed ([replay] true, before `ready`) or live. Events
  /// are handed over one at a time, in the order they arrived.
  Future<void> onEvent(Json event, {required bool replay});

  /// A `reload` frame: throw away what is held for one conversation.
  Future<void> onReload(Json frame);

  /// The replay ended. [newConversationIds] are conversations the account
  /// is in that were not in the positions sent.
  Future<void> onReady(List<String> newConversationIds);
}

/// The WebSocket of docs/EVENTS.md: connect, `hello`, `resume`, replay,
/// `ready`, heartbeats, and reconnecting for as long as it is started.
///
/// It knows nothing about what events mean; it hands them to a
/// [LiveDelegate] strictly in order. Reconnecting is always safe: the
/// next `resume` replays whatever was missed.
class LiveConnection {
  LiveConnection({
    required this.api,
    required this.delegate,
    LiveConnector? connector,
    Random? random,
  }) : _connect = connector ?? ioLiveConnector,
       _random = random ?? Random();

  final ApiClient api;
  final LiveDelegate delegate;
  final LiveConnector _connect;
  final Random _random;

  /// The subprotocol the server requires on the upgrade.
  static const subprotocol = 'churr.v1';

  /// The retry interval, in seconds, before the first, second, …
  /// reconnect attempt (docs/CLIENTS.md section 7). The wait itself is
  /// drawn at random around it by [reconnectDelay], so a server that
  /// restarts is not met by every client at once.
  static const backoffSeconds = [1, 2, 5, 10, 30];

  /// The spread used until a server has said its own in `hello`: an older
  /// server never does.
  static const defaultSpreadSeconds = 5;

  /// The longest spread a server asks for (docs/EVENTS.md section 2);
  /// anything larger is taken as this.
  static const maxSpreadSeconds = 60;

  /// The retry interval after a 4029 close: the server has too much to do,
  /// or was sent too much (docs/EVENTS.md section 6).
  static const busySeconds = 30;

  /// `reconnect_spread_seconds` of the last `hello`: how many seconds the
  /// server asks its clients to spread a reconnect over, which grows with
  /// how many of them there are.
  int get spreadSeconds => _spreadSeconds;
  int _spreadSeconds = defaultSpreadSeconds;

  /// When the next attempt will be made, while [state] is
  /// [LiveState.offline] and the connection is waiting to try again; null
  /// otherwise. It is set before the state changes, so whoever redraws on
  /// the change can show the countdown at once.
  DateTime? get retryAt => _retryAt;
  DateTime? _retryAt;

  /// The whole seconds left until [retryAt], rounded up, for "Reconnecting
  /// in N seconds"; null when no attempt is waiting. Never below one: the
  /// attempt is made as the last second runs out.
  int? retrySeconds([DateTime? now]) =>
      secondsUntil(_retryAt, now ?? DateTime.now());

  /// The whole seconds from [now] until [at], rounded up and at least one;
  /// null without an [at].
  static int? secondsUntil(DateTime? at, DateTime now) {
    if (at == null) return null;
    final ms = at.difference(now).inMilliseconds;
    return ms <= 1000 ? 1 : (ms / 1000).ceil();
  }

  /// The wait before a reconnect: a time drawn evenly between half the
  /// retry interval and the interval plus the server's spread. [unit] is
  /// the draw, from 0 up to 1.
  static Duration reconnectDelay({
    required int intervalSeconds,
    required int spreadSeconds,
    required double unit,
  }) {
    final low = intervalSeconds * 500;
    final high = (intervalSeconds + spreadSeconds) * 1000;
    return Duration(milliseconds: (low + (high - low) * unit).round());
  }

  final _states = StreamController<LiveState>.broadcast();
  LiveState _state = LiveState.offline;
  LiveState get state => _state;

  /// Every change of [state].
  Stream<LiveState> get states => _states.stream;

  bool _running = false;
  bool _stopped = true;
  LiveSocket? _socket;
  Completer<void>? _waiting;
  Timer? _heartbeat, _pongDue, _helloDue;
  DateTime? _lastActivity;

  /// The `connection_id` of the current socket, for logs and reports.
  String? connectionId;

  void _set(LiveState s) {
    if (_state == s) return;
    _state = s;
    if (!_states.isClosed) _states.add(s);
  }

  /// Starts connecting, and keeps reconnecting until [stop].
  void start() {
    _stopped = false;
    if (!_running) unawaited(_run());
  }

  /// Closes the socket and stops reconnecting.
  Future<void> stop() async {
    _stopped = true;
    _waiting?.complete();
    await _socket?.close();
    _set(LiveState.offline);
  }

  /// Skips the wait before the next attempt: the device reports the
  /// network is back, or the app came to the front.
  void connectNow() {
    if (_waiting != null && !_waiting!.isCompleted) _waiting!.complete();
  }

  /// Sends a frame that is not part of the connection's own protocol (a
  /// call's signalling, lib/core/calls). False when not connected and
  /// ready: nothing is held for later.
  bool sendFrame(Map<String, Object?> frame) {
    final s = _socket;
    if (s == null || _state != LiveState.ready) return false;
    s.send(jsonEncode(frame));
    return true;
  }

  /// The person did something. Sent at most once a minute; it is what
  /// makes them `online` and keeps their phone from being woken.
  void noteActivity() {
    final now = DateTime.now();
    if (_state != LiveState.ready) return;
    if (_lastActivity != null &&
        now.difference(_lastActivity!) < const Duration(minutes: 1)) {
      return;
    }
    _lastActivity = now;
    _socket?.send('{"type":"activity"}');
  }

  Uri get _url {
    final u = api.uri(Ops.openEvents.path);
    return u.replace(scheme: u.scheme == 'https' ? 'wss' : 'ws');
  }

  Future<void> _run() async {
    _running = true;
    var attempt = 0;
    try {
      while (!_stopped) {
        _set(LiveState.connecting);
        int? code;
        var reachedReady = false;
        try {
          final token = await api.accessToken();
          final socket = await _connect(
            _url,
            {'Authorization': 'Bearer $token'},
            const [subprotocol],
          );
          _socket = socket;
          reachedReady = await _session(socket);
          code = socket.closeCode;
        } on SignedOutException {
          _set(LiveState.signedOut);
          return;
        } on Object {
          // The upgrade failed. A refused credential looks the same as
          // no network from here, so ask over HTTP, which can tell and
          // will refresh the token if that is all it needs.
          if (!_stopped && await _credentialGone()) {
            _set(LiveState.signedOut);
            return;
          }
        } finally {
          _stopTimers();
          _socket = null;
        }
        if (_stopped) break;
        if (code == 4003) {
          _set(LiveState.removed);
          return;
        }
        if (code == 4001) {
          // The credential ended; only a fresh token may reconnect.
          try {
            await api.refresh();
          } on SignedOutException {
            _set(LiveState.signedOut);
            return;
          } on Object {
            // No network: the next attempt tries again.
          }
        }
        if (reachedReady) attempt = 0;
        final delay = code == 4029 ? _delay(busySeconds) : backoff(attempt++);
        // The moment is known before anyone is told the connection is
        // down, and it is the one the countdown shows.
        _retryAt = DateTime.now().add(delay);
        _set(LiveState.offline);
        await _wait(delay);
        _retryAt = null;
      }
    } finally {
      _running = false;
      if (_stopped) _set(LiveState.offline);
    }
  }

  Future<bool> _credentialGone() async {
    try {
      await api.call(Ops.getStatusSummary);
    } on SignedOutException {
      return true;
    } on Object {
      // Unreachable or busy: not a verdict on the credential.
    }
    return false;
  }

  /// The delay before reconnect attempt number [attempt] (from 0).
  Duration backoff(int attempt) =>
      _delay(backoffSeconds[min(attempt, backoffSeconds.length - 1)]);

  Duration _delay(int intervalSeconds) => reconnectDelay(
    intervalSeconds: intervalSeconds,
    spreadSeconds: _spreadSeconds,
    unit: _random.nextDouble(),
  );

  Future<void> _wait(Duration d) async {
    final c = _waiting = Completer<void>();
    final t = Timer(d, () {
      if (!c.isCompleted) c.complete();
    });
    await c.future;
    t.cancel();
    _waiting = null;
  }

  void _stopTimers() {
    _heartbeat?.cancel();
    _pongDue?.cancel();
    _helloDue?.cancel();
  }

  /// Serves one socket until it closes. Returns whether `ready` was
  /// reached, which is what resets the backoff.
  Future<bool> _session(LiveSocket socket) async {
    var ready = false;
    final done = Completer<void>();
    // Frames are applied one after another: the next waits for the
    // delegate to finish the one before, so a conversation's events are
    // never applied out of order. `pong` alone is handled at once, so a
    // slow catch-up is not mistaken for a dead connection.
    var chain = Future<void>.value();
    var failed = false;

    _helloDue = Timer(const Duration(seconds: 15), socket.close);

    Future<void> handle(Json f) async {
      final type = f['type'];
      switch (type) {
        case 'hello':
          _helloDue?.cancel();
          connectionId = f['connection_id'] as String?;
          // Kept for the next reconnect, which may be to a server that
          // has just started and cannot say yet. An older server never
          // says, and the default stands.
          final spread = f['reconnect_spread_seconds'];
          if (spread is num && spread >= 0) {
            _spreadSeconds = min(spread.round(), maxSpreadSeconds);
          }
          final beat = Duration(seconds: asInt(f['heartbeat_seconds'], 30));
          socket.send(
            jsonEncode({
              'type': 'resume',
              'positions': await delegate.positions(),
            }),
          );
          _set(LiveState.catchingUp);
          _heartbeat = Timer.periodic(beat, (_) {
            socket.send('{"type":"ping"}');
            // No pong within one heartbeat: the connection is dead.
            _pongDue ??= Timer(beat, socket.close);
          });
        case 'reload':
          await delegate.onReload(f);
        case 'ready':
          ready = true;
          _set(LiveState.ready);
          await delegate.onReady(strings(f['new_conversation_ids']));
        case 'ack' || 'error':
          // Answers to frames this client sent with an `id`. Nothing it
          // sends yet asks for one.
          break;
        default:
          // Events are `thing.happened`. Control frames this build does
          // not know are ignored, like unknown events.
          if (type is String && type.contains('.')) {
            await delegate.onEvent(f, replay: !ready);
          }
      }
    }

    socket.incoming.listen(
      (text) {
        final Object? decoded;
        try {
          decoded = jsonDecode(text);
        } on Object {
          return;
        }
        if (decoded is! Map) return;
        final f = decoded.cast<String, dynamic>();
        if (f['type'] == 'pong') {
          _pongDue?.cancel();
          _pongDue = null;
          return;
        }
        chain = chain.then((_) => failed ? null : handle(f)).catchError((
          Object _,
        ) {
          failed = true;
          // An event could not be applied (the database refused it, a
          // request it needed failed). Nothing was acknowledged, so
          // dropping the connection loses nothing: `resume` replays from
          // the last position that was applied.
          socket.close();
        });
      },
      onDone: () {
        if (!done.isCompleted) done.complete();
      },
      onError: (Object _) {
        if (!done.isCompleted) done.complete();
      },
      cancelOnError: true,
    );
    // The stream has ended (or failed, which cancels it), so there is no
    // subscription left to cancel.
    await done.future;
    await chain;
    return ready;
  }

  /// Stops for good and releases the state stream.
  Future<void> dispose() async {
    await stop();
    await _states.close();
  }
}
