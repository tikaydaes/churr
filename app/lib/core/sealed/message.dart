// The sealed message of docs/SEALED.md section 8.1: an 82-byte header, a
// 16-byte salt, the encrypted content with its tag, and the sending
// device's signature.

import 'dart:typed_data';

import 'epoch.dart' show signatureSize;
import 'errors.dart';
import 'prim.dart';
import 'seal.dart';
import 'sign.dart';

const headerSize = 82;

/// The most content one message may carry.
const maxContent = 128 * 1024;

/// Bit 0 of the flags.
const flagQuiet = 1;

/// The readable front of a sealed message.
class Header {
  final int format;
  final int flags;
  final Uuid conversationId;
  final int keyEpoch;
  final Uuid accountId; // the sender's account
  final Uuid deviceId; // the sender's device
  final int counter;
  final Uuid reference; // also the message's client_id

  Header({
    this.format = 1,
    this.flags = 0,
    required this.conversationId,
    required this.keyEpoch,
    required this.accountId,
    required this.deviceId,
    required this.counter,
    required this.reference,
  });

  bool get quiet => flags & flagQuiet != 0;

  Uint8List encode() => cat([
    [format, flags],
    conversationId.bytes,
    u64be(keyEpoch),
    accountId.bytes,
    deviceId.bytes,
    u64be(counter),
    reference.bytes,
  ]);

  /// Reads the header of a sealed message.
  static Header decode(List<int> sealed) {
    if (sealed.length < headerSize) throw const MessageError('it is shorter than a header');
    final h = Header(
      format: sealed[0],
      flags: sealed[1],
      conversationId: Uuid(sealed.sublist(2, 18)),
      keyEpoch: readU64(sealed, 18),
      accountId: Uuid(sealed.sublist(26, 42)),
      deviceId: Uuid(sealed.sublist(42, 58)),
      counter: readU64(sealed, 58),
      reference: Uuid(sealed.sublist(66, 82)),
    );
    if (h.format != 1) throw const MessageError('its format is not 1');
    if (h.flags & ~flagQuiet != 0) throw const MessageError('it sets flags that do not exist');
    return h;
  }
}

/// Encrypts and signs one message. [senderKey] is the sending device's for
/// the header's epoch, [salt] 16 random bytes, [content] the UTF-8 JSON.
Future<Uint8List> sealMessage(
  Header h,
  List<int> senderKey,
  List<int> salt,
  List<int> content,
  SigningKey signKey,
) async {
  if (content.length > maxContent) throw ArgumentError("a message's content is at most 128 KB");
  final header = h.encode();
  final body = cat([
    header,
    lock(senderKey, labelLockMessage, header, content, salt),
  ]);
  return cat([body, await sign(signKey, labelMessage, body)]);
}

/// Checks the signature of a sealed message under the sending device's
/// signing key and decrypts its content. The checks that need the row or
/// the history are [Chat.receive]'s.
Future<(Header, Uint8List)> openMessage(
  List<int> sealed,
  List<int> senderKey,
  List<int>? signKey,
) async {
  final h = Header.decode(sealed);
  if (sealed.length < headerSize + saltSize + 16 + signatureSize) throw const MessageError('it is too short');
  final n = sealed.length - signatureSize;
  final body = sealed.sublist(0, n);
  final sig = sealed.sublist(n);
  if (signKey == null || !await verify(signKey, labelMessage, body, sig)) {
    throw const MessageError('the signature does not verify');
  }
  try {
    final content = unlockLocked(
      senderKey,
      labelLockMessage,
      body.sublist(0, headerSize),
      body.sublist(headerSize),
    );
    return (h, content);
  } on OpenError {
    throw const MessageError('the content does not decrypt');
  }
}
