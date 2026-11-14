// The sealed message of docs/SEALED.md section 8.1: an 82-byte header, a
// 16-byte salt, the encrypted content with its tag, and the sending
// device's signature.

import { concat, idBytes, idOf, readU64, u64 } from './bytes.js';
import { SIGNATURE_SIZE } from './epoch.js';
import { MessageError, OpenError } from './errors.js';
import { SALT_SIZE, lock, unlockLocked } from './seal.js';
import { labelLockMessage, labelMessage, sign, verify } from './sign.js';

export const HEADER_SIZE = 82;
export const MAX_CONTENT = 128 * 1024;
export const FLAG_QUIET = 1;

// encodeHeader: h = { format, flags, conversationId, keyEpoch, accountId,
// deviceId, counter, reference }.
export function encodeHeader(h) {
  return concat(new Uint8Array([h.format ?? 1, h.flags || 0]), idBytes(h.conversationId), u64(h.keyEpoch),
    idBytes(h.accountId), idBytes(h.deviceId), u64(h.counter), idBytes(h.reference));
}

export function decodeHeader(sealed) {
  if (sealed.length < HEADER_SIZE) throw new MessageError('it is shorter than a header');
  const h = {
    format: sealed[0], flags: sealed[1],
    conversationId: idOf(sealed.subarray(2, 18)), keyEpoch: readU64(sealed, 18),
    accountId: idOf(sealed.subarray(26, 42)), deviceId: idOf(sealed.subarray(42, 58)),
    counter: readU64(sealed, 58), reference: idOf(sealed.subarray(66, 82)),
  };
  if (h.format !== 1) throw new MessageError('its format is not 1');
  if (h.flags & ~FLAG_QUIET) throw new MessageError('it sets flags that do not exist');
  h.quiet = (h.flags & FLAG_QUIET) !== 0;
  return h;
}

// sealMessage encrypts and signs one message. senderKey is the sending
// device's for the header's epoch, salt 16 random bytes, content the UTF-8
// JSON.
export async function sealMessage(h, senderKey, salt, content, signKey) {
  if (content.length > MAX_CONTENT) throw new Error("a message's content is at most 128 KB");
  const header = encodeHeader(h);
  const body = concat(header, await lock(senderKey, labelLockMessage, header, content, salt));
  return concat(body, await sign(signKey, labelMessage, body));
}

// openMessage checks the signature under the sending device's signing key
// and decrypts the content: [header, content]. The checks that need the
// row or the history are the chat's (chat.js).
export async function openMessage(sealed, senderKey, signKey) {
  const h = decodeHeader(sealed);
  if (sealed.length < HEADER_SIZE + SALT_SIZE + 16 + SIGNATURE_SIZE) throw new MessageError('it is too short');
  const n = sealed.length - SIGNATURE_SIZE;
  const body = sealed.subarray(0, n);
  if (!signKey || !(await verify(signKey, labelMessage, body, sealed.subarray(n)))) throw new MessageError('the signature does not verify');
  try {
    return [h, await unlockLocked(senderKey, labelLockMessage, body.subarray(0, HEADER_SIZE), body.subarray(HEADER_SIZE))];
  } catch (err) {
    if (err instanceof OpenError) throw new MessageError('the content does not decrypt');
    throw err;
  }
}
