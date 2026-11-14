// The page an app opens to unlock sealed chats with a passkey
// (docs/SEALED.md section 13.3, docs/CLIENTS.md section 15).
//
// A passkey only works at the server's own web address, so this way of
// unlocking passes through a page the server supplies. The page asks for
// one touch of a passkey that has a locked copy, opens the backup key in
// the browser, and stores a locked copy for the app's device, which the
// app then finds in its backup. The PRF output, the unlock key and the
// backup key never leave this page: the server is sent only the new copy,
// which it cannot open. The cryptography is in sealed.js.

import { h } from '/shared/dom.js';
import { get, post } from '/shared/api.js';
import { frame, loadServer, whoAmI, problemBox, noPasskeys } from '/shared/door.js';
import * as sealed from '/unlock/sealed.js';

const deviceID = new URLSearchParams(location.search).get('device_id') || '';

// touch runs a passkey check with the PRF extension and returns the
// credential's identifier and its PRF output. The check is also sent to
// the server, without the PRF output, so the session counts as freshly
// checked.
async function touch() {
  const begin = await post('/checks/passkey/begin');
  const opts = structuredClone(begin.public_key);
  opts.challenge = sealed.fromB64(opts.challenge);
  for (const c of opts.allowCredentials || []) c.id = sealed.fromB64(c.id);
  opts.extensions = { ...(opts.extensions || {}), prf: { eval: { first: await sealed.prfInput() } } };
  const cred = await navigator.credentials.get({ publicKey: opts });
  const ext = cred.getClientExtensionResults ? cred.getClientExtensionResults() : {};
  const first = ext.prf && ext.prf.results && ext.prf.results.first;
  await post('/checks/passkey/finish', {
    ceremony_id: begin.ceremony_id,
    credential: {
      id: cred.id, rawId: sealed.toB64(new Uint8Array(cred.rawId)), type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment || undefined,
      clientExtensionResults: {},
      response: {
        clientDataJSON: sealed.toB64(new Uint8Array(cred.response.clientDataJSON)),
        authenticatorData: sealed.toB64(new Uint8Array(cred.response.authenticatorData)),
        signature: sealed.toB64(new Uint8Array(cred.response.signature)),
        userHandle: cred.response.userHandle ? sealed.toB64(new Uint8Array(cred.response.userHandle)) : undefined,
      },
    },
  });
  if (!first) throw new Error('That passkey cannot unlock sealed chats: its passkey store gave no unlocking secret. Use the recovery phrase, or another device, in the app.');
  return { credentialID: new Uint8Array(cred.rawId), prfOutput: new Uint8Array(first) };
}

// unlock is the whole flow once the button is pressed.
async function unlock(account, device) {
  const { credentialID, prfOutput } = await touch();
  // Which passkey was touched: the one whose unlock public key the PRF
  // output derives.
  const unlockPublic = sealed.toB64(await sealed.unlockPublicFromPRF(prfOutput, account.id, credentialID));
  const passkeys = (await get('/me/passkeys')).passkeys || [];
  const passkey = passkeys.find((p) => p.unlock_public === unlockPublic);
  let backup;
  try {
    backup = (await get('/me/backup')).backup;
  } catch (err) {
    if (err.status === 404) throw new Error('This account has no sealed chats to unlock yet.');
    throw err;
  }
  const copy = passkey && backup.locked_copies.find((c) => c.kind === 'passkey' && c.passkey_id === passkey.id);
  if (!copy) {
    throw new Error('That passkey has not been allowed to unlock sealed chats yet. A device that is already unlocked must approve it first. Until then, use the recovery phrase or another device in the app.');
  }
  const lists = (await get('/accounts/' + account.id + '/device-lists')).versions || [];
  if (!lists.length) throw new Error('This account has no device list yet.');
  const newest = lists[lists.length - 1];
  const made = await sealed.unlockForDevice({
    accountID: account.id, prfOutput, credentialID, passkeyID: passkey.id,
    copy: sealed.fromB64(copy.locked_key), keyVersion: backup.key_version, revision: backup.account_record.revision,
    accountRecord: sealed.fromB64(backup.account_record.record), list: sealed.fromB64(newest.list), listSig: sealed.fromB64(newest.sig),
    deviceID: device.id, deviceBoxKey: sealed.fromB64(device.box_key),
  });
  await post('/me/backup/copies', {
    locked_copy: { kind: 'device', device_id: device.id, key_version: backup.key_version, locked_key: sealed.toB64(made) },
  });
}

async function main() {
  const server = await loadServer();
  const who = await whoAmI();
  if (!who.account) {
    const back = encodeURIComponent(location.pathname + location.search);
    frame(server,
      h('h1', {}, 'Sign in to ', h('em', {}, 'unlock')),
      h('p', {}, 'Sign in here first, with the same account as in the app. Then this page can unlock the app\'s sealed chats with a passkey.'),
      h('p', { class: 'row' }, h('a', { class: 'btn primary', href: '/login/?next=' + back }, 'Sign in')));
    return;
  }
  let device = null;
  try {
    device = ((await get('/me/devices')).devices || []).find((d) => d.id === deviceID && d.box_key && !d.retired_at);
  } catch { /* told below */ }
  if (!device) {
    frame(server,
      h('h1', {}, 'Nothing to ', h('em', {}, 'unlock')),
      h('p', {}, 'This page was opened for a device that is not one of this account\'s, or has not registered its keys. Go back to the app and try again.'));
    return;
  }
  const problem = problemBox();
  const go = h('button', { type: 'button', class: 'btn primary' }, 'Unlock with a passkey');
  go.addEventListener('click', async () => {
    problem.show(null);
    go.disabled = true;
    try {
      await unlock(who.account, device);
      frame(server,
        h('h1', {}, 'Sealed chats are ', h('em', {}, 'unlocked')),
        h('p', {}, h('strong', {}, device.name), ' can now read this account\'s sealed chats. Go back to the app; you can close this page.'));
    } catch (err) {
      go.disabled = false;
      problem.show(err);
    }
  });
  frame(server,
    h('h1', {}, 'Unlock sealed ', h('em', {}, 'chats')),
    h('p', {}, 'Let ', h('strong', {}, device.name), ' read the sealed chats of ', h('strong', {}, who.account.username), '? Touch a passkey that is allowed to unlock them.'),
    h('p', { class: 'note' }, 'The key is opened in this browser and handed to the app sealed, so the server is never sent it. But this page comes from the server: a passkey works only at the server\'s own web address, so unlocking with a passkey always passes through a page the server supplies, for the apps as for the web chat. A dishonest server could serve a page that keeps the key. The recovery phrase, or approval from another app, does not pass through a server page.'),
    noPasskeys(), problem,
    h('p', { class: 'row' }, go));
}

main();
