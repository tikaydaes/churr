// Checks the logic of the screens for channels on a hub
// (internal/web/static/shared/hub.js) with no server: the browser tests'
// server has no hub, so who is a manager or a moderator, which delete is
// offered, what a directory entry and a move allow, what a private channel
// offers, what asking with a join code says, and the clock figures are
// checked here against the shapes of docs/openapi.yaml (HubChannel,
// HubAccount, HubDirectoryEntry, StreamMove, PeerClock).
//
//   node e2e/hub.mjs

const hub = await import(new URL('../internal/web/static/shared/hub.js', import.meta.url).href);

let failures = 0;
function check(name, fn) {
  try { fn(); console.log(`ok   hub: ${name}`); } catch (e) { failures++; console.log(`FAIL hub: ${name} - ${e.message}`); }
}
const eq = (got, want, what) => { if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`); };

const channel = {
  stream_id: 's1', hub: 'hub.example', version: 7, name: 'Makers', description: 'Things we build', listing: 'request',
  servers: ['a.example', 'b.example'], invited: [], requests: [], key_maker: 'a.example', closed: false, updated_at: '2026-11-14T10:00:00Z',
  managers: [{ account_id: 'acc-kaity', server: 'a.example', username: 'kaity' }, { account_id: 'remote-id', server: 'b.example', username: 'sam' }],
  moderators: [{ account_id: 'acc-lee', server: 'a.example', username: 'lee' }, { account_id: 'remote-ana', server: 'b.example', username: 'ana' }],
  private: false,
};

check('a local account is a manager or moderator by its id under this instance’s name', () => {
  eq(hub.isManager(channel, { id: 'acc-kaity', username: 'kaity' }, 'a.example'), true, 'kaity on a');
  eq(hub.isManager(channel, { id: 'acc-kaity', username: 'kaity' }, 'b.example'), false, 'kaity seen from b as local');
  eq(hub.isManager(channel, { id: 'acc-other', username: 'ana' }, 'a.example'), false, 'ana');
  eq(hub.isModerator(channel, { id: 'acc-lee', username: 'lee' }, 'a.example'), true, 'lee moderates');
  eq(hub.isManager(channel, { id: 'acc-lee', username: 'lee' }, 'a.example'), false, 'lee does not manage');
  eq(hub.isModerator(channel, { id: 'acc-kaity', username: 'kaity' }, 'a.example'), false, 'a manager is not on the moderator list');
});

check('an account from another instance is on a list by username under its instance', () => {
  eq(hub.isManager(channel, { id: 'shadow-1', username: 'sam', server_name: 'b.example' }, 'a.example'), true, 'sam@b');
  eq(hub.isManager(channel, { id: 'shadow-2', username: 'sam', server_name: 'c.example' }, 'a.example'), false, 'sam@c is not sam@b');
  eq(hub.isManager(channel, { id: 'shadow-3', username: 'kaity', server_name: 'b.example' }, 'a.example'), false, 'kaity@b is not kaity@a');
  eq(hub.isModerator(channel, { id: 'shadow-4', username: 'ana', server_name: 'b.example' }, 'a.example'), true, 'ana@b');
  eq(hub.isManager(null, { id: 'x' }, 'a'), false, 'no record');
  eq(hub.isModerator(null, { id: 'x' }, 'a'), false, 'no record');
});

check('the tag beside a name: manager, moderator or none', () => {
  eq(hub.hubRole(channel, { id: 'acc-kaity', username: 'kaity' }, 'a.example'), 'manager', 'kaity');
  eq(hub.hubRole(channel, { id: 'acc-lee', username: 'lee' }, 'a.example'), 'moderator', 'lee');
  eq(hub.hubRole(channel, { id: 'acc-other', username: 'bo' }, 'a.example'), null, 'bo');
  eq(hub.hubRole({ ...channel, moderators: [...channel.moderators, channel.managers[0]] }, { id: 'acc-kaity', username: 'kaity' }, 'a.example'), 'manager', 'on both lists');
});

check('the delete offered: a manager or moderator for everyone, local manager here only', () => {
  eq(hub.deleteChoice({ mine: false, manager: true, hub: null, keeper: false }), { label: 'Delete…', everyone: true }, 'manager off a hub');
  eq(hub.deleteChoice({ mine: false, manager: false, hub: null, keeper: false }), null, 'member off a hub');
  eq(hub.deleteChoice({ mine: false, manager: false, hub: channel, keeper: true }).label, 'Delete for everyone…', 'hub manager or moderator, not a local manager');
  eq(hub.deleteChoice({ mine: false, manager: true, hub: channel, keeper: true }).label, 'Delete for everyone…', 'on the hub lists and a local manager');
  eq(hub.deleteChoice({ mine: false, manager: true, hub: channel, keeper: false }), { label: 'Delete here…', everyone: false }, 'local manager only');
  eq(hub.deleteChoice({ mine: true, manager: false, hub: channel, keeper: false }), { label: 'Delete…', everyone: true }, 'own message');
  eq(hub.deleteChoice({ mine: false, manager: false, hub: channel, keeper: false }), null, 'plain member');
  if (!hub.deleteWords({ label: 'Delete here…', everyone: false }, channel).includes('stays on the other instances')) throw new Error('"Delete here" does not say it stays elsewhere');
  if (!hub.deleteWords({ label: 'Delete for everyone…', everyone: true }, channel).includes('hub.example')) throw new Error('"Delete for everyone" does not name the hub');
});

check('a directory entry: join, ask, accept an invitation, or wait', () => {
  const e = (listing, state) => ({ stream_id: 's', name: 'x', description: '', listing, managers: [], servers: 2, state });
  eq(hub.joinAction(e('open', 'none')).action, 'join', 'open');
  eq(hub.joinAction(e('request', 'none')).action, 'ask', 'request');
  eq(hub.joinAction(e('request', 'invited')).action, 'accept', 'invited');
  eq(hub.joinAction(e('request', 'requested')).action, null, 'requested');
  eq(hub.joinAction(e('open', 'member')).action, null, 'member');
});

check('pending and declined requests are told apart by the stream’s state', () => {
  const streams = [
    { id: '1', peer_id: 'h', kind: 'channel', state: 'offered', conversation_id: 'c1' },
    { id: '2', peer_id: 'h', kind: 'channel', state: 'removed', conversation_id: 'c2' },
    { id: '3', peer_id: 'h', kind: 'channel', state: 'active', conversation_id: 'c3' },
    { id: '4', peer_id: 'other', kind: 'channel', state: 'offered', conversation_id: 'c4' },
    { id: '5', peer_id: 'h', kind: 'channel', state: 'offered' }, // an invitation not yet accepted
  ];
  const r = hub.requests(streams, 'h');
  eq(r.pending.map((s) => s.id), ['1'], 'pending');
  eq(r.declined.map((s) => s.id), ['2'], 'declined');
});

check('a move: who may accept, decline and cancel, and when', () => {
  const m = (direction, state, onHub = true) => ({ id: 'm', stream_id: 's', direction, other_server: 'b', hub_name: 'hub', hub_fingerprint: 'fp', hub_peer_id: onHub ? 'p' : undefined, state });
  eq(hub.moveActions(m('incoming', 'proposed')), { accept: true, decline: true, cancel: false, notOnHub: false }, 'incoming proposed');
  eq(hub.moveActions(m('incoming', 'proposed', false)), { accept: false, decline: true, cancel: false, notOnHub: true }, 'incoming, not on the hub');
  eq(hub.moveActions(m('outgoing', 'proposed')), { accept: false, decline: false, cancel: true, notOnHub: false }, 'outgoing proposed');
  eq(hub.moveActions(m('outgoing', 'accepted')).cancel, true, 'outgoing accepted');
  eq(hub.moveActions(m('outgoing', 'committed')).cancel, false, 'committed cannot be cancelled');
  for (const s of ['proposed', 'accepted', 'committed', 'done', 'declined', 'abandoned']) if (!hub.MOVE_STATE[s]) throw new Error('no words for ' + s);
});

check('only a channel this server owns over a direct link, with no move running, can be moved', () => {
  const peers = [{ id: 'd', kind: 'server' }, { id: 'h', kind: 'hub' }];
  const s = { id: 's', peer_id: 'd', kind: 'channel', state: 'active' };
  eq(hub.movable(s, peers, []), true, 'owned, direct');
  eq(hub.movable({ ...s, owner_server: 'b' }, peers, []), false, 'owned by the other');
  eq(hub.movable({ ...s, peer_id: 'h' }, peers, []), false, 'already on a hub');
  eq(hub.movable(s, peers, [{ stream_id: 's', state: 'proposed' }]), false, 'a move is running');
  eq(hub.movable(s, peers, [{ stream_id: 's', state: 'abandoned' }]), true, 'an abandoned move does not stop another');
});

check('clock figures', () => {
  const c = { offset_ms: -12, latency_ms: 41, jitter_ms: 3, uncertainty_ms: 21, forward_ms: 19, forward_jitter_ms: 2, back_ms: 22, back_jitter_ms: 4, asymmetry_ms: 0, samples: 16, measured_at: '2026-11-14T10:00:00Z' };
  const f = Object.fromEntries(hub.clockFigures(c));
  eq(f.Offset, '−12 ms', 'offset');
  eq(f.Out, '19 ms ± 2 ms', 'out');
  eq(f.Back, '22 ms ± 4 ms', 'back');
  eq(f.Uncertainty, '± 21 ms', 'uncertainty');
  eq('Asymmetry' in f, false, 'no asymmetry when 0');
  eq(hub.ms(12500, true), '+12.5 s', 'seconds');
  eq(hub.clockWarning(c), null, 'no warning');
  if (!hub.clockWarning({ ...c, offset_ms: 31000 })) throw new Error('no warning past 30 s');
  eq(hub.clockFigures(null), [], 'nothing measured');
});

check('the statement before sharing to a hub says who owns it and what the operator sees', () => {
  const s = hub.ownerStatement('hub.example').join(' ');
  for (const part of ['hub.example will own this channel', 'first managers', 'Moderators keep the peace', 'cannot read messages', 'never holds the key']) if (!s.includes(part)) throw new Error('missing: ' + part);
});

check('a channel keeps at least one manager; a closed channel has no tools', () => {
  eq(hub.canRemoveManager(channel), true, 'two');
  eq(hub.canRemoveManager({ ...channel, managers: channel.managers.slice(0, 1) }), false, 'one');
  eq(hub.managerActions({ ...channel, closed: true }), [], 'closed');
  eq(hub.memberActions({ ...channel, closed: true }), [], 'closed, people');
  for (const a of ['add_manager', 'remove_manager', 'add_moderator', 'remove_moderator', 'set_listing']) if (!hub.managerActions(channel).includes(a)) throw new Error('no ' + a);
});

check('a private channel is never listed, and its people are admitted', () => {
  const priv = { ...channel, private: true, listing: 'unlisted' };
  eq(hub.managerActions(priv).includes('set_listing'), false, 'no listing change');
  eq(hub.memberActions(priv), ['admit', 'decline', 'remove'], 'private');
  eq(hub.memberActions(channel), ['remove'], 'not private');
});

check('asking with a join code says what came of it', () => {
  eq(hub.joinCodeAnswer(null).ok, true, 'asked');
  eq(hub.joinCodeAnswer({ code: 'not_found', status: 404 }).says.includes('Check it'), true, 'unknown code');
  eq(hub.joinCodeAnswer({ code: 'invalid_request', status: 400 }).ok, false, 'not a code');
  eq(hub.joinCodeAnswer({ code: 'conflict', status: 409, message: 'You are in it already.' }).says, 'You are in it already.', 'conflict carries the server’s words');
  eq(hub.joinCodeAnswer({ code: 'rate_limited', status: 429 }).ok, false, 'too many');
});

if (failures) process.exitCode = 1;
