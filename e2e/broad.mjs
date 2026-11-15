// Checks, without a browser, what the web chat's composer does about broad
// mentions (ruling R64): which words it takes for one, when it warns that a
// message will go as plain text, and when it asks before notifying many
// people.

import { broadMentions, broadHint, beforeSend, confirmLine, BROAD_PLAIN } from '../internal/web/static/shared/md/broad.js';

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL broad: ${name}${detail ? '\n       ' + detail : ''}`); }
}
const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

// Which words are broad mentions.
for (const [text, want] of [
  ['@channel lunch', ['channel']],
  ['hello @here and @everyone, and @here again', ['here', 'everyone']],
  ['- [ ] tell @channel\n> and @here', ['channel', 'here']],
  ['**@channel**', ['channel']],
  ['type `@channel` to shout', []],
  ['```\n@here\n```', []],
  ['mail me@here.example', []],
  ['@channels and @hereafter', []],
  ['@admins and @sam', []],
  ['@here@other.example', []],
  ['', []],
]) {
  const got = broadMentions(text);
  check(`the broad mentions of ${JSON.stringify(text)}`, same(got, want), `got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

// The hint: only where the conversation says no.
const open = { kind: 'channel', broad_mentions_allowed: true };
const closed = { kind: 'channel', broad_mentions_allowed: false };
const unknown = { kind: 'channel' };
check('a hint where broad mentions are not allowed', broadHint('@channel hi', closed) === BROAD_PLAIN);
check('no hint without a broad mention', broadHint('hi @sam', closed) === null);
check('no hint for one in code', broadHint('`@channel`', closed) === null);
check('no hint where they are allowed', broadHint('@channel hi', open) === null);
check('no hint where the conversation does not say', broadHint('@channel hi', unknown) === null);
check('no hint in a sealed chat', broadHint('@channel hi', { kind: 'sealed', broad_mentions_allowed: false }) === null);
check('the hint says it goes as plain text and notifies nobody', /plain text/.test(BROAD_PLAIN) && /notify nobody/.test(BROAD_PLAIN));

// Before sending: who is asked what.
async function run(text, conversation, answer, says) {
  const seen = { asked: 0, confirmed: [] };
  const ask = async (t) => { seen.asked++; seen.text = t; if (answer instanceof Error) throw answer; return answer; };
  const confirm = async (n) => { seen.confirmed.push(n); return says; };
  seen.send = await beforeSend(text, conversation, ask, confirm);
  return seen;
}
const many = { broad: true, allowed: true, recipients: 4200, confirm: true };
const few = { broad: true, allowed: true, recipients: 12, confirm: false };

let r = await run('hello there', open, many, true);
check('a text with no broad mention is sent without asking', r.send && r.asked === 0);
r = await run('@channel hi', closed, many, true);
check('where they are not allowed the text is sent as it is, without asking', r.send && r.asked === 0 && r.confirmed.length === 0);
r = await run('@here hi', open, few, true);
check('a few recipients: asked of the server, not of the person', r.send && r.asked === 1 && r.text === '@here hi' && r.confirmed.length === 0);
r = await run('@channel hi', open, many, true);
check('many recipients: the person is asked, with the server\'s number', r.send && same(r.confirmed, [4200]));
r = await run('@channel hi', open, many, false);
check('many recipients and the person says no: not sent', r.send === false && same(r.confirmed, [4200]));
r = await run('@channel hi', open, many, undefined);
check('a dialog closed without an answer does not send', r.send === false);
r = await run('@everyone hi', unknown, many, true);
check('where the conversation does not say, the server is asked', r.asked === 1 && same(r.confirmed, [4200]));
r = await run('@channel hi', open, { broad: true, allowed: false, recipients: 0, confirm: false }, true);
check('the server says no after all: sent as it is, nothing to confirm', r.send && r.confirmed.length === 0);
r = await run('@channel hi', open, new Error('offline'), true);
check('the server cannot be asked: sent, and the post decides', r.send && r.confirmed.length === 0);
r = await run('@channel hi', { kind: 'sealed' }, many, true);
check('a sealed chat asks nothing', r.send && r.asked === 0);

check('the question names the number', confirmLine(4200) === 'This will notify 4,200 people.', confirmLine(4200));

console.log(`${failures ? 'FAIL' : 'ok  '} broad: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
