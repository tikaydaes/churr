// The API client every page uses: one fetch wrapper, the error shape,
// paging, uploads with progress, and the two answers that are not failures
// of the request but of the session: "sign in" and "prove it is you again".
//
// The pages have no private endpoints. Everything here is an operation of
// docs/openapi.yaml, named in the comments by its operationId.

import { forgetDrafts, browserStorage } from './drafts.js';
import { drawT, learnServerTime } from './fair/clock.js';

export class ApiError extends Error {
  constructor(status, body) {
    const e = (body && body.error) || {};
    super(e.message || (status ? `The server answered ${status}.` : 'The server could not be reached.'));
    this.name = 'ApiError';
    this.status = status;
    this.code = e.code || (status ? 'internal' : 'network');
    this.field = e.field;
    this.retryAfter = e.retry_after_seconds;
    this.serverTime = e.server_time;
  }
}

// What to do when the API says the session needs a fresh check. A page
// sets this to the shared dialog (fresh.js); it resolves true once the
// check passed, and the request is then made again.
let freshHandler = null;
export function onFreshCheck(fn) { freshHandler = fn; }

// What to do when the session is gone. The pages that need a session send
// the person to sign in and come back; the sign-in pages leave it alone.
let signedOutHandler = null;
export function onSignedOut(fn) { signedOutHandler = fn; }

// What to do when the session is locked (the lock, docs/AUTH.md section
// 10.6): every page that needs a session goes to the lock screen, which
// draws nothing of the account. The lock screen itself turns this off.
let lockedHandler = () => goToLock();
export function onLocked(fn) { lockedHandler = fn; }

// goToLock replaces this page with the lock screen. Replacing, not adding
// to the history, so Back cannot bring the page and what it held back; the
// page is unloaded, and everything it held in memory with it.
let goingToLock = false;
export function goToLock() {
  // Several things notice the lock at once (the tab that locked, the live
  // connection, a request); one navigation is enough.
  if (goingToLock) return;
  goingToLock = true;
  // A locked browser keeps nothing of the account that can be read: the
  // web chat's drafts go with the page.
  forgetDrafts(browserStorage);
  const here = location.pathname + location.search;
  location.replace('/locked/?next=' + encodeURIComponent(here));
}

export function goToSignIn() {
  const here = location.pathname + location.search + location.hash;
  location.assign('/login?next=' + encodeURIComponent(here));
}

function query(params) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params || {})) {
    if (v == null || v === '' || v === false) continue;
    q.set(k, v === true ? 'true' : String(v));
  }
  const s = q.toString();
  return s ? '?' + s : '';
}

// api makes one request. path is everything after /api/v1 (or a full path
// starting with "/oauth"). body is sent as JSON; a FormData body is sent
// as it is. opts.query adds query parameters; opts.raw returns the
// Response.
export async function api(method, path, body, opts = {}) {
  const url = (path.startsWith('/oauth') ? path : '/api/v1' + path) + query(opts.query);
  const init = { method, credentials: 'same-origin', headers: {} };
  if (body instanceof FormData) init.body = body;
  else if (body !== undefined) { init.headers['Content-Type'] = 'application/json'; init.body = JSON.stringify(body); }
  if (opts.signal) init.signal = opts.signal;
  let res;
  try {
    res = await fetch(url, init);
  } catch (err) {
    if (err.name === 'AbortError') throw err;
    throw new ApiError(0);
  }
  if (opts.raw && res.ok) return res;
  let data = {};
  if (res.status !== 204) {
    try { data = await res.json(); } catch { /* an empty or non-JSON answer */ }
  }
  if (res.ok) return data;
  const err = new ApiError(res.status, data);
  // Past the request limit the server refuses before it does anything, and
  // says how long to wait. Waiting and asking again is what it asks for
  // (docs/API.md section 10). `slow_mode` has the same status and is not
  // this: it is about the channel, and the person is told.
  if (err.code === 'rate_limited' && (opts.tries || 0) < 3 && (err.retryAfter || 1) <= 20) {
    await new Promise((r) => setTimeout(r, (err.retryAfter || 1) * 1000));
    return api(method, path, body, { ...opts, tries: (opts.tries || 0) + 1 });
  }
  // A draw's second too far from the server's clock, or one the server
  // cannot vouch for just after it started (docs/FAIR.md section 6): the
  // clock is corrected by the server's time and the post sent once more,
  // with the same client_id and the corrected second, a second later if
  // that is the same one.
  if (err.code === 'clock' && body && body.draw_t && !opts.clockTried) {
    learnServerTime(err.serverTime);
    let t = drawT();
    if (t <= body.draw_t) { await new Promise((r) => setTimeout(r, 1100)); t = drawT(); }
    return api(method, path, { ...body, draw_t: t }, { ...opts, clockTried: true });
  }
  if (err.code === 'fresh_check_required' && freshHandler && !opts.noFresh) {
    if (await freshHandler()) return api(method, path, body, { ...opts, noFresh: true });
    err.silent = true; // the person closed the dialog; nothing more to say
    throw err;
  }
  if (err.code === 'session_locked') {
    if (lockedHandler) lockedHandler(err);
    throw err;
  }
  if (res.status === 401 && signedOutHandler && !opts.quiet401) signedOutHandler(err);
  throw err;
}

export const get = (path, q) => api('GET', path, undefined, { query: q });
export const post = (path, body) => api('POST', path, body === undefined ? {} : body);
export const put = (path, body) => api('PUT', path, body === undefined ? {} : body);
export const patch = (path, body) => api('PATCH', path, body);
export const del = (path, q) => api('DELETE', path, undefined, { query: q });

// all follows next_cursor until a cursor-paged list is whole. key is the
// name of the list in the answer. max stops a runaway list.
export async function all(path, key, q = {}, max = 2000) {
  const out = [];
  let cursor;
  do {
    const page = await get(path, { limit: 200, ...q, cursor });
    out.push(...(page[key] || []));
    cursor = page.next_cursor;
  } while (cursor && out.length < max);
  return out;
}

// upload posts a multipart body and reports progress as a fraction. It
// returns { promise, abort }. fetch cannot report upload progress, so this
// one request uses XMLHttpRequest.
export function upload(path, form, onProgress, method = 'POST') {
  const xhr = new XMLHttpRequest();
  const promise = new Promise((resolve, reject) => {
    xhr.open(method, '/api/v1' + path);
    xhr.responseType = 'json';
    xhr.upload.onprogress = (e) => { if (e.lengthComputable && onProgress) onProgress(e.loaded / e.total); };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve(xhr.response || {});
      else reject(new ApiError(xhr.status, xhr.response));
    };
    xhr.onerror = () => reject(new ApiError(0));
    xhr.onabort = () => { const e = new Error('Cancelled'); e.name = 'AbortError'; reject(e); };
    xhr.send(form);
  });
  return { promise, abort: () => xhr.abort() };
}

// safeNext keeps a "come back here" address on this server: a path, never
// another site. Anything else becomes the fallback.
export function safeNext(value, fallback = '/chat/') {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) return fallback;
  return value;
}

// unavailableText says plainly that an area of the server is not built or
// not reachable, for the screens whose operations answer `unavailable`.
export function isUnavailable(err) {
  return err && (err.code === 'unavailable' || err.code === 'feature_off');
}
