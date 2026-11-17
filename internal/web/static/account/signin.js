// Passkeys (with whether each can unlock sealed history), the access key,
// recovery codes, the authenticator app, and company sign-in identities. Every change here
// needs a fresh check, which the shared dialog asks for when the server
// says so (docs/AUTH.md section 10.4).

import { get, post, put, patch, del } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, promptDialog, openDialog, copyText, dateTime, plural, check } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';
import { createPasskey, passkeyName, passkeysSupported } from '/shared/passkey.js';
import { totpEnrol } from '/shared/door.js';
import { freshCheck } from '/shared/fresh.js';
import { keepKey, makeKey, keyAuth, keyUnlockPublic, allowNewKey } from '/shared/accesskeyui.js';

export default async function signIn(ctx) {
  const passkeys = card('Passkeys');
  const accessKey = card('Access key');
  const codes = card('Recovery codes');
  const totp = card('Authenticator app');
  const identities = card('Company sign-in');
  ctx.redrawAccessKey = () => drawAccessKey(accessKey, ctx);
  ctx.redrawPasskeys = () => drawPasskeys(passkeys, ctx);
  await Promise.all([drawPasskeys(passkeys, ctx), drawAccessKey(accessKey, ctx), drawCodes(codes), drawTotp(totp, ctx), drawIdentities(identities, ctx)]);
  return [passkeys, accessKey, codes, totp, identities];
}

async function drawPasskeys(el, ctx) {
  const redraw = () => drawPasskeys(el, ctx);
  const got = await tryLoad(() => get('/me/passkeys'));
  const add = button('Add a passkey', () => busy(add, async () => {
    const begin = await post('/me/passkeys/begin');
    const made = await createPasskey(begin.public_key);
    const out = await post('/me/passkeys/finish', { ceremony_id: begin.ceremony_id, credential: made.credential, passkey_name: passkeyName(), prf_enabled: made.prfEnabled });
    const fresh = out.sign_in && out.sign_in.recovery_codes;
    if (fresh && fresh.length) await showCodes(fresh);
    toast('Passkey added.');
    redraw();
    if (ctx.redrawAccessKey) ctx.redrawAccessKey();
  }), 'primary small');
  add.disabled = !passkeysSupported();
  const list = got.problem || h('ul', { class: 'rows' }, got.ok.passkeys.map((p) => {
    const rename = button('Rename', async () => {
      const name = await promptDialog('Rename this passkey', 'Name', { value: p.name, maxLength: 60 });
      if (name == null) return;
      await busy(rename, async () => { await patch('/me/passkeys/' + p.id, { name }); redraw(); });
    }, 'small');
    const remove = button('Remove', async () => {
      const last = got.ok.passkeys.length === 1;
      const yes = await confirmDialog('Remove this passkey?', [
        `“${p.name}” will no longer sign you in.`,
        last && 'It is your only passkey. The server refuses to remove the last one unless you have an access key or a linked company identity.',
        (ctx.me.role === 'owner' || ctx.me.role === 'admin') && got.ok.passkeys.length <= 2 && 'Owners and admins should keep at least two.',
      ].filter(Boolean), { yes: 'Remove', danger: true });
      if (yes) await busy(remove, async () => { await del('/me/passkeys/' + p.id); toast('Passkey removed.'); redraw(); });
    }, 'small danger');
    // The PRF marker: what the browser reported when the passkey was made,
    // and whether a locked copy of the backup key exists for it.
    const marker = p.can_unlock
      ? h('span', { class: 'tag green', title: 'This passkey can unlock sealed chat history' }, 'unlocks sealed chats')
      : p.has_prf
        ? h('span', { class: 'tag cyan', title: 'This passkey supports PRF. It can unlock sealed chats once a device that is already unlocked approves it.' }, 'PRF · not yet unlocking')
        : h('span', { class: 'tag line', title: 'This passkey did not report PRF when it was made, and that cannot be switched on later.' }, 'signs in · cannot unlock sealed chats');
    return h('li', {},
      h('div', { class: 'what' }, h('strong', {}, p.name), h('div', {}, marker),
        h('small', {}, `Made ${dateTime(p.created_at)} · last used ${p.last_used_at ? dateTime(p.last_used_at) : 'never'}`)),
      h('div', { class: 'does' }, rename, remove));
  }));
  el.replaceChildren(h('h2', {}, 'Passkeys', add),
    h('p', { class: 'dim' }, 'A passkey signs you in with a device’s fingerprint, face or PIN. Keep more than one, on more than one device.'),
    !passkeysSupported() && h('p', { class: 'note warn' }, 'This browser cannot use passkeys.') || '', list);
}

function showCodes(list) {
  return openDialog('Your recovery codes', (close) => {
    const tick = h('input', { type: 'checkbox', onchange: () => { done.disabled = !tick.checked; } });
    const done = button('Done', () => close(true), 'primary');
    done.disabled = true;
    return {
      body: [
        h('p', {}, 'Each code gets you back in once if you lose every passkey. They are shown now and never again.'),
        h('ul', { class: 'codes', 'aria-label': 'Recovery codes' }, list.map((c) => h('li', {}, c))),
        h('p', { class: 'row' }, button('Copy them', () => copyText(list.join('\n'), 'Recovery codes copied'), 'small')),
        check('I have saved these codes', tick),
      ],
      foot: [done],
    };
  });
}

// newKeyDialog makes an access key in this page, shows it once, and sends
// the server the value derived from it when the person has confirmed it.
// The key itself never leaves the page.
async function newKeyDialog(ctx, replacing) {
  const key = await makeKey();
  return openDialog(replacing ? 'Your new access key' : 'Your access key', (close) => {
    const problem = h('p', { class: 'problem', role: 'alert', hidden: true });
    problem.show = (err) => { problem.hidden = !err; problem.textContent = err ? err.message || 'Something went wrong.' : ''; };
    return [
      replacing && h('p', {}, 'Your old access key stops working as soon as this one is saved.'),
      problem,
      keepKey(key, { username: ctx.me.username, problem, confirmLabel: 'Save this key', onConfirm: async () => {
        // The server may ask for a fresh check first; api.js asks and
        // repeats the request.
        await put('/me/access-key', { access_key_auth: await keyAuth(key), unlock_public: await keyUnlockPublic(key.random) });
        // An unlocked browser lets the new key unlock sealed chats.
        await allowNewKey(key.random);
        close(true);
      } }),
    ];
  });
}

async function drawAccessKey(el, ctx) {
  const redraw = () => drawAccessKey(el, ctx);
  const got = await tryLoad(() => get('/me/access-key'));
  const k = got.ok && got.ok.access_key;
  const may = got.ok && got.ok.may_create;
  const make = may && button(k ? 'Make a new access key' : 'Make an access key', async () => {
    if (k && !(await confirmDialog('Make a new access key?', 'Your access key stops working at once. The new one is shown once.', { yes: 'Make a new key' }))) return;
    await busy(make, async () => {
      if (await newKeyDialog(ctx, !!k)) { toast(k ? 'New access key saved. The old one no longer works.' : 'Access key saved.'); redraw(); }
    });
  }, 'small' + (k ? '' : ' primary'));
  const remove = k && button('Remove', async () => {
    if (!(await confirmDialog('Remove your access key?', ['It will no longer sign you in.', 'The server refuses unless you have a passkey or a linked company identity: an account always keeps a way in.'], { yes: 'Remove', danger: true }))) return;
    await busy(remove, async () => { await del('/me/access-key'); toast('Access key removed.'); redraw(); if (ctx.redrawPasskeys) ctx.redrawPasskeys(); });
  }, 'small danger');
  el.replaceChildren(h('h2', {}, 'Access key', h('span', { class: 'row' }, make || '', remove || '')),
    got.problem || (k
      ? h('p', {}, 'You have an access key. It was made ', dateTime(k.created_at), k.last_used_at ? ['; last used ', dateTime(k.last_used_at)] : '; never used yet', '. Nobody can show it again, this server included: if it is lost, make a new one.')
      : h('p', {}, 'No access key. An access key is a long code made for you, for signing in where a passkey cannot be used. You keep it, in a password manager, and type it on each device.')),
    got.ok && !may && h('p', { class: 'note' }, 'This server no longer makes new access keys. Use a passkey.') || '');
}

async function drawCodes(el) {
  const got = await tryLoad(() => get('/me/recovery-codes'));
  const make = button('Make a new set', async () => {
    if (!(await confirmDialog('Make new recovery codes?', 'The codes you have now stop working at once. Eight new ones are shown once.', { yes: 'Make new codes' }))) return;
    await busy(make, async () => {
      const out = await post('/me/recovery-codes');
      await showCodes(out.recovery_codes);
      drawCodes(el);
    });
  }, 'small');
  el.replaceChildren(h('h2', {}, 'Recovery codes', make),
    got.problem || h('p', {}, h('strong', {}, `${got.ok.remaining} of 8`), ' left. A recovery code gets you in far enough to make a new passkey, and nothing else.'),
    got.ok && got.ok.remaining <= 2 && h('p', { class: 'note warn' }, 'You are nearly out. Make a new set.') || '');
}

async function drawTotp(el, ctx) {
  const redraw = () => drawTotp(el, ctx);
  const got = await tryLoad(() => get('/me/totp'));
  const enabled = got.ok && got.ok.totp.enabled;
  const setUp = button(enabled ? 'Replace' : 'Set up', () => busy(setUp, async () => {
    const enrolment = await post('/me/totp');
    await openDialog('Set up an authenticator app', (close) => [totpEnrol(enrolment, () => { close(true); toast('Authenticator app set up.'); })]);
    redraw();
  }), 'small' + (enabled ? '' : ' primary'));
  const remove = enabled && button('Remove', async () => {
    if (!(await confirmDialog('Remove the authenticator app?', 'Codes from it will no longer be accepted. The server refuses if that would leave you with no second step where one is required.', { yes: 'Remove', danger: true }))) return;
    await busy(remove, async () => { await del('/me/totp'); toast('Authenticator app removed.'); redraw(); });
  }, 'small danger');
  el.replaceChildren(h('h2', {}, 'Authenticator app', h('span', { class: 'row' }, setUp, remove || '')),
    got.problem || h('p', {}, enabled
      ? ['Set up', got.ok.totp.confirmed_at ? ' on ' + dateTime(got.ok.totp.confirmed_at) : '', '. Its six-digit codes count as a second step after company sign-in, and as a fresh check.']
      : 'Not set up. An authenticator app gives six-digit codes that count as a second step after company sign-in, and as a fresh check.'));
}

async function drawIdentities(el, ctx) {
  const got = await tryLoad(() => get('/me/identities'));
  const providers = ctx.server.sign_in.providers || [];
  // Linking sends the browser to the provider. It needs a fresh check, and
  // a page the browser is sent to cannot ask for one, so the address is
  // tried here first.
  const link = (p) => button('Link ' + p.name, async () => {
    const url = `/api/v1/sso/${encodeURIComponent(p.id)}/start?purpose=link&return_to=${encodeURIComponent('/account/sign-in')}`;
    const res = await fetch(url, { credentials: 'same-origin', redirect: 'manual' });
    if (res.status === 403) {
      const body = await res.json().catch(() => ({}));
      if (body.error && body.error.code === 'fresh_check_required') { if (!(await freshCheck())) return; } else { toast((body.error && body.error.message) || 'The server refused.', true); return; }
    }
    location.assign(url);
  }, 'small');
  const list = got.problem || (got.ok.identities.length
    ? h('ul', { class: 'rows' }, got.ok.identities.map((i) => h('li', {}, h('div', { class: 'what' }, h('strong', {}, i.provider_name),
      h('small', {}, `Linked ${dateTime(i.linked_at)} · last used ${i.last_used_at ? dateTime(i.last_used_at) : 'never'}`)))))
    : h('p', { class: 'empty' }, 'No company identity is linked to this account.'));
  el.replaceChildren(h('h2', {}, 'Company sign-in'),
    h('p', { class: 'dim' }, providers.length
      ? `This server offers ${plural(providers.length, 'provider')}. A linked identity signs you in through it.`
      : 'This server has no company sign-in provider. An admin adds them in the admin console.'),
    list, providers.length ? h('p', { class: 'row' }, providers.map(link)) : '');
}
