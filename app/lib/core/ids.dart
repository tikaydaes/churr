import 'dart:math';

final _random = Random.secure();

/// A new UUIDv7 (docs/CONVENTIONS.md, Identifiers): the `client_id` of a
/// message, and the local identifier of a server's folder. Time-ordered,
/// so the outbox sorts in the order things were written.
String newUuid([DateTime? now]) {
  final ms = (now ?? DateTime.now()).millisecondsSinceEpoch;
  final b = List<int>.generate(16, (_) => _random.nextInt(256));
  for (var i = 0; i < 6; i++) {
    b[i] = (ms >> (8 * (5 - i))) & 0xFF;
  }
  b[6] = 0x70 | (b[6] & 0x0F);
  b[8] = 0x80 | (b[8] & 0x3F);
  final h = [for (final x in b) x.toRadixString(16).padLeft(2, '0')].join();
  return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-'
      '${h.substring(16, 20)}-${h.substring(20)}';
}
