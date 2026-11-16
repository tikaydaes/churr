// The reference patterns that apply in each conversation (ruling R73):
// the server's and the channel's own, for the composer, which offers a
// reference as it is typed, and for sealed chats, where the sending device
// finds references itself with the server's patterns. Fetched once per
// conversation and replaced when reference_patterns.changed says so.

import { get } from '/shared/api.js';
import { store } from './store.js';

const SERVER = '';
const scopes = new Map(); // conversation id, or '' for the server's → list
const loading = new Map();

store.on('patterns', (e) => {
  scopes.set(e.conversation_id || SERVER, e.patterns || []);
});

// load fetches the patterns of a conversation, and the server's with them.
export function loadPatterns(cid) {
  const key = cid || SERVER;
  if (scopes.has(key) && scopes.has(SERVER)) return Promise.resolve(patternsFor(cid));
  if (!loading.has(key)) {
    loading.set(key, get('/reference-patterns', cid ? { conversation_id: cid } : {}).then((r) => {
      const list = r.patterns || [];
      scopes.set(SERVER, list.filter((p) => !p.conversation_id));
      if (cid) scopes.set(cid, list.filter((p) => p.conversation_id === cid));
      return patternsFor(cid);
    }).catch(() => patternsFor(cid)).finally(() => loading.delete(key)));
  }
  return loading.get(key);
}

// patternsFor is what is known now: the server's patterns and, for a
// channel, its own. Sealed chats have none of their own.
export function patternsFor(cid) {
  return [...(scopes.get(SERVER) || []), ...((cid && scopes.get(cid)) || [])];
}

// serverPatterns is the server's patterns alone.
export const serverPatterns = () => scopes.get(SERVER) || [];
