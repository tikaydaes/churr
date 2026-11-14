import 'dart:async';

import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/hubs.dart';
import '../../../core/models/conversation.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/hub_controllers.dart';
import '../../../state/server_state.dart';
import '../../theme/churr_icons.dart';
import '../../theme/shapes.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../account/common.dart';

/// The hub screens (rulings R56, R59): sharing a channel to a hub, a
/// hub's directory for a server's owner, and a hub channel's details with
/// its moderators' tools. Linking servers and hubs stays in the web admin
/// console; these screens link to it.

String listingName(L10n t, Listing l) => switch (l) {
  Listing.unlisted => t.listingUnlisted,
  Listing.request => t.listingRequest,
  Listing.open => t.listingOpen,
};

String listingNote(L10n t, Listing l) => switch (l) {
  Listing.unlisted => t.listingUnlistedNote,
  Listing.request => t.listingRequestNote,
  Listing.open => t.listingOpenNote,
};

String _refusal(Object e) => e is ApiException ? e.message : '$e';

/// Opens the web admin console's linked servers page.
Future<void> openLinkedServersConsole(ServerState server) => launchUrl(
  Uri.parse(server.entry.address).replace(path: '/admin/linked-servers'),
  mode: LaunchMode.externalApplication,
);

// ---------------------------------------------------------------- hubs

/// Opens the hubs this server is on, for its owner.
Future<void> openHubs(BuildContext context, ServerState server) =>
    showAccountScreen<void>(
      context,
      title: L10n.of(context).hubsOf(server.name),
      width: 640,
      child: HubsScreen(server: server),
    );

class HubsScreen extends StatefulWidget {
  const HubsScreen({super.key, required this.server});
  final ServerState server;

  @override
  State<HubsScreen> createState() => _HubsScreenState();
}

class _HubsScreenState extends State<HubsScreen> {
  List<Peer>? hubs;
  String? error;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final peers = await widget.server.api.listPeers();
      if (mounted) {
        setState(
          () => hubs = [
            for (final p in peers)
              if (p.isHub && p.state != 'removed') p,
          ],
        );
      }
    } on Exception catch (e) {
      if (mounted) setState(() => error = _refusal(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
      children: [
        Text(t.hubsNote, style: TextStyle(color: c.dim, fontSize: 13)),
        const SizedBox(height: 8),
        Align(
          alignment: AlignmentDirectional.centerStart,
          child: OutlinedButton.icon(
            icon: const Glyph(ChurrIcons.external, size: 16),
            label: Text(t.linkInConsole),
            onPressed: () => openLinkedServersConsole(widget.server),
          ),
        ),
        if (error != null) ErrorLine(error!),
        if (hubs == null && error == null)
          Text(t.loading, style: TextStyle(color: c.dim)),
        if (hubs != null && hubs!.isEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 12),
            child: Text(t.noHubs, style: TextStyle(color: c.dim)),
          ),
        for (final h in hubs ?? const <Peer>[])
          Container(
            key: Key('hub-${h.id}'),
            margin: const EdgeInsets.only(top: 8),
            color: c.panel,
            padding: const EdgeInsets.all(12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        h.name,
                        style: ChurrType.title(c.ink, size: 16),
                      ),
                    ),
                    Tag(
                      h.active ? t.hubConnected : h.state,
                      fill: h.active ? c.cyan : c.yellow,
                      degrees: 2,
                    ),
                  ],
                ),
                if (h.toldClock != null) _ClockLine(clock: h.toldClock!),
                const SizedBox(height: 8),
                FilledButton(
                  key: Key('open-directory-${h.id}'),
                  onPressed: h.active
                      ? () => openHubDirectory(context, widget.server, h)
                      : null,
                  child: Text(t.hubDirectory),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

/// The figures the hub measured of this server's clock.
class _ClockLine extends StatelessWidget {
  const _ClockLine({required this.clock});
  final PeerClock clock;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Text(
        clock.synced
            ? t.clockSynced(clock.latencyMs, clock.samples)
            : t.clockFigures(
                clock.offsetMs,
                clock.uncertaintyMs,
                clock.latencyMs,
                clock.jitterMs,
                clock.samples,
              ),
        style: monoStyle(context),
      ),
    );
  }
}

// ---------------------------------------------------------------- directory

Future<void> openHubDirectory(
  BuildContext context,
  ServerState server,
  Peer hub,
) => showAccountScreen<void>(
  context,
  title: L10n.of(context).directoryOf(hub.name),
  width: 680,
  child: HubDirectoryScreen(server: server, hub: hub),
);

class HubDirectoryScreen extends StatefulWidget {
  const HubDirectoryScreen({
    super.key,
    required this.server,
    required this.hub,
  });
  final ServerState server;
  final Peer hub;

  @override
  State<HubDirectoryScreen> createState() => _HubDirectoryScreenState();
}

class _HubDirectoryScreenState extends State<HubDirectoryScreen> {
  late final ctl = HubDirectoryController(widget.server, widget.hub)..load();

  @override
  void dispose() {
    ctl.dispose();
    super.dispose();
  }

  Future<void> _join(HubDirectoryEntry e) async {
    final t = L10n.of(context);
    final ok = await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (_) => _JoinDialog(server: widget.server, entry: e, ctl: ctl),
    );
    if (ok == true && mounted) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        SnackBar(
          content: Text(
            e.listing == Listing.open
                ? t.joinedHubChannel(e.name)
                : t.askedToJoin(e.name),
          ),
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: ctl,
      builder: (context, _) {
        final list = ctl.channels;
        return ListView(
          key: const Key('hub-directory'),
          padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
          children: [
            Text(t.directoryNote, style: TextStyle(color: c.dim, fontSize: 13)),
            if (ctl.error != null) ErrorLine(ctl.error!),
            if (list == null && ctl.error == null)
              Text(t.loading, style: TextStyle(color: c.dim)),
            if (list != null && list.isEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Text(t.directoryEmpty, style: TextStyle(color: c.dim)),
              ),
            if (list != null && list.isNotEmpty) SectionHead(t.listedChannels),
            for (final e in list ?? const <HubDirectoryEntry>[])
              _entry(context, e),
            if (ctl.pending.isNotEmpty) ...[
              SectionHead(t.pendingRequests),
              for (final e in ctl.pending)
                _plainRow(
                  context,
                  e.name,
                  t.waitingForModerator,
                  key: Key('pending-${e.streamId}'),
                ),
            ],
            if (ctl.declined.isNotEmpty) ...[
              SectionHead(t.declinedRequests),
              for (final s in ctl.declined)
                _plainRow(
                  context,
                  s.offeredName ?? s.id,
                  t.declinedNote,
                  key: Key('declined-${s.id}'),
                ),
            ],
          ],
        );
      },
    );
  }

  Widget _plainRow(BuildContext context, String name, String note, {Key? key}) {
    final c = ChurrColors.of(context);
    return Container(
      key: key,
      margin: const EdgeInsets.only(bottom: 2),
      color: c.panel,
      padding: const EdgeInsets.all(10),
      child: Row(
        children: [
          Glyph(ChurrIcons.hash, size: 16, color: c.dim),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(name, style: const TextStyle(fontWeight: FontWeight.w700)),
                Text(note, style: TextStyle(color: c.dim, fontSize: 13)),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _entry(BuildContext context, HubDirectoryEntry e) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final action = switch (e.state) {
      'member' => Tag(t.stateMember, fill: c.cyan, degrees: 2),
      'requested' => Tag(t.stateRequested, fill: c.yellow, degrees: -2),
      _ => FilledButton(
        key: Key('join-${e.streamId}'),
        onPressed: () => _join(e),
        child: Text(
          e.listing == Listing.open || e.state == 'invited'
              ? t.joinChannel
              : t.askToJoin,
        ),
      ),
    };
    return Container(
      key: Key('directory-${e.streamId}'),
      margin: const EdgeInsets.only(bottom: 2),
      color: c.panel,
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Wrap(
            spacing: 8,
            runSpacing: 4,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Glyph(ChurrIcons.hash, size: 16, color: c.dim),
                  const SizedBox(width: 6),
                  Text(
                    e.name,
                    style: const TextStyle(
                      fontWeight: FontWeight.w800,
                      fontSize: 15,
                    ),
                  ),
                ],
              ),
              Tag(
                listingName(t, e.listing),
                fill: e.listing == Listing.open ? c.cyan : c.lavender,
                degrees: -2,
              ),
              if (e.state == 'invited')
                Tag(t.stateInvited, fill: c.yellow, degrees: 2),
            ],
          ),
          if (e.description.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(e.description),
            ),
          const SizedBox(height: 4),
          Text(
            t.directoryFacts(
              e.servers,
              e.moderators.map((m) => m.handle).join(', '),
            ),
            style: TextStyle(color: c.dim, fontSize: 12.5),
          ),
          const SizedBox(height: 6),
          Align(alignment: AlignmentDirectional.centerEnd, child: action),
        ],
      ),
    );
  }
}

/// Join or ask to join: the local name, public or private, and section.
class _JoinDialog extends StatefulWidget {
  const _JoinDialog({
    required this.server,
    required this.entry,
    required this.ctl,
  });
  final ServerState server;
  final HubDirectoryEntry entry;
  final HubDirectoryController ctl;

  @override
  State<_JoinDialog> createState() => _JoinDialogState();
}

class _JoinDialogState extends State<_JoinDialog> {
  late final name = TextEditingController(text: widget.entry.name);
  bool private = false;
  String? section;
  String? error;
  bool busy = false;

  @override
  void dispose() {
    name.dispose();
    super.dispose();
  }

  Future<void> _go() async {
    setState(() {
      busy = true;
      error = null;
    });
    final err = await widget.ctl.join(
      widget.entry,
      name: name.text.trim(),
      private: private,
      sectionId: section,
    );
    if (!mounted) return;
    if (err == null) {
      Navigator.pop(context, true);
    } else {
      setState(() {
        busy = false;
        error = _refusal(err);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final open =
        widget.entry.listing == Listing.open || widget.entry.state == 'invited';
    return AlertDialog(
      title: Text(
        open ? t.joinTitle(widget.entry.name) : t.askTitle(widget.entry.name),
      ),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(open ? t.joinNote : t.askNote),
            const SizedBox(height: 12),
            TextField(
              key: const Key('join-name'),
              controller: name,
              maxLength: 64,
              decoration: InputDecoration(labelText: t.localName),
              onChanged: (_) => setState(() {}),
            ),
            SegmentedButton<bool>(
              key: const Key('join-private'),
              showSelectedIcon: false,
              segments: [
                ButtonSegment(value: false, label: Text(t.publicHere)),
                ButtonSegment(value: true, label: Text(t.privateHere)),
              ],
              selected: {private},
              onSelectionChanged: (v) => setState(() => private = v.first),
            ),
            const SizedBox(height: 10),
            DropdownButton<String?>(
              key: const Key('join-section'),
              isExpanded: true,
              elevation: 0,
              icon: const Glyph(ChurrIcons.chevronDown, size: 16),
              value: section,
              items: [
                DropdownMenuItem(value: null, child: Text(t.noSection)),
                for (final s in widget.server.sections)
                  DropdownMenuItem(value: s.id, child: Text(s.name)),
              ],
              onChanged: (v) => setState(() => section = v),
            ),
            if (error != null) ErrorLine(error!),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: Text(t.cancel),
        ),
        FilledButton(
          key: const Key('join-go'),
          onPressed: busy || name.text.trim().isEmpty ? null : _go,
          child: Text(open ? t.joinChannel : t.askToJoin),
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------- share

/// Shares a channel to a hub (owner only): the listing mode, servers to
/// invite, and the plain statement of what the hub will own and see.
Future<void> openShareToHub(
  BuildContext context,
  ServerState server,
  Conversation conv,
) => showAccountScreen<void>(
  context,
  title: L10n.of(context).shareToHubTitle(conv.name ?? ''),
  width: 600,
  child: ShareToHubScreen(server: server, conversation: conv),
);

class ShareToHubScreen extends StatefulWidget {
  const ShareToHubScreen({
    super.key,
    required this.server,
    required this.conversation,
  });
  final ServerState server;
  final Conversation conversation;

  @override
  State<ShareToHubScreen> createState() => _ShareToHubScreenState();
}

class _ShareToHubScreenState extends State<ShareToHubScreen> {
  List<Peer>? hubs;
  Peer? hub;
  Listing listing = Listing.unlisted;
  final invite = <String>{};
  String? error;
  bool busy = false;
  bool done = false;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final peers = await widget.server.api.listPeers();
      if (!mounted) return;
      setState(() {
        hubs = [
          for (final p in peers)
            if (p.isHub && p.active) p,
        ];
        hub = hubs!.firstOrNull;
      });
    } on Exception catch (e) {
      if (mounted) setState(() => error = _refusal(e));
    }
  }

  Future<void> _share() async {
    final h = hub;
    if (h == null) return;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await widget.server.api.shareChannel(
        conversationId: widget.conversation.id,
        peerId: h.id,
        listing: listing,
        serverIds: invite.toList(),
      );
      if (mounted) setState(() => done = true);
    } on Exception catch (e) {
      if (mounted) setState(() => error = _refusal(e));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final h = hub;
    final ownName = widget.server.info?.name;
    final others = [
      for (final s in h?.servers ?? const <KnownServer>[])
        if (s.name != ownName) s,
    ];
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
      children: [
        if (error != null) ErrorLine(error!),
        if (hubs == null && error == null)
          Text(t.loading, style: TextStyle(color: c.dim)),
        if (hubs != null && hubs!.isEmpty) ...[
          Text(t.noHubsToShare),
          const SizedBox(height: 8),
          Align(
            alignment: AlignmentDirectional.centerStart,
            child: OutlinedButton.icon(
              icon: const Glyph(ChurrIcons.external, size: 16),
              label: Text(t.linkInConsole),
              onPressed: () => openLinkedServersConsole(widget.server),
            ),
          ),
        ],
        if (hubs != null && hubs!.isNotEmpty) ...[
          if (hubs!.length > 1) ...[
            SectionHead(t.whichHub),
            DropdownButton<Peer>(
              isExpanded: true,
              elevation: 0,
              icon: const Glyph(ChurrIcons.chevronDown, size: 16),
              value: h,
              items: [
                for (final p in hubs!)
                  DropdownMenuItem(value: p, child: Text(p.name)),
              ],
              onChanged: (v) => setState(() {
                hub = v;
                invite.clear();
              }),
            ),
          ],
          // What the person is agreeing to, in plain words.
          Container(
            key: const Key('hub-statement'),
            margin: const EdgeInsets.only(top: 8),
            decoration: BoxDecoration(
              color: c.raised,
              border: Border(left: BorderSide(color: c.yellow, width: 4)),
            ),
            padding: const EdgeInsets.all(12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  t.hubWillOwn(h?.name ?? ''),
                  style: const TextStyle(fontWeight: FontWeight.w800),
                ),
                const SizedBox(height: 6),
                Text(t.hubOperatorSees),
                const SizedBox(height: 4),
                Text(t.hubOperatorCannot),
                const SizedBox(height: 4),
                Text(t.hubYouModerate),
              ],
            ),
          ),
          SectionHead(t.listingMode),
          for (final l in Listing.values)
            RadioListTile<Listing>(
              key: Key('listing-${l.name}'),
              contentPadding: EdgeInsets.zero,
              value: l,
              // ignore: deprecated_member_use
              groupValue: listing,
              // ignore: deprecated_member_use
              onChanged: (v) => setState(() => listing = v!),
              title: Text(listingName(t, l)),
              subtitle: Text(listingNote(t, l)),
            ),
          SectionHead(t.serversToInvite),
          if (others.isEmpty)
            Text(t.noOtherServers, style: TextStyle(color: c.dim)),
          for (final s in others)
            CheckboxListTile(
              key: Key('invite-${s.id}'),
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              value: invite.contains(s.id),
              onChanged: (v) => setState(
                () => v == true ? invite.add(s.id) : invite.remove(s.id),
              ),
              title: Text(s.name),
              subtitle: Text(
                s.fingerprint,
                style: monoStyle(context, size: 10),
              ),
            ),
          const SizedBox(height: 12),
          if (done)
            NoteLine(t.sharedToHub(h?.name ?? ''))
          else
            Align(
              alignment: AlignmentDirectional.centerEnd,
              child: FilledButton(
                key: const Key('share-to-hub'),
                onPressed: busy || h == null ? null : _share,
                child: Text(t.shareToHub),
              ),
            ),
        ],
      ],
    );
  }
}

// ---------------------------------------------------------------- details

/// A hub channel's details in its settings: hub, listing mode, member
/// servers and moderators, kept current from `stream.channel_changed`;
/// and where the reader is a moderator, the moderators' tools.
class HubChannelSection extends StatefulWidget {
  const HubChannelSection({
    super.key,
    required this.server,
    required this.conversation,
  });
  final ServerState server;
  final Conversation conversation;

  @override
  State<HubChannelSection> createState() => _HubChannelSectionState();
}

class _HubChannelSectionState extends State<HubChannelSection> {
  late final ctl = HubChannelController(widget.server, widget.conversation.id)
    ..start();
  String? error;

  @override
  void dispose() {
    ctl.dispose();
    super.dispose();
  }

  Future<void> _act(
    ModAction a, {
    String? serverName,
    String? accountId,
    String? name,
    String? description,
    Listing? listing,
  }) async {
    final err = await ctl.act(
      a,
      serverName: serverName,
      accountId: accountId,
      name: name,
      description: description,
      listing: listing,
    );
    if (mounted) setState(() => error = err == null ? null : _refusal(err));
  }

  Future<String?> _ask(
    String title,
    String label, {
    String initial = '',
    int maxLength = 64,
    int lines = 1,
  }) async {
    final t = L10n.of(context);
    final ctrl = TextEditingController(text: initial);
    final v = await showDialog<String>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: SizedBox(
          width: 400,
          child: TextField(
            key: const Key('hub-ask-field'),
            controller: ctrl,
            autofocus: true,
            maxLength: maxLength,
            minLines: lines,
            maxLines: lines,
            decoration: InputDecoration(labelText: label),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(t.cancel),
          ),
          FilledButton(
            key: const Key('hub-ask-ok'),
            onPressed: () => Navigator.pop(ctx, ctrl.text.trim()),
            child: Text(t.save),
          ),
        ],
      ),
    );
    ctrl.dispose();
    return v;
  }

  Future<bool> _confirm(String title, String body, String yes) async {
    final t = L10n.of(context);
    return await showDialog<bool>(
          animationStyle: AnimationStyle.noAnimation,
          context: context,
          builder: (ctx) => AlertDialog(
            title: Text(title),
            content: Text(body),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx, false),
                child: Text(t.cancel),
              ),
              FilledButton(
                key: const Key('hub-confirm'),
                style: dangerButton(ChurrColors.of(ctx)),
                onPressed: () => Navigator.pop(ctx, true),
                child: Text(yes),
              ),
            ],
          ),
        ) ==
        true;
  }

  Future<void> _addModerator(HubChannel ch) async {
    final t = L10n.of(context);
    final s = widget.server;
    final mods = {for (final m in ch.moderators) m.accountId};
    final people = [
      for (final a in s.accounts.values)
        if (!a.isBot && !mods.contains(a.id)) a,
    ]..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    final picked = await showDialog<String>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => SimpleDialog(
        title: Text(t.addModerator),
        children: [
          for (final a in people)
            SimpleDialogOption(
              key: Key('pick-mod-${a.id}'),
              onPressed: () => Navigator.pop(ctx, a.id),
              child: Text('${a.name} (@${a.username})'),
            ),
        ],
      ),
    );
    if (picked != null) await _act(ModAction.addModerator, accountId: picked);
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: ctl,
      builder: (context, _) {
        final ch = ctl.channel;
        // Not a channel a hub owns: nothing to add here.
        if (ch == null) return const SizedBox.shrink();
        final mod = ctl.moderator;
        // The tools are the moderators' alone; the hub refuses anyone
        // else, the server's owner included.
        final tools = mod;
        return Column(
          key: const Key('hub-channel'),
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            SectionHead(t.onAHub),
            Row(
              children: [
                Glyph(ChurrIcons.globe, size: 16, color: c.dim),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(
                    t.ownedByHub(ch.hub),
                    style: const TextStyle(fontWeight: FontWeight.w700),
                  ),
                ),
                Tag(
                  listingName(t, ch.listing),
                  fill: ch.listing == Listing.open ? c.cyan : c.lavender,
                  degrees: -2,
                ),
                if (mod) ...[
                  const SizedBox(width: 6),
                  Tag(t.moderatorBadge, fill: c.yellow, degrees: 2),
                ],
              ],
            ),
            if (ch.closed)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  t.hubChannelClosed,
                  style: monoStyle(context, color: c.yellowInk),
                ),
              ),
            if (ch.name.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(t.nameOnHub(ch.name)),
              ),
            if (ch.description.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 2),
                child: Text(ch.description, style: TextStyle(color: c.dim)),
              ),
            const SizedBox(height: 6),
            Text(
              listingNote(t, ch.listing),
              style: TextStyle(color: c.dim, fontSize: 13),
            ),
            SectionHead(t.memberServers(ch.servers.length)),
            for (final name in ch.servers)
              _line(
                context,
                name,
                key: Key('hub-server-$name'),
                trailing:
                    tools && name != widget.server.info?.name && !ch.closed
                    ? TextButton(
                        key: Key('remove-server-$name'),
                        onPressed: ctl.busy
                            ? null
                            : () async {
                                if (await _confirm(
                                  t.removeServerTitle(name),
                                  t.removeServerBody(name),
                                  t.remove,
                                )) {
                                  await _act(
                                    ModAction.removeServer,
                                    serverName: name,
                                  );
                                }
                              },
                        child: Text(t.remove, style: TextStyle(color: c.pink)),
                      )
                    : null,
              ),
            SectionHead(t.moderators(ch.moderators.length)),
            for (final m in ch.moderators)
              _line(
                context,
                m.handle,
                key: Key('hub-mod-${m.accountId}'),
                badge: Tag(t.moderatorBadge, fill: c.yellow, degrees: 2),
                trailing: tools && !ch.closed && ch.moderators.length > 1
                    ? TextButton(
                        key: Key('remove-mod-${m.accountId}'),
                        onPressed: ctl.busy
                            ? null
                            : () => _act(
                                ModAction.removeModerator,
                                accountId: m.accountId,
                              ),
                        child: Text(t.remove, style: TextStyle(color: c.pink)),
                      )
                    : null,
              ),
            if (tools && !ch.closed) ...[
              if (ch.requests.isNotEmpty) ...[
                SectionHead(t.joinRequests),
                for (final name in ch.requests)
                  _line(
                    context,
                    name,
                    key: Key('hub-request-$name'),
                    trailing: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        TextButton(
                          key: Key('approve-$name'),
                          onPressed: ctl.busy
                              ? null
                              : () => _act(ModAction.approve, serverName: name),
                          child: Text(t.approve),
                        ),
                        const SizedBox(width: 6),
                        TextButton(
                          key: Key('decline-$name'),
                          onPressed: ctl.busy
                              ? null
                              : () => _act(ModAction.decline, serverName: name),
                          child: Text(
                            t.decline,
                            style: TextStyle(color: c.pink),
                          ),
                        ),
                      ],
                    ),
                  ),
              ],
              if (ch.invited.isNotEmpty) ...[
                SectionHead(t.invitedServers),
                for (final name in ch.invited)
                  _line(
                    context,
                    name,
                    key: Key('hub-invited-$name'),
                    trailing: TextButton(
                      onPressed: ctl.busy
                          ? null
                          : () =>
                                _act(ModAction.removeServer, serverName: name),
                      child: Text(t.withdraw),
                    ),
                  ),
              ],
              SectionHead(t.moderatorTools),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  OutlinedButton(
                    key: const Key('hub-invite'),
                    onPressed: ctl.busy
                        ? null
                        : () async {
                            final name = await _ask(
                              t.inviteServer,
                              t.serverNameLabel,
                            );
                            if (name != null && name.isNotEmpty) {
                              await _act(ModAction.invite, serverName: name);
                            }
                          },
                    child: Text(t.inviteServer),
                  ),
                  OutlinedButton(
                    key: const Key('hub-rename'),
                    onPressed: ctl.busy
                        ? null
                        : () async {
                            final name = await _ask(
                              t.renameOnHub,
                              t.nameLabel,
                              initial: ch.name,
                            );
                            if (name != null &&
                                name.isNotEmpty &&
                                name != ch.name) {
                              await _act(ModAction.describe, name: name);
                            }
                          },
                    child: Text(t.renameOnHub),
                  ),
                  OutlinedButton(
                    key: const Key('hub-describe'),
                    onPressed: ctl.busy
                        ? null
                        : () async {
                            final d = await _ask(
                              t.describeOnHub,
                              t.descriptionLabel,
                              initial: ch.description,
                              maxLength: 1000,
                              lines: 4,
                            );
                            if (d != null && d != ch.description) {
                              await _act(ModAction.describe, description: d);
                            }
                          },
                    child: Text(t.describeOnHub),
                  ),
                  OutlinedButton(
                    key: const Key('hub-add-mod'),
                    onPressed: ctl.busy ? null : () => _addModerator(ch),
                    child: Text(t.addModerator),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              Text(t.listingMode, style: ChurrType.label(c.dim)),
              const SizedBox(height: 4),
              SegmentedButton<Listing>(
                key: const Key('hub-listing'),
                showSelectedIcon: false,
                segments: [
                  for (final l in Listing.values)
                    ButtonSegment(value: l, label: Text(listingName(t, l))),
                ],
                selected: {ch.listing},
                onSelectionChanged: ctl.busy
                    ? null
                    : (v) => _act(ModAction.setListing, listing: v.first),
              ),
              const SizedBox(height: 10),
              Align(
                alignment: AlignmentDirectional.centerStart,
                child: FilledButton(
                  key: const Key('hub-close'),
                  style: dangerButton(c),
                  onPressed: ctl.busy
                      ? null
                      : () async {
                          if (await _confirm(
                            t.closeHubChannelTitle,
                            t.closeHubChannelBody,
                            t.closeChannel,
                          )) {
                            await _act(ModAction.close);
                          }
                        },
                  child: Text(t.closeChannel),
                ),
              ),
            ],
            if (error != null) ErrorLine(error!),
            const Divider(height: 24),
          ],
        );
      },
    );
  }

  Widget _line(
    BuildContext context,
    String text, {
    Key? key,
    Widget? badge,
    Widget? trailing,
  }) {
    final c = ChurrColors.of(context);
    return Container(
      key: key,
      margin: const EdgeInsets.only(bottom: 2),
      color: c.panel,
      padding: const EdgeInsets.fromLTRB(10, 4, 4, 4),
      constraints: const BoxConstraints(minHeight: 40),
      child: Row(
        children: [
          Expanded(
            child: Text(
              text,
              style: const TextStyle(fontWeight: FontWeight.w600),
            ),
          ),
          if (badge != null) ...[badge, const SizedBox(width: 6)],
          ?trailing,
        ],
      ),
    );
  }
}
