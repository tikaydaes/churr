// First contact in the web chat (ruling R103): nobody ends up in a
// conversation they did not accept. With the people of the requests flow,
// who keep the default preferences, and the owner, who approves new chats
// automatically:
//
//   a group chat: Ada includes the owner and Cyd; the owner is in at once,
//   Cyd is asked, sees who is in it, accepts and is in the chat;
//   a channel invitation: Ada, its manager, invites Cyd from the members
//   list; Cyd reads the public channel before answering, and accepts;
//   a group offer: a group Cyd is in is added to two private channels; Cyd
//   accepts one from the sidebar, declines the other, and joins it later
//   from Browse with Join, not Ask to join;
//   a default channel: the owner adds one in the admin console, and Cyd is
//   offered it in the sidebar.

import { step, shot, assert, until } from './lib.mjs';

// call makes one API request from a page, as its person, and returns the
// status and the body.
const call = (page, method, path, body) => page.evaluate(async ([m, p, b]) => {
  const r = await fetch(p, { method: m, headers: b ? { 'content-type': 'application/json' } : {}, body: b ? JSON.stringify(b) : undefined });
  const text = await r.text();
  return { status: r.status, body: text ? JSON.parse(text) : null };
}, [method, path, body]);

async function open(page, origin, path) {
  await page.goto(origin + path);
  await page.locator('.conn.live').waitFor();
}

const requestRow = (page, text) => page.locator('#main article.request').filter({ hasText: text });
const offerRow = (page, name) => page.locator('.side-offer').filter({ hasText: name });

export async function firstContactFlow(owner, { ada, cyd }, origin) {
  const o = owner.page, a = ada.page, c = cyd.page;
  const ownerId = (await call(o, 'GET', '/api/v1/me')).body.account.id;
  const tag = Date.now().toString(36).slice(-4);
  let chat = '', den = '', vault = '', attic = '', hall = '';

  await step('first contact: a group chat asks whoever does not approve chats automatically, and shows them invited', async () => {
    const out = await call(a, 'POST', '/api/v1/chats', { account_ids: [ownerId, cyd.id] });
    assert(out.status === 201, 'the group chat was not made: ' + JSON.stringify(out));
    const conv = out.body.conversation;
    chat = conv.id;
    assert(conv.member_ids.includes(ownerId) && !conv.member_ids.includes(cyd.id), 'the owner is in and Cyd is not: ' + JSON.stringify(conv));
    assert((conv.invited_ids || []).includes(cyd.id), 'Cyd is listed as invited: ' + JSON.stringify(conv));
    await open(a, origin, '/chat/c/' + chat);
    // Named by everyone in it, the invited too; and said to be waiting.
    await a.locator('#main .conv-title h1', { hasText: 'Cyd Charisse' }).waitFor();
    await a.locator('#main [data-part="invited"]', { hasText: '1 person invited, not yet joined' }).waitFor();
    await a.locator('#main .editor').waitFor();
  }, a);

  await step('first contact: the person asked sees who is in the group chat, accepts, and is in it', async () => {
    await open(c, origin, '/chat/requests');
    const row = requestRow(c, 'added you to a group chat');
    await row.getByText(/added you to a group chat with .*Kaity/).waitFor();
    await row.getByText('Ada Lovelace', { exact: false }).first().waitFor();
    assert(await row.getByRole('button', { name: 'Preview' }).count() === 0, 'a group chat is not read before accepting');
    await shot(c, 'fc-group-request', { phone: true });
    await row.getByRole('button', { name: 'Accept' }).click();
    await c.waitForURL('**/chat/c/' + chat);
    await c.locator('#main .editor').click();
    await c.keyboard.type('glad to be here');
    await c.keyboard.press('Enter');
    await a.locator('#main .msg', { hasText: 'glad to be here' }).waitFor();
    // The member list and the heading follow the join.
    await a.locator('#main [data-part="invited"]').waitFor({ state: 'detached' });
  }, c);

  await step('first contact: a manager invites someone to a channel from the members list; they show as invited', async () => {
    const made = await call(a, 'POST', '/api/v1/channels', { name: 'fc-den-' + tag, private: false, topic: 'Where the den meets' });
    assert(made.status === 201, 'the channel was not made: ' + JSON.stringify(made));
    den = made.body.conversation.id;
    await call(a, 'POST', `/api/v1/conversations/${den}/messages`, { text: 'the den is open for reading' });
    await open(a, origin, '/chat/c/' + den);
    await a.locator('#main .conv-head').getByRole('button', { name: /^Members/ }).click();
    const dlg = a.getByRole('dialog', { name: 'Channel' });
    await dlg.getByRole('button', { name: 'Add people' }).click();
    const pickDlg = a.getByRole('dialog', { name: 'Add to fc-den-' + tag });
    await pickDlg.getByLabel('Search people').fill('cyd');
    await pickDlg.getByRole('button', { name: /Cyd Charisse/ }).click();
    await pickDlg.getByRole('button', { name: 'Invite', exact: true }).click();
    await a.getByText('Invited. They are in once they accept.').first().waitFor();
    await dlg.locator(`li[data-account="${cyd.id}"][data-invited="true"]`).getByText('invited, not yet joined').waitFor();
    await shot(a, 'fc-members-invited', { fullPage: false });
    await dlg.getByRole('button', { name: 'Done' }).click();
  }, a);

  await step('first contact: the invited person reads the public channel before answering, then accepts', async () => {
    await open(c, origin, '/chat/requests');
    const row = requestRow(c, 'invited you to #fc-den-' + tag);
    await row.getByText('Where the den meets').waitFor();
    await row.getByRole('button', { name: 'Preview' }).click();
    await c.waitForURL('**/chat/c/' + den);
    await c.locator('#main .msg', { hasText: 'the den is open for reading' }).waitFor();
    const bar = c.locator('#main .join-bar.invitation');
    await bar.getByText('Ada Lovelace invited you to this channel.').waitFor();
    assert(await c.locator('#main .editor').count() === 0, 'no composer before accepting');
    await shot(c, 'fc-channel-preview', { phone: true });
    await bar.getByRole('button', { name: 'Accept' }).click();
    await c.locator('#main .editor').waitFor();
    await c.locator('.side-row', { hasText: 'fc-den-' + tag }).waitFor();
    await until(a, 'Cyd is a member of the den, no longer invited', async (id) => {
      const conv = (await (await fetch(`/api/v1/conversations/${id}`)).json()).conversation;
      return conv.member_count === 2 && !(conv.invited_ids || []).length;
    }, den);
  }, c);

  await step('first contact: a group added to private channels offers them in the sidebar; one is accepted there', async () => {
    const grp = await call(o, 'POST', '/api/v1/admin/groups', { handle: 'fc-crew-' + tag, name: 'Crew' });
    assert(grp.status === 201, 'the group was not made: ' + JSON.stringify(grp));
    const gid = grp.body.group.id;
    const put = await call(o, 'PUT', `/api/v1/admin/groups/${gid}/members/${cyd.id}`);
    assert(put.status < 300, 'Cyd was not put in the group: ' + JSON.stringify(put));
    for (const name of ['fc-vault-', 'fc-attic-']) {
      const made = await call(a, 'POST', '/api/v1/channels', { name: name + tag, private: true });
      assert(made.status === 201, 'the channel was not made: ' + JSON.stringify(made));
      if (name === 'fc-vault-') vault = made.body.conversation.id; else attic = made.body.conversation.id;
    }
    await open(c, origin, '/chat/');
    for (const id of [vault, attic]) {
      const added = await call(a, 'PUT', `/api/v1/conversations/${id}/groups/${gid}`, { level: 'member' });
      assert(added.status < 300, 'the group was not added: ' + JSON.stringify(added));
    }
    // Arrives live.
    const row = offerRow(c, 'fc-vault-' + tag);
    await row.waitFor();
    await offerRow(c, 'fc-attic-' + tag).waitFor();
    // A private channel shows nothing before joining: no link to read it.
    assert(await row.locator('a').count() === 0, 'a private offer opens nothing before it is accepted');
    await shot(c, 'fc-sidebar-offers', { phone: true });
    await row.getByRole('button', { name: 'Accept' }).click();
    await c.waitForURL('**/chat/c/' + vault);
    await c.locator('#main .editor').waitFor();
    await c.locator('.side-row', { hasText: 'fc-vault-' + tag }).waitFor();
    await row.waitFor({ state: 'detached' });
  }, c);

  await step('first contact: a declined offer leaves the sidebar, and Browse joins it later without knocking', async () => {
    await offerRow(c, 'fc-attic-' + tag).getByRole('button', { name: 'Decline' }).click();
    await offerRow(c, 'fc-attic-' + tag).waitFor({ state: 'detached' });
    await open(c, origin, '/chat/browse');
    const li = c.locator('#main ul.browse li').filter({ hasText: 'fc-attic-' + tag });
    await li.waitFor();
    assert(await li.getByRole('button', { name: 'Ask to join' }).count() === 0, 'a channel the group gives access to is not knocked on');
    assert(await li.getByRole('button', { name: 'Read' }).count() === 0, 'a private channel is not read before joining');
    await li.getByRole('button', { name: 'Join' }).click();
    await c.getByText('Joined fc-attic-' + tag + '.').first().waitFor();
    await c.locator('.side-row', { hasText: 'fc-attic-' + tag }).waitFor();
  }, c);

  await step('first contact: a default channel added in the admin console is offered to the people already here', async () => {
    const made = await call(a, 'POST', '/api/v1/channels', { name: 'fc-hall-' + tag, private: false });
    assert(made.status === 201, 'the channel was not made: ' + JSON.stringify(made));
    hall = made.body.conversation.id;
    await open(c, origin, '/chat/');
    await o.goto(origin + '/admin/messages');
    const setting = o.locator('.setting', { hasText: 'Default channels' });
    await setting.waitFor();
    await setting.getByRole('button', { name: 'Add a channel…' }).click();
    const dlg = o.getByRole('dialog', { name: 'Add a default channel' });
    await dlg.getByLabel('Search channels').fill('fc-hall-' + tag);
    await dlg.getByRole('button', { name: new RegExp('fc-hall-' + tag) }).click();
    await setting.locator(`li[data-channel="${hall}"]`).waitFor();
    await shot(o, 'fc-admin-default-channels', { fullPage: false });
    const row = offerRow(c, 'fc-hall-' + tag);
    await row.waitFor();
    // A public one can be read from the sidebar before answering.
    await row.locator('a.offer-link').click();
    await c.waitForURL('**/chat/c/' + hall);
    await c.locator('#main .join-bar.invitation', { hasText: 'default channel' }).getByRole('button', { name: 'Decline' }).click();
    await row.waitFor({ state: 'detached' });
    // Put back: the setting as it was, which withdraws the offer from
    // everyone who has not answered it.
    await setting.locator(`li[data-channel="${hall}"]`).getByRole('button', { name: 'Remove' }).click();
    await setting.locator(`li[data-channel="${hall}"]`).waitFor({ state: 'detached' });
    await until(o, 'the offer is withdrawn', async (id) => !(await (await fetch('/api/v1/me/channel-offers')).json()).offers.some((x) => x.conversation_id === id), hall);
  }, o);
}
