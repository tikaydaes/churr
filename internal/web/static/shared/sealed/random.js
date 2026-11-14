// Where random values come from. Every value is drawn for a named purpose
// (the `for` names of testdata/sealed/README.md), so a test can hand the
// library the vectors' values in place of the generator.

import { randomBytes } from './bytes.js';

// The browser's generator (section 2.2).
export class SystemRandom {
  take(what, n) { return randomBytes(n); }
}

// For tests only: values given in advance, in the order they will be
// drawn, each with its purpose. A draw for another purpose or length, or
// past the end, throws.
export class GivenRandom {
  constructor(values) { this.values = values; this.used = 0; }
  get left() { return this.values.length - this.used; }
  take(what, n) {
    if (this.used >= this.values.length) throw new Error(`no given random value left for "${what}"`);
    const [forWhat, value] = this.values[this.used];
    if (forWhat !== what || value.length !== n) {
      throw new Error(`random value ${this.used + 1} is for "${forWhat}" (${value.length} bytes), but "${what}" (${n} bytes) was drawn`);
    }
    this.used++;
    return new Uint8Array(value);
  }
}
