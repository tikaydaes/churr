// Colour themes in the pages (ruling R61), driven by two people against
// the real server: the editor with its live preview and measured
// pairings, a refusal in the server's own words, "fix", saving,
// publishing, using a theme before first paint, export and import of a
// .churrtheme file, the library (search, try, add), theme cards for a link
// and for an attached file, the page at /themes/{id}, and the admin's
// defaults and removal from the library.

import { step, shot, assert, noSidewaysScroll, PHONE, DESKTOP } from './lib.mjs';
import { fileURLToPath } from 'node:url';
import { writeFileSync, readFileSync } from 'node:fs';

const editor = (page) => page.locator('#main .editor');
const message = (page, text) => page.locator('#main .msg', { hasText: text }).last();
const resetLook = (page) => page.evaluate(() => { localStorage.removeItem('churr-look'); window.churrLook.tryTokens(null); window.churrLook.apply(); });

export async function themesFlow(kaity, sam, origin) {
  const a = kaity.page, b = sam.page;
  let themeId = null;

  await step('themes: the editor draws a conversation with the theme and measures every pairing', async () => {
    await a.goto(origin + '/chat/themes');
    await a.getByRole('heading', { name: 'Themes', level: 1 }).waitFor();
    await a.locator('.theme-preview .msg').first().waitFor();
    // a dark theme measures 31 pairings, a light one 28 (no focus ring)
    const pairs = await a.locator('.te-pair').count();
    assert(pairs === 31 || pairs === 28, `${pairs} pairings drawn`);
    assert(await a.locator('.te-pair.bad').count() === 0, 'the built-in copy fails a pairing');
    // the preview carries the theme as its own tokens
    const bg = await a.locator('.theme-preview').evaluate((el) => el.style.getPropertyValue('--bg'));
    assert(/^#[0-9a-f]{6}$/.test(bg), `the preview has no --bg of its own (${bg})`);
    for (const what of ['.side-row.current', '.side-row.unread .badge', '.side-row.quiet .row-quiet', '.side-row.hidden-row .tag', '.form.preset-poll', '.msg-thread', '.new-line', '.composer .editor.empty', '.btn.danger', '.tag.yellow']) {
      assert(await a.locator('.theme-preview ' + what).count() > 0, `the preview lacks ${what}`);
    }
    // The view scrolls inside the window, so the window is made tall
    // enough to show the whole editor.
    await a.setViewportSize({ width: 1440, height: 2000 });
    try { await shot(a, '50-themes-editor', { fullPage: false }); } finally { await a.setViewportSize(DESKTOP); }
  }, a);

  await step('themes: the server refuses a failing theme in its own words, and "fix" makes the smallest change', async () => {
    await a.getByLabel('Name of the theme').fill('Night Market');
    await a.getByLabel('Kind').selectOption('dark');
    const dim = a.getByLabel('Secondary text as hex');
    await dim.fill('#6a6890');
    await a.locator('.te-pair.bad').first().waitFor();
    assert(await a.locator('.te-pair.bad').filter({ hasText: 'dim on bg' }).locator('.te-ratio').innerText() === '3.57:1', 'the editor does not measure dim on bg as 3.57:1');
    await a.getByRole('button', { name: 'Save as a new theme' }).click();
    await a.getByText('The server refused it: dim on bg is 3.57:1; text needs at least 4.5:1.').waitFor();
    await shot(a, '50-themes-editor-refused', { fullPage: false });
    await a.getByRole('button', { name: 'Fix Secondary text' }).click();
    await a.getByText(/Secondary text changed from #6a6890 to #[0-9a-f]{6}/).waitFor();
    assert(await a.locator('.te-pair.bad').count() === 0, 'a pairing still fails after the fix');
    // a cyan of our own, typed without the hash
    await a.getByLabel('Cyan as hex').fill('4fe3c1');
    await a.getByRole('button', { name: 'Save as a new theme' }).click();
    await a.locator('.toast', { hasText: 'Saved.' }).waitFor();
    await a.locator('.te-pick.current', { hasText: 'Night Market' }).waitFor();
    const mine = await a.evaluate(async () => (await (await fetch('/api/v1/me/themes')).json()).themes);
    const t = mine.find((x) => x.name === 'Night Market');
    assert(t && t.colours.cyan === '#4fe3c1', 'the theme was not saved as edited');
    themeId = t.id;
  }, a);

  await step('themes: publish, and use it in this browser from before the first paint', async () => {
    await a.getByRole('button', { name: 'Publish to the library' }).click();
    await a.getByRole('button', { name: 'Unpublish' }).waitFor();
    await a.getByRole('button', { name: 'Use it in this browser' }).click();
    const on = await a.evaluate(() => ({ attr: document.documentElement.dataset.customTheme, cyan: getComputedStyle(document.documentElement).getPropertyValue('--cyan').trim() }));
    assert(on.attr === 'dark' && on.cyan === '#4fe3c1', `the theme was not applied: ${JSON.stringify(on)}`);
    // Loaded afresh, it is on <html> by the time the page is parsed: look.js
    // reads it from this browser, with no request.
    await a.goto(origin + '/account/appearance');
    const early = await a.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--cyan').trim());
    assert(early === '#4fe3c1', `the theme was not applied on load (${early})`);
    await a.getByRole('group', { name: 'Colours for dark' }).getByRole('button', { name: 'Night Market' }).waitFor();
    await shot(a, '26-account-appearance-themes');
    // following the system: light uses the built-in light, not this one
    await a.emulateMedia({ colorScheme: 'light' });
    const light = await a.evaluate(() => ({ attr: document.documentElement.dataset.customTheme || '', bg: getComputedStyle(document.documentElement).getPropertyValue('--bg').trim() }));
    assert(light.attr === '' && light.bg === '#f5f2ff', `in light the dark theme stayed: ${JSON.stringify(light)}`);
    await a.emulateMedia({ colorScheme: 'dark' });
    await resetLook(a);
  }, a);

  await step('themes: export to a .churrtheme file and import it again', async () => {
    await a.goto(origin + '/chat/themes');
    await a.locator('.te-pick', { hasText: 'Night Market' }).click();
    const [dl] = await Promise.all([a.waitForEvent('download'), a.getByRole('button', { name: 'Export a .churrtheme file' }).click()]);
    assert(dl.suggestedFilename() === 'night-market.churrtheme', dl.suggestedFilename());
    const file = fileURLToPath(new URL('./shots/night-market.churrtheme', import.meta.url));
    await dl.saveAs(file);
    const json = JSON.parse(readFileSync(file, 'utf8'));
    assert(json.name === 'Night Market' && json.kind === 'dark' && Object.keys(json.colours).length === 9, 'the file is not a theme');
    json.name = 'Night Market, imported';
    const again = fileURLToPath(new URL('./shots/imported.churrtheme', import.meta.url));
    writeFileSync(again, JSON.stringify(json));
    await a.locator('.theme-editor input[type=file]').setInputFiles(again);
    await a.getByText('Imported “Night Market, imported”. Save it to keep it.').waitFor();
    assert(await a.getByLabel('Name of the theme').inputValue() === 'Night Market, imported', 'the import did not open');
  }, a);

  await step('themes: the library is searched, a theme tried without keeping it, and added', async () => {
    await b.goto(origin + '/chat/themes/library');
    await b.getByRole('heading', { name: 'Theme library', level: 1 }).waitFor();
    const card = b.locator('.theme-card', { hasText: 'Night Market' });
    await card.waitFor();
    await card.getByText(/by Kaity/).waitFor();
    await b.getByRole('searchbox', { name: 'Search the theme library' }).fill('zzz');
    await b.getByText('No published theme has that in its name.').waitFor();
    await b.getByRole('searchbox', { name: 'Search the theme library' }).fill('night');
    await card.waitFor();
    await shot(b, '51-themes-library', { phone: true });
    await card.getByRole('button', { name: 'Try' }).click();
    await b.locator('.trying-bar', { hasText: 'Trying Night Market' }).waitFor();
    assert(await b.evaluate(() => document.documentElement.dataset.customTheme === 'dark' && !localStorage.getItem('churr-look')), 'trying kept the theme, or did not show it');
    await shot(b, '51-themes-library-trying', { fullPage: false });
    await b.locator('.trying-bar').getByRole('button', { name: 'Stop trying' }).click();
    assert(await b.evaluate(() => !document.documentElement.dataset.customTheme), 'stopping did not put the page back');
    await card.getByRole('button', { name: 'Add', exact: true }).click();
    await card.getByRole('button', { name: 'Added' }).waitFor();
    await noSidewaysScroll(b);
  }, b);

  await step('themes: a theme link in a message is drawn as a theme card', async () => {
    await a.goto(origin + '/chat/');
    await a.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
    await editor(a).click();
    await a.keyboard.type(`Try my theme ${origin}/themes/${themeId}`);
    await a.keyboard.press('Enter');
    await b.goto(origin + '/chat/');
    await b.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
    const card = message(b, 'Try my theme').locator('.theme-card');
    await card.waitFor({ timeout: 15000 });
    await card.getByText('Night Market').waitFor();
    await card.getByRole('button', { name: 'Try' }).waitFor();
    await card.getByRole('button', { name: 'Add' }).waitFor();
    await shot(b, '52-theme-card', { fullPage: false });
  }, b);

  await step('themes: an attached .churrtheme file is drawn as a theme card', async () => {
    const file = fileURLToPath(new URL('./shots/dawn.churrtheme', import.meta.url));
    writeFileSync(file, JSON.stringify({ name: 'Dawn', kind: 'light', colours: { bg: '#fff8f0', panel: '#ffffff', raise: '#f3e9dc', ink: '#2a1a12', dim: '#5e4a3e', lav: '#5b3fd6', cyan: '#006e66', pink: '#b3127f', yel: '#ffd95e' } }));
    await a.locator('#main .composer input[type=file]').setInputFiles(file);
    await editor(a).click();
    await a.keyboard.type('a light one');
    await a.keyboard.press('Enter');
    const card = message(b, 'a light one').locator('.theme-card');
    await card.waitFor({ timeout: 15000 });
    await card.getByText('From the file dawn.churrtheme').waitFor();
    await card.getByRole('button', { name: 'Add' }).click();
    await b.locator('.toast', { hasText: '“Dawn” is in your themes.' }).waitFor();
    await shot(b, '52-theme-card-file', { fullPage: false });
  }, b);

  await step('themes: the page at /themes/{id} shows the theme with try and add', async () => {
    await b.goto(origin + '/themes/' + themeId);
    await b.getByRole('heading', { name: /A colour theme/ }).waitFor();
    await b.locator('.theme-card', { hasText: 'Night Market' }).waitFor();
    await b.locator('.theme-sample').waitFor();
    await shot(b, '53-theme-page', { phone: true });
    await b.getByRole('button', { name: 'Try' }).click();
    await b.locator('.trying-bar').waitFor();
    await b.locator('.trying-bar').getByRole('button', { name: 'Stop trying' }).click();
    const missing = await b.goto(origin + '/themes/0193a000-0000-7000-8000-00000000dead');
    assert(missing.status() === 200, 'the page itself is not served');
    await b.getByText(/There is no theme at this address/).waitFor();
  }, b);

  await step('themes: an admin sets the server’s default dark theme, everyone follows, and clears it', async () => {
    await a.goto(origin + '/admin/themes');
    await a.getByRole('heading', { name: 'Themes', level: 1 }).waitFor();
    await b.goto(origin + '/chat/');
    await b.locator('.conn.live').waitFor();
    await a.locator('.setting', { hasText: 'Default dark theme' }).getByRole('button', { name: 'Choose' }).click();
    const select = a.getByRole('dialog').locator('select[name="theme_id"]');
    const value = await select.evaluate((el) => [...el.options].find((o) => o.textContent.startsWith('Night Market (library)')).value);
    await select.selectOption(value);
    await a.getByRole('button', { name: 'Make it the default' }).click();
    await a.locator('.setting', { hasText: 'Default dark theme' }).locator('.theme-card', { hasText: 'Night Market' }).waitFor();
    await shot(a, '36-admin-themes');
    // sam has chosen nothing, so the server's default reaches the page live
    await b.waitForFunction(() => document.documentElement.dataset.customTheme === 'dark', null, { timeout: 10000 });
    await a.locator('.setting', { hasText: 'Default dark theme' }).getByRole('button', { name: 'Clear' }).click();
    await b.waitForFunction(() => !document.documentElement.dataset.customTheme, null, { timeout: 10000 });
  }, a);

  await step('themes: an admin removes a theme from the library; its owner keeps it', async () => {
    await a.locator('.library .theme-card', { hasText: 'Night Market' }).getByRole('button', { name: 'Remove from the library' }).click();
    await a.getByRole('dialog').getByRole('button', { name: 'Remove' }).click();
    await a.locator('.toast', { hasText: 'Removed from the library.' }).waitFor();
    const lib = await b.evaluate(async () => (await (await fetch('/api/v1/themes')).json()).themes.map((t) => t.name));
    assert(!lib.includes('Night Market'), 'still in the library');
    const mine = await a.evaluate(async () => (await (await fetch('/api/v1/me/themes')).json()).themes.map((t) => t.name));
    assert(mine.includes('Night Market'), 'the owner lost it');
  }, a);

  await step('themes: phone width', async () => {
    await a.setViewportSize(PHONE);
    try {
      await a.goto(origin + '/chat/themes');
      await a.locator('.te-pair').first().waitFor();
      await noSidewaysScroll(a);
      await shot(a, '50-themes-editor-phone', { fullPage: false });
    } finally { await a.setViewportSize(DESKTOP); }
  }, a);
}
