// The dialogs of the commands of ruling R74 in the web chat: /help (every
// command this person can use here, grouped and searchable, each with its
// full entry, and the things people can type), /banlist with lifting, and the requests to join a
// channel that its managers answer (/knock), with the bar that says some
// are waiting. The words are in /shared/commands.js, STRINGS.

import { get, post, del } from '/shared/api.js';
import { h, fill, toast, openDialog, dateTime, avatar, button, icon } from '/shared/dom.js';
import { STRINGS, helpGroups, aliasesOf } from '/shared/commands.js';
import { HELP, LABELS, words, entry, findListed, things, thingMatches } from '/shared/help.js';
import { store, conversation, account, nameOf, needAccounts, actsAsManager } from './store.js';

const S = STRINGS;

// Every dialog here closes with one button at its foot.
export const closeButton = (close) => button(S.close, () => close(undefined));

// helpDialog is /help (ruling R74): every command this person can use
// here, grouped, with its one-line summary, then "Things you can type",
// all searchable; choosing a command, or naming it after /help, shows its
// full entry, and from there it can go in the message box. Everything it
// says comes from /shared/help.js, which the app draws from too. ctx:
// { sealed, patterns: { server, here } }. It resolves with the name of
// the command to put in the box, or undefined.
export function helpDialog(listed, query, ctx = {}) {
  return openDialog(S.helpTitle, (close) => {
    const search = h('input', { type: 'search', 'aria-label': S.helpSearch, placeholder: S.helpSearch, value: '', autofocus: true });
    const view = h('div', { class: 'help' });
    let opener = null;
    const putIn = h('button', { type: 'button', class: 'btn primary', hidden: true }, '');
    const showList = (focusName) => {
      putIn.hidden = true;
      search.hidden = false;
      fill(view, ...helpList(listed, search.value, ctx, (c) => { opener = c.name; showEntry(c); }));
      if (focusName) {
        const back = view.querySelector(`[data-cmd="${CSS.escape(focusName)}"]`);
        if (back) back.focus();
      }
    };
    const showEntry = (c) => {
      const e = entry(c, listed);
      search.hidden = true;
      fill(view, ...helpEntry(e, () => showList(opener), (name) => {
        const to = (listed || []).find((x) => x.name === name);
        if (to) showEntry(to);
      }));
      putIn.textContent = S.helpInsert(e.name);
      putIn.hidden = false;
      putIn.onclick = () => close(e.name);
      view.querySelector('h3').focus();
      const body = view.closest('.dialog-body');
      if (body) body.scrollTop = 0;
    };
    search.addEventListener('input', () => showList());
    // Escape in a search box would only empty it; here it closes, as
    // everywhere else in a dialog.
    search.addEventListener('keydown', (e) => { if (e.key === 'Escape') { e.preventDefault(); close(undefined); } });
    // /help roll is the entry for /roll; any other word searches.
    const named = findListed(listed, query);
    if (named) { opener = named.name; queueMicrotask(() => showEntry(named)); } else { search.value = query || ''; showList(); }
    return { body: [search, view], foot: [putIn, closeButton(close)] };
  }, { wide: true });
}

// helpList is the overview: the commands by group, each a button that
// opens its entry, then the things people can type.
function helpList(listed, query, ctx, open) {
  const groups = helpGroups(listed, query);
  const typed = things(ctx).filter((t) => thingMatches(t, query));
  const mdRows = HELP.markdown.filter((t) => thingMatches(t, query));
  const out = [];
  if (typed.length || mdRows.length) {
    out.push(h('a', { class: 'help-jump', href: '#help-things', onclick: (e) => { e.preventDefault(); const t = document.getElementById('help-things'); t.scrollIntoView({ block: 'start' }); t.focus(); } }, LABELS.jumpThings));
  }
  if (!groups.length) out.push(h('p', { class: 'dim' }, S.helpNone));
  for (const [g, cmds] of groups) {
    out.push(h('section', { class: 'help-group' },
      h('h3', { class: 'label' }, S.groups[g]),
      h('ul', { class: 'rows' }, cmds.map((c) => {
        const also = c.kind === 'builtin' ? aliasesOf(c.name).map((a) => '/' + a) : [];
        const detail = [c.description, also.length ? S.helpAlso(also.join(', ')) : ''].filter(Boolean).join(' · ');
        const b = h('button', { type: 'button', class: 'help-cmd', 'data-cmd': c.name, title: words('open', { name: c.name }), onclick: () => open(c) },
          h('strong', {}, '/' + c.name), c.hint ? h('span', { class: 'dim' }, ' ' + c.hint) : null,
          detail ? h('small', {}, detail) : null);
        return h('li', {}, b);
      }))));
  }
  if (typed.length || mdRows.length) out.push(thingsSection(typed, mdRows, ctx));
  return out;
}

// thingsSection is "Things you can type": the list's two rules, a row per
// thing that opens to say what it becomes, how the list behaves and an
// example, and the formatting.
function thingsSection(typed, mdRows, ctx) {
  return h('section', { class: 'help-things', 'aria-labelledby': 'help-things' },
    h('h3', { class: 'label', id: 'help-things', tabindex: '-1' }, LABELS.things),
    h('p', { class: 'dim' }, LABELS.thingsIntro),
    h('div', { class: 'help-rules' }, h('h4', {}, LABELS.popupRules), ...HELP.popup_rules.map((r) => h('p', {}, r))),
    ...typed.map((t) => h('details', { class: 'fold help-thing', 'data-thing': t.id },
      h('summary', {}, h('span', { class: 'grow' }, t.title), h('code', { class: 'wrap' }, t.type)),
      h('div', { class: 'stack tight' },
        fact(LABELS.becomes, t.becomes),
        fact(LABELS.popup, t.popup),
        t.patterns ? patternsBlock(t.patterns) : null,
        t.units ? unitsBlock(t.units) : null,
        h('p', {}, h('span', { class: 'label' }, LABELS.example), ' ', h('code', { class: 'wrap' }, t.example.type), ' ', t.example.then),
        t.sealed ? fact(LABELS.inSealed, t.sealed) : null))),
    mdRows.length ? h('h4', {}, LABELS.formatting) : null,
    mdRows.length ? h('dl', { class: 'help-md' }, mdRows.flatMap((m) => [h('dt', {}, h('code', { class: 'wrap' }, m.type)), h('dd', {}, m.becomes)])) : null);
}

const fact = (label, text) => h('p', {}, h('span', { class: 'label' }, label), ' ', text);

function patternsBlock(p) {
  const list = (title, rows) => rows.length ? [h('h5', { class: 'label' }, title), h('ul', { class: 'help-patterns' }, rows.map((r) => h('li', {}, h('strong', {}, r.name), ' ', h('code', {}, r.prefix), ': ', words('patternExample', { example: r.example, host: r.host }))))] : [];
  return h('div', { class: 'help-live' },
    ...list(LABELS.patternsHere, p.here), ...list(LABELS.patternsServer, p.server),
    p.none ? h('p', {}, LABELS.patternsNone) : null,
    p.sealed ? h('p', { class: 'dim' }, LABELS.patternsSealed) : null);
}

function unitsBlock(kinds) {
  return h('dl', { class: 'help-units' }, kinds.flatMap((k) => [h('dt', {}, k.title), h('dd', {}, k.units.map((u) => u.typed.join(' or ')).join(', '))]));
}

// helpEntry is one command's full entry.
function helpEntry(e, back, go) {
  const out = [
    h('button', { type: 'button', class: 'btn quiet small help-back', onclick: back }, icon('back'), ' ', LABELS.back),
    h('h3', { class: 'help-title', tabindex: '-1' }, '/' + e.name),
  ];
  if (e.summary) out.push(h('p', { class: 'help-summary' }, e.summary));
  out.push(h('section', {}, h('h4', { class: 'label' }, LABELS.usage), h('p', {}, h('code', { class: 'wrap' }, e.usage)),
    e.args.length ? h('dl', { class: 'help-args' }, e.args.flatMap((a) => [h('dt', {}, h('code', {}, a.name)), h('dd', {}, a.about)])) : null));
  if (e.description) out.push(h('p', {}, e.description));
  if (e.examples.length) {
    out.push(h('section', {}, h('h4', { class: 'label' }, LABELS.examples),
      h('ul', { class: 'help-examples' }, e.examples.map((x) => h('li', {}, h('code', { class: 'wrap' }, x.type), h('span', {}, x.then))))));
  }
  if (e.more) {
    out.push(h('section', {}, h('h4', { class: 'label' }, e.more.title),
      h('ul', { class: 'help-more' }, e.more.rows.map((r) => h('li', {}, h('strong', {}, r.name), r.aliases.length ? h('span', { class: 'dim' }, ' (' + r.aliases.join(', ') + ')') : null, ' · ', r.count, r.about ? h('small', {}, r.about) : null)))));
  }
  const facts = [
    e.aliases.length ? [LABELS.aliases, e.aliases.join(', ')] : null,
    e.where ? [LABELS.where, e.where] : null,
    e.who ? [LABELS.who, e.who] : null,
  ].filter(Boolean);
  if (facts.length) out.push(h('dl', { class: 'help-facts' }, facts.flatMap(([k, v]) => [h('dt', { class: 'label' }, k), h('dd', {}, v)])));
  for (const n of e.notes) out.push(h('p', { class: 'dim' }, n));
  if (e.related.length) {
    out.push(h('section', {}, h('h4', { class: 'label' }, LABELS.related),
      h('div', { class: 'help-related' }, e.related.map((r) => h('button', { type: 'button', class: 'btn small', onclick: () => go(r) }, '/' + r)))));
  }
  return out;
}

// banListDialog lists who is banned from a channel; whoever may see the list
// may lift a ban from it. A refusal is the server's, in its words.
export async function banListDialog(id) {
  let bans;
  try { bans = (await get(`/conversations/${id}/bans`)).bans; } catch (err) { toast(err.message, true); return; }
  needAccounts(bans.flatMap((b) => [b.account_id, b.created_by_id]));
  const c = conversation(id) || {};
  await openDialog(S.banListTitle(c.name ? '#' + c.name : ''), (close) => {
    const list = h('ul', { class: 'rows' });
    const draw = () => fill(list, bans.length ? bans.map((b) => h('li', { class: 'row' },
      avatar(account(b.account_id), 'small'),
      h('span', { class: 'grow' }, h('strong', {}, nameOf(b.account_id)),
        h('small', {}, [S.bannedBy(nameOf(b.created_by_id), b.created_at ? dateTime(b.created_at) : ''), b.expires_at ? S.bannedUntil(dateTime(b.expires_at)) : '', b.reason ? '“' + b.reason + '”' : ''].filter(Boolean).join(' · '))),
      button(S.liftBan, async () => {
        try { await del(`/conversations/${id}/bans/${b.account_id}`); bans = bans.filter((x) => x !== b); draw(); toast(S.banLifted); } catch (err) { toast(err.message, true); }
      }, 'small'))) : [h('li', { class: 'dim' }, S.noBans)]);
    draw();
    return { body: list, foot: closeButton(close) };
  });
}

// knocksDialog lists who asks to join a channel, each with Add and Decline.
export async function knocksDialog(id) {
  let knocks;
  try { knocks = (await get(`/conversations/${id}/knocks`)).knocks; } catch (err) { toast(err.message, true); return; }
  needAccounts(knocks.map((k) => k.account_id));
  const c = conversation(id) || {};
  await openDialog(S.knocksTitle('#' + (c.name || '')), (close) => {
    const list = h('ul', { class: 'rows' });
    const answer = async (k, accept) => {
      try {
        await post(`/conversations/${id}/knocks/${k.id}`, { accept });
        knocks = knocks.filter((x) => x !== k);
        draw();
        toast(accept ? S.knockAdded(nameOf(k.account_id)) : S.knockTurnedAway(nameOf(k.account_id)));
      } catch (err) { toast(err.message, true); }
    };
    const draw = () => fill(list, knocks.length ? knocks.map((k) => h('li', { class: 'row' },
      avatar(account(k.account_id), 'small'),
      h('span', { class: 'grow' }, h('strong', {}, nameOf(k.account_id)), h('small', {}, dateTime(k.created_at))),
      button(S.knockDecline, () => answer(k, false), 'small'),
      button(S.knockAccept, () => answer(k, true), 'small primary'))) : [h('li', { class: 'dim' }, S.knocksNone)]);
    draw();
    return { body: list, foot: closeButton(close) };
  });
}

// knocksBar is the bar a private channel's managers see while people ask
// to join it. It keeps itself current from knock events, and returns the
// element and a function that stops it.
export function knocksBar(id) {
  const el = h('div', { class: 'knocks-bar', hidden: true });
  const refresh = async () => {
    const c = conversation(id);
    if (!c || !c.name || !c.private || !actsAsManager(c)) return;
    let n = 0;
    try { n = (await get(`/conversations/${id}/knocks`)).knocks.length; } catch { /* not theirs to see */ }
    el.hidden = n === 0;
    fill(el, h('span', { class: 'grow' }, S.knocksWaiting(n)), button(S.knocksReview, async () => { await knocksDialog(id); refresh(); }, 'small'));
  };
  const off = store.on('knock', (e) => { if (e.knock && e.knock.conversation_id === id) refresh(); });
  refresh();
  return { el, stop: off };
}

// knockOn asks to join a knock channel and says so.
export async function knockOn(c) {
  try {
    await post(`/conversations/${c.id}/knocks`);
    toast(S.knockAsked('#' + (c.name || '')));
    return true;
  } catch (err) { toast(err.message, true); return false; }
}

// knockPicker lists the knock channels this person can see and is not in
// (several may share a name, so they choose one) and asks to join the one
// chosen (`/knock`, ruling R79).
export async function knockPicker() {
  let list;
  try { list = (await get('/channels', { limit: 200 })).conversations.filter((c) => c.access === 'knock' && !c.membership); } catch (err) { toast(err.message, true); return; }
  needAccounts(list.flatMap((c) => c.manager_ids || []));
  const chosen = await openDialog(S.knockPickerTitle, (close) => ({
    body: list.length ? h('ul', { class: 'rows' }, list.map((c) => h('li', {},
      h('span', { class: 'grow' }, h('strong', {}, '#' + c.name),
        h('small', {}, [c.topic, `${c.member_count} members`, c.manager_ids && c.manager_ids.length ? S.knockManagers(c.manager_ids.map(nameOf).join(', ')) : ''].filter(Boolean).join(' · '))),
      button(S.askToJoin, () => close(c), 'small primary')))) : h('p', { class: 'dim' }, S.knockPickerNone),
    foot: closeButton(close),
  }));
  if (chosen) await knockOn(chosen);
}
