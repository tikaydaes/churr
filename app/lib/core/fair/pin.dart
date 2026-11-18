// The app's pins of servers' VRF key histories (docs/DESIGN-FAIR.md
// section 1), one per server name, as the web chat keeps them
// (internal/web/static/shared/fair/pin.js). getServer gives this server's
// history, oldest first, and a shared conversation gives the history of every
// server whose draws it may hold (`shared.vrf_keys`). The first one seen for
// a name is kept on this device. Later, the same history or the kept one with
// keys added after it is fine and kept; anything else is a change, shown as a
// warning and not taken. Entries are compared by key and start; the server
// checks the signatures.

import 'dart:async';
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

/// The history a check of a server's draws is made against: the pin, never
/// a history that changed it, and whether one was shown that did.
class FairHistory {
  const FairHistory(this.keys, {this.changed = false});
  final List<Map<String, dynamic>> keys;
  final bool changed;
}

/// Every pin this device keeps for one server it signs in to, by server
/// name, in one setting. It is read once ([load]); from then on each source
/// that shows a history ([show], [showShared]) is compared with the pin at
/// once, and a pin made or grown is written behind. [version] grows whenever
/// what a check would find changes.
class FairHistories {
  FairHistories(this._write);
  final Future<void> Function(String) _write;
  final _pins = <String, List<Map<String, dynamic>>>{};
  final _shown = <String, (String, PinResult)>{};
  int version = 0;

  /// Takes the pins as stored; anything that cannot be read is no pin.
  void load(String? raw) {
    _pins.clear();
    _shown.clear();
    version++;
    if (raw == null) return;
    try {
      final j = jsonDecode(raw);
      if (j is Map) {
        for (final e in j.entries) {
          if (e.key is String) _pins[e.key as String] = vrfKeysOf(e.value);
        }
      }
    } on FormatException {
      _pins.clear();
    }
  }

  /// Compares what a source shows for a server with its pin: null when
  /// nothing was shown and nothing has been. A change seen in this run
  /// stays a change.
  PinResult? show(String name, List<Map<String, dynamic>> shown) {
    final had = _shown[name];
    if (name.isEmpty || shown.isEmpty) return had?.$2;
    final seen = jsonEncode(shown);
    if (had != null && had.$1 == seen) return had.$2;
    final pinned = _pins[name];
    var result = comparePin(pinned, shown);
    if (result.state == PinState.first || result.state == PinState.grown) {
      _pins[name] = shown;
      unawaited(_write(jsonEncode(_pins)));
    }
    if (had?.$2.state == PinState.changed) {
      result = const PinResult(PinState.changed);
    }
    _shown[name] = (seen, result);
    version++;
    return result;
  }

  /// Pins every history a conversation's `shared` gives.
  void showShared(Object? shared) {
    final list = shared is Map ? shared['vrf_keys'] : null;
    for (final e in (list is List ? list : const [])) {
      if (e is Map && e['server'] is String) {
        show(e['server'] as String, vrfKeysOf(e['keys']));
      }
    }
  }

  /// The history a check of [name]'s draws is made against, or null for a
  /// server no source has shown a history of.
  FairHistory? of(String name) {
    final had = _shown[name];
    if (had == null) return null;
    return FairHistory(
      _pins[name] ?? const [],
      changed: had.$2.state == PinState.changed,
    );
  }
}
