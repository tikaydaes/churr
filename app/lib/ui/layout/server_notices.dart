import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart';

import '../../core/models/json.dart';
import '../../l10n/app_localizations.dart';
import '../../state/app_state.dart';
import '../../state/server_state.dart';
import '../message/markdown_view.dart';
import '../screens/channels/common.dart' show say;
import '../theme/churr_icons.dart';
import '../theme/tokens.dart';

/// What a server tells everyone, or one person, outside any conversation
/// (ruling R74): the server broadcast, drawn as a banner above everything
/// until it runs out or is dismissed, and the answer to a request to join a
/// channel (`/knock`) or to a request for a channel, said once.
class ServerNoticesHost extends StatefulWidget {
  const ServerNoticesHost({super.key, required this.app, required this.child});
  final AppState app;
  final Widget child;

  @override
  State<ServerNoticesHost> createState() => _ServerNoticesHostState();
}

class _ServerNoticesHostState extends State<ServerNoticesHost> {
  final _subs = <String, StreamSubscription<Json>>{};
  final _servers = <ServerState>{};
  final _announced = <String>{};
  // Servers whose changed VRF key history has been warned of this run.
  final _vrfWarned = <String>{};
  Timer? _expiry;

  @override
  void initState() {
    super.initState();
    widget.app.addListener(_listen);
    _listen();
  }

  void _listen() {
    final now = {for (final s in widget.app.servers) s};
    for (final s in _servers.difference(now)) {
      s.removeListener(_changed);
      _subs.remove(s.localId)?.cancel();
    }
    for (final s in now.difference(_servers)) {
      s.addListener(_changed);
      _subs[s.localId] = s.sync.localEvents.listen((e) => _onEvent(s, e));
    }
    _servers
      ..clear()
      ..addAll(now);
    _changed();
  }

  void _changed() {
    if (!mounted) return;
    setState(() {});
    // A server whose VRF key history is not the pinned one with keys
    // added (core/fair/pin.dart) is warned of once a run.
    for (final srv in _servers) {
      if (srv.vrfPinChanged && _vrfWarned.add(srv.localId)) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) say(context, L10n.of(context).vrfKeyChanged(srv.name));
        });
      }
    }
    // Hidden once it runs out, with no event: the earliest end is waited for.
    _expiry?.cancel();
    DateTime? next;
    for (final s in _servers) {
      final until = parseTime(s.broadcastShowing?['until']);
      if (until != null && (next == null || until.isBefore(next))) next = until;
    }
    if (next != null) {
      _expiry = Timer(next.difference(DateTime.now()), _changed);
    }
  }

  void _onEvent(ServerState server, Json e) {
    if (!mounted) return;
    if (e['type'] == 'approval.decided') {
      _decided(server, object(e['approval']));
      return;
    }
    if (e['type'] != 'knock.answered') return;
    final k = object(e['knock']);
    if (k == null || k['account_id'] != server.me?.id) return;
    final t = L10n.of(context);
    final where = '#${e['conversation_name'] ?? ''}';
    say(
      context,
      k['outcome'] == 'accepted'
          ? t.knockAccepted(where)
          : t.knockDeclined(where),
    );
  }

  /// The answer to the person's own request for a channel (FEATURES.md
  /// section 31). What deciders are sent names who decided; what the one
  /// who asked is sent does not, and only that is said.
  void _decided(ServerState server, Json? a) {
    if (a == null ||
        a['kind'] != 'channel' ||
        a['requester_id'] != server.me?.id ||
        (a['decided_by_id'] as String? ?? '').isNotEmpty) {
      return;
    }
    final t = L10n.of(context);
    final where = '#${a['summary'] ?? ''}';
    final reason = a['reason'] as String? ?? '';
    say(
      context,
      a['state'] == 'approved'
          ? t.channelRequestApproved(where)
          : reason.isEmpty
          ? t.channelRequestDeclined(where)
          : t.channelRequestDeclinedReason(where, reason),
    );
  }

  @override
  void dispose() {
    widget.app.removeListener(_listen);
    for (final s in _servers) {
      s.removeListener(_changed);
    }
    for (final s in _subs.values) {
      s.cancel();
    }
    _expiry?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final showing = [
      for (final s in widget.app.servers)
        if (s.broadcastShowing != null) (s, s.broadcastShowing!),
    ];
    if (showing.isEmpty) return widget.child;
    final t = L10n.of(context);
    for (final (_, b) in showing) {
      // Said once to a screen reader, when it first shows.
      if (_announced.add(b['id'] as String? ?? '')) {
        final text = t.broadcastAnnounce(b['text'] as String? ?? '');
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) {
            unawaited(
              SemanticsService.sendAnnouncement(
                View.of(context),
                text,
                Directionality.of(context),
              ),
            );
          }
        });
      }
    }
    return Column(
      children: [
        SafeArea(
          bottom: false,
          child: Column(
            children: [
              for (final (s, b) in showing)
                BroadcastBanner(
                  server: s,
                  broadcast: b,
                  several: widget.app.servers.length > 1,
                ),
            ],
          ),
        ),
        Expanded(
          child: MediaQuery.removePadding(
            context: context,
            removeTop: true,
            child: widget.child,
          ),
        ),
      ],
    );
  }
}

/// One server broadcast: a marked bar with the text and who sent it, and a
/// button that hides it on this device.
class BroadcastBanner extends StatelessWidget {
  const BroadcastBanner({
    super.key,
    required this.server,
    required this.broadcast,
    this.several = false,
  });
  final ServerState server;
  final Json broadcast;

  /// More than one server is signed in: the banner names its server.
  final bool several;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final by = server.accounts[broadcast['by_id']]?.name;
    return Material(
      key: Key('broadcast-${broadcast['id']}'),
      color: c.raised,
      child: Container(
        decoration: BoxDecoration(
          // The yellow bar of a marker (docs/STYLE.md), with a word beside it.
          border: Border(left: BorderSide(color: c.yellow, width: 4)),
        ),
        padding: const EdgeInsets.fromLTRB(12, 6, 4, 6),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    [
                      t.broadcastLabel.toUpperCase(),
                      if (several) server.name,
                      if (by != null) t.broadcastFrom(by),
                    ].join(' · '),
                    style: monoStyle(context, size: 10, color: c.yellowInk),
                  ),
                  MarkdownView(
                    text: broadcast['text'] as String? ?? '',
                    inlineOnly: true,
                  ),
                ],
              ),
            ),
            IconButton(
              key: Key('broadcast-dismiss-${broadcast['id']}'),
              tooltip: t.broadcastDismiss,
              icon: const Glyph(ChurrIcons.close),
              onPressed: server.dismissBroadcast,
            ),
          ],
        ),
      ),
    );
  }
}
