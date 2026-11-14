/// Which conversations have a row in the sidebar, in which group, and
/// how each row is drawn (docs/CLIENTS.md sections 9.1, 9.2, 9.25 and
/// 9.26; ruling R46).
///
/// This is plain Dart with no Flutter in it: the three layouts draw the
/// same rows and differ only in grouping, so the rules live here once
/// and are tested without a widget.
library;

import '../core/models/account.dart';
import '../core/models/conversation.dart';

/// What kind of group a heading is.
enum GroupKind { starred, section, channels, chats, sealed }

/// The icon that says what kind of conversation a row is. The hash sign
/// is an icon and never part of a channel's name.
enum RowIcon { public, private, locked, chat, sealed }

/// One row of the sidebar.
class SidebarRow {
  SidebarRow({
    required this.conversation,
    required this.name,
    required this.icon,
    this.parentName,
    this.indent = 0,
    this.badge = 0,
    this.bold = false,
    this.dimmed = false,
    this.hiddenMark = false,
    this.starred = false,
    this.hasChildren = false,
    this.collapsed = false,
    this.moreCount = 0,
    this.detail,
    this.presence,
  });

  final Conversation conversation;

  /// The channel's name exactly as written, or the other members' names
  /// for an ad-hoc channel.
  final String name;

  /// Set when the row reads `parent › name`: a sub-channel whose parent
  /// has no row above it.
  final String? parentName;
  final RowIcon icon;

  /// 1 for a sub-channel drawn under its parent's row.
  final int indent;

  /// The count the mode notifies. Totals ignore `show`; this is only
  /// what the row itself shows.
  final int badge;

  /// Unread, in a mode that is not `nothing`, and not muted.
  final bool bold;

  /// Mode `nothing`, or muted. Never bold.
  final bool dimmed;

  /// `show` is `hidden` and the row is here because "Show hidden" is on.
  final bool hiddenMark;
  final bool starred;

  /// A parent with sub-channel rows: it has a disclosure mark.
  final bool hasChildren;
  final bool collapsed;

  /// On a folded parent: how many of its sub-channels are not drawn
  /// ("N more"). Unread and selected sub-channels stay drawn.
  final int moreCount;

  /// The creation date, shown after the name when another row in the
  /// same place has the same name (9.26).
  final String? detail;

  /// The other person's presence, for a two-person chat: online, away,
  /// dnd or offline.
  final String? presence;

  String get id => conversation.id;

  /// Whether the row stays drawn when its section or parent is folded:
  /// unread, counted, or the conversation being viewed.
  bool keepsWhenFolded(String? openId) => bold || badge > 0 || id == openId;

  SidebarRow copyWith({int? indent, String? parentName}) => SidebarRow(
    conversation: conversation,
    name: name,
    icon: icon,
    parentName: parentName ?? this.parentName,
    indent: indent ?? this.indent,
    badge: badge,
    bold: bold,
    dimmed: dimmed,
    hiddenMark: hiddenMark,
    starred: starred,
    hasChildren: hasChildren,
    collapsed: collapsed,
    moreCount: moreCount,
    detail: detail,
    presence: presence,
  );
}

/// The rows of a folded group: only those that keep (unread, counted,
/// selected). A sub-channel whose parent's row is folded away is drawn at
/// the parent's level as `parent › name`.
List<SidebarRow> foldRows(List<SidebarRow> rows, {String? openId}) {
  final out = <SidebarRow>[];
  SidebarRow? parent;
  var parentKept = false;
  for (final r in rows) {
    if (r.indent == 0) {
      parent = r;
      parentKept = r.keepsWhenFolded(openId);
      if (parentKept) out.add(r);
      continue;
    }
    if (!r.keepsWhenFolded(openId)) continue;
    out.add(
      parentKept || parent == null
          ? r
          : r.copyWith(indent: 0, parentName: parent.name),
    );
  }
  return out;
}

/// A heading and its rows. A group with no rows is never made.
class SidebarGroup {
  SidebarGroup(
    this.kind,
    this.title,
    this.rows, {
    this.sectionId,
    this.folded = false,
    int? total,
  }) : total = total ?? rows.length;
  final GroupKind kind;
  final String title;

  /// The rows drawn: all of them, or when [folded] only those that keep.
  final List<SidebarRow> rows;
  final String? sectionId;
  final bool folded;

  /// How many conversations the group holds, drawn or not.
  final int total;

  /// The key under which "collapsed" is remembered for this group.
  String get key => sectionId ?? kind.name;
}

/// Everything the rules need to know about one server.
class SidebarInput {
  SidebarInput({
    required this.conversations,
    required this.sections,
    this.outsideParents = const {},
    this.accounts = const {},
    this.myId,
    this.defaultShow = ShowSetting.always,
    this.showHidden = false,
    this.openConversationId,
    this.collapsedParents = const {},
    DateTime? now,
  }) : now = now ?? DateTime.now();

  final List<Conversation> conversations;
  final List<Section> sections;

  /// Public parents the account is not in (see `CacheDb.outsideParents`).
  final Map<String, Conversation> outsideParents;
  final Map<String, Account> accounts;
  final String? myId;

  /// The app's default for `show`, used where a membership's is not set.
  final ShowSetting defaultShow;
  final bool showHidden;

  /// The conversation being viewed, which always has its row.
  final String? openConversationId;

  /// What is folded away on this device: parents (by conversation id),
  /// whose read sub-channels are then not drawn, and groups (by
  /// [SidebarGroup.key]), which then draw only the rows that keep.
  final Set<String> collapsedParents;
  final DateTime now;
}

const _months = [
  'January', 'February', 'March', 'April', 'May', 'June', 'July', //
  'August', 'September', 'October', 'November', 'December',
];

int _byName(SidebarRow a, SidebarRow b) {
  // Sorting ignores case. Dart has no locale collation without a
  // package, so this is by lower-cased code units; the creation date
  // settles equal names.
  final n = a.name.toLowerCase().compareTo(b.name.toLowerCase());
  if (n != 0) return n;
  final ac = a.conversation.createdAt, bc = b.conversation.createdAt;
  if (ac == null || bc == null) return a.id.compareTo(b.id);
  return ac.compareTo(bc);
}

/// Whether a conversation has a row (9.2). The same in every layout.
bool hasRow(Conversation c, SidebarInput input) {
  // 1. The conversation being viewed always has its row, which is also
  // what keeps a row from vanishing under the person's hand when reading
  // it makes its condition false.
  if (c.id == input.openConversationId) return true;
  final m = c.membership;
  if (m == null) return false;
  return switch (m.show ?? input.defaultShow) {
    ShowSetting.always => true,
    ShowSetting.activity => m.unreadCount > 0,
    ShowSetting.notification => m.badgeCount > 0,
    ShowSetting.hidden => input.showHidden,
  };
}

/// The name a conversation is listed under: a channel's own name, or the
/// other members of an ad-hoc channel or sealed chat.
String conversationName(
  Conversation c,
  Map<String, Account> accounts,
  String? myId,
) {
  if (c.isNamed) return c.name!;
  final others = [
    for (final id in c.memberIds)
      if (id != myId) accounts[id]?.name ?? 'Someone',
  ];
  if (others.isEmpty) return accounts[myId]?.name ?? 'Just you';
  return others.join(', ');
}

/// The groups of one server, in the order of the rail and merged
/// layouts: Starred, the admin's sections, Channels (those in no
/// section), Chats, Sealed.
List<SidebarGroup> buildServerGroups(SidebarInput input) {
  final byId = {for (final c in input.conversations) c.id: c};
  final visible = {
    for (final c in input.conversations)
      if (hasRow(c, input)) c.id,
  };

  SidebarRow row(Conversation c, {int indent = 0, String? parentName}) {
    final m = c.membership;
    final muted = m?.mutedAt(input.now) ?? false;
    final dimmed = m?.mode == NotifyMode.nothing || muted;
    String? presence;
    if (!c.isNamed && c.memberIds.length == 2) {
      final other = c.memberIds.firstWhere(
        (id) => id != input.myId,
        orElse: () => '',
      );
      final a = input.accounts[other];
      presence = a == null
          ? null
          : a.dnd && a.presence != 'offline'
          ? 'dnd'
          : a.presence;
    }
    return SidebarRow(
      conversation: c,
      name: conversationName(c, input.accounts, input.myId),
      parentName: parentName,
      icon: c.isSealed
          ? RowIcon.sealed
          : !c.isNamed
          ? RowIcon.chat
          : c.isLocked
          ? RowIcon.locked
          : c.private
          ? RowIcon.private
          : RowIcon.public,
      indent: indent,
      badge: m?.badgeCount ?? 0,
      bold: !dimmed && (m?.unreadCount ?? 0) > 0,
      dimmed: dimmed,
      hiddenMark: m?.show == ShowSetting.hidden,
      starred: m?.starred ?? false,
      presence: presence,
    );
  }

  bool starred(Conversation c) => c.membership?.starred ?? false;

  /// The parent as far as the sidebar knows it: one the account is in,
  /// or a public one it can see. Null draws the channel as top-level.
  Conversation? parentOf(Conversation c) => c.parentId == null
      ? null
      : byId[c.parentId] ?? input.outsideParents[c.parentId];

  /// A parent's row is "there" for its sub-channels only where it would
  /// be drawn in its section: visible, and not moved to Starred.
  bool parentRowInSection(Conversation p) =>
      byId.containsKey(p.id) && visible.contains(p.id) && !starred(p);

  final groups = <SidebarGroup>[];

  /// A group, folded when its key is folded on this device.
  SidebarGroup makeGroup(
    GroupKind kind,
    String title,
    List<SidebarRow> rows, {
    String? sectionId,
  }) {
    final key = sectionId ?? kind.name;
    final folded = input.collapsedParents.contains(key);
    return SidebarGroup(
      kind,
      title,
      folded ? foldRows(rows, openId: input.openConversationId) : rows,
      sectionId: sectionId,
      folded: folded,
      total: rows.length,
    );
  }

  // Starred. A starred sub-channel reads `parent › name` here and is not
  // listed again under its parent. Starring does not override `show`.
  final starredRows = [
    for (final c in input.conversations)
      if (visible.contains(c.id) && starred(c))
        row(c, parentName: parentOf(c)?.name),
  ]..sort(_byName);
  if (starredRows.isNotEmpty) {
    groups.add(makeGroup(GroupKind.starred, 'Starred', starredRows));
  }

  // Named channels, by section. A sub-channel takes its parent's section
  // for as long as it has a parent the sidebar knows.
  final bySection = <String?, List<SidebarRow>>{};
  final named = [
    for (final c in input.conversations)
      if (c.isNamed && visible.contains(c.id) && !starred(c)) c,
  ];
  final childrenOf = <String, List<Conversation>>{};
  final topLevel = <Conversation>[];
  for (final c in named) {
    final p = parentOf(c);
    if (p != null && parentRowInSection(p)) {
      childrenOf.putIfAbsent(p.id, () => []).add(c);
    } else {
      topLevel.add(c);
    }
  }
  final knownSections = {for (final s in input.sections) s.id};
  for (final c in topLevel) {
    final p = parentOf(c);
    // No row ever stands in for a parent: the sub-channel is drawn at the
    // parent's level, in the parent's section, as `parent › name`.
    var section = (p ?? c).sectionId;
    if (!knownSections.contains(section)) section = null;
    final kids =
        (childrenOf[c.id] ?? const <Conversation>[])
            .map((k) => row(k, indent: 1))
            .toList()
          ..sort(_byName);
    final collapsed = input.collapsedParents.contains(c.id);
    final kept = [
      for (final k in kids)
        if (k.keepsWhenFolded(input.openConversationId)) k,
    ];
    final base = row(c, parentName: p?.name);
    final rows = bySection.putIfAbsent(section, () => []);
    rows.add(
      SidebarRow(
        conversation: base.conversation,
        name: base.name,
        parentName: base.parentName,
        icon: base.icon,
        badge: base.badge,
        bold: base.bold,
        dimmed: base.dimmed,
        hiddenMark: base.hiddenMark,
        hasChildren: kids.isNotEmpty,
        collapsed: collapsed && kids.isNotEmpty,
        moreCount: collapsed ? kids.length - kept.length : 0,
      ),
    );
  }
  void addSection(GroupKind kind, String title, String? sectionId) {
    final tops = bySection[sectionId];
    if (tops == null || tops.isEmpty) return;
    tops.sort(_byName);
    final rows = <SidebarRow>[];
    for (final t in tops) {
      rows.add(t);
      if (t.hasChildren) {
        final kids = childrenOf[t.id]!.map((k) => row(k, indent: 1)).toList()
          ..sort(_byName);
        // A folded parent keeps its unread and selected sub-channels.
        rows.addAll(
          t.collapsed
              ? kids.where((k) => k.keepsWhenFolded(input.openConversationId))
              : kids,
        );
      }
    }
    groups.add(
      makeGroup(kind, title, _markSameNames(rows), sectionId: sectionId),
    );
  }

  for (final s in input.sections) {
    addSection(GroupKind.section, s.name, s.id);
  }
  addSection(GroupKind.channels, 'Channels', null);

  // Chats: ad-hoc channels, newest activity first.
  int byActivity(SidebarRow a, SidebarRow b) {
    final at = a.conversation.lastMessageAt ?? a.conversation.createdAt;
    final bt = b.conversation.lastMessageAt ?? b.conversation.createdAt;
    if (at == null || bt == null) return a.id.compareTo(b.id);
    return bt.compareTo(at);
  }

  final chats = [
    for (final c in input.conversations)
      if (c.isAdHoc && visible.contains(c.id) && !starred(c)) row(c),
  ]..sort(byActivity);
  if (chats.isNotEmpty) groups.add(makeGroup(GroupKind.chats, 'Chats', chats));

  final sealed = [
    for (final c in input.conversations)
      if (c.isSealed && visible.contains(c.id) && !starred(c)) row(c),
  ]..sort(byActivity);
  if (sealed.isNotEmpty) {
    groups.add(makeGroup(GroupKind.sealed, 'Sealed', sealed));
  }
  return groups;
}

/// When two rows with the same name sit under the same parent in the
/// same group, each gets its creation date after its name (9.26).
List<SidebarRow> _markSameNames(List<SidebarRow> rows) {
  final counts = <String, int>{};
  String key(SidebarRow r) =>
      '${r.indent}|${r.conversation.parentId ?? ''}|${r.name.toLowerCase()}';
  for (final r in rows) {
    counts[key(r)] = (counts[key(r)] ?? 0) + 1;
  }
  return [
    for (final r in rows)
      if ((counts[key(r)] ?? 0) > 1 && r.conversation.createdAt != null)
        SidebarRow(
          conversation: r.conversation,
          name: r.name,
          parentName: r.parentName,
          icon: r.icon,
          indent: r.indent,
          badge: r.badge,
          bold: r.bold,
          dimmed: r.dimmed,
          hiddenMark: r.hiddenMark,
          starred: r.starred,
          hasChildren: r.hasChildren,
          collapsed: r.collapsed,
          moreCount: r.moreCount,
          presence: r.presence,
          detail: sinceDate(r.conversation.createdAt!),
        )
      else
        r,
  ];
}

/// "since March 2026": the creation-date part of the detail line.
String sinceDate(DateTime t) => 'since ${_months[t.month - 1]} ${t.year}';

/// The one line that tells same-named channels apart, with its parts in
/// the fixed order of 9.26, leaving out what does not apply.
String detailLine(
  Conversation c, {
  String? serverName,
  String? parentName,
  String? sectionName,
}) => [
  ?serverName,
  if (parentName != null) '$parentName ›',
  ?sectionName,
  '${c.memberCount} ${c.memberCount == 1 ? 'member' : 'members'}'
      '${c.membership != null ? ", you're in it" : ''}',
  if (c.createdAt != null) sinceDate(c.createdAt!),
].join(' · ');
