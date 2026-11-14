import 'package:flutter/material.dart';

import '../../core/models/account.dart';
import '../../state/server_state.dart';
import '../message/server_image.dart';
import '../theme/shapes.dart';
import '../theme/tokens.dart';

/// An avatar with the notch: the account's picture, or its initials (in
/// Martian Mono, as the guide draws them) on one of the four fills chosen
/// from its id, so a person keeps their colour.
class Avatar extends StatelessWidget {
  const Avatar({super.key, required this.account, this.server, this.size = 36});
  final Account? account;

  /// Needed to fetch a picture; without it initials are drawn.
  final ServerState? server;
  final double size;

  @override
  Widget build(BuildContext context) {
    final a = account;
    final url = a?.avatarUrl;
    final cut = size >= 28 ? 6.0 : 4.0;
    if (a != null && url != null && server != null) {
      return ClipPath(
        clipper: CutCornerClipper(cut),
        child: ServerImage(
          server: server!,
          path: url,
          width: size,
          height: size,
        ),
      );
    }
    return InitialsTile(name: a?.name ?? '?', id: a?.id, size: size);
  }
}

/// Initials on a notched fill: avatars without a picture, server tiles.
class InitialsTile extends StatelessWidget {
  const InitialsTile({
    super.key,
    required this.name,
    this.id,
    this.size = 36,
    this.dimmed = false,
  });
  final String name;

  /// Picks the fill, so the same thing keeps its colour.
  final String? id;
  final double size;

  /// Drawn on raised with faint letters (a muted server).
  final bool dimmed;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final fill = dimmed ? c.raised : c.fillFor(id);
    return Container(
      width: size,
      height: size,
      decoration: ShapeDecoration(
        color: fill,
        shape: CutCornerBorder(cut: size >= 28 ? 6 : 4),
      ),
      alignment: Alignment.center,
      child: Text(
        initials(name),
        maxLines: 1,
        style: TextStyle(
          fontFamily: fontMono,
          color: dimmed ? c.faint : c.on(fill),
          fontWeight: FontWeight.w800,
          fontSize: size * 0.34,
          height: 1,
        ),
      ),
    );
  }
}

/// One or two initials of a name.
String initials(String name) {
  final words = name
      .trim()
      .split(RegExp(r'\s+'))
      .where((w) => w.isNotEmpty)
      .toList();
  if (words.isEmpty) return '?';
  final first = String.fromCharCode(words.first.runes.first);
  if (words.length == 1) return first.toUpperCase();
  return (first + String.fromCharCode(words.last.runes.first)).toUpperCase();
}
