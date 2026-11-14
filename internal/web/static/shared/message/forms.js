// How a form is drawn and answered (docs/MESSAGE-FORMAT.md section 7).
//
// A form is how a message asks for input: a poll, an RSVP, a sign-up
// sheet, an approval, a ticket form, or any fields at all. A single choice
// of up to five options is answered in the message itself; anything else
// is filled in a dialog opened by "Answer", so the message stays compact.
// A form with a field type, action or preset this client does not know is
// not drawn at all: its fallback text is shown, with a line saying why.

import { put, post, del, all } from '../api.js';
import { h, button, busy, toast, openDialog, promptDialog, confirmDialog, avatar, plural, dateTime, until, fill } from '../dom.js';
import { formDialog } from '../form.js';

const FIELD_TYPES = new Set(['choice', 'text', 'longtext', 'number', 'date', 'time', 'account', 'conversation', 'checkbox']);
const ACTIONS = new Set(['tally', 'collect', 'thread', 'channel', 'group', 'bot']);
const PRESETS = new Set(['poll', 'rsvp', 'signup', 'approve', 'checkin', 'ticket', 'roles']);
const BUTTON_FIELD = '_button'; // what a button press is tallied under

export function understood(form) {
  if (!ACTIONS.has(form.action && form.action.type)) return false;
  if (form.preset && !PRESETS.has(form.preset)) return false;
  return (form.fields || []).every((f) => FIELD_TYPES.has(f.type));
}

// renderForm draws the form of a message. env:
//   me                         the reader's account
//   canAnswer                  the reader is in the conversation and not timed out
//   canClose                   the reader is the author or acts as manager
//   seesResults                the reader may read the answers of a `collect` form
//   account(id), nameOf(id)
//   pickAccounts(title, opts), pickChannel(title)  for those field types
//   onForm(form)               the form changed: draw it again
//   onOpenConversation(id)     a ticket channel was opened
//   onInteraction(id)          a bot was sent the answer and will respond
export function renderForm(message, env) {
  const form = message.form;
  if (!form) return null;
  if (!understood(form)) {
    return h('div', { class: 'form' },
      message.fallback ? h('p', { class: 'form-fallback' }, message.fallback) : null,
      h('p', { class: 'note warn small' }, 'This form uses something this version of the web chat does not know, so it cannot be answered here. Use a newer client.'));
  }
  const closed = !!form.closed_at;
  const open = !closed && env.canAnswer;
  const mine = form.my_answer || {};
  const answered = Object.keys(mine).length > 0;
  const fields = form.fields || [];
  const buttons = form.buttons || [];
  const inlineChoice = fields.length === 1 && fields[0].type === 'choice' && (fields[0].options || []).length <= 5 && !buttons.length;

  const send = async (fn, said) => {
    try {
      const out = await fn();
      if (out && out.form) env.onForm(out.form);
      if (out && out.conversation_id && env.onOpenConversation) env.onOpenConversation(out.conversation_id);
      if (out && out.interaction_id && env.onInteraction) env.onInteraction(out.interaction_id);
      if (said) toast(said);
      return true;
    } catch (err) {
      if (!err.silent) toast(err.message, true);
      return false;
    }
  };
  const answer = (answers) => send(() => put(`/messages/${message.id}/form/answer`, { answers }));
  const press = (b, answers) => send(() => post(`/messages/${message.id}/form/buttons/${encodeURIComponent(b.id)}`, answers ? { answers } : {}));

  const el = h('div', { class: 'form' + (closed ? ' closed' : '') + (form.preset ? ' preset-' + form.preset : '') });
  if (form.title) el.append(h('div', { class: 'form-title' }, form.title));

  // A single choice, answered in place.
  if (inlineChoice) {
    el.append(choiceRows(form, fields[0], mine, open, env, (ids) => (ids.length ? answer({ [fields[0].id]: ids }) : send(() => del(`/messages/${message.id}/form/answer`)))));
  } else if (fields.length) {
    // Fields the message does not show: a summary of what is asked, and
    // the reader's own answer if they gave one.
    el.append(h('ul', { class: 'form-asks' }, fields.map((f) => h('li', {}, f.label, f.required ? '' : h('span', { class: 'dim' }, ' (optional)'),
      mine[f.id] != null && mine[f.id] !== '' ? h('span', { class: 'form-mine' }, ' — ', showValue(f, mine[f.id], env)) : null))));
    for (const f of fields) if (f.type === 'choice' && form.tally && form.tally[f.id]) el.append(tallyBars(form, f, env));
  }

  // The buttons. With fields, pressing one asks for the fields first.
  const row = h('div', { class: 'form-buttons' });
  const ask = (b) => answerDialog(form, env, b ? b.label : 'Send', mine).then((answers) => {
    if (!answers) return undefined;
    return b ? press(b, answers) : answer(answers);
  });
  if (buttons.length) {
    const counts = (form.tally && form.tally[BUTTON_FIELD]) || null;
    for (const b of buttons) {
      const chosen = mine[BUTTON_FIELD] === b.id;
      const btn = h('button', { type: 'button', class: 'btn small ' + (b.style === 'primary' ? 'primary' : b.style === 'danger' ? 'danger' : '') + (chosen ? ' chosen' : ''),
        'aria-pressed': chosen ? 'true' : null, disabled: !open,
        onclick: () => busy(btn, () => (fields.length && !inlineChoice ? ask(b) : press(b))) },
      b.label, counts && counts[b.id] != null ? h('span', { class: 'count' }, String(counts[b.id])) : null);
      row.append(btn);
    }
    // Who pressed what, on a named form whose results the reader may see.
    const voters = form.voters && form.voters[BUTTON_FIELD];
    if (voters) el.append(h('ul', { class: 'form-voters' }, buttons.filter((b) => (voters[b.id] || []).length).map((b) =>
      h('li', {}, h('span', { class: 'label' }, b.label + ' '), faces(voters[b.id], env)))));
  } else if (fields.length && !inlineChoice) {
    const btn = button(answered ? 'Change your answer' : 'Answer', () => ask(null), 'small primary');
    btn.disabled = !open || (answered && form.one_answer !== false && (form.action.type === 'thread' || form.action.type === 'channel'));
    row.append(btn);
  }
  if (row.children.length) el.append(row);

  // The small print: how many answered, when it closes, what the reader
  // may still do.
  const foot = h('div', { class: 'form-foot' });
  const facts = [plural(form.answer_count, 'answer')];
  if (form.anonymous) facts.push('anonymous');
  if (closed) facts.push('closed ' + dateTime(form.closed_at));
  else if (form.closes_at) facts.push('closes in ' + until(form.closes_at));
  if (!closed && form.action.type === 'tally' && !form.tally) facts.push(form.results === 'after_close' ? 'results when it closes' : 'results once you answer');
  foot.append(h('span', {}, facts.join(' · ')));
  if (!env.canAnswer && !closed) foot.append(h('span', { class: 'dim' }, 'Join to answer'));
  if (open && answered && (form.action.type === 'tally' || form.action.type === 'collect') && !inlineChoice) {
    foot.append(h('button', { type: 'button', class: 'link-btn', onclick: () => send(() => del(`/messages/${message.id}/form/answer`), 'Answer withdrawn.') }, 'Withdraw'));
  }
  if (open && form.allow_added_options) {
    const f = fields.find((x) => x.type === 'choice');
    if (f) foot.append(h('button', { type: 'button', class: 'link-btn', onclick: async () => {
      const label = await promptDialog('Add an option', 'Option', { maxLength: 100, yes: 'Add' });
      if (label) send(() => post(`/messages/${message.id}/form/options`, { field_id: f.id, label }));
    } }, 'Add an option'));
  }
  if (env.seesResults && (form.action.type === 'collect' || (form.action.type === 'tally' && !form.anonymous && form.tally))) {
    foot.append(h('button', { type: 'button', class: 'link-btn', onclick: () => resultsDialog(message, env) }, 'Results'));
  }
  if (!closed && env.canClose) {
    foot.append(h('button', { type: 'button', class: 'link-btn', onclick: async () => {
      if (await confirmDialog('Close this form?', 'It keeps its result on the message and takes no more answers.', { yes: 'Close it' })) send(() => post(`/messages/${message.id}/form/close`));
    } }, 'Close'));
  }
  el.append(foot);
  return el;
}

function faces(ids, env) {
  return h('span', { class: 'faces' }, (ids || []).slice(0, 8).map((id) => {
    const a = env.account(id) || { id, display_name: '?' };
    const av = avatar(a, 'small');
    av.title = a.display_name || a.username || '';
    return av;
  }), (ids || []).length > 8 ? h('span', { class: 'dim small' }, ` +${ids.length - 8}`) : null);
}

// A choice of up to five options, drawn as rows. Each row is a button that
// picks or unpicks the option; where the tally is visible the row also
// carries its bar and count, and who chose it on a named form.
function choiceRows(form, field, mine, open, env, onPick) {
  const chosen = new Set(mine[field.id] || []);
  const tally = form.tally && form.tally[field.id];
  const voters = form.voters && form.voters[field.id];
  const total = tally ? Math.max(1, Object.values(tally).reduce((a, b) => a + b, 0)) : 0;
  const list = h('div', { class: 'form-choices', role: 'group', 'aria-label': field.label || form.title || 'Options' });
  for (const o of field.options || []) {
    const n = tally ? (tally[o.id] || 0) : null;
    const on = chosen.has(o.id);
    // A sign-up slot has a number of places; a full one cannot be picked.
    const full = o.max != null && n != null && n >= o.max && !on;
    const bar = h('span', { class: 'bar' });
    if (n != null) bar.style.width = Math.round((n / (o.max || total)) * 100) + '%';
    const btn = h('button', {
      type: 'button', class: 'choice' + (on ? ' chosen' : ''), 'aria-pressed': String(on), disabled: !open || full,
      onclick: () => busy(btn, () => {
        let next;
        if (field.multiple) { next = new Set(chosen); if (on) next.delete(o.id); else next.add(o.id); next = [...next]; }
        else next = on ? [] : [o.id];
        return onPick(next);
      }),
    }, bar,
    h('span', { class: 'mark', 'aria-hidden': 'true' }, on ? '✓' : ''),
    h('span', { class: 'text' }, o.label),
    voters && (voters[o.id] || []).length ? faces(voters[o.id], env) : null,
    n != null ? h('span', { class: 'count' }, o.max != null ? `${n} of ${o.max}` : String(n)) : (o.max != null ? h('span', { class: 'count' }, plural(o.max, 'place')) : null));
    list.append(btn);
  }
  return list;
}

// Counts for a choice field that is answered in the dialog.
function tallyBars(form, field, env) {
  const tally = form.tally[field.id];
  const total = Math.max(1, Object.values(tally).reduce((a, b) => a + b, 0));
  return h('div', { class: 'form-choices static', role: 'group', 'aria-label': field.label }, (field.options || []).map((o) => {
    const n = tally[o.id] || 0;
    const bar = h('span', { class: 'bar' });
    bar.style.width = Math.round((n / total) * 100) + '%';
    return h('div', { class: 'choice' }, bar, h('span', { class: 'text' }, o.label), h('span', { class: 'count' }, String(n)));
  }));
}

function showValue(field, value, env) {
  if (field.type === 'choice') return [].concat(value).map((id) => ((field.options || []).find((o) => o.id === id) || {}).label || id).join(', ');
  if (field.type === 'account') return [].concat(value).map((id) => env.nameOf(id)).join(', ');
  if (field.type === 'conversation') return [].concat(value).map((id) => (env.conversationName ? env.conversationName(id) : 'a channel')).join(', ');
  if (field.type === 'checkbox') return value ? 'yes' : 'no';
  return String(value);
}

// answerDialog asks for the fields of a form, a dialog's or a message's.
// It resolves with the answers by field id, or undefined.
export function answerDialog(form, env, yes, mine = {}) {
  const picked = new Map(); // field id → list of ids, for people and channels
  const specs = (form.fields || []).map((f) => {
    const base = { name: f.id, label: f.label, hint: f.help, required: !!f.required };
    switch (f.type) {
      case 'choice': {
        const options = (f.options || []).map((o) => [o.id, o.label]);
        if (f.multiple) return { ...base, type: 'checks', options, value: mine[f.id] || [] };
        // Up to five options are radio rows; more are a menu.
        return options.length <= 5
          ? { ...base, type: 'radio', options, value: (mine[f.id] || [])[0] }
          : { ...base, type: 'select', options: [['', 'Choose…'], ...options], value: (mine[f.id] || [])[0] || '' };
      }
      case 'longtext': return { ...base, type: 'textarea', placeholder: f.placeholder, maxLength: f.max_length || 4000, value: mine[f.id] };
      case 'number': return { ...base, type: 'number', min: f.min, max: f.max, step: f.step || 'any', value: mine[f.id] };
      case 'date': return { ...base, type: 'date', min: f.min, max: f.max, value: mine[f.id] };
      case 'time': return { ...base, type: 'time', value: mine[f.id] };
      case 'checkbox': return { ...base, type: 'checkbox', required: false, value: mine[f.id] };
      case 'account': case 'conversation': {
        picked.set(f.id, [...(mine[f.id] || [])]);
        const shown = h('span', { class: 'dim' });
        const draw = () => { shown.textContent = picked.get(f.id).length ? showValue(f, picked.get(f.id), env) : 'Nobody chosen'; if (f.type === 'conversation' && !picked.get(f.id).length) shown.textContent = 'None chosen'; };
        draw();
        const choose = button('Choose…', async () => {
          if (f.type === 'account') {
            const got = await env.pickAccounts(f.label, { multiple: !!f.multiple, yes: 'Choose' });
            if (got) picked.set(f.id, got.map((a) => a.id));
          } else {
            const got = await env.pickChannel(f.label);
            if (got) picked.set(f.id, f.multiple ? [...new Set([...picked.get(f.id), got.id])] : [got.id]);
          }
          draw();
        }, 'small');
        return { node: h('div', { class: 'field' }, h('span', {}, f.label + (f.required ? '' : ' (optional)')), h('div', { class: 'row' }, choose, shown), f.help && h('small', {}, f.help)) };
      }
      default: return { ...base, placeholder: f.placeholder, maxLength: f.max_length || 400, value: mine[f.id] };
    }
  });
  return formDialog(form.title || 'Answer', specs, { yes: yes || 'Send', submit: async (v) => {
    const out = {};
    for (const f of form.fields || []) {
      let val;
      if (f.type === 'account' || f.type === 'conversation') {
        val = picked.get(f.id);
        if (f.required && !val.length) throw Object.assign(new Error(`“${f.label}” needs an answer.`), {});
        if (!val.length) continue;
      } else if (f.type === 'choice') {
        val = f.multiple ? v[f.id] : (v[f.id] ? [v[f.id]] : []);
        if (f.required && !val.length) throw new Error(`“${f.label}” needs an answer.`);
        if (!val.length) continue;
      } else {
        val = v[f.id];
        if (val === undefined || val === '') continue;
      }
      out[f.id] = val;
    }
    return out;
  } }).then((v) => (v && v !== true ? v : v === true ? {} : undefined));
}

// The answers of a form, for those who may read them, with the CSV.
function resultsDialog(message, env) {
  const form = message.form;
  return openDialog('Answers', (close) => {
    const body = h('div', { class: 'stack' }, h('p', { class: 'loading' }, 'Loading'));
    all(`/messages/${message.id}/form/results`, 'answers').then((answers) => {
      const fields = form.fields || [];
      fill(body, answers.length ? h('div', { class: 'table-wrap' }, h('table', { class: 'data' },
        h('thead', {}, h('tr', {}, !form.anonymous && h('th', { scope: 'col' }, 'Who'), fields.map((f) => h('th', { scope: 'col' }, f.label)),
          (form.buttons || []).length ? h('th', { scope: 'col' }, 'Button') : null, !form.anonymous && h('th', { scope: 'col' }, 'When'))),
        h('tbody', {}, answers.map((a) => h('tr', {},
          !form.anonymous && h('td', {}, a.account_id ? env.nameOf(a.account_id) : ''),
          fields.map((f) => h('td', {}, a.answers[f.id] != null ? showValue(f, a.answers[f.id], env) : '')),
          (form.buttons || []).length ? h('td', {}, ((form.buttons || []).find((b) => b.id === a.button_id) || {}).label || '') : null,
          !form.anonymous && h('td', { class: 'mono' }, dateTime(a.updated_at || a.created_at))))))) : h('p', { class: 'dim' }, 'Nobody has answered yet.'));
    }).catch((err) => fill(body, h('p', { class: 'problem' }, err.message)));
    return { body, foot: [h('a', { class: 'btn', href: `/api/v1/messages/${message.id}/form/results.csv`, download: 'answers.csv' }, 'Download as CSV'), button('Close', () => close())] };
  }, { wide: true });
}

// --- building forms from the built-in commands ---
//
// A preset is a form the client builds from a short command. The server
// stores an ordinary form; `preset` only says how to draw it.

const ids = (n) => Array.from({ length: n }, (_, i) => String.fromCharCode(97 + (i % 26)) + (i >= 26 ? Math.floor(i / 26) : ''));
const parts = (text) => text.split('|').map((s) => s.trim()).filter(Boolean);

// presetForm turns `/poll Question? | A | B` and its kin into a form, or
// returns a string saying what is missing.
export function presetForm(name, text) {
  const p = parts(text);
  // Flags are words at the end: `anonymous`, `multiple`, and `2` after a
  // sign-up slot for its number of places.
  const flags = new Set();
  if (p.length) {
    const words = p[p.length - 1].split(/\s+/);
    while (words.length > 1 && ['anonymous', 'multiple', 'hidden'].includes(words[words.length - 1].toLowerCase())) flags.add(words.pop().toLowerCase());
    p[p.length - 1] = words.join(' ');
  }
  const results = flags.has('hidden') ? 'after_answer' : 'live';
  switch (name) {
    case 'poll': {
      if (p.length < 3) return 'A poll needs a question and at least two options: /poll Question? | Option | Option';
      const opts = p.slice(1);
      return { preset: 'poll', title: p[0], fields: [{ id: 'choice', type: 'choice', label: p[0], required: true, multiple: flags.has('multiple'), options: opts.map((label, i) => ({ id: ids(opts.length)[i], label })) }],
        action: { type: 'tally' }, anonymous: flags.has('anonymous'), results };
    }
    case 'rsvp': {
      if (!p.length) return 'An RSVP needs to say what it is for: /rsvp What | When';
      return { preset: 'rsvp', title: p.length > 1 ? `${p[0]} · ${p[1]}` : p[0], fields: [{ id: 'choice', type: 'choice', label: p[0], required: true,
        options: [{ id: 'going', label: 'Going' }, { id: 'maybe', label: 'Maybe' }, { id: 'no', label: 'Can’t' }] }], action: { type: 'tally' }, anonymous: false, results: 'live' };
    }
    case 'signup': {
      if (p.length < 2) return 'A sign-up sheet needs a name and at least one slot: /signup What | Slot | Slot';
      const slots = p.slice(1).map((s, i) => {
        // `Slot x3` gives a slot three places; one place otherwise.
        const m = /^(.*?)\s*[x×](\d+)$/i.exec(s);
        return { id: ids(p.length)[i], label: m ? m[1] : s, max: m ? Number(m[2]) : 1 };
      });
      return { preset: 'signup', title: p[0], fields: [{ id: 'choice', type: 'choice', label: p[0], required: true, multiple: flags.has('multiple'), options: slots }], action: { type: 'tally' }, anonymous: false, results: 'live' };
    }
    case 'approve': {
      if (!p.length) return 'An approval needs to say what is to be approved: /approve What';
      return { preset: 'approve', title: p[0], fields: [], buttons: [{ id: 'approve', label: 'Approve', style: 'primary' }, { id: 'decline', label: 'Decline', style: 'danger' }], action: { type: 'tally' }, anonymous: false, results: 'live' };
    }
    default: return 'That is not a form command.';
  }
}

// formBuilder is what `/form` opens: any fields, answers collected for the
// author and the channel's managers. It resolves with a form to post, or
// undefined.
export function formBuilder() {
  return openDialog('Build a form', (close) => {
    const title = h('input', { maxLength: 200, required: true, placeholder: 'What is this form for?' });
    const rows = h('div', { class: 'stack tight' });
    const TYPES = [['text', 'One line'], ['longtext', 'Several lines'], ['choice', 'Pick one'], ['multi', 'Pick several'], ['number', 'A number'], ['date', 'A date'], ['time', 'A time'], ['checkbox', 'A tick-box'], ['account', 'People'], ['conversation', 'A channel']];
    const addRow = () => {
      const label = h('input', { placeholder: 'Question', maxLength: 200, 'aria-label': 'Question' });
      const type = h('select', { 'aria-label': 'Kind of answer' }, TYPES.map(([v, t]) => h('option', { value: v }, t)));
      const options = h('input', { placeholder: 'Options, separated by |', 'aria-label': 'Options', hidden: true });
      const required = h('input', { type: 'checkbox', checked: true });
      type.addEventListener('change', () => { options.hidden = type.value !== 'choice' && type.value !== 'multi'; });
      const row = h('div', { class: 'builder-row' }, label, type, options, h('label', { class: 'check' }, required, h('span', {}, 'Required')),
        button('Remove', () => row.remove(), 'small quiet'));
      row.read = () => ({ label: label.value.trim(), type: type.value, options: parts(options.value), required: required.checked });
      rows.append(row);
    };
    addRow();
    const anonymous = h('input', { type: 'checkbox' });
    const closes = h('input', { type: 'datetime-local' });
    const problem = h('p', { class: 'problem', role: 'alert', hidden: true });
    const done = button('Post the form', () => {
      const fields = [];
      [...rows.children].forEach((row, i) => {
        const r = row.read();
        if (!r.label) return;
        const f = { id: 'f' + (i + 1), label: r.label, required: r.required, type: r.type === 'multi' ? 'choice' : r.type };
        if (f.type === 'choice') { f.multiple = r.type === 'multi'; f.options = r.options.map((label, k) => ({ id: 'o' + (k + 1), label })); }
        if (f.type === 'account') f.multiple = true;
        fields.push(f);
      });
      const bad = !title.value.trim() ? 'Give the form a title.' : !fields.length ? 'Add at least one question.'
        : fields.some((f) => f.type === 'choice' && f.options.length < 2) ? 'A question to pick from needs at least two options.' : null;
      if (bad) { problem.textContent = bad; problem.hidden = false; return; }
      close({ title: title.value.trim(), fields, action: { type: 'collect' }, anonymous: anonymous.checked, closes_at: closes.value ? new Date(closes.value).toISOString() : undefined });
    }, 'primary');
    return {
      body: [h('label', { class: 'field' }, h('span', {}, 'Title'), title), h('span', { class: 'label' }, 'Questions'), rows,
        h('p', { class: 'row' }, button('Add a question', addRow, 'small')),
        h('label', { class: 'check' }, anonymous, h('span', {}, 'Anonymous', h('small', {}, 'Nobody is shown who answered what. It cannot be changed after posting.'))),
        h('label', { class: 'field' }, h('span', {}, 'Closes at (optional)'), closes),
        h('p', { class: 'dim small' }, 'Answers are collected for you and the channel’s managers to read and download.'), problem],
      foot: [button('Cancel', () => close(undefined)), done],
    };
  }, { wide: true });
}
