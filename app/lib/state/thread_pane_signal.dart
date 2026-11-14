import 'package:flutter/foundation.dart';

/// Whether the open conversation shows its thread pane, for the shell
/// (docs/CLIENTS.md section 10): on expanded widths the sidebar narrows
/// to make room, and on compact ones the back gesture closes the thread
/// before the conversation. The conversation screen keeps it current.
abstract final class ThreadPaneSignal {
  static final open = ValueNotifier<bool>(false);

  /// Closes the open thread pane; set while one is open.
  static VoidCallback? close;

  /// Called by the conversation screen after each frame.
  static void report(bool isOpen, VoidCallback closer) {
    close = isOpen ? closer : null;
    if (open.value != isOpen) open.value = isOpen;
  }

  /// The back gesture: closes the thread if one is open. Returns whether
  /// it did.
  static bool closeIfOpen() {
    final c = close;
    if (!open.value || c == null) return false;
    c();
    return true;
  }
}

/// The sidebar's width beside an open thread pane on an expanded window.
const narrowSidebarWidth = 240.0;
