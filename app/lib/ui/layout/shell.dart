import 'package:flutter/material.dart';

import '../../state/app_prefs.dart';
import '../../state/thread_pane_signal.dart';
import '../forms/find_and_create_host.dart';
import '../scopes.dart';
import '../screens/add_server/add_server_screen.dart';
import '../screens/call/call_overlay.dart';
import '../screens/channels/ban_notice.dart';
import '../screens/conversation/conversation_screen.dart';
import '../screens/sealed/sealed_chat_screen.dart';
import '../screens/sealed/sealed_prompts.dart';
import '../screens/settings/app_settings_screen.dart';
import '../theme/tokens.dart';
import '../widgets/controls.dart';
import 'breakpoints.dart';
import 'server_rail.dart';
import 'sidebar.dart';

/// The one layout shell (docs/CLIENTS.md section 10). It arranges the
/// sidebar and the conversation by available width:
///
/// - **compact:** one pane at a time, the conversation pushed over the
///   sidebar with a way back;
/// - **medium and up:** sidebar and conversation side by side, the
///   sidebar's width dragged to taste where the layout allows.
///
/// The thread pane (a third pane on expanded and wide) is not built
/// yet; it belongs between the conversation and the right edge here.
class Shell extends StatelessWidget {
  const Shell({super.key});

  void _addServer(BuildContext context) => Navigator.of(context)
      .push(MaterialPageRoute<void>(builder: (_) => const AddServerScreen()));

  void _settings(BuildContext context) {
    final compact =
        WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
    if (compact) {
      Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) => Scaffold(
            appBar: AppBar(title: const Text('App settings')),
            body: const AppSettingsScreen(),
          ),
        ),
      );
    } else {
      // Settings are a dialog where there is room for one.
      showDialog<void>(
  animationStyle: AnimationStyle.noAnimation,
        context: context,
        builder: (_) => const Dialog(
          child: SizedBox(
            width: 560,
            height: 640,
            child: AppSettingsScreen(showTitle: true),
          ),
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final c = ChurrColors.of(context);
    // Bot dialogs, form commands and the find-and-create keys. Calls
    // (the call screen, the compact bar, the incoming-call prompt) wrap it.
    return CallOverlay(
      app: app,
      // Sealed chats: unlock requests and passkey approvals.
      child: SealedPromptHost(
      app: app,
      child: FindAndCreateHost(
      app: app,
      // Telling someone they were banned from a channel (ruling R72).
      child: BanNoticeHost(
      app: app,
      child: ListenableBuilder(
        listenable: Listenable.merge([app.prefs, ThreadPaneSignal.open]),
        builder: (context, _) => LayoutBuilder(
          builder: (context, box) {
            final cls = WidthClass.of(box.maxWidth);
            final rail = app.prefs.layout == SidebarLayout.rail;
            final open = app.open;
            final openServer = open == null ? null : app.server(open.serverId);
            final sidebar = Sidebar(
              onAddServer: () => _addServer(context),
              onOpenSettings: () => _settings(context),
            );
            Widget conversation({VoidCallback? onBack}) =>
                // A sealed chat has its own screen (lib/ui/screens/sealed).
                openServer!.conversation(open!.conversationId)?.isSealed ?? false
                ? SealedChatScreen(
                    key: ValueKey(open),
                    server: openServer,
                    conversationId: open.conversationId,
                    onBack: onBack,
                  )
                : ConversationScreen(
              // A new conversation is a new screen with a new controller.
              key: ValueKey(open),
              server: openServer,
              conversationId: open.conversationId,
              onBack: onBack,
            );

            if (cls == WidthClass.compact) {
              if (openServer != null) {
                return PopScope(
                  canPop: false,
                  // Back closes an open thread before the conversation.
                  onPopInvokedWithResult: (_, _) {
                    if (!ThreadPaneSignal.closeIfOpen()) {
                      app.openConversation(null);
                    }
                  },
                  child: SafeArea(
                    child: conversation(
                      onBack: () => app.openConversation(null),
                    ),
                  ),
                );
              }
              return SafeArea(
                child: Column(
                  children: [
                    // On a phone the rail is a strip of tiles across the top.
                    if (rail)
                      ServerRail(
                        axis: Axis.horizontal,
                        onAddServer: () => _addServer(context),
                      ),
                    Expanded(child: sidebar),
                  ],
                ),
              );
            }

            final full = rail
                ? railListWidth
                : cls == WidthClass.medium
                ? mediumSidebarWidth
                : app.prefs.sidebarWidth;
            // An open thread pane on an expanded window takes room from
            // the sidebar (docs/CLIENTS.md section 10).
            final width =
                cls == WidthClass.expanded &&
                    openServer != null &&
                    ThreadPaneSignal.open.value
                ? narrowSidebarWidth
                : full;
            final draggable =
                !rail && cls != WidthClass.medium && width == full;
            return Row(
              children: [
                if (rail)
                  ServerRail(
                    axis: Axis.vertical,
                    onAddServer: () => _addServer(context),
                  ),
                SizedBox(width: width, child: sidebar),
                MouseRegion(
                  cursor: draggable
                      ? SystemMouseCursors.resizeColumn
                      : MouseCursor.defer,
                  child: GestureDetector(
                    behavior: HitTestBehavior.opaque,
                    onHorizontalDragUpdate: draggable
                        ? (d) => app.prefs.sidebarWidth = width + d.delta.dx
                        : null,
                    // No rule: the panel of the list meets the ground of the
                  // conversation. Where it can be dragged, a strip of panel.
                  child: Container(width: draggable ? 5 : 0, color: c.panel),
                  ),
                ),
                Expanded(
                  child: openServer == null ? const _Empty() : conversation(),
                ),
              ],
            );
          },
        ),
      ),
    ),
    ),
    ),
    );
  }
}

class _Empty extends StatelessWidget {
  const _Empty();

  @override
  Widget build(BuildContext context) =>
      const EmptyState(title: 'Nothing open', body: 'Pick a conversation.');
}
