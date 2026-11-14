// The theme editor's live preview: a real conversation drawn with the
// chat's own pieces (the sidebar's rows in every state, messages drawn by
// the message renderer, a poll, a thread marker, the new-messages line,
// buttons, tags and the message box) inside a box that carries the theme
// being edited as its colour tokens. Nothing in it does anything.

import { h, icon, avatar } from '/shared/dom.js';
import { renderMessage } from '/shared/message/message.js';
import { tokens, parse, derive, hex, BUILTIN, KEYS } from '/shared/palette.js';
import { store } from './store.js';
import { presenceDot } from './conversation.js';

const NAMES = ['bg', 'panel', 'raise', 'line', 'grid', 'ink', 'dim', 'faint', 'edge', 'lav', 'cyan', 'pink', 'yel',
  'on-lav', 'on-cyan', 'on-pink', 'on-yel', 'yel-ink', 'on', 'paper'];

const ago = (min) => new Date(Date.now() - min * 60000).toISOString();

function fakeEnv(people, me) {
  const account = (id) => people[id];
  const md = () => ({
    meId: me.id, myGroupIds: new Set(), account, group: () => null, conversation: () => null, emoji: () => null,
    standardEmoji: () => null, tasks: [], canTick: false, onTask: null, onTaskHistory: null, onAccount: () => {}, onConversation: () => {},
  });
  const nothing = () => {};
  return {
    me, account, nameOf: (id) => (people[id] ? people[id].display_name : 'Someone'), conversation: () => null, custom: () => null,
    quoted: () => null, md, blocked: () => false, bookmarked: () => false, threadState: (id) => (id === 'p-1' ? { state: 'following', unread_count: 2 } : null),
    perms: () => ({ member: true, timedOut: false, manager: false, canPost: true, canRemovePreview: false }),
    pickAccounts: nothing, pickChannel: nothing,
    on: new Proxy({}, { get: () => nothing }),
  };
}

function sideRow(name, opts = {}) {
  return h('li', { class: 'side-row' + (opts.cls ? ' ' + opts.cls : '') },
    h('span', { class: 'row-link' },
      h('span', { class: 'g' }, opts.glyph || icon('hash', null, 'small')),
      h('span', { class: 'row-name' }, opts.prefix ? h('span', { class: 'pp' }, opts.prefix) : null, name),
      opts.quiet ? h('span', { class: 'row-quiet' }, icon('bell-off', 'Notifies nothing', 'small')) : null,
      opts.hidden ? h('span', { class: 'tag' }, 'hidden') : null,
      opts.more ? h('span', { class: 'row-kids' }, opts.more + ' more') : null,
      opts.badge ? h('span', { class: 'badge' }, String(opts.badge)) : null));
}

// makePreview returns { el, paint(theme) }.
export function makePreview() {
  const me = { id: 'tp-me', username: store.me ? store.me.username : 'you', display_name: store.me ? (store.me.display_name || store.me.username) : 'You', presence: 'online' };
  const people = {
    [me.id]: me,
    'tp-ana': { id: 'tp-ana', username: 'ana', display_name: 'Ana Reyes', presence: 'online' },
    'tp-ben': { id: 'tp-ben', username: 'ben', display_name: 'Ben Ito', presence: 'away' },
    'tp-kim': { id: 'tp-kim', username: 'kim', display_name: 'Kim Novak', presence: 'offline' },
  };
  const env = fakeEnv(people, me);
  const messages = [
    { id: 'p-1', conversation_id: 'tp', author_id: 'tp-ana', created_at: ago(42), text: 'Morning! The **release notes** are up: [read the draft](https://example.org/notes). Two things left in `build.sh`.',
      reactions: [{ emoji: '👍', count: 3, me: true, account_ids: [me.id, 'tp-ben'] }, { emoji: '🎉', count: 1, account_ids: ['tp-kim'] }],
      thread: { reply_count: 4, participant_ids: ['tp-ben', me.id], last_reply_at: ago(5) } },
    { id: 'p-2', conversation_id: 'tp', author_id: 'tp-ben', created_at: ago(30), text: '@you can you check the last step? It failed twice.', entities: [{ type: 'account', at: 0, len: 4, account_id: me.id }], edited_at: ago(29) },
    { id: 'p-3', conversation_id: 'tp', author_id: 'tp-kim', created_at: ago(12), text: 'When do we ship?',
      form: { preset: 'poll', title: 'Ship on', action: { type: 'tally' }, fields: [{ id: 'when', type: 'choice', label: 'Ship on', options: [{ id: 'thu', label: 'Thursday' }, { id: 'fri', label: 'Friday' }, { id: 'mon', label: 'Next Monday' }] }],
        tally: { when: { thu: 4, fri: 2, mon: 1 } }, answer_count: 7, my_answer: { when: ['thu'] } } },
    { id: 'p-4', conversation_id: 'tp', author_id: me.id, created_at: ago(3), text: 'On it: running `make release` now.' },
  ];
  const log = h('div', { class: 'messages tp-messages' },
    h('div', { class: 'day' }, 'Today'),
    renderMessage(messages[0], env), renderMessage(messages[1], env),
    h('div', { class: 'new-line', role: 'separator' }, h('span', {}, 'New')),
    renderMessage(messages[2], env), renderMessage(messages[3], env));
  for (const el of log.querySelectorAll('[tabindex], button, a, input')) { el.tabIndex = -1; }

  const side = h('div', { class: 'side tp-side' },
    h('div', { class: 'side-brand' }, h('div', { class: 'grow' }, h('strong', { class: 'clip' }, 'Night shift'), h('small', { class: 'conn live' }, h('i', { 'aria-hidden': 'true' }), 'connected'))),
    h('div', { class: 'side-nav' },
      h('span', { class: 'side-link' }, h('span', { class: 'g' }, icon('bell', null, 'small')), h('span', { class: 'grow' }, 'Notifications'), h('span', { class: 'badge' }, '2')),
      h('span', { class: 'side-link', 'aria-current': 'page' }, h('span', { class: 'g' }, icon('thread', null, 'small')), h('span', { class: 'grow' }, 'Threads'))),
    h('section', { class: 'side-group' }, h('h2', {}, h('span', { class: 'sec', 'aria-expanded': 'true' }, h('span', { class: 'grow' }, 'Channels'), h('em', {}, '7'))),
      h('ul', {},
        sideRow('general', { cls: 'current' }),
        sideRow('design', { cls: 'unread', badge: 3 }),
        sideRow('critique', { cls: 'child' }),
        sideRow('random'),
        sideRow('releases', { cls: 'quiet', quiet: true }),
        sideRow('ops', { more: 2, cls: 'has-kids' }),
        sideRow('archive', { cls: 'hidden-row', hidden: true }),
        sideRow('budget', { glyph: icon('lock', null, 'small') }))),
    h('section', { class: 'side-group' }, h('h2', {}, h('span', { class: 'sec', 'aria-expanded': 'true' }, h('span', { class: 'grow' }, 'Chats'), h('em', {}, '3'))),
      h('ul', {},
        sideRow('Ana Reyes', { glyph: presenceDot(people['tp-ana']), cls: 'unread', badge: 1 }),
        sideRow('Ben Ito', { glyph: presenceDot(people['tp-ben']) }),
        sideRow('Kim Novak', { glyph: presenceDot(people['tp-kim']) }))),
    h('div', { class: 'side-me' }, h('span', { class: 'me-status' }, avatar(me), h('span', { class: 'grow clip' }, h('strong', {}, me.display_name), h('small', {}, presenceDot(me), 'Shipping today')))));

  const editor = h('div', { class: 'editor empty', dataset: { placeholder: 'Message #general' } });
  const main = h('div', { class: 'tp-main' },
    h('header', { class: 'conv-head' }, h('div', { class: 'conv-title' }, h('h1', {}, icon('hash'), ' general'), h('small', { class: 'dim' }, 'Ship notes and stand-ups'))),
    log,
    h('div', { class: 'tp-bits' },
      h('div', { class: 'row' },
        h('span', { class: 'btn primary small' }, 'Send'), h('span', { class: 'btn lav small' }, 'Invite'), h('span', { class: 'btn danger small' }, 'Delete'),
        h('span', { class: 'btn small' }, 'Cancel'), h('span', { class: 'btn quiet small' }, 'Later')),
      h('div', { class: 'row' },
        h('span', { class: 'tag lavender' }, 'yours'), h('span', { class: 'tag cyan' }, 'on'), h('span', { class: 'tag pink' }, 'failed'), h('span', { class: 'tag yellow' }, 'waiting'),
        h('span', { class: 'tag line' }, 'bot'), h('a', { href: '#', tabIndex: -1, onclick: (e) => e.preventDefault() }, 'a link'), h('span', { class: 'faint-sample' }, '10:42 · faint text')),
      h('p', { class: 'problem small' }, 'Problem: the build failed on step 3.')),
    h('div', { class: 'composer' }, h('div', { class: 'composer-row' }, editor, h('span', { class: 'btn primary send' }, icon('send'), h('span', {}, 'Send')))));

  const el = h('div', { class: 'theme-preview', 'aria-label': 'Preview of the theme', role: 'img' }, side, main);
  el.setAttribute('inert', '');

  return {
    el,
    paint(theme) {
      const more = document.documentElement.dataset.contrast === 'more' || (document.documentElement.dataset.contrast !== 'normal' && matchMedia('(prefers-contrast: more)').matches);
      const t = tokens(theme, more) || tokensLoose(theme);
      for (const n of NAMES) el.style.setProperty('--' + n, t[n]);
      el.style.setProperty('--sep', more ? t.edge : 'transparent');
      el.style.setProperty('--halo', theme.kind === 'light' ? t.ink : 'transparent');
      el.style.colorScheme = theme.kind;
      el.classList.toggle('failing', !tokens(theme));
    },
  };
}

// A theme that fails is still drawn in the editor, so the person can see
// what is wrong; it is never applied to the page.
function tokensLoose(theme) {
  const got = parse(theme.colours);
  const out = { ...BUILTIN[theme.kind === 'light' ? 'light' : 'dark'] };
  if (got.message) return out;
  const d = derive(got.colours);
  for (const k of KEYS) out[k] = hex(got.colours[k]);
  for (const [k, v] of Object.entries(d)) out[k] = hex(v);
  out.on = out['on-lav'];
  return out;
}
