// How a card is drawn (docs/MESSAGE-FORMAT.md section 6): a fixed layout
// with a coloured bar down its edge. A card never makes the page load
// anything from another site: its pictures are files on this server.

import { h, iconButton, dateTime } from '../dom.js';
import { renderCardText, renderInline } from '../md/render.js';
import { cardTheme } from './theme.js';

const COLOURS = new Set(['lavender', 'cyan', 'pink', 'yellow', 'green', 'red', 'grey']);
const fileURL = (id) => `/api/v1/files/${id}/thumb`;

// renderCards draws the cards of a message. env.md is the Markdown
// context; env.onRemovePreview(index), if given, lets the author or a
// manager take a link preview off; env.nameOf names a theme's author.
export function renderCards(cards, env = {}) {
  if (!cards || !cards.length) return null;
  return h('div', { class: 'cards' }, cards.map((c, i) => card(c, i, env)));
}

function card(c, index, env) {
  // A link to one of this server's themes: the theme card.
  if (c.theme && c.theme.colours) {
    return h('div', { class: 'card theme lavender' },
      c.preview_url && env.onRemovePreview ? iconButton('close', 'Remove this link preview', () => env.onRemovePreview(index), 'small card-remove') : null,
      cardTheme(c, env));
  }
  const md = { ...(env.md || {}), linkBare: true };
  const title = c.title ? (c.url
    ? h('a', { class: 'card-title', href: c.url, target: '_blank', rel: 'noopener noreferrer nofollow' }, c.title)
    : h('div', { class: 'card-title' }, c.title)) : null;
  const fields = (c.fields || []).length ? h('dl', { class: 'card-fields' }, c.fields.map((f) =>
    h('div', { class: f.short ? 'short' : '' }, h('dt', {}, f.label), h('dd', {}, renderInline(f.value, [], md))))) : null;
  return h('div', { class: 'card ' + (COLOURS.has(c.color) ? c.color : 'grey') },
    c.preview_url && env.onRemovePreview ? iconButton('close', 'Remove this link preview', () => env.onRemovePreview(index), 'small card-remove') : null,
    c.author ? h('div', { class: 'card-author' }, c.author.icon_file_id && h('img', { src: fileURL(c.author.icon_file_id), alt: '' }), c.author.name) : null,
    title,
    c.text ? renderCardText(c.text, md) : null,
    fields,
    c.image_file_id ? h('img', { class: 'card-image', src: fileURL(c.image_file_id), alt: c.title || '', loading: 'lazy' }) : null,
    (c.footer || c.at) ? h('div', { class: 'card-footer' }, [c.footer, c.at && dateTime(c.at)].filter(Boolean).join(' · ')) : null);
}
