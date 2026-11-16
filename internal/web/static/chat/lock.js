// The lock (ruling R75, docs/CLIENTS.md section 9.27, docs/AUTH.md section
// 10.6): Churr locked on this browser until a passkey or the access key
// unlocks it, without signing out, which would throw away the browser's
// sealed-chat set-up.
//
// Locking leaves any call, makes this browser a locked sealed-chat device,
// marks the session locked on the server and replaces the page with the
// lock screen (/locked/), so nothing the page held is in the page or in
// memory any more. Every tab of this browser shares the session, and goes
// with it: the others are told on a BroadcastChannel, and a tab that
// misses that finds out from its next request or its live connection.
//
// Ways to lock: /lock, "Lock" in the account menu, Mod+Shift+L, and the
// optional automatic lock after a time without use (a setting of this
// browser, off by default).

import { api, goToLock } from '/shared/api.js';
import { confirmDialog } from '/shared/dom.js';
import { inCall, leaveCall } from './call/index.js';
import { lockSealed } from './sealed/index.js';
import { AUTO_KEY, autoMinutes } from '/shared/lockprefs.js';

const CHANNEL = 'churr-lock';
const SEEN_KEY = 'churr-last-active';

let tabs = null;
try { tabs = new BroadcastChannel(CHANNEL); } catch { /* an old browser: requests find out */ }
if (tabs) tabs.onmessage = (e) => { if (e.data === 'locked') goToLock(); };

let locking = false;

// lockNow locks this browser. ask is true for the command and the menu: a
// call in progress is ended, and the person is told so first. The
// automatic lock does not ask (the owner's decision: it ends the call too).
export async function lockNow({ ask = false } = {}) {
  if (locking) return;
  if (ask && inCall()) {
    const ok = await confirmDialog('Lock and leave the call?', 'Locking ends your part in the call on this device.', { yes: 'Lock' });
    if (!ok) return;
  }
  locking = true;
  try { await leaveCall(); } catch { /* the page is going anyway, and leaving it leaves the call */ }
  try { await lockSealed(); } catch (err) { console.warn('sealed chats were not locked:', err.message); }
  // The lock screen locks the session again if this does not get through.
  try { await api('POST', '/session/lock', {}); } catch { /* see above */ }
  if (tabs) tabs.postMessage('locked');
  goToLock();
}

let autoNow = autoMinutes();

// Use in any tab of this browser counts: the time of the last use is
// shared through storage, written at most every few seconds.
let lastWrite = 0;
function used() {
  const now = Date.now();
  if (now - lastWrite < 5000) return;
  lastWrite = now;
  try { localStorage.setItem(SEEN_KEY, String(now)); } catch { /* this tab's own clock below */ }
  ownSeen = now;
}
let ownSeen = Date.now();

function lastUse() {
  let shared = 0;
  try { shared = Number(localStorage.getItem(SEEN_KEY)) || 0; } catch { /* none */ }
  return Math.max(shared, ownSeen);
}

// startAutoLock watches for use and locks after the chosen time without
// any. It runs during a call as well, and ends it (the setting says so).
export function startAutoLock() {
  for (const ev of ['pointerdown', 'keydown', 'wheel', 'touchstart', 'pointermove']) window.addEventListener(ev, used, { passive: true, capture: true });
  used();
  setInterval(() => {
    if (!autoNow) return;
    if (Date.now() - lastUse() >= autoNow * 60000) lockNow();
  }, 10000);
  window.addEventListener('storage', (e) => { if (e.key === AUTO_KEY) autoNow = autoMinutes(); });
}

// isLockKey is Mod+Shift+L.
export function isLockKey(e) {
  return (e.metaKey || e.ctrlKey) && e.shiftKey && !e.altKey && e.key.toLowerCase() === 'l';
}
