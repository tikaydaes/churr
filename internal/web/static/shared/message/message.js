// How one message is drawn (docs/MESSAGE-FORMAT.md): its text in Churr
// Markdown, then its files, its cards and its form, with reactions, the
// thread summary and the actions beside it. Everything from the server
// goes into the page as text or through the Markdown renderer; nothing is
// parsed as HTML.
//
// The chat hands in an `env` that knows names and permissions and carries
// out actions. This file decides only what a message looks like.

import { h, icon, iconButton, avatar, clock, dateTime, plural, ago } from '../dom.js';
import { renderMarkdown, plainText } from '../md/render.js';
import { renderFiles } from './files.js';
import { renderCards } from './cards.js';
import { renderForm } from './forms.js';
import { renderReactions } from './reactions.js';
import { renderChance, spinLive, spokenResult } from './chance.js';

// What the server posts itself is worded here, so it follows the reader's
// language. An unknown kind is "Something happened here".
export function systemText(m, env) {
  const who = env.nameOf(m.author_id);
  const s = m.system || {};
  switch (s.kind) {
    case 'member_joined': return s.by_id ? `${who} was added by ${env.nameOf(s.by_id)}` : `${who} joined`;
    case 'member_left': return `${who} left`;
    case 'call_started': return `${who} started a call`;
    case 'call_ended': return `A call ended${s.duration_seconds ? ` after ${Math.max(1, Math.round(s.duration_seconds / 60))} min` : ''}`;
    case 'thread_moved': return 'This thread moved to its own channel';
    case 'channel_renamed': return `${who} renamed the channel${s.name ? ' to ' + s.name : ''}`;
    case 'pinned': return `${who} pinned a message`;
    default: return 'Something happened here';
  }
}

const VIA = { webhook: 'webhook', reminder: 'reminder', scheduled: 'sent later', command: 'command', import: 'imported' };

// firstLine is the start of a message's text, for a quote header, a
// marker or a notification.
export function firstLine(m, env, max = 90) {
  if (!m) return '';
  if (m.deleted_at) return 'deleted message';
  let text = m.text ? plainText(m.text, m.entities, env.md ? env.md(m) : {}).split('\n').find((l) => l.trim()) || '' : '';
  if (text && m.action) text = env.nameOf(m.author_id) + ' ' + text;
  if (!text) text = (m.files || []).length ? (m.files[0].caption || m.files[0].name) : m.form ? (m.form.title || 'a form') : (m.cards || []).length ? (m.cards[0].title || 'a card') : m.chance ? spokenResult(m.chance) : m.system ? systemText(m, env) : '';
  return text.length > max ? text.slice(0, max - 1) + '…' : text;
}

// What a screen reader hears for a message: who, when, the text, then
// what it carries.
function spoken(m, env) {
  const carries = [];
  if ((m.files || []).length) carries.push(plural(m.files.length, 'file'));
  if ((m.cards || []).length) carries.push(plural(m.cards.length, 'card'));
  if (m.form) carries.push(m.form.preset === 'poll' ? 'a poll' : 'a form');
  if ((m.reactions || []).length) carries.push(plural(m.reactions.reduce((n, r) => n + r.count, 0), 'reaction'));
  if (m.thread && m.thread.reply_count) carries.push(plural(m.thread.reply_count, 'reply', 'replies'));
  return `${env.nameOf(m.author_id)}, ${clock(m.created_at)}: ${firstLine(m, env, 200)}${carries.length ? '. ' + carries.join(', ') : ''}`;
}

// The quote of a reply is drawn from the quoted message's current text.
// If that message is deleted, or the range no longer exists, it says
// "original message changed".
function quoteBlock(m, env) {
  const q = m.quote;
  if (!q) return null;
  const src = env.quoted(q.message_id);
  let body;
  if (src === null && q.text) {
    // A message this reader may not open (posted before they joined a
    // private channel): the quote is shown from what the server carried,
    // and there is nothing to jump to.
    return h('blockquote', { class: 'msg-quote' },
      h('span', { class: 'quote-by dim', title: 'You cannot open this message' }, 'a message you can’t see'),
      h('div', { class: 'quote-text' }, h('span', {}, q.text)));
  }
  if (src === undefined) body = h('span', { class: 'dim' }, 'Loading the quoted message…');
  else if (!src || src.deleted_at) body = h('span', { class: 'dim' }, 'original message changed');
  else if (q.start != null && q.end != null) {
    const text = src.text || '';
    body = q.end <= text.length && q.start < q.end ? h('span', {}, text.slice(q.start, q.end)) : h('span', { class: 'dim' }, 'original message changed');
  } else body = h('span', {}, firstLine(src, env, 240));
  const by = src && !src.deleted_at ? env.nameOf(src.author_id) : null;
  return h('blockquote', { class: 'msg-quote' },
    by && h('button', { type: 'button', class: 'quote-by link-btn', onclick: () => env.on.jump(src) }, by),
    h('div', { class: 'quote-text' }, body));
}

// The header of a reply in the flat view: who it answers and the start of
// that message, as a link up to it; a link to the top message when that
// is a different one; and "Open thread".
function replyHeader(m, env) {
  const parent = env.quoted(m.parent_id);
  const answered = m.quote ? env.quoted(m.quote.message_id) : parent;
  const line = h('div', { class: 'msg-replyto' }, icon('reply', null, 'small'));
  if (answered) {
    line.append(h('button', { type: 'button', class: 'link-btn', onclick: () => env.on.jump(answered) }, `${env.nameOf(answered.author_id)}: ${firstLine(answered, env, 60)}`));
  } else line.append(h('span', { class: 'dim' }, 'a reply'));
  if (parent && answered && parent.id !== answered.id) {
    line.append(' · ', h('button', { type: 'button', class: 'link-btn', onclick: () => env.on.jump(parent) }, 'top message'));
  }
  line.append(' · ', h('button', { type: 'button', class: 'link-btn', onclick: () => env.on.openThread(parent || { id: m.parent_id, conversation_id: m.conversation_id }) }, 'Open thread'));
  return line;
}

// The summary line under a message that has replies: how many, the faces
// of who replied, when the last one came.
function threadSummary(m, env) {
  const t = m.thread;
  if (!t || !t.reply_count) return null;
  const state = env.threadState(m.id);
  const unread = state && state.state !== 'ignored' ? state.unread_count : 0;
  return h('button', { type: 'button', class: 'msg-thread' + (unread ? ' unread' : ''), onclick: () => env.on.openThread(m) },
    h('span', { class: 'faces' }, (t.participant_ids || []).slice(0, 4).map((id) => avatar(env.account(id) || { id }, 'small'))),
    h('span', { class: 'n' }, plural(t.reply_count, 'reply', 'replies')),
    unread ? h('span', { class: 'badge' }, String(unread)) : null,
    h('span', { class: 'when' }, 'last ' + ago(t.last_reply_at)),
    state && state.state === 'following' ? h('span', { class: 'tag line' }, 'following') : state && state.state === 'ignored' ? h('span', { class: 'tag line' }, 'ignored') : null);
}

// renderMessage draws one message. opts:
//   grouped   it follows another by the same author within five minutes
//   flat      the flat view: a reply shows who it answers
//   inThread  drawn in the thread pane: no summary line, no reply header
//   bare      no actions (search results, pins, the notifications list)
export function renderMessage(m, env, opts = {}) {
  // What the server posts itself is a small centred line.
  if (m.system || (m.via && m.via.kind === 'system')) {
    const s = m.system || {};
    const to = s.kind === 'thread_moved' && s.conversation_id ? env.conversation(s.conversation_id) : null;
    return h('div', { class: 'msg-system', dataset: { id: m.id }, role: 'note' },
      // A place may word some kinds itself, with actions (a call's Join).
      (env.systemLine && env.systemLine(m)) || systemText(m, env),
      s.kind === 'thread_moved' && s.conversation_id ? [': ', to
        ? h('button', { type: 'button', class: 'link-btn', onclick: () => env.on.openConversation(s.conversation_id) }, to.name || 'the new channel')
        : h('span', { class: 'dim' }, 'a channel you are not in')] : null,
      h('span', { class: 'when' }, ' · ' + clock(m.created_at)));
  }

  const author = env.account(m.author_id) || { id: m.author_id, display_name: '…' };
  const perms = env.perms(m);
  const mentionsMe = (m.entities || []).some((e) => (e.type === 'account' && e.account_id === env.me.id) || e.type === 'broadcast' || (e.type === 'group' && env.md(m).myGroupIds.has(e.group_id)));
  const el = h('article', {
    class: 'msg' + (opts.grouped ? ' grouped' : '') + (mentionsMe ? ' mentions-me' : '') + (m._pending ? ' pending' : '') + (m.unsent ? ' unsent' : '') + (opts.late ? ' late' : '') + (m._failed ? ' failed' : ''),
    dataset: { id: m.id }, tabIndex: opts.bare ? null : 0, 'aria-label': spoken(m, env),
  });

  // A message from a blocked account is collapsed to one line that can be
  // opened.
  if (env.blocked(m.author_id) && !m._shown) {
    el.classList.add('collapsed');
    el.append(h('div', { class: 'msg-gutter' }), h('div', { class: 'msg-main' }, h('span', { class: 'dim' }, 'A message from someone you blocked. '),
      h('button', { type: 'button', class: 'link-btn', onclick: () => { m._shown = true; m._v = (m._v || 0) + 1; env.on.redraw(m); } }, 'Show')));
    return el;
  }

  const name = h('button', { type: 'button', class: 'msg-author', onclick: (e) => env.on.profile(m.author_id, e.currentTarget) }, author.display_name || author.username);
  const via = m.via && VIA[m.via.kind];
  // The marks beside the time. A message drawn under the one before it has
  // no header, and carries its marks on a small line of its own.
  const marks = [
    m.edited_at ? h('span', { class: 'msg-mark', title: 'Edited ' + dateTime(m.edited_at) }, 'edited') : null,
    m.pinned ? h('span', { class: 'msg-mark' }, icon('pin', 'Pinned', 'small')) : null,
    m.notified_anyway ? h('span', { class: 'msg-mark', title: 'The author asked for this to notify despite do not disturb' }, icon('bell', 'Notified anyway', 'small')) : null,
    env.bookmarked(m.id) ? h('span', { class: 'msg-mark' }, icon('bookmark', 'Bookmarked', 'small')) : null,
    m._pending ? h('span', { class: 'msg-mark' }, m._failed ? 'not sent' : 'sending…') : null,
    // Stored and shown in its place, but the other servers of a shared
    // conversation have not acknowledged it yet (docs/EVENTS.md section 8).
    m.unsent ? h('span', { class: 'msg-mark', title: 'The other servers have not received this yet' }, 'unsent') : null,
    // Unread, and it arrived after messages that come below it: it is shown
    // where it belongs, so it is marked by itself.
    opts.late ? h('span', { class: 'tag late', title: 'This arrived after the messages below it, and is shown in its place' }, 'arrived late') : null,
  ].filter(Boolean);
  const head = h('header', { class: 'msg-head' }, name,
    author.role === 'bot' ? h('span', { class: 'tag line' }, 'bot') : null,
    // A moderator of a channel a hub owns (docs/LINK-PROTOCOL.md 23a).
    env.moderator && env.moderator(m) ? h('span', { class: 'tag lavender', title: 'A moderator of this channel, for every server in it' }, 'moderator') : null,
    via ? h('span', { class: 'tag line', title: m.via.name || '' }, m.via.kind === 'webhook' || m.via.kind === 'command' ? (m.via.name || via) : via) : null,
    m.via && m.via.account_id ? h('span', { class: 'dim small' }, 'for ' + env.nameOf(m.via.account_id)) : null,
    h('time', { class: 'msg-time', dateTime: m.created_at, title: dateTime(m.created_at) }, clock(m.created_at)),
    opts.grouped ? null : marks);

  const main = h('div', { class: 'msg-main' });
  if (!opts.grouped) main.append(head);
  else if (marks.length) main.append(h('div', { class: 'msg-marks' }, marks));
  if (opts.flat && m.parent_id && !opts.inThread) main.append(replyHeader(m, env));

  if (m.deleted_at) {
    main.append(h('p', { class: 'msg-deleted' }, icon('trash', null, 'small'), ` Deleted${m.deleted_by_id && m.deleted_by_id !== m.author_id ? ' by ' + env.nameOf(m.deleted_by_id) : ''} · ${dateTime(m.deleted_at)}`,
      m.has_history ? [' · ', h('button', { type: 'button', class: 'link-btn', onclick: () => env.on.history(m) }, 'History')] : null,
      // The author may ask for what is kept of it to be purged.
      m.has_history && m.author_id === env.me.id && !opts.bare ? [' · ', h('button', { type: 'button', class: 'link-btn', onclick: () => env.on.purge(m) }, 'Ask for purge')] : null));
  } else if (m.sealed) {
    main.append(h('p', { class: 'dim' }, icon('sealed', null, 'small'), ' A sealed message. Sealed chats cannot be read in this build of the web chat.'));
  } else {
    const quote = quoteBlock(m, env);
    if (quote) main.append(quote);
    // /me: the author's name, then what they are doing (ruling R67).
    if (m.text && m.action) main.append(h('div', { class: 'msg-action' }, h('span', { class: 'action-name' }, author.display_name || author.username), ' ', renderMarkdown(m.text, m.entities, env.md(m))));
    else if (m.text) main.append(renderMarkdown(m.text, m.entities, env.md(m)));
    // A result the server drew. It plays once when it arrives live, and
    // never again (rulings R67, R68, R87).
    if (m.chance) {
      const res = renderChance(m.chance);
      main.append(res);
      if (m._live && !opts.bare) spinLive(res, m.chance, m._spins = m._spins || {}, 'chance');
    }
    const files = renderFiles(m.files);
    if (files) main.append(files);
    const cards = renderCards(m.cards, { md: env.md(m), nameOf: env.nameOf, onRemovePreview: perms.canRemovePreview ? (i) => env.on.removePreview(m, i) : null });
    if (cards) main.append(cards);
    if (m.form) {
      main.append(renderForm(m, {
        me: env.me, canAnswer: perms.member && !perms.timedOut, canClose: m.author_id === env.me.id || perms.manager,
        seesResults: m.author_id === env.me.id || perms.manager || (m.form.action && m.form.action.type === 'tally'),
        account: env.account, nameOf: env.nameOf, conversationName: (id) => { const c = env.conversation(id); return c ? c.name || 'a chat' : 'a channel'; },
        pickAccounts: env.pickAccounts, pickChannel: env.pickChannel,
        onForm: (form) => env.on.formChanged(m, form), onOpenConversation: env.on.openConversation, onInteraction: env.on.interaction,
      }));
    }
    if (!m.text && !files && !cards && !m.form && !m.chance && m.fallback) main.append(h('p', {}, m.fallback));
    if (m._failed) main.append(h('p', { class: 'msg-failed' }, icon('warn', null, 'small'), ' ', m._failed, ' ',
      h('button', { type: 'button', class: 'link-btn', onclick: () => env.on.retry(m) }, 'Try again'), ' · ',
      h('button', { type: 'button', class: 'link-btn', onclick: () => env.on.discard(m) }, 'Discard')));
  }

  const reactions = renderReactions(m.reactions, {
    custom: env.custom, names: (ids) => ids.map(env.nameOf).join(', '), canReact: perms.member && !perms.timedOut && !opts.bare,
    onToggle: (emoji, on) => env.on.toggleReaction(m, emoji, on), onAdd: (anchor) => env.on.react(m, anchor),
  });
  if (reactions) main.append(reactions);
  if (!opts.inThread && !opts.flat && !m.parent_id) { const t = threadSummary(m, env); if (t) main.append(t); }

  const gutter = h('div', { class: 'msg-gutter' }, opts.grouped
    ? h('time', { class: 'msg-time-side', dateTime: m.created_at }, clock(m.created_at))
    : (m.via && m.via.icon_file_id ? h('img', { class: 'avatar', src: `/api/v1/files/${m.via.icon_file_id}/thumb`, alt: '' }) : avatar(author)));
  el.append(gutter, main);

  // The actions, on hover and on focus; the menu holds the rest.
  if (!opts.bare && !m.deleted_at && !m._pending) {
    el.append(h('div', { class: 'msg-actions', role: 'toolbar', 'aria-label': 'Message actions' },
      perms.member && !perms.timedOut ? iconButton('emoji', 'React', (e) => env.on.react(m, e.currentTarget), 'small') : null,
      perms.canPost ? iconButton(opts.inThread ? 'quote' : 'reply', opts.inThread ? 'Reply with a quote' : 'Reply in thread', () => env.on.reply(m), 'small') : null,
      iconButton('more', 'More actions', (e) => env.on.more(m, e.currentTarget), 'small')));
    el.addEventListener('contextmenu', (e) => {
      // The browser's own menu is left alone over a link, or when text is
      // selected to copy.
      if (e.target.closest('a') || String(window.getSelection())) return;
      e.preventDefault();
      env.on.more(m, { x: e.clientX, y: e.clientY });
    });
  }
  return el;
}
