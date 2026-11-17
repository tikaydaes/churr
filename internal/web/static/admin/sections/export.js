// Export (Owners): start an export, download it when it is ready. Both need
// a check of who you are from the last ten minutes; the download is asked
// for as a one-time address, which is what the audit log records.

import { get, post } from '/shared/api.js';
import { h, button, busy, toast, dateTime, bytes, confirmDialog } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';
import { people, name } from '../kit.js';

export default async function exportSection(ctx) {
  const got = await tryLoad(() => get('/admin/export'));
  const ex = got.ok && got.ok.export;
  if (ex) await people([ex.started_by_id]);
  const start = button(ex ? 'Start a new export' : 'Start an export', async () => {
    if (!(await confirmDialog('Start an export?', ['The export holds every channel, private and ad-hoc ones included, and every account. Sealed chats are left out entirely.', 'It is written to the audit log and every owner is told. One export runs at a time.'], { yes: 'Start' }))) return;
    await busy(start, async () => { await post('/admin/export'); toast('The export has started.'); ctx.redraw(); });
  }, 'primary');
  start.disabled = !!(ex && ex.state === 'running');
  const download = button('Download', () => busy(download, async () => {
    const link = await post('/admin/export/download');
    const a = h('a', { href: link.url, download: '', hidden: true });
    document.body.append(a);
    a.click();
    a.remove();
    toast('The download has started. It is written to the audit log and every owner is told.');
  }), 'small primary');
  // A running export is asked about again every few seconds while this
  // section is open.
  if (ex && ex.state === 'running') setTimeout(() => { if (location.pathname.endsWith('/export')) ctx.redraw(); }, 4000);
  return card('Export',
    h('p', {}, 'Everything the server holds that it can read, as one archive, for moving to another server or keeping a copy. The archive is deleted 24 hours after it is ready.'),
    got.problem || (ex ? h('ul', { class: 'rows' }, h('li', {},
      h('div', { class: 'what' }, h('strong', {}, 'Export ', h('span', { class: 'tag ' + (ex.state === 'ready' ? 'green' : ex.state === 'failed' ? 'red' : ex.state === 'expired' ? 'grey' : 'yellow') }, ex.state)),
        h('small', {}, `Started by ${name(ex.started_by_id)} on ${dateTime(ex.started_at)}` + (ex.finished_at ? ` · finished ${dateTime(ex.finished_at)}` : '') + (ex.size ? ` · ${bytes(ex.size)}` : '')),
        ex.error && h('small', { class: 'note danger' }, ex.error)),
      ex.state === 'ready' && h('div', { class: 'does' }, download))) : h('p', { class: 'empty' }, 'No export has been made.')),
    h('p', { class: 'row' }, start));
}
