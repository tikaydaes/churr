import 'json.dart';

/// How one heading of the sidebar sorts its rows.
enum SortMode {
  alphabetical,
  activity,
  custom;

  static SortMode parse(Object? v) => switch (v) {
    'activity' => activity,
    'custom' => custom,
    _ => alphabetical,
  };
}

/// One of the person's own sections. Its heading key is `p:<id>`.
class OwnSection {
  const OwnSection(this.id, this.name);
  final int id;
  final String name;
  String get key => 'p:$id';
}

/// How one heading sorts, where it is not the default.
class HeadingSort {
  const HeadingSort(this.mode, this.order);
  final SortMode mode;

  /// The custom order; conversations not in it follow, by name.
  final List<String> order;
}

/// A person's own sidebar on one server (ruling R83, docs/FEATURES.md
/// 21.4; schema `Sidebar`): their sections, which conversations they put
/// in them, the order of the headings, how each heading sorts, and the
/// order of stars. Kept by the server, so it is the same on every device
/// and in the web chat.
///
/// Heading keys: `p:<n>` one of the person's own sections, `s:<id>` an
/// admin section, `s:` channels in no section, `chats`, `sealed`.
class PersonalLayout {
  const PersonalLayout({
    this.version = 0,
    this.sections = const [],
    this.headings = const [],
    this.sorts = const {},
    this.placements = const {},
    this.stars = const [],
  });

  static const empty = PersonalLayout();

  factory PersonalLayout.fromJson(Json j) => PersonalLayout(
    version: asInt(j['version']),
    sections: [
      for (final s in (j['sections'] as List? ?? const []))
        if (s is Map) OwnSection(asInt(s['id']), s['name'] as String? ?? ''),
    ],
    headings: [
      for (final h in (j['headings'] as List? ?? const []))
        if (h is String) h,
    ],
    sorts: {
      for (final g in (j['groups'] as List? ?? const []))
        if (g is Map && g['key'] is String)
          g['key'] as String: HeadingSort(SortMode.parse(g['sort']), [
            for (final id in (g['order'] as List? ?? const []))
              if (id is String) id,
          ]),
    },
    placements: {
      for (final p in (j['placements'] as List? ?? const []))
        if (p is Map && p['conversation_id'] is String)
          p['conversation_id'] as String: asInt(p['section_id']),
    },
    stars: [
      for (final id in (j['stars'] as List? ?? const []))
        if (id is String) id,
    ],
  );

  final int version;
  final List<OwnSection> sections;
  final List<String> headings;
  final Map<String, HeadingSort> sorts;

  /// Conversation id → the number of the person's own section.
  final Map<String, int> placements;
  final List<String> stars;

  Json toJson() => {
    'version': version,
    'sections': [
      for (final s in sections) {'id': s.id, 'name': s.name},
    ],
    'headings': headings,
    'groups': [
      for (final e in sorts.entries)
        {'key': e.key, 'sort': e.value.mode.name, 'order': e.value.order},
    ],
    'placements': [
      for (final e in placements.entries)
        {'conversation_id': e.key, 'section_id': e.value},
    ],
    'stars': stars,
  };

  static bool isOwn(String key) => RegExp(r'^p:\d+$').hasMatch(key);

  static SortMode defaultSort(String key) =>
      key == 'chats' || key == 'sealed' ? SortMode.activity : SortMode.alphabetical;

  SortMode sortOf(String key) => sorts[key]?.mode ?? defaultSort(key);
  List<String> orderOf(String key) => sorts[key]?.order ?? const [];
}
