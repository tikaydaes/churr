/// Which conversations have a row in the sidebar, in which group, and
/// how each row is drawn (docs/CLIENTS.md sections 9.1, 9.2, 9.25 and
/// 9.26; ruling R46).
///
/// The headings and their order are the person's own (ruling R83,
/// docs/FEATURES.md 21.4): their own sections, the admin's sections and
/// Channels in their order, then Chats and Sealed, each sorted
/// alphabetically, by last activity or by hand, starred rows first.
///
/// This is plain Dart with no Flutter in it: both layouts draw the same
/// rows, so the rules live here once and are tested without a widget.
library;

import '../core/models/account.dart';
import '../core/models/conversation.dart';
import '../core/models/json.dart';
import '../core/models/personal_layout.dart';

/// What kind of group a heading is.
enum GroupKind { own, section, channels, chats, sealed }

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
    required this.key,
    this.sort = SortMode.alphabetical,
    this.tops = const [],
    this.starredCount = 0,
    this.folded = false,
    int? total,
  }) : total = total ?? rows.length;
  final GroupKind kind;
  final String title;

  /// The rows drawn: all of them, or when [folded] only those that keep.
  final List<SidebarRow> rows;

  /// The heading's key in the person's layout (`p:1`, `s:<id>`, `s:`,
  /// `chats`, `sealed`), which is also what "collapsed" is remembered by.
  final String key;
  final SortMode sort;

  /// Every top-level conversation under the heading in order, drawn or
  /// folded away, the starred ones first: what a move is counted in.
  final List<String> tops;
  final int starredCount;
  final bool folded;

  /// How many conversations the group holds, drawn or not.
  final int total;

  bool get own => kind == GroupKind.own;

  /// Chats and Sealed stay at the bottom; the rest can be moved.
  bool get movable => key != 'chats' && key != 'sealed';
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
    this.layout = PersonalLayout.empty,
    DateTime? now,
  }) : now = now ?? DateTime.now();

  /// The person's own sidebar on this server.
  final PersonalLayout layout;

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

/// Every heading in the person's order: the channel headings as
/// `layout.headings` says (own sections, then the admin's in their order,
/// then Channels, for any it leaves out), then Chats and Sealed.
List<(String key, String title, GroupKind kind)> headingsOf(
  PersonalLayout layout,
  List<Section> sections,
) {
  final def = [
    for (final s in layout.sections) (s.key, s.name, GroupKind.own),
    for (final s in sections) ('s:${s.id}', s.name, GroupKind.section),
    ('s:', 'Channels', GroupKind.channels),
  ];
  final out = <(String, String, GroupKind)>[];
  for (final k in layout.headings) {
    for (final d in def) {
      if (d.$1 == k && !out.contains(d)) out.add(d);
    }
  }
  for (final d in def) {
    if (!out.contains(d)) out.add(d);
  }
  return [
    ...out,
    ('chats', 'Chats', GroupKind.chats),
    ('sealed', 'Sealed', GroupKind.sealed),
  ];
}

/// The groups of one server, in the person's order (see [headingsOf]).
/// A heading with no rows is left out, except the person's own sections,
/// which stay so something can be moved into them.
List<SidebarGroup> buildServerGroups(SidebarInput input) {
  final layout = input.layout;
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

  /// The parent as far as the sidebar knows it: one the account is in,
  /// or a public one it can see. Null draws the channel as top-level.
  Conversation? parentOf(Conversation c) => c.parentId == null || !c.isNamed
      ? null
      : byId[c.parentId] ?? input.outsideParents[c.parentId];

  final knownSections = {for (final s in input.sections) s.id};
  final ownSections = {for (final s in layout.sections) s.id};
  /// The section the person put a conversation in, while it exists.
  String? placed(String id) {
    final k = layout.placements[id];
    if (k == null) return null;
    final ok = PersonalLayout.isOwn(k)
        ? ownSections.contains(int.tryParse(k.substring(2)))
        : k == 's:' ||
              (k.startsWith('s:') && knownSections.contains(k.substring(2)));
    return ok ? k : null;
  }

  /// Where the server lists a conversation.
  String homeOf(Conversation c) {
    if (c.isSealed) return 'sealed';
    if (!c.isNamed) return 'chats';
    final section = (parentOf(c) ?? c).sectionId;
    return knownSections.contains(section) ? 's:$section' : 's:';
  }

  /// The heading a conversation is listed under: the section the person
  /// put it (or its parent) in, else its home.
  String keyOf(Conversation c) {
    final p = parentOf(c);
    return placed(c.id) ?? (p == null ? null : placed(p.id)) ?? homeOf(c);
  }

  final keyed = <String, List<Conversation>>{};
  final childrenOf = <String, List<Conversation>>{};
  for (final c in input.conversations) {
    if (!visible.contains(c.id)) continue;
    final p = parentOf(c);
    final k = keyOf(c);
    // A sub-channel sits under its parent's row when that row is there,
    // under the same heading.
    if (p != null && byId.containsKey(p.id) && visible.contains(p.id) && keyOf(p) == k) {
      childrenOf.putIfAbsent(p.id, () => []).add(c);
    } else {
      keyed.putIfAbsent(k, () => []).add(c);
    }
  }

  final groups = <SidebarGroup>[];
  for (final (key, title, kind) in headingsOf(layout, input.sections)) {
    final tops = sortForHeading(keyed[key] ?? const [], key, layout, input);
    if (tops.isEmpty && kind != GroupKind.own) continue;
    final rows = <SidebarRow>[];
    for (final c in tops) {
      final kids = (childrenOf[c.id] ?? const <Conversation>[])
          .map((k) => row(k, indent: 1))
          .toList()
        ..sort(_byName);
      final collapsed = input.collapsedParents.contains(c.id) && kids.isNotEmpty;
      final kept = [
        for (final k in kids)
          if (k.keepsWhenFolded(input.openConversationId)) k,
      ];
      final p = parentOf(c);
      final base = row(c, parentName: p?.name);
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
          starred: base.starred,
          presence: base.presence,
          hasChildren: kids.isNotEmpty,
          collapsed: collapsed,
          moreCount: collapsed ? kids.length - kept.length : 0,
        ),
      );
      // A folded parent keeps its unread and selected sub-channels.
      rows.addAll(collapsed ? kept : kids);
    }
    final folded = input.collapsedParents.contains(key);
    final marked = kind == GroupKind.chats || kind == GroupKind.sealed
        ? rows
        : _markSameNames(rows);
    groups.add(
      SidebarGroup(
        kind,
        title,
        folded ? foldRows(marked, openId: input.openConversationId) : marked,
        key: key,
        sort: layout.sortOf(key),
        tops: [for (final c in tops) c.id],
        starredCount: tops.where((c) => c.membership?.starred ?? false).length,
        folded: folded,
        total: rows.length,
      ),
    );
  }
  return groups;
}

/// Orders the top-level conversations of one heading: starred first, in
/// the order of stars; then by the heading's sort. A custom order lists
/// what it names first; the rest follow by name.
List<Conversation> sortForHeading(
  List<Conversation> list,
  String key,
  PersonalLayout layout,
  SidebarInput input,
) {
  String name(Conversation c) =>
      conversationName(c, input.accounts, input.myId).toLowerCase();
  int byName(Conversation a, Conversation b) {
    final n = name(a).compareTo(name(b));
    if (n != 0) return n;
    final ac = a.createdAt, bc = b.createdAt;
    if (ac == null || bc == null) return a.id.compareTo(b.id);
    return ac.compareTo(bc);
  }

  int rank(List<String> order, String id) {
    final i = order.indexOf(id);
    return i < 0 ? 1 << 30 : i;
  }

  DateTime at(Conversation c) =>
      c.lastMessageAt ?? c.createdAt ?? DateTime.fromMillisecondsSinceEpoch(0);
  bool starred(Conversation c) => c.membership?.starred ?? false;
  final sort = layout.sortOf(key);
  final order = layout.orderOf(key);
  return [...list]..sort((a, b) {
    final sa = starred(a), sb = starred(b);
    if (sa != sb) return sa ? -1 : 1;
    if (sa) {
      final r = rank(layout.stars, a.id).compareTo(rank(layout.stars, b.id));
      return r != 0 ? r : byName(a, b);
    }
    switch (sort) {
      case SortMode.activity:
        final r = at(b).compareTo(at(a));
        return r != 0 ? r : byName(a, b);
      case SortMode.custom:
        final r = rank(order, a.id).compareTo(rank(order, b.id));
        return r != 0 ? r : byName(a, b);
      case SortMode.alphabetical:
        return byName(a, b);
    }
  });
}

/// What moving a row to [index] among its heading's top-level rows sends.
/// A starred row moves among the stars; any other among the rest, which
/// makes the heading's order custom.
List<Json> reorderOps(SidebarGroup g, String id, int index, PersonalLayout layout) {
  final i = g.tops.indexOf(id);
  if (i < 0) return const [];
  final starred = i < g.starredCount;
  final pool = starred
      ? g.tops.sublist(0, g.starredCount)
      : g.tops.sublist(g.starredCount);
  final offset = starred ? 0 : g.starredCount;
  final ids = [...pool]..remove(id);
  ids.insert((index - offset).clamp(0, ids.length), id);
  if (starred) {
    return [
      {
        'op': 'order_stars',
        'ids': [...ids, ...layout.stars.where((x) => !ids.contains(x))],
      },
    ];
  }
  return [
    {'op': 'reorder', 'key': g.key, 'ids': _keepRest(ids, layout, g.key)},
  ];
}

/// What moving a conversation under another heading sends: into one of
/// the person's own sections, or back where the server lists it. Under a
/// heading sorted by hand it goes in at [index].
List<Json> moveOps(String id, SidebarGroup? target, String key, int? index, PersonalLayout layout) {
  final op = <String, Object?>{'op': 'move', 'conversation_id': id, 'key': key};
  if (layout.sortOf(key) == SortMode.custom) {
    final starredCount = target?.starredCount ?? 0;
    final ids = [
      for (final x in (target?.tops ?? const <String>[]).skip(starredCount))
        if (x != id) x,
    ];
    ids.insert(((index ?? ids.length + starredCount) - starredCount).clamp(0, ids.length), id);
    op['ids'] = _keepRest(ids, layout, key);
  }
  return [op];
}

/// Keeps, after a new order of the rows drawn, the conversations the old
/// order named that have no row now, so they are not forgotten.
List<String> _keepRest(List<String> ids, PersonalLayout layout, String key) => [
  ...ids,
  ...layout.orderOf(key).where((x) => !ids.contains(x)),
];

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
