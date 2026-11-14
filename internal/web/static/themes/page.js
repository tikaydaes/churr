// A theme's own page, /themes/{id}: where a theme link leads (docs/STYLE.md
// "Themes", "Sharing"). It shows the theme as its card, a sample drawn in
// its colours, and the same actions as a theme card in a message: try it
// on this page, add a copy to one's own themes, and use it. Looking at a
// theme needs a signed-in account on this server (getTheme).

import { api, get, post, goToSignIn } from '/shared/api.js';
import { h, button, toast } from '/shared/dom.js';
import { frame, loadServer, whoAmI } from '/shared/door.js';
import { themeCard } from '/shared/themecard.js';
import { tokens, problem } from '/shared/palette.js';
import { tryTheme, useTheme, rememberServerDefaults } from '/shared/themes.js';

const NAMES = ['bg', 'panel', 'raise', 'line', 'ink', 'dim', 'faint', 'edge', 'lav', 'cyan', 'pink', 'yel', 'on-lav', 'on-cyan', 'on-pink', 'on-yel', 'yel-ink', 'on'];

// sample draws a few of the pages' own pieces in the theme's colours.
function sample(theme) {
  const t = tokens(theme);
  const el = h('div', { class: 'theme-sample', role: 'img', 'aria-label': 'A sample drawn with the theme' },
    h('div', { class: 'ts-panel' }, h('strong', {}, '# general'), h('small', {}, 'Ship notes and stand-ups')),
    h('p', {}, 'Text on the ground, ', h('span', { class: 'dim' }, 'secondary text'), ', ', h('a', { href: '#', tabIndex: -1 }, 'a link'), ' and ', h('code', {}, 'code'), '.'),
    h('p', { class: 'row' }, h('span', { class: 'btn primary small' }, 'Send'), h('span', { class: 'btn lav small' }, 'Invite'), h('span', { class: 'btn danger small' }, 'Delete'), h('span', { class: 'btn small' }, 'Cancel')),
    h('p', { class: 'row' }, h('span', { class: 'tag lavender' }, 'yours'), h('span', { class: 'tag cyan' }, 'on'), h('span', { class: 'tag pink' }, 'failed'), h('span', { class: 'tag yellow' }, 'waiting'), h('span', { class: 'badge' }, '3')));
  el.setAttribute('inert', '');
  if (t) {
    for (const n of NAMES) el.style.setProperty('--' + n, t[n]);
    el.style.colorScheme = theme.kind;
  }
  return el;
}

async function start() {
  const server = await loadServer();
  if (server) rememberServerDefaults(server.default_themes);
  const id = decodeURIComponent(location.pathname.split('/')[2] || '');
  if (!/^[0-9a-f-]{36}$/i.test(id)) {
    frame(server, h('h1', {}, 'No ', h('em', {}, 'theme'), ' here'), h('p', {}, 'This address does not name a theme.'), h('p', {}, h('a', { class: 'btn primary', href: '/chat/themes/library' }, 'Open the theme library')));
    return;
  }
  const who = await whoAmI();
  if (!who.account) {
    frame(server, h('h1', {}, 'A colour ', h('em', {}, 'theme')),
      h('p', {}, 'Someone shared a theme on this server. Sign in to see it, try it and add it.'),
      h('p', { class: 'row' }, button('Sign in', () => goToSignIn(), 'primary')));
    return;
  }
  let theme;
  try { theme = (await get('/themes/' + id)).theme; } catch (err) {
    frame(server, h('h1', {}, 'No ', h('em', {}, 'theme'), ' here'), h('p', {}, err.code === 'not_found' ? 'There is no theme at this address. It may have been deleted.' : err.message),
      h('p', {}, h('a', { class: 'btn primary', href: '/chat/themes/library' }, 'Open the theme library')));
    return;
  }
  document.title = `${theme.name} · Theme · ${(server && server.community_name) || 'Churr'}`;
  let author = null;
  try { const a = (await get('/accounts', { ids: theme.owner_id, include_removed: true })).accounts[0]; if (a) author = a.id === who.account.id ? 'you' : (a.display_name || a.username); } catch { /* the card says "Colour theme" */ }
  const mine = theme.owner_id === who.account.id;
  const add = async (e) => {
    const btn = e.currentTarget;
    btn.disabled = true;
    try { await post(`/themes/${theme.id}/add`); btn.textContent = 'Added'; toast(`“${theme.name}” is in your themes.`); } catch (err) { btn.disabled = false; if (!err.silent) toast(err.message, true); }
  };
  const bad = problem(theme);
  frame(server, h('h1', {}, 'A colour ', h('em', {}, 'theme')),
    themeCard(theme, {
      author,
      onTry: bad ? null : () => { try { tryTheme(theme, mine ? [] : [h('button', { type: 'button', class: 'btn small', onclick: add }, 'Add to my themes')]); } catch (err) { toast(err.message, true); } },
      onAdd: mine ? null : add,
      note: mine ? (theme.published ? 'Yours, published in the library.' : 'Yours, not published.') : theme.published ? 'In this server’s theme library.' : null,
      extra: bad ? null : button('Use it', () => { useTheme(theme); toast(`Using “${theme.name}” in this browser.`); }, 'small'),
    }),
    bad ? h('p', { class: 'note warn' }, 'This theme no longer passes the contrast check, so it cannot be applied: ' + bad) : sample(theme),
    h('p', { class: 'dim small' }, 'Trying shows it on this page only. Adding keeps a copy in your themes, which follow you to every device. Using it makes it this browser’s ' + theme.kind + ' theme.'),
    h('p', { class: 'row' }, h('a', { class: 'btn', href: '/chat/themes' }, mine ? 'Edit it' : 'Your themes'), h('a', { class: 'btn', href: '/chat/themes/library' }, 'Theme library'), h('a', { class: 'btn quiet', href: '/chat/' }, 'Back to the chat')));
}

start();
