// The screens for channels on a hub, in the browser, against stubbed
// answers. The browser tests' server is on no hub and has no peers, so
// every hub operation (listHubDirectory, joinHubChannel, getHubChannel,
// moderateHubChannel, the requests, candidates and admissions of a
// private channel, askToJoinHubChannel, the moves, shareChannel's listing, the peers' clock
// figures) is answered here by page.route with the shapes of
// docs/openapi.yaml, and what the pages send is checked. The general
// channel is made to look shared through a hub by adding `shared` to the
// server's real answer. Nothing here talks to a real hub.

import { step, shot, assert } from './lib.mjs';

const HUB = '0193d000-0000-7000-8000-00000000a001';
const DIRECT = '0193d000-0000-7000-8000-00000000a002';
const STREAM = '0193d100-0000-7000-8000-00000000b001';
const clock = { offset_ms: -12, latency_ms: 41, jitter_ms: 3, uncertainty_ms: 21, forward_ms: 19, forward_jitter_ms: 2, back_ms: 22, back_jitter_ms: 4, asymmetry_ms: 0, samples: 16, measured_at: new Date(Date.now() - 20000).toISOString() };
const known = (id, name, fp) => ({ id, name, fingerprint: fp, first_seen_at: '2026-11-01T10:00:00Z' });

export async function hubFlow(kaity, sam, origin) {
  const a = kaity.page;
  const sent = [];
  const json = (route, body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
  const own = await a.evaluate(async () => (await (await fetch('/api/v1/server')).json()).server.name);
  const me = await a.evaluate(async () => (await (await fetch('/api/v1/me')).json()).account);

  const peers = [
    { id: HUB, kind: 'hub', name: 'hub.example', fingerprint: 'SHA256:hub-4c1f-9a07-e2d3', dials: true, state: 'active', created_by_id: me.id, created_at: '2026-11-01T10:00:00Z', connected_at: new Date(Date.now() - 3600e3).toISOString(),
      servers: [known('0193e000-0000-7000-8000-0000000000c1', 'b.example', 'SHA256:b-77aa'), known('0193e000-0000-7000-8000-0000000000c2', 'c.example', 'SHA256:c-1203')], told_clock: clock },
    { id: DIRECT, kind: 'server', name: 'b.example', fingerprint: 'SHA256:b-77aa', dials: false, state: 'active', created_by_id: me.id, created_at: '2026-11-02T10:00:00Z', connected_at: new Date(Date.now() - 600e3).toISOString(),
      measured_clock: { ...clock, offset_ms: 31500, uncertainty_ms: 40 } },
  ];
  const general = await a.evaluate(async () => (await (await fetch('/api/v1/conversations?limit=200')).json()).conversations.find((c) => c.name === 'general'));
  // A message from each, to carry the badge and the menu.
  const say = (page, text) => page.evaluate(async ([id, t]) => (await fetch(`/api/v1/conversations/${id}/messages`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: t, client_id: crypto.randomUUID() }) })).status, [general.id, text]);
  await say(sam.page, 'Hub test from Sam');
  await say(a, 'Hub test from Kaity');
  const streams = [
    { id: STREAM, peer_id: HUB, conversation_id: general.id, owner_server: 'hub.example', reference: 'hub', kind: 'channel', state: 'active', key_epoch: 3, history_days: 0, servers: [{ name: 'b.example', state: 'active' }], created_at: '2026-11-03T10:00:00Z' },
    { id: '0193d100-0000-7000-8000-00000000b002', peer_id: HUB, conversation_id: '0193d100-0000-7000-8000-00000000c002', owner_server: 'hub.example', reference: 'hub', kind: 'channel', state: 'offered', offered_name: 'Field notes', offered_at: '2026-11-13T09:00:00Z', key_epoch: 0, history_days: 0, servers: [], created_at: '2026-11-13T09:00:00Z' },
    { id: '0193d100-0000-7000-8000-00000000b003', peer_id: HUB, conversation_id: '0193d100-0000-7000-8000-00000000c003', owner_server: 'hub.example', reference: 'hub', kind: 'channel', state: 'removed', offered_name: 'Night shift', offered_at: '2026-11-10T09:00:00Z', key_epoch: 1, history_days: 0, servers: [], created_at: '2026-11-10T09:00:00Z' },
    { id: '0193d100-0000-7000-8000-00000000b004', peer_id: DIRECT, conversation_id: general.id, reference: 'self', kind: 'channel', state: 'active', key_epoch: 2, history_days: 7, servers: [{ name: 'b.example', state: 'active' }], created_at: '2026-11-04T10:00:00Z' },
  ];
  const directory = [
    { stream_id: '0193d100-0000-7000-8000-00000000d001', name: 'Makers', description: 'Things we build, and how.', listing: 'open', managers: [{ account_id: 'x1', server: 'b.example', username: 'ana' }], servers: 5, state: 'none' },
    { stream_id: '0193d100-0000-7000-8000-00000000d002', name: 'Release managers', description: 'Coordinating releases across the federation.', listing: 'request', managers: [{ account_id: 'x2', server: 'c.example', username: 'ben' }, { account_id: 'x3', server: 'b.example', username: 'kim' }], servers: 3, state: 'none' },
    { stream_id: '0193d100-0000-7000-8000-00000000b002', name: 'Field notes', description: '', listing: 'request', managers: [{ account_id: 'x4', server: 'c.example', username: 'lee' }], servers: 2, state: 'requested' },
    { stream_id: STREAM, name: 'General', description: 'Everyone, everywhere.', listing: 'open', managers: [{ account_id: me.id, server: own, username: me.username }], servers: 3, state: 'member' },
  ];
  const moves = [
    { id: '0193f000-0000-7000-8000-0000000000e1', stream_id: '0193d100-0000-7000-8000-00000000b004', direction: 'incoming', other_server: 'b.example', hub_name: 'hub.example', hub_fingerprint: 'SHA256:hub-4c1f-9a07-e2d3', hub_peer_id: HUB, state: 'proposed', created_at: '2026-11-14T08:00:00Z', expires_at: '2026-11-15T08:00:00Z', updated_at: '2026-11-14T08:00:00Z' },
    { id: '0193f000-0000-7000-8000-0000000000e2', stream_id: '0193d100-0000-7000-8000-00000000b004', direction: 'outgoing', other_server: 'b.example', hub_name: 'hub.example', hub_fingerprint: 'SHA256:hub-4c1f-9a07-e2d3', hub_peer_id: HUB, state: 'abandoned', reason: 'b.example could not be reached before the proposal lapsed.', created_at: '2026-11-12T08:00:00Z', expires_at: '2026-11-13T08:00:00Z', updated_at: '2026-11-13T08:00:00Z' },
  ];
  let record = {
    stream_id: STREAM, hub: 'hub.example', version: 12, name: 'General', description: 'Everyone, everywhere.', listing: 'open',
    servers: [own, 'b.example', 'c.example'], managers: [{ account_id: me.id, server: own, username: me.username }, { account_id: 'x9', server: 'b.example', username: 'ana' }],
    moderators: [{ account_id: 'x7', server: 'c.example', username: 'lee' }],
    invited: ['d.example'], requests: ['e.example'], key_maker: own, closed: false, private: false, updated_at: new Date().toISOString(),
  };
  // A private channel: who asks to join, and whom this instance knows to admit.
  const samId = await sam.page.evaluate(async () => (await (await fetch('/api/v1/me')).json()).account.id);
  let asking = [{ account_id: '0193e000-0000-7000-8000-0000000000f1', username: 'rio', display_name: 'Rio', server: 'b.example', requested_at: new Date(Date.now() - 600e3).toISOString() }];
  const candidates = [{ account_id: samId, username: 'sam', display_name: 'Sam', server: own }];
  const onLists = (r) => ({ manager: r.managers.some((m) => m.account_id === me.id), moderator: r.moderators.some((m) => m.account_id === me.id) });

  await a.route((url) => url.pathname.startsWith('/api/v1/admin/peers') || url.pathname.startsWith('/api/v1/admin/streams') || url.pathname.startsWith('/api/v1/admin/moves') || url.pathname.startsWith('/api/v1/streams/') || url.pathname === '/api/v1/conversations', async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const body = req.postData() ? JSON.parse(req.postData()) : null;
    if (req.method() !== 'GET') sent.push({ method: req.method(), path, body });
    if (path === '/api/v1/conversations') {
      // the real list, with general shared through the hub
      const res = await route.fetch();
      const data = await res.json();
      for (const c of data.conversations) if (c.id === general.id) c.shared = { stream_id: STREAM, owner_server: 'hub.example', servers: ['b.example', 'c.example'] };
      return json(route, data);
    }
    if (path === '/api/v1/admin/peers') return json(route, { peers });
    if (path === `/api/v1/admin/peers/${HUB}/directory`) return json(route, { channels: directory });
    if (path === `/api/v1/admin/peers/${HUB}/directory/join`) return json(route, { stream: { ...streams[1], id: body.stream_id, offered_name: body.name }, conversation: { ...general, id: '0193d100-0000-7000-8000-00000000c009', name: body.name } });
    if (path === '/api/v1/admin/streams' && req.method() === 'GET') return json(route, { streams });
    if (path === '/api/v1/admin/streams') return json(route, { stream: { ...streams[0], owner_server: 'hub.example' } }, 201);
    if (path === '/api/v1/admin/moves') return json(route, { moves });
    if (/\/api\/v1\/admin\/moves\/.*\/accept$/.test(path)) return json(route, { move: { ...moves[0], state: 'accepted' } });
    if (path === `/api/v1/streams/${STREAM}/channel`) return json(route, { channel: record, ...onLists(record) });
    if (path === `/api/v1/streams/${STREAM}/channel/requests`) return req.method() === 'GET' ? json(route, { requests: asking }) : json(route, { request: { account_id: me.id, username: me.username, display_name: me.display_name || me.username, server: own, requested_at: new Date().toISOString() } });
    if (path === `/api/v1/streams/${STREAM}/channel/candidates`) return json(route, { people: candidates });
    if (path === `/api/v1/streams/${STREAM}/channel/members`) {
      asking = asking.filter((p) => p.account_id !== body.account_id);
      return route.fulfill({ status: 204 });
    }
    if (path === `/api/v1/streams/${STREAM}/channel/actions`) {
      if (body.action === 'approve') record = { ...record, version: record.version + 1, requests: [], servers: [...record.servers, body.server] };
      return json(route, { channel: record });
    }
    return route.continue();
  });

  try {
    await step('hub: linked servers show each link’s clock figures', async () => {
      await a.goto(origin + '/admin/linked-servers');
      const hubRow = a.locator('.rows > li', { hasText: 'hub.example' });
      await hubRow.getByText('This server’s clock, as the hub measures it').waitFor();
      await hubRow.locator('.clock-figures', { hasText: 'Offset' }).getByText('−12 ms').waitFor();
      await hubRow.getByText('19 ms ± 2 ms').waitFor();
      await hubRow.getByText('± 21 ms').waitFor();
      await a.locator('.rows > li', { hasText: 'b.example’s clock, as this server measures it' }).getByText(/more than 30 seconds off/).waitFor();
      await shot(a, '37-admin-linked-servers-clock');
    }, a);

    await step('hub: the directory lists channels with listing, managers and instances, and asks to join', async () => {
      await a.locator('.rows > li', { hasText: 'hub.example' }).getByRole('link', { name: 'Directory' }).click();
      await a.getByRole('heading', { name: 'Hub directories', level: 1 }).waitFor();
      const row = a.locator('.directory > li', { hasText: 'Release managers' });
      await row.getByText('managed by @ben@c.example, @kim@b.example').waitFor();
      await row.getByText(/3 instances/).waitFor();
      await a.locator('.directory > li', { hasText: 'Field notes' }).getByText('Asked. Waiting for a manager.').waitFor();
      await a.locator('.directory > li', { hasText: 'General' }).getByText('joined').waitFor();
      await a.getByText('A manager declined, or later removed this instance.').waitFor();
      await shot(a, '54-hub-directory', { phone: true });
      await row.getByRole('button', { name: 'Ask to join' }).click();
      await a.getByLabel('Its name here').fill('releases');
      await a.getByLabel('Private here').check();
      await a.getByRole('dialog').getByRole('button', { name: 'Ask to join' }).click();
      await a.locator('.toast', { hasText: 'Asked.' }).waitFor();
      const join = sent.find((s) => s.path.endsWith('/directory/join'));
      assert(join && join.body.stream_id === directory[1].stream_id && join.body.name === 'releases' && join.body.private === true, 'the join request was not sent as asked: ' + JSON.stringify(join));
    }, a);

    await step('hub: sharing to a hub says it will own the channel, and sends the listing and invitations', async () => {
      await a.goto(origin + '/admin/shared-channels');
      await a.getByRole('button', { name: 'Share a channel' }).click();
      await a.getByLabel('Through').selectOption(HUB);
      await a.getByText('hub.example will own this channel.', { exact: false }).waitFor();
      await a.getByText(/cannot read messages, files or anything else inside it/).waitFor();
      await a.getByRole('radio', { name: /On request/ }).check();
      await a.getByRole('checkbox', { name: /c\.example/ }).check();
      await shot(a, '55-hub-share', { fullPage: false });
      await a.getByRole('dialog').getByRole('button', { name: 'Share' }).click();
      await a.locator('.toast', { hasText: 'hub.example owns it now.' }).waitFor();
      const share = sent.find((s) => s.path === '/api/v1/admin/streams' && s.method === 'POST');
      assert(share && share.body.listing === 'request' && share.body.peer_id === HUB && share.body.server_ids.length === 1, 'the share was not sent as chosen: ' + JSON.stringify(share));
    }, a);

    await step('hub: a proposed move shows the hub’s name and key; accepting it is sent; an abandoned one says why', async () => {
      const moveCard = a.locator('.console-card', { hasText: 'Moves onto a hub' });
      await moveCard.getByText('Why: b.example could not be reached before the proposal lapsed.').waitFor();
      await moveCard.getByRole('button', { name: 'Accept' }).click();
      const dlg = a.getByRole('dialog');
      await dlg.getByText('SHA256:hub-4c1f-9a07-e2d3').waitFor();
      await dlg.getByText(/Check that it is the fingerprint/).waitFor();
      await shot(a, '56-hub-move-accept', { fullPage: false });
      await dlg.getByRole('button', { name: 'Accept the move' }).click();
      await a.locator('.toast', { hasText: 'Accepted.' }).waitFor();
      assert(sent.some((s) => s.path === `/api/v1/admin/moves/${moves[0].id}/accept`), 'the acceptance was not sent');
    }, a);

    await step('hub: a hub channel shows its record, managers and moderators are marked, and a manager approves an instance', async () => {
      await a.goto(origin + '/chat/');
      await a.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
      await a.locator('#main .editor').waitFor();
      // kaity is on the manager list: her messages carry the tag
      await a.locator('#main .msg', { hasText: 'Hub test from Kaity' }).locator('.msg-head .tag', { hasText: 'manager' }).waitFor();
      await a.getByRole('button', { name: /^Members/ }).click();
      await a.getByRole('tab', { name: 'On the hub' }).click();
      const dlg = a.getByRole('dialog');
      await dlg.getByText('hub.example owns this channel.', { exact: false }).waitFor();
      await dlg.getByText('@ana@b.example').waitFor();
      await dlg.getByText('e.example').waitFor();
      await dlg.locator('[data-list="managers"]').getByText('@ana@b.example').waitFor();
      await dlg.locator('[data-list="moderators"]').getByText('@lee@c.example').waitFor();
      await dlg.getByRole('button', { name: 'Add a manager' }).waitFor();
      await dlg.getByRole('button', { name: 'Add a moderator' }).waitFor();
      await dlg.getByRole('button', { name: 'Listing' }).waitFor();
      await shot(a, '57-hub-channel', { fullPage: false });
      await dlg.locator('.rows > li', { hasText: 'e.example' }).getByRole('button', { name: 'Approve' }).click();
      await dlg.locator('.rows > li', { hasText: 'e.example' }).getByText('this instance').or(dlg.getByText('Instances (4)')).first().waitFor();
      const act = sent.find((s) => s.path.endsWith('/channel/actions'));
      assert(act && act.body.action === 'approve' && act.body.server === 'e.example', 'the approval was not sent: ' + JSON.stringify(act));
      await dlg.getByRole('button', { name: 'Done' }).click();
    }, a);

    await step('hub: the message menu offers "Delete for everyone" to a manager and "Delete here" to a local manager', async () => {
      const theirs = a.locator('#main .msg', { hasText: 'Hub test from Sam' }).last();
      await theirs.hover();
      await theirs.getByRole('button', { name: 'More actions' }).click();
      await a.getByRole('menuitem', { name: 'Delete for everyone…' }).waitFor();
      await a.keyboard.press('Escape');
      // The hub signs a new record without kaity: stream.channel_changed.
      await a.evaluate(async ([cid, rec]) => {
        const s = await import('/chat/store.js');
        s.apply({ type: 'stream.channel_changed', at: new Date().toISOString(), conversation_id: cid, stream_id: rec.stream_id, channel: { ...rec, invited: [], requests: [], managers: rec.managers.slice(1), version: rec.version + 1 } });
      }, [general.id, record]);
      await a.locator('#main .msg .msg-head .tag', { hasText: 'manager' }).first().waitFor({ state: 'detached' });
      await theirs.hover();
      await theirs.getByRole('button', { name: 'More actions' }).click();
      await a.getByRole('menuitem', { name: 'Delete here…' }).click();
      await a.getByRole('dialog').getByText(/deleted here and stays on the other instances/).waitFor();
      await a.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
    }, a);

    await step('hub: in a private hub channel a moderator deletes for everyone, admits from the requests and the candidates, and has the join code', async () => {
      // The hub signs the channel private, with kaity a moderator, not a manager.
      record = { ...record, version: record.version + 2, private: true, listing: 'unlisted', invited: [], requests: [], managers: record.managers.slice(1), moderators: [...record.moderators, { account_id: me.id, server: own, username: me.username }] };
      await a.evaluate(async ([cid, rec]) => {
        const s = await import('/chat/store.js');
        s.apply({ type: 'stream.channel_changed', at: new Date().toISOString(), conversation_id: cid, stream_id: rec.stream_id, channel: rec });
      }, [general.id, record]);
      await a.locator('#main .msg', { hasText: 'Hub test from Kaity' }).last().locator('.msg-head .tag', { hasText: 'moderator' }).waitFor();
      const theirs = a.locator('#main .msg', { hasText: 'Hub test from Sam' }).last();
      await theirs.hover();
      await theirs.getByRole('button', { name: 'More actions' }).click();
      await a.getByRole('menuitem', { name: 'Delete for everyone…' }).waitFor();
      await a.keyboard.press('Escape');
      await a.getByRole('button', { name: /^Members/ }).click();
      const dlg = a.getByRole('dialog');
      await dlg.getByText('Join code').waitFor();
      await dlg.locator('.join-code code').getByText(STREAM).waitFor();
      await dlg.getByText(/enter it on their own instance/).waitFor();
      assert(await dlg.getByRole('button', { name: 'Add a group' }).count() === 0, 'the ordinary adding is offered in a private hub channel');
      const rio = dlg.locator('.join-requests > li', { hasText: 'Rio' });
      await rio.getByText('@rio@b.example').waitFor();
      await shot(a, '92-hub-private-members', { fullPage: false });
      await rio.getByRole('button', { name: 'Admit' }).click();
      await dlg.getByText('Nobody is asking to join.').waitFor();
      const admitted = sent.find((x) => x.path.endsWith('/channel/members'));
      assert(admitted && admitted.body.action === 'admit' && admitted.body.account_id === '0193e000-0000-7000-8000-0000000000f1', 'the admission was not sent: ' + JSON.stringify(admitted));
      await dlg.getByRole('button', { name: 'Add people' }).click();
      const pick = a.getByRole('dialog', { name: /^Add to/ });
      await pick.getByRole('button', { name: /Sam/ }).click();
      await pick.getByRole('button', { name: 'Admit' }).click();
      await a.locator('.toast', { hasText: 'Their instance adds them.' }).waitFor();
      const both = sent.filter((x) => x.path.endsWith('/channel/members'));
      assert(both.length === 2 && both[1].body.account_id === samId && both[1].body.action === 'admit', 'the candidate was not admitted: ' + JSON.stringify(both));
      // The hub pane: private, so no listing change; no manager tools for a moderator.
      await a.getByRole('tab', { name: 'On the hub' }).click();
      await dlg.getByText(/Private for good/).waitFor();
      assert(await dlg.getByRole('button', { name: 'Listing' }).count() === 0, 'a listing change is offered for a private channel');
      await a.getByRole('dialog').getByRole('button', { name: 'Done' }).click();
    }, a);

    await step('hub: anyone asks to join a private hub channel with its join code from Browse channels', async () => {
      await a.goto(origin + '/chat/browse');
      const box = a.locator('.ask-code');
      await box.getByLabel('Join code').fill(STREAM);
      await box.getByRole('button', { name: 'Ask to join' }).click();
      await box.getByText(/^Asked\. A moderator or manager/).waitFor();
      const ask = sent.find((x) => x.method === 'POST' && x.path === `/api/v1/streams/${STREAM}/channel/requests`);
      assert(ask, 'the request was not sent');
      // What a wrong code says is checked without a browser (e2e/hub.mjs):
      // a refused request would be logged as a console error here.
    }, a);
  } finally {
    await a.unrouteAll({ behavior: 'ignoreErrors' });
  }
}
