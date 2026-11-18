// Checking a message's draw on the device (docs/FAIR.md section 9), as the
// web chat does (shared/fair/verify.js), with nothing asked of any server:
// the message and the VRF key history pinned on this device for the server
// that drew it hold all it needs. The block's layout and rule are known; alpha is the one the
// conversation's chain gives for its n and prev; pk is the key its owner had
// when the message was made, by the pinned history; the proof verifies under
// pk and gives beta; and every roll that took chance from the draw is what
// derive 1 gives for it. A check that fails says why; a draw by a server
// whose history this device was never given says it cannot be checked here,
// which is not a failure. It never hides the roll. Plain Dart, tested
// without Flutter.

import 'dart:convert';
import 'dart:typed_data';

import 'derive.dart';
import 'pin.dart';
import 'ecvrf.dart' as ecvrf;

const _label = 'churr-fair-1';

/// What a check found: [unknown] is a draw by a server this device holds
/// no key history for, which cannot be checked here; [failed] a check made
/// that did not hold.
enum FairState { verified, failed, unknown, none }

/// The outcome of [verifyMessage]: the state, why it failed, and the block.
class FairCheck {
  const FairCheck(this.state, {this.why = '', this.block = const {}});
  final FairState state;
  final String why;
  final Map<String, dynamic> block;
}

/// Bytes from base64url (padding allowed), or null.
Uint8List? fromB64(Object? s) {
  if (s is! String) return null;
  try {
    return base64Url.decode(base64Url.normalize(s.replaceAll('=', '')));
  } on FormatException {
    return null;
  }
}

bool _same(List<int>? a, List<int>? b) {
  if (a == null || b == null || a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

/// alpha for draw [n] of a chain (docs/FAIR.md section 6).
Uint8List alphaOf(String owner, String chain, int n, List<int> prev) {
  final o = utf8.encode(owner), c = utf8.encode(chain);
  final b = BytesBuilder()..add(utf8.encode(_label));
  final len = ByteData(2);
  len.setUint16(0, o.length);
  b
    ..add(len.buffer.asUint8List())
    ..add(o);
  len.setUint16(0, c.length);
  b
    ..add(len.buffer.asUint8List())
    ..add(c);
  final nb = ByteData(8)
    ..setUint32(0, n ~/ 4294967296)
    ..setUint32(4, n % 4294967296);
  b
    ..add(nb.buffer.asUint8List())
    ..add(prev);
  return b.toBytes();
}

/// The chain an alpha names, or null.
String? chainOfAlpha(Uint8List alpha) {
  final label = utf8.encode(_label);
  if (alpha.length < label.length + 4 ||
      !_same(alpha.sublist(0, label.length), label)) {
    return null;
  }
  final v = ByteData.sublistView(alpha);
  var at = label.length;
  at += 2 + v.getUint16(at);
  if (at + 2 > alpha.length) return null;
  final m = v.getUint16(at);
  at += 2;
  if (alpha.length != at + m + 8 + 32) return null;
  return utf8.decode(alpha.sublist(at, at + m));
}

/// The key of a history in force at [at]: the last to have begun by then.
String? keyAt(List<Map<String, dynamic>> history, DateTime at) {
  String? out;
  for (final k in history) {
    final began = DateTime.tryParse(k['began'] as String? ?? '');
    if (began != null && !began.isAfter(at)) out = k['key'] as String?;
  }
  return out;
}

/// Checks a message's draw. [keysOf] gives the VRF key history pinned for a
/// server by name, or null when no source has given one (getServer for
/// this server, a shared conversation's `shared.vrf_keys` for the others);
/// [chains] the names the conversation's chain may have (its id, and its
/// stream's when shared).
FairCheck verifyMessage(
  Map<String, dynamic> m, {
  required FairHistory? Function(String name) keysOf,
  required List<String> chains,
}) {
  final raw = m['fair'];
  if (raw is! Map) return const FairCheck(FairState.none);
  final block = Map<String, dynamic>.from(raw);
  FairCheck fail(String why) =>
      FairCheck(FairState.failed, why: why, block: block);
  if (block['v'] != 1) {
    return fail('This proof is in a layout this app does not know.');
  }
  if (block['derive'] != deriveVersion) {
    return fail('This roll was worked out by a rule this app does not know.');
  }
  final pk = fromB64(block['pk']), alpha = fromB64(block['alpha']);
  final beta = fromB64(block['beta']), proof = fromB64(block['proof']);
  final prev = fromB64(block['prev']);
  final n = block['n'];
  if (pk == null ||
      alpha == null ||
      beta == null ||
      proof == null ||
      prev == null ||
      prev.length != 32 ||
      n is! int ||
      n < 1) {
    return fail('The proof is not whole.');
  }
  final chain = chainOfAlpha(alpha);
  if (chain == null || !chains.contains(chain)) {
    return fail('The proof is for another conversation.');
  }
  final owner = block['owner'] as String? ?? '';
  if (!_same(alpha, alphaOf(owner, chain, n, prev))) {
    return fail('The proof is not for this place in the conversation.');
  }
  final history = keysOf(owner);
  if (history == null) {
    return FairCheck(
      FairState.unknown,
      why:
          "It was drawn by $owner, and this conversation does not carry that "
          "server's key history, so it cannot be checked here.",
      block: block,
    );
  }
  if (history.changed) {
    return fail(
      '$owner now shows a different key history than the one this app '
      'pinned for it, so its proofs are not trusted.',
    );
  }
  final created = DateTime.tryParse(m['created_at'] as String? ?? '');
  final key = created == null ? null : fromB64(keyAt(history.keys, created));
  if (!_same(key, pk)) {
    return fail('The roll was not proved with the key $owner had at the time.');
  }
  final r = ecvrf.verify(pk, proof, alpha);
  if (!r.valid || !_same(r.beta, beta)) {
    return fail('The proof does not check out.');
  }
  bool rollOk(Object? v) {
    if (v is! Map || v['draw'] is! Map) return true;
    final i = (v['draw'] as Map)['i'];
    final data = v['data'];
    if (i is! int) return false;
    if (v['kind'] != 'dice_roll' || data is! Map || data['rows'] is! List) {
      return true;
    }
    return sameRoll(FairStream(beta, i), data['rows'] as List);
  }

  var ok = rollOk(m['view']);
  for (final e in (m['entities'] as List? ?? const [])) {
    if (e is Map && e['type'] == 'object') ok = ok && rollOk(e['object']);
  }
  if (!ok) return fail('A die shown is not the one the proof gives.');
  return FairCheck(FairState.verified, block: block);
}
