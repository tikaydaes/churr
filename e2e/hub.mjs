// Checks the logic of the screens for channels on a hub
// (internal/web/static/shared/hub.js) with no server: the browser tests'
// server has no hub, so who is a moderator, which delete is offered, what
// a directory entry and a move allow, and the clock figures are checked
// here against the shapes of docs/openapi.yaml (HubChannel,
// HubDirectoryEntry, StreamMove, PeerClock).
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
  moderators: [{ account_id: 'acc-kaity', server: 'a.example', username: 'kaity' }, { account_id: 'remote-id', server: 'b.example', username: 'sam' }],
};

check('a local account is a moderator by its id under this server’s name', () => {
  eq(hub.isModerator(channel, { id: 'acc-kaity', username: 'kaity' }, 'a.example'), true, 'kaity on a');
  eq(hub.isModerator(channel, { id: 'acc-kaity', username: 'kaity' }, 'b.example'), false, 'kaity seen from b as local');
  eq(hub.isModerator(channel, { id: 'acc-other', username: 'ana' }, 'a.example'), false, 'ana');
});

check('an account from another server is a moderator by username under its server', () => {
  eq(hub.isModerator(channel, { id: 'shadow-1', username: 'sam', server_name: 'b.example' }, 'a.example'), true, 'sam@b');
  eq(hub.isModerator(channel, { id: 'shadow-2', username: 'sam', server_name: 'c.example' }, 'a.example'), false, 'sam@c is not sam@b');
  eq(hub.isModerator(channel, { id: 'shadow-3', username: 'kaity', server_name: 'b.example' }, 'a.example'), false, 'kaity@b is not kaity@a');
  eq(hub.isModerator(null, { id: 'x' }, 'a'), false, 'no record');
});

check('the delete offered: moderator for everyone, local manager here only', () => {
  eq(hub.deleteChoice({ mine: false, manager: true, hub: null, moderator: false }), { label: 'Delete…', everyone: true }, 'manager off a hub');
  eq(hub.deleteChoice({ mine: false, manager: false, hub: null, moderator: false }), null, 'member off a hub');
  eq(hub.deleteChoice({ mine: false, manager: false, hub: channel, moderator: true }).label, 'Delete for everyone…', 'moderator, not a manager');
  eq(hub.deleteChoice({ mine: false, manager: true, hub: channel, moderator: true }).label, 'Delete for everyone…', 'moderator and manager');
  eq(hub.deleteChoice({ mine: false, manager: true, hub: channel, moderator: false }), { label: 'Delete here…', everyone: false }, 'local manager only');
  eq(hub.deleteChoice({ mine: true, manager: false, hub: channel, moderator: false }), { label: 'Delete…', everyone: true }, 'own message');
  eq(hub.deleteChoice({ mine: false, manager: false, hub: channel, moderator: false }), null, 'plain member');
  if (!hub.deleteWords({ label: 'Delete here…', everyone: false }, channel).includes('stays on the other servers')) throw new Error('"Delete here" does not say it stays elsewhere');
  if (!hub.deleteWords({ label: 'Delete for everyone…', everyone: true }, channel).includes('hub.example')) throw new Error('"Delete for everyone" does not name the hub');
});

check('a directory entry: join, ask, accept an invitation, or wait', () => {
  const e = (listing, state) => ({ stream_id: 's', name: 'x', description: '', listing, moderators: [], servers: 2, state });
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
  for (const part of ['hub.example will own this channel', 'first moderator', 'cannot read messages', 'never holds the key']) if (!s.includes(part)) throw new Error('missing: ' + part);
});

check('a channel keeps at least one moderator; a closed channel has no tools', () => {
  eq(hub.canRemoveModerator(channel), true, 'two');
  eq(hub.canRemoveModerator({ ...channel, moderators: channel.moderators.slice(0, 1) }), false, 'one');
  eq(hub.moderatorActions({ ...channel, closed: true }), [], 'closed');
});

if (failures) process.exitCode = 1;
