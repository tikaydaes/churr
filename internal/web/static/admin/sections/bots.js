// Bots: create (name, picture), tokens, the channels a bot is in, its
// commands, remove.

import { get, post, patch, del, upload, all } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, openDialog, avatar, fill, dateTime, ago } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog, secretDialog } from '/shared/form.js';

export default async function botsSection(ctx) {
  const bots = (await all('/admin/bots', 'bots')).filter((b) => !b.webhook_only);
  const channelNames = new Map();
  try { for (const c of await all('/admin/channels', 'channels')) channelNames.set(c.id, c.name); } catch { /* names fall back */ }

  const tokens = (b) => openDialog(`Tokens of @${b.account.username}`, (close) => {
    const list = h('ul', { class: 'rows' });
    const load = async () => {
      const { tokens: ts } = await get(`/admin/bots/${b.account.id}/tokens`);
      fill(list, ts.length ? ts.map((t) => {
        const revoke = button('Revoke', () => busy(revoke, async () => { await del(`/admin/bots/${b.account.id}/tokens/${t.id}`); load(); }), 'small danger');
        return h('li', {}, h('div', { class: 'what' }, h('strong', {}, t.label || 'Token'), h('small', {}, `Made ${dateTime(t.created_at)} · last used ${ago(t.last_seen_at)}`)), h('div', { class: 'does' }, revoke));
      }) : h('li', { class: 'dim' }, 'No live token. The bot cannot connect.'));
    };
    load().catch((err) => fill(list, h('li', {}, err.message)));
    const make = button('New token', async () => {
      const v = await formDialog('New bot token', [{ name: 'label', label: 'What it is for', maxLength: 60, placeholder: 'Production' }],
        { yes: 'Make the token', submit: (x) => post(`/admin/bots/${b.account.id}/tokens`, { label: x.label || undefined }) });
      if (v) { await secretDialog('Bot token', [['Token', v.secret]]); load(); }
    }, 'primary');
    return { body: [h('p', { class: 'dim' }, 'A bot token does not expire. At most ten live tokens per bot.'), list], foot: [make, button('Done', () => close(true))] };
  });

  const add = button('New bot', async () => {
    const out = await formDialog('New bot', [
      { name: 'username', label: 'Username', required: true, code: true, minLength: 2, maxLength: 32 },
      { name: 'display_name', label: 'Display name', required: true, maxLength: 64 },
      { name: 'description', label: 'What it does', maxLength: 200 },
    ], { yes: 'Create', intro: 'A bot is an account with a token. It is in no channel until someone adds it, and it sees nothing outside the channels it is in.',
      submit: (v) => post('/admin/bots', { username: v.username, display_name: v.display_name, description: v.description || undefined }) });
    if (out) { await secretDialog('The bot’s token', [['Token for @' + out.bot.account.username, out.secret]]); ctx.redraw(); }
  }, 'small primary');

  return card(h('span', { class: 'row grow between' }, 'Bots', add),
    h('p', { class: 'dim' }, 'Creating a bot or a token needs a fresh check. A channel’s managers add a bot to it like any member.'),
    bots.length ? h('ul', { class: 'rows' }, bots.map((b) => {
      const a = b.account;
      const edit = button('Change', async () => {
        const out = await formDialog('Change @' + a.username, [
          { name: 'username', label: 'Username', required: true, code: true, value: a.username, maxLength: 32 },
          { name: 'display_name', label: 'Display name', required: true, value: a.display_name, maxLength: 64 },
          { name: 'description', label: 'What it does', value: b.description, maxLength: 200 },
          { name: 'file', label: 'New picture', type: 'file', accept: 'image/png,image/jpeg,image/webp,image/gif' },
        ], { submit: async (v) => {
          await patch('/admin/bots/' + a.id, { username: v.username, display_name: v.display_name, description: v.description });
          if (v.file) { const body = new FormData(); body.append('file', v.file); await upload(`/admin/bots/${a.id}/avatar`, body, null, 'PUT').promise; }
        } });
        if (out) ctx.redraw();
      }, 'small');
      const remove = button('Remove', async () => {
        if (!(await confirmDialog(`Remove @${a.username}?`, 'Its tokens end and it leaves every channel. Its messages stay.', { yes: 'Remove', danger: true }))) return;
        await busy(remove, async () => { await del('/admin/bots/' + a.id); toast('Removed.'); ctx.redraw(); });
      }, 'small danger');
      const chans = (b.channel_ids || []).map((id) => channelNames.get(id) || 'a private channel');
      return h('li', {}, avatar(a),
        h('div', { class: 'what' }, h('strong', {}, a.display_name, ' ', h('small', { class: 'dim' }, '@' + a.username), ' ', h('span', { class: 'tag ' + (a.presence === 'online' ? 'green' : 'line') }, a.presence === 'online' ? 'connected' : 'not connected')),
          b.description && h('small', {}, b.description),
          h('small', {}, chans.length ? 'In ' + chans.join(', ') : 'In no channel yet'),
          (b.commands || []).length ? h('small', {}, 'Commands: ', b.commands.map((c) => h('code', { title: c.description }, '/' + c.name + ' '))) : null),
        h('div', { class: 'does' }, button('Tokens', () => tokens(b), 'small'), edit, remove));
    })) : h('p', { class: 'empty' }, 'No bots.'));
}
