// The page an app opens in the system browser to sign in: the buttons of
// templates/authorize.html. The page itself is drawn by the server; this
// file gives its buttons their behaviour. The approval is a plain form
// post and needs none.

import { api, post } from '/shared/api.js';
import { createPasskey, getPasskey, passkeyName } from '/shared/passkey.js';
import { qrSvg } from '/shared/qr.js';

const problem = document.getElementById('problem');

// run does one step and shows its failure in the page.
async function run(fn) {
  problem.hidden = true;
  try {
    await fn();
  } catch (err) {
    problem.textContent = err.name === 'NotAllowedError' || err.name === 'AbortError'
      ? 'The passkey prompt was closed before it finished. Try again.'
      : (err.message || 'Something went wrong.');
    problem.hidden = false;
  }
}

async function passkey(begin, finish) {
  const b = await api('POST', begin, undefined, { quiet401: true });
  const credential = await getPasskey(b.public_key);
  await api('POST', finish, { ceremony_id: b.ceremony_id, credential }, { quiet401: true });
  location.reload();
}

// A first proof enrolled here may come with recovery codes, shown once.
function showCodesOrReload(out) {
  const codes = out.sign_in && out.sign_in.recovery_codes;
  if (codes && codes.length) {
    document.getElementById('codelist').textContent = codes.join('\n');
    document.getElementById('codes').hidden = false;
  } else location.reload();
}

async function addPasskey() {
  const b = await post('/me/passkeys/begin');
  const made = await createPasskey(b.public_key);
  showCodesOrReload(await post('/me/passkeys/finish', { ceremony_id: b.ceremony_id, credential: made.credential, passkey_name: passkeyName(), prf_enabled: made.prfEnabled }));
}

async function beginTotp() {
  const b = await post('/me/totp');
  document.getElementById('otpauth').textContent = b.otpauth_url;
  document.getElementById('totp-qr').replaceChildren(qrSvg(b.otpauth_url, 'QR code for your authenticator app'));
  document.getElementById('totp').hidden = false;
}

async function code(path, value) {
  await post(path, { code: value.replace(/\s+/g, '') });
  location.reload();
}

const actions = {
  'passkey-login': () => passkey('/passkey/login/begin', '/passkey/login/finish'),
  'passkey-check': () => passkey('/checks/passkey/begin', '/checks/passkey/finish'),
  'passkey-add': addPasskey,
  'totp-begin': beginTotp,
  'totp-check': (el) => code('/checks/totp', el.elements.code.value),
  'totp-confirm': (el) => code('/me/totp/confirm', el.elements.code.value),
  reload: () => location.reload(),
};

for (const el of document.querySelectorAll('[data-do]')) {
  const action = actions[el.dataset.do];
  if (!action) continue;
  if (el.tagName === 'FORM') el.addEventListener('submit', (e) => { e.preventDefault(); run(() => action(el)); });
  else el.addEventListener('click', () => run(() => action(el)));
}
