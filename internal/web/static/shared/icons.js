// Every icon the pages draw, as inline SVG (docs/STYLE.md, "Icons").
//
// The rules: a 24px grid, a 2px line, square line ends and sharp joins.
// Corners are cut at 45° instead of rounded; a circle is drawn only where
// the thing is round (a clock, a face). Outline only: a solid fill means
// "on". The first fourteen are the guide's own drawings (chat, thread,
// bell, bookmark, search, pin, plus, send, edit, attach, call, sealed,
// settings, star); the rest are drawn to the same rules.
//
// Each entry is a list of path data. A path that starts with "F" is filled
// instead of stroked.

const SVG = 'http://www.w3.org/2000/svg';

export const ICONS = {
  // the guide's fourteen
  chat: ['M4 5h13l3 3v9H9l-5 4z'],
  thread: ['M4 5h16v10H10l-4 4v-4H4z', 'M8 9h8M8 12h5'],
  bell: ['M10 4h4l4 4v6l2 3H4l2-3V8z', 'M10 20h4'],
  bookmark: ['M6 4h12v16l-6-4-6 4z'],
  search: ['M5 5h8l3 3v5l-3 3H8l-3-3z', 'M15 15l5 5'],
  pin: ['M9 4h6v6l3 4H6l3-4z', 'M12 14v6'],
  plus: ['M12 5v14M5 12h14'],
  send: ['M4 12L20 5l-4 15-4-6z', 'M12 14l8-9'],
  edit: ['M14 4l-8 8v6h6l8-8z', 'M12 6l6 6'],
  attach: ['M8 13l7-7a3 3 0 014 4l-9 9a5 5 0 01-7-7l8-8'],
  call: ['M4 7h10v10H4z', 'M14 10l6-3v10l-6-3'],
  sealed: ['M6 10V7l3-3h6l3 3v3', 'M4 10h16v10H4z', 'M12 14v3'],
  settings: ['M4 6h16M4 12h16M4 18h16', 'FM7 4h4v4H7zM14 10h4v4h-4zM9 16h4v4H9z'],
  star: ['M12 3l3 6 6 1-4.5 4.5 1 6.5-5.5-3-5.5 3 1-6.5L3 10l6-1z'],
  // a card of four swatches, its corner cut: a colour theme
  palette: ['M4 4h12l4 4v12H4z', 'M4 12h16', 'M12 4v16'],

  // channels and conversations
  hash: ['M10 4L8 20M16 4l-2 16M5 9h15M4 15h15'],
  // a private channel: the padlock of the channel list
  lock: ['M8 10V7l2-2h4l2 2v3', 'FM5 10h14v10H5z'],
  // a locked channel: nobody can be added or join
  padlock: ['M8 10V7l2-2h4l2 2v3', 'M5 10h14v10H5z', 'M12 13v4'],
  shield: ['M12 3l8 3v7l-3 5-5 3-5-3-3-5V6z'],
  people: ['M7 5h4l1 1v4l-1 1H7L6 10V6z', 'M3 20v-3l2-2h8l2 2v3', 'M15 5h2l1 1v4l-1 1h-2', 'M17 15h2l2 2v3'],
  person: ['M10 4h4l2 2v4l-2 2h-4l-2-2V6z', 'M5 21v-3l3-3h8l3 3v3'],
  bot: ['M5 8h14v11H5z', 'M12 8V4M9 12v2M15 12v2M2 12v4M22 12v4'],
  server: ['M3 4h18v7H3zM3 13h18v7H3z', 'M7 7.5h2M7 16.5h2'],
  at: ['M9 9h6v6H9z', 'M15 13l2 2h1l2-2V8l-4-4H8L4 8v8l4 4h9'],

  // notifications and state
  'bell-off': ['M10 4h4l4 4v6l2 3H4l2-3V8z', 'M10 20h4', 'M3 3l18 18'],
  eye: ['M2 12l5-6h10l5 6-5 6H7z', 'M10 10h4v4h-4z'],
  'eye-off': ['M2 12l5-6h10l5 6-5 6H7z', 'M4 4l16 16'],
  moon: ['M14 3H9L4 8v8l5 5h6l5-5v-1c-7 2-11-5-6-12z'],
  clock: ['M12 3a9 9 0 110 18 9 9 0 010-18z', 'M12 7v5l4 3'],
  history: ['M4 12a8 8 0 102.3-5.7', 'M4 3v5h5', 'M12 8v4l3 2'],
  warn: ['M12 3l10 18H2z', 'M12 10v5M12 17v2'],
  info: ['M4 4h13l3 3v13H4z', 'M12 11v6M12 7v2'],
  dot: ['FM8 8h8v8H8z'],
  check: ['M4 12l5 5L20 6'],
  close: ['M5 5l14 14M19 5L5 19'],

  // moving about
  'chevron-right': ['M9 5l7 7-7 7'],
  'chevron-down': ['M5 9l7 7 7-7'],
  back: ['M20 12H5M11 5l-7 7 7 7'],
  'arrow-up': ['M12 20V5M5 11l7-7 7 7'],
  'arrow-down': ['M12 4v15M5 13l7 7 7-7'],
  menu: ['M4 6h16M4 12h16M4 18h16'],
  more: ['FM4 10h4v4H4zM10 10h4v4h-4zM16 10h4v4h-4z'],
  out: ['M14 4h6v6M20 4l-9 9M18 14v6H4V6h6'],
  'sign-out': ['M10 4H4v16h6M15 7l5 5-5 5M9 12h11'],
  refresh: ['M20 4v6h-6', 'M19 10l-2-4-4-2H10L6 6l-2 4v3l2 4 4 2h3l4-2 2-3'],

  // messages
  reply: ['M10 6l-6 6 6 6', 'M4 12h11l5 5v3'],
  quote: ['M4 4v16M9 8h11M9 12h11M9 16h7'],
  code: ['M8 6l-6 6 6 6M16 6l6 6-6 6'],
  emoji: ['M12 3a9 9 0 110 18 9 9 0 010-18z', 'M8 14l2 2h4l2-2', 'M9 9v1M15 9v1'],
  trash: ['M4 7h16M9 7V4h6v3', 'M6 7l1 14h10l1-14M10 11v6M14 11v6'],
  flag: ['M5 21V4h13l-3 4.5 3 4.5H5'],
  copy: ['M9 9h11v12H9z', 'M5 15H4V3h11v2'],
  download: ['M12 3v13M6 10l6 6 6-6M4 21h16'],
  link: ['M10 14l4-4', 'M9 11l-4 4v1l3 3h1l4-4', 'M15 13l4-4V8l-3-3h-1l-4 4'],
  file: ['M6 3h8l4 4v14H6z', 'M14 3v4h4'],
  image: ['M3 5h18v14H3z', 'M3 16l5-5 4 4 3-3 6 6', 'FM15 8h2v2h-2z'],
  poll: ['M4 20V10M10 20V4M16 20v-7M21 20H3'],
  // a die, its corner cut: a result the server drew (ruling R67)
  dice: ['M4 4h12l4 4v12H4z', 'FM7 7h3v3H7zM10.5 10.5h3v3h-3zM14 14h3v3h-3z'],
  'list-check': ['M3 6l2 2 4-4M3 15l2 2 4-4', 'M12 7h9M12 16h9'],
  key: ['M6 10h3l2 2v3l-2 2H6l-2-2v-3z', 'M10 12l9-9M15 7l3 3M13 9l2 2'],

  // calls (chat/call/): the camera is the guide's `call`
  mic: ['M9 3h6v10H9z', 'M5 11v2l3 4h8l3-4v-2', 'M12 17v4M8 21h8'],
  'mic-off': ['M9 3h6v10H9z', 'M5 11v2l3 4h8l3-4v-2', 'M12 17v4M8 21h8', 'M3 3l18 18'],
  'camera-off': ['M4 7h10v10H4z', 'M14 10l6-3v10l-6-3', 'M3 3l18 18'],
  screen: ['M3 4h18v12H3z', 'M8 20h8M12 16v4'],
  'call-leave': ['M3 14l3-5h12l3 5v3h-5v-3H8v3H3z'],
  expand: ['M4 10V4h6M20 14v6h-6M4 4l6 6M20 20l-6-6'],
  shrink: ['M10 4v6H4M14 20v-6h6M10 10L4 4M14 14l6 6'],
  // speaking: three solid bars, the shape that goes with the outline
  speaking: ['FM4 10h3v4H4zM10 5h3v14h-3zM16 8h3v8h-3z'],
  grid: ['M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z'],
  spotlight: ['M3 4h18v11H3z', 'M3 19h5M10 19h4M16 19h5'],
};

// draw makes one icon. Without a label it is decoration and hidden from a
// screen reader; with one it is read out as an image of that name.
export function draw(name, label, cls) {
  const svg = document.createElementNS(SVG, 'svg');
  svg.setAttribute('class', 'icon' + (cls ? ' ' + cls : ''));
  svg.setAttribute('viewBox', '0 0 24 24');
  if (label) { svg.setAttribute('role', 'img'); svg.setAttribute('aria-label', label); }
  else svg.setAttribute('aria-hidden', 'true');
  for (const d of ICONS[name] || ICONS.dot) {
    const p = document.createElementNS(SVG, 'path');
    if (d[0] === 'F') { p.setAttribute('d', d.slice(1)); p.setAttribute('class', 'fill'); } else p.setAttribute('d', d);
    svg.append(p);
  }
  return svg;
}

// presence draws a person's state as a shape, never as colour alone:
// a solid square when online, a hollow square when away, a diamond for do
// not disturb and a dotted hollow square when offline. In a sealed chat
// the square becomes a shield with the same rules: solid, outline, solid
// with a bar, dotted outline.
export function presence(state, opts = {}) {
  const s = ['online', 'away', 'dnd', 'offline'].includes(state) ? state : 'offline';
  const words = { online: 'online', away: 'away', dnd: 'do not disturb', offline: 'offline' }[s];
  const label = opts.sealed ? 'sealed, ' + words : words;
  if (!opts.sealed) {
    const el = document.createElement('span');
    el.className = 'presence ' + s;
    el.setAttribute('role', 'img');
    el.setAttribute('aria-label', label);
    return el;
  }
  const svg = document.createElementNS(SVG, 'svg');
  svg.setAttribute('class', 'shield-p ' + s);
  svg.setAttribute('viewBox', '0 0 14 14');
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', label);
  const p = document.createElementNS(SVG, 'path');
  p.setAttribute('d', 'M7 1.2l5 1.8v4.6l-1.6 2.8L7 12.8 3.6 10.4 2 7.6V3z');
  svg.append(p);
  if (s === 'dnd') {
    const bar = document.createElementNS(SVG, 'path');
    bar.setAttribute('d', 'M4.5 7h5');
    bar.setAttribute('class', 'bar');
    svg.append(bar);
  }
  return svg;
}
