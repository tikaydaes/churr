import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import '../../../state/app_state.dart';
import '../../../state/channel_detail.dart';
import '../../../state/conversation_intents.dart';
import '../../../state/server_state.dart';
import '../../../state/unified_search.dart';
import '../../message/checklist.dart' show shortWhen;
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import '../channels/common.dart';

/// Opens search (docs/CLIENTS.md 9.8). From inside a conversation it
/// starts with that server and conversation set.
Future<void> openSearch(
  BuildContext context, {
  ServerState? server,
  String? conversationId,
  bool hasFile = false,
}) {
  final app = AppScope.read(context);
  final conv = conversationId == null
      ? null
      : server?.conversation(conversationId);
  return showScreen<void>(
    context,
    title: L10n.of(context).searchTitle,
    width: 720,
    height: 720,
    child: SearchScreen(
      app: app,
      serverId: server?.localId,
      inConversation: conv == null || server == null || conv.isSealed
          ? null
          : ServerPick(server.localId, conv.id, channelTitle(server, conv)),
      sealedOpen: conv?.isSealed ?? false,
      hasFile: hasFile,
    ),
  );
}

/// The search screen: a query box with filters for server, conversation,
/// author, dates and "has a file"; results newest first across every
/// signed-in server, each with its server, conversation, author, time
/// and the matching lines. Opening a result shows it in place.
class SearchScreen extends StatefulWidget {
  const SearchScreen({
    super.key,
    required this.app,
    this.serverId,
    this.inConversation,
    this.hasFile = false,
    this.sealedOpen = false,
  });
  final AppState app;
  final String? serverId;
  final ServerPick? inConversation;
  final bool sealedOpen;

  /// Starts with "has a file" set, and searches at once: "Files posted
  /// here" from a conversation's header.
  final bool hasFile;

  @override
  State<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends State<SearchScreen> {
  late final UnifiedSearch search = UnifiedSearch(
    widget.app,
    serverId: widget.serverId,
    inConversation: widget.inConversation,
  );
  final box = TextEditingController();

  @override
  void initState() {
    super.initState();
    if (widget.hasFile) {
      search.hasFile = true;
      search.run();
    }
  }

  @override
  void dispose() {
    search.dispose();
    box.dispose();
    super.dispose();
  }

  void _run() {
    search.text = box.text;
    search.run();
  }

  ServerState? get _filterServer {
    final id =
        search.inConversation?.serverId ??
        search.from?.serverId ??
        search.serverId;
    return id == null ? null : widget.app.server(id);
  }

  Future<void> _pickConversation() async {
    final s = _filterServer ?? await pickServer(context, widget.app);
    if (s == null || !mounted) return;
    final rows = [
      for (final r in s.conversations)
        if (!r.conversation.isSealed) r.conversation,
    ];
    final picked = await showDialog<ServerPick>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => _Picker(
        title: L10n.of(context).searchInWhich,
        items: [
          for (final c in rows)
            (
              ServerPick(s.localId, c.id, channelTitle(s, c)),
              channelDetail(s, c, showServer: widget.app.servers.length > 1),
            ),
        ],
      ),
    );
    if (picked != null) {
      setState(() => search.inConversation = picked);
      _run();
    }
  }

  Future<void> _pickAuthor() async {
    final s = _filterServer ?? await pickServer(context, widget.app);
    if (s == null || !mounted) return;
    final people = s.accounts.values.toList()
      ..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    final picked = await showDialog<ServerPick>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => _Picker(
        title: L10n.of(context).searchFromWhom,
        items: [
          for (final a in people)
            (ServerPick(s.localId, a.id, a.name), '@${a.username}'),
        ],
      ),
    );
    if (picked != null) {
      setState(() => search.from = picked);
      _run();
    }
  }

  Future<void> _pickDate(bool since) async {
    final now = DateTime.now();
    final d = await showDatePicker(
      context: context,
      firstDate: DateTime(2000),
      lastDate: DateTime(now.year + 1),
      initialDate: (since ? search.since : search.until) ?? now,
    );
    if (d == null) return;
    setState(() => since ? search.since = d : search.until = d);
    _run();
  }

  void _open(ServerHit h) {
    final app = widget.app;
    final target = OpenTarget(h.server.localId, h.message.conversationId);
    Navigator.of(context).maybePop();
    if (h.message.parentId != null) {
      ConversationIntents.instance.openThread(app, target, h.message.parentId!);
    } else {
      ConversationIntents.instance.openAt(app, target, h.message.id);
    }
  }

  String _date(DateTime d) =>
      '${d.year}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: search,
      builder: (context, _) {
        final results = search.results;
        final many = widget.app.servers.length > 1;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
              child: TextField(
                key: const Key('search-box'),
                controller: box,
                autofocus: true,
                textInputAction: TextInputAction.search,
                onSubmitted: (_) => _run(),
                decoration: InputDecoration(
                  hintText: L10n.of(context).searchHint,
                  prefixIcon: const Glyph(ChurrIcons.search),
                  suffixIcon: IconButton(
                    tooltip: L10n.of(context).searchTitle,
                    icon: const Glyph(ChurrIcons.arrowRight),
                    onPressed: _run,
                  ),
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: Wrap(
                spacing: 6,
                runSpacing: 4,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  if (many)
                    DropdownButton<String?>(
                      elevation: 0,
                      icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                      key: const Key('search-server'),
                      value: search.serverId,
                      underline: const SizedBox.shrink(),
                      items: [
                        DropdownMenuItem(
                          value: null,
                          child: Text(L10n.of(context).searchAllServers),
                        ),
                        for (final s in widget.app.servers)
                          DropdownMenuItem(
                            value: s.localId,
                            child: Text(s.name),
                          ),
                      ],
                      onChanged: (v) {
                        setState(() {
                          search.serverId = v;
                          // Conversation and author filters belong to one server.
                          if (v != null &&
                              search.inConversation?.serverId != v) {
                            search.inConversation = null;
                          }
                          if (v != null && search.from?.serverId != v) {
                            search.from = null;
                          }
                        });
                        _run();
                      },
                    ),
                  InputChip(
                    key: const Key('search-in'),
                    avatar: const Glyph(ChurrIcons.hash, size: 14),
                    label: Text(
                      search.inConversation == null
                          ? L10n.of(context).searchIn
                          : L10n.of(context)
                                .searchInNamed(search.inConversation!.label),
                    ),
                    onPressed: _pickConversation,
                    onDeleted: search.inConversation == null
                        ? null
                        : () {
                            setState(() => search.inConversation = null);
                            _run();
                          },
                  ),
                  InputChip(
                    key: const Key('search-from'),
                    avatar: const Glyph(ChurrIcons.person, size: 14),
                    label: Text(
                      search.from == null
                          ? L10n.of(context).searchFrom
                          : L10n.of(context)
                                .searchFromNamed(search.from!.label),
                    ),
                    onPressed: _pickAuthor,
                    onDeleted: search.from == null
                        ? null
                        : () {
                            setState(() => search.from = null);
                            _run();
                          },
                  ),
                  InputChip(
                    label: Text(
                      search.since == null
                          ? L10n.of(context).searchSince
                          : L10n.of(context)
                                .searchSinceDate(_date(search.since!)),
                    ),
                    onPressed: () => _pickDate(true),
                    onDeleted: search.since == null
                        ? null
                        : () {
                            setState(() => search.since = null);
                            _run();
                          },
                  ),
                  InputChip(
                    label: Text(
                      search.until == null
                          ? L10n.of(context).searchUntil
                          : L10n.of(context)
                                .searchUntilDate(_date(search.until!)),
                    ),
                    onPressed: () => _pickDate(false),
                    onDeleted: search.until == null
                        ? null
                        : () {
                            setState(() => search.until = null);
                            _run();
                          },
                  ),
                  FilterChip(
                    key: const Key('search-has-file'),
                    label: Text(L10n.of(context).searchHasFile),
                    selected: search.hasFile,
                    onSelected: (v) {
                      setState(() => search.hasFile = v);
                      _run();
                    },
                  ),
                ],
              ),
            ),
            if (widget.sealedOpen)
              _Line(text: L10n.of(context).searchNoSealed, color: c.yellowInk),
            for (final f in search.failures.values)
              _Line(text: L10n.of(context).searchCouldNot(f), color: c.pink),
            if (search.slowServers.isNotEmpty)
              _Line(
                text: L10n.of(context)
                    .searchWaiting(search.slowServers.join(', ')),
                color: c.dim,
              ),
            Divider(height: 8, thickness: 1.5, color: c.line),
            Expanded(
              child: results.isEmpty
                  ? Center(
                      child: search.loading
                          ? const Busy()
                          : Text(
                              search.searched
                                  ? L10n.of(context).searchNothing
                                  : L10n.of(context).searchEmpty,
                              style: TextStyle(color: c.dim),
                            ),
                    )
                  : ListView.separated(
                      key: const Key('search-results'),
                      itemCount: results.length + (search.hasMore ? 1 : 0),
                      separatorBuilder: (_, _) =>
                          Divider(height: 1, color: c.line),
                      itemBuilder: (context, i) {
                        if (i == results.length) {
                          return Padding(
                            padding: const EdgeInsets.all(8),
                            child: Center(
                              child: TextButton(
                                key: const Key('search-more'),
                                onPressed: search.loading
                                    ? null
                                    : search.loadMore,
                                child: Text(L10n.of(context).searchMore),
                              ),
                            ),
                          );
                        }
                        return _ResultTile(
                          hit: results[i],
                          many: many,
                          onOpen: () => _open(results[i]),
                        );
                      },
                    ),
            ),
          ],
        );
      },
    );
  }
}

class _Line extends StatelessWidget {
  const _Line({required this.text, required this.color});
  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(16, 4, 16, 0),
    child: Text(text, style: TextStyle(fontSize: 13, color: color)),
  );
}

class _ResultTile extends StatelessWidget {
  const _ResultTile({
    required this.hit,
    required this.many,
    required this.onOpen,
  });
  final ServerHit hit;
  final bool many;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final s = hit.server;
    final m = hit.message;
    final conv = s.conversation(m.conversationId) ?? hit.hit.conversation;
    final where = conv == null
        ? L10n.of(context).searchAConversation
        : channelTitle(s, conv);
    final who = s.accounts[m.authorId]?.name ?? L10n.of(context).someone;
    final text = (hit.hit.snippet ?? (m.text.isNotEmpty ? m.text : m.fallback))
        .trim();
    return Semantics(
      button: true,
      label: many
          ? L10n.of(context).searchHitLabelServer(
              who,
              where,
              shortWhen(m.createdAt),
              s.name,
              text,
            )
          : L10n.of(context)
                .searchHitLabel(who, where, shortWhen(m.createdAt), text),
      excludeSemantics: true,
      child: InkWell(
        onTap: onOpen,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  if (many) ...[
                    ServerMark(server: s),
                    const SizedBox(width: 6),
                  ],
                  Flexible(
                    child: Text(
                      where,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        color: c.lavender,
                        fontWeight: FontWeight.w700,
                        fontSize: 13,
                      ),
                    ),
                  ),
                  if (m.parentId != null) ...[
                    const SizedBox(width: 6),
                    Text(L10n.of(context).inAThread, style: monoStyle(context)),
                  ],
                  const Spacer(),
                  Text(shortWhen(m.createdAt), style: monoStyle(context)),
                ],
              ),
              const SizedBox(height: 2),
              Text(who, style: const TextStyle(fontWeight: FontWeight.w700)),
              Text(text, maxLines: 3, overflow: TextOverflow.ellipsis),
            ],
          ),
        ),
      ),
    );
  }
}

/// A list to pick one thing from, with a filter box and a detail line
/// under each entry.
class _Picker extends StatefulWidget {
  const _Picker({required this.title, required this.items});
  final String title;
  final List<(ServerPick, String)> items;

  @override
  State<_Picker> createState() => _PickerState();
}

class _PickerState extends State<_Picker> {
  String q = '';

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final shown = [
      for (final it in widget.items)
        if (nameMatches(q, it.$1.label)) it,
    ];
    return Dialog(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 460, maxHeight: 520),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
              child: Text(
                widget.title,
                style: const TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w800,
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: TextField(
                autofocus: true,
                decoration: InputDecoration(
                  hintText: L10n.of(context).filter,
                  prefixIcon: const Glyph(ChurrIcons.search),
                ),
                onChanged: (v) => setState(() => q = v),
              ),
            ),
            Expanded(
              child: ListView(
                children: [
                  for (final it in shown)
                    ListTile(
                      dense: true,
                      title: Text(it.$1.label),
                      subtitle: Text(
                        it.$2,
                        style: TextStyle(color: c.dim, fontSize: 11),
                      ),
                      onTap: () => Navigator.pop(context, it.$1),
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
