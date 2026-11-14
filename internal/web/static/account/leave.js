// Leaving the server.

import { h } from '/shared/dom.js';
import { card } from '/shared/console.js';

export default async function leave(ctx) {
  return card('Leave this server',
    h('p', {}, 'Leaving removes your account: your sessions and passkeys end, you leave every channel, and your messages stay under your name.'),
    h('div', { class: 'note warn stack tight' },
      h('p', {}, h('strong', {}, 'There is no operation for this yet.')),
      h('p', {}, 'The API lets an admin remove an account, and has nothing a person can call to remove their own. Ask an admin of this server to remove you.')),
    ctx.me.role === 'owner' ? h('p', { class: 'dim' }, 'As the owner you would first hand the server to someone else, in the admin console under People.') : '');
}
