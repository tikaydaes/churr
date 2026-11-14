// What the screens for channels on a hub (rulings R55, R56, R59;
// docs/LINK-PROTOCOL.md sections 21a and 23a) decide, kept apart from
// drawing so it can be checked under Node (e2e/hub.mjs): who is a
// moderator, which delete a person is offered, what a directory entry
// lets this server do, what a move's state means and who may act on it,
// and the clock figures of a link.
//
// Pure: no DOM, no requests.

export const LISTINGS = ['unlisted', 'request', 'open'];

// LISTING says what each listing mode means, in the words the screens use.
export const LISTING = {
  unlisted: { name: 'Unlisted', says: 'Not in the hub’s directory. Servers join only when a moderator invites them.' },
  request: { name: 'On request', says: 'Every server on the hub sees it in the directory and may ask to join; a moderator approves or declines.' },
  open: { name: 'Open', says: 'Every server on the hub sees it in the directory and may join without asking.' },
};

export const listingName = (mode) => (LISTING[mode] || { name: mode || 'unknown' }).name;

// ownerStatement is what is said before a channel is shared to a hub:
// that the hub will own it, and what its operator can and cannot see.
export function ownerStatement(hub) {
  const name = hub || 'the hub';
  return [
    `${name} will own this channel. From then on only the hub signs changes to its name, description, listing mode, servers and moderators, and every server takes those changes from the hub.`,
    'You become its first moderator. Moderators are accounts, on any server in the channel: they invite and admit servers, remove them, rename it, set its listing, add and remove moderators, close it, and delete any message for everyone.',
    `${name}’s operator can see the channel’s name, description, moderators and member servers, and can admit and remove servers and appoint moderators. They cannot read messages, files or anything else inside it: the hub never holds the key.`,
    'Each server’s own levels, pins, slow mode and retention stay its own.',
  ];
}

// A HubModerator is { account_id, server, username }. An account is a
// moderator when the list names it as an account of its home server: a
// local account by its id (or username) under this server's name, an
// account from another server by its username under that server's name.
export function isModerator(channel, account, ownServer) {
  if (!channel || !account) return false;
  return (channel.moderators || []).some((m) => {
    if (account.server_name) return m.server === account.server_name && m.username === account.username;
    return m.server === ownServer && (m.account_id === account.id || (!!account.username && m.username === account.username));
  });
}

export const moderatorName = (m) => `@${m.username}@${m.server}`;

// deleteChoice says which delete the message menu offers, or null:
//   mine        the reader wrote it
//   manager     the reader acts as a manager of the local channel
//   hub         the hub's record of the channel, or null when it is not on a hub
//   moderator   the reader is on that record's moderator list
// A moderator deletes any message for everyone; a local manager who is
// not one deletes another's message on this server only. An author's own
// delete reaches everyone as it always has.
export function deleteChoice({ mine, manager, hub, moderator }) {
  if (!hub) return mine || manager ? { label: 'Delete…', everyone: true } : null;
  if (moderator) return { label: 'Delete for everyone…', everyone: true };
  if (mine) return { label: 'Delete…', everyone: true };
  if (manager) return { label: 'Delete here…', everyone: false };
  return null;
}

// deleteWords is what the delete dialog says for a choice.
export function deleteWords(choice, hub) {
  if (!choice) return '';
  if (!hub) return 'A marker stays saying who deleted it and when. Its content is kept as history until it is purged.';
  if (choice.label.startsWith('Delete for everyone')) return `As a moderator you delete it on every server in the channel. Each server checks you are on the moderator list ${hub.hub} signed.`;
  if (!choice.everyone) return 'You manage this channel on this server only, so it is deleted here and stays on the other servers. Only the channel’s moderators delete for everyone.';
  return 'Your own message is deleted on every server in the channel.';
}

// joinAction says what this server may do with a directory entry:
// 'join' (open), 'ask' (on request), 'accept' (invited), or null with a
// reason.
export function joinAction(entry) {
  switch (entry.state) {
    case 'member': return { action: null, says: 'This server is in it.' };
    case 'requested': return { action: null, says: 'Asked. Waiting for a moderator.' };
    case 'invited': return { action: 'accept', label: 'Join (invited)', says: 'A moderator invited this server.' };
    default:
      if (entry.listing === 'open') return { action: 'join', label: 'Join', says: 'Open: this server joins at once.' };
      return { action: 'ask', label: 'Ask to join', says: 'On request: a moderator approves or declines.' };
  }
}

// requests sorts this server's streams through one hub into those it has
// asked to join and waits on, and those that were declined or removed.
export function requests(streams, peerId) {
  const here = (streams || []).filter((s) => s.peer_id === peerId && s.kind === 'channel');
  return {
    pending: here.filter((s) => s.state === 'offered' && s.conversation_id),
    declined: here.filter((s) => s.state === 'removed'),
  };
}

// MOVE_STATE says what each state of a move means.
export const MOVE_STATE = {
  proposed: { tag: 'yellow', says: 'Proposed. Waiting for the other server’s owner.' },
  accepted: { tag: 'yellow', says: 'Accepted. The move is running; messages posted meanwhile wait and are sent through the hub.' },
  committed: { tag: 'cyan', says: 'Handed over to the hub. Finishing.' },
  done: { tag: 'cyan', says: 'Done. The hub owns the channel.' },
  declined: { tag: 'grey', says: 'Declined. Nothing changed.' },
  abandoned: { tag: 'pink', says: 'Abandoned. Nothing changed; the channel carries on directly.' },
};

// moveActions says what this server's owner may do with a move.
export function moveActions(move) {
  const open = move.state === 'proposed';
  return {
    accept: open && move.direction === 'incoming' && !!move.hub_peer_id,
    decline: open && move.direction === 'incoming',
    cancel: move.direction === 'outgoing' && (move.state === 'proposed' || move.state === 'accepted'),
    // Incoming, but this server is not on the hub: it cannot accept.
    notOnHub: open && move.direction === 'incoming' && !move.hub_peer_id,
  };
}

// movable says whether a stream can be proposed for a move: a channel this
// server owns, shared over a direct link, that is active, with no move
// in progress.
export function movable(stream, peers, moves) {
  const peer = (peers || []).find((p) => p.id === stream.peer_id);
  if (!peer || peer.kind !== 'server' || stream.kind !== 'channel' || stream.state !== 'active' || stream.owner_server) return false;
  return !(moves || []).some((m) => m.stream_id === stream.id && ['proposed', 'accepted', 'committed'].includes(m.state));
}

// ms writes a duration in milliseconds the way the clock figures show it.
export function ms(n, signed) {
  if (n == null || !Number.isFinite(n)) return '–';
  const s = Math.abs(n) >= 10000 ? (n / 1000).toFixed(1) + ' s' : Math.round(n) + ' ms';
  return signed && n > 0 ? '+' + s : signed && n < 0 ? '−' + s.slice(1) : s;
}

// clockFigures lists what a PeerClock says, as [label, value] pairs.
export function clockFigures(c) {
  if (!c) return [];
  return [
    ['Offset', ms(c.offset_ms, true)],
    ['Latency', ms(c.latency_ms)],
    ['Jitter', ms(c.jitter_ms)],
    ['Out', `${ms(c.forward_ms)} ± ${ms(c.forward_jitter_ms)}`],
    ['Back', `${ms(c.back_ms)} ± ${ms(c.back_jitter_ms)}`],
    ['Uncertainty', '± ' + ms(c.uncertainty_ms)],
    c.asymmetry_ms ? ['Asymmetry', ms(c.asymmetry_ms, true)] : null,
    ['Samples', String(c.samples) + (c.synced ? ', clocks declared synchronised' : '')],
  ].filter(Boolean);
}

// clockWarning is a line worth showing when the figures are poor, or null:
// events more than 30 seconds off the reference clock are refused.
export function clockWarning(c) {
  if (!c) return null;
  if (Math.abs(c.offset_ms) + c.uncertainty_ms > 30000) return 'This clock is more than 30 seconds off the reference: events stamped by it are refused. Fix the server’s time.';
  if (Math.abs(c.offset_ms) > 5000) return 'This clock is more than 5 seconds off the reference. Events still order correctly, but check the server’s time.';
  return null;
}

// moderatorActions lists the tools a moderator has on a channel record.
export function moderatorActions(channel) {
  if (!channel || channel.closed) return [];
  return ['invite', 'approve', 'decline', 'remove_server', 'describe', 'set_listing', 'add_moderator', 'remove_moderator', 'close'];
}

// canRemoveModerator: a channel keeps at least one moderator.
export const canRemoveModerator = (channel) => (channel.moderators || []).length > 1;
