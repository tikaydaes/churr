// The browser's passkey calls. The server gives creation and request
// options as JSON with base64url where the browser wants bytes, and takes
// the answer back the same way; this file does the conversion both ways.
//
// Every creation asks for the PRF extension, which is what later unlocks
// sealed chats and cannot be switched on afterwards (docs/AUTH.md section
// 5.2). The server put the extension input in the options. The server must
// never see a PRF value, so the answer sent back says only whether the
// browser reported PRF as enabled.

const toBuf = (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
const toB64 = (buf) => {
  let s = '';
  for (const b of new Uint8Array(buf)) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
};

export function passkeysSupported() {
  return !!(window.PublicKeyCredential && navigator.credentials);
}

// extensionResults keeps what is safe to send: whether PRF is enabled, and
// the credProps the library reads. Any PRF output stays in the page.
function extensionResults(cred) {
  const got = cred.getClientExtensionResults ? cred.getClientExtensionResults() : {};
  const out = {};
  if (got.credProps) out.credProps = got.credProps;
  if (got.prf) out.prf = { enabled: got.prf.enabled === true };
  return out;
}

// createPasskey runs a creation. It returns { credential, prfEnabled }.
export async function createPasskey(publicKey) {
  const opts = structuredClone(publicKey);
  opts.challenge = toBuf(opts.challenge);
  opts.user.id = toBuf(opts.user.id);
  for (const c of opts.excludeCredentials || []) c.id = toBuf(c.id);
  const cred = await navigator.credentials.create({ publicKey: opts });
  const ext = extensionResults(cred);
  return {
    prfEnabled: !!(ext.prf && ext.prf.enabled),
    credential: {
      id: cred.id, rawId: toB64(cred.rawId), type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment || undefined,
      clientExtensionResults: ext,
      response: {
        clientDataJSON: toB64(cred.response.clientDataJSON),
        attestationObject: toB64(cred.response.attestationObject),
        transports: cred.response.getTransports ? cred.response.getTransports() : [],
      },
    },
  };
}

// getPasskey runs a sign-in or a check and returns the credential to send.
export async function getPasskey(publicKey) {
  const opts = structuredClone(publicKey);
  opts.challenge = toBuf(opts.challenge);
  for (const c of opts.allowCredentials || []) c.id = toBuf(c.id);
  // No PRF evaluation is asked for here: unlocking sealed history is the
  // unlock page's job, and that page is not built yet.
  if (opts.extensions && opts.extensions.prf) delete opts.extensions.prf;
  const cred = await navigator.credentials.get({ publicKey: opts });
  return {
    id: cred.id, rawId: toB64(cred.rawId), type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment || undefined,
    clientExtensionResults: {},
    response: {
      clientDataJSON: toB64(cred.response.clientDataJSON),
      authenticatorData: toB64(cred.response.authenticatorData),
      signature: toB64(cred.response.signature),
      userHandle: cred.response.userHandle ? toB64(cred.response.userHandle) : undefined,
    },
  };
}

// A default name for a new passkey, so the list on the account page says
// when each was made.
export function passkeyName() {
  return 'Passkey added ' + new Date().toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
}
