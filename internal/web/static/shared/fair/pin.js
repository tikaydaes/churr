// The client's pin of its server's VRF key history (docs/DESIGN-FAIR.md
// section 1). getServer gives the history, oldest first; the first one seen
// is kept in this browser. Later, the same history or the kept one with keys
// added after it is fine (and kept); anything else is a change, which the
// chat shows as a warning and does not take: a proof of a chance outcome
// under a key the server was not seen to begin with is not to be trusted.
//
// An entry is { key, began, sig } as the API writes it (base64url key and
// sig, RFC 3339 time). Entries are compared by key and start; the server
// checks the signatures.

const PREFIX = 'churr.fair.pin.';

const sameEntry = (a, b) => a && b && a.key === b.key && a.began === b.began;

// comparePin says what a shown history is against the pinned one:
// { state: 'new' | 'same' | 'grown' | 'changed', added: [entries] }.
export function comparePin(pinned, shown) {
  shown = Array.isArray(shown) ? shown : [];
  if (!pinned || pinned.length === 0) return { state: shown.length ? 'new' : 'changed', added: shown };
  if (shown.length < pinned.length || !pinned.every((p, i) => sameEntry(p, shown[i]))) return { state: 'changed', added: [] };
  const added = shown.slice(pinned.length);
  return { state: added.length ? 'grown' : 'same', added };
}

// checkServerPin compares the server's history with the pin kept for its
// name and keeps the new one when it is sound. Without storage (a private
// window, blocked site data) every load is a first sight.
export function checkServerPin(server, storage = globalThis.localStorage) {
  const shown = (server && server.vrf_keys) || [];
  const key = PREFIX + ((server && server.name) || '');
  let pinned = null;
  try {
    const raw = storage && storage.getItem(key);
    pinned = raw ? JSON.parse(raw) : null;
  } catch { pinned = null; }
  const result = comparePin(pinned, shown);
  if (result.state === 'new' || result.state === 'grown') {
    try { storage && storage.setItem(key, JSON.stringify(shown)); } catch { /* kept for this load only */ }
  }
  return { ...result, pinned: pinned || shown };
}

// The warning the chat shows when the history changed.
export const PIN_CHANGED = 'This server now shows a different key for proving dice and other chance results than the one this browser saw before. Results proved under the new key cannot be checked against the old one; ask your server\'s owner what happened.';
