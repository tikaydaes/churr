// Calls: live calls with participant counts (never who), the media
// address in use, and "Test calls" (docs/CALLS.md section 12).

import { get, post } from '/shared/api.js';
import { h, button, busy, dateTime } from '/shared/dom.js';
import { card, tryLoad, sectionProblem } from '/shared/console.js';

export default async function calls(ctx) {
  const got = await tryLoad(() => get('/admin/calls'));
  const result = h('div', { class: 'stack', 'aria-live': 'polite' });
  // The test: this browser makes a media connection to the server with a
  // data channel only, and reports whether it came up. It needs no camera
  // or microphone.
  const test = button('Test calls', () => busy(test, async () => {
    result.replaceChildren(h('p', { class: 'loading' }, 'Testing'));
    const pc = new RTCPeerConnection();
    try {
      pc.createDataChannel('test');
      await pc.setLocalDescription(await pc.createOffer());
      const out = await post('/admin/calls/test', { sdp: pc.localDescription.sdp });
      await pc.setRemoteDescription({ type: 'answer', sdp: out.sdp });
      const ok = await new Promise((resolve) => {
        const timer = setTimeout(() => resolve(false), 8000);
        pc.addEventListener('connectionstatechange', () => {
          if (pc.connectionState === 'connected') { clearTimeout(timer); resolve(true); }
          if (pc.connectionState === 'failed') { clearTimeout(timer); resolve(false); }
        });
      });
      // Which path was chosen, and which of UDP and TCP answered at all:
      // the browser checks both against the server's candidates.
      const seen = { udp: false, tcp: false };
      let how = '', where = '';
      const stats = await pc.getStats();
      let pair = null;
      stats.forEach((s) => { if (s.type === 'transport' && s.selectedCandidatePairId) pair = stats.get(s.selectedCandidatePairId); });
      stats.forEach((s) => {
        if (s.type !== 'candidate-pair') return;
        if (!pair && s.nominated && s.state === 'succeeded') pair = s;
        const r = stats.get(s.remoteCandidateId);
        if (r && s.state === 'succeeded' && seen[r.protocol] !== undefined) seen[r.protocol] = true;
      });
      const remote = pair && stats.get(pair.remoteCandidateId);
      if (remote) { how = (remote.protocol || '').toUpperCase(); where = `${remote.address || remote.ip}:${remote.port}`; }
      const both = `UDP ${seen.udp ? 'answered' : 'did not answer'}, TCP ${seen.tcp ? 'answered' : 'did not answer'}.`;
      result.replaceChildren(h('p', { class: 'note ' + (ok ? '' : 'danger') }, ok
        ? `A media connection came up${how ? ' over ' + how : ''}${where ? ' to ' + where : ''}. ${both}${how === 'TCP' ? ' Media over TCP stalls when packets are lost: open UDP on the media port too.' : ''}`
        : `No media connection to ${out.media_address || 'the server'} came up in eight seconds. The media port is probably closed, or the media address is wrong. A server whose media port cannot be reached cannot host calls.`),
        h('p', { class: 'dim small' }, `Media address in use: ${out.media_address || 'none'}.`));
    } catch (err) {
      result.replaceChildren(sectionProblem(err));
    } finally { pc.close(); }
  }), 'primary');
  return [
    card('Live calls', got.problem || [
      h('p', {}, 'Media address in use: ', h('code', {}, got.ok.media_address || 'none')),
      got.ok.calls.length ? h('ul', { class: 'rows' }, got.ok.calls.map((c) => h('li', {}, h('div', { class: 'what' },
        h('strong', {}, `${c.participant_count} in the call, ${c.camera_count} with a camera on`), h('small', {}, 'Since ' + dateTime(c.started_at)))))) : h('p', { class: 'empty' }, 'No call is on.'),
    ]),
    card('Test calls',
      h('p', {}, ctx.server.features.calls ? 'Checks that a browser can reach the server’s media port. The quickest way to find a closed port or a wrong media address.' : 'Calls are turned off on this server (Settings, Outside connections).'),
      h('p', { class: 'row' }, test), result),
  ];
}
