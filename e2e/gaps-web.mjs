// Things the web chat does that the server had long supported: joining
// and leaving an open group from the browser (docs/CLIENTS.md 9.15), and
// a member adding a custom emoji where the server allows it (9.5), and
// drafts kept across reloads (section 22; their removal on locking is in
// lock.mjs).

import { step, shot, assert, until } from './lib.mjs';

// call makes one API request from a page, as its person, and returns the
// status and the body.
const call = (page, method, path, body) => page.evaluate(async ([m, p, b]) => {
  const r = await fetch(p, { method: m, headers: b ? { 'content-type': 'application/json' } : {}, body: b ? JSON.stringify(b) : undefined });
  const text = await r.text();
  return { status: r.status, body: text ? JSON.parse(text) : null };
}, [method, path, body]);

const groupOf = async (page, handle) => (await call(page, 'GET', '/api/v1/groups')).body.groups.find((g) => g.handle === handle);

// groupsFlow: the owner makes an open group and a closed one; the second
// person joins the open one from Browse and leaves it again, and is
// offered nothing for the closed one.
export async function groupsFlow(owner, second, origin) {
  const p = second.page;
  await step('groups: an open group is joined and left from Browse', async () => {
    for (const [handle, open] of [['gardeners', true], ['wardens', false]]) {
      const out = await call(owner.page, 'POST', '/api/v1/admin/groups', { handle, name: handle[0].toUpperCase() + handle.slice(1), open });
      assert(out.status === 201, `making @${handle}: ${out.status} ${JSON.stringify(out.body)}`);
    }
    await p.goto(origin + '/chat/browse');
    const list = p.locator('[data-list="groups"]');
    await list.locator('li', { hasText: '@gardeners' }).waitFor();
    assert(await list.locator('button[data-group="wardens"]').count() === 0, 'a closed group was offered to join');
    await shot(p, 'groups-browse');
    await list.locator('button[data-group="gardeners"]', { hasText: 'Join' }).click();
    await until(p, 'the server has sam in @gardeners', async () => {
      const r = await fetch('/api/v1/groups'); return (await r.json()).groups.some((g) => g.handle === 'gardeners' && g.is_member);
    });
    await list.locator('button[data-group="gardeners"]', { hasText: 'Leave' }).click();
    await until(p, 'the server has sam out of @gardeners', async () => {
      const r = await fetch('/api/v1/groups'); return (await r.json()).groups.some((g) => g.handle === 'gardeners' && !g.is_member);
    });
    assert(!(await groupOf(p, 'gardeners')).is_member, 'still in the group after Leave');
  }, p);
}

// A 2 by 2 PNG, for the emoji upload.
const PNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR4nGPYueA/EDFAKABAlglhfi8n8gAAAABJRU5ErkJggg==', 'base64');

async function openGeneral(page, origin) {
  await page.goto(origin + '/chat/');
  await page.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
  await page.locator('#main .editor').waitFor();
}

// pickerOffersAdd opens the composer's emoji picker, says whether it has
// "Add emoji", and closes it again unless asked to leave it open.
async function pickerOffersAdd(page, leaveOpen) {
  await page.locator('#main').getByRole('button', { name: 'Emoji', exact: true }).click();
  const picker = page.getByRole('dialog', { name: 'Choose an emoji' });
  await picker.waitFor();
  const offers = await picker.getByRole('button', { name: 'Add emoji' }).count() === 1;
  if (!leaveOpen) await page.keyboard.press('Escape');
  return offers;
}

async function settingsOffersAdd(page) {
  await page.getByRole('button', { name: 'Web chat settings' }).click();
  const dlg = page.getByRole('dialog', { name: 'Web chat settings' });
  await dlg.waitFor();
  const offers = await dlg.locator('[data-part="emoji"]').getByRole('button', { name: 'Add emoji' }).count() === 1;
  await dlg.getByRole('button', { name: 'Done' }).click();
  return offers;
}

const setMembersMayAdd = async (owner, on) => {
  const out = await call(owner.page, 'PATCH', '/api/v1/admin/settings', { 'emoji.members_may_add': on });
  assert(out.status === 200, `setting emoji.members_may_add: ${out.status} ${JSON.stringify(out.body)}`);
};

// emojiFlow: the second person, a member, is offered "Add emoji" only
// while the server allows members to add emoji, adds one from the picker
// and has it in the composer at once.
export async function emojiFlow(owner, second, origin) {
  const p = second.page;
  await step('emoji: a member is not offered Add emoji while the setting is off', async () => {
    await setMembersMayAdd(owner, false);
    await openGeneral(p, origin);
    assert(!(await pickerOffersAdd(p)), 'the picker offered Add emoji');
    assert(!(await settingsOffersAdd(p)), 'the settings offered Add emoji');
  }, p);
  await step('emoji: with the setting on, a member adds one from the picker', async () => {
    await setMembersMayAdd(owner, true);
    await openGeneral(p, origin);
    assert(await settingsOffersAdd(p), 'the settings did not offer Add emoji');
    assert(await pickerOffersAdd(p, true), 'the picker did not offer Add emoji');
    await p.getByRole('dialog', { name: 'Choose an emoji' }).getByRole('button', { name: 'Add emoji' }).click();
    const dlg = p.getByRole('dialog', { name: 'Add an emoji' });
    await dlg.getByLabel('Name').fill('gecko');
    await dlg.getByLabel('Picture').setInputFiles({ name: 'gecko.png', mimeType: 'image/png', buffer: PNG });
    await shot(p, 'emoji-add');
    await dlg.getByRole('button', { name: 'Add' }).click();
    await dlg.waitFor({ state: 'detached' });
    await until(p, 'the composer has the new emoji', () => document.querySelector('#main .editor').textContent.includes(':gecko:'));
    const mine = (await call(p, 'GET', '/api/v1/emoji')).body.emoji.find((e) => e.name === 'gecko');
    assert(mine, 'the server has no :gecko:');
    // Left as the run found it: the emoji gone, the setting off.
    await p.evaluate(() => { const ed = document.querySelector('#main .editor'); ed.textContent = ''; ed.dispatchEvent(new Event('input', { bubbles: true })); });
    const gone = await call(owner.page, 'DELETE', '/api/v1/admin/emoji/' + mine.id);
    assert(gone.status === 200 || gone.status === 204, `removing :gecko:: ${gone.status}`);
    await setMembersMayAdd(owner, false);
  }, p);
}

const storedDrafts = (page) => page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('churr-drafts.')).map((k) => localStorage.getItem(k)).join(' '));

// draftsFlow: a draft in a channel and one in a thread come back after a
// reload, each in its own place; sending one removes it.
export async function draftsFlow(owner, second, origin) {
  const p = second.page;
  await step('drafts: a draft comes back after a reload, and goes when sent', async () => {
    await openGeneral(p, origin);
    const ed = p.locator('#main .editor');
    await ed.click();
    await p.keyboard.type('a draft that outlives the page');
    await until(p, 'the draft is stored', () => Object.keys(localStorage).some((k) => k.startsWith('churr-drafts.') && localStorage.getItem(k).includes('outlives')));
    await p.reload();
    await p.locator('#main .editor').waitFor();
    await until(p, 'the draft is back in the box', () => document.querySelector('#main .editor').textContent.includes('a draft that outlives the page'));
    await shot(p, 'drafts-kept');
    await p.locator('#main .editor').press('Enter');
    await p.locator('#main .msg', { hasText: 'a draft that outlives the page' }).first().waitFor();
    await until(p, 'the sent draft is no longer stored', () => !Object.keys(localStorage).some((k) => k.startsWith('churr-drafts.') && localStorage.getItem(k).includes('outlives')));
    await p.reload();
    await p.locator('#main .editor').waitFor();
    assert(!(await p.locator('#main .editor').textContent()).includes('outlives'), 'a sent draft came back');
    assert(!(await storedDrafts(p)).includes('outlives'), 'a sent draft is still stored');
  }, p);
}
