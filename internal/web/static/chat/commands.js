// Carrying out the built-in commands of ruling R67 in the web chat. The
// text is read by /shared/commands.js (the one grammar); each command here
// makes the ordinary requests a button elsewhere would make, so every
// permission is the server's, and a refusal is shown in the server's words.

import { get, post, put, patch, del } from '/shared/api.js';
import { toast, confirmDialog, dateTime } from '/shared/dom.js';
import { STRINGS, CHANCE, COMMANDS, durationWords, SERVER_BAN, SERVER_UNBAN } from '/shared/commands.js';
import { store, conversation, putMessage, title, isSealed, needAccounts } from './store.js';
import { visibleChannels } from './env.js';
import { ui } from './ui.js';
import { openProfile, watchFor, unwatchFor } from './profile.js';
import { openSettings } from './settings.js';
import { updateMembership } from './modes.js';

// A thrown Said is shown as it is and is not a failure of the request.
class Said extends Error {}
const say = (text) => { throw new Said(text); };

// person finds the account a typed `@name` or `@name@server` means.
async function person(p) {
  const [username, server] = p.split('@');
  const match = (a) => a.username === username && (a.server_name || '') === (server || '') && !a.removed_at;
  let found = [...store.accounts.values()].find(match);
  if (!found) {
    try {
      const r = await get('/accounts', { q: username, limit: 20 });
      for (const a of r.accounts) store.accounts.set(a.id, { ...store.accounts.get(a.id), ...a });
      found = r.accounts.find(match);
    } catch { /* not found below */ }
  }
  return found || say(STRINGS.noSuchPerson(p));
}

// channel finds the conversation a typed channel means: a link the picker
// put in, or a name, which must be one channel's.
function channel(ref) {
  if (ref.id) return conversation(ref.id) || say(STRINGS.noSuchChannel(ref.id));
  const name = ref.name.toLowerCase();
  const found = visibleChannels().filter((c) => (c.name || '').toLowerCase() === name);
  if (found.length > 1) say(STRINGS.severalChannels(ref.name));
  return found[0] || say(STRINGS.noSuchChannel(ref.name));
}

const name = (a) => a.display_name || a.username;

// chanceInput is what the server is asked to draw.
function chanceInput(c) {
  switch (c.name) {
    case 'roll': return { kind: 'roll', dice: c.dice || undefined };
    case 'flip': return { kind: 'flip' };
    case 'pick': case 'shuffle': return { kind: c.name, options: c.options };
    case 'draw': return { kind: 'draw', count: c.count, deck: c.deck };
    case 'mahjongg': return { kind: 'draw', count: c.count, deck: 'mahjong' };
    case 'tarot': return { kind: 'tarot', spread: c.spread };
  }
  return null;
}

// run carries out a command read by parse(). ctx: { conversationId,
// parentId, composer } where composer has clear(), post(content) and
// say(text). It returns true when the command was dealt with, and false
// for the commands the composer carries out itself (forms, /remind).
export async function run(c, ctx) {
  const here = conversation(ctx.conversationId) || { id: ctx.conversationId };
  const sealed = isSealed(here);
  if (c.error) { toast(STRINGS.errors[c.error](c.name), true); return true; }
  if (['poll', 'rsvp', 'signup', 'approve', 'form', 'remind'].includes(c.name)) return false;
  if (sealed && !(COMMANDS[c.name] || [])[1]) { toast(STRINGS.notInSealed(c.name), true); return true; }
  try {
    await carryOut(c, ctx, here);
  } catch (err) {
    if (err instanceof Said) toast(err.message, true);
    else if (!err.silent) toast(err.message || 'That did not work.', true);
  }
  return true;
}

async function carryOut(c, ctx, here) {
  const id = here.id;
  const done = (text) => { ctx.composer.clear(); if (text) toast(text); };
  if (c.post !== undefined) {
    // /me and the text conveniences post a message as typed.
    if (ctx.composer.broadOK && !(await ctx.composer.broadOK(c.post))) return;
    await ctx.composer.post({ text: c.post, action: c.action || undefined, parent_id: ctx.parentId || undefined, quote: ctx.composer.quote || undefined });
    return;
  }
  if (CHANCE.has(c.name)) {
    ctx.composer.clear();
    const out = await post(`/conversations/${id}/messages`, { client_id: crypto.randomUUID(), chance: chanceInput(c), parent_id: ctx.parentId || undefined });
    const m = putMessage(out.message);
    if (m && !m._played) { m._live = true; store.emit('messages', id); }
    return;
  }
  switch (c.name) {
    case 'join': {
      const to = channel(c.channel);
      if (!to.membership) {
        const out = await post(`/conversations/${to.id}/join`);
        store.conversations.set(to.id, { ...to, ...out.conversation });
        store.emit('conversations');
      }
      done(STRINGS.joined(title(conversation(to.id) || to)));
      ui.openConversation(to.id);
      return;
    }
    case 'leave': {
      const from = c.channel ? channel(c.channel) : here;
      await post(`/conversations/${from.id}/leave`);
      done(STRINGS.left(title(from)));
      return;
    }
    case 'msg': {
      const a = await person(c.person);
      const out = await post('/chats', { account_ids: [a.id] });
      store.conversations.set(out.conversation.id, { ...store.conversations.get(out.conversation.id), ...out.conversation });
      needAccounts(out.conversation.member_ids);
      store.emit('conversations');
      if (c.text) putMessage((await post(`/conversations/${out.conversation.id}/messages`, { client_id: crypto.randomUUID(), text: c.text })).message);
      done();
      ui.openConversation(out.conversation.id);
      return;
    }
    case 'topic':
      if (!c.text) { done(STRINGS.topicIs(here.topic)); return; }
      store.conversations.set(id, { ...here, ...(await patch(`/conversations/${id}`, { topic: c.text })).conversation });
      store.emit('conversations');
      done(STRINGS.topicSet);
      return;
    case 'invite': {
      const a = await person(c.person);
      const to = c.channel ? channel(c.channel) : here;
      await post(`/conversations/${to.id}/members`, { account_ids: [a.id] });
      done(STRINGS.invited(name(a)));
      return;
    }
    case 'kick': {
      const a = await person(c.person);
      await del(`/conversations/${id}/members/${a.id}`);
      done(STRINGS.kicked(name(a)));
      return;
    }
    case 'ban': {
      // The channel ban (ruling R72): out of this channel, and kept out.
      const a = await person(c.person);
      await post(`/conversations/${id}/bans`, { account_id: a.id, reason: c.text || undefined });
      done(STRINGS.banned(name(a)));
      return;
    }
    case 'unban': {
      const a = await person(c.person);
      await del(`/conversations/${id}/bans/${a.id}`);
      done(STRINGS.unbanned(name(a)));
      return;
    }
    case SERVER_BAN: {
      // The server-wide ban, after asking, since it is far more than this
      // channel.
      const a = await person(c.person);
      if (!(await confirmDialog(STRINGS.exileQuestion(name(a)), STRINGS.exileBody(name(a)), { yes: STRINGS.exileYes, no: STRINGS.cancel, danger: true }))) return;
      await post('/admin/bans', { account_id: a.id, reason: c.text || undefined });
      done(STRINGS.exiled(name(a)));
      return;
    }
    case SERVER_UNBAN: {
      // An exiled account is removed, so it is found among the bans.
      const [username, server] = c.person.split('@');
      if (server) say(STRINGS.notExiled(c.person));
      const bans = (await get('/admin/bans', { limit: 200 })).bans.filter((b) => !b.lifted_at && b.account_id);
      for (const b of bans) {
        if (store.accounts.has(b.account_id)) continue;
        try { const a = (await get(`/accounts/${b.account_id}`)).account; store.accounts.set(a.id, a); } catch { /* not this one */ }
      }
      const ban = bans.find((b) => (store.accounts.get(b.account_id) || {}).username === username);
      if (!ban) say(STRINGS.notExiled(c.person));
      await del(`/admin/bans/${ban.id}`);
      done(STRINGS.unexiled(c.person));
      return;
    }
    case 'mute': {
      const a = await person(c.person);
      await post(`/conversations/${id}/timeouts`, { account_id: a.id, seconds: c.seconds });
      done(STRINGS.muted(name(a), durationWords(c.seconds)));
      return;
    }
    case 'op': case 'deop': {
      const a = await person(c.person);
      await patch(`/conversations/${id}/members/${a.id}`, { level: c.name === 'op' ? 'manager' : 'member' });
      done(c.name === 'op' ? STRINGS.opped(name(a)) : STRINGS.deopped(name(a)));
      return;
    }
    case 'ignore': {
      const a = await person(c.person);
      await put(`/me/blocks/${a.id}`);
      done(STRINGS.ignored(name(a)));
      return;
    }
    case 'unignore': {
      const a = await person(c.person);
      await del(`/me/blocks/${a.id}`);
      done(STRINGS.unignored(name(a)));
      return;
    }
    case 'watch': {
      const a = await person(c.person);
      done(await watchFor(a));
      return;
    }
    case 'unwatch': {
      const a = await person(c.person);
      done(await unwatchFor(a));
      return;
    }
    case 'whois': {
      const a = await person(c.person);
      done();
      openProfile(a.id);
      return;
    }
    case 'nick': {
      const out = await patch('/me', { display_name: c.text });
      Object.assign(store.me, out.account);
      done(STRINGS.nick(out.account.display_name));
      return;
    }
    case 'list': done(); ui.go('/chat/browse'); return;
    case 'names': done(); openSettings(id, 'members'); return;
    case 'away': await put('/me/away', c.seconds ? { duration_seconds: c.seconds } : {}); done(STRINGS.away); return;
    case 'back': await del('/me/away'); done(STRINGS.back); return;
    case 'lock': done(); ui.lock({ ask: true }); return;
    case 'dnd':
      if (c.off) await del('/me/dnd'); else await put('/me/dnd', c.seconds ? { duration_seconds: c.seconds } : {});
      done(c.off ? STRINGS.dndOff : STRINGS.dndOn);
      return;
    case 'quiet': {
      const until = c.off ? null : new Date(Date.now() + c.seconds * 1000).toISOString();
      if (await updateMembership(id, { muted_until: until })) done(c.off ? STRINGS.quietOff : STRINGS.quietOn(dateTime(until)));
      return;
    }
  }
}
