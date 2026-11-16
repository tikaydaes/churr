// Adding a custom emoji from the web chat (docs/CLIENTS.md 9.5): admins
// always may; members when the server's setting allows it. Guests and bots
// never. Where it is not allowed, "Add emoji" is not shown at all.

import { upload } from '/shared/api.js';
import { toast } from '/shared/dom.js';
import { formDialog } from '/shared/form.js';
import { store } from './store.js';
import { mayAddEmoji } from './emojirule.js';

export { mayAddEmoji };

// canAdd says whether the signed-in person may add emoji on this server.
export const canAdd = () => mayAddEmoji(store.me && store.me.role, store.server && store.server.features);

// addEmoji asks for a name and a picture and uploads them. It gives back
// the new emoji, or null when the person backed out.
export async function addEmoji() {
  const out = await formDialog('Add an emoji', [
    { name: 'name', label: 'Name', required: true, code: true, maxLength: 32, hint: 'Typed as :name:. Lowercase letters, digits, underscore, plus and dash.' },
    { name: 'file', label: 'Picture', type: 'file', required: true, accept: 'image/png,image/gif,image/webp,image/jpeg', hint: 'At most 256 KB and 256 pixels square.' },
  ], { yes: 'Add', submit: (v) => {
    const body = new FormData();
    body.append('name', v.name);
    body.append('file', v.file);
    return upload('/emoji', body).promise;
  } });
  if (!out || !out.emoji) return null;
  // The emoji.added event brings it too; it is added now so the picker
  // has it at once.
  store.emoji = [...store.emoji.filter((x) => x.id !== out.emoji.id), out.emoji];
  store.emit('lists');
  toast(`:${out.emoji.name}: is added.`);
  return out.emoji;
}
