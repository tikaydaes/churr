import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/themes.dart';
import '../../../core/themes/theme.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/app_state.dart';
import '../../../state/server_state.dart';
import '../account/common.dart';
import 'theme_widgets.dart';

/// Opens a link from a message. A theme link (`{origin}/themes/{id}`) of
/// a signed-in server opens its theme card in the app; anything else
/// opens outside the app, as every link does.
Future<void> openMessageLink(
  BuildContext context,
  AppState app,
  String url,
) async {
  final link = parseThemeLink(url);
  if (link != null) {
    for (final s in app.servers) {
      if (Uri.parse(s.entry.address).origin == link.$1) {
        await showThemeFromLink(context, s, link.$2);
        return;
      }
    }
  }
  await launchUrl(Uri.parse(url), mode: LaunchMode.externalApplication);
}

/// Fetches one of [server]'s themes by id and shows it as a theme card.
Future<void> showThemeFromLink(
  BuildContext context,
  ServerState server,
  String id,
) => showAccountScreen<void>(
  context,
  title: L10n.of(context).themeCardLabel,
  width: 480,
  child: _LinkedTheme(server: server, id: id),
);

class _LinkedTheme extends StatelessWidget {
  const _LinkedTheme({required this.server, required this.id});
  final ServerState server;
  final String id;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return FutureBuilder<ChurrTheme>(
      future: server.api.getTheme(id),
      builder: (context, snap) {
        if (snap.hasError) {
          final e = snap.error;
          return Padding(
            padding: const EdgeInsets.all(20),
            child: ErrorLine(
              e is ApiException && e.code == 'not_found' ? t.themeGone : '$e',
            ),
          );
        }
        final th = snap.data;
        if (th == null) {
          return Padding(
            padding: const EdgeInsets.all(20),
            child: Text(t.loading),
          );
        }
        return Padding(
          padding: const EdgeInsets.all(20),
          child: Align(
            alignment: Alignment.topLeft,
            child: ThemeCardView(
              server: server,
              theme: th,
              author: server.accounts[th.ownerId]?.name,
            ),
          ),
        );
      },
    );
  }
}
