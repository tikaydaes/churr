import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../state/app_state.dart';
import '../screens/channels/new_channel_dialog.dart';
import '../screens/conversation/chat_commands.dart';
import '../screens/lock/lock_actions.dart';
import '../screens/search/quick_switcher_dialog.dart';
import '../screens/search/search_screen.dart';
import 'bot_dialog_host.dart';
import 'register.dart';

/// Wraps the shell with what the forms, search and channels screens need
/// everywhere: bot dialogs, the form commands in the composer, and the
/// desktop keys of docs/CLIENTS.md 13 that open these screens (Mod+K
/// quick switcher, Mod+F search here, Mod+Shift+F search everywhere,
/// Mod+N new chat, Mod+Shift+N new channel), and Mod+Shift+L, the lock.
class FindAndCreateHost extends StatelessWidget {
  const FindAndCreateHost({super.key, required this.app, required this.child});
  final AppState app;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    registerFormCommands();
    registerChatCommands();
    final mac =
        defaultTargetPlatform == TargetPlatform.macOS ||
        defaultTargetPlatform == TargetPlatform.iOS;
    SingleActivator mod(LogicalKeyboardKey k, {bool shift = false}) =>
        SingleActivator(k, meta: mac, control: !mac, shift: shift);
    return BotDialogHost(
      app: app,
      child: CallbackShortcuts(
        bindings: {
          mod(LogicalKeyboardKey.keyK): () => openQuickSwitcher(context),
          mod(LogicalKeyboardKey.keyF, shift: true): () => openSearch(context),
          mod(LogicalKeyboardKey.keyF): () {
            final open = app.open;
            final server = open == null ? null : app.server(open.serverId);
            openSearch(
              context,
              server: server,
              conversationId: open?.conversationId,
            );
          },
          mod(LogicalKeyboardKey.keyN): () => openNewChat(context),
          mod(LogicalKeyboardKey.keyN, shift: true): () =>
              openNewChannel(context),
          // The lock (docs/CLIENTS.md section 13).
          mod(LogicalKeyboardKey.keyL, shift: true): () => lockAsking(context),
        },
        child: child,
      ),
    );
  }
}
