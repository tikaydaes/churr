// Checks, without a browser, which groups the web chat lets a person join
// or leave by themselves, and what it asks the server to do
// (docs/CLIENTS.md 9.15).

const { selfServe, listed, toggle } = await import(new URL('../internal/web/static/chat/groups.js', import.meta.url).href);

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL groups: ${name}${detail ? '\n       ' + detail : ''}`); }
}

const open = { id: 'g1', handle: 'design', open: true, is_member: false, member_count: 2 };
check('a member may join an open group', selfServe(open, 'member'));
check('so may an admin and the owner', selfServe(open, 'admin') && selfServe(open, 'owner'));
check('a guest may not', !selfServe(open, 'guest'));
check('a bot may not', !selfServe(open, 'bot'));
check('nobody joins a closed group by hand', !selfServe({ ...open, open: false }, 'owner'));
check('nor a built-in one', !selfServe({ ...open, builtin: 'everyone' }, 'member'));
check('nor one filled from a provider', !selfServe({ ...open, provider_id: 'p1' }, 'admin'));
check('nor nothing', !selfServe(undefined, 'member'));

const shown = listed([{ handle: 'zeta' }, { handle: 'everyone', builtin: 'everyone' }, { handle: 'alpha' }, { handle: 'admins', builtin: 'admins' }]);
check('the built-in groups are not listed, the rest by handle', JSON.stringify(shown.map((g) => g.handle)) === '["alpha","zeta"]', JSON.stringify(shown));

const asked = [];
const api = {
  post: async (p) => { asked.push('POST ' + p); return { group: { ...open, is_member: true, member_count: 3 } }; },
  del: async (p) => { asked.push('DELETE ' + p); return {}; },
};
const joined = await toggle(open, api);
check('joining posts to members/me', asked[0] === 'POST /groups/g1/members/me', asked[0]);
check('and takes the group the server answers with', joined.is_member === true && joined.member_count === 3);
const left = await toggle(joined, api);
check('leaving deletes members/me', asked[1] === 'DELETE /groups/g1/members/me', asked[1]);
check('an answer without the group is filled in', left.is_member === false && left.member_count === 2, JSON.stringify(left));

console.log(`${failures ? 'FAIL' : 'ok  '} groups: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
