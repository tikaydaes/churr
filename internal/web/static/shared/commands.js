// The built-in slash commands (docs/FEATURES.md sections 12.3 and 12.4,
// ruling R67): the table every client holds and the one grammar they read
// typed text by. testdata/commands/commands.json is the table and
// testdata/commands/parse.json the cases this file is held to
// (e2e/commands.mjs). Nothing here touches the page, so it runs under Node.
//
// The words the web chat says about commands are here too, in STRINGS: the
// web chat keeps its wording beside the code that uses it.

import { DECKS, SPREADS } from './message/cardnames.js';

// name: [args, offered in a sealed chat]. The order is the menu's.
export const COMMANDS = {
  poll: ['form', false], rsvp: ['form', false], signup: ['form', false], approve: ['form', false], form: ['none', false],
  remind: ['raw', true],
  me: ['text', true], shrug: ['text_optional', true], tableflip: ['text_optional', true], unflip: ['text_optional', true], spoiler: ['text', true],
  roll: ['dice', false], flip: ['none', false], pick: ['options', false], shuffle: ['options', false], draw: ['draw', false],
  tarot: ['spread', false], mahjongg: ['count', false], mahjong: ['count', false],
  join: ['channel', true], leave: ['channel_optional', false], part: ['channel_optional', false], msg: ['person_text', true], topic: ['text_optional', false],
  invite: ['person_channel', false], kick: ['person', false], ban: ['person_text', false], mute: ['person_duration', false],
  op: ['person', false], deop: ['person', false], names: ['none', false], list: ['none', true],
  whois: ['person', true], ignore: ['person', true], nick: ['text', true],
  away: ['duration_optional', true], back: ['none', true], dnd: ['duration_off', true], quiet: ['duration_off', true],
};

// Another name for a command.
const ALIASES = { part: 'leave', mahjong: 'mahjongg' };

// The results the server draws: never offered in a sealed chat, which the
// server cannot write into.
export const CHANCE = new Set(['roll', 'flip', 'pick', 'shuffle', 'draw', 'tarot', 'mahjongg', 'mahjong']);

export const SHRUG = '¯\\\\\\_(ツ)\\_/¯';
const ENDINGS = { shrug: SHRUG, tableflip: '(╯°□°)╯︵ ┻━┻', unflip: '┬─┬ノ( º _ ºノ)' };

const MAX_SECONDS = 366 * 86400;
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
export function parse(text) {
  const m = /^\/([a-z0-9_+-]+)(?:\s+([\s\S]*))?$/.exec(text);
  if (!m || !Object.prototype.hasOwnProperty.call(COMMANDS, m[1])) return null;
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
  switch (COMMANDS[typed][0]) {
    case 'form': case 'raw':
      out.args = rest;
      return out;
    case 'none':
      return rest ? fail('takes_nothing') : out;
    case 'text':
      if (!rest) return fail('needs_text');
      if (name === 'me') return { name, post: rest, action: true };
      if (name === 'spoiler') return { name, post: '||' + rest + '||' };
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
    case 'person':
      return person() || (words.length > 1 ? fail('bad_person') : out);
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
      if (COMMANDS[typed][0] === 'duration_off' && rest.toLowerCase() === 'off') return { name, off: true };
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
  banQuestion: 'Ban from the server?',
  banBody: (p) => `${p} will be removed from the server and kept from coming back. This is not only this channel.`,
  banYes: 'Ban',
  banned: (p) => `${p} is banned.`,
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
