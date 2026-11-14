// Settings: General. The community's name and icon, and the welcome text.

import { get, upload } from '/shared/api.js';
import { h, button, busy, toast } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { settingsCard } from '../kit.js';

export default async function general(ctx) {
  const settings = (await get('/admin/settings')).settings;
  const file = h('input', { type: 'file', accept: 'image/png,image/jpeg,image/webp', 'aria-label': 'Choose an icon' });
  const send = button('Upload icon', () => busy(send, async () => {
    if (!file.files[0]) { toast('Choose a picture first.', true); return; }
    const body = new FormData();
    body.append('file', file.files[0]);
    await upload('/admin/settings/icon', body, null, 'PUT').promise;
    toast('Icon saved.');
    ctx.redraw();
  }), 'primary');
  return [
    settingsCard('Community', null, [
      { key: 'server.name', label: 'Name of the community', hint: 'Shown on the sign-in page and at the top of the chat.', type: 'text', maxLength: 64 },
      { key: 'server.time_zone', label: 'Time zone', hint: 'Used where the server needs a clock of its own, such as recurring forms. For example Australia/Brisbane.', type: 'text', placeholder: 'UTC' },
      { key: 'welcome.text', label: 'Welcome text', hint: 'Shown to each person the first time they sign in. Churr Markdown.', type: 'textarea' },
    ], settings),
    card('Icon',
      h('div', { class: 'row' },
        ctx.server.icon_url ? h('img', { class: 'avatar large', src: ctx.server.icon_url, alt: 'The current icon' }) : h('span', { class: 'avatar large c0', 'aria-hidden': 'true' }, (settings['server.name'] || 'C').slice(0, 2)),
        h('div', { class: 'grow' }, file), send),
      h('p', { class: 'dim small' }, 'A square picture. It marks this server in the apps.')),
  ];
}
