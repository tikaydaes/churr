// Settings: Outside connections. The table from first-run setup, with
// every switch: the push relay and its address, UnifiedPush, badge numbers
// in wake-ups, link previews, object storage, sealed chats, calls and
// their limits, and presence to linked servers (DESIGN.md section 17).

import { get, post, patch } from '/shared/api.js';
import { h, button, busy, toast } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { settingsCard } from '../kit.js';

export default async function outside(ctx) {
  const settings = (await get('/admin/settings')).settings;
  const s3 = settings['files.s3'] || {};
  const storage = card('Object storage');
  const drawStorage = () => {
    const test = button('Test the stored settings', () => busy(test, async () => {
      const out = await post('/admin/settings/test-storage');
      toast(out.ok ? 'It wrote, read and deleted one small object.' : 'It failed: ' + (out.error || 'no reason given'), !out.ok);
    }));
    const edit = button('Change', async () => {
      const done = await formDialog('Object storage', [
        { name: 'store', label: 'Keep uploaded files', type: 'select', value: settings['files.store'] || 'disk', options: [['disk', 'In the data directory'], ['s3', 'In an S3-compatible store']] },
        { name: 'endpoint', label: 'Endpoint', value: s3.endpoint, placeholder: 'https://s3.example.com', code: true, when: (v) => v.store === 's3' },
        { name: 'region', label: 'Region', value: s3.region, code: true, when: (v) => v.store === 's3' },
        { name: 'bucket', label: 'Bucket', value: s3.bucket, code: true, when: (v) => v.store === 's3' },
        { name: 'prefix', label: 'Prefix', value: s3.prefix, code: true, when: (v) => v.store === 's3' },
        { name: 'path_style', label: 'Path-style addresses', type: 'checkbox', value: s3.path_style, hint: 'Most stores that are not Amazon’s need this.', when: (v) => v.store === 's3' },
        { name: 'access_key_id', label: 'Access key', value: s3.access_key_id, code: true, when: (v) => v.store === 's3' },
        { name: 'secret', label: 'Secret key', code: true, hint: settings['files.s3_secret_set'] ? 'One is stored. Leave empty to keep it. It is never shown.' : 'Never shown again once saved.', when: (v) => v.store === 's3' },
      ], { submit: async (v) => {
        const body = { 'files.store': v.store };
        if (v.store === 's3') {
          body['files.s3'] = { endpoint: v.endpoint, region: v.region, bucket: v.bucket, prefix: v.prefix, path_style: v.path_style, access_key_id: v.access_key_id };
          if (v.secret) body['files.s3_secret'] = v.secret;
        }
        Object.assign(settings, (await patch('/admin/settings', body)).settings);
      } });
      if (done) { toast('Saved.'); ctx.redraw(); }
    }, 'small');
    storage.replaceChildren(h('h2', {}, 'Object storage', edit),
      h('p', {}, settings['files.store'] === 's3'
        ? ['Files are kept in the bucket ', h('code', {}, s3.bucket || '?'), ' at ', h('code', {}, s3.endpoint || '?'), '.']
        : 'Files are kept in the data directory. The server reaches no object store.'),
      settings['files.store'] === 's3' ? h('p', { class: 'row' }, test) : '');
  };
  drawStorage();
  return [
    h('p', { class: 'note' }, 'A Churr server talks to nothing outside itself unless something here is on. Certificate renewal is decided when the server starts; identity providers are under Company sign-in; links to other servers are under Linked servers.'),
    settingsCard('Waking phones', null, [
      { key: 'push.relay_enabled', label: 'Push relay', type: 'switch', fallback: false,
        hint: 'To wake an iPhone or a Play Store Android phone, the server tells the relay which device. It never sends what was said. Off until it is switched on and has an address.' },
      { key: 'push.relay_url', label: 'Relay address', type: 'code', placeholder: 'https://relay.example.org', hint: 'There is no relay by default. A relay wakes only the apps whose push keys it holds.' },
      { key: 'push.unifiedpush_enabled', label: 'UnifiedPush', type: 'switch', fallback: false,
        hint: 'Wake Android phones at an address the phone registered with a UnifiedPush distributor.' },
      { key: 'push.send_badge', label: 'Badge numbers in wake-ups', type: 'switch', fallback: true,
        hint: 'A wake-up carries the unread count for the app’s icon. The relay then sees that number.' },
    ], settings),
    settingsCard('Fetching pages', null, [
      { key: 'previews.enabled', label: 'Link previews', type: 'switch', fallback: false,
        hint: 'When someone posts a link, the server fetches the page once for its title and picture. The site sees the server’s address.' },
    ], settings),
    storage,
    settingsCard('Sealed chats and calls', 'Neither reaches outside by itself. Calls open the media port to people’s devices.', [
      { key: 'sealed.enabled', label: 'Sealed chats', type: 'switch', fallback: true, hint: 'Two people, encrypted on their devices. The server cannot read them.' },
      { key: 'calls.enabled', label: 'Calls', type: 'switch', fallback: true },
      { key: 'calls.max_participants', label: 'Most people in one call', type: 'number', min: 2, max: 200, fallback: 50 },
      { key: 'calls.max_cameras', label: 'Most cameras on in one call', type: 'number', min: 1, max: 50, fallback: 12 },
    ], settings),
    settingsCard('Linked servers', null, [
      { key: 'links.send_presence', label: 'Send presence to linked servers', type: 'switch', fallback: false,
        hint: 'People on a linked server who share a channel with someone here see whether they are online.' },
      { key: 'links.default_max_file_bytes', label: 'Largest file fetched from another server', type: 'bytes' },
    ], settings),
  ];
}
