// Themes (admins; ruling R61): the server's default dark and light themes,
// which members who have not chosen their own get, and the theme library,
// from which an admin may remove a published theme (its owner keeps it).

import { get, put, del, all } from '/shared/api.js';
import { h, button, toast, confirmDialog, busy } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { themeCard } from '/shared/themecard.js';
import { rememberServerDefaults } from '/shared/themes.js';
import { people, name } from '../kit.js';

export default async function themesSection(ctx) {
  const [libGot, mineGot, server] = await Promise.all([
    tryLoad(() => all('/themes', 'themes', {}, 400)),
    tryLoad(() => get('/me/themes')),
    get('/server').then((r) => r.server).catch(() => ctx.server),
  ]);
  const library = libGot.ok || [];
  const mine = mineGot.ok ? mineGot.ok.themes : [];
  const defaults = server.default_themes || {};
  await people([...library.map((t) => t.owner_id), defaults.dark && defaults.dark.author_id, defaults.light && defaults.light.author_id]);

  const setDefault = async (kind) => {
    const seen = new Set();
    const choices = [...library, ...mine].filter((t) => t.kind === kind && !seen.has(t.id) && seen.add(t.id));
    if (!choices.length) { toast(`There is no ${kind} theme to choose: publish one to the library, or save one of your own.`, true); return; }
    const out = await formDialog(`The server’s default ${kind} theme`, [
      { name: 'theme_id', label: 'Theme', type: 'select', options: choices.map((t) => [t.id, `${t.name}${t.published ? ' (library)' : ' (yours)'} · by ${t.owner_id === ctx.me.id ? 'you' : name(t.owner_id)}`]) },
    ], { yes: 'Make it the default', intro: `Members who have not chosen a ${kind} theme see this one. The default is a copy: later changes to the theme, or its deletion, do not change it.`,
      submit: (v) => put('/admin/theme-defaults', { kind, theme_id: v.theme_id }) });
    if (out) { rememberServerDefaults(out.default_themes); toast('Saved. Everyone’s pages follow at once.'); ctx.redraw(); }
  };
  const clearDefault = async (kind, btn) => busy(btn, async () => {
    const out = await put('/admin/theme-defaults', { kind });
    rememberServerDefaults(out.default_themes);
    toast(`The default ${kind} theme is the built-in one again.`);
    ctx.redraw();
  });

  const defaultRow = (kind) => {
    const d = defaults[kind];
    const clear = d ? button('Clear', () => clearDefault(kind, clear), 'small') : null;
    return h('div', { class: 'setting' },
      h('div', { class: 'what' }, h('strong', {}, kind === 'dark' ? 'Default dark theme' : 'Default light theme'),
        d ? themeCard(d, { author: d.author_id ? name(d.author_id) : null }) : h('small', {}, 'None set: the built-in ' + kind + ' theme.')),
      h('div', { class: 'control' }, button(d ? 'Change' : 'Choose', () => setDefault(kind), 'small primary'), clear));
  };

  const remove = (t) => async () => {
    if (!(await confirmDialog(`Remove “${t.name}” from the library?`, [`It is unpublished. ${name(t.owner_id)} keeps it, and so does everyone who added a copy.`, 'This is written to the audit log.'], { yes: 'Remove', danger: true }))) return;
    try { await del('/admin/themes/' + t.id); toast('Removed from the library.'); ctx.redraw(); } catch (err) { if (!err.silent) toast(err.message, true); }
  };

  return [
    card('Server defaults',
      h('p', { class: 'dim' }, 'Pages show these to members who have not picked their own theme, and before sign-in. Anyone can still choose another in their settings.'),
      defaultRow('dark'), defaultRow('light')),
    card('Theme library',
      h('p', { class: 'dim' }, 'Themes members have published. Removing one unpublishes it; its owner keeps it.'),
      libGot.problem || (library.length ? h('div', { class: 'library' }, library.map((t) => themeCard(t, {
        author: t.owner_id === ctx.me.id ? 'you' : name(t.owner_id), link: '/themes/' + t.id,
        extra: button('Remove from the library', remove(t), 'small danger'),
      }))) : h('p', { class: 'empty' }, 'Nobody has published a theme yet.'))),
  ];
}
