// Invites: create (role, uses, expiry, groups, and for guests the
// channels), list, revoke. Claim links for imported accounts are invites
// too and are listed here; they are made under People.

import { get, post, del, all } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, copyText, dateTime, fill } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { people, name, pager } from '../kit.js';

export default async function invites(ctx) {
  const [groups, channels] = await Promise.all([get('/groups').then((r) => r.groups), all('/admin/channels', 'channels').catch(() => [])]);
  const list = h('ul', { class: 'rows' });
  const holder = h('div', {});
  const reload = () => {
    fill(list);
    const p = pager(async (cursor) => {
      const out = await get('/invites', { limit: 50, cursor });
      await people(out.invites.flatMap((i) => [i.created_by_id, i.claims_account_id]));
      return { items: out.invites, next: out.next_cursor };
    }, (items) => {
      for (const i of items) {
        const over = i.revoked_at ? 'withdrawn' : (i.expires_at && new Date(i.expires_at) < new Date()) ? 'run out' : (i.max_uses && i.uses >= i.max_uses) ? 'used up' : null;
        const revoke = button('Withdraw', async () => {
          if (!(await confirmDialog('Withdraw this invite?', 'The link stops working. People who already joined with it stay.', { yes: 'Withdraw', danger: true }))) return;
          await busy(revoke, async () => { await del('/invites/' + i.id); reload(); });
        }, 'small danger');
        list.append(h('li', {},
          h('div', { class: 'what' },
            h('strong', {}, i.claims_account_id ? ['Claim link for ', name(i.claims_account_id)] : [i.role === 'guest' ? 'Guest invite' : 'Member invite'], ' ',
              h('span', { class: 'tag ' + (over ? 'grey' : 'green') }, over || 'usable')),
            h('code', { class: 'wrap' }, i.url),
            h('small', {}, `Used ${i.uses}${i.max_uses ? ' of ' + i.max_uses : ' times, no limit'} · ${i.expires_at ? 'until ' + dateTime(i.expires_at) : 'no expiry'} · made by ${name(i.created_by_id)} on ${dateTime(i.created_at)}`),
            (i.group_ids.length || i.conversation_ids.length) ? h('small', {},
              [i.group_ids.length ? 'Groups: ' + i.group_ids.map((id) => '@' + ((groups.find((g) => g.id === id) || {}).handle || '?')).join(' ') : null,
                i.conversation_ids.length ? 'Channels: ' + i.conversation_ids.map((id) => (channels.find((c) => c.id === id) || {}).name || '?').join(', ') : null].filter(Boolean).join(' · ')) : null),
          h('div', { class: 'does' }, !over && button('Copy link', () => copyText(i.url, 'Invite link copied'), 'small'), !over && revoke)));
      }
      if (!list.children.length) list.append(h('li', { class: 'dim' }, 'No invites yet.'));
    });
    holder.replaceChildren(p.node);
    p.first().catch((err) => toast(err.message, true));
  };
  const make = button('New invite', async () => {
    const usable = groups.filter((g) => !g.builtin && !g.provider_id);
    const out = await formDialog('New invite', [
      { name: 'role', label: 'They join as', type: 'radio', value: 'member', options: [['member', 'A member', 'Lands in the channels @everyone is in.'], ['guest', 'A guest', 'Sees only the channels you tick below.']] },
      { name: 'max_uses', label: 'How many people may use it', type: 'number', value: 1, min: 0, max: 10000, hint: '0 means no limit.' },
      { name: 'expires', label: 'Stops working', type: 'select', value: '604800', options: [['86400', 'In a day'], ['604800', 'In a week'], ['2592000', 'In 30 days'], ['0', 'Never']] },
      { name: 'group_ids', label: 'Add them to groups', type: 'checks', options: usable.map((g) => [g.id, '@' + g.handle, g.name]), empty: 'There are no groups to add to.' },
      { name: 'conversation_ids', label: 'Channels for a guest', type: 'checks', options: channels.filter((c) => !c.archived_at).map((c) => [c.id, c.name, c.private ? 'private' : 'public']),
        when: (v) => v.role === 'guest', hint: 'A guest invite must name at least one.', empty: 'There are no channels.' },
    ], { yes: 'Make the invite', wide: true, submit: (v) => post('/invites', {
      role: v.role, max_uses: v.max_uses, expires_in_seconds: Number(v.expires) || undefined,
      group_ids: v.group_ids.length ? v.group_ids : undefined, conversation_ids: v.role === 'guest' ? v.conversation_ids : undefined,
    }) });
    if (out) { copyText(out.invite.url, 'Invite made and its link copied'); reload(); }
  }, 'small primary');
  reload();
  return card(h('span', { class: 'row grow between' }, 'Invite links', make),
    h('p', { class: 'dim' }, `Registration is “${ctx.server.sign_in.registration}”. An invite link can be copied again at any time until it is used up, runs out or is withdrawn.`),
    list, holder);
}
