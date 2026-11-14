import 'package:flutter/widgets.dart';

import '../state/app_state.dart';
import '../state/server_state.dart';

/// Hands the [AppState] down the tree (docs/CLIENTS.md section 4).
/// `AppScope.of(context)` rebuilds the caller when the app state
/// notifies; `AppScope.read(context)` does not, for use in callbacks.
class AppScope extends InheritedNotifier<AppState> {
  const AppScope({super.key, required AppState state, required super.child})
    : super(notifier: state);

  static AppState of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppScope>()!.notifier!;

  static AppState read(BuildContext context) =>
      context.getInheritedWidgetOfExactType<AppScope>()!.notifier!;
}

/// Hands one server's [ServerState] to the screens drawn for it.
class ServerScope extends InheritedNotifier<ServerState> {
  const ServerScope({
    super.key,
    required ServerState server,
    required super.child,
  }) : super(notifier: server);

  static ServerState of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<ServerScope>()!.notifier!;
}
