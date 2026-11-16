// Drawing Churr Markdown: the parsed tree and the entities the server sent
// become DOM nodes, one by one. No string is ever parsed as HTML, so
// nothing a person types can become markup (docs/CLIENTS.md section 17,
// docs/MESSAGE-FORMAT.md sections 2 to 4).
//
// The parser makes candidates (mention, channel link, emoji, bare link)
// from the shape of the text; the entity over exactly the same range says
// what a candidate is. One with no entity is drawn as the text it covers.
// Names are looked up through the context at the moment of drawing, so a
// renamed person, group or channel shows its new name everywhere.

import { parse, parseInline } from './parse.js';
import { entityAt, objectsIn, taskItems } from './entities.js';
import { instantControl } from '../instant/view.js';
import { measureControl } from '../objects/measure-view.js';
import { eventControl } from '../objects/event.js';
import { diceControl } from '../objects/dice.js';
import { h, icon, iconButton, copyText, openMenu, clock, day, sameDay } from '../dom.js';
import { highlightInto } from '../highlight/hl.js';

// The context a renderer may be given. Every member is optional.
//
//   meId                       the reader's account id
//   myGroupIds                 Set of group ids the reader is in
//   account(id)                → { display_name, username } or undefined
//   group(id)                  → { handle } or undefined
//   conversation(id)           → a conversation the reader can see, or undefined
//   emoji(id)                  → { name, url } or undefined
//   standardEmoji(name)        → the character for a standard short code, or undefined
//   tasks                      the message's `tasks` list
//   canTick                    whether the reader may tick items
//   onTask(itemId, checked)    called when a box is ticked or unticked
//   onTaskHistory(itemId, el)  opens an item's history
//   onAccount(id, el)          a mention was selected
//   onConversation(id, el)     a channel link was selected
//   linkBare                   draw bare links without an entity as links
//                              (cards carry no entities)
//   remindAt(iso)              sets a reminder at an instant (ruling R69)

// renderMarkdown draws a message's text. It returns a <div class="md">.
export function renderMarkdown(text, entities, ctx = {}) {
  const src = String(text || '');
  const tree = parse(src);
  const r = new Renderer(src, entities || [], ctx);
  r.prepareTasks(tree);
  return h('div', { class: 'md' }, r.blocks(tree));
}

// renderInline draws text that holds inline constructs only: a card field,
// a title. It returns a list of nodes.
export function renderInline(text, entities, ctx = {}) {
  const src = String(text || '');
  return new Renderer(src, entities || [], ctx).inlines(parseInline(src));
}

// renderCardText draws a card's text: inline constructs and lists only
// (docs/MESSAGE-FORMAT.md section 6). Other blocks are shown as typed.
export function renderCardText(text, ctx = {}) {
  const src = String(text || '');
  const r = new Renderer(src, [], { ...ctx, linkBare: true, cardBlocks: true });
  return h('div', { class: 'md' }, r.blocks(parse(src)));
}

// looksLikeAddress says whether the visible text of a link reads as an
// address, and gives the host it names.
function hostInText(text) {
  const m = /^\s*(?:https?:\/\/)?((?:[a-z0-9-]+\.)+[a-z]{2,})(?:[/:?#]|\s*$)/i.exec(text);
  return m ? m[1].toLowerCase() : null;
}

function hostOf(url) {
  try { return new URL(url).host.toLowerCase(); } catch { return ''; }
}

class Renderer {
  constructor(src, entities, ctx) {
    this.src = src;
    this.entities = entities;
    this.ctx = ctx;
    this.taskOf = new Map(); // item node → item_id
  }

  // The n-th checklist item of the text is the n-th task entity; an entity
  // over exactly the item's text is preferred when there is one.
  prepareTasks(tree) {
    const tasks = this.entities.filter((e) => e.type === 'task');
    if (!tasks.length) return;
    taskItems(tree, this.src).forEach((it, i) => {
      const e = tasks.find((x) => x.at === it.at && x.len === it.len) || tasks[i];
      if (e) this.taskOf.set(it.node, e.item_id);
    });
  }

  text(n) { return this.src.slice(n.at, n.at + n.len); }

  blocks(nodes) { return (nodes || []).map((n) => this.block(n)); }

  block(n) {
    if (this.ctx.cardBlocks && n.t !== 'p' && n.t !== 'list') {
      // A card's text takes inline constructs and lists only.
      return h('p', {}, this.text(n));
    }
    switch (n.t) {
      case 'p': return h('p', {}, this.inlines(n.c));
      case 'h': return h('div', { class: 'md-h md-h' + n.level, role: 'heading', 'aria-level': String(n.level + 2) }, this.inlines(n.c));
      case 'quote': return h('blockquote', {}, this.blocks(n.b));
      case 'list': return this.list(n);
      case 'codeblock': return codeBlock(n.text || '', n.lang || '');
      case 'table': return this.table(n);
      case 'rule': return h('hr', {});
      default: return h('p', {}, this.text(n));
    }
  }

  list(n) {
    const hasTask = (n.items || []).some((it) => it.checked !== undefined);
    const el = h(n.kind === 'numbered' ? 'ol' : 'ul', { class: hasTask ? 'has-tasks' : '' });
    if (n.kind === 'numbered' && n.start !== 1) el.start = n.start;
    for (const it of n.items || []) el.append(this.item(it));
    return el;
  }

  item(it) {
    const nested = (it.b || []).map((b) => this.list(b));
    if (it.checked === undefined) return h('li', {}, this.inlines(it.c), nested);
    // A checklist item. After posting, its state lives in `tasks`, not in
    // the text: the box the author typed only sets where it starts.
    const id = this.taskOf.get(it);
    const state = id && (this.ctx.tasks || []).find((t) => t.id === id);
    const checked = state ? state.checked : it.checked;
    const live = !!(id && this.ctx.onTask);
    const box = h('input', { type: 'checkbox', checked, disabled: !(live && this.ctx.canTick) });
    if (live) box.addEventListener('change', () => this.ctx.onTask(id, box.checked, box));
    const body = h('span', { class: 'task-text' }, this.inlines(it.c));
    const label = h('label', { class: 'task-label' }, box, body);
    const meta = h('span', { class: 'task-meta' });
    if (state && state.checked && state.checked_by_id) {
      const who = this.ctx.account && this.ctx.account(state.checked_by_id);
      const at = state.checked_at;
      meta.append(`${who ? who.display_name || who.username : 'someone'}${at ? ', ' + (sameDay(at, new Date()) ? clock(at) : day(at)) : ''}`);
    }
    const li = h('li', { class: 'task' + (checked ? ' done' : ''), dataset: id ? { item: id } : {} }, label, meta);
    if (id && this.ctx.onTaskHistory) {
      li.append(iconButton('history', 'History of this item', (e) => this.ctx.onTaskHistory(id, e.currentTarget), 'small task-history'));
    }
    li.append(nested);
    return li;
  }

  table(n) {
    const cls = (i) => (n.align && n.align[i] && n.align[i] !== 'none' ? 'align-' + n.align[i] : null);
    const row = (cells, tag) => h('tr', {}, cells.map((c, i) => h(tag, { class: cls(i), scope: tag === 'th' ? 'col' : null }, this.inlines(c.c))));
    return h('div', { class: 'md-table', tabIndex: 0, role: 'group', 'aria-label': 'Table' },
      h('table', {}, h('thead', {}, row(n.head || [], 'th')), h('tbody', {}, (n.rows || []).map((r) => row(r, 'td')))));
  }

  inlines(nodes) { return (nodes || []).map((n) => this.inline(n)); }

  inline(n) {
    const ctx = this.ctx;
    switch (n.t) {
      case 'text': return this.plain(n);
      case 'bold': return h('strong', {}, this.inlines(n.c));
      case 'italic': return h('em', {}, this.inlines(n.c));
      case 'strike': return h('s', {}, this.inlines(n.c));
      case 'spoiler': return spoiler(this.inlines(n.c));
      case 'code': return h('code', {}, n.text || '');
      case 'br': return h('br', {});
      case 'esc': return n.ch;
      case 'link': return link(n.url, this.inlines(n.c), this.src.slice(n.at + 1, n.at + n.len).split('](')[0]);
      case 'nolink': return this.inlines(n.c);
      // An image is never loaded from another site: it is shown as a link
      // to its address.
      case 'image': return link(n.url, [n.url], n.url);
      case 'url': {
        const e = entityAt(this.entities, n);
        if (e && e.type === 'message_link') return this.object(e, this.text(n));
        return e || ctx.linkBare ? link(e ? e.url : n.url, [this.text(n)], this.text(n)) : this.text(n);
      }
      case 'mention': return this.mention(n);
      case 'channel': return this.channel(n);
      case 'emoji': return this.emoji(n);
      case 'instant': {
        const e = entityAt(this.entities, n);
        if (e && e.type === 'event') return eventControl(e, this.text(n), { remind: ctx.remindAt, ...(ctx.eventFile ? ctx.eventFile(e) : {}) });
        return e ? instantControl(e, this.text(n), { remind: ctx.remindAt }) : this.text(n);
      }
      // A kind of node this client does not know is drawn as its text.
      default: return this.text(n);
    }
  }

  // plain draws a run of text, with what lies inside it cut out: instants
  // the author's client resolved from phrases ("tomorrow at 5pm") and the
  // objects of ruling R73.
  plain(n) {
    const inside = objectsIn(this.entities, n);
    if (!inside.length) return this.text(n);
    const out = [];
    let at = n.at;
    for (const e of inside) {
      if (e.at < at) continue;
      if (e.at > at) out.push(this.src.slice(at, e.at));
      out.push(this.object(e, this.src.slice(e.at, e.at + e.len)));
      at = e.at + e.len;
    }
    if (at < n.at + n.len) out.push(this.src.slice(at, n.at + n.len));
    return out;
  }

  // object draws one entity found inside a run of text. The typed text is
  // always what is shown, or what a control is labelled with.
  object(e, typed) {
    switch (e.type) {
      case 'instant': return instantControl(e, typed, { remind: this.ctx.remindAt });
      // A reference opens the address it resolved to when it was written.
      case 'reference': {
        const a = h('a', { class: 'ref', href: e.url, target: '_blank', rel: 'noopener noreferrer nofollow', title: 'Opens ' + (hostOf(e.url) || e.url) }, typed);
        return a;
      }
      // A link to a message of this server: the chat draws a preview of
      // it as this reader may see it; anywhere else it is drawn as typed.
      case 'message_link': return this.ctx.messageLink ? this.ctx.messageLink(e, typed) : typed;
      // A measurement keeps what was typed; the reader's system's
      // equivalent follows in muted text, unless it is the same system.
      // Selecting it opens the unit converter.
      case 'measure': return measureControl(e, typed, this.ctx.unitSystem);
      // A colour: a swatch before the typed text; selecting it copies the
      // colour.
      case 'colour': {
        const swatch = h('span', { class: 'swatch', 'aria-hidden': 'true' });
        swatch.style.background = e.value;
        return h('button', { type: 'button', class: 'colour', title: 'Copy ' + e.value, 'aria-label': `Colour ${typed}. Copy`,
          onclick: () => copyText(e.value, 'Copied ' + e.value) }, swatch, typed);
      }
      // A telephone number or an email address: a menu to call or write
      // (the device's own app, through tel: or mailto:) or copy.
      case 'phone':
      case 'email': {
        const phone = e.type === 'phone';
        const el = h('button', { type: 'button', class: 'contact', 'aria-haspopup': 'menu', 'aria-label': (phone ? 'Phone number ' : 'Email address ') + typed }, typed);
        el.addEventListener('click', (ev) => {
          ev.preventDefault(); ev.stopPropagation();
          openMenu(el, [
            { label: phone ? 'Call' : 'Write', icon: phone ? 'call' : 'send', onclick: () => { location.href = (phone ? 'tel:' : 'mailto:') + e.value; } },
            { label: 'Copy', icon: 'copy', onclick: () => copyText(e.value, 'Copied ' + e.value) },
          ], { label: typed });
        });
        return el;
      }
      // A place: the web has no maps of its own, so selecting it copies
      // the coordinates for any map the reader likes.
      case 'geo': {
        const value = e.lat + ',' + e.lon;
        return h('button', { type: 'button', class: 'geo', title: 'Copy ' + value, 'aria-label': `Place ${typed}. Copy`,
          onclick: () => copyText(value, 'Copied ' + value) }, typed);
      }
      // Inline dice: the server's roll, spun once when it arrives live.
      case 'dice': return diceControl(e, typed, { live: this.ctx.diceLive || null });
      default: return typed;
    }
  }

  mention(n) {
    const ctx = this.ctx;
    const e = entityAt(this.entities, n);
    if (!e) return this.text(n);
    if (e.type === 'account') {
      const a = ctx.account && ctx.account(e.account_id);
      // A removed account shows the name as it was written.
      const label = a ? '@' + (a.display_name || a.username) : this.text(n);
      const me = e.account_id === ctx.meId;
      const el = h('span', { class: 'mention' + (me ? ' me' : ''), title: a ? '@' + a.username + (a.server_name ? '@' + a.server_name : '') : null }, label);
      if (ctx.onAccount) makeButton(el, () => ctx.onAccount(e.account_id, el));
      return el;
    }
    if (e.type === 'group') {
      const g = ctx.group && ctx.group(e.group_id);
      const mine = !!(ctx.myGroupIds && ctx.myGroupIds.has(e.group_id));
      return h('span', { class: 'mention group' + (mine ? ' me' : '') }, g ? '@' + g.handle : this.text(n));
    }
    // @channel and @here are for everyone.
    return h('span', { class: 'mention me broadcast' }, this.text(n));
  }

  channel(n) {
    const ctx = this.ctx;
    const e = entityAt(this.entities, n);
    // A `<#…>` that names no channel has no entity.
    if (!e) return h('span', { class: 'chip unknown' }, 'unknown channel');
    const conv = ctx.conversation && ctx.conversation(e.conversation_id);
    // A link to a channel the reader cannot see has no name and no card.
    if (!conv) return h('span', { class: 'chip unknown' }, icon('lock', null, 'small'), 'private channel');
    const el = h('span', { class: 'chip channel', dataset: { conversation: conv.id } },
      icon(conv.locked_at ? 'padlock' : conv.private ? 'lock' : 'hash', null, 'small'), h('bdi', {}, conv.name || 'chat'));
    if (ctx.onConversation) makeButton(el, () => ctx.onConversation(conv.id, el));
    return el;
  }

  emoji(n) {
    const ctx = this.ctx;
    const e = entityAt(this.entities, n);
    if (e) {
      const em = ctx.emoji && ctx.emoji(e.emoji_id);
      if (em && em.url) return h('img', { class: 'emoji', src: em.url, alt: this.text(n), title: this.text(n), loading: 'lazy' });
      return this.text(n);
    }
    // No entity: a standard short code is its character; anything else is
    // the text as typed.
    const ch = !n.server && ctx.standardEmoji && ctx.standardEmoji(n.name);
    return ch ? h('span', { class: 'emoji-char', title: this.text(n) }, ch) : this.text(n);
  }
}

// makeButton lets a span be selected with the keyboard as well as the
// pointer.
function makeButton(el, fn) {
  el.tabIndex = 0;
  el.setAttribute('role', 'button');
  el.addEventListener('click', (e) => { e.preventDefault(); fn(); });
  el.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); fn(); } });
}

// A link always opens outside the page, shows its destination host on
// hover, and is marked when the visible text looks like a different
// address from the real one.
function link(url, kids, visible) {
  const host = hostOf(url) || url;
  const a = h('a', { href: url, target: '_blank', rel: 'noopener noreferrer nofollow', title: 'Opens ' + host }, kids);
  const shown = hostInText(String(visible || ''));
  if (shown && hostOf(url) && shown !== hostOf(url) && !/^(mailto|tel):/i.test(url)) {
    return h('span', { class: 'link-warn' }, a, h('span', { class: 'link-real' }, icon('warn', 'Warning', 'small'), ' goes to ' + host));
  }
  return a;
}

// Hidden until the reader selects it.
function spoiler(kids) {
  const el = h('span', { class: 'spoiler', role: 'button', tabIndex: 0, 'aria-label': 'Spoiler. Select to show.', 'aria-expanded': 'false' },
    h('span', { class: 'spoiler-text', 'aria-hidden': 'true' }, kids));
  const show = () => {
    el.classList.add('shown');
    el.removeAttribute('role'); el.removeAttribute('aria-label'); el.removeAttribute('tabindex'); el.removeAttribute('aria-expanded');
    el.firstChild.removeAttribute('aria-hidden');
  };
  el.addEventListener('click', show);
  el.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); show(); } });
  return el;
}

// codeBlock draws code in monospace with a copy button. It scrolls
// sideways and does not wrap unless the reader turns wrapping on.
export function codeBlock(code, lang, opts = {}) {
  const pre = h('pre', { tabIndex: 0 });
  const codeEl = h('code', {});
  pre.append(codeEl);
  highlightInto(codeEl, code, lang);
  const wrap = h('button', { type: 'button', class: 'link-btn small', 'aria-pressed': 'false', onclick: () => {
    const on = pre.classList.toggle('wrap');
    wrap.setAttribute('aria-pressed', String(on));
  } }, 'Wrap');
  return h('div', { class: 'codeblock' },
    h('div', { class: 'codeblock-bar' },
      h('span', { class: 'codeblock-lang' }, opts.title || lang || 'code'),
      wrap,
      h('button', { type: 'button', class: 'link-btn small', onclick: () => copyText(code, 'Code copied') }, 'Copy')),
    pre);
}

// plainText writes a message's text the way "copy text" does: marks
// dropped, a channel link as the channel's name at this moment (or the
// words "private channel" or "unknown channel"), never an identifier
// (docs/CLIENTS.md section 9.26).
export function plainText(text, entities, ctx = {}) {
  const src = String(text || '');
  const ents = entities || [];
  const inl = (nodes) => (nodes || []).map((n) => {
    switch (n.t) {
      case 'text': return src.slice(n.at, n.at + n.len);
      case 'bold': case 'italic': case 'strike': case 'spoiler': case 'link': case 'nolink': return inl(n.c);
      case 'code': return n.text || '';
      case 'image': return n.url;
      case 'br': return '\n';
      case 'esc': return n.ch;
      case 'channel': {
        const e = entityAt(ents, n);
        if (!e) return 'unknown channel';
        const conv = ctx.conversation && ctx.conversation(e.conversation_id);
        return conv ? (conv.name || 'chat') : 'private channel';
      }
      default: return src.slice(n.at, n.at + n.len);
    }
  }).join('');
  const blk = (nodes, indent = '') => (nodes || []).map((n) => {
    switch (n.t) {
      case 'p': case 'h': return inl(n.c);
      case 'quote': return blk(n.b).split('\n').map((l) => '> ' + l).join('\n');
      case 'list': return (n.items || []).map((it, i) => {
        const mark = n.kind === 'numbered' ? `${(n.start || 1) + i}. ` : it.checked === undefined ? '- ' : it.checked ? '- [x] ' : '- [ ] ';
        return indent + mark + inl(it.c) + ((it.b || []).length ? '\n' + blk(it.b, indent + '  ') : '');
      }).join('\n');
      case 'codeblock': return n.text || '';
      case 'table': return [n.head, ...(n.rows || [])].map((r) => (r || []).map((c) => inl(c.c)).join(' | ')).join('\n');
      case 'rule': return '---';
      default: return src.slice(n.at, n.at + n.len);
    }
  }).join('\n');
  return blk(parse(src));
}
