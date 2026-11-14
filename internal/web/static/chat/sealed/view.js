// A sealed chat on screen (docs/SEALED.md sections 8, 9, 14 and 15): its
// messages, decrypted in this tab and never stored; text in Churr
// Markdown; files encrypted and decrypted in the browser with progress;
// reactions, edits, deletes, checklist ticks and replies, each a sealed
// message of its own; the notices about the two accounts' devices; the
// comparison code; and a composer. There is no search, no cards, no forms
// and no link previews. The conversation's header is the ordinary one.

import { get, del } from '/shared/api.js';
import { h, icon, iconButton, button, fill, toast, busy, openMenu, openDialog, confirmDialog, copyText, avatar, clock, dateTime, dayName, sameDay, bytes, plural } from '/shared/dom.js';
import { renderMarkdown, plainText } from '/shared/md/render.js';
import { parse } from '/shared/md/parse.js';
import { taskItems } from '/shared/md/entities.js';
import { renderReactions } from '/shared/message/reactions.js';
import { qrSvg } from '/shared/qr.js';
import { messageContent, editContent, deleteContent, reactionContent, taskContent, platformLabel, NOTICE_DEVICE_ADDED, NOTICE_DEVICE_REMOVED, NOTICE_KEY_CHANGE, NOTICE_RESET, NOTICE_LIST_REFUSED, NOTICE_OUT_OF_STEP } from '/shared/sealed/index.js';
import { store, account, nameOf, markRead } from '../store.js';
import { pickEmoji } from '../env.js';
import { session, otherOf } from './state.js';
import { setupPanel, loadStyles, BROWSER } from './setup.js';
import { sealFile, openFile, imageType, saveBlob } from './files.js';

const FIVE_MINUTES = 5 * 60 * 1000;

// taskEntities gives a checklist's items their ids, as the sender does
// (section 8.3): t1, t2, … in order.
export function taskEntities(text) {
  return taskItems(parse(text), text).map((it, i) => ({ type: 'task', at: it.at, len: it.len, item_id: 't' + (i + 1) }));
}

export class SealedChat {
  constructor(cv, conv) {
    loadStyles();
    this.cv = cv;
    this.id = conv.id;
    this.other = otherOf(conv);
    this.list = cv.list;
    this.foot = cv.foot;
    this.files = [];
    this.editing = null;
    this.replyTo = null;
    this.seenNotices = null;
    this.urls = [];
    this.thumbs = new Map();
    this.off = [];
    this.mount();
  }

  async mount() {
    fill(this.list, h('p', { class: 'loading' }, 'Loading'));
    try {
      this.s = await session();
      // A conversation known only from an event may lack its members.
      if (!this.other) this.other = otherOf((await get('/conversations/' + this.id)).conversation);
    } catch (err) { fill(this.list, h('p', { class: 'problem' }, err.message)); return; }
    this.off.push(this.s.on((w) => {
      if (w === 'state') this.draw();
      else if (w && w.chat === this.id) this.drawMessages();
    }));
    this.off.push(store.on('accounts', () => this.drawMessages()));
    this.draw();
  }

  destroy() {
    for (const f of this.off) f();
    for (const u of this.urls) URL.revokeObjectURL(u);
  }

  chat() { return this.s && this.s.device ? this.s.device.chats.get(this.id) : null; }

  async draw() {
    if (!this.s) return;
    if (this.s.state !== 'unlocked') {
      this.composer = null;
      fill(this.list, h('div', { class: 'conv-start sealed' }, this.intro(), setupPanel(this.s, () => this.draw())));
      fill(this.foot, h('div', { class: 'composer-blocked' }, 'Unlock this browser to read and write in this sealed chat.'));
      return;
    }
    if (!this.composer) this.buildComposer();
    this.drawMessages();
    try { await this.s.sync(this.id, this.other); } catch (err) { toast(err.message, true); }
  }

  intro() {
    return [
      h('h2', {}, icon('sealed'), ' A sealed chat'),
      h('p', {}, `Messages here are encrypted on your devices and ${nameOf(this.other)}’s, and this server cannot read them. There is no search, and no cards, forms or link previews.`),
    ];
  }

  // --- the messages ---

  notices() {
    const d = this.s.device;
    return d.notices.filter((n) => (n.kind === NOTICE_OUT_OF_STEP ? n.conversation === this.id : n.account === this.other || n.account === store.me.id));
  }

  noticeText(n) {
    const mine = n.account === store.me.id;
    const who = mine ? 'your account' : nameOf(n.account);
    switch (n.kind) {
      case NOTICE_DEVICE_ADDED: return mine ? `A device was added to your account: ${platformLabel(n.platform)}.` : `${who} added a device: ${platformLabel(n.platform)}.`;
      case NOTICE_DEVICE_REMOVED: return mine ? 'A device was removed from your account.' : `${who} removed a device.`;
      case NOTICE_KEY_CHANGE: return mine ? 'Your security key changed. Your old key signed the change.' : `${who}’s security key changed. Their old key signed the change.`;
      case NOTICE_RESET: return mine ? 'Your sealed-chat identity was reset.' : `${who}’s sealed-chat identity was reset. Messages from now on go to new devices. If you did not expect this, compare codes before you send anything private.`;
      case NOTICE_LIST_REFUSED: return 'The server sent a device list that does not check out.';
      case NOTICE_OUT_OF_STEP: return 'This chat’s keys are out of step. A new key epoch is being made.';
      default: return 'Something about the devices in this chat changed.';
    }
  }

  start(c) {
    const pin = this.s.device.pins.get(this.other);
    const verified = !!(pin && pin.verified);
    const held = !!(pin && pin.hold);
    const notices = this.notices();
    const devices = pin && pin.newestList ? pin.newestList.list.devices : [];
    return h('div', { class: 'conv-start sealed' }, this.intro(),
      h('p', { class: 'row' },
        verified ? h('span', { class: 'tag green' }, 'codes compared') : h('span', { class: 'tag line' }, 'codes not compared'),
        button('Compare codes', () => compareDialog(this.s, this.other), 'small')),
      devices.length ? h('p', { class: 'dim small' }, `${nameOf(this.other)}’s devices: ${devices.map((d) => platformLabel(d.platform)).join(', ')}.`) : null,
      held ? h('p', { class: 'note warn' }, `Sending is held until you have read the notice about ${nameOf(this.other)}’s reset. `, button('I have read it', () => this.s.acknowledge(this.other), 'small')) : null,
      notices.length ? h('ul', { class: 'sealed-notices', 'aria-label': 'Changes to devices' }, notices.map((n) => h('li', {}, icon(n.kind === NOTICE_RESET || n.kind === NOTICE_LIST_REFUSED || n.kind === NOTICE_OUT_OF_STEP ? 'warn' : 'sealed', null, 'small'), ' ', this.noticeText(n)))) : null,
      c && c.unchecked ? h('p', { class: 'problem' }, plural(c.unchecked, 'message') + ' could not be checked, and ' + (c.unchecked === 1 ? 'is' : 'are') + ' not shown.') : null,
      h('p', { class: 'dim small' }, BROWSER));
  }

  drawMessages() {
    if (!this.s || this.s.state !== 'unlocked') return;
    const c = this.chat();
    const list = c ? c.view().filter((m) => !m.deleted) : [];
    const sc = this.cv.scroller;
    const atBottom = sc.scrollHeight - sc.scrollTop - sc.clientHeight < 60;
    const rows = [this.start(c)];
    let prev = null;
    for (const m of list) {
      const at = m.at || new Date().toISOString();
      if (!prev || !sameDay(prev.at, at)) { rows.push(h('div', { class: 'day', role: 'separator', 'aria-label': dayName(at) }, h('span', {}, dayName(at)))); prev = null; }
      if (m.gap) rows.push(h('p', { class: 'sealed-gap', role: 'note' }, 'A message may be missing here.'));
      const grouped = !!(prev && prev.author === m.author && !m.parentId && !prev.parent && new Date(at) - new Date(prev.at) < FIVE_MINUTES && !m.gap);
      rows.push(this.message(m, grouped, c));
      prev = { author: m.author, at, parent: m.parentId };
    }
    fill(this.list, rows);
    this.drawNotices();
    if (atBottom || this.first !== false) { sc.scrollTop = sc.scrollHeight; this.first = false; }
    // The read position moves when the newest message is on screen.
    if (document.visibilityState === 'visible') markRead(this.id);
  }

  // drawNotices shows, above the composer, the notices that came while
  // this chat was open.
  drawNotices() {
    if (!this.noticeBox) return;
    const all = this.notices();
    if (this.seenNotices == null) this.seenNotices = all.length;
    const fresh = all.slice(this.seenNotices);
    fill(this.noticeBox, fresh.map((n) => h('p', {}, this.noticeText(n))), fresh.length ? button('Dismiss', () => { this.seenNotices = all.length; this.drawNotices(); }, 'small') : null);
    this.noticeBox.hidden = !fresh.length;
  }

  quoted(ref) {
    const c = this.chat();
    return c ? c.messages.get(ref) : null;
  }

  message(m, grouped, c) {
    const me = store.me.id;
    const author = account(m.author) || { id: m.author, display_name: '…' };
    const el = h('article', { class: 'msg sealed-msg' + (grouped ? ' grouped' : ''), dataset: { ref: m.reference }, tabIndex: 0, 'aria-label': `${nameOf(m.author)}: ${m.text.slice(0, 200)}` });
    const marks = [
      m.edited ? h('span', { class: 'msg-mark' }, 'edited') : null,
      m.outOfOrder ? h('span', { class: 'tag yellow', title: 'The server delivered this out of order' }, 'out of order') : null,
    ].filter(Boolean);
    const main = h('div', { class: 'msg-main' });
    if (!grouped) main.append(h('header', { class: 'msg-head' }, h('span', { class: 'msg-author' }, author.display_name || author.username), m.at ? h('time', { class: 'msg-time', dateTime: m.at, title: dateTime(m.at) }, clock(m.at)) : null, marks));
    else if (marks.length) main.append(h('div', { class: 'msg-marks' }, marks));
    if (m.parentId) {
      const p = this.quoted(m.parentId);
      main.append(h('div', { class: 'msg-replyto' }, icon('reply', null, 'small'), p ? h('button', { type: 'button', class: 'link-btn', onclick: () => this.jump(p.reference) }, `${nameOf(p.author)}: ${p.deleted ? 'deleted message' : p.text.slice(0, 60)}`) : h('span', { class: 'dim' }, 'a reply')));
    }
    if (m.quote && m.quote.message_id) {
      const q = this.quoted(String(m.quote.message_id).toLowerCase());
      main.append(h('blockquote', { class: 'msg-quote' }, h('div', { class: 'quote-text' }, !q || q.deleted ? h('span', { class: 'dim' }, 'original message changed') : q.text.slice(0, 240))));
    }
    if (m.text) {
      main.append(renderMarkdown(m.text, m.entities, {
        meId: me, account, standardEmoji: (n) => store.standard.byCode.get(n),
        tasks: [...m.tasks].map(([id, checked]) => ({ id, checked, checked_by_id: checked ? m.taskBy.get(id) : null })),
        canTick: true,
        onTask: (itemId, checked, box) => this.send(taskContent(m.reference, itemId, checked)).catch((err) => { box.checked = !checked; toast(err.message, true); }),
      }));
    }
    if (m.files.length) main.append(this.drawFiles(m.files));
    const reactions = renderReactions([...m.reactions].map(([emoji, set]) => ({ emoji, count: set.size, account_ids: [...set], me: set.has(me) })), {
      names: (ids) => ids.map(nameOf).join(', '), canReact: true,
      onToggle: (emoji, on) => this.send(reactionContent(m.reference, emoji, on)).catch((err) => toast(err.message, true)),
      onAdd: (anchor) => pickEmoji(anchor, (v) => this.send(reactionContent(m.reference, v, true)).catch((err) => toast(err.message, true))),
    });
    if (reactions) main.append(reactions);
    el.append(h('div', { class: 'msg-gutter' }, grouped && m.at ? h('time', { class: 'msg-time-side', dateTime: m.at }, clock(m.at)) : grouped ? null : avatar(author)), main);
    el.append(h('div', { class: 'msg-actions', role: 'toolbar', 'aria-label': 'Message actions' },
      iconButton('emoji', 'React', (e) => pickEmoji(e.currentTarget, (v) => this.send(reactionContent(m.reference, v, true)).catch((err) => toast(err.message, true))), 'small'),
      iconButton('reply', 'Reply', () => this.startReply(m), 'small'),
      iconButton('more', 'More actions', (e) => this.more(m, e.currentTarget), 'small')));
    return el;
  }

  jump(ref) {
    const el = this.list.querySelector(`[data-ref="${ref}"]`);
    if (el) { el.scrollIntoView({ block: 'center' }); el.focus(); }
  }

  more(m, anchor) {
    const mine = m.author === store.me.id;
    openMenu(anchor, [
      { label: 'Reply', icon: 'reply', onclick: () => this.startReply(m) },
      m.text && { label: 'Copy text', icon: 'copy', onclick: () => copyText(plainText(m.text, m.entities, {}), 'Text copied') },
      mine && !m.files.length && { label: 'Edit', icon: 'edit', onclick: () => this.startEdit(m) },
      mine && m.files.length && { label: 'Edit text', icon: 'edit', onclick: () => this.startEdit(m) },
      mine && { sep: true },
      mine && { label: 'Delete', icon: 'trash', danger: true, onclick: () => this.remove(m) },
    ], { alignRight: true, label: 'Message actions' });
  }

  async remove(m) {
    if (!(await confirmDialog('Delete this message?', 'It is removed for both of you. The server erases its encrypted copy.', { yes: 'Delete', danger: true }))) return;
    try {
      await this.send(deleteContent(m.reference));
      // The author asks the server to erase the row and its files too.
      if (m.rowId) await del('/messages/' + m.rowId).catch(() => {});
    } catch (err) { toast(err.message, true); }
  }

  // --- files ---

  drawFiles(files) {
    return h('div', { class: 'files' }, files.map((f) => this.drawFile(f)));
  }

  drawFile(f) {
    const status = h('small', { class: 'dim' }, [bytes(f.size || 0), f.caption].filter(Boolean).join(' · '));
    const bar = h('span', { class: 'bar' });
    const progress = h('span', { class: 'progress', role: 'progressbar', 'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-label': 'Decrypting ' + (f.name || 'file'), hidden: true }, bar);
    const setProgress = (p) => { progress.hidden = false; bar.style.width = Math.round(p * 100) + '%'; progress.setAttribute('aria-valuenow', String(Math.round(p * 100))); };
    const save = button('Download', () => busy(save, async () => {
      try {
        const blob = await openFile(f.id, f.key, f.size, setProgress);
        saveBlob(blob, f.name);
        status.textContent = `${bytes(f.size || 0)} · decrypted and checked`;
      } catch (err) { status.textContent = err.message; throw err; } finally { progress.hidden = true; }
    }), 'small');
    save.prepend(icon('download', null, 'small'));
    const card = h('div', { class: 'file-card sealed-file', dataset: { file: f.id } }, icon('file'), h('div', { class: 'what' }, h('strong', {}, f.name || 'file'), status, progress), save);
    // An image is shown from its encrypted thumbnail, once its content
    // says it is one.
    if (f.thumb && f.thumb.id && /^image\//.test(f.media_type || '') && !/svg/i.test(f.media_type || '')) {
      const holder = h('figure', { class: 'file-image sealed-image' }, card);
      this.thumb(f).then((url) => {
        if (!url) return;
        const img = h('img', { src: url, alt: f.caption || f.name || 'image' });
        if (f.thumb.width && f.thumb.height) { img.width = f.thumb.width; img.height = f.thumb.height; }
        holder.prepend(h('button', { type: 'button', class: 'image-open', 'aria-label': 'Open ' + (f.name || 'the image'), onclick: () => this.viewImage(f) }, img));
      }).catch(() => {});
      return holder;
    }
    return card;
  }

  async thumb(f) {
    if (this.thumbs.has(f.thumb.id)) return this.thumbs.get(f.thumb.id);
    const p = (async () => {
      const blob = await openFile(f.thumb.id, f.thumb.key, null);
      const type = await imageType(blob);
      if (!type) return null;
      const url = URL.createObjectURL(new Blob([blob], { type }));
      this.urls.push(url);
      return url;
    })();
    this.thumbs.set(f.thumb.id, p);
    return p;
  }

  viewImage(f) {
    openDialog(f.name || 'Image', (close, dlg) => {
      dlg.classList.add('viewer');
      const body = h('div', { class: 'viewer-image' }, h('p', { class: 'loading' }, 'Decrypting'));
      openFile(f.id, f.key, f.size).then(async (blob) => {
        const type = await imageType(blob);
        if (!type) { fill(body, h('p', { class: 'dim' }, 'This file is not an image this browser shows. Download it instead.')); return; }
        const url = URL.createObjectURL(new Blob([blob], { type }));
        this.urls.push(url);
        fill(body, h('img', { src: url, alt: f.caption || f.name || '' }));
      }).catch((err) => fill(body, h('p', { class: 'problem' }, err.message)));
      return { body: [body, h('p', { class: 'dim small' }, `${f.name || ''} · ${bytes(f.size || 0)}`)], foot: [button('Close', () => close())] };
    }, { wide: true });
  }

  // --- the composer ---

  buildComposer() {
    this.input = h('textarea', { class: 'sealed-input', rows: 1, 'aria-label': 'Message ' + nameOf(this.other) + ' (sealed)', placeholder: 'A sealed message' });
    this.input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); this.submit(); }
      if (e.key === 'Escape') { this.cancel(); }
    });
    this.input.addEventListener('input', () => { this.input.style.height = 'auto'; this.input.style.height = Math.min(this.input.scrollHeight, 240) + 'px'; });
    this.fileInput = h('input', { type: 'file', multiple: true, hidden: true, onchange: () => { this.addFiles(this.fileInput.files); this.fileInput.value = ''; } });
    this.banner = h('div', { class: 'composer-banner', hidden: true });
    this.tray = h('div', { class: 'composer-files', hidden: true });
    this.noticeBox = h('div', { class: 'composer-notice', hidden: true, role: 'status' });
    this.sendBtn = h('button', { type: 'button', class: 'btn primary send', onclick: () => this.submit() }, icon('send'), h('span', {}, 'Send'));
    this.composer = h('div', { class: 'composer sealed-composer' }, this.noticeBox, this.banner, this.tray,
      h('div', { class: 'composer-row' }, iconButton('attach', 'Attach files (encrypted in this browser)', () => this.fileInput.click()), this.input, this.sendBtn), this.fileInput);
    fill(this.foot, this.composer);
    this.cv.composer = { addFiles: (files) => this.addFiles(files), focus: () => this.input.focus(), destroy() {}, startEdit() {}, setLocked() {} };
  }

  addFiles(list) {
    for (const file of list) if (this.files.length < 10) this.files.push({ file, progress: null, entry: null, error: null });
    this.drawTray();
  }

  drawTray() {
    fill(this.tray, this.files.map((f, i) => {
      const bar = h('span', { class: 'bar' });
      if (f.progress != null) bar.style.width = Math.round(f.progress * 100) + '%';
      return h('div', { class: 'attach' + (f.error ? ' failed' : '') }, h('span', { class: 'attach-thumb' }, icon('file')),
        h('div', { class: 'what' }, h('strong', { class: 'clip' }, f.file.name),
          h('small', { class: 'dim' }, f.error || (f.progress != null ? `${f.word || 'Encrypting'} ${Math.round(f.progress * 100)}%` : bytes(f.file.size) + ' · encrypted before upload')),
          h('span', { class: 'progress', role: 'progressbar', 'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-valuenow': String(Math.round((f.progress || 0) * 100)), 'aria-label': 'Upload of ' + f.file.name, hidden: f.progress == null }, bar)),
        iconButton('close', 'Remove ' + f.file.name, () => { this.files.splice(i, 1); this.drawTray(); }, 'small'));
    }));
    this.tray.hidden = !this.files.length;
  }

  startReply(m) {
    this.editing = null;
    this.replyTo = m;
    fill(this.banner, icon('reply', null, 'small'), h('span', { class: 'grow clip' }, h('strong', {}, 'Replying to ' + nameOf(m.author) + ': '), m.text.slice(0, 80)), iconButton('close', 'Cancel the reply', () => this.cancel(), 'small'));
    this.banner.hidden = false;
    this.input.focus();
  }

  startEdit(m) {
    this.replyTo = null;
    this.editing = m;
    this.input.value = m.text;
    fill(this.banner, icon('edit', null, 'small'), h('span', { class: 'grow' }, 'Editing your message. Escape cancels.'), iconButton('close', 'Cancel editing', () => this.cancel(), 'small'));
    this.banner.hidden = false;
    this.input.focus();
  }

  cancel() {
    if (this.editing) this.input.value = '';
    this.editing = null;
    this.replyTo = null;
    this.banner.hidden = true;
  }

  async send(content, fileIds) {
    await this.s.send(this.id, this.other, content, fileIds);
    this.drawMessages();
  }

  async submit() {
    if (this.sending) return;
    const text = this.input.value.replace(/\s+$/, '');
    if (!text && !this.files.length) return;
    this.sending = true;
    this.sendBtn.disabled = true;
    try {
      if (this.editing) {
        await this.send(editContent(this.editing.reference, text, taskEntities(text)));
      } else {
        const entries = [];
        for (const f of this.files) {
          if (!f.entry) {
            f.entry = await sealFile(this.id, f.file, (p, word) => { f.progress = p; f.word = word; this.drawTray(); }).catch((err) => { f.error = err.message; this.drawTray(); throw err; });
          }
          entries.push(f.entry);
        }
        const parent = this.replyTo ? (this.replyTo.parentId || this.replyTo.reference) : null;
        const quote = this.replyTo && this.replyTo.parentId ? { message_id: this.replyTo.reference } : null;
        await this.send(messageContent(text, { entities: taskEntities(text), files: entries, parent, quote }), entries.flatMap((e) => [e.id, e.thumb && e.thumb.id].filter(Boolean)));
        this.files = [];
        this.drawTray();
      }
      this.input.value = '';
      this.input.style.height = '';
      this.cancel();
    } catch (err) {
      toast(err.message, true);
    } finally {
      this.sending = false;
      this.sendBtn.disabled = false;
    }
  }
}

// compareDialog shows the 60 digits for this chat (section 15), the same
// on both people's devices if and only if each has pinned the other's true
// account key.
export function compareDialog(s, other) {
  loadStyles();
  return openDialog('Compare codes', (close) => {
    const body = h('div', { class: 'stack' }, h('p', { class: 'loading' }, 'Working out the code'));
    s.compareCode(other).then((code) => {
      const pin = s.device.pins.get(other);
      const mark = button(pin && pin.verified ? 'Marked as compared' : 'The codes match', () => busy(mark, async () => { await s.markVerified(other); toast('Marked as compared on this browser.'); close(true); }), 'primary');
      mark.disabled = !!(pin && pin.verified);
      fill(body,
        h('p', {}, `Compare this code with ${nameOf(other)}, in person or on a call you trust. Their device shows the same 60 digits only if nobody stands between you.`),
        h('p', { class: 'sealed-digits', 'aria-label': 'Comparison code' }, code.split(' ').map((g) => h('span', {}, g))),
        h('div', { class: 'qr' }, qrSvg(code.replace(/ /g, ''), 'The code as a QR code')),
        h('p', { class: 'dim small' }, 'The mark is kept on this browser only. It stays when their security key changes, and is removed if their sealed-chat identity is reset.'),
        h('p', { class: 'row' }, mark));
    }).catch((err) => fill(body, h('p', { class: 'problem' }, err.message)));
    return { body, foot: [button('Close', () => close())] };
  });
}

