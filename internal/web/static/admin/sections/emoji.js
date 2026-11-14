// Custom emoji: upload, rename, remove.

import { patch, del, upload, all } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, promptDialog } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { people, name } from '../kit.js';

export default async function emojiSection(ctx) {
  const emoji = await all('/admin/emoji', 'emoji');
  await people(emoji.map((e) => e.created_by_id));
  const add = button('Upload emoji', async () => {
    const out = await formDialog('Upload an emoji', [
      { name: 'name', label: 'Name', required: true, code: true, maxLength: 32, hint: 'Typed as :name:. Lowercase letters, digits, underscore, plus and dash.' },
      { name: 'file', label: 'Picture', type: 'file', required: true, accept: 'image/png,image/gif,image/webp,image/jpeg', hint: 'At most 256 KB and 256 pixels square.' },
    ], { yes: 'Upload', submit: (v) => {
      const body = new FormData();
      body.append('name', v.name);
      body.append('file', v.file);
      return upload('/emoji', body).promise;
    } });
    if (out) ctx.redraw();
  }, 'small primary');
  const own = emoji.filter((e) => !e.server_name);
  return card(h('span', { class: 'row grow between' }, 'Custom emoji', add),
    h('p', { class: 'dim' }, ctx.server.features.members_may_add_emoji ? 'Members may add emoji too (Settings, Messages).' : 'Only admins add emoji (Settings, Messages).'),
    own.length ? h('ul', { class: 'emoji-grid' }, own.map((e) => {
      const rename = button('Rename', async () => {
        const next = await promptDialog('Rename :' + e.name + ':', 'Name', { value: e.name, maxLength: 32 });
        if (next) await busy(rename, async () => { await patch('/admin/emoji/' + e.id, { name: next }); ctx.redraw(); });
      }, 'small');
      const remove = button('Remove', async () => {
        if (!(await confirmDialog(`Remove :${e.name}:?`, 'Messages that used it show the name as text.', { yes: 'Remove', danger: true }))) return;
        await busy(remove, async () => { await del('/admin/emoji/' + e.id); toast('Removed.'); ctx.redraw(); });
      }, 'small danger');
      return h('li', {}, h('img', { src: e.url, alt: '', loading: 'lazy' }),
        h('div', { class: 'what' }, h('strong', { class: 'clip' }, ':' + e.name + ':'), h('small', { class: 'dim clip' }, 'by ' + name(e.created_by_id))), rename, remove);
    })) : h('p', { class: 'empty' }, 'No custom emoji yet.'));
}
