// Settings: Sign-in. Who may join, who may invite, how members sign in,
// the second factor, and how long sessions last. Each change needs a fresh
// check, which the shared dialog asks for (docs/AUTH.md sections 2 and 8).

import { get, post } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { settingsCard } from '../kit.js';

export default async function signIn(ctx) {
  const settings = (await get('/admin/settings')).settings;
  const everyone = button('Sign everyone out', async () => {
    if (!(await confirmDialog('Sign everyone out?', 'Every browser and app but this one is signed out, so a changed sign-in policy takes hold at once. Personal and bot tokens are left alone.', { yes: 'Sign everyone out', danger: true }))) return;
    await busy(everyone, async () => { await post('/admin/sessions/sign-out-all'); toast('Everyone else is signed out.'); });
  }, 'danger');
  return [
    settingsCard('Joining', 'Changing these needs a fresh check of who you are.', [
      { key: 'registration.mode', label: 'Who can make an account', type: 'select', fallback: 'invite',
        options: [['invite', 'By invite only'], ['open', 'Anyone who can reach the server'], ['closed', 'Nobody']] },
      { key: 'registration.approval', label: 'New registrations need approval', type: 'switch', fallback: false,
        hint: 'Only while anyone may make an account. Someone who registers without an invite waits until an owner, an admin or someone who may approve requests approves them, in the chat’s Approvals. Their note says why they would like to join. A request not decided in 14 days lapses.' },
      { key: 'invites.members_may_invite', label: 'Members may invite', hint: 'Each member may make single-use invites for new members. Guest invites are always made by admins.', type: 'switch', fallback: false },
      { key: 'roles.new_owner_delay_hours', label: 'Hours a new owner waits', type: 'number', min: 0, max: 720, fallback: 0,
        hint: 'Someone made an owner becomes one after this many hours. Until then any owner can withdraw it, and every owner is told. 0 is at once.' },
    ], settings),
    settingsCard('How people sign in', 'The owner and admins can always use a passkey or their access key, so a broken provider cannot lock the server.', [
      { key: 'auth.member_sign_in', label: 'Members and guests sign in with', type: 'select', fallback: 'passkey',
        hint: '“Company sign-in only” is refused while no enabled provider exists.',
        options: [['passkey', 'A passkey or access key'], ['either', 'A passkey, access key or company sign-in'], ['sso', 'Company sign-in only']] },
      { key: 'auth.allow_access_keys', label: 'Allow accounts without a passkey', type: 'switch', fallback: true,
        hint: 'On: someone who cannot use a passkey gets an access key, a long code made for them, when the account is made. Off: no new access keys are made and the pages stop offering them; access keys that exist keep working.' },
      { key: 'auth.second_factor', label: 'Second step after company sign-in', type: 'select', fallback: 'off',
        hint: 'A passkey sign-in already counts as two factors and is never asked for more.',
        options: [['off', 'Not asked for'], ['required', 'Required']] },
      { key: 'auth.second_factor_accepted', label: 'What counts as the second step', type: 'checks', fallback: ['passkey', 'totp', 'provider'],
        hint: 'At least one must be ticked while a second step is required.',
        options: [['passkey', 'One of the person’s passkeys'], ['totp', 'A code from an authenticator app'], ['provider', 'The provider’s own statement that it used more than one factor']] },
    ], settings),
    settingsCard('How long a session lasts', null, [
      { key: 'sessions.passkey_seconds', label: 'After a passkey or access key sign-in', hint: 'Sliding: each use starts the time again.', type: 'duration', fallback: 2592000, min: 3600 },
      { key: 'sessions.sso_seconds', label: 'After company sign-in', hint: 'Fixed. A person dropped by the provider is out within this time.', type: 'duration', fallback: 86400, min: 3600 },
    ], settings),
    card('Make it bite now', h('p', {}, 'Changing the policy does not end sessions that already exist.'), h('p', { class: 'row' }, everyone)),
  ];
}
