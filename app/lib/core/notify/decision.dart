// Whether a connected app shows a notification for a message event, and
// how notifications are grouped and cleared (docs/PUSH.md sections 12 and
// 13). The server has already applied the rule of section 2 (modes,
// mentions, blocks, do not disturb and quiet hours) and put the answer in
// the event's `notify` value; what is left to the app is whether the
// person is looking, and their own switches on this device.

/// Why a notification is or is not shown. Tests read it; the app only
/// needs [NotifyVerdict.show].
enum NotifyVerdict {
  show,

  /// The event's `notify` was `none` or `badge`: the server said no
  /// (a mode that does not count it, do not disturb, quiet hours).
  serverSaidNo,

  /// The person wrote it.
  own,

  /// Notifications are off in this device's settings.
  switchedOff,

  /// The server is muted from this app (docs/CLIENTS.md 8.8), or the
  /// account is in do not disturb as this device last heard. The server
  /// marks events `badge` in that state anyway; this covers an event that
  /// was decided just before the change reached it.
  quiet,

  /// The conversation is on screen in a window that is in front.
  onScreen,

  /// Replayed on catch-up and too old to be news: the phone was woken
  /// for it at the time.
  stale,
}

/// How old a replayed message may be and still pop up.
const staleAfter = Duration(minutes: 10);

/// What the decision looks at.
class NotifyInputs {
  const NotifyInputs({
    required this.notify,
    required this.messageAt,
    required this.now,
    this.own = false,
    this.notificationsOn = true,
    this.serverMuted = false,
    this.dnd = false,
    this.windowInFront = false,
    this.conversationOnScreen = false,
  });

  /// The event's `notify` value: `none`, `badge` or `notify`.
  final String? notify;
  final DateTime messageAt;
  final DateTime now;
  final bool own;
  final bool notificationsOn;
  final bool serverMuted;
  final bool dnd;

  /// The app's window is in front and focused (on a phone: the app is in
  /// the foreground).
  final bool windowInFront;

  /// The message's conversation (or its thread) is the one open.
  final bool conversationOnScreen;
}

/// The rule, in order. The first "no" ends it.
NotifyVerdict decide(NotifyInputs i) {
  if (i.notify != 'notify') return NotifyVerdict.serverSaidNo;
  if (i.own) return NotifyVerdict.own;
  if (!i.notificationsOn) return NotifyVerdict.switchedOff;
  if (i.serverMuted || i.dnd) return NotifyVerdict.quiet;
  if (i.windowInFront && i.conversationOnScreen) return NotifyVerdict.onScreen;
  if (i.now.difference(i.messageAt) > staleAfter) return NotifyVerdict.stale;
  return NotifyVerdict.show;
}

/// The key notifications of one conversation share: they are grouped
/// under it (a thread identifier on Apple platforms, a group and tag on
/// Android) and a newer one replaces the older one where the platform
/// shows only one per key. Unique across servers.
String groupKey(String serverLocalId, String conversationId) =>
    '$serverLocalId/$conversationId';

/// A notification's whole-number identifier, which the platforms need
/// for replacing and removing it: a stable 31-bit hash of [key] (FNV-1a,
/// so it is the same in every run and every isolate).
int notificationId(String key) {
  var h = 0x811c9dc5;
  for (final c in key.codeUnits) {
    h ^= c;
    h = (h * 0x01000193) & 0xffffffff;
  }
  return h & 0x7fffffff;
}

/// One item of a server's notifications list as the clearing logic sees
/// it: which conversation, and whether it has been read.
class PendingItem {
  const PendingItem(this.conversationId, {required this.cleared});
  final String conversationId;
  final bool cleared;
}

/// The conversations whose shown notifications should now be removed:
/// those in [shown] with no uncleared item left in [items], because the
/// read position passed them here or on another device (the read event
/// cleared the items in `cache.db`).
Set<String> conversationsToClear(
  Set<String> shown,
  Iterable<PendingItem> items,
) {
  final open = {
    for (final i in items)
      if (!i.cleared) i.conversationId,
  };
  return {
    for (final c in shown)
      if (!open.contains(c)) c,
  };
}
