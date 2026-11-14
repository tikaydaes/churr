// Hub directories (owner): for each hub this server is on, the channels
// the hub lists (rulings R56 and R59: listing mode `request` or `open`;
// unlisted channels are never shown), with name, description, listing
// mode, moderators and number of servers; joining an open channel or
// asking to join one on request, choosing its local name, whether it is
// private here and its section; and this server's requests that wait on a
// moderator or were declined.

import { get, post } from '/shared/api.js';
import { h, button, toast, dateTime, plural, fill } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { listingName, joinAction, requests, moderatorName } from '/shared/hub.js';

export default async function hubsSection(ctx) {
  const [peersGot, streamsGot] = await Promise.all([tryLoad(() => get('/admin/peers')), tryLoad(() => get('/admin/streams'))]);
  if (peersGot.problem) return card('Hub directories', peersGot.problem);
  const hubs = peersGot.ok.peers.filter((p) => p.kind === 'hub' && p.state !== 'removed' && p.state !== 'invited');
  if (!hubs.length) {
    return card('Hub directories', h('p', { class: 'empty' }, 'This server is on no hub. Accept a hub’s invitation under Linked servers; its directory then appears here.'));
  }
  const streams = streamsGot.ok ? streamsGot.ok.streams : [];
  const wanted = decodeURIComponent(location.pathname.split('/')[3] || '');
  const hub = hubs.find((p) => p.id === wanted) || hubs[0];
  const sections = await get('/sections').then((r) => r.sections).catch(() => []);

  const picker = hubs.length > 1 ? h('div', { class: 'tabs', role: 'tablist', 'aria-label': 'Hubs' }, hubs.map((p) => h('button', {
    type: 'button', role: 'tab', class: 'tab', 'aria-selected': String(p === hub), onclick: () => ctx.go('hubs', p.id),
  }, p.name || 'A hub'))) : null;

  const list = h('div', {});
  const got = await tryLoad(() => get(`/admin/peers/${hub.id}/directory`));
  const entries = got.ok ? got.ok.channels : [];

  const join = async (e, how) => {
    const out = await formDialog(how === 'ask' ? `Ask to join ${e.name}` : `Join ${e.name}`, [
      { name: 'name', label: 'Its name here', required: true, value: e.name, maxLength: 64, hint: 'People on this server see this name. It need not match the name on the hub.' },
      { name: 'private', label: 'Private here', type: 'checkbox', hint: 'Only the people you add see it on this server.' },
      { name: 'section_id', label: 'Section', type: 'select', value: '', options: [['', 'None'], ...sections.map((x) => [x.id, x.name])] },
    ], { yes: how === 'ask' ? 'Ask to join' : 'Join', intro: [
      how === 'ask' ? `The hub passes the request to the channel’s moderators. The channel is made here now and fills once a moderator approves; if they decline it stays as an archived local channel.`
        : how === 'accept' ? 'A moderator invited this server. It is joined at once.' : 'This channel is open: it is joined at once.',
      `${hub.name} owns the channel; its moderators decide who is in it. Your own levels, pins and slow mode stay yours.`],
    submit: (v) => post(`/admin/peers/${hub.id}/directory/join`, { stream_id: e.stream_id, name: v.name, private: v.private, section_id: v.section_id || undefined }) });
    if (out) { toast(how === 'ask' ? 'Asked. A moderator will approve or decline.' : 'Joined.'); ctx.redraw(); }
  };

  if (got.problem) fill(list, got.problem);
  else if (!entries.length) fill(list, h('p', { class: 'empty' }, `${hub.name} lists no channels. Unlisted channels are never shown: a moderator invites servers to those.`));
  else {
    fill(list, h('ul', { class: 'rows directory' }, entries.map((e) => {
      const a = joinAction(e);
      return h('li', { dataset: { stream: e.stream_id } }, h('div', { class: 'what' },
        h('strong', {}, e.name, ' ', h('span', { class: 'tag ' + (e.listing === 'open' ? 'cyan' : 'yellow') }, listingName(e.listing).toLowerCase()),
          e.state === 'member' ? [' ', h('span', { class: 'tag lavender' }, 'joined')] : e.state === 'requested' ? [' ', h('span', { class: 'tag line' }, 'asked')] : e.state === 'invited' ? [' ', h('span', { class: 'tag pink' }, 'invited')] : null),
        e.description ? h('small', {}, e.description) : null,
        h('small', {}, plural(e.servers, 'server') + ' · moderated by ' + (e.moderators.length ? e.moderators.map(moderatorName).join(', ') : 'nobody')),
        h('small', { class: 'dim' }, a.says)),
      h('div', { class: 'does' }, a.action ? button(a.label, () => join(e, a.action), 'small ' + (a.action === 'ask' ? '' : 'primary')) : null));
    })));
  }

  const r = requests(streams, hub.id);
  const reqRow = (s, said) => h('li', {}, h('div', { class: 'what' }, h('strong', {}, s.offered_name || 'A channel'),
    h('small', {}, said + (s.offered_at ? ' · asked ' + dateTime(s.offered_at) : ''))));

  return [
    card(h('span', { class: 'row grow between' }, `Directory of ${hub.name}`, h('span', { class: 'dim small' }, 'Key ' + (hub.fingerprint || ''))),
      picker,
      h('p', { class: 'dim' }, `Channels ${hub.name} owns and lists for the servers on it. The hub signs this list; it never sees what is said in them.`),
      list),
    card('Requests from this server',
      r.pending.length || r.declined.length ? [
        r.pending.length ? h('ul', { class: 'rows' }, r.pending.map((s) => reqRow(s, 'Waiting for a moderator'))) : null,
        r.declined.length ? [h('h3', { class: 'label' }, 'Declined or removed'), h('ul', { class: 'rows' }, r.declined.map((s) => reqRow(s, 'A moderator declined, or later removed this server. The channel stays here, archived.')))] : null,
      ] : h('p', { class: 'empty' }, 'No requests waiting or declined.')),
  ];
}
