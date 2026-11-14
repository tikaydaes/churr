// The frame the account page and the admin console share: a bar across the
// top, a list of sections down the side, and one section drawn at a time.
// Moving between sections changes the address with the History interface;
// the server answers any address under the area with the same file.

import { api, get, onSignedOut, goToSignIn } from './api.js';
import { h, fill, icon, button, act, avatar } from './dom.js';
import { installFreshCheck } from './fresh.js';
import { rememberServerDefaults } from './themes.js';

// start draws the frame and the section the address names.
//   area      'account' or 'admin'
//   title     the name in the bar
//   sections  [{ id, label, group, load: () => import(…), show: (ctx) => bool }]
//   allow     (me) => true, or a string saying why this person may not be here
export async function start({ area, title, sections, allow }) {
  installFreshCheck();
  onSignedOut(() => goToSignIn());
  const root = document.getElementById('app');
  let me, server;
  try {
    [me, server] = await Promise.all([get('/me').then((r) => r.account), get('/server').then((r) => r.server)]);
  } catch (err) {
    if (err.status === 401) return; // on the way to the sign-in page
    fill(root, h('main', { class: 'console-main' }, h('h1', {}, 'This page could not load'), h('p', { class: 'problem' }, err.message),
      h('p', { class: 'row' }, button('Try again', () => location.reload(), 'primary'))));
    return;
  }
  rememberServerDefaults(server.default_themes);
  const refusal = allow ? allow(me) : true;
  if (refusal !== true) {
    fill(root, h('main', { class: 'console-main' }, h('h1', {}, title), h('p', { class: 'note warn' }, refusal),
      h('p', { class: 'row' }, h('a', { class: 'btn primary', href: '/chat/' }, 'Back to the chat'))));
    return;
  }
  document.title = `${title} · ${server.community_name || 'Churr'}`;

  const ctx = { me, server, area, go, redraw: () => draw(false) };
  const shown = sections.filter((s) => !s.show || s.show(ctx));
  const main = h('main', { class: 'console-main', id: 'main', tabIndex: -1 });
  const nav = h('nav', { class: 'console-nav', 'aria-label': title + ' sections' });
  const navToggle = h('button', { type: 'button', class: 'btn console-nav-toggle', 'aria-expanded': 'false', onclick: () => {
    const open = nav.classList.toggle('open');
    navToggle.setAttribute('aria-expanded', String(open));
  } }, icon('menu'), h('span', {}, 'Sections'));

  const current = () => {
    const id = decodeURIComponent(location.pathname.split('/')[2] || '');
    return shown.find((s) => s.id === id) || shown[0];
  };

  function drawNav() {
    const cur = current();
    const groups = [];
    for (const s of shown) {
      let g = groups.find((x) => x.name === (s.group || ''));
      if (!g) { g = { name: s.group || '', items: [] }; groups.push(g); }
      g.items.push(s);
    }
    fill(nav, groups.map((g) => [
      g.name && h('p', { class: 'console-nav-head' }, g.name),
      h('ul', {}, g.items.map((s) => h('li', {}, h('a', {
        href: `/${area}/${s.id}`, 'aria-current': s === cur ? 'page' : null,
        onclick: (e) => { if (e.metaKey || e.ctrlKey || e.shiftKey || e.button) return; e.preventDefault(); go(s.id); },
      }, s.label, s.badge && s.badge(ctx) ? h('span', { class: 'badge' }, String(s.badge(ctx))) : null)))),
    ]));
    navToggle.lastChild.textContent = cur ? cur.label : 'Sections';
  }

  let drawing = 0;
  async function draw(moveFocus) {
    const mine = ++drawing;
    const s = current();
    drawNav();
    nav.classList.remove('open');
    navToggle.setAttribute('aria-expanded', 'false');
    fill(main, h('h1', {}, s.label), h('p', { class: 'loading' }, 'Loading'));
    try {
      const mod = await s.load();
      const out = await mod.default(ctx);
      if (mine !== drawing) return; // the person moved on while this loaded
      fill(main, h('h1', {}, s.title || s.label), out);
    } catch (err) {
      if (mine !== drawing) return;
      fill(main, h('h1', {}, s.title || s.label), sectionProblem(err));
    }
    if (moveFocus) main.focus({ preventScroll: true });
    window.scrollTo(0, 0);
  }

  function go(id, sub) {
    history.pushState(null, '', `/${area}/${id}${sub ? '/' + sub : ''}`);
    draw(true);
  }
  window.addEventListener('popstate', () => draw(false));

  const links = [
    h('a', { class: 'btn quiet', href: '/chat/' }, icon('chat'), 'Chat'),
    area !== 'account' && h('a', { class: 'btn quiet', href: '/account' }, icon('person'), 'Account'),
    area !== 'admin' && (me.role === 'owner' || me.role === 'admin') && h('a', { class: 'btn quiet', href: '/admin' }, icon('settings'), 'Admin'),
    button([icon('sign-out'), 'Sign out'], () => act(async () => {
      try { localStorage.removeItem('churr.' + me.id); } catch { /* nothing kept */ }
      // Sealed-chat keys leave the device list and are deleted first
      // (docs/SEALED.md section 14).
      await import('./sealed/session.js').then((m) => m.signOutSealed(me, server)).catch(() => {});
      await api('POST', '/logout', {});
      location.assign('/login');
    }), 'quiet'),
  ];

  fill(root,
    h('a', { class: 'skip', href: '#main' }, 'Skip to the content'),
    h('header', { class: 'console-bar' },
      h('a', { class: 'console-brand', href: `/${area}` }, h('img', { src: '/icons/sticker.png', alt: '' }), h('span', {}, title)),
      h('span', { class: 'console-where clip' }, server.community_name || server.name),
      h('span', { class: 'console-me' }, avatar(me, 'small'), h('span', { class: 'clip' }, '@' + me.username)),
      h('div', { class: 'console-links' }, links)),
    h('div', { class: 'console-body' }, h('div', { class: 'console-side' }, navToggle, nav), main));
  draw(false);
}

// What a section shows when its first request failed. An area of the
// server that is not built answers `unavailable`, and the page says so
// plainly instead of looking broken.
export function sectionProblem(err) {
  if (err && (err.code === 'unavailable' || err.code === 'feature_off')) {
    return h('div', { class: 'note warn stack tight' },
      h('p', {}, h('strong', {}, 'The server says this is unavailable.')),
      h('p', {}, err.message),
      h('p', { class: 'dim small' }, 'This screen calls the operation the documents describe. It will work, with no change here, once the server answers it.'));
  }
  if (err && err.code === 'forbidden') return h('p', { class: 'note warn' }, err.message || 'You may not see this.');
  return h('p', { class: 'problem', role: 'alert' }, (err && err.message) || 'Something went wrong.');
}

// tryLoad runs a request for a part of a screen and returns either its
// answer or a node that says why there is none.
export async function tryLoad(fn) {
  try { return { ok: await fn() }; } catch (err) { return { problem: sectionProblem(err), err }; }
}

// card is one titled block of a section.
export function card(title, ...kids) {
  return h('section', { class: 'console-card' }, title && h('h2', {}, title), kids);
}
