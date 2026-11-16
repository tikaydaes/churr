// Who may add a custom emoji (docs/PERMISSIONS.md): admins and the owner
// always, members when the server's `members_may_add_emoji` is on, guests
// and bots never. Kept apart from the page so it can be checked without
// one (e2e/emoji.mjs).

export function mayAddEmoji(role, features) {
  if (role === 'owner' || role === 'admin') return true;
  return role === 'member' && !!(features && features.members_may_add_emoji);
}
