// This browser as a sealed-chat device (docs/SEALED.md sections 13.3, 14):
// the one place where the library meets the server. It registers the
// browser's keys, unlocks it (passkey, recovery phrase, or approval from
// another device), makes the backup the first time, keeps device lists,
// epoch records and backup records in step, and seals and opens messages.
//
// It draws nothing; chat/sealed and the account page do. Everything that
// changes stored state runs inside one Web Lock per origin and account
// (tabs.js), on a copy of the device loaded afresh from IndexedDB, so two
// tabs never make an epoch or spend a counter at once. What a tab draws
// (the decrypted messages) lives only in that tab's memory.

import { api, get, post, put, del } from '../api.js';
import * as S from './index.js';
import { IdbStore, wipeAccount, wipeOthers } from './idb.js';
import { exclusive, channel } from './tabs.js';

const b64 = S.toB64;
const unb64 = (s) => S.fromB64(s) || new Uint8Array(0);

// The wire forms of what the library makes.
const signedListOf = (v) => ({ list: unb64(v.list), sig: unb64(v.sig), prevSig: v.prev_sig ? unb64(v.prev_sig) : null });
const listInput = (s) => { const l = S.decodeList(s.list); const o = { version: l.version, list: b64(s.list), sig: b64(s.sig) }; if (s.prevSig) o.prev_sig = b64(s.prevSig); return o; };
const copyInput = (c, keyVersion) => {
  const o = { kind: S.WAY_NAMES[c.kind], key_version: keyVersion, locked_key: b64(c.locked) };
  if (c.kind === S.WAY_PASSKEY) o.passkey_id = c.id;
  if (c.kind === S.WAY_DEVICE) o.device_id = c.id;
  return o;
};
const storedBackup = (b) => ({ keyVersion: b.key_version, revision: b.account_record.revision, accountRecord: unb64(b.account_record.record) });
const recordInput = (keyVersion, revision, record) => ({ key_version: keyVersion, revision, record: b64(record) });

// The view state of a chat, kept in the tab when the device is loaded
// again from storage.
const VIEW = ['seen', 'byDevice', 'messages', 'order', 'unchecked', 'sentUnder'];

export class Session {
  constructor(me, server, store) {
    this.me = me;
    this.server = server;
    this.store = store;
    this.device = null;
    this.rows = new Map(); // conversation → Map seq → row, as the server gave them
    this.listeners = new Set();
    this.lockName = 'churr-sealed-' + me.id;
    this.tabs = channel(this.lockName, () => this.reloadQuietly());
  }

  // open reads what this browser holds for the signed-in account. A
  // different account's keys are deleted first.
  static async open(me, server) {
    await wipeOthers(me.id);
    const s = new Session(me, server, await IdbStore.open(me.id));
    s.device = await s.load();
    return s;
  }

  on(fn) { this.listeners.add(fn); return () => this.listeners.delete(fn); }
  emit(what) { for (const fn of this.listeners) { try { fn(what); } catch (err) { console.error(err); } } }

  get state() { return !this.device ? 'none' : this.device.unlocked ? 'unlocked' : 'locked'; }

  async load() {
    const d = await S.loadDevice(this.store, { random: new S.SystemRandom(), now: () => Date.now() });
    if (d) this.wire(d);
    return d;
  }

  wire(d) {
    d.fetchLists = async (account, after) => this.fetchLists(account, after);
    return d;
  }

  async reloadQuietly() {
    const fresh = await this.load();
    if (fresh && this.device) this.transplant(this.device, fresh);
    this.device = fresh;
    this.emit('state');
  }

  transplant(from, to) {
    for (const [id, c] of from.chats) {
      const n = to.chats.get(id) || to.chat(id, c.other);
      for (const k of VIEW) n[k] = c[k];
    }
  }

  // locked runs fn(device) holding the lock every tab of this browser
  // shares, on the device as stored, and writes it back.
  locked(fn) {
    return exclusive(this.lockName, async () => {
      const fresh = await this.load();
      if (fresh) { if (this.device) this.transplant(this.device, fresh); this.device = fresh; }
      try { return await fn(this.device); } finally { if (this.device) await S.saveDevice(this.device); }
    });
  }

  announce() { this.tabs.post({ changed: true }); this.emit('state'); }

  // --- the device row ---

  // ensureDevice gives the browser its keys and its device row. The keys
  // are made by Web Crypto as non-extractable. A browser that held keys
  // for this account from a session that ended by itself proves its
  // signing key and carries on as the same device (section 4.1).
  async ensureDevice() {
    return this.locked(async (d) => {
      const devices = (await get('/me/devices')).devices || [];
      const current = devices.find((x) => x.current);
      if (d) {
        if (current && current.id === d.id) return d;
        if (!current) {
          try {
            const { challenge } = await post('/me/devices/reattach/begin', { device_id: d.id });
            await post('/me/devices/reattach/finish', { device_id: d.id, signature: b64(await d.attachSignature(unb64(challenge))) });
            return d;
          } catch { /* retired, or not ours any more: start again below */ }
        }
        await S.wipe(this.store);
        this.device = null;
      }
      const keys = { sign: await S.SigningKey.generate(), box: await S.BoxKey.generate() };
      const out = await put('/me/devices/current', { platform: 'web', name: current && current.name !== 'Device' ? current.name : browserName(), sign_key: b64(keys.sign.public), box_key: b64(keys.box.public) });
      const nd = await S.Device.create({ account: this.me.id, server: this.server.name, platform: S.PLATFORM_WEB, random: new S.SystemRandom(), now: () => Date.now(), id: out.device.id, keys });
      S.attachStore(nd, this.store);
      this.wire(nd);
      this.device = nd;
      return nd;
    }).finally(() => this.emit('state'));
  }

  // --- lists ---

  async fetchLists(account, after) {
    const q = after != null ? { after } : undefined;
    const { versions } = await get(`/accounts/${account}/device-lists`, q);
    return (versions || []).map(signedListOf);
  }

  // syncLists brings one account's pin up to date. A refused list keeps
  // the pin and leaves a notice.
  async syncLists(d, account) {
    const p = d.pins.get(account);
    let lists;
    try { lists = await this.fetchLists(account, p ? p.newest : null); } catch (err) { if (err.status === 404) return; throw err; }
    if (!lists.length) return;
    try { await d.learnLists(account, lists); } catch (err) { if (!(err instanceof S.SealedError)) throw err; }
  }

  // catchUpKeys follows a key replacement made on another device (13.6,
  // step 8): when the account's list carries a newer key version than the
  // one this browser holds, it opens the copy left for it with its box key.
  async catchUpKeys(d) {
    if (!d || !d.unlocked) return;
    await this.syncLists(d, d.account);
    const mine = d.pins.get(d.account)?.newestList;
    if (!mine || mine.list.keyVersion === d.keyVersion) return;
    const backup = await this.backup();
    const copy = backup && backup.locked_copies.find((c) => c.kind === 'device' && c.device_id === d.id && c.key_version === backup.key_version);
    if (!copy) { d.lockDevice(); return; } // left out: this browser was removed, or must unlock again
    await d.unlockWithDeviceCopy(unb64(copy.locked_key), storedBackup(backup));
  }

  async publishList(d, signed) {
    await post('/me/device-lists', listInput(signed));
    await d.learnLists(d.account, [signed]);
  }

  // --- the backup ---

  async backup() {
    try { return (await get('/me/backup')).backup; } catch (err) { if (err.status === 404) return null; throw err; }
  }

  // createBackup is what the first device of an account does (13.1). It
  // returns the twelve words, to be shown once.
  async createBackup() {
    await this.ensureDevice();
    const phrase = await this.locked(async (d) => {
      const b = await d.createBackup();
      await post('/me/backup', {
        account_record: recordInput(b.keyVersion, b.revision, b.accountRecord),
        locked_copies: b.copies.map((c) => copyInput(c, b.keyVersion)),
        device_list: listInput(b.list),
      });
      await d.adopt(b);
      return b.phrase;
    });
    this.announce();
    return phrase;
  }

  // unlockWith runs one way of unlocking and then joins (7.2).
  async unlockWith(fn) {
    await this.ensureDevice();
    const backup = await this.backup();
    if (!backup) throw new S.SealedError('This account has no sealed-chat backup yet.');
    await this.locked(async (d) => {
      await this.syncLists(d, d.account);
      await fn(d, backup);
    });
    await this.join();
    this.announce();
  }

  unlockWithPhrase(words) {
    return this.unlockWith(async (d, backup) => {
      const copy = backup.locked_copies.find((c) => c.kind === 'phrase' && c.key_version === backup.key_version);
      if (!copy) throw new S.SealedError('This account has no recovery phrase to unlock with.');
      await d.unlockWithPhrase(words, unb64(copy.locked_key), storedBackup(backup));
    });
  }

  // unlockWithPasskey asks for one touch of a passkey with the PRF
  // extension (13.3). The PRF output never leaves this page. Unlocking
  // with a passkey always passes through a page the server supplies, in
  // the apps as much as here (ruling R43).
  unlockWithPasskey() {
    return this.unlockWithPrf(touchPasskey);
  }

  // unlockWithPrf unlocks with a PRF output: from a touch made now
  // (touchPasskey) or from the touch that just unlocked the session (the
  // lock screen), given as a function returning { credentialId, prfOutput }.
  unlockWithPrf(touch) {
    return this.unlockWith(async (d, backup) => {
      const { credentialId, prfOutput } = await touch();
      const unlockPublic = b64(await S.x25519Public(await S.unlockKeyFromPrf(prfOutput, d.account, credentialId)));
      const passkeys = (await get('/me/passkeys')).passkeys || [];
      const pk = passkeys.find((p) => p.unlock_public === unlockPublic);
      const copy = pk && backup.locked_copies.find((c) => c.kind === 'passkey' && c.passkey_id === pk.id);
      if (!copy) throw new S.SealedError('That passkey has not been allowed to unlock sealed chats yet. A device that is already unlocked must allow it first. Until then, use the recovery phrase or another device.');
      await d.unlockWithPrf(prfOutput, credentialId, pk.id, unb64(copy.locked_key), storedBackup(backup));
    });
  }

  // lockForSessionLock is what locking Churr does to sealed chats (the
  // lock, docs/AUTH.md section 10.6): it leaves on the server a copy of
  // the backup key sealed to this browser's own box key, if there is none
  // for the current key version, and then forgets the backup key and the
  // account key, here and in storage, so the browser is a locked device.
  // The server gives the copy back only to an unlocked session, and the
  // box key opens it, so the touch that unlocks the session unlocks the
  // sealed chats too, PRF or not. If the copy cannot be left (no backup
  // yet, or no network), the keys are kept in storage: nothing sealed is
  // lost by locking.
  async lockForSessionLock() {
    if (this.state !== 'unlocked') return;
    await this.locked(async (d) => {
      if (!d || !d.unlocked) return;
      const backup = await this.backup();
      if (!backup || backup.key_version !== d.keyVersion) return;
      const have = backup.locked_copies.find((c) => c.kind === 'device' && c.device_id === d.id && c.key_version === backup.key_version);
      if (!have) {
        const copy = await d.deviceCopyFor(d.id, d.boxPublic);
        await post('/me/backup/copies', { locked_copy: copyInput({ kind: S.WAY_DEVICE, id: d.id, locked: copy }, d.keyVersion) });
      }
      d.lockDevice();
    });
    this.rows.clear();
    this.announce();
  }

  // hasDeviceCopy says whether the backup holds a copy for this browser's
  // box key at the current key version: what locking left.
  async hasDeviceCopy() {
    const d = this.device;
    const backup = d && await this.backup();
    return !!(backup && backup.locked_copies.some((c) => c.kind === 'device' && c.device_id === d.id && c.key_version === backup.key_version));
  }

  // requestApproval posts an unlock request (13.7) and returns its id and
  // the 20-character code this browser shows.
  async requestApproval() {
    const d = await this.ensureDevice();
    const [request, code] = await d.newUnlockRequest();
    const { unlock_request: u } = await post('/me/unlock-requests', { request: b64(request) });
    return { id: u.id, code };
  }

  // finishApproval unlocks with the answer, once there is one. It returns
  // false while there is none yet.
  async finishApproval(id) {
    const { unlock_request: u } = await get('/me/unlock-requests/' + id);
    if (!u.answer) return false;
    await this.unlockWith((d, backup) => d.unlockWithDeviceCopy(unb64(u.answer), storedBackup(backup)));
    return true;
  }

  // unlockWithDeviceCopy unlocks with a copy sealed to this browser's box
  // key: one the unlock page made, or one left by a key replacement.
  unlockWithDeviceCopy() {
    return this.unlockWith(async (d, backup) => {
      const copy = backup.locked_copies.find((c) => c.kind === 'device' && c.device_id === d.id && c.key_version === backup.key_version);
      if (!copy) throw new S.SealedError('No copy of the key has been left for this browser.');
      await d.unlockWithDeviceCopy(unb64(copy.locked_key), storedBackup(backup));
    });
  }

  // answerRequest approves another device of this account after the
  // person typed or scanned the code it shows.
  async answerRequest(u, typedCode) {
    await this.locked(async (d) => {
      const cp = await d.answerUnlockRequest(unb64(u.request), typedCode);
      const dev = S.decodeUnlockRequest(unb64(u.request));
      await put(`/me/unlock-requests/${u.id}/answer`, { locked_copy: copyInput({ kind: S.WAY_DEVICE, id: dev.deviceId, locked: cp }, d.keyVersion) });
    });
  }

  // allowPasskey: "Let the passkey unlock sealed chats?" answered yes
  // (13.4). touchFirst gives the passkey's unlock key when the server has
  // none yet; that needs a touch of that very passkey.
  async allowPasskey(pk) {
    let unlockPublic = pk.unlock_public ? unb64(pk.unlock_public) : null;
    if (!unlockPublic) {
      const given = await this.giveUnlockKey();
      if (given.id !== pk.id) throw new S.SealedError(`That was the passkey “${given.name}”. Its unlock key is given; allow it from its own row.`);
      unlockPublic = unb64(given.unlock_public);
    }
    await this.locked(async (d) => {
      const up = await d.approvePasskey(pk.id, unlockPublic);
      await post('/me/backup/copies', { locked_copy: copyInput(up.copy, d.keyVersion), account_record: recordInput(d.keyVersion, up.revision, up.accountRecord) });
      d.adoptRecord(up);
    });
  }

  // giveUnlockKey is step 1 of section 13.4 for a passkey that supports
  // PRF but whose unlock key the server does not have yet: one touch, and
  // the public half of the unlock key goes to the server. The server's
  // answer to a check does not say which passkey was touched, so the
  // passkey is the one whose last use moved.
  async giveUnlockKey() {
    const before = new Map(((await get('/me/passkeys')).passkeys || []).map((p) => [p.id, p.last_used_at || '']));
    const { credentialId, prfOutput } = await touchPasskey();
    const after = (await get('/me/passkeys')).passkeys || [];
    const touched = after.filter((p) => (p.last_used_at || '') !== before.get(p.id));
    if (touched.length !== 1) throw new S.SealedError('It is not clear which passkey was touched. Try again.');
    const unlockPublic = await S.x25519Public(await S.unlockKeyFromPrf(prfOutput, this.me.id, credentialId));
    return (await put(`/me/passkeys/${touched[0].id}/unlock-key`, { unlock_public: b64(unlockPublic) })).passkey;
  }

  // replaceKeys replaces the backup key and account key (13.6): after a
  // device is revoked, the phrase is replaced, or because the person asked.
  // It returns new words when the phrase was replaced.
  async replaceKeys(r) {
    const phrase = await this.locked(async (d) => {
      await this.syncLists(d, d.account);
      const records = [];
      for (const x of await allPages('/me/backup/records', 'records')) records.push({ conversationId: x.conversation_id, keyEpoch: x.key_epoch, keyVersion: x.key_version, locked: unb64(x.record) });
      const b = await d.replaceBackup(r, records);
      await post('/me/backup/replace', {
        replaces_key_version: b.replaces,
        account_record: recordInput(b.keyVersion, b.revision, b.accountRecord),
        locked_copies: b.copies.map((c) => copyInput(c, b.keyVersion)),
        records: b.records.map((x) => ({ conversation_id: x.conversationId, key_epoch: x.keyEpoch, key_version: x.keyVersion, record: b64(x.locked) })),
        device_list: listInput(b.list),
      });
      await d.adopt(b);
      return b.phrase;
    });
    await this.newEpochsEverywhere();
    this.announce();
    return phrase;
  }

  // revokeDevice removes another device of this account ("lost phone",
  // 7.3): its session ends, it leaves the list, and the keys it held are
  // replaced.
  async revokeDevice(deviceId) {
    await del('/me/devices/' + deviceId);
    if (this.state === 'unlocked') await this.replaceKeys({ devices: [deviceId] });
  }

  // --- joining and chats ---

  // sealedChats lists the account's sealed chats: the chat page's own
  // list when it has one, otherwise asked of the server.
  async sealedChats() {
    if (this.conversations) return this.conversations();
    return (await allPages('/conversations', 'conversations')).filter((c) => c.kind === 'sealed');
  }

  // join is what a device does once unlocked (7.2): it lists itself, reads
  // the backup records of every sealed chat, and makes an epoch with
  // itself in it.
  async join() {
    await this.locked(async (d) => {
      await this.syncLists(d, d.account);
      const mine = d.pins.get(d.account)?.newestList;
      if (mine && !S.listDevice(mine.list, d.id)) await this.publishList(d, await d.listWithSelf());
    });
    await this.newEpochsEverywhere(true);
  }

  async newEpochsEverywhere(readBackup = false) {
    for (const c of await this.sealedChats()) {
      const other = (c.member_ids || []).find((x) => x !== this.me.id) || this.device?.chats.get(c.id)?.other;
      if (!other) continue;
      try { await this.sync(c.id, other, { readBackup, ours: true }); } catch (err) { quiet(err); }
    }
  }

  async fetchRows(conv) {
    let rows = this.rows.get(conv);
    if (!rows) { rows = new Map(); this.rows.set(conv, rows); }
    let after = rows.size ? Math.max(...rows.keys()) : 0;
    for (;;) {
      const page = await get(`/conversations/${conv}/messages`, { after, limit: 200 });
      for (const m of page.messages) { rows.set(m.seq, m); after = Math.max(after, m.seq); }
      if (!page.has_more || !page.messages.length) break;
    }
    return rows;
  }

  async fetchEpochs(c) {
    const from = c.epochs.length ? c.epochs[c.epochs.length - 1].keyEpoch + 1 : 1;
    const out = [];
    let at = from;
    for (;;) {
      const page = await get(`/conversations/${c.id}/sealed/epochs`, { from: at, limit: 200 });
      out.push(...page.epochs);
      if (!page.has_more || !page.epochs.length) break;
      at = page.epochs[page.epochs.length - 1].key_epoch + 1;
    }
    return out;
  }

  // sync brings one sealed chat up to date: both accounts' lists, the
  // epoch records, the backup records, and every message the server holds,
  // drawn into the chat's view. It makes the epoch section 7.1 asks for
  // when the device lists changed or the current record was refused.
  async sync(conv, other, opts = {}) {
    if (this.state !== 'unlocked' || !other) return null;
    const out = await this.locked(async (d) => {
      const c = d.chat(conv, other);
      await this.catchUpKeys(d); // brings the account's own list up to date
      if (!d.unlocked) return null;
      await this.syncLists(d, other);
      for (const e of await this.fetchEpochs(c)) {
        try { await c.acceptEpoch(e.key_epoch, e.seq, unb64(e.record)); } catch (err) { if (!(err instanceof S.SealedError)) throw err; }
      }
      if (opts.readBackup) await this.readBackup(c);
      await this.drawRows(c, await this.fetchRows(conv));
      await this.uploadSecrets(c);
      return { reason: c.needsEpoch(), shareDue: c.shareDue };
    });
    if (!out) return null;
    if (out.reason === 'device_list' || out.reason === 'refused') {
      // A device that did not make the change waits a moment for the one
      // that did (7.1, step 3).
      if (!opts.ours) await new Promise((r) => setTimeout(r, Math.random() * 2000));
      await this.makeEpochIfNeeded(conv, other, ['device_list', 'refused']);
    }
    if (out.shareDue) await this.shareHistory(conv, other);
    this.emit({ chat: conv });
    return this.device.chats.get(conv);
  }

  async readBackup(c) {
    for (const x of await allPages('/me/backup/records', 'records', { conversation_id: c.id })) {
      const e = c.epoch(x.key_epoch);
      if (!e || e.history) continue;
      try { await c.readBackupRecord({ conversationId: x.conversation_id, keyEpoch: x.key_epoch, keyVersion: x.key_version, locked: unb64(x.record) }); } catch (err) { if (!(err instanceof S.SealedError)) throw err; }
    }
  }

  // drawRows takes every row and epoch record in sequence order. Rows the
  // device cannot read yet are tried again on the next pass.
  async drawRows(c, rows) {
    for (const m of [...rows.values()].sort((a, b) => a.seq - b.seq)) {
      if (!m.sealed || !m.sealed.ciphertext) continue;
      const ct = unb64(m.sealed.ciphertext);
      try {
        if (ct.length >= S.HEADER_SIZE) { const h = S.decodeHeader(ct); if (c.seen.get(`${h.deviceId}/${h.counter}`) === m.seq) continue; }
      } catch { /* refused below */ }
      try {
        const r = await c.receive({ seq: m.seq, author: m.author_id, keyEpoch: m.sealed.key_epoch, quiet: !!m.quiet, ciphertext: ct });
        const shown = c.messages.get(r.header.reference);
        if (shown && shown.seq === m.seq) { shown.at = m.created_at; shown.rowId = m.id; }
      } catch (err) { if (!(err instanceof S.SealedError)) throw err; }
    }
  }

  // A record locked under a backup key another device has just replaced is
  // refused (400, field key_version). The secret is then kept for the next
  // pass: after catching up it is locked again under the new key, and a
  // device left out of the replacement is locked and uploads nothing.
  async uploadSecrets(c, again = true) {
    const records = await c.takeNewSecrets();
    for (let i = 0; i < records.length; i++) {
      const r = records[i];
      try {
        await put(`/me/backup/records/${r.conversationId}/${r.keyEpoch}`, { key_version: r.keyVersion, record: b64(r.locked) });
      } catch (err) {
        for (const x of records.slice(i)) if (!c.newSecrets.includes(x.keyEpoch)) c.newSecrets.push(x.keyEpoch);
        if (!(err && err.status === 400 && err.field === 'key_version')) throw err;
        await this.catchUpKeys(c.d);
        if (again && c.d.unlocked && c.d.keyVersion !== r.keyVersion) await this.uploadSecrets(c, false);
        return;
      }
    }
  }

  // makeEpochIfNeeded makes and posts the chat's next epoch when one of
  // reasons applies, and takes the record the server stores, or the one
  // that beat it.
  async makeEpochIfNeeded(conv, other, reasons) {
    return this.locked(async (d) => {
      await this.catchUpKeys(d);
      if (!d.unlocked) return false;
      const c = d.chat(conv, other);
      await this.syncLists(d, other);
      for (const e of await this.fetchEpochs(c)) {
        try { await c.acceptEpoch(e.key_epoch, e.seq, unb64(e.record)); } catch (err) { if (!(err instanceof S.SealedError)) throw err; }
      }
      const reason = c.needsEpoch();
      if (!reason || !reasons.includes(reason)) return false;
      const mine = d.pins.get(d.account)?.newestList;
      if (!mine || !S.listDevice(mine.list, d.id)) return false;
      const [stored, keyEpoch] = await c.makeEpoch();
      let epoch;
      try {
        epoch = (await post(`/conversations/${conv}/sealed/epochs`, { key_epoch: keyEpoch, record: b64(stored) })).epoch;
      } catch (err) {
        if (err.code !== 'conflict') throw err;
        // Another device's record holds this key epoch: check and use it.
        for (const e of await this.fetchEpochs(c)) {
          try { await c.acceptEpoch(e.key_epoch, e.seq, unb64(e.record)); } catch (x) { if (!(x instanceof S.SealedError)) throw x; }
        }
        return false;
      }
      try { await c.acceptEpoch(epoch.key_epoch, epoch.seq, unb64(epoch.record)); } catch (err) { if (!(err instanceof S.SealedError)) throw err; }
      await this.uploadSecrets(c);
      return true;
    });
  }

  async shareHistory(conv, other) {
    const content = await this.locked(async (d) => d.chat(conv, other).historyShare());
    if (content) await this.send(conv, other, content);
  }

  // epochFor is what a new sealed chat needs: epoch record 1 for a
  // conversation id this browser makes, a UUIDv7 (openSealedChat).
  async startChat(otherId) {
    await this.locked(async (d) => {
      await this.syncLists(d, d.account);
      await this.syncLists(d, otherId);
    });
    const conv = uuidv7();
    const stored = await this.locked(async (d) => {
      const c = d.chat(conv, otherId);
      const [st] = await c.makeEpoch();
      return st;
    });
    const res = await api('POST', '/sealed', { account_id: otherId, epoch_record: b64(stored) });
    const id = res.conversation.id;
    if (id !== conv) {
      // The chat already existed, or the other person's record won.
      await this.locked(async (d) => { d.chats.delete(conv); });
    }
    await this.sync(id, otherId, { readBackup: true, ours: true });
    return res.conversation;
  }

  // send seals content and posts it, making the epoch section 7.1 asks for
  // first. A message refused as stale is sealed again in the new epoch.
  async send(conv, other, content, fileIds = []) {
    for (let tries = 0; tries < 3; tries++) {
      await this.makeEpochIfNeeded(conv, other, ['device_list', 'refused', 'age', 'clock']);
      const out = await this.locked(async (d) => d.chat(conv, other).seal(content));
      try {
        const res = await post(`/conversations/${conv}/messages`, { client_id: out.clientId, sealed: { key_epoch: out.keyEpoch, ciphertext: b64(out.ciphertext) }, quiet: out.quiet, file_ids: fileIds.length ? fileIds : undefined });
        const m = res.message;
        this.rows.get(conv)?.set(m.seq, m);
        await this.locked(async (d) => this.drawRows(d.chat(conv, other), new Map([[m.seq, m]])));
        this.emit({ chat: conv });
        return out.clientId;
      } catch (err) {
        if (err.code !== 'conflict') throw err;
        await this.locked(async (d) => d.chat(conv, other).unsend(out));
        await this.sync(conv, other, { ours: true });
      }
    }
    throw new S.SealedError('The message could not be sent: the chat\'s keys kept changing.');
  }

  // callFrameKeys gives the call sender keys of the chat's current epoch
  // (section 10): { keyEpoch, keyIndex, deviceId, keys: Map device → 32
  // bytes }. The calls code asks for it again when the epoch changes.
  async callFrameKeys(conv, other, callId) {
    if (this.state !== 'unlocked') throw new S.LockedError();
    const c = this.device.chat(conv, other);
    const k = await c.callKeys(callId);
    return { ...k, deviceId: this.device.id };
  }

  // compareCode gives the 60 digits for the chat with other.
  async compareCode(other) {
    const theirs = this.device.pins.get(other)?.newestList;
    if (!theirs) throw new S.SealedError('No device list is pinned for that person yet.');
    return this.device.compareCodeWith(other, theirs.list.server);
  }

  async markVerified(other) { await this.locked(async (d) => d.markVerified(other)); this.emit('state'); }
  async acknowledge(other) { await this.locked(async (d) => d.acknowledge(other)); this.emit('state'); }

  // signOut takes this browser out of its device list, as its last act,
  // and deletes its keys. If the server cannot be reached it wipes anyway;
  // the stale entry is cleaned up by the account's other devices.
  async signOut() {
    try {
      if (this.state === 'unlocked') {
        await this.locked(async (d) => {
          await this.catchUpKeys(d);
          if (!d.unlocked) return;
          const mine = d.pins.get(d.account)?.newestList;
          if (mine && S.listDevice(mine.list, d.id)) await post('/me/device-lists', listInput(await d.listWithout([d.id])));
        });
      }
    } catch (err) { quiet(err); }
    await this.wipe();
  }

  async wipe() {
    this.tabs.post({ changed: true });
    this.store.close();
    await wipeAccount(this.me.id);
    this.device = null;
    this.emit('state');
  }
}

// signOutSealed is what the pages' sign-out does before the session is
// revoked: if this browser holds sealed-chat keys for the account, it
// leaves the device list and deletes them.
export async function signOutSealed(me, server) {
  try {
    const s = await Session.open(me, server);
    await s.signOut();
  } catch (err) {
    console.error(err);
    await wipeAccount(me.id);
  }
}

// touchPasskey runs one passkey check with the PRF extension and returns
// the credential's identifier and its PRF output. The check is sent to the
// server without the PRF output.
export async function touchPasskey() {
  const begin = await post('/checks/passkey/begin');
  const opts = structuredClone(begin.public_key);
  opts.challenge = unb64(opts.challenge);
  for (const c of opts.allowCredentials || []) c.id = unb64(c.id);
  opts.extensions = { ...(opts.extensions || {}), prf: { eval: { first: await S.prfInput() } } };
  const cred = await navigator.credentials.get({ publicKey: opts });
  const ext = cred.getClientExtensionResults ? cred.getClientExtensionResults() : {};
  const first = ext.prf && ext.prf.results && ext.prf.results.first;
  await post('/checks/passkey/finish', {
    ceremony_id: begin.ceremony_id,
    credential: {
      id: cred.id, rawId: b64(new Uint8Array(cred.rawId)), type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment || undefined,
      clientExtensionResults: {},
      response: {
        clientDataJSON: b64(new Uint8Array(cred.response.clientDataJSON)),
        authenticatorData: b64(new Uint8Array(cred.response.authenticatorData)),
        signature: b64(new Uint8Array(cred.response.signature)),
        userHandle: cred.response.userHandle ? b64(new Uint8Array(cred.response.userHandle)) : undefined,
      },
    },
  });
  if (!first) throw new S.SealedError('That passkey cannot unlock sealed chats: its passkey store gave no unlocking secret. Use the recovery phrase, or another device.');
  return { credentialId: new Uint8Array(cred.rawId), prfOutput: new Uint8Array(first) };
}

// quiet reports a failure that the next sync or event tries again: a busy
// server or a lost connection is a warning, anything else an error.
function quiet(err) {
  if (err && (err.code === 'rate_limited' || err.code === 'network')) console.warn(err.message); else console.error(err);
}

async function allPages(path, key, q = {}) {
  const out = [];
  let cursor;
  do {
    const page = await get(path, { limit: 200, ...q, cursor });
    out.push(...(page[key] || []));
    cursor = page.next_cursor;
  } while (cursor && out.length < 100000);
  return out;
}

// A UUIDv7: 48 bits of milliseconds, the version, and random bits.
export function uuidv7() {
  const b = S.randomBytes(16);
  const ms = BigInt(Date.now());
  for (let i = 0; i < 6; i++) b[i] = Number((ms >> BigInt(8 * (5 - i))) & 0xffn);
  b[6] = (b[6] & 0x0f) | 0x70;
  b[8] = (b[8] & 0x3f) | 0x80;
  return S.idOf(b);
}

function browserName() {
  const ua = navigator.userAgent;
  const name = /Firefox\//.test(ua) ? 'Firefox' : /Edg\//.test(ua) ? 'Edge' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'A browser';
  return name + ' (web chat)';
}
