// The browser's side of a call's media: what the browser allows
// (docs/CALLS.md section 13), the camera, microphone and screen, the
// sizes a camera is sent in (section 7) and how a screen is sent
// (section 9).

// --- what this browser and page can do ---

// secure is whether the camera and microphone can work here at all: only
// on HTTPS, or from localhost.
export function secure() {
  return !!(window.isSecureContext && navigator.mediaDevices && navigator.mediaDevices.getUserMedia);
}

// phone is a phone or tablet browser, where screens cannot be shared and
// the camera is sent in two sizes.
export function phone() {
  const ua = navigator.userAgent || '';
  return /Android|iPhone|iPad|iPod|Mobile/i.test(ua) || (navigator.maxTouchPoints > 1 && /Macintosh/.test(ua) && 'ontouchend' in document);
}

export function iphone() {
  const ua = navigator.userAgent || '';
  return /iPhone|iPad|iPod/.test(ua) || (navigator.maxTouchPoints > 1 && /Macintosh/.test(ua));
}

// canShareScreen: desktop browsers with the screen-capture interface.
export function canShareScreen() {
  return !phone() && !!(navigator.mediaDevices && navigator.mediaDevices.getDisplayMedia);
}

// decodes lists the video codecs of the server's list this browser can
// receive.
export function decodes() {
  const out = [];
  try {
    const caps = RTCRtpReceiver.getCapabilities && RTCRtpReceiver.getCapabilities('video');
    const names = new Set(((caps && caps.codecs) || []).map((c) => c.mimeType.toLowerCase()));
    if (names.has('video/vp8')) out.push('vp8');
    if (names.has('video/h264')) out.push('h264');
  } catch { /* an old browser: say VP8, which every one has */ }
  return out.length ? out : ['vp8'];
}

// simulcast is whether this browser sends a camera in several sizes on one
// media line. It is found out once, with a connection that is never used.
let simulcastKnown = null;
export function simulcast() {
  if (simulcastKnown !== null) return simulcastKnown;
  simulcastKnown = false;
  let pc;
  try {
    pc = new RTCPeerConnection();
    const t = pc.addTransceiver('video', { direction: 'sendonly', sendEncodings: [{ rid: 'q' }, { rid: 'h' }, { rid: 'f' }] });
    const enc = t.sender.getParameters().encodings || [];
    simulcastKnown = enc.length === 3 && enc.every((e) => e.rid);
  } catch { simulcastKnown = false; }
  if (pc) pc.close();
  return simulcastKnown;
}

// --- devices ---

export const prefs = { mic: '', camera: '', speaker: '' };
try { Object.assign(prefs, JSON.parse(localStorage.getItem('churr-call-devices') || '{}')); } catch { /* kept for this page only */ }
export function savePrefs() { try { localStorage.setItem('churr-call-devices', JSON.stringify(prefs)); } catch { /* kept for this page only */ } }

export async function devices() {
  try {
    const list = await navigator.mediaDevices.enumerateDevices();
    return {
      mics: list.filter((d) => d.kind === 'audioinput'),
      cameras: list.filter((d) => d.kind === 'videoinput'),
      speakers: list.filter((d) => d.kind === 'audiooutput'),
    };
  } catch { return { mics: [], cameras: [], speakers: [] }; }
}

// The camera is asked for at the largest layer's size; a phone at the
// middle one.
function cameraConstraints() {
  const big = !phone();
  return {
    width: { ideal: big ? 1280 : 640 }, height: { ideal: big ? 720 : 360 }, frameRate: { ideal: 30, max: 30 },
    ...(prefs.camera ? { deviceId: { exact: prefs.camera } } : {}),
  };
}

function micConstraints() {
  return { echoCancellation: true, noiseSuppression: true, autoGainControl: true, channelCount: 1, ...(prefs.mic ? { deviceId: { exact: prefs.mic } } : {}) };
}

// open asks for the microphone and camera. A refusal, or a device that is
// missing, is not an error here: the person joins with what they have and
// is told what that means. Returns { mic, camera, problems: [] }.
export async function open({ audio = true, video = true } = {}) {
  const out = { mic: null, camera: null, problems: [] };
  if (!secure()) {
    out.problems.push('insecure');
    return out;
  }
  const ask = async (kind) => {
    const c = kind === 'audio' ? { audio: micConstraints() } : { video: cameraConstraints() };
    try {
      const s = await navigator.mediaDevices.getUserMedia(c);
      return s.getTracks()[0] || null;
    } catch (err) {
      // A remembered device that has gone: ask again for any.
      if (err && err.name === 'OverconstrainedError') {
        if (kind === 'audio') prefs.mic = ''; else prefs.camera = '';
        try { return (await navigator.mediaDevices.getUserMedia(kind === 'audio' ? { audio: true } : { video: cameraConstraints() })).getTracks()[0] || null; } catch { /* fall through */ }
      }
      out.problems.push(problemOf(err, kind));
      return null;
    }
  };
  if (audio) out.mic = await ask('audio');
  if (video) out.camera = await ask('video');
  return out;
}

export async function openCamera() { return (await open({ audio: false, video: true })); }
export async function openMic() { return (await open({ audio: true, video: false })); }

function problemOf(err, kind) {
  const name = (err && err.name) || '';
  if (name === 'NotAllowedError' || name === 'SecurityError') return kind === 'audio' ? 'mic-refused' : 'camera-refused';
  if (name === 'NotFoundError' || name === 'OverconstrainedError') return kind === 'audio' ? 'no-mic' : 'no-camera';
  if (name === 'NotReadableError') return kind === 'audio' ? 'mic-busy' : 'camera-busy';
  return kind === 'audio' ? 'mic-failed' : 'camera-failed';
}

// problemText says what a media problem means and what to do about it.
export function problemText(p) {
  switch (p) {
    case 'insecure': return 'This page is not on HTTPS, so the browser allows no camera or microphone. Calls in the web chat need the server reached over HTTPS, or from localhost. You can still listen.';
    case 'mic-refused': return 'The browser was refused the microphone, so you join to listen only. To speak, allow the microphone for this site in the browser’s address bar or site settings, then turn the microphone on.';
    case 'camera-refused': return 'The browser was refused the camera. To show your camera, allow it for this site in the browser’s address bar or site settings, then turn the camera on.';
    case 'no-mic': return 'No microphone was found, so you join to listen only.';
    case 'no-camera': return 'No camera was found.';
    case 'mic-busy': return 'The microphone is in use by another program.';
    case 'camera-busy': return 'The camera is in use by another program.';
    case 'screen-refused': return 'Sharing the screen was cancelled or refused.';
    default: return p && p.startsWith('mic') ? 'The microphone could not be opened.' : 'The camera could not be opened.';
  }
}

// screen asks the browser's own picker for a screen or a window. The
// screen's sound is not shared (CALLS.md section 9).
export async function screen() {
  const s = await navigator.mediaDevices.getDisplayMedia({ video: { width: { max: 1920 }, height: { max: 1080 }, frameRate: { ideal: 15, max: 15 } }, audio: false });
  const t = s.getVideoTracks()[0];
  // Sharp detail over smooth motion.
  try { t.contentHint = 'detail'; } catch { /* not every browser has hints */ }
  return t;
}

// --- how tracks are sent ---

// cameraEncodings are the layers of CALLS.md section 7: q, h and f on a
// desktop, q and h on a phone, smallest first, each with its own rid.
export function cameraEncodings(track, many) {
  if (!many) return [{ maxBitrate: 1_200_000, maxFramerate: 30 }];
  const s = track && track.getSettings ? track.getSettings() : {};
  const height = s.height || (phone() ? 360 : 720);
  if (phone() || height <= 400) {
    return [
      { rid: 'q', scaleResolutionDownBy: Math.max(1, height / 180), maxBitrate: 150_000, maxFramerate: 15 },
      { rid: 'h', scaleResolutionDownBy: Math.max(1, height / 360), maxBitrate: 500_000, maxFramerate: 30 },
    ];
  }
  return [
    { rid: 'q', scaleResolutionDownBy: height / 180, maxBitrate: 150_000, maxFramerate: 15 },
    { rid: 'h', scaleResolutionDownBy: height / 360, maxBitrate: 500_000, maxFramerate: 30 },
    { rid: 'f', scaleResolutionDownBy: Math.max(1, height / 720), maxBitrate: 1_200_000, maxFramerate: 30 },
  ];
}

export const SCREEN_ENCODING = { maxBitrate: 2_500_000, maxFramerate: 15 };

// preferVP8 puts VP8 first on a video transceiver, so it is what the
// browser publishes unless it cannot (CALLS.md section 6).
export function preferVP8(transceiver, only) {
  try {
    const caps = RTCRtpSender.getCapabilities('video');
    if (!caps || !transceiver.setCodecPreferences) return;
    const rank = (c) => {
      const m = c.mimeType.toLowerCase();
      if (m === 'video/vp8') return 0;
      if (m === 'video/h264') return /packetization-mode=1/.test(c.sdpFmtpLine || '') && /42e01f/.test(c.sdpFmtpLine || '') ? 1 : 2;
      if (m === 'video/rtx' || m === 'video/red' || m === 'video/ulpfec') return 3;
      return 4;
    };
    // An encrypted call sends VP8 and nothing else.
    const list = caps.codecs.filter((c) => rank(c) < 4 && !(only && rank(c) > 0 && rank(c) < 3)).sort((a, b) => rank(a) - rank(b));
    if (list.length) transceiver.setCodecPreferences(list);
  } catch { /* the browser picks; the server prefers VP8 anyway */ }
}

// level measures how loud a microphone is, 0 to 1, for the meter before
// joining and the reader's own speaking mark. It returns { read(), stop() }.
export function meter(track) {
  let ctx, analyser, data;
  try {
    ctx = new AudioContext();
    const src = ctx.createMediaStreamSource(new MediaStream([track]));
    analyser = ctx.createAnalyser();
    analyser.fftSize = 512;
    src.connect(analyser);
    data = new Uint8Array(analyser.fftSize);
  } catch { return { read: () => 0, stop() {} }; }
  return {
    read() {
      analyser.getByteTimeDomainData(data);
      let sum = 0;
      for (const v of data) { const x = (v - 128) / 128; sum += x * x; }
      return Math.min(1, Math.sqrt(sum / data.length) * 4);
    },
    stop() { try { ctx.close(); } catch { /* closed */ } },
  };
}
