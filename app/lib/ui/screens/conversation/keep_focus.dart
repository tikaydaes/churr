// The composer keeps the keyboard (docs/CLIENTS.md section 9.5). Typing
// goes to the message box unless another text box has been chosen:
//
// - After sending, the box that sent keeps focus (on phones too).
// - On a desktop: opening a conversation or a thread, a click on a
//   message, a button or empty space, and closing a dialog, menu or picker
//   that a click opened all put focus back in the box. A click that leaves
//   text selected does not, and another text box keeps focus.
// - A printable key pressed while focus is on no text box (a message, a
//   selection, a button) goes to the box: it moves there and the character
//   goes in, once. Ctrl, Cmd and Alt combinations, Space on a control, and
//   keys a focused widget already handles are left alone; Ctrl or Cmd+V
//   there pastes in the box.
//
// Keyboard use moves focus as it always did: Tab, Shift+Tab and the arrow
// keys go where they go, and a dialog or menu opened from the keyboard
// gives focus back to what opened it. On a phone nothing but sending
// moves focus, so the on-screen keyboard never pops up by itself.
//
// "The box" is whichever composer was used last that can take text now:
// the conversation's or the thread pane's. A locked or read-only
// conversation has none.

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// A composer as the keeper of focus sees it.
class ComposerBox {
  ComposerBox({
    required this.node,
    required this.canFocus,
    required this.type,
    required this.paste,
  });

  final FocusNode node;

  /// Whether the box is on screen and takes text now.
  final bool Function() canFocus;

  /// Puts [text] in at the cursor, with focus.
  final void Function(String text) type;

  /// Pastes what the clipboard holds, with focus.
  final Future<void> Function() paste;

  bool get ready => node.context != null && node.context!.mounted && canFocus();
}

abstract final class KeepFocus {
  static final _boxes = <ComposerBox>[];
  static ComposerBox? _last;
  static final _selections = <Object>{};
  static bool _byPointer = false;
  static bool _installed = false;

  /// Whether the last thing the person did was with a pointer (as against
  /// a key that is not typing).
  static bool get byPointer => _byPointer;

  static void _install() {
    if (_installed) return;
    _installed = true;
    GestureBinding.instance.pointerRouter.addGlobalRoute((e) {
      if (e is PointerDownEvent) _byPointer = true;
    });
    HardwareKeyboard.instance.addHandler((e) {
      if (e is KeyDownEvent && (e.character == null || e.character!.isEmpty)) {
        _byPointer = false;
      }
      return false;
    });
  }

  /// Adds a composer; the function returned removes it.
  static VoidCallback register(ComposerBox box) {
    _install();
    _boxes.add(box);
    void heard() {
      if (box.node.hasFocus) _last = box;
    }

    box.node.addListener(heard);
    return () {
      box.node.removeListener(heard);
      _boxes.remove(box);
      if (_last == box) _last = null;
    };
  }

  /// The composer typing goes to, or null.
  static ComposerBox? get target {
    final ready = [
      for (final b in _boxes)
        if (b.ready) b,
    ];
    if (_last != null && ready.contains(_last)) return _last;
    return ready.isEmpty ? null : ready.first;
  }

  /// A message's text selection, made or gone ([owner] is the message).
  static void noteSelection(Object owner, bool selected) =>
      selected ? _selections.add(owner) : _selections.remove(owner);

  /// Whether text in a message is selected.
  static bool get selecting => _selections.isNotEmpty;

  /// Whether this is a desktop, where a click or a closed dialog gives the
  /// composer its focus back.
  static bool desktop(BuildContext context) =>
      switch (Theme.of(context).platform) {
        TargetPlatform.macOS ||
        TargetPlatform.windows ||
        TargetPlatform.linux => true,
        _ => false,
      };

  /// Whether a focus node is a text box's.
  static bool isTextBox(FocusNode? n) {
    final c = n?.context;
    if (c == null || !c.mounted) return false;
    return c.widget is EditableText ||
        c.findAncestorWidgetOfExactType<EditableText>() != null;
  }

  static bool _isComposer(FocusNode? n) =>
      n != null && _boxes.any((b) => b.node == n);

  /// Puts focus in [box] (the target by default), unless another text box
  /// has it, a dialog or menu is over the screen or, after a click, text
  /// is selected; with [force] a text box gives it up too. Returns whether
  /// the composer has focus afterwards.
  static bool returnFocus({
    ComposerBox? box,
    bool force = false,
    bool click = false,
  }) {
    final b = box ?? target;
    if (b == null || !b.ready) return false;
    final route = ModalRoute.of(b.node.context!);
    if (route != null && !route.isCurrent) return false;
    final primary = FocusManager.instance.primaryFocus;
    if (primary == b.node) return true;
    if (!force && (_isComposer(primary) || isTextBox(primary))) {
      return _isComposer(primary);
    }
    if (click && selecting) return false;
    b.node.requestFocus();
    return true;
  }
}

/// Keeps the composer's focus over a conversation: around the whole
/// screen, the thread pane included.
class KeepComposerFocus extends StatefulWidget {
  const KeepComposerFocus({super.key, required this.child});
  final Widget child;

  @override
  State<KeepComposerFocus> createState() => _KeepComposerFocusState();
}

class _KeepComposerFocusState extends State<KeepComposerFocus> {
  final _node = FocusNode(
    debugLabel: 'keep composer focus',
    canRequestFocus: false,
    skipTraversal: true,
  );
  bool _current = true;
  bool _coveredByPointer = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    // A dialog, menu or picker over the screen, and gone again: one a
    // click opened gives the composer its focus back.
    final now = ModalRoute.isCurrentOf(context) ?? true;
    if (now == _current) return;
    _current = now;
    if (!now) {
      _coveredByPointer = KeepFocus.byPointer;
    } else if (_coveredByPointer && KeepFocus.desktop(context)) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) KeepFocus.returnFocus();
      });
    }
  }

  @override
  void dispose() {
    _node.dispose();
    super.dispose();
  }

  void _pointerUp(PointerUpEvent e) {
    if (!KeepFocus.desktop(context) || e.kind == PointerDeviceKind.touch) {
      return;
    }
    // Once the click has done what it does (opened a dialog, made a
    // selection), focus goes back.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) KeepFocus.returnFocus(click: true);
    });
  }

  KeyEventResult _key(FocusNode _, KeyEvent e) {
    if (e is! KeyDownEvent && e is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    final primary = FocusManager.instance.primaryFocus;
    if (KeepFocus.isTextBox(primary)) return KeyEventResult.ignored;
    final box = KeepFocus.target;
    if (box == null) return KeyEventResult.ignored;
    final kb = HardwareKeyboard.instance;
    final mod = kb.isControlPressed || kb.isMetaPressed;
    if (mod && !kb.isAltPressed && e.logicalKey == LogicalKeyboardKey.keyV) {
      if (KeepFocus.returnFocus(box: box, force: true)) box.paste();
      return KeyEventResult.handled;
    }
    if (mod || kb.isAltPressed) return KeyEventResult.ignored;
    final ch = e.character;
    if (ch == null ||
        ch.isEmpty ||
        ch.runes.any((r) => r < 0x20 || r == 0x7f)) {
      return KeyEventResult.ignored;
    }
    // Space activates a focused control.
    final ctx = primary?.context;
    if (ch == ' ' &&
        ctx != null &&
        ctx.mounted &&
        Actions.maybeFind<ActivateIntent>(ctx) != null) {
      return KeyEventResult.ignored;
    }
    if (!KeepFocus.returnFocus(box: box, force: true)) {
      return KeyEventResult.ignored;
    }
    box.type(ch);
    return KeyEventResult.handled;
  }

  @override
  Widget build(BuildContext context) => Listener(
    behavior: HitTestBehavior.translucent,
    onPointerUp: _pointerUp,
    child: Focus(focusNode: _node, onKeyEvent: _key, child: widget.child),
  );
}
