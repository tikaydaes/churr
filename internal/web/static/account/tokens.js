// Personal tokens: credentials for one's own scripts (docs/AUTH.md section
// 12.1). The secret is shown once, when the token is made.

import { get, post, del } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, dateTime, ago } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';
import { formDialog, secretDialog } from '/shared/form.js';

export default async function tokens(ctx) {
  const el = card('Personal tokens');
  await draw(el, ctx);
  return el;
}

async function draw(el, ctx) {
  const redraw = () => draw(el, ctx);
  const got = await tryLoad(() => get('/me/tokens'));
  const guest = ctx.me.role === 'guest';
  const make = button('New token', async () => {
    const out = await formDialog('New personal token', [
      { name: 'label', label: 'What it is for', required: true, maxLength: 60, placeholder: 'Status script on my laptop' },
      { name: 'expires', label: 'Stops working', type: 'select', value: '2592000', options: [['2592000', 'In 30 days'], ['7776000', 'In 90 days'], ['31536000', 'In a year'], ['', 'Never']] },
    ], {
      yes: 'Make the token',
      intro: 'A personal token acts as you: the same channels, the same level in each. It cannot manage sign-in and cannot use sealed chats.',
      submit: (v) => post('/me/tokens', { label: v.label, expires_in_seconds: v.expires ? Number(v.expires) : undefined }),
    });
    if (!out) return;
    await secretDialog('Your new token', [['Token', out.secret]], 'This token is shown once. Copy it now; the server keeps only a hash of it.');
    redraw();
  }, 'small primary');
  make.disabled = guest;
  const rows = got.problem || (got.ok.tokens.length ? h('ul', { class: 'rows' }, got.ok.tokens.map((t) => {
    const revoke = button('Revoke', async () => {
      if (!(await confirmDialog('Revoke this token?', `“${t.label}” stops working at once.`, { yes: 'Revoke', danger: true }))) return;
      await busy(revoke, async () => { await del('/me/tokens/' + t.id); toast('Token revoked.'); redraw(); });
    }, 'small danger');
    return h('li', {}, h('div', { class: 'what' }, h('strong', {}, t.label),
      h('small', {}, `Made ${dateTime(t.created_at)} · last used ${ago(t.last_seen_at)} · ${t.expires_at ? 'ends ' + dateTime(t.expires_at) : 'does not expire'}`)),
    h('div', { class: 'does' }, revoke));
  })) : h('p', { class: 'empty' }, 'No personal tokens.'));
  el.replaceChildren(h('h2', {}, 'Personal tokens', make),
    h('p', { class: 'dim' }, 'For your own scripts, such as one that sets your status. Send it as ', h('code', {}, 'Authorization: Bearer churr_pat_…'), '.'),
    guest && h('p', { class: 'note' }, 'Guests cannot make personal tokens.') || '', rows);
}
