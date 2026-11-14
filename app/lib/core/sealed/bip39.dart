// The recovery phrase (docs/SEALED.md section 13.3): 12 words from the
// BIP-39 English list, standing for 128 random bits and BIP-39's 4-bit
// checksum. BIP-39's own key derivation is not used.

import 'dart:typed_data';

import 'bip39_words.dart';
import 'errors.dart';
import 'prim.dart';

export 'bip39_words.dart' show bip39English;

final Map<String, int> _index = {
  for (var i = 0; i < bip39English.length; i++) bip39English[i]: i,
};
final Map<String, int> _byFour = {
  for (var i = 0; i < bip39English.length; i++)
    if (bip39English[i].length >= 4) bip39English[i].substring(0, 4): i,
};

/// Writes 16 bytes as twelve words.
String phraseOf(List<int> entropy) {
  if (entropy.length != 16) throw ArgumentError('a phrase stands for 16 bytes');
  final bits = cat([
    entropy,
    [sha256(entropy)[0]],
  ]);
  return [for (var i = 0; i < 12; i++) bip39English[_bits11(bits, i)]]
      .join(' ');
}

int _bits11(List<int> b, int i) {
  var v = 0;
  for (var bit = i * 11; bit < (i + 1) * 11; bit++) {
    v = (v << 1) | ((b[bit ~/ 8] >> (7 - bit % 8)) & 1);
  }
  return v;
}

/// The index of a typed word: the word itself, or, for a typed word of at
/// least four letters, the list word with the same first four letters.
/// -1 if there is none.
int wordIndex(String w) {
  final exact = _index[w];
  if (exact != null) return exact;
  if (w.length < 4) return -1;
  return _byFour[w.substring(0, 4)] ?? -1;
}

/// Turns a typed phrase back into its 16 bytes: lower-cased, split on
/// white space, each word matched to the list, the checksum checked.
Uint8List readPhrase(String typed) {
  final words = typed
      .toLowerCase()
      .split(RegExp(r'\s+'))
      .where((w) => w.isNotEmpty)
      .toList();
  if (words.length != 12) throw const PhraseError();
  final bits = Uint8List(17);
  for (var i = 0; i < 12; i++) {
    final idx = wordIndex(words[i]);
    if (idx < 0) throw const PhraseError();
    for (var k = 0; k < 11; k++) {
      if ((idx >> (10 - k)) & 1 == 1) {
        final bit = i * 11 + k;
        bits[bit ~/ 8] |= 1 << (7 - bit % 8);
      }
    }
  }
  final entropy = bits.sublist(0, 16);
  if (bits[16] >> 4 != sha256(entropy)[0] >> 4) throw const PhraseError();
  return entropy;
}
