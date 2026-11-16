// The dialogs of the commands of ruling R74 in the web chat: /help (every
// command this person can use here, grouped, searchable, the one chosen put
// in the message box), /banlist with lifting, and the requests to join a
// channel that its managers answer (/knock), with the bar that says some
// are waiting. The words are in /shared/commands.js, STRINGS.

import { get, post, del } from '/shared/api.js';
import { h, fill, toast, openDialog, dateTime, avatar, button } from '/shared/dom.js';
import { STRINGS, helpGroups, aliasesOf } from '/shared/commands.js';
import { store, conversation, account, nameOf, needAccounts, actsAsManager } from './store.js';

const S = STRINGS;

// Every dialog here closes with one button at its foot.
export const closeButton = (close) => button(S.close, () => close(undefined));

// helpDialog lists the commands of the server's list for the conversation
// and resolves with the name of the one chosen, or undefined.
export function helpDialog(listed, query) {
  return openDialog(S.helpTitle, (close) => {
    const search = h('input', { type: 'search', 'aria-label': S.helpSearch, placeholder: S.helpSearch, value: query || '', autofocus: true });
    const list = h('div', { class: 'help-list' });
    const draw = () => {
      const groups = helpGroups(listed, search.value);
      if (!groups.length) { fill(list, h('p', { class: 'dim' }, S.helpNone)); return; }
      fill(list, ...groups.map(([g, cmds]) => h('section', {},
        h('h3', { class: 'label' }, S.groups[g]),
        h('ul', { class: 'rows' }, cmds.map((c) => {
          const also = c.kind === 'builtin' ? aliasesOf(c.name).map((a) => '/' + a) : [];
          const detail = [c.description, also.length ? S.helpAlso(also.join(', ')) : ''].filter(Boolean).join(' · ');
          return h('li', {}, h('button', { type: 'button', class: 'help-cmd', title: S.helpInsert(c.name), onclick: () => close(c.name) },
            h('strong', {}, '/' + c.name), c.hint ? h('span', { class: 'dim' }, ' ' + c.hint) : null,
            detail ? h('small', {}, detail) : null));
        })))));
    };
    search.addEventListener('input', draw);
    // Escape in a search box would only empty it; here it closes, as
    // everywhere else in a dialog.
    search.addEventListener('keydown', (e) => { if (e.key === 'Escape') { e.preventDefault(); close(undefined); } });
    draw();
    return { body: [search, list], foot: closeButton(close) };
  }, { wide: true });
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
