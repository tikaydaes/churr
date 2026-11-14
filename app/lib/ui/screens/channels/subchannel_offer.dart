import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import '../../../core/models/conversation.dart';
import '../../../state/channel_actions.dart';
import '../../../state/server_state.dart';
import '../../theme/tokens.dart';
import 'common.dart';

/// After joining a channel that has public sub-channels: a short list of
/// them with member counts and topics, none ticked, and "Join selected".
/// Skipping joins none; private ones are never listed (docs/CLIENTS.md
/// 9.25). Does nothing when there are none. [always] shows the list
/// even then, for the parent's "Sub-channels" menu entry.
Future<void> offerSubChannels(
  BuildContext context,
  ServerState server,
  Conversation parent, {
  bool always = false,
}) async {
  if (parent.parentId != null) return;
  List<Conversation> subs;
  try {
    subs = await ChannelActions(server).publicSubChannels(parent.id);
  } on Exception {
    return;
  }
  if (!context.mounted || (subs.isEmpty && !always)) return;
  await showDialog<void>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (_) => _Offer(server: server, parent: parent, subs: subs),
  );
}

class _Offer extends StatefulWidget {
  const _Offer({
    required this.server,
    required this.parent,
    required this.subs,
  });
  final ServerState server;
  final Conversation parent;
  final List<Conversation> subs;

  @override
  State<_Offer> createState() => _OfferState();
}

class _OfferState extends State<_Offer> {
  final picked = <String>{};
  bool busy = false;
  String? error;

  Future<void> _join() async {
    setState(() => busy = true);
    final actions = ChannelActions(widget.server);
    final failed = <String>[];
    for (final id in picked) {
      try {
        await actions.join(id);
      } on Exception catch (e) {
        failed.add(refusal(e));
      }
    }
    if (!mounted) return;
    if (failed.isEmpty) {
      Navigator.pop(context);
    } else {
      setState(() {
        busy = false;
        error = failed.join('\n');
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return AlertDialog(
      title: Text(
        L10n.of(context)
            .subchannelsOf(widget.parent.name ?? L10n.of(context).thisChannel),
      ),
      content: SizedBox(
        width: (MediaQuery.sizeOf(context).width - 160)
            .clamp(160, 420)
            .toDouble(),
        child: widget.subs.isEmpty
            ? Text(
                L10n.of(context).noPublicSubchannels,
                style: TextStyle(color: c.dim),
              )
            : ListView(
                shrinkWrap: true,
                children: [
                  Text(
                    L10n.of(context).joiningDoesNotJoinSubs,
                    style: monoStyle(context),
                  ),
                  for (final s in widget.subs)
                    CheckboxListTile(
                      key: Key('offer-${s.id}'),
                      controlAffinity: ListTileControlAffinity.leading,
                      value: picked.contains(s.id),
                      onChanged: busy
                          ? null
                          : (v) => setState(
                              () => v == true
                                  ? picked.add(s.id)
                                  : picked.remove(s.id),
                            ),
                      title: Text(s.name ?? ''),
                      subtitle: Text(
                        [
                          L10n.of(context).memberCount(s.memberCount),
                          if (s.topic.isNotEmpty) s.topic,
                        ].join(' · '),
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  if (error != null)
                    Text(error!, style: TextStyle(color: c.pink)),
                ],
              ),
      ),
      actions: [
        TextButton(
          onPressed: busy ? null : () => Navigator.pop(context),
          child: Text(L10n.of(context).skip),
        ),
        if (widget.subs.isNotEmpty)
          FilledButton(
            key: const Key('offer-join'),
            onPressed: busy || picked.isEmpty ? null : _join,
            child: Text(L10n.of(context).joinSelected),
          ),
      ],
    );
  }
}
