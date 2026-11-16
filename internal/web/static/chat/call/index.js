// Calls in the web chat (docs/CALLS.md section 13, docs/CLIENTS.md
// sections 9.14 and 22). Where the rest of the chat reaches calls:
//
//   callButton(conv)      the header's call control
//   systemLine(m)         the words, and Join, of a call's system message
//   startCall(convId)     start or join from anywhere (the ring prompt)
//
// The parts: signal.js (frames, state, ICE policy), room.js (one call),
// media.js (the browser), layout.js (tiles and subscriptions), view.js
// (the screen), ring.js (incoming calls), frame-keys.js (the seam for
// sealed chats). Importing this module sets up the listeners; nothing
// happens until the person starts, joins or is rung.

import { h, iconButton, button, toast, plural } from '/shared/dom.js';
import { store, live, conversation, nameOf, isSealed, findMessage } from '../store.js';
import { ui } from '../ui.js';
import { Call } from './room.js';
import { CallView, prejoin } from './view.js';
import { Ringer, rung } from './ring.js';
import { sealedRefusal } from './frame-keys.js';
import { endedText, leftText, errorText } from './signal.js';
import * as media from './media.js';

let current = null; // the Call this page is in
let view = null;

// For the browser tests and for debugging: what the page's call is doing.
window.churrCall = { get current() { return current; }, get view() { return view; } };

function calls() { return !!(store.server && store.server.features && store.server.features.calls); }

function mayJoin(conv) {
  const m = conv && conv.membership;
  if (!m || m.level === 'reader' || conv.archived_at) return false;
  if (m.timed_out_until && new Date(m.timed_out_until) > new Date()) return false;
  return true;
}

// --- the header ---

export function callButton(conv) {
  if (!conv || !calls()) return null;
  const on = !!conv.call;
  const mine = current && !current.over && current.conversationId === conv.id;
  const n = on && conv.call.participants ? conv.call.participants.length : 0;
  if (!mayJoin(conv)) {
    // A reader sees that a call is on, and cannot join.
    return on ? h('span', { class: 'tag cyan call-tag', title: 'A call is on. Readers cannot join.' }, `call · ${n}`) : null;
  }
  const label = mine ? 'Show the call' : on ? `Join the call (${plural(n, 'person', 'people')} in it)` : 'Start a call';
  const btn = iconButton('call', label, () => (mine ? view && view.open() : startCall(conv.id)));
  if (on) {
    btn.classList.add('call-on');
    btn.append(h('span', { class: 'call-count', 'aria-hidden': 'true' }, String(n)));
  }
  if (mine) btn.setAttribute('aria-pressed', 'true');
  return btn;
}

// --- the system message ---

// systemLine words a call's system message: "Sam started a call" with
// Join while it is live, "Call, 21 minutes, 4 people" after, or "Missed
// call from Sam" for someone it rang who never joined. null for other
// kinds.
export function systemLine(m) {
  const s = m.system || {};
  if (s.kind !== 'call_started' && s.kind !== 'call_ended') return null;
  const who = nameOf(m.author_id);
  const conv = conversation(m.conversation_id);
  if (s.kind === 'call_started') {
    const live = conv && conv.call && conv.call.id === s.call_id;
    const mine = current && !current.over && current.callId === s.call_id;
    return [
      h('span', {}, `${m.author_id === store.me.id ? 'You' : who} started a call`),
      live && calls() && mayJoin(conv) ? [' ', button(mine ? 'Show the call' : 'Join', () => (mine ? view && view.open() : startCall(m.conversation_id)), 'small primary call-join')] : null,
    ];
  }
  const ids = s.participant_ids || [];
  const missed = m.author_id !== store.me.id && !ids.includes(store.me.id)
    && (rung.has(s.call_id) || (conv && (isSealed(conv) || (conv.kind === 'channel' && !conv.name)) && conv.membership && conv.membership.mode === 'any'));
  if (missed) return h('span', { class: 'call-missed' }, `Missed call from ${who}`);
  const minutes = Math.max(1, Math.round((s.duration_seconds || 0) / 60));
  return h('span', {}, `Call, ${plural(minutes, 'minute')}, ${plural(ids.length, 'person', 'people')}`);
}

// --- starting and joining ---

export async function startCall(conversationId) {
  const conv = conversation(conversationId);
  if (!conv) return;
  if (current && !current.over) {
    if (current.conversationId === conversationId) { if (view) view.open(); return; }
    toast('You are in another call. Leave it first.', true);
    return;
  }
  // A sealed chat's call needs its media encrypted end to end. Until this
  // browser can, it does not join, and does not start one either: starting
  // would ring the other person for a call this page cannot take part in.
  if (isSealed(conv) || (conv.call && conv.call.sealed)) {
    // A call not started yet has no id to derive keys for; the provider is
    // asked with what is known, and asked again once the call exists.
    const why = await sealedRefusal(conv.call || { conversation_id: conv.id, sealed: true });
    if (why) {
      toast(why, true);
      return;
    }
  }
  if (!media.secure()) toast('This page is not on HTTPS, so the browser allows no camera or microphone here. You can join to listen.', true);
  const choice = await prejoin(conv, !conv.call);
  if (!choice) return;
  const call = new Call(conv, { live, me: store.me.id });
  current = call;
  view = new CallView(call);
  call.on(() => {
    if (call.over && current === call) over(call);
  });
  store.emit('conversations');
  try {
    await call.join(choice);
    redrawMessage(call.state.callId && conversation(conversationId) && conversation(conversationId).call && conversation(conversationId).call.message_id, conversationId);
    store.emit('conversations');
  } catch (err) {
    const code = err.code || '';
    toast(code === 'conflict' ? errorText('call_full') : err.message || errorText(code), true);
    if (!call.over) call.finish('failed');
  }
}

function redrawMessage(messageId, conversationId) {
  const m = messageId && findMessage(messageId);
  if (m) m._v = (m._v || 0) + 1;
  store.emit('messages', conversationId);
}

function over(call) {
  if (view) { view.destroy(); view = null; }
  current = null;
  const why = call.why;
  if (why === 'ended') toast(endedText(call.state.ended && call.state.ended.reason));
  else if (why === 'server_restart') toast('The call ended because the server restarted.', true);
  else if (why && !['left', 'failed'].includes(why)) toast(leftText(why), why !== 'left');
  store.emit('conversations');
  const conv = conversation(call.conversationId);
  redrawMessage(conv && conv.call && conv.call.message_id, call.conversationId);
}

// --- the listeners ---

const ringer = new Ringer({
  onJoin: (cid) => { ui.openConversation(cid); startCall(cid); },
  inCall: () => !!(current && !current.over),
  decline: (callId) => live.send({ type: 'call.decline', id: 'decline-' + Date.now(), call_id: callId }),
});

store.on('event', (f) => {
  if (!f || typeof f.type !== 'string' || !f.type.startsWith('call.')) return;
  const c = f.conversation_id && store.conversations.get(f.conversation_id);
  // Who is in the call, kept on the conversation for the header and the
  // system message (the store keeps `call.started` and `call.ended`).
  if (f.type === 'call.participant_joined' && c && c.call && c.call.id === f.call_id) {
    const list = (c.call.participants || []).filter((p) => p.account_id !== f.participant.account_id);
    c.call = { ...c.call, participants: [...list, f.participant] };
    store.emit('conversations');
  }
  if (f.type === 'call.participant_left' && c && c.call && c.call.id === f.call_id) {
    c.call = { ...c.call, participants: (c.call.participants || []).filter((p) => p.account_id !== f.account_id) };
    store.emit('conversations');
  }
  if (f.type === 'call.started' || f.type === 'call.ended') {
    // The system message's Join comes and goes with the call: it is drawn
    // again once the store has taken the event in.
    const mid = f.type === 'call.started' ? f.call && f.call.message_id : c && c.call && c.call.message_id;
    setTimeout(() => redrawMessage(mid, f.conversation_id));
  }
  // Rings.
  if (f.type === 'call.ring_started') ringer.start(f);
  if (f.type === 'call.ring_stopped') ringer.stop(f.call_id);
  if (f.type === 'call.ended') ringer.stop(f.call_id);
  // The page's own call: answers carry no call id, events do.
  if (current && !current.over && (f.reply_to || f.type === 'call.offer' || f.type === 'call.error' || !f.call_id || f.call_id === current.callId)) {
    if (f.type === 'call.ring_started' || f.type === 'call.ring_stopped' || f.type === 'call.started') return;
    if (f.conversation_id && f.conversation_id !== current.conversationId) return;
    current.handle(f);
  }
});

// The live connection: a drop holds the call's signalling, and a new
// connection resumes it.
store.on('connection', (s) => {
  if (!current || current.over) return;
  if (s === 'live') current.signalBack(); else current.signalLost();
  store.emit('conversations');
});

// The page going to the background shows no video: the subscriptions
// empty. A phone also turns its camera off, and back on when it returns
// (CALLS.md section 11).
let cameraParked = false;
document.addEventListener('visibilitychange', () => {
  if (!current || current.over || !media.phone()) return;
  if (document.visibilityState === 'hidden' && !current.muted.camera) { cameraParked = true; current.toggleCamera(); }
  else if (document.visibilityState === 'visible' && cameraParked) { cameraParked = false; current.toggleCamera(); }
});

// inCall says whether this page is in a call; leaveCall leaves it. Locking
// Churr ends this device's part in a call (the lock).
export function inCall() { return !!(current && !current.over); }
export async function leaveCall() { if (current && !current.over) await current.leave(); }

// Closing the page leaves the call: there is nothing to keep it going.
window.addEventListener('pagehide', () => {
  if (!current || current.over) return;
  const id = current.callId;
  const sent = live.send({ type: 'call.leave', id: 'leave-unload', call_id: id });
  if (!sent) fetch(`/api/v1/conversations/${current.conversationId}/call/leave`, { method: 'POST', keepalive: true, credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: '{}' }).catch(() => {});
});

// Mod+Shift+M, V and H: the microphone, the camera, leaving.
window.addEventListener('keydown', (e) => {
  if (!current || current.over || !(e.metaKey || e.ctrlKey) || !e.shiftKey) return;
  const k = e.key.toLowerCase();
  if (k === 'm') { e.preventDefault(); current.toggleMic(); }
  else if (k === 'v') { e.preventDefault(); current.toggleCamera(); }
  else if (k === 'h') { e.preventDefault(); current.leave(); }
});
