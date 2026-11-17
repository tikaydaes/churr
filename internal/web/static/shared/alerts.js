// Access alerts and kept notices (docs/PERMISSIONS.md section 9.2,
// docs/FEATURES.md section 21.13), worded once for the web chat and the
// admin console.
//
// An access alert is a system notice posted in every conversation of a
// person's that is not public when an Owner or Admin did something that
// lets them, or whoever they handed something to, read there as that
// person. Like an owner override it wants attention and nobody can hide it.
// A notice is kept for one account and shown in its Notices list.

// ATTENTION are the system notice kinds drawn as wanting attention.
export const ATTENTION = new Set(['owner_override', 'manager_dismissed', 'bot_token_made', 'account_recovered', 'account_claimed', 'group_access', 'channel_shared']);

// alertText words an access alert. who is the author's name; by the name
// of whoever did it, '' when unknown (and, for group_access, when it is
// the author themself);
// name the group or instance named.
export function alertText(kind, who, by, name) {
  switch (kind) {
    case 'bot_token_made': return by ? `A new token was made for ${who} by ${by}. Whoever holds it reads here as ${who}.` : `A new token was made for ${who}. Whoever holds it reads here as ${who}.`;
    case 'account_recovered': return by ? `${who}’s account was recovered with a link made by ${by}. Whoever used it reads here as ${who}.` : `${who}’s account was recovered with a link made on the server. Whoever used it reads here as ${who}.`;
    case 'account_claimed': return by ? `${who}’s imported account was claimed with a link made by ${by}.` : `${who}’s imported account was claimed.`;
    case 'group_access': {
      const via = name ? ` through the group ${name}` : ' through a group';
      return by ? `${who} was added${via}, by ${by}` : `${who} added themself${via}`;
    }
    case 'channel_shared': return name ? `${who} shared this channel with ${name}. Its people there read what is said here from now on.` : `${who} shared this channel with another instance.`;
    default: return null;
  }
}

// noticeText words a kept notice. nameOf turns an account id into a name;
// convName a conversation id into one ('' when not known).
export function noticeText(n, nameOf, convName = () => '') {
  const by = n.by_id ? nameOf(n.by_id) : '';
  const about = n.subject_id ? nameOf(n.subject_id) : '';
  switch (n.kind) {
    case 'recovery_link_used': return by
      ? `Someone signed in to your account with a recovery link ${by} made. If that was not you, tell an owner or an admin.`
      : 'Someone signed in to your account with a recovery link made on the server. If that was not you, tell an owner or an admin.';
    case 'owner_made': return `${about} was made an owner${by && by !== about ? ' by ' + by : ''}.`;
    case 'owner_stepped_down': return `${about} stepped down as an owner.`;
    case 'export_started': return `${by || 'An owner'} started the full export.`;
    case 'export_downloaded': return `${by || 'An owner'} downloaded the full export.`;
    case 'override_used': {
      const where = n.conversation_id ? convName(n.conversation_id) : '';
      return `${by || 'Someone'} added ${about || 'someone'} to ${where ? '#' + where : 'a private channel'} by override.`;
    }
    default: return 'Something was done that concerns your account.';
  }
}
