import 'dart:async';

import '../../../l10n/app_localizations.dart';
import '../sealed/new_sealed_chat.dart';

import 'package:flutter/material.dart';

import '../../../core/api/approvals.dart';
import '../../../core/api/channels.dart';
import '../../../core/api/errors.dart';
import '../../../core/models/conversation.dart';
import '../../../state/app_state.dart';
import '../../../state/channel_actions.dart';
import '../../../state/channel_detail.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import '../../widgets/avatar.dart';
import 'common.dart';

/// "New…": a new channel or a new chat.
Future<void> openNewMenu(BuildContext context, {ServerState? server}) async {
  final what = await showDialog<String>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (ctx) => SimpleDialog(
      title: Text(L10n.of(ctx).newMenuTitle),
      children: [
        SimpleDialogOption(
          key: const Key('new-channel'),
          onPressed: () => Navigator.pop(ctx, 'channel'),
          child: ListTile(
            leading: const Glyph(ChurrIcons.hash),
            title: Text(L10n.of(ctx).newChannelOption),
          ),
        ),
        SimpleDialogOption(
          key: const Key('new-chat'),
          onPressed: () => Navigator.pop(ctx, 'chat'),
          child: ListTile(
            leading: const Glyph(ChurrIcons.chat),
            title: Text(L10n.of(ctx).chat),
          ),
        ),
        // Sealed chats (lib/ui/screens/sealed/new_sealed_chat.dart).
        SimpleDialogOption(
          key: const Key('new-sealed-chat'),
          onPressed: () => Navigator.pop(ctx, 'sealed'),
          child: ListTile(
            leading: const Glyph(ChurrIcons.sealed),
            title: Text(L10n.of(ctx).newSealedTitle),
          ),
        ),
      ],
    ),
  );
  if (!context.mounted || what == null) return;
  if (what == 'channel') {
    await openNewChannel(context, server: server);
  } else if (what == 'chat') {
    await openNewChat(context, server: server);
  } else if (what == 'sealed') {
    await openNewSealedChat(context, server: server);
  }
}

/// "New channel", or "New sub-channel" with [parentId] set.
Future<void> openNewChannel(
  BuildContext context, {
  ServerState? server,
  String? parentId,
}) async {
  final app = AppScope.read(context);
  final s = await pickServer(context, app, preferred: server);
  if (s == null || !context.mounted) return;
  await showScreen<void>(
    context,
    title: parentId == null
        ? L10n.of(context).newChannelTitle(s.name)
        : L10n.of(context).newSubchannelTitle,
    width: 560,
    height: 640,
    child: NewChannelForm(app: app, server: s, parentId: parentId),
  );
}

/// The new-channel form: a free-form name (any script, spaces and
/// capitals; not unique), topic, public or private, announcement, a
/// section, and the optional "Under" parent (one level only). Channels
/// the person can see that already have the name are listed with their
/// detail lines; the form never refuses for that (9.26).
class NewChannelForm extends StatefulWidget {
  const NewChannelForm({
    super.key,
    required this.app,
    required this.server,
    this.parentId,
  });
  final AppState app;
  final ServerState server;
  final String? parentId;

  @override
  State<NewChannelForm> createState() => _NewChannelFormState();
}

class _NewChannelFormState extends State<NewChannelForm> {
  final name = TextEditingController();
  final topic = TextEditingController();

  /// The note for whoever decides, once the server said new channels
  /// here wait for approval (`approval_required`).
  final note = TextEditingController();
  bool needsApproval = false;
  bool private = false;
  bool announcement = false;
  String? sectionId;
  late String? parentId = widget.parentId;
  String? error;
  bool busy = false;
  List<Conversation> sameName = const [];
  Timer? _debounce;

  ServerState get s => widget.server;

  @override
  void dispose() {
    name.dispose();
    topic.dispose();
    note.dispose();
    _debounce?.cancel();
    super.dispose();
  }

  /// Parents on offer: top-level named channels the person is a member of.
  List<Conversation> get parents => [
    for (final r in s.conversations)
      if (r.conversation.isNamed &&
          r.conversation.parentId == null &&
          r.conversation.membership != null)
        r.conversation,
  ]..sort((a, b) => foldName(a.name!).compareTo(foldName(b.name!)));

  void _nameChanged(String v) {
    _debounce?.cancel();
    _debounce = Timer(
      const Duration(milliseconds: 300),
      () => _findSameName(v.trim()),
    );
    setState(() => error = null);
  }

  Future<void> _findSameName(String v) async {
    if (v.isEmpty) {
      if (mounted) setState(() => sameName = const []);
      return;
    }
    final want = foldName(v);
    final found = <String, Conversation>{
      for (final r in s.conversations)
        if (r.conversation.isNamed && foldName(r.conversation.name!) == want)
          r.conversation.id: r.conversation,
    };
    try {
      for (final c in await s.api.browseChannels(q: v)) {
        if (c.isNamed && foldName(c.name!) == want) {
          found.putIfAbsent(c.id, () => c);
        }
      }
    } on Exception {
      // The local list is enough to warn with.
    }
    if (mounted && name.text.trim() == v) {
      setState(() => sameName = found.values.toList());
    }
  }

  Future<void> _create() async {
    final n = name.text;
    final problem = checkChannelName(n);
    if (problem != null) {
      setState(() => error = problem);
      return;
    }
    setState(() => busy = true);
    try {
      final c = await ChannelActions(s).create(
        name: n,
        private: private,
        topic: topic.text.trim(),
        sectionId: sectionId,
        parentId: parentId,
        announcement: announcement,
      );
      if (!mounted) return;
      Navigator.of(context).maybePop();
      widget.app.openConversation(OpenTarget(s.localId, c.id));
    } on ApiException catch (e) {
      // New channels wait for approval here: the form offers to ask.
      if (e.code == 'approval_required') {
        if (mounted) {
          setState(() {
            busy = false;
            needsApproval = true;
            error = null;
          });
        }
        return;
      }
      if (mounted) {
        setState(() {
          busy = false;
          error = refusal(e);
        });
      }
    } on Exception catch (e) {
      if (mounted) {
        setState(() {
          busy = false;
          error = refusal(e);
        });
      }
    }
  }

  /// Asks for the channel as the form describes it (`requestChannel`),
  /// and says it was sent.
  Future<void> _ask() async {
    final n = name.text;
    final problem = checkChannelName(n);
    if (problem != null) {
      setState(() => error = problem);
      return;
    }
    setState(() => busy = true);
    try {
      final t = topic.text.trim();
      await s.api.requestChannel({
        'name': n,
        'private': private,
        if (t.isNotEmpty) 'topic': t,
        if (parentId == null) 'section_id': ?sectionId,
        'parent_id': ?parentId,
        if (announcement) 'announcement': true,
      }, note: note.text.trim());
      if (!mounted) return;
      say(context, L10n.of(context).channelRequestSent(n));
      Navigator.of(context).maybePop();
    } on Exception catch (e) {
      if (mounted) {
        setState(() {
          busy = false;
          error = refusal(e);
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final many = widget.app.servers.length > 1;
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        TextField(
          key: const Key('channel-name'),
          controller: name,
          autofocus: true,
          maxLength: 64,
          decoration: InputDecoration(
            labelText: L10n.of(context).channelNameLabel,
            helperText: L10n.of(context).channelNameHelp,
          ),
          onChanged: _nameChanged,
        ),
        if (sameName.isNotEmpty)
          Container(
            key: const Key('same-name'),
            margin: const EdgeInsets.only(bottom: 12),
            padding: const EdgeInsets.all(10),
            decoration: BoxDecoration(
              border: Border.all(color: c.yellowInk, width: 1.5),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  sameName.length == 1
                      ? L10n.of(context).sameNameOne
                      : L10n.of(context).sameNameMany(sameName.length),
                  style: TextStyle(color: c.yellowInk),
                ),
                for (final x in sameName)
                  Padding(
                    padding: const EdgeInsets.only(top: 4),
                    child: Text(
                      '${x.name} — ${channelDetail(s, x, showServer: many)}',
                      style: monoStyle(context),
                    ),
                  ),
              ],
            ),
          ),
        TextField(
          controller: topic,
          maxLength: 250,
          decoration: InputDecoration(
            labelText: L10n.of(context).channelTopicOptional,
          ),
        ),
        Row(
          children: [
            Expanded(child: Text(L10n.of(context).channelUnder)),
            Flexible(
              child: DropdownButton<String?>(
                elevation: 0,
                icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                isExpanded: true,
                key: const Key('channel-under'),
                value: parentId,
                items: [
                  DropdownMenuItem(
                    value: null,
                    child: Text(L10n.of(context).channelTopLevel),
                  ),
                  for (final p in parents)
                    DropdownMenuItem(value: p.id, child: Text(p.name!)),
                ],
                onChanged: (v) => setState(() => parentId = v),
              ),
            ),
          ],
        ),
        if (parentId != null)
          Text(L10n.of(context).subchannelExplained, style: monoStyle(context)),
        if (parentId == null)
          Row(
            children: [
              Expanded(child: Text(L10n.of(context).channelSection)),
              Flexible(
                child: DropdownButton<String?>(
                  elevation: 0,
                  icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                  isExpanded: true,
                  value: sectionId,
                  items: [
                    DropdownMenuItem(
                      value: null,
                      child: Text(L10n.of(context).none),
                    ),
                    for (final sec in s.sections)
                      DropdownMenuItem(value: sec.id, child: Text(sec.name)),
                  ],
                  onChanged: (v) => setState(() => sectionId = v),
                ),
              ),
            ],
          ),
        ChurrSwitchTile(
          key: const Key('channel-private'),
          contentPadding: EdgeInsets.zero,
          title: Text(L10n.of(context).channelPrivate),
          subtitle: Text(L10n.of(context).channelPrivateNote),
          value: private,
          onChanged: (v) => setState(() => private = v),
        ),
        ChurrSwitchTile(
          contentPadding: EdgeInsets.zero,
          title: Text(L10n.of(context).channelAnnouncements),
          subtitle: Text(L10n.of(context).channelAnnouncementsNote),
          value: announcement,
          onChanged: (v) => setState(() => announcement = v),
        ),
        if (error != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(error!, style: TextStyle(color: c.pink)),
          ),
        if (needsApproval) ...[
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(
              L10n.of(context).channelNeedsApproval,
              key: const Key('channel-needs-approval'),
              style: TextStyle(color: c.yellowInk),
            ),
          ),
          TextField(
            key: const Key('channel-approval-note'),
            controller: note,
            maxLength: 500,
            minLines: 1,
            maxLines: 4,
            decoration: InputDecoration(
              labelText: L10n.of(context).approvalNoteLabel,
            ),
          ),
        ],
        const SizedBox(height: 12),
        Align(
          alignment: Alignment.centerRight,
          child: needsApproval
              ? FilledButton(
                  key: const Key('channel-ask-approval'),
                  onPressed: busy ? null : _ask,
                  child: Text(L10n.of(context).askForApproval),
                )
              : FilledButton(
                  key: const Key('channel-create'),
                  onPressed: busy ? null : _create,
                  child: Text(L10n.of(context).create),
                ),
        ),
      ],
    );
  }
}

/// "New chat": pick people; opens the ad-hoc channel with exactly them,
/// or makes one.
Future<void> openNewChat(BuildContext context, {ServerState? server}) async {
  final app = AppScope.read(context);
  final s = await pickServer(context, app, preferred: server);
  if (s == null || !context.mounted) return;
  await showScreen<void>(
    context,
    title: L10n.of(context).newChatTitle(s.name),
    width: 520,
    height: 600,
    child: PeoplePicker(
      server: s,
      action: L10n.of(context).openChat,
      notice: L10n.of(context).chatHistoryNotice,
      onDone: (ids) async {
        final c = await ChannelActions(s).openChat(ids);
        app.openConversation(OpenTarget(s.localId, c.id));
        return true;
      },
    ),
  );
}

/// Picks people of one server, with a filter box. [onDone] gets the
/// ids; a refusal it throws is shown, and true closes the screen.
class PeoplePicker extends StatefulWidget {
  const PeoplePicker({
    super.key,
    required this.server,
    required this.action,
    required this.onDone,
    this.notice,
    this.exclude = const {},
  });
  final ServerState server;
  final String action;
  final String? notice;
  final Set<String> exclude;

  final Future<bool> Function(List<String> ids) onDone;

  @override
  State<PeoplePicker> createState() => _PeoplePickerState();
}

class _PeoplePickerState extends State<PeoplePicker> {
  final picked = <String>[];
  String q = '';
  bool busy = false;
  String? error;

  Future<void> _done() async {
    setState(() => busy = true);
    try {
      final close = await widget.onDone(List.of(picked));
      if (close && mounted) Navigator.of(context).maybePop();
    } on Exception catch (e) {
      if (mounted) setState(() => error = refusal(e));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final s = widget.server;
    final people = [
      for (final a in s.accounts.values)
        if (a.id != s.me?.id &&
            a.removedAt == null &&
            !widget.exclude.contains(a.id) &&
            (nameMatches(q, a.name) ||
                foldName(a.username).startsWith(foldName(q))))
          a,
    ]..sort((a, b) => foldName(a.name).compareTo(foldName(b.name)));
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
          child: TextField(
            key: const Key('people-filter'),
            autofocus: true,
            decoration: InputDecoration(
              hintText: L10n.of(context).findPeople,
              prefixIcon: const Glyph(ChurrIcons.search),
            ),
            onChanged: (v) => setState(() => q = v),
          ),
        ),
        if (picked.isNotEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Wrap(
              spacing: 6,
              children: [
                for (final id in picked)
                  InputChip(
                    label: Text(
                      s.accounts[id]?.name ?? L10n.of(context).someone,
                    ),
                    onDeleted: () => setState(() => picked.remove(id)),
                  ),
              ],
            ),
          ),
        if (widget.notice != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 6, 16, 0),
            child: Text(
              widget.notice!,
              style: monoStyle(context, color: c.yellowInk),
            ),
          ),
        Expanded(
          child: ListView(
            children: [
              for (final a in people)
                CheckboxListTile(
                  key: Key('person-${a.id}'),
                  dense: true,
                  secondary: Avatar(account: a, server: s, size: 24),
                  title: Text(a.name),
                  subtitle: Text('@${a.username}'),
                  value: picked.contains(a.id),
                  onChanged: (v) => setState(
                    () => v == true ? picked.add(a.id) : picked.remove(a.id),
                  ),
                ),
            ],
          ),
        ),
        if (error != null)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Text(error!, style: TextStyle(color: c.pink)),
          ),
        Padding(
          padding: const EdgeInsets.all(12),
          child: Align(
            alignment: Alignment.centerRight,
            child: FilledButton(
              key: const Key('people-done'),
              onPressed: busy || picked.isEmpty ? null : _done,
              child: Text(widget.action),
            ),
          ),
        ),
      ],
    );
  }
}
