// Checks, without a browser, what the web chat and the admin console offer
// by level and role (rulings R97 and R99; docs/PERMISSIONS.md sections 2.4,
// 6 and 9.1): the levels a manager may give, the target rule of removing,
// banning and timing out, the tag beside a member, the owner override's
// wording, and the approvals queue's count and outcomes
// (internal/web/static/shared/roles.js).
//
//   node e2e/roles.mjs

const R = await import(new URL('../internal/web/static/shared/roles.js', import.meta.url).href);
const { COMMANDS, GROUP_OF, isProtected } = await import(new URL('../internal/web/static/shared/commands.js', import.meta.url).href);

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL roles: ${name}${detail ? '\n       ' + detail : ''}`); }
}
const names = (list) => list.map(([v]) => v).join(' ');

// The levels, in order.
check('four levels, reader to manager', names(R.LEVELS) === 'reader member moderator manager', names(R.LEVELS));
check('moderator ranks above member and below manager', R.rank('member') < R.rank('moderator') && R.rank('moderator') < R.rank('manager'));
check('moderators and managers keep the peace', R.keepsPeace('moderator') && R.keepsPeace('manager'));
check('members and readers do not', !R.keepsPeace('member') && !R.keepsPeace('reader') && !R.keepsPeace(null));

// What a manager may set: a guest or a remote account reads or is a
// member; in a channel a hub owns the hub's lists set the rest.
const person = { id: 'p', role: 'member' };
check('a member may be given every level', names(R.levelChoices(person)) === 'reader member moderator manager');
check('an admin too: the role does not matter', names(R.levelChoices({ id: 'a', role: 'admin' })) === 'reader member moderator manager');
check('a bot may be a moderator or a manager', names(R.levelChoices({ id: 'b', role: 'bot' })) === 'reader member moderator manager');
check('a guest only reads or is a member', names(R.levelChoices({ id: 'g', role: 'guest' })) === 'reader member');
check('someone of another server only reads or is a member', names(R.levelChoices({ id: 'r', role: 'remote', server_name: 'chat.example.org' })) === 'reader member');
check('in a hub channel a member is given reader or member only', names(R.levelChoices(person, { hubOwned: true, level: 'member' })) === 'reader member');
check('in a hub channel a moderator on the lists is given nothing here', R.levelChoices(person, { hubOwned: true, level: 'moderator' }).length === 0);
check('nobody but a person of this instance may hold a keeper level', !R.mayHoldKeeper({ role: 'guest' }) && !R.mayHoldKeeper({ role: 'remote' }) && R.mayHoldKeeper({ role: 'owner' }));

// The target rule.
const me = 'me';
const moderator = { meId: me, manager: false, keeper: true };
const manager = { meId: me, manager: true, keeper: true };
const member = { meId: me, manager: false, keeper: false };
check('a moderator acts on a member', R.mayActOn(moderator, { id: 'x', role: 'member', level: 'member' }));
check('a moderator acts on an admin, who has no shield in a channel', R.mayActOn(moderator, { id: 'x', role: 'admin', level: 'member' }));
check('a moderator does not act on another moderator', !R.mayActOn(moderator, { id: 'x', role: 'member', level: 'moderator' }));
check('a moderator does not act on a manager', !R.mayActOn(moderator, { id: 'x', role: 'member', level: 'manager' }));
check('a manager acts on a moderator', R.mayActOn(manager, { id: 'x', role: 'member', level: 'moderator' }));
check('a manager acts on another manager', R.mayActOn(manager, { id: 'x', role: 'admin', level: 'manager' }));
check('nobody acts on an owner of the instance', !R.mayActOn(manager, { id: 'x', role: 'owner', level: 'member' }));
check('nobody acts on themselves', !R.mayActOn(manager, { id: me, role: 'member', level: 'manager' }));
check('a member acts on nobody', !R.mayActOn(member, { id: 'x', role: 'member', level: 'reader' }));

// The tag beside a name.
check('a guest is shown as a guest', R.levelTag('member', 'guest').text === 'guest');
check('a guest who reads says so', R.levelTag('reader', 'guest').text === 'guest, reads');
check('a moderator is tagged moderator, apart from a manager', R.levelTag('moderator', 'member').text === 'moderator' && R.levelTag('moderator', 'member').cls !== R.levelTag('manager', 'member').cls);
check('an owner who is a member here is tagged member', R.levelTag('member', 'owner').text === 'member');
for (const [v] of R.LEVELS) check(`what is said when someone becomes ${v}`, typeof R.LEVEL_SAID[v] === 'function' && R.LEVEL_SAID[v]('Sam').startsWith('Sam '));

// The owner override.
check('the notice names who was added and the owner', R.ownerOverrideText('Lee', 'Kaity') === 'Lee was added by override, by Kaity');
const warn = R.ownerOverrideWarning('#ops', 'Lee').join(' ');
check('a dismissal names who and by whom', R.managerDismissedText('Lee', 'Kaity') === 'Lee is no longer a manager, by Kaity');
check('the dismissal warning says they stay a member and everyone sees it', R.dismissWarning('#ops', 'Lee')[0].includes('Lee stays in #ops as a member. Every member will see that you dismissed them.'));
check('the warning says every member sees it', warn.includes('Every member of #ops will see that Lee was added by override, by you.'));
check('the warning says what the person and the owner see', warn.includes('Lee sees nothing posted before. You still cannot read the channel.'));
check('making an owner names the way back', R.makeOwnerWarning('Sam').join(' ').includes('This cannot be undone except by Sam or by the operator on the machine (churr owner revoke).'));

// The approvals queue.
check('nothing to approve: no entry', !R.mayApproveAny({ may_approve: false, kinds: [], counts: {} }) && !R.mayApproveAny(null));
check('one kind: the entry', R.mayApproveAny({ may_approve: true, kinds: ['registration'], counts: {} }));
check('the count adds the kinds the reader decides', R.approvalCount({ kinds: ['registration', 'channel'], counts: { registration: 2, channel: 3, webhook: 4 } }) === 5);
check('no counts is nought', R.approvalCount({ kinds: ['channel'] }) === 0 && R.approvalCount(null) === 0);
check('every kind is named', ['registration', 'channel', 'webhook'].every((k) => R.APPROVAL_KINDS[k] && R.APPROVAL_KIND_ONE[k]));
check('a channel approved', R.decidedText({ kind: 'channel', summary: 'design', state: 'approved' }) === 'Your channel #design was approved.');
check('a channel declined with the reason', R.decidedText({ kind: 'channel', summary: 'design', state: 'declined', reason: 'We have #design-team' }) === 'Your channel #design was declined: We have #design-team');
check('a channel declined without one', R.decidedText({ kind: 'channel', summary: 'x', state: 'declined' }) === 'Your channel #x was declined.');
check('the waiting page says it was sent', R.APPLICATION_SENT === 'Your request to join has been sent. You can sign in once it is approved.');

// /mod and /unmod: in the table, beside /op and /deop, protected.
for (const n of ['mod', 'unmod']) {
  check(`/${n} takes a person`, COMMANDS[n] && COMMANDS[n][0] === 'person' && COMMANDS[n][1] === false && !COMMANDS[n][2], JSON.stringify(COMMANDS[n]));
  check(`/${n} is listed with the members' commands`, GROUP_OF[n] === 'members');
  check(`/${n} cannot be replaced by a community command`, isProtected(n));
}

console.log(`${failures ? 'FAIL' : 'ok  '} roles: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
