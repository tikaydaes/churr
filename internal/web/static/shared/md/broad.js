// Broad mentions: `@channel`, `@here` and `@everyone` (ruling R64;
// docs/FEATURES.md section 2.5).
//
// In a large channel only its managers and the admins may use them. Anyone
// else's is posted as plain text and notifies nobody, and the composer says
// so before sending. Those who may are asked to confirm when a great many
// people would be notified. Whether someone may, and how many people a
// mention reaches, is the server's to say: this file only finds the words
// and decides what to ask.
//
// It has no DOM in it, so it runs under Node as it is (e2e/broad.mjs).

import { parse } from './parse.js';

const BROAD = ['channel', 'here', 'everyone'];

export const BROAD_PLAIN = 'Only managers can use @channel, @here and @everyone here. This will be sent as plain text and will notify nobody.';
export const BROAD_WAS_PLAIN = 'Only managers can use @channel, @here and @everyone here. Your message was sent with them as plain text and notified nobody.';

// broadMentions lists the broad mentions written in a text, each once, in
// the order they first appear. A word inside code is not a mention, and
// neither is `@here@server.name`.
export function broadMentions(text) {
  const found = [];
  const walk = (v) => {
    if (Array.isArray(v)) { for (const n of v) walk(n); return; }
    if (!v || typeof v !== 'object') return;
    if (v.t === 'mention' && !v.server && BROAD.includes(v.name) && !found.includes(v.name)) found.push(v.name);
    for (const k in v) if (typeof v[k] === 'object') walk(v[k]);
  };
  walk(parse(text));
  return found;
}

// broadHint is the line shown under the box while the text holds a broad
// mention and the conversation says its reader may not use one; otherwise
// null. A conversation that does not say (an older server, or one that has
// only just been joined) gives no hint: the server answers when asked.
export function broadHint(text, conversation) {
  if (!conversation || conversation.kind === 'sealed' || conversation.broad_mentions_allowed !== false) return null;
  return broadMentions(text).length ? BROAD_PLAIN : null;
}

// confirmLine is the question put to someone about to notify many people.
export function confirmLine(n) {
  return `This will notify ${Number(n).toLocaleString('en')} people.`;
}

// beforeSend decides whether a text may go as it is. `ask` asks the server
// what the text's broad mentions would do (checkBroadMentions) and
// `confirm` puts the question to the person, answering true to send. It
// resolves true to send and false when the person chose not to.
//
// Nothing is asked for a text with no broad mention, in a sealed chat (the
// server cannot read it), or where the conversation already says no: that
// text goes as plain text and the hint has said so. If the server cannot be
// asked the message is sent all the same, since the post decides anyway.
export async function beforeSend(text, conversation, ask, confirm) {
  if (!conversation || conversation.kind === 'sealed') return true;
  if (!broadMentions(text).length) return true;
  if (conversation.broad_mentions_allowed === false) return true;
  let out;
  try { out = await ask(text); } catch { return true; }
  if (!out || !out.broad || !out.allowed || !out.confirm) return true;
  return (await confirm(out.recipients)) === true;
}
