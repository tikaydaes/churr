// Editing reference patterns (ruling R73): the server's in the admin
// console, a channel's in its settings. One editor for both: a list with
// an example of each, and a dialog to add or change one. The server checks
// every field and says why it refuses.

import { get, post, patch, del } from '../api.js';
import { h, button, busy, toast, confirmDialog } from '../dom.js';
import { formDialog } from '../form.js';
import { t } from './words.js';

// example is what a pattern turns into a link, for the list.
function example(p) {
  const v = p.expression ? 'ABC-1' : '123';
  return `${p.prefix}${p.expression ? '…' : v} → ${p.url_template.split('{value}').join(p.expression ? '…' : v)}`;
}

const fields = (p = {}) => [
  { name: 'name', label: t('patternName'), required: true, maxLength: 64, value: p.name },
  { name: 'prefix', label: t('patternPrefix'), required: true, maxLength: 16, value: p.prefix, hint: t('patternPrefixHelp'), code: true },
  { name: 'expression', label: t('patternExpression'), maxLength: 64, value: p.expression, hint: t('patternExpressionHelp'), code: true },
  { name: 'url_template', label: t('patternURL'), required: true, maxLength: 500, value: p.url_template, hint: t('patternURLHelp'), code: true, placeholder: 'https://tracker.example/browse/{value}' },
];

// patternEditor draws the patterns of one scope: the server's (no
// conversation) or one channel's. canEdit says whether the reader may
// change them; redraw is called after a change.
export async function patternEditor({ conversationId = null, canEdit = true, redraw }) {
  const all = (await get('/reference-patterns', conversationId ? { conversation_id: conversationId } : {})).patterns || [];
  const mine = all.filter((p) => (conversationId ? p.conversation_id === conversationId : !p.conversation_id));
  const add = canEdit ? button(t('patternAdd'), async () => {
    const v = await formDialog(t('patternAdd'), fields(), { yes: t('patternSave'),
      submit: (x) => post('/reference-patterns', { ...x, conversation_id: conversationId || undefined }) });
    if (v) redraw();
  }, 'small primary') : null;
  const list = mine.length ? h('ul', { class: 'rows' }, mine.map((p) => {
    const change = canEdit ? button(t('patternEdit'), async () => {
      const v = await formDialog(t('patternEdit'), fields(p), { yes: t('patternSave'), submit: (x) => patch('/reference-patterns/' + p.id, x) });
      if (v) redraw();
    }, 'small') : null;
    const remove = canEdit ? button(t('patternRemove', { name: p.name }), async () => {
      if (!(await confirmDialog(t('patternRemove', { name: p.name }), t('patternRemoveAsk', { name: p.name }), { yes: 'Remove', danger: true }))) return;
      await busy(remove, async () => { try { await del('/reference-patterns/' + p.id); redraw(); } catch (err) { if (!err.silent) toast(err.message, true); } });
    }, 'small danger') : null;
    return h('li', {}, h('div', { class: 'what' }, h('strong', {}, p.name), h('small', { class: 'dim mono' }, t('patternExample', { example: example(p) }))),
      canEdit ? h('div', { class: 'does' }, change, remove) : null);
  })) : h('p', { class: 'empty dim' }, t('patternsNone'));
  return h('div', { class: 'stack' }, h('p', { class: 'dim' }, conversationId ? t('patternsChannelAbout') : t('patternsAbout')), add ? h('p', { class: 'row' }, add) : null, list);
}
