// The web chat's sealed-chat library (internal/web/static/shared/sealed)
// against every test vector in testdata/sealed but the transcript, which
// sealed-transcript.mjs replays. Node stands in for the browser: both have
// the same Web Crypto. Bytes are compared exactly; a mismatch names the
// first byte that differs. Every case runs and is counted.
//
//   node e2e/sealed-vectors.mjs          prints "VECTORS <file>: n/m passed"
//
// The same cases run in Chromium (web-sealed.mjs), which loads this file
// and the vectors from the test, and the library from the server: so the
// library and the vector files are handed in rather than imported.

let S, load;
const hx = (s) => S.fromHex(s);
const enc = new TextEncoder();

export class Mismatch extends Error {}

export function sameBytes(got, want, what) {
  if (S.equal(got, want)) return;
  let i = 0;
  while (i < got.length && i < want.length && got[i] === want[i]) i++;
  throw new Mismatch(`${what}: first differing byte at offset ${i} (got ${got.length} bytes, want ${want.length}); got ${S.toHex(got.subarray(i, i + 8))}…, want ${S.toHex(want.subarray(i, i + 8))}…`);
}
export function eq(got, want, what) {
  if (got !== want && !(typeof got === 'bigint' || typeof want === 'bigint' ? String(got) === String(want) : false)) throw new Mismatch(`${what}: got ${got}, want ${want}`);
}
export function yes(ok, what) { if (!ok) throw new Mismatch(what); }
export async function refused(fn, what) {
  try { await fn(); } catch { return; }
  throw new Mismatch('accepted what must be refused: ' + what);
}

export class Cases {
  constructor(file) { this.file = file; this.passed = 0; this.failures = []; }
  get total() { return this.passed + this.failures.length; }
  async check(name, body) {
    try { await body(); this.passed++; } catch (e) { this.failures.push(`${name}: ${e instanceof Mismatch ? e.message : e.stack}`); }
  }
}

const pattern = (n) => { const b = new Uint8Array(n); for (let i = 0; i < n; i++) b[i] = i % 251; return b; };
function pieces(b, size) {
  return new ReadableStream({ start(c) { for (let i = 0; i < b.length; i += size) c.enqueue(b.slice(i, i + size)); c.close(); } });
}

const files = {};

files['primitives.json'] = async (c) => {
  const v = load('primitives.json');
  for (const x of v.x25519) {
    await c.check('x25519 ' + x.source, async () => {
      if (x.refused) await refused(() => S.x25519(hx(x.private), hx(x.public)), 'all-zero X25519 result');
      else sameBytes(await S.x25519(hx(x.private), hx(x.public)), hx(x.shared), 'shared');
    });
  }
  for (const x of v.x25519_public) await c.check('x25519_public ' + x.source, async () => sameBytes(await S.x25519Public(hx(x.private)), hx(x.public), 'public'));
  for (const x of v.hkdf_sha256) await c.check('hkdf ' + x.source, async () => sameBytes(await S.hkdf(hx(x.ikm), hx(x.salt), hx(x.info), x.length), hx(x.okm), 'okm'));
  for (const x of v.aes_256_gcm) {
    await c.check('gcm ' + x.source, async () => {
      const ct = await S.gcmSeal(hx(x.key), hx(x.nonce), hx(x.plaintext), hx(x.associated_data));
      sameBytes(ct, hx(x.ciphertext), 'ciphertext');
      sameBytes(await S.gcmOpen(hx(x.key), hx(x.nonce), ct, hx(x.associated_data)), hx(x.plaintext), 'plaintext');
    });
  }
  for (const x of v.sha256) await c.check('sha256 ' + x.source, async () => sameBytes(await S.sha256(hx(x.message)), hx(x.digest), 'digest'));
  for (const x of v.ed25519) {
    await c.check('ed25519 ' + x.source, async () => {
      eq(await S.verifyRaw(hx(x.public), hx(x.message), hx(x.signature)), x.valid, 'valid');
      if (x.seed) {
        const k = await S.SigningKey.fromSeed(hx(x.seed));
        sameBytes(k.public, hx(x.public), 'public from seed');
        if (x.valid) sameBytes(await k.signRaw(hx(x.message)), hx(x.signature), 'signature');
      }
    });
  }
};

files['encoding.json'] = async (c) => {
  const v = load('encoding.json');
  for (const t of v.types) {
    await c.check(`type ${t.type} ${typeof t.value === 'string' ? t.value.slice(0, 30) : ''}`, async () => {
      const w = new S.Writer();
      const r = new S.Reader(hx(t.bytes));
      const value = t.value;
      switch (t.type) {
        case 'u8': w.u8(Number(value)); eq(r.u8(), Number(value), 'read'); break;
        case 'u32': w.u32(Number(value)); eq(r.u32(), Number(value), 'read'); break;
        case 'u64': case 'time': {
          const want = BigInt(value);
          w.u64(want);
          eq(S.big(r.u64()), want, 'read');
          break;
        }
        case 'uuid': w.uuid(value); eq(r.uuid(), value, 'read'); break;
        case 'blob': w.blob(hx(value)); sameBytes(r.blob(), hx(value), 'read'); break;
        case 'text': w.text(value); eq(r.text(), value, 'read'); break;
        case 'list of uuid': {
          w.u32(value.length);
          for (const u of value) w.uuid(u);
          const n = r.count(16);
          eq(Array.from({ length: n }, () => r.uuid()).join(','), value.join(','), 'read');
          break;
        }
        default: throw new Mismatch('unknown type ' + t.type);
      }
      r.done();
      sameBytes(w.bytes(), hx(t.bytes), 'bytes');
    });
  }
  const reencode = (record, b) => {
    if (record === 'unlock_request') return S.encodeUnlockRequest(S.decodeUnlockRequest(b));
    if (record === 'device_list') return S.encodeList(S.decodeList(b));
    throw new Mismatch('unknown record ' + record);
  };
  for (const a of v.accepted) await c.check('accepted ' + a.record, async () => sameBytes(reencode(a.record, hx(a.bytes)), hx(a.bytes), 'round trip'));
  for (const x of v.refusals) {
    await c.check(`refused ${x.record}: ${x.why}`, async () => {
      try { reencode(x.record, hx(x.bytes)); } catch (e) { if (e instanceof S.RecordError) return; throw e; }
      throw new Mismatch('accepted');
    });
  }
};

files['signatures.json'] = async (c) => {
  const v = load('signatures.json');
  const key = await S.SigningKey.fromSeed(hx(v.seed));
  for (const s of v.signatures) {
    await c.check('signature ' + s.label, async () => {
      sameBytes(key.public, hx(s.public), 'public');
      sameBytes(S.labelled(s.label, hx(s.record)), hx(s.signed_bytes), 'signed bytes');
      yes(await S.verify(hx(s.public), s.label, hx(s.record), hx(s.signature)), 'verifies');
      sameBytes(await S.sign(key, s.label, hx(s.record)), hx(s.signature), 'signature');
      for (const other of s.refused_under) yes(!(await S.verify(hx(s.public), other, hx(s.record), hx(s.signature))), 'refused under ' + other);
    });
  }
  for (const h of v.hashes) await c.check('hash ' + h.label, async () => sameBytes(await S.hashOf(h.label, hx(h.record)), hx(h.hash), 'hash'));
};

files['seal.json'] = async (c) => {
  const v = load('seal.json');
  for (const x of v.vectors) {
    await c.check('seal ' + x.label, async () => {
      const pub = await S.x25519Public(hx(x.recipient_private));
      sameBytes(pub, hx(x.recipient_public), 'recipient public');
      sameBytes(await S.x25519Public(hx(x.one_time_private)), hx(x.one_time_public), 'one-time public');
      const sealed = await S.seal(pub, x.label, hx(x.context), hx(x.plaintext), hx(x.one_time_private));
      sameBytes(sealed, hx(x.sealed), 'sealed');
      sameBytes(await S.openSealed(hx(x.recipient_private), x.label, hx(x.context), sealed), hx(x.plaintext), 'opened');
      // The same with the private key held as a key that cannot be read.
      const box = await S.BoxKey.fromBytes(hx(x.recipient_private));
      sameBytes(await S.openSealed(box, x.label, hx(x.context), sealed), hx(x.plaintext), 'opened with a non-extractable key');
    });
  }
  for (const x of v.refusals) await c.check('refused: ' + x.why, () => refused(() => S.openSealed(hx(x.recipient_private), x.label, hx(x.context), hx(x.sealed)), x.why));
};

files['lock.json'] = async (c) => {
  const v = load('lock.json');
  for (const x of v.vectors) {
    await c.check('lock ' + x.label, async () => {
      const locked = await S.lock(hx(x.key), x.label, hx(x.context), hx(x.plaintext), hx(x.salt));
      sameBytes(locked, hx(x.locked), 'locked');
      sameBytes(await S.unlockLocked(hx(x.key), x.label, hx(x.context), locked), hx(x.plaintext), 'unlocked');
    });
  }
  for (const x of v.refusals) await c.check('refused: ' + x.why, () => refused(() => S.unlockLocked(hx(x.key), x.label, hx(x.context), hx(x.locked)), x.why));
};

files['device_list.json'] = async (c) => {
  const v = load('device_list.json');
  const chain = v.chain;
  for (let i = 0; i < chain.length; i++) {
    const x = chain[i];
    await c.check(`chain version ${i + 1}`, async () => {
      const f = x.fields;
      const l = { format: f.format, accountId: f.account_id, server: f.server, version: f.version, prev: hx(f.prev), reset: f.reset, keyVersion: f.key_version, accountKey: hx(f.account_key), at: f.at,
        devices: f.devices.map((d) => ({ deviceId: d.device_id, signKey: hx(d.sign_key), boxKey: hx(d.box_key), platform: d.platform, addedAt: d.added_at, proof: hx(d.proof) })) };
      sameBytes(S.encodeList(l), hx(x.list), 'list record');
      sameBytes(await S.listHash(hx(x.list)), hx(x.hash), 'hash');
      const key = await S.SigningKey.fromSeed(hx(x.account_key_seed));
      sameBytes(key.public, l.accountKey, 'account key from seed');
      const previous = x.prev_sig ? await S.SigningKey.fromSeed(hx(chain[i - 1].account_key_seed)) : null;
      const signed = await S.signList(hx(x.list), key, previous);
      sameBytes(signed.sig, hx(x.sig), 'sig');
      if (x.prev_sig) sameBytes(signed.prevSig, hx(x.prev_sig), 'prev_sig');
      sameBytes(S.encodeSignedList(signed), hx(x.signed_list), 'signed_list');
      const given = S.decodeSignedList(hx(x.signed_list));
      let step;
      if (i === 0) { await S.checkFirstList(given); step = 'first'; } else step = (await S.checkListStep(hx(chain[i - 1].list), given))[1];
      eq(step, x.step, 'step');
    });
  }
  for (const p of v.proofs) {
    await c.check('proof ' + p.device.device_id, async () => {
      const d = p.device;
      const k = await S.SigningKey.fromSeed(hx(d.sign_seed), false);
      sameBytes(k.public, hx(d.sign_key), 'sign key');
      sameBytes(await S.x25519Public(hx(d.box_private)), hx(d.box_key), 'box key');
      sameBytes(S.proofBytes(d.account_id, d.device_id, k.public, hx(d.box_key)), hx(p.signed_record), 'signed record');
      sameBytes(await S.deviceProof(k, d.account_id, d.device_id, hx(d.box_key)), hx(p.proof), 'proof');
      yes(await S.verify(k.public, p.label, hx(p.signed_record), hx(p.proof)), 'proof verifies');
    });
  }
  for (const x of v.refusals) {
    await c.check(`refused (rule ${x.rule}): ${x.why}`, async () => {
      try { await S.checkListStep(hx(x.pinned_list), S.decodeSignedList(hx(x.signed_list))); } catch (e) {
        if (e instanceof S.ListError) { eq(e.rule, x.rule, 'rule'); return; }
        throw e;
      }
      throw new Mismatch('accepted');
    });
  }
  for (const x of v.first_contact) {
    await c.check('first contact: ' + x.why, async () => {
      let ok = true;
      try { await S.checkFirstList(S.decodeSignedList(hx(x.signed_list))); } catch (e) { if (e instanceof S.ListError) ok = false; else throw e; }
      eq(ok, x.accepted, 'accepted');
    });
  }
};

files['epoch_record.json'] = async (c) => {
  const v = load('epoch_record.json');
  const conv = v.conversation_id;
  const devices = new Map(v.devices.map((d) => [d.device_id, d]));
  const lists = new Map(v.lists_checked.map((l) => [`${l.account_id}/${l.version}`, hx(l.list)]));
  const check = async (stored, previous, device) => S.checkEpochRecord({ stored, conversationId: conv, accounts: v.accounts, previous, listOf: (a, ver) => lists.get(`${a}/${ver}`) || null, deviceId: device, box: await S.BoxKey.fromBytes(hx(devices.get(device).box_private)) });
  const e1 = v.epoch_1, in1 = e1.inputs;
  await c.check('epoch_1 from its inputs', async () => {
    const maker = in1.maker;
    const stored = await S.makeEpoch({ conversationId: in1.conversation_id, keyEpoch: in1.key_epoch, makerAccountId: maker.account_id, makerDeviceId: maker.device_id,
      makerSign: await S.SigningKey.fromSeed(hx(maker.sign_seed), false), lists: in1.lists.map((l) => ({ accountId: l.account_id, list: hx(l.list) })),
      secret: hx(in1.epoch_secret), oneTime: (d) => hx(in1.one_time_private[d]), at: in1.at });
    sameBytes(stored.subarray(0, stored.length - 64), hx(e1.record), 'record');
    yes(await S.verify(hx(maker.sign_key), S.labelEpoch, hx(e1.record), hx(e1.signature)), "the vector's signature verifies");
    yes(await S.verify(hx(maker.sign_key), S.labelEpoch, hx(e1.record), stored.subarray(stored.length - 64)), 'our signature verifies');
    sameBytes(stored, hx(e1.stored), 'stored (record and signature)');
    sameBytes(await S.epochHash(stored), hx(e1.hash), 'hash');
  });
  const fromFields = (f) => S.encodeEpoch({ format: f.format, conversationId: f.conversation_id, keyEpoch: f.key_epoch, prev: hx(f.prev), makerAccountId: f.maker_account_id, makerDeviceId: f.maker_device_id,
    accounts: f.accounts.map((a) => ({ accountId: a.account_id, listVersion: a.list_version, listHash: hx(a.list_hash) })), check: hx(f.check),
    recipients: f.recipients.map((r) => ({ accountId: r.account_id, deviceId: r.device_id, sealed: hx(r.sealed) })), at: f.at });
  await c.check('epoch_1 fields', async () => sameBytes(fromFields(e1.fields), hx(e1.record), 'record from fields'));
  await c.check('epoch_1 derived', async () => {
    const d = e1.derived;
    const secret = hx(in1.epoch_secret);
    sameBytes(S.epochSalt(conv, 1), hx(d.salt), 'salt');
    const history = await S.historySecret(secret, conv, 1);
    sameBytes(history, hx(d.history_secret), 'history secret');
    const content = await S.contentKey(history, conv, 1);
    sameBytes(content, hx(d.content_key), 'content key');
    sameBytes(await S.epochCheck(history, conv, 1), hx(d.check), 'check');
    for (const [dev, k] of Object.entries(d.sender_keys)) sameBytes(await S.senderKey(content, conv, 1, dev), hx(k), 'sender key ' + dev);
    for (const [dev, k] of Object.entries(d.seal_context)) sameBytes(S.epochSealContext(conv, 1, dev), hx(k), 'seal context ' + dev);
  });
  for (const r of e1.fields.recipients) {
    await c.check('epoch_1 accepted by ' + r.device_id, async () => {
      const ok = await check(hx(e1.stored), null, r.device_id);
      sameBytes(ok.secret, hx(in1.epoch_secret), 'opened secret');
    });
  }
  const e2 = v.epoch_2;
  await c.check('epoch_2', async () => {
    sameBytes(fromFields(e2.fields), hx(e2.record), 'record from fields');
    sameBytes(S.concat(hx(e2.record), hx(e2.signature)), hx(e2.stored), 'stored');
    sameBytes(await S.epochHash(hx(e2.stored)), hx(e2.hash), 'hash');
    sameBytes(hx(e2.fields.prev), hx(e1.hash), 'prev is the hash of epoch 1');
    const ok = await check(hx(e2.stored), hx(e1.stored), '0193aa00-0000-7000-8000-0000000000a2');
    yes(ok.secret != null, 'opened');
  });
  for (const x of v.refusals) {
    await c.check(`refused (rule ${x.rule}): ${x.why}`, async () => {
      try { await check(hx(x.stored), x.previous_stored ? hx(x.previous_stored) : null, x.checking_device); } catch (e) {
        if (e instanceof S.EpochError) { eq(e.rule, x.rule, 'rule'); return; }
        throw e;
      }
      throw new Mismatch('accepted');
    });
  }
  const outsider = v.accepted_by_a_device_that_is_not_a_recipient;
  await c.check('accepted by a device that is not a recipient', async () => {
    const ok = await check(hx(outsider.stored), null, outsider.checking_device);
    yes(ok.secret == null, 'no secret for a non-recipient');
  });
};

files['message.json'] = async (c) => {
  const v = load('message.json');
  const ev = load('epoch_record.json');
  const dl = load('device_list.json');
  const conv = v.conversation_id;
  const devices = new Map(ev.devices.map((d) => [d.device_id, d]));
  const content = await S.contentKey(hx(v.history_secret), conv, 1);
  await c.check('content key', async () => sameBytes(content, hx(v.content_key), 'content key'));
  for (const m of v.messages) {
    await c.check('message ' + m.kind, async () => {
      const hf = m.header;
      const h = { format: hf.format, flags: hf.flags, conversationId: hf.conversation_id, keyEpoch: hf.key_epoch, accountId: hf.account_id, deviceId: hf.device_id, counter: hf.counter, reference: hf.reference };
      sameBytes(S.encodeHeader(h), hx(hf.bytes), 'header');
      eq(S.decodeHeader(hx(hf.bytes)).quiet, m.quiet, 'quiet');
      sameBytes(enc.encode(m.content), hx(m.content_bytes), 'content bytes');
      const sk = await S.senderKey(content, conv, 1, h.deviceId);
      sameBytes(sk, hx(m.sender_key), 'sender key');
      const signer = await S.SigningKey.fromSeed(hx(devices.get(h.deviceId).sign_seed), false);
      sameBytes(signer.public, hx(m.sign_key), 'sign key');
      const sealed = await S.sealMessage(h, sk, hx(m.salt), hx(m.content_bytes), signer);
      const want = hx(m.sealed);
      sameBytes(sealed.subarray(0, sealed.length - 64), want.subarray(0, want.length - 64), 'sealed, without the signature');
      sameBytes(sealed, want, 'sealed');
      const [, opened] = await S.openMessage(want, sk, hx(m.sign_key));
      sameBytes(opened, hx(m.content_bytes), 'opened content');
    });
  }
  // The receiving device: Bob's phone, holding epoch 1 at seq 1.
  const bobId = v.receiving_device;
  const bob = devices.get(bobId);
  const receiver = await S.Device.create({ account: bob.account_id, server: 'chat.example.org', platform: bob.platform, id: bobId,
    random: new S.GivenRandom([['device_sign_seed', hx(bob.sign_seed)], ['device_box_private', hx(bob.box_private)]]), now: () => 1800000006000 });
  const ann = '0193a000-0000-7000-8000-00000000000a';
  await receiver.learnLists(ann, [S.decodeSignedList(hx(dl.chain[1].signed_list))]);
  // Bob's version 1 is given as its record only; it is pinned directly.
  const bobList = hx(ev.lists_checked.find((l) => l.account_id === bob.account_id).list);
  receiver.pins.set(receiver.account, new S.Pin(new Map([[1, { signed: { list: bobList, sig: new Uint8Array(64), prevSig: null }, list: S.decodeList(bobList) }]]), 1, 1));
  const chat = receiver.chat(conv, ann);
  await chat.acceptEpoch(1, v.epoch_seq, hx(v.epoch_stored));
  for (const x of v.receive) {
    await c.check('receive: ' + x.why, async () => {
      const r = x.row;
      let outcome = 'refused';
      try {
        const got = await chat.receive({ seq: r.seq, author: r.author_id, keyEpoch: r.key_epoch, quiet: r.quiet, ciphertext: hx(r.ciphertext) });
        outcome = got.dropped ? 'dropped' : got.outOfOrder ? 'shown_out_of_order' : got.gap ? 'shown_with_gap' : 'shown';
      } catch (e) { if (!(e instanceof S.MessageError)) throw e; }
      eq(outcome, x.outcome, 'outcome');
    });
  }
  await c.check('shown after', async () => {
    const view = chat.view();
    const want = v.shown_after;
    eq(view.length, want.length, 'messages shown');
    want.forEach((w, i) => {
      eq(view[i].reference, w.reference, 'reference ' + i);
      eq(view[i].text, w.text, 'text ' + i);
      if ('edited' in w) eq(view[i].edited, w.edited, 'edited ' + i);
      if ('out_of_order' in w) eq(view[i].outOfOrder, w.out_of_order, 'out of order ' + i);
    });
    eq(chat.unchecked, 7, 'rows shown as not checked');
  });
};

files['file.json'] = async (c) => {
  const v = load('file.json');
  const key = hx(v.key);
  eq(v.chunk_size, S.CHUNK_SIZE, 'chunk size');
  for (const n of v.nonces) await c.check(`nonce ${n.chunk} ${n.last}`, async () => sameBytes(S.chunkNonce(n.chunk, n.last), hx(n.nonce), 'nonce'));
  for (const f of v.files) {
    await c.check(`file of ${f.size} bytes`, async () => {
      const plain = pattern(f.size);
      sameBytes(await S.sha256(plain), hx(f.plaintext_sha256), 'plaintext');
      const blob = await S.encryptFile(key, plain);
      eq(blob.length, f.blob_size, 'blob size');
      eq(S.blobSize(f.size), f.blob_size, 'blob size worked out');
      if (f.blob) sameBytes(blob, hx(f.blob), 'blob');
      sameBytes(blob.subarray(0, Math.min(32, blob.length)), hx(f.blob_first_bytes), 'first bytes');
      sameBytes(blob.subarray(Math.max(0, blob.length - 32)), hx(f.blob_last_bytes), 'last bytes');
      sameBytes(await S.sha256(blob), hx(f.blob_sha256), 'blob sha256');
      eq(Math.ceil(blob.length / S.SEALED_CHUNK_SIZE), f.chunks, 'chunks');
      // As streams, fed in pieces that do not line up with chunks.
      sameBytes(await S.collect(pieces(plain, 7777).pipeThrough(S.encryptStream(key)), true), blob, 'streamed blob');
      sameBytes(await S.decryptFile(key, blob), plain, 'decrypted');
      sameBytes(await S.collect(pieces(blob, 10007).pipeThrough(S.decryptStream(key)), true), plain, 'stream-decrypted');
    });
  }
  const big = await S.encryptFile(key, pattern(200000));
  const SC = S.SEALED_CHUNK_SIZE;
  const changed = await S.encryptFile(key, pattern(1));
  changed[0] ^= 1;
  const blobs = {
    'chunks reordered': S.concat(big.subarray(SC, 2 * SC), big.subarray(0, SC), big.subarray(2 * SC)),
    'the file cut short': big.subarray(0, 3 * SC),
    'the file cut short to one chunk': (await S.encryptFile(key, pattern(65537))).subarray(0, SC),
    'a missing last-chunk mark': await S.encryptChunk(key, 0, false, pattern(1)),
    'a changed byte': changed,
    'shorter than a tag': new Uint8Array(15),
  };
  for (const x of v.refusals) {
    await c.check('refused: ' + x.why, async () => {
      const blob = blobs[x.why];
      if (!blob) throw new Mismatch('no blob made for ' + x.why);
      eq(blob.length, x.blob_size, 'blob size');
      sameBytes(await S.sha256(blob), hx(x.blob_sha256), 'blob sha256');
      if (x.blob) sameBytes(blob, hx(x.blob), 'blob');
      await refused(() => S.decryptFile(key, blob), x.why);
      await refused(() => S.collect(pieces(blob, 4096).pipeThrough(S.decryptStream(key)), true), x.why + ' (stream)');
    });
  }
};

files['account_record.json'] = async (c) => {
  const v = load('account_record.json');
  const f = v.fields;
  const rec = { format: f.format, accountId: f.account_id, keyVersion: f.key_version, revision: f.revision, accountKey: hx(f.account_key), accountKeyPrivate: hx(f.account_key_private), ways: f.ways.map((w) => ({ kind: w.kind, id: w.id, unlockPublic: hx(w.unlock_public) })), at: f.at };
  await c.check('content and stored', async () => {
    sameBytes(S.encodeAccountRecord(rec), hx(v.content), 'content');
    sameBytes(S.accountRecordContext(rec.accountId, rec.keyVersion, rec.revision), hx(v.context), 'context');
    const stored = await S.lockAccountRecord(hx(v.backup_key), rec, hx(v.salt));
    sameBytes(stored, hx(v.stored), 'stored');
    const back = await S.openAccountRecord(hx(v.backup_key), rec.accountId, rec.keyVersion, rec.revision, stored);
    sameBytes(S.encodeAccountRecord(back), hx(v.content), 'opened');
  });
  for (const x of v.refusals) await c.check('refused: ' + x.why, () => refused(() => S.openAccountRecord(hx(x.backup_key), x.account_id, x.key_version, x.revision, hx(x.stored)), x.why));
};

files['backup_record.json'] = async (c) => {
  const v = load('backup_record.json');
  const f = v.fields;
  const account = v.account_id;
  const br = { format: f.format, conversationId: f.conversation_id, keyEpoch: f.key_epoch, secret: hx(f.secret), at: f.at };
  await c.check('content and stored', async () => {
    sameBytes(S.encodeBackupRecord(br), hx(v.content), 'content');
    sameBytes(S.backupRecordContext(account, br.conversationId, br.keyEpoch, v.key_version), hx(v.context), 'context');
    const stored = await S.lockBackupRecord(hx(v.backup_key), account, v.key_version, br, hx(v.salt));
    eq(stored.length, 97, 'stored size');
    sameBytes(stored, hx(v.stored), 'stored');
    const back = await S.openBackupRecord(hx(v.backup_key), account, br.conversationId, br.keyEpoch, v.key_version, stored);
    sameBytes(back.secret, br.secret, 'secret');
    sameBytes(await S.epochCheck(back.secret, br.conversationId, br.keyEpoch), hx(v.epoch_check), 'epoch check');
  });
  for (const x of v.refusals) await c.check('refused: ' + x.why, () => refused(() => S.openBackupRecord(hx(v.backup_key), x.account_id, x.conversation_id, x.key_epoch, x.key_version, hx(x.stored)), x.why));
  await c.check('opens but fails the epoch check', async () => {
    const o = v.opens_but_fails_the_epoch_check;
    const back = await S.openBackupRecord(hx(v.backup_key), account, br.conversationId, 1, v.key_version, hx(o.stored));
    sameBytes(back.secret, hx(o.secret), 'secret');
    yes(!S.equal(await S.epochCheck(back.secret, br.conversationId, 1), hx(v.epoch_check)), 'refused at the epoch check');
  });
};

files['locked_copy.json'] = async (c) => {
  const v = load('locked_copy.json');
  const account = v.account_id, kv = v.key_version;
  const accountKey = await S.SigningKey.fromSeed(hx(v.account_key_seed));
  await c.check('account key', async () => sameBytes(accountKey.public, hx(v.account_key), 'account key'));
  for (const x of v.copies) {
    await c.check('copy ' + x.way, async () => {
      sameBytes(S.wayTag(x.kind, x.id), hx(x.way_tag), 'way tag');
      sameBytes(S.lockedCopyContext(account, kv, x.kind, x.id), hx(x.context), 'context');
      const pub = await S.x25519Public(hx(x.unlock_private));
      sameBytes(pub, hx(x.unlock_public), 'unlock public');
      const cp = await S.makeLockedCopy(pub, account, kv, x.kind, x.id, hx(v.backup_key), accountKey, hx(x.one_time_private));
      sameBytes(cp.subarray(0, 80), hx(x.sealed), 'sealed');
      sameBytes(S.concat(hx(x.context), hx(x.sealed)), hx(x.signed_record), 'signed record');
      yes(await S.verify(accountKey.public, S.labelLockedCopy, hx(x.signed_record), hx(x.signature)), "the vector's signature verifies");
      sameBytes(cp, hx(x.stored), 'stored');
      sameBytes(await S.openLockedCopy(hx(x.unlock_private), account, kv, x.kind, x.id, hx(x.stored)), hx(v.backup_key), 'opened');
      yes(await S.verifyLockedCopy(accountKey.public, account, kv, x.kind, x.id, hx(x.stored)), 'copy verifies');
    });
  }
  for (const x of v.refusals) await c.check('refused: ' + x.why, () => refused(() => S.openLockedCopy(hx(x.unlock_private), account, x.key_version, x.kind, x.id, hx(x.stored)), x.why));
  await c.check('opens but is not signed by the account key', async () => {
    const o = v.opens_but_is_not_signed_by_the_account_key;
    await S.openLockedCopy(hx(o.unlock_private), account, o.key_version, o.kind, o.id, hx(o.stored));
    yes(!(await S.verifyLockedCopy(hx(o.account_key), account, o.key_version, o.kind, o.id, hx(o.stored))), 'refused at the signature check');
  });
};

files['unlock_keys.json'] = async (c) => {
  const v = load('unlock_keys.json');
  const account = v.account_id;
  await c.check('prf input', async () => sameBytes(await S.prfInput(), hx(v.prf_input.bytes), 'prf input'));
  await c.check('passkey', async () => {
    const p = v.passkey;
    sameBytes(S.labelled(S.labelUnlockPasskey, S.idBytes(account), hx(p.credential_id)), hx(p.info), 'info');
    const priv = await S.unlockKeyFromPrf(hx(p.prf_output), account, hx(p.credential_id));
    sameBytes(priv, hx(p.unlock_private), 'private');
    sameBytes(await S.x25519Public(priv), hx(p.unlock_public), 'public');
  });
  await c.check('phrase', async () => {
    const p = v.phrase;
    eq(await S.phraseOf(hx(p.entropy)), p.words, 'words');
    sameBytes(await S.readPhrase(p.words), hx(p.entropy), 'entropy');
    sameBytes(S.labelled(S.labelUnlockPhrase, S.idBytes(account)), hx(p.info), 'info');
    const priv = await S.unlockKeyFromPhrase(hx(p.entropy), account);
    sameBytes(priv, hx(p.unlock_private), 'private');
    sameBytes(await S.x25519Public(priv), hx(p.unlock_public), 'public');
  });
  await c.check('access key', async () => {
    const p = v.access_key;
    sameBytes(await S.hkdf(hx(p.random), hx(p.wrap_salt), hx(p.wrap_info), 32), hx(p.wrap), 'wrap');
    sameBytes(S.labelled(S.labelUnlockAccessKey), hx(p.info), 'info');
    const priv = await S.unlockKeyFromAccessKey(hx(p.wrap));
    sameBytes(priv, hx(p.unlock_private), 'private');
    sameBytes(await S.x25519Public(priv), hx(p.unlock_public), 'public');
  });
  for (const x of v.phrases) {
    await c.check('phrase ' + x.entropy, async () => {
      eq(await S.phraseOf(hx(x.entropy)), x.words, 'words');
      sameBytes(await S.readPhrase(x.words), hx(x.entropy), 'entropy');
    });
  }
  for (const x of v.reading) {
    await c.check('reading: ' + x.why, async () => {
      let got = null;
      try { got = await S.readPhrase(x.typed); } catch (e) { if (!(e instanceof S.PhraseError)) throw e; }
      eq(got != null, x.accepted, 'accepted');
      if (got) sameBytes(got, hx(x.entropy), 'entropy');
    });
  }
};

files['handover.json'] = async (c) => {
  const v = load('handover.json');
  const account = v.account_id;
  const app = hx(v.app_box_private);
  await c.check('app box key', async () => sameBytes(await S.x25519Public(app), hx(v.app_box_key), 'app box key'));
  const s = v.sign_in;
  await c.check('sign-in hand-over', async () => {
    sameBytes(S.concat(S.idBytes(account), enc.encode(s.code_challenge)), hx(s.context), 'context');
    const ho = await S.sealHandover(hx(v.app_box_key), account, s.code_challenge, hx(v.backup_key), hx(s.one_time_private));
    eq(ho.length, 80, 'size');
    sameBytes(ho, hx(s.handover), 'handover');
    sameBytes(await S.openHandover(app, account, s.code_challenge, ho), hx(v.backup_key), 'opened');
  });
  await c.check('sign-in hand-over refused with another challenge', () => refused(() => S.openHandover(app, account, s.refused_with_code_challenge, hx(s.handover)), 'another challenge'));
  const p = v.pairing;
  await c.check('pairing hand-over', async () => {
    sameBytes(await S.seal(hx(v.app_box_key), S.labelSealPairing, S.idBytes(account), hx(v.backup_key), hx(p.one_time_private)), hx(p.inner), 'inner');
    const ho = await S.sealPairingHandover(hx(v.app_box_key), hx(p.pairing_secret), account, hx(v.backup_key), hx(p.one_time_private), hx(p.salt));
    eq(ho.length, 112, 'size');
    sameBytes(ho, hx(p.handover), 'handover');
    sameBytes(await S.openPairingHandover(app, hx(p.pairing_secret), account, ho), hx(v.backup_key), 'opened');
  });
  await c.check('pairing hand-over refused with another secret', () => refused(() => S.openPairingHandover(app, hx(p.refused_with_pairing_secret), account, hx(p.handover)), 'another secret'));
};

files['unlock_request.json'] = async (c) => {
  const v = load('unlock_request.json');
  const f = v.fields;
  await c.check('request and code', async () => {
    const u = { format: f.format, accountId: f.account_id, deviceId: f.device_id, signKey: hx(f.sign_key), boxKey: hx(f.box_key), platform: f.platform, at: f.at, proof: hx(f.proof) };
    sameBytes(S.encodeUnlockRequest(u), hx(v.request), 'request');
    await S.checkUnlockRequest(hx(v.request));
    sameBytes(await S.hashOf(S.labelDeviceApproval, hx(v.request)), hx(v.code_hash), 'code hash');
    eq(await S.approvalCode(hx(v.request)), v.code, 'code');
  });
  for (const t of v.typed_forms_that_match) await c.check(`typed "${t}"`, async () => eq(S.normalizeApprovalCode(t), v.code, 'normalised'));
  await c.check('answer', async () => {
    const a = v.answer;
    const key = await S.SigningKey.fromSeed(hx(a.account_key_seed));
    const cp = await S.makeLockedCopy(hx(f.box_key), f.account_id, a.key_version, S.WAY_DEVICE, f.device_id, hx(a.backup_key), key, hx(a.one_time_private));
    sameBytes(cp, hx(a.locked_copy), 'locked copy');
    sameBytes(await S.openLockedCopy(hx(a.device_box_private), f.account_id, a.key_version, S.WAY_DEVICE, f.device_id, cp), hx(a.backup_key), 'opened');
  });
  for (const x of v.refusals) {
    await c.check('refused: ' + x.why, async () => {
      await refused(() => S.checkUnlockRequest(hx(x.request)), x.why);
      eq(await S.approvalCode(hx(x.request)), x.code, 'its code');
      yes(x.code !== v.code, 'another request has another code');
    });
  }
};

files['comparison_code.json'] = async (c) => {
  const v = load('comparison_code.json');
  eq(v.rounds, S.COMPARE_ROUNDS, 'rounds');
  for (const h of v.halves) {
    await c.check(`half ${h.account_id} ${h.server}`, async () => {
      if (h.first_hash) sameBytes(await S.hashOf(S.labelCompare, S.concat(hx(h.account_key), S.idBytes(h.account_id), enc.encode(h.server))), hx(h.first_hash), 'first hash');
      eq(await S.compareHalf(hx(h.account_key), h.account_id, h.server), h.half, 'half');
    });
  }
  await c.check('code', async () => {
    eq(S.compareCode(v.halves[0].half, v.halves[1].half), v.code, 'code');
    eq(S.compareCode(v.halves[1].half, v.halves[0].half), v.code, 'from the other side');
  });
};

files['call_keys.json'] = async (c) => {
  const v = load('call_keys.json');
  const conv = v.conversation_id, k = v.key_epoch;
  const secret = await S.callSecret(hx(v.epoch_secret), conv, k, v.call_id);
  await c.check('salt and call secret', async () => {
    sameBytes(S.epochSalt(conv, k), hx(v.salt), 'salt');
    sameBytes(secret, hx(v.call_secret), 'call secret');
  });
  for (const [dev, want] of Object.entries(v.call_sender_keys)) await c.check('call sender key ' + dev, async () => sameBytes(await S.callSenderKey(secret, conv, k, dev), hx(want), 'key'));
  await c.check('deriveCallKeys, as the calls code gets them', async () => {
    const got = await S.deriveCallKeys(hx(v.epoch_secret), conv, k, v.call_id, Object.keys(v.call_sender_keys));
    for (const [dev, want] of Object.entries(v.call_sender_keys)) sameBytes(got.keys.get(dev), hx(want), 'key ' + dev);
    eq(got.keyIndex, S.callKeyIndex(k), 'index');
  });
  for (const x of v.key_index) await c.check('key index ' + x.key_epoch, async () => eq(S.callKeyIndex(x.key_epoch), x.index, 'index'));
};

// runVectors runs every file. lib is the library (shared/sealed/index.js);
// vectors(name) gives a vector file's parsed JSON.
export async function runVectors(lib, vectors, log = console.log) {
  S = lib;
  load = vectors;
  let passed = 0, total = 0;
  const failures = [];
  for (const [name, fn] of Object.entries(files)) {
    const c = new Cases(name);
    try { await fn(c); } catch (e) { c.failures.push('the file could not be run: ' + (e.stack || e)); }
    log(`VECTORS ${name}: ${c.passed}/${c.total} passed`);
    passed += c.passed;
    total += c.total;
    for (const f of c.failures) failures.push(`${name}: ${f}`);
  }
  return { passed, total, failures };
}

export const VECTOR_FILES = Object.keys(files);

if (typeof process !== 'undefined' && process.argv && process.argv[1] && import.meta.url.endsWith(process.argv[1].split('/').pop())) {
  const { readFileSync } = await import('node:fs');
  const dir = new URL('../testdata/sealed/', import.meta.url);
  const lib = await import('../internal/web/static/shared/sealed/index.js');
  const { passed, total, failures } = await runVectors(lib, (name) => JSON.parse(readFileSync(new URL(name, dir), 'utf8')));
  for (const f of failures) console.log('FAIL ' + f);
  console.log(`VECTORS total: ${passed}/${total} passed`);
  if (failures.length) process.exitCode = 1;
}
