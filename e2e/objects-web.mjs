// Objects in the browser (ruling R73): what the composer does with an
// exact token as it is typed, and how the message is drawn for the other
// reader. Reference patterns: an admin defines one, the composer offers a
// match with "Plain text" after it, space takes the chosen row, Escape
// keeps plain text, Backspace straight after accepting undoes it, and
// the arrows wrap.

import { step, shot, assert, consoleErrors, countReels, reels, DESKTOP } from './lib.mjs';

const message = (page, text) => page.locator('#main .msg').filter({ hasText: text }).last();

async function reader(browser, who, origin) {
  const context = await browser.newContext({ viewport: DESKTOP, timezoneId: 'Australia/Brisbane', locale: 'en-AU', colorScheme: 'dark', storageState: await who.context.storageState() });
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin });
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

  await step('objects: a measurement opens the unit converter', async () => {
    await editor.click();
    await pa.keyboard.type('Walked 5 km');
    await offer('5 km').waitFor();
    await pa.keyboard.press(' ');
    await pa.keyboard.type('today');
    await pa.keyboard.press('Enter');
    const control = message(pb, 'Walked 5 km').locator('button.measure');
    await control.waitFor();
    assert((await control.getAttribute('aria-label')) === 'Measurement 5 km. Opens the unit converter', 'label: ' + await control.getAttribute('aria-label'));
    await control.focus();
    await pb.keyboard.press('Enter');
    const dlg = pb.getByRole('dialog', { name: 'Unit converter' });
    await dlg.waitFor();
    const rows = (await dlg.locator('.measure-row .measure-value').allTextContents());
    assert(rows[0] === '5,000,000 mm' && rows.includes('3.107 mi') && rows.length === 8, 'rows: ' + rows.join(' | '));
    assert(/Typed as/.test(await dlg.locator('.instant-typed').textContent()), 'typed as');
    assert(!/undefined|null/.test(await dlg.textContent()), 'nothing undefined');
    await shot(pb, 'measure-popup', { fullPage: false, phone: true });
    await dlg.getByRole('button', { name: '3.107 mi, miles. Copy' }).click();
    await pb.locator('.toast').filter({ hasText: 'Copied 3.107 mi' }).first().waitFor();
    assert(await pb.evaluate(() => navigator.clipboard.readText()) === '3.107 mi', 'copied');
    await pb.keyboard.press('Escape');
    await dlg.waitFor({ state: 'detached' });
    assert(await control.evaluate((el) => el === document.activeElement), 'the keyboard is back on the measurement');
    // Space opens it too.
    await pb.keyboard.press(' ');
    await dlg.waitFor();
    await pb.keyboard.press('Escape');
    await dlg.waitFor({ state: 'detached' });
  }, pb);

  await step('objects: a number in another base opens the number converter', async () => {
    await editor.click();
    await pa.keyboard.type('mask 0x4f22');
    const list = offer('0x4f22');
    await list.waitFor();
    assert(/^Number 0x4f22: 20,258 in decimal/.test((await list.getByRole('option').first().textContent()).trim()), 'the number row');
    await pa.keyboard.press(' ');
    await pa.keyboard.type('set');
    await pa.keyboard.press('Enter');
    const control = message(pb, 'mask 0x4f22').locator('button.numeric');
    await control.waitFor();
    await control.focus();
    await pb.keyboard.press('Enter');
    const dlg = pb.getByRole('dialog', { name: 'Number converter' });
    await dlg.waitFor();
    const rows = await dlg.locator('.measure-row .measure-value').allTextContents();
    assert(rows.join(' | ') === '20,258 | 0x4F22 | 0o47442 | 0b0100 1111 0010 0010', 'rows: ' + rows.join(' | '));
    await shot(pb, 'number-popup', { fullPage: false, phone: true });
    await dlg.getByRole('button', { name: 'Binary: 0b100111100100010. Copy' }).click();
    assert(await pb.evaluate(() => navigator.clipboard.readText()) === '0b100111100100010', 'copied');
    await pb.keyboard.press('Escape');
    await dlg.waitFor({ state: 'detached' });
    assert(await control.evaluate((el) => el === document.activeElement), 'the keyboard is back on the number');
    // As a character: the code point.
    await control.click();
    await dlg.getByRole('button', { name: /As a character: .* \(U\+4F22\)/ }).click();
    const cp = pb.getByRole('dialog', { name: 'Code point' });
    await cp.waitFor();
    await cp.getByText('CJK UNIFIED IDEOGRAPH-4F22').waitFor();
    await pb.keyboard.press('Escape');
    await cp.waitFor({ state: 'detached' });
  }, pb);

  await step('objects: a code point shows its character and what it is', async () => {
    await editor.click();
    await pa.keyboard.type('smile U+1F600');
    const list = offer('U+1F600');
    await list.waitFor();
    await pa.keyboard.press(' ');
    await pa.keyboard.type('and U+200D');
    await pa.keyboard.press(' ');
    await pa.keyboard.press('Enter');
    const control = message(pb, 'smile U+1F600').locator('button.codepoint');
    await control.first().waitFor();
    assert((await control.first().locator('.cp-shown').textContent()) === '😀', 'the character beside it');
    assert((await control.nth(1).locator('.cp-box').textContent()) === 'ZWJ', 'a box for what cannot be seen');
    const asked = [];
    pb.on('request', (r) => { if (r.url().includes('/api/v1/unicode')) asked.push(r.url()); });
    assert(asked.length === 0, 'nothing is asked before it is opened');
    await control.first().focus();
    await pb.keyboard.press(' ');
    const dlg = pb.getByRole('dialog', { name: 'Code point' });
    await dlg.waitFor();
    await dlg.getByText('GRINNING FACE').waitFor();
    await dlg.getByText('Other Symbol (So)').waitFor();
    const rows = await dlg.locator('.measure-row .measure-value').allTextContents();
    for (const want of ['U+1F600', '128512', 'F0 9F 98 80', 'D83D DE00', '&#x1F600;', '\\uD83D\\uDE00', '\\u{1F600}', '\\U0001F600']) assert(rows.includes(want), 'missing ' + want + ' in ' + rows.join(' | '));
    assert(asked.length === 1 && /code_points=U%2B1F600/.test(asked[0]), 'asked: ' + asked.join(' '));
    await shot(pb, 'codepoint-popup', { fullPage: false, phone: true });
    await dlg.getByRole('button', { name: 'UTF-8: F0 9F 98 80. Copy' }).click();
    assert(await pb.evaluate(() => navigator.clipboard.readText()) === 'F0 9F 98 80', 'copied');
    await pb.keyboard.press('Escape');
    await dlg.waitFor({ state: 'detached' });
    assert(await control.first().evaluate((el) => el === document.activeElement), 'the keyboard is back on the code point');
    // Opened again, the name is not asked for again.
    await control.first().click();
    await dlg.getByText('GRINNING FACE').waitFor();
    assert(asked.length === 1, 'asked again: ' + asked.length);
    await pb.keyboard.press('Escape');
  }, pb);

  await step('objects: an event shown as one range, added to a calendar', async () => {
    await editor.click();
    await pa.keyboard.type('Dinner 2026-12-24 18:00–20:00 Europe/Berlin');
    const typed = '2026-12-24 18:00–20:00 Europe/Berlin';
    const list = offer(typed);
    await list.waitFor();
    assert(/^Event: /.test((await list.getByRole('option').first().textContent()).trim()), 'the event row');
    await pa.keyboard.press(' ');
    await pa.keyboard.type('sharp');
    await pa.keyboard.press('Enter');
    const ev = message(pb, 'Dinner').locator('button.event');
    await ev.waitFor();
    // The reader is in Brisbane: 3 am to 5 am on the 25th.
    assert(/–/.test(await ev.textContent()), 'one range: ' + await ev.textContent());
    await ev.click();
    const [file] = await Promise.all([pb.waitForEvent('download'), pb.getByRole('menuitem', { name: 'Add to calendar' }).click()]);
    const text = await (await file.createReadStream()).toArray().then((xs) => Buffer.concat(xs).toString('utf8'));
    assert(text.includes('DTSTART:20261224T170000Z') && text.includes('SUMMARY:Dinner'), 'the calendar file: ' + text);
  }, pb);

  await step('objects: a phone number and an email address offer call, write and copy', async () => {
    await editor.click();
    await pa.keyboard.type('Ring +61 7 3123 4567');
    const list = offer('+61 7 3123 4567');
    await list.waitFor();
    await pa.keyboard.press(' ');
    await pa.keyboard.type('or sam@example.com');
    await offer('sam@example.com').waitFor();
    await pa.keyboard.press('Enter');
    const phone = message(pb, 'Ring').locator('button.contact').first();
    await phone.waitFor();
    assert(await phone.getAttribute('aria-label') === 'Phone number +61 7 3123 4567', 'the phone label');
    await phone.click();
    assert(await pb.getByRole('menuitem', { name: 'Call' }).count() === 1, 'Call is offered');
    await pb.keyboard.press('Escape');
    assert(await message(pb, 'Ring').locator('button.contact').nth(1).getAttribute('aria-label') === 'Email address sam@example.com', 'the email label');
  }, pa);

  await step('objects: a place is offered and copied on the web', async () => {
    await editor.click();
    await pa.keyboard.type('Meet at -27.4698, 153.0251');
    await offer('-27.4698, 153.0251').waitFor();
    await pa.keyboard.press(' ');
    await pa.keyboard.type('ok');
    await pa.keyboard.press('Enter');
    const place = message(pb, 'Meet at').locator('button.geo');
    await place.waitFor();
    assert(await place.getAttribute('aria-label') === 'Place -27.4698, 153.0251. Copy', 'the place label');
  }, pa);

  await step('objects: inline dice rolled by the server, the same for both', async () => {
    await editor.click();
    await pa.keyboard.type('I hit for [[2d6+3]]');
    const list = offer('[[2d6+3]]');
    await list.waitFor();
    assert((await list.getByRole('option').first().textContent()).trim() === 'Roll 2d6+3 when it is sent', 'the dice row');
    await pa.keyboard.press(' ');
    await pa.keyboard.type('damage');
    for (const p of [pa, pb]) await countReels(p, '#main .dice-inline.spinning');
    await pa.keyboard.press('Enter');
    const theirs = message(pb, 'I hit for').locator('.dice-inline');
    const mine = message(pa, 'I hit for').locator('.dice-inline');
    await theirs.waitFor();
    await mine.waitFor();
    // The dice spin once for the sender as the message first appears, and
    // once for the other person as it arrives; by now they have stopped on
    // the server's result.
    await pb.waitForTimeout(2500);
    assert(await reels(pa) === 1, `the sender saw the dice spin once (${await reels(pa)})`);
    assert(await reels(pb) === 1, `the other person saw the dice spin once (${await reels(pb)})`);
    const total = async (el) => (await el.locator('.chance-total strong').textContent()).trim();
    const n = Number(await total(theirs));
    assert(n >= 5 && n <= 15, 'a total of 2d6+3: ' + n);
    assert(await total(mine) === String(n), 'both see the one roll');
  }, pa);

  const swatch = (text) => message(pb, text).locator('button.colour');
  await step('objects: a colour accepted with space is drawn as a swatch', async () => {
    await editor.click();
    await pa.keyboard.type('Paint it #ff8800');
    const list = offer('#ff8800');
    await list.waitFor();
    assert((await list.getByRole('option').first().textContent()).trim() === 'Colour #ff8800', 'the colour row');
    await pa.keyboard.press(' ');
    await pa.keyboard.type('please');
    await pa.keyboard.press('Enter');
    await swatch('Paint it').waitFor();
    assert(await swatch('Paint it').getAttribute('aria-label') === 'Colour #ff8800. Copy', 'the swatch label');
  }, pa);

  await step('objects: a colour dismissed with Escape stays text', async () => {
    await editor.click();
    await pa.keyboard.type('Hex #abc');
    await offer('#abc').waitFor();
    await pa.keyboard.press('Escape');
    await pa.keyboard.type(' as text');
    await pa.keyboard.press('Enter');
    await message(pb, 'Hex #abc as text').waitFor();
    assert(await swatch('Hex #abc as text').count() === 0, 'Escape still made a colour');
  }, pa);

  await step('objects: a colour undone with Backspace stays text', async () => {
    await editor.click();
    await pa.keyboard.type('Undo #123456');
    await offer('#123456').waitFor();
    await pa.keyboard.press(' ');
    await pa.keyboard.press('Backspace');
    await pa.keyboard.type('again');
    await pa.keyboard.press('Enter');
    await message(pb, 'Undo #123456 again').waitFor();
    assert(await swatch('Undo #123456 again').count() === 0, 'Backspace did not undo the colour');
  }, pa);

  await a.context.close();
  await b.context.close();
}
