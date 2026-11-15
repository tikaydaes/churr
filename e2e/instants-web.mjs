// Instants in the browser (ruling R69): one person in Brisbane posts a time
// written for Berlin, the other in New York sees it in New York time, opens
// the converter and adds a zone; a phrase is offered as a time and sent.
//
// Each reader gets a browser context of their own with Playwright's
// timezoneId and locale, signed in with the session of the person the
// main run made.

import { step, shot, assert, consoleErrors, DESKTOP } from './lib.mjs';

const message = (page, text) => page.locator('#main .msg').filter({ hasText: text }).last();

async function reader(browser, who, origin, timezoneId, locale) {
  const context = await browser.newContext({ viewport: DESKTOP, timezoneId, locale, colorScheme: 'dark', storageState: await who.context.storageState() });
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin });
  context.setDefaultTimeout(8000);
  context.setDefaultNavigationTimeout(25000);
  const page = await context.newPage();
  page.on('console', (m) => { if (m.type() === 'error' && !/status of (401|403|429|503)/.test(m.text())) consoleErrors.push(`[${who.name} ${timezoneId}] ${m.text()}`); });
  page.on('pageerror', (e) => consoleErrors.push(`[${who.name} ${timezoneId}] PAGEERROR ${e.message}`));
  await page.goto(origin + '/chat/');
  await page.getByRole('button', { name: 'Jump to…' }).click();
  await page.getByRole('searchbox', { name: 'Jump to' }).fill('general');
  await page.getByRole('dialog').getByRole('button', { name: /general/ }).first().click();
  await page.locator('#main .editor').waitFor();
  return { context, page };
}

export async function instantsFlow(kaity, sam, browser, origin) {
  let a, b;
  const ok = await step('instants: two readers, in Brisbane and in New York', async () => {
    a = await reader(browser, kaity, origin, 'Australia/Brisbane', 'en-AU');
    b = await reader(browser, sam, origin, 'America/New_York', 'en-US');
    assert(await b.page.evaluate(() => Intl.DateTimeFormat().resolvedOptions().timeZone) === 'America/New_York', 'the second reader is not in New York');
  }, kaity.page);
  if (!ok) return;
  const pa = a.page, pb = b.page;

  await step('instants: the composer says what a typed time will mean', async () => {
    await pa.locator('#main .editor').click();
    await pa.keyboard.type('Party at 2026-12-24 18:00 Europe/Berlin');
    const chip = pa.locator('#main .composer-instants .instant-chip');
    await chip.waitFor();
    // 17:00 UTC is 3 in the morning on the 25th in Brisbane.
    const said = (await chip.textContent()).replace(/[  ]/g, ' ');
    assert(/Fri, 25 Dec, 3:00 am your time/.test(said), 'the composer said: ' + said);
    assert(await chip.getByRole('button', { name: 'Send “2026-12-24 18:00 Europe/Berlin” as plain text' }).count() === 1, 'no way to keep it as text');
    await shot(pa, '90-instants-composer', { fullPage: false });
    await pa.keyboard.press('Enter');
  }, pa);

  await step('instants: shown in the New York reader\'s time', async () => {
    const control = message(pb, 'Party at').locator('button.instant');
    await control.waitFor();
    const shown = (await control.textContent()).replace(/[  ]/g, ' ');
    assert(shown === 'Thu, Dec 24, 12:00 PM', 'shown as ' + shown);
    const label = await control.getAttribute('aria-label');
    assert(/Thursday, December 24, 2026/.test(label) && /America\/New York/.test(label) && label.includes('Written as “2026-12-24 18:00 Europe/Berlin”'), 'label: ' + label);
    // The author in Brisbane sees their own time.
    const mine = (await message(pa, 'Party at').locator('button.instant').textContent()).replace(/[  ]/g, ' ');
    assert(mine === 'Fri, 25 Dec, 3:00 am', 'the author sees ' + mine);
  }, pb);

  await step('instants: the converter, with a zone added and kept', async () => {
    const control = message(pb, 'Party at').locator('button.instant');
    await control.focus();
    await pb.keyboard.press('Enter');
    const dlg = pb.getByRole('dialog', { name: 'Time converter' });
    await dlg.waitFor();
    const rows = dlg.locator('.instant-row');
    const text = async () => (await rows.allTextContents()).join(' | ').replace(/[  ]/g, ' ');
    let all = await text();
    assert(/Your time · America\/New York \(UTC−5\).*12:00 PM/.test(all), 'your time: ' + all);
    assert(/As written · Europe\/Berlin \(UTC\+1\).*6:00 PM/.test(all), 'as written: ' + all);
    assert(/UTC.*5:00 PM/.test(all), 'UTC: ' + all);
    assert(/^in \d+ days$/.test(await dlg.locator('.instant-rel').textContent()), 'how long until');
    await dlg.getByRole('button', { name: 'Remind me' }).waitFor();
    await dlg.getByRole('searchbox', { name: 'Search time zones and cities' }).fill('tokyo');
    await dlg.getByRole('option', { name: /Asia\/Tokyo/ }).click();
    await rows.filter({ hasText: 'Asia/Tokyo' }).waitFor();
    all = await text();
    assert(/Asia\/Tokyo \(UTC\+9\).*Friday, December 25, 2026, 2:00 AM/.test(all), 'Tokyo: ' + all);
    await shot(pb, '91-instants-converter', { fullPage: false });
    await dlg.getByRole('button', { name: 'Close' }).last().click();
    // Opened again, the added zone is still there.
    await control.click();
    await dlg.locator('.instant-row', { hasText: 'Asia/Tokyo' }).waitFor();
    await dlg.getByRole('button', { name: 'Close' }).last().click();
  }, pb);

  await step('instants: a phrase offered as a time, accepted and sent', async () => {
    await pa.locator('#main .editor').click();
    await pa.keyboard.type('Lunch tomorrow at 1pm');
    const offer = pa.getByRole('listbox', { name: 'Make this a time' });
    await offer.waitFor();
    assert(/Make this a time: Tomorrow, 1:00 pm \(your time\)/.test((await offer.textContent()).replace(/[  ]/g, ' ')), 'offered: ' + await offer.textContent());
    await pa.keyboard.press('Enter');
    await offer.waitFor({ state: 'detached' });
    await pa.locator('#main .composer-instants .instant-chip', { hasText: 'tomorrow at 1pm' }).waitFor();
    await pa.keyboard.press('Enter');
    const control = message(pb, 'Lunch').locator('button.instant');
    await control.waitFor();
    const label = await control.getAttribute('aria-label');
    assert(label.includes('Written as “tomorrow at 1pm”'), 'label: ' + label);
    assert(await message(pb, 'Lunch').getByText('Lunch').count() > 0, 'the words are kept');
  }, pa);

  await step('instants: a phrase dismissed with Escape is not converted', async () => {
    await pa.locator('#main .editor').click();
    await pa.keyboard.type('Maybe Friday at 5pm');
    const offer = pa.getByRole('listbox', { name: 'Make this a time' });
    await offer.waitFor();
    await pa.keyboard.press('Escape');
    await offer.waitFor({ state: 'detached' });
    await pa.keyboard.press('Enter');
    await message(pb, 'Maybe Friday at 5pm').waitFor();
    assert(await message(pb, 'Maybe Friday at 5pm').locator('button.instant').count() === 0, 'a dismissed phrase became a time');
  }, pa);

  await a.context.close();
  await b.context.close();
}
