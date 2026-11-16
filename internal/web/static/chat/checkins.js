// Recurring check-in forms in a channel's settings (docs/FEATURES.md
// section 11, docs/CLIENTS.md 9.15): its managers list them, set one up,
// change one (whoever changes it becomes its owner) and stop one.

import { get, post, patch, del } from '/shared/api.js';
import { h, openDialog, button, toast, fill, check, dateTime, confirmDialog } from '/shared/dom.js';
import { blank, problem, content, repeat, createBody, fromItem, repeatWords, DAY_NAMES, MAX_QUESTIONS } from '/shared/checkin.js';
import { needAccounts, nameOf } from './store.js';

const zone = () => { try { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'; } catch { return 'UTC'; } };

// drawCheckins fills `body` with the channel's check-ins.
export async function drawCheckins(body, conversationId) {
  fill(body, h('p', { class: 'loading' }, 'Loading'));
  let items;
  try { items = (await get('/scheduled', { conversation_id: conversationId, kind: 'form', limit: 200 })).scheduled; } catch (err) { fill(body, h('p', { class: 'problem' }, err.message)); return; }
  needAccounts(items.map((s) => s.account_id));
  const again = () => drawCheckins(body, conversationId);
  const add = button('New check-in', async () => {
    const v = await editCheckin('New check-in', blank(zone()), (x) => post('/scheduled', createBody(conversationId, x)));
    if (v) { toast('The check-in is set up.'); again(); }
  }, 'small primary');
  fill(body,
    h('p', { class: 'dim' }, 'A check-in posts the same questions on a schedule, as a form. Each person answers once per run, and the answers are replies in that run’s thread. Each run closes when the next is posted.'),
    h('p', { class: 'row' }, add),
    items.length ? h('ul', { class: 'rows', 'data-list': 'checkins' }, items.map((s) => {
      const f = (s.content && s.content.form) || {};
      const edit = button('Edit', async () => {
        const v = await editCheckin('Edit ' + (f.title || 'check-in'), fromItem(s), (x) => patch('/scheduled/' + s.id, { content: content(x), repeat: repeat(x) }));
        if (v) { toast('Saved. You are now its owner.'); again(); }
      }, 'small');
      const stop = button('Stop', async () => {
        if (!(await confirmDialog('Stop this check-in?', 'No more runs are posted. Runs already posted, and their answers, stay.', { yes: 'Stop it', danger: true }))) return;
        try { await del('/scheduled/' + s.id); toast('Stopped.'); again(); } catch (err) { toast(err.message, true); }
      }, 'small danger');
      const state = s.failed_at ? `failed: ${s.failure || 'it could not post'}` : s.next_run_at ? 'next ' + dateTime(s.next_run_at) : 'not running';
      return h('li', { 'data-checkin': f.title || '' },
        h('div', { class: 'what' }, h('strong', {}, f.title || 'Check-in'),
          h('small', {}, `${repeatWords(s.repeat)} · ${(f.fields || []).length} question${(f.fields || []).length === 1 ? '' : 's'} · ${state} · set up by ${nameOf(s.account_id)}`)),
        h('div', { class: 'does' }, edit, stop));
    })) : h('p', { class: 'empty' }, 'No check-ins in this channel.'));
}

// editCheckin shows the editor for a setup and runs `save` with it. It
// gives back what was saved, or undefined when the person backed out.
function editCheckin(name, start, save) {
  const v = structuredClone(start);
  return openDialog(name, (close) => {
    const err = h('p', { class: 'problem', hidden: true, role: 'alert' });
    const title = h('input', { type: 'text', value: v.title, maxLength: 100, 'aria-label': 'Title', oninput: (e) => { v.title = e.target.value; } });
    const text = h('textarea', { rows: 2, 'aria-label': 'Message with it', oninput: (e) => { v.text = e.target.value; } }, v.text);
    const qs = h('ol', { class: 'stack tight checkin-questions' });
    const addQ = button('Add a question', () => { v.questions.push({ label: '', long: false, required: false }); drawQs(); qs.querySelector('li:last-child input').focus(); }, 'small');
    const drawQs = () => {
      addQ.disabled = v.questions.length >= MAX_QUESTIONS;
      fill(qs, v.questions.map((q, i) => h('li', { class: 'row' },
        h('input', { type: 'text', class: 'grow', value: q.label, maxLength: 200, 'aria-label': `Question ${i + 1}`, oninput: (e) => { q.label = e.target.value; } }),
        check('Long', h('input', { type: 'checkbox', checked: q.long, onchange: (e) => { q.long = e.target.checked; } })),
        check('Required', h('input', { type: 'checkbox', checked: q.required, onchange: (e) => { q.required = e.target.checked; } })),
        button('Remove', () => { v.questions.splice(i, 1); drawQs(); }, 'small quiet'))));
    };
    drawQs();
    const days = h('div', { class: 'row', role: 'group', 'aria-label': 'Days' }, DAY_NAMES.map((d, i) => check(d.slice(0, 3), h('input', { type: 'checkbox', 'aria-label': d, checked: v.days[i] === '1',
      onchange: (e) => { v.days = v.days.slice(0, i) + (e.target.checked ? '1' : '0') + v.days.slice(i + 1); } }))));
    const at = h('input', { type: 'time', value: v.at, 'aria-label': 'At', oninput: (e) => { v.at = e.target.value; } });
    const tz = h('input', { type: 'text', value: v.zone, 'aria-label': 'Time zone', oninput: (e) => { v.zone = e.target.value.trim(); } });
    const label = (t, node) => h('label', { class: 'field' }, h('span', { class: 'label' }, t), node);
    const saveBtn = button('Save', async () => {
      const bad = problem(v);
      if (bad) { err.textContent = bad; err.hidden = false; return; }
      saveBtn.disabled = true;
      try { await save(v); close(v); } catch (e) { err.textContent = e.message; err.hidden = false; saveBtn.disabled = false; }
    }, 'primary');
    return {
      body: h('div', { class: 'stack' }, label('Title', title), label('Message with it (optional)', text),
        h('fieldset', { class: 'plain stack tight' }, h('legend', { class: 'label' }, 'Questions'), qs, h('p', { class: 'row' }, addQ)),
        h('fieldset', { class: 'plain stack tight' }, h('legend', { class: 'label' }, 'When'), days, h('div', { class: 'row' }, label('At', at), label('Time zone', tz))),
        err),
      foot: [button('Cancel', () => close(undefined)), saveBtn],
    };
  }, { wide: true });
}
