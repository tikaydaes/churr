// Replays testdata/sealed/transcript.json step by step "as a writer"
// (testdata/sealed/README.md section 3) with the web chat's sealed-chat
// library: the conversation of internal/sealed/ref/transcript.go is played
// again by this library's Devices, each fed the random values the
// transcript lists for it, against a stand-in server that only files what
// it is given. After every step:
//
// - every value drawn matches the transcript's list (purpose, length and
//   order), and none is left over at the end;
// - what the server stored equals the step's `stored`, byte for byte;
// - what was refused equals `refused`;
// - the plaintext of a posted message equals `sent`;
// - every device's state equals its entry in `after`;
// - every device is saved to a store and loaded back, and the replay goes
//   on with the loaded copy; and, as the browser does it, a copy saved
//   without its messages and fed every row again draws the same.
//
//   node e2e/sealed-transcript.mjs

// It runs in Chromium too (web-sealed.mjs), so the library and the
// transcript are handed in rather than imported.

let S;
const hx = (s) => S.fromHex(s);
const utf8 = (s) => new TextEncoder().encode(s);
const dec = (b) => new TextDecoder().decode(b);

const ann = '0193a000-0000-7000-8000-00000000000a';
const bob = '0193a000-0000-7000-8000-00000000000b';
const conv = '0193a0e0-0000-7000-8000-0000000000c0';
const passkeyId = '0193a0aa-0000-7000-8000-0000000000f1';
const server = 'chat.example.org';
const start = 1800000000000;
const day = 24 * 60 * 60 * 1000;

// The reference's fixed stream (README section 1), used only for the
// values no device draws: the passkey's PRF output and credential, and the
// intruder's forged record.
async function fixed(seed, n) {
  const out = [];
  let len = 0;
  for (let i = 0; len < n; i++) {
    const b = await S.sha256(S.concat(utf8('churr test stream v1'), new Uint8Array(1), utf8(seed), S.u64(i)));
    out.push(b);
    len += b.length;
  }
  return S.concat(...out).slice(0, n);
}

// diff compares decoded JSON and reports the first path that differs and,
// for hex strings, the first differing byte.
function diff(got, want, path) {
  if (want && typeof want === 'object' && !Array.isArray(want)) {
    if (!got || typeof got !== 'object' || Array.isArray(got)) return `${path}: got ${JSON.stringify(got)}, want an object`;
    for (const k of new Set([...Object.keys(want), ...Object.keys(got)])) {
      const d = diff(got[k], want[k], `${path}.${k}`);
      if (d) return d;
    }
    return null;
  }
  if (Array.isArray(want)) {
    if (!Array.isArray(got)) return `${path}: got ${JSON.stringify(got)}, want a list`;
    for (let i = 0; i < Math.max(want.length, got.length); i++) {
      if (i >= got.length || i >= want.length) return `${path}: got ${got.length} items, want ${want.length}`;
      const d = diff(got[i], want[i], `${path}[${i}]`);
      if (d) return d;
    }
    return null;
  }
  if (got === want) return null;
  if (typeof got === 'string' && typeof want === 'string' && /^[0-9a-f]+$/.test(want) && want.length > 40) {
    let i = 0;
    while (i < got.length && got[i] === want[i]) i++;
    return `${path}: first differing byte at offset ${i >> 1} (got ${got.length / 2} bytes, want ${want.length / 2})`;
  }
  return `${path}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`;
}

class Sim {
  constructor(t) {
    this.t = t;
    this.steps = t.steps;
    this.now = start;
    this.lists = new Map();
    this.backups = new Map();
    this.items = [];
    this.nextSeq = 0;
    this.epochNow = 0;
    this.devices = [];
    this.randomOf = new Map();
    this.drawsBy = new Map();
    this.stepNo = 0;
    this.failures = [];
    this.stepsMatched = 0;
    this.comparisons = 0;
    this.rebuilds = 0;
    for (const s of this.steps) for (const r of s.random) {
      if (!this.randomOf.has(r.device)) this.randomOf.set(r.device, []);
      this.randomOf.get(r.device).push([r.for, hx(r.value)]);
    }
    for (const name of this.randomOf.keys()) {
      let n = 0;
      this.drawsBy.set(name, this.steps.map((s) => (n += s.random.filter((r) => r.device === name).length)));
    }
  }

  async device(name, account, platform, devId) {
    const random = new S.GivenRandom(this.randomOf.get(name) || []);
    const d = await S.Device.create({ account, server, platform, random, now: () => this.now, id: devId });
    const dev = { name, d, random, pos: 0, gone: false };
    this.wire(dev);
    this.devices.push(dev);
    return dev;
  }

  wire(dev) {
    dev.d.fetchLists = async (account, after) => this.listsAfter(account, after);
    dev.d.now = () => this.now;
    if (!dev.d.store) S.attachStore(dev.d, new S.MemoryStore());
  }

  listsAfter(account, after) { return (this.lists.get(account) || []).filter((_, i) => i + 1 > after); }
  other(account) { return account === ann ? bob : ann; }
  chatOf(dev) { return dev.d.chat(conv, this.other(dev.d.account)); }

  begin() { this.stored = []; this.refusedNow = []; this.sent = null; }
  store(e) { this.stored.push(e); }
  refuse(e) { this.refusedNow.push(e); }

  publish(l) {
    const parsed = S.decodeList(l.list);
    if (!this.lists.has(parsed.accountId)) this.lists.set(parsed.accountId, []);
    const have = this.lists.get(parsed.accountId);
    if (parsed.version !== have.length + 1) throw new Error('not the next list version');
    have.push(l);
    this.store({ table: 'device_lists', account_id: parsed.accountId, version: parsed.version, signed_list: S.toHex(S.encodeSignedList(l)) });
  }

  storeCopies(account, keyVersion, copies) {
    for (const c of copies) {
      const e = { table: 'backup_keys', account_id: account, kind: S.WAY_NAMES[c.kind], key_version: keyVersion, locked_key: S.toHex(c.locked) };
      if (c.kind === S.WAY_PASSKEY) e.passkey_id = c.id;
      if (c.kind === S.WAY_DEVICE) e.device_id = c.id;
      this.store(e);
    }
  }

  async putBundle(dev, b, how) {
    const account = dev.d.account;
    if (how !== 'create') this.store({ deleted: ['account_records', 'backup_keys', 'key_backups'], account_id: account, because: how });
    this.backups.set(account, { keyVersion: b.keyVersion, revision: b.revision, record: b.accountRecord, copies: [...b.copies], records: [...b.records] });
    this.store({ table: 'account_records', account_id: account, key_version: b.keyVersion, revision: b.revision, record: S.toHex(b.accountRecord) });
    this.storeCopies(account, b.keyVersion, b.copies);
    for (const r of b.records) this.store({ table: 'key_backups', account_id: account, conversation_id: r.conversationId, key_epoch: r.keyEpoch, key_version: r.keyVersion, record: S.toHex(r.locked) });
    this.publish(b.list);
    await dev.d.adopt(b);
  }

  storedOf(account) { const b = this.backups.get(account); return { keyVersion: b.keyVersion, revision: b.revision, accountRecord: b.record }; }
  copyOf(account, kind, id) { return this.backups.get(account).copies.find((c) => c.kind === kind && c.id === id).locked; }

  async learn(dev) {
    for (const account of [ann, bob]) {
      const all = this.lists.get(account) || [];
      if (!all.length) continue;
      const p = dev.d.pins.get(account);
      const give = p ? this.listsAfter(account, p.newest) : [all[all.length - 1]];
      if (!give.length) continue;
      try { await dev.d.learnLists(account, give); } catch (e) {
        if (!(e instanceof S.SealedError)) throw e;
        this.refuse({ device: dev.name, refused: 'device_list', account_id: account, why: e.message });
      }
    }
  }

  async sync(dev) {
    if (dev.gone) return;
    await this.learn(dev);
    const c = this.chatOf(dev);
    for (; dev.pos < this.items.length; dev.pos++) {
      const it = this.items[dev.pos];
      if (it.stored) {
        try { await c.acceptEpoch(it.keyEpoch, it.seq, it.stored); } catch (e) {
          if (!(e instanceof S.SealedError)) throw e;
          this.refuse({ device: dev.name, refused: 'epoch_record', key_epoch: it.keyEpoch, why: e.message });
        }
        continue;
      }
      if (it.erased) continue; // a deletion marker
      try { await c.receive(it.row); } catch (e) {
        if (e instanceof S.NoKeyError) continue; // read once the secret arrives
        if (!(e instanceof S.SealedError)) throw e;
        this.refuse({ device: dev.name, refused: 'message', seq: it.seq, why: e.message });
      }
    }
    await this.upload(dev);
  }

  async upload(dev) {
    if (!dev.d.unlocked) return;
    const recs = await this.chatOf(dev).takeNewSecrets();
    const b = this.backups.get(dev.d.account);
    for (const r of recs) {
      if (b.records.some((o) => o.conversationId === r.conversationId && o.keyEpoch === r.keyEpoch)) continue;
      b.records.push(r);
      this.store({ table: 'key_backups', account_id: dev.d.account, conversation_id: r.conversationId, key_epoch: r.keyEpoch, key_version: r.keyVersion, record: S.toHex(r.locked) });
    }
  }

  async syncAll() { for (const dev of this.devices) await this.sync(dev); }

  async readBackup(dev) {
    for (const r of this.backups.get(dev.d.account).records) {
      const e = this.chatOf(dev).epoch(r.keyEpoch);
      if (e && !e.history) {
        try { await this.chatOf(dev).readBackupRecord(r); } catch (err) {
          if (!(err instanceof S.SealedError)) throw err;
          this.refuse({ device: dev.name, refused: 'backup_record', key_epoch: r.keyEpoch, why: err.message });
        }
      }
    }
    await this.reread(dev);
  }

  async reread(dev) {
    const c = this.chatOf(dev);
    for (const it of this.items.slice(0, dev.pos)) {
      if (!it.row || it.erased) continue;
      const h = S.decodeHeader(it.row.ciphertext);
      if (c.hasSeen(h.deviceId, h.counter)) continue;
      try { await c.receive(it.row); } catch (e) { if (!(e instanceof S.SealedError)) throw e; }
    }
  }

  async makeEpoch(dev) {
    const [st, keyEpoch] = await this.chatOf(dev).makeEpoch();
    return this.postEpoch(dev, keyEpoch, st);
  }

  postEpoch(dev, keyEpoch, st) {
    if (keyEpoch !== this.epochNow + 1) {
      this.refuse({ device: dev.name, refused: 'epoch_record', by: 'server', code: 'conflict', key_epoch: keyEpoch, record: S.toHex(st), why: 'another record already holds this key epoch' });
      return false;
    }
    this.forceEpoch(keyEpoch, st);
    return true;
  }

  lastEpoch() { return [...this.items].reverse().find((i) => i.stored).stored; }

  forceEpoch(keyEpoch, st) {
    this.epochNow = keyEpoch;
    this.nextSeq++;
    this.items.push({ seq: this.nextSeq, keyEpoch, stored: st });
    this.store({ table: 'sealed_epochs', conversation_id: conv, key_epoch: keyEpoch, seq: this.nextSeq, record: S.toHex(st) });
  }

  async send(dev, content) {
    const out = await this.chatOf(dev).seal(content);
    if (out.keyEpoch !== this.epochNow) throw new Error('a message in a stale epoch');
    this.nextSeq++;
    const row = { seq: this.nextSeq, author: dev.d.account, keyEpoch: out.keyEpoch, quiet: out.quiet, ciphertext: out.ciphertext };
    this.items.push({ seq: this.nextSeq, keyEpoch: 0, row });
    this.sent = { content: dec(content), reference: out.clientId, counter: out.counter, key_epoch: out.keyEpoch, quiet: out.quiet };
    this.store({ table: 'messages', conversation_id: conv, seq: this.nextSeq, author_id: dev.d.account, client_id: out.clientId, key_epoch: out.keyEpoch, quiet: out.quiet, ciphertext: S.toHex(out.ciphertext) });
    return out.clientId;
  }

  erase(ref) {
    for (const it of this.items) {
      if (it.row && !it.erased && S.decodeHeader(it.row.ciphertext).reference === ref) {
        it.erased = true;
        this.store({ erased: 'messages.ciphertext', seq: it.row.seq, because: 'deleted by its author' });
      }
    }
  }

  async join(dev) {
    await this.learn(dev);
    const list = await dev.d.listWithSelf();
    this.publish(list);
    await dev.d.learnLists(dev.d.account, [list]);
    await this.sync(dev);
    await this.readBackup(dev);
    if (this.items.length) await this.makeEpoch(dev);
  }

  view(d) {
    const c = d.chats.get(conv);
    return {
      unlocked: d.unlocked,
      key_version: d.keyVersion,
      pins: Object.fromEntries([...d.pins].map(([k, p]) => [k, p.newest])),
      epochs: (c ? c.epochs : []).map((e) => ({ key_epoch: e.keyEpoch, accepted: e.accepted, can_read: e.history != null })),
      shows: (c ? c.view() : []).map((m) => {
        const o = { reference: m.reference, author_id: m.author, seq: m.seq, text: m.text };
        if (m.edited) o.edited = true;
        if (m.deleted) o.deleted = true;
        if (m.outOfOrder) o.out_of_order = true;
        if (m.reactions.size) o.reactions = Object.fromEntries([...m.reactions].map(([e, s]) => [e, [...s].sort()]));
        if (m.tasks.size) o.tasks = Object.fromEntries(m.tasks);
        return o;
      }),
      unchecked: c ? c.unchecked : 0,
      notices: d.notices.map((n) => { const o = { kind: n.kind, account_id: n.account }; if (n.platform) o.platform = S.PLATFORMS[n.platform]; return o; }),
    };
  }

  // rebuilt is what the browser does on every load: the device as stored
  // without its messages, fed every row the server holds again.
  async rebuilt(dev) {
    const store = new S.MemoryStore({ keepsMessages: false });
    const saved = dev.d.store;
    S.attachStore(dev.d, store);
    await S.saveDevice(dev.d);
    S.attachStore(dev.d, saved);
    const d = await S.loadDevice(store, { random: new S.GivenRandom([]), now: () => this.now });
    d.fetchLists = async (account, after) => this.listsAfter(account, after);
    const c = d.chat(conv, this.other(d.account));
    for (const it of this.items.slice(0, dev.pos)) {
      if (!it.row || it.erased) continue;
      try { await c.receive(it.row); } catch (e) { if (!(e instanceof S.SealedError)) throw e; }
    }
    return d;
  }

  async end() {
    const want = this.steps[this.stepNo];
    this.stepNo++;
    const where = 'step ' + this.stepNo;
    const problems = [];
    const cmp = (got, w, what) => { this.comparisons++; const d = diff(got, w, `${where} ${what}`); if (d) problems.push(d); };
    cmp(this.now, want.clock, 'clock');
    cmp(this.stored, want.stored, 'stored');
    cmp(this.sent, want.sent ?? null, 'sent');
    // A refusal's `why` is the reference's error text with its "ref: "
    // prefix; this library's messages are the same text.
    cmp(this.refusedNow, (want.refused || []).map((r) => ({ ...r, why: r.why.replace(/^ref: /, '') })), 'refused');
    const gotAfter = {};
    for (const dev of this.devices) if (!dev.gone) gotAfter[dev.name] = this.view(dev.d);
    cmp(Object.keys(gotAfter).sort(), Object.keys(want.after).sort(), 'devices in after');
    for (const name of Object.keys(want.after)) cmp(gotAfter[name], want.after[name], 'after.' + name);
    for (const dev of this.devices) cmp(dev.random.used, this.drawsBy.get(dev.name)?.[this.stepNo - 1] ?? 0, 'random values drawn by ' + dev.name);
    if (!problems.length) this.stepsMatched++; else this.failures.push(...problems);
    // Save and load every device; carry on with the loaded copy.
    for (const dev of this.devices) {
      if (dev.gone) continue;
      const r = await this.rebuilt(dev);
      const rv = this.view(r);
      // Rows a device took in before it could read them are drawn again
      // once it can; the rebuilt copy has no memory of refusals counted
      // earlier for rows it can now read, and none for rows the server no
      // longer holds, so `unchecked` is left out of this comparison.
      // A deleted message's row has no ciphertext left on the server; the
      // page draws the server's deletion marker for it instead.
      const live = (v) => ({ ...v, unchecked: 0, shows: v.shows.filter((m) => !m.deleted) });
      const d1 = diff(live(rv), live(gotAfter[dev.name]), `${where} rebuilt from storage without messages, ${dev.name}`);
      this.rebuilds++;
      if (d1) this.failures.push(d1);
      await S.saveDevice(dev.d);
      const loaded = await S.loadDevice(dev.d.store, { random: dev.random, now: () => this.now });
      dev.d = loaded;
      this.wire(dev);
      const d = diff(this.view(dev.d), gotAfter[dev.name], `${where} after reload ${dev.name}`);
      if (d) this.failures.push(d);
    }
  }

  async say(dev, content) {
    this.begin();
    const ref = await this.send(dev, content);
    await this.syncAll();
    await this.end();
    return ref;
  }
}

function expect(ok, why) { if (!ok) throw new Error(why); }

// runTranscript plays the whole transcript. lib is the library
// (shared/sealed/index.js), t the parsed transcript.json.
export async function runTranscript(lib, t, log = console.log) {
  S = lib;
  const s = new Sim(t);
  const cast = Object.fromEntries(t.devices.map((d) => [d.name, d]));
  const dev = (name) => s.device(name, cast[name].account_id, S.platformNumber(cast[name].platform), cast[name].device_id);
  const text = (x) => S.messageContent(x);

  // Setting up: each account's first device makes its backup.
  const annPhone = await dev('ann-phone');
  s.begin();
  const annBundle = await annPhone.d.createBackup();
  await s.putBundle(annPhone, annBundle, 'create');
  await s.end();
  const annPhrase = annBundle.phrase;

  const bobPhone = await dev('bob-phone');
  s.begin();
  const bobBundle = await bobPhone.d.createBackup();
  await s.putBundle(bobPhone, bobBundle, 'create');
  await s.end();
  const bobPhrase = bobBundle.phrase;

  // A second device each, before the chat exists.
  const annLaptop = await dev('ann-laptop');
  s.begin();
  await s.learn(annLaptop);
  await annLaptop.d.unlockWithPhrase(annPhrase, s.copyOf(ann, S.WAY_PHRASE, S.ZERO_ID), s.storedOf(ann));
  await s.join(annLaptop);
  await s.syncAll();
  await s.end();

  const bobDesktop = await dev('bob-desktop');
  s.begin();
  await s.learn(bobDesktop);
  const [request, code] = await bobDesktop.d.newUnlockRequest();
  const answer = await bobPhone.d.answerUnlockRequest(request, code);
  s.store({ table: 'unlock_requests', account_id: bob, device_id: bobDesktop.d.id, request: S.toHex(request), code_shown: code, answer: S.toHex(answer) });
  await bobDesktop.d.unlockWithDeviceCopy(answer, s.storedOf(bob));
  await s.join(bobDesktop);
  await s.syncAll();
  await s.end();

  // Creation: epoch 1.
  s.begin();
  await s.learn(annPhone);
  await s.makeEpoch(annPhone);
  await s.syncAll();
  await s.end();

  // Messages from every device, and every kind of content.
  const hello = await s.say(annPhone, text('Hello Bob'));
  await s.say(bobPhone, text('Hello Ann'));
  const list = await s.say(annLaptop, text('- [ ] bring the keys\n- [ ] lock up'));
  const typo = await s.say(bobDesktop, text('See you at sevn'));
  await s.say(bobPhone, S.reactionContent(hello, '👍', true));
  await s.say(bobDesktop, S.editContent(typo, 'See you at seven'));
  await s.say(annPhone, S.taskContent(list, 't1', true));
  const oops = await s.say(annLaptop, text('wrong chat, sorry'));
  s.begin();
  await s.send(annLaptop, S.deleteContent(oops));
  s.erase(oops);
  await s.syncAll();
  await s.end();

  // A device is added, through a passkey.
  const prf = await fixed('transcript passkey prf output', 32);
  const credential = await fixed('transcript passkey credential id', 20);
  s.begin();
  const unlockPublic = await S.x25519Public(await S.unlockKeyFromPrf(prf, ann, credential));
  const up = await annPhone.d.approvePasskey(passkeyId, unlockPublic);
  const ab = s.backups.get(ann);
  ab.revision = up.revision;
  ab.record = up.accountRecord;
  ab.copies.push(up.copy);
  annPhone.d.adoptRecord(up);
  s.store({ passkey_id: passkeyId, credential_id: S.toHex(credential), prf_output: S.toHex(prf), unlock_public: S.toHex(unlockPublic), note: 'not stored by the server except unlock_public; given here so the unlock key can be derived' });
  s.store({ table: 'account_records', account_id: ann, key_version: ab.keyVersion, revision: up.revision, record: S.toHex(up.accountRecord) });
  s.storeCopies(ann, ab.keyVersion, [up.copy]);
  await s.end();

  const annTablet = await dev('ann-tablet');
  s.begin();
  await s.learn(annTablet);
  await annTablet.d.unlockWithPrf(prf, credential, passkeyId, s.copyOf(ann, S.WAY_PASSKEY, passkeyId), s.storedOf(ann));
  await s.join(annTablet);
  await s.syncAll();
  await s.end();
  await s.say(annTablet, text('Hello from the tablet'));

  // A device is removed.
  s.begin();
  const without = await bobDesktop.d.listWithout([bobDesktop.d.id]);
  s.publish(without);
  await s.learn(bobPhone);
  await s.makeEpoch(bobPhone);
  await s.syncAll();
  await s.end();
  const afterRemoval = await s.say(bobPhone, text('Only my phone now'));
  expect(!s.chatOf(bobDesktop).messages.get(afterRemoval), 'a removed device read a later message');
  const e3 = s.chatOf(bobDesktop).epoch(3);
  expect(e3 && e3.accepted && e3.history == null, 'a removed device has a key for epoch 3');
  bobDesktop.gone = true;

  // An epoch made for age.
  s.now += 8 * day;
  s.begin();
  expect(s.chatOf(annLaptop).needsEpoch() === 'age', 'no epoch is due for age');
  await s.makeEpoch(annLaptop);
  await s.syncAll();
  await s.end();
  await s.say(annLaptop, text('A week later'));

  // Two devices make the same epoch at once.
  s.now += 8 * day;
  s.begin();
  const [annFive] = await s.chatOf(annPhone).makeEpoch();
  const [bobFive] = await s.chatOf(bobPhone).makeEpoch();
  s.postEpoch(annPhone, 5, annFive);
  s.postEpoch(bobPhone, 5, bobFive);
  await s.syncAll();
  expect(s.chatOf(bobPhone).needsEpoch() == null, 'the loser of the race still wants an epoch');
  await s.end();
  await s.say(bobPhone, text('Still here'));

  // An account with no device for two epochs, then a history share.
  s.begin();
  const empty = await bobPhone.d.listWithout([bobPhone.d.id]);
  s.publish(empty);
  bobPhone.gone = true;
  await s.learn(annPhone);
  await s.makeEpoch(annPhone);
  await s.syncAll();
  await s.end();
  await s.say(annPhone, text('Call me when you are back'));
  s.now += 8 * day;
  s.begin();
  await s.makeEpoch(annLaptop);
  await s.syncAll();
  await s.end();
  await s.say(annLaptop, text('Any news?'));

  const bobNew = await dev('bob-new-phone');
  s.begin();
  await s.learn(bobNew);
  await bobNew.d.unlockWithPhrase(bobPhrase, s.copyOf(bob, S.WAY_PHRASE, S.ZERO_ID), s.storedOf(bob));
  await s.join(bobNew);
  await s.syncAll();
  await s.end();
  s.begin();
  expect(s.chatOf(annPhone).shareDue, 'no history share is due');
  const share = s.chatOf(annPhone).historyShare();
  expect(share, 'nothing to share');
  await s.send(annPhone, share);
  await s.syncAll();
  await s.reread(bobNew);
  await s.upload(bobNew);
  await s.end();
  await s.say(bobNew, text('Back now, calling'));

  // A key replacement.
  s.begin();
  annTablet.gone = true;
  const replacement = await annPhone.d.replaceBackup({ devices: [annTablet.d.id] }, s.backups.get(ann).records);
  await s.putBundle(annPhone, replacement, 'replace');
  await s.makeEpoch(annPhone);
  await s.learn(annLaptop);
  await annLaptop.d.unlockWithDeviceCopy(s.copyOf(ann, S.WAY_DEVICE, annLaptop.d.id), s.storedOf(ann));
  await s.syncAll();
  await s.end();
  await s.say(annLaptop, text('Lost my tablet; it is revoked'));

  // A reset.
  const bobReset = await dev('bob-reset-phone');
  s.begin();
  bobNew.gone = true;
  await s.learn(bobReset);
  const reset = await bobReset.d.resetBackup();
  await s.putBundle(bobReset, reset, 'reset');
  await s.sync(bobReset);
  await s.makeEpoch(bobReset);
  await s.syncAll();
  await s.end();
  await s.say(bobReset, text('New phone. Lost everything, sorry'));
  await s.say(annPhone, text('Compare codes with me when we meet'));

  // A refused record and its repair.
  s.begin();
  const intruder = await S.SigningKey.fromSeed(await fixed('transcript intruder sign seed', 32));
  const intruderOneTime = new Map();
  for (const l of [s.lists.get(ann).at(-1), s.lists.get(bob).at(-1)]) for (const d of S.decodeList(l.list).devices) intruderOneTime.set(d.deviceId, await fixed('transcript intruder one-time ' + d.deviceId, 32));
  const forged = await S.makeEpoch({
    conversationId: conv, keyEpoch: 11, prev: await S.epochHash(s.lastEpoch()), makerAccountId: ann, makerDeviceId: annPhone.d.id, makerSign: intruder,
    lists: [{ accountId: ann, list: s.lists.get(ann).at(-1).list }, { accountId: bob, list: s.lists.get(bob).at(-1).list }],
    secret: await fixed('transcript intruder epoch secret', 32), oneTime: (d) => intruderOneTime.get(d), at: s.now,
  });
  s.forceEpoch(11, forged);
  await s.syncAll();
  await s.end();
  s.begin();
  expect(s.chatOf(annPhone).needsEpoch() === 'refused', 'the refused record does not ask for a repair');
  await s.makeEpoch(annPhone);
  await s.syncAll();
  await s.end();
  await s.say(bobReset, text('Working again'));

  log(`TRANSCRIPT: ${s.stepsMatched}/${s.steps.length} steps matched in every compared value (${s.comparisons} comparisons); every device saved and reloaded after every step, and rebuilt from storage without messages ${s.rebuilds} times`);
  expect(s.stepNo === s.steps.length, `${s.stepNo} of ${s.steps.length} steps were played`);
  for (const d of s.devices) expect(d.random.left === 0, `${d.name} left ${d.random.left} given random values unused`);
  return { matched: s.stepsMatched, total: s.steps.length, failures: s.failures };
}

if (typeof process !== 'undefined' && process.argv && process.argv[1] && import.meta.url.endsWith(process.argv[1].split('/').pop())) {
  const { readFileSync } = await import('node:fs');
  const lib = await import('../internal/web/static/shared/sealed/index.js');
  const r = await runTranscript(lib, JSON.parse(readFileSync(new URL('../testdata/sealed/transcript.json', import.meta.url), 'utf8')));
  for (const f of r.failures.slice(0, 30)) console.log('FAIL ' + f);
  if (r.failures.length) process.exitCode = 1;
}
