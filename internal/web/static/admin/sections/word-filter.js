// Word filter: words and patterns, each set to block or to flag.

import { get, post, patch, del } from '/shared/api.js';
import { h, button, busy, confirmDialog } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { people, name } from '../kit.js';

export default async function wordFilter(ctx) {
  const filters = (await get('/admin/word-filters')).word_filters;
  await people(filters.map((f) => f.created_by_id));
  const fields = (f) => [
    { name: 'pattern', label: 'Word or pattern', required: true, value: f && f.pattern, maxLength: 200, spellcheck: false },
    { name: 'is_regex', label: 'It is a regular expression', type: 'checkbox', value: f && f.is_regex, hint: 'Otherwise it matches the word, without regard to case.' },
    { name: 'action', label: 'When a message matches', type: 'radio', value: (f && f.action) || 'flag', options: [
      ['flag', 'Flag it', 'The message is posted, and a report is filed for whoever handles reports in that channel.'],
      ['block', 'Block it', 'The post or edit is refused. The author is told the filter refused it.']] },
  ];
  const add = button('New entry', async () => { if (await formDialog('New filter entry', fields(null), { yes: 'Add', submit: (v) => post('/admin/word-filters', v) })) ctx.redraw(); }, 'small primary');
  return card(h('span', { class: 'row grow between' }, 'Words and patterns', add),
    h('p', { class: 'dim' }, 'The filter reads posts and edits in every channel. It cannot read sealed chats.'),
    filters.length ? h('ul', { class: 'rows' }, filters.map((f) => {
      const edit = button('Change', async () => { if (await formDialog('Change filter entry', fields(f), { submit: (v) => patch('/admin/word-filters/' + f.id, v) })) ctx.redraw(); }, 'small');
      const remove = button('Delete', async () => {
        if (!(await confirmDialog('Delete this entry?', 'It stops matching at once.', { yes: 'Delete', danger: true }))) return;
        await busy(remove, async () => { await del('/admin/word-filters/' + f.id); ctx.redraw(); });
      }, 'small danger');
      return h('li', {}, h('div', { class: 'what' }, h('strong', {}, h('code', {}, f.pattern), ' ', h('span', { class: 'tag ' + (f.action === 'block' ? 'red' : 'yellow') }, f.action), ' ', f.is_regex && h('span', { class: 'tag line' }, 'pattern')),
        h('small', {}, 'Added by ' + name(f.created_by_id))), h('div', { class: 'does' }, edit, remove));
    })) : h('p', { class: 'empty' }, 'The filter is empty.'));
}
