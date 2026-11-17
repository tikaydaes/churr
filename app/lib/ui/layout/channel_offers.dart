import 'package:flutter/material.dart';

import '../../core/models/chat_request.dart';
import '../../l10n/app_localizations.dart';
import '../../state/app_state.dart';
import '../../state/chat_requests.dart';
import '../../state/server_state.dart';
import '../screens/channels/common.dart';
import '../scopes.dart';
import '../theme/tokens.dart';
import '../theme/churr_icons.dart';
import 'conversation_row.dart' show rowGutter;

/// The channels offered to the person on one server (ruling R103,
/// docs/CLIENTS.md 9.28, "Offers"), at the top of its part of the left
/// panel: each with Accept, which joins and opens it, and Decline, which
/// takes it out. A public one opens to read before answering. Nothing is
/// drawn while there are none.
class ChannelOffersBlock extends StatelessWidget {
  const ChannelOffersBlock({super.key, required this.server, this.inset = 0});
  final ServerState server;
  final double inset;

  @override
  Widget build(BuildContext context) =>
      ListenableBuilder(listenable: server, builder: _build);

  Widget _build(BuildContext context, Widget? _) {
    final offers = server.channelOffers;
    if (offers.isEmpty) return const SizedBox.shrink();
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    return Column(
      key: Key('channel-offers-${server.localId}'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Semantics(
          header: true,
          child: Padding(
            padding: EdgeInsets.fromLTRB(inset + rowGutter, 12, 12, 4),
            child: Text(
              t.offersHeading.toUpperCase(),
              style: ChurrType.label(c.faint),
            ),
          ),
        ),
        for (final o in offers)
          _OfferTile(
            key: Key('channel-offer-${o.conversationId}'),
            server: server,
            offer: o,
            inset: inset,
          ),
      ],
    );
  }
}

class _OfferTile extends StatefulWidget {
  const _OfferTile({
    super.key,
    required this.server,
    required this.offer,
    required this.inset,
  });
  final ServerState server;
  final ChannelOffer offer;
  final double inset;

  @override
  State<_OfferTile> createState() => _OfferTileState();
}

class _OfferTileState extends State<_OfferTile> {
  bool busy = false;

  Future<void> _run(Future<void> Function(ChannelOfferActions a) f) async {
    setState(() => busy = true);
    try {
      await f(ChannelOfferActions(widget.server));
    } on Exception catch (e) {
      if (mounted) say(context, refusal(e));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  void _open(AppState app, String id) =>
      app.openConversation(OpenTarget(widget.server.localId, id));

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.read(context);
    final o = widget.offer;
    final ch = o.channel;
    final why = o.viaDefault ? t.offerDefault : t.offerThroughGroup;
    return InkWell(
      // A public one is read before answering; the others show nothing.
      onTap: o.previewable && !busy
          ? () => _run((a) async {
              final conv = await a.preview(o);
              if (conv != null) _open(app, conv.id);
            })
          : null,
      child: Padding(
        padding: EdgeInsets.fromLTRB(widget.inset + rowGutter, 4, 12, 6),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Glyph(
                  channelIcon(true, ch.access != 'public', ch.isLocked),
                  size: 14,
                  color: c.dim,
                ),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(
                    ch.name ?? '',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: c.ink, fontWeight: FontWeight.w600),
                  ),
                ),
              ],
            ),
            if (ch.topic.isNotEmpty)
              Text(
                ch.topic,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(color: c.dim, fontSize: 12),
              ),
            Text(why, style: TextStyle(color: c.faint, fontSize: 11.5)),
            const SizedBox(height: 4),
            Wrap(
              spacing: 6,
              runSpacing: 4,
              children: [
                FilledButton(
                  key: const Key('channel-offer-accept'),
                  style: FilledButton.styleFrom(
                    visualDensity: VisualDensity.compact,
                  ),
                  onPressed: busy
                      ? null
                      : () => _run((a) async {
                          final conv = await a.accept(o);
                          _open(app, conv.id);
                        }),
                  child: Text(t.requestAccept),
                ),
                OutlinedButton(
                  key: const Key('channel-offer-decline'),
                  style: OutlinedButton.styleFrom(
                    visualDensity: VisualDensity.compact,
                  ),
                  onPressed: busy ? null : () => _run((a) => a.decline(o)),
                  child: Text(t.requestDecline),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
