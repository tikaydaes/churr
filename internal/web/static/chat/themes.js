// Colour themes in the web chat (ruling R61, docs/STYLE.md "Themes"):
//
//   /chat/themes            the editor: start from any theme, change each
//                           base colour, see a real conversation drawn with
//                           it and every pairing's measured ratio, fix a
//                           failing colour, save, copy, delete, publish,
//                           export and import a .churrtheme file
//   /chat/themes/library    the server's library: browse, search, try, add
//
// The built-in themes cannot be edited, only copied. Saving goes to the
// server, which checks the contrast itself and refuses a theme that fails;
// its words are shown as they are.

import { get, post, patch, del } from '/shared/api.js';
import { h, fill, icon, iconButton, button, toast, confirmDialog, busy, copyText } from '/shared/dom.js';
import { KEYS, KEY_NAMES, TEXT, parse, measure, measured, kindFits, problem, fix, sourceOf, builtinTheme, toFile, fromFile, fileName, tokens } from '/shared/palette.js';
import { themeCard } from '/shared/themecard.js';
import { readLook, useTheme, tryTheme, stopTrying, refreshChosen } from '/shared/themes.js';
import { store, nameOf, needAccounts } from './store.js';
import { ui } from './ui.js';
import { makePreview } from './themepreview.js';

function frame(name, tools, body) {
  return h('section', { class: 'view' },
    h('header', { class: 'conv-head' }, iconButton('back', 'Back to the list', () => ui.go('/chat/'), 'only-compact'),
      h('div', { class: 'conv-title' }, h('h1', {}, icon('palette'), ' ' + name)), h('div', { class: 'conv-tools' }, tools)),
    h('div', { class: 'view-body' }, body));
}

const HEXRE = /^#[0-9a-fA-F]{6}$/;
const cleanName = (s) => String(s || '').trim().slice(0, 64);

// download hands the browser a file to save.
function download(name, text) {
  const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }));
  const a = h('a', { href: url, download: name, hidden: true });
  document.body.append(a);
  a.click();
  setTimeout(() => { URL.revokeObjectURL(url); a.remove(); }, 1000);
}

// --- the editor ---

export class ThemesView {
  constructor() {
    this.mine = [];
    this.draft = null;      // { id?, name, kind, colours, published? }
    this.saved = '';        // the draft as last saved or loaded, to tell a change
    this.problem = null;    // the server's refusal, shown by Save
    this.preview = makePreview();
    this.side = h('div', { class: 'te-side stack' });
    this.edit = h('div', { class: 'te-edit stack' });
    this.importInput = h('input', { type: 'file', accept: '.churrtheme,application/json', hidden: true, onchange: () => this.importFile() });
    this.el = frame('Themes', [h('a', { class: 'btn small', href: '/chat/themes/library', onclick: (e) => { e.preventDefault(); ui.go('/chat/themes/library'); } }, 'Theme library')],
      h('div', { class: 'theme-editor' }, this.side, this.edit, this.importInput));
    this.off = [store.on('themes', () => this.load(true))];
    this.load();
  }

  destroy() { for (const off of this.off) off(); stopTrying(); }

  async load(quiet) {
    if (!quiet) fill(this.side, h('p', { class: 'loading' }, 'Loading'));
    try { this.mine = (await get('/me/themes')).themes || []; } catch (err) { this.loadProblem = err.message; this.mine = []; }
    if (!this.draft) {
      // Start from what this browser uses for the kind on screen.
      const v = readLook();
      const root = document.documentElement.dataset;
      const kind = root.theme || (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');
      const chosen = v[kind];
      const own = chosen && typeof chosen === 'object' && this.mine.find((t) => t.id === chosen.id);
      this.open(own || this.copyOf(builtinTheme(kind, v.palette === 'hd')), true);
    } else this.drawSide();
  }

  copyOf(t) {
    return { name: t.builtin ? 'My ' + t.name.toLowerCase() : 'Copy of ' + t.name, kind: t.kind, colours: { ...t.colours } };
  }

  dirty() { return JSON.stringify(this.pick(this.draft)) !== this.saved; }
  pick(t) { return t ? { name: t.name, kind: t.kind, colours: t.colours } : null; }

  async open(t, force) {
    if (!force && this.draft && this.dirty() && !(await confirmDialog('Leave this theme?', 'Its changes are not saved.', { yes: 'Leave it', danger: true }))) return;
    this.draft = { id: t.id, name: t.name, kind: t.kind, colours: { ...t.colours }, published: !!t.published, owner_id: t.owner_id };
    this.saved = JSON.stringify(this.pick(this.draft));
    this.problem = null;
    this.drawSide();
    this.drawEdit();
  }

  drawSide() {
    const v = readLook();
    const start = (t, note, copy) => h('li', {}, h('button', { type: 'button', class: 'te-pick' + (!copy && this.draft && this.draft.id === t.id ? ' current' : ''), onclick: () => this.open(copy ? this.copyOf(t) : t) },
      h('span', { class: 'swatch-mini', style: { background: t.colours.bg, boxShadow: `inset 0 -6px 0 ${t.colours.cyan}` } }),
      h('span', { class: 'grow' }, h('strong', {}, t.name), h('small', { class: 'dim' }, note)),
      t.published ? h('span', { class: 'tag cyan' }, 'published') : null));
    const builtins = [builtinTheme('dark'), builtinTheme('light'), builtinTheme('dark', true), builtinTheme('light', true)];
    const defaults = ['dark', 'light'].map((k) => v.server && v.server[k]).filter(Boolean);
    fill(this.side,
      h('div', { class: 'stack tight' }, h('h2', { class: 'label' }, 'Your themes'),
        this.loadProblem ? h('p', { class: 'problem small' }, this.loadProblem) : null,
        this.mine.length ? h('ul', { class: 'te-list' }, this.mine.map((t) => start(t, t.kind + (t.published ? '' : ', not published'), false))) : h('p', { class: 'dim small' }, 'None saved yet. Start from one below, change it and save it.')),
      h('div', { class: 'stack tight' }, h('h2', { class: 'label' }, 'Start from'),
        h('p', { class: 'dim small' }, 'The built-in themes and the server’s defaults cannot be changed; you edit a copy.'),
        h('ul', { class: 'te-list' }, builtins.map((t) => start(t, 'built-in ' + t.kind, true)), defaults.map((t) => start(t, 'this server’s default ' + t.kind, true)))),
      h('p', { class: 'row' },
        button('Import a .churrtheme file', () => this.importInput.click(), 'small'),
        h('a', { class: 'btn small', href: '/chat/themes/library', onclick: (e) => { e.preventDefault(); ui.go('/chat/themes/library'); } }, 'Find one in the library')));
  }

  set(key, value) {
    this.draft.colours[key] = value.toLowerCase();
    this.problem = null;
    this.update();
  }

  // drawEdit builds the editor once per theme opened; update() then only
  // changes values, so focus and typing are never lost.
  drawEdit() {
    const d = this.draft;
    this.rows = {};
    const name = h('input', { value: d.name, maxLength: 64, required: true, 'aria-label': 'Name of the theme', oninput: () => { d.name = name.value; this.update(); } });
    const kind = h('select', { 'aria-label': 'Kind', onchange: () => { d.kind = kind.value; this.problem = null; this.update(); } },
      h('option', { value: 'dark', selected: d.kind === 'dark' }, 'Dark: light text on a dark ground'),
      h('option', { value: 'light', selected: d.kind === 'light' }, 'Light: dark text on a light ground'));
    const colourRows = KEYS.map((k) => {
      const picker = h('input', { type: 'color', value: d.colours[k], 'aria-label': KEY_NAMES[k] + ' colour', oninput: () => { hexIn.value = picker.value; this.set(k, picker.value); } });
      const hexIn = h('input', { class: 'mono hex', value: d.colours[k], maxLength: 7, spellcheck: false, autocomplete: 'off', 'aria-label': KEY_NAMES[k] + ' as hex',
        oninput: () => { let x = hexIn.value.trim(); if (/^[0-9a-fA-F]{6}$/.test(x)) x = '#' + x; if (HEXRE.test(x)) { picker.value = x.toLowerCase(); this.set(k, x); hexIn.removeAttribute('aria-invalid'); } else hexIn.setAttribute('aria-invalid', 'true'); },
        onblur: () => { hexIn.value = d.colours[k]; hexIn.removeAttribute('aria-invalid'); } });
      const state = h('span', { class: 'te-state' });
      const fixBtn = h('button', { type: 'button', class: 'btn small', hidden: true, onclick: () => this.fix(k) }, 'Fix');
      this.rows[k] = { picker, hexIn, state, fixBtn };
      return h('div', { class: 'te-colour' }, h('label', { class: 'te-key' }, h('strong', {}, KEY_NAMES[k]), h('code', {}, k)), picker, hexIn, state, fixBtn);
    });
    this.kindNote = h('p', { class: 'problem small', role: 'alert', hidden: true });
    this.pairs = h('div', { class: 'te-pairs' });
    this.saveProblem = h('p', { class: 'problem', role: 'alert', hidden: true });
    this.status = h('p', { class: 'dim small te-status', role: 'status' });
    this.actions = h('div', { class: 'row te-actions' });
    fill(this.edit,
      h('div', { class: 'cols-2' }, h('label', { class: 'field' }, h('span', {}, 'Name'), name), h('label', { class: 'field' }, h('span', {}, 'Kind'), kind)),
      this.kindNote,
      h('div', { class: 'te-body' },
        h('div', { class: 'stack tight te-colours' }, h('h2', { class: 'label' }, 'Base colours'),
          h('p', { class: 'dim small' }, 'Everything else is worked out from these nine, by the rules every Churr app and the server follow.'), colourRows),
        h('div', { class: 'stack tight te-look' }, h('h2', { class: 'label' }, 'Preview'), this.preview.el)),
      this.saveProblem, this.actions, this.status,
      h('div', { class: 'stack tight' }, h('h2', { class: 'label' }, 'Every pairing, measured'),
        h('p', { class: 'dim small' }, 'Text needs 4.5:1 against what it sits on; control edges, icons and the focus ring need 3:1. A theme that fails any of them cannot be saved, shared or used.'),
        this.pairs));
    this.update();
  }

  update() {
    const d = this.draft;
    const got = parse(d.colours);
    const all = got.colours ? measure(d.kind, got.colours) : [];
    const failing = new Set(all.filter((p) => !p.ok).map((p) => sourceOf(p.fore, p.back)));
    const fits = got.colours && kindFits(d.kind, got.colours);
    this.preview.paint(d);
    for (const k of KEYS) {
      const r = this.rows[k];
      if (document.activeElement !== r.hexIn) r.hexIn.value = d.colours[k];
      r.picker.value = d.colours[k];
      const bad = failing.has(k);
      fill(r.state, bad ? h('span', { class: 'tag pink' }, 'fails') : h('span', { class: 'tag line' }, 'passes'));
      r.fixBtn.hidden = !bad;
      r.fixBtn.setAttribute('aria-label', 'Fix ' + KEY_NAMES[k]);
    }
    this.kindNote.hidden = !!fits || !got.colours;
    this.kindNote.textContent = d.kind === 'dark' ? 'A dark theme’s ground (bg) must be darker than its ink. Change the kind, or the ground and text colours.' : 'A light theme’s ground (bg) must be lighter than its ink. Change the kind, or the ground and text colours.';
    const t = tokens(d) || {};
    const chip = (p) => {
      const fg = p.fore === 'faint' || p.fore === 'edge' || p.fore === 'yel-ink' || p.fore.startsWith('on-') ? t[p.fore] : d.colours[p.fore];
      const bg = d.colours[p.back];
      return h('span', { class: 'te-chip', style: { color: fg || d.colours.ink, background: bg }, 'aria-hidden': 'true' }, p.need === TEXT ? 'Aa' : '■');
    };
    fill(this.pairs, all.map((p) => h('div', { class: 'te-pair' + (p.ok ? '' : ' bad') },
      chip(p),
      h('span', { class: 'grow' }, h('strong', {}, `${p.fore} on ${p.back}`)),
      h('span', { class: 'mono te-ratio' }, measured(p.ratio) + ':1'),
      h('span', { class: 'dim small mono te-need' }, 'needs ' + p.need + ':1'),
      p.ok ? h('span', { class: 'tag cyan' }, 'pass') : h('span', { class: 'tag pink' }, 'fail'),
      p.ok ? h('span', { class: 'te-fix-gap' }) : h('button', { type: 'button', class: 'btn small', onclick: () => this.fix(sourceOf(p.fore, p.back)) }, 'Fix ' + sourceOf(p.fore, p.back)))));
    const fails = all.filter((p) => !p.ok).length;
    this.status.textContent = !got.colours ? got.message : !fits ? 'The kind does not fit the colours.' : fails ? `${fails} of ${all.length} pairings fail. Fix them before saving.` : `All ${all.length} pairings pass.`;
    this.saveProblem.hidden = !this.problem;
    this.saveProblem.textContent = this.problem ? 'The server refused it: ' + this.problem : '';
    this.drawActions();
  }

  drawActions() {
    const d = this.draft;
    const mineSaved = d.id && this.mine.some((t) => t.id === d.id);
    const ok = !problem(d);
    const save = button(mineSaved ? 'Save' : 'Save as a new theme', () => busy(save, () => this.save(false)), 'primary');
    const copy = mineSaved ? button('Save a copy', () => busy(copy, () => this.save(true))) : null;
    const tryIt = button('Try on this page', () => { try { tryTheme(d); } catch (err) { toast(err.message, true); } });
    const use = button('Use it in this browser', () => { try { useTheme(d); toast(`Using “${d.name}” for ${d.kind}.`); } catch (err) { toast(err.message, true); } });
    tryIt.disabled = !ok; use.disabled = !ok;
    const pub = mineSaved ? (d.published
      ? button('Unpublish', () => busy(pub, () => this.publish(false)))
      : button('Publish to the library', () => busy(pub, () => this.publish(true)), 'lav')) : null;
    if (pub && store.me.role === 'guest') pub.disabled = true;
    fill(this.actions, save, copy, tryIt, use, pub,
      button('Export a .churrtheme file', () => download(fileName(d), toFile(d))),
      mineSaved ? button('Copy link', () => copyText(location.origin + '/themes/' + d.id, 'Link copied')) : null,
      mineSaved ? button('Delete', () => this.remove(), 'danger') : null,
      !mineSaved ? h('span', { class: 'dim small' }, 'Not saved yet.') : this.dirty() ? h('span', { class: 'dim small' }, 'Changed, not saved.') : d.published ? h('span', { class: 'tag cyan' }, 'published') : null);
  }

  fix(key) {
    const next = fix(this.draft.kind, this.draft.colours, key);
    if (!next) { toast(`No change to ${KEY_NAMES[key].toLowerCase()} alone makes this theme pass. Try another colour.`, true); return; }
    const was = this.draft.colours[key];
    this.set(key, next);
    toast(`${KEY_NAMES[key]} changed from ${was} to ${next}.`);
  }

  async save(asCopy) {
    const d = this.draft;
    const body = { name: cleanName(d.name) || 'Untitled theme', kind: d.kind, colours: { ...d.colours } };
    if (asCopy) body.name = cleanName('Copy of ' + body.name);
    try {
      const mineSaved = d.id && this.mine.some((t) => t.id === d.id);
      const out = mineSaved && !asCopy ? await patch('/me/themes/' + d.id, body) : await post('/me/themes', body);
      const t = out.theme;
      this.mine = [...this.mine.filter((x) => x.id !== t.id), t].sort((a, b) => String(a.created_at).localeCompare(String(b.created_at)));
      refreshChosen(t);
      toast(asCopy ? 'Saved as a copy.' : 'Saved.');
      await this.open(t, true);
    } catch (err) {
      if (err.silent) return;
      this.problem = err.message;
      this.update();
    }
  }

  async publish(on) {
    const d = this.draft;
    try {
      const out = on ? await post(`/me/themes/${d.id}/publish`) : await del(`/me/themes/${d.id}/publish`);
      this.mine = this.mine.map((x) => (x.id === out.theme.id ? out.theme : x));
      d.published = out.theme.published;
      toast(on ? 'Published. Everyone on this server can find it in the library.' : 'Unpublished. Copies others added stay theirs.');
      this.drawSide();
      this.update();
    } catch (err) { if (!err.silent) toast(err.message, true); }
  }

  async remove() {
    const d = this.draft;
    if (!(await confirmDialog(`Delete “${d.name}”?`, [d.published ? 'It leaves the library.' : 'It is removed from your themes.', 'Copies others added, theme cards in messages and a server default made from it stay.'], { yes: 'Delete', danger: true }))) return;
    try {
      await del('/me/themes/' + d.id);
      this.mine = this.mine.filter((x) => x.id !== d.id);
      refreshChosen(null, d.id);
      toast('Deleted.');
      this.open({ ...d, id: undefined, published: false }, true);
    } catch (err) { if (!err.silent) toast(err.message, true); }
  }

  async importFile() {
    const f = this.importInput.files[0];
    this.importInput.value = '';
    if (!f) return;
    if (f.size > 64 * 1024) { toast('That file is too large to be a theme.', true); return; }
    const got = fromFile(await f.text());
    if (got.error) { toast(`${f.name}: ${got.error}`, true); return; }
    await this.open(got.theme);
    toast(`Imported “${got.theme.name}”. Save it to keep it.`);
  }
}

// --- the library ---

export class LibraryView {
  constructor() {
    this.q = h('input', { type: 'search', placeholder: 'Search by name', maxLength: 64, 'aria-label': 'Search the theme library', oninput: () => { clearTimeout(this.t); this.t = setTimeout(() => this.load(), 250); } });
    this.list = h('div', { class: 'library' });
    this.more = h('p', { class: 'row', hidden: true }, button('Load more', () => this.load(true)));
    this.added = new Set();
    this.el = frame('Theme library', [h('a', { class: 'btn small', href: '/chat/themes', onclick: (e) => { e.preventDefault(); ui.go('/chat/themes'); } }, 'Your themes')],
      [h('p', { class: 'dim' }, 'Themes people on this server have published. Try one on this page without keeping it, or add a copy to your own themes to use and change.'),
        h('div', { class: 'library-search' }, this.q), this.list, this.more]);
    this.off = [store.on('themes', () => this.load()), store.on('accounts', () => this.draw())];
    this.items = [];
    this.load();
  }

  destroy() { for (const off of this.off) off(); clearTimeout(this.t); stopTrying(); }

  async load(more) {
    if (!more) { this.cursor = undefined; if (!this.items.length) fill(this.list, h('p', { class: 'loading' }, 'Loading')); }
    try {
      const out = await get('/themes', { q: this.q.value.trim() || undefined, limit: 30, cursor: more ? this.cursor : undefined });
      this.items = more ? [...this.items, ...out.themes] : out.themes;
      this.cursor = out.next_cursor;
      needAccounts(this.items.map((t) => t.owner_id));
      this.draw();
    } catch (err) { fill(this.list, h('p', { class: 'problem' }, err.message)); }
  }

  draw() {
    this.more.hidden = !this.cursor;
    if (!this.items.length) {
      fill(this.list, h('div', { class: 'empty-view small-empty' }, h('p', { class: 'dim' }, this.q.value.trim() ? 'No published theme has that in its name.' : 'Nobody has published a theme yet. Make one under Your themes and publish it.')));
      return;
    }
    fill(this.list, this.items.map((t) => themeCard(t, {
      author: t.owner_id === store.me.id ? 'you' : nameOf(t.owner_id),
      link: '/themes/' + t.id,
      added: this.added.has(t.id),
      onTry: () => {
        try {
          tryTheme(t, t.owner_id === store.me.id ? [] : [h('button', { type: 'button', class: 'btn small', onclick: () => this.add(t) }, 'Add to my themes')]);
        } catch (err) { toast(err.message, true); }
      },
      onAdd: t.owner_id === store.me.id ? null : () => this.add(t),
      note: t.owner_id === store.me.id ? 'Yours. Edit or unpublish it under Your themes.' : null,
    })));
  }

  async add(t) {
    try {
      await post(`/themes/${t.id}/add`);
      this.added.add(t.id);
      toast(`“${t.name}” is in your themes.`);
      this.draw();
    } catch (err) { if (!err.silent) toast(err.message, true); }
  }
}
