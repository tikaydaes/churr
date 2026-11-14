// Time-outs and bans: the current ones, and lifting them.

import { get, del } from '/shared/api.js';
import { h, button, busy, toast, dateTime, until } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';
import { people, name, handle } from '../kit.js';

export default async function timeouts(ctx) {
  const t = card('Time-outs');
  const b = card('Bans');
  const [timeoutsGot, bansGot] = await Promise.all([
    tryLoad(() => get('/admin/timeouts', { limit: 200 })), tryLoad(() => get('/admin/bans', { limit: 200 }))]);
  const ids = [];
  if (timeoutsGot.ok) ids.push(...timeoutsGot.ok.timeouts.flatMap((x) => [x.account_id, x.created_by_id]));
  if (bansGot.ok) ids.push(...bansGot.ok.bans.flatMap((x) => [x.account_id, x.created_by_id]));
  await people(ids);
  const lift = (path, said) => { const btn = button('Lift', () => busy(btn, async () => { await del(path); toast(said); ctx.redraw(); }), 'small'); return btn; };

  const current = timeoutsGot.ok ? timeoutsGot.ok.timeouts.filter((x) => !x.lifted_at && new Date(x.until) > new Date()) : [];
  t.append(h('p', { class: 'dim' }, 'Server-wide time-outs and those in public channels. A timed-out account can read and cannot post, react, answer forms or join calls. Time someone out from People.'),
    timeoutsGot.problem || (current.length ? h('ul', { class: 'rows' }, current.map((x) => h('li', {},
      h('div', { class: 'what' }, h('strong', {}, name(x.account_id), ' ', h('small', { class: 'dim' }, handle(x.account_id)), ' ', h('span', { class: 'tag line' }, x.conversation_id ? 'one channel' : 'whole server')),
        h('small', {}, `Until ${dateTime(x.until)} (${until(x.until)} left) · by ${name(x.created_by_id)}${x.reason ? ' · “' + x.reason + '”' : ''}`)),
      h('div', { class: 'does' }, lift('/admin/timeouts/' + x.id, 'Time-out lifted.'))))) : h('p', { class: 'empty' }, 'Nobody is timed out.')));

  const bans = bansGot.ok ? bansGot.ok.bans.filter((x) => !x.lifted_at) : [];
  b.append(h('p', { class: 'dim' }, 'A ban is a removal plus a record that stops the same person coming back. Lifting it does not bring the removed account back.'),
    bansGot.problem || (bans.length ? h('ul', { class: 'rows' }, bans.map((x) => h('li', {},
      h('div', { class: 'what' }, h('strong', {}, x.account_id ? [name(x.account_id), ' ', h('small', { class: 'dim' }, handle(x.account_id))] : ['Identity ', h('code', {}, x.subject || '?')]),
        h('small', {}, `Since ${dateTime(x.created_at)} · by ${name(x.created_by_id)}${x.reason ? ' · “' + x.reason + '”' : ''}`)),
      h('div', { class: 'does' }, lift('/admin/bans/' + x.id, 'Ban lifted.'))))) : h('p', { class: 'empty' }, 'Nobody is banned.')));
  return [t, b];
}
