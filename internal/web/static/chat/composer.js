// The composer: a growing box that takes Churr Markdown as typed, with
// mentions, the channel picker, emoji, commands, attachments, replying
// with a quote, editing and sending later (docs/CLIENTS.md section 9.5).
//
// The box is an editable element, not a textarea, for one reason: a link
// to a channel is a chip, one unit the cursor steps over, that shows the
// channel's current name and is sent as `<#conversation-id>` (section
// 9.26). Everything else in it is plain characters.

import { post, patch, get, del, upload } from '/shared/api.js';
import { h, icon, iconButton, toast, fill, bytes, openMenu, place, fold, dateTime, avatar, confirmDialog } from '/shared/dom.js';
import { broadHint, beforeSend, confirmLine, BROAD_WAS_PLAIN } from '/shared/md/broad.js';
import { renderMarkdown } from '/shared/md/render.js';
import { validID } from '/shared/md/parse.js';
import { detailLine, channelIcon } from '/shared/pick.js';
import { formDialog } from '/shared/form.js';
import { presetForm, formBuilder, answerDialog } from '/shared/message/forms.js';
import { firstLine } from '/shared/message/message.js';
import { store, account, nameOf, conversation, room, putMessage, live, title, isSealed, typingIn, isLarge, isAdmin } from './store.js';
import { look, visibleChannels, pickEmoji, mdContext } from './env.js';
import { parse as parseCommand, COMMANDS, hiddenBuiltins, literal } from '/shared/commands.js';
import { run as runBuiltin } from './commands.js';
import { viewerZone } from '/shared/instant/zone.js';
import { endingAt as phraseEndingAt } from '/shared/instant/phrases.js';
import { hintFits, track } from '/shared/instant/compose.js';
import { candidate as objectCandidate, choose as chooseObject, track as trackObjects, hintsOf as objectHints, undo as undoObject, keyAction, PLAIN } from '/shared/objects/compose.js';
import { t as objectWords } from '/shared/objects/words.js';
import { objectRowLabel } from '/shared/objects/rows.js';
import { loadPatterns, patternsFor, serverPatterns } from './patterns.js';
import { compact as compactTime } from '/shared/instant/format.js';
import { t as words } from '/shared/instant/words.js';

const MAX_FILES = 10;
// Drafts are kept for the life of the page, per conversation and thread.
const drafts = new Map();
const memberCache = new Map();
const commandCache = new Map();

// The members of a conversation, for listing them first under `@`.
async function membersOf(id) {
  if (!memberCache.has(id)) {
    memberCache.set(id, get(`/conversations/${id}/members`, { limit: 200 }).then((r) => new Set(r.members.map((m) => m.account_id))).catch(() => new Set()));
    store.on('members', (cid) => { if (cid === id) memberCache.delete(id); });
  }
  return memberCache.get(id);
}

// commandsChanged forgets the command lists, after this person changes a
// saved reply here.
export function commandsChanged() { commandCache.clear(); }

// commandsOf is the server's command list for a conversation.
export function commandsOf(id) {
  if (!commandCache.has(id)) {
    commandCache.set(id, get('/commands', { conversation_id: id }).then((r) => r.commands).catch(() => []));
    // Saved replies have no event; the list is fetched again after a while.
    setTimeout(() => commandCache.delete(id), 60000);
  }
  return commandCache.get(id);
}

const hostOf = (url) => { try { return new URL(url).host; } catch { return url; } };

const coarse = () => window.matchMedia('(pointer: coarse)').matches;

export class Composer {
  // opts: conversationId, parentId, label, onEditLast(), locked: text|null
  constructor(opts) {
    this.o = opts;
    this.key = opts.conversationId + ':' + (opts.parentId || '');
    this.files = [];      // { file, caption, progress, error, id, url }
    this.quote = null;    // { message_id, start, end } and the message quoted
    this.quoteOf = null;
    this.editing = null;  // the message being edited
    this.popup = null;    // the open suggestion list
    this.dismissed = null;
    // Phrases the author made into times ("tomorrow at 5pm"), sent as
    // hints beside the words (ruling R69): { start, end, typed, time,
    // precision, zone }.
    this.hints = [];
    // What the author chose for the objects typed (ruling R73): { start,
    // end, typed, kind, first }, sent as object hints; and the object just
    // accepted, which Backspace makes plain text again.
    this.decided = [];
    this.accepted = null;
    this.busy = false;
    this.lastTyping = 0;
    this.build();
    this.restore();
  }

  build() {
    const o = this.o;
    this.editor = h('div', {
      class: 'editor', contentEditable: 'true', role: 'textbox', 'aria-multiline': 'true', 'aria-label': o.label || 'Message', 'aria-autocomplete': 'list', 'aria-haspopup': 'listbox', spellcheck: true,
      dataset: { placeholder: o.label || 'Message' },
    });
    // Where the browser has it, plain-text editing keeps pasted and
    // dragged-in formatting out by itself.
    try { this.editor.contentEditable = 'plaintext-only'; } catch { /* an older browser: paste is cleaned by hand below */ }
    if (this.editor.contentEditable !== 'plaintext-only') this.editor.contentEditable = 'true';
    this.tail();

    this.banner = h('div', { class: 'composer-banner', hidden: true });
    this.tray = h('div', { class: 'composer-files', hidden: true });
    this.notice = h('div', { class: 'composer-notice', hidden: true, role: 'status' });
    this.typingLine = h('div', { class: 'composer-typing', 'aria-live': 'polite' });
    this.preview = h('div', { class: 'composer-preview', hidden: true });
    // The times the text holds, each with what it will mean (ruling R69).
    this.instantLine = h('div', { class: 'composer-instants', hidden: true, role: 'group', 'aria-label': words('instantsHere') });
    this.fileInput = h('input', { type: 'file', multiple: true, hidden: true, 'aria-hidden': 'true', tabIndex: -1, onchange: () => { this.addFiles(this.fileInput.files); this.fileInput.value = ''; } });

    const fmt = (name, label, fn) => h('button', { type: 'button', class: 'fmt', title: label, 'aria-label': label, onclick: () => { fn(); this.editor.focus(); } }, name);
    this.previewBtn = h('button', { type: 'button', class: 'fmt wide', 'aria-pressed': 'false', onclick: () => this.togglePreview() }, 'Preview');
    this.bar = h('div', { class: 'composer-bar', role: 'toolbar', 'aria-label': 'Formatting' },
      fmt(h('b', {}, 'B'), 'Bold', () => this.wrap('**')),
      fmt(h('i', {}, 'I'), 'Italic', () => this.wrap('*')),
      fmt(h('s', {}, 'S'), 'Strikethrough', () => this.wrap('~~')),
      fmt(icon('code', null, 'small'), 'Inline code', () => this.wrap('`')),
      fmt(icon('link', null, 'small'), 'Link', () => this.link()),
      fmt('•', 'Bullet list', () => this.linePrefix('- ')),
      fmt('1.', 'Numbered list', () => this.linePrefix('1. ')),
      fmt(icon('list-check', null, 'small'), 'Checklist', () => this.linePrefix('- [ ] ')),
      fmt(icon('quote', null, 'small'), 'Quote', () => this.linePrefix('> ')),
      fmt('{ }', 'Code block', () => this.wrap('```\n', '\n```')),
      this.previewBtn);

    this.sendBtn = h('button', { type: 'button', class: 'btn primary send', onclick: () => this.send() }, icon('send'), h('span', {}, 'Send'));
    this.laterBtn = iconButton('clock', 'Send later', (e) => this.laterMenu(e.currentTarget));
    this.row = h('div', { class: 'composer-row' },
      iconButton('attach', 'Attach files', () => this.fileInput.click()),
      this.editor,
      iconButton('emoji', 'Emoji', (e) => pickEmoji(e.currentTarget, (v) => { this.editor.focus(); this.insert(v + ' '); })),
      this.laterBtn, this.sendBtn);
    this.blocked = h('div', { class: 'composer-blocked', hidden: true });
    // Said while the text holds @channel, @here or @everyone that the
    // person may not use here (ruling R64).
    this.broadLine = h('div', { class: 'composer-notice broad', hidden: true, role: 'status' });
    // Says what happened to an object as the author chooses (ruling R73).
    this.announcer = h('div', { class: 'sr-only', 'aria-live': 'polite' });
    this.el = h('div', { class: 'composer' }, this.announcer, this.typingLine, this.notice, this.broadLine, this.banner, this.tray, this.bar, this.preview, this.instantLine, this.row, this.blocked, this.fileInput);

    this.editor.addEventListener('keydown', (e) => this.keydown(e));
    this.editor.addEventListener('input', () => { this.tail(); this.drawInstants(); this.suggest(); this.sayTyping(); this.saveDraft(); this.drawBroad(); if (!this.preview.hidden) this.drawPreview(); });
    this.editor.addEventListener('click', () => this.suggest());
    this.editor.addEventListener('blur', () => setTimeout(() => { if (this.popup && !this.popup.el.contains(document.activeElement)) this.closePopup(); }, 150));
    this.editor.addEventListener('paste', (e) => this.paste(e));
    this.editor.addEventListener('drop', (e) => { if (e.dataTransfer && e.dataTransfer.files.length) { e.preventDefault(); this.addFiles(e.dataTransfer.files); } });

    this.off = [store.on('typing', (cid) => { if (cid === o.conversationId) this.drawTyping(); }),
      // Whether broad mentions are allowed here may have changed.
      store.on('conversations', () => this.drawBroad())];
    this.setLocked(o.locked);
    // The reference patterns that apply here, for offering references as
    // they are typed. A sealed chat uses the server's alone.
    loadPatterns(isSealed(conversation(o.conversationId) || {}) ? '' : o.conversationId);
  }

  destroy() {
    this.saveDraft();
    this.closePopup();
    for (const off of this.off) off();
    for (const f of this.files) if (f.url) URL.revokeObjectURL(f.url);
  }

  focus() { if (!this.editor.hidden) { this.editor.focus(); this.caretToEnd(); } }

  // setLocked replaces the box with a notice when the person cannot post:
  // a reader, timed out, an archived channel, a thread that moved.
  setLocked(node) {
    const locked = !!node;
    for (const el of [this.bar, this.row, this.tray]) el.classList.toggle('off', locked);
    this.blocked.hidden = !locked;
    if (locked) fill(this.blocked, node);
  }

  // --- the text ---

  // The box always ends with one line break of its own, so that a new
  // line at the very end has somewhere to show its cursor.
  tail() {
    const last = this.editor.lastChild;
    if (!last || last.nodeName !== 'BR' || !last.classList.contains('tail')) {
      for (const br of this.editor.querySelectorAll('br.tail')) br.remove();
      this.editor.append(h('br', { class: 'tail' }));
    }
    this.editor.classList.toggle('empty', this.text() === '');
  }

  // text reads the box as the text to send: characters as typed, a line
  // break for each break, and `<#conversation-id>` for each chip.
  text(root = this.editor) {
    let out = '';
    const walk = (node) => {
      for (const n of node.childNodes) {
        if (n.nodeType === Node.TEXT_NODE) out += n.data;
        else if (n.nodeName === 'BR') { if (!n.classList.contains('tail') && !(n.parentNode !== root && n.parentNode.childNodes.length === 1)) out += '\n'; }
        else if (n.dataset && n.dataset.conversation) out += `<#${n.dataset.conversation}>`;
        else if (n.nodeName === 'DIV' || n.nodeName === 'P') { if (out) out += '\n'; walk(n); }
        else walk(n);
      }
    };
    walk(root);
    return out.replace(/ /g, ' ');
  }

  chip(conv) {
    return h('span', { class: 'chip channel', contentEditable: 'false', dataset: { conversation: conv.id }, title: detailLine(conv, look) }, channelIcon(conv, 'small'), h('bdi', {}, title(conv)));
  }

  // nodesFor turns text into what the box holds: text, and a chip for each
  // `<#…>` that names a channel the person can see. Anything else stays
  // exactly as it is.
  nodesFor(text) {
    const out = [];
    let last = 0;
    for (const m of String(text).matchAll(/<#([0-9a-f-]{36})>/g)) {
      const conv = validID(m[1]) && conversation(m[1]);
      if (!conv) continue;
      if (m.index > last) out.push(document.createTextNode(text.slice(last, m.index)));
      out.push(this.chip(conv));
      last = m.index + m[0].length;
    }
    if (last < text.length) out.push(document.createTextNode(text.slice(last)));
    return out;
  }

  setText(text) {
    this.editor.replaceChildren(...this.nodesFor(text || ''));
    this.tail();
    this.drawInstants();
  }

  // caretIndex is where the cursor is in the text as sent.
  caretIndex() {
    const sel = window.getSelection();
    if (!sel.rangeCount || !this.editor.contains(sel.anchorNode)) return this.text().length;
    const r = document.createRange();
    r.setStart(this.editor, 0);
    r.setEnd(sel.anchorNode, sel.anchorOffset);
    const box = document.createElement('div');
    box.append(r.cloneContents());
    return this.text(box).length;
  }

  // --- times (ruling R69) ---

  // drawInstants keeps the accepted phrases and the choices made about
  // objects on their words, and shows each accepted phrase with what it
  // will mean and a way to send it as plain words instead. A typed date
  // and time is offered in the list as it is typed, like every exact
  // token (objectHere).
  drawInstants() {
    if (!this.instantLine) return;
    const text = this.text();
    this.hints = track(this.hints, text);
    this.decided = trackObjects(this.decided, text);
    const zone = viewerZone();
    const locale = navigator.language || 'en';
    const chips = [];
    const chip = (key, typed, when, actionText, actionLabel, act) => h('span', { class: 'instant-chip' }, icon('clock', null, 'small'),
      h('span', { class: 'what' }, words(key, { typed, when, yourTime: words('yourTimeShort') })),
      h('button', { type: 'button', class: 'link-btn small', 'aria-label': actionLabel, onclick: act }, actionText));
    for (const hint of this.hints) {
      chips.push(chip('composerPhrase', hint.typed, compactTime(Date.parse(hint.time), { zone, locale }), words('dropTime'), words('dropTimeLabel', { typed: hint.typed }), () => {
        this.hints = this.hints.filter((x) => x !== hint);
        this.drawInstants();
        this.editor.focus();
      }));
    }
    this.instantLine.replaceChildren(...chips);
    this.instantLine.hidden = !chips.length;
  }

  // phraseHere is a phrase naming a time that ends at the cursor and may be
  // offered, or null.
  phraseHere(b) {
    if (!b.text) return null;
    const zone = viewerZone();
    const p = phraseEndingAt(b.text, b.text.length, { zone });
    if (!p) return null;
    const text = this.text();
    const end = this.caretIndex();
    const start = end - (p.end - p.start);
    if (start < 0 || !hintFits(text, start, end, this.hints)) return null;
    return { ...p, start, end, typed: text.slice(start, end) };
  }

  caretToEnd() {
    const r = document.createRange();
    const tail = this.editor.querySelector('br.tail');
    if (tail) r.setStartBefore(tail); else r.selectNodeContents(this.editor);
    r.collapse(true);
    const sel = window.getSelection();
    sel.removeAllRanges(); sel.addRange(r);
  }

  range() {
    const sel = window.getSelection();
    if (!sel.rangeCount || !this.editor.contains(sel.anchorNode)) this.caretToEnd();
    return window.getSelection().getRangeAt(0);
  }

  // insert puts nodes or text where the cursor is, replacing what is
  // selected, and leaves the cursor after it.
  insert(what, range = this.range()) {
    const nodes = typeof what === 'string' ? this.nodesFor(what) : [].concat(what);
    range.deleteContents();
    const frag = document.createDocumentFragment();
    frag.append(...nodes);
    const last = nodes[nodes.length - 1];
    range.insertNode(frag);
    if (last) { range.setStartAfter(last); range.collapse(true); }
    const sel = window.getSelection();
    sel.removeAllRanges(); sel.addRange(range);
    this.tail(); this.saveDraft();
    this.editor.classList.toggle('empty', this.text() === '');
    if (!this.preview.hidden) this.drawPreview();
  }

  wrap(before, after = before) {
    const r = this.range();
    const picked = r.toString();
    this.insert(before + (picked || '') + after, r);
    if (!picked) {
      // The cursor goes between the marks.
      const sel = window.getSelection();
      for (let i = 0; i < after.length; i++) sel.modify('move', 'backward', 'character');
    }
  }

  link() {
    const r = this.range();
    const picked = r.toString();
    this.insert(`[${picked || 'text'}](https://)`, r);
    const sel = window.getSelection();
    sel.modify('move', 'backward', 'character');
  }

  linePrefix(prefix) {
    const before = this.before();
    this.insert((before.text && !before.text.endsWith('\n') ? '\n' : '') + prefix);
  }

  // before gives the plain characters just before the cursor, back to the
  // start of the box or the nearest chip, with where each came from, so a
  // suggestion can replace exactly what was typed.
  before() {
    const sel = window.getSelection();
    if (!sel.rangeCount || !sel.isCollapsed || !this.editor.contains(sel.anchorNode)) return { text: '', spans: [], atStart: false };
    let node = sel.anchorNode, offset = sel.anchorOffset;
    if (node.nodeType !== Node.TEXT_NODE) {
      // Between nodes: look at the one before the cursor.
      node = node.childNodes[offset - 1];
      if (!node || node.nodeType !== Node.TEXT_NODE) return { text: '', spans: [], atStart: !node, caret: { node: sel.anchorNode, offset: sel.anchorOffset } };
      offset = node.data.length;
    }
    const spans = [{ node, from: 0, to: offset }];
    let text = node.data.slice(0, offset);
    let prev = node.previousSibling;
    while (prev && prev.nodeType === Node.TEXT_NODE) { spans.unshift({ node: prev, from: 0, to: prev.data.length }); text = prev.data + text; prev = prev.previousSibling; }
    return { text, spans, atStart: !prev && node.parentNode === this.editor, caret: { node: sel.anchorNode, offset: sel.anchorOffset } };
  }

  // A range over the last n characters before the cursor.
  rangeBack(b, n) {
    const r = document.createRange();
    r.setEnd(b.caret.node, b.caret.offset);
    let left = n;
    for (let i = b.spans.length - 1; i >= 0; i--) {
      const s = b.spans[i];
      const len = s.to - s.from;
      if (left <= len) { r.setStart(s.node, s.to - left); return r; }
      left -= len;
    }
    r.setStart(b.spans[0].node, 0);
    return r;
  }

  // --- suggestions: @ people, # channels, : emoji, / commands ---

  // What was typed can change while a list of suggestions is being
  // fetched. Each call takes a number, and only the newest may open a
  // list; what a choice replaces is measured when it is made.
  async suggest() {
    const seq = this.suggestSeq = (this.suggestSeq || 0) + 1;
    const stale = () => seq !== this.suggestSeq;
    const b = this.before();
    const t = b.text;
    let m;
    const o = this.o;
    const line = t.slice(t.lastIndexOf('\n') + 1);
    if (b.atStart && !this.editing && (m = /^\/([a-z0-9_+-]*)$/.exec(t))) {
      const q = m[1];
      const all = await commandsOf(o.conversationId);
      if (stale()) return undefined;
      const cmds = all.filter((c) => c.name.startsWith(q) && !(isSealed(conversation(o.conversationId) || {}) && ['poll', 'rsvp', 'signup', 'approve', 'form'].includes(c.name)));
      return this.openPopup('Commands', t.length, cmds.slice(0, 12).map((c) => ({
        key: c.name, label: h('span', {}, h('strong', {}, '/' + c.name), c.hint ? h('span', { class: 'dim' }, ' ' + c.hint) : null),
        detail: c.description + (c.kind === 'bot' ? ' · ' + nameOf(c.bot_id) : c.kind === 'saved_reply' ? ' · saved reply' : ''),
        // A command about a person opens the people picker at once.
        pick: () => '/' + c.name + ' ' + (c.kind === 'builtin' && /^person/.test((COMMANDS[c.name] || [])[0]) ? '@' : ''),
      })), 'cmd:' + q);
    }
    if ((m = /(?:^|[^\p{L}\p{N}])@([a-z0-9._-]*)$/u.exec(line))) {
      const q = m[1];
      const members = await membersOf(o.conversationId);
      let people = [...store.accounts.values()].filter((a) => !a.unknown && !a.removed_at && a.role !== 'remote' && (a.username.startsWith(q) || fold(a.display_name).includes(fold(q))));
      // The server is asked only when what this page already knows is thin.
      if (q.length >= 2 && people.length < 5) {
        try { const found = (await get('/accounts', { q, limit: 12 })).accounts; for (const a of found) { store.accounts.set(a.id, { ...store.accounts.get(a.id), ...a }); if (!people.some((p) => p.id === a.id)) people.push(a); } } catch { /* what is known already will do */ }
      }
      if (stale()) return undefined;
      // People in the conversation come first.
      people.sort((x, y) => (members.has(y.id) - members.has(x.id)) || x.username.localeCompare(y.username));
      const items = people.slice(0, 8).map((a) => {
        const notes = [];
        if (!members.has(a.id)) notes.push('not in this conversation: they will not be notified');
        if (a.dnd) notes.push('in do not disturb');
        if (a.availability && a.availability.value === 'out_of_office') notes.push('out of office' + (a.availability.until ? ' until ' + dateTime(a.availability.until) : ''));
        return { key: a.id, label: h('span', {}, avatar(a, 'small'), ' ', h('strong', {}, a.display_name || a.username), h('span', { class: 'dim' }, ' @' + a.username + (a.server_name ? '@' + a.server_name : ''))),
          detail: notes.join(' · '), pick: () => { if (notes.length) this.say(`${a.display_name || a.username} is ${notes.join(', ')}.`); return '@' + a.username + (a.server_name ? '@' + a.server_name : '') + ' '; } };
      });
      for (const g of store.groups.filter((x) => x.handle.startsWith(q)).slice(0, 4)) items.push({ key: g.id, label: h('span', {}, h('strong', {}, '@' + g.handle), h('span', { class: 'dim' }, ' group · ' + g.name)), pick: () => '@' + g.handle + ' ' });
      for (const w of ['channel', 'here']) if (w.startsWith(q) && q.length) items.push({ key: w, label: h('span', {}, h('strong', {}, '@' + w), h('span', { class: 'dim' }, w === 'channel' ? ' everyone in this conversation' : ' everyone here who is online')), pick: () => '@' + w + ' ' });
      return this.openPopup('People and groups', q.length + 1, items, 'at:' + q);
    }
    // `#` and then characters that match the start of a word in the name
    // of a channel the person can see. Spaces are allowed, so `#design
    // rev` still matches.
    if ((m = /(?:^|\s)#([^#\n]{1,48})$/.exec(line))) {
      const q = fold(m[1]);
      const starts = (name) => fold(name).split(/\s+/).some((w, i, ws) => ws.slice(i).join(' ').startsWith(q));
      const found = visibleChannels().filter((c) => starts(c.name) || (c.parent_id && conversation(c.parent_id) && starts(title(conversation(c.parent_id)) + ' ' + c.name)));
      // Those the person is in come first, then by recent use.
      found.sort((x, y) => (!!y.membership - !!x.membership) || String(y.last_message_at || '').localeCompare(String(x.last_message_at || '')));
      // No channel by that name: `#fff` or `#4521` may be an object.
      if (found.length || !this.objectHere()) return this.openPopup('Link a channel?', m[1].length + 1, found.slice(0, 8).map((c) => ({
        key: c.id, label: h('span', {}, channelIcon(c, 'small'), ' ', h('strong', {}, h('bdi', {}, c.name))), detail: detailLine(c, look),
        pick: () => [this.chip(c), document.createTextNode(' ')],
      })), 'hash:' + (t.length - m[1].length));
    }
    if ((m = /(?:^|[^\p{L}\p{N}]):([a-z0-9_+-]{2,})$/u.exec(line))) {
      const q = m[1];
      const items = [];
      for (const e of store.emoji) if (e.name.includes(q) && !e.server_name && items.length < 4) items.push({ key: 'c' + e.id, label: h('span', {}, h('img', { class: 'emoji', src: e.url, alt: '' }), ' :' + e.name + ':'), pick: () => `:${e.name}: ` });
      const seen = new Set();
      for (const [code, ch] of store.standard.byCode) {
        if (items.length >= 10) break;
        if (code.startsWith(q) && !seen.has(ch)) { seen.add(ch); items.push({ key: code, label: h('span', {}, h('span', { class: 'emoji-char' }, ch), ' :' + code + ':'), pick: () => ch + ' ' }); }
      }
      return this.openPopup('Emoji', q.length + 1, items, 'emoji:' + q);
    }
    // An exact token just completed at the cursor: a reference, a typed
    // date and time (class A, ruling R73).
    const obj = this.objectHere();
    if (obj) return this.openObjectPopup(obj);
    // A phrase that names a time, just typed: offered, never converted
    // without the author's say (ruling R69). Class B: Tab or Enter takes it.
    const ph = this.phraseHere(b);
    if (ph) {
      const when = compactTime(Date.parse(ph.time), { zone: viewerZone(), locale: navigator.language || 'en' });
      return this.openPopup(words('makeTime'), 0, [{ key: 'time', label: h('span', {}, icon('clock', null, 'small'), ' ', words('makeTimeLine', { when, yourTime: words('yourTimeShort') })),
        pick: () => { this.hints = [...this.hints, ph]; this.drawInstants(); } },
      { key: 'plain', label: h('span', {}, objectWords('plainRow', { typed: ph.typed })), pick: () => { this.dismissed = 'time'; } }], 'time:' + ph.start);
    }
    // Nothing here asks for a suggestion. A list that was dismissed may
    // open again the next time something does.
    this.dismissed = null;
    this.closePopup();
    return undefined;
  }

  // objectHere is the exact token that ends at the cursor and has not been
  // decided about, with its rows, or null.
  objectHere() {
    const sel = window.getSelection();
    if (!sel.rangeCount || !sel.isCollapsed || !this.editor.contains(sel.anchorNode)) return null;
    const sealed = isSealed(conversation(this.o.conversationId) || {});
    return objectCandidate(this.text(), this.caretIndex(), {
      patterns: sealed ? serverPatterns() : patternsFor(this.o.conversationId), zone: viewerZone(), decided: this.decided,
      // Message links are not made in a sealed chat (ruling R73).
      origin: sealed ? '' : location.origin,
      // Nor are inline dice rolled there.
      dice: !sealed,
    });
  }

  // openObjectPopup offers each reading of a token and then plain text,
  // the first chosen until the author moves.
  openObjectPopup(cand) {
    const zone = viewerZone(), locale = navigator.language || 'en';
    const items = cand.rows.map((row, i) => {
      let label;
      if (row.kind === 'instant') label = h('span', {}, icon('clock', null, 'small'), ' ', objectWords('instantRow', { when: compactTime(Date.parse(row.entity.time), { zone, locale, precision: row.entity.precision }), yourTime: words('yourTimeShort') }));
      else if (row.kind === 'reference') label = h('span', {}, icon('link', null, 'small'), ' ', objectWords('referenceRow', { typed: cand.typed, host: hostOf(row.entity.url) }));
      else if (row.kind !== PLAIN) label = objectRowLabel(row, cand.typed);
      else label = h('span', {}, objectWords('plainRow', { typed: cand.typed }));
      return { key: row.kind + i, label, pick: () => this.takeObject(cand, i) };
    });
    this.openPopup(objectWords('title', { typed: cand.typed }), 0, items, 'obj:' + cand.start);
    if (this.popup) { this.popup.cls = 'A'; this.popup.cand = cand; }
  }

  // takeObject records the row chosen for a token and says so.
  takeObject(cand, index) {
    const d = chooseObject(cand, index);
    this.decided = [...this.decided.filter((x) => x.start !== d.start), d];
    this.accepted = d.kind === PLAIN ? null : { start: d.start, end: d.end, typed: d.typed };
    this.announce(objectWords(d.kind === PLAIN ? 'saidPlain' : 'saidObject', { typed: d.typed }));
    this.closePopup();
    this.suggestSeq = (this.suggestSeq || 0) + 1;
  }

  announce(text) {
    this.announcer.textContent = '';
    setTimeout(() => { this.announcer.textContent = text; }, 30);
  }

  openPopup(titleText, replace, items, token) {
    // Dismissing (Escape, or typing on past the last match) leaves what
    // was typed exactly as it is, and the list stays shut for it.
    const kind = token.split(':')[0];
    if (!items.length || this.dismissed === kind) { this.closePopup(); return; }
    this.closePopup();
    const list = h('ul', { class: 'suggest', role: 'listbox', id: 'suggestions', 'aria-label': titleText });
    const p = { el: list, items, index: 0, replace, token };
    list.append(h('li', { class: 'head', role: 'presentation' }, titleText));
    items.forEach((it, i) => {
      it.el = h('li', { role: 'option', id: 'suggestion-' + i, 'aria-selected': String(i === 0), onmousedown: (e) => { e.preventDefault(); p.index = i; this.accept(); } },
        h('div', {}, it.label), it.detail ? h('small', { class: 'dim' }, it.detail) : null);
      list.append(it.el);
    });
    document.body.append(list);
    place(list, this.editor, { above: true });
    this.popup = p;
    // The list is announced as belonging to the box, and the choice the
    // arrow keys are on is read out, while focus stays in the box.
    this.editor.setAttribute('aria-expanded', 'true');
    this.editor.setAttribute('aria-controls', 'suggestions');
    this.editor.setAttribute('aria-activedescendant', 'suggestion-0');
  }

  closePopup() {
    if (this.popup) this.popup.el.remove();
    this.popup = null;
    this.editor.removeAttribute('aria-expanded');
    this.editor.removeAttribute('aria-controls');
    this.editor.removeAttribute('aria-activedescendant');
  }

  movePopup(by) {
    const p = this.popup;
    p.items[p.index].el.setAttribute('aria-selected', 'false');
    p.index = (p.index + by + p.items.length) % p.items.length;
    p.items[p.index].el.setAttribute('aria-selected', 'true');
    p.items[p.index].el.scrollIntoView({ block: 'nearest' });
    this.editor.setAttribute('aria-activedescendant', 'suggestion-' + p.index);
  }

  accept() {
    const p = this.popup;
    if (!p) return;
    if (p.cls === 'A') {
      // Choosing what a token is changes no text.
      p.items[p.index].pick();
      this.editor.focus();
      return;
    }
    if (p.token.startsWith('time:')) {
      // Making a phrase a time changes no text.
      const it = p.items[p.index];
      this.closePopup();
      this.suggestSeq++;
      it.pick();
      this.editor.focus();
      return;
    }
    const b = this.before();
    // How much of what is before the cursor the choice replaces: from the
    // mark that opened the list to the cursor, as it is now.
    const line = b.text.slice(b.text.lastIndexOf('\n') + 1);
    const kind = p.token.split(':')[0];
    const m = kind === 'cmd' ? [b.text] : kind === 'at' ? /@[a-z0-9._-]*$/.exec(line) : kind === 'emoji' ? /:[a-z0-9_+-]*$/.exec(line) : /#[^#\n]*$/.exec(line);
    const what = p.items[p.index].pick();
    this.closePopup();
    this.suggestSeq++;
    this.insert(what, this.rangeBack(b, m ? m[0].length : Math.min(p.replace, b.text.length)));
    this.editor.focus();
    if (kind === 'cmd') this.suggest();
  }

  // drawBroad shows or hides the line that says a broad mention in the
  // text will go as plain text.
  drawBroad() {
    const hint = this.editing ? null : broadHint(this.text(), conversation(this.o.conversationId));
    this.broadLine.hidden = !hint;
    this.broadLine.textContent = hint || '';
  }

  // broadOK asks, for a text with a broad mention the person may use,
  // how many people it would notify, and above a few dozen asks them.
  broadOK(text) {
    const id = this.o.conversationId;
    return beforeSend(text, conversation(id),
      (t) => post(`/conversations/${id}/broad-mentions/check`, { text: t }),
      (n) => confirmDialog('Notify everyone?', confirmLine(n), { yes: 'Send', no: 'Cancel' }));
  }

  say(text) {
    this.notice.hidden = false;
    fill(this.notice, text);
    clearTimeout(this.noticeTimer);
    this.noticeTimer = setTimeout(() => { this.notice.hidden = true; }, 8000);
  }

  // --- keys ---

  keydown(e) {
    if (e.isComposing) return;
    const mod = e.metaKey || e.ctrlKey;
    // Exact tokens (class A, ruling R73): finishing the word takes the row
    // chosen, Escape takes plain text, and Backspace straight after an
    // accepted object makes it plain text again.
    if (this.popup && this.popup.cls === 'A') {
      const p = this.popup;
      const act = keyAction('A', e.key, { shift: e.shiftKey });
      if (act === 'down' || act === 'up') { e.preventDefault(); this.movePopup(act === 'down' ? 1 : -1); return; }
      if (act === 'plain') { e.preventDefault(); e.stopPropagation(); this.takeObject(p.cand || this.objectHere(), p.items.length - 1); return; }
      if (act === 'commit' || act === 'commit-send' || act === 'accept') {
        p.items[p.index].pick();
        if (act === 'commit-send') { e.preventDefault(); this.send(); return; }
        if (act === 'accept') { e.preventDefault(); return; }
        if (e.key === 'Enter') { e.preventDefault(); this.insert('\n'); return; }
        return; // the space or punctuation goes in as typed
      }
    } else if (this.accepted && !['Shift', 'Control', 'Alt', 'Meta'].includes(e.key)) {
      const a = this.accepted;
      this.accepted = null;
      if (keyAction('A', e.key, { open: false, afterAccept: true }) === 'undo') {
        e.preventDefault();
        this.decided = undoObject(this.decided, a.start, a.end);
        this.announce(objectWords('saidPlain', { typed: a.typed }));
        return;
      }
    }
    if (this.popup) {
      if (e.key === 'ArrowDown') { e.preventDefault(); this.movePopup(1); return; }
      if (e.key === 'ArrowUp') { e.preventDefault(); this.movePopup(-1); return; }
      if (e.key === 'Enter' || e.key === 'Tab') {
        // A command typed out in full is run by Enter, not completed again.
        const typed = this.text();
        const whole = this.popup.token.startsWith('cmd') && e.key === 'Enter' && typed === '/' + this.popup.items[this.popup.index].key;
        if (!whole) { e.preventDefault(); this.accept(); return; }
        this.closePopup();
      }
      if (e.key === 'Escape') {
        e.preventDefault(); e.stopPropagation();
        this.dismissed = this.popup.token.split(':')[0];
        this.closePopup();
        return;
      }
    }
    if (e.key === 'Enter') {
      // Enter sends on a keyboard; Shift+Enter makes a new line. On a
      // phone the send button sends and Enter is a new line.
      e.preventDefault();
      if (e.shiftKey || coarse()) this.insert('\n'); else this.send();
      return;
    }
    if (e.key === 'Escape') {
      if (this.editing) { e.preventDefault(); e.stopPropagation(); this.cancelEdit(); } else if (this.quote) { e.preventDefault(); e.stopPropagation(); this.setQuote(null); }
      return;
    }
    if (e.key === 'ArrowUp' && this.text() === '' && !this.editing && this.o.onEditLast) { e.preventDefault(); this.o.onEditLast(); return; }
    if (mod && !e.altKey) {
      const k = e.key.toLowerCase();
      const hit = (fn) => { e.preventDefault(); fn(); };
      if (k === 'b' && !e.shiftKey) hit(() => this.wrap('**'));
      else if (k === 'i' && !e.shiftKey) hit(() => this.wrap('*'));
      else if (k === 'x' && e.shiftKey) hit(() => this.wrap('~~'));
      else if (k === 'e' && !e.shiftKey) hit(() => this.wrap('`'));
      else if (k === 'c' && e.shiftKey) hit(() => this.wrap('```\n', '\n```'));
      else if (k === 'k' && e.shiftKey) hit(() => this.link());
      else if (k === 'u' && !e.shiftKey) hit(() => this.fileInput.click());
    }
  }

  paste(e) {
    const data = e.clipboardData;
    if (!data) return;
    const files = [...data.files];
    if (files.length) { e.preventDefault(); this.addFiles(files); return; }
    // Pasted text goes in as text; a `<#…>` that names a channel the
    // person can see becomes a chip, and anything else is pasted as it is.
    e.preventDefault();
    this.insert(data.getData('text/plain'));
    this.suggest();
  }

  sayTyping() {
    const now = Date.now();
    if (now - this.lastTyping < 4000 || this.editing || !this.text()) return;
    // Nobody is shown typing in a large conversation or an announcement
    // channel, so nothing is sent.
    if (isLarge(conversation(this.o.conversationId))) return;
    this.lastTyping = now;
    live.typing(this.o.conversationId, this.o.parentId);
  }

  drawTyping() {
    const ids = isLarge(conversation(this.o.conversationId)) ? [] : typingIn(this.o.conversationId, this.o.parentId);
    const names = ids.map(nameOf);
    this.typingLine.textContent = !names.length ? '' : names.length === 1 ? `${names[0]} is typing…` : names.length <= 3 ? `${names.join(', ')} are typing…` : 'Several people are typing…';
  }

  togglePreview() {
    const on = this.preview.hidden;
    this.preview.hidden = !on;
    this.previewBtn.setAttribute('aria-pressed', String(on));
    if (on) this.drawPreview();
  }

  drawPreview() {
    // The server works out mentions and channel links when the message is
    // posted. In the preview they are drawn as typed.
    const text = this.text();
    fill(this.preview, text.trim() ? renderMarkdown(text, [], { standardEmoji: (n) => store.standard.byCode.get(n) }) : h('p', { class: 'dim' }, 'Nothing to preview yet.'));
  }

  // --- drafts ---

  saveDraft() {
    if (this.editing) return;
    const text = this.text();
    if (text || this.quote) drafts.set(this.key, { text, quote: this.quote, quoteOf: this.quoteOf }); else drafts.delete(this.key);
  }

  restore() {
    const d = drafts.get(this.key);
    if (!d) return;
    this.setText(d.text);
    if (d.quote) this.setQuote(d.quote, d.quoteOf);
  }

  // --- replying and editing ---

  // setQuote puts the quoted text above the box. Dismissing it leaves a
  // plain reply.
  setQuote(quote, message) {
    this.quote = quote;
    this.quoteOf = quote ? message : null;
    this.drawBanner();
    this.saveDraft();
  }

  startEdit(m) {
    this.editing = m;
    this.decided = []; this.accepted = null;
    this.setText(m.text || '');
    this.drawBanner();
    this.sendBtn.lastChild.textContent = 'Save';
    this.laterBtn.hidden = true;
    this.focus();
  }

  cancelEdit() {
    this.editing = null;
    this.setText('');
    this.restore();
    this.drawBanner();
    this.sendBtn.lastChild.textContent = 'Send';
    this.laterBtn.hidden = false;
  }

  drawBanner() {
    if (this.editing) {
      this.banner.hidden = false;
      fill(this.banner, icon('edit', null, 'small'), h('span', { class: 'grow' }, 'Editing your message. Escape cancels.'), iconButton('close', 'Cancel editing', () => this.cancelEdit(), 'small'));
    } else if (this.quote) {
      const m = this.quoteOf;
      const text = m ? (this.quote.start != null ? (m.text || '').slice(this.quote.start, this.quote.end) : firstLine(m, { nameOf, md: mdContext }, 200)) : '';
      this.banner.hidden = false;
      fill(this.banner, icon('quote', null, 'small'), h('span', { class: 'grow clip' }, h('strong', {}, m ? nameOf(m.author_id) + ': ' : 'Quote: '), text),
        iconButton('close', 'Remove the quote', () => this.setQuote(null), 'small'));
    } else this.banner.hidden = true;
  }

  // --- attachments ---

  addFiles(list) {
    const limit = store.server.limits.file_size;
    for (const file of list) {
      if (this.files.length >= MAX_FILES) { toast(`A message carries at most ${MAX_FILES} files.`, true); break; }
      // The server's size limit is checked before anything is uploaded.
      if (limit && file.size > limit) { toast(`“${file.name}” is ${bytes(file.size)}; this server takes files up to ${bytes(limit)}.`, true); continue; }
      const f = { file, caption: '', progress: null, error: null, id: null, url: null };
      if (/^image\/(png|jpeg|gif|webp)$/.test(file.type)) f.url = URL.createObjectURL(file);
      this.files.push(f);
    }
    this.drawFiles();
    this.editor.focus();
  }

  drawFiles() {
    this.tray.hidden = !this.files.length;
    fill(this.tray, this.files.map((f) => {
      const cap = h('input', { value: f.caption, placeholder: 'Caption (optional)', maxLength: 200, 'aria-label': 'Caption for ' + f.file.name, disabled: f.progress != null, oninput: () => { f.caption = cap.value; } });
      const bar = h('span', { class: 'bar' });
      if (f.progress != null) bar.style.width = Math.round(f.progress * 100) + '%';
      return h('div', { class: 'attach' + (f.error ? ' failed' : '') },
        f.url ? h('img', { class: 'attach-thumb', src: f.url, alt: '' }) : h('span', { class: 'attach-thumb' }, icon('file')),
        h('div', { class: 'what' }, h('strong', { class: 'clip' }, f.file.name),
          h('small', { class: 'dim' }, f.error ? f.error : f.progress != null ? (f.id ? 'Uploaded' : `Uploading ${Math.round(f.progress * 100)}%`) : bytes(f.file.size)),
          h('span', { class: 'progress', role: 'progressbar', 'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-valuenow': String(Math.round((f.progress || 0) * 100)), 'aria-label': 'Upload of ' + f.file.name, hidden: f.progress == null }, bar),
          cap),
        iconButton('close', 'Remove ' + f.file.name, () => { if (f.abort) f.abort(); if (f.url) URL.revokeObjectURL(f.url); this.files = this.files.filter((x) => x !== f); this.drawFiles(); }, 'small'));
    }));
  }

  // The server makes no poster frame for a video, so one frame is taken
  // here and sent with the upload.
  poster(file) {
    return new Promise((resolve) => {
      if (!/^video\//.test(file.type)) { resolve(null); return; }
      const url = URL.createObjectURL(file);
      const v = document.createElement('video');
      const done = (blob) => { URL.revokeObjectURL(url); resolve(blob); };
      const timer = setTimeout(() => done(null), 4000);
      v.muted = true; v.preload = 'auto'; v.src = url;
      v.addEventListener('error', () => { clearTimeout(timer); done(null); });
      v.addEventListener('loadeddata', () => { v.currentTime = Math.min(0.5, (v.duration || 1) / 2); });
      v.addEventListener('seeked', () => {
        clearTimeout(timer);
        try {
          const c = document.createElement('canvas');
          c.width = v.videoWidth; c.height = v.videoHeight;
          c.getContext('2d').drawImage(v, 0, 0);
          c.toBlob((b) => done(b), 'image/jpeg', 0.8);
        } catch { done(null); }
      });
    });
  }

  async uploadAll() {
    for (const f of this.files) {
      if (f.id) continue;
      f.error = null; f.progress = 0;
      this.drawFiles();
      const body = new FormData();
      body.append('file', f.file, f.file.name);
      if (f.caption.trim()) body.append('caption', f.caption.trim());
      const frame = await this.poster(f.file);
      if (frame) body.append('poster', frame, 'poster.jpg');
      const up = upload(`/conversations/${this.o.conversationId}/files`, body, (p) => { f.progress = p; this.drawFiles(); });
      f.abort = up.abort;
      try {
        f.id = (await up.promise).file.id;
        f.progress = 1;
      } catch (err) {
        f.progress = null;
        f.error = err.name === 'AbortError' ? 'Cancelled' : err.message;
        this.drawFiles();
        throw err;
      }
      this.drawFiles();
    }
    return this.files.map((f) => f.id);
  }

  // --- sending ---

  clear() {
    for (const f of this.files) if (f.url) URL.revokeObjectURL(f.url);
    this.files = [];
    this.drawFiles();
    this.decided = []; this.accepted = null;
    this.setText('');
    this.drawBroad();
    this.quote = null; this.quoteOf = null;
    this.drawBanner();
    drafts.delete(this.key);
    if (!this.preview.hidden) this.togglePreview();
  }

  async send(runAt) {
    if (this.busy) return;
    let text = this.text().replace(/\s+$/, '');
    if (!text && !this.files.length) return;
    // Whitespace then / is text, never a command (ruling R74): it is sent
    // from the /, the whitespace dropped.
    const lit = literal(text);
    if (lit !== null) text = lit;
    this.closePopup();
    this.busy = true;
    this.sendBtn.disabled = true;
    try {
      if (this.editing) { await this.saveEdit(text); return; }
      // A line that starts with `/` and names a command is a command.
      const cmd = lit === null && /^\/([a-z0-9_+-]+)(?:\s+([\s\S]*))?$/.exec(text);
      if (cmd && !runAt && await this.command(cmd[1], cmd[2] || '')) return;
      if (text && !(await this.broadOK(text))) return;
      const file_ids = this.files.length ? await this.uploadAll() : undefined;
      const content = { text: text || undefined, file_ids, parent_id: this.o.parentId || undefined, quote: this.quote || undefined, ...this.instantFields(text) };
      if (runAt) {
        await post('/scheduled', { conversation_id: this.o.conversationId, kind: 'message', content, run_at: runAt });
        toast('It will be sent ' + dateTime(runAt) + '. See Scheduled to change it.');
        this.clear();
        return;
      }
      await this.post(content);
    } catch (err) {
      this.failed(err);
    } finally {
      this.busy = false;
      this.sendBtn.disabled = false;
    }
  }

  // post sends one message. It is drawn at once as pending, and the
  // `client_id` makes a retry safe: the server posts it once.
  async post(content) {
    const client_id = crypto.randomUUID();
    const r = room(this.o.conversationId);
    const pending = { id: 'pending-' + client_id, client_id, conversation_id: this.o.conversationId, author_id: store.me.id, parent_id: content.parent_id, quote: content.quote,
      text: content.text || '', action: content.action || undefined, entities: [], created_at: new Date().toISOString(), _pending: true, _v: 0, _content: content };
    if (r.loaded && !content.form) { r.byId.set(pending.id, pending); r.list.push(pending); store.emit('messages', this.o.conversationId); }
    this.clear();
    if (this.o.onSent) this.o.onSent(pending);
    try {
      const out = await post(`/conversations/${this.o.conversationId}/messages`, { ...content, client_id });
      this.dropPending(pending);
      const m = putMessage(out.message);
      if (this.o.onPosted) this.o.onPosted(m);
      this.afterPost(m, out.cannot_read_ids || [], out.broad_mentions_refused === true);
    } catch (err) {
      if (err.code === 'slow_mode') { this.dropPending(pending); this.setText(content.text || ''); this.slow(err.retryAfter || 5); return; }
      if (r.byId.has(pending.id)) { pending._failed = err.message; pending._v++; store.emit('messages', this.o.conversationId); } else throw err;
    }
  }

  dropPending(p) {
    const r = room(this.o.conversationId);
    if (r.byId.has(p.id)) { r.list.splice(r.list.indexOf(p), 1); r.byId.delete(p.id); store.emit('messages', this.o.conversationId); }
  }

  // retry sends a message that failed again, with the same client_id.
  async retry(p) {
    p._failed = null; p._v++;
    store.emit('messages', this.o.conversationId);
    try {
      const out = await post(`/conversations/${this.o.conversationId}/messages`, { ...p._content, client_id: p.client_id });
      this.dropPending(p);
      putMessage(out.message);
    } catch (err) { p._failed = err.message; p._v++; store.emit('messages', this.o.conversationId); }
  }

  // After posting: say who cannot read the conversation, and offer
  // "notify anyway" for anyone mentioned who is in do not disturb.
  afterPost(m, cannotRead, broadRefused) {
    const parts = [];
    // The server left @channel, @here or @everyone as plain text.
    if (broadRefused) parts.push(h('span', { class: 'broad-refused' }, BROAD_WAS_PLAIN + ' '));
    if (cannotRead.length) parts.push(h('span', {}, `${cannotRead.map(nameOf).join(', ')} cannot read this conversation and will not be notified. `));
    const dnd = (m.entities || []).filter((e) => e.type === 'account').map((e) => account(e.account_id)).filter((a) => a && a.dnd && a.id !== store.me.id);
    for (const a of dnd) {
      const btn = h('button', { type: 'button', class: 'link-btn', onclick: async () => {
        btn.disabled = true;
        try {
          putMessage((await post(`/messages/${m.id}/notify-anyway`, { account_id: a.id })).message);
          // It always appears to work. Whether they were reached is never
          // told, and never shown.
          btn.replaceWith(h('span', { class: 'dim' }, 'Asked.'));
        } catch (err) { toast(err.message, true); btn.disabled = false; }
      } }, 'Notify anyway');
      parts.push(h('span', {}, `${a.display_name || a.username} is in do not disturb. `, btn, ' '));
    }
    if (parts.length) { this.notice.hidden = false; fill(this.notice, parts); clearTimeout(this.noticeTimer); this.noticeTimer = setTimeout(() => { this.notice.hidden = true; }, 30000); }
  }

  // instantFields is what a post says about the times in its text: the
  // author's zone, and the phrases they made into times (ruling R69).
  instantFields(text) {
    if (!text) return {};
    const hints = track(this.hints, text).map(({ start, end, time, precision, zone }) => ({ start, end, time, precision, zone }));
    const objects = objectHints(trackObjects(this.decided, text));
    return { time_zone: viewerZone(), instant_hints: hints.length ? hints : undefined, object_hints: objects.length ? objects : undefined };
  }

  async saveEdit(text) {
    const m = this.editing;
    const out = await patch('/messages/' + m.id, { text, ...this.instantFields(text) });
    putMessage(out.message);
    this.cancelEdit();
  }

  failed(err) {
    if (err && err.silent) return;
    if (err && err.name === 'AbortError') return;
    const said = {
      archived: 'This channel is archived and takes no more posts.',
      timed_out: 'You are timed out here and cannot post.',
      blocked_by_filter: 'The word filter on this server refused that message.',
      too_large: 'That is too large for this server.',
      conflict: 'This thread moved to a channel and takes no more replies.',
    };
    toast((err && said[err.code]) || (err && err.message) || 'That could not be sent.', true);
  }

  // Slow mode: each person may post once per interval. The box says how
  // long is left.
  slow(seconds) {
    const end = Date.now() + seconds * 1000;
    const tick = () => {
      const left = Math.ceil((end - Date.now()) / 1000);
      if (left <= 0) { this.notice.hidden = true; this.sendBtn.disabled = false; clearInterval(this.slowTimer); return; }
      this.notice.hidden = false;
      this.notice.textContent = `Slow mode: you can post again in ${left} s.`;
      this.sendBtn.disabled = true;
    };
    clearInterval(this.slowTimer);
    this.slowTimer = setInterval(tick, 500);
    tick();
  }

  // --- commands ---

  // command carries out `/name text`. It returns false if the name is not
  // a command here, and the line is then posted as an ordinary message.
  async command(name, text) {
    // The built-ins of ruling R67 are read by the one grammar and carried
    // out here; forms and /remind go on below.
    // A built-in the community has a command of the same name for, or one
    // for admins only, is not one here (ruling R72).
    const cmds = await commandsOf(this.o.conversationId);
    const parsed = parseCommand('/' + name + (text ? ' ' + text : ''), hiddenBuiltins(cmds, isAdmin()));
    if (parsed && await runBuiltin(parsed, { conversationId: this.o.conversationId, parentId: this.o.parentId, composer: this })) return true;
    const c = cmds.find((x) => x.name === name);
    if (!c) return false;
    if (c.kind === 'builtin') {
      if (name === 'form') {
        const form = await formBuilder();
        if (form) { this.clear(); await post(`/conversations/${this.o.conversationId}/messages`, { client_id: crypto.randomUUID(), form, parent_id: this.o.parentId || undefined }).then((out) => putMessage(out.message)); }
        return true;
      }
      if (name === 'remind') { await this.remind(text); return true; }
      const form = presetForm(name, text);
      if (typeof form === 'string') { toast(form, true); return true; }
      this.clear();
      const out = await post(`/conversations/${this.o.conversationId}/messages`, { client_id: crypto.randomUUID(), form, parent_id: this.o.parentId || undefined });
      putMessage(out.message);
      if (this.o.onPosted) this.o.onPosted(out.message);
      return true;
    }
    // A saved reply posts its text; a bot's command is sent to the bot,
    // which answers for everyone or for this person alone.
    this.clear();
    const out = await post(`/conversations/${this.o.conversationId}/commands`, { name, text: text || undefined, parent_id: this.o.parentId || undefined });
    if (out.message) putMessage(out.message);
    if (out.interaction_id) store.emit('waiting', { interactionId: out.interaction_id, botId: c.bot_id, conversationId: this.o.conversationId, parentId: this.o.parentId || null });
    return true;
  }

  // `/remind [target] when | what`. The when is asked for in a dialog, so
  // nobody has to learn a grammar for times.
  async remind(text) {
    const what = text.includes('|') ? text.split('|').slice(1).join('|').trim() : text.trim();
    const v = await formDialog('Set a reminder', [
      { name: 'what', label: 'Remind about', type: 'textarea', rows: 2, required: true, value: what },
      { name: 'at', label: 'When', type: 'datetime', required: true, value: new Date(Date.now() + 3600e3) },
      { name: 'where', label: 'Where it arrives', type: 'radio', value: 'me', options: [['me', 'To me alone'], ['here', 'In this conversation, for everyone']] },
    ], { yes: 'Set the reminder' });
    if (!v) return;
    let target = this.o.conversationId;
    if (v.where === 'me') { const mine = (await post('/chats', { account_ids: [] })).conversation; if (!store.conversations.has(mine.id)) { store.conversations.set(mine.id, mine); store.emit('conversations'); } target = mine.id; }
    await post('/scheduled', { conversation_id: target, kind: 'reminder', content: { text: v.what }, run_at: v.at });
    this.clear();
    toast('Reminder set for ' + dateTime(v.at) + '.');
  }

  // "Send later": a few suggested times, or pick one.
  laterMenu(anchor) {
    const at = (h0, addDays) => { const d = new Date(); d.setDate(d.getDate() + addDays); d.setHours(h0, 0, 0, 0); return d; };
    const monday = (() => { const d = at(9, 1); while (d.getDay() !== 1) d.setDate(d.getDate() + 1); return d; })();
    const pick = (d) => this.send(d.toISOString());
    openMenu(anchor, [
      { head: 'Send later' },
      { label: 'In 30 minutes', onclick: () => pick(new Date(Date.now() + 30 * 60e3)) },
      { label: 'In 2 hours', onclick: () => pick(new Date(Date.now() + 2 * 3600e3)) },
      { label: 'Tomorrow at 09:00', onclick: () => pick(at(9, 1)) },
      { label: 'Monday at 09:00', onclick: () => pick(monday) },
      { label: 'Pick a time…', onclick: async () => {
        const v = await formDialog('Send later', [{ name: 'at', label: 'When', type: 'datetime', required: true, value: new Date(Date.now() + 3600e3) }], { yes: 'Schedule' });
        if (v && v.at) this.send(v.at);
      } },
    ], { alignRight: true, above: true, label: 'Send later' });
  }
}

// waiting draws "Waiting for <bot>…" from the moment a command is sent
// until the bot's first response, and "<bot> did not answer" after ten
// seconds. A private response is drawn where the person asked, marked
// "only you can see this"; it is not a message and is gone on reload.
export function watchInteractions(showPrivate) {
  const open = new Map();
  store.on('waiting', (w) => {
    const name = w.botId ? nameOf(w.botId) : 'the bot';
    const el = h('div', { class: 'toast' }, `Waiting for ${name}…`);
    (document.getElementById('toasts') || document.body).append(el);
    const timer = setTimeout(() => { el.textContent = `${name} did not answer.`; el.classList.add('error'); setTimeout(() => el.remove(), 5000); }, 10000);
    open.set(w.interactionId, { el, timer });
  });
  store.on('interaction', (e) => {
    const w = open.get(e.interaction_id);
    if (w) { clearTimeout(w.timer); w.el.remove(); open.delete(e.interaction_id); }
    const r = e.response || {};
    if (r.type === 'private') showPrivate(e);
    else if (r.type === 'dialog' && r.dialog) {
      const d = r.dialog;
      answerDialog({ title: d.title, fields: d.fields, buttons: d.buttons }, {
        nameOf, pickAccounts: () => Promise.resolve(null), pickChannel: () => Promise.resolve(null), conversationName: (id) => title(conversation(id)),
      }, (d.buttons && d.buttons[0] && d.buttons[0].label) || 'Send').then(async (answers) => {
        try {
          if (!answers) { await del('/dialogs/' + d.id); return; }
          const out = await post(`/dialogs/${d.id}/submit`, { answers, button_id: d.buttons && d.buttons[0] ? d.buttons[0].id : undefined });
          if (out.interaction_id) store.emit('waiting', { interactionId: out.interaction_id, botId: d.bot_id });
        } catch (err) { toast(err.message, true); }
      });
    }
  });
}

