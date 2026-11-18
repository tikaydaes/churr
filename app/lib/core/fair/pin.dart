// The app's pin of a server's VRF key history (docs/DESIGN-FAIR.md
// section 1), as the web chat keeps it (internal/web/static/shared/fair/
// pin.js). getServer gives the history, oldest first; the first one seen is
// kept on this device. Later, the same history or the kept one with keys
// added after it is fine and kept; anything else is a change, shown as a
// warning and not taken. Entries are compared by key and start; the server
// checks the signatures.

import 'dart:convert';

/// What a shown history is against the pinned one.
enum PinState { first, same, grown, changed }

/// The outcome of a comparison: the state and the entries added.
class PinResult {
  const PinResult(this.state, [this.added = const []]);
  final PinState state;
  final List<Map<String, dynamic>> added;
}

bool _same(Map<String, dynamic> a, Map<String, dynamic> b) =>
    a['key'] == b['key'] && a['began'] == b['began'];

/// Compares a shown history with the pinned one; null is no pin yet.
PinResult comparePin(
  List<Map<String, dynamic>>? pinned,
  List<Map<String, dynamic>> shown,
) {
  if (pinned == null || pinned.isEmpty) {
    return shown.isEmpty
        ? const PinResult(PinState.changed)
        : PinResult(PinState.first, shown);
  }
  if (shown.length < pinned.length) return const PinResult(PinState.changed);
  for (var i = 0; i < pinned.length; i++) {
    if (!_same(pinned[i], shown[i])) return const PinResult(PinState.changed);
  }
  final added = shown.sublist(pinned.length);
  return PinResult(added.isEmpty ? PinState.same : PinState.grown, added);
}

/// The history as getServer gives it: a list of objects, others dropped.
List<Map<String, dynamic>> vrfKeysOf(Object? raw) => [
  for (final e in (raw is List ? raw : const []))
    if (e is Map) Map<String, dynamic>.from(e),
];

/// Compares [shown] with the pin [read] gives and keeps it with [write]
/// when it is the first sight or has grown. A pin that cannot be read is
/// no pin.
Future<PinResult> checkPin(
  Future<String?> Function() read,
  Future<void> Function(String) write,
  List<Map<String, dynamic>> shown,
) async {
  List<Map<String, dynamic>>? pinned;
  try {
    final raw = await read();
    if (raw != null) pinned = vrfKeysOf(jsonDecode(raw));
  } on FormatException {
    pinned = null;
  }
  final result = comparePin(pinned, shown);
  if (result.state == PinState.first || result.state == PinState.grown) {
    await write(jsonEncode(shown));
  }
  return result;
}
