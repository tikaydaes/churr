// Every browser and app signed in to the account, the devices they are,
// and "Add device", which shows the pairing QR code (docs/AUTH.md sections
// 10.5 and 11.4).

import { api, get, post, del } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, openDialog, dateTime, ago, until } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';
import { qrSvg } from '/shared/qr.js';

const HOW = { passkey: 'a passkey', oidc: 'company sign-in', saml: 'company sign-in', recovery_code: 'a recovery code', recovery_link: 'a recovery link', pairing: 'pairing from a browser' };
const KIND = { web: 'Browser', app: 'App', personal: 'Personal token', bot: 'Bot token' };

export default async function sessions(ctx) {
  const list = card('Signed in');
  const devices = card('Devices');
  await Promise.all([drawSessions(list, ctx), drawDevices(devices)]);
  return [list, devices];
}

async function drawSessions(el, ctx) {
  const redraw = () => drawSessions(el, ctx);
  const got = await tryLoad(() => get('/me/sessions'));
  const others = button('Sign out everywhere else', async () => {
    if (!(await confirmDialog('Sign out everywhere else?', 'Every other browser and app is signed out at once. Personal tokens too.', { yes: 'Sign them out', danger: true }))) return;
    await busy(others, async () => { await del('/me/sessions'); toast('Every other session is signed out.'); redraw(); });
  }, 'small danger');
  const rows = got.problem || h('ul', { class: 'rows' }, got.ok.sessions.map((s) => {
    const end = button(s.current ? 'Sign out' : 'End', async () => {
      if (s.current) {
        // What the web chat kept in this browser goes with the session.
        try { localStorage.removeItem('churr.' + ctx.me.id); } catch { /* nothing kept */ }
        await api('POST', '/logout', {});
        location.assign('/login');
        return;
      }
      await busy(end, async () => { await del('/me/sessions/' + s.id); redraw(); });
    }, 'small' + (s.current ? '' : ' danger'));
    return h('li', {},
      h('div', { class: 'what' },
        h('strong', {}, s.label || KIND[s.kind] || s.kind, ' ', s.current && h('span', { class: 'tag cyan' }, 'this one'), ' ',
          s.recovery && h('span', { class: 'tag yellow' }, 'recovery'), s.pending_second_factor && h('span', { class: 'tag yellow' }, 'waiting for second step')),
        h('small', {}, `${KIND[s.kind] || s.kind}${s.signed_in_by ? ' · signed in with ' + (HOW[s.signed_in_by] || s.signed_in_by) : ''}${s.second_factor ? ' and ' + s.second_factor : ''}`),
        h('small', {}, `Since ${dateTime(s.created_at)} · last seen ${ago(s.last_seen_at)}${s.expires_at ? ' · ends ' + dateTime(s.expires_at) : ''}`)),
      h('div', { class: 'does' }, end));
  }));
  el.replaceChildren(h('h2', {}, 'Signed in', others),
    h('p', { class: 'dim' }, 'A session you do not recognise should be ended, and your passkeys checked.'), rows);
}

async function drawDevices(el) {
  const redraw = () => drawDevices(el);
  const got = await tryLoad(() => get('/me/devices'));
  const add = button('Add device', () => pair().then(redraw), 'small primary');
  const rows = got.problem || (got.ok.devices.length ? h('ul', { class: 'rows' }, got.ok.devices.map((d) => {
    const remove = button('Remove', async () => {
      if (!(await confirmDialog('Remove this device?', `“${d.name}” is signed out and taken out of your sealed chats.`, { yes: 'Remove', danger: true }))) return;
      await busy(remove, async () => { await del('/me/devices/' + d.id); redraw(); });
    }, 'small danger');
    return h('li', {},
      h('div', { class: 'what' },
        h('strong', {}, d.name, ' ', h('span', { class: 'tag line' }, d.platform), ' ', d.current && h('span', { class: 'tag cyan' }, 'this one'), ' ',
          d.platform === 'web' && h('span', { class: 'tag yellow', title: 'A browser runs whatever code the server sends each time' }, 'browser'),
          d.retired_at && h('span', { class: 'tag grey' }, 'retired')),
        h('small', {}, `Added ${dateTime(d.created_at)} · last seen ${ago(d.last_seen_at)}`)),
      !d.retired_at && h('div', { class: 'does' }, remove));
  })) : h('p', { class: 'empty' }, 'No device is registered. Apps register themselves when they sign in.'));
  el.replaceChildren(h('h2', {}, 'Devices', add),
    h('p', { class: 'dim' }, 'To sign a phone in, choose Add device and scan the code with the Churr app.'), rows);
}

// pair shows the pairing code as a QR code, waits for an app to claim it,
// and asks the person to confirm the device by name. The confirmation is
// the defence against someone who photographed the screen, so it cannot be
// skipped.
async function pair() {
  let started;
  try { started = await post('/oauth/pair'); } catch (err) { if (!err.silent) toast(err.message, true); return; }
  let timer;
  await openDialog('Add a device', (close) => {
    const state = h('div', { class: 'stack' });
    const waiting = () => state.replaceChildren(
      h('p', {}, 'Open the Churr app on the new device, choose “Scan a code”, and point it at this.'),
      h('div', { class: 'qr' }, qrSvg(started.qr_text, 'Pairing QR code')),
      h('p', { class: 'dim small' }, `This code works once, for ${until(started.expires_at)}. Anyone who scans it is named here before anything happens.`),
      h('p', { class: 'loading' }, 'Waiting for a device'));
    waiting();
    const poll = async () => {
      let p;
      try { p = await get('/oauth/pair/' + encodeURIComponent(started.pairing_code)); } catch (err) {
        state.replaceChildren(h('p', { class: 'note warn' }, err.code === 'not_found' ? 'The code ran out. Close this and choose Add device again.' : err.message));
        return;
      }
      if (p.state === 'claimed') {
        const yes = button('Sign this device in', () => busy(yes, async () => {
          await post('/oauth/pair/' + encodeURIComponent(started.pairing_code) + '/confirm');
          toast('The device is signed in.');
          close(true);
        }), 'primary');
        state.replaceChildren(
          h('p', {}, 'A device called ', h('strong', {}, p.device_name || 'unnamed'), p.platform ? [' ', h('span', { class: 'tag line' }, p.platform)] : '', ' wants to sign in as you.'),
          h('p', { class: 'note warn' }, 'Only go on if that is the device in your hand. If it is not, close this: nothing has been signed in.'),
          h('p', { class: 'row' }, yes, button('It is not mine', () => close(false))));
        return;
      }
      if (p.state === 'confirmed') { close(true); return; }
      timer = setTimeout(poll, 2000);
    };
    timer = setTimeout(poll, 2000);
    return { body: state, foot: [button('Close', () => close(false))] };
  });
  clearTimeout(timer);
}
