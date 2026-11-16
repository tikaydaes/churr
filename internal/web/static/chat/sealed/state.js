// The web chat's one sealed-chat session (shared/sealed/session.js), and
// the events that keep it in step: a new epoch record, a sealed message, a
// device list, an unlock request, the backup.

import { store, conversation } from '../store.js';
import { Session } from '/shared/sealed/session.js';
import { frameKeysChanged } from '../call/frame-keys.js';

let opening = null;

// session opens the session once per page.
export function session() {
  if (!opening) {
    opening = Session.open(store.me, store.server).then(async (s) => {
      s.conversations = () => [...store.conversations.values()].filter((c) => c.kind === 'sealed');
      wire(s);
      await afterLock(s);
      return s;
    });
  }
  return opening;
}

// afterLock unlocks a browser that locking left locked (the lock): the copy
// of the backup key it left for its own box key is given back now that the
// session is unlocked. Any other locked browser stays as it is and asks.
async function afterLock(s) {
  if (s.state !== 'locked') return;
  try {
    if (await s.hasDeviceCopy()) await s.unlockWithDeviceCopy();
  } catch (err) { console.warn(err.message); }
}

// otherOf names the other person of a sealed chat.
export function otherOf(c) {
  return (c && (c.member_ids || []).find((x) => x !== store.me.id)) || null;
}

const pending = new Map();

// later runs one sync per chat at a time, and runs it again if more came
// in while it ran.
function later(id, fn) {
  if (!id) return;
  const p = pending.get(id);
  if (p) { p.again = true; return; }
  const entry = { again: false };
  pending.set(id, entry);
  (async () => {
    // A sync that fails (the server busy, the connection gone) is tried
    // again with the next event or reconnection. One cut short because
    // the session ended or was locked meanwhile is no fault of the page:
    // the sign-in or lock screen follows.
    // Events come in bursts (an epoch, then messages); a moment's wait
    // lets one sync take in all of them.
    try { do { entry.again = false; await new Promise((r) => setTimeout(r, 250)); await fn(); } while (entry.again); } catch (err) { if (err && ['rate_limited', 'network', 'unauthenticated', 'session_locked'].includes(err.code)) console.warn(err.message); else console.error(err); } finally { pending.delete(id); }
  })();
}

// The key epoch each sealed chat was last seen in. When a chat moves to
// another one while its call is on, the call takes new frame keys
// (docs/SEALED.md section 10).
const epochSeen = new Map();
function epochMoved(s, conv) {
  const chat = s.device && s.device.chats.get(conv);
  const cur = chat && chat.current;
  if (!cur || !cur.accepted) return;
  const was = epochSeen.get(conv);
  epochSeen.set(conv, cur.keyEpoch);
  // The first sighting counts too: the call may have been joined before
  // this page had synced the chat. A call already on these keys ignores it.
  if (was === cur.keyEpoch) return;
  const c = conversation(conv);
  if (c && c.call && c.call.id) frameKeysChanged(c.call.id);
}

function wire(s) {
  s.on((what) => { if (what && what.chat) epochMoved(s, what.chat); });
  store.on('event', (e) => {
    if (s.state !== 'unlocked') {
      if (e.type === 'unlock_request.answered' || e.type === 'backup.changed') s.emit({ event: e });
      return;
    }
    const c = e.conversation_id && conversation(e.conversation_id);
    switch (e.type) {
      case 'sealed.epoch_started':
      case 'message.created':
      case 'message.deleted':
        if (c && c.kind === 'sealed' && (e.type === 'sealed.epoch_started' || (e.message && e.message.sealed) || e.type === 'message.deleted')) {
          if (e.type === 'message.deleted' && e.message) s.rows.get(c.id)?.set(e.message.seq, e.message);
          if (otherOf(c)) later(c.id, () => s.sync(c.id, otherOf(c)));
        }
        break;
      case 'device_list.changed':
        for (const x of store.conversations.values()) {
          if (x.kind === 'sealed' && otherOf(x) && (e.account_id === store.me.id || (x.member_ids || []).includes(e.account_id))) later(x.id, () => s.sync(x.id, otherOf(x)));
        }
        break;
      case 'backup.changed':
        // A key replacement elsewhere: open the copy left for this browser.
        s.emit({ event: e });
        s.locked((d) => s.catchUpKeys(d)).then(() => s.emit('state')).catch((err) => console.warn(err.message));
        break;
      case 'unlock_request.created': case 'unlock_request.answered':
        s.emit({ event: e });
        break;
      default:
    }
  });
  // After the live connection comes back, every open sealed chat catches
  // up.
  store.on('ready', () => { if (store.current && s.state === 'unlocked') { const c = conversation(store.current); if (c && c.kind === 'sealed') later(c.id, () => s.sync(c.id, otherOf(c))); } });
}
