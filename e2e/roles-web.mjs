// Moderators, managers, owners and the approvals queue in the browser
// (rulings R97 and R99; docs/FEATURES.md sections 21.12 and 31,
// docs/CLIENTS.md sections 16 and 22), with people of their own:
//
//   mia   a member who makes channels, and so manages them; later the
//         owner lets her approve requests
//   moe   made a moderator by mia in the members list; then made an
//         admin by the owner, and still no manager of mia's channel
//   kip   asks to join; later added by owner override
//   zed   applies to join while registrations wait for approval
//
// The server allows ten registrations an hour from one address, and the
// whole run makes others, so this flow makes as few people as it can.
//
// The owner (kaity) works from the admin console: appointing a manager of
// a channel she is not in, and adding someone to a private channel by
// owner override. The settings this flow changes are put back at its end.

import { person, step, shot, assert, until } from './lib.mjs';
import { joinByInvite } from './web.mjs';

// call makes one API request from a page, as its person, and returns the
// status and the body.
const call = (page, method, path, body) => page.evaluate(async ([m, p, b]) => {
  const r = await fetch(p, { method: m, headers: b ? { 'content-type': 'application/json' } : {}, body: b ? JSON.stringify(b) : undefined });
  const text = await r.text();
  return { status: r.status, body: text ? JSON.parse(text) : null };
}, [method, path, body]);

const meOf = async (page) => (await call(page, 'GET', '/api/v1/me')).body.account;

async function openChat(page, origin, id) {
  await page.goto(origin + '/chat/c/' + id);
  await page.locator('.conn.live').waitFor();
}

// fresh does an action that may need a fresh check of who is acting
// (docs/AUTH.md section 10.4): if the dialog asks, the passkey answers,
// until done shows.
async function fresh(page, act, done) {
  await act();
  const dlg = page.getByRole('dialog', { name: 'Check it is you' });
  const end = Date.now() + 20000;
  for (;;) {
    if (await done.isVisible().catch(() => false)) return;
    if (await dlg.isVisible().catch(() => false)) await dlg.getByRole('button', { name: 'Use a passkey' }).click().catch(() => {});
    if (Date.now() > end) throw new Error('the action never finished');
    await page.waitForTimeout(250);
  }
}

// pick chooses one person in the shared people picker.
async function pick(page, title, username, display) {
  const dlg = page.getByRole('dialog', { name: title });
  await dlg.getByLabel('Search people').fill(username);
  await dlg.getByRole('button', { name: new RegExp(display) }).click();
}

// membersOf opens a channel's Members tab in the chat.
async function membersOf(page) {
  await page.locator('#main .conv-head').getByRole('button', { name: /^Members/ }).click();
  const dlg = page.getByRole('dialog', { name: 'Channel' });
  await dlg.locator('ul.rows li[data-account]').first().waitFor();
  return dlg;
}

// consoleRow is a row of a section of the admin console with this text.
const consoleRow = (page, text) => page.locator('.console-main li').filter({ hasText: text }).first();

export async function rolesFlow(owner, browser, origin) {
  const o = owner.page;
  const people = {};
  for (const [name, display] of [['mia', 'Mia Wong'], ['moe', 'Moe Szyslak'], ['kip', 'Kip Lee']]) {
    const out = await call(o, 'POST', '/api/v1/invites', {});
    const p = await person(browser, name, origin);
    await joinByInvite(p, out.body.invite.url, name, display);
    p.id = (await meOf(p.page)).id;
    people[name] = p;
  }
  const { mia, moe, kip } = people;
  const m = mia.page, d = moe.page, k = kip.page, a = moe.page, v = mia.page;
  let knockId = '', townId = '', vaultId = '';

  await step('roles: a manager makes a member a moderator from the members list', async () => {
    const made = await call(m, 'POST', '/api/v1/channels', { name: 'roles-knock', access: 'knock' });
    assert(made.status === 201, 'the knock channel was not made: ' + JSON.stringify(made));
    knockId = made.body.conversation.id;
    const added = await call(m, 'POST', `/api/v1/conversations/${knockId}/members`, { account_ids: [moe.id] });
    assert(added.status < 300, 'moe was not added: ' + JSON.stringify(added));
    await openChat(m, origin, knockId);
    const dlg = await membersOf(m);
    await dlg.locator(`button[data-member="${moe.id}"]`).click();
    await m.getByRole('menuitemradio', { name: 'Moderator' }).click();
    await m.getByText('Moe Szyslak is a moderator of this channel.').first().waitFor();
    await dlg.locator(`li[data-account="${moe.id}"][data-level="moderator"]`).getByText('moderator', { exact: true }).waitFor();
    await shot(m, '90-roles-members-moderator', { fullPage: false });
    await dlg.getByRole('button', { name: 'Done' }).click();
  }, m);

  await step('roles: someone asks to join, and the moderator is told at once', async () => {
    await d.goto(origin + '/chat/');
    await d.locator('.conn.live').waitFor();
    const asked = await call(k, 'POST', `/api/v1/conversations/${knockId}/knocks`);
    assert(asked.status < 300, 'kip could not ask: ' + JSON.stringify(asked));
    await d.getByText('Kip Lee asks to join #roles-knock.').first().waitFor();
    await d.locator('.side-row', { hasText: 'roles-knock' }).locator('.tag', { hasText: '1 asking' }).waitFor();
    await shot(d, '91-roles-knock-arrived', { fullPage: false });
  }, d);

  await step('roles: the moderator accepts the request in the channel', async () => {
    await openChat(d, origin, knockId);
    const bar = d.locator('#main .knocks-bar', { hasText: '1 person asks to join' });
    await bar.waitFor();
    await bar.getByRole('button', { name: 'Review' }).click();
    const dlg = d.getByRole('dialog', { name: 'Asking to join #roles-knock' });
    await dlg.getByRole('button', { name: 'Add', exact: true }).click();
    await d.getByText('Kip Lee was added.').first().waitFor();
    await dlg.getByRole('button', { name: 'Close', exact: true }).last().click();
    await until(k, 'kip is in the channel', async (id) => { const r = await fetch('/api/v1/conversations/' + id); return r.ok && !!(await r.json()).conversation.membership; }, knockId);
    await d.locator('.side-row', { hasText: 'roles-knock' }).locator('.tag', { hasText: 'asking' }).waitFor({ state: 'detached' });
  }, d);

  await step('roles: the moderator removes a member, and is offered nothing against the manager', async () => {
    const dlg = await membersOf(d);
    assert(await dlg.locator(`button[data-member="${mia.id}"]`).count() === 0, 'a moderator was offered actions on the manager');
    await dlg.locator(`button[data-member="${kip.id}"]`).click();
    assert(await d.getByRole('menuitemradio', { name: 'Moderator' }).count() === 0, 'a moderator was offered levels');
    await d.getByRole('menuitem', { name: 'Remove from the channel' }).click();
    await d.getByRole('dialog', { name: 'Remove Kip Lee?' }).getByRole('button', { name: 'Remove' }).click();
    await dlg.locator(`li[data-account="${kip.id}"]`).waitFor({ state: 'detached' });
    // The Moderate tab: slow mode, and no settings of the managers'.
    await dlg.getByRole('tab', { name: 'Moderate' }).click();
    await dlg.getByLabel('Slow mode').waitFor();
    assert(await dlg.getByRole('button', { name: 'Archive' }).count() === 0, 'a moderator was offered archiving');
    await dlg.getByRole('button', { name: 'Done' }).click();
  }, d);

  await step('roles: the owner makes someone an admin, who is still no manager of a channel', async () => {
    await o.goto(origin + '/admin/people');
    const row = consoleRow(o, '@moe');
    await row.getByRole('button', { name: 'Actions' }).click();
    await o.getByRole('menuitem', { name: 'Change role…' }).click();
    const dlg = o.getByRole('dialog', { name: 'Role of @moe' });
    await dlg.getByLabel(/^Admin/).check();
    await fresh(o, () => dlg.getByRole('button', { name: 'Save' }).click(), o.getByText('Role changed.').first());
    assert((await meOf(a)).role === 'admin', 'moe is not an admin');
    const made = await call(m, 'POST', '/api/v1/channels', { name: 'roles-town', access: 'public' });
    townId = made.body.conversation.id;
    const joined = await call(a, 'POST', `/api/v1/conversations/${townId}/join`);
    assert(joined.status < 300, 'moe could not join: ' + JSON.stringify(joined));
    await openChat(a, origin, townId);
    await a.locator('#main .conv-head').getByRole('button', { name: /^Members/ }).click();
    const settings = a.getByRole('dialog', { name: 'Channel' });
    await settings.getByRole('tab', { name: 'About' }).waitFor();
    assert(await settings.getByRole('tab', { name: 'Manage' }).count() === 0, 'an admin was offered Manage');
    assert(await settings.getByRole('tab', { name: 'Moderate' }).count() === 0, 'an admin was offered Moderate');
    await settings.getByRole('button', { name: 'Done' }).click();
    const topic = await call(a, 'PATCH', `/api/v1/conversations/${townId}`, { topic: 'set by an admin' });
    assert(topic.status === 403, `an admin set a channel's topic: ${topic.status}`);
  }, a);

  await step('roles: the owner appoints a manager of a channel she is not in, from the console', async () => {
    await o.goto(origin + '/admin/channels');
    const row = consoleRow(o, 'roles-town');
    await row.getByRole('button', { name: 'Actions' }).click();
    await o.getByRole('menuitem', { name: 'Appoint a manager…' }).click();
    await pick(o, 'Appoint a manager of roles-town', 'moe', 'Moe Szyslak');
    await o.getByText('Moe Szyslak is a manager of roles-town.').first().waitFor();
    const got = await call(o, 'GET', `/api/v1/admin/channels/${townId}`);
    const mine = got.body.members.find((x) => x.account_id === moe.id);
    assert(mine && mine.level === 'manager', 'moe is not its manager: ' + JSON.stringify(got.body.members));
    const me = await meOf(o);
    assert(!got.body.members.some((x) => x.account_id === me.id), 'the owner joined the channel');
  }, o);

  await step('roles: an owner override adds someone to a private channel, and its members are shown it in pink', async () => {
    const made = await call(m, 'POST', '/api/v1/channels', { name: 'roles-vault', access: 'private', account_ids: [moe.id] });
    vaultId = made.body.conversation.id;
    await openChat(m, origin, vaultId);
    await m.locator('#main .editor').click();
    await m.keyboard.type('said before the override');
    await m.keyboard.press('Enter');
    await m.locator('#main .msg', { hasText: 'said before the override' }).waitFor();
    await o.goto(origin + '/admin/channels');
    await consoleRow(o, 'roles-vault').getByRole('button', { name: 'Actions' }).click();
    await o.getByRole('menuitem', { name: 'Add by override…' }).click();
    await pick(o, 'Add to roles-vault by override', 'kip', 'Kip Lee');
    const sure = o.getByRole('dialog', { name: 'Add Kip Lee to roles-vault by override?' });
    await sure.getByText('Every member of roles-vault will see that Kip Lee was added by override, by you.').waitFor();
    await sure.getByText('Kip Lee sees nothing posted before. You still cannot read the channel.').waitFor();
    assert(await sure.getByRole('button', { name: 'Add by override' }).isDisabled(), 'the confirmation was not typed');
    await sure.getByLabel('Type “roles-vault” to confirm').fill('roles-vault');
    await sure.getByRole('button', { name: 'Add by override' }).click();
    await o.getByText('Kip Lee was added to roles-vault by override.').first().waitFor();
    const notice = m.locator('#main .msg-system.attention[data-kind="owner_override"]', { hasText: 'Kip Lee was added by override, by' });
    await notice.waitFor();
    const colour = await notice.evaluate((el) => getComputedStyle(el).color);
    const pink = await m.evaluate(() => { const t = document.createElement('span'); t.style.color = 'var(--pink)'; document.body.append(t); const c = getComputedStyle(t).color; t.remove(); return c; });
    assert(colour === pink, `the notice is not pink: ${colour} against ${pink}`);
    await shot(m, '92-roles-owner-override', { fullPage: false });
    await openChat(k, origin, vaultId);
    await k.locator('#main .msg-system[data-kind="owner_override"]').waitFor();
    assert(await k.locator('#main .msg', { hasText: 'said before the override' }).count() === 0, 'the newcomer read what was said before');
  }, m);

  const zed = await person(browser, 'zed', origin);
  const z = zed.page;
  const before = (await call(o, 'GET', '/api/v1/admin/settings')).body.settings;

  await step('approvals: the owner opens registration and has new accounts approved', async () => {
    await o.goto(origin + '/admin/sign-in');
    await fresh(o, () => o.getByRole('combobox', { name: 'Who can make an account' }).selectOption('open'), o.getByText('Saved.').first());
    await o.getByText('Saved.').first().waitFor({ state: 'detached', timeout: 10000 }).catch(() => {});
    await fresh(o, () => o.getByRole('checkbox', { name: 'New registrations need approval' }).check(), o.getByText('Saved.').first());
    await until(o, 'the settings are saved', async () => {
      const s = (await (await fetch('/api/v1/admin/settings')).json()).settings;
      return s['registration.mode'] === 'open' && s['registration.approval'] === true;
    });
  }, o);

  await step('approvals: someone applies with a note, and is told it waits', async () => {
    await z.goto(origin + '/join');
    await z.getByLabel('Username').fill('zed');
    await z.getByLabel('Display name').fill('Zed Rivers');
    await z.getByLabel('Why you’d like to join (optional)').fill('A friend of Mia');
    await z.getByRole('button', { name: 'Create a passkey and join' }).click();
    await z.getByRole('heading', { name: 'Your request is sent' }).waitFor();
    await z.getByText('Your request to join has been sent. You can sign in once it is approved.').waitFor();
    await shot(z, '93-approvals-waiting', { phone: true });
    await z.goto(origin + '/login');
    await z.getByRole('button', { name: 'Sign in with a passkey' }).click();
    await z.getByText('Your request to join waits for approval. You can sign in once it is approved.').waitFor();
  }, z);

  await step('approvals: the owner lets a member approve requests; the applicant is marked', async () => {
    await o.goto(origin + '/admin/people');
    await consoleRow(o, '@mia').getByRole('button', { name: 'Actions' }).click();
    await o.getByRole('menuitemradio', { name: 'May approve requests' }).click();
    await o.getByText('They may approve requests now').first().waitFor();
    await consoleRow(o, '@mia').locator('.tag', { hasText: 'approves' }).waitFor();
    await o.locator('.console-main label.field', { hasText: 'Show' }).locator('select').selectOption('unclaimed');
    await consoleRow(o, '@zed').locator('.tag', { hasText: 'applying' }).waitFor();
  }, o);

  await step('approvals: the member approves the application from the chat', async () => {
    await v.goto(origin + '/chat/');
    const entry = v.locator('.side-link', { hasText: 'Approvals' });
    await entry.locator('.badge', { hasText: '1' }).waitFor();
    await entry.click();
    const row = v.locator('#main article.approval[data-kind="registration"]').filter({ hasText: 'Zed Rivers' });
    await row.getByText('A friend of Mia').waitFor();
    await shot(v, '94-approvals', { phone: true });
    await row.getByRole('button', { name: 'Approve' }).click();
    await v.getByText('Approved. They are told.').first().waitFor();
    await row.waitFor({ state: 'detached' });
    await entry.locator('.badge').waitFor({ state: 'detached' });
  }, v);

  await step('approvals: the applicant signs in once approved', async () => {
    await z.goto(origin + '/login');
    await z.getByRole('button', { name: 'Sign in with a passkey' }).click();
    await z.waitForURL(/\/chat\//);
    assert((await meOf(z)).username === 'zed', 'zed is not signed in');
  }, z);

  await step('approvals: an ordinary member has no Approvals and is refused the queue', async () => {
    await k.goto(origin + '/chat/');
    await k.locator('.conn.live').waitFor();
    assert(await k.locator('.side-link', { hasText: 'Approvals' }).count() === 0, 'a member without the flag was shown Approvals');
    const got = await call(k, 'GET', '/api/v1/approvals');
    assert(got.status === 403, `the queue answered a member ${got.status}`);
  }, k);

  await step('approvals: the settings are put back', async () => {
    await o.goto(origin + '/admin/sign-in');
    await fresh(o, () => o.getByRole('checkbox', { name: 'New registrations need approval' }).uncheck(), o.getByText('Saved.').first());
    await o.getByText('Saved.').first().waitFor({ state: 'detached', timeout: 10000 }).catch(() => {});
    await fresh(o, () => o.getByRole('combobox', { name: 'Who can make an account' }).selectOption(before['registration.mode'] || 'invite'), o.getByText('Saved.').first());
    await until(o, 'the settings are back', async (mode) => {
      const s = (await (await fetch('/api/v1/admin/settings')).json()).settings;
      return s['registration.mode'] === mode && !s['registration.approval'];
    }, before['registration.mode'] || 'invite');
  }, o);

  for (const p of [mia, moe, kip, zed]) await p.context.close();
}
