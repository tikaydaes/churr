import '../core/models/conversation.dart';
import 'server_state.dart';
import 'sidebar_model.dart';

/// Lower case with common accents taken off, so "design" finds "Design
/// reviews" and "cafe" finds "Café" (docs/CLIENTS.md 9.26). Without a
/// collation package this covers the Latin letters people type most.
String foldName(String s) {
  const from = 'àáâãäåāăąçćčďèéêëēėęěìíîïīįłñńňòóôõöøōőřśšşßťùúûüūůűųýÿžźż';
  const to = 'aaaaaaaaacccdeeeeeeeeiiiiiilnnnoooooooorsssstuuuuuuuuyyzzz';
  final lower = s.toLowerCase();
  final b = StringBuffer();
  for (final r in lower.runes) {
    final ch = String.fromCharCode(r);
    final i = from.indexOf(ch);
    b.write(i < 0 ? ch : to[i]);
  }
  return b.toString();
}

/// Whether every word of [query] starts a word of [name] or of
/// [parentName] (typing both narrows to that parent, 9.26).
bool nameMatches(String query, String name, {String? parentName}) {
  final words = foldName(query)
      .split(RegExp(r'\s+'))
      .where((w) => w.isNotEmpty)
      .toList();
  if (words.isEmpty) return true;
  final hay = [
    ...foldName(name).split(RegExp(r'[\s\-_/.]+')),
    if (parentName != null)
      ...foldName(parentName).split(RegExp(r'[\s\-_/.]+')),
  ];
  final whole = foldName(name);
  return words.every(
    (w) => hay.any((h) => h.startsWith(w)) || whole.contains(w),
  );
}

/// The line that tells same-named channels apart, in the order of
/// docs/CLIENTS.md 9.26: server (when more than one is signed in),
/// `parent ›`, section, members and whether the person is in it, up to
/// two managers, and the creation date.
String channelDetail(
  ServerState server,
  Conversation c, {
  bool showServer = false,
  List<String> managerIds = const [],
}) {
  final parent = c.parentId == null ? null : server.conversation(c.parentId!);
  String? section;
  if (c.parentId == null) {
    for (final s in server.sections) {
      if (s.id == c.sectionId) section = s.name;
    }
  } else if (parent != null) {
    for (final s in server.sections) {
      if (s.id == parent.sectionId) section = s.name;
    }
  }
  final managers = [
    for (final id in managerIds.take(2)) server.accounts[id]?.name ?? 'someone',
  ];
  final n = c.memberCount;
  return [
    if (showServer) server.name,
    if (parent?.name != null) '${parent!.name} ›',
    ?section,
    '$n ${n == 1 ? 'member' : 'members'}${c.membership != null ? ", you're in it" : ''}',
    if (managers.isNotEmpty) 'managed by ${managers.join(', ')}',
    if (c.createdAt != null) sinceDate(c.createdAt!),
  ].join(' · ');
}

/// Managers when the server sends them on the conversation. The contract
/// has no such field (FEATURES.md 21.8 says browsing returns them); this
/// reads `manager_ids` if a server adds it.
List<String> managerIdsOf(Conversation c) {
  final v = c.raw['manager_ids'];
  return v is List ? [for (final x in v) '$x'] : const [];
}

/// The name as drawn in lists: `parent › name` for a sub-channel.
String channelTitle(ServerState server, Conversation c) {
  final name = conversationName(c, server.accounts, server.me?.id);
  final parent = c.parentId == null ? null : server.conversation(c.parentId!);
  return parent?.name == null ? name : '${parent!.name} › $name';
}
