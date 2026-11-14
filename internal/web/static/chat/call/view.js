// The call on screen (docs/CLIENTS.md section 9.14): the call view over
// the conversation pane, with a grid of tiles sized to who is shown, the
// loudest emphasised, the reader's own preview, a shared screen in the
// main area; the controls; and the compact bar that keeps the call going
// while the reader is elsewhere in the chat.
//
// Colour never works alone here: speaking is an outline and three bars,
// muted a struck microphone and the word, a paused video the words.

import { h, fill, icon, iconButton, button, avatar, openDialog, toast } from '/shared/dom.js';
import { store, nameOf, account, conversation, title } from '../store.js';
import { ui } from '../ui.js';
import { arrange, subscriptions, NARROW } from './layout.js';
import { people as peopleOf, sharer } from './signal.js';
import * as media from './media.js';

const fmtTime = (s) => {
  s = Math.max(0, Math.floor(s));
  const hh = Math.floor(s / 3600), mm = Math.floor((s % 3600) / 60), ss = s % 60;
  return (hh ? hh + ':' + String(mm).padStart(2, '0') : String(mm)) + ':' + String(ss).padStart(2, '0');
};

export class CallView {
  constructor(call, { onClose } = {}) {
    this.call = call;
    this.onClose = onClose;
    this.mode = 'full';
    this.layout = 'grid'; // or 'speaker'
    this.pinned = null;
    this.mutedForMe = new Set();
    this.videos = new Map(); // track id or 'self' → <video>
    this.audios = new Map(); // track id → <audio>
    this.focus = null;       // the loudest, kept until someone else speaks
    this.joinedNames = new Set();
    call.nameOf = (id) => nameOf(id);

    this.head = h('header', { class: 'call-head' });
    this.stage = h('div', { class: 'call-stage', role: 'group', 'aria-label': 'People in the call' });
    this.notesEl = h('div', { class: 'call-notes', role: 'status', 'aria-live': 'polite' });
    this.controls = h('div', { class: 'call-controls', role: 'toolbar', 'aria-label': 'Call controls' });
    this.full = h('div', { class: 'call-full' }, this.head, h('div', { class: 'call-middle' }, this.stage, this.notesEl), this.controls);
    this.bar = h('div', { class: 'call-bar', role: 'region', 'aria-label': 'Call' });
    this.sound = h('div', { class: 'call-sound', hidden: true });
    this.el = h('section', { id: 'call', class: 'call', 'aria-label': 'Call' }, this.full, this.bar, this.sound);

    this.off = [
      call.on(() => this.schedule()),
      store.on('accounts', () => this.schedule()),
      store.on('route', () => this.routed()),
    ];
    this.onResize = () => this.schedule();
    this.ro = new ResizeObserver(() => this.schedule());
    this.ro.observe(this.stage);
    window.addEventListener('resize', this.onResize);
    this.onVisible = () => this.schedule();
    document.addEventListener('visibilitychange', this.onVisible);
    this.timer = setInterval(() => this.tick(), 1000);
    document.getElementById('app').append(this.el);
    this.setMode('full');
  }

  destroy() {
    for (const off of this.off) off();
    this.ro.disconnect();
    window.removeEventListener('resize', this.onResize);
    document.removeEventListener('visibilitychange', this.onVisible);
    clearInterval(this.timer);
    cancelAnimationFrame(this.frame);
    for (const a of this.audios.values()) { a.srcObject = null; }
    document.body.classList.remove('call-bar-on', 'call-full-on');
    this.el.remove();
  }

  setMode(mode) {
    this.mode = mode;
    this.el.dataset.mode = mode;
    document.body.classList.toggle('call-bar-on', mode === 'bar');
    document.body.classList.toggle('call-full-on', mode === 'full');
    this.schedule();
    if (mode === 'full') requestAnimationFrame(() => { const b = this.controls.querySelector('button'); if (b && !this.el.contains(document.activeElement)) b.focus({ preventScroll: true }); });
  }

  // Moving to another conversation, or another screen, leaves the call
  // going in the compact bar.
  routed() {
    if (this.mode === 'full' && store.current !== this.call.conversationId) this.setMode('bar');
  }

  schedule() {
    if (this.frame) return;
    this.frame = requestAnimationFrame(() => { this.frame = null; this.draw(); });
  }

  showing() {
    return this.mode === 'full' && document.visibilityState === 'visible';
  }

  name(id) { return id === store.me.id ? 'You' : nameOf(id); }

  draw() {
    const c = this.call;
    if (c.over) return;
    const conv = conversation(c.conversationId);
    this.el.setAttribute('aria-label', 'Call in ' + (conv ? title(conv) : 'a conversation'));
    this.announceChanges();
    this.syncAudio();
    if (this.mode === 'full') { this.drawHead(conv); this.drawStage(); this.drawNotes(); this.drawControls(); } else this.call.want([]);
    this.drawBar(conv);
  }

  // Joins and leaves are read out.
  announceChanges() {
    const now = new Set(Object.keys(this.call.state.participants).filter((id) => id !== store.me.id));
    if (this.seenPeople) {
      for (const id of now) if (!this.seenPeople.has(id)) ui.announce(`${nameOf(id)} joined the call`);
      for (const id of this.seenPeople) if (!now.has(id)) ui.announce(`${nameOf(id)} left the call`);
    }
    this.seenPeople = now;
  }

  // --- what is heard ---

  // Every other participant's microphone plays, whatever is on screen. The
  // elements live outside the tiles so the sound goes on in the bar.
  syncAudio() {
    const want = new Map();
    for (const p of peopleOf(this.call.state)) if (p.microphone) want.set(p.microphone, p.account_id);
    for (const [id, el] of this.audios) if (!want.has(id)) { el.srcObject = null; el.remove(); this.audios.delete(id); }
    for (const [id, who] of want) {
      const track = this.call.remoteTrack(id);
      let el = this.audios.get(id);
      if (!el) { el = h('audio', { autoplay: true }); this.audios.set(id, el); this.sound.append(el); }
      if (track && (!el.srcObject || el.srcObject.getTracks()[0] !== track)) { el.srcObject = new MediaStream([track]); el.play().catch(() => {}); }
      el.muted = this.mutedForMe.has(who);
      el.dataset.account = who;
      if (media.prefs.speaker && el.setSinkId && el.sinkId !== media.prefs.speaker) el.setSinkId(media.prefs.speaker).catch(() => {});
    }
  }

  // --- the full view ---

  drawHead(conv) {
    const c = this.call;
    const n = Object.keys(c.state.participants).length;
    const tags = [];
    if (c.held) tags.push(h('span', { class: 'tag yellow' }, 'reconnecting'));
    if (c.transport === 'tcp') tags.push(h('span', { class: 'tag pink', title: 'The media goes over TCP because UDP is blocked. It stalls when packets are lost.' }, 'poor connection'));
    if (c.phase === 'joining') tags.push(h('span', { class: 'tag line' }, 'joining'));
    fill(this.head,
      h('div', { class: 'call-title' },
        h('h2', {}, icon('call'), h('bdi', {}, conv ? title(conv) : 'Call')),
        h('p', { class: 'call-meta' }, h('span', { class: 'call-n' }, n === 1 ? '1 person' : `${n} people`), ' · ', h('span', { class: 'call-time' }, this.elapsed()), tags)),
      iconButton(this.layout === 'grid' ? 'spotlight' : 'grid', this.layout === 'grid' ? 'Speaker layout' : 'Grid layout', () => { this.layout = this.layout === 'grid' ? 'speaker' : 'grid'; this.pinned = null; this.schedule(); }),
      iconButton('shrink', 'Make the call small and go on reading', () => this.setMode('bar')));
  }

  elapsed() {
    const at = this.call.state.startedAt;
    return at ? fmtTime((Date.now() - new Date(at).getTime()) / 1000) : '0:00';
  }

  tick() {
    for (const el of this.el.querySelectorAll('.call-time')) el.textContent = this.elapsed();
  }

  drawStage() {
    const c = this.call;
    const s = c.state;
    const ppl = peopleOf(s);
    const rect = this.stage.getBoundingClientRect();
    const width = Math.floor(rect.width), height = Math.floor(rect.height);
    if (!width || !height) return;
    // The speaker layout puts the loudest in the main tile, and keeps them
    // there through silence until someone else speaks.
    const loud = s.speakers.find((id) => id !== store.me.id && s.participants[id]);
    if (loud) this.focus = loud;
    if (this.focus && !s.participants[this.focus]) this.focus = null;
    const focus = this.pinned && s.participants[this.pinned] ? this.pinned : this.layout === 'speaker' ? (this.focus || (ppl[0] && ppl[0].account_id)) : null;
    const lay = arrange({ people: ppl, speakers: s.speakers, pinned: focus, width, height });
    this.el.dataset.layout = lay.mode;
    const byId = new Map(ppl.map((p) => [p.account_id, p]));
    const keep = new Set();
    for (const t of lay.tiles) {
      keep.add(t.key);
      const p = byId.get(t.account_id);
      this.placeTile(this.tile(t, p), t);
    }
    // The reader's own picture.
    const self = this.selfTile();
    keep.add('self');
    this.placeTile(self, lay.preview);
    self.classList.toggle('alone', !!lay.preview.self);
    for (const el of [...this.stage.children]) if (!keep.has(el.dataset.key) && !el.classList.contains('call-overflow')) el.remove();
    let more = this.stage.querySelector('.call-overflow');
    if (lay.overflow) {
      if (!more) { more = h('div', { class: 'call-overflow' }); this.stage.append(more); }
      more.textContent = `and ${lay.overflow} more`;
    } else if (more) more.remove();
    const list = subscriptions(lay.tiles, ppl, { dpr: window.devicePixelRatio || 1, showing: this.showing() });
    c.want(list);
  }

  placeTile(el, box) {
    Object.assign(el.style, { left: box.x + 'px', top: box.y + 'px', width: box.w + 'px', height: box.h + 'px' });
    if (!el.parentNode) this.stage.append(el);
    // A video taken out of the page is paused by the browser, and putting
    // it back does not start it again.
    const v = el.querySelector('video');
    if (v && v.paused && v.srcObject) v.play().catch(() => {});
  }

  videoFor(key, track, mirror) {
    let v = this.videos.get(key);
    if (!v) { v = h('video', { autoplay: true, playsInline: true, muted: true, class: mirror ? 'mirror' : '' }); this.videos.set(key, v); }
    if (track && (!v.srcObject || v.srcObject.getTracks()[0] !== track)) { v.srcObject = new MediaStream([track]); v.play().catch(() => {}); }
    if (!track && v.srcObject) v.srcObject = null;
    return v;
  }

  tile(t, p) {
    let el = this.stage.querySelector(`.call-tile[data-key="${CSS.escape(t.key)}"]`);
    if (!el) {
      el = h('div', { class: 'call-tile', dataset: { key: t.key, account: t.account_id }, tabIndex: 0 });
      el.addEventListener('dblclick', () => this.togglePin(t.account_id));
    }
    const s = this.call.state;
    const id = t.source === 'screen' ? p.screen : p.camera;
    const layer = id ? s.layers[id] : null;
    const live = t.source === 'screen' || p.cameraOn;
    const track = live ? this.call.remoteTrack(id) : null;
    const paused = layer && layer.state === 'paused';
    const unavailable = layer && layer.state === 'unavailable';
    const showVideo = !!(live && track && !paused && !unavailable);
    const a = account(t.account_id) || { id: t.account_id };
    el.classList.toggle('speaking', p.speaking && t.source === 'camera');
    el.classList.toggle('main', !!t.main);
    el.classList.toggle('screen', t.source === 'screen');
    el.classList.toggle('has-video', showVideo);
    el.dataset.state = layer ? layer.state : (live ? 'waiting' : 'off');
    const words = [this.name(t.account_id)];
    if (t.source === 'screen') words.push('sharing a screen');
    if (p.micMuted) words.push('muted');
    if (p.speaking) words.push('speaking');
    if (this.mutedForMe.has(t.account_id)) words.push('muted for you');
    el.setAttribute('aria-label', words.join(', '));
    const status = paused ? 'video paused: not enough bandwidth' : unavailable ? 'video unavailable' : !live ? null : !track ? 'video starting' : null;
    fill(el,
      showVideo ? this.videoFor(id, track, false) : h('div', { class: 'call-face' }, avatar(a, 'large')),
      status ? h('p', { class: 'call-status' }, status) : null,
      h('div', { class: 'call-label' },
        p.speaking && t.source === 'camera' ? icon('speaking', null, 'small speak-mark') : null,
        t.source === 'camera' && p.micMuted ? h('span', { class: 'call-muted' }, icon('mic-off', null, 'small'), h('span', { class: 'sr-only' }, 'muted')) : null,
        h('bdi', {}, t.source === 'screen' ? `${this.name(t.account_id)}’s screen` : this.name(t.account_id)),
        this.mutedForMe.has(t.account_id) ? h('span', { class: 'tag line' }, 'muted for you') : null),
      h('div', { class: 'call-tile-tools' },
        iconButton('pin', this.pinned === t.account_id ? 'Unpin' : 'Pin to the main tile', () => this.togglePin(t.account_id), 'small'),
        p.microphone ? iconButton(this.mutedForMe.has(t.account_id) ? 'mic' : 'mic-off', this.mutedForMe.has(t.account_id) ? `Hear ${nameOf(t.account_id)} again` : `Mute ${nameOf(t.account_id)} for yourself`, () => this.toggleMuteForMe(t.account_id), 'small') : null));
    return el;
  }

  selfTile() {
    let el = this.stage.querySelector('.call-tile[data-key="self"]');
    if (!el) el = h('div', { class: 'call-tile self', dataset: { key: 'self' } });
    const c = this.call;
    const cam = !c.muted.camera && c.local.camera;
    const speaking = c.state.speakers.includes(store.me.id) && !c.muted.microphone;
    el.classList.toggle('speaking', speaking);
    el.classList.toggle('has-video', !!cam);
    const micOff = c.muted.microphone || !c.local.mic;
    el.setAttribute('aria-label', ['Your own picture', micOff ? 'muted' : null, c.local.screen ? 'sharing your screen' : null].filter(Boolean).join(', '));
    fill(el,
      cam ? this.videoFor('self', c.local.camera, true) : h('div', { class: 'call-face' }, avatar(account(store.me.id) || store.me, 'large')),
      c.local.screen ? h('p', { class: 'call-status' }, 'You are sharing your screen') : null,
      h('div', { class: 'call-label' },
        speaking ? icon('speaking', null, 'small speak-mark') : null,
        micOff ? h('span', { class: 'call-muted' }, icon('mic-off', null, 'small'), h('span', { class: 'sr-only' }, 'muted')) : null,
        h('bdi', {}, 'You')));
    return el;
  }

  togglePin(id) {
    this.pinned = this.pinned === id ? null : id;
    this.schedule();
  }

  toggleMuteForMe(id) {
    if (this.mutedForMe.has(id)) this.mutedForMe.delete(id); else this.mutedForMe.add(id);
    this.schedule();
  }

  drawNotes() {
    const c = this.call;
    const notes = [...c.notes];
    if (media.iphone() && c.phase === 'live') notes.push({ key: 'iphone', kind: 'warn', text: 'On an iPhone the call’s sound may stop when this tab goes to the background or the phone locks.' });
    const sig = notes.map((n) => n.key + n.text).join('|');
    if (sig === this.notesSig) return;
    this.notesSig = sig;
    fill(this.notesEl, notes.map((n) => h('p', { class: 'note ' + (n.kind === 'danger' ? 'danger' : 'warn') },
      n.kind === 'danger' ? h('strong', {}, 'Problem: ') : null, n.text,
      n.key !== 'iphone' ? iconButton('close', 'Dismiss', () => c.dropNote(n.key), 'small') : null)));
  }

  controlButtons(compact) {
    const c = this.call;
    const micOff = c.muted.microphone || !c.local.mic;
    const camOff = c.muted.camera;
    const ctl = (name, label, word, pressed, onclick, cls) => {
      const b = h('button', { type: 'button', class: 'call-ctl' + (cls ? ' ' + cls : ''), 'aria-label': label, title: label, onclick }, icon(name), compact ? null : h('span', {}, word));
      if (pressed != null) b.setAttribute('aria-pressed', pressed ? 'true' : 'false');
      return b;
    };
    const out = [
      ctl(micOff ? 'mic-off' : 'mic', micOff ? 'Unmute microphone' : 'Mute microphone', micOff ? 'Muted' : 'Mic on', micOff, () => c.toggleMic(), micOff ? 'off' : ''),
      ctl(camOff ? 'camera-off' : 'call', camOff ? 'Turn camera on' : 'Turn camera off', camOff ? 'Camera off' : 'Camera on', camOff, () => c.toggleCamera(), camOff ? 'off' : ''),
    ];
    // Phone browsers cannot share a screen, so the button is not there.
    if (media.canShareScreen()) out.push(ctl('screen', c.local.screen ? 'Stop sharing your screen' : 'Share your screen', c.local.screen ? 'Stop sharing' : 'Share screen', !!c.local.screen, () => this.shareScreen(), c.local.screen ? 'on' : ''));
    return out;
  }

  async shareScreen() {
    const c = this.call;
    if (!c.local.screen) {
      const by = sharer(c.state);
      if (by && by !== store.me.id) { c.note('screen', `${nameOf(by)} is already sharing a screen. One screen at a time.`); return; }
    }
    await c.toggleScreen();
  }

  drawControls() {
    fill(this.controls,
      this.controlButtons(window.innerWidth < NARROW),
      h('button', { type: 'button', class: 'call-ctl', 'aria-label': 'Microphone, camera and speaker', title: 'Microphone, camera and speaker', onclick: () => this.devices() }, icon('settings'), window.innerWidth < NARROW ? null : h('span', {}, 'Devices')),
      h('button', { type: 'button', class: 'call-ctl leave', 'aria-label': 'Leave the call', title: 'Leave the call', onclick: () => this.call.leave() }, icon('call-leave'), h('span', {}, 'Leave')));
  }

  async devices() {
    const list = await media.devices();
    const c = this.call;
    openDialog('Microphone, camera and speaker', (close) => {
      const sel = (kind, items, current, label) => {
        const s = h('select', { onchange: () => c.setDevice(kind, s.value) },
          h('option', { value: '' }, 'The browser’s default'),
          items.map((d, i) => h('option', { value: d.deviceId, selected: d.deviceId === current }, d.label || `${label} ${i + 1}`)));
        return h('label', { class: 'field' }, h('span', {}, label), s);
      };
      const speakers = list.speakers.length && HTMLMediaElement.prototype.setSinkId;
      return {
        body: [
          sel('mic', list.mics, media.prefs.mic, 'Microphone'),
          sel('camera', list.cameras, media.prefs.camera, 'Camera'),
          speakers ? sel('speaker', list.speakers, media.prefs.speaker, 'Speaker') : h('p', { class: 'dim small' }, 'This browser plays the call on its own choice of speaker.'),
          !list.mics.some((d) => d.label) ? h('p', { class: 'dim small' }, 'Device names appear once the browser has been allowed the microphone or camera.') : null,
          media.canShareScreen() ? null : h('p', { class: 'note' }, 'Screens cannot be shared from a phone browser.'),
        ],
        foot: [button('Done', () => close(), 'primary')],
      };
    });
  }

  // --- the compact bar ---

  drawBar(conv) {
    const c = this.call;
    const s = c.state;
    const speaking = s.speakers.filter((id) => s.participants[id]).map((id) => this.name(id));
    const n = Object.keys(s.participants).length;
    fill(this.bar,
      h('button', { type: 'button', class: 'call-bar-open', onclick: () => this.open(), 'aria-label': 'Open the call' },
        icon('call'),
        h('span', { class: 'grow' },
          h('strong', {}, h('bdi', {}, conv ? title(conv) : 'Call')),
          h('small', {}, h('span', { class: 'call-time' }, this.elapsed()), ` · ${n === 1 ? '1 person' : n + ' people'}`,
            speaking.length ? [' · ', icon('speaking', null, 'small speak-mark'), ' ', speaking.join(', ')] : null,
            c.held ? ' · reconnecting' : null))),
      this.controlButtons(true).slice(0, 2),
      iconButton('expand', 'Open the call', () => this.open()),
      h('button', { type: 'button', class: 'call-ctl leave', 'aria-label': 'Leave the call', title: 'Leave the call', onclick: () => c.leave() }, icon('call-leave')));
  }

  open() {
    if (store.current !== this.call.conversationId) ui.openConversation(this.call.conversationId);
    this.setMode('full');
  }
}

// prejoin is the screen before joining: a preview of the camera, the
// microphone's level, device choice, and Join with the microphone and
// camera on or off. It resolves with { audio, video, tracks } or null.
export function prejoin(conv, starting) {
  return openDialog(starting ? 'Start a call' : 'Join the call', (close) => {
    const st = { audio: true, video: false, mic: null, camera: null, problems: [] };
    const preview = h('div', { class: 'call-preview' });
    const level = h('div', { class: 'call-level', role: 'meter', 'aria-label': 'Microphone level', 'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-valuenow': '0' }, h('i'));
    const notes = h('div', { class: 'stack' });
    let meter = null, raf = 0, done = false;
    const stopAll = () => { cancelAnimationFrame(raf); if (meter) meter.stop(); };
    const drawPreview = () => {
      fill(preview, st.camera && st.video ? h('video', { autoplay: true, playsInline: true, muted: true, class: 'mirror', srcObject: new MediaStream([st.camera]) }) : h('div', { class: 'call-face' }, avatar(account(store.me.id) || store.me, 'large'), h('p', { class: 'dim small' }, st.video ? 'Starting the camera' : 'Your camera is off')));
      const v = preview.querySelector('video'); if (v) v.play().catch(() => {});
      fill(notes, st.problems.map((p) => h('p', { class: 'note ' + (p === 'insecure' ? 'danger' : 'warn') }, media.problemText(p))),
        media.canShareScreen() ? null : h('p', { class: 'dim small' }, 'Screens cannot be shared from a phone browser.'),
        media.iphone() ? h('p', { class: 'dim small' }, 'On an iPhone the call’s sound may stop when this tab goes to the background or the phone locks.') : null,
        h('p', { class: 'dim small' }, 'The call goes on only while this page is open.'));
      micBtn.setAttribute('aria-pressed', String(st.audio && !!st.mic));
      micBtn.replaceChildren(icon(st.audio && st.mic ? 'mic' : 'mic-off'), h('span', {}, st.audio && st.mic ? 'Microphone on' : 'Microphone off'));
      camBtn.setAttribute('aria-pressed', String(st.video && !!st.camera));
      camBtn.replaceChildren(icon(st.video && st.camera ? 'call' : 'camera-off'), h('span', {}, st.video && st.camera ? 'Camera on' : 'Camera off'));
    };
    const meterLoop = () => {
      if (done) return;
      const v = meter ? meter.read() : 0;
      level.firstChild.style.width = Math.round((st.audio ? v : 0) * 100) + '%';
      level.setAttribute('aria-valuenow', String(Math.round((st.audio ? v : 0) * 100)));
      raf = requestAnimationFrame(meterLoop);
    };
    const want = async (kind) => {
      const got = await media.open({ audio: kind === 'audio', video: kind === 'video' });
      st.problems = [...st.problems.filter((p) => !got.problems.includes(p)), ...got.problems];
      if (got.mic) { st.mic = got.mic; st.problems = st.problems.filter((p) => !p.startsWith('mic') && p !== 'no-mic'); if (meter) meter.stop(); meter = media.meter(got.mic); }
      if (got.camera) { st.camera = got.camera; st.problems = st.problems.filter((p) => !p.startsWith('camera') && p !== 'no-camera'); }
      drawPreview();
    };
    const micBtn = h('button', { type: 'button', class: 'btn call-toggle', onclick: async () => { if (!st.mic) { st.audio = true; await want('audio'); } else { st.audio = !st.audio; drawPreview(); } } });
    const camBtn = h('button', { type: 'button', class: 'btn call-toggle', onclick: async () => {
      if (st.video) { st.video = false; if (st.camera) { st.camera.stop(); st.camera = null; } drawPreview(); return; }
      st.video = true; drawPreview(); await want('video'); if (!st.camera) st.video = false; drawPreview();
    } });
    const join = button(starting ? 'Start the call' : 'Join', () => {
      done = true; stopAll();
      close({ audio: st.audio && !!st.mic, video: st.video && !!st.camera, tracks: { mic: st.mic, camera: st.video ? st.camera : null, problems: st.problems } });
    }, 'primary');
    const cancel = button('Cancel', () => close(null));
    // The microphone is asked for at once, so the level shows and the
    // browser's question comes while the person is looking at this.
    if (!media.secure()) st.problems.push('insecure');
    else want('audio').then(meterLoop);
    drawPreview();
    const dlgClosed = () => { if (!done) { stopAll(); for (const t of [st.mic, st.camera]) if (t) t.stop(); } };
    setTimeout(() => { const d = join.closest('dialog'); if (d) d.addEventListener('close', dlgClosed); });
    return {
      body: [
        h('p', { class: 'dim' }, h('bdi', {}, title(conv)), conv.call ? ` · ${conv.call.participants ? conv.call.participants.length : 0} in the call` : ' · nobody is in a call here yet'),
        preview, level,
        h('div', { class: 'row' }, micBtn, camBtn),
        notes,
      ],
      foot: [cancel, join],
    };
  }, { wide: true });
}

export function ended(text) { toast(text); }
