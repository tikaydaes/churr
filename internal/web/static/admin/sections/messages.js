// Settings: Messages.

import { get } from '/shared/api.js';
import { settingsCard } from '../kit.js';

export default async function messages() {
  const settings = (await get('/admin/settings')).settings;
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
        hint: 'Off lets every member notify a whole channel.' },
      { key: 'emoji.members_may_add', label: 'Members may add emoji', type: 'switch', fallback: false, hint: 'Otherwise only admins add custom emoji.' },
      { key: 'blocking.enabled', label: 'People may block each other', type: 'switch', fallback: true,
        hint: 'A blocked account’s messages are collapsed for the person who blocked it.' },
    ], settings),
  ];
}
