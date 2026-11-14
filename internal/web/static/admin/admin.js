// The admin console: one page with a section per row of docs/CLIENTS.md
// section 16. It exists only on the web. Each section is a module under
// sections/, loaded when it is opened. Sections marked "owner" are shown
// to the owner alone.
//
// Linked servers, shared channels, calls and export call operations the
// server may not have built yet. They are drawn all the same and show the
// server's `unavailable` answer plainly until those land.

import { start } from '/shared/console.js';

const owner = (ctx) => ctx.me.role === 'owner';
const s = (id, label, group, extra = {}) => ({ id, label, group, load: () => import(`./sections/${id}.js`), ...extra });

start({
  area: 'admin',
  title: 'Admin',
  allow: (me) => (me.role === 'owner' || me.role === 'admin') || 'The admin console is for the owner and admins of this server.',
  sections: [
    s('overview', 'Overview', ''),
    s('general', 'General', 'Settings'),
    s('sign-in', 'Sign-in', 'Settings'),
    s('messages', 'Messages', 'Settings'),
    s('outside', 'Outside connections', 'Settings'),
    s('company-sign-in', 'Company sign-in', 'Settings'),
    s('people', 'People', 'People'),
    s('invites', 'Invites', 'People'),
    s('groups', 'Groups', 'People'),
    s('timeouts', 'Time-outs and bans', 'People'),
    s('channels', 'Channels', 'Channels'),
    s('sections', 'Sections', 'Channels'),
    s('emoji', 'Emoji', 'Channels'),
    s('saved-replies', 'Saved replies', 'Channels'),
    s('status-presets', 'Status presets', 'Channels'),
    s('themes', 'Themes', 'Channels'),
    s('word-filter', 'Word filter', 'Moderation'),
    s('reports', 'Reports', 'Moderation'),
    s('purge-requests', 'Purge requests', 'Moderation'),
    s('bots', 'Bots', 'Bots and integrations'),
    s('webhooks', 'Webhooks', 'Bots and integrations'),
    s('integrations', 'Integrations', 'Bots and integrations'),
    s('linked-servers', 'Linked servers', 'Other servers', { show: owner }),
    s('shared-channels', 'Shared channels', 'Other servers', { show: owner }),
    s('hubs', 'Hub directories', 'Other servers', { show: owner }),
    s('calls', 'Calls', 'Records'),
    s('audit', 'Audit log', 'Records'),
    s('export', 'Export', 'Records', { show: owner }),
  ],
});
