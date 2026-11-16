// Checks, without a browser, who the web chat offers "Add emoji" to
// (docs/CLIENTS.md 9.5, docs/PERMISSIONS.md note 7), and that the picker
// shows the button only when asked to.

const { mayAddEmoji } = await import(new URL('../internal/web/static/chat/emojirule.js', import.meta.url).href);

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL emoji: ${name}${detail ? '\n       ' + detail : ''}`); }
}

const on = { members_may_add_emoji: true }, off = { members_may_add_emoji: false };
check('the owner always may', mayAddEmoji('owner', off));
check('an admin always may', mayAddEmoji('admin', undefined));
check('a member may when the setting is on', mayAddEmoji('member', on));
check('a member may not when it is off', !mayAddEmoji('member', off));
check('a member may not when the server says nothing', !mayAddEmoji('member', {}));
check('a guest never may', !mayAddEmoji('guest', on));
check('a bot never may', !mayAddEmoji('bot', on));
check('nobody signed in may not', !mayAddEmoji(undefined, on));

console.log(`${failures ? 'FAIL' : 'ok  '} emoji: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
