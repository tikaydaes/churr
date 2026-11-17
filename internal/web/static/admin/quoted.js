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

// snapshotView draws what a report kept of what it names, for a report
// whose message the reader cannot read or that is gone: the message's text
// as plain text, its author, time and file names; or a person's names,
// status and picture address. Never the files themselves.
export function snapshotView(r, where) {
  const s = r.snapshot || {};
  if (r.kind === 'account') {
    return h('div', { class: 'quoted-message', dataset: { snapshot: '' } },
      h('div', { class: 'by' }, `${s.display_name || ''} (@${s.username || ''}) as reported`),
      s.status_text ? h('p', {}, 'Status: ' + s.status_text) : null,
      s.avatar_url && s.avatar_url.startsWith('/') ? h('img', { src: s.avatar_url, alt: 'Their picture as reported', width: 64, height: 64 }) : null);
  }
  return h('div', { class: 'quoted-message', dataset: { snapshot: '' } },
    h('div', { class: 'by' }, `${name(s.author_id)}${where ? ' in ' + where : ''} · ${s.created_at ? dateTime(s.created_at) : ''}${s.edited_at ? ' · edited' : ''} · as reported`),
    s.text ? h('p', { class: 'snapshot-text' }, s.text) : h('p', { class: 'dim' }, 'No text.'),
    (s.file_names || []).length ? h('p', { class: 'dim small' }, 'Files: ' + s.file_names.join(', ')) : null);
}
