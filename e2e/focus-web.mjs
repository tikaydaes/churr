// The composer keeps the keyboard (docs/CLIENTS.md section 9.5): typing
// anywhere in a conversation goes into the message box, while selecting
// and copying text, other text boxes, keyboard navigation and phones are
// left alone.

import { devices } from 'playwright';
import { step, assert, consoleErrors } from './lib.mjs';

const box = (page, where = '#main') => page.locator(`${where} .editor`);
const boxText = (page, where = '#main') => box(page, where).evaluate((el) => el.textContent);
const focused = (page) => page.evaluate(() => {
  const a = document.activeElement;
  if (!a || a === document.body) return 'body';
  if (a.matches('#main .editor')) return 'composer';
  if (a.matches('#thread .editor')) return 'thread composer';
  return a.tagName.toLowerCase() + (a.className ? '.' + String(a.className).split(' ')[0] : '');
});
const clear = async (page, where) => { await box(page, where).click(); await page.keyboard.press('ControlOrMeta+a'); await page.keyboard.press('Backspace'); };

async function openGeneral(page, origin) {
  await page.goto(origin + '/chat/');
  await page.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
  await box(page).waitFor();
  await page.locator('.conn.live').waitFor();
}

export async function focusFlow(kaity, browser, origin) {
  const a = kaity.page;
  const msg = (text) => a.locator('#main .msg').filter({ hasText: text }).last();

  await step('focus: opening a conversation puts focus in its composer', async () => {
    await openGeneral(a, origin);
    await a.waitForFunction(() => document.activeElement && document.activeElement.matches('#main .editor'));
    await a.keyboard.type('focus check one');
    await a.keyboard.press('Enter');
    await msg('focus check one').waitFor();
    assert(await focused(a) === 'composer', 'after sending with Enter the composer has focus: ' + await focused(a));
    await a.keyboard.type('focus check two');
    await a.locator('#main .btn.send').click();
    await msg('focus check two').waitFor();
    assert(await focused(a) === 'composer', 'after pressing Send the composer has focus: ' + await focused(a));
  }, a);

  await step('focus: click a message, then type: the text is in the composer', async () => {
    await msg('focus check one').locator('.msg-main p').first().click();
    await a.waitForTimeout(100);
    assert(await focused(a) === 'composer', 'a click on a message gives focus back: ' + await focused(a));
    await a.keyboard.type('hello there');
    assert(await boxText(a) === 'hello there', 'the typing is in the composer once: ' + JSON.stringify(await boxText(a)));
    // Empty space and a button do the same; the cursor stays where it was.
    await a.locator('#main .conv-scroll').click({ position: { x: 5, y: 5 } });
    await a.keyboard.type('!');
    assert(await boxText(a) === 'hello there!', 'after a click on empty space: ' + JSON.stringify(await boxText(a)));
    await clear(a);
  }, a);

  await step('focus: a printable key on a focused message goes to the composer; shortcuts and navigation do not', async () => {
    // Focus moved by the keyboard (as Tab would) stays on the message.
    await msg('focus check one').focus();
    await a.keyboard.press('ArrowDown');
    assert((await focused(a)).startsWith('article.msg'), 'the arrow keys still move between messages: ' + await focused(a));
    await a.keyboard.press('Shift+Tab');
    assert(await focused(a) !== 'composer', 'Shift+Tab still moves focus');
    // R on a focused message is its shortcut: it reacts, it is not typed.
    await msg('focus check one').focus();
    await a.keyboard.press('r');
    await a.locator('.picker').waitFor();
    assert(await boxText(a) === '', 'the shortcut was not typed into the composer');
    await a.keyboard.press('Escape');
    await a.locator('.picker').waitFor({ state: 'detached' });
    await msg('focus check one').focus();
    await a.keyboard.type('xyz');
    assert(await boxText(a) === 'xyz', 'the letters went into the composer, once each: ' + JSON.stringify(await boxText(a)));
    assert(await focused(a) === 'composer', 'and focus with them');
    await clear(a);
    // Pasting with nothing typed into pastes into the composer.
    await msg('focus check one').focus();
    await a.evaluate(() => {
      const dt = new DataTransfer();
      dt.setData('text/plain', 'pasted words');
      document.activeElement.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
    });
    assert(await boxText(a) === 'pasted words', 'a paste outside any text box lands in the composer: ' + JSON.stringify(await boxText(a)));
    await clear(a);
  }, a);

  await step('focus: selecting text in a message keeps the selection, and copies', async () => {
    const p = msg('focus check two').locator('.msg-main p').first();
    await p.click({ clickCount: 3 });
    await a.waitForTimeout(150);
    const sel = await a.evaluate(() => String(window.getSelection()).trim());
    assert(sel.includes('focus check two'), 'the triple-click selected the message text: ' + JSON.stringify(sel));
    assert(await focused(a) !== 'composer', 'focus was not taken while text is selected');
    await a.keyboard.press('ControlOrMeta+c');
    const copied = await a.evaluate(() => navigator.clipboard.readText());
    assert(copied.includes('focus check two'), 'Ctrl/Cmd+C copied the selection: ' + JSON.stringify(copied));
    assert(await boxText(a) === '', 'nothing went into the composer');
    // A drag across the text selects too.
    await a.evaluate(() => window.getSelection().removeAllRanges());
    const r = await p.boundingBox();
    await a.mouse.move(r.x + 2, r.y + r.height / 2);
    await a.mouse.down();
    await a.mouse.move(r.x + r.width - 2, r.y + r.height / 2, { steps: 5 });
    await a.mouse.up();
    await a.waitForTimeout(150);
    assert((await a.evaluate(() => String(window.getSelection()))).length > 3, 'a drag selection survives the mouse-up');
  }, a);

  await step('focus: other text boxes keep focus; a menu closed gives it back', async () => {
    await a.getByRole('button', { name: 'Conversation menu' }).click();
    await a.getByRole('menu').waitFor();
    await a.keyboard.press('Escape');
    await a.waitForTimeout(100);
    assert(await focused(a) === 'composer', 'closing a menu opened by a click gives focus to the composer: ' + await focused(a));
    // A dialog's field keeps typing; closing it gives focus back.
    await a.getByRole('button', { name: 'Jump to…' }).click();
    const jump = a.getByRole('searchbox', { name: 'Jump to' });
    await jump.waitFor();
    await a.keyboard.type('gen');
    assert(await jump.inputValue() === 'gen', 'the dialog field kept the typing');
    assert(await boxText(a) === '', 'the composer did not take it');
    // Escape empties the search field first, then closes the dialog.
    await a.keyboard.press('Escape');
    if (await a.locator('dialog[open]').count()) await a.keyboard.press('Escape');
    await a.waitForTimeout(100);
    assert(await focused(a) === 'composer', 'closing the dialog gives focus to the composer: ' + await focused(a));
  }, a);

  await step('focus: with a thread open, typing goes to the composer used last', async () => {
    const parent = msg('focus check one');
    await parent.hover();
    await parent.getByRole('button', { name: 'Reply in thread' }).click();
    await box(a, '#thread').waitFor();
    await box(a, '#thread').click();
    await a.keyboard.type('in the thread');
    await msg('focus check two').locator('.msg-main p').first().click();
    await a.waitForTimeout(100);
    await a.keyboard.type('!');
    assert(await boxText(a, '#thread') === 'in the thread!', 'typing went back to the thread composer: ' + JSON.stringify(await boxText(a, '#thread')));
    assert(await boxText(a) === '', 'not to the conversation composer');
    await clear(a, '#thread');
    await box(a).click();
    await a.locator('#thread').getByRole('button', { name: 'Close the thread' }).click();
    await a.waitForTimeout(100);
    assert(await focused(a) === 'composer', 'after the thread closes the composer has focus: ' + await focused(a));
  }, a);

  await step('focus: on a phone only sending keeps focus', async () => {
    const context = await browser.newContext({ ...devices['Pixel 7'], timezoneId: 'UTC', locale: 'en-GB', colorScheme: 'dark', storageState: await kaity.context.storageState() });
    context.setDefaultTimeout(8000);
    const page = await context.newPage();
    page.on('pageerror', (e) => consoleErrors.push(`[phone focus] PAGEERROR ${e.message}`));
    try {
      await page.goto(origin + '/chat/');
      await page.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
      await box(page).waitFor();
      await page.waitForTimeout(300);
      assert(await focused(page) !== 'composer', 'opening a conversation on a phone does not pop the keyboard up');
      await page.locator('#main .msg').filter({ hasText: 'focus check one' }).last().locator('.msg-main p').first().tap();
      await page.waitForTimeout(150);
      assert(await focused(page) !== 'composer', 'a tap on a message does not either');
      await box(page).tap();
      await page.keyboard.type('from the phone');
      await page.locator('#main .btn.send').tap();
      await page.locator('#main .msg').filter({ hasText: 'from the phone' }).waitFor();
      assert(await focused(page) === 'composer', 'after sending the composer keeps focus: ' + await focused(page));
    } finally { await context.close(); }
  }, a);
}
