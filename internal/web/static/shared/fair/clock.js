// The second a draw is asked for (docs/FAIR.md section 6): the client's own
// clock, corrected to the server's. A post that takes chance carries it as
// draw_t beside its client_id, and the two are its draw's input.
//
// An honest client is never refused, so the correction is known before the
// first draw and kept current from ordinary traffic: the live connection's
// hello and every pong say the server's time, and every answer of the API
// its Date header (whole seconds, taken only until the live connection has
// said better). A `clock` refusal is not routine: it is shown to the person
// and never answered by sending the draw again; the time it states is learnt
// for later posts only.
//
// A stated time more than five minutes from this device's own clock is not
// taken unless nothing else has been learnt: a server that says the time is
// far from where this device thinks it is has to be believed by its first
// word or not at all.

/** How far a stated time may be from the device's clock, in milliseconds. */
export const MAX_STATED_MS = 5 * 60 * 1000;

/** The window of a draw, in seconds: as the server's (FAIR.md section 6). */
export const WINDOW = 10;

let offsetMs = 0;
// What the offset was learnt from: 'none', 'date' (a Date header, whole
// seconds) or 'live' (the live connection, or a refusal's server_time).
let source = 'none';

/** The second to give a draw now: Unix time, by the corrected clock. */
export function drawT(now = Date.now()) {
  return Math.floor((now + offsetMs) / 1000);
}

/**
 * Learns the offset from a time the server stated, an RFC 3339 string, as
 * received at now; rttMs, when known, is the round trip it came back on, and
 * half of it is taken as its age. Returns null when it was taken (or could
 * not be read), or why it was not: a time more than five minutes from this
 * device's clock while another has been learnt.
 */
export function learnServerTime(serverTime, now = Date.now(), rttMs = 0) {
  const t = Date.parse(serverTime);
  if (!Number.isFinite(t)) return null;
  return take(t + rttMs / 2 - now, 'live');
}

/** Learns from an HTTP Date header, until the live connection has spoken. */
export function learnDateHeader(date, now = Date.now()) {
  if (source === 'live' || !date) return null;
  const t = Date.parse(date);
  if (!Number.isFinite(t)) return null;
  // The header is whole seconds, cut down: half a second is its middle.
  return take(t + 500 - now, 'date');
}

function take(off, from) {
  if (Math.abs(off) > MAX_STATED_MS && source !== 'none') {
    const minutes = Math.round(Math.abs(off) / 60000);
    return `The server stated a time ${minutes} minutes from this device's clock, which this device does not take.`;
  }
  offsetMs = Math.round(off);
  source = from;
  return null;
}

/** Whether a second given to a draw is still inside its window now. */
export function inWindow(drawSecond, now = Date.now()) {
  return Math.abs(drawT(now) - drawSecond) <= WINDOW;
}

/** The offset in use, for tests. */
export const clockOffset = () => offsetMs;

/** Forgets what was learnt, for tests. */
export function resetClock() { offsetMs = 0; source = 'none'; }
