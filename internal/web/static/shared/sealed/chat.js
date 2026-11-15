// A Chat is one sealed chat as one device holds it: the chain of epoch
// records, the secrets it has for them, its send counter, and what it
// shows. It follows internal/sealed/ref/chat.go and the Dart port.

import { big, equal, fromB64, idOf, isId, sameNumber } from './bytes.js';
import { historyContent, KIND_DELETE, KIND_EDIT, KIND_HISTORY, KIND_MESSAGE, KIND_REACTION, KIND_TASK } from './content.js';
import { callKeyIndex, callSecret, callSenderKey, checkEpochRecord, checkHistorySecret, contentKey, decodeEpoch, epochHash, epochHasAccount, epochRecipient, epochRef, historySecret, makeEpoch, senderKey, splitStoredEpoch } from './epoch.js';
import { BackupRecordError, ChainGapError, EpochError, HeldError, LockedError, MessageError, NeedsEpochError, NoKeyError, RecordError, SealedError, SecondRecordError } from './errors.js';
import { lockBackupRecord, openBackupRecord } from './backup.js';
import { decodeHeader, FLAG_QUIET, openMessage, sealMessage } from './message.js';
import { SALT_SIZE } from './seal.js';
import { decodeList } from './devicelist.js';

// The seven-day rule, and how far ahead of the device's clock a record's
// time may be.
export const EPOCH_MAX_AGE = 7 * 24 * 60 * 60 * 1000;
export const EPOCH_MAX_FUTURE = 5 * 60 * 1000;

const strictJson = (bytes) => JSON.parse(new TextDecoder().decode(bytes));

// One key epoch as a device holds it.
export class Epoch {
  constructor(keyEpoch, seq, stored) {
    this.keyEpoch = keyEpoch;
    this.seq = seq;
    this.stored = stored; // the record with its signature, exactly as received
    this.record = null;
    this.accepted = false;
    this.refusal = null;
    this.devices = new Map(); // recipient devices with their keys, from the lists named
    this.secret = null; // the epoch secret, while the epoch is current
    this.history = null; // the history secret
  }
}

export class Chat {
  constructor(device, id, other) {
    this.d = device;
    this.id = id;
    this.other = other;
    this.epochs = []; // in order, refused ones included
    this.counter = 0; // the counter of the last message this device sent here
    this.shareDue = false;
    this.seen = new Map(); // "device/counter" → seq
    this.byDevice = new Map(); // device → [{ counter, seq }] in seq order
    this.messages = new Map(); // reference → shown message
    this.order = []; // references by seq
    this.unchecked = 0;
    this.newSecrets = []; // key epochs whose history secret is not yet in the backup
    this.sentUnder = new Map(); // key epoch → [[reference, content]]
  }

  hasSeen(device, counter) { return this.seen.has(`${device}/${counter}`); }
  get current() { return this.epochs.length ? this.epochs[this.epochs.length - 1] : null; }
  epoch(keyEpoch) { return this.epochs.find((e) => sameNumber(e.keyEpoch, keyEpoch)) || null; }

  // needsEpoch says whether this device must make a new epoch before it
  // sends (7.1), and why: first, refused, device_list, age or clock. Null
  // if not.
  needsEpoch() {
    const cur = this.current;
    if (!cur) return 'first';
    if (!cur.accepted) return 'refused';
    for (const a of cur.record.accounts) {
      const p = this.d.pins.get(a.accountId);
      if (p && big(p.newest) > big(a.listVersion)) return 'device_list';
    }
    if (!epochRecipient(cur.record, this.d.id)) return 'device_list';
    const now = this.d.now();
    if (cur.record.at + EPOCH_MAX_AGE < now) return 'age';
    if (cur.record.at > now + EPOCH_MAX_FUTURE) return 'clock';
    return null;
  }

  // makeEpoch builds the chat's next epoch record from the newest lists
  // the device has checked for both accounts (5.3). The record joins the
  // chain only once the server has stored it and it comes back through
  // acceptEpoch. Returns [stored, keyEpoch].
  async makeEpoch() {
    const d = this.d;
    if (!d.unlocked) throw new LockedError();
    let keyEpoch = 1, prev = null;
    const cur = this.current;
    if (cur) { keyEpoch = cur.keyEpoch + 1; prev = await epochHash(cur.stored); }
    const lists = [d.account, this.other].map((acc) => ({ accountId: acc, list: d.pins.get(acc)?.newestList?.signed.list || null }));
    const mine = d.pins.get(d.account)?.newestList;
    if (!mine) throw new SealedError("the device's account has no device list");
    if (!mine.list.devices.some((x) => x.deviceId === d.id)) throw new SealedError("the device is not in its account's device list");
    const secret = d._random(`epoch_secret:${keyEpoch}`, 32);
    const stored = await makeEpoch({
      conversationId: this.id, keyEpoch, prev, makerAccountId: d.account, makerDeviceId: d.id, makerSign: d.sign,
      lists, secret, oneTime: (device) => d._random(`epoch_seal_one_time:${keyEpoch}:${device}`, 32), at: d.now(),
    });
    return [stored, keyEpoch];
  }

  // acceptEpoch takes one epoch record from the server, with the key epoch
  // and sequence number it was filed under, and checks it (5.4). A record
  // that passes becomes current. One that fails stays in the chain as
  // refused, the device shows "This chat's keys are out of step", and the
  // EpochError is thrown; needsEpoch then asks for a repair.
  async acceptEpoch(keyEpoch, seq, stored) {
    const d = this.d;
    const have = this.epoch(keyEpoch);
    if (have) {
      if (equal(have.stored, stored)) return;
      throw new SecondRecordError();
    }
    const prev = this.current;
    if (prev && !sameNumber(keyEpoch, big(prev.keyEpoch) + 1n)) throw new ChainGapError();
    const e = new Epoch(keyEpoch, seq, new Uint8Array(stored));
    // The lists the record names are fetched before it is checked.
    try {
      const parsed = decodeEpoch(splitStoredEpoch(e.stored)[0]);
      e.record = parsed;
      for (const a of parsed.accounts) await d._ensureList(a.accountId, a.listVersion);
    } catch (err) {
      if (!(err instanceof RecordError)) throw err; // rule 1 refuses it below
    }
    let err = null, ok = null;
    try {
      ok = await checkEpochRecord({ stored: e.stored, conversationId: this.id, accounts: [d.account, this.other], previous: prev ? prev.stored : null, listOf: (a, v) => d.listOf(a, v), deviceId: d.id, box: d.box });
      if (!sameNumber(ok.record.keyEpoch, keyEpoch)) err = new EpochError(2, 'the record is not for the key epoch the server filed it under');
    } catch (x) {
      if (!(x instanceof EpochError)) throw x;
      err = x;
    }
    this.epochs.push(e);
    if (err) {
      e.refusal = err;
      d.notices.push({ kind: 'out_of_step', account: this.other, platform: 0, conversation: this.id });
      throw err;
    }
    e.accepted = true;
    e.record = ok.record;
    e.devices = ok.devices;
    if (ok.secret) {
      e.secret = ok.secret;
      e.history = await historySecret(ok.secret, this.id, keyEpoch);
      this.newSecrets.push(keyEpoch);
    }
    if (prev) {
      // The previous epoch secret is discarded; its history secret stays.
      prev.secret = null;
      if (prev.accepted && epochHasAccount(e.record, this.other) && !epochHasAccount(prev.record, this.other)) this.shareDue = true;
    }
  }

  // takeNewSecrets gives the backup records to upload for history secrets
  // just learned, one per key epoch, locked under the backup key (13.2).
  async takeNewSecrets() {
    const d = this.d;
    if (!d.unlocked) throw new LockedError();
    const out = [];
    for (const keyEpoch of this.newSecrets) {
      const e = this.epoch(keyEpoch);
      if (!e || !e.history) continue;
      const locked = await lockBackupRecord(d.backupKey, d.account, d.keyVersion, { conversationId: this.id, keyEpoch, secret: e.history, at: d.now() }, d._random(`backup_record_salt:${this.id}:${keyEpoch}`, SALT_SIZE));
      out.push({ conversationId: this.id, keyEpoch, keyVersion: d.keyVersion, locked });
    }
    this.newSecrets = [];
    return out;
  }

  // readBackupRecord opens one of the account's backup records for this
  // chat and keeps its history secret if it matches the epoch record's
  // `check` (13.2). r: { conversationId, keyEpoch, keyVersion, locked }.
  async readBackupRecord(r) {
    const d = this.d;
    if (!d.unlocked) throw new LockedError();
    const e = this.epoch(r.keyEpoch);
    if (!e || !e.accepted || r.conversationId !== this.id) throw new BackupRecordError();
    let b;
    try { b = await openBackupRecord(d.backupKey, d.account, this.id, r.keyEpoch, r.keyVersion, r.locked); } catch (err) {
      if (err instanceof SealedError) throw new BackupRecordError();
      throw err;
    }
    if (!(await checkHistorySecret(b.secret, e.record))) throw new BackupRecordError();
    e.history = b.secret;
  }

  // seal encrypts and signs one message in the current epoch. Every kind
  // but `message` is sent quiet. The raised counter is saved before the
  // message is returned, so a reload cannot reuse it. Returns { ciphertext,
  // clientId, keyEpoch, quiet, counter }.
  seal(content) { return this._seal(content, null); }

  async _seal(content, reference) {
    const d = this.d;
    if (d.pins.get(this.other)?.hold) throw new HeldError();
    if (this.needsEpoch() != null) throw new NeedsEpochError();
    const cur = this.current;
    if (!cur.history) throw new NeedsEpochError();
    let parsed;
    try { parsed = strictJson(content); } catch { throw new Error('content must be a JSON object'); }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('content must be a JSON object');
    const kind = typeof parsed.kind === 'string' ? parsed.kind : '';
    let ref;
    if (reference) ref = reference;
    else {
      const b = d._random('message_reference', 16);
      // A reference is a UUID; these bits make it a version 4 one.
      b[6] = (b[6] & 0x0f) | 0x40;
      b[8] = (b[8] & 0x3f) | 0x80;
      ref = idOf(b);
    }
    const h = { flags: kind !== KIND_MESSAGE ? FLAG_QUIET : 0, conversationId: this.id, keyEpoch: cur.keyEpoch, accountId: d.account, deviceId: d.id, counter: this.counter + 1, reference: ref };
    const sk = await senderKey(await contentKey(cur.history, this.id, cur.keyEpoch), this.id, cur.keyEpoch, d.id);
    const sealed = await sealMessage(h, sk, d._random('message_salt', SALT_SIZE), content, d.sign);
    this.counter++;
    for (const k of [...this.sentUnder.keys()]) if (k !== cur.keyEpoch) this.sentUnder.delete(k);
    if (!this.sentUnder.has(cur.keyEpoch)) this.sentUnder.set(cur.keyEpoch, []);
    this.sentUnder.get(cur.keyEpoch).push([ref, new Uint8Array(content)]);
    if (d.saveChat) await d.saveChat(this);
    return { ciphertext: sealed, clientId: ref, keyEpoch: cur.keyEpoch, quiet: h.flags === FLAG_QUIET, counter: h.counter };
  }

  sentUnderEpoch(keyEpoch) { return [...(this.sentUnder.get(keyEpoch) || [])]; }

  // replaceEpoch takes `sealed.epoch_started` with "replaced": true: in a
  // chat across servers, the other server's record for keyEpoch stands. It
  // takes the place of the record this device was given, and what the
  // device had sent under the record that gave way is sealed again under
  // it with the same references and new counters, for the page to post.
  async replaceEpoch(keyEpoch, seq, stored) {
    const have = this.epoch(keyEpoch);
    if (!have) { await this.acceptEpoch(keyEpoch, seq, stored); return []; }
    if (equal(have.stored, stored)) return [];
    if (have !== this.current) throw new ChainGapError();
    const pending = this.sentUnder.get(keyEpoch) || [];
    this.sentUnder.delete(keyEpoch);
    this.epochs.pop();
    this.newSecrets = this.newSecrets.filter((k) => !sameNumber(k, keyEpoch));
    await this.acceptEpoch(keyEpoch, seq, stored);
    const out = [];
    for (const [ref, content] of pending) out.push(await this._seal(content, ref));
    if (!pending.length && this.d.saveChat) await this.d.saveChat(this);
    return out;
  }

  // epochStarted takes a `sealed.epoch_started` event as the server sent
  // it. Returns what must be posted again.
  async epochStarted(event) {
    const e = event.epoch;
    if (!e || typeof e !== 'object') throw new SealedError('the event has no epoch record');
    const stored = fromB64(e.record);
    if (!stored) throw new SealedError('the epoch record is not base64');
    const seq = e.seq ?? event.seq;
    if (event.replaced === true) return this.replaceEpoch(e.key_epoch, seq, stored);
    await this.acceptEpoch(e.key_epoch, seq, stored);
    return [];
  }

  // unsend takes back the counter of a message the server refused, so it
  // can be encrypted again in the current epoch with the same counter.
  unsend(o) {
    if (this.counter === o.counter) this.counter--;
    const list = this.sentUnder.get(o.keyEpoch);
    if (list) this.sentUnder.set(o.keyEpoch, list.filter((x) => x[0] !== o.clientId));
  }

  // receive checks one row (8.2, "Receiving") and, if it passes, applies
  // its content. A row that fails throws a MessageError and is counted in
  // unchecked; a row in an epoch with no key yet throws NoKeyError. row:
  // { seq, author, keyEpoch, quiet, ciphertext }. Returns { header, kind,
  // content, dropped, outOfOrder, already, gap }.
  async receive(row) {
    try { return await this._receive(row); } catch (err) {
      if (err instanceof MessageError) this.unchecked++;
      throw err;
    }
  }

  async _receive(row) {
    // 1. The header agrees with the row it arrived in.
    const h = decodeHeader(row.ciphertext);
    if (h.conversationId !== this.id || !sameNumber(h.keyEpoch, row.keyEpoch) || h.accountId !== row.author) throw new MessageError('the header does not match the row it arrived in');
    if (h.quiet !== row.quiet) throw new MessageError('the quiet flag was changed');
    // 2. The device accepted that epoch's record, the message sits in that
    // epoch's part of the sequence, and the sender is a recipient.
    const e = this.epoch(h.keyEpoch);
    if (!e || !e.accepted) throw new MessageError("the device did not accept that epoch's record");
    if (row.seq <= e.seq) throw new MessageError("the message comes before its epoch's record");
    const next = this.epoch(big(h.keyEpoch) + 1n);
    if (next && row.seq > next.seq) throw new MessageError('the message comes after a newer epoch began');
    const rc = epochRecipient(e.record, h.deviceId);
    if (!rc || rc.accountId !== h.accountId) throw new MessageError('the sending device is not in that epoch');
    if (!e.history) throw new NoKeyError();
    // 3. The signature verifies under the device's key from the list the
    // epoch names, and the content decrypts.
    const sk = await senderKey(await contentKey(e.history, this.id, e.keyEpoch), this.id, e.keyEpoch, h.deviceId);
    const dev = e.devices.get(h.deviceId);
    const [, content] = await openMessage(row.ciphertext, sk, dev ? dev.signKey : null);
    const out = { header: h, kind: '', content: {}, dropped: false, outOfOrder: false, already: false, gap: false };
    // 4. Replays: of two rows with one device and counter, the lower
    // sequence number is the message.
    const key = `${h.deviceId}/${h.counter}`;
    const at = this.seen.get(key);
    if (at != null && at === row.seq) { out.already = true; return out; }
    if (at != null) {
      if (at < row.seq) { out.dropped = true; return out; }
      // The copy seen first was the replay; this one takes its place.
      for (const m of [...this.messages.values()]) {
        if (m.seq === at && m.device === h.deviceId) { this.messages.delete(m.reference); this.order = this.order.filter((r) => r !== m.reference); }
      }
    }
    this.seen.set(key, row.seq);
    // 5. Order: the counter should be higher than the last one seen from
    // that device at the nearest lower sequence number.
    if (!this.byDevice.has(h.deviceId)) this.byDevice.set(h.deviceId, []);
    const rows = this.byDevice.get(h.deviceId);
    let i = 0;
    while (i < rows.length && rows[i].seq < row.seq) i++;
    if (i < rows.length && rows[i].seq === row.seq) rows[i].counter = h.counter;
    else rows.splice(i, 0, { counter: h.counter, seq: row.seq });
    if (i === 0) out.gap = big(h.counter) > 1n;
    else if (big(h.counter) <= big(rows[i - 1].counter)) out.outOfOrder = true;
    else out.gap = big(h.counter) > big(rows[i - 1].counter) + 1n;
    let parsed;
    try { parsed = JSON.parse(new TextDecoder().decode(content)); } catch { throw new MessageError('the content is not a JSON object'); }
    if (parsed !== null && (typeof parsed !== 'object' || Array.isArray(parsed))) throw new MessageError('the content is not a JSON object');
    out.content = parsed || {};
    out.kind = typeof out.content.kind === 'string' ? out.content.kind : '';
    await this._apply(out, row.seq, e.keyEpoch);
    return out;
  }

  // _apply draws one checked message. A kind the device does not know is
  // ignored, and so is anything that refers to a message it does not have.
  async _apply(r, seq, keyEpoch) {
    const h = r.header;
    const m = r.content;
    const str = (k) => (typeof m[k] === 'string' ? m[k] : '');
    const target = () => (isId(m.message_id) ? this.messages.get(m.message_id.toLowerCase()) : null);
    switch (r.kind) {
      case KIND_MESSAGE: {
        if (this.messages.has(h.reference)) return; // a reference is used once
        this.messages.set(h.reference, {
          reference: h.reference, author: h.accountId, device: h.deviceId, seq, keyEpoch,
          text: str('text'), entities: Array.isArray(m.entities) ? m.entities : [], files: Array.isArray(m.files) ? m.files : [],
          parentId: str('parent_id'), quote: m.quote && typeof m.quote === 'object' ? m.quote : null,
          action: m.action === true,
          edited: false, deleted: false, outOfOrder: r.outOfOrder, gap: r.gap,
          reactions: new Map(), tasks: new Map(), taskSeq: new Map(), taskBy: new Map(),
        });
        this.order.push(h.reference);
        // A stable sort by sequence number.
        this.order = this.order.map((ref, i) => [ref, i]).sort((a, b) => (this.messages.get(a[0])?.seq ?? 0) - (this.messages.get(b[0])?.seq ?? 0) || a[1] - b[1]).map((x) => x[0]);
        return;
      }
      case KIND_EDIT: {
        const t = target();
        if (t && t.author === h.accountId && !t.deleted) { t.text = str('text'); t.entities = Array.isArray(m.entities) ? m.entities : []; t.edited = true; }
        return;
      }
      case KIND_DELETE: {
        const t = target();
        if (t && t.author === h.accountId) { t.deleted = true; t.text = ''; t.files = []; }
        return;
      }
      case KIND_REACTION: {
        const t = target();
        const emoji = str('emoji');
        const on = m.on === true;
        if (!t || !emoji || t.deleted) return;
        if (!on) {
          const set = t.reactions.get(emoji);
          if (set) { set.delete(h.accountId); if (!set.size) t.reactions.delete(emoji); }
          return;
        }
        if (!t.reactions.has(emoji)) {
          if (t.reactions.size >= 20) return;
          t.reactions.set(emoji, new Set());
        }
        t.reactions.get(emoji).add(h.accountId);
        return;
      }
      case KIND_TASK: {
        const t = target();
        const item = str('item_id');
        if (t && item && seq > (t.taskSeq.get(item) || 0)) { t.tasks.set(item, m.checked === true); t.taskSeq.set(item, seq); t.taskBy.set(item, h.accountId); }
        return;
      }
      case KIND_HISTORY: {
        if (!Array.isArray(m.secrets)) return;
        for (const entry of m.secrets) {
          const n = entry && typeof entry.key_epoch === 'number' ? entry.key_epoch : 0;
          const secret = entry && typeof entry.secret === 'string' ? fromB64(entry.secret) : null;
          const e = this.epoch(n);
          // Each secret is checked against that epoch's record.
          if (!e || !e.accepted || e.history || !(await checkHistorySecret(secret, e.record))) continue;
          e.history = secret;
          this.newSecrets.push(e.keyEpoch);
        }
        return;
      }
      default:
    }
  }

  // historyShare gives the `history` message to send when the other
  // account has a device again after epochs in which it had none (12.5);
  // null if there is nothing to share. Nothing from before a reset of the
  // other account's list is ever shared.
  historyShare() {
    this.shareDue = false;
    const reset = this.d.pins.get(this.other)?.lastReset || 0;
    const secrets = [];
    for (const e of this.epochs) {
      if (!e.accepted || !e.history || epochHasAccount(e.record, this.other)) continue;
      const ref = epochRef(e.record, this.other);
      if (big(ref ? ref.listVersion : 0) < big(reset)) continue;
      secrets.push({ keyEpoch: e.keyEpoch, secret: e.history });
    }
    return secrets.length ? historyContent(secrets) : null;
  }

  // callKeys gives, for a call in the current epoch, each device's call
  // sender key and the key index every frame carries (section 10):
  // { keyEpoch, keyIndex, keys: Map device → 32 bytes }.
  async callKeys(call) {
    const cur = this.current;
    if (!cur || !cur.accepted || !cur.secret) throw new SealedError('the device does not hold the current epoch secret');
    const secret = await callSecret(cur.secret, this.id, cur.keyEpoch, call);
    const keys = new Map();
    for (const r of cur.record.recipients) keys.set(r.deviceId, await callSenderKey(secret, this.id, cur.keyEpoch, r.deviceId));
    return { keyEpoch: cur.keyEpoch, keyIndex: callKeyIndex(cur.keyEpoch), keys };
  }

  // view is what the device shows, in order.
  view() { return this.order.map((r) => this.messages.get(r)).filter(Boolean); }

  // restoreDevices fills an accepted epoch's devices from the lists its
  // record names, after it was loaded from storage.
  restoreDevices(e) {
    for (const a of e.record.accounts) {
      const raw = this.d.listOf(a.accountId, a.listVersion);
      if (!raw) continue;
      for (const dev of decodeList(raw).devices) e.devices.set(dev.deviceId, dev);
    }
  }
}
