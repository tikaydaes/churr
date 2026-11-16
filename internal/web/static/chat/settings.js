// Members and conversation settings (docs/CLIENTS.md section 9.15): the
// member list with levels and groups; for managers the name, topic,
// section, default level, welcome text, retention, slow mode, archive,
// make private and lock; and for everyone "Request a webhook".

import { STRINGS } from '/shared/commands.js';
import { get, post, patch, put, del, all } from '/shared/api.js';
import { h, openDialog, button, toast, fill, avatar, confirmDialog, busy, plural, duration, dateTime, openMenu } from '/shared/dom.js';
import { formDialog, secretDialog } from '/shared/form.js';
import { patternEditor } from '/shared/objects/patterns.js';
import { pickAccounts, detailLine } from '/shared/pick.js';
import { store, conversation, account, needAccounts, title, isAdHoc, actsAsManager, isAdmin, hubOf, loadHub } from './store.js';
import { look, visibleChannels, isHubModerator } from './env.js';
import { LISTINGS, LISTING, listingName, moderatorName, isModerator, canRemoveModerator } from '/shared/hub.js';

const LEVELS = [['reader', 'Reader'], ['member', 'Member'], ['manager', 'Manager']];
const SLOW = [['0', 'Off'], ['5', '5 seconds'], ['30', '30 seconds'], ['60', 'A minute'], ['300', '5 minutes'], ['3600', 'An hour']];
const BROAD = [['server', 'Follow the server'], ['anyone', 'Anyone'], ['managers', 'Managers only']];
const LAZY = [['server', 'Follow the server'], ['on', 'Only to people who have it open'], ['off', 'To everyone, always']];
const BAN_FOR = [['0', 'Until it is lifted'], ['3600', 'An hour'], ['86400', 'A day'], ['604800', 'A week'], ['2592000', '30 days']];
const KEEP = [['0', 'The server’s period'], ['86400', 'A day'], ['604800', 'A week'], ['2592000', '30 days'], ['7776000', '90 days'], ['31536000', 'A year']];

export function openSettings(id, tab = 'about') {
  return openDialog('Channel', (close, dlg) => {
    const tabs = h('div', { class: 'tabs', role: 'tablist' });
    const body = h('div', { class: 'stack' });
    let current = tab;
    const off = store.on('conversations', () => { if (!conversation(id)) close(); });
    // The hub's record changed (stream.channel_changed): a moderator reads
    // it again for who is invited and asking, everyone redraws.
    const offHub = store.on('hub', (cid) => {
      if (cid !== id) return;
      drawTabs();
      if (current === 'hub') panes.hub();
    });
    // The list of bans is read again when one is made or lifted.
    const offBans = store.on('bans', (cid) => { if (cid === id && current === 'bans') panes.bans(); });
    dlg.addEventListener('close', () => { off(); offHub(); offBans(); });
    // banDialog bans someone from this channel (ruling R72), after asking
    // why and for how long.
    const banDialog = async (a) => {
      const who = a.display_name || a.username;
      const v = await formDialog(`Ban ${who} from ${title(conversation(id))}`, [
        { name: 'reason', label: 'Reason', maxLength: 1000, hint: 'Shown to them and to the channel’s managers.' },
        { name: 'seconds', label: 'For', type: 'select', value: '0', options: BAN_FOR },
      ], { yes: 'Ban', danger: true, intro: 'They are removed from this channel and cannot join, be added or come back through a group until the ban is lifted. Only this channel: to keep someone off the whole server, an admin exiles them.',
        submit: (x) => post(`/conversations/${id}/bans`, { account_id: a.id, reason: x.reason || undefined, seconds: Number(x.seconds) || undefined }) });
      if (v) toast(`${who} is banned from this channel.`);
      return !!v;
    };
    const show = (name) => { current = name; drawTabs(); panes[name](); };
    const drawTabs = () => {
      const c = conversation(id);
      const hub = hubOf(c);
      // An announcement channel shows who is in it to its managers only.
      const names = [['about', 'About'], c && c.announcement && !actsAsManager(c) ? null : ['members', `Members (${c ? c.member_count || 0 : 0})`], hub && hub.channel ? ['hub', 'On the hub'] : null, c && c.name ? ['webhooks', 'Webhooks'] : null, c && c.name ? ['references', 'References'] : null, c && actsAsManager(c) ? ['manage', 'Manage'] : null, c && actsAsManager(c) && c.kind === 'channel' ? ['bans', 'Bans'] : null].filter(Boolean);
      fill(tabs, names.map(([n, label]) => h('button', { type: 'button', role: 'tab', class: 'tab', 'aria-selected': String(n === current), onclick: () => show(n) }, label)));
    };
    const run = async (fn, said) => { try { const out = await fn(); if (said) toast(said); return out === undefined ? true : out; } catch (err) { if (!err.silent) toast(err.message, true); return false; } };
    const update = (change, said) => run(async () => {
      const out = await patch('/conversations/' + id, change);
      const old = store.conversations.get(id);
      store.conversations.set(id, { ...out.conversation, membership: out.conversation.membership || (old && old.membership) });
      store.emit('conversations');
    }, said || 'Saved.');

    const panes = {
      about: () => {
        const c = conversation(id);
        if (!c) return;
        const manager = actsAsManager(c);
        const section = store.sections.find((s) => s.id === c.section_id);
        const parent = c.parent_id && conversation(c.parent_id);
        if (!manager) {
          fill(body, h('h3', {}, title(c)), h('p', { class: 'dim' }, detailLine(c, look)), c.topic ? h('p', {}, c.topic) : null,
            h('dl', { class: 'profile-facts' },
              h('div', {}, h('dt', {}, 'Kind'), h('dd', {}, isAdHoc(c) ? 'A chat' : c.access === 'knock' ? STRINGS.kindKnock : c.private ? 'Private channel' : 'Public channel', c.locked_at ? ', locked' : '')),
              parent ? h('div', {}, h('dt', {}, 'Under'), h('dd', {}, title(parent))) : null,
              section ? h('div', {}, h('dt', {}, 'Section'), h('dd', {}, section.name)) : null,
              c.retention_seconds ? h('div', {}, h('dt', {}, 'Keeps messages'), h('dd', {}, duration(c.retention_seconds))) : null,
              c.slow_mode_seconds ? h('div', {}, h('dt', {}, 'Slow mode'), h('dd', {}, 'one post every ' + duration(c.slow_mode_seconds))) : null));
          return;
        }
        // Names need not be unique. When the name typed is already used by
        // channels the person can see, they are listed with their details,
        // and the person may carry on.
        const name = h('input', { value: c.name || '', maxLength: 64, placeholder: isAdHoc(c) ? 'Give this chat a name to make it a channel' : '' });
        const same = h('div', {});
        const check = () => {
          const typed = name.value.trim().toLowerCase();
          const twins = typed ? visibleChannels().filter((x) => x.id !== id && (x.name || '').toLowerCase() === typed) : [];
          fill(same, twins.length ? h('div', { class: 'note' }, h('p', {}, `${plural(twins.length, 'channel')} already ${twins.length === 1 ? 'has' : 'have'} this name. That is allowed.`),
            h('ul', {}, twins.map((x) => h('li', { class: 'small' }, detailLine(x, look) || 'a channel')))) : null);
        };
        name.addEventListener('input', check);
        const topic = h('input', { value: c.topic || '', maxLength: 250 });
        const welcome = h('textarea', { value: c.welcome || '', rows: 3, maxLength: 4000 });
        const sectionSel = h('select', {}, [['', 'None'], ...store.sections.map((s) => [s.id, s.name])].map(([v, t]) => h('option', { value: v, selected: v === (c.section_id || '') }, t)));
        const level = h('select', {}, [['member', 'Member: may post'], ['reader', 'Reader: may read and react']].map(([v, t]) => h('option', { value: v, selected: v === c.default_level }, t)));
        // The parent: a top-level named channel the person is in, or none.
        const tops = [...store.conversations.values()].filter((x) => x.kind === 'channel' && x.name && !x.parent_id && x.id !== id && (x.membership || isAdmin()));
        const parentSel = h('select', {}, [['', 'None: a top-level channel'], ...tops.map((x) => [x.id, x.name])].map(([v, t]) => h('option', { value: v, selected: v === (c.parent_id || '') }, t)));
        const save = h('button', { type: 'submit', class: 'btn primary' }, 'Save');
        fill(body, h('form', { class: 'stack', onsubmit: (e) => {
          e.preventDefault();
          const change = {};
          if (name.value.trim() !== (c.name || '')) change.name = name.value.trim();
          if (topic.value.trim() !== (c.topic || '')) change.topic = topic.value.trim();
          if (welcome.value !== (c.welcome || '')) change.welcome = welcome.value;
          if (level.value !== c.default_level) change.default_level = level.value;
          if (!isAdHoc(c)) {
            if (sectionSel.value !== (c.section_id || '') && sectionSel.value) change.section_id = sectionSel.value;
            if (parentSel.value !== (c.parent_id || '')) change.parent_id = parentSel.value || null;
          }
          if (!Object.keys(change).length) { toast('Nothing changed.'); return; }
          busy(save, () => update(change).then(() => panes.about()));
        } },
        h('label', { class: 'field' }, h('span', {}, 'Name'), name, h('small', {}, isAdHoc(c) ? 'Naming a chat makes it a named private channel.' : '1 to 64 characters of any script. Names need not be unique.')), same,
        h('label', { class: 'field' }, h('span', {}, 'Topic'), topic),
        h('label', { class: 'field' }, h('span', {}, 'Welcome text'), welcome, h('small', {}, 'Shown at the start of the channel. Churr Markdown.')),
        !isAdHoc(c) ? h('div', { class: 'cols-2' },
          h('label', { class: 'field' }, h('span', {}, 'Section'), sectionSel, h('small', {}, 'Ignored while the channel has a parent.')),
          h('label', { class: 'field' }, h('span', {}, 'Parent channel'), parentSel, h('small', {}, 'It changes where the channel is listed and nothing else: not who is in it, not whether it is private.'))) : null,
        h('label', { class: 'field' }, h('span', {}, 'People who join start as'), level),
        h('p', { class: 'row' }, save)));
        check();
      },

      members: async () => {
        const c = conversation(id);
        if (!c) return;
        const manager = actsAsManager(c);
        if (c.announcement && !manager) { fill(body, h('p', { class: 'dim' }, `${plural(c.member_count || 0, 'member')}. An announcement channel does not show who is in it.`)); return; }
        const mayAdd = !c.locked_at && (manager || (isAdHoc(c) && c.membership));
        fill(body, h('p', { class: 'loading' }, 'Loading'));
        let members, groups;
        try {
          [members, groups] = await Promise.all([all(`/conversations/${id}/members`, 'members'), get(`/conversations/${id}/groups`).then((r) => r.groups).catch(() => [])]);
        } catch (err) { fill(body, h('p', { class: 'problem' }, err.message)); return; }
        needAccounts(members.map((m) => m.account_id));
        const add = button('Add people', async () => {
          const chosen = await pickAccounts('Add to ' + title(c), { multiple: true, exclude: new Set(members.map((m) => m.account_id)), yes: 'Add',
            note: isAdHoc(c) ? 'Anyone added sees the whole history of this chat.' : 'Whoever is added sees the whole history.' });
          if (!chosen) return;
          if (await run(() => post(`/conversations/${id}/members`, { account_ids: chosen.map((a) => a.id) }), 'Added.')) panes.members();
        }, 'small primary');
        const addGroup = button('Add a group', async () => {
          const have = new Set(groups.map((g) => g.group_id));
          const v = await formDialog('Add a group to ' + title(c), [
            { name: 'group', label: 'Group', type: 'select', options: store.groups.filter((g) => !have.has(g.id)).map((g) => [g.id, `@${g.handle} · ${g.name}`]) },
            { name: 'level', label: 'At level', type: 'select', value: 'member', options: LEVELS },
          ], { yes: 'Add', intro: 'Everyone in the group is then in the channel at that level, and stays in step as the group changes.',
            submit: (x) => put(`/conversations/${id}/groups/${x.group}`, { level: x.level }) });
          if (v) panes.members();
        }, 'small');
        const rows = members.sort((a, b) => (LEVELS.findIndex((l) => l[0] === b.level) - LEVELS.findIndex((l) => l[0] === a.level))).map((m) => {
          const a = account(m.account_id) || { id: m.account_id, username: '…', display_name: '…' };
          const more = manager && m.account_id !== store.me.id ? h('button', { type: 'button', class: 'btn small', onclick: (e) => openMenu(e.currentTarget, [
            { head: 'Level' },
            ...LEVELS.map(([v, label]) => ({ label, checked: (m.direct_level || m.level) === v, onclick: async () => { if (await run(() => patch(`/conversations/${id}/members/${m.account_id}`, { level: v }))) panes.members(); } })),
            { sep: true },
            // In an announcement channel: who posts besides its managers.
            c.announcement && m.level !== 'reader' && m.level !== 'manager' ? { label: 'May post here', checked: !!m.may_post, onclick: async () => { if (await run(() => put(`/conversations/${id}/members/${m.account_id}/may-post`, { may_post: !m.may_post }), m.may_post ? 'They can no longer post here.' : 'They can post here now.')) panes.members(); } } : null,
            { label: 'Time out for an hour', onclick: () => run(() => post(`/conversations/${id}/timeouts`, { account_id: m.account_id, seconds: 3600 }), 'Timed out for an hour. They can still read.') },
            !c.shared ? { label: 'Ban from the channel…', danger: true, onclick: async () => { if (await banDialog(a)) panes.members(); } } : null,
            !m.via_group || m.direct_level ? { label: 'Remove from the channel', danger: true, onclick: async () => {
              if (await confirmDialog(`Remove ${a.display_name || a.username}?`, c.locked_at ? 'This channel is locked: once removed, they cannot come back.' : 'They can be added again.', { yes: 'Remove', danger: true })
                && await run(() => del(`/conversations/${id}/members/${m.account_id}`), 'Removed.')) panes.members();
            } } : null,
          ], { alignRight: true, label: 'Member' }) }, 'Change') : null;
          return h('li', {}, avatar(a), h('div', { class: 'what' }, h('strong', {}, a.display_name || a.username, m.account_id === store.me.id ? ' (you)' : ''),
            h('small', {}, '@' + a.username + (a.server_name ? '@' + a.server_name : '') + (m.via_group ? ' · here through a group' : ''))),
          isHubModerator(m.account_id, id) ? h('span', { class: 'tag lavender', title: 'A moderator of this channel, for every server in it' }, 'moderator') : null,
          c.announcement && m.may_post && m.level === 'member' ? h('span', { class: 'tag cyan', title: 'Allowed to post in this announcement channel' }, 'posts') : null,
          h('span', { class: 'tag ' + (m.level === 'manager' ? 'lavender' : 'line') }, m.level), more);
        });
        fill(body,
          c.locked_at ? h('p', { class: 'note warn' }, 'This channel is locked: nobody can be added or join, and a group attached to it brings in nobody new. People can still leave, and cannot come back.')
            : h('p', { class: 'row' }, mayAdd ? add : null, mayAdd && manager && !isAdHoc(c) ? addGroup : null),
          groups.length ? h('ul', { class: 'rows' }, groups.map((g) => {
            const grp = store.groups.find((x) => x.id === g.group_id);
            const rm = manager ? button('Take out', async () => { if (await run(() => del(`/conversations/${id}/groups/${g.group_id}`), 'The group was taken out.')) panes.members(); }, 'small danger') : null;
            return h('li', {}, h('div', { class: 'what' }, h('strong', {}, grp ? '@' + grp.handle : 'A group'), h('small', {}, `A group · ${grp ? plural(grp.member_count, 'member') : ''} · everyone in it is here at level ${g.level}`)), rm);
          })) : null,
          h('ul', { class: 'rows' }, rows));
      },

      hub: async () => hubPane(id, body, run),

      bans: async () => {
        const c = conversation(id);
        if (!c) return;
        fill(body, h('p', { class: 'loading' }, 'Loading'));
        let bans;
        try { bans = (await get(`/conversations/${id}/bans`)).bans; } catch (err) { fill(body, h('p', { class: 'problem' }, err.message)); return; }
        needAccounts(bans.flatMap((b) => [b.account_id, b.created_by_id]));
        const add = c.shared ? null : button('Ban someone…', async () => {
          const chosen = await pickAccounts('Ban from ' + title(c), {});
          if (chosen && chosen[0] && await banDialog(chosen[0])) panes.bans();
        }, 'small danger');
        const nameOf = (aid) => { const a = account(aid); return a ? a.display_name || a.username : 'someone'; };
        fill(body,
          h('p', { class: 'dim' }, c.shared ? 'This channel is shared with other servers, which cannot ban anyone yet. Bans made before it was shared still hold here.'
            : 'Someone banned is out of this channel and cannot join, be added or come back through a group until the ban is lifted or ends. Sub-channels have bans of their own.'),
          add ? h('p', { class: 'row' }, add) : null,
          bans.length ? h('ul', { class: 'rows' }, bans.map((b) => {
            const a = account(b.account_id) || { id: b.account_id, username: '…', display_name: '…' };
            const lift = button('Lift', async () => { if (await run(() => del(`/conversations/${id}/bans/${b.account_id}`), 'The ban is lifted. They can join or be added again.')) panes.bans(); }, 'small');
            return h('li', {}, avatar(a), h('div', { class: 'what' }, h('strong', {}, a.display_name || a.username),
              h('small', {}, `Banned by ${nameOf(b.created_by_id)} on ${dateTime(b.created_at)}${b.expires_at ? ' · until ' + dateTime(b.expires_at) : ''}${b.reason ? ' · “' + b.reason + '”' : ''}`)), lift);
          })) : h('p', { class: 'empty' }, 'Nobody is banned from this channel.'));
      },

      webhooks: async () => {
        const c = conversation(id);
        fill(body, h('p', { class: 'loading' }, 'Loading'));
        let hooks = [];
        try { hooks = (await get(`/conversations/${id}/webhooks`)).webhooks; } catch (err) { fill(body, h('p', { class: 'problem' }, err.message)); return; }
        needAccounts(hooks.map((w) => w.requested_by_id));
        const ask = button('Request a webhook', async () => {
          const v = await formDialog('Request a webhook', [
            { name: 'name', label: 'Name', required: true, maxLength: 60, placeholder: 'CI builds' },
            { name: 'purpose', label: 'What it is for', maxLength: 200 },
          ], { yes: 'Ask', intro: 'A webhook is an address that outside tools post to. It posts into this channel and can read nothing. An admin approves the request; you then collect the address, once.',
            submit: (x) => post(`/conversations/${id}/webhook-requests`, { name: x.name, purpose: x.purpose || undefined }) });
          if (v) { toast('Asked. You will see it here once it is approved.'); panes.webhooks(); }
        }, 'small primary');
        const address = async (w, path) => {
          const out = await run(() => post(`/webhooks/${w.id}/${path}`));
          if (out && out.url) { await secretDialog('Webhook address', [['Post to', out.url], ['Signing secret (for GitHub)', out.signing_secret]], 'Both are shown once. Whoever holds the address can post into this channel.'); panes.webhooks(); }
        };
        fill(body, h('p', { class: 'row' }, ask),
          hooks.length ? h('ul', { class: 'rows' }, hooks.map((w) => {
            const mine = w.requested_by_id === store.me.id;
            const a = account(w.requested_by_id);
            return h('li', {}, h('div', { class: 'what' }, h('strong', {}, w.name, ' ', h('span', { class: 'tag ' + (w.state === 'approved' ? 'green' : w.state === 'waiting' ? 'yellow' : 'grey') }, w.state)),
              h('small', {}, `Asked for by ${a ? a.display_name || a.username : 'someone'} on ${dateTime(w.requested_at)}${w.last_used_at ? ' · last used ' + dateTime(w.last_used_at) : ''}`)),
            h('div', { class: 'does' },
              w.state === 'approved' && mine && !w.has_secret ? button('Collect the address', () => address(w, 'address'), 'small primary') : null,
              w.state === 'approved' && w.has_secret && (mine || actsAsManager(c)) ? button('Reset the address', async () => { if (await confirmDialog('Reset this address?', 'The old address stops working at once.', { yes: 'Reset', danger: true })) address(w, 'reset'); }, 'small') : null,
              (w.state === 'approved' || w.state === 'waiting') && (mine || actsAsManager(c)) ? button('Revoke', async () => { if (await run(() => del('/webhooks/' + w.id), 'Revoked.')) panes.webhooks(); }, 'small danger') : null));
          })) : h('p', { class: 'dim' }, 'No webhooks here.'));
      },

      // The channel's reference patterns (ruling R73): its managers change
      // them, everyone may read what turns into a link here.
      references: async () => {
        const c = conversation(id);
        if (!c) return;
        fill(body, await patternEditor({ conversationId: id, canEdit: actsAsManager(c), redraw: () => panes.references() }));
      },

      manage: () => {
        const c = conversation(id);
        if (!c) return;
        const slow = h('select', { 'aria-label': 'Slow mode', onchange: () => update({ slow_mode_seconds: Number(slow.value) }) }, SLOW.map(([v, t]) => h('option', { value: v, selected: Number(v) === (c.slow_mode_seconds || 0) }, t)));
        const broad = h('select', { 'aria-label': 'Who may use @channel, @here and @everyone', onchange: () => update({ broad_mentions: broad.value }) },
          BROAD.map(([v, t]) => h('option', { value: v, selected: v === (c.broad_mentions || 'server') }, t)));
        // Announcement channel, replies in it, and lazy delivery (ruling
        // R66). Only a named channel that is not shared can be one.
        const sw = (label, on, change) => h('input', { type: 'checkbox', class: 'switch', 'aria-label': label, checked: on, onchange: async (e) => { await change(e.target.checked); panes.manage(); drawTabs(); } });
        const announce = sw('Announcement channel', !!c.announcement, (on) => update({ announcement: on }, on ? 'Only managers and the people they allow post here now.' : 'Everyone at level member posts here again.'));
        const replies = sw('Replies in threads', !!c.announcement_replies, (on) => update({ announcement_replies: on }));
        const lazy = h('select', { 'aria-label': 'Delivery to people who do not have the channel open', onchange: () => update({ lazy_delivery: lazy.value }) },
          LAZY.map(([v, t]) => h('option', { value: v, selected: v === (c.lazy_delivery || 'server') }, t)));
        const cur = String(c.retention_seconds || 0);
        const keep = h('select', { 'aria-label': 'Retention', onchange: () => update({ retention_seconds: Number(keep.value) }) },
          (KEEP.some((k) => k[0] === cur) ? KEEP : [...KEEP, [cur, duration(Number(cur))]]).map(([v, t]) => h('option', { value: v, selected: v === cur }, t)));
        const row = (name, hint, control) => h('div', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, name), h('small', { class: 'dim' }, hint)), h('div', { class: 'control' }, control));
        const archive = c.archived_at
          ? button('Unarchive', async () => { if (await run(async () => { const out = await del(`/conversations/${id}/archive`); store.conversations.set(id, { ...out.conversation, membership: c.membership }); store.emit('conversations'); }, 'Unarchived.')) panes.manage(); })
          : button('Archive', async () => {
            if (await confirmDialog('Archive ' + title(c) + '?', 'It can still be read and searched, and takes no more posts.', { yes: 'Archive' })
              && await run(async () => { const out = await post(`/conversations/${id}/archive`); store.conversations.set(id, { ...out.conversation, membership: c.membership }); store.emit('conversations'); }, 'Archived.')) panes.manage();
          });
        const makePrivate = button('Make private…', async () => {
          const yes = await confirmDialog('Make ' + title(c) + ' private?', [
            'People who are not members lose the channel at once: its history and its search results too.',
            'A private channel can never be made public again.'], { type: c.name, yes: 'Make it private', danger: true });
          if (yes && await update({ private: true }, 'The channel is private.')) panes.manage();
        }, 'danger');
        // Public and knock change into each other freely (ruling R79).
        const makeKnock = button(STRINGS.makeKnock + '…', async () => {
          const yes = !c.private ? await confirmDialog(STRINGS.makeKnock + '?', STRINGS.makeKnockNote, { yes: STRINGS.makeKnockYes }) : true;
          if (yes && await update({ access: 'knock' }, STRINGS.madeKnock)) panes.manage();
        });
        const makePublic = button(STRINGS.makePublic, async () => {
          if (await update({ access: 'public' }, STRINGS.madePublic)) panes.manage();
        });
        const lock = button('Lock…', async () => {
          const yes = await confirmDialog('Lock ' + title(c) + '?', [
            'After locking nobody can be added or join, and a group attached to the channel brings in nobody new.',
            'People can still leave, and someone who leaves cannot come back.', 'Locking cannot be undone.'], { type: 'lock', yes: 'Lock it', danger: true });
          if (yes && await run(async () => { const out = await post(`/conversations/${id}/lock`); store.conversations.set(id, { ...out.conversation, membership: c.membership }); store.emit('conversations'); }, 'The channel is locked.')) panes.manage();
        }, 'danger');
        fill(body,
          row('Slow mode', 'Each person may post once per interval. Managers and bots are exempt.', slow),
          row('@channel, @here and @everyone', 'Who may notify the whole channel. The server lets anyone, until a channel is larger than the size its admins set; then only managers. Anyone else’s is posted as plain text.', broad),
          c.name && !c.shared ? row('Announcement channel', 'Only managers and the people they allow post (Members, “May post here”). Everyone else reads and reacts. Who is in the channel is shown to managers only, and nobody is shown typing or online.', announce) : null,
          c.name && c.announcement ? row('Replies in threads', 'Let members who may not post reply in threads.', replies) : null,
          c.name ? row('Delivery when not open', 'A very large channel is sent in full only to people who have it open; the others see that it changed and get the rest when they open it. The server decides by size unless you say.', lazy) : null,
          row('Keep messages for', 'Messages older than this are removed for good. A manager may only shorten the server’s period.', keep),
          row(c.archived_at ? 'Archived' : 'Archive', 'An archived channel can be read and searched and takes no posts.', archive),
          !c.private && c.name && !c.parent_id && !c.shared ? row(STRINGS.makeKnock, STRINGS.makeKnockNote, makeKnock) : null,
          c.access === 'knock' ? row(STRINGS.makePublic, STRINGS.makePublicNote, makePublic) : null,
          (!c.private || c.access === 'knock') && c.name ? row('Make private', 'One-way. Non-members lose the channel at once.', makePrivate) : null,
          c.private && !c.locked_at ? row('Lock', 'One-way. Nobody can be added or join afterwards.', lock) : null,
          c.locked_at ? h('p', { class: 'note' }, `Locked on ${dateTime(c.locked_at)}.`) : null);
      },
    };
    if (!panes[current]) current = 'about';
    drawTabs();
    panes[current]();
    return { body: [tabs, body], foot: [button('Done', () => close(), 'primary')] };
  }, { wide: true });
}

// --- a channel a hub owns ---

// hubPane draws the hub's record of the channel (docs/LINK-PROTOCOL.md
// section 23a) and, for one of its moderators, the tools: invite a
// server, approve or decline one asking, remove one, rename and describe,
// set the listing mode, add and remove moderators, close.
async function hubPane(id, body, run) {
  const c = conversation(id);
  let hub = hubOf(c);
  if (!hub || !hub.channel) { fill(body, h('p', { class: 'loading' }, 'Loading')); hub = await loadHub(c); }
  if (!hub || !hub.channel) { fill(body, h('p', { class: 'dim' }, 'This channel is not on a hub.')); return; }
  // A moderator needs the record with who is invited and asking, which
  // the change events leave out.
  if (hub.moderator && !hub.full) {
    try { const out = await get(`/streams/${c.shared.stream_id}/channel`); hub = { channel: out.channel, moderator: out.moderator, full: true }; store.hubChannels.set(id, hub); } catch { /* the record as last seen */ }
  }
  const ch = hub.channel;
  const mod = hub.moderator && !ch.closed;
  const act = async (action, extra, said) => {
    const out = await run(() => post(`/streams/${c.shared.stream_id}/channel/actions`, { action, ...extra }), said);
    if (out && out.channel) {
      store.hubChannels.set(id, { channel: out.channel, moderator: isModerator(out.channel, store.me, store.server.name), full: true });
      store.emit('hub', id);
      store.emit('messages', id);
    }
  };
  const fact = (k, v) => h('div', {}, h('dt', {}, k), h('dd', {}, v));
  const serverRow = (name, tags, does) => h('li', {}, h('div', { class: 'what' }, h('strong', {}, name, ' ', tags)), does && h('div', { class: 'does' }, does));
  const own = store.server.name;
  fill(body,
    h('p', { class: 'note' }, `${ch.hub} owns this channel. Its moderators act for every server in it; managers here act for this server only. The hub sees its name, description, moderators and servers, never its messages.`),
    ch.closed ? h('p', { class: 'note warn' }, 'A moderator closed this channel for every server.') : null,
    h('dl', { class: 'profile-facts' },
      fact('Hub', ch.hub),
      fact('Name on the hub', ch.name),
      ch.description ? fact('Description', ch.description) : null,
      fact('Listing', `${listingName(ch.listing)}. ${(LISTING[ch.listing] || {}).says || ''}`),
      fact('Key made by', ch.key_maker || '–'),
      fact('Record', `version ${ch.version}, signed by the hub ${dateTime(ch.updated_at)}`)),
    h('h3', { class: 'label' }, `Servers (${ch.servers.length})`),
    h('ul', { class: 'rows' }, ch.servers.map((name, i) => serverRow(name, [name === own ? h('span', { class: 'tag line' }, 'this server') : null, name === own && i === 0 ? ' ' : null, i === 0 ? h('span', { class: 'tag line' }, 'longest standing') : null],
      mod ? button('Remove', async () => {
        if (await confirmDialog(`Remove ${name} from the channel?`, [name === own ? 'That is this server: it leaves the channel and keeps its copy, archived.' : 'It keeps its copy, archived, and gets nothing more. Its moderators stop being moderators.', 'A new key is made for the servers that stay.'], { yes: 'Remove', danger: true })) act('remove_server', { server: name }, 'Removed.');
      }, 'small') : null))),
    mod && ch.requests.length ? [h('h3', { class: 'label' }, 'Asking to join'), h('ul', { class: 'rows' }, ch.requests.map((name) => serverRow(name, h('span', { class: 'tag yellow' }, 'asking'), [
      button('Approve', () => act('approve', { server: name }, `${name} is in.`), 'small primary'),
      button('Decline', () => act('decline', { server: name }, 'Declined.'), 'small'),
    ])))] : null,
    mod && ch.invited.length ? [h('h3', { class: 'label' }, 'Invited'), h('ul', { class: 'rows' }, ch.invited.map((name) => serverRow(name, h('span', { class: 'tag line' }, 'invited'),
      button('Withdraw', () => act('remove_server', { server: name }, 'Invitation withdrawn.'), 'small'))))] : null,
    mod ? h('p', { class: 'row' }, button('Invite a server', async () => {
      const v = await formDialog('Invite a server', [{ name: 'server', label: 'Its name', required: true, code: true, maxLength: 253, placeholder: 'chat.example.org', hint: 'A server on the same hub. It joins when its owner accepts.' }], { yes: 'Invite' });
      if (v) act('invite', { server: v.server.trim().toLowerCase() }, 'Invited.');
    }, 'small')) : null,
    h('h3', { class: 'label' }, `Moderators (${ch.moderators.length})`),
    h('ul', { class: 'rows' }, ch.moderators.map((m) => serverRow(moderatorName(m), m.server === own && m.account_id === store.me.id ? h('span', { class: 'tag lavender' }, 'you') : null,
      mod && canRemoveModerator(ch) ? button('Remove', async () => {
        if (await confirmDialog(`Stop ${moderatorName(m)} being a moderator?`, 'They keep whatever level they have on their own server.', { yes: 'Remove', danger: true })) act('remove_moderator', { account_id: m.account_id }, 'Removed.');
      }, 'small') : null))),
    mod ? h('p', { class: 'row' }, button('Add a moderator', async () => {
      const chosen = await pickAccounts('Add a moderator', { yes: 'Add', note: 'An account on a server in this channel. A moderator acts for every server in it.' });
      if (chosen && chosen[0]) act('add_moderator', { account_id: chosen[0].id }, 'Added.');
    }, 'small')) : null,
    mod ? [h('h3', { class: 'label' }, 'Moderator tools'),
      h('p', { class: 'row' },
        button('Rename and describe', async () => {
          const v = await formDialog('Name and description on the hub', [
            { name: 'name', label: 'Name', required: true, value: ch.name, maxLength: 64, hint: 'Each server keeps its own local name; this is the one in the hub’s directory.' },
            { name: 'description', label: 'Description', type: 'textarea', value: ch.description, maxLength: 1000, rows: 3 },
          ], { yes: 'Save' });
          if (v) act('describe', { name: v.name, description: v.description }, 'Saved.');
        }, 'small'),
        button('Listing', async () => {
          const v = await formDialog('Listing on the hub', [{ name: 'listing', label: 'Listing', type: 'radio', value: ch.listing, options: LISTINGS.map((x) => [x, LISTING[x].name, LISTING[x].says]) }], { yes: 'Save' });
          if (v && v.listing !== ch.listing) act('set_listing', { listing: v.listing }, 'Saved.');
        }, 'small'),
        button('Close the channel…', async () => {
          if (await confirmDialog('Close this channel for every server?', ['Every server keeps its copy, archived, and nothing more is said in it.', 'This cannot be undone.'], { type: 'close', yes: 'Close it', danger: true })) act('close', {}, 'Closed.');
        }, 'small danger'))] : null);
}
