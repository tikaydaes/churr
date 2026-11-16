// Numbers in another base and code points (MESSAGE-FORMAT.md section
// 3.2): what their converters show. testdata/objects/codepoints.json holds
// the cases both clients pass. No DOM.

const group = (s, n, sep) => s.replace(new RegExp(`\\B(?=(.{${n}})+$)`, 'g'), sep);

// numberRows are a number's rows: decimal grouped with "," (before the
// locale's marks), hexadecimal, octal, binary in groups of four, what
// each copies, and the code point it is as a character, or null.
export function numberRows(value) {
  const n = BigInt(value);
  let bin = n.toString(2);
  bin = bin.padStart(Math.ceil(bin.length / 4) * 4, '0');
  const hex = '0x' + n.toString(16).toUpperCase(), oct = '0o' + n.toString(8);
  const scalar = n <= 0x10ffffn && !(n >= 0xd800n && n <= 0xdfffn);
  return {
    decimal: group(n.toString(), 3, ','), hex, octal: oct, binary: '0b' + group(bin, 4, ' '),
    copy: { decimal: n.toString(), hex, octal: oct, binary: '0b' + n.toString(2) },
    codepoint: scalar ? 'U+' + n.toString(16).toUpperCase().padStart(4, '0') : null,
  };
}

const C0 = 'NUL SOH STX ETX EOT ENQ ACK BEL BS HT LF VT FF CR SO SI DLE DC1 DC2 DC3 DC4 NAK SYN ETB CAN EM SUB ESC FS GS RS US'.split(' ');
const SHORT = { 0x20: 'SP', 0x7f: 'DEL', 0x85: 'NEL', 0xa0: 'NBSP', 0xad: 'SHY', 0x200b: 'ZWSP', 0x200c: 'ZWNJ', 0x200d: 'ZWJ', 0x2028: 'LSEP', 0x2029: 'PSEP', 0x202f: 'NNBSP', 0x2060: 'WJ', 0xfeff: 'BOM' };
const UNSEEN = /^[\p{Cc}\p{Cf}\p{Z}\p{Co}\p{Cn}]$/u;
const MARK = /^\p{M}$/u;
const hex = (v, w) => v.toString(16).toUpperCase().padStart(w, '0');

// codePointFacts are what the converter shows of a code point (`U+1F600`).
export function codePointFacts(value) {
  const v = parseInt(String(value).replace(/^U\+/, ''), 16);
  const ch = String.fromCodePoint(v);
  const u16 = [];
  for (let i = 0; i < ch.length; i++) u16.push(ch.charCodeAt(i));
  const u8 = [...new TextEncoder().encode(ch)];
  let shown;
  if (MARK.test(ch)) shown = { kind: 'mark', text: '◌' + ch };
  else if (UNSEEN.test(ch)) shown = { kind: 'box', text: SHORT[v] || (v < 32 ? C0[v] : 'U+' + hex(v, 4)) };
  else shown = { kind: 'char', text: ch };
  return {
    value: 'U+' + hex(v, 4), char: ch, decimal: String(v),
    utf8: u8.map((b) => hex(b, 2)).join(' '), utf16: u16.map((u) => hex(u, 4)).join(' '),
    html: `&#x${hex(v, 1)};`, json: u16.map((u) => '\\u' + hex(u, 4)).join(''), js: `\\u{${hex(v, 1)}}`,
    python: v < 0x10000 ? '\\u' + hex(v, 4) : '\\U' + hex(v, 8), shown,
  };
}

// The names already asked for this session: code point → promise.
const names = new Map();

// codePointName asks the server once a session for a code point's name,
// through lookup(cp) (the API's lookupCodePoints); null when it fails.
export function codePointName(value, lookup) {
  if (!lookup) return Promise.resolve(null);
  if (!names.has(value)) {
    const p = Promise.resolve().then(() => lookup(value)).catch(() => null);
    names.set(value, p);
    p.then((r) => { if (!r) names.delete(value); });
  }
  return names.get(value);
}
