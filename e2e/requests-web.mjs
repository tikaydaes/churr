// Chat requests, closing a chat, and a block as deaf ears in the web chat
// (rulings R77, R78, R79; docs/FEATURES.md section 20, docs/CLIENTS.md
// 9.28), with three new people who keep the default preferences: a new
// chat with any of them is a request.
//
//   request → accept → chat; decline; dismiss; block from the request;
//   close with /close and ask again; and a blocked person's new chat and
//   group, which look normal to them while nothing reaches the blocker.

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

const requestRow = (page, name) => page.locator('#main article.request').filter({ hasText: name });

export async function requestsFlow(owner, browser, origin) {
  const people = {};
  for (const [name, display] of [['ada', 'Ada Lovelace'], ['bea', 'Bea Arthur'], ['cyd', 'Cyd Charisse']]) {
    const out = await call(owner.page, 'POST', '/api/v1/invites', {});
    const p = await person(browser, name, origin);
    await joinByInvite(p, out.body.invite.url, name, display);
    p.id = (await meOf(p.page)).id;
    people[name] = p;
  }
  const { ada, bea, cyd } = people;
  const a = ada.page, b = bea.page, c = cyd.page;
  let chat = '';

  await step('requests: a new chat with someone who does not approve chats automatically waits, with no composer', async () => {
    const out = await call(a, 'POST', '/api/v1/chats', { account_ids: [bea.id] });
    assert(out.status === 201 && out.body.conversation.chat_state === 'waiting', 'the chat should wait: ' + JSON.stringify(out));
    chat = out.body.conversation.id;
    await openChat(a, origin, chat);
    await a.locator('#main .chat-state.waiting', { hasText: 'Waiting for Bea Arthur to accept your request' }).waitFor();
    assert(await a.locator('#main .editor').count() === 0, 'a waiting chat has no composer');
    await a.locator('.side-row', { hasText: 'Bea Arthur' }).locator('.tag', { hasText: 'waiting' }).waitFor();
    await shot(a, '60-request-waiting', { phone: true });
  }, a);

  await step('requests: the person asked sees who is asking under Requests, and accepts', async () => {
    await b.goto(origin + '/chat/');
    await b.locator('.side-link', { hasText: 'Requests' }).locator('.badge', { hasText: '1' }).waitFor();
    await b.locator('.side-link', { hasText: 'Requests' }).click();
    const row = requestRow(b, 'Ada Lovelace');
    await row.getByText('@ada').waitFor();
    await row.getByText('Asks for a direct chat').waitFor();
    await shot(b, '61-requests', { phone: true });
    await row.getByRole('button', { name: 'Accept' }).click();
    await b.locator('#main .editor').waitFor();
    // The asker's chat opens where it stands.
    await a.locator('#main .editor').waitFor();
    assert(await a.locator('#main .chat-state').count() === 0, 'the bar is gone once accepted');
    await b.locator('#main .editor').click();
    await b.keyboard.type('hello ada');
    await b.keyboard.press('Enter');
    await a.locator('#main .msg', { hasText: 'hello ada' }).waitFor();
  }, b);

  await step('requests: a decline is told to the person asking, who cannot ask again for a day', async () => {
    const out = await call(c, 'POST', '/api/v1/chats', { account_ids: [bea.id] });
    const id = out.body.conversation.id;
    await openChat(c, origin, id);
    await b.goto(origin + '/chat/requests');
    await requestRow(b, 'Cyd Charisse').getByRole('button', { name: 'Decline' }).click();
    await requestRow(b, 'Cyd Charisse').waitFor({ state: 'detached' });
    await c.locator('#main .chat-state.declined', { hasText: 'Bea Arthur declined this chat' }).waitFor();
    const again = await c.request.post(origin + '/api/v1/chats', { data: { account_ids: [bea.id] }, headers: { origin } });
    assert(again.status() === 429, 'asking again at once should wait a day: ' + again.status());
  }, c);

  await step('requests: dismissed, it goes on waiting for the person asking', async () => {
    const out = await call(a, 'POST', '/api/v1/chats', { account_ids: [cyd.id] });
    const id = out.body.conversation.id;
    await c.goto(origin + '/chat/requests');
    await requestRow(c, 'Ada Lovelace').getByRole('button', { name: 'Dismiss' }).click();
    await requestRow(c, 'Ada Lovelace').waitFor({ state: 'detached' });
    await openChat(a, origin, id);
    await a.locator('#main .chat-state.waiting').waitFor();
    const held = await call(a, 'GET', '/api/v1/conversations/' + id);
    assert(held.body.conversation.chat_state === 'waiting', 'still waiting after a dismissal');
  }, c);

  await step('requests: blocking from the request leaves it waiting for the person asking', async () => {
    const out = await call(b, 'POST', '/api/v1/chats', { account_ids: [cyd.id] });
    const before = out.body.conversation;
    await c.goto(origin + '/chat/requests');
    await requestRow(c, 'Bea Arthur').getByRole('button', { name: 'Block' }).click();
    await requestRow(c, 'Bea Arthur').waitFor({ state: 'detached' });
    const after = (await call(b, 'GET', '/api/v1/conversations/' + before.id)).body.conversation;
    assert(after.chat_state === 'waiting' && after.member_count === before.member_count, 'blocking changed what Bea sees: ' + JSON.stringify(after));
    const blocks = (await call(c, 'GET', '/api/v1/me/blocks')).body.account_ids;
    assert(blocks.includes(bea.id), 'Cyd has blocked Bea');
  }, c);

  await step('requests: /close closes the chat for both, without saying who; ask to chat again makes a new request', async () => {
    await openChat(a, origin, chat);
    await a.locator('#main .editor').click();
    await a.keyboard.type('/close');
    await a.keyboard.press('Enter');
    await a.getByRole('dialog').getByRole('button', { name: 'Close chat' }).click();
    await a.locator('#main .chat-state.closed', { hasText: 'This chat is closed' }).waitFor();
    await openChat(b, origin, chat);
    await b.locator('#main .chat-state.closed').waitFor();
    await b.locator('#main .msg', { hasText: 'hello ada' }).waitFor();
    assert(await b.locator('#main .editor').count() === 0, 'nobody writes in a closed chat');
    await shot(b, '62-chat-closed', { phone: true });
    await a.getByRole('button', { name: 'Ask to chat again' }).click();
    await a.locator('#main .chat-state.waiting').waitFor();
    assert(!a.url().endsWith(chat), 'asking again opens a new chat');
    await b.goto(origin + '/chat/requests');
    await requestRow(b, 'Ada Lovelace').waitFor();
  }, a);

  await step('blocks: the blocked person’s new chat and group look normal to them, and nothing reaches the blocker', async () => {
    // Cyd blocked Bea above. Bea's chat with Cyd waits like her chat with
    // anyone; a group with Cyd in it shows Cyd to Bea alone.
    const group = await call(b, 'POST', '/api/v1/chats', { account_ids: [cyd.id, ada.id] });
    assert(group.status === 201 && group.body.conversation.member_ids.length === 3, 'Bea is shown all three: ' + JSON.stringify(group.body));
    const gid = group.body.conversation.id;
    await openChat(b, origin, gid);
    await b.locator('#main .editor').click();
    await b.keyboard.type('hi both');
    await b.keyboard.press('Enter');
    await b.locator('#main .msg', { hasText: 'hi both' }).waitFor();
    const forAda = await call(a, 'GET', '/api/v1/conversations/' + gid);
    assert(forAda.body.conversation.member_ids.length === 2, 'Ada is shown who is really there');
    // Asked outside the page, so the refusal is not logged as a page error.
    const forCyd = await c.request.get(origin + '/api/v1/conversations/' + gid);
    assert(forCyd.status() === 404, 'Cyd is not in it');
    await c.goto(origin + '/chat/');
    await c.locator('.conn.live').waitFor();
    await c.waitForTimeout(800);
    assert(await c.locator(`.side-row[data-conversation="${gid}"]`).count() === 0, 'Bea\u2019s group does not reach Cyd\u2019s sidebar');
    const reqs = (await call(c, 'GET', '/api/v1/me/chat-requests')).body.requests;
    assert(reqs.length === 0, 'Cyd has no request: ' + JSON.stringify(reqs));
    // And to Bea, Cyd is online with nothing more.
    const seen = (await call(b, 'GET', '/api/v1/accounts/' + cyd.id)).body.account;
    assert(seen.presence === 'online' && !seen.status && !seen.dnd, 'Bea sees Cyd online and nothing more: ' + JSON.stringify(seen));
  }, b);

  await step('requests: the two preferences are on the account page, both off', async () => {
    await c.goto(origin + '/account/notifications');
    const direct = c.getByRole('checkbox', { name: /Auto approve new direct chats/ });
    await direct.waitFor();
    assert(!(await direct.isChecked()), 'off by default');
    assert(!(await c.getByRole('checkbox', { name: /Auto approve new sealed chats/ }).isChecked()), 'off by default');
  }, c);

  for (const p of [ada, bea, cyd]) await p.context.close();
}

// autoApprove turns both preferences on for a person, so the older flows
// open chats between the first two people straight away, as before.
export async function autoApprove(who) {
  await until(who.page, 'the preferences are saved', async () => {
    const r = await fetch('/api/v1/me/prefs', { method: 'PATCH', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ auto_approve_direct: true, auto_approve_sealed: true }) });
    return r.ok;
  });
}
