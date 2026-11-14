// Notification and sidebar settings (docs/CLIENTS.md section 9.12).
//
// Each membership has two independent settings on the server: `mode`, what
// notifies, and `show`, whether the conversation has a row in the sidebar.
// They do not constrain each other. The default used where `show` is not
// set, and the "Show hidden" toggle, are kept in this browser.

import { patch } from '/shared/api.js';
import { h, openDialog, button, toast, check, fill, dateTime, localInput, fromLocalInput } from '/shared/dom.js';
import { appearanceControls } from '/shared/appearance.js';
import { store, conversation, title, isSealed, isMuted, savePrefs, showOf } from './store.js';

const MODES = [
  ['any', 'Any activity', 'Every message notifies and counts in the badge.'],
  ['mentions', 'Mentions', 'Mentions and replies in threads you follow notify and count.'],
  ['nothing', 'Nothing', 'Nothing notifies. The row is dimmed. A mention of you by name still counts in the badge.'],
];
const SHOWS = [
  ['', 'This browser’s default', null],
  ['always', 'Always', 'The row is always there.'],
  ['activity', 'When there is activity', 'Only while there are unread messages.'],
  ['notification', 'When it notifies', 'Only while it has a badge.'],
  ['hidden', 'Hidden', 'No row. Still reachable by search, links, the quick switcher and “Show hidden”.'],
];
export const SHOW_NAMES = { always: 'Always', activity: 'When there is activity', notification: 'When it notifies', hidden: 'Hidden' };
const MUTES = [['An hour', 3600], ['Eight hours', 8 * 3600], ['A day', 86400], ['A week', 7 * 86400]];

// updateMembership makes one change to the reader's own membership.
export async function updateMembership(id, change) {
  try {
    const out = await patch(`/conversations/${id}/membership`, change);
    const c = store.conversations.get(id);
    if (c) c.membership = out.membership;
    store.emit('conversations');
    store.emit('membership', id);
    store.emit('messages', id);
    return true;
  } catch (err) {
    if (!err.silent) toast(err.message, true);
    return false;
  }
}

// summary puts the pair into words. When the conversation is hidden with a
// mode that notifies, it says where those notifications will be seen.
export function summary(c) {
  const m = c.membership;
  const muted = isMuted(c);
  const what = muted ? `Muted until ${dateTime(m.muted_until)}: nothing notifies you until then.`
    : m.mode === 'any' ? 'Every message will notify you.' : m.mode === 'mentions' ? 'Mentions will notify you.' : 'Nothing here will notify you.';
  const show = showOf(c);
  const where = show === 'always' ? 'It is always in the sidebar.'
    : show === 'activity' ? 'It appears in the sidebar only while it has something unread.'
      : show === 'notification' ? 'It appears in the sidebar only while it has a badge.'
        : m.mode !== 'nothing' && !muted ? 'It has no row in the sidebar: its notifications are seen in the notifications list, and in the totals.' : 'It has no row in the sidebar.';
  return what + ' ' + where;
}

export function openModes(id) {
  return openDialog('Notifications and sidebar', (close) => {
    const body = h('div', { class: 'stack' });
    const draw = () => {
      const c = conversation(id);
      if (!c || !c.membership) { close(); return; }
      const m = c.membership;
      const radio = (name, value, label, hint, checked, onchange) => check(label, h('input', { type: 'radio', name, value, checked, onchange }), hint);
      const until = h('input', { type: 'datetime-local', 'aria-label': 'Mute until', value: isMuted(c) ? localInput(m.muted_until) : '' });
      fill(body,
        h('p', { class: 'dim' }, title(c)),
        h('fieldset', { class: 'plain stack tight' }, h('legend', { class: 'label' }, 'What notifies'),
          // A sealed chat has no Mentions choice.
          MODES.filter(([v]) => !(isSealed(c) && v === 'mentions')).map(([v, label, hint]) => radio('mode', v, label, hint, m.mode === v, () => updateMembership(id, { mode: v }).then(draw)))),
        h('div', { class: 'stack tight' }, h('span', { class: 'label' }, 'Mute for'),
          h('div', { class: 'row' }, MUTES.map(([label, s]) => button(label, () => updateMembership(id, { muted_until: new Date(Date.now() + s * 1000).toISOString() }).then(draw), 'small')),
            isMuted(c) ? button('Unmute', () => updateMembership(id, { muted_until: null }).then(draw), 'small primary') : null),
          h('div', { class: 'row' }, h('div', { class: 'grow' }, until), button('Mute until then', () => { const at = fromLocalInput(until.value); if (at) updateMembership(id, { muted_until: at }).then(draw); }, 'small')),
          h('p', { class: 'dim small' }, 'A mute behaves as Nothing until it ends, and then the choice above returns.')),
        h('fieldset', { class: 'plain stack tight' }, h('legend', { class: 'label' }, 'Show in sidebar'),
          SHOWS.map(([v, label, hint]) => radio('show', v, label, v === '' ? `Currently “${SHOW_NAMES[store.prefs.showDefault]}”. Change it in the web chat’s settings.` : hint, (m.show || '') === v,
            () => updateMembership(id, { show: v || null }).then(draw)))),
        h('p', { class: 'note', role: 'status' }, summary(c)));
    };
    draw();
    return { body, foot: [button('Done', () => close(), 'primary')] };
  });
}

// The web chat's own settings: the default for "show in sidebar" and
// "Show hidden", which live in this browser; browser notifications; and
// the default thread view, which lives on the server.
export function openChatSettings() {
  return openDialog('Web chat settings', (close) => {
    const body = h('div', { class: 'stack' });
    const draw = () => {
      const perm = 'Notification' in window ? Notification.permission : 'unsupported';
      fill(body,
        h('fieldset', { class: 'plain stack tight' }, h('legend', { class: 'label' }, 'Default for “show in sidebar”'),
          h('p', { class: 'dim small' }, 'Used by every conversation whose own setting is “this browser’s default”. Kept in this browser, not on the server, and removed when you sign out.'),
          ['always', 'activity', 'notification'].map((v) => check(SHOW_NAMES[v], h('input', { type: 'radio', name: 'def', checked: store.prefs.showDefault === v, onchange: () => { store.prefs.showDefault = v; savePrefs(); } })))),
        check('Show hidden conversations', h('input', { type: 'checkbox', class: 'switch', checked: store.prefs.showHidden, onchange: (e) => { store.prefs.showHidden = e.target.checked; savePrefs(); } }),
          'Hidden conversations get their rows back, marked “hidden”.'),
        h('div', { class: 'stack tight' }, h('span', { class: 'label' }, 'Browser notifications'),
          h('p', { class: 'dim small' }, 'The web chat notifies only while this page is open. Closing it is like quitting an app: no notifications, no badge. It catches up when opened again.'),
          perm === 'unsupported' ? h('p', { class: 'note' }, 'This browser has no notifications.')
            : perm === 'denied' ? h('p', { class: 'note warn' }, 'Notifications are blocked for this site in the browser’s own settings.')
              : perm === 'default' ? h('p', { class: 'row' }, button('Allow notifications', async () => { await Notification.requestPermission(); draw(); }, 'primary small'))
                : [check('Show notifications', h('input', { type: 'checkbox', class: 'switch', checked: store.prefs.notify !== false, onchange: (e) => { store.prefs.notify = e.target.checked; savePrefs(); } })),
                  h('p', { class: 'row' }, button('Send a test notification', () => { try { new Notification('Churr', { body: 'This is what a notification looks like.', icon: '/icons/icon-192.png' }); } catch (err) { toast(err.message, true); } }, 'small'))]),
        h('div', { class: 'stack tight' }, h('span', { class: 'label' }, 'Default thread view'),
          h('div', { class: 'row' }, ['threaded', 'flat'].map((v) => h('button', { type: 'button', class: 'btn small' + ((store.me.prefs && store.me.prefs.thread_view) === v ? ' primary' : ''), 'aria-pressed': String((store.me.prefs && store.me.prefs.thread_view) === v),
            onclick: async () => { try { store.me.prefs = (await patch('/me/prefs', { thread_view: v })).prefs; store.emit('me'); store.emit('messages', store.current); draw(); } catch (err) { toast(err.message, true); } } }, v === 'threaded' ? 'Threaded' : 'Flat')))),
        h('div', { class: 'stack' }, appearanceControls()),
        h('p', { class: 'dim small' }, 'Passkeys, sessions, quiet hours and tokens are on your ', h('a', { href: '/account' }, 'account page'), '.'));
    };
    draw();
    return { body, foot: [button('Done', () => close(), 'primary')] };
  });
}
