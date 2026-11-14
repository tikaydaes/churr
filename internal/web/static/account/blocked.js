// Accounts this person has blocked.

import { get, del } from '/shared/api.js';
import { h, button, busy, avatar } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';

export default async function blocked(ctx) {
  const el = card('Blocked accounts');
  await draw(el, ctx);
  return el;
}

async function draw(el, ctx) {
  const got = await tryLoad(async () => {
    const ids = (await get('/me/blocks')).account_ids || [];
    const accounts = ids.length ? (await get('/accounts', { ids: ids.join(','), include_removed: true, limit: 200 })).accounts : [];
    return ids.map((id) => accounts.find((a) => a.id === id) || { id, username: 'unknown', display_name: 'An account you can no longer see' });
  });
  const intro = h('p', { class: 'dim' }, ctx.server.features.blocking
    ? 'A blocked account’s messages are collapsed for you, it cannot start a chat with you, and its mentions do not notify you. It is told nothing. Block someone from their profile in the chat.'
    : 'Blocking is turned off on this server.');
  const rows = got.problem || (got.ok.length ? h('ul', { class: 'rows' }, got.ok.map((a) => {
    const un = button('Unblock', () => busy(un, async () => { await del('/me/blocks/' + a.id); el.replaceChildren(h('h2', {}, 'Blocked accounts')); draw(el, ctx); }), 'small');
    return h('li', {}, avatar(a), h('div', { class: 'what' }, h('strong', {}, a.display_name || a.username), h('small', {}, '@' + a.username)), h('div', { class: 'does' }, un));
  })) : h('p', { class: 'empty' }, 'You have blocked nobody.'));
  el.append(intro, rows);
}
