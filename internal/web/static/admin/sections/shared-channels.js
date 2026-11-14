// Shared channels (owner): offers received and sent; share a channel
// (peer, servers, days of history; on a hub, its listing mode and the
// servers to invite); stop sharing; the file size limit of a shared
// channel; and moving a channel shared over a direct link onto a hub
// (ruling R55, docs/LINK-PROTOCOL.md section 21a): propose, follow its
// state, accept or decline one the other server proposed, withdraw.

import { get, post, patch, del, all } from '/shared/api.js';
import { h, button, toast, confirmDialog, dateTime, bytes } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { LISTINGS, LISTING, ownerStatement, MOVE_STATE, moveActions, movable } from '/shared/hub.js';

export default async function sharedChannels(ctx) {
  const [streamsGot, peersGot, movesGot] = await Promise.all([tryLoad(() => get('/admin/streams')), tryLoad(() => get('/admin/peers')), tryLoad(() => get('/admin/moves'))]);
  if (streamsGot.problem) return card('Shared channels', h('p', { class: 'dim' }, 'A channel shared with other servers appears on each of them, with its own name there.'), streamsGot.problem);
  const streams = streamsGot.ok.streams;
  const peers = peersGot.ok ? peersGot.ok.peers.filter((p) => p.state === 'active') : [];
  const hubs = peers.filter((p) => p.kind === 'hub');
  const moves = movesGot.ok ? movesGot.ok.moves : [];
  const [sections, channels] = await Promise.all([get('/sections').then((r) => r.sections).catch(() => []), all('/admin/channels', 'channels').catch(() => [])]);
  const chan = (id) => (channels.find((c) => c.id === id) || {}).name;
  const peerOf = (id) => peers.find((p) => p.id === id);
  const run = async (fn, said) => { try { await fn(); if (said) toast(said); ctx.redraw(); } catch (err) { if (!err.silent) toast(err.message, true); } };
  const isHub = (v) => !!hubs.find((p) => p.id === v.peer_id);

  const share = button('Share a channel', async () => {
    const out = await formDialog('Share a channel', [
      { name: 'conversation_id', label: 'Channel', type: 'select', options: channels.filter((c) => !c.archived_at).map((c) => [c.id, c.name]) },
      { name: 'peer_id', label: 'Through', type: 'select', options: peers.map((p) => [p.id, (p.name || 'a link') + (p.kind === 'hub' ? ' (hub)' : '')]) },
      // On a hub: the statement, the listing mode and whom to invite.
      ...hubs.map((p) => ({ when: (v) => v.peer_id === p.id, node: h('div', { class: 'note stack tight hub-statement' }, h('p', {}, h('strong', {}, 'On a hub, the hub owns the channel.')), ownerStatement(p.name).map((t) => h('p', { class: 'small' }, t))) })),
      { name: 'listing', label: 'Listing on the hub', type: 'radio', value: 'unlisted', when: isHub, options: LISTINGS.map((m) => [m, LISTING[m].name, LISTING[m].says]) },
      ...hubs.map((p) => ({ name: 'servers_' + p.id, label: 'Invite these servers (optional)', type: 'checks', when: (v) => v.peer_id === p.id,
        options: (p.servers || []).filter((s) => !s.blocked_at).map((s) => [s.id, s.name, 'Key ' + s.fingerprint]), empty: 'The hub has told this server of no other servers yet. Servers can join later from the directory, or a moderator can invite them.' })),
      { name: 'history_days', label: 'Days of history to send', type: 'number', value: 0, min: 0, max: 90, hint: '0 sends none: the other side starts from now.' },
      { name: 'max_mb', label: 'Largest file, in MB', type: 'number', min: 1, hint: 'Empty uses the server’s default.' },
    ], { yes: 'Share', wide: true, intro: 'Over a direct link the other server gets an offer, which lapses after seven days; its owner chooses the channel’s name there and whether it is public. What is said here is then copied there; deletes and retention become requests to them.',
      submit: (v) => post('/admin/streams', {
        conversation_id: v.conversation_id, peer_id: v.peer_id, history_days: v.history_days || 0, max_blob_bytes: v.max_mb ? v.max_mb * 1048576 : undefined,
        listing: isHub(v) ? v.listing : undefined, server_ids: isHub(v) && (v['servers_' + v.peer_id] || []).length ? v['servers_' + v.peer_id] : undefined,
      }) });
    if (out) { toast(out.stream && out.stream.owner_server ? `Shared. ${out.stream.owner_server} owns it now.` : 'Offer sent.'); ctx.redraw(); }
  }, 'small primary');
  share.disabled = !peers.length || !channels.length;

  const offers = streams.filter((s) => s.state === 'offered');
  const active = streams.filter((s) => s.state === 'active');
  const row = (s, does) => {
    const p = peerOf(s.peer_id);
    const onHub = p && p.kind === 'hub';
    return h('li', {}, h('div', { class: 'what' },
      h('strong', {}, chan(s.conversation_id) || s.offered_name || 'A channel', ' ', h('span', { class: 'tag line' }, onHub ? 'on a hub' : s.kind)),
      s.offered_topic && h('small', {}, s.offered_topic),
      h('small', {}, [onHub ? `Owned by the hub ${s.owner_server || p.name}` : s.owner_server ? 'Owned by ' + s.owner_server : 'Owned by this server', s.servers.length ? 'with ' + s.servers.map((x) => x.name + (x.state !== 'active' ? ` (${x.state})` : '')).join(', ') : null,
        s.history_days ? s.history_days + ' days of history' : null, s.max_blob_bytes ? 'files up to ' + bytes(s.max_blob_bytes) : null, s.offer_expires_at && s.state === 'offered' ? 'offer until ' + dateTime(s.offer_expires_at) : null].filter(Boolean).join(' · '))),
    h('div', { class: 'does' }, does));
  };

  const propose = (s) => button('Move onto a hub', async () => {
    const out = await formDialog('Move this channel onto a hub', [
      { name: 'peer_id', label: 'Hub', type: 'select', options: hubs.map((p) => [p.id, p.name]) },
      { name: 'days', label: 'The proposal stands for', type: 'select', value: '86400', options: [['3600', 'An hour'], ['86400', 'A day'], ['259200', 'Three days'], ['604800', 'A week']] },
    ], { yes: 'Propose the move', wide: true, intro: [
      `Both servers must already be on the hub. ${s.servers.map((x) => x.name).join(', ') || 'The other server'}’s owner is asked, and nothing changes until they accept.`,
      'On acceptance the hub becomes the channel’s owner: the same channel on both servers, with no message lost, doubled or reordered. Every manager of the channel on either server becomes a moderator, and this server keeps no special role.',
      'If the hub refuses, either server cannot be reached before the proposal lapses, or any step fails, nothing changes and the channel carries on directly.'],
    submit: (v) => post(`/admin/streams/${s.id}/move`, { peer_id: v.peer_id, expires_in_seconds: Number(v.days) }) });
    if (out) { toast('Proposed.'); ctx.redraw(); }
  }, 'small');

  const moveRow = (m) => {
    const st = MOVE_STATE[m.state] || { tag: 'grey', says: m.state };
    const can = moveActions(m);
    const stream = streams.find((s) => s.id === m.stream_id);
    const accept = can.accept ? button('Accept', async () => {
      const yes = await confirmDialog(`Move the channel onto ${m.hub_name}?`, [
        h('p', {}, h('strong', {}, m.other_server), ' proposes moving ', h('strong', {}, (stream && chan(stream.conversation_id)) || 'a shared channel'), ' onto the hub ', h('strong', {}, m.hub_name), '.'),
        h('p', {}, 'The hub’s key fingerprint: ', h('code', { class: 'wrap' }, m.hub_fingerprint)),
        'Check that it is the fingerprint of the hub this server is on before going on.',
        'The hub then owns the channel. The managers of the channel here become moderators. The hub’s operator sees its name, description, moderators and servers, and never its messages.',
      ], { yes: 'Accept the move' });
      if (yes) run(() => post(`/admin/moves/${m.id}/accept`), 'Accepted. The move runs by itself.');
    }, 'small primary') : null;
    return h('li', {}, h('div', { class: 'what' },
      h('strong', {}, (stream && chan(stream.conversation_id)) || 'A shared channel', ' onto ', m.hub_name, ' ', h('span', { class: 'tag ' + st.tag }, m.state)),
      h('small', {}, (m.direction === 'outgoing' ? 'Proposed by this server to ' : 'Proposed by ') + m.other_server + ' · ' + dateTime(m.created_at) + (['proposed', 'accepted'].includes(m.state) ? ' · lapses ' + dateTime(m.expires_at) : '')),
      h('small', {}, 'Hub key ', h('code', { class: 'wrap' }, m.hub_fingerprint)),
      h('small', {}, st.says),
      m.state === 'abandoned' && m.reason ? h('small', { class: 'note warn' }, 'Why: ' + m.reason) : null,
      can.notOnHub ? h('small', { class: 'note warn' }, `This server is not on ${m.hub_name}, so it cannot accept. Join the hub under Linked servers first, or decline.`) : null),
    h('div', { class: 'does' }, accept,
      can.decline ? button('Decline', () => run(() => post(`/admin/moves/${m.id}/decline`), 'Declined.'), 'small') : null,
      can.cancel ? button('Withdraw', async () => { if (await confirmDialog('Withdraw this move?', 'The channel carries on directly as before.', { yes: 'Withdraw' })) run(() => post(`/admin/moves/${m.id}/cancel`), 'Withdrawn.'); }, 'small danger') : null));
  };

  return [
    card(h('span', { class: 'row grow between' }, 'Offers', share),
      !peers.length && h('p', { class: 'note' }, 'Link to another server or join a hub first, under Linked servers.'),
      offers.length ? h('ul', { class: 'rows' }, offers.map((s) => row(s, s.owner_server ? [
        button('Accept', async () => {
          const out = await formDialog('Accept this channel', [
            { name: 'name', label: 'Its name here', required: true, value: s.offered_name, maxLength: 64, hint: 'It need not match the name on the other server.' },
            { name: 'private', label: 'Private here', type: 'checkbox' },
            { name: 'section_id', label: 'Section', type: 'select', value: '', options: [['', 'None'], ...sections.map((x) => [x.id, x.name])] },
          ], { yes: 'Accept', submit: (v) => post(`/admin/streams/${s.id}/accept`, { name: v.name, private: v.private, section_id: v.section_id || undefined }) });
          if (out) { toast('Accepted.'); ctx.redraw(); }
        }, 'small primary'),
        button('Decline', () => run(() => post(`/admin/streams/${s.id}/decline`), 'Declined.'), 'small'),
      ] : [button('Withdraw', () => run(() => post(`/admin/streams/${s.id}/close`), 'Withdrawn.'), 'small')]))) : h('p', { class: 'empty' }, 'No offers waiting, sent or received.')),
    card('Shared now', active.length ? h('ul', { class: 'rows' }, active.map((s) => row(s, s.owner_server ? [
      button('Leave', async () => { if (await confirmDialog('Stop taking part?', 'The channel stays here, archived, with the history it has.', { yes: 'Leave', danger: true })) run(() => del('/admin/streams/' + s.id), 'Left.'); }, 'small danger'),
    ] : [
      hubs.length && movable(s, peers, moves) ? propose(s) : null,
      button('File limit', async () => {
        const v = await formDialog('Largest file in this channel', [{ name: 'mb', label: 'Megabytes', type: 'number', min: 1, required: true, value: s.max_blob_bytes ? Math.round(s.max_blob_bytes / 1048576) : '' }],
          { submit: (x) => patch('/admin/streams/' + s.id, { max_blob_bytes: x.mb * 1048576 }) });
        if (v) ctx.redraw();
      }, 'small'),
      button('Stop sharing', async () => { if (await confirmDialog('Stop sharing this channel?', 'Every server detaches and keeps its copy.', { yes: 'Stop sharing', danger: true })) run(() => post(`/admin/streams/${s.id}/close`), 'Sharing stopped.'); }, 'small danger'),
    ]))) : h('p', { class: 'empty' }, 'No channel is shared with another server.')),
    card('Moves onto a hub',
      h('p', { class: 'dim' }, 'A channel shared directly between two servers can move onto a hub both are on, so more servers can join. It is the same channel afterwards; the hub owns it.'),
      movesGot.problem ? movesGot.problem : moves.length ? h('ul', { class: 'rows' }, moves.map(moveRow)) : h('p', { class: 'empty' }, 'No move proposed, by this server or to it.')),
  ];
}
