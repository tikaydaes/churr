import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import '../../../platform/notify_wording.dart';
import '../../../core/api/errors.dart';
import '../../../state/app_state.dart';
import '../../../state/server_state.dart';
import '../../layout/breakpoints.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';

/// Shows [child] as a page over everything on compact widths, and as a
/// dialog of [width] by [height] where there is room (docs/CLIENTS.md 10).
Future<T?> showScreen<T>(
  BuildContext context, {
  required String title,
  required Widget child,
  double width = 600,
  double height = 680,
}) {
  final compact =
      WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
  if (compact) {
    return Navigator.of(context).push<T>(
      MaterialPageRoute(
        builder: (_) => Scaffold(
          appBar: AppBar(title: Text(title)),
          body: SafeArea(top: false, child: child),
        ),
      ),
    );
  }
  return showDialog<T>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (ctx) => Dialog(
      child: SizedBox(
        width: width,
        height: height,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 12, 8, 4),
              child: Row(
                children: [
                  Expanded(
                    child: Semantics(
                      header: true,
                      child: Text(
                        title,
                        style: const TextStyle(
                          fontSize: 17,
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                    ),
                  ),
                  IconButton(
                    tooltip: L10n.of(ctx).close,
                    icon: const Glyph(ChurrIcons.close),
                    onPressed: () => Navigator.pop(ctx),
                  ),
                ],
              ),
            ),
            Divider(
              height: 1.5,
              thickness: 1.5,
              color: ChurrColors.of(ctx).line,
            ),
            Expanded(child: child),
          ],
        ),
      ),
    ),
  );
}

/// Asks which server, when more than one is signed in (the "New…" and
/// "Browse" actions in the merged and hybrid layouts, 9.2). With one
/// server, or a server already chosen, returns it at once.
Future<ServerState?> pickServer(
  BuildContext context,
  AppState app, {
  ServerState? preferred,
}) async {
  if (preferred != null) return preferred;
  if (app.servers.length <= 1) return app.servers.firstOrNull;
  return showDialog<ServerState>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (ctx) => SimpleDialog(
      title: Text(L10n.of(ctx).whichServer),
      children: [
        for (final s in app.servers)
          SimpleDialogOption(
            key: Key('pick-server-${s.localId}'),
            onPressed: () => Navigator.pop(ctx, s),
            child: Text(s.name),
          ),
      ],
    ),
  );
}

/// A refusal as a plain sentence for a person.
String refusal(Object e) {
  if (e is ApiException) {
    // No context here: the words are the device language's.
    final t = notifyL10n();
    return switch (e.code) {
      'locked' => t.refusalLocked,
      'archived' => t.refusalArchived,
      'forbidden' => e.message.isEmpty ? t.refusalForbidden : e.message,
      'too_large' => t.refusalTooLarge,
      _ => e.message,
    };
  }
  return '$e';
}

void say(BuildContext context, String text) =>
    ScaffoldMessenger.maybeOf(context)
      ?..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(text)));

/// A small server mark: the server's name in a quiet edge box, for
/// results that mix servers.
class ServerMark extends StatelessWidget {
  const ServerMark({super.key, required this.server});
  final ServerState server;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final color = c.dim;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
      decoration: BoxDecoration(border: Border.all(color: color, width: 1.5)),
      child: Text(
        server.name,
        style: monoStyle(context, size: 9, color: color),
      ),
    );
  }
}

/// Icon for a channel: hash for public, lock for private, a closed
/// padlock for locked, a bubble for ad-hoc (9.26).
ChurrGlyph channelIcon(bool named, bool private, bool locked) => !named
    ? ChurrIcons.chat
    : locked
    ? ChurrIcons.locked
    : private
    ? ChurrIcons.private
    : ChurrIcons.hash;
