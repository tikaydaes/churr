// The second a draw is asked for (docs/FAIR.md section 6): the client's own
// clock, corrected to the server's. A post that takes chance carries it as
// draw_t beside its client_id, and the two are its draw's input. The
// correction is learnt from the server: a post refused with `clock` says
// the server's time, and the offset to it is kept for the rest of the load.

let offsetMs = 0;

/** The second to give a draw now: Unix time, by the corrected clock. */
export function drawT(now = Date.now()) {
  return Math.floor((now + offsetMs) / 1000);
}

/** Learns the offset from the server's time, an RFC 3339 string. */
export function learnServerTime(serverTime, now = Date.now()) {
  const t = Date.parse(serverTime);
  if (Number.isFinite(t)) offsetMs = t - now;
}

/** The offset in use, for tests. */
export const clockOffset = () => offsetMs;
