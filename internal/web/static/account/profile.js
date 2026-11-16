// Profile: the same fields as in the apps.

import { get, patch, del, upload } from '/shared/api.js';
import { h, button, field, busy, toast, avatar, confirmDialog, day } from '/shared/dom.js';
import { card } from '/shared/console.js';

export default async function profile(ctx) {
  let me = (await get('/me')).account;
  const zones = (() => { try { return Intl.supportedValuesOf('timeZone'); } catch { return []; } })();
  const here = Intl.DateTimeFormat().resolvedOptions().timeZone;

  const display = h('input', { value: me.display_name || '', maxLength: 64, required: true, autocomplete: 'name' });
  const username = h('input', { value: me.username, maxLength: 32, minLength: 2, required: true, autocapitalize: 'none', spellcheck: false, autocomplete: 'username' });
  const title = h('input', { value: me.title || '', maxLength: 80 });
  const pronouns = h('input', { value: me.pronouns || '', maxLength: 40 });
  const zone = h('input', { value: me.time_zone || '', maxLength: 64, placeholder: here, list: 'zones', autocomplete: 'off', spellcheck: false });
  const save = h('button', { type: 'submit', class: 'btn primary' }, 'Save profile');
  const form = h('form', { class: 'stack', onsubmit: (e) => {
    e.preventDefault();
    busy(save, async () => {
      me = (await patch('/me', { display_name: display.value.trim(), username: username.value.trim().toLowerCase(), title: title.value.trim(), pronouns: pronouns.value.trim(), time_zone: zone.value.trim() })).account;
      ctx.me = me;
      toast('Profile saved.');
    });
  } },
  h('div', { class: 'cols-2' },
    field('Display name', display, 'Shown beside your messages.'),
    field('Username', username, 'People mention you with it. Changing it changes how you are mentioned from now on.')),
  h('div', { class: 'cols-2' }, field('Title', title, 'What you do here, if you like.'), field('Pronouns', pronouns)),
  field('Time zone', zone, `Used for quiet hours and to show others your local time. This browser is in ${here}.`),
  h('datalist', { id: 'zones' }, zones.map((z) => h('option', { value: z }))),
  h('p', { class: 'row' }, save, button('Use this browser’s time zone', () => { zone.value = here; })));

  const picture = h('div', { class: 'row' });
  const drawPicture = () => {
    const file = h('input', { type: 'file', accept: 'image/png,image/jpeg,image/gif,image/webp', 'aria-label': 'Choose a picture' });
    const send = button('Upload', () => busy(send, async () => {
      if (!file.files[0]) { toast('Choose a picture first.', true); return; }
      const body = new FormData();
      body.append('file', file.files[0]);
      me = (await upload('/me/avatar', body, null, 'PUT').promise).account;
      drawPicture();
      toast('Picture saved.');
    }), 'primary');
    const remove = me.avatar_url && button('Remove', () => busy(remove, async () => {
      if (!(await confirmDialog('Remove your picture?', 'Your initials are shown instead.', { yes: 'Remove', danger: true }))) return;
      await del('/me/avatar');
      me = (await get('/me')).account;
      drawPicture();
    }), 'danger');
    picture.replaceChildren(avatar(me, 'large'), h('div', { class: 'grow' }, file), send, remove || '');
  };
  drawPicture();

  return [
    card('Picture', picture, h('p', { class: 'dim small' }, 'A square picture works best. The server keeps a small copy with location details removed.')),
    card('About you', form),
    card('This account', h('dl', { class: 'facts' },
      h('div', {}, h('dt', {}, 'Role'), h('dd', {}, me.role)),
      h('div', {}, h('dt', {}, 'Joined'), h('dd', {}, day(me.created_at))),
      h('div', {}, h('dt', {}, 'Passkeys'), h('dd', {}, String(me.passkey_count))),
      h('div', {}, h('dt', {}, 'Access key'), h('dd', {}, me.has_access_key ? 'Yes' : 'No')),
      h('div', {}, h('dt', {}, 'Company identities'), h('dd', {}, String(me.identity_count || 0))))),
  ];
}
