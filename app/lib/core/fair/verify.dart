// Checking a message's draw on the device (docs/FAIR.md section 9), as the
// web chat does (shared/fair/verify.js), with nothing asked of any server:
// the message and the VRF key history pinned on this device for the server
// that drew it hold all it needs. The block's layout and rule are known;
// alpha is exactly the input spelt from its second `t` and the message's own
// client_id, so a proof cannot be moved to another message; the second is
// near the message's time; pk is the key its owner had when the message was
// made, by the pinned history; the proof verifies under pk and gives beta;
// and every roll that took chance from the draw is what derive 1 gives for
// it. A check that fails says why; a draw by a server whose history this
// device was never given says it cannot be checked here, which is not a
// failure. It never hides the roll. Plain Dart, tested without Flutter.

import 'dart:convert';
import 'dart:typed_data';

import '../types/catalogue.dart';
import 'derive.dart';
import 'kinds.dart';
import 'pin.dart';
import 'ecvrf.dart' as ecvrf;

/// How far a draw's second may be from its message's time: the server's
/// window, and the looser one for a message drawn by another server, whose
/// clock may be off from this one's (internal/fair, Window and LinkWindow).
const fairWindow = 10, fairLinkWindow = 120;

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

final _canonical = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);

/// Whether a nonce is a UUID in its one spelling: lowercase, hyphenated.
bool canonicalNonce(Object? s) => s is String && _canonical.hasMatch(s);

/// The input of a draw (docs/FAIR.md section 6): `<t>|<nonce>`.
String alphaOf(int t, String nonce) => '$t|$nonce';

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
/// [server] is this server's name, whose draws are held to the narrower
/// window.
FairCheck verifyMessage(
  Map<String, dynamic> m, {
  required FairHistory? Function(String name) keysOf,
  required String server,
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
  final pk = fromB64(block['pk']), beta = fromB64(block['beta']);
  final proof = fromB64(block['proof']);
  final alpha = block['alpha'], t = block['t'];
  if (pk == null ||
      beta == null ||
      proof == null ||
      alpha is! String ||
      t is! int ||
      t < 1) {
    return fail('The proof is not whole.');
  }
  final clientId = m['client_id'];
  if (!canonicalNonce(clientId) || alpha != alphaOf(t, clientId as String)) {
    return fail("The proof is not this message's.");
  }
  final owner = block['owner'] as String? ?? '';
  final created = DateTime.tryParse(m['created_at'] as String? ?? '');
  final window = owner == server ? fairWindow : fairLinkWindow;
  if (created == null ||
      (t * 1000 - created.millisecondsSinceEpoch).abs() > window * 1000) {
    return fail("The proof is for another time than the message's.");
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
  final key = fromB64(keyAt(history.keys, created));
  if (!_same(key, pk)) {
    return fail('The roll was not proved with the key $owner had at the time.');
  }
  final r = ecvrf.verify(pk, proof, utf8.encode(alpha));
  if (!r.valid || !_same(r.beta, beta)) {
    return fail('The proof does not check out.');
  }
  // A kind with a composition of derive 1 is re-derived in full, from its
  // checked data; a kind this device checks whose data does not hold
  // together fails; one it does not know is only proved, not re-derived.
  bool rollOk(Object? v) {
    if (v is! Map || v['draw'] is! Map) return true;
    final i = (v['draw'] as Map)['i'];
    if (i is! int || i < 0) return false;
    final r = readValue(v);
    final kind = v['kind'];
    if (r.drawn && !r.removed && !r.alt) {
      return rederive(r.kind!, r.data!, beta, i) ?? true;
    }
    final def = kind is String ? catalogueKinds[kind] : null;
    final level = v['level'];
    if (def != null &&
        v['removed'] != true &&
        level is int &&
        level <= catalogueLevel) {
      return false;
    }
    return true;
  }

  var ok = rollOk(m['view']);
  for (final e in (m['entities'] as List? ?? const [])) {
    if (e is Map && e['type'] == 'object') ok = ok && rollOk(e['object']);
  }
  if (!ok) return fail('An outcome shown is not the one the proof gives.');
  return FairCheck(FairState.verified, block: block);
}
