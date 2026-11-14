// The browser's store for a sealed-chat device (docs/SEALED.md section
// 14): one IndexedDB database per account, belonging to the server's
// origin, named "churr-sealed-<account id>".
//
// - The device's signing key and box key are CryptoKeys made
//   non-extractable, stored as they are: a script can use them and cannot
//   read their bytes.
// - Everything else is encrypted before it is written, under an
//   AES-256-GCM key the browser generated as non-extractable and keeps in
//   the same database.
//
// This keeps raw key bytes out of the stored files. It does not stop a
// script running in the page from using the keys; it is a small
// protection and is not counted on. Decrypted messages are never written
// (keepsMessages is false).

import { replace, revive } from './store.js';

const PREFIX = 'churr-sealed-';
const subtle = globalThis.crypto.subtle;

function req(r) {
  return new Promise((resolve, reject) => { r.onsuccess = () => resolve(r.result); r.onerror = () => reject(r.error); });
}

function openDb(name) {
  const r = indexedDB.open(name, 1);
  r.onupgradeneeded = () => { r.result.createObjectStore('kv'); };
  return req(r);
}

export const dbName = (account) => PREFIX + account;

export class IdbStore {
  constructor(db) { this.db = db; this.keepsMessages = false; this.wrap = null; }

  static async open(account) {
    const s = new IdbStore(await openDb(dbName(account)));
    s.wrap = await s._get('wrap');
    if (!s.wrap) {
      s.wrap = await subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']);
      await s._put('wrap', s.wrap);
    }
    return s;
  }

  _tx(mode) { return this.db.transaction('kv', mode).objectStore('kv'); }
  _get(key) { return req(this._tx('readonly').get(key)); }
  _put(key, value) { return req(this._tx('readwrite').put(value, key)); }

  async read(key) {
    const v = await this._get('v:' + key);
    if (!v) return null;
    const plain = await subtle.decrypt({ name: 'AES-GCM', iv: v.iv }, this.wrap, v.ct);
    return { data: JSON.parse(new TextDecoder().decode(plain), revive), keys: v.keys || null };
  }

  async write(key, value) {
    const iv = globalThis.crypto.getRandomValues(new Uint8Array(12));
    const ct = await subtle.encrypt({ name: 'AES-GCM', iv }, this.wrap, new TextEncoder().encode(JSON.stringify(value.data, replace)));
    await this._put('v:' + key, { iv, ct, keys: value.keys || null });
  }

  async delete(key) { await req(this._tx('readwrite').delete('v:' + key)); }

  async keys() {
    const all = await req(this._tx('readonly').getAllKeys());
    return all.filter((k) => typeof k === 'string' && k.startsWith('v:')).map((k) => k.slice(2));
  }

  close() { this.db.close(); }
}

// wipeAccount deletes an account's database: what signing out does.
export async function wipeAccount(account) {
  await new Promise((resolve) => {
    const r = indexedDB.deleteDatabase(dbName(account));
    r.onsuccess = r.onerror = r.onblocked = () => resolve();
  });
}

// wipeOthers deletes the databases of every other account: when a
// different account signs in on this browser, the old keys go first.
export async function wipeOthers(account) {
  if (!indexedDB.databases) return;
  for (const d of await indexedDB.databases()) {
    if (d.name && d.name.startsWith(PREFIX) && d.name !== dbName(account)) await wipeAccount(d.name.slice(PREFIX.length));
  }
}

// hasDatabase says whether this browser holds keys for an account.
export async function sealedDatabases() {
  if (!indexedDB.databases) return [];
  return (await indexedDB.databases()).map((d) => d.name).filter((n) => n && n.startsWith(PREFIX));
}
