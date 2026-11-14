import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';

import 'package:flutter/services.dart';

import '../../../state/app_state.dart';
import '../../../state/channel_actions.dart';
import '../../../state/channel_detail.dart';
import '../../../state/quick_switcher.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';
import '../../widgets/avatar.dart';
import '../channels/common.dart';

/// Opens the quick switcher (docs/CLIENTS.md 9.17).
Future<void> openQuickSwitcher(BuildContext context) {
  final app = AppScope.read(context);
  return showScreen<void>(
    context,
    title: L10n.of(context).goTo,
    width: 560,
    height: 560,
    child: QuickSwitcherView(app: app),
  );
}

/// A box that finds conversations and people by name as it is typed,
/// across every signed-in server, most recent first, each with its
/// server's mark and, for channels, the detail line. Enter opens the
/// highlighted one; arrows move. Hidden conversations are found too.
class QuickSwitcherView extends StatefulWidget {
  const QuickSwitcherView({super.key, required this.app});
  final AppState app;

  @override
  State<QuickSwitcherView> createState() => _QuickSwitcherViewState();
}

class _QuickSwitcherViewState extends State<QuickSwitcherView> {
  late final QuickSwitcher model = QuickSwitcher(widget.app);
  int highlighted = 0;

  @override
  void dispose() {
    model.dispose();
    super.dispose();
  }

  Future<void> _open(SwitchHit h) async {
    final app = widget.app;
    final nav = Navigator.of(context);
    if (h.conversation != null) {
      nav.maybePop();
      app.openConversation(OpenTarget(h.server.localId, h.conversation!.id));
      return;
    }
    try {
      final c = await ChannelActions(h.server).openChat([h.account!.id]);
      nav.maybePop();
      app.openConversation(OpenTarget(h.server.localId, c.id));
    } on Exception catch (e) {
      if (mounted) say(context, refusal(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: Listenable.merge([model, widget.app]),
      builder: (context, _) {
        final hits = model.results;
        if (highlighted >= hits.length) {
          highlighted = hits.isEmpty ? 0 : hits.length - 1;
        }
        final many = widget.app.servers.length > 1;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.all(12),
              child: CallbackShortcuts(
                bindings: {
                  const SingleActivator(LogicalKeyboardKey.arrowDown): () =>
                      setState(
                        () => highlighted = (highlighted + 1).clamp(
                          0,
                          hits.isEmpty ? 0 : hits.length - 1,
                        ),
                      ),
                  const SingleActivator(LogicalKeyboardKey.arrowUp): () =>
                      setState(
                        () => highlighted = (highlighted - 1).clamp(
                          0,
                          hits.isEmpty ? 0 : hits.length - 1,
                        ),
                      ),
                },
                child: TextField(
                  key: const Key('switcher-box'),
                  autofocus: true,
                  decoration: InputDecoration(
                    hintText: L10n.of(context).switcherHint,
                    prefixIcon: const Glyph(ChurrIcons.search),
                  ),
                  onChanged: (v) {
                    highlighted = 0;
                    model.setQuery(v);
                  },
                  onSubmitted: (_) {
                    if (hits.isNotEmpty) _open(hits[highlighted]);
                  },
                ),
              ),
            ),
            Expanded(
              child: hits.isEmpty
                  ? Center(
                      child: Text(
                        L10n.of(context).nothingMatches,
                        style: TextStyle(color: c.dim),
                      ),
                    )
                  : ListView.builder(
                      key: const Key('switcher-results'),
                      itemCount: hits.length,
                      itemBuilder: (context, i) {
                        final h = hits[i];
                        final s = h.server;
                        final conv = h.conversation;
                        final title = conv != null
                            ? channelTitle(s, conv)
                            : h.account!.name;
                        final detail = conv != null
                            ? (conv.isNamed
                                  ? channelDetail(s, conv, showServer: false)
                                  : conv.isSealed
                                  ? L10n.of(context).sealedChat
                                  : L10n.of(context).chat)
                            : '@${h.account!.username}';
                        return Material(
                          type: MaterialType.transparency,
                          child: ListTile(
                            tileColor: i == highlighted ? c.raised : null,
                            dense: true,
                            selected: i == highlighted,
                            leading: conv != null
                                ? Glyph(
                                    conv.isSealed
                                        ? ChurrIcons.shield
                                        : channelIcon(
                                            conv.isNamed,
                                            conv.private,
                                            conv.isLocked,
                                          ),
                                    color: c.dim,
                                    size: 18,
                                  )
                                : Avatar(
                                    account: h.account,
                                    server: s,
                                    size: 22,
                                  ),
                            title: Row(
                              children: [
                                Flexible(
                                  child: Text(
                                    title,
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ),
                                if (conv?.membership?.show?.name ==
                                    'hidden') ...[
                                  const SizedBox(width: 6),
                                  Text(
                                    L10n.of(context).hiddenTag,
                                    style: monoStyle(context),
                                  ),
                                ],
                              ],
                            ),
                            subtitle: Text(
                              detail,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(fontSize: 11, color: c.dim),
                            ),
                            trailing: many ? ServerMark(server: s) : null,
                            onTap: () => _open(h),
                          ),
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
