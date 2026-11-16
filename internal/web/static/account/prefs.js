// The notification preferences that belong to the account and not to a
// device: the default thread view, notify anyway, whether a meeting pauses
// notifications, whether new chats with the person open at once, and quiet
// hours.

import { get, patch, put, del } from '/shared/api.js';
import { h, button, busy, toast, check, field, dateTime } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';

// The schedule's days are seven characters of 0 and 1, Monday first.
const DAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

export default async function prefs(ctx) {
  const general = card('How you are notified');
  const quiet = card('Quiet hours');
  await Promise.all([drawGeneral(general), drawQuiet(quiet, ctx)]);
  return [general, quiet];
}

async function drawGeneral(el) {
  const got = await tryLoad(() => get('/me/prefs'));
  if (got.problem) { el.append(got.problem); return; }
  const p = got.ok.prefs;
  const save = (body, said) => patch('/me/prefs', body).then(() => toast(said)).catch((err) => toast(err.message, true));
  const view = h('select', { 'aria-label': 'Default thread view', onchange: () => save({ thread_view: view.value }, 'Thread view saved.') },
    h('option', { value: 'threaded', selected: p.thread_view === 'threaded' }, 'Threaded: replies live in their thread'),
    h('option', { value: 'flat', selected: p.thread_view === 'flat' }, 'Flat: replies appear in the channel in order'));
  const anyway = h('input', { type: 'checkbox', class: 'switch', checked: p.allow_notify_anyway, onchange: () => save({ allow_notify_anyway: anyway.checked }, 'Saved.') });
  const meeting = h('input', { type: 'checkbox', class: 'switch', checked: p.meeting_pauses, onchange: () => save({ meeting_pauses: meeting.checked }, 'Saved.') });
  const hide = h('input', { type: 'checkbox', class: 'switch', checked: p.hide_last_seen === true, onchange: () => save({ hide_last_seen: hide.checked }, 'Saved.') });
  // Ruling R78: both off by default, so a new chat with this person is a
  // request they answer.
  const autoDirect = h('input', { type: 'checkbox', class: 'switch', checked: p.auto_approve_direct === true, onchange: () => save({ auto_approve_direct: autoDirect.checked }, 'Saved.') });
  const autoSealed = h('input', { type: 'checkbox', class: 'switch', checked: p.auto_approve_sealed === true, onchange: () => save({ auto_approve_sealed: autoSealed.checked }, 'Saved.') });
  const row = (title, hint, control) => h('div', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, title), h('small', {}, hint)), h('div', { class: 'control' }, control));
  el.append(
    row('Default thread view', 'Each conversation can override this from its own menu.', view),
    h('label', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, 'Allow “notify anyway”'), h('small', {}, 'While you are in do not disturb, a sender may push one message through to you.')), h('div', { class: 'control' }, anyway)),
    h('label', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, 'A meeting pauses notifications'), h('small', {}, 'While your availability says you are in a meeting, you are in do not disturb.')), h('div', { class: 'control' }, meeting)),
    h('label', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, 'Hide when I was last here'), h('small', {}, 'Your profile otherwise says roughly when you were last here: recently, within a week, or a while ago. Never the time.')), h('div', { class: 'control' }, hide)),
    h('label', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, 'Auto approve new direct chats'), h('small', {}, 'Off: someone starting a new direct chat with you asks first, and you accept, decline, dismiss or block. On: it opens straight away.')), h('div', { class: 'control' }, autoDirect)),
    h('label', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, 'Auto approve new sealed chats'), h('small', {}, 'The same for new sealed chats.')), h('div', { class: 'control' }, autoSealed)));
}

async function drawQuiet(el, ctx) {
  const redraw = () => { el.replaceChildren(h('h2', {}, 'Quiet hours')); return drawQuiet(el, ctx); };
  const got = await tryLoad(() => get('/me/quiet-hours'));
  if (got.problem) { el.append(got.problem); return; }
  const q = got.ok.quiet_hours;
  const start = h('input', { type: 'time', value: q ? q.start : '22:00', required: true });
  const end = h('input', { type: 'time', value: q ? q.end : '07:00', required: true });
  const on = q && /^[01]{7}$/.test(q.days) ? q.days : '1111111';
  const boxes = DAYS.map((label, i) => ({ input: h('input', { type: 'checkbox', checked: on[i] === '1' }), label }));
  const save = h('button', { type: 'submit', class: 'btn primary' }, q ? 'Save quiet hours' : 'Turn quiet hours on');
  const form = h('form', { class: 'stack', onsubmit: (e) => {
    e.preventDefault();
    busy(save, async () => {
      const days = boxes.map((b) => (b.input.checked ? '1' : '0')).join('');
      if (!days.includes('1')) throw new Error('Choose at least one day.');
      await put('/me/quiet-hours', { start: start.value, end: end.value, days });
      toast('Quiet hours saved.');
      redraw();
    });
  } },
  h('div', { class: 'cols-2' }, field('From', start), field('Until', end)),
  h('fieldset', { class: 'plain' }, h('legend', { class: 'label' }, 'On the nights of'), h('div', { class: 'row' }, boxes.map((b) => check(b.label, b.input)))),
  h('p', { class: 'dim small' }, `Times are in your time zone${q ? ', ' + q.time_zone : ctx.me.time_zone ? ', ' + ctx.me.time_zone : ''}. Set the zone on your profile.`),
  h('p', { class: 'row' }, save,
    q && button('Turn off', () => busy(save, async () => { await del('/me/quiet-hours'); toast('Quiet hours are off.'); redraw(); }), 'danger'),
    q && !q.skip_until && button('Skip tonight', () => busy(save, async () => { await put('/me/quiet-hours/skip'); toast('The next quiet period is skipped.'); redraw(); })),
    q && q.skip_until && button('Do not skip', () => busy(save, async () => { await del('/me/quiet-hours/skip'); redraw(); }))));
  el.append(
    h('p', { class: 'dim' }, 'During quiet hours you are in do not disturb: nothing notifies or wakes your devices, and badges keep counting.'),
    q && q.skip_until ? h('p', { class: 'note' }, `Skipped until ${dateTime(q.skip_until)}.`) : '',
    form);
}
