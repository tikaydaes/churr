// Joining and leaving an open group (docs/CLIENTS.md 9.15). Joining adds
// the person to every channel the group is in. Only an open group that is
// not built in and not filled from a provider can be joined or left by the
// person; guests and bots are put in groups by admins.

// selfServe says whether someone with `role` may join or leave group `g`
// by themselves.
export function selfServe(g, role) {
  return !!(g && g.open && !g.builtin && !g.provider_id && (role === 'owner' || role === 'admin' || role === 'member'));
}

// listed is the groups the browser shows: every group but the two
// built-in ones, by handle.
export function listed(groups) {
  return groups.filter((g) => !g.builtin).sort((a, b) => a.handle.localeCompare(b.handle));
}

// toggle joins `g` or leaves it, through `api` ({ post, del }), and returns
// the group as the server now has it.
export async function toggle(g, api) {
  const path = `/groups/${g.id}/members/me`;
  const out = g.is_member ? await api.del(path) : await api.post(path);
  return (out && out.group) || { ...g, is_member: !g.is_member, member_count: (g.member_count || 0) + (g.is_member ? -1 : 1) };
}
