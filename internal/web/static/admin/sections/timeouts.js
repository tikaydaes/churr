// Time-outs, exiles (the server-wide ban) and channel bans: the current
// ones, and lifting them. "Exile" is always the server-wide one and "ban"
// the one channel's (ruling R72).

import { get, del } from '/shared/api.js';
import { h, button, busy, toast, dateTime, until } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';
import { people, name, handle } from '../kit.js';

export default async function timeouts(ctx) {
  const t = card('Time-outs');
  const b = card('Exiles');
  const cb = card('Channel bans');
  const [timeoutsGot, bansGot, channelBansGot] = await Promise.all([
    tryLoad(() => get('/admin/timeouts', { limit: 200 })), tryLoad(() => get('/admin/bans', { limit: 200 })),
    tryLoad(() => get('/admin/channel-bans'))]);
  const ids = [];
  if (timeoutsGot.ok) ids.push(...timeoutsGot.ok.timeouts.flatMap((x) => [x.account_id, x.created_by_id]));
  if (bansGot.ok) ids.push(...bansGot.ok.bans.flatMap((x) => [x.account_id, x.created_by_id]));
  if (channelBansGot.ok) ids.push(...channelBansGot.ok.bans.flatMap((x) => [x.account_id, x.created_by_id]));
  await people(ids);
  // The channels' names, from the console's own list.
  const channelNames = new Map();
  if (channelBansGot.ok && channelBansGot.ok.bans.length) {
    for (const cid of new Set(channelBansGot.ok.bans.map((x) => x.conversation_id))) {
      try { channelNames.set(cid, (await get('/admin/channels/' + cid)).channel.name); } catch { /* shown without */ }
    }
  }
  const lift = (path, said) => { const btn = button('Lift', () => busy(btn, async () => { await del(path); toast(said); ctx.redraw(); }), 'small'); return btn; };

  const current = timeoutsGot.ok ? timeoutsGot.ok.timeouts.filter((x) => !x.lifted_at && new Date(x.until) > new Date()) : [];
  t.append(h('p', { class: 'dim' }, 'Server-wide time-outs and those in public channels. A timed-out account can read and cannot post, react, answer forms or join calls. Time someone out from People.'),
    timeoutsGot.problem || (current.length ? h('ul', { class: 'rows' }, current.map((x) => h('li', {},
      h('div', { class: 'what' }, h('strong', {}, name(x.account_id), ' ', h('small', { class: 'dim' }, handle(x.account_id)), ' ', h('span', { class: 'tag line' }, x.conversation_id ? 'one channel' : 'whole server')),
        h('small', {}, `Until ${dateTime(x.until)} (${until(x.until)} left) · by ${name(x.created_by_id)}${x.reason ? ' · “' + x.reason + '”' : ''}`)),
      h('div', { class: 'does' }, lift('/admin/timeouts/' + x.id, 'Time-out lifted.'))))) : h('p', { class: 'empty' }, 'Nobody is timed out.')));

  const bans = bansGot.ok ? bansGot.ok.bans.filter((x) => !x.lifted_at) : [];
  b.append(h('p', { class: 'dim' }, 'Exiling someone removes them from the whole server and keeps a record that stops the same person coming back. Lifting it does not bring the removed account back.'),
    bansGot.problem || (bans.length ? h('ul', { class: 'rows' }, bans.map((x) => h('li', {},
      h('div', { class: 'what' }, h('strong', {}, x.account_id ? [name(x.account_id), ' ', h('small', { class: 'dim' }, handle(x.account_id))] : ['Identity ', h('code', {}, x.subject || '?')]),
        h('small', {}, `Since ${dateTime(x.created_at)} · by ${name(x.created_by_id)}${x.reason ? ' · “' + x.reason + '”' : ''}`)),
      h('div', { class: 'does' }, lift('/admin/bans/' + x.id, 'Exile lifted.'))))) : h('p', { class: 'empty' }, 'Nobody is exiled.')));

  const channelBans = channelBansGot.ok ? channelBansGot.ok.bans : [];
  cb.append(h('p', { class: 'dim' }, 'Bans from one named channel, set by its managers or an admin. A banned account cannot join, be added or come back through a group until the ban is lifted or ends.'),
    channelBansGot.problem || (channelBans.length ? h('ul', { class: 'rows' }, channelBans.map((x) => h('li', {},
      h('div', { class: 'what' }, h('strong', {}, name(x.account_id), ' ', h('small', { class: 'dim' }, handle(x.account_id)), ' ', h('span', { class: 'tag line' }, '#' + (channelNames.get(x.conversation_id) || '?'))),
        h('small', {}, `Since ${dateTime(x.created_at)}${x.expires_at ? ' · until ' + dateTime(x.expires_at) : ''} · by ${name(x.created_by_id)}${x.reason ? ' · “' + x.reason + '”' : ''}`)),
      h('div', { class: 'does' }, lift(`/conversations/${x.conversation_id}/bans/${x.account_id}`, 'Channel ban lifted.'))))) : h('p', { class: 'empty' }, 'Nobody is banned from a channel.')));
  return [t, b, cb];
}
