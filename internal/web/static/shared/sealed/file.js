// Files (docs/SEALED.md section 9): a file is cut into chunks of 65,536
// bytes and each is encrypted under a key used for that one file. Both
// directions work as Web Streams (TransformStream), so a large file is
// never held whole in memory: Web Crypto encrypts one buffer at a time, and
// here a buffer is one chunk.

import { concat, u64 } from './bytes.js';
import { FileError, OpenError } from './errors.js';
import { gcmOpen, gcmSeal } from './prim.js';
import { labelFile, } from './sign.js';

export const CHUNK_SIZE = 65536;
export const SEALED_CHUNK_SIZE = CHUNK_SIZE + 16;
const AD = new TextEncoder().encode(labelFile);

// The nonce of chunk i: 3 zero bytes, i as a u64, then 1 for the last
// chunk and 0 otherwise.
export function chunkNonce(i, last) {
  return concat(new Uint8Array(3), u64(i), new Uint8Array([last ? 1 : 0]));
}

export const encryptChunk = (key, i, last, plain) => gcmSeal(key, chunkNonce(i, last), plain, AD);

export async function decryptChunk(key, i, last, sealed) {
  try { return await gcmOpen(key, chunkNonce(i, last), sealed, AD); } catch (err) {
    if (err instanceof OpenError) throw new FileError();
    throw err;
  }
}

// The size of the blob a file of n bytes becomes.
export function blobSize(n) {
  return n + 16 * Math.max(1, Math.ceil(n / CHUNK_SIZE));
}

// Pieces holds bytes until a whole piece of `size` is known not to be the
// last.
class Pieces {
  constructor(size) { this.size = size; this.buf = new Uint8Array(size * 2); this.len = 0; }
  add(data) {
    if (this.len + data.length > this.buf.length) {
      const grown = new Uint8Array((this.len + data.length) * 2);
      grown.set(this.buf.subarray(0, this.len));
      this.buf = grown;
    }
    this.buf.set(data, this.len);
    this.len += data.length;
  }
  // Pieces that are certainly not the last: only while more than one
  // piece's worth is held.
  takeNotLast() {
    const out = [];
    let start = 0;
    while (this.len - start > this.size) { out.push(this.buf.slice(start, start + this.size)); start += this.size; }
    if (start > 0) { this.buf.copyWithin(0, start, this.len); this.len -= start; }
    return out;
  }
  rest() { return this.buf.slice(0, this.len); }
}

// encryptStream is a TransformStream from the file's bytes to the blob to
// upload, one sealed chunk per piece. An empty file is one empty chunk; a
// file whose length is a multiple of the chunk size ends with a full one.
export function encryptStream(key) {
  if (key.length !== 32) throw new Error('a file key is 32 bytes');
  const pieces = new Pieces(CHUNK_SIZE);
  let i = 0;
  return new TransformStream({
    async transform(data, ctl) {
      pieces.add(data instanceof Uint8Array ? data : new Uint8Array(data));
      for (const p of pieces.takeNotLast()) ctl.enqueue(await encryptChunk(key, i++, false, p));
    },
    async flush(ctl) { ctl.enqueue(await encryptChunk(key, i, true, pieces.rest())); },
  });
}

// decryptStream is a TransformStream from a blob to the file's bytes, one
// checked chunk per piece. A chunk changed, moved or missing, or the blob
// cut short, errors the stream with a FileError: the file must be used
// only once the stream has ended without one, since only then has its
// last chunk checked out.
export function decryptStream(key) {
  if (key.length !== 32) throw new Error('a file key is 32 bytes');
  const pieces = new Pieces(SEALED_CHUNK_SIZE);
  let i = 0;
  return new TransformStream({
    async transform(data, ctl) {
      pieces.add(data instanceof Uint8Array ? data : new Uint8Array(data));
      for (const p of pieces.takeNotLast()) ctl.enqueue(await decryptChunk(key, i++, false, p));
    },
    async flush(ctl) {
      const last = pieces.rest();
      // An empty blob, or a trailing piece shorter than a tag, is no chunk.
      if (last.length < 16) throw new FileError();
      ctl.enqueue(await decryptChunk(key, i, true, last));
    },
  });
}

// The whole-buffer forms, for small files and tests.
export async function encryptFile(key, plain) {
  const chunks = Math.max(1, Math.ceil(plain.length / CHUNK_SIZE));
  const out = [];
  for (let i = 0; i < chunks; i++) out.push(await encryptChunk(key, i, i === chunks - 1, plain.subarray(i * CHUNK_SIZE, Math.min((i + 1) * CHUNK_SIZE, plain.length))));
  return concat(...out);
}

export async function decryptFile(key, blob) {
  if (blob.length < 16) throw new FileError();
  const rest = blob.length % SEALED_CHUNK_SIZE;
  if (rest !== 0 && rest < 16) throw new FileError();
  const chunks = Math.ceil(blob.length / SEALED_CHUNK_SIZE);
  const out = [];
  for (let i = 0; i < chunks; i++) out.push(await decryptChunk(key, i, i === chunks - 1, blob.subarray(i * SEALED_CHUNK_SIZE, Math.min((i + 1) * SEALED_CHUNK_SIZE, blob.length))));
  return concat(...out);
}

// progressStream counts bytes going through: onProgress(done).
export function progressStream(onProgress) {
  let done = 0;
  return new TransformStream({ transform(data, ctl) { done += data.length; onProgress(done); ctl.enqueue(data); } });
}

// collect reads a stream of byte arrays to the end into a Blob, or with
// asBytes into one Uint8Array.
export async function collect(stream, asBytes = false) {
  const parts = [];
  const reader = stream.getReader();
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    parts.push(value);
  }
  return asBytes ? concat(...parts) : new Blob(parts);
}
