// The account page: one's own profile, the ways one signs in, where one is
// signed in, tokens for scripts, and the preferences that are not kept per
// device (docs/CLIENTS.md section 15). One module per section.

import { start } from '/shared/console.js';

start({
  area: 'account',
  title: 'Account',
  sections: [
    { id: 'profile', label: 'Profile', load: () => import('./profile.js') },
    { id: 'sign-in', label: 'Passkeys and access key', group: 'Signing in', load: () => import('./signin.js') },
    { id: 'sessions', label: 'Sessions and devices', group: 'Signing in', load: () => import('./sessions.js') },
    { id: 'tokens', label: 'Personal tokens', group: 'Signing in', load: () => import('./tokens.js') },
    { id: 'notifications', label: 'Notifications', group: 'Preferences', load: () => import('./prefs.js') },
    { id: 'appearance', label: 'Appearance', group: 'Preferences', load: () => import('./appearance.js') },
    { id: 'blocked', label: 'Blocked accounts', group: 'Preferences', load: () => import('./blocked.js') },
    { id: 'sealed', label: 'Sealed chats', group: 'Preferences', load: () => import('./sealed.js') },
    { id: 'leave', label: 'Leave this server', group: 'Preferences', load: () => import('./leave.js') },
  ],
});
