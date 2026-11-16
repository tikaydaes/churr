// The web chat's drafts, kept across reloads (docs/CLIENTS.md section 22):
// per conversation and thread, in this browser's local storage under the
// account, as the other web preferences are. A draft goes when it is sent
// or emptied, and all of them go when the session is locked or signed out:
// a locked browser keeps nothing of the account that can be read
// (docs/CLIENTS.md 9.27). Sealed chats have their own composer and keep
// no draft here, as the apps keep none in their unencrypted cache.
//
// Every use of storage is wrapped: storage closed to the page, or full,
// leaves drafts for the life of the page only.

const PREFIX = 'churr-drafts.';
// Drafts older than this are dropped, and at most MAX are kept.
const KEEP_MS = 30 * 24 * 3600 * 1000;
const MAX = 100;

// Once the lock or sign-out has wiped them, nothing is written again by
// this page, which is about to go.
let stopped = false;

// keptDrafts gives the drafts of one account: get, set and remove by key
// ("conversation:parent"). Each is kept in memory and in `storage`.
export function keptDrafts(storage, accountId, now = () => Date.now()) {
  const name = PREFIX + accountId;
  let all = {};
  try {
    const v = JSON.parse(storage.getItem(name) || '{}');
    if (v && typeof v === 'object') all = v;
  } catch { /* nothing kept, or storage is closed to this page */ }
  const cut = now() - KEEP_MS;
  for (const [k, d] of Object.entries(all)) if (!d || typeof d.text !== 'string' || !(d.at > cut)) delete all[k];
  const write = () => {
    if (stopped) return;
    try {
      if (Object.keys(all).length) storage.setItem(name, JSON.stringify(all));
      else storage.removeItem(name);
    } catch { /* kept for the life of the page only */ }
  };
  return {
    get: (key) => all[key] || null,
    set(key, d) {
      all[key] = { text: d.text, quote: d.quote || null, quoteOf: d.quoteOf || null, at: now() };
      const keys = Object.keys(all);
      if (keys.length > MAX) {
        keys.sort((a, b) => all[a].at - all[b].at);
        for (const k of keys.slice(0, keys.length - MAX)) delete all[k];
      }
      write();
    },
    remove(key) {
      if (!(key in all)) return;
      delete all[key];
      write();
    },
  };
}

// forgetDrafts removes every account's drafts from `storage`, and stops
// this page writing any more. Called on locking and signing out.
export function forgetDrafts(storage) {
  stopped = true;
  try {
    const names = [];
    for (let i = 0; i < storage.length; i++) { const k = storage.key(i); if (k && k.startsWith(PREFIX)) names.push(k); }
    for (const k of names) storage.removeItem(k);
  } catch { /* storage closed to this page: nothing was kept */ }
}

// browserStorage is the page's local storage, reached only inside the
// calls, which are all wrapped: reading `localStorage` itself throws where
// storage is closed to the page.
export const browserStorage = {
  getItem: (k) => localStorage.getItem(k),
  setItem: (k, v) => localStorage.setItem(k, v),
  removeItem: (k) => localStorage.removeItem(k),
  key: (i) => localStorage.key(i),
  get length() { return localStorage.length; },
};

// resetForTests lets a test start again after forgetDrafts.
export function resetForTests() { stopped = false; }
