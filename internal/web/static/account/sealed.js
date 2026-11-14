// Sealed chats on the account page (docs/SEALED.md sections 13 and 14):
// whether this browser is unlocked, and unlocking it; the account's
// devices as the signed device list has them, with browsers labelled as
// such; devices waiting for approval; which passkeys can unlock and which
// cannot; and the actions the document gives: allow a passkey, remove a
// device, replace the recovery phrase, replace the keys, start again.

import { get } from '/shared/api.js';
import { h, fill, button, busy, toast, confirmDialog, dateTime } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { Session } from '/shared/sealed/session.js';
import { platformLabel, platformNumber, listDevice } from '/shared/sealed/index.js';
import { setupPanel, showPhraseOnce, answerDialog, waitingRequests, loadStyles, R43, BROWSER } from '/chat/sealed/setup.js';

export default async function sealed(ctx) {
  loadStyles();
  const el = card('Sealed chats');
  const body = h('div', { class: 'stack' }, h('p', { class: 'loading' }, 'Loading'));
  el.append(body);
  if (!ctx.server.features.sealed_chats) {
    fill(body, h('p', {}, 'A sealed chat is between two people and is encrypted on their devices, so this server cannot read it.'), h('p', { class: 'note' }, 'Sealed chats are turned off on this server.'));
    return el;
  }
  let s;
  try { s = await Session.open(ctx.me, ctx.server); } catch (err) { fill(body, h('p', { class: 'problem' }, err.message)); return el; }
  const draw = async () => {
    let backup, devices, passkeys;
    try {
      [backup, devices, passkeys] = await Promise.all([s.backup(), get('/me/devices').then((r) => r.devices || []), get('/me/passkeys').then((r) => r.passkeys || [])]);
    } catch (err) { fill(body, h('p', { class: 'problem' }, err.message)); return; }
    const unlocked = s.state === 'unlocked';
    if (unlocked) await s.locked((d) => s.syncLists(d, d.account)).catch(() => {});
    const pin = unlocked ? s.device.pins.get(ctx.me.id) : null;
    const listed = pin && pin.newestList ? pin.newestList.list : null;
    const parts = [h('p', {}, 'A sealed chat is between two people and is encrypted on their devices, so this server cannot read it.')];

    // This browser.
    if (unlocked) {
      parts.push(h('p', { class: 'note' }, 'This browser is unlocked: it reads and writes your sealed chats. Signing out here takes it out of your device list and deletes its keys.'));
    } else {
      parts.push(setupPanel(s, () => draw()));
    }

    // Devices.
    if (backup) {
      const rows = devices.map((d) => {
        const entry = listed && listDevice(listed, d.id);
        const kind = entry ? platformLabel(entry.platform) : platformLabel(platformNumber(d.platform));
        const isMe = s.device && d.id === s.device.id;
        const remove = unlocked && !isMe && entry ? button('Remove', async () => {
          const yes = await confirmDialog('Remove this device?', [`“${d.name}” is signed out and taken out of your sealed chats.`, 'Because it held your keys, they are replaced: your other devices carry on, and the other person in each chat sees that your security key changed.'], { yes: 'Remove', danger: true });
          if (yes) await busy(remove, async () => { await s.revokeDevice(d.id); toast('Removed, and the keys replaced.'); draw(); });
        }, 'small danger') : null;
        return h('li', { dataset: { device: d.id } }, h('div', { class: 'what' }, h('strong', {}, d.name, isMe ? ' (this browser)' : ''),
          h('small', {}, [kind === 'a browser' ? 'A browser' : kind, entry ? 'in your sealed-chat device list' : listed ? 'not in the device list' : null, d.retired_at ? 'retired ' + dateTime(d.retired_at) : null].filter(Boolean).join(' · '))), remove);
      });
      parts.push(h('h3', {}, 'Devices'), h('p', { class: 'dim small' }, 'The other person in a sealed chat sees what kind each of your devices is, and that a browser is one of them. ' + BROWSER),
        rows.length ? h('ul', { class: 'sealed-list', 'aria-label': 'Devices' }, rows) : h('p', { class: 'dim' }, 'No devices.'));
      if (unlocked) {
        try {
          const waiting = await waitingRequests(s, get);
          if (waiting.length) {
            parts.push(h('h3', {}, 'Waiting for approval'), h('ul', { class: 'sealed-list' }, waiting.map((u) => h('li', {}, h('div', { class: 'what' }, h('strong', {}, (u.device && u.device.name) || 'A new device'), h('small', {}, 'asked ' + dateTime(u.created_at))),
              button('Approve…', () => answerDialog(s, u).then((ok) => ok && draw()), 'small primary')))));
          }
        } catch { /* listed again on the next visit */ }
      }
    }

    // Passkeys.
    const pk = passkeys.map((p) => {
      const allow = unlocked && p.has_prf && !p.can_unlock ? button('Let it unlock', async () => {
        const yes = await confirmDialog(`Let the passkey “${p.name}” unlock sealed chats?`, ['Say yes only for a passkey you have just added yourself. ' + (p.unlock_public ? '' : 'You will be asked to touch it.'), R43], { yes: 'Let it unlock' });
        if (yes) await busy(allow, async () => { await s.allowPasskey(p); toast('That passkey can now unlock sealed chats.'); draw(); });
      }, 'small') : null;
      const give = p.has_prf && !p.unlock_public ? button('Give its unlock key', () => busy(give, async () => {
        const got = await s.giveUnlockKey();
        toast(`“${got.name}” has given its unlock key. An unlocked device of yours can now let it unlock.`);
        draw();
      }), 'small') : null;
      return h('li', {}, h('div', { class: 'what' }, h('strong', {}, p.name),
        h('small', {}, p.can_unlock ? 'can unlock sealed chats' : p.has_prf ? (p.unlock_public ? 'can be allowed to unlock sealed chats' : 'supports unlocking; touch it once to give its unlock key') : 'signs in · cannot unlock sealed chats')), unlocked ? null : give, allow);
    });
    parts.push(h('h3', {}, 'Passkeys'), h('p', { class: 'dim small' }, R43), pk.length ? h('ul', { class: 'sealed-list', 'aria-label': 'Passkeys' }, pk) : h('p', { class: 'dim' }, 'No passkeys.'));

    // Actions.
    if (backup && unlocked) {
      const phrase = button('Replace the recovery phrase', async () => {
        if (!(await confirmDialog('Replace the recovery phrase?', 'The old words stop working. Your keys are replaced too, and the other person in each chat sees that your security key changed.', { yes: 'Replace' }))) return;
        await busy(phrase, async () => { const words = await s.replaceKeys({ newPhrase: true }); if (words) await showPhraseOnce(words, 'Your new recovery phrase'); draw(); });
      }, 'small');
      const keys = button('Replace the keys', async () => {
        if (!(await confirmDialog('Replace your sealed-chat keys?', 'Do this if you think someone has had your recovery phrase, a passkey or a device. Your devices and passkeys carry on; the other person in each chat sees that your security key changed.', { yes: 'Replace' }))) return;
        await busy(keys, async () => { await s.replaceKeys({}); toast('The keys are replaced.'); draw(); });
      }, 'small');
      parts.push(h('h3', {}, 'Keys'), h('p', { class: 'row' }, phrase, keys), backup.replacement_due ? h('p', { class: 'note warn' }, 'A device or passkey was removed where no unlocked device was at hand. Replace the keys now.') : null);
    }
    if (backup && !unlocked) {
      const reset = button('Start again…', async () => {
        const yes = await confirmDialog('Start sealed chats again?', ['Only for when no passkey, no recovery phrase and no unlocked device is left. Earlier sealed messages can then never be read again on any of your devices, and everyone you have a sealed chat with is told your sealed-chat identity was reset.'], { yes: 'Start again', danger: true, type: 'start again' });
        if (yes) await busy(reset, async () => { const words = await resetAccount(s); await showPhraseOnce(words); draw(); });
      }, 'small danger');
      parts.push(h('details', {}, h('summary', {}, 'No way of unlocking left?'), h('p', { class: 'row' }, reset)));
    }
    fill(body, parts);
  };
  s.on((w) => { if (w === 'state') draw(); });
  draw();
  return el;
}

// resetAccount is section 13.8: new keys and phrase with the next key
// version, and a device list marked reset holding only this browser.
async function resetAccount(s) {
  const { post } = await import('/shared/api.js');
  const { toB64, decodeList, WAY_NAMES, WAY_PASSKEY, WAY_DEVICE } = await import('/shared/sealed/index.js');
  await s.ensureDevice();
  const words = await s.locked(async (d) => {
    await s.syncLists(d, d.account);
    const b = await d.resetBackup();
    const copy = (c) => { const o = { kind: WAY_NAMES[c.kind], key_version: b.keyVersion, locked_key: toB64(c.locked) }; if (c.kind === WAY_PASSKEY) o.passkey_id = c.id; if (c.kind === WAY_DEVICE) o.device_id = c.id; return o; };
    const list = { version: decodeList(b.list.list).version, list: toB64(b.list.list), sig: toB64(b.list.sig) };
    await post('/me/backup/reset', { account_record: { key_version: b.keyVersion, revision: b.revision, record: toB64(b.accountRecord) }, locked_copies: b.copies.map(copy), device_list: list });
    await d.adopt(b);
    return b.phrase;
  });
  await s.newEpochsEverywhere();
  s.announce();
  return words;
}
