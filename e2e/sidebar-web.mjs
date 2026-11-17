// Each person's own sidebar in the web chat (ruling R83, docs/FEATURES.md
// section 21.4): make a section, move channels into it, drag to reorder,
// Alt+Up, switch the heading to last activity, reload and find it kept,
// and see a second browser session follow along live.

import { step, assert, SHOTS, DESKTOP, PHONE } from './lib.mjs';

const call = (page, method, path, body) => page.evaluate(async ([m, p, b]) => {
  const r = await fetch('/api/v1' + p, { method: m, headers: { 'Content-Type': 'application/json' }, body: b === undefined ? undefined : JSON.stringify(b) });
  return r.status === 204 ? {} : r.json();
}, [method, path, body]);

const group = (page, name) => page.locator('.side-group').filter({ has: page.locator('h2 .sec', { hasText: name }) });
const rowsOf = (page, name) => group(page, name).locator('.side-row:not(.child) .row-name').allTextContents();
const row = (page, name) => page.locator('.side-row').filter({ hasText: name }).first();

async function waitOrder(page, heading, want) {
  await page.waitForFunction(([h, w]) => {
    const g = [...document.querySelectorAll('.side-group')].find((x) => x.querySelector('h2 .sec .grow')?.textContent === h);
    return g && [...g.querySelectorAll('.side-row:not(.child) .row-name')].map((x) => x.textContent).join(',') === w;
  }, [heading, want.join(',')], { timeout: 8000 }).catch(async () => {
    throw new Error(`${heading}: want ${want.join(',')}, have ${(await rowsOf(page, heading)).join(',')}`);
  });
}

export async function sidebarFlow(kaity, browser, origin) {
  const a = kaity.page;
  const ids = {};
  let b = null;

  await step('sidebar: make a section of one\'s own from New…', async () => {
    await a.goto(origin + '/chat/');
    await a.locator('.conn.live').waitFor();
    await call(a, 'POST', '/me/sidebar', { version: (await call(a, 'GET', '/me/sidebar')).sidebar.version, ops: [{ op: 'reset_all' }] });
    for (const n of ['sb-alpha', 'sb-bravo', 'sb-charlie']) ids[n] = (await call(a, 'POST', '/channels', { name: n, private: true })).conversation.id;
    await row(a, 'sb-charlie').waitFor();
    await a.locator('.side-bottom button', { hasText: 'New…' }).click();
    await a.getByRole('menuitem', { name: 'New section' }).click();
    await a.getByLabel('Name').fill('Focus');
    await a.getByRole('button', { name: 'Create' }).click();
    await group(a, 'Focus').locator('.side-drop').waitFor();
    await a.waitForFunction(() => /Section Focus made/.test(document.getElementById('announcer').textContent));
  }, a);

  await step('sidebar: move channels into it with Move… and by dragging', async () => {
    for (const n of ['sb-bravo', 'sb-alpha']) {
      await row(a, n).hover();
      await row(a, n).locator('.row-menu').click();
      await a.getByRole('menuitem', { name: 'Move…' }).click();
      await a.getByRole('menuitemradio', { name: 'Focus' }).click();
      await group(a, 'Focus').locator('.side-row', { hasText: n }).waitFor();
    }
    await waitOrder(a, 'Focus', ['sb-alpha', 'sb-bravo']);
    await a.waitForFunction(() => /sb-alpha moved to Focus/.test(document.getElementById('announcer').textContent));
    // Dropped on the section's heading, a row goes in at its end.
    await row(a, 'sb-charlie').dragTo(group(a, 'Focus').locator('h2'), { targetPosition: { x: 40, y: 10 } });
    await group(a, 'Focus').locator('.side-row', { hasText: 'sb-charlie' }).waitFor();
    await waitOrder(a, 'Focus', ['sb-alpha', 'sb-bravo', 'sb-charlie']);
  }, a);

  await step('sidebar: drag to reorder, then Alt+Up', async () => {
    await group(a, 'Focus').locator('.side-row', { hasText: 'sb-charlie' }).dragTo(group(a, 'Focus').locator('.side-row', { hasText: 'sb-alpha' }), { targetPosition: { x: 40, y: 4 } });
    await waitOrder(a, 'Focus', ['sb-charlie', 'sb-alpha', 'sb-bravo']);
    await group(a, 'Focus').locator('.side-row', { hasText: 'sb-bravo' }).getByRole('link').focus();
    await a.keyboard.press('Alt+ArrowUp');
    await waitOrder(a, 'Focus', ['sb-charlie', 'sb-bravo', 'sb-alpha']);
    await a.waitForFunction(() => /sb-bravo is in Focus, 2 of 3/.test(document.getElementById('announcer').textContent));
    const focused = await a.evaluate(() => document.activeElement.closest('.side-row')?.textContent || '');
    assert(focused.includes('sb-bravo'), 'focus stays on the moved row: ' + focused);
    const l = (await call(a, 'GET', '/me/sidebar')).sidebar;
    assert(l.groups.some((g) => g.key.startsWith('p:') && g.sort === 'custom'), 'the section is sorted by hand now: ' + JSON.stringify(l.groups));
  }, a);

  await step('sidebar: a second browser session sees it, and follows live', async () => {
    const context = await browser.newContext({ viewport: DESKTOP, timezoneId: 'UTC', locale: 'en-GB', colorScheme: 'dark', storageState: await kaity.context.storageState() });
    b = await context.newPage();
    await b.goto(origin + '/chat/');
    await b.locator('.conn.live').waitFor();
    await waitOrder(b, 'Focus', ['sb-charlie', 'sb-bravo', 'sb-alpha']);
  }, a);

  await step('sidebar: the heading menu switches to last activity', async () => {
    await call(a, 'POST', `/conversations/${ids['sb-alpha']}/messages`, { text: 'newest here' });
    await a.waitForTimeout(300);
    await group(a, 'Focus').locator('h2').hover();
    await group(a, 'Focus').locator('.sec-menu').click();
    await a.locator('.menu').waitFor();
    await a.screenshot({ path: SHOTS + 'sidebar-section-menu.png' });
    await a.getByRole('menuitemradio', { name: 'Last activity' }).click();
    await waitOrder(a, 'Focus', ['sb-alpha', 'sb-charlie', 'sb-bravo']);
    if (b) await waitOrder(b, 'Focus', ['sb-alpha', 'sb-charlie', 'sb-bravo']);
  }, a);

  await step('sidebar: reloading keeps all of it', async () => {
    await a.reload();
    await a.locator('.conn.live').waitFor();
    await waitOrder(a, 'Focus', ['sb-alpha', 'sb-charlie', 'sb-bravo']);
    await a.screenshot({ path: SHOTS + 'sidebar-personal.png' });
  }, a);

  await step('sidebar: on a phone, a long press starts reordering', async () => {
    const context = await browser.newContext({ viewport: PHONE, hasTouch: true, isMobile: true, timezoneId: 'UTC', locale: 'en-GB', colorScheme: 'dark', storageState: await kaity.context.storageState() });
    const p = await context.newPage();
    await p.goto(origin + '/chat/');
    await p.locator('.conn.live').waitFor();
    const r = p.locator('.side-row', { hasText: 'sb-bravo' }).first();
    await r.waitFor();
    await r.dispatchEvent('touchstart');
    await p.waitForTimeout(700);
    await r.dispatchEvent('touchend');
    await p.locator('.reorder-bar').waitFor();
    await p.locator('.side-row', { hasText: 'sb-bravo' }).first().getByRole('button', { name: 'Move sb-bravo up' }).click();
    await waitOrder(p, 'Focus', ['sb-alpha', 'sb-bravo', 'sb-charlie']);
    await p.screenshot({ path: SHOTS + 'sidebar-phone-reorder.png' });
    await p.getByRole('button', { name: 'Done' }).click();
    await context.close();
  }, a);

  await step('sidebar: any channel goes into any section, an admin\'s too', async () => {
    const sec = (await call(a, 'POST', '/admin/sections', { name: 'Teams' })).section;
    ids.sec = sec.id;
    await group(a, 'Focus').waitFor();
    await row(a, 'sb-bravo').hover();
    await row(a, 'sb-bravo').locator('.row-menu').click();
    await a.getByRole('menuitem', { name: 'Move…' }).click();
    // Every section is offered: the person's own, the admin's, Channels.
    await a.getByRole('menuitemradio', { name: 'Channels (where the server lists it)' }).waitFor();
    await a.getByRole('menuitemradio', { name: 'Teams' }).click();
    await group(a, 'Teams').locator('.side-row', { hasText: 'sb-bravo' }).waitFor();
    await a.waitForFunction(() => /sb-bravo moved to Teams/.test(document.getElementById('announcer').textContent));
    let l = (await call(a, 'GET', '/me/sidebar')).sidebar;
    assert(l.placements.some((p) => p.conversation_id === ids['sb-bravo'] && p.key === 's:' + sec.id), 'placed in the admin section: ' + JSON.stringify(l.placements));
    // Dragged onto Channels, where the server lists it, the placement goes.
    await group(a, 'Teams').locator('.side-row', { hasText: 'sb-bravo' }).dragTo(group(a, 'Channels').locator('h2'), { targetPosition: { x: 40, y: 10 } });
    await group(a, 'Channels').locator('.side-row', { hasText: 'sb-bravo' }).waitFor();
    l = (await call(a, 'GET', '/me/sidebar')).sidebar;
    assert(!l.placements.some((p) => p.conversation_id === ids['sb-bravo']), 'back where the server lists it: ' + JSON.stringify(l.placements));
  }, a);

  await step('sidebar: Reset my sidebar goes back to the server\'s default', async () => {
    await group(a, 'Focus').locator('h2').hover();
    await group(a, 'Focus').locator('.sec-menu').click();
    await a.getByRole('menuitem', { name: 'Reset my sidebar…' }).click();
    await a.getByRole('button', { name: 'Reset' }).click();
    await group(a, 'Focus').waitFor({ state: 'detached' });
    if (b) await group(b, 'Focus').waitFor({ state: 'detached' });
    for (const [k, n] of Object.entries(ids)) if (k !== 'sec') await call(a, 'POST', `/conversations/${n}/leave`);
    if (ids.sec) await call(a, 'DELETE', `/admin/sections/${ids.sec}`);
  }, a);
  if (b) await b.context().close();
}
