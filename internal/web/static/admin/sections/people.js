// Accounts: search and list; role (the owner alone changes roles),
// sign-in methods, groups, last seen; make a recovery link; end sessions;
// time out server-wide; remove; ban. Imported accounts that nobody has
// claimed get a claim link here.

import { get, post, put, del } from '/shared/api.js';
import { h, button, toast, confirmDialog, avatar, ago, day, debounce, openMenu, fill, dateTime } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog, secretDialog } from '/shared/form.js';
import { pager } from '../kit.js';

export default async function peopleSection(ctx) {
  const isOwner = ctx.me.role === 'owner';
  const groups = (await get('/groups')).groups;
  const groupName = (id) => { const g = groups.find((x) => x.id === id); return g ? '@' + g.handle : ''; };
  const q = h('input', { type: 'search', placeholder: 'Name or username', autocomplete: 'off' });
  const role = h('select', {}, [['', 'Any role'], ['owner', 'Owner'], ['admin', 'Admins'], ['member', 'Members'], ['guest', 'Guests'], ['bot', 'Bots']].map(([v, t]) => h('option', { value: v }, t)));
  const state = h('select', {}, [['active', 'Active'], ['unclaimed', 'Imported, not claimed'], ['removed', 'Removed']].map(([v, t]) => h('option', { value: v }, t)));
  const list = h('ul', { class: 'rows' });
  let pages;

  const act = async (fn, said) => {
    try { await fn(); if (said) toast(said); reload(); } catch (err) { if (!err.silent) toast(err.message, true); }
  };

  const menu = (a, anchor) => {
    const person = a.role !== 'bot' && a.role !== 'remote';
    const mayTouch = a.role !== 'owner' && (isOwner || a.role !== 'admin') && a.id !== ctx.me.id;
    openMenu(anchor, [
      { head: '@' + a.username },
      isOwner && person && a.id !== ctx.me.id && !a.removed_at && { label: 'Change role…', onclick: async () => {
        const out = await formDialog(`Role of @${a.username}`, [
          { name: 'role', label: 'Role', type: 'radio', value: a.role, options: [
            ['admin', 'Admin', 'Runs the server with you: settings, people, channels.'],
            ['member', 'Member', 'An ordinary account.'],
            ['guest', 'Guest', 'Sees only the channels they are added to.'],
            ['owner', 'Owner', 'Hands the server over. You become an admin.']] },
        ], { submit: async (v) => {
          if (v.role === 'owner' && !(await confirmDialog('Hand the server over?', `@${a.username} becomes the owner and you become an admin. Only they can hand it back.`, { type: a.username, yes: 'Hand it over', danger: true }))) throw Object.assign(new Error('Not changed.'), { silent: false });
          return put(`/admin/accounts/${a.id}/role`, { role: v.role });
        } });
        if (out) { toast('Role changed.'); if (out.account && out.account.role === 'owner') location.reload(); else reload(); }
      } },
      person && mayTouch && a.claimed !== false && !a.removed_at && { label: 'Make a recovery link…', onclick: async () => {
        if (!(await confirmDialog('Make a recovery link?', [`Whoever opens the link becomes @${a.username}, far enough to make a new passkey or access key. Give it to them and nobody else.`, 'It works once, for 24 hours. Making it and using it are both written to the audit log, and their other sessions are told.'], { yes: 'Make the link' }))) return;
        try {
          const out = await post(`/admin/accounts/${a.id}/recovery-link`);
          await secretDialog('Recovery link', [['Link for @' + a.username, out.url]], `Shown once. It works until ${dateTime(out.expires_at)}.`);
        } catch (err) { if (!err.silent) toast(err.message, true); }
      } },
      state.value === 'unclaimed' && { label: 'Make a claim link…', onclick: async () => {
        try {
          const out = await post(`/admin/accounts/${a.id}/claim-link`);
          await secretDialog('Claim link', [['Link for @' + a.username, out.invite.url]], 'Whoever opens this link takes the account and its imported history. It replaces any earlier link and works for 30 days.');
        } catch (err) { if (!err.silent) toast(err.message, true); }
      } },
      person && mayTouch && a.has_totp && { label: 'Reset authenticator app', onclick: () => act(() => del(`/admin/accounts/${a.id}/totp`), 'They will enrol again at their next sign-in.') },
      mayTouch && !a.removed_at && { label: 'Sign out everywhere', onclick: () => act(() => del(`/admin/accounts/${a.id}/sessions`), 'Every session and token of the account is ended.') },
      person && mayTouch && !a.removed_at && { label: 'Time out…', onclick: async () => {
        const out = await formDialog(`Time out @${a.username}`, [
          { name: 'seconds', label: 'For', type: 'select', value: '3600', options: [['600', '10 minutes'], ['3600', 'An hour'], ['86400', 'A day'], ['604800', 'A week'], ['2419200', '28 days']] },
          { name: 'reason', label: 'Reason', maxLength: 200, hint: 'Shown to them and kept in the audit log.' },
        ], { yes: 'Time out', intro: 'On the whole server. They can still read, and cannot post, react, answer forms or join calls.',
          submit: (v) => post('/admin/timeouts', { account_id: a.id, seconds: Number(v.seconds), reason: v.reason || undefined }) });
        if (out) { toast('Timed out.'); reload(); }
      } },
      { sep: true },
      mayTouch && !a.removed_at && { label: 'Remove…', danger: true, onclick: async () => {
        if (!(await confirmDialog(`Remove @${a.username}?`, ['Their sessions, tokens and passkeys end and they leave every channel. Their messages stay.', 'They could be invited again. To stop that, exile them instead.'], { yes: 'Remove', danger: true }))) return;
        act(() => del('/admin/accounts/' + a.id), 'Removed.');
      } },
      mayTouch && !a.banned && { label: 'Exile…', danger: true, onclick: async () => {
        const out = await formDialog(`Exile @${a.username} from the server`, [{ name: 'reason', label: 'Reason', maxLength: 200 }],
          { yes: 'Exile', danger: true, intro: 'A removal from the whole server, plus a record that stops the same person coming back through the same company identity. Someone who registered with a passkey can only be recognised if they reuse an invite or identity.',
            submit: (v) => post('/admin/bans', { account_id: a.id, reason: v.reason || undefined }) });
        if (out) { toast('Exiled.'); reload(); }
      } },
    ], { alignRight: true, label: 'Actions for @' + a.username });
  };

  const draw = (accounts) => {
    for (const a of accounts) {
      const methods = [a.passkey_count ? `${a.passkey_count} passkey${a.passkey_count === 1 ? '' : 's'}` : null, a.has_access_key ? 'access key' : null, a.has_totp ? 'authenticator app' : null,
        a.provider_ids && a.provider_ids.length ? 'company sign-in' : null].filter(Boolean);
      const more = h('button', { type: 'button', class: 'btn small', onclick: (e) => menu(a, e.currentTarget) }, 'Actions');
      list.append(h('li', {}, avatar(a),
        h('div', { class: 'what' },
          h('strong', {}, a.display_name || a.username, ' ', h('span', { class: 'tag ' + (a.role === 'owner' ? 'pink' : a.role === 'admin' ? 'lavender' : 'line') }, a.role), ' ',
            a.banned && h('span', { class: 'tag red' }, 'exiled'), a.removed_at && h('span', { class: 'tag grey' }, 'removed'),
            !a.removed_at && a.has_access_key && !a.passkey_count && h('span', { class: 'tag yellow', title: 'Signs in with an access key and has no passkey' }, 'no passkey')),
          h('small', {}, '@' + a.username + (methods.length ? ' · signs in with ' + methods.join(', ') : a.role === 'bot' ? ' · a bot token' : ' · no way to sign in')),
          h('small', {}, `Last seen ${ago(a.last_seen_at)} · joined ${day(a.created_at)}` + ((a.group_ids || []).length ? ' · ' + a.group_ids.map(groupName).filter(Boolean).join(' ') : ''))),
        h('div', { class: 'does' }, more)));
    }
    if (!list.children.length) list.append(h('li', { class: 'dim' }, 'Nobody matches.'));
  };

  const reload = () => {
    fill(list);
    pages = pager(async (cursor) => {
      const out = await get('/admin/accounts', { q: q.value.trim(), role: role.value, state: state.value, limit: 50, cursor });
      return { items: out.accounts, next: out.next_cursor };
    }, draw);
    holder.replaceChildren(pages.node);
    pages.first().catch((err) => toast(err.message, true));
  };
  const holder = h('div', {});
  q.addEventListener('input', debounce(reload, 200));
  role.addEventListener('change', reload);
  state.addEventListener('change', reload);
  reload();
  return card(null,
    h('div', { class: 'filters' },
      h('label', { class: 'field' }, h('span', {}, 'Search'), q),
      h('label', { class: 'field narrow' }, h('span', {}, 'Role'), role),
      h('label', { class: 'field narrow' }, h('span', {}, 'Show'), state)),
    !isOwner && h('p', { class: 'dim small' }, 'Only the owner changes roles, and only the owner can act on an admin.'),
    list, holder);
}
