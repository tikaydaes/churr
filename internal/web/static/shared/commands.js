// The built-in slash commands (docs/FEATURES.md sections 12.3 and 12.4,
// ruling R67): the table every client holds and the one grammar they read
// typed text by. testdata/commands/commands.json is the table and
// testdata/commands/parse.json the cases this file is held to
// (e2e/commands.mjs). Nothing here touches the page, so it runs under Node.
//
// The words the web chat says about commands are here too, in STRINGS: the
// web chat keeps its wording beside the code that uses it.

import { DECKS, SPREADS } from './message/cardnames.js';

// name: [args, offered in a sealed chat, offered to admins only]. The order
// is the menu's.
export const COMMANDS = {
  poll: ['form', false], rsvp: ['form', false], signup: ['form', false], approve: ['form', false], form: ['none', false],
  remind: ['raw', true],
  me: ['text', true], shrug: ['text_optional', true], tableflip: ['text_optional', true], unflip: ['text_optional', true], spoiler: ['text', true],
  roll: ['dice', false], flip: ['none', false], pick: ['options', false], shuffle: ['options', false], draw: ['draw', false],
  tarot: ['spread', false], mahjongg: ['count', false], mahjong: ['count', false],
  join: ['channel', true], leave: ['channel_optional', false], part: ['channel_optional', false], msg: ['person_text', true], topic: ['text_optional', false],
  invite: ['person_channel', false], kick: ['person', false], ban: ['person_text', false], unban: ['person', false],
  exile: ['person_text', false, true], unexile: ['person', false, true], mute: ['person_duration', false],
  op: ['person', false], deop: ['person', false], names: ['none', false], list: ['none', true],
  whois: ['person', true], ignore: ['person', true], unignore: ['person', true], nick: ['text', true],
  away: ['duration_optional', true], back: ['none', true], dnd: ['duration_off', true], quiet: ['duration_off', true],
  // Ruling R74.
  help: ['text_optional', true], say: ['text', true], slap: ['person', true], query: ['person', true],
  clear: ['none', false], close: ['none', true], knock: ['channel', true],
  banlist: ['none', false], voice: ['person', false], devoice: ['person', false],
  whoami: ['none', true], time: ['person', true], invisible: ['none', true],
  motd: ['none', true], admin: ['none', true], version: ['none', true], info: ['none', true], ping: ['none', true],
  lusers: ['none', false], broadcast: ['broadcast', true, true],
};

// The group each command is listed under in the /help dialog
// (commands.json, group).
export const GROUP_OF = {};
for (const [g, names] of Object.entries({
  forms: 'poll rsvp signup approve form', reminders: 'remind',
  post: 'me shrug tableflip unflip spoiler say slap',
  chance: 'roll flip pick shuffle draw tarot mahjongg mahjong',
  conversations: 'join leave part msg topic list query clear close knock',
  members: 'invite kick ban unban exile unexile mute op deop names banlist voice devoice',
  people: 'whois ignore unignore nick whoami time',
  status: 'away back dnd quiet invisible',
  info: 'help', server: 'motd admin version info ping lusers broadcast',
})) for (const n of names.split(' ')) GROUP_OF[n] = g;

// The order the groups are shown in by /help.
export const GROUPS = ['post', 'conversations', 'members', 'people', 'status', 'server', 'info', 'forms', 'reminders', 'chance'];

// The server-wide ban's two commands (ruling R72). Their names are the
// owner's to change; everything else here and in the chat reaches them by
// these two constants.
export const SERVER_BAN = 'exile';
export const SERVER_UNBAN = 'unexile';

// Other names a command answers to that the menu does not show
// (commands.json, aliases).
export const HIDDEN_ALIASES = { kline: SERVER_BAN, unkline: SERVER_UNBAN, who: 'names', kickban: 'ban', kb: 'ban', amsg: 'broadcast' };

// Another name for a command.
const ALIASES = { part: 'leave', mahjong: 'mahjongg', info: 'version', ...HIDDEN_ALIASES };

// aliasesOf lists the other names a command answers to, for /help.
export const aliasesOf = (n) => Object.keys(ALIASES).filter((a) => ALIASES[a] === n);

export const isBuiltin = (n) => Object.prototype.hasOwnProperty.call(COMMANDS, n) || Object.prototype.hasOwnProperty.call(HIDDEN_ALIASES, n);

// The built-ins a community's command may replace: the fun and games
// (commands.json, replaceable). Every other built-in is protected, a new one
// included: the server refuses its name and aliases to saved replies, custom
// commands and bot commands, and ignores any made before.
export const REPLACEABLE = new Set(['shrug', 'tableflip', 'unflip', 'roll', 'flip', 'pick', 'shuffle', 'draw', 'tarot', 'mahjongg', 'mahjong', 'slap']);

// isProtected says whether a name is a protected built-in or one of its
// aliases.
export const isProtected = (n) => isBuiltin(n) && !REPLACEABLE.has(HIDDEN_ALIASES[n] || n);

// hiddenBuiltins is the one rule of which built-ins are not commands for
// this reader here (ruling R72; parse.json, hidden_cases). listed is the
// server's command list for the conversation: a saved reply, custom command
// or bot command in it hides the built-in of its name, each name on its own,
// unless the built-in is protected. The admin-only built-ins and their aliases are hidden from anyone else.
// What is hidden is left out of the menu, not read by parse() and not run.
export function hiddenBuiltins(listed, admin) {
  const out = new Set();
  for (const c of listed || []) if (c.kind !== 'builtin' && isBuiltin(c.name) && !isProtected(c.name)) out.add(c.name);
  if (!admin) {
    for (const [n, v] of Object.entries(COMMANDS)) if (v[2]) out.add(n);
    for (const [n, to] of Object.entries(HIDDEN_ALIASES)) if (COMMANDS[to][2]) out.add(n);
  }
  return out;
}

// The results the server draws: never offered in a sealed chat, which the
// server cannot write into.
export const CHANCE = new Set(['roll', 'flip', 'pick', 'shuffle', 'draw', 'tarot', 'mahjongg', 'mahjong']);

export const SHRUG = '¯\\\\\\_(ツ)\\_/¯';
const ENDINGS = { shrug: SHRUG, tableflip: '(╯°□°)╯︵ ┻━┻', unflip: '┬─┬ノ( º _ ºノ)' };

const MAX_SECONDS = 366 * 86400;

// A broadcast shows for 10 minutes unless a time is given, and for at most
// a day (ruling R74).
export const BROADCAST_SECONDS = 600;
export const BROADCAST_MAX_SECONDS = 86400;

// literal is the literal slash (ruling R74): text that starts with
// whitespace and then / is never a command, and is sent as a message with
// that whitespace dropped; this gives that message. For any other text it
// gives null, and the text is read as usual. Text in a code span or block
// starts with a backtick, so it is never a command either.
// (parse.json, literal_cases.)
export function literal(text) {
  const m = /^\s+(\/[\s\S]*)$/.exec(text || '');
  return m ? m[1] : null;
}
const UNITS = { m: 60, h: 3600, d: 86400, w: 604800 };

// parseDuration reads `30m`, `2h`, `1h30m`, `1d`, `2w`, or a bare number of
// minutes, as seconds; null if it cannot.
export function parseDuration(s) {
  const t = String(s).trim().toLowerCase();
  let secs = 0;
  if (/^\d+$/.test(t)) secs = Number(t) * 60;
  else if (/^(\d+[mhdw])+$/.test(t)) for (const m of t.matchAll(/(\d+)([mhdw])/g)) secs += Number(m[1]) * UNITS[m[2]];
  else return null;
  return secs > 0 && secs <= MAX_SECONDS ? secs : null;
}

const PERSON = /^@?([a-z0-9._-]+(?:@[a-z0-9.-]+)?)$/i;
const LINK = /^<#([0-9a-f-]{36})>$/;

function channelRef(s) {
  const m = LINK.exec(s);
  if (m) return { id: m[1] };
  return { name: s.replace(/^#/, '').trim() };
}

// findDeck finds a deck by name or alias, without regard to case.
export function findDeck(name) {
  const n = String(name || 'standard').toLowerCase();
  for (const [key, d] of Object.entries(DECKS)) if (key === n || d.aliases.includes(n)) return key;
  return null;
}

// findSpread finds a tarot spread by name or alias, without regard to case.
export function findSpread(name) {
  const n = String(name || 'three').toLowerCase();
  for (const [key, sp] of Object.entries(SPREADS)) if (key === n || sp.aliases.includes(n)) return key;
  return null;
}

// parse reads text typed in a composer. It returns null when the text is not
// a built-in command, or { name, … } with what was read, or { name, error }.
// hidden is what hiddenBuiltins() gives for the reader: those names are not
// built-ins here.
export function parse(text, hidden) {
  const m = /^\/([a-z0-9_+-]+)(?:\s+([\s\S]*))?$/.exec(text);
  if (!m || !isBuiltin(m[1]) || (hidden && hidden.has(m[1]))) return null;
  const typed = m[1];
  const name = ALIASES[typed] || typed;
  const rest = (m[2] || '').trim();
  const out = { name };
  const fail = (error) => ({ name, error });
  const words = rest ? rest.split(/\s+/) : [];
  // The first word as a person, and what follows it.
  const person = () => {
    if (!words.length) return fail('needs_person');
    const p = PERSON.exec(words[0]);
    if (!p) return fail('bad_person');
    out.person = p[1].toLowerCase();
    return null;
  };
  const after = () => rest.slice(rest.indexOf(words[0]) + words[0].length).trim();
  const args = COMMANDS[name][0];
  switch (args) {
    case 'form': case 'raw':
      out.args = rest;
      return out;
    case 'none':
      return rest ? fail('takes_nothing') : out;
    case 'text':
      if (!rest) return fail('needs_text');
      if (name === 'me') return { name, post: rest, action: true };
      if (name === 'spoiler') return { name, post: '||' + rest + '||' };
      if (name === 'say') return { name, post: rest };
      out.text = rest;
      return out;
    case 'text_optional':
      if (ENDINGS[name]) return { name, post: (rest ? rest + ' ' : '') + ENDINGS[name] };
      if (rest) out.text = rest;
      return out;
    case 'dice':
      out.dice = rest;
      return out;
    case 'options': {
      const opts = rest ? rest.split('|').map((o) => o.trim()) : [];
      if (opts.length < 2) return fail('needs_options');
      if (opts.length > 20) return fail('too_many_options');
      if (opts.some((o) => !o)) return fail('empty_option');
      out.options = opts;
      return out;
    }
    case 'draw': {
      let count = 1, deck = 'standard';
      if (words.length > 2) return fail('bad_draw');
      if (words.length) {
        if (/^\d+$/.test(words[0])) {
          count = Number(words[0]);
          if (words[1]) deck = words[1];
        } else if (words.length === 1) deck = words[0];
        else return fail('bad_draw');
      }
      if (count < 1) return fail('bad_draw');
      const key = findDeck(deck);
      if (!key) return fail('unknown_deck');
      if (count > DECKS[key].cards.length) return fail('too_many_cards');
      return { name, count, deck: key };
    }
    case 'spread': {
      const key = words.length > 1 ? null : findSpread(rest);
      return key ? { name, spread: key } : fail('unknown_spread');
    }
    case 'count': {
      const n = rest ? (/^\d+$/.test(rest) ? Number(rest) : 0) : 1;
      if (n < 1 || n > DECKS.mahjong.cards.length) return fail('bad_count');
      return { name, count: n };
    }
    case 'channel':
      if (!rest) return fail('needs_channel');
      out.channel = channelRef(rest);
      return out;
    case 'channel_optional':
      if (rest) out.channel = channelRef(rest);
      return out;
    case 'person': {
      const bad = person() || (words.length > 1 ? fail('bad_person') : null);
      if (bad) return bad;
      // A fixed action, as /shrug's ending is fixed (ruling R74).
      if (name === 'slap') return { name, post: `slaps @${out.person} around a bit with a large trout`, action: true };
      return out;
    }
    case 'broadcast': {
      // `[duration] text`, or `clear` alone (ruling R74). Only a time with
      // its unit is read as one, so "5 minutes until…" is all text.
      if (!rest) return fail('needs_text');
      if (rest.toLowerCase() === 'clear') return { name, clear: true };
      let secs = BROADCAST_SECONDS, text = rest;
      if (/^(\d+[mhdw])+$/i.test(words[0])) {
        secs = parseDuration(words[0]);
        if (secs == null || secs > BROADCAST_MAX_SECONDS) return fail('bad_broadcast_time');
        text = after();
        if (!text) return fail('needs_text');
      }
      return { name, seconds: secs, text };
    }
    case 'person_text': {
      const bad = person();
      if (bad) return bad;
      const t = after();
      if (t) out.text = t;
      return out;
    }
    case 'person_channel': {
      const bad = person();
      if (bad) return bad;
      const t = after();
      if (t) out.channel = channelRef(t);
      return out;
    }
    case 'person_duration': {
      const bad = person();
      if (bad) return bad;
      const t = after();
      const secs = t ? parseDuration(t) : 3600;
      if (secs == null) return fail('bad_duration');
      out.seconds = secs;
      return out;
    }
    case 'duration_optional':
    case 'duration_off': {
      if (!rest) {
        if (name === 'quiet') out.seconds = 3600;
        return out;
      }
      if (args === 'duration_off' && rest.toLowerCase() === 'off') return { name, off: true };
      const secs = parseDuration(rest);
      if (secs == null) return fail('bad_duration');
      out.seconds = secs;
      return out;
    }
  }
  return null;
}

// What the web chat says about commands.
export const STRINGS = {
  errors: {
    needs_text: (n) => `/${n} needs some text after it.`,
    takes_nothing: (n) => `/${n} takes nothing after it.`,
    needs_options: (n) => `/${n} needs at least two options, separated by |.`,
    too_many_options: (n) => `/${n} takes at most 20 options.`,
    empty_option: (n) => `An option of /${n} is empty.`,
    bad_draw: () => 'Write /draw [count] [deck], for example /draw 3 tarot.',
    unknown_deck: () => `There is no such deck. The decks are: ${Object.keys(DECKS).join(', ')}.`,
    too_many_cards: () => 'That deck does not have that many cards.',
    unknown_spread: () => `There is no such spread. The spreads are: ${Object.keys(SPREADS).join(', ')}.`,
    bad_count: () => `Draw 1 to ${DECKS.mahjong.cards.length} tiles.`,
    needs_channel: (n) => `/${n} needs a channel: type # and pick one.`,
    needs_person: (n) => `/${n} needs a person: type @ and pick one.`,
    bad_person: (n) => `/${n} takes one person, written @name.`,
    bad_duration: () => 'Write a time like 30m, 2h, 1h30m, 1d or 1w (at most a year).',
    bad_broadcast_time: () => 'Show it for 1m to 24h, written like 15m or 2h.',
  },
  notInSealed: (n) => CHANCE.has(n)
    ? `/${n} is drawn by the server, which cannot write into a sealed chat.`
    : `/${n} is not available in a sealed chat.`,
  noSuchPerson: (p) => `Nobody here is called @${p}.`,
  noSuchChannel: (c) => `No channel you can see is called “${c}”.`,
  severalChannels: (c) => `Several channels are called “${c}”: type # and pick the one you mean.`,
  joined: (c) => `You joined ${c}.`,
  left: (c) => `You left ${c}.`,
  topicIs: (t) => t ? `The topic: ${t}` : 'This channel has no topic.',
  topicSet: 'Topic set.',
  away: 'You appear away.',
  back: 'You no longer appear away.',
  dndOn: 'Do not disturb is on.',
  dndOff: 'You are available now.',
  quietOn: (until) => `This conversation is muted until ${until}.`,
  quietOff: 'This conversation is no longer muted.',
  invited: (p) => `${p} was added.`,
  kicked: (p) => `${p} was removed from this channel.`,
  banned: (p) => `${p} is banned from this channel. They cannot come back until the ban is lifted.`,
  unbanned: (p) => `${p} may come back to this channel.`,
  exileQuestion: (p) => `Exile ${p} from the server?`,
  exileBody: (p) => `${p} will be removed from the whole server, every channel and chat, and kept from coming back. To keep them out of this channel only, use /ban.`,
  exileYes: 'Exile',
  exiled: (p) => `${p} is exiled from the server.`,
  notExiled: (p) => `@${p} is not exiled.`,
  unexiled: (p) => `@${p} may register again. Their old account is not brought back.`,
  unignored: (p) => `You unblocked ${p}.`,
  protectedName: (n) => `/${n} is a built-in command that cannot be replaced. Rename this one: until then the built-in runs.`,
  protectedBotCommand: (n) => `/${n} is a built-in command that cannot be replaced: the built-in runs. The bot must register another name.`,
  replacesBuiltin: (n) => `/${n} is also a built-in command; on this server this one replaces it.`,
  replacesBuiltinForYou: (n) => `/${n} is also a built-in command; for you this one replaces it.`,
  muted: (p, d) => `${p} cannot post here for ${d}.`,
  opped: (p) => `${p} is a manager of this channel.`,
  deopped: (p) => `${p} is a member of this channel.`,
  ignored: (p) => `You blocked ${p}. Their messages are collapsed for you.`,
  nick: (n) => `Your display name is now ${n}.`,
  cancel: 'Cancel',
};

// A duration in words, for what the chat says.
export function durationWords(secs) {
  const parts = [];
  for (const [n, w] of [[604800, 'week'], [86400, 'day'], [3600, 'hour'], [60, 'minute']]) {
    const k = Math.floor(secs / n);
    if (k) { parts.push(`${k} ${w}${k === 1 ? '' : 's'}`); secs -= k * n; }
  }
  return parts.join(' ') || 'a moment';
}
