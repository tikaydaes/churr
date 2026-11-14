// The record encoding of docs/SEALED.md section 2.3: a fixed binary
// layout with exactly one valid form. A record is its fields in order with
// nothing between them and nothing after the last.

import { idBytes, idOf, readU32, readU64, u32, u64, utf8 } from './bytes.js';
import { RecordError } from './errors.js';

export const MAX_RECORD = 64 * 1024;

const strictUtf8 = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });

export class Writer {
  constructor() { this.parts = []; this.n = 0; }
  raw(b) { this.parts.push(b); this.n += b.length; return this; }
  u8(v) { return this.raw(new Uint8Array([v & 255])); }
  u32(v) { return this.raw(u32(v)); }
  u64(v) { return this.raw(u64(v)); }
  time(ms) { return this.u64(ms); }
  uuid(id) { return this.raw(idBytes(id)); }
  blob(b) { this.u32(b.length); return this.raw(b); }
  text(s) { return this.blob(utf8(s)); }

  // The record. One over 64 KB is refused.
  bytes() {
    if (this.n > MAX_RECORD) throw RecordError.tooLong();
    const out = new Uint8Array(this.n);
    let at = 0;
    for (const p of this.parts) { out.set(p, at); at += p.length; }
    return out;
  }
}

// Reader takes a record apart. The first failure sticks: later reads give
// zero values, and done() throws it.
export class Reader {
  constructor(b) {
    this.b = b;
    this.at = 0;
    this.err = b.length > MAX_RECORD ? RecordError.tooLong() : null;
  }

  get left() { return this.b.length - this.at; }

  take(n) {
    if (this.err) return null;
    if (n < 0 || this.left < n) { this.err = RecordError.short(); return null; }
    const out = this.b.subarray(this.at, this.at + n);
    this.at += n;
    return out;
  }

  u8() { const b = this.take(1); return b ? b[0] : 0; }

  // A u8 that must be one of allowed.
  choice(allowed) {
    const v = this.u8();
    if (this.err) return 0;
    if (allowed.includes(v)) return v;
    this.err = RecordError.choice();
    return 0;
  }

  u32() { const b = this.take(4); return b ? readU32(b) : 0; }
  u64() { const b = this.take(8); return b ? readU64(b) : 0; }

  // Exactly n bytes, as a copy.
  raw(n) { const b = this.take(n); return b ? new Uint8Array(b) : new Uint8Array(n); }
  uuid() { return idOf(this.raw(16)); }

  blob() {
    const n = this.u32();
    if (this.err) return new Uint8Array(0);
    if (n > this.left) { this.err = RecordError.short(); return new Uint8Array(0); }
    return this.raw(n);
  }

  // A blob that must hold valid UTF-8. It is never normalised.
  text() {
    const b = this.blob();
    if (this.err) return '';
    try { return strictUtf8.decode(b); } catch { this.err = RecordError.utf8(); return ''; }
  }

  // The u32 count in front of a list. A count that cannot fit in what is
  // left, given the smallest size of one item, is refused at once.
  count(minItem) {
    const n = this.u32();
    if (this.err) return 0;
    if (n * minItem > this.left) { this.err = RecordError.short(); return 0; }
    return n;
  }

  fail(e) { if (!this.err) this.err = e; }

  // Ends the record: bytes left over are refused.
  done() {
    if (this.err) throw this.err;
    if (this.left !== 0) throw RecordError.trailing();
  }
}
