// The web chat, driven by two people in two browser contexts against one
// server: posting live, threads in both views with the marker, a poll, a
// checklist, a file, a reaction, search, the sub-channel tree, the "show"
// setting, a channel link from the picker, and catching up after the live
// connection was away.

import { step, shot, noSidewaysScroll, assert, PHONE, DESKTOP, SHOTS } from './lib.mjs';
import { fileURLToPath } from 'node:url';
import { writeFileSync } from 'node:fs';

const IMAGE = fileURLToPath(new URL('../internal/web/static/icons/icon-192.png', import.meta.url));

const editor = (page, thread) => page.locator(thread ? '#thread .editor' : '#main .editor');

// say types a message into a composer and sends it with Enter.
async function say(page, text, thread) {
  const box = editor(page, thread);
  await box.click();
  const lines = text.split('\n');
  for (let i = 0; i < lines.length; i++) {
    if (i) await page.keyboard.press('Shift+Enter');
    await page.keyboard.type(lines[i]);
  }
  await page.keyboard.press('Enter');
}

const message = (page, text, where = '#main') => page.locator(`${where} .msg`).filter({ hasText: text }).last();

async function openGeneral(page, origin) {
  await page.goto(origin + '/chat/');
  await page.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
  await page.locator('#main .editor').waitFor();
  await page.locator('.conn.live').waitFor();
}

export async function chatFlow(kaity, sam, origin) {
  const a = kaity.page, b = sam.page;

  await step('chat: both open #general and are connected live', async () => {
    await openGeneral(a, origin);
    await openGeneral(b, origin);
    await a.getByRole('heading', { name: /general/, level: 1 }).waitFor();
    assert(await a.evaluate(() => document.title.includes('Night Shift')), 'the title does not name the community');
  }, a);

  await step('chat: a message arrives live, drawn from Markdown', async () => {
    await say(a, 'Hello **night shift**, the `deploy` is at 5');
    await message(b, 'Hello night shift').locator('strong', { hasText: 'night shift' }).waitFor();
    await message(b, 'Hello night shift').locator('code', { hasText: 'deploy' }).waitFor();
    // Nothing from a message becomes markup.
    await say(b, '<img src=x onerror=alert(1)> <b>not bold</b>');
    await message(a, '<img src=x onerror=alert(1)>').waitFor();
    assert(await a.locator('#main .msg img[src="x"]').count() === 0, 'message text was parsed as HTML');
  }, b);

  await step('chat: a mention picked from the list is highlighted for its reader', async () => {
    await editor(b).click();
    await b.keyboard.type('thanks @kai');
    await b.getByRole('listbox', { name: 'People and groups' }).getByRole('option').filter({ hasText: 'Kaity' }).waitFor();
    await b.keyboard.press('Enter');
    await b.keyboard.type('see you then');
    await b.keyboard.press('Enter');
    await message(a, 'see you then').locator('.mention.me', { hasText: '@Kaity' }).waitFor();
    await shot(a, '40-chat-conversation');
    // the settings put theme, palette and contrast in this browser's hands
    await a.getByRole('button', { name: 'Web chat settings' }).click();
    await a.getByRole('group', { name: 'Theme' }).waitFor();
    await shot(a, '40-chat-appearance', { fullPage: false });
    await a.getByRole('group', { name: 'Palette' }).getByRole('button', { name: 'High distinction' }).click();
    assert(await a.evaluate(() => document.documentElement.dataset.palette === 'hd'), 'the high-distinction palette was not applied');
    await a.getByRole('group', { name: 'Theme' }).getByRole('button', { name: 'Light' }).click();
    assert(await a.evaluate(() => document.documentElement.dataset.theme === 'light' && JSON.parse(localStorage.getItem('churr-look')).theme === 'light'), 'the theme was not applied or kept');
    await a.getByRole('dialog').getByRole('button', { name: 'Done' }).click();
    await a.screenshot({ path: SHOTS + '40-chat-conversation-light-hd.png', animations: 'disabled' });
    await a.evaluate(() => { localStorage.removeItem('churr-look'); window.churrLook.apply(); });
  }, b);

  await step('chat: messages are shown in the order of `order`, and settled ones are not dashed', async () => {
    // What the server says, and what is drawn. The order of messages on
    // screen and in the page is by `order`, compared as plain text;
    // `seq` is only the arrival number.
    const out = await a.evaluate(async () => {
      const id = (location.pathname.match(/[0-9a-f-]{36}/) || [])[0];
      const page = await (await fetch(`/api/v1/conversations/${id}/messages?limit=60`)).json();
      const drawn = [...document.querySelectorAll('#main .messages > .msg[data-id]')].map((el) => el.dataset.id);
      return { id, messages: page.messages, drawn };
    });
    assert(out.id, 'the conversation id is not in the address');
    const bad = out.messages.filter((m) => typeof m.order !== 'string' || !m.order || typeof m.seq !== 'number' || 'pending' in m);
    assert(!bad.length, `messages without a text order or a seq, or with the removed pending flag: ${JSON.stringify(bad[0])}`);
    const want = out.messages.filter((m) => !m.parent_id).sort((x, y) => (x.order < y.order ? -1 : x.order > y.order ? 1 : 0)).map((m) => m.id).filter((id) => out.drawn.includes(id));
    assert(want.length >= 3 && want.join() === out.drawn.filter((id) => want.includes(id)).join(), `drawn ${out.drawn.join()} but by order ${want.join()}`);
    // Nothing here is shared with another server: nothing is unsent, late or dashed.
    assert(!out.messages.some((m) => m.unsent || m.late), 'a message of a conversation that is not shared is unsent or late');
    assert(await a.locator('#main .msg.unsent, #main .msg.pending, #main .tag.late').count() === 0, 'a settled message is drawn as provisional or late');
  }, a);

  await step('chat: a thread, in the threaded view with its marker', async () => {
    const parent = message(b, 'Hello night shift');
    await parent.hover();
    await parent.getByRole('button', { name: 'Reply in thread' }).click();
    await b.locator('#thread .editor').waitFor();
    await say(b, 'Who is on call for it?', true);
    await message(b, 'Who is on call', '#thread').waitFor();
    // For the other reader: a summary line under the parent, and one
    // marker in the body at the position of the reply. The reply itself
    // is not in the body.
    await message(a, 'Hello night shift').getByRole('button', { name: /1 reply/ }).waitFor();
    await a.locator('#main .marker', { hasText: 'Hello night shift' }).waitFor();
    assert(await a.locator('#main .msg', { hasText: 'Who is on call' }).count() === 0, 'a reply is drawn in the body of the threaded view');
    await shot(a, '41-chat-marker');
    // A later reply moves the marker; there is still only one.
    await say(b, 'I can take it', true);
    await a.waitForFunction(() => /2 replies/.test(document.querySelector('#main').innerText));
    assert(await a.locator('#main .marker').count() === 1, 'there is more than one marker for one thread');
  }, a);

  await step('chat: the same thread in the flat view', async () => {
    await a.getByRole('button', { name: 'Conversation menu' }).click();
    await a.getByRole('menuitemradio', { name: 'Flat' }).click();
    const reply = message(a, 'Who is on call');
    await reply.waitFor();
    await reply.getByRole('button', { name: 'Open thread' }).waitFor();
    await reply.locator('.msg-replyto', { hasText: 'Hello night shift' }).waitFor();
    assert(await a.locator('#main .marker').count() === 0, 'the flat view draws markers');
    await shot(a, '42-chat-flat');
    await a.getByRole('button', { name: 'Conversation menu' }).click();
    await a.getByRole('menuitemradio', { name: 'Threaded' }).click();
    await a.locator('#main .marker').waitFor();
  }, a);

  await step('chat: opening the thread from the marker reads it, and the marker goes', async () => {
    await a.locator('#main .marker-open').click();
    await message(a, 'I can take it', '#thread').waitFor();
    await a.locator('#main .marker').waitFor({ state: 'detached' });
    await say(a, 'Thanks Sam', true);
    await message(b, 'Thanks Sam', '#thread').waitFor();
    await shot(a, '43-chat-thread');
    await a.getByRole('button', { name: 'Close the thread' }).click();
    await a.locator('#thread').waitFor({ state: 'hidden' });
    await b.getByRole('button', { name: 'Close the thread' }).click();
  }, a);

  await step('chat: a poll from /poll, voted on live', async () => {
    await editor(a).click();
    await a.keyboard.type('/po');
    await a.getByRole('listbox', { name: 'Commands' }).getByRole('option').filter({ hasText: '/poll' }).waitFor();
    await a.keyboard.press('Enter');
    await a.keyboard.type('Lunch on Friday? | Noodles | Tacos');
    await a.keyboard.press('Enter');
    const pollB = b.locator('#main .form', { hasText: 'Lunch on Friday?' });
    await pollB.getByRole('button', { name: /Tacos/ }).click();
    await pollB.getByRole('button', { name: /Tacos/, pressed: true }).waitFor();
    const pollA = a.locator('#main .form', { hasText: 'Lunch on Friday?' });
    await pollA.locator('.choice', { hasText: 'Tacos' }).locator('.count', { hasText: '1' }).waitFor();
    await pollA.getByRole('button', { name: /Noodles/ }).click();
    await pollB.locator('.choice', { hasText: 'Noodles' }).locator('.count', { hasText: '1' }).waitFor();
    await pollB.getByText('2 answers').waitFor();
    await shot(b, '44-chat-poll');
  }, a);

  await step('chat: a checklist, ticked by the other person, with its history', async () => {
    await say(a, 'Before the deploy:\n- [ ] Tag the release\n- [ ] Tell support');
    const listB = message(b, 'Before the deploy');
    await listB.getByRole('checkbox', { name: 'Tag the release' }).check();
    const itemA = message(a, 'Before the deploy').locator('li.task', { hasText: 'Tag the release' });
    await itemA.locator('input:checked').waitFor();
    await itemA.getByText(/Sam Okafor/).waitFor();
    assert(await itemA.evaluate((el) => el.classList.contains('done')), 'a ticked item is not struck through');
    await itemA.hover();
    await itemA.getByRole('button', { name: 'History of this item' }).click();
    await a.getByRole('dialog').getByText('Sam Okafor ticked it').waitFor();
    await a.getByRole('dialog').getByRole('button', { name: 'Close' }).first().click();
    await shot(a, '45-chat-checklist');
  }, a);

  await step('chat: an image is attached with a caption, uploaded with progress, and opens', async () => {
    await a.locator('#main .composer input[type=file]').setInputFiles(IMAGE);
    await a.getByLabel('Caption for icon-192.png').fill('The owl');
    await editor(a).click();
    await a.keyboard.type('Our mascot');
    await shot(a, '46-chat-attachment');
    await a.keyboard.press('Enter');
    const img = message(b, 'Our mascot').locator('.file-image img');
    await img.waitFor();
    await b.waitForFunction(() => { const i = [...document.querySelectorAll('#main .file-image img')].pop(); return i && i.complete && i.naturalWidth > 0; });
    assert(await img.getAttribute('alt') === 'The owl', 'the caption is not the image’s text');
    await message(b, 'Our mascot').getByRole('button', { name: 'Open The owl' }).click();
    await b.getByRole('dialog').locator('.viewer-image img').waitFor();
    await b.getByRole('dialog').getByRole('button', { name: 'Close' }).first().click();
    // A text file is previewed in monospace.
    const notes = fileURLToPath(new URL('./shots/notes.go', import.meta.url));
    writeFileSync(notes, 'package main\n\nfunc main() { println("churr") }\n');
    await a.locator('#main .composer input[type=file]').setInputFiles(notes);
    await editor(a).click();
    await a.keyboard.press('Enter');
    await b.locator('#main .file-text .codeblock', { hasText: 'println' }).waitFor();
    await b.locator('#main .file-text .hl-kw', { hasText: 'func' }).waitFor();
  }, a);

  await step('chat: a reaction from the picker shows for both, and can be joined', async () => {
    const m = message(b, 'Our mascot');
    await m.hover();
    await m.getByRole('button', { name: 'React' }).click();
    await b.getByRole('searchbox', { name: 'Search emoji' }).fill('thumbs_up');
    await b.locator('.picker-cell').first().click();
    const chip = message(a, 'Our mascot').locator('.reaction').first();
    await chip.locator('.count', { hasText: '1' }).waitFor();
    await chip.click();
    await message(b, 'Our mascot').locator('.reaction .count', { hasText: '2' }).waitFor();
    await message(a, 'Our mascot').locator('.reaction.mine').waitFor();
  }, a);

  await step('chat: editing with the up arrow changes it for everyone', async () => {
    await say(a, 'The deploy is at 6');
    await message(b, 'The deploy is at 6').waitFor();
    await editor(a).click();
    await a.keyboard.press('ArrowUp');
    await a.getByText('Editing your message').waitFor();
    await a.keyboard.press('End');
    await a.keyboard.type(', not 5');
    await a.keyboard.press('Enter');
    await message(b, 'The deploy is at 6, not 5').waitFor();
  }, a);

  let designId = '';
  await step('chat: a channel, a sub-channel under it, and the tree in the sidebar', async () => {
    await a.getByRole('button', { name: 'New…' }).click();
    await a.getByRole('menuitem', { name: 'New channel' }).click();
    await a.getByLabel('Name').fill('Design reviews');
    await a.getByRole('button', { name: 'Create' }).click();
    await a.getByRole('heading', { name: /Design reviews/, level: 1 }).waitFor();
    designId = a.url().split('/c/')[1].split('/')[0];
    await a.getByRole('button', { name: 'New…' }).click();
    await a.getByRole('menuitem', { name: 'New channel' }).click();
    await a.getByLabel('Name').fill('Sketches');
    await a.getByLabel('Under').selectOption({ label: 'Design reviews' });
    await a.getByRole('button', { name: 'Create' }).click();
    await a.getByRole('heading', { name: /Sketches/, level: 1 }).waitFor();
    // The header reads `parent › name`, and the row sits indented under
    // its parent.
    await a.locator('.conv-title h1 .parent', { hasText: 'Design reviews' }).waitFor();
    const child = a.locator('.side-row.child', { hasText: 'Sketches' });
    await child.waitFor();
    const order = await a.locator('.side-row').allInnerTexts();
    const at = order.findIndex((t) => t.includes('Design reviews'));
    assert(at >= 0 && order[at + 1].includes('Sketches'), `the sub-channel is not listed under its parent: ${order.join(' | ')}`);
    await shot(a, '47-chat-tree');
    // Collapsing the parent hides the sub-channel's row, except while it
    // is the one open; so go to the parent first. A folded parent says how
    // many it keeps out of view.
    await a.locator('.side-row', { hasText: 'Design reviews' }).first().getByRole('link').click();
    await a.getByRole('button', { name: 'Hide the sub-channels of Design reviews' }).click();
    await child.waitFor({ state: 'detached' });
    await a.locator('.side-row', { hasText: 'Design reviews' }).locator('.row-kids', { hasText: '1 more' }).waitFor();
    await a.getByRole('button', { name: 'Show the sub-channels of Design reviews' }).click();
    await child.waitFor();
  }, a);

  await step('chat: folding keeps unread rows in view, and a lone sub-channel names its parent', async () => {
    // A second sub-channel, and something unread in the first: Sam joins
    // Sketches and says something there.
    await a.getByRole('button', { name: 'New…' }).click();
    await a.getByRole('menuitem', { name: 'New channel' }).click();
    await a.getByLabel('Name').fill('Moodboards');
    await a.getByLabel('Under').selectOption({ label: 'Design reviews' });
    await a.getByRole('button', { name: 'Create' }).click();
    await a.getByRole('heading', { name: /Moodboards/, level: 1 }).waitFor();
    await a.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
    await a.getByRole('heading', { name: /general/, level: 1 }).waitFor();
    const sketches = await a.evaluate(async () => { const { store } = await import('/chat/store.js'); return [...store.conversations.values()].find((c) => c.name === 'Sketches').id; });
    await b.evaluate(async (id) => {
      const { post } = await import('/shared/api.js');
      await post(`/conversations/${id}/join`);
      await post(`/conversations/${id}/messages`, { text: 'First sketches are up', client_id: crypto.randomUUID() });
    }, sketches);
    const sketchRow = a.locator('.side-row', { hasText: 'Sketches' });
    await a.locator('.side-row.unread', { hasText: 'Sketches' }).waitFor();
    const mood = a.locator('.side-row', { hasText: 'Moodboards' });

    // A folded parent keeps its unread sub-channel under it and counts
    // the rest as "1 more"; pressing "1 more" unfolds it.
    await a.getByRole('button', { name: 'Hide the sub-channels of Design reviews' }).click();
    await mood.waitFor({ state: 'detached' });
    assert(await a.locator('.side-row.child.unread', { hasText: 'Sketches' }).count() === 1, 'the unread sub-channel left with its folded parent');
    await a.locator('.side-row', { hasText: 'Design reviews' }).locator('.row-kids', { hasText: '1 more' }).click();
    await mood.waitFor();
    await a.getByRole('button', { name: 'Hide the sub-channels of Design reviews' }).waitFor({ state: 'attached' });

    // A folded section keeps the open conversation and the unread rows.
    // Sketches stays without its parent's row, so it stands at the
    // parent's level and reads "Design reviews › Sketches".
    const section = a.locator('.side-group[aria-label="Channels"] .sec');
    await section.click();
    await section.and(a.locator('[aria-expanded="false"]')).waitFor();
    await mood.waitFor({ state: 'detached' });
    assert(await a.locator('.side-row', { hasText: 'Design reviews' }).filter({ hasNot: a.locator('.pp') }).count() === 0, 'the parent kept its row in a folded section');
    assert(await a.locator('.side-row.current', { hasText: 'general' }).count() === 1, 'the open conversation left with its folded section');
    const lone = a.locator('.side-row.alone', { hasText: 'Sketches' });
    await lone.waitFor();
    assert((await lone.locator('.row-name').innerText()).replace(/\s+/g, ' ').trim() === 'Design reviews › Sketches', `the lone sub-channel reads ${await lone.locator('.row-name').innerText()}`);
    assert(!(await lone.evaluate((el) => el.classList.contains('child'))), 'the lone sub-channel is indented');
    // Rows line up in one column: the names of a lone sub-channel and of
    // the open conversation start at the same place.
    const lefts = await a.evaluate(() => [...document.querySelectorAll('.side-row:not(.child) .row-name')].map((n) => Math.round(n.getBoundingClientRect().left)));
    assert(new Set(lefts).size === 1, `the rows do not line up: ${lefts.join(', ')}`);
    await shot(a, '47-chat-folded');
    // At the width of a phone the list is a pane of its own.
    await a.setViewportSize(PHONE);
    try {
      await a.getByRole('button', { name: 'Back to the list' }).click();
      await lone.waitFor();
      await noSidewaysScroll(a);
      await shot(a, '47-chat-folded-phone', { fullPage: false });
    } finally { await a.setViewportSize(DESKTOP); }

    // All of it by keyboard: the section name opens with Enter, the arrows
    // move between rows, Left folds a parent and Right opens it again.
    await section.focus();
    await a.keyboard.press('Enter');
    await section.and(a.locator('[aria-expanded="true"]')).waitFor();
    assert(await a.evaluate(() => document.activeElement.classList.contains('sec')), 'the section name lost focus when it opened');
    await a.keyboard.press('ArrowDown');
    assert(await a.evaluate(() => document.activeElement.classList.contains('row-link')), 'ArrowDown did not move to a row');
    await a.locator('.side-row', { hasText: 'Design reviews' }).first().getByRole('link').focus();
    await a.keyboard.press('ArrowLeft');
    await a.getByRole('button', { name: 'Show the sub-channels of Design reviews' }).waitFor();
    assert(await a.evaluate(() => document.activeElement.closest('.side-row') && document.activeElement.closest('.side-row').textContent.includes('Design reviews')), 'folding lost the focus');
    await a.keyboard.press('ArrowRight');
    await a.getByRole('button', { name: 'Hide the sub-channels of Design reviews' }).waitFor();
    await mood.waitFor();
    await sketchRow.first().getByRole('link').click();
    await message(a, 'First sketches are up').waitFor();
  }, a);

  await step('chat: a channel link from the picker is sent by id and drawn by name', async () => {
    await a.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
    await editor(a).click();
    await a.keyboard.type('Notes are in #des');
    const pick = a.getByRole('listbox', { name: 'Link a channel?' });
    await pick.getByRole('option').filter({ hasText: 'Design reviews' }).first().waitFor();
    await shot(a, '48-chat-channel-picker', { fullPage: false });
    // its sub-channels match too, through their parent's name
    await pick.getByRole('option').filter({ hasText: 'Design reviews' }).filter({ hasNotText: /Sketches|Moodboards/ }).first().click();
    await a.locator('#main .editor .chip.channel', { hasText: 'Design reviews' }).waitFor();
    await a.keyboard.type('from now on');
    await a.keyboard.press('Enter');
    const sent = message(a, 'Notes are in');
    await sent.locator('.chip.channel', { hasText: 'Design reviews' }).waitFor();
    // What was sent holds the identifier and never the name.
    const text = await a.evaluate(async (id) => {
      const page = await (await fetch(`/api/v1/conversations/${id}/messages?limit=5`)).json();
      return page.messages.map((m) => m.text).find((t) => t && t.startsWith('Notes are in'));
    }, await a.evaluate(() => location.pathname.split('/c/')[1]));
    assert(text === `Notes are in <#${designId}> from now on`, `the text sent was ${text}`);
    // The other reader is not in the channel; it is public, so they see
    // its name, and the chip offers to open or join it.
    const seen = message(b, 'Notes are in').locator('.chip.channel', { hasText: 'Design reviews' });
    await seen.waitFor();
    await seen.click();
    await b.getByRole('menuitem', { name: 'Join' }).waitFor();
    await b.keyboard.press('Escape');
    // `#games` typed and left alone is plain text.
    await say(a, 'no such #games channel');
    const plain = message(b, 'no such #games channel');
    await plain.waitFor();
    assert(await plain.locator('.chip').count() === 0, 'a typed #name became a link');
  }, a);

  await step('chat: "show in sidebar" hides a row and brings it back', async () => {
    // Sam sets #general to show only while it has something unread.
    const row = b.locator('.side-row', { hasText: 'general' });
    await row.hover();
    await row.getByRole('button', { name: 'Menu for general' }).click();
    await b.getByRole('menuitemradio', { name: 'When there is activity' }).click();
    // The row does not vanish under the person's hand: it stays while the
    // conversation is open.
    await b.waitForTimeout(300);
    assert(await row.count() === 1, 'the row of the open conversation vanished');
    // Once Sam has moved on, and with nothing unread, it goes.
    await b.getByRole('link', { name: 'Threads' }).click();
    await b.getByRole('heading', { name: 'Threads', level: 1 }).waitFor();
    await row.waitFor({ state: 'detached' });
    await shot(b, '49-chat-row-hidden');
    // An unread message brings it back, in its place.
    await say(a, 'Anyone around?');
    await row.waitFor();
    await shot(b, '50-chat-row-back');
    // Hidden: no row at all, until "Show hidden" is on.
    await row.getByRole('link').click();
    await message(b, 'Anyone around?').waitFor();
    await row.hover();
    await row.getByRole('button', { name: 'Menu for general' }).click();
    await b.getByRole('menuitemradio', { name: 'Hidden' }).click();
    await b.getByRole('link', { name: 'Threads' }).click();
    await row.waitFor({ state: 'detached' });
    await say(a, 'Still here');
    await b.waitForTimeout(500);
    assert(await row.count() === 0, 'a hidden conversation got a row from new activity');
    await b.getByRole('checkbox', { name: 'Show hidden' }).check();
    await row.waitFor();
    await row.getByRole('img', { name: 'Hidden' }).waitFor();
    // The choice is kept in the browser under the account, and nowhere else.
    const kept = await b.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('churr.')).map((k) => JSON.parse(localStorage.getItem(k)).showHidden));
    assert(kept.length === 1 && kept[0] === true, 'the toggle is not kept in local storage');
    await b.getByRole('checkbox', { name: 'Show hidden' }).uncheck();
    await row.waitFor({ state: 'detached' });
    // The settings dialog puts the pair into words.
    await b.getByRole('button', { name: 'Jump to…' }).click();
    await b.getByRole('searchbox', { name: 'Jump to' }).fill('gen');
    await b.getByRole('dialog').getByRole('button', { name: /general/ }).click();
    await b.getByRole('button', { name: 'Conversation menu' }).click();
    await b.getByRole('menuitem', { name: 'Notifications and sidebar…' }).click();
    await b.getByText(/It has no row in the sidebar/).waitFor();
    await shot(b, '51-chat-modes', { fullPage: false });
    await b.getByRole('radio', { name: /^Always/ }).check();
    await b.getByText('It is always in the sidebar.').waitFor();
    await b.getByRole('button', { name: 'Done' }).click();
  }, b);

  await step('chat: search finds a message and opens it in place', async () => {
    await a.getByRole('link', { name: 'Search', exact: true }).click();
    await a.getByRole('searchbox', { name: 'Search messages' }).fill('mascot');
    await a.keyboard.press('Enter');
    const hit = a.locator('.result', { hasText: 'Our mascot' });
    await hit.waitFor();
    await shot(a, '52-chat-search');
    await hit.locator('.result-where').click();
    await a.locator('#main .msg.flash', { hasText: 'Our mascot' }).waitFor();
  }, a);

  await step('chat: catching up after the live connection was away', async () => {
    await a.evaluate(async () => { const { live } = await import('/chat/store.js'); live.stop(); });
    await say(b, 'Said while Kaity was away');
    await b.waitForTimeout(300);
    assert(await a.locator('#main .msg', { hasText: 'Said while Kaity was away' }).count() === 0, 'the message arrived with the connection stopped');
    await a.evaluate(async () => { const { live } = await import('/chat/store.js'); live.start(); });
    await message(a, 'Said while Kaity was away').waitFor();
    await a.locator('.conn.live').waitFor();
  }, a);

  await step('chat: status, do not disturb and the other screens', async () => {
    await a.locator('.me-status').click();
    await a.getByLabel('Status text').fill('Deploying');
    await a.getByRole('button', { name: 'Set status' }).click();
    await a.locator('.me-status', { hasText: 'Deploying' }).waitFor();
    await a.getByRole('button', { name: 'Do not disturb' }).click();
    await a.getByRole('button', { name: '30 minutes' }).click();
    await a.getByRole('button', { name: 'Do not disturb is on' }).waitFor();
    await a.getByRole('button', { name: 'Do not disturb is on' }).click();
    await shot(a, '53-chat-dnd', { fullPage: false });
    await a.getByRole('button', { name: 'Turn off now' }).click();
    await a.getByRole('button', { name: 'Do not disturb', exact: true }).waitFor();
    for (const [name, shows] of [['Notifications', /Nothing new|mentioned you|new message/], ['Bookmarks', /No bookmarks|bookmarked/], ['Scheduled', 'Your saved replies'], ['Threads', /thread/i]]) {
      await a.getByRole('link', { name, exact: true }).click();
      await a.getByRole('heading', { name, level: 1 }).waitFor();
      await a.locator('.view-body').getByText(shows).first().waitFor();
    }
    await a.getByRole('button', { name: 'Browse channels' }).click();
    await a.locator('.browse', { hasText: 'Sketches' }).waitFor();
    await shot(a, '54-chat-browse');
    // The members and settings of a channel.
    await a.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
    await a.getByRole('button', { name: /^Members/ }).click();
    await a.getByRole('dialog').getByText('@sam').waitFor();
    await a.getByRole('tab', { name: 'Manage' }).click();
    await a.getByRole('dialog').getByText('Slow mode').waitFor();
    await shot(a, '55-chat-settings', { fullPage: false });
    await a.getByRole('dialog').getByRole('button', { name: 'Done' }).click();
    // Starting a call shows the screen before joining; cancelling it
    // starts nothing (calls themselves are in calls-web.mjs).
    await a.getByRole('button', { name: 'Start a call' }).click();
    await a.getByRole('dialog', { name: 'Start a call' }).getByRole('button', { name: 'Cancel' }).click();
    assert(!(await a.locator('#call').count()), 'cancelling started a call');
  }, a);

  await step('chat: phone width: one pane at a time, nothing scrolls sideways', async () => {
    await b.setViewportSize(PHONE);
    try {
      await b.goto(origin + '/chat/');
      await b.locator('.side-row').first().waitFor();
      await noSidewaysScroll(b);
      await shot(b, '56-chat-phone-list', { fullPage: false });
      await b.getByRole('button', { name: 'Jump to…' }).click();
      await b.getByRole('searchbox', { name: 'Jump to' }).fill('gen');
      await b.getByRole('dialog').getByRole('button', { name: /general/ }).click();
      await b.locator('#main .editor').waitFor();
      await message(b, 'Hello night shift').waitFor();
      assert(!(await b.locator('#sidebar').isVisible()), 'the list is still showing beside the conversation');
      await noSidewaysScroll(b);
      await shot(b, '57-chat-phone-conversation', { fullPage: false });
      await message(b, 'Hello night shift').getByRole('button', { name: /replies/ }).click();
      await b.locator('#thread .editor').waitFor();
      await message(b, 'Thanks Sam', '#thread').waitFor();
      assert(!(await b.locator('#main .editor').isVisible()), 'the conversation is still showing under the thread');
      await noSidewaysScroll(b);
      await shot(b, '58-chat-phone-thread', { fullPage: false });
      await b.getByRole('button', { name: 'Back to the conversation' }).click();
      await b.getByRole('button', { name: 'Back to the list' }).click();
      await b.locator('.side-row').first().waitFor();
    } finally { await b.setViewportSize(DESKTOP); }
  }, b);

  await step('chat: the page is installable and sends nothing inline', async () => {
    const res = await a.request.get(origin + '/chat/manifest.webmanifest');
    const m = await res.json();
    assert(m.display === 'standalone' && m.start_url === '/chat/' && m.name.includes('Night Shift'), 'the manifest is wrong');
    assert(m.icons.some((i) => i.purpose === 'maskable'), 'no maskable icon');
    const csp = (await a.request.get(origin + '/chat/')).headers()['content-security-policy'];
    assert(/script-src 'self'/.test(csp) && /style-src 'self'/.test(csp) && !/unsafe/.test(csp), `the policy is ${csp}`);
    assert(await a.evaluate(() => !('serviceWorker' in navigator) || navigator.serviceWorker.getRegistrations().then((r) => r.length === 0)), 'a service worker is registered');
  }, a);
}

async function menuOf(page, text, item, where = '#main') {
  const m = message(page, text, where);
  await m.hover();
  await m.getByRole('button', { name: 'More actions' }).click();
  await page.getByRole('menuitem', { name: item }).click();
}

// The message actions of docs/CLIENTS.md section 9.6 that the first flow
// did not use: bookmark, pin, history, delete, purge request, report,
// notify anyway and moving a thread to a channel.
export async function actionsFlow(kaity, sam, origin) {
  const a = kaity.page, b = sam.page;
  await step('actions: both back in #general', async () => {
    for (const p of [a, b]) {
      await p.goto(origin + '/chat/');
      await p.getByRole('button', { name: 'Jump to…' }).click();
      await p.getByRole('searchbox', { name: 'Jump to' }).fill('general');
      await p.getByRole('dialog').getByRole('button', { name: /general/ }).first().click();
      await message(p, 'Hello night shift').waitFor();
    }
  }, a);

  await step('actions: bookmark, and the bookmarks list', async () => {
    await menuOf(b, 'Our mascot', 'Bookmark');
    await b.getByText('Bookmarked.').waitFor();
    await b.getByRole('link', { name: 'Bookmarks', exact: true }).click();
    await b.locator('.result', { hasText: 'Our mascot' }).waitFor();
    await shot(b, '60-chat-bookmarks');
    await b.locator('.result', { hasText: 'Our mascot' }).locator('.result-where').click();
    await b.locator('#main .msg.flash', { hasText: 'Our mascot' }).waitFor();
  }, b);

  await step('actions: pin, seen by the other person, and the pins list', async () => {
    await menuOf(a, 'Our mascot', 'Pin');
    await message(b, 'Our mascot').getByRole('img', { name: 'Pinned' }).waitFor();
    await a.getByRole('button', { name: 'Pinned messages' }).click();
    await a.getByRole('dialog').locator('.pin', { hasText: 'Our mascot' }).waitFor();
    await a.getByRole('dialog').getByRole('button', { name: 'Close' }).last().click();
  }, a);

  await step('actions: an edit after the quick-correction window keeps history', async () => {
    // The window is shortened so the test does not have to wait it out.
    await a.evaluate(async () => { await fetch('/api/v1/admin/settings', { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ 'messages.quick_correction_seconds': 1 }) }); });
    await say(a, 'The first wording of this');
    await message(b, 'The first wording of this').waitFor();
    await a.waitForTimeout(1600);
    await menuOf(a, 'The first wording of this', 'Edit');
    await a.keyboard.press('ControlOrMeta+a');
    await a.keyboard.type('The second wording of this');
    await a.keyboard.press('Enter');
    const edited = message(b, 'The second wording of this');
    await edited.getByText('edited').waitFor();
    await menuOf(b, 'The second wording of this', 'History');
    const dlg = b.getByRole('dialog');
    await dlg.getByText('The first wording of this').first().waitFor();
    await dlg.locator('.history-diff del', { hasText: 'first' }).waitFor();
    await dlg.locator('.history-diff ins', { hasText: 'second' }).waitFor();
    await shot(b, '61-chat-history', { fullPage: false });
    await dlg.getByRole('button', { name: 'Close' }).last().click();
  }, a);

  await step('actions: delete leaves a marker; the author asks for a purge', async () => {
    await menuOf(a, 'The second wording of this', 'Delete…');
    await a.getByRole('dialog').getByText('Its content is kept as history until it is purged.').waitFor();
    await a.getByRole('dialog').getByRole('button', { name: 'Delete' }).click();
    await b.locator('#main .msg-deleted').last().waitFor();
    assert(await b.locator('#main .msg', { hasText: 'The second wording of this' }).count() === 0, 'a deleted message still shows its text');
    await a.locator('#main .msg-deleted').last().getByRole('button', { name: 'Ask for purge' }).click();
    await a.getByRole('dialog').getByText(/an admin decides whether they are erased/).waitFor();
    await a.getByRole('dialog').getByRole('button', { name: 'Ask for it' }).click();
    await a.getByText('Asked. The history is hidden meanwhile.').waitFor();
  }, a);

  await step('actions: a report with a reason', async () => {
    await menuOf(b, 'Anyone around?', 'Report…');
    await b.getByLabel('Why').fill('Testing the report queue');
    await b.getByRole('dialog').getByRole('button', { name: 'Report' }).click();
    await b.getByText('Reported.').waitFor();
  }, b);

  await step('actions: notify anyway is offered for someone in do not disturb', async () => {
    await b.getByRole('button', { name: 'Do not disturb', exact: true }).click();
    await b.getByRole('button', { name: 'An hour' }).click();
    await b.getByRole('button', { name: 'Do not disturb is on' }).waitFor();
    // Kaity sees the mark arrive, then mentions Sam.
    await a.waitForFunction(async () => { const { store } = await import('/chat/store.js'); return [...store.accounts.values()].some((x) => x.username === 'sam' && x.dnd); });
    await editor(a).click();
    await a.keyboard.type('@sam');
    await a.getByRole('listbox', { name: 'People and groups' }).getByRole('option').filter({ hasText: 'do not disturb' }).waitFor();
    await a.keyboard.press('Enter');
    await a.keyboard.type('the build is red');
    await a.keyboard.press('Enter');
    const offer = a.locator('.composer-notice', { hasText: 'Sam Okafor is in do not disturb' });
    await offer.getByRole('button', { name: 'Notify anyway' }).click();
    await offer.getByText('Asked.').waitFor();
    // The mark is on the message; nothing says whether Sam was reached.
    await message(b, 'the build is red').getByRole('img', { name: 'Notified anyway' }).waitFor();
    await b.getByRole('button', { name: 'Do not disturb is on' }).click();
    await b.getByRole('button', { name: 'Turn off now' }).click();
  }, a);

  await step('actions: a thread moved to a channel becomes a sub-channel', async () => {
    await message(a, 'Hello night shift').getByRole('button', { name: /replies/ }).click();
    await a.getByRole('button', { name: 'Move to a channel' }).click();
    await a.getByLabel('Name of the new channel').fill('On call');
    await a.getByRole('dialog').getByText('This cannot be undone.').waitFor();
    await a.getByRole('button', { name: 'Move it' }).click();
    await a.getByRole('heading', { name: /On call/, level: 1 }).waitFor();
    // The new channel starts with a copy of the parent, then the replies.
    await message(a, 'Hello night shift').waitFor();
    await message(a, 'Who is on call').waitFor();
    // It sits under the channel the thread was in.
    await a.locator('.side-row.child', { hasText: 'On call' }).waitFor();
    await shot(a, '62-chat-thread-moved');
    // In the old channel the thread holds a pointer and takes no replies.
    await b.locator('.side-row').filter({ hasText: /^general/ }).first().getByRole('link').click().catch(() => {});
    await message(b, 'Hello night shift').getByRole('button', { name: /1 reply/ }).click();
    await b.locator('#thread .msg-system', { hasText: 'This thread moved to its own channel' }).waitFor();
    await b.locator('#thread .composer-blocked', { hasText: 'takes no more replies here' }).waitFor();
    await b.getByRole('button', { name: 'Close the thread' }).click();
  }, a);

  await step('actions: every control on the chat page has a name', async () => {
    for (const p of [a, b]) {
      const unnamed = await p.evaluate(() => [...document.querySelectorAll('button, a[href], input:not([type=hidden]), select, textarea, [role=button], [role=textbox]')].filter((el) => {
        if (el.hidden || el.closest('[hidden]')) return false;
        const label = el.getAttribute('aria-label') || el.getAttribute('title') || (el.labels && el.labels.length && el.labels[0].innerText) || el.innerText || el.getAttribute('placeholder') || (el.querySelector('img[alt]:not([alt=""]), svg[aria-label]') ? 'x' : '');
        return !String(label).trim();
      }).map((el) => el.outerHTML.slice(0, 140)));
      assert(unnamed.length === 0, `controls with no name: ${unnamed.join(' || ')}`);
    }
  }, a);
}

// What needs something the admin console made first: a bot to post a card,
// the community's saved reply; and the rest of the composer: sending
// later, the form builder with its answer dialog, a new chat.
export async function extrasFlow(kaity, sam, origin) {
  const a = kaity.page, b = sam.page;
  await step('extras: back in #general', async () => {
    for (const p of [a, b]) {
      await p.goto(origin + '/chat/');
      await p.getByRole('button', { name: 'Jump to…' }).click();
      await p.getByRole('searchbox', { name: 'Jump to' }).fill('general');
      await p.getByRole('dialog').getByRole('button', { name: /general/ }).first().click();
      await p.locator('#main .editor').waitFor();
      await p.locator('.conn.live').waitFor();
    }
  }, a);

  await step('extras: a card posted by a bot is drawn in its fixed layout', async () => {
    assert(kaity.botToken, 'the admin flow did not leave a bot token');
    const general = await a.evaluate(() => location.pathname.split('/c/')[1]);
    // A manager adds the bot to the channel; the bot then posts with its
    // own token, as a bot outside the browser would.
    const added = await a.evaluate(async (id) => {
      const bots = (await (await fetch('/api/v1/admin/bots')).json()).bots;
      const res = await fetch(`/api/v1/conversations/${id}/members`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ account_ids: [bots[0].account.id] }) });
      return res.status;
    }, general);
    assert(added === 200, `adding the bot answered ${added}`);
    const res = await a.request.fetch(`${origin}/api/v1/conversations/${general}/messages`, {
      method: 'POST', headers: { Authorization: 'Bearer ' + kaity.botToken, 'Content-Type': 'application/json' },
      data: { text: 'Build finished', cards: [{ color: 'pink', author: { name: 'CI' }, title: 'Build #482 failed', url: 'https://ci.example/builds/482', text: '`main` · **3** tests failing\n- unit\n- e2e',
        fields: [{ label: 'Branch', value: 'main', short: true }, { label: 'Duration', value: '4m 12s', short: true }], footer: 'ci.example' }] },
    });
    assert(res.status() === 201, `the bot's post answered ${res.status()}: ${await res.text()}`);
    const card = b.locator('#main .card.pink');
    await card.getByRole('link', { name: 'Build #482 failed' }).waitFor();
    await card.locator('.card-fields dt', { hasText: 'Branch' }).waitFor();
    await card.locator('strong', { hasText: '3' }).waitFor();
    await card.locator('li', { hasText: 'e2e' }).waitFor();
    await b.locator('#main .msg', { hasText: 'Build finished' }).locator('.tag', { hasText: 'bot' }).waitFor();
    await shot(b, '70-chat-card');
  }, a);

  await step('extras: a community saved reply posts its text', async () => {
    await editor(a).click();
    await a.keyboard.type('/shr');
    await a.getByRole('listbox', { name: 'Commands' }).getByRole('option').filter({ hasText: '/shrug' }).waitFor();
    await a.keyboard.press('Enter');
    await a.keyboard.press('Enter');
    await message(b, '¯').waitFor();
    // A line that starts with a slash and names no command is a message.
    await say(a, '/not-a-command at all');
    await message(b, '/not-a-command at all').waitFor();
  }, a);

  await step('extras: send later, then cancel it from Scheduled', async () => {
    await editor(a).click();
    await a.keyboard.type('See you all on Monday');
    await a.getByRole('button', { name: 'Send later' }).click();
    await a.getByRole('menuitem', { name: 'In 2 hours' }).click();
    await a.getByText(/It will be sent/).waitFor();
    await a.getByRole('link', { name: 'Scheduled', exact: true }).click();
    const item = a.locator('.result', { hasText: 'See you all on Monday' });
    await item.waitFor();
    await shot(a, '71-chat-scheduled');
    await item.getByRole('button', { name: 'Cancel it' }).click();
    await a.getByRole('dialog').getByRole('button', { name: 'Cancel it' }).click();
    await item.waitFor({ state: 'detached' });
    await a.locator('.side-row').filter({ hasText: /^general/ }).first().getByRole('link').click();
  }, a);

  await step('extras: /form opens the builder; the form is answered in a dialog', async () => {
    await say(a, '/form');
    const dlg = a.getByRole('dialog');
    await dlg.getByPlaceholder('What is this form for?').fill('Shift swap');
    await dlg.getByLabel('Question').fill('Which day?');
    await dlg.getByRole('button', { name: 'Add a question' }).click();
    await dlg.getByLabel('Question').nth(1).fill('Why?');
    await dlg.getByLabel('Kind of answer').nth(1).selectOption('longtext');
    await shot(a, '72-chat-form-builder', { fullPage: false });
    await dlg.getByRole('button', { name: 'Post the form' }).click();
    const form = b.locator('#main .form', { hasText: 'Shift swap' });
    await form.getByRole('button', { name: 'Answer' }).click();
    await b.getByRole('dialog').getByLabel('Which day?').fill('Thursday');
    await b.getByRole('dialog').getByLabel('Why?').fill('Dentist');
    await b.getByRole('dialog').getByRole('button', { name: 'Send' }).click();
    await form.getByText('1 answer').waitFor();
    await form.getByText('Thursday').waitFor();
    // The author reads the answers; nobody else is shown them.
    const mine = a.locator('#main .form', { hasText: 'Shift swap' });
    await mine.getByText('1 answer').waitFor();
    await mine.getByRole('button', { name: 'Results' }).click();
    await a.getByRole('dialog').getByRole('cell', { name: 'Dentist' }).waitFor();
    await a.getByRole('dialog').getByRole('link', { name: 'Download as CSV' }).waitFor();
    await a.getByRole('dialog').getByRole('button', { name: 'Close' }).last().click();
  }, a);

  await step('extras: a new chat appears under Chats for both, with presence', async () => {
    await a.getByRole('button', { name: 'New…' }).click();
    await a.getByRole('menuitem', { name: 'New chat' }).click();
    await a.getByRole('dialog').getByRole('button', { name: /Sam Okafor/ }).click();
    await a.getByRole('button', { name: 'Open the chat' }).click();
    await a.getByRole('heading', { name: /Sam Okafor/, level: 1 }).waitFor();
    await say(a, 'Just us two here');
    const row = b.locator('.side-group', { hasText: 'Chats' }).locator('.side-row', { hasText: 'Kaity' });
    await row.waitFor();
    await row.locator('.presence').waitFor();
    // An ad-hoc channel starts at "any activity", so it badges.
    await row.locator('.badge', { hasText: '1' }).waitFor();
    await row.getByRole('link').click();
    await message(b, 'Just us two here').waitFor();
    await shot(b, '73-chat-direct');
  }, a);
}
