"""Server information, setup, sign-in and the apps' OAuth."""
from lib import area, op


# ----------------------------------------------------------------------------
area('Server',
     'What the server is, and first-run setup.')

op('GET server', 'getServer',
   "Get the server's name, sign-in methods and switched-on features",
   who='Anyone, signed in or not.',
   desc='The apps read it before sign-in. Until the owner exists, every other operation answers `unavailable`.',
   res={'server': '#Server'},
   auth='optional')

op('GET /.well-known/churr', 'getWellKnown',
   "Get the server's name and public keys",
   who='Anyone. Other servers and hubs use it to check keys.',
   res='#WellKnown',
   auth='none')

op('POST setup/begin', 'beginSetup',
   'Start making the owner account',
   who='Whoever holds the one-time setup link printed when the server first starts.',
   desc=('Works until an owner exists. Each start of the server without an owner makes a new setup link, so an old '
         'one answers `not_found`.'),
   body={'setup_token': 'str', 'username': 'str', 'display_name': 'str'},
   res='#Ceremony',
   err='not_found name_taken',
   auth='none')

op('POST setup/finish', 'finishSetup',
   'Finish making the owner account',
   who='Whoever began setup.',
   desc=('Creates the owner with its first passkey and eight recovery codes, and signs them in. The '
         'outside-connection switches are then set with `updateSettings`.'),
   body='#PasskeyCreated',
   res='#SignIn',
   st=201,
   err='not_found unauthenticated',
   auth='none')


op('POST setup/access-key', 'setupWithAccessKey',
   'Make the owner account with an access key in place of a passkey',
   who='Whoever holds the one-time setup link printed when the server first starts.',
   desc=('For an owner who cannot use a passkey. The page makes the access key, shows it once and has it confirmed '
         'before it calls this; the server is sent only the value derived from it. Creates the owner with that '
         'access key and no recovery codes, signs them in, and finishes setup as `finishSetup` does.'),
   body={'setup_token': 'str', 'username': 'str', 'display_name': 'str', 'access_key_auth': ('b64', 'The 32-byte value the page derives from the access key (AUTH.md section 5.6), never the key itself.'),
         'unlock_public?': ('b64', 'The X25519 public key derived from the access key for sealed chats (SEALED.md section 13.3). The server keeps it with the key; an unlocked device seals the backup key to it.')},
   res='#SignIn',
   st=201,
   err='not_found name_taken',
   auth='none')


# ----------------------------------------------------------------------------
area('Sign-in',
     'Passkeys, recovery, company sign-in, second factors and sign-out. Every endpoint here is limited per address.')

op('GET invite-codes/{code}', 'checkInvite',
   'Check an invite or claim link before registering',
   who='Anyone holding the code.',
   desc='Checking uses nothing up. The invite is counted only when registration finishes.',
   res={'role': 'enum:member|guest', 'claims_username?': ('str', 'Set on a claim link for an imported account.')},
   err='not_found',
   auth='none')

op('POST passkey/register/begin', 'beginPasskeyRegistration',
   'Start registering a new account with a passkey',
   who="Anyone, if registration is `open`; otherwise only with a usable invite code. Refused with `forbidden` when the invite's role must use company sign-in. A claim link works in every registration mode.",
   desc='With a claim link the account already exists and `username` is ignored.',
   body={'invite_code?': 'str', 'username?': 'str', 'display_name?': 'str'},
   res='#Ceremony',
   err='forbidden not_found name_taken',
   auth='none')

op('POST passkey/register/finish', 'finishPasskeyRegistration',
   'Finish registering and sign in',
   who='Whoever began the registration.',
   desc='Returns the eight recovery codes, once.',
   body='#PasskeyCreated',
   res='#SignIn',
   st=201,
   err='not_found unauthenticated name_taken',
   auth='none')

op('POST access-key/register', 'registerWithAccessKey',
   'Register a new account with an access key in place of a passkey',
   who=("As `beginPasskeyRegistration`, while the server allows accounts without a passkey (setting "
        '`auth.allow_access_keys`). A claim link works in every registration mode.'),
   desc=('The page makes the access key, shows it once and has it confirmed before it calls this; the server is '
         'sent only the value derived from it. Makes the account (or claims the imported one) with that access key '
         'and no recovery codes, and signs it in.'),
   body={'invite_code?': 'str', 'username?': 'str', 'display_name?': 'str', 'access_key_auth': ('b64', 'The 32-byte value the page derives from the access key (AUTH.md section 5.6), never the key itself.'),
         'unlock_public?': ('b64', 'The X25519 public key derived from the access key for sealed chats (SEALED.md section 13.3). The server keeps it with the key; an unlocked device seals the backup key to it.')},
   res='#SignIn',
   st=201,
   err='forbidden not_found name_taken',
   auth='none')

op('POST access-key/login', 'signInWithAccessKey',
   'Sign in with a username and an access key',
   who='Anyone. A member or guest is refused with `forbidden` when member sign-in is `sso`; the owner and admins never are.',
   desc=('Makes a session as a passkey sign-in does. Limited per address and per account. Any failure gives the '
         'same answer after the same work, whether or not the username exists. Existing access keys keep working '
         'when the server stops allowing new ones.'),
   body={'username': 'str', 'access_key_auth': ('b64', 'The 32-byte value the page derives from the access key (AUTH.md section 5.6), never the key itself.')},
   res='#SignIn',
   err='unauthenticated forbidden',
   auth='none')

op('POST passkey/login/begin', 'beginPasskeyLogin',
   'Start a passkey sign-in',
   who='Anyone. No username is needed.',
   res='#Ceremony',
   auth='none')

op('POST passkey/login/finish', 'finishPasskeyLogin',
   'Finish a passkey sign-in',
   who='Whoever began it. A member or guest is refused with `forbidden` when member sign-in is `sso`; the owner and admins never are.',
   desc=('A passkey sign-in already counts as two factors and is never asked for a second. The error does not say '
         'which part was wrong.'),
   body='#CeremonyResult',
   res='#SignIn',
   err='unauthenticated forbidden not_found',
   auth='none')

op('GET sso/{provider}/start', 'startSso',
   'Start company sign-in at a provider, for a purpose',
   who='Anyone, to sign in, when member sign-in is `either` or `sso`. For `link`: a signed-in person with a fresh check, or a recovery session made by a link.',
   desc=('Redirects the browser to the provider and sets the short-lived `churr_sso` cookie that binds the answer to '
         'this browser. OIDC uses the authorization code flow with PKCE; SAML sends an `AuthnRequest`.'),
   q=['purpose:enum:sign_in|link:Default `sign_in`.',
      'invite_code:str:An invite to register with, when the person has no account yet.',
      'return_to:str:Path on this server to go to afterwards.'],
   err='not_found feature_off forbidden fresh_check_required',
   auth='optional',
   raw={'302': {'description': 'Redirect.', 'headers': {'Location': {'schema': {'type': 'string'}}}}})

op('GET sso/{provider}/callback', 'finishOidcSso',
   'Return from an OIDC provider',
   who='The browser, sent back by the provider.',
   desc=('Validates the ID token, then finds, claims or creates the account as registration allows, or links the '
         'identity. If a second factor is owed the session it sets is pending. A banned identity is refused.'),
   q=['code:str:Authorization code.', 'state:str:The value Churr sent.', 'error:str:Set by the provider on failure.'],
   err='unauthenticated forbidden not_found',
   auth='none',
   raw={'200': {'description': "A small page from this server's own origin that loads the next page. Not a redirect, "
                               'so the SameSite=Strict session cookie is sent.',
                'content': {'text/html': {'schema': {'type': 'string'}}}}})

op('POST sso/{provider}/acs', 'finishSamlSso',
   'Receive a SAML response',
   who='The browser, sent back by the provider.',
   desc=('Only a signed assertion from the configured provider that answers a request Churr made is accepted. Then '
         'as `finishOidcSso`.'),
   form={'SAMLResponse': 'str', 'RelayState?': 'str'},
   err='unauthenticated forbidden not_found',
   auth='none',
   raw={'200': {'description': "A small page from this server's own origin that loads the next page. Not a redirect, "
                               'so the SameSite=Strict session cookie is sent.',
                'content': {'text/html': {'schema': {'type': 'string'}}}}})

op('GET sso/{provider}/metadata', 'getSamlMetadata',
   "Get this server's SAML service-provider metadata",
   who='Anyone.',
   err='not_found',
   auth='none',
   raw={'200': {'description': 'Metadata.',
                'content': {'application/samlmetadata+xml': {'schema': {'type': 'string'}}}}})

op('GET second-factor', 'getSecondFactor',
   'List the proofs a pending session may give',
   who='A session that still owes a second factor.',
   res={'second_factor': '#SecondFactorState'},
   auth='pending')

op('POST logout', 'logout',
   'End the current session',
   who='Any signed-in account. Also a pending session, and a locked one.',
   desc=('Revokes the session, closes its live connections and clears the cookie. A browser removes itself from the '
         'device list and wipes its sealed-chat keys first.'),
   auth='pending')

op('POST recovery/code', 'recoverWithCode',
   'Sign in with a recovery code',
   who='Anyone holding an unused recovery code for the named account. Refused for a member or guest when member sign-in is `sso`.',
   desc=('Makes a recovery session: one hour, never extended, able only to read the profile, list and remove '
         'passkeys, list and revoke sessions, and create a passkey. Creating the passkey replaces it with an '
         'ordinary session. Any failure gives the same answer.'),
   body={'username': 'str', 'code': ('str', 'Sixteen characters; spaces, hyphens and case are ignored.')},
   res='#SignIn',
   err='unauthenticated',
   auth='none')

op('POST recovery/link', 'recoverWithLink',
   'Open a recovery link',
   who="Anyone holding an unused recovery link. Opening the link's page uses nothing up; this call, made by the button on that page, does.",
   desc=("Makes a recovery session as `recoverWithCode` does. If the account's role must use company sign-in, the "
         'only thing the session can do is link an identity with `startSso`.'),
   body={'token': 'str'},
   res='#SignIn',
   err='unauthenticated',
   auth='none')

op('POST checks/passkey/begin', 'beginPasskeyCheck',
   'Start a passkey check',
   who='A pending session, as its second factor. A signed-in web or app session, as a fresh check.',
   desc="The challenge is limited to the account's own passkeys.",
   res='#Ceremony',
   err='forbidden',
   auth='pending')

op('POST checks/passkey/finish', 'finishPasskeyCheck',
   'Finish a passkey check',
   who='A pending session, as its second factor. A signed-in web or app session, as a fresh check.',
   desc=('For a pending session this completes the sign-in: the answer carries the account and the cookie is '
         'replaced. For a signed-in session it marks the session freshly checked for ten minutes.'),
   body='#CeremonyResult',
   res='#SignIn',
   err='unauthenticated forbidden not_found',
   auth='pending')

op('POST checks/access-key', 'checkAccessKey',
   'Give the access key as a fresh check',
   who='A signed-in web or app session whose account has an access key. Not a pending session: an access key is not a second factor.',
   desc='Marks the session freshly checked for ten minutes. Five wrong keys for the account in fifteen minutes and it is refused with `rate_limited` until the window has passed.',
   body={'access_key_auth': ('b64', 'The 32-byte value the page derives from the access key (AUTH.md section 5.6), never the key itself.')},
   res='#SignIn',
   err='unauthenticated forbidden')

op('POST checks/totp', 'checkTotp',
   'Give an authenticator-app code',
   who='A pending session, as its second factor. A signed-in web or app session, as a fresh check.',
   desc='As `finishPasskeyCheck`. A code works once. Five wrong proofs end a pending session.',
   body={'code': 'str'},
   res='#SignIn',
   err='unauthenticated forbidden',
   auth='pending')


op('POST session/lock', 'lockSession',
   'Lock this session',
   who='A signed-in web or app session, for itself. Each session locks on its own; the account\'s others are untouched.',
   desc=('Marks the session locked and closes its live connections with 4002. Until it is unlocked every other '
         'operation, the live connection and the wake-up fetch included, answers 401 `session_locked`; only '
         '`beginSessionUnlock`, `getSessionUnlockOptions`, `finishSessionUnlock` and `logout` are answered, and an '
         'app may still refresh its tokens. Locking a locked session changes nothing.'),
   err='forbidden')

op('POST session/unlock/begin', 'beginSessionUnlock',
   'Start unlocking this session',
   who='A locked web or app session, for itself.',
   desc=("A passkey assertion limited to the account's own passkeys, with user verification required, as the sign-in "
         'ceremony, or the access key when the account has one (`finishSessionUnlockWithAccessKey`). The web chat '
         'runs it in its own page; an app opens `unlock_url` in the system browser. Refused with `forbidden` for an '
         'account with neither, and with `rate_limited` after five wrong tries for the account in fifteen minutes.'),
   res='#SessionUnlock',
   err='forbidden conflict')

op('POST session/unlock/options', 'getSessionUnlockOptions',
   'Get the options of an unlock ceremony',
   who="Anyone holding the ceremony's identifier: the page an app opened.",
   desc='Knowing the identifier is not enough to unlock: only an assertion by one of the account\'s passkeys, or its access key, is.',
   body={'ceremony_id': 'str'},
   res={'public_key?': ('obj', 'Absent when the account has no passkey.'),
        'access_key': ('bool', 'The account has an access key, which unlocks too.')},
   err='not_found',
   auth='none')

op('POST session/unlock/finish', 'finishSessionUnlock',
   'Finish unlocking a session',
   who="Whoever holds the assertion: the locked session's own page, or the page an app opened, which has no credential of its own.",
   desc=('The server verifies the assertion against the account the ceremony was begun for (challenge, origin, '
         'relying party, the user-present and user-verified flags, signature, counter) and unlocks that session, '
         'which also counts as a fresh check. A failure does not say which part was wrong and counts against the '
         'account: five in fifteen minutes and unlocking is refused with `rate_limited` until the window has passed.'),
   body='#CeremonyResult',
   err='unauthenticated not_found',
   auth='none')

op('POST session/unlock/access-key', 'finishSessionUnlockWithAccessKey',
   'Unlock a session with the access key',
   who="Whoever holds the ceremony and the account's access key: the locked session's own page, or the page an app opened.",
   desc=('Unlocks the session the ceremony was begun for, which also counts as a fresh check. A wrong key does not '
         'use the ceremony up, and counts against the account with the wrong passkey assertions: five in fifteen '
         'minutes and unlocking is refused with `rate_limited` until the window has passed.'),
   body={'ceremony_id': 'str', 'access_key_auth': ('b64', 'The 32-byte value the page derives from the access key (AUTH.md section 5.6), never the key itself.')},
   err='unauthenticated not_found',
   auth='none')

# ----------------------------------------------------------------------------
area('OAuth',
     'How the apps sign in: authorization code with PKCE in the system browser, or QR pairing from a signed-in web page. Outside `/api/v1`.')

op('GET /.well-known/oauth-authorization-server', 'getOAuthMetadata',
   'Get OAuth server metadata (RFC 8414)',
   who='Anyone.',
   desc=('States the issuer, the three endpoints, the `code` response type, the three grant types, `S256`, and that '
         'clients are public.'),
   res='obj',
   auth='none')

op('GET /oauth/authorize', 'authorize',
   'Open the sign-in and approval page for an app',
   who='A person in a browser. They sign in by any method the server offers, then approve the app.',
   desc=('Checks the redirect address and PKCE values before anything else and never redirects to an address it has '
         "not accepted: `http://127.0.0.1:<port>/…` or `http://[::1]:<port>/…` for desktop apps, or the apps' own "
         'address scheme for phones. The person signs in if the browser has no complete session, second factor '
         'included, and approves "Sign in <device name> as <username>?". The browser is then sent to the redirect '
         'address with `code`, `state` and `iss`. A browser session made only for this is revoked once the code is '
         'issued. The approval itself is a form post from this page to `POST /oauth/authorize`, which is not an '
         'operation of this contract; when the page is an unlocked web chat it may add `handover`, the backup key '
         'sealed to the app\'s box key (SEALED.md section 13.3), which the server keeps with the code and cannot open.'),
   q=['response_type!:enum:code:Always `code`.',
      'client_id!:str:A label such as `churr-app`. Apps are not registered and it is given no trust.',
      'redirect_uri!:str:Where to send the code.', 'code_challenge!:str:PKCE challenge, at least 43 characters.',
      'code_challenge_method!:enum:S256:Always `S256`.', 'state:str:Returned unchanged.',
      'device_name:str:Shown on the approval page and kept as the session label.',
      "platform:enum:android|ios|macos|windows|linux:The app's platform.",
      "box_key:str:The app's device box public key, base64url. With it, a passkey sign-in on this page also hands "
      'the app the backup key.'],
   err='invalid_request',
   auth='optional',
   raw={'200': {'description': 'The sign-in and approval page.',
                'content': {'text/html': {'schema': {'type': 'string'}}}},
        '302': {'description': 'Redirect.', 'headers': {'Location': {'schema': {'type': 'string'}}}}})

op('POST /oauth/token', 'exchangeToken',
   'Exchange a code, a refresh token or a confirmed pairing for tokens',
   who='An app.',
   desc=('Grant types: `authorization_code` (with `code`, `code_verifier`, `redirect_uri`), `refresh_token`, and '
         '`urn:churr:params:oauth:grant-type:pairing-code` (with `pairing_code`, `device_name`, `platform`). The '
         "first pairing request records the device's name; until the person confirms on the web page the answer is "
         '`authorization_pending`, and the app asks again every two seconds. A refresh token presented again within '
         '60 seconds of its replacement is treated as a retry; later than that, the session is revoked.'),
   form={'grant_type': 'enum:authorization_code|refresh_token|urn:churr:params:oauth:grant-type:pairing-code',
         'code?': 'str',
         'code_verifier?': 'str',
         'redirect_uri?': 'str',
         'client_id?': 'str',
         'refresh_token?': 'str',
         'pairing_code?': 'str',
         'device_name?': 'str',
         'platform?': 'enum:android|ios|macos|windows|linux'},
   err='rate_limited',
   auth='none',
   raw={'200': {'description': 'Tokens.',
                'content': {'application/json': {'schema': {'$ref': '#/components/schemas/OAuthTokens'}}}},
        '400': {'description': 'Refused, in the RFC 6749 shape.',
                'content': {'application/json': {'schema': {'$ref': '#/components/schemas/OAuthError'}}}}})

op('POST /oauth/revoke', 'revokeOAuthToken',
   "Revoke an app's tokens (RFC 7009)",
   who='An app holding the token.',
   desc='Either token identifies the session. The answer is always success.',
   form={'token': 'str'},
   auth='none',
   raw={'200': {'description': 'Revoked, or the token was already unknown.'}})

op('POST /oauth/pair', 'startPairing',
   'Make a pairing code to show as a QR code',
   who='A person signed in on a web page. Needs a fresh check.',
   desc=('The code is valid for two minutes. The QR code may also hold a secret that unlocks sealed chats on the new '
         'device; that secret never reaches the server.'),
   res={'pairing_code': 'str',
        'qr_text': ('str', '`churr://pair?server=…&code=…`, the text of the QR code.'),
        'expires_at': 'time'},
   st=201,
   err='forbidden fresh_check_required',
   auth='web')

op('GET /oauth/pair/{code}', 'getPairing',
   'See whether a pairing code has been claimed, and by what',
   who='A person signed in on a web page who holds the code.',
   desc='`claimed` once an app has posted the code to the token endpoint with its device name.',
   res={'state': 'enum:waiting|claimed|confirmed', 'device_name?': 'str', 'platform?': 'str', 'expires_at': 'time'},
   err='not_found',
   auth='web')

op('POST /oauth/pair/{code}/confirm', 'confirmPairing',
   'Approve the app that claimed a pairing code',
   who='A person signed in on a web page who holds the code, after checking the device name shown. The app is then signed in as that person.',
   desc=('The confirmation cannot be skipped: it is what stops someone who photographed the screen. An unlocked web '
         'chat may send the two-layer hand-over of the backup key with it (SEALED.md section 13.7); the body is '
         'optional.'),
   body={'handover?': ('b64', 'The backup key sealed for the app, which the server stores with the pairing and cannot open.')},
   body_optional=True,
   err='not_found conflict',
   auth='web')

op('GET /oauth/unlock', 'openUnlockPage',
   'Open the page that unlocks sealed chats for an app with a passkey',
   who='A person in a browser, sent there by an app that is already signed in.',
   desc=("A passkey only works at the server's own web address, so the passkey way of unlocking always passes "
         'through a page the server supplies, for the apps as much as for the web chat. The page asks for one touch '
         'of a passkey that has a locked copy, opens the backup key, and stores a locked copy of kind `device` for '
         "the app's device with `addLockedCopy`. The app then finds it with `getBackup`."),
   q=["device_id!:uuid:The app's device."],
   err='not_found',
   auth='optional',
   raw={'200': {'description': 'The unlock page.', 'content': {'text/html': {'schema': {'type': 'string'}}}}})
