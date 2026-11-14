// Reactions under a message (docs/MESSAGE-FORMAT.md section 8). A
// reaction is a standard emoji, or `:name:` for a custom one.

import { h, icon } from '../dom.js';

// emojiNode draws one emoji: the character, or the custom emoji's picture.
// custom(name) finds a custom emoji by the name between the colons.
export function emojiNode(emoji, custom) {
  const m = /^:([^:]+):$/.exec(emoji);
  if (!m) return h('span', { class: 'emoji-char' }, emoji);
  const e = custom && custom(m[1]);
  return e ? h('img', { class: 'emoji', src: e.url, alt: emoji }) : h('span', {}, emoji);
}

// renderReactions draws the row. env:
//   custom(name), names(accountIds) → text, canReact
//   onToggle(emoji, on), onAdd(anchor)
export function renderReactions(reactions, env) {
  const list = reactions || [];
  if (!list.length) return null;
  const row = h('div', { class: 'reactions', role: 'group', 'aria-label': 'Reactions' });
  for (const r of list) {
    const who = env.names ? env.names(r.account_ids || []) : '';
    const more = r.count - (r.account_ids || []).length;
    const tip = `${who}${more > 0 ? ` and ${more} more` : ''} reacted with ${r.emoji}`;
    row.append(h('button', {
      type: 'button', class: 'reaction' + (r.me ? ' mine' : ''), 'aria-pressed': String(!!r.me), title: tip, 'aria-label': `${r.emoji}, ${r.count}. ${tip}`,
      disabled: !env.canReact, onclick: () => env.onToggle(r.emoji, !r.me),
    }, emojiNode(r.emoji, env.custom), h('span', { class: 'count' }, String(r.count))));
  }
  if (env.canReact && env.onAdd) {
    row.append(h('button', { type: 'button', class: 'reaction add', 'aria-label': 'Add a reaction', title: 'Add a reaction', onclick: (e) => env.onAdd(e.currentTarget) }, icon('emoji', null, 'small'), '+'));
  }
  return row;
}
