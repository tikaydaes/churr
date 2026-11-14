// Browser notifications while the page is open (docs/CLIENTS.md section
// 22, docs/PUSH.md section 13), and the web chat's own title badge.
//
// The server decides what is worth notifying and marks each
// `message.created` for its reader. `notify` means it would have notified;
// the page then shows a notification unless that conversation is on screen
// and in front. Closing the page is like quitting an app: nothing arrives.

import { store, conversation, nameOf, pathTitle, title } from './store.js';
import { firstLine } from '/shared/message/message.js';
import { ui } from './ui.js';

export function startNotifications() {
  store.on('message.created', (e) => {
    if (e.notify !== 'notify') return;
    const m = e.message;
    if (m.author_id === store.me.id) return;
    const onScreen = store.current === m.conversation_id && document.visibilityState === 'visible' && document.hasFocus();
    if (onScreen) return;
    if (!('Notification' in window) || Notification.permission !== 'granted' || store.prefs.notify === false) return;
    const c = conversation(m.conversation_id);
    const where = c ? (c.name ? pathTitle(c) : title(c)) : '';
    const sealed = !!m.sealed;
    try {
      const n = new Notification(sealed ? nameOf(m.author_id) : `${nameOf(m.author_id)}${c && c.name ? ' in ' + where : ''}`, {
        // A sealed message cannot be read by this build, so it says only
        // that there is one.
        body: sealed ? 'A sealed message' : firstLine(m, { nameOf, md: () => ({ conversation }) }, 180),
        tag: m.conversation_id + (m.parent_id || ''), icon: '/icons/icon-192.png',
      });
      n.onclick = () => { window.focus(); ui.openConversation(m.conversation_id, m.parent_id ? { threadId: m.parent_id } : {}); n.close(); };
    } catch { /* some browsers only allow notifications from a service worker, which this page has none of on purpose */ }
  });
  const badge = () => {
    const n = store.total.badge || 0;
    const base = (store.server && store.server.community_name) || 'Churr';
    const c = store.current && conversation(store.current);
    document.title = `${n ? `(${n}) ` : ''}${c ? title(c) + ' · ' : ''}${base}`;
    if (navigator.setAppBadge) { (n ? navigator.setAppBadge(n) : navigator.clearAppBadge()).catch(() => {}); }
  };
  store.on('conversations', badge);
  store.on('route', badge);
  badge();
}
