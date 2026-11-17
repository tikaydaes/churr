// Who may do what in a channel, and how the levels and the approvals
// queue are worded (docs/PERMISSIONS.md sections 2.4, 6 and 9.1,
// docs/FEATURES.md sections 21.12 and 31; rulings R97 and R99). The web
// chat and the admin console read these; the server decides every action
// anyway, so this only chooses what is offered. Nothing here touches the
// page, so it runs under Node (e2e/roles.mjs).
//
// The levels are reader < member < moderator < manager. An account's role
// on the instance (owner, admin) gives nothing in a channel. A guest
// account is a guest in every channel it is in, at reader or member; an
// account of another server is never a moderator or a manager here.

export const LEVELS = [['reader', 'Reader'], ['member', 'Member'], ['moderator', 'Moderator'], ['manager', 'Manager']];

// rank orders the levels; an unknown one is below reader.
export const rank = (level) => LEVELS.findIndex(([v]) => v === level);

// keepsPeace says whether a level keeps the peace: a moderator, or a
// manager, who may do all a moderator may.
export const keepsPeace = (level) => level === 'moderator' || level === 'manager';

// mayHoldKeeper says whether an account may be made a moderator or a
// manager: not a guest, and not someone of another server.
export const mayHoldKeeper = (a) => !!a && a.role !== 'guest' && a.role !== 'remote' && !a.server_name;

// levelChoices lists the levels a manager may give a member. A guest or a
// remote account reads or takes part as a member. In a channel a hub owns
// the moderators and managers are those on the hub's lists, so they are
// not set here, and one who is on them has nothing to choose.
export function levelChoices(target, { hubOwned = false, level } = {}) {
  if (hubOwned) return keepsPeace(level) ? [] : LEVELS.slice(0, 2);
  return mayHoldKeeper(target) ? LEVELS : LEVELS.slice(0, 2);
}

// mayActOn says whether the reader may remove, ban or time out a member
// (the target rule): they keep the peace there; never on themselves or on
// an owner of the instance; and only a manager acts on a moderator or a
// manager. target is { id, role, level }.
export function mayActOn({ meId, manager, keeper }, target) {
  if (!keeper || !target || target.id === meId || target.role === 'owner') return false;
  return !keepsPeace(target.level) || !!manager;
}

// levelTag is the tag beside a member's name: their level, or "guest"
// for a guest account taking part, with the colour it is drawn in.
export function levelTag(level, role) {
  if (role === 'guest') return { text: level === 'reader' ? 'guest, reads' : 'guest', cls: 'yellow' };
  if (level === 'manager') return { text: 'manager', cls: 'lavender' };
  if (level === 'moderator') return { text: 'moderator', cls: 'cyan' };
  return { text: level || 'member', cls: 'line' };
}

// What is said when a manager changes someone's level, by /op, /deop,
// /mod and /unmod or the members list.
export const LEVEL_SAID = {
  reader: (p) => `${p} reads here now and does not post.`,
  member: (p) => `${p} is a member of this channel.`,
  moderator: (p) => `${p} is a moderator of this channel.`,
  manager: (p) => `${p} is a manager of this channel.`,
};

// --- override and dismissal, from the console (rulings R99, R104) ---

// ownerOverrideText is the notice every member of a channel is shown when
// an owner or an admin added someone by override.
export const ownerOverrideText = (who, by) => `${who} was added by override, by ${by}`;

// managerDismissedText is the notice every member is shown when an owner
// or an admin took a manager's level away.
export const managerDismissedText = (who, by) => `${who} is no longer a manager, by ${by}`;

// dismissWarning is what an owner or an admin reads before dismissing a
// manager.
export const dismissWarning = (channel, person) => [
  `${person} stays in ${channel} as a member. Every member will see that you dismissed them. Nobody can hide that notice.`,
  'It is written to the audit log.',
];

// ownerOverrideWarning is what an owner or an admin reads before adding
// someone by override.
export const ownerOverrideWarning = (channel, person) => [
  `Every member of ${channel} will see that ${person} was added by override, by you. Nobody can hide that notice.`,
  `${person} sees nothing posted before. You still cannot read the channel.`,
];

// makeOwnerWarning is what an owner reads before making someone an owner.
export const makeOwnerWarning = (name) => [
  `${name} becomes an owner of this instance. This cannot be undone except by ${name} or by the operator on the machine (churr owner revoke).`,
  'An owner may do all you may: change roles, make other owners, run the export, link other instances, add people to private channels by override and appoint or dismiss managers.',
];

// --- the approvals queue ---

export const APPROVAL_KINDS = { registration: 'Registrations', channel: 'Channels', webhook: 'Webhooks' };
export const APPROVAL_KIND_ONE = { registration: 'Wants to join', channel: 'Asks for a channel', webhook: 'Asks for a webhook' };

// approvalCount is how many requests wait for the reader, of the kinds
// they may decide.
export function approvalCount(summary) {
  if (!summary || !Array.isArray(summary.kinds)) return 0;
  return summary.kinds.reduce((n, k) => n + (Number((summary.counts || {})[k]) || 0), 0);
}

// mayApproveAny says whether the reader is shown the approvals queue.
export const mayApproveAny = (summary) => !!(summary && Array.isArray(summary.kinds) && summary.kinds.length);

// decidedText is what the requester of a channel is told when it was
// decided: approved, or declined with the reason.
export function decidedText(a) {
  const what = a.kind === 'channel' ? `Your channel ${a.summary ? '#' + a.summary : ''}`.trim() : a.kind === 'webhook' ? `Your webhook ${a.summary || ''}`.trim() : 'Your request';
  if (a.state === 'approved') return `${what} was approved.`;
  if (a.state === 'lapsed') return `${what} waited too long and has lapsed.`;
  return `${what} was declined${a.reason ? ': ' + a.reason : '.'}`;
}

// The waiting page and the sign-in page say this to someone whose
// registration waits for approval.
export const APPLICATION_SENT = 'Your request to join has been sent. You can sign in once it is approved.';
export const APPLICATION_WAITS = 'Your request to join waits for approval. You can sign in once it is approved.';
