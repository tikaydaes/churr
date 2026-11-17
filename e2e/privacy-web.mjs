// A private channel's contract (ruling R87, docs/PERMISSIONS.md section
// 4.4): someone added to a private channel with history reads only what is
// posted from their first join, and the members are told who arrived.

import { step, shot, assert, until } from './lib.mjs';

// call makes one API request from a page, as its person, and returns the
// status and the body.
const call = (page, method, path, body) => page.evaluate(async ([m, p, b]) => {
  const r = await fetch(p, { method: m, headers: b ? { 'content-type': 'application/json' } : {}, body: b ? JSON.stringify(b) : undefined });
  const text = await r.text();
  return { status: r.status, body: text ? JSON.parse(text) : null };
}, [method, path, body]);

// privacyFlow: the owner makes a private channel and talks in it alone,
// then invites the second person, who accepts. The owner sees "Sam Okafor was added by
// …"; the second person sees the notice and what came after, and nothing
// from before, in the channel and in search.
export async function privacyFlow(owner, second, origin) {
  const a = owner.page, b = second.page;
  const made = await call(a, 'POST', '/api/v1/channels', { name: 'inner-circle-' + Date.now().toString(36), private: true });
  assert(made.status === 201, `making the channel: ${made.status} ${JSON.stringify(made.body)}`);
  const room = made.body.conversation.id;
  const post = async (text) => {
    const out = await call(a, 'POST', `/api/v1/conversations/${room}/messages`, { text });
    assert(out.status === 201, `posting: ${out.status}`);
  };
  await post('the plan before sam came, zephyrine');
  await post('a second word from before');
  await a.waitForTimeout(20);

  await step('privacy: adding someone to a private channel is announced to its members', async () => {
    await a.goto(origin + '/chat/c/' + room);
    await a.locator('#main').getByText('the plan before sam came').waitFor();
    const me = (await call(b, 'GET', '/api/v1/me')).body.account;
    const added = await call(a, 'POST', `/api/v1/conversations/${room}/members`, { account_ids: [me.id] });
    assert(added.status === 200, `adding sam: ${added.status} ${JSON.stringify(added.body)}`);
    // Nobody is placed in a channel unasked (ruling R103): Sam is invited,
    // and joins by accepting.
    const asked = (await call(b, 'GET', '/api/v1/me/chat-requests')).body.requests.find((r) => r.conversation_id === room);
    assert(asked && asked.kind === 'channel', 'sam has no invitation: ' + JSON.stringify(asked));
    const accepted = await call(b, 'POST', `/api/v1/me/chat-requests/${asked.id}/accept`);
    assert(accepted.status === 200, `sam accepting: ${accepted.status} ${JSON.stringify(accepted.body)}`);
    // The names come in as the page learns the accounts.
    await a.locator('#main .msg-system', { hasText: 'Sam Okafor was added by' }).waitFor();
    await shot(a, 'privacy-announced');
  }, a);

  await step('privacy: the newcomer reads nothing from before their join', async () => {
    await post('fresh after sam came');
    await b.goto(origin + '/chat/c/' + room);
    const main = b.locator('#main');
    await main.getByText('fresh after sam came').waitFor();
    await main.locator('.msg-system', { hasText: 'was added by' }).waitFor();
    assert(await main.getByText('before sam came').count() === 0, 'sam sees a message from before he joined');
    assert(await main.getByText('a second word from before').count() === 0, 'sam sees a message from before he joined');
    const found = await call(b, 'GET', '/api/v1/search?q=zephyrine');
    assert(found.status === 200 && found.body.results.length === 0, 'search finds the old message for sam: ' + JSON.stringify(found.body));
    // Live: what is posted now arrives.
    await post('live after sam came');
    await main.getByText('live after sam came').waitFor();
    await shot(b, 'privacy-newcomer');
    // The owner, there from the start, still has all of it.
    await until(a, 'the owner still reads the old messages', async (id) => {
      const r = await fetch(`/api/v1/conversations/${id}/messages`);
      return (await r.json()).messages.some((m) => (m.text || '').includes('before sam came'));
    }, room);
  }, b);
}
