import 'dart:async';

import 'package:flutter/material.dart';

import '../../core/models/conversation.dart';
import '../../l10n/app_localizations.dart';
import '../theme/churr_icons.dart';
import '../theme/shapes.dart';
import '../theme/tokens.dart';

/// A channel link's card: shown when the pointer rests on the link for a
/// moment, or on a long press on touch (docs/CLIENTS.md 9.3). It says
/// what the channel is (its kind, topic, members, parent) and opens it.
class ChannelLinkCard extends StatefulWidget {
  const ChannelLinkCard({
    super.key,
    required this.conversation,
    required this.child,
    this.parentName,
    this.onOpen,
  });

  final Conversation conversation;
  final String? parentName;
  final VoidCallback? onOpen;

  /// The link as drawn in the text.
  final Widget child;

  @override
  State<ChannelLinkCard> createState() => _ChannelLinkCardState();
}

class _ChannelLinkCardState extends State<ChannelLinkCard> {
  final _portal = OverlayPortalController();
  final _link = LayerLink();
  Timer? _hover;
  bool _overCard = false;

  void _show() {
    if (mounted && !_portal.isShowing) setState(_portal.show);
  }

  void _hide() {
    _hover?.cancel();
    if (mounted && _portal.isShowing) setState(_portal.hide);
  }

  @override
  void dispose() {
    _hover?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => CompositedTransformTarget(
    link: _link,
    child: OverlayPortal(
      controller: _portal,
      overlayChildBuilder: (context) => Stack(
        children: [
          // A tap anywhere else closes it (touch).
          Positioned.fill(
            child: GestureDetector(
              behavior: HitTestBehavior.translucent,
              onTap: _hide,
            ),
          ),
          CompositedTransformFollower(
            link: _link,
            targetAnchor: Alignment.bottomLeft,
            offset: const Offset(0, 6),
            child: Align(
              alignment: Alignment.topLeft,
              child: MouseRegion(
                onEnter: (_) => _overCard = true,
                onExit: (_) {
                  _overCard = false;
                  _hide();
                },
                child: _Card(
                  conversation: widget.conversation,
                  parentName: widget.parentName,
                  onOpen: widget.onOpen == null
                      ? null
                      : () {
                          _hide();
                          widget.onOpen!();
                        },
                ),
              ),
            ),
          ),
        ],
      ),
      child: MouseRegion(
        onEnter: (_) {
          _hover?.cancel();
          _hover = Timer(const Duration(milliseconds: 500), _show);
        },
        onExit: (_) {
          _hover?.cancel();
          // Leaving the link for the card keeps it open.
          Future<void>.delayed(const Duration(milliseconds: 150), () {
            if (!_overCard) _hide();
          });
        },
        child: GestureDetector(onLongPress: _show, child: widget.child),
      ),
    ),
  );
}

class _Card extends StatelessWidget {
  const _Card({required this.conversation, this.parentName, this.onOpen});
  final Conversation conversation;
  final String? parentName;
  final VoidCallback? onOpen;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final conv = conversation;
    final kind = conv.isLocked
        ? t.channelCardLocked
        : conv.private
        ? t.channelCardPrivate
        : t.channelCardPublic;
    final icon = conv.isLocked
        ? ChurrIcons.locked
        : conv.private
        ? ChurrIcons.private
        : ChurrIcons.hash;
    return Semantics(
      container: true,
      label: t.channelCardLabel(conv.name ?? ''),
      child: Container(
        key: const Key('channel-card'),
        width: 280,
        padding: const EdgeInsets.all(12),
        decoration: floatingBox(c),
        child: Material(
          type: MaterialType.transparency,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Glyph(icon, size: 18, color: c.lavender),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      conv.name ?? '',
                      style: TextStyle(
                        color: c.ink,
                        fontWeight: FontWeight.w800,
                        fontSize: 16,
                      ),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              Text(
                [
                  kind,
                  t.channelCardMembers(conv.memberCount),
                  if (conv.isArchived) t.channelCardArchived,
                ].join(' · '),
                style: TextStyle(color: c.dim, fontSize: 12.5),
              ),
              if (parentName != null)
                Padding(
                  padding: const EdgeInsets.only(top: 2),
                  child: Text(
                    t.channelCardUnder(parentName!),
                    style: TextStyle(color: c.dim, fontSize: 12.5),
                  ),
                ),
              if (conv.topic.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                    conv.topic,
                    maxLines: 3,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: c.ink, fontSize: 14),
                  ),
                ),
              if (onOpen != null)
                Padding(
                  padding: const EdgeInsets.only(top: 10),
                  child: OutlinedButton(
                    key: const Key('channel-card-open'),
                    onPressed: onOpen,
                    child: Text(
                      conv.membership == null && conv.isKnock
                          ? t.askToJoin
                          : conv.membership == null
                          ? t.channelCardView
                          : t.channelCardOpen,
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
