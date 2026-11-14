// The desktop tray icon's state (docs/PUSH.md section 13, "Tray icon
// states"): worked out from each signed-in server's totals and
// connection, so it can be tested without a tray.

/// What the tray icon shows. Each is a different shape, not only a
/// different colour (docs/STYLE.md, "Colour never alone").
enum TrayIcon {
  /// The mascot's outline alone.
  normal,

  /// A small square dot: something unread that does not call for the
  /// person.
  unread,

  /// A larger block in the attention colour, with the count where the
  /// platform can draw one: a mention or a direct message.
  mention,

  /// A small moon: do not disturb on every server taking part.
  dnd,

  /// A dotted outline in grey: no server connected.
  disconnected,
}

/// One server as the tray sees it.
class TrayServer {
  const TrayServer({
    required this.localId,
    required this.name,
    this.badge = 0,
    this.mentions = 0,
    this.directBadge = 0,
    this.unreadConversations = 0,
    this.connected = true,
    this.dnd = false,
    this.muted = false,
  });

  final String localId;
  final String name;

  /// The server's total badge (`StatusSummary.badge`).
  final int badge;
  final int mentions;

  /// Badges of chats with people (ad-hoc channels and sealed chats).
  final int directBadge;
  final int unreadConversations;
  final bool connected;

  /// In do not disturb on that server, from any cause.
  final bool dnd;

  /// Muted from this app (docs/CLIENTS.md 8.8).
  final bool muted;
}

/// The icon, the number and the tooltip's values.
class TrayState {
  const TrayState(this.icon, this.count, this.servers);
  final TrayIcon icon;

  /// The sum of the servers' badges: the dock and taskbar number too.
  final int count;
  final List<TrayServer> servers;

  /// What the number reads where room is short: `1`…`9`, then `9+`.
  String get shortCount => count <= 0
      ? ''
      : count > 9
      ? '9+'
      : '$count';

  @override
  bool operator ==(Object other) =>
      other is TrayState &&
      other.icon == icon &&
      other.count == count &&
      other.servers.length == servers.length;

  @override
  int get hashCode => Object.hash(icon, count, servers.length);
}

/// The rule, in order: no server connected; every server quiet (do not
/// disturb or muted); a mention or a direct message anywhere; anything
/// unread; otherwise normal. A quiet server's counts still add up in the
/// number, as badges keep counting in do not disturb.
TrayState trayState(List<TrayServer> servers) {
  final count = servers.fold(0, (n, s) => n + s.badge);
  TrayIcon icon;
  if (servers.isEmpty || servers.every((s) => !s.connected)) {
    icon = TrayIcon.disconnected;
  } else if (servers.every((s) => s.dnd || s.muted)) {
    icon = TrayIcon.dnd;
  } else if (servers.any(
    (s) => !(s.dnd || s.muted) && (s.mentions > 0 || s.directBadge > 0),
  )) {
    icon = TrayIcon.mention;
  } else if (servers.any((s) => s.unreadConversations > 0 || s.badge > 0)) {
    icon = TrayIcon.unread;
  } else {
    icon = TrayIcon.normal;
  }
  return TrayState(icon, count, servers);
}

/// The file name (without folder or `.png`) of the icon for [s], as
/// `tool/tray_icons.py` draws them into `assets/tray`.
String trayIconName(TrayState s) => switch (s.icon) {
  TrayIcon.normal => 'normal',
  TrayIcon.unread => 'unread',
  TrayIcon.dnd => 'dnd',
  TrayIcon.disconnected => 'disconnected',
  TrayIcon.mention =>
    s.count > 9 ? 'mention-9plus' : 'mention-${s.count < 1 ? 1 : s.count}',
};
