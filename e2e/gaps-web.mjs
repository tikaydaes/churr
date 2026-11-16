// Things the web chat does that the server had long supported: joining
// and leaving an open group from the browser (docs/CLIENTS.md 9.15).

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
