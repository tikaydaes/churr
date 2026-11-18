// The client's pins of servers' VRF key histories (docs/DESIGN-FAIR.md
// section 1), one per server name. getServer gives this server's history,
// oldest first, and a shared conversation gives the history of every server
// whose draws it may hold (`shared.vrf_keys`, from the hub channel's record
// or this server's pin of a direct link's other server). The first history
// seen for a name is kept in this browser. Later, the same history or the
// kept one with keys added after it is fine (and kept); anything else is a
// change, which the chat shows as a warning and does not take: a proof of a
// chance outcome under a key the server was not seen to begin with is not to
// be trusted.
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

// pinHistory compares a history shown for a server with the pin kept for its
// name and keeps the new one when it is sound. It gives the comparison and
// `pinned`, the history checks are made against: the pin, never a history
// that changed it. Without storage (a private window, blocked site data)
// every load is a first sight.
export function pinHistory(name, shown, storage = globalThis.localStorage) {
  shown = Array.isArray(shown) ? shown : [];
  const key = PREFIX + (name || '');
  let pinned = null;
  try {
    const raw = storage && storage.getItem(key);
    pinned = raw ? JSON.parse(raw) : null;
  } catch { pinned = null; }
  const result = comparePin(pinned, shown);
  if (result.state === 'new' || result.state === 'grown') {
    try { storage && storage.setItem(key, JSON.stringify(shown)); } catch { /* kept for this load only */ }
  }
  return { ...result, pinned: result.state === 'changed' ? pinned || [] : shown };
}

// Histories is what the chat holds of every server's history in one load:
// each name pinned once, again only when what is shown for it changes. Its
// of() gives a check's history for a server: { keys, changed }, or null for a
// server no source has given a history of.
export class Histories {
  constructor(storage = globalThis.localStorage) {
    this.storage = storage;
    this.byName = new Map();
  }

  // show pins what a source shows for a server: getServer for this one, a
  // conversation's `shared.vrf_keys` for the rest.
  show(name, shown) {
    const had = this.byName.get(name);
    if (!name || !Array.isArray(shown) || shown.length === 0) return had ? had.result : null;
    const seen = JSON.stringify(shown);
    if (had && had.seen === seen) return had.result;
    const result = pinHistory(name, shown, this.storage);
    // Once a change has been seen for a name in this load it stays: a
    // source that shows the pinned one again does not take the warning back.
    if (had && had.result.state === 'changed') result.state = 'changed';
    this.byName.set(name, { seen, result });
    return result;
  }

  // showShared pins every history a conversation gives.
  showShared(conversation) {
    const list = conversation && conversation.shared && conversation.shared.vrf_keys;
    for (const s of Array.isArray(list) ? list : []) if (s && typeof s.server === 'string') this.show(s.server, s.keys);
  }

  of(name) {
    const had = this.byName.get(name);
    if (!had) return null;
    return { keys: had.result.pinned, changed: had.result.state === 'changed' };
  }
}

// The warning the chat shows when this server's history changed.
export const PIN_CHANGED = 'This server now shows a different key for proving dice and other chance results than the one this browser saw before. Results proved under the new key cannot be checked against the old one; ask your server\'s owner what happened.';
