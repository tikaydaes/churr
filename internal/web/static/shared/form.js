// A dialog with a form in it, described by a list of fields. The account
// page, the admin console and the web chat all ask for things the same
// way, so the asking is written once.

import { h, openDialog, button, field as labelled, check, localInput, fromLocalInput } from './dom.js';

// A field is { name, label, type, value, hint, required, options, … }.
// Types: text (the default), textarea, number, select, checkbox, radio,
// datetime, password-less secrets are never typed here.
//   options   for select and radio: [[value, label, hint?], …]
//   when      (values) => boolean: the field is shown only while true
//   node      a node to show as it is (with `when`, only while true)
function control(f) {
  const common = { name: f.name, required: !!f.required, disabled: !!f.disabled };
  switch (f.type) {
    case 'textarea':
      return h('textarea', { ...common, rows: f.rows || 4, maxLength: f.maxLength || 8000, placeholder: f.placeholder || '', value: f.value || '' });
    case 'number':
      return h('input', { ...common, type: 'number', min: f.min, max: f.max, step: f.step || 1, value: f.value != null ? String(f.value) : '', placeholder: f.placeholder || '' });
    case 'select':
      return h('select', common, f.options.map(([v, label]) => h('option', { value: v, selected: String(v) === String(f.value == null ? '' : f.value) }, label)));
    case 'checkbox':
      return h('input', { ...common, type: 'checkbox', required: false, checked: !!f.value, class: f.switch ? 'switch' : '' });
    case 'datetime':
      return h('input', { ...common, type: 'datetime-local', value: f.value ? localInput(f.value) : '' });
    case 'date':
      return h('input', { ...common, type: 'date', value: f.value || '', min: f.min, max: f.max });
    case 'time':
      return h('input', { ...common, type: 'time', value: f.value || '' });
    case 'file':
      return h('input', { ...common, type: 'file', accept: f.accept || '' });
    default:
      return h('input', { ...common, type: f.type === 'url' ? 'url' : 'text', value: f.value != null ? String(f.value) : '', maxLength: f.maxLength || 400,
        minLength: f.minLength, placeholder: f.placeholder || '', autocomplete: f.autocomplete || 'off', spellcheck: f.spellcheck !== false && !f.code,
        autocapitalize: f.code ? 'none' : null, inputMode: f.inputMode, pattern: f.pattern });
  }
}

// buildFields draws the fields and returns { nodes, read, controls }.
// read() gives the values by name.
export function buildFields(fields) {
  const controls = new Map();
  const wraps = new Map();
  const nodes = [];
  const shown = []; // nodes given whole, with a `when`
  for (const f of fields.filter(Boolean)) {
    if (f.node) { nodes.push(f.node); if (f.when) shown.push(f); continue; }
    let wrap;
    if (f.type === 'radio') {
      const inputs = f.options.map(([v, label, hint]) => {
        const input = h('input', { type: 'radio', name: f.name, value: v, checked: String(v) === String(f.value) });
        return { input, row: check(label, input, hint) };
      });
      controls.set(f.name, { f, inputs: inputs.map((x) => x.input) });
      wrap = h('fieldset', { class: 'stack tight plain' }, h('legend', { class: 'label' }, f.label), f.hint && h('p', { class: 'dim small' }, f.hint), inputs.map((x) => x.row));
    } else if (f.type === 'checks') {
      // Several tick-boxes under one name; the value is the list ticked.
      const chosen = new Set(f.value || []);
      const inputs = f.options.map(([v, label, hint]) => {
        const input = h('input', { type: 'checkbox', value: v, checked: chosen.has(v) });
        return { input, row: check(label, input, hint) };
      });
      controls.set(f.name, { f, inputs: inputs.map((x) => x.input) });
      wrap = h('fieldset', { class: 'stack tight plain' }, h('legend', { class: 'label' }, f.label), f.hint && h('p', { class: 'dim small' }, f.hint),
        inputs.length ? inputs.map((x) => x.row) : h('p', { class: 'dim small' }, f.empty || 'There is nothing to choose from.'));
    } else {
      const input = control(f);
      controls.set(f.name, { f, input });
      wrap = f.type === 'checkbox' ? check(f.label, input, f.hint) : labelled(f.label, input, f.hint);
    }
    wraps.set(f.name, wrap);
    nodes.push(wrap);
  }
  const read = () => {
    const out = {};
    for (const [name, c] of controls) {
      const f = c.f;
      if (f.type === 'radio') { const on = c.inputs.find((i) => i.checked); out[name] = on ? on.value : undefined; }
      else if (f.type === 'checks') out[name] = c.inputs.filter((i) => i.checked).map((i) => i.value);
      else if (f.type === 'checkbox') out[name] = c.input.checked;
      else if (f.type === 'number') out[name] = c.input.value === '' ? undefined : Number(c.input.value);
      else if (f.type === 'datetime') out[name] = fromLocalInput(c.input.value);
      else if (f.type === 'file') out[name] = c.input.files[0];
      else out[name] = f.type === 'textarea' ? c.input.value : c.input.value.trim();
    }
    return out;
  };
  // Fields with `when` come and go as the others change.
  const sync = () => {
    const v = read();
    for (const [name, c] of controls) if (c.f.when) wraps.get(name).hidden = !c.f.when(v);
    for (const f of shown) f.node.hidden = !f.when(v);
  };
  const container = h('div', { class: 'stack', onchange: sync, oninput: sync }, nodes);
  sync();
  return { node: container, read, controls };
}

// formDialog shows the fields in a dialog. opts.submit(values) does the
// work; while it fails the dialog stays open and says why. It resolves
// with what submit returned (true if nothing), or undefined if closed.
export function formDialog(title, fields, opts = {}) {
  return openDialog(title, (close) => {
    const built = buildFields(fields);
    const problem = h('p', { class: 'problem', role: 'alert', hidden: true });
    const ok = h('button', { type: 'submit', class: 'btn ' + (opts.danger ? 'danger' : 'primary') }, opts.yes || 'Save');
    const id = 'f' + Math.random().toString(36).slice(2);
    ok.setAttribute('form', id);
    const form = h('form', { id, class: 'stack', onsubmit: async (e) => {
      e.preventDefault();
      if (ok.disabled) return;
      ok.disabled = true;
      problem.hidden = true;
      try {
        const result = opts.submit ? await opts.submit(built.read()) : built.read();
        close(result === undefined ? true : result);
      } catch (err) {
        if (!(err && err.silent)) {
          problem.textContent = (err && err.message) || 'That did not work.';
          problem.hidden = false;
          const c = err && err.field && built.controls.get(err.field);
          if (c && c.input) c.input.focus();
        }
      } finally { ok.disabled = false; }
    } }, [].concat(opts.intro || []).map((m) => (typeof m === 'string' ? h('p', {}, m) : m)), built.node, problem);
    return { body: form, foot: [button(opts.no || 'Cancel', () => close(undefined)), ok] };
  }, { wide: opts.wide });
}

// secretDialog shows something the server gives once: a token, an
// address. It says so, and offers to copy it.
export function secretDialog(title, parts, note) {
  return openDialog(title, (close) => ({
    body: [
      h('p', { class: 'note warn' }, note || 'This is shown once. Copy it now; it cannot be shown again.'),
      parts.map(([label, value]) => h('div', { class: 'stack tight' }, h('span', { class: 'label' }, label),
        h('code', { class: 'secret', tabIndex: 0 }, value),
        h('p', { class: 'row' }, button('Copy', async () => {
          try { await navigator.clipboard.writeText(value); } catch { /* the text can still be selected by hand */ }
        }, 'small')))),
    ],
    foot: [button('Done', () => close(true), 'primary')],
  }));
}
