import 'package:flutter/widgets.dart';

import '../../core/models/message.dart';
import '../../state/server_state.dart';
import 'checklist.dart';
import 'form_view.dart';
import 'markdown_view.dart';

/// Draws one part of a message (its form).
typedef MessagePartBuilder = Widget Function(
  BuildContext context,
  ServerState server,
  Message message,
  MarkdownContext markdown,
);

/// Adjusts the Markdown context a message's text is drawn with (the
/// checklist rules: who may tick, who ticked and when, the history).
typedef MarkdownDecorator = MarkdownContext Function(
  BuildContext context,
  ServerState server,
  Message message,
  MarkdownContext base,
);

/// Places in a message that other parts of the app draw. Made by the
/// forms builder; the conversation screen's `MessageTile` calls both.
///
/// - [form]: the message's form. Defaults to the forms builder's
///   `FormView`, which shows the `fallback` text and "too old to answer"
///   for anything it cannot draw (docs/MESSAGE-FORMAT.md section 10).
/// - [markdown]: applied to the context of every message's text.
///
/// To put them back after a test changed them, call
/// `registerFormWidgets()` from lib/ui/forms/register.dart.
abstract final class MessageSlots {
  static MessagePartBuilder form = (context, server, message, markdown) =>
      FormView(server: server, message: message);

  static MarkdownDecorator markdown = checklistMarkdown;

  /// Draws a system message instead of the plain line, or returns null
  /// to leave it to the plain line. Calls use it for a call's message
  /// (lib/ui/screens/call/call_overlay.dart).
  static Widget? Function(BuildContext context, ServerState server, Message message)?
  system;
}
