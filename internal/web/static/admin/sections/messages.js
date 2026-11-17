// Settings: Messages.

import { get, all } from '/shared/api.js';
import { h, button, busy, fill } from '/shared/dom.js';
import { channelIcon, pickChannel } from '/shared/pick.js';
import { settingsCard, saveSetting } from '../kit.js';

// defaultChannels is the setting channels.default (ruling R103): the named
// channels every new owner, admin and member joins at sign-up. A channel
// added later is offered to the people already here, not joined for them.
// Archived and locked channels cannot be chosen.
async function defaultChannels(settings) {
  let channels = [];
  try { channels = await all('/admin/channels', 'channels', { archived: 'false' }); } catch { /* the ids are shown as they are */ }
  const byId = new Map(channels.map((c) => [c.id, c]));
  let ids = [...(settings['channels.default'] || [])];
  const list = h('ul', { class: 'rows', 'aria-label': 'Default channels' });
  const save = async (next) => {
    const out = await saveSetting('channels.default', next);
    if (out) { Object.assign(settings, out); ids = [...(out['channels.default'] || next)]; draw(); }
  };
  const draw = () => fill(list, ids.length ? ids.map((id) => {
    const c = byId.get(id) || { id, name: 'a channel that is gone' };
    const rm = button('Remove', () => busy(rm, () => save(ids.filter((x) => x !== id))), 'small');
    return h('li', { dataset: { channel: id } }, channelIcon(c), h('div', { class: 'what' }, h('strong', {}, h('bdi', {}, c.name)),
      c.private ? h('small', {}, c.knock ? 'Knock channel' : 'Private channel') : null), h('div', { class: 'does' }, rm));
  }) : h('li', { class: 'dim' }, 'None: new people join no channel at sign-up.'));
  const add = button('Add a channel…', async () => {
    const free = channels.filter((c) => !ids.includes(c.id) && !c.archived_at && !c.locked_at);
    const c = await pickChannel('Add a default channel', free, {}, { note: 'New people join it when they sign up. The people already here are offered it in their sidebar, to accept or decline.' });
    if (c) await save([...ids, c.id]);
  }, 'small');
  draw();
  return h('div', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, 'Default channels'),
    h('small', {}, 'Every new owner, admin and member joins these at sign-up. Adding one later offers it to everyone already here; nobody is put in it unasked. Guests join none.'), list),
  h('div', { class: 'control' }, add));
}

export default async function messages() {
  const settings = (await get('/admin/settings')).settings;
  const newChannels = settingsCard('New channels', null, [
    { key: 'channels.creation_approval', label: 'Creating a channel needs approval', type: 'switch', fallback: false,
      hint: 'A member who makes a channel asks for it instead, with a note, and it is made once an owner, an admin or someone who may approve requests approves it, in the chat’s Approvals. Its requester is then its first manager. Owners, admins and those who may approve requests make channels at once.' },
  ], settings);
  newChannels.lastElementChild.append(await defaultChannels(settings));
  return [
    settingsCard('Editing', null, [
      { key: 'messages.quick_correction_seconds', label: 'Quick-correction window', type: 'duration', fallback: 30, unit: 1,
        hint: 'An edit this soon after posting is not marked as edited and keeps no earlier version.' },
      { key: 'messages.edit_window_seconds', label: 'Edit window', type: 'duration', nullable: true, nullLabel: 'No limit: a message can always be edited',
        hint: 'How long after posting an author may still edit.' },
    ], settings),
    settingsCard('Keeping', null, [
      { key: 'retention.seconds', label: 'Server retention period', type: 'duration', nullable: true, nullLabel: 'Keep messages for good', min: 86400,
        hint: 'Messages older than this are removed. A channel may set a shorter period of its own.' },
      { key: 'files.max_bytes', label: 'Largest upload', type: 'bytes', hint: 'One file. Larger uploads are refused.' },
    ], settings),
    settingsCard('Who may', null, [
      { key: 'mentions.broadcast_managers_only', label: 'Only managers use @channel, @here and @everyone', type: 'switch', fallback: false,
        hint: 'Off lets every member notify a whole channel, up to the size below.' },
      { key: 'mentions.broad_mention_limit', label: 'Largest channel anyone may notify whole', type: 'number', min: 0, max: 1000000, fallback: 250,
        hint: 'In a channel with more members than this, only its managers and the admins use @channel, @here and @everyone; anyone else’s is posted as plain text and notifies nobody. A channel’s managers can change this for their channel. 0 is no limit.' },
      { key: 'mentions.broad_per_hour', label: 'Broad mentions one person may make in an hour', type: 'number', min: 0, max: 100000, fallback: 20,
        hint: 'Across the whole instance, managers and admins included. Past it, @channel, @here and @everyone are posted as plain text and notify nobody. Bots are not held to it. 0 is no limit.' },
      { key: 'members.adds_per_hour', label: 'People one person may add in an hour', type: 'number', min: 0, max: 100000, fallback: 60,
        hint: 'Across the whole instance, into channels and when a channel is made. Past it, adding someone is refused for a while. Bots are not held to it. 0 is no limit.' },
      { key: 'emoji.members_may_add', label: 'Members may add emoji', type: 'switch', fallback: false, hint: 'Otherwise only admins add custom emoji.' },
      { key: 'blocking.enabled', label: 'People may block each other', type: 'switch', fallback: true,
        hint: 'A blocked account’s messages are collapsed for the person who blocked it.' },
    ], settings),
    newChannels,
    settingsCard('Large channels', null, [
      { key: 'channels.large_channel_limit', label: 'Deliver lazily above', type: 'number', min: 0, max: 1000000, fallback: 1000,
        hint: 'A channel with more members than this is sent in full only to the people who have it open. Everyone else sees that it changed, at most once a second, and gets the rest when they open it; a message that mentions them still arrives at once. A channel’s managers can say otherwise for their channel. 0 sends every channel in full to everyone.' },
      { key: 'channels.large_feature_limit', label: 'No typing or presence above', type: 'number', min: 0, max: 1000000, fallback: 50,
        hint: 'In a conversation with more members than this nobody is shown typing, and it shows how many people are online in place of who. 0 is no limit.' },
    ], settings),
  ];
}
