// The lock in the web chat (ruling R75, docs/CLIENTS.md section 9.29), in
// Chromium against the real server, after the sealed-chat flow has left
// Kaity and Sam a sealed chat with messages in it:
//
// It is Sam who locks: the sealed flow copied Kaity's passkey to other
// browsers with higher signature counters, so her own authenticator now
// looks like a copy to the server, which refuses it (as it would sign-in).
//
// - /lock in the sealed chat replaces the page with the lock screen: no
//   message, name, avatar or title of the account in the page or its
//   memory, the tab says "Churr", and the server refuses the session;
// - another tab of the same browser lands on the lock screen too;
// - Unlock with the virtual passkey brings the chat back, the earlier
//   sealed messages are readable and a new one is sent and read;
// - Ctrl Shift L locks too, and Unlock comes back the same way.

import { step, shot, assert, noSidewaysScroll, PHONE, DESKTOP } from './lib.mjs';
import { box, say, msg, openSealedChat, settles, inPage } from './web-sealed.mjs';

// readable lists what of the account the page still holds: its words, its
// title, and what the chat's own modules would have kept.
async function leftovers(page, words) {
  return page.evaluate((ws) => {
    const text = document.documentElement.outerHTML + ' ' + document.title;
    const found = ws.filter((w) => text.toLowerCase().includes(w.toLowerCase()));
    if (document.querySelector('#sidebar, .side-row, .sealed-msg, .msg, img.avatar, .avatar')) found.push('chat elements');
    if (window.churrCall) found.push('the chat page is still loaded');
    if (document.title !== 'Churr') found.push('title: ' + document.title);
    return found;
  }, words);
}

export async function lockFlow(kaity, sam, origin) {
  // a locks; b is the other side of the sealed chat.
  const a = sam.page, b = kaity.page;
  let meId = null;
  const words = ['Kaity', 'Sam', 'this is sealed', 'bring the keys', 'sealed chat', 'kaity', 'Okafor'];

  await step('lock: /lock in a sealed chat replaces everything with the lock screen', async () => {
    await openSealedChat(a, origin, 'Kaity');
    await settles(a, msg(a, 'this is sealed'));
    meId = await inPage(a, (s) => s.me.id);
    await box(a).fill('/lock');
    await box(a).press('Enter');
    await a.waitForURL(/\/locked\//);
    await a.getByRole('heading', { name: 'Locked' }).waitFor();
    assert(await a.getByRole('button').count() === 1, 'the lock screen has one button');
    await a.getByRole('button', { name: 'Unlock' }).waitFor();
    const left = await leftovers(a, words);
    assert(left.length === 0, 'the lock screen still holds: ' + left.join(', '));
    // Back does not bring the chat back: the page was replaced.
    await a.goBack().catch(() => {});
    if (!/\/locked\//.test(a.url())) {
      await a.waitForURL(/\/locked\//, { timeout: 8000 });
    }
    // The server refuses the session, the live connection included.
    const r = await a.evaluate(async () => { const x = await fetch('/api/v1/me'); return [x.status, (await x.json()).error.code]; });
    assert(r[0] === 401 && r[1] === 'session_locked', 'the session is not locked on the server: ' + r);
    // The browser is a locked sealed-chat device: no backup key is kept,
    // in memory (the page is gone) or in storage.
    const kept = await a.evaluate(async (id) => {
      const { IdbStore } = await import('/shared/sealed/idb.js');
      const r = await (await IdbStore.open(id)).read('sealed/device');
      return r ? r.data.backup_key : 'no device';
    }, meId);
    assert(kept === null, 'the backup key is still stored: ' + kept);
    await shot(a, '90-locked', { phone: true });
    await a.setViewportSize(PHONE);
    try { await noSidewaysScroll(a); } finally { await a.setViewportSize(DESKTOP); }
  }, a);

  await step('lock: another tab of the same browser is locked too', async () => {
    const tab = await sam.context.newPage();
    try {
      await tab.goto(origin + '/chat/');
      await tab.waitForURL(/\/locked\//);
      await tab.getByRole('heading', { name: 'Locked' }).waitFor();
      const left = await leftovers(tab, words);
      assert(left.length === 0, 'the second tab still holds: ' + left.join(', '));
    } finally { await tab.close(); }
  }, a);

  await step('lock: Unlock with the passkey brings the chat back, earlier sealed messages read, a new one sent', async () => {
    await a.getByRole('button', { name: 'Unlock' }).click();
    await a.waitForURL(/\/chat\//, { timeout: 20000 });
    // Back from the lock screen went to the list (Back above), so the
    // chat is opened from there.
    await openSealedChat(a, origin, 'Kaity');
    await settles(a, msg(a, 'this is sealed'));
    await msg(a, 'bring the keys').waitFor();
    assert(await inPage(a, (s) => s.state) === 'unlocked', 'the sealed chats are not unlocked again');
    await say(a, 'Back after the lock');
    await msg(a, 'Back after the lock').waitFor();
    await openSealedChat(b, origin, 'Sam');
    await settles(b, msg(b, 'Back after the lock'));
    await say(b, 'Welcome back');
    await settles(a, msg(a, 'Welcome back'));
  }, a);

  await step('lock: Ctrl Shift L locks, and Unlock brings the conversation back', async () => {
    await a.keyboard.press('Control+Shift+L');
    await a.waitForURL(/\/locked\//);
    const left = await leftovers(a, words);
    assert(left.length === 0, 'the lock screen still holds: ' + left.join(', '));
    await a.getByRole('button', { name: 'Unlock' }).click();
    await a.waitForURL(/\/chat\//, { timeout: 20000 });
    await settles(a, msg(a, 'Welcome back'));
  }, a);
}
