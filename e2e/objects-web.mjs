// Objects in the browser (ruling R73): what the composer does with an
// exact token as it is typed, and how the message is drawn for the other
// reader. Reference patterns: an admin defines one, the composer offers a
// match with "Plain text" after it, space takes the chosen row, Escape
// keeps plain text, Backspace straight after accepting undoes it, and
// the arrows wrap.

import { step, assert, consoleErrors, DESKTOP } from './lib.mjs';

const message = (page, text) => page.locator('#main .msg').filter({ hasText: text }).last();

async function reader(browser, who, origin) {
  const context = await browser.newContext({ viewport: DESKTOP, timezoneId: 'Australia/Brisbane', locale: 'en-AU', colorScheme: 'dark', storageState: await who.context.storageState() });
  context.setDefaultTimeout(8000);
  context.setDefaultNavigationTimeout(25000);
  const page = await context.newPage();
  page.on('console', (m) => { if (m.type() === 'error' && !/status of (401|403|429|503)/.test(m.text())) consoleErrors.push(`[${who.name} objects] ${m.text()}`); });
  page.on('pageerror', (e) => consoleErrors.push(`[${who.name} objects] PAGEERROR ${e.message}`));
  await page.goto(origin + '/chat/');
  await page.getByRole('button', { name: 'Jump to…' }).click();
  await page.getByRole('searchbox', { name: 'Jump to' }).fill('general');
  await page.getByRole('dialog').getByRole('button', { name: /general/ }).first().click();
  await page.locator('#main .editor').waitFor();
  return { context, page };
}

export async function objectsFlow(kaity, sam, browser, origin) {
  let a, b;
  const ok = await step('objects: an admin defines a reference pattern', async () => {
    a = await reader(browser, kaity, origin);
    b = await reader(browser, sam, origin);
    const status = await a.page.evaluate(async () => (await fetch('/api/v1/reference-patterns', { method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'Tracker', prefix: 'PROJ-', url_template: 'https://tracker.example/browse/PROJ-{value}' }) })).status);
    assert(status === 201, 'creating the pattern answered ' + status);
    // The composer hears of it through reference_patterns.changed.
    await a.page.waitForTimeout(400);
  }, kaity.page);
  if (!ok) return;
  const pa = a.page, pb = b.page;
  const editor = pa.locator('#main .editor');
  const offer = (typed) => pa.getByRole('listbox', { name: `What is “${typed}”?` });
  const ref = (text) => message(pb, text).locator('a.ref');

  await step('objects: a reference offered, taken with space and drawn as a link', async () => {
    await editor.click();
    await pa.keyboard.type('Fixed PROJ-12');
    const list = offer('PROJ-12');
    await list.waitFor();
    const rows = (await list.getByRole('option').allTextContents()).map((x) => x.trim());
    assert(rows.length === 2 && rows[0] === 'Reference PROJ-12, opens tracker.example' && rows[1] === 'Plain text: “PROJ-12”', 'offered: ' + rows.join(' | '));
    await pa.keyboard.press(' ');
    await list.waitFor({ state: 'detached' });
    await pa.keyboard.type('today');
    await pa.keyboard.press('Enter');
    await ref('Fixed PROJ-12 today').waitFor();
    assert(await ref('Fixed PROJ-12 today').getAttribute('href') === 'https://tracker.example/browse/PROJ-12', 'the address');
    assert(await ref('Fixed PROJ-12 today').textContent() === 'PROJ-12', 'the typed text is shown');
  }, pa);

  await step('objects: Escape keeps a reference as plain text', async () => {
    await editor.click();
    await pa.keyboard.type('Not PROJ-13');
    await offer('PROJ-13').waitFor();
    await pa.keyboard.press('Escape');
    await offer('PROJ-13').waitFor({ state: 'detached' });
    await pa.keyboard.type(' at all');
    await pa.keyboard.press('Enter');
    await message(pb, 'Not PROJ-13 at all').waitFor();
    assert(await ref('Not PROJ-13 at all').count() === 0, 'Escape still made a reference');
  }, pa);

  await step('objects: Backspace straight after accepting undoes it', async () => {
    await editor.click();
    await pa.keyboard.type('Undo PROJ-14');
    await offer('PROJ-14').waitFor();
    await pa.keyboard.press(' ');
    await pa.keyboard.press('Backspace');
    await pa.keyboard.type('please');
    await pa.keyboard.press('Enter');
    await message(pb, 'Undo PROJ-14 please').waitFor();
    assert(await ref('Undo PROJ-14 please').count() === 0, 'Backspace did not undo the reference');
  }, pa);

  await step('objects: the arrows wrap to Plain text', async () => {
    await editor.click();
    await pa.keyboard.type('Wrap PROJ-15');
    const list = offer('PROJ-15');
    await list.waitFor();
    await pa.keyboard.press('ArrowUp');
    assert((await list.getByRole('option', { selected: true }).textContent()).trim() === 'Plain text: “PROJ-15”', 'up from the first row did not wrap');
    await pa.keyboard.press('ArrowDown');
    assert(/^Reference/.test((await list.getByRole('option', { selected: true }).textContent()).trim()), 'down from the last row did not wrap');
    await pa.keyboard.press('ArrowDown');
    await pa.keyboard.press(' ');
    await pa.keyboard.type('done');
    await pa.keyboard.press('Enter');
    await message(pb, 'Wrap PROJ-15 done').waitFor();
    assert(await ref('Wrap PROJ-15 done').count() === 0, 'Plain text chosen with the arrows still made a reference');
  }, pa);

  await step('objects: a message link drawn as a preview of the message', async () => {
    const id = await message(pb, 'Wrap PROJ-15 done').getAttribute('data-id');
    const conv = new URL(pa.url()).pathname.split('/')[3];
    const link = origin + '/chat/c/' + conv + '/m/' + id;
    await editor.click();
    await pa.keyboard.type('Linked ' + link);
    await offer(link).waitFor();
    await pa.keyboard.press(' ');
    await pa.keyboard.type('here');
    await pa.keyboard.press('Enter');
    const view = message(pb, 'Linked').locator('a.msg-link');
    await view.waitFor();
    await view.filter({ hasText: 'Wrap PROJ-15 done' }).waitFor();
    assert((await view.getAttribute('aria-label')).startsWith('Link to a message (' + link + ')'), 'the label holds the link as typed');
  }, pa);

  await step('objects: a measurement shows the reader\'s equivalent', async () => {
    await editor.click();
    await pa.keyboard.type('Ran 3 mi');
    const list = offer('3 mi');
    await list.waitFor();
    assert(/^Measurement: 3 mi/.test((await list.getByRole('option').first().textContent()).trim()), 'the measurement row');
    await pa.keyboard.press(' ');
    await pa.keyboard.type('today');
    await pa.keyboard.press('Enter');
    // The reader's browser is in Australia: metric.
    const alt = message(pb, 'Ran 3 mi').locator('.measure-alt');
    await alt.waitFor();
    assert((await alt.textContent()) === ' (4.83 km)', 'the equivalent: ' + await alt.textContent());
  }, pa);

  await a.context.close();
  await b.context.close();
}
