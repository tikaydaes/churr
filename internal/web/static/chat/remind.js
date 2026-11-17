// /remind in the web chat (docs/FEATURES.md 9.2): the text is read by the
// grammar both clients share (/shared/remind.js) and set with the ordinary
// request to create a scheduled item. With nothing typed after /remind, a
// form asks instead. Used by the composer and by sealed chats, where the
// reminder arrives elsewhere since the server cannot write into one.

import { post } from '/shared/api.js';
import { toast, dateTime } from '/shared/dom.js';
import { formDialog } from '/shared/form.js';
import { accountZone } from '/shared/presets.js';
import { parseRemind, repeatWords, RemindError } from '/shared/remind.js';
import { store, isSealed, conversation } from './store.js';
import { findPerson } from './commands.js';

// chatWith finds or makes the ad-hoc channel with these people: nobody else
// gives the person's own one-person channel.
async function chatWith(ids) {
  const c = (await post('/chats', { account_ids: ids })).conversation;
  if (!store.conversations.has(c.id)) { store.conversations.set(c.id, c); store.emit('conversations'); }
  return c.id;
}

// remind carries out `/remind text` typed in conversationId; clear empties
// the box once the reminder is set.
export async function remind(text, { conversationId, clear }) {
  try {
    if (!text.trim()) { await remindForm(conversationId, clear); return; }
    let r;
    try {
      r = parseRemind(text, new Date(), accountZone(store.me, store.server));
    } catch (err) {
      if (err instanceof RemindError) { toast(err.message, true); return; }
      throw err;
    }
    const target = r.target.kind === 'channel' ? r.target.id
      : await chatWith(r.target.kind === 'person' ? [(await findPerson(r.target.person)).id] : []);
    const body = { conversation_id: target, kind: 'reminder', content: { text: r.what } };
    if (r.at) body.run_at = r.at.toISOString();
    else body.repeat = r.repeat;
    await post('/scheduled', body);
    clear();
    toast(r.at ? `Reminder set for ${dateTime(r.at)}.` : `Reminder set, ${repeatWords(r.repeat)}.`);
  } catch (err) {
    if (!err.silent) toast(err.message || 'That did not work.', true);
  }
}

// remindForm asks what, when and where, for /remind with nothing after it.
async function remindForm(conversationId, clear) {
  const sealed = isSealed(conversation(conversationId) || {});
  const v = await formDialog('Set a reminder', [
    { name: 'what', label: 'Remind about', type: 'textarea', rows: 2, required: true, value: '' },
    { name: 'at', label: 'When', type: 'datetime', required: true, value: new Date(Date.now() + 3600e3) },
    ...(sealed ? [] : [{ name: 'where', label: 'Where it arrives', type: 'radio', value: 'me', options: [['me', 'To me alone'], ['here', 'In this conversation, for everyone']] }]),
  ], { yes: 'Set the reminder' });
  if (!v) return;
  const target = sealed || v.where === 'me' ? await chatWith([]) : conversationId;
  await post('/scheduled', { conversation_id: target, kind: 'reminder', content: { text: v.what }, run_at: v.at });
  clear();
  toast('Reminder set for ' + dateTime(v.at) + '.');
}
