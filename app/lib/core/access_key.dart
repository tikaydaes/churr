import 'dart:convert';
import 'dart:typed_data';

import 'sealed/prim.dart' show hkdf, sha256;

/// Access keys (docs/AUTH.md section 5.6): the long random code an account
/// signs in with when its person cannot use a passkey. The apps sign in
/// through the server's own pages in the system browser, so the pages make
/// and read keys there; this is the same grammar, kept here so the three
/// implementations (internal/secret/accesskey.go, static/shared/accesskey.js
/// and this file) run the one set of vectors in testdata/accesskey.
///
///   35 bytes = version (1) || 32 random bytes || first 2 bytes of SHA-256 of the first 33
///
/// read as one big-endian number, written in base 58 with [accessKeyAlphabet],
/// left-padded with its zero digit ("2") to 48 characters, shown as 12
/// groups of 4. Typed text may have spaces, other whitespace and hyphens
/// anywhere; case matters.
const accessKeyAlphabet =
    '23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';

const _version = 1;
const _chars = 48;
const _salt = 'churr access key v1';
const _authInfo = 'churr access key auth v1';
const _wrapInfo = 'churr access key wrap v1';

const _msgCharacter =
    'An access key never has 0, 1, capital O or capital I. Check that '
    'character: it may be a small o or l.';
const _msgOther = 'That character is not part of an access key.';
const _msgChecksum =
    'That key has a typing mistake. Check each character: capital and '
    'small letters are different.';
const _msgVersion = 'That is not an access key from this version of Churr.';

/// What is wrong with typed text. [kind] is length, character, checksum or
/// version, as the shared vectors name them.
class AccessKeyException implements Exception {
  AccessKeyException(this.kind, this.message);
  final String kind;
  final String message;
  @override
  String toString() => message;
}

final _base = BigInt.from(58);

Uint8List _checksum(List<int> head) =>
    Uint8List.fromList(sha256(head).sublist(0, 2));

/// Writes 32 random bytes as an access key.
String formatAccessKey(List<int> random) {
  if (random.length != 32) {
    throw ArgumentError('an access key holds 32 random bytes');
  }
  final head = [_version, ...random];
  final raw = [...head, ..._checksum(head)];
  var n = BigInt.zero;
  for (final b in raw) {
    n = (n << 8) | BigInt.from(b);
  }
  final digits = List.filled(_chars, '');
  for (var i = _chars - 1; i >= 0; i--) {
    digits[i] = accessKeyAlphabet[(n % _base).toInt()];
    n = n ~/ _base;
  }
  final groups = <String>[];
  for (var i = 0; i < _chars; i += 4) {
    groups.add(digits.sublist(i, i + 4).join());
  }
  return groups.join(' ');
}

const _skip = {'-', ' ', '\t', '\n', '\r', '\f', '\v', ' '};

/// Reads typed text back to its 32 random bytes, or throws an
/// [AccessKeyException] with words to show.
Uint8List parseAccessKey(String text) {
  var n = BigInt.zero;
  var count = 0;
  for (final rune in text.runes) {
    final ch = String.fromCharCode(rune);
    if (_skip.contains(ch)) continue;
    if (ch == '0' || ch == '1' || ch == 'O' || ch == 'I') {
      throw AccessKeyException('character', _msgCharacter);
    }
    final d = rune < 128 ? accessKeyAlphabet.indexOf(ch) : -1;
    if (d < 0) throw AccessKeyException('character', _msgOther);
    n = n * _base + BigInt.from(d);
    count++;
  }
  if (count != _chars) {
    throw AccessKeyException(
      'length',
      'An access key is 48 characters; this has $count.',
    );
  }
  if (n.bitLength > 280) throw AccessKeyException('checksum', _msgChecksum);
  final raw = Uint8List(35);
  for (var i = 34; i >= 0; i--) {
    raw[i] = (n & BigInt.from(0xff)).toInt();
    n = n >> 8;
  }
  final head = raw.sublist(0, 33);
  final want = _checksum(head);
  if (raw[33] != want[0] || raw[34] != want[1]) {
    throw AccessKeyException('checksum', _msgChecksum);
  }
  if (head[0] != _version) throw AccessKeyException('version', _msgVersion);
  return Uint8List.fromList(head.sublist(1));
}

/// The value sent to sign in, never the key itself.
Uint8List accessKeyAuth(List<int> random) =>
    hkdf(random, utf8.encode(_salt), utf8.encode(_authInfo), 32);

/// Reserved for locking keys under an access key; nothing uses it yet.
Uint8List accessKeyWrap(List<int> random) =>
    hkdf(random, utf8.encode(_salt), utf8.encode(_wrapInfo), 32);
