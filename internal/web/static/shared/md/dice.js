// The grammar of a dice expression (internal/model/chance.go ParseDice;
// docs/FEATURES.md section 12.4), for inline dice (ruling R73), which the
// client recognises in the composer while the server rolls them.
// testdata/commands/dice.json holds the cases. No DOM.

const MAX_TEXT = 100, MAX_TERMS = 10, MAX_DICE = 100, MAX_SIDES = 1000, MAX_NUMBER = 10000;

// diceTerm reads one term at the start of s: { t, n } or null.
function diceTerm(s) {
  for (const [word, keep] of [['adv', 'kh'], ['dis', 'kl']]) if (s.startsWith(word)) return { t: { count: 2, sides: 20, keep, keepN: 1, value: 0 }, n: word.length };
  let i = 0;
  const num = () => {
    const start = i;
    while (i < s.length && s[i] >= '0' && s[i] <= '9') i++;
    if (i === start) return { has: false };
    if (i - start > 6) return { err: true };
    return { has: true, v: Number(s.slice(start, i)) };
  };
  const c = num();
  if (c.err) return null;
  if (i >= s.length || s[i] !== 'd') {
    if (!c.has || c.v > MAX_NUMBER) return null;
    return { t: { count: 0, value: c.v }, n: i };
  }
  i++;
  const count = c.has ? c.v : 1;
  if (count < 1 || count > MAX_DICE) return null;
  let sides;
  if (s[i] === '%') { sides = 100; i++; } else {
    const x = num();
    if (x.err || !x.has) return null;
    sides = x.v;
  }
  if (sides < 2 || sides > MAX_SIDES) return null;
  const t = { count, sides, value: 0 };
  if (s[i] === 'k' || (s[i] === 'd' && (s[i + 1] === 'h' || s[i + 1] === 'l'))) {
    let mode = s[i];
    i++;
    if (s[i] === 'h' || s[i] === 'l') { mode += s[i]; i++; } else mode += 'h';
    const x = num();
    if (x.err) return null;
    const n = x.has ? x.v : 1;
    if (mode[0] === 'k' && (n < 1 || n > count)) return null;
    if (mode[0] === 'd' && (n < 1 || n >= count)) return null;
    t.keep = mode; t.keepN = n;
  }
  return { t, n: i };
}

// parseDice reads an expression and returns it written out in full
// ("d20" is "1d20"), or null when /roll would refuse it.
export function parseDice(expr) {
  if ([...String(expr)].length > MAX_TEXT) return null;
  const fields = String(expr).toLowerCase().split(/\s+/).filter(Boolean);
  for (let k = 1; k < fields.length; k++) {
    const a = fields[k - 1], b = fields[k];
    if (!'+-'.includes(a[a.length - 1]) && !'+-'.includes(b[0])) return null;
  }
  let s = fields.join('');
  if (!s) s = '1d20';
  const terms = [];
  let dice = 0, i = 0;
  while (i < s.length) {
    let sign = 1;
    if (s[i] === '+') i++;
    else if (s[i] === '-') { sign = -1; i++; } else if (terms.length) return null;
    if (i >= s.length) return null;
    const r = diceTerm(s.slice(i));
    if (!r) return null;
    i += r.n;
    r.t.sign = sign;
    terms.push(r.t);
    if (terms.length > MAX_TERMS) return null;
    dice += r.t.count;
    if (dice > MAX_DICE) return null;
  }
  return terms.map((t, k) => (t.sign < 0 ? '-' : k > 0 ? '+' : '') + (t.count ? `${t.count}d${t.sides}${t.keep ? t.keep + t.keepN : ''}` : String(t.value))).join('');
}
