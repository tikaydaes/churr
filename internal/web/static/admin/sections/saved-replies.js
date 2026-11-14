// Saved replies: the community's /name commands. Typing /name in the
// composer posts the text.

import { get, post, patch, del } from '/shared/api.js';
import { h, button, busy, confirmDialog } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';

export default async function savedReplies(ctx) {
  const replies = (await get('/saved-replies')).saved_replies.filter((r) => r.community);
  const fields = (r) => [
    { name: 'name', label: 'Command', required: true, code: true, value: r && r.name, maxLength: 32, hint: 'Typed as /name. It must not be a built-in command or a bot’s.' },
    { name: 'text', label: 'What it posts', type: 'textarea', required: true, value: r && r.text, rows: 6, hint: 'Churr Markdown.' },
  ];
  const add = button('New saved reply', async () => { if (await formDialog('New saved reply', fields(null), { yes: 'Create', submit: (v) => post('/admin/saved-replies', v) })) ctx.redraw(); }, 'small primary');
  return card(h('span', { class: 'row grow between' }, 'Community saved replies', add),
    h('p', { class: 'dim' }, 'Everyone can use these. Each person also keeps saved replies of their own.'),
    replies.length ? h('ul', { class: 'rows' }, replies.map((r) => {
      const edit = button('Change', async () => { if (await formDialog('Change /' + r.name, fields(r), { submit: (v) => patch('/admin/saved-replies/' + r.id, v) })) ctx.redraw(); }, 'small');
      const remove = button('Delete', async () => {
        if (!(await confirmDialog(`Delete /${r.name}?`, 'The command stops working for everyone.', { yes: 'Delete', danger: true }))) return;
        await busy(remove, async () => { await del('/admin/saved-replies/' + r.id); ctx.redraw(); });
      }, 'small danger');
      return h('li', {}, h('div', { class: 'what' }, h('strong', {}, h('code', {}, '/' + r.name)), h('small', { class: 'clip' }, r.text)), h('div', { class: 'does' }, edit, remove));
    })) : h('p', { class: 'empty' }, 'No community saved replies.'));
}
