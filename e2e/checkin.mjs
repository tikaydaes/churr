// Checks, without a browser, what the web chat sends to set up a recurring
// check-in (shared/checkin.js, docs/FEATURES.md section 11), how it reads
// one back, and how it says when one runs.

const C = await import(new URL('../internal/web/static/shared/checkin.js', import.meta.url).href);

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL checkin: ${name}${detail ? '\n       ' + detail : ''}`); }
}

const v = C.blank('Europe/Dublin');
check('a new one is valid as it starts', C.problem(v) === null, C.problem(v));
check('weekdays at nine in the given zone', v.days === '1111100' && v.at === '09:00' && v.zone === 'Europe/Dublin');

v.title = ' Stand-up ';
v.text = 'Morning!';
v.questions = [{ label: 'Yesterday', long: false, required: true }, { label: '  ', long: false, required: false }, { label: 'Today', long: true, required: false }];
const body = C.createBody('c1', v);
check('the body is a recurring form in the channel', body.conversation_id === 'c1' && body.kind === 'form' && !('run_at' in body));
check('the schedule is daily on the days chosen', JSON.stringify(body.repeat) === '{"every":"day","days":"1111100","at":"09:00","time_zone":"Europe/Dublin"}', JSON.stringify(body.repeat));
const f = body.content.form;
check('the form is a checkin, answered in the thread, once each', f.preset === 'checkin' && f.action.type === 'thread' && f.one_answer === true && f.title === 'Stand-up');
check('it carries no closing time of its own', !('closes_at' in f));
check('empty questions are left out, the rest numbered', JSON.stringify(f.fields) === '[{"id":"q1","type":"text","label":"Yesterday","required":true},{"id":"q2","type":"longtext","label":"Today"}]', JSON.stringify(f.fields));
check('the message text goes with it', body.content.text === 'Morning!');
check('no text is sent when there is none', !('text' in C.content({ ...v, text: '  ' })));

const back = C.fromItem({ content: body.content, repeat: body.repeat });
check('a stored one reads back as it was set up', back.title === 'Stand-up' && back.days === '1111100' && back.questions.length === 2 && back.questions[1].long && back.questions[0].required, JSON.stringify(back));
check('a weekly one reads back as every day', C.fromItem({ content: body.content, repeat: { every: 'week', at: '10:00', time_zone: 'UTC' } }).days === '1111111');

check('no title', C.problem({ ...v, title: ' ' }) === 'Give it a title.');
check('no question', C.problem({ ...v, questions: [{ label: '' }] }) === 'Ask at least one question.');
check('too many questions', /At most 20/.test(C.problem({ ...v, questions: Array.from({ length: 21 }, (_, i) => ({ label: 'q' + i })) }) || ''));
check('no day', C.problem({ ...v, days: '0000000' }) === 'Choose at least one day.');
check('a bad time', C.problem({ ...v, at: '9am' }) === 'Give the time as HH:MM.');

check('every day', C.repeatWords({ every: 'day', days: '1111111', at: '09:30', time_zone: 'UTC' }) === 'Every day at 09:30 (UTC)');
check('weekdays', C.repeatWords({ every: 'day', days: '1111100', at: '09:30', time_zone: 'UTC' }).startsWith('Weekdays'));
check('chosen days', C.repeatWords({ every: 'day', days: '1001000', at: '08:00', time_zone: 'UTC' }) === 'Monday, Thursday at 08:00 (UTC)');
check('every week', C.repeatWords({ every: 'week', at: '08:00', time_zone: 'UTC' }).startsWith('Every week'));

console.log(`${failures ? 'FAIL' : 'ok  '} checkin: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
