// A message shown inside the console: in a report, a purge request. It is
// drawn with the same renderer as the chat, with only the names the
// console knows.

import { h, dateTime } from '/shared/dom.js';
import { renderMarkdown } from '/shared/md/render.js';
import { name } from './kit.js';

export function quotedMessage(m, channelName) {
  if (!m) return h('div', { class: 'quoted-message dim' }, 'The message can no longer be read.');
  const parts = [];
  if (m.deleted_at) parts.push(h('p', { class: 'dim' }, 'This message was deleted. Its last content is in its history.'));
  else if (m.text) parts.push(renderMarkdown(m.text, m.entities, {}));
  if ((m.files || []).length) parts.push(h('p', { class: 'dim small' }, m.files.map((f) => f.name).join(', ')));
  if (m.form) parts.push(h('p', { class: 'dim small' }, 'A form: ' + (m.form.title || 'untitled')));
  if (!parts.length) parts.push(h('p', { class: 'dim' }, m.fallback || 'Nothing to show.'));
  return h('div', { class: 'quoted-message' },
    h('div', { class: 'by' }, `${name(m.author_id)}${channelName ? ' in ' + channelName : ''} · ${dateTime(m.created_at)}${m.edited_at ? ' · edited' : ''}`), parts);
}
