// A Device is one browser profile (or, in tests, any device) playing its
// whole part in sealed chats: its two key pairs, the account's backup key
// once it is unlocked, the device lists it has pinned, and its sealed
// chats (chat.js). It follows the Go reference's Device
// (internal/sealed/ref/device.go) and the Dart port in the apps, method
// for method.
//
// It talks to nobody. Its methods return what to send to the server and
// take what the server gave back. Its state is saved through a store
// (store.js).

import { big, equal, ZERO_ID } from './bytes.js';
import { approvalCode, checkUnlockRequest, compareCode, compareHalf, encodeUnlockRequest, normalizeApprovalCode } from './unlock.js';
import { lockAccountRecord, lockBackupRecord, makeLockedCopy, openAccountRecord, openBackupRecord, openHandover, openLockedCopy, openPairingHandover, phraseOf, readPhrase, sealHandover, sealPairingHandover, sortWays, unlockKeyFromPhrase, unlockKeyFromPrf, verifyLockedCopy, WAY_DEVICE, WAY_NAMES, WAY_PASSKEY, WAY_PHRASE } from './backup.js';
import { checkFirstList, checkListAlone, checkListStep, deviceProof, encodeList, listDevice, listHash, signList, STEP_KEY_CHANGE, STEP_RESET } from './devicelist.js';
import { CodeError, ListError, LockedError, SealedError, UnlockError } from './errors.js';
import { BoxKey, SigningKey, x25519Public } from './prim.js';
import { SALT_SIZE } from './seal.js';
import { labelDeviceAttach, labelled } from './sign.js';
import { Chat } from './chat.js';

export const NOTICE_DEVICE_ADDED = 'device_added';
export const NOTICE_DEVICE_REMOVED = 'device_removed';
export const NOTICE_KEY_CHANGE = 'key_change';
export const NOTICE_RESET = 'reset';
export const NOTICE_LIST_REFUSED = 'list_refused';
export const NOTICE_OUT_OF_STEP = 'out_of_step';

// A list version as a number. A version beyond 2^53 cannot be told apart
// from its neighbours in a number and is refused; no honest account gets
// near it.
function versionOf(l) {
  if (typeof l.version !== 'number') throw new ListError(1, 'the version is too large');
  return l.version;
}

export class Pin {
  constructor(versions, lowest, newest, { lastReset = 0, verified = false, hold = false } = {}) {
    this.versions = versions; // Map version → { signed, list }
    this.lowest = lowest;
    this.newest = newest;
    this.lastReset = lastReset;
    this.verified = verified;
    this.hold = hold;
  }
  get newestList() { return this.versions.get(this.newest) || null; }
}

export class Device {
  constructor(o) {
    Object.assign(this, {
      account: o.account, server: o.server, id: o.id || ZERO_ID, platform: o.platform, addedAt: o.addedAt,
      sign: o.sign, box: o.box, random: o.random, now: o.now, trace: o.trace || null,
      fetchLists: null, store: null,
      backupKey: null, keyVersion: 0, accountKey: null, record: null,
      pins: new Map(), chats: new Map(), notices: [],
    });
  }

  // create makes a device and its two key pairs. With keys given ({ sign,
  // box }, from SigningKey.generate and BoxKey.generate) nothing is drawn;
  // otherwise they are made from `device_sign_seed` then
  // `device_box_private`, as the tests give them.
  static async create({ account, server, platform, random, now, id, trace, keys }) {
    let sign, box;
    if (keys) ({ sign, box } = keys);
    else {
      const draw = (what, n) => { const b = random.take(what, n); if (trace) trace(what, b); return b; };
      sign = await SigningKey.fromSeed(draw('device_sign_seed', 32), false);
      box = await BoxKey.fromBytes(draw('device_box_private', 32));
    }
    return new Device({ account, server, platform, random, now, id, trace, sign, box, addedAt: now() });
  }

  _random(what, n) {
    const b = this.random.take(what, n);
    if (this.trace) this.trace(what, b);
    return b;
  }

  get signPublic() { return this.sign.public; }
  get boxPublic() { return this.box.public; }
  get unlocked() { return this.backupKey != null; }

  async _entry() {
    return { deviceId: this.id, signKey: this.signPublic, boxKey: this.boxPublic, platform: this.platform, addedAt: this.addedAt, proof: await deviceProof(this.sign, this.account, this.id, this.boxPublic) };
  }

  // attachSignature proves the signing key when the same browser signs in
  // again (section 4.1).
  attachSignature(challenge) { return this.sign.signRaw(labelled(labelDeviceAttach, challenge)); }

  // listOf gives the record of one accepted version of one account's list.
  listOf(account, version) {
    const p = this.pins.get(account);
    const v = p && p.versions.get(Number(version));
    return v ? v.signed.list : null;
  }

  // learnLists takes versions of one account's device list and checks them
  // (4.4). With no pin, the newest one given is pinned (trust on first
  // use). Versions above the pin are accepted one step at a time, with a
  // notice for each change. Versions below what the device holds are
  // history: accepted only if they chain into it. A version already held
  // must be byte for byte the same. On a refusal the device keeps its pin
  // and shows "The server sent a device list that does not check out".
  async learnLists(forAccount, lists) {
    try { await this._learnLists(forAccount, lists); } catch (err) {
      if (err instanceof SealedError) this.notices.push({ kind: NOTICE_LIST_REFUSED, account: forAccount, platform: 0 });
      throw err;
    }
  }

  async _learnLists(forAccount, lists) {
    const given = [];
    for (const s of lists) {
      const l = await checkListAlone(s);
      if (l.accountId !== forAccount) throw new ListError(1, "the list is another account's");
      given.push([s, l, versionOf(l)]);
    }
    given.sort((a, b) => a[2] - b[2]);
    if (!given.length) return;
    let p = this.pins.get(forAccount);
    if (!p) {
      const [s, l, v] = given[given.length - 1];
      await checkFirstList(s);
      p = new Pin(new Map([[v, { signed: s, list: l }]]), v, v, { lastReset: l.reset === 1 ? v : 0 });
      this.pins.set(forAccount, p);
    }
    // Upwards from the pin.
    for (const [s, , v] of given) {
      const have = p.versions.get(v);
      if (have) {
        if (!equal(have.signed.list, s.list)) throw new ListError(1, 'the server sent a different list for a version already accepted');
        continue;
      }
      if (v < p.lowest) continue; // history, below
      if (v !== p.newest + 1) throw new ListError(1, 'a version is missing between the pin and this list');
      const prev = p.versions.get(p.newest);
      const [checked, step] = await checkListStep(prev.signed.list, s);
      p.versions.set(v, { signed: s, list: checked });
      p.newest = v;
      this._noteStep(forAccount, p, prev.list, checked, step);
    }
    // Downwards, for history: each older version must step into the one
    // above it.
    for (const [s, l, v] of [...given].reverse()) {
      if (v >= p.lowest) continue;
      if (v !== p.lowest - 1) throw new ListError(1, 'a version is missing below what this device holds');
      const above = p.versions.get(p.lowest);
      const [, step] = await checkListStep(s.list, above.signed);
      p.versions.set(v, { signed: s, list: l });
      p.lowest = v;
      if (step === STEP_RESET && above.list.version > p.lastReset) p.lastReset = above.list.version;
    }
  }

  _noteStep(forAccount, p, before, after, step) {
    if (step === STEP_RESET) {
      this.notices.push({ kind: NOTICE_RESET, account: forAccount, platform: 0 });
      p.lastReset = after.version;
      if (p.verified) { p.verified = false; p.hold = true; }
      return;
    }
    if (step === STEP_KEY_CHANGE) this.notices.push({ kind: NOTICE_KEY_CHANGE, account: forAccount, platform: 0 });
    for (const d of after.devices) if (!listDevice(before, d.deviceId)) this.notices.push({ kind: NOTICE_DEVICE_ADDED, account: forAccount, platform: d.platform });
    for (const d of before.devices) if (!listDevice(after, d.deviceId)) this.notices.push({ kind: NOTICE_DEVICE_REMOVED, account: forAccount, platform: 0 });
  }

  // The person acknowledging a reset notice, which lets the device send.
  acknowledge(forAccount) { const p = this.pins.get(forAccount); if (p) p.hold = false; }

  // The person marking another as verified after their codes matched.
  markVerified(forAccount) { const p = this.pins.get(forAccount); if (p) p.verified = true; }

  // compareCodeWith gives the 60 digits this device shows for its chat
  // with another account.
  async compareCodeWith(other, otherServer) {
    const mine = this.pins.get(this.account)?.newestList, theirs = this.pins.get(other)?.newestList;
    if (!mine || !theirs) throw new SealedError('no device list is pinned for one of the two accounts');
    return compareCode(await compareHalf(mine.list.accountKey, this.account, this.server), await compareHalf(theirs.list.accountKey, other, otherServer));
  }

  // _ensureList makes sure the device has checked one version of an
  // account's list, asking the server for versions if it must.
  async _ensureList(forAccount, version) {
    if (big(version) === 0n || this.listOf(forAccount, version)) return true;
    if (!this.fetchLists) return false;
    try {
      await this.learnLists(forAccount, await this.fetchLists(forAccount, 0));
    } catch (err) {
      if (err instanceof SealedError) return false;
      throw err;
    }
    return !!this.listOf(forAccount, version);
  }

  // adopt switches to the keys of a bundle the server has accepted, and
  // pins the device list that came with it.
  async adopt(b) {
    this.backupKey = b._backupKey;
    this.accountKey = b._accountKey;
    this.keyVersion = b.keyVersion;
    this.record = b._record;
    await this.learnLists(this.account, [b.list]);
  }

  async _phraseWay(entropy) {
    return { kind: WAY_PHRASE, id: ZERO_ID, unlockPublic: await x25519Public(await unlockKeyFromPhrase(entropy, this.account)) };
  }

  async _copiesFor(backupKey, accountKey, keyVersion, ways, devices) {
    const out = [];
    for (const w of ways) {
      const locked = await makeLockedCopy(w.unlockPublic, this.account, keyVersion, w.kind, w.id, backupKey, accountKey, this._random(`locked_copy_one_time:${WAY_NAMES[w.kind]}:${w.id}`, 32));
      out.push({ kind: w.kind, id: w.id, locked });
    }
    for (const d of devices) {
      const locked = await makeLockedCopy(d.boxKey, this.account, keyVersion, WAY_DEVICE, d.deviceId, backupKey, accountKey, this._random(`locked_copy_one_time:device:${d.deviceId}`, 32));
      out.push({ kind: WAY_DEVICE, id: d.deviceId, locked });
    }
    return out;
  }

  // createBackup is what the first device of an account does when it
  // finds the account has no backup (13.1): backup key, account key,
  // phrase, and version 1 of the device list holding itself.
  async createBackup() {
    const backupKey = this._random('backup_key', 32);
    const accountKey = await SigningKey.fromSeed(this._random('account_key_seed', 32));
    const entropy = this._random('phrase_entropy', 16);
    const rec = { accountId: this.account, keyVersion: 1, revision: 1, accountKey: accountKey.public, accountKeyPrivate: accountKey.seed, ways: [await this._phraseWay(entropy)], at: this.now() };
    return this._bundle(backupKey, accountKey, rec, await phraseOf(entropy), { version: 1, devices: [await this._entry()] }, []);
  }

  async _bundle(backupKey, accountKey, rec, phrase, lp, records) {
    const locked = await lockAccountRecord(backupKey, rec, this._random('account_record_salt', SALT_SIZE));
    // A device copy for every listed device but this one.
    const others = lp.devices.filter((d) => d.deviceId !== this.id);
    const copies = await this._copiesFor(backupKey, accountKey, rec.keyVersion, rec.ways, others);
    const list = await this._signedList(lp, rec.keyVersion, accountKey);
    return {
      keyVersion: rec.keyVersion, replaces: 0, revision: rec.revision, accountRecord: locked, copies, records,
      listVersion: lp.version, list, phrase, _backupKey: backupKey, _accountKey: accountKey, _record: rec,
    };
  }

  _signedList(lp, keyVersion, accountKey) {
    const devices = [...lp.devices].sort((a, b) => (a.deviceId < b.deviceId ? -1 : a.deviceId > b.deviceId ? 1 : 0));
    const l = { accountId: this.account, server: this.server, version: lp.version, prev: lp.prev || new Uint8Array(32), reset: lp.reset ? 1 : 0, keyVersion, accountKey: accountKey.public, devices, at: this.now() };
    return signList(encodeList(l), accountKey, lp.previous || null);
  }

  async _nextOwnList(change) {
    if (!this.unlocked) throw new LockedError();
    const cur = this.pins.get(this.account)?.newestList;
    if (!cur) throw new SealedError("the device has not pinned its own account's list");
    if (!equal(cur.list.accountKey, this.accountKey.public)) throw new SealedError("the device's account key is not the one in the list; it must unlock again");
    return this._signedList({ version: cur.list.version + 1, devices: await change([...cur.list.devices]), prev: await listHash(cur.signed.list) }, this.keyVersion, this.accountKey);
  }

  // listWithSelf is the next version of the account's list with this
  // device in it: what a device publishes when it has just been unlocked.
  listWithSelf() {
    return this._nextOwnList(async (devices) => (devices.some((d) => d.deviceId === this.id) ? devices : [...devices, await this._entry()]));
  }

  // listWithout is the next version without some devices: signing out, or
  // clearing a retired one away (7.3).
  listWithout(gone) {
    return this._nextOwnList(async (devices) => devices.filter((d) => !gone.includes(d.deviceId)));
  }

  // _adoptBackupKey makes the checks every way of unlocking ends with
  // (13.3, "Why the signature"); if they pass, the device is unlocked.
  async _adoptBackupKey(key, sb, copyKind, copyId, copyStored) {
    const r = this.record;
    if (r && (sb.keyVersion < r.keyVersion || (sb.keyVersion === r.keyVersion && big(sb.revision) < big(r.revision)))) {
      throw new UnlockError('the server gave an account record older than one this device has seen');
    }
    let rec;
    try { rec = await openAccountRecord(key, this.account, sb.keyVersion, sb.revision, sb.accountRecord); } catch (err) {
      if (err instanceof SealedError) throw new UnlockError('the account record does not open with that key');
      throw err;
    }
    if (copyStored && !(await verifyLockedCopy(rec.accountKey, this.account, sb.keyVersion, copyKind, copyId, copyStored))) {
      throw new UnlockError('the locked copy is not signed by the account key');
    }
    const pin = this.pins.get(this.account)?.newestList;
    if (pin && (!equal(pin.list.accountKey, rec.accountKey) || pin.list.keyVersion !== rec.keyVersion)) {
      throw new UnlockError("the account key is not the one in the account's device list");
    }
    this.backupKey = key;
    this.keyVersion = rec.keyVersion;
    this.accountKey = await SigningKey.fromSeed(rec.accountKeyPrivate);
    this.record = rec;
  }

  // unlockWithCopy opens a locked copy with an unlock key's private half.
  // sb is what the server gives: { keyVersion, revision, accountRecord }.
  async unlockWithCopy(unlockPrivate, kind, copyId, copyStored, sb) {
    let key;
    try { key = await openLockedCopy(unlockPrivate, this.account, sb.keyVersion, kind, copyId, copyStored); } catch (err) {
      if (err instanceof SealedError) throw new UnlockError('the locked copy does not open');
      throw err;
    }
    await this._adoptBackupKey(key, sb, kind, copyId, copyStored);
  }

  async unlockWithPhrase(typed, copyStored, sb) {
    return this.unlockWithCopy(await unlockKeyFromPhrase(await readPhrase(typed), this.account), WAY_PHRASE, ZERO_ID, copyStored, sb);
  }

  async unlockWithPrf(prfOutput, credentialId, passkey, copyStored, sb) {
    return this.unlockWithCopy(await unlockKeyFromPrf(prfOutput, this.account, credentialId), WAY_PASSKEY, passkey, copyStored, sb);
  }

  // unlockWithDeviceCopy unlocks with a copy sealed to this device's own
  // box key: the answer to its unlock request, a copy the unlock page made,
  // or its copy after a key replacement.
  unlockWithDeviceCopy(copyStored, sb) { return this.unlockWithCopy(this.box, WAY_DEVICE, this.id, copyStored, sb); }

  async unlockWithHandover(handover, codeChallenge, sb) {
    let key;
    try { key = await openHandover(this.box, this.account, codeChallenge, handover); } catch { throw new UnlockError('the hand-over does not open'); }
    await this._adoptBackupKey(key, sb, 0, ZERO_ID, null);
  }

  async unlockWithPairingHandover(handover, pairingSecret, sb) {
    let key;
    try { key = await openPairingHandover(this.box, pairingSecret, this.account, handover); } catch { throw new UnlockError('the hand-over does not open'); }
    await this._adoptBackupKey(key, sb, 0, ZERO_ID, null);
  }

  lockDevice() { this.backupKey = null; this.accountKey = null; this.record = null; this.keyVersion = 0; }

  handoverFor(appBoxKey, codeChallenge) {
    if (!this.unlocked) throw new LockedError();
    return sealHandover(appBoxKey, this.account, codeChallenge, this.backupKey, this._random('handover_one_time', 32));
  }

  pairingHandoverFor(appBoxKey, pairingSecret) {
    if (!this.unlocked) throw new LockedError();
    return sealPairingHandover(appBoxKey, pairingSecret, this.account, this.backupKey, this._random('pairing_one_time', 32), this._random('pairing_salt', SALT_SIZE));
  }

  // newUnlockRequest gives the request a new, locked device posts and the
  // code it shows: [request, code].
  async newUnlockRequest() {
    const request = encodeUnlockRequest({ accountId: this.account, deviceId: this.id, signKey: this.signPublic, boxKey: this.boxPublic, platform: this.platform, at: this.now(), proof: await deviceProof(this.sign, this.account, this.id, this.boxPublic) });
    return [request, await approvalCode(request)];
  }

  // answerUnlockRequest answers a request after the owner scanned or typed
  // the new device's code: only if the code computed from the request is
  // that code, with a locked copy for the new device's box key.
  async answerUnlockRequest(request, code) {
    if (!this.unlocked) throw new LockedError();
    const u = await checkUnlockRequest(request);
    if (u.accountId !== this.account) throw new SealedError('the request is for another account');
    if (normalizeApprovalCode(code) !== (await approvalCode(request))) throw new CodeError();
    return makeLockedCopy(u.boxKey, this.account, this.keyVersion, WAY_DEVICE, u.deviceId, this.backupKey, this.accountKey, this._random(`locked_copy_one_time:device:${u.deviceId}`, 32));
  }

  deviceCopyFor(device, boxKey) {
    if (!this.unlocked) throw new LockedError();
    return makeLockedCopy(boxKey, this.account, this.keyVersion, WAY_DEVICE, device, this.backupKey, this.accountKey, this._random(`locked_copy_one_time:device:${device}`, 32));
  }

  // approvePasskey is "Let the passkey unlock sealed chats?" answered yes
  // (13.4): the next revision of the account record and the passkey's copy.
  async approvePasskey(passkey, unlockPublic) {
    if (!this.unlocked) throw new LockedError();
    const rec = { ...this.record, ways: [...this.record.ways.filter((w) => !(w.kind === WAY_PASSKEY && w.id === passkey)), { kind: WAY_PASSKEY, id: passkey, unlockPublic }] };
    sortWays(rec.ways);
    rec.revision = Number(rec.revision) + 1;
    rec.at = this.now();
    const locked = await lockAccountRecord(this.backupKey, rec, this._random('account_record_salt', SALT_SIZE));
    const cp = await makeLockedCopy(unlockPublic, this.account, this.keyVersion, WAY_PASSKEY, passkey, this.backupKey, this.accountKey, this._random(`locked_copy_one_time:passkey:${passkey}`, 32));
    return { revision: rec.revision, accountRecord: locked, copy: { kind: WAY_PASSKEY, id: passkey, locked: cp }, _record: rec };
  }

  // removeWay is the next revision of the account record without a
  // passkey's way, for when the passkey is deleted (the replacement of
  // 13.6 follows).
  async removeWay(passkey) {
    if (!this.unlocked) throw new LockedError();
    const rec = { ...this.record, ways: this.record.ways.filter((w) => !(w.kind === WAY_PASSKEY && w.id === passkey)) };
    rec.revision = Number(rec.revision) + 1;
    rec.at = this.now();
    const locked = await lockAccountRecord(this.backupKey, rec, this._random('account_record_salt', SALT_SIZE));
    return { revision: rec.revision, accountRecord: locked, _record: rec };
  }

  adoptRecord(u) { this.record = u._record; }

  // replaceBackup replaces the backup key and account key together
  // (13.6). records are every backup record of the account; each is
  // opened and locked again. The ways come from the old account record,
  // never from the server. r: { devices, passkeys, newPhrase }.
  async replaceBackup(r, records) {
    if (!this.unlocked) throw new LockedError();
    const cur = this.pins.get(this.account)?.newestList;
    if (!cur) throw new SealedError("the device has not pinned its own account's list");
    const devicesGone = r.devices || [], passkeysGone = r.passkeys || [];
    const newBackupKey = this._random('backup_key', 32);
    const newAccountKey = await SigningKey.fromSeed(this._random('account_key_seed', 32));
    const rec = { accountId: this.account, keyVersion: this.keyVersion + 1, revision: Number(this.record.revision) + 1, accountKey: newAccountKey.public, accountKeyPrivate: newAccountKey.seed, ways: [], at: this.now() };
    let phrase = '';
    for (let w of this.record.ways) {
      const drop = w.kind === WAY_PASSKEY && passkeysGone.includes(w.id);
      if (w.kind === WAY_PHRASE && r.newPhrase) {
        const entropy = this._random('phrase_entropy', 16);
        w = await this._phraseWay(entropy);
        phrase = await phraseOf(entropy);
      }
      if (!drop) rec.ways.push(w);
    }
    sortWays(rec.ways);
    const relocked = [];
    for (const old of records) {
      let b;
      try { b = await openBackupRecord(this.backupKey, this.account, old.conversationId, old.keyEpoch, old.keyVersion, old.locked); } catch (err) {
        if (err instanceof SealedError) continue; // left by an earlier key and lost already
        throw err;
      }
      const locked = await lockBackupRecord(newBackupKey, this.account, rec.keyVersion, b, this._random(`backup_record_salt:${old.conversationId}:${old.keyEpoch}`, SALT_SIZE));
      relocked.push({ conversationId: old.conversationId, keyEpoch: old.keyEpoch, keyVersion: rec.keyVersion, locked });
    }
    const devices = cur.list.devices.filter((d) => !devicesGone.includes(d.deviceId));
    const b = await this._bundle(newBackupKey, newAccountKey, rec, phrase, { version: cur.list.version + 1, devices, prev: await listHash(cur.signed.list), previous: this.accountKey }, relocked);
    b.replaces = this.keyVersion;
    return b;
  }

  // resetBackup is what a device does when no way of unlocking is left
  // (13.8): new keys and phrase with the next key version, and a list
  // marked reset holding only itself.
  async resetBackup() {
    const cur = this.pins.get(this.account)?.newestList;
    if (!cur) throw new SealedError("the device has not pinned its own account's list");
    const newBackupKey = this._random('backup_key', 32);
    const newAccountKey = await SigningKey.fromSeed(this._random('account_key_seed', 32));
    const entropy = this._random('phrase_entropy', 16);
    const rec = { accountId: this.account, keyVersion: cur.list.keyVersion + 1, revision: 1, accountKey: newAccountKey.public, accountKeyPrivate: newAccountKey.seed, ways: [await this._phraseWay(entropy)], at: this.now() };
    const b = await this._bundle(newBackupKey, newAccountKey, rec, await phraseOf(entropy), { version: cur.list.version + 1, devices: [await this._entry()], prev: await listHash(cur.signed.list), reset: true }, []);
    b.replaces = cur.list.keyVersion;
    // Nothing the device held from before the reset is of use.
    for (const c of this.chats.values()) for (const e of c.epochs) { e.history = null; e.secret = null; }
    return b;
  }

  // chat gives the device's state for a sealed chat, made if needed.
  chat(conv, other) {
    let c = this.chats.get(conv);
    if (!c) { c = new Chat(this, conv, other); this.chats.set(conv, c); }
    return c;
  }
}

