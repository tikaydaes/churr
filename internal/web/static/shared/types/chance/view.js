// How the chance kinds besides a roll look (docs/FAIR.md section 5a): a coin
// flipped, a pick, a shuffle, cards drawn and a tarot reading, each a whole
// message's card, drawn as the server's chance results always were: the
// reels, the card faces and tarot's spreads of shared/message/chance.js and
// its artwork, spinning or turning once when the message arrives live, still
// with reduced motion. Everything shown is text the server's data holds, set
// as text.

import { renderChance, spinLive, spokenResult } from '../../message/chance.js';
import { asChance } from './data.js';

// viewOf is a kind's view: its card and the words a screen reader hears.
export function viewOf(kind) {
  return {
    tooltip: (d) => spokenResult(asChance(kind, d)),
    card(d, { live } = {}) {
      const c = asChance(kind, d);
      const el = renderChance(c);
      if (live) spinLive(el, c, live, 'view');
      return el;
    },
  };
}
