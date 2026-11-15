// Saved replies: the community's /name commands. Typing /name in the
// composer posts the text.

import { get, post, patch, del } from '/shared/api.js';
import { h, button, busy, confirmDialog, toast } from '/shared/dom.js';
import { isBuiltin, STRINGS } from '/shared/commands.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';

export default async function savedReplies(ctx) {
  const replies = (await get('/saved-replies')).saved_replies.filter((r) => r.community);
  const fields = (r) => [
    { name: 'name', label: 'Command', required: true, code: true, value: r && r.name, maxLength: 32, hint: 'Typed as /name. Not a bot’s command. A built-in’s name is allowed: this one then replaces it on this server.' },
    { name: 'text', label: 'What it posts', type: 'textarea', required: true, value: r && r.text, rows: 6, hint: 'Churr Markdown.' },
  ];
  // A built-in of the same name is replaced on this server (ruling R72):
  // say so once, when it happens.
  const saved = (out) => { if (!out) return; if (out.saved_reply && isBuiltin(out.saved_reply.name)) toast(STRINGS.replacesBuiltin(out.saved_reply.name)); ctx.redraw(); };
  const add = button('New saved reply', async () => saved(await formDialog('New saved reply', fields(null), { yes: 'Create', submit: (v) => post('/admin/saved-replies', v) })), 'small primary');
  return card(h('span', { class: 'row grow between' }, 'Community saved replies', add),
    h('p', { class: 'dim' }, 'Everyone can use these. Each person also keeps saved replies of their own.'),
    replies.length ? h('ul', { class: 'rows' }, replies.map((r) => {
      const edit = button('Change', async () => saved(await formDialog('Change /' + r.name, fields(r), { submit: (v) => patch('/admin/saved-replies/' + r.id, v) })), 'small');
      const remove = button('Delete', async () => {
        if (!(await confirmDialog(`Delete /${r.name}?`, 'The command stops working for everyone.', { yes: 'Delete', danger: true }))) return;
        await busy(remove, async () => { await del('/admin/saved-replies/' + r.id); ctx.redraw(); });
      }, 'small danger');
      return h('li', {}, h('div', { class: 'what' }, h('strong', {}, h('code', {}, '/' + r.name)), h('small', { class: 'clip' }, r.text)), h('div', { class: 'does' }, edit, remove));
    })) : h('p', { class: 'empty' }, 'No community saved replies.'));
}
