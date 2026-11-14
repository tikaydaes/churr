// Ticking checklist items, and an item's history (docs/MESSAGE-FORMAT.md
// section 4). A tick is not an edit: it changes `tasks`, produces
// `task.changed`, and every change is kept.

import { get, put } from '../api.js';
import { h, openDialog, button, toast, dateTime } from '../dom.js';

// toggleTask ticks or unticks one item. The box has already changed on
// screen; if the server refuses, it is put back.
export async function toggleTask(message, itemId, checked, box) {
  try {
    const { task } = await put(`/messages/${message.id}/checklist/${encodeURIComponent(itemId)}`, { checked });
    return task;
  } catch (err) {
    if (box) box.checked = !checked;
    if (!err.silent) toast(err.message, true);
    return null;
  }
}

// taskHistory shows every tick and untick of one item: who, which, when.
export function taskHistory(message, itemId, nameOf) {
  return openDialog('History of this item', (close) => {
    const body = h('div', { class: 'stack' }, h('p', { class: 'loading' }, 'Loading'));
    get(`/messages/${message.id}/checklist/${encodeURIComponent(itemId)}/history`).then(({ changes }) => {
      body.replaceChildren(changes.length
        ? h('ol', { class: 'rows' }, changes.map((c) => h('li', {}, h('div', { class: 'what' },
          h('strong', {}, `${nameOf(c.account_id)} ${c.checked ? 'ticked' : 'unticked'} it`), h('small', {}, dateTime(c.at))))))
        : h('p', { class: 'dim' }, 'Nobody has ticked or unticked this item.'));
    }).catch((err) => body.replaceChildren(h('p', { class: 'problem' }, err.message)));
    return { body, foot: [button('Close', () => close())] };
  });
}
