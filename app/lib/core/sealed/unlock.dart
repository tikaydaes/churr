// Approval from another device (docs/SEALED.md section 13.7): the unlock
// request a new, locked device posts and the code that stops the server
// swapping its keys; and the comparison code of section 15.

import 'dart:convert';
import 'dart:typed_data';

import 'device_list.dart';
import 'enc.dart';
import 'errors.dart';
import 'prim.dart';
import 'sign.dart';

/// The record a new device posts to ask for the backup key: 170 bytes.
class UnlockRequest {
  final int format;
  final Uuid accountId, deviceId;
  final Uint8List signKey, boxKey;
  final int platform;
  final int at;
  final Uint8List proof;
  UnlockRequest({
    this.format = 1,
    required this.accountId,
    required this.deviceId,
    required this.signKey,
    required this.boxKey,
    required this.platform,
    required this.at,
    required this.proof,
  });

  Uint8List encode() {
    if (signKey.length != 32 || boxKey.length != 32 || proof.length != 64) throw RecordError.short;
    return (Writer()
          ..u8(format)
          ..uuid(accountId)
          ..uuid(deviceId)
          ..raw(signKey)
          ..raw(boxKey)
          ..u8(platform)
          ..time(at)
          ..raw(proof))
        .bytes();
  }

  /// Reads the record without checking the proof.
  static UnlockRequest decode(List<int> b) {
    final r = Reader(b);
    final u = UnlockRequest(
      format: r.choice(const [1]),
      accountId: r.uuid(),
      deviceId: r.uuid(),
      signKey: r.raw(32),
      boxKey: r.raw(32),
      platform: r.choice(allPlatforms),
      at: r.u64(),
      proof: r.raw(64),
    );
    r.done();
    return u;
  }
}

/// Reads an unlock request and checks its device proof.
Future<UnlockRequest> checkUnlockRequest(List<int> b) async {
  final u = UnlockRequest.decode(b);
  if (!await verify(
    u.signKey,
    labelDeviceProof,
    proofBytes(u.accountId, u.deviceId, u.signKey, u.boxKey),
    u.proof,
  )) {
    throw const ListError(2, "the device's proof does not verify");
  }
  return u;
}

const _crockford = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

/// The first 100 bits of SHA-256("churr device approval v1" || 0x00 ||
/// request) as 20 Crockford base32 characters in four hyphenated groups.
String approvalCode(List<int> request) {
  final sum = hashOf(labelDeviceApproval, request);
  final sb = StringBuffer();
  for (var i = 0; i < 20; i++) {
    var v = 0;
    for (var bit = i * 5; bit < (i + 1) * 5; bit++) {
      v = (v << 1) | ((sum[bit ~/ 8] >> (7 - bit % 8)) & 1);
    }
    if (i > 0 && i % 5 == 0) sb.write('-');
    sb.write(_crockford[v]);
  }
  return sb.toString();
}

/// A typed code in the form [approvalCode] gives: upper case, hyphens and
/// spaces dropped and put back, O read as 0 and I and L as 1.
String normalizeApprovalCode(String typed) {
  final chars = <String>[];
  for (final c in typed.toUpperCase().split('')) {
    switch (c) {
      case '-' || ' ':
        continue;
      case 'O':
        chars.add('0');
      case 'I' || 'L':
        chars.add('1');
      default:
        chars.add(c);
    }
  }
  final sb = StringBuffer();
  for (var i = 0; i < chars.length; i++) {
    if (i > 0 && i % 5 == 0) sb.write('-');
    sb.write(chars[i]);
  }
  return sb.toString();
}

/// How many times each half of the comparison code is hashed again.
const compareRounds = 5200;

/// One account's 30-digit half of the comparison code.
String compareHalf(List<int> accountKey, Uuid account, String server) {
  var h = hashOf(
    labelCompare,
    cat([accountKey, account.bytes, utf8.encode(server)]),
  );
  for (var i = 0; i < compareRounds; i++) {
    h = sha256(cat([h, accountKey]));
  }
  final sb = StringBuffer();
  for (var g = 0; g < 6; g++) {
    var v = 0;
    for (final b in h.sublist(g * 5, g * 5 + 5)) {
      v = (v << 8) | b;
    }
    sb.write((v % 100000).toString().padLeft(5, '0'));
  }
  return sb.toString();
}

/// The 60 digits two people compare: the smaller half first, in twelve
/// groups of five separated by spaces.
String compareCode(String a, String b) {
  if (b.compareTo(a) < 0) (a, b) = (b, a);
  final digits = a + b;
  return [for (var i = 0; i < digits.length; i += 5) digits.substring(i, i + 5)]
      .join(' ');
}
