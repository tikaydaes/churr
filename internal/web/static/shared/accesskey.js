// Access keys (docs/AUTH.md section 5.6): the long random code an account
// signs in with when its person cannot use a passkey. This page makes it,
// shows it once, and sends the server only a value derived from it, so the
// key itself never leaves the browser.
//
// The grammar is the server's (internal/secret/accesskey.go) and the app's
// (app/lib/core/access_key.dart); all three run testdata/accesskey.
//
//   35 bytes = version (1) || 32 random bytes || first 2 bytes of SHA-256 of the first 33
//
// read as one big-endian number, written in base 58 with the alphabet below,
// left-padded with its zero digit ("2") to 48 characters, and shown as 12
// groups of 4. Typed text may have spaces, other whitespace and hyphens
// anywhere; case matters. Web Crypto does the hashing, so this needs a
// secure context, which the server always is (HTTPS, or the local trial at
// http://localhost).

export const ALPHABET = '23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
const VERSION = 1;
const CHARS = 48;
const SALT = 'churr access key v1';
const AUTH_INFO = 'churr access key auth v1';
const WRAP_INFO = 'churr access key wrap v1';

const MSG_CHARACTER = 'An access key never has 0, 1, capital O or capital I. Check that character: it may be a small o or l.';
const MSG_OTHER = 'That character is not part of an access key.';
const MSG_CHECKSUM = 'That key has a typing mistake. Check each character: capital and small letters are different.';
const MSG_VERSION = 'That is not an access key from this version of Churr.';

// AccessKeyError says what is wrong with typed text. kind is length,
// character, checksum or version, as the shared vectors name them.
export class AccessKeyError extends Error {
  constructor(kind, message) { super(message); this.name = 'AccessKeyError'; this.kind = kind; }
}

const subtle = () => {
  const s = globalThis.crypto && globalThis.crypto.subtle;
  if (!s) throw new Error('This page needs a secure connection (https) to work with access keys.');
  return s;
};
const enc = (s) => new TextEncoder().encode(s);

async function checksum(head) {
  return new Uint8Array(await subtle().digest('SHA-256', head)).slice(0, 2);
}

function toNumber(bytes) {
  let n = 0n;
  for (const b of bytes) n = (n << 8n) | BigInt(b);
  return n;
}

// formatAccessKey writes 32 random bytes as an access key.
export async function formatAccessKey(random) {
  if (random.length !== 32) throw new Error('an access key holds 32 random bytes');
  const head = new Uint8Array(33);
  head[0] = VERSION;
  head.set(random, 1);
  const raw = new Uint8Array(35);
  raw.set(head);
  raw.set(await checksum(head), 33);
  let n = toNumber(raw);
  const digits = new Array(CHARS);
  for (let i = CHARS - 1; i >= 0; i--) { digits[i] = ALPHABET[Number(n % 58n)]; n /= 58n; }
  const groups = [];
  for (let i = 0; i < CHARS; i += 4) groups.push(digits.slice(i, i + 4).join(''));
  return groups.join(' ');
}

// newAccessKey makes a new key: { random, text }.
export async function newAccessKey() {
  const random = crypto.getRandomValues(new Uint8Array(32));
  return { random, text: await formatAccessKey(random) };
}

const SKIP = new Set(['-', ' ', '\t', '\n', '\r', '\f', '\v', ' ']);

// parseAccessKey reads typed text back to its 32 random bytes, or throws an
// AccessKeyError with words to show.
export async function parseAccessKey(text) {
  let n = 0n, count = 0;
  for (const ch of String(text)) {
    if (SKIP.has(ch)) continue;
    if (ch === '0' || ch === '1' || ch === 'O' || ch === 'I') throw new AccessKeyError('character', MSG_CHARACTER);
    const d = ALPHABET.indexOf(ch);
    if (d < 0 || ch.length !== 1) throw new AccessKeyError('character', MSG_OTHER);
    n = n * 58n + BigInt(d);
    count++;
  }
  if (count !== CHARS) throw new AccessKeyError('length', `An access key is 48 characters; this has ${count}.`);
  if (n >> 280n) throw new AccessKeyError('checksum', MSG_CHECKSUM);
  const raw = new Uint8Array(35);
  for (let i = 34; i >= 0; i--) { raw[i] = Number(n & 0xffn); n >>= 8n; }
  const head = raw.slice(0, 33);
  const want = await checksum(head);
  if (raw[33] !== want[0] || raw[34] !== want[1]) throw new AccessKeyError('checksum', MSG_CHECKSUM);
  if (head[0] !== VERSION) throw new AccessKeyError('version', MSG_VERSION);
  return head.slice(1);
}

async function derive(random, info) {
  const key = await subtle().importKey('raw', random, 'HKDF', false, ['deriveBits']);
  return new Uint8Array(await subtle().deriveBits({ name: 'HKDF', hash: 'SHA-256', salt: enc(SALT), info: enc(info) }, key, 256));
}

// accessKeyAuth is the value sent to sign in: never the key itself.
export const accessKeyAuth = (random) => derive(random, AUTH_INFO);
// accessKeyWrap is reserved for locking keys under an access key.
export const accessKeyWrap = (random) => derive(random, WRAP_INFO);

const b64url = (bytes) => {
  let s = '';
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
};

// authFor turns typed text into what the API takes as `access_key_auth`.
// A typing mistake is reported here, before anything is sent.
export async function authFor(text) {
  return b64url(await accessKeyAuth(await parseAccessKey(text)));
}

// authOf is authFor for a key this page has just made.
export async function authOf(random) {
  return b64url(await accessKeyAuth(random));
}
