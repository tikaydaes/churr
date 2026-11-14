// Where a device's state is kept. The library writes values under string
// keys through a store with four methods:
//
//   read(key) → { data, keys } or null      write(key, { data, keys })
//   delete(key)                              keys() → [key]
//
// `data` is plain JSON (bytes as base64url); `keys` holds the device's
// own CryptoKeys, which are never turned into bytes. The browser's store
// (idb.js) encrypts `data` before it is written and keeps the CryptoKeys
// as they are, non-extractable. MemoryStore is for tests.
//
// What is written: the device's keys, the backup key and account key,
// pins and verified marks, notices, and for every chat its epoch records,
// secrets, send counter and history secrets still to back up. What the
// device draws (messages, reactions, ticks, and the replay records that
// go with them) is written only by a store whose keepsMessages is true;
// the browser's is false, so decrypted messages are never stored and the
// page decrypts what it shows each time it loads.

import { fromB64, toB64 } from './bytes.js';
import { decodeAccountRecord, encodeAccountRecord } from './backup.js';
import { decodeList, decodeSignedList, encodeSignedList } from './devicelist.js';
import { decodeEpoch, splitStoredEpoch } from './epoch.js';
import { EpochError, SealedError } from './errors.js';
import { BoxKey, SigningKey } from './prim.js';
import { Device, Pin } from './device.js';
import { Epoch } from './chat.js';

export class MemoryStore {
  constructor({ keepsMessages = true } = {}) { this.values = new Map(); this.keepsMessages = keepsMessages; }
  async read(key) { const v = this.values.get(key); return v ? { data: JSON.parse(v.data, revive), keys: v.keys } : null; }
  async write(key, value) { this.values.set(key, { data: JSON.stringify(value.data, replace), keys: value.keys || null }); }
  async delete(key) { this.values.delete(key); }
  async keys() { return [...this.values.keys()]; }
}

// JSON with BigInts: a u64 beyond 2^53 is written as { "$big": "…" }.
export const replace = (k, v) => (typeof v === 'bigint' ? { $big: v.toString() } : v);
export const revive = (k, v) => (v && typeof v === 'object' && typeof v.$big === 'string' ? BigInt(v.$big) : v);

const DEVICE_KEY = 'sealed/device';
const chatKey = (id) => 'sealed/chat/' + id;
const b = (v) => (v ? toB64(v) : null);
const ub = (v) => (v ? fromB64(v) : null);

function deviceJson(d) {
  const pins = {};
  for (const [acc, p] of d.pins) {
    pins[acc] = { lowest: p.lowest, newest: p.newest, last_reset: p.lastReset, verified: p.verified, hold: p.hold, versions: [...p.versions.values()].map((v) => toB64(encodeSignedList(v.signed))) };
  }
  return {
    v: 1, account: d.account, server: d.server, id: d.id, platform: d.platform, added_at: d.addedAt,
    sign_public: toB64(d.sign.public), box_public: toB64(d.box.public),
    backup_key: b(d.backupKey), key_version: d.keyVersion, account_key_seed: b(d.accountKey && d.accountKey.seed),
    record: d.record ? toB64(encodeAccountRecord(d.record)) : null,
    pins, notices: d.notices.map((n) => ({ ...n })),
  };
}

function chatJson(c, keepMessages) {
  const out = {
    v: 1, id: c.id, other: c.other, counter: c.counter, share_due: c.shareDue, new_secrets: c.newSecrets,
    epochs: c.epochs.map((e) => ({
      key_epoch: e.keyEpoch, seq: e.seq, stored: toB64(e.stored), accepted: e.accepted,
      refusal: e.refusal ? e.refusal.message : null, refusal_rule: e.refusal instanceof EpochError ? e.refusal.rule : null,
      secret: b(e.secret), history: b(e.history),
    })),
  };
  if (keepMessages) {
    out.unchecked = c.unchecked;
    out.seen = [...c.seen];
    out.by_device = [...c.byDevice].map(([dev, rows]) => [dev, rows.map((r) => [r.counter, r.seq])]);
    out.messages = c.view().map((m) => ({
      ...m, reactions: [...m.reactions].map(([e, s]) => [e, [...s]]), tasks: [...m.tasks], taskSeq: [...m.taskSeq], taskBy: [...m.taskBy],
    }));
    out.sent_under = [...c.sentUnder].map(([k, list]) => [k, list.map(([ref, content]) => [ref, toB64(content)])]);
  }
  return out;
}

// attachStore gives a device the store its state is saved to.
export function attachStore(d, store) {
  d.store = store;
  d.saveChat = (c) => saveChat(d, c);
  return d;
}

// saveDevice writes the whole device: its own state and every chat.
export async function saveDevice(d) {
  const s = d.store;
  if (!s) return;
  await s.write(DEVICE_KEY, { data: deviceJson(d), keys: { sign: d.sign.key, box: d.box.key } });
  for (const c of d.chats.values()) await s.write(chatKey(c.id), { data: chatJson(c, s.keepsMessages) });
}

// saveChat writes one chat; what sealing does before it returns a message,
// so a raised counter is never lost. The device itself is written too if
// the store has none yet.
export async function saveChat(d, c) {
  const s = d.store;
  if (!s) return;
  if (!(await s.read(DEVICE_KEY))) await s.write(DEVICE_KEY, { data: deviceJson(d), keys: { sign: d.sign.key, box: d.box.key } });
  await s.write(chatKey(c.id), { data: chatJson(c, s.keepsMessages) });
}

// wipe deletes everything the device has written: what signing out does.
export async function wipe(store) {
  for (const k of await store.keys()) if (k.startsWith('sealed/')) await store.delete(k);
}

// loadDevice reads a device back from a store; null if it holds none.
export async function loadDevice(store, { random, now }) {
  const raw = await store.read(DEVICE_KEY);
  if (!raw) return null;
  const j = raw.data;
  const d = new Device({
    account: j.account, server: j.server, id: j.id, platform: j.platform, addedAt: j.added_at, random, now,
    sign: new SigningKey(raw.keys.sign, fromB64(j.sign_public)), box: new BoxKey(raw.keys.box, fromB64(j.box_public)),
  });
  attachStore(d, store);
  d.backupKey = ub(j.backup_key);
  d.keyVersion = j.key_version;
  if (j.account_key_seed) d.accountKey = await SigningKey.fromSeed(fromB64(j.account_key_seed));
  if (j.record) d.record = decodeAccountRecord(fromB64(j.record));
  for (const [acc, p] of Object.entries(j.pins)) {
    const versions = new Map();
    for (const sv of p.versions) {
      const signed = decodeSignedList(fromB64(sv));
      const l = decodeList(signed.list);
      versions.set(Number(l.version), { signed, list: l });
    }
    d.pins.set(acc, new Pin(versions, p.lowest, p.newest, { lastReset: p.last_reset, verified: p.verified, hold: p.hold }));
  }
  d.notices = j.notices.map((n) => ({ ...n }));
  for (const k of await store.keys()) {
    if (!k.startsWith('sealed/chat/')) continue;
    const cj = await store.read(k);
    if (cj) loadChat(d, cj.data);
  }
  return d;
}

function loadChat(d, j) {
  const c = d.chat(j.id, j.other);
  c.counter = j.counter;
  c.shareDue = j.share_due;
  c.newSecrets = [...j.new_secrets];
  for (const m of j.epochs) {
    const e = new Epoch(m.key_epoch, m.seq, fromB64(m.stored));
    e.accepted = m.accepted;
    e.secret = ub(m.secret);
    e.history = ub(m.history);
    if (m.refusal != null) e.refusal = m.refusal_rule != null ? Object.assign(new EpochError(m.refusal_rule, ''), { message: m.refusal }) : new SealedError(m.refusal);
    try { e.record = decodeEpoch(splitStoredEpoch(e.stored)[0]); } catch { e.record = null; }
    if (e.accepted) c.restoreDevices(e);
    c.epochs.push(e);
  }
  if (j.messages) {
    c.unchecked = j.unchecked;
    c.seen = new Map(j.seen);
    c.byDevice = new Map(j.by_device.map(([dev, rows]) => [dev, rows.map(([counter, seq]) => ({ counter, seq }))]));
    for (const m of j.messages) {
      const s = { ...m, reactions: new Map(m.reactions.map(([e, a]) => [e, new Set(a)])), tasks: new Map(m.tasks), taskSeq: new Map(m.taskSeq), taskBy: new Map(m.taskBy) };
      c.messages.set(s.reference, s);
      c.order.push(s.reference);
    }
    c.sentUnder = new Map(j.sent_under.map(([k, list]) => [k, list.map(([ref, content]) => [ref, fromB64(content)])]));
  }
}
