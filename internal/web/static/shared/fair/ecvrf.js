// ECVRF-SECP256K1-SHA256-TAI (RFC 9381), the half a reader needs: verify a
// proof and turn a proof into its output. The web chat never proves; only a
// server holds a VRF key (docs/DESIGN-FAIR.md).
//
// A port of the owner's library github.com/atikayda/ecvrf at 93a33bb (the
// Dart implementation's arithmetic, which needs no curve library; LICENSE
// beside this file is the library's) to plain JavaScript: secp256k1 on
// BigInt, SHA-256 from the browser's Web Crypto.
// Nothing here is constant-time, and nothing here touches a secret. It is
// held to the library's vectors, testdata/ecvrf/vectors.json, by
// e2e/fair.mjs, which runs this file as it is under Node.
//
// Bytes are Uint8Array. A public key is 33 bytes (SEC1 compressed), a proof
// 81 bytes: Gamma (33) || c (16) || s (32). The output, beta, is 32 bytes.

const SUITE = 0xfe;

// The field prime, the group order and the generator.
const P = 0xfffffffffffffffffffffffffffffffffffffffffffffffffffffffefffffc2fn;
const N = 0xfffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141n;
const G = {
  x: 0x79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798n,
  y: 0x483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8n,
};
const SQRT_EXP = (P + 1n) >> 2n; // p = 3 (mod 4)

export const PROOF_LENGTH = 81;
export const PUBLIC_KEY_LENGTH = 33;
export const BETA_LENGTH = 32;

const mod = (a, m = P) => { const r = a % m; return r < 0n ? r + m : r; };

function pow(b, e, m) {
  let r = 1n;
  b = mod(b, m);
  while (e > 0n) {
    if (e & 1n) r = r * b % m;
    b = b * b % m;
    e >>= 1n;
  }
  return r;
}

function toBig(bytes) {
  let r = 0n;
  for (const b of bytes) r = (r << 8n) | BigInt(b);
  return r;
}

function toBytes(v, len) {
  const out = new Uint8Array(len);
  for (let i = len - 1; i >= 0; i--) { out[i] = Number(v & 0xffn); v >>= 8n; }
  return out;
}

function concat(...parts) {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of parts) { out.set(p, at); at += p.length; }
  return out;
}

async function sha256(bytes) {
  return new Uint8Array(await globalThis.crypto.subtle.digest('SHA-256', bytes));
}

// --- points: affine { x, y }, or null for the identity ---

// decodePoint reads a 33-byte compressed point; null if it is not one.
function decodePoint(b) {
  if (!(b instanceof Uint8Array) || b.length !== 33 || (b[0] !== 2 && b[0] !== 3)) return null;
  const x = toBig(b.subarray(1));
  if (x >= P) return null;
  const rhs = mod(x * x % P * x + 7n);
  let y = pow(rhs, SQRT_EXP, P);
  if (y * y % P !== rhs) return null;
  if ((y & 1n) !== BigInt(b[0] & 1)) y = P - y;
  return { x, y };
}

function encodePoint(p) {
  return concat(new Uint8Array([p.y & 1n ? 3 : 2]), toBytes(p.x, 32));
}

// Jacobian coordinates [X, Y, Z] for (X/Z^2, Y/Z^3); Z = 0 is the identity.
const INF = [1n, 1n, 0n];

function jDouble([x, y, z]) {
  if (z === 0n || y === 0n) return INF;
  const a = x * x % P, b = y * y % P, c = b * b % P;
  const xb = x + b;
  const d = mod(2n * (xb * xb - a - c));
  const e = 3n * a % P, f = e * e % P;
  const x3 = mod(f - 2n * d);
  const y3 = mod(e * (d - x3) - 8n * c);
  return [x3, y3, 2n * y * z % P];
}

function jAdd(p, q) {
  if (p[2] === 0n) return q;
  if (q[2] === 0n) return p;
  const [x1, y1, z1] = p, [x2, y2, z2] = q;
  const z1z1 = z1 * z1 % P, z2z2 = z2 * z2 % P;
  const u1 = x1 * z2z2 % P, u2 = x2 * z1z1 % P;
  const s1 = y1 * z2 % P * z2z2 % P, s2 = y2 * z1 % P * z1z1 % P;
  const h = mod(u2 - u1), r = mod(s2 - s1);
  if (h === 0n) return r === 0n ? jDouble(p) : INF;
  const hh = h * h % P, hhh = h * hh % P, v = u1 * hh % P;
  const x3 = mod(r * r - hhh - 2n * v);
  const y3 = mod(r * (v - x3) - s1 * hhh);
  return [x3, y3, z1 * z2 % P * h % P];
}

function toAffine([x, y, z]) {
  if (z === 0n) return null;
  const zi = pow(z, P - 2n, P), zi2 = zi * zi % P;
  return { x: x * zi2 % P, y: y * zi2 % P * zi % P };
}

// mul is k * p by double and add. Verification only: k is public.
function mul(p, k) {
  k = mod(k, N);
  let acc = INF;
  const base = [p.x, p.y, 1n];
  for (let i = k.toString(2).length - 1; i >= 0; i--) {
    acc = jDouble(acc);
    if ((k >> BigInt(i)) & 1n) acc = jAdd(acc, base);
  }
  return acc;
}

// --- the suite ---

// encodeToCurve is try_and_increment (RFC 9381 5.4.1.1), even y.
async function encodeToCurve(pk, alpha) {
  const prefix = concat(new Uint8Array([SUITE, 0x01]), pk, alpha);
  for (let ctr = 0; ctr < 256; ctr++) {
    const hash = await sha256(concat(prefix, new Uint8Array([ctr, 0x00])));
    const h = decodePoint(concat(new Uint8Array([0x02]), hash));
    if (h) return h;
  }
  return null;
}

async function challenge(...points) {
  const hash = await sha256(concat(new Uint8Array([SUITE, 0x02]), ...points.map(encodePoint), new Uint8Array([0x00])));
  return toBig(hash.subarray(0, 16));
}

async function gammaToHash(gamma) {
  return sha256(concat(new Uint8Array([SUITE, 0x03]), encodePoint(gamma), new Uint8Array([0x00])));
}

function decodeProof(pi) {
  if (!(pi instanceof Uint8Array) || pi.length !== PROOF_LENGTH) return null;
  const gamma = decodePoint(pi.subarray(0, 33));
  const c = toBig(pi.subarray(33, 49));
  const s = toBig(pi.subarray(49, 81));
  if (!gamma || s >= N) return null;
  return { gamma, c, s };
}

// isPublicKey says whether b is a 33-byte compressed point of the curve.
export function isPublicKey(b) {
  return decodePoint(b) !== null;
}

// proofToHash is beta of a proof (RFC 9381 5.2), without verifying it.
// Throws if the proof cannot be decoded.
export async function proofToHash(pi) {
  const d = decodeProof(pi);
  if (!d) throw new Error('ecvrf: not a proof');
  return gammaToHash(d.gamma);
}

// verify checks pi for the public key pk and alpha (RFC 9381 5.3). It never
// throws: { valid: true, beta } or { valid: false, beta: null }.
export async function verify(pk, pi, alpha) {
  const invalid = { valid: false, beta: null };
  const d = decodeProof(pi);
  const y = decodePoint(pk);
  if (!d || !y || !(alpha instanceof Uint8Array)) return invalid;
  const h = await encodeToCurve(pk, alpha);
  if (!h) return invalid;
  const negC = mod(N - d.c, N);
  // U = s*G - c*Y, V = s*H - c*Gamma.
  const u = toAffine(jAdd(mul(G, d.s), mul(y, negC)));
  const v = toAffine(jAdd(mul(h, d.s), mul(d.gamma, negC)));
  // The identity has no encoding, so no challenge can be made over it.
  if (!u || !v) return invalid;
  if ((await challenge(y, h, d.gamma, u, v)) !== d.c) return invalid;
  return { valid: true, beta: await gammaToHash(d.gamma) };
}
