// The seam between the web chat and sealed chats (docs/SEALED.md section
// 14). Everything else in the chat reaches sealed chats through these:
//
//   mountSealed(view, conv)          a sealed chat in a ConversationView
//   startSealedChat(accountId)       find or start the sealed chat with someone
//   newSealedChat()                  pick a person, then the above
//   callFrameKeys(convId, callId)    the call sender keys for a call (calls)
//   signOutSealed()                  leave the device list and wipe, before sign-out
//   lockSealed()                     become a locked device, before the session is locked
//
// The cryptography is the library in /shared/sealed, checked against the
// test vectors; the browser's device is /shared/sealed/session.js.

import { h, icon, toast, openDialog, button } from '/shared/dom.js';
import { pickAccounts } from '/shared/pick.js';
import { signOutSealed as wipeSealed } from '/shared/sealed/session.js';
import { store, conversation, needAccounts } from '../store.js';
import { ui } from '../ui.js';
import { session, otherOf } from './state.js';
import { SealedChat } from './view.js';
import { setupPanel, loadStyles } from './setup.js';
import { registerFrameKeys } from '../call/frame-keys.js';

export function mountSealed(view, conv) {
  if (view.sealed) view.sealed.destroy();
  view.sealed = new SealedChat(view, conv);
  return view.sealed;
}

// startSealedChat opens the sealed chat with one person, making it (and
// its epoch record 1) if there is none. A browser that is not unlocked is
// set up first.
export async function startSealedChat(accountId) {
  loadStyles();
  try {
    const s = await session();
    if (s.state !== 'unlocked') {
      const ok = await openDialog('Sealed chats on this browser', (close) => ({ body: setupPanel(s, () => close(true)), foot: [button('Cancel', () => close(false))] }), { wide: true });
      if (!ok || s.state !== 'unlocked') return;
    }
    const conv = await s.startChat(accountId);
    store.conversations.set(conv.id, { ...store.conversations.get(conv.id), ...conv });
    needAccounts(conv.member_ids);
    store.emit('conversations');
    ui.openConversation(conv.id);
  } catch (err) { toast(err.message, true); }
}

export async function newSealedChat() {
  const chosen = await pickAccounts('New sealed chat', { exclude: new Set([store.me.id]), roles: ['owner', 'admin', 'member', 'guest'],
    note: 'A sealed chat is between you and one other person, encrypted on your devices and theirs. This server cannot read it. Nobody can be added.' });
  if (chosen && chosen[0]) await startSealedChat(chosen[0].id);
}

// callFrameKeys is what the calls code asks for a sealed chat's call: the
// call sender key of every device of the current key epoch, derived from
// the epoch secret (sections 5.2 and 10), and the key index frames carry.
// It returns { keyEpoch, keyIndex, deviceId, keys: Map device id → 32 bytes
// }, where deviceId is this browser's own: its frames use keys.get(deviceId)
// and each other device's frames use theirs. When a new epoch begins the
// calls code asks again and keeps the old keys for ten seconds. It throws
// while this browser is not unlocked or holds no current epoch secret.
export async function callFrameKeys(conversationId, callId) {
  const s = await session();
  const c = conversation(conversationId);
  return s.callFrameKeys(conversationId, otherOf(c), callId);
}

// The calls code is given the keys through its one seam
// (chat/call/frame-keys.js). Before a call exists (the check made before
// starting one) there is no call id to derive from: the nil id stands in,
// and the keys it gives are never used, only whether there are any.
const NO_CALL = '00000000-0000-0000-0000-000000000000';
registerFrameKeys((call) => callFrameKeys(call.conversation_id, call.id || NO_CALL));

// lockSealed is what locking Churr does to sealed chats: this browser
// becomes a locked device until the session is unlocked (session.js,
// lockForSessionLock).
export async function lockSealed() {
  if (!store.server || !store.server.features || !store.server.features.sealed_chats) return;
  const s = await session();
  await s.lockForSessionLock();
}

// signOutSealed is the first step of signing out here.
export function signOutSealed() {
  return wipeSealed(store.me, store.server);
}

// sealedNotice is kept for anything that still draws a sealed chat it
// cannot open.
export function sealedNotice() {
  return h('div', { class: 'conv-start sealed' },
    h('h2', {}, icon('sealed'), ' A sealed chat'),
    h('p', {}, 'This is a sealed chat: its messages are encrypted on the two people’s devices, and this server cannot read them.'));
}
