// Checks, without a browser, how the web chat arranges a person's own
// sidebar (ruling R83, internal/web/static/chat/arrange.js): heading order,
// own sections, the three sorts, stars at the top of their heading, and
// the changes a move or a reorder sends.

const { arrange, headings, reorderOps, moveOps, emptyLayout, keyOf, isSection } = await import('../internal/web/static/chat/arrange.js');

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL arrange: ${name}${detail ? '\n       ' + detail : ''}`); }
}

const sections = [{ id: 'A', name: 'Teams', position: 0 }, { id: 'B', name: 'Social', position: 1 }];
const ch = (id, name, extra = {}) => ({ id, name, kind: 'channel', created_at: '2026-01-01', last_message_at: extra.at || '2026-01-01', section_id: extra.section || '', parent_id: extra.parent || null, membership: { starred: !!extra.star } });
const chat = (id, at) => ({ id, kind: 'channel', name: null, created_at: '2026-01-01', last_message_at: at, membership: {} });
const convs = [
  ch('t1', 'zeta', { section: 'A', at: '2026-03-01' }), ch('t2', 'alpha', { section: 'A', at: '2026-02-01' }), ch('t3', 'Mid', { section: 'A', at: '2026-04-01' }),
  ch('k1', 'kid', { parent: 't1' }), ch('n1', 'loose'), chat('d1', '2026-05-01'), chat('d2', '2026-06-01'),
];
const byId = new Map(convs.map((c) => [c.id, c]));
const look = { kind: (c) => (c.name ? 'channel' : 'chat'), parent: (c) => byId.get(c.parent_id) || null, sections };
const title = (c) => c.name || c.id;
const names = (g) => g.items.map((x) => title(x.c)).join(',');
const find = (gs, key) => gs.find((g) => g.key === key);

let l = emptyLayout();
let gs = arrange(convs, l, sections, look, title);
check('default heading order', gs.map((g) => g.key).join(' ') === 's:A s:B s: chats sealed', gs.map((g) => g.key).join(' '));
check('alphabetical by default, ignoring case', names(find(gs, 's:A')) === 'alpha,Mid,zeta', names(find(gs, 's:A')));
check('a sub-channel sits under its parent and takes its section', find(gs, 's:A').items.find((x) => x.c.id === 't1').kids[0].id === 'k1');
check('chats by last activity by default', names(find(gs, 'chats')) === 'd2,d1');

l = { ...emptyLayout(), sections: [{ id: 1, name: 'Focus' }], placements: [{ conversation_id: 't1', key: 'p:1' }],
  groups: [{ key: 's:A', sort: 'activity', order: [] }] };
gs = arrange(convs, l, sections, look, title);
check('own sections first by default', gs[0].key === 'p:1' && gs[0].name === 'Focus');
check('a placed channel moves with its sub-channel', names(find(gs, 'p:1')) === 'zeta' && find(gs, 'p:1').items[0].kids[0].id === 'k1');
check('last activity', names(find(gs, 's:A')) === 'Mid,alpha', names(find(gs, 's:A')));
check('keyOf a placed parent\'s sub-channel', keyOf(byId.get('k1'), l, look) === 'p:1');

l = { ...l, headings: ['s:B', 's:', 'p:1'], groups: [{ key: 's:A', sort: 'custom', order: ['t2'] }] };
check('the person\'s heading order, and the missing after it', headings(l, sections).map((h) => h.key).join(' ') === 's:B s: p:1 s:A chats sealed');
gs = arrange(convs, l, sections, look, title);
check('custom order, then the rest by name', names(find(gs, 's:A')) === 'alpha,Mid', names(find(gs, 's:A')));

const starred = convs.map((c) => (c.id === 't3' || c.id === 't2' ? { ...c, membership: { starred: true } } : c));
l = { ...emptyLayout(), stars: ['t3', 't2'] };
gs = arrange(starred, l, sections, look, title);
check('stars at the top of their heading, in the order of stars', names(find(gs, 's:A')) === 'Mid,alpha,zeta', names(find(gs, 's:A')));

// Any conversation in any section (ruling R84): an admin's, Channels.
l = { ...emptyLayout(), placements: [{ conversation_id: 't2', key: 's:B' }, { conversation_id: 'd1', key: 's:' }, { conversation_id: 'n1', key: 's:Z' }] };
gs = arrange(convs, l, sections, look, title);
check('a channel placed in another admin section', names(find(gs, 's:B')) === 'alpha' && !names(find(gs, 's:A')).includes('alpha'), names(find(gs, 's:B')));
check('a chat placed in Channels', names(find(gs, 's:')).split(',').includes('d1') && !names(find(gs, 'chats')).includes('d1'), names(find(gs, 's:')));
check('a placement in a section that is gone falls back home', keyOf(byId.get('n1'), l, look) === 's:');
check('sections take rows, Chats and Sealed do not', isSection('p:1') && isSection('s:A') && isSection('s:') && !isSection('chats') && !isSection('sealed'));

// What dragging sends.
l = emptyLayout();
gs = arrange(convs, l, sections, look, title);
let ops = reorderOps(find(gs, 's:A'), 't1', 0, l);
check('dragging to the top makes the order custom', ops[0].op === 'reorder' && ops[0].ids.join() === 't1,t2,t3', JSON.stringify(ops));
gs = arrange(starred, { ...emptyLayout(), stars: ['t3', 't2'] }, sections, look, title);
ops = reorderOps(find(gs, 's:A'), 't2', 0, { ...emptyLayout(), stars: ['t3', 't2'] });
check('a starred row moves among the stars', ops[0].op === 'order_stars' && ops[0].ids.join() === 't2,t3', JSON.stringify(ops));
l = { ...emptyLayout(), sections: [{ id: 1, name: 'Focus' }], groups: [{ key: 'p:1', sort: 'custom', order: ['gone'] }], placements: [{ conversation_id: 't2', key: 'p:1' }] };
gs = arrange(convs, l, sections, look, title);
ops = moveOps(byId.get('t3'), find(gs, 'p:1'), 0, l);
check('into a custom section at a place, keeping what has no row', ops[0].key === 'p:1' && ops[0].ids.join() === 't3,t2,gone', JSON.stringify(ops));
ops = moveOps(byId.get('t3'), { key: 's:A', items: [] }, null, emptyLayout());
check('back home in an alphabetical heading sends no order', ops[0].key === 's:A' && !ops[0].ids);

console.log(`${failures ? 'FAIL' : 'ok  '} arrange: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
