// Phone wake-ups (docs/PUSH.md sections 4, 8 and 9): the event identifier
// a push carries, the answer of the wake-up fetch, and what to draw from
// it. Used by the Android push handlers (lib/push_background.dart) and by
// the app when it is woken in front; the iPhone does the same in Swift
// (ios/NotificationService).

import 'dart:async';

import '../api/client.dart';
import '../api/devices.dart';
import '../api/errors.dart';
import '../models/json.dart';
import '../models/message.dart';
import 'content.dart';

/// One message of a wake-up, with its author's display name.
class WakeUpMessage {
  WakeUpMessage(this.message, this.authorName);
  final Message message;
  final String authorName;
}

/// For a ring: who is calling and whether it should still ring.
class WakeCall {
  WakeCall({
    required this.id,
    required this.fromAccountId,
    required this.fromName,
    required this.ringing,
  });
  final String id;
  final String fromAccountId;
  final String fromName;
  final bool ringing;
}

/// The answer of `fetchWakeUp` (`WakeUp`).
class WakeUp {
  WakeUp({
    required this.kind,
    required this.conversationId,
    required this.conversationKind,
    this.name,
    this.title,
    this.messages = const [],
    this.more = 0,
    this.read = false,
    this.call,
    this.badge = 0,
  });

  factory WakeUp.fromJson(Json j) {
    final c = object(j['conversation']) ?? const {};
    final call = object(j['call']);
    return WakeUp(
      kind: j['kind'] as String? ?? 'message',
      conversationId: c['id'] as String? ?? '',
      conversationKind: c['kind'] as String? ?? 'channel',
      name: c['name'] as String?,
      title: c['title'] as String?,
      messages: [
        for (final m in j['messages'] as List? ?? const [])
          if (m is Map && m['message'] is Map)
            WakeUpMessage(
              Message.fromJson((m['message'] as Map).cast<String, dynamic>()),
              m['author_name'] as String? ?? '',
            ),
      ],
      more: asInt(j['more']),
      read: j['read'] == true,
      call: call == null
          ? null
          : WakeCall(
              id: call['id'] as String? ?? '',
              fromAccountId: call['from_account_id'] as String? ?? '',
              fromName: call['from_name'] as String? ?? '',
              ringing: call['ringing'] == true,
            ),
      badge: asInt(j['badge']),
    );
  }

  /// `message` or `ring`.
  final String kind;
  final String conversationId;

  /// `channel` or `sealed`.
  final String conversationKind;

  /// A named channel's name.
  final String? name;

  /// An ad-hoc channel's or sealed chat's title (who else is in it).
  final String? title;

  /// Newest last, at most five.
  final List<WakeUpMessage> messages;
  final int more;
  final bool read;
  final WakeCall? call;
  final int badge;
}

/// The characters an event identifier may hold.
final _eventIdChars = RegExp(r'^[A-Za-z0-9_.-]{1,256}$');

/// Splits an event identifier (`<wake_route>.<token>`) into its route and
/// token; null when it is not one.
(String, String)? splitEventId(String eventId) {
  if (!_eventIdChars.hasMatch(eventId)) return null;
  final dot = eventId.indexOf('.');
  if (dot <= 0 || dot == eventId.length - 1) return null;
  return (eventId.substring(0, dot), eventId.substring(dot + 1));
}

/// What a wake-up comes to on screen.
enum WakeOutcome {
  /// The real message (or messages): [WakeDisplay.text].
  message,

  /// Nothing: the account has read past it already (Android only; iOS
  /// cannot withdraw a notification).
  nothing,

  /// "Sam is calling", with the ring sound.
  ring,

  /// "Missed call from Sam": the ring stopped, or reached the phone late.
  missedCall,

  /// The placeholder "New message": no server with that route, the fetch
  /// failed, or a sealed message this device cannot open.
  placeholder,
}

/// What to draw for one wake-up.
class WakeDisplay {
  const WakeDisplay(
    this.outcome, {
    this.serverLocalId,
    this.conversationId,
    this.messageId,
    this.parentId,
    this.text,
    this.callId,
    this.callerName,
    this.badge,
  });

  final WakeOutcome outcome;

  /// Where opening it goes; null for a placeholder from an unknown route.
  final String? serverLocalId;
  final String? conversationId;
  final String? messageId;
  final String? parentId;

  /// For [WakeOutcome.message].
  final NotificationText? text;

  /// For a ring or a missed call.
  final String? callId;
  final String? callerName;

  /// The icon badge to set: the fetch's total, else the push's own.
  final int? badge;
}

/// Opens one sealed message for a notification: returns its text, or
/// null when this device cannot (no key for its epoch, a failed check).
/// The sealed-chat code provides it; without one, sealed wake-ups show
/// the placeholder (docs/SEALED.md section 11, step 5).
typedef SealedOpener = Future<String?> Function(
  String serverLocalId,
  Message message,
);

/// Turns a wake-up into what to show (docs/PUSH.md section 8, steps 1 to
/// 6, and the Android rules).
class WakeHandler {
  WakeHandler({
    required this.routes,
    required this.clientFor,
    required this.serverName,
    this.multiServer = false,
    this.showText = true,
    this.openSealed,
    this.timeout = const Duration(seconds: 10),
  });

  /// Wake route to the local id of the server it belongs to.
  final Map<String, String> routes;

  /// The API client of a signed-in server, by local id.
  final ApiClient? Function(String localId) clientFor;
  final String Function(String localId) serverName;
  final bool multiServer;

  /// The "show the message text" setting.
  final bool showText;
  final SealedOpener? openSealed;
  final Duration timeout;

  /// [badge] is the number the push itself carried, if any.
  Future<WakeDisplay> handle(String eventId, {int? badge}) async {
    final parts = splitEventId(eventId);
    final localId = parts == null ? null : routes[parts.$1];
    final api = localId == null ? null : clientFor(localId);
    if (localId == null || api == null) {
      return WakeDisplay(WakeOutcome.placeholder, badge: badge);
    }
    final WakeUp w;
    try {
      w = await api.fetchWakeUp(eventId).timeout(timeout);
    } on ApiException {
      return WakeDisplay(
        WakeOutcome.placeholder,
        serverLocalId: localId,
        badge: badge,
      );
    } on TimeoutException {
      return WakeDisplay(
        WakeOutcome.placeholder,
        serverLocalId: localId,
        badge: badge,
      );
    } on Exception {
      return WakeDisplay(
        WakeOutcome.placeholder,
        serverLocalId: localId,
        badge: badge,
      );
    }
    if (w.kind == 'ring' && w.call != null) {
      final c = w.call!;
      return WakeDisplay(
        c.ringing ? WakeOutcome.ring : WakeOutcome.missedCall,
        serverLocalId: localId,
        conversationId: w.conversationId,
        callId: c.id,
        callerName: c.fromName,
        badge: w.badge,
      );
    }
    if (w.read) {
      return WakeDisplay(
        WakeOutcome.nothing,
        serverLocalId: localId,
        conversationId: w.conversationId,
        badge: w.badge,
      );
    }
    if (w.messages.isEmpty) {
      return WakeDisplay(
        WakeOutcome.placeholder,
        serverLocalId: localId,
        conversationId: w.conversationId,
        badge: w.badge,
      );
    }
    final newest = w.messages.last;
    final m = newest.message;
    var text = describeMessage(
      m,
      sender: newest.authorName,
      channel: w.name,
      chatTitle: w.title,
      server: multiServer ? serverName(localId) : null,
      showText: showText,
      more: w.messages.length - 1 + w.more,
    );
    if (m.sealed != null && showText) {
      String? opened;
      try {
        opened = await openSealed?.call(localId, m);
      } on Object {
        opened = null;
      }
      if (opened != null) {
        text = describeMessage(
          Message.fromJson({...m.raw, 'text': opened, 'sealed': null}),
          sender: newest.authorName,
          channel: w.name,
          chatTitle: w.title,
          server: multiServer ? serverName(localId) : null,
          more: text.more,
        );
      }
    }
    return WakeDisplay(
      WakeOutcome.message,
      serverLocalId: localId,
      conversationId: w.conversationId,
      messageId: m.id,
      parentId: m.parentId,
      text: text,
      badge: w.badge,
    );
  }
}
