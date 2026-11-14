import '../message/checklist.dart';
import '../message/form_view.dart';
import '../message/message_slots.dart';
import '../screens/conversation/command_handlers.dart';
import 'preset_dialogs.dart';

/// Puts the forms builder's widgets in the message widget's slots. The
/// slots default to these already; this is for code that resets them.
void registerFormWidgets() {
  MessageSlots.form = (context, server, message, markdown) =>
      FormView(server: server, message: message);
  MessageSlots.markdown = checklistMarkdown;
}

/// Registers `/poll`, `/rsvp`, `/signup`, `/approve` and `/form` with the
/// composer's built-in commands. Safe to call more than once; the shell
/// calls it when it is built.
void registerFormCommands() {
  for (final name in formCommands.keys) {
    if (BuiltinCommands.handlerFor(name) != null) continue;
    BuiltinCommands.register(
      name,
      (context, r) => runFormCommand(
        context,
        server: r.server,
        conversationId: r.conversationId,
        command: r.name,
        args: r.args,
        parentId: r.parentId,
      ),
    );
  }
}
