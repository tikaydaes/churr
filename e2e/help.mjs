// Checks, without a browser, what the web chat's /help says (rulings R67
// and R74) against the one source, testdata/commands/help.json: its copy
// is the file as it is, every command of the table has a full entry, a
// name after /help finds the right command and never one hidden from the
// reader, and "Things you can type" covers every object the composer
// knows, the units, the decks and the reference patterns that apply.
//
//   node e2e/help.mjs

import { readFileSync } from 'node:fs';
import { HELP, LABELS, entry, findListed, things, more, unitTable, patternExample, patternLists, thingMatches } from '../internal/web/static/shared/help.js';
import { COMMANDS, hiddenBuiltins } from '../internal/web/static/shared/commands.js';
import { DECKS, SPREADS } from '../internal/web/static/shared/message/cardnames.js';
import { OBJECT_KINDS } from '../internal/web/static/shared/md/objects.js';
import { MEASURE_UNITS } from '../internal/web/static/shared/objects/units.js';

const read = (p) => JSON.parse(readFileSync(new URL('../testdata/commands/' + p, import.meta.url), 'utf8'));
let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL help: ${name}${detail ? '\n       ' + detail : ''}`); }
}

check('the web chat holds help.json as it is', JSON.stringify(HELP) === JSON.stringify(read('help.json')));

// The server's list for someone, as GET /commands gives it: the table's
// built-ins less those hidden for them.
const table = read('commands.json');
const listedFor = (admin) => {
  const hidden = hiddenBuiltins([], admin);
  return table.commands.filter((c) => !hidden.has(c.name)).map((c) => ({ name: c.name, kind: 'builtin', description: c.description, hint: c.hint }));
};
const admin = listedFor(true);
const member = listedFor(false);

for (const c of admin) {
  const e = entry(c, admin);
  const named = `/${c.name}`;
  check(`${named}: a summary`, e.summary.length > 0);
  check(`${named}: a description`, e.description.length > 40, e.description);
  check(`${named}: a usage line`, e.usage.startsWith('/' + e.name), e.usage);
  check(`${named}: at least two examples`, e.examples.length >= 2 && e.examples.every((x) => x.type && x.then));
  check(`${named}: where it works says whether sealed chats have it`, e.where.endsWith(COMMANDS[e.name][1] ? LABELS.sealedYes : LABELS.sealedNo), e.where);
  check(`${named}: who may use it`, e.who.length > 0 && (!COMMANDS[e.name][2] || e.who.includes(LABELS.adminOnly)), e.who);
  if (c.hint) check(`${named}: its arguments are explained`, e.args.length > 0);
}

// Another name shows the command it stands for.
check('/part is /leave', entry({ name: 'part', kind: 'builtin', description: '' }, admin).name === 'leave');
check('/leave names /part', entry(admin.find((c) => c.name === 'leave'), admin).aliases.includes('/part'));
check('/ban names /kickban and /kb', ['/kickban', '/kb'].every((a) => entry(admin.find((c) => c.name === 'ban'), admin).aliases.includes(a)));

// /help <word>.
check('/help roll finds /roll', findListed(member, 'roll')?.name === 'roll');
check('/help /roll finds /roll', findListed(member, '/roll')?.name === 'roll');
check('/help kb finds /ban', findListed(member, 'kb')?.name === 'ban');
check('/help part finds /leave', findListed(member, 'part')?.name === 'leave');
check('/help trout is a search', findListed(member, 'trout') === null);
check('/help two words is a search', findListed(member, 'roll dice') === null);
for (const n of ['exile', 'kline', 'broadcast', 'amsg']) {
  check(`/help ${n} finds nothing for someone who is not an admin`, findListed(member, n) === null);
  check(`/help ${n} finds it for an admin`, findListed(admin, n) !== null);
}
// A command hidden from someone is never named to them as related.
for (const c of member) {
  const e = entry(c, member);
  check(`/${c.name}: names nothing hidden from a member`, e.related.every((r) => member.some((m) => m.name === r)), e.related.join(' '));
}
check('/ban names /exile to an admin', entry(admin.find((c) => c.name === 'ban'), admin).related.includes('exile'));
check('/ban does not name /exile to a member', !entry(member.find((c) => c.name === 'ban'), member).related.includes('exile'));
// A community command shows what its admin said of it.
const custom = { name: 'deploy', kind: 'saved_reply', description: 'Ships the build' };
check('a community command shows its description', entry(custom, [custom]).summary === 'Ships the build');
const fish = { name: 'slap', kind: 'saved_reply', description: '' };
check('one that replaces a built-in says so', entry(fish, [fish]).notes.some((n) => n.includes('/slap')));
const bot = { name: 'ship', kind: 'bot', description: '' };
check('a bot command with no description says so', entry(bot, [bot]).summary === LABELS.noDescription);

// The registries, not retyped.
const decks = more('decks');
check('/draw lists every deck', decks.rows.map((r) => r.name).join() === Object.keys(DECKS).join());
for (const r of decks.rows) {
  check(`deck ${r.name}: its count and aliases`, r.count.includes(String(DECKS[r.name].cards.length)) && r.aliases === DECKS[r.name].aliases && r.about.length > 0);
}
check('/tarot lists every spread', more('spreads').rows.map((r) => r.name).join() === Object.keys(SPREADS).join());
check('/mahjongg says how many tiles', more('mahjong').rows[0].count.includes('144'));

// Things you can type: every kind of object the composer knows.
const covered = new Set(HELP.things.flatMap((t) => t.covers));
// The composer's inline dice become an `object` entity of kind dice_roll.
const entityOf = (k) => (k === 'dice' ? 'object' : k);
for (const k of OBJECT_KINDS) check(`things cover ${k}`, covered.has('entity:' + entityOf(k)));
for (const k of ['account', 'group', 'broadcast', 'conversation', 'emoji', 'link', 'task', 'instant', 'event']) check(`things cover ${k}`, covered.has('entity:' + k));
const codes = new Set(MEASURE_UNITS.map((u) => u[1]));
const sorted = new Set(HELP.measure_kinds.flatMap((k) => k.units));
check('every unit is sorted into a kind', [...codes].every((c) => sorted.has(c)) && sorted.size === codes.size, [...codes].filter((c) => !sorted.has(c)).join(' '));
check('every way of typing a unit is shown', unitTable().flatMap((k) => k.units.flatMap((u) => u.typed)).length === MEASURE_UNITS.length);

// The reference patterns that apply here, with an example each.
for (const c of HELP.pattern_examples) check(`example for ${JSON.stringify(c.expression)}`, patternExample(c.expression) === c.want, patternExample(c.expression));
const pats = { server: [{ name: 'Tickets', prefix: 'PROJ-', expression: '', url_template: 'https://issues.example.com/browse/PROJ-{value}' }], here: [{ name: 'Builds', prefix: 'BUILD', expression: '[A-Z]{2}\\d{3}', url_template: 'https://ci.example.com/{value}' }] };
const live = patternLists({ patterns: pats });
check('the patterns that apply are listed', live.server[0].example === 'PROJ-123' && live.server[0].host === 'issues.example.com' && live.here[0].example === 'BUILDAB123' && !live.none);
check('a sealed chat lists the server\'s alone', patternLists({ patterns: pats, sealed: true }).here.length === 0 && patternLists({ patterns: pats, sealed: true }).sealed);
check('no patterns says who can add them', patternLists({}).none);
const refs = things({ patterns: pats }).find((t) => t.live === 'patterns');
check('the references row carries them', refs && refs.patterns.server.length === 1);
check('the measurements row carries the units', things().find((t) => t.live === 'units').units.length === HELP.measure_kinds.length);
check('a search finds a thing by what is typed', HELP.things.filter((t) => thingMatches(t, '[[')).map((t) => t.id).join() === 'dice');

console.log(failures ? `${failures} of ${checks} help checks failed` : `help: all ${checks} checks passed`);
if (failures) process.exitCode = 1;
