import 'package:flutter/widgets.dart';

import '../../../state/server_state.dart';

/// A built-in slash command typed in a composer, handed to whoever
/// carries it out.
class CommandRequest {
  const CommandRequest({
    required this.server,
    required this.conversationId,
    required this.name,
    required this.args,
    this.parentId,
  });

  final ServerState server;
  final String conversationId;

  /// Set when typed in a thread: what is posted goes in that thread.
  final String? parentId;

  /// The command without its slash: `poll`, `rsvp`, `signup`, `approve`,
  /// `form`, `remind`.
  final String name;

  /// What was typed after the name, trimmed. Channel links in it are
  /// written `<#conversation-id>`.
  final String args;
}

/// Carries out a built-in command: opens its dialog, posts with ordinary
/// calls. Returns true when it was carried out or is under way, so the
/// composer clears; false leaves the typed text in the box.
typedef BuiltinCommandHandler = Future<bool> Function(
  BuildContext context,
  CommandRequest request,
);

/// The registry of built-in slash commands (docs/FEATURES.md 12.3).
///
/// Built-in commands are carried out by the client, not by `runCommand`.
/// The forms builder registers the presets and the form builder; the
/// reminders code registers `remind`:
///
/// ```dart
/// BuiltinCommands.register('poll', (context, request) async { … });
/// ```
///
/// Saved replies, custom commands and bot commands do not come here:
/// the composer runs them through `runCommand`.
abstract final class BuiltinCommands {
  static final _handlers = <String, BuiltinCommandHandler>{};

  static void register(String name, BuiltinCommandHandler handler) =>
      _handlers[name] = handler;

  static void unregister(String name) => _handlers.remove(name);

  static BuiltinCommandHandler? handlerFor(String name) => _handlers[name];
}
