// Setting this browser up as a sealed-chat device (docs/SEALED.md sections
// 13 and 14): unlocking it with a passkey, the recovery phrase or approval
// from another device; making the backup the first time and showing the
// recovery phrase once; and, on a browser that is unlocked, approving
// another device by the code it shows. Used by the web chat and by the
// account page.

import { h, fill, button, toast, busy, openDialog, field, check, copyText } from '/shared/dom.js';
import { qrSvg } from '/shared/qr.js';
import { normalizeApprovalCode, approvalCode, decodeUnlockRequest, fromB64, platformLabel } from '/shared/sealed/index.js';

// Said wherever a passkey unlocks, and wherever the browser is compared
// with the apps (ruling R43).
export const R43 = 'A passkey works only at this server\'s own web address, so unlocking with a passkey always passes through a page this server supplies, in the apps as well as here. A dishonest server could serve a page that keeps the key.';
export const BROWSER = 'A browser loads its code from this server each time, so a dishonest or compromised server could send code that copies your keys, and you would not see it. The apps run code from their release instead; but unlocking with a passkey passes through a page this server supplies in the apps too. The other person sees that one of your devices is a browser.';

export function loadStyles() {
  if (document.querySelector('link[data-sealed-css]')) return;
  document.head.append(h('link', { rel: 'stylesheet', href: '/chat/sealed/sealed.css', dataset: { sealedCss: '1' } }));
}

// setupPanel says where this browser stands and offers the ways forward.
// onDone runs once the browser is unlocked.
export function setupPanel(s, onDone) {
  loadStyles();
  const el = h('div', { class: 'sealed-setup stack' }, h('p', { class: 'loading' }, 'Loading'));
  const draw = async () => {
    let backup;
    try { backup = await s.backup(); } catch (err) { fill(el, h('p', { class: 'problem' }, err.message)); return; }
    if (!backup) {
      const start = button('Set up sealed chats', () => busy(start, async () => {
        const phrase = await s.createBackup();
        await showPhraseOnce(phrase);
        onDone();
      }), 'primary');
      fill(el,
        h('h3', {}, 'Set up sealed chats on this account'),
        h('p', {}, 'This account has no sealed-chat keys yet. Setting up makes them in this browser, lists this browser as your first device, and gives you a recovery phrase of twelve words to write down.'),
        h('p', { class: 'dim small' }, BROWSER),
        h('p', { class: 'row' }, start));
      return;
    }
    const passkey = button('Unlock with a passkey', () => busy(passkey, async () => { await s.unlockWithPasskey(); toast('This browser is unlocked.'); onDone(); }), 'primary');
    const phrase = button('Use the recovery phrase', () => phraseDialog(s).then((ok) => ok && onDone()));
    const approve = button('Approve from another device', () => approvalDialog(s).then((ok) => ok && onDone()));
    const leftCopy = s.device && backup.locked_copies.some((c) => c.kind === 'device' && c.device_id === s.device.id && c.key_version === backup.key_version);
    const copy = leftCopy ? button('Use the key left for this browser', () => busy(copy, async () => { await s.unlockWithDeviceCopy(); onDone(); }), 'primary') : null;
    fill(el,
      h('h3', {}, 'Unlock this browser for sealed chats'),
      h('p', {}, 'This browser is not one of your sealed-chat devices yet. Unlock it once and it reads your sealed chats, earlier messages included, until you sign out here.'),
      copy && h('p', { class: 'note' }, 'An unlocked device or the unlock page left a key for this browser. ', copy),
      h('div', { class: 'sealed-ways' },
        h('div', { class: 'stack tight' }, passkey, h('small', { class: 'dim' }, 'One touch of a passkey that is allowed to unlock. ', R43)),
        h('div', { class: 'stack tight' }, phrase, h('small', { class: 'dim' }, 'The twelve words you wrote down when sealed chats were set up.')),
        h('div', { class: 'stack tight' }, approve, h('small', { class: 'dim' }, 'An unlocked device of yours types the code this browser shows.'))),
      h('p', { class: 'dim small' }, BROWSER));
  };
  draw();
  return el;
}

// showPhraseOnce shows the twelve words, the only time they are shown.
export function showPhraseOnce(phrase, title = 'Your recovery phrase') {
  loadStyles();
  return openDialog(title, (close) => {
    const tick = h('input', { type: 'checkbox', onchange: () => { done.disabled = !tick.checked; } });
    const done = button('Done', () => close(true), 'primary');
    done.disabled = true;
    return {
      body: [
        h('p', {}, 'Write these twelve words down, in order, and keep them somewhere safe. With them you can unlock your sealed chats on a new device when no other device is at hand. They are shown now and never again, and this server never sees them.'),
        h('ol', { class: 'sealed-phrase', 'aria-label': 'Recovery phrase' }, phrase.split(' ').map((w) => h('li', {}, w))),
        h('p', { class: 'row' }, button('Copy them', () => copyText(phrase, 'Recovery phrase copied'), 'small')),
        check('I have written these words down', tick),
      ],
      foot: [done],
    };
  });
}

// phraseDialog asks for the twelve words and unlocks with them.
export function phraseDialog(s) {
  return openDialog('Unlock with the recovery phrase', (close) => {
    const input = h('textarea', { rows: 3, autocomplete: 'off', spellcheck: false, autocapitalize: 'none', 'aria-label': 'Recovery phrase' });
    const problem = h('p', { class: 'problem', hidden: true });
    const go = h('button', { type: 'submit', class: 'btn primary' }, 'Unlock');
    go.setAttribute('form', 'sealed-phrase-form');
    const form = h('form', { class: 'stack', id: 'sealed-phrase-form', onsubmit: (e) => {
      e.preventDefault();
      problem.hidden = true;
      busy(go, async () => {
        try { await s.unlockWithPhrase(input.value); toast('This browser is unlocked.'); close(true); } catch (err) {
          problem.textContent = /recovery phrase/.test(err.message) ? 'That is not your recovery phrase. Check each word; the first four letters of each are enough.' : err.message;
          problem.hidden = false;
        }
      });
    } }, field('The twelve words, in order', input, 'Separated by spaces. The first four letters of each word are enough.'), problem);
    return { body: form, foot: [button('Cancel', () => close(false)), go] };
  });
}

// approvalDialog posts an unlock request and shows its code, as text and
// as a QR code, until an unlocked device of the account answers.
export function approvalDialog(s) {
  loadStyles();
  return openDialog('Approve from another device', (close) => {
    const body = h('div', { class: 'stack' }, h('p', { class: 'loading' }, 'Asking'));
    let stop = false;
    let off = null;
    (async () => {
      try {
        const { id, code } = await s.requestApproval();
        fill(body,
          h('p', {}, 'On a device of yours that is already unlocked, open sealed chats and approve the new device by typing this code. It works for ten minutes.'),
          h('p', { class: 'sealed-code', 'aria-label': 'Code: ' + code.split('').join(' ') }, code),
          h('div', { class: 'qr' }, qrSvg(code, 'The code as a QR code')),
          h('p', { class: 'dim small' }, 'Only someone with this code can approve this browser. Check that the device you approve on shows the same 20 characters if it shows them.'),
          h('p', { class: 'loading' }, 'Waiting for the other device'));
        const check = async () => {
          if (stop) return;
          try { if (await s.finishApproval(id)) { stop = true; toast('This browser is unlocked.'); close(true); return; } } catch (err) { if (!stop) { stop = true; fill(body, h('p', { class: 'problem' }, err.message)); } return; }
          setTimeout(check, 2000);
        };
        off = s.on((w) => { if (w && w.event && w.event.type === 'unlock_request.answered') check(); });
        setTimeout(check, 1500);
      } catch (err) { fill(body, h('p', { class: 'problem' }, err.message)); }
    })();
    return { body, foot: [button('Cancel', () => { stop = true; close(false); })] };
  }).finally(() => { /* waiting stops with the dialog */ });
}

// answerDialog approves a waiting device on a browser that is unlocked:
// the person types the code the new device shows, and only a code that
// matches the request the server passed on is accepted.
export function answerDialog(s, u) {
  let what = 'a device';
  try { what = platformLabel(decodeUnlockRequest(fromB64(u.request)).platform); } catch { /* a request that does not parse is refused below */ }
  return openDialog(`Unlock a new device (${what})?`, (close) => {
    const input = h('input', { autocomplete: 'off', spellcheck: false, autocapitalize: 'characters', placeholder: 'XXXXX-XXXXX-XXXXX-XXXXX', 'aria-label': 'Code' });
    const problem = h('p', { class: 'problem', hidden: true });
    const go = h('button', { type: 'submit', class: 'btn primary' }, 'Approve');
    go.setAttribute('form', 'sealed-answer-form');
    const form = h('form', { class: 'stack', id: 'sealed-answer-form', onsubmit: (e) => {
      e.preventDefault();
      problem.hidden = true;
      busy(go, async () => {
        const want = await approvalCode(fromB64(u.request));
        if (normalizeApprovalCode(input.value) !== want) { problem.textContent = 'That code does not match the request. Approve only a device you are setting up yourself, with the code it shows.'; problem.hidden = false; return; }
        await s.answerRequest(u, input.value);
        toast('The new device is approved.');
        close(true);
      });
    } },
    h('p', {}, `${u.device && u.device.name ? `“${u.device.name}”` : 'A new device'} asks for the key to your sealed chats. Type the 20 characters it shows. Naming the device is not enough: the code is what proves the request is really from it.`),
    field('The code it shows', input), problem);
    return { body: form, foot: [button('Not now', () => close(false)), go] };
  });
}

// waitingRequests lists the account's unlock requests with an Approve
// button each, on an unlocked browser.
export async function waitingRequests(s, get) {
  const list = (await get('/me/unlock-requests')).unlock_requests || [];
  return list.filter((u) => !u.answered_at && (!s.device || u.device_id !== s.device.id));
}
