// Profiles, and one's own status, availability and do not disturb
// (docs/CLIENTS.md sections 9.11 and 9.13).

import { get, post, put, del } from '/shared/api.js';
import { h, openDialog, button, toast, avatar, dateTime, check, busy, localInput, fromLocalInput, icon, promptDialog } from '/shared/dom.js';
import { REPORT_PERSON } from '/shared/roles.js';
import { formDialog } from '/shared/form.js';
import { presence } from '/shared/icons.js';
import { store, account, refreshMe, showingProfile } from './store.js';
import { ui } from './ui.js';
import { startSealedChat } from './sealed/index.js';
import { STRINGS } from '/shared/commands.js';
import { selfServe, toggle } from './groups.js';

const nameOf = (a) => a.display_name || a.username;

// watchFor asks to be told once when someone is next around (ruling R76)
// and returns what to say: that it is set, or that they are around now.
export async function watchFor(a) {
  const out = await put('/me/watches/' + a.id);
  return out.around ? STRINGS.isAround(nameOf(a)) : STRINGS.watching(nameOf(a));
}

// unwatchFor stops watching for someone and returns what to say.
export async function unwatchFor(a) {
  await del('/me/watches/' + a.id);
  return STRINGS.unwatched(nameOf(a));
}

// watchingFor reports whether the caller has a watch waiting for someone.
async function watchingFor(id) {
  try {
    return (await get('/scheduled', { kind: 'watch', limit: 200 })).scheduled.some((s) => s.watch_account_id === id && s.next_run_at);
  } catch { return false; }
}

function localTime(zone) {
  try { return new Date().toLocaleTimeString(undefined, { timeZone: zone, hour: '2-digit', minute: '2-digit' }); } catch { return null; }
}

export function statusLine(a) {
  if (!a) return '';
  const parts = [];
  if (a.status && (a.status.text || a.status.emoji)) parts.push(`${a.status.emoji || ''} ${a.status.text || ''}`.trim());
  if (a.availability) parts.push((a.availability.value === 'meeting' ? 'In a meeting' : 'Out of office') + (a.availability.until ? ' until ' + dateTime(a.availability.until) : ''));
  if (a.dnd) parts.push('Do not disturb');
  return parts.join(' · ');
}

// openProfile shows someone's profile, with the things one can do from it.
export async function openProfile(id) {
  let a = account(id);
  try { a = (await get('/accounts/' + id)).account; store.accounts.set(id, { ...store.accounts.get(id), ...a }); } catch { /* what is known already is shown */ }
  if (!a) { toast('That account cannot be seen.', true); return; }
  const me = id === store.me.id;
  const groups = store.groups.filter((g) => (a.group_ids || []).includes(g.id) && !g.builtin);
  // While the profile is open its presence is one the page shows.
  showingProfile(id);
  const watching = !me && await watchingFor(id);
  try { await profileDialog(id, a, me, groups, watching); } finally { showingProfile(null); }
}

// lastSeen says roughly when someone was last here, in words. The server
// gives one of four answers and never a time; nothing for someone online
// (their presence says so) or who hides it.
const SEEN = { recently: 'last seen recently', within_week: 'last seen within a week', long_ago: 'last seen a while ago' };
export function lastSeen(a) {
  return (a && a.presence !== 'online' && SEEN[a.last_seen]) || '';
}

// groupButton is Join or Leave beside an open group on a profile, for
// whoever is looking (docs/CLIENTS.md 9.15).
function groupButton(g, close) {
  if (!selfServe(g, store.me.role)) return null;
  const b = button(g.is_member ? 'Leave @' + g.handle : 'Join @' + g.handle, () => busy(b, async () => {
    try {
      const now = await toggle(g, { post, del });
      store.groups = store.groups.map((x) => (x.id === g.id ? now : x));
      toast(g.is_member ? `You left @${g.handle}.` : `You joined @${g.handle}, and the channels it is in.`);
      close();
    } catch (err) { toast(err.message, true); }
  }), 'small');
  b.dataset.group = g.handle;
  return b;
}

async function profileDialog(id, a, me, groups, watching) {
  await openDialog(a.display_name || a.username, (close) => {
    const blocked = store.blocked.has(id);
    const message = button('Message', () => busy(message, async () => {
      const out = await post('/chats', { account_ids: [id] });
      store.conversations.set(out.conversation.id, { ...store.conversations.get(out.conversation.id), ...out.conversation });
      store.emit('conversations');
      close();
      ui.openConversation(out.conversation.id);
    }), 'primary');
    const block = button(blocked ? 'Unblock' : 'Block', () => busy(block, async () => {
      if (blocked) { await del('/me/blocks/' + id); store.blocked.delete(id); } else { await put('/me/blocks/' + id); store.blocked.add(id); }
      store.emit('messages', store.current);
      toast(blocked ? 'Unblocked.' : 'Blocked. Their messages are collapsed for you, and they are told nothing.');
      close();
    }), blocked ? '' : 'danger');
    // "Tell me when Ada is around" (ruling R76), or stopping it.
    const watch = button(watching ? STRINGS.unwatchLabel(nameOf(a)) : STRINGS.watchLabel(nameOf(a)), () => busy(watch, async () => {
      toast(await (watching ? unwatchFor(a) : watchFor(a)));
      close();
    }));
    watch.dataset.action = 'watch';
    // A person may be reported to the admins for their name, status or
    // picture (docs/PERMISSIONS.md section 8.4).
    const report = button('Report…', async () => {
      close();
      const reason = await promptDialog('Report ' + nameOf(a), 'Why', { required: false, multiline: true, yes: 'Report', maxLength: 500, message: REPORT_PERSON });
      if (reason == null) return;
      try { await post(`/accounts/${id}/report`, { reason: reason.trim() || undefined }); toast('Reported.'); } catch (err) { if (!err.silent) toast(err.message, true); }
    });
    report.dataset.action = 'report';
    const zone = a.time_zone && localTime(a.time_zone);
    return {
      body: [
        h('div', { class: 'profile-top' }, avatar(a, 'large'),
          h('div', { class: 'stack tight' },
            h('p', { class: 'mono' }, '@' + a.username + (a.server_name ? '@' + a.server_name : '')),
            h('p', {}, h('span', { class: 'tag ' + (a.role === 'owner' ? 'pink' : a.role === 'admin' ? 'lavender' : 'line') }, a.role), ' ',
              groups.map((g) => [h('span', { class: 'tag ' + (g.color || 'line') }, '@' + g.handle), ' ', groupButton(g, close), ' '])),
            h('p', {}, presence(a.presence || 'offline'), ' ', a.presence || 'offline', a.in_call ? ' · in a call' : '', lastSeen(a) ? ' · ' + lastSeen(a) : ''))),
        statusLine(a) ? h('p', { class: 'note' }, statusLine(a)) : null,
        h('dl', { class: 'profile-facts' },
          a.title ? h('div', {}, h('dt', {}, 'Title'), h('dd', {}, a.title)) : null,
          a.pronouns ? h('div', {}, h('dt', {}, 'Pronouns'), h('dd', {}, a.pronouns)) : null,
          zone ? h('div', {}, h('dt', {}, 'Local time'), h('dd', {}, `${zone} (${a.time_zone})`)) : null),
        a.removed_at ? h('p', { class: 'note warn' }, 'This account has been removed.') : null,
      ],
      foot: me
        ? [h('a', { class: 'btn', href: '/account' }, 'Edit profile'), button('Set a status', () => { close(); openStatus(); }, 'primary')]
        : [a.role !== 'bot' && !a.removed_at && !blocked ? watch : null,
          a.role !== 'bot' && !a.removed_at && store.server.features.blocking ? block : null,
          a.role !== 'bot' && !a.removed_at && store.server.features.sealed_chats ? button('Sealed chat', () => startSealedChat(a.id)) : null,
          !a.removed_at ? report : null,
          !a.removed_at ? message : null],
    };
  });
}

const CLEARS = [['', 'Until I clear it'], ['1800', 'In 30 minutes'], ['3600', 'In an hour'], ['14400', 'In 4 hours'], ['86400', 'In a day'], ['604800', 'In a week']];

// openStatus sets or clears one's own status: an emoji, a line of text,
// when it clears, and the two switches. Presets, with "save as preset".
export async function openStatus() {
  let presets = [];
  try { presets = (await get('/status-presets')).presets; } catch { /* the picker still works without them */ }
  const s = store.me.status || {};
  const run = async (fn, said) => { try { await fn(); await refreshMe(); if (said) toast(said); return true; } catch (err) { if (!err.silent) toast(err.message, true); return false; } };
  await openDialog('Your status', (close) => {
    const emoji = h('input', { value: s.emoji || '', maxLength: 40, 'aria-label': 'Emoji', placeholder: '🌴', class: 'status-emoji' });
    const text = h('input', { value: s.text || '', maxLength: 100, 'aria-label': 'Status text', placeholder: 'What are you up to?' });
    const clears = h('select', { 'aria-label': 'Clears' }, CLEARS.map(([v, t]) => h('option', { value: v }, t)));
    const pause = h('input', { type: 'checkbox', checked: !!s.pause_notifications });
    const away = h('input', { type: 'checkbox', checked: !!s.appear_away });
    const body = () => ({ emoji: emoji.value.trim() || undefined, text: text.value.trim() || undefined, duration_seconds: clears.value ? Number(clears.value) : undefined, pause_notifications: pause.checked, appear_away: away.checked });
    const av = store.me.availability;
    return {
      body: [
        presets.length ? h('div', { class: 'stack tight' }, h('span', { class: 'label' }, 'Presets'),
          h('div', { class: 'row' }, presets.map((p) => button(`${p.emoji || ''} ${p.text}`.trim(), async () => { if (await run(() => put('/me/status', { preset_id: p.id }), 'Status set.')) close(); }, 'small')))) : null,
        h('div', { class: 'row' }, emoji, h('div', { class: 'grow' }, text)),
        h('label', { class: 'field' }, h('span', {}, 'Clears'), clears),
        check('Pause notifications', pause, 'You are in do not disturb while this status is set.'),
        check('Appear away', away),
        s.until ? h('p', { class: 'dim small' }, 'Your current status clears ' + dateTime(s.until) + '.') : null,
        h('hr', {}),
        h('div', { class: 'stack tight' }, h('span', { class: 'label' }, 'Availability'),
          h('p', {}, av ? `${av.value === 'meeting' ? 'In a meeting' : 'Out of office'}${av.until ? ' until ' + dateTime(av.until) : ''}${av.set_by === 'integration' ? ' (set by an integration)' : ''}` : 'Nothing set.'),
          h('div', { class: 'row' },
            button('In a meeting…', () => availability('meeting', close), 'small'), button('Out of office…', () => availability('out_of_office', close), 'small'),
            av ? button('Clear', async () => { if (await run(() => del('/me/availability'))) close(); }, 'small') : null)),
        h('div', { class: 'stack tight' }, h('span', { class: 'label' }, 'Appear away'),
          h('div', { class: 'row' }, store.me.appear_away && store.me.appear_away.on
            ? [h('span', {}, 'On' + (store.me.appear_away.until ? ' until ' + dateTime(store.me.appear_away.until) : '')), button('Turn off', async () => { if (await run(() => del('/me/away'))) close(); }, 'small')]
            : [button('For an hour', async () => { if (await run(() => put('/me/away', { duration_seconds: 3600 }))) close(); }, 'small'), button('Until I turn it off', async () => { if (await run(() => put('/me/away', {}))) close(); }, 'small')])),
      ],
      foot: [
        (s.text || s.emoji) ? button('Clear status', async () => { if (await run(() => del('/me/status'), 'Status cleared.')) close(); }) : null,
        button('Save as preset', async () => { const b = body(); if (!b.text) { toast('A preset needs some text.', true); return; } await run(() => post('/me/status-presets', { emoji: b.emoji, text: b.text, duration_seconds: b.duration_seconds, pause_notifications: b.pause_notifications, appear_away: b.appear_away }), 'Saved as a preset.'); }),
        button('Set status', async () => { if (await run(() => put('/me/status', body()), 'Status set.')) close(); }, 'primary'),
      ],
    };
  });
}

async function availability(value, closeParent) {
  const v = await formDialog(value === 'meeting' ? 'In a meeting' : 'Out of office', [
    { name: 'until', label: 'Until', type: 'datetime', value: new Date(Date.now() + (value === 'meeting' ? 3600e3 : 24 * 3600e3)) },
  ], { yes: 'Set', intro: value === 'meeting' ? 'Whether a meeting pauses your notifications is a preference on your account page.' : 'People who mention you are shown the date you are back.',
    submit: (x) => put('/me/availability', { value, until: x.until }) });
  if (v) { await refreshMe(); closeParent(); toast('Availability set.'); }
}

// openDnd: whether the person is in do not disturb now, why and until
// when; turning it on for a while, and off; and quiet hours.
export async function openDnd() {
  await refreshMe().catch(() => {});
  let quiet = null;
  try { quiet = (await get('/me/quiet-hours')).quiet_hours || null; } catch { /* shown as not set */ }
  await openDialog('Do not disturb', (close) => {
    const st = store.me.dnd_state || { on: false, sources: [] };
    const WHY = { manual: 'you turned it on', quiet: 'quiet hours', status: 'your status pauses notifications', meeting: 'you are in a meeting' };
    const set = async (bodyOrNull) => {
      try {
        if (bodyOrNull) await put('/me/dnd', bodyOrNull); else await del('/me/dnd');
        await refreshMe();
        close();
        toast(bodyOrNull ? 'Do not disturb is on.' : 'You are available now.');
      } catch (err) { if (!err.silent) toast(err.message, true); }
    };
    const tomorrow = () => { const d = new Date(); d.setDate(d.getDate() + 1); d.setHours(8, 0, 0, 0); return d.toISOString(); };
    const at = h('input', { type: 'datetime-local', 'aria-label': 'Until', value: localInput(new Date(Date.now() + 2 * 3600e3)) });
    const start = h('input', { type: 'time', value: quiet ? quiet.start : '22:00', 'aria-label': 'Quiet hours start' });
    const end = h('input', { type: 'time', value: quiet ? quiet.end : '07:00', 'aria-label': 'Quiet hours end' });
    const saveQuiet = button(quiet ? 'Save' : 'Turn on', () => busy(saveQuiet, async () => { await put('/me/quiet-hours', { start: start.value, end: end.value, days: quiet ? quiet.days : '1111111' }); await refreshMe(); close(); toast('Quiet hours saved.'); }), 'small');
    return {
      body: [
        h('p', { class: 'note' + (st.on ? ' warn' : '') }, st.on
          ? [icon('moon'), ` You are in do not disturb because ${(st.sources || []).map((x) => WHY[x] || x).join(', and ')}${st.until || st.manual_until ? ', until ' + dateTime(st.until || st.manual_until) : ''}.`]
          : 'You are available. Nothing is holding notifications back.'),
        st.on ? h('p', { class: 'dim small' }, 'Turning it off makes you available now, whatever the cause: it clears a manual do not disturb, the pause of your status, the current quiet period and the pause from the current meeting.') : null,
        h('div', { class: 'stack tight' }, h('span', { class: 'label' }, 'Turn on for'),
          h('div', { class: 'row' }, button('30 minutes', () => set({ duration_seconds: 1800 }), 'small'), button('An hour', () => set({ duration_seconds: 3600 }), 'small'),
            button('Until tomorrow morning', () => set({ until: tomorrow() }), 'small'), button('Until I turn it off', () => set({}), 'small')),
          h('div', { class: 'row' }, h('div', { class: 'grow' }, at), button('Until then', () => { const v = fromLocalInput(at.value); if (v) set({ until: v }); }, 'small'))),
        h('p', { class: 'dim small' }, 'While it is on nothing notifies you and badges keep counting. People see the mark, and may be offered “notify anyway” if you allow it.'),
        h('hr', {}),
        h('div', { class: 'stack tight' }, h('span', { class: 'label' }, 'Quiet hours'),
          h('p', { class: 'dim small' }, quiet ? `Every night from ${quiet.start} to ${quiet.end} (${quiet.time_zone}).${quiet.skip_until ? ' Skipped until ' + dateTime(quiet.skip_until) + '.' : ''}` : 'Not set. During quiet hours you are in do not disturb.'),
          h('div', { class: 'row' }, start, h('span', {}, 'to'), end, saveQuiet,
            quiet && !quiet.skip_until ? button('Skip tonight', async () => { try { await put('/me/quiet-hours/skip'); await refreshMe(); close(); toast('The next quiet period is skipped.'); } catch (err) { toast(err.message, true); } }, 'small') : null,
            quiet ? button('Turn off', async () => { try { await del('/me/quiet-hours'); await refreshMe(); close(); } catch (err) { toast(err.message, true); } }, 'small') : null),
          h('p', { class: 'dim small' }, 'The days, and your time zone, are on your ', h('a', { href: '/account/notifications' }, 'account page'), '.')),
      ],
      foot: [st.on ? button('Turn off now', () => set(null), 'primary') : null, button('Close', () => close())],
    };
  });
}

