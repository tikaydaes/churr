// Several tabs of one browser are one device: they share its keys, its
// chats' epochs and its send counters (docs/SEALED.md section 14). Two tabs
// must never make an epoch at once or spend the same counter, which would
// sign two messages with one counter and look like a replay.
//
// The mechanism is the Web Locks API: every change to the device's stored
// state happens inside exclusive(), which holds one lock per origin and
// account across every tab, loads the state afresh from storage, changes
// it and writes it back before letting go. Other tabs are then told over a
// BroadcastChannel to read the state again.

const local = new Map();

// exclusive runs fn while holding the lock `name` in every tab of this
// origin. locks is the Web Locks API (navigator.locks); a page without it
// falls back to a queue within the page.
export function exclusive(name, fn, locks = globalThis.navigator && globalThis.navigator.locks) {
  if (locks && locks.request) return locks.request(name, { mode: 'exclusive' }, () => fn());
  const prev = local.get(name) || Promise.resolve();
  const run = prev.then(() => fn());
  local.set(name, run.catch(() => {}));
  return run;
}

// A channel to the other tabs of this origin, for "the stored state
// changed". onMessage gets what another tab posted.
export function channel(name, onMessage) {
  if (typeof BroadcastChannel === 'undefined') return { post() {}, close() {} };
  const ch = new BroadcastChannel(name);
  ch.onmessage = (e) => onMessage(e.data);
  return { post: (m) => ch.postMessage(m), close: () => ch.close() };
}
