// Checks, without a browser, how the web chat words the access alerts and
// the notices kept for an account (docs/FEATURES.md section 21.13,
// internal/web/static/shared/alerts.js).
//
//   node e2e/alerts.mjs

const A = await import(new URL('../internal/web/static/shared/alerts.js', import.meta.url).href);

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL alerts: ${name}${detail ? '\n       ' + detail : ''}`); }
}

// Every access alert wants attention, as an owner override does.
for (const k of ['owner_override', 'bot_token_made', 'account_recovered', 'account_claimed', 'group_access', 'channel_shared']) check(`${k} wants attention`, A.ATTENTION.has(k));
check('an ordinary arrival does not', !A.ATTENTION.has('member_joined'));

const t = (k, who, by, name) => A.alertText(k, who, by, name);
check('a bot token names who made it', t('bot_token_made', 'Helper', 'Ada', '') === 'A new token was made for Helper by Ada. Whoever holds it reads here as Helper.', t('bot_token_made', 'Helper', 'Ada', ''));
check('a recovery names who made the link', t('account_recovered', 'Lee', 'Ada', '').startsWith('Lee’s account was recovered with a link made by Ada.'));
check('a recovery from the command line says so', t('account_recovered', 'Lee', '', '').includes('made on the server'));
check('a claim names who made the link', t('account_claimed', 'Lee', 'Ada', '') === 'Lee’s imported account was claimed with a link made by Ada.');
check('an admin who put themself in a group', t('group_access', 'Ada', '', 'Crew') === 'Ada added themself through the group Crew');
check('someone put in a group by an admin', t('group_access', 'Lee', 'Ada', 'Crew') === 'Lee was added through the group Crew, by Ada');
check('a share names the instance', t('channel_shared', 'Ada', 'Ada', 'b.example').startsWith('Ada shared this channel with b.example.'));
check('an unknown kind is left to the caller', t('member_joined', 'Ada', '', '') === null);

const names = { ada: 'Ada', lee: 'Lee' };
const nameOf = (id) => names[id] || '…';
const n = (kind, more = {}) => A.noticeText({ id: 'n', kind, created_at: '2026-11-17T00:00:00Z', ...more }, nameOf, (id) => (id === 'c1' ? 'ops' : ''));
check('a recovery link used on my account', n('recovery_link_used', { by_id: 'ada' }).startsWith('Someone signed in to your account with a recovery link Ada made.'));
check('an owner made', n('owner_made', { by_id: 'ada', subject_id: 'lee' }) === 'Lee was made an owner by Ada.');
check('an owner stepped down', n('owner_stepped_down', { subject_id: 'lee' }) === 'Lee stepped down as an owner.');
check('the export started', n('export_started', { by_id: 'ada' }) === 'Ada started the full export.');
check('the export downloaded', n('export_downloaded', { by_id: 'ada' }) === 'Ada downloaded the full export.');
check('an override used', n('override_used', { by_id: 'ada', subject_id: 'lee', conversation_id: 'c1' }) === 'Ada added Lee to #ops by override.');
check('an unknown kind still says something', n('something_new').length > 0);

console.log(`${failures ? 'FAIL' : 'ok  '} alerts: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
