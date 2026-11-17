// The web chat, driven by two people in two browser contexts against one
// server: posting live, threads in both views with the marker, a poll, a
// checklist, a file, a reaction, search, the sub-channel tree, the "show"
// setting, a channel link from the picker, and catching up after the live
// connection was away.

import { step, shot, noSidewaysScroll, assert, countReels, reels, PHONE, DESKTOP, SHOTS } from './lib.mjs';

// watchChance follows, frame by frame, the next live result of a kind on a
// page (ruling R87): whether its total was ever seen while its dice
// rolled, the order its cards came face up (and in a tarot reading the
// order they were marked as just landed), and whether a reading's summary
// showed before its last card.
async function watchChance(page, kind) {
  await page.evaluate((k) => {
    const before = new Set(document.querySelectorAll(`#main .chance[data-kind="${k}"]`));
    const w = window.__chance = { frames: 0, rolling: 0, totalWhileRolling: false, firstDown: null, upOrder: [], justOrder: [], early: [], done: false };
    const seen = (el) => !!el && el.checkVisibility({ opacityProperty: true, visibilityProperty: true });
    const look = () => {
      const el = [...document.querySelectorAll(`#main .chance[data-kind="${k}"]`)].find((e) => !before.has(e));
      if (el) {
        w.frames++;
        const playing = el.classList.contains('spinning');
        if (playing && k === 'roll') { w.rolling++; if (seen(el.querySelector('.chance-total'))) w.totalWhileRolling = true; }
        const cards = [...el.querySelectorAll('.slot.card')];
        const down = cards.map((c) => c.classList.contains('down'));
        if (w.firstDown == null) w.firstDown = down;
        down.forEach((d, i) => { if (!d && !w.upOrder.includes(i)) w.upOrder.push(i); });
        cards.forEach((c, i) => { if (c.classList.contains('just') && w.justOrder[w.justOrder.length - 1] !== i) w.justOrder.push(i); });
        if (down.some(Boolean)) el.querySelectorAll('.chance-after').forEach((s) => { if (seen(s)) w.early.push('summary'); });
        if (!playing && w.frames > 1) { w.done = true; return; }
      }
      requestAnimationFrame(look);
    };
    look();
  }, kind);
}
const chanceWatched = async (page) => { await page.waitForFunction(() => window.__chance.done, null, { timeout: 8000 }); return page.evaluate(() => window.__chance); };
import { fileURLToPath } from 'node:url';
import { writeFileSync } from 'node:fs';

const IMAGE = fileURLToPath(new URL('../internal/web/static/icons/icon-192.png', import.meta.url));

const editor = (page, thread) => page.locator(thread ? '#thread .editor' : '#main .editor');

// sendTyped types a line and sends it with the Send button: a typed time
// opens the suggestion list, where Enter would pick the suggestion.
async function sendTyped(page, text) {
  await editor(page).click();
  await page.keyboard.type(text);
  await page.locator('#main .send').click();
}

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
    // The empty box shows its placeholder and, focused or not, the
    // browser's own caret: nothing is drawn after it and nothing blinks.
    const drawn = await a.evaluate(() => {
      const ed = document.querySelector('#main .editor');
      const was = document.activeElement;
      ed.blur();
      const out = { empty: ed.classList.contains('empty'), after: getComputedStyle(ed, '::after').content, moving: document.getAnimations().filter((x) => x.effect && x.effect.target === ed).length };
      if (was) was.focus();
      return out;
    });
    assert(drawn.empty && drawn.after === 'none' && drawn.moving === 0, `the empty composer draws no cursor of its own: ${JSON.stringify(drawn)}`);
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

  await step('chat: /poll, /signup, /rsvp and /approve read the parts and flags of FEATURES.md 7.5', async () => {
    // What was posted, as the server stores it.
    const formOf = async (title) => {
      const msg = a.locator('#main .msg[data-id]').filter({ has: a.locator('.form-title', { hasText: title }) }).last();
      await msg.waitFor();
      const id = await msg.getAttribute('data-id');
      return a.evaluate(async (m) => (await (await fetch(`/api/v1/messages/${m}`)).json()).message.form, id);
    };
    const clearBox = async () => { await editor(a).click(); await a.keyboard.press('ControlOrMeta+a'); await a.keyboard.press('Backspace'); };
    await sendTyped(a, '/poll Which days suit you? | Mon | Tue | Wed --multiple --until friday at 17:00');
    const poll = await formOf('Which days suit you?');
    assert(poll && poll.fields[0].multiple === true && poll.fields[0].options.map((o) => o.label).join() === 'Mon,Tue,Wed', `the poll's flags were not read: ${JSON.stringify(poll)}`);
    const friday = await a.evaluate((at) => { const d = new Date(); do d.setDate(d.getDate() + 1); while (d.getDay() !== 5); d.setHours(17, 0, 0, 0); return d.getTime() === new Date(at).getTime(); }, poll.closes_at);
    assert(friday, `--until friday at 17:00 closes at ${poll.closes_at}`);
    await say(a, '/signup Bring food | Salad | Drinks (3)');
    const signup = await formOf('Bring food');
    const slots = signup && signup.fields[0].options.map((o) => [o.label, o.max || null]);
    assert(JSON.stringify(slots) === JSON.stringify([['Salad', null], ['Drinks', 3]]), `the slots: ${JSON.stringify(slots)}`);
    await sendTyped(a, '/rsvp Team dinner | tomorrow at 19:30');
    const rsvp = await formOf('Team dinner');
    const tomorrow = await a.evaluate((at) => { const d = new Date(); d.setDate(d.getDate() + 1); d.setHours(19, 30, 0, 0); return d.getTime() === new Date(at).getTime(); }, rsvp && rsvp.closes_at);
    assert(tomorrow && rsvp.fields[0].options.map((o) => o.id).join() === 'going,maybe,cant', `the RSVP: ${JSON.stringify(rsvp)}`);
    await a.locator('#main .form', { hasText: 'Team dinner' }).locator('.form-when time').waitFor();
    // A mistake is said before anything is sent.
    await say(a, '/poll Lunch? | Noodles | Tacos --hidden');
    await a.locator('.toast.error', { hasText: 'does not know the flag --hidden' }).waitFor();
    await clearBox();
    await say(a, '/rsvp Retro | whenever suits');
    await a.locator('.toast.error', { hasText: 'as a time' }).waitFor();
    await clearBox();
    await say(a, '/approve Ship it | @nobody-here');
    await a.locator('.toast.error', { hasText: 'No group @nobody-here here.' }).waitFor();
    await clearBox();
    // The group part names who may answer.
    const group = await a.evaluate(async () => (await (await fetch('/api/v1/admin/groups', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ handle: 'shippers', name: 'Shippers' }) })).json()).group);
    assert(group && group.id, 'the group was not made');
    await a.reload({ waitUntil: 'domcontentloaded' });
    await editor(a).waitFor();
    // The group's handle ends the line, so the composer offers @shippers
    // to complete; Escape keeps it as typed, and Enter then sends.
    await editor(a).click();
    await a.keyboard.type('/approve Ship it | @shippers');
    await a.getByRole('listbox', { name: 'People and groups' }).waitFor();
    await a.keyboard.press('Escape');
    await a.keyboard.press('Enter');
    const approve = await formOf('Ship it');
    assert(approve && approve.answerer_group_id === group.id, `the approval's group: ${JSON.stringify(approve)}`);
  }, a);

  await step('chat: /me is drawn after the name, and /roll is rolled by the server and lands on its result', async () => {
    await say(a, '/me waves at everyone');
    for (const p of [a, b]) {
      const act = p.locator('#main .msg-action').filter({ hasText: 'waves at everyone' }).last();
      await act.locator('.action-name', { hasText: 'Kaity' }).waitFor();
      assert(!(await p.locator('#main .msg').filter({ hasText: '/me waves' }).count()), 'the command itself was not posted');
    }
    // Each side counts the reels that start on screen: a spin starts when
    // a result in the conversation turns while none was turning.
    for (const p of [a, b]) await countReels(p, '#main .chance.spinning');
    await watchChance(b, 'roll');
    await say(a, '/roll 4d6kh3+2');
    // Both see the same result: the server's, stored on the message.
    const res = (p) => p.locator('#main .chance[data-kind="roll"]').last();
    await res(b).waitFor();
    await res(a).waitFor();
    // The sender sees the reels turn once as the message first appears,
    // and so does everyone else on its live arrival; nothing turns again
    // after the spin has ended.
    for (const p of [a, b]) await p.waitForFunction(() => window.__reels.starts > 0, null, { timeout: 5000 }).catch(() => {});
    await a.waitForTimeout(2500);
    assert(await reels(a) === 1, `the sender saw the reels turn once (${await reels(a)})`);
    assert(await reels(b) === 1, `the other person saw the reels turn once (${await reels(b)})`);
    // The total shows only once the dice have stopped (ruling R87).
    const rolled = await chanceWatched(b);
    assert(rolled.rolling > 5 && !rolled.totalWhileRolling, `the total waits for the dice: ${JSON.stringify(rolled)}`);
    assert(await res(b).locator('.chance-total').isVisible(), 'the total shows once the dice stop');
    // Pressing the result does nothing: it does not roll again.
    await res(b).click();
    await b.waitForTimeout(300);
    assert(await reels(b) === 1 && !(await res(b).evaluate((el) => el.classList.contains('spinning'))), 'a press does not roll the dice again');
    // Loaded again from history, the result is drawn still.
    await b.reload({ waitUntil: 'domcontentloaded' });
    const turnedOnLoad = await b.evaluate(() => new Promise((done) => {
      const end = performance.now() + 4000;
      const look = () => { if (document.querySelector('#main .chance.spinning')) done(true); else if (performance.now() > end) done(false); else requestAnimationFrame(look); };
      look();
    }));
    assert(!turnedOnLoad, 'a result loaded from history does not spin');
    await b.locator('#main .chance[data-kind="roll"]').last().waitFor();
    await b.locator('.conn.live').waitFor();
    // The reels stop on the server's values: wait for the spin to end.
    await b.waitForFunction(() => !document.querySelector('#main .chance.spinning'), null, { timeout: 5000 });
    await a.waitForFunction(() => !document.querySelector('#main .chance.spinning'), null, { timeout: 5000 });
    const faces = async (p) => res(p).locator('.slot-face').allTextContents();
    const fa = await faces(a), fb = await faces(b);
    assert(fa.length === 5 && JSON.stringify(fa) === JSON.stringify(fb), `both see the same dice: ${fa} / ${fb}`);
    const id = await res(b).evaluate((el) => el.closest('.msg').dataset.id);
    const m = (await b.request.get(origin + '/api/v1/messages/' + id)).ok() ? (await (await b.request.get(origin + '/api/v1/messages/' + id)).json()).message : null;
    assert(m && m.chance && m.chance.dice === '4d6kh3+2', 'the message carries the server\'s roll');
    const row = m.chance.rows[0];
    const want = [...row.terms[0].rolls.map(String), String(row.terms[1].value)];
    assert(JSON.stringify(fa) === JSON.stringify(want), `each reel landed on the server's value: ${fa} / ${want}`);
    assert((await res(a).locator('.chance-total strong').textContent()) === String(row.total), 'the total is the server\'s');
    assert((await res(a).locator('.slot.dropped').count()) === 1, 'one die is dropped');
    // Rows: /roll 4d4+d6,3d10k2 is two lines, the second with its lowest
    // die greyed; /dice 4+3 is four d6 and 3.
    await say(a, '/roll 4d4+d6,3d10k2');
    await a.waitForFunction(() => [...document.querySelectorAll('#main .chance[data-kind="roll"]')].pop().querySelectorAll('.chance-slots').length === 2, null, { timeout: 5000 });
    await a.waitForFunction(() => !document.querySelector('#main .chance.spinning'), null, { timeout: 5000 });
    const rows = await res(a).evaluate((el) => [...el.querySelectorAll('.chance-slots')].map((l) => ({
      dice: l.querySelector('.chance-row-dice').textContent,
      slots: l.querySelectorAll('.slot').length,
      dropped: l.querySelectorAll('.slot.dropped').length,
      total: l.querySelector('.chance-total strong').textContent,
    })));
    assert(rows.length === 2 && rows[0].dice === '4d4+1d6' && rows[0].slots === 5 && rows[0].dropped === 0 && rows[1].dice === '3d10kh2' && rows[1].slots === 3 && rows[1].dropped === 1, `two rows: ${JSON.stringify(rows)}`);
    await say(a, '/dice 4+3');
    await a.waitForFunction(() => { const el = [...document.querySelectorAll('#main .chance[data-kind="roll"]')].pop(); return el && el.querySelector('.chance-title').textContent === 'Rolled 4d6+3'; }, null, { timeout: 5000 });
    await a.waitForFunction(() => !document.querySelector('#main .chance.spinning'), null, { timeout: 5000 });
    const d6 = await res(a).evaluate((el) => ({
      cubes: [...el.querySelectorAll('.slot-face.art > svg use')].map((u) => u.getAttribute('href').split('#')[1]),
      still: [...el.querySelectorAll('.slot.still')].map((s) => s.textContent),
    }));
    assert(d6.cubes.length === 4 && d6.cubes.every((f) => /^d6-[1-6]$/.test(f)) && d6.still.join() === '+3', `/dice 4+3 is four d6 and +3: ${JSON.stringify(d6)}`);
    // A mistake is said before anything is sent. (The server's refusals
    // of dice are covered by the Go tests: a 400 here would be logged.)
    await say(a, '/pick tea');
    await a.locator('.toast.error', { hasText: 'at least two options' }).waitFor();
    // The text stays in the box to be put right; here it is cleared.
    assert((await editor(a).textContent()).includes('/pick tea'), 'the mistaken command stays in the box');
    await editor(a).click();
    await a.keyboard.press('ControlOrMeta+a');
    await a.keyboard.press('Backspace');
    await shot(b, '45-chat-me-roll');
  }, a);

  await step('chat: /draw shows its cards from the artwork sprite, and text when the sprite cannot load', async () => {
    await say(a, '/roll 1d20+2d6');
    // Wait for this roll itself before the next command.
    await a.locator('#main .chance[data-kind="roll"]').filter({ hasText: 'Rolled 1d20+2d6' }).last().waitFor();
    await watchChance(a, 'draw');
    await say(a, '/draw 3');
    const last = (p) => p.locator('#main .chance[data-kind="draw"]').last();
    await last(a).waitFor();
    // The cards arrive face down and are turned over one after another.
    const drawn = await chanceWatched(a);
    assert(JSON.stringify(drawn.firstDown) === '[true,true,true]' && drawn.upOrder.join() === '0,1,2', `face down, then turned over in order: ${JSON.stringify(drawn)}`);
    // Each card is an <svg> using its symbol in the French sprite; the
    // card's words stay in the element, and no names are listed below.
    await a.waitForFunction(() => {
      const r = [...document.querySelectorAll('#main .chance[data-kind="draw"]')].pop();
      return r && r.querySelectorAll('.slot-face.art > svg use').length === 3;
    }, null, { timeout: 8000 });
    // Read the faces once the live reels have stopped.
    await a.waitForFunction(() => !document.querySelector('#main .chance.spinning'), null, { timeout: 5000 });
    const hrefs = await last(a).locator('.slot-face.art > svg use').evaluateAll((us) => us.map((u) => u.getAttribute('href')));
    assert(hrefs.every((h) => /^\/shared\/chance\/french\.svg#(spades|hearts|diamonds|clubs)-(ace|[2-9]|10|jack|queen|king)$/.test(h)), `the cards use the sprite: ${hrefs}`);
    const box = await last(a).locator('.slot-face.art').first().boundingBox();
    assert(box && Math.round(box.width) === 88 && Math.round(box.height) === 123, `a card is drawn at 88 x 123: ${JSON.stringify(box)}`);
    const words = await last(a).locator('.slot-face').allTextContents();
    assert(words.length === 3 && words.every((w) => /^(A|[2-9]|10|J|Q|K)[♠♥♦♣]$/.test(w)), `each card keeps its words: ${words}`);
    assert((await last(a).locator('.chance-names').count()) === 0, 'no names are listed');
    // Its name pops up on hover, and is the card's label.
    const first = last(a).locator('.slot.card').first();
    const label = await first.getAttribute('aria-label');
    assert(/^(Ace|Two|Three|Four|Five|Six|Seven|Eight|Nine|Ten|Jack|Queen|King) of (spades|hearts|diamonds|clubs)$/.test(label), `the card is labelled with its name: ${label}`);
    assert(!(await first.locator('.chance-tip').isVisible()), 'the name is not shown until asked for');
    await first.hover();
    await first.locator('.chance-tip').waitFor({ state: 'visible' });
    assert((await first.locator('.chance-tip').textContent()) === label, 'hover shows the card\'s name');
    await shot(a, '45-chat-draw-hover', { fullPage: false });
    await a.mouse.move(0, 0);
    await first.locator('.chance-tip').waitFor({ state: 'hidden' });
    // A click shows it too, and turns nothing over again.
    await first.click();
    await first.locator('.chance-tip').waitFor({ state: 'visible' });
    assert(!(await last(a).evaluate((el) => el.classList.contains('spinning') || !!el.querySelector('.slot.down'))), 'a click does not turn the cards again');
    await a.keyboard.press('Escape');
    await a.mouse.click(5, 5);
    // The sprite draws: the card is not blank where the picture is.
    const painted = await last(a).locator('.slot-face.art').first().screenshot();
    assert(painted.length > 1500, 'the card is drawn');
    // In a page where the sprite cannot be fetched, the same result is text.
    const c = await a.context().newPage();
    await c.route('**/shared/chance/french.svg', (r) => r.abort());
    await c.goto(a.url(), { waitUntil: 'domcontentloaded' });
    await last(c).waitFor();
    await c.waitForTimeout(1500);
    assert((await last(c).locator('.slot-face.art').count()) === 0, 'without the sprite no card is drawn as a picture');
    const plain = await last(c).locator('.slot-face').allTextContents();
    assert(JSON.stringify(plain) === JSON.stringify(words), `without the sprite the cards are text: ${plain}`);
    await c.close();
    // A d20 is a 20-cornered polygon drawn on the page with its number in
    // it; a six-sided die is a cube face from its sprite.
    const roll = a.locator('#main .chance[data-kind="roll"]').filter({ hasText: 'Rolled 1d20+2d6' }).last();
    await a.waitForFunction(() => !document.querySelector('#main .chance.spinning'), null, { timeout: 5000 });
    const dice = await roll.locator('.slot-face.art > svg use').evaluateAll((us) => us.map((u) => u.getAttribute('href').split('#')[1]));
    assert(dice.length === 2 && dice.every((d) => /^d6-[1-6]$/.test(d)), `the d6s use their cube faces: ${dice}`);
    const d20 = await roll.locator('.slot-face.art-die').first().evaluate((f) => {
      const poly = f.querySelector('svg polygon');
      const own = [...f.childNodes].filter((n) => n.nodeType === 3).map((n) => n.data).join('');
      return { corners: poly ? poly.getAttribute('points').trim().split(/\s+/).length : 0, number: f.querySelector('svg text').textContent, words: own };
    });
    const rolled = d20.words;
    assert(d20.corners === 20, `the d20 is a 20-cornered polygon: ${JSON.stringify(d20)}`);
    assert(d20.number === rolled && /^([1-9]|1[0-9]|20)$/.test(rolled), `the d20 shows its number: ${JSON.stringify(d20)} ${rolled}`);
    // A flip is a coin, its word kept for screen readers.
    await say(a, '/flip');
    const flip = a.locator('#main .chance[data-kind="flip"]').last();
    await flip.waitFor();
    await a.waitForFunction(() => !document.querySelector('#main .chance.spinning'), null, { timeout: 5000 });
    const coin = await flip.locator('.slot-face.art-die').evaluate((f) => ({ letter: f.querySelector('svg text').textContent, circles: f.querySelectorAll('svg circle').length, words: [...f.childNodes].filter((n) => n.nodeType === 3).map((n) => n.data).join('') }));
    assert(coin.circles === 2 && ((coin.letter === 'H' && coin.words === 'Heads') || (coin.letter === 'T' && coin.words === 'Tails')), `the flip is a coin: ${JSON.stringify(coin)}`);
    await shot(a, '45-chat-draw-roll', { fullPage: false });
    // A tarot reading and mahjong tiles draw from their own sprites. The
    // reading's cards are turned over in order, each marked as it lands,
    // and the summary after the last.
    await watchChance(a, 'tarot');
    await say(a, '/tarot');
    await a.locator('#main .chance[data-kind="tarot"]').last().waitFor();
    const read = await chanceWatched(a);
    assert(JSON.stringify(read.firstDown) === '[true,true,true]' && read.upOrder.join() === '0,1,2' && read.justOrder.join() === '0,1,2' && read.early.length === 0, `the reading rolls out card by card: ${JSON.stringify(read)}`);
    await say(a, '/mahjongg 5');
    await a.locator('#main .chance[data-kind="draw"]').filter({ hasText: 'mahjong' }).last().waitFor();
    await a.waitForFunction(() => !document.querySelector('#main .chance.spinning'), null, { timeout: 5000 });
    for (const [kind, set, n] of [['tarot', 'tarot', 3], ['draw', 'mahjong', 5]]) {
      const r = a.locator(`#main .chance[data-kind="${kind}"]`).last();
      await a.waitForFunction(([k, want]) => {
        const el = [...document.querySelectorAll(`#main .chance[data-kind="${k}"]`)].pop();
        return el && el.querySelectorAll('.slot-face.art > svg use').length === want;
      }, [kind, n], { timeout: 8000 });
      const hrefs = await r.locator('.slot-face.art > svg use').evaluateAll((us) => us.map((u) => u.getAttribute('href')));
      assert(hrefs.every((h) => h.startsWith(`/shared/chance/${set}.svg#`)), `${set} uses its sprite: ${hrefs}`);
    }
    // A reversed tarot card's picture is turned upside down; an upright
    // one's is not.
    const turns = await a.locator('#main .chance[data-kind="tarot"]').last().locator('.slot').evaluateAll((ss) => ss.map((s) => ({
      reversed: s.classList.contains('reversed'),
      turned: getComputedStyle(s.querySelector('.art-svg')).transform === 'matrix(-1, 0, 0, -1, 0, 0)',
    })));
    assert(turns.length === 3 && turns.every((t) => t.reversed === t.turned), `reversed cards are turned: ${JSON.stringify(turns)}`);
    await shot(a, '46-chat-tarot-mahjongg', { fullPage: false });
    // A Celtic cross lies as a cross and a staff, the challenge across the
    // present, inside the message; a press on a card opens its details.
    await say(a, '/tarot celtic');
    const cross = a.locator('#main .chance[data-kind="tarot"]').last();
    await cross.locator('.slot.card').nth(9).waitFor();
    await a.waitForFunction(() => !document.querySelector('#main .chance.spinning'), null, { timeout: 8000 });
    const laid = await cross.evaluate((el) => {
      const r = (i) => el.querySelectorAll('.slot.card')[i].getBoundingClientRect();
      const box = el.getBoundingClientRect();
      const cards = [...el.querySelectorAll('.slot.card')].map((c) => c.getBoundingClientRect());
      return {
        across: el.querySelectorAll('.slot.card')[1].classList.contains('cross') && r(1).width > r(1).height && Math.abs((r(0).left + r(0).right) / 2 - (r(1).left + r(1).right) / 2) < 1,
        cross: r(2).right <= r(0).left + 1 && r(3).left >= r(0).right - 1 && r(4).bottom <= r(0).top + 1 && r(5).top >= r(0).bottom - 1,
        staff: [6, 7, 8].every((i) => r(i).top > r(i + 1).top) && [6, 7, 8, 9].every((i) => r(i).left >= r(3).right),
        inside: cards.every((c) => c.left >= box.left - 1 && c.right <= box.right + 1),
        list: el.querySelectorAll('ol, .chance-say').length,
      };
    });
    assert(laid.across && laid.cross && laid.staff && laid.inside && laid.list === 0, `the Celtic cross is laid out: ${JSON.stringify(laid)}`);
    await cross.locator('.slot.card').nth(1).click();
    const detail = cross.locator('.slot.card').nth(1).locator('.chance-detail');
    await detail.waitFor({ state: 'visible' });
    const words = await detail.textContent();
    assert(words.startsWith('Challenge') && /upright|reversed/.test(words), `a press opens the card's details: ${words}`);
    await a.keyboard.press('Escape');
    await detail.waitFor({ state: 'hidden' });
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

  await step('extras: @channel in a channel too large for it is plain text, and an admin is asked first', async () => {
    const general = await a.evaluate(() => location.pathname.split('/c/')[1]);
    const limit = (n) => a.evaluate(async (v) => (await fetch('/api/v1/admin/settings', { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ 'mentions.broad_mention_limit': v }) })).status, n);
    // The admin lowers the server's limit below the size of #general.
    assert(await limit(1) === 200, 'the limit could not be set');
    try {
      // Sam, a member, is told before sending, and the words go as text.
      await b.reload();
      await b.locator('#main .editor').waitFor();
      await b.locator('.conn.live').waitFor();
      await editor(b).click();
      await b.keyboard.type('lunch @channel today?');
      await b.locator('#main .composer-notice.broad', { hasText: 'sent as plain text and will notify nobody' }).waitFor();
      await shot(b, '71-chat-broad-hint');
      await b.keyboard.press('Enter');
      await message(a, 'lunch @channel today?').waitFor();
      assert(await message(a, 'lunch @channel today?').locator('.mention').count() === 0, 'a member\'s @channel above the limit was drawn as a mention');
      await b.locator('#main .composer-notice', { hasText: 'notified nobody' }).waitFor();
      assert(await b.locator('#main .composer-notice.broad').isHidden(), 'the hint stayed after the text was sent');

      // Kaity, an admin, may. With few people nothing is asked.
      await say(a, 'standup @channel in five');
      await message(b, 'standup @channel in five').locator('.mention.broadcast', { hasText: '@channel' }).waitFor();
      assert(await a.locator('#main .composer-notice.broad').isHidden(), 'an admin was shown the hint');

      // With many, she is asked, with the number the server gave. The
      // server's answer is stood in for: #general has three members.
      await a.route('**/broad-mentions/check', (route) => route.fulfill({ json: { broad: true, allowed: true, recipients: 4200, confirm: true } }));
      await say(a, 'fire drill @here now');
      const ask = a.getByRole('dialog').filter({ hasText: 'This will notify 4,200 people.' });
      await ask.waitFor();
      await shot(a, '72-chat-broad-confirm');
      await ask.getByRole('button', { name: 'Cancel' }).click();
      await ask.waitFor({ state: 'detached' });
      assert((await editor(a).innerText()).includes('fire drill @here now'), 'cancelling lost the text');
      assert(await message(b, 'fire drill @here now').count() === 0, 'a cancelled message was sent');
      await editor(a).click();
      await a.keyboard.press('Enter');
      await a.getByRole('dialog').getByRole('button', { name: 'Send' }).click();
      await message(b, 'fire drill @here now').locator('.mention.broadcast', { hasText: '@here' }).waitFor();
      await a.unroute('**/broad-mentions/check');

      // Sam chooses not to be notified by them here, beside the mode:
      // the server then counts one person fewer for Kaity's @channel.
      const reach = () => a.evaluate(async (id) => (await (await fetch(`/api/v1/conversations/${id}/broad-mentions/check`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: '@channel' }) })).json()).recipients, general);
      const before = await reach();
      await b.getByRole('button', { name: 'Conversation menu' }).click();
      await b.getByRole('menuitem', { name: 'Notifications and sidebar…' }).click();
      await b.getByRole('checkbox', { name: /Ignore @channel, @here and @everyone/ }).check();
      await shot(b, '73-chat-broad-ignore', { fullPage: false });
      await b.getByRole('button', { name: 'Done' }).click();
      const after = await reach();
      assert(after === before - 1, `ignoring broad mentions changed the reach from ${before} to ${after}`);
      await b.getByRole('button', { name: 'Conversation menu' }).click();
      await b.getByRole('menuitem', { name: 'Notifications and sidebar…' }).click();
      await b.getByRole('checkbox', { name: /Ignore @channel, @here and @everyone/ }).uncheck();
      await b.getByRole('button', { name: 'Done' }).click();
      assert(await reach() === before, 'listening again did not restore the reach');
    } finally {
      await limit(250);
    }
  }, a);

  await step('extras: a community saved reply named like a built-in runs in its place', async () => {
    await editor(a).click();
    await a.keyboard.type('/shrug');
    // One /shrug in the menu: the community's, not the built-in.
    const options = a.getByRole('listbox', { name: 'Commands' }).getByRole('option').filter({ hasText: /^\/shrug/ });
    await options.first().waitFor();
    assert(await options.count() === 1, 'the built-in /shrug is still offered beside the saved reply');
    assert(/saved reply/.test(await options.first().innerText()), 'the /shrug offered is not the saved reply');
    await a.keyboard.press('Enter');
    await a.keyboard.press('Enter');
    await message(b, 'the house shrug').waitFor();
    // A line that starts with a slash and names no command is a message.
    await say(a, '/not-a-command at all');
    await message(b, '/not-a-command at all').waitFor();
  }, a);

  await step('extras: /ban keeps someone out of one channel until /unban', async () => {
    const me = await b.evaluate(async () => (await (await fetch('/api/v1/me')).json()).account);
    const room = await a.evaluate(async (id) => {
      const res = await fetch('/api/v1/channels', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'ban-room', private: false, account_ids: [id] }) });
      return (await res.json()).conversation.id;
    }, me.id);
    // Sam is invited (ruling R103) and accepts.
    const accepted = await b.evaluate(async (id) => {
      const r = (await (await fetch('/api/v1/me/chat-requests')).json()).requests.find((x) => x.conversation_id === id);
      return r ? (await fetch(`/api/v1/me/chat-requests/${r.id}/accept`, { method: 'POST' })).status : 0;
    }, room);
    assert(accepted === 200, 'sam did not accept the invitation to ban-room: ' + accepted);
    await a.goto(origin + '/chat/c/' + room);
    await a.locator('#main .editor').waitFor();
    await b.locator('.side-row').filter({ hasText: 'ban-room' }).waitFor();
    // Asked first, naming them and the channel; Cancel bans nobody.
    await say(a, `/ban @${me.username} spamming`);
    let ask = a.getByRole('dialog').filter({ hasText: 'from ban-room?' });
    await ask.getByText('kept from coming back').waitFor();
    assert(await ask.getByText(new RegExp(`^Ban .+ from ban-room\\?$`)).count() === 1, 'the question does not name the person and the channel');
    await ask.getByRole('button', { name: 'Cancel' }).click();
    await ask.waitFor({ state: 'detached' });
    const none = await a.evaluate(async (id) => (await (await fetch(`/api/v1/conversations/${id}/bans`)).json()).bans.length, room);
    assert(none === 0, 'Cancel still banned them');
    // The command stays in the box, to send again.
    assert((await editor(a).innerText()).includes('/ban @'), 'Cancel emptied the box');
    await editor(a).click();
    await a.keyboard.press('Enter');
    ask = a.getByRole('dialog').filter({ hasText: 'from ban-room?' });
    await ask.getByRole('button', { name: 'Ban', exact: true }).click();
    await a.getByText('is banned from this channel').waitFor();
    // Told once, plainly, and the channel is gone from the sidebar.
    await b.getByText('You were banned from #ban-room: “spamming”').waitFor();
    await b.locator('.side-row').filter({ hasText: 'ban-room' }).waitFor({ state: 'detached' });
    // The channel browser no longer shows it to them.
    const listed = await b.evaluate(async () => (await (await fetch('/api/v1/channels')).json()).conversations.map((c) => c.name));
    assert(!listed.includes('ban-room'), 'the browser still shows a banned member the channel');
    // The managers see who banned whom, and when.
    await a.getByRole('button', { name: 'Conversation menu' }).click();
    await a.getByRole('menuitem', { name: 'Settings', exact: true }).click();
    await a.getByRole('tab', { name: 'Bans' }).click();
    await a.getByRole('dialog').getByText('spamming').waitFor();
    await a.keyboard.press('Escape');
    await say(a, `/unban @${me.username}`);
    await a.getByText('may come back to this channel').waitFor();
    const again = await b.evaluate(async (id) => (await fetch(`/api/v1/conversations/${id}/join`, { method: 'POST' })).status, room);
    assert(again === 200, `joining after /unban answered ${again}`);
  }, a);

  await step('extras: /remind reads its time and target, and opens the form only when nothing is typed', async () => {
    const reminders = () => a.evaluate(async () => (await (await fetch('/api/v1/scheduled?kind=reminder&limit=50')).json()).scheduled);
    const find = async (text) => {
      for (let i = 0; i < 50; i++) {
        const r = (await reminders()).find((x) => x.content && x.content.text === text && !x.cancelled_at);
        if (r) return r;
        await a.waitForTimeout(100);
      }
      return null;
    };
    // Times are read in the account's time zone, not the browser's: give
    // the account one far from the browser's and reload so the page has it.
    const browserZone = await a.evaluate(() => Intl.DateTimeFormat().resolvedOptions().timeZone);
    const zone = browserZone === 'Pacific/Kiritimati' ? 'America/St_Johns' : 'Pacific/Kiritimati';
    const setZone = (z) => a.evaluate(async (v) => (await fetch('/api/v1/me', { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ time_zone: v }) })).status, z);
    const oldZone = await a.evaluate(async () => (await (await fetch('/api/v1/me')).json()).account.time_zone || '');
    assert(await setZone(zone) === 200, 'the account time zone was not set');
    await a.reload();
    await a.locator('.conn.live').waitFor();
    await editor(a).waitFor();
    await sendTyped(a, '/remind tomorrow at 9 | Water the plants');
    await a.locator('.toast', { hasText: 'Reminder set for' }).waitFor();
    const once = await find('Water the plants');
    const wall = (at) => a.evaluate(([t, z]) => new Intl.DateTimeFormat('en-GB', { timeZone: z, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(new Date(t)), [at, zone]);
    const nine = once && await wall(once.next_run_at || once.run_at);
    assert(nine === '09:00', `tomorrow at 9 was not read in ${zone}: ${nine} ${JSON.stringify(once)}`);
    // A time with its own zone is that exact moment.
    await sendTyped(a, '/remind 2031-06-01 14:30 Europe/Paris | Call Paris');
    await a.locator('.toast', { hasText: 'Reminder set for' }).last().waitFor();
    const paris = await find('Call Paris');
    assert(paris && new Date(paris.next_run_at || paris.run_at).getTime() === Date.parse('2031-06-01T12:30:00Z'), `the instant was not exact: ${JSON.stringify(paris)}`);
    // It goes to the person's own one-person channel, not here.
    const here = await a.evaluate(() => location.pathname.split('/c/')[1].split('/')[0]);
    assert(once.conversation_id !== here, 'the reminder went to this conversation, not to the person');
    await sendTyped(a, '/remind every weekday at 09:30 | Stand-up');
    await a.locator('.toast', { hasText: 'every weekday at 09:30' }).waitFor();
    const rep = await find('Stand-up');
    assert(rep && rep.repeat && rep.repeat.every === 'day' && rep.repeat.days === '1111100' && rep.repeat.at === '09:30' && rep.repeat.time_zone === zone, `the repeat: ${JSON.stringify(rep)}`);
    // A mistake is said before anything is sent, and the text stays.
    await say(a, '/remind tomorrow');
    await a.locator('.toast.error', { hasText: 'Say what to be reminded of' }).waitFor();
    assert((await editor(a).textContent()).includes('/remind tomorrow'), 'the mistaken /remind left the box');
    await editor(a).click();
    await a.keyboard.press('ControlOrMeta+a');
    await a.keyboard.press('Backspace');
    // With nothing after it, the form.
    await say(a, '/remind');
    await a.getByRole('dialog').filter({ hasText: 'Set a reminder' }).waitFor();
    await a.keyboard.press('Escape');
    for (const r of [once, paris, rep]) await a.evaluate((id) => fetch(`/api/v1/scheduled/${id}`, { method: 'DELETE' }), r.id);
    await setZone(oldZone);
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

  let bulletins = '';
  await step('extras: an announcement channel: its managers post, the others read, react and are not shown who is there', async () => {
    await a.getByRole('button', { name: 'New…' }).click();
    await a.getByRole('menuitem', { name: 'New channel' }).click();
    await a.getByLabel('Name').fill('Bulletins');
    await a.getByLabel('Announcement channel').check();
    await a.getByRole('button', { name: 'Create' }).click();
    await a.getByRole('heading', { name: /Bulletins/, level: 1 }).waitFor();
    bulletins = a.url().split('/c/')[1].split('/')[0];
    await say(a, 'The office is closed on Friday');
    await message(a, 'The office is closed on Friday').waitFor();
    // Its manager is shown the switches, set as the channel was made.
    await a.getByRole('button', { name: 'Conversation menu' }).click();
    await a.getByRole('menuitem', { name: 'Settings' }).click();
    await a.getByRole('tab', { name: 'Manage' }).click();
    assert(await a.getByRole('checkbox', { name: 'Announcement channel' }).isChecked(), 'the manager is not shown that this is an announcement channel');
    assert(!(await a.getByRole('checkbox', { name: 'Replies in threads' }).isChecked()), 'replies are on in a new announcement channel');
    await shot(a, '74-chat-announcement-manage', { fullPage: false });
    await a.getByRole('button', { name: 'Done' }).click();

    // Sam joins and reads; the composer is replaced by why he cannot post,
    // there is no members button, and the list is refused him.
    const sam = await b.evaluate(async (id) => {
      await fetch(`/api/v1/conversations/${id}/join`, { method: 'POST' });
      return (await (await fetch('/api/v1/me')).json()).account.id;
    }, bulletins);
    await b.goto(origin + '/chat/c/' + bulletins);
    await message(b, 'The office is closed on Friday').waitFor();
    await b.locator('#main', { hasText: 'only its managers and the people they allow post here' }).first().waitFor();
    assert(await b.locator('#main .editor').isHidden(), 'someone who may not post in an announcement channel was given a composer');
    assert(await b.getByRole('button', { name: /^Members/ }).count() === 0, 'a member of an announcement channel was offered its member list');
    const refused = await b.evaluate(async (id) => (await fetch(`/api/v1/conversations/${id}/members`)).status, bulletins);
    assert(refused === 403, `the member list answered ${refused} to a member`);
    const post = (text) => b.evaluate(async ([id, t]) => (await fetch(`/api/v1/conversations/${id}/messages`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: t }) })).status, [bulletins, text]);
    assert(await post('let me in') === 403, 'a member posted in an announcement channel');
    await shot(b, '75-chat-announcement-reader');
    // He can react.
    const m = message(b, 'The office is closed on Friday');
    await m.hover();
    await m.getByRole('button', { name: 'React' }).click();
    await b.getByRole('searchbox', { name: 'Search emoji' }).fill('thumbs_up');
    await b.locator('.picker-cell').first().click();
    await message(a, 'The office is closed on Friday').locator('.reaction .count', { hasText: '1' }).waitFor();

    // The manager allows him: the composer appears without a reload.
    const allowed = await a.evaluate(async ([id, who]) => (await fetch(`/api/v1/conversations/${id}/members/${who}/may-post`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ may_post: true }) })).status, [bulletins, sam]);
    assert(allowed === 200, `allowing a member to post answered ${allowed}`);
    await b.locator('#main .editor').waitFor();
    await say(b, 'Thanks, noted');
    await message(a, 'Thanks, noted').waitFor();
  }, b);

  await step('extras: a large channel is sent whole to the one looking at it, and the other is told it changed', async () => {
    const set = (body) => a.evaluate(async (v) => (await fetch('/api/v1/admin/settings', { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(v) })).status, body);
    // #general has three members: with both sizes at one it is large.
    assert(await set({ 'channels.large_channel_limit': 1, 'channels.large_feature_limit': 1 }) === 200, 'the sizes could not be set');
    try {
      // Kaity stays in Bulletins; what her connection is sent is kept.
      const frames = [];
      a.on('websocket', (ws) => ws.on('framereceived', (f) => { try { frames.push(JSON.parse(f.payload)); } catch { /* not JSON */ } }));
      await a.reload();
      await a.locator('.conn.live').waitFor();
      await a.getByRole('heading', { name: /Bulletins/, level: 1 }).waitFor();
      // Sam looks at #general, which now shows how many are online, not who.
      await b.goto(origin + '/chat/');
      await b.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').first().click();
      await b.locator('#main .editor').waitFor();
      await b.locator('.conn.live').waitFor();
      await b.locator('.conv-topic .online-count', { hasText: /\d+ online/ }).waitFor();
      const general = b.url().split('/c/')[1].split('/')[0];

      await say(b, 'A word to the big room');
      await message(b, 'A word to the big room').waitFor();
      // Kaity's list shows it as unread; her connection was told that the
      // conversation changed and was not sent the message.
      await a.locator(`.side-row.unread[data-conversation="${general}"]`).waitFor();
      const of = (type) => frames.filter((f) => f.type === type && f.conversation_id === general);
      assert(of('conversation.changed').length >= 1, 'no conversation.changed reached the one who was not looking');
      assert(of('message.created').length === 0, 'a message of a large channel was sent whole to someone not looking at it');
      assert(of('conversation.changed').length <= 2, `one message made ${of('conversation.changed').length} signals`);

      // A mention of her is sent whole at once, inside the signal.
      await editor(b).click();
      await b.keyboard.type('over to @kai');
      await b.getByRole('listbox', { name: 'People and groups' }).getByRole('option').filter({ hasText: 'Kaity' }).waitFor();
      await b.keyboard.press('Enter');
      await b.keyboard.press('Enter');
      await message(b, 'over to').waitFor();
      await a.locator(`.side-row[data-conversation="${general}"] .badge`).waitFor();
      const within = of('conversation.changed').filter((f) => f.event && f.event.type === 'message.created');
      assert(within.length === 1 && within[0].event.notify === 'notify', `the mention did not arrive inside a signal: ${JSON.stringify(of('conversation.changed')).slice(0, 400)}`);
      assert(of('message.created').length === 0, 'the mention was sent as an event of its own');

      // Opening it brings what she missed, in order, and then it is live.
      await a.locator(`.side-row[data-conversation="${general}"]`).getByRole('link').click();
      await message(a, 'A word to the big room').waitFor();
      await message(a, 'over to').waitFor();
      const texts = await a.locator('#main .msg').allInnerTexts();
      const first = texts.findIndex((t) => t.includes('A word to the big room')), second = texts.findIndex((t) => t.includes('over to'));
      assert(first >= 0 && second > first, 'what was missed is not in order');
      assert(texts.filter((t) => t.includes('A word to the big room')).length === 1, 'a missed message is shown twice');
      await say(b, 'and now you are here');
      await message(a, 'and now you are here').waitFor();
      assert(of('message.created').some((f) => f.message && f.message.text === 'and now you are here'), 'the one looking was not sent the message whole');
      await a.locator('.conv-topic .online-count', { hasText: /\d+ online/ }).waitFor();
      await shot(a, '76-chat-large-channel');
    } finally {
      await set({ 'channels.large_channel_limit': 1000, 'channels.large_feature_limit': 50 });
    }
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

  // Ruling R74.
  await step('extras: /help lists the commands and the things people can type, and searches', async () => {
    await say(a, '/help');
    const dlg = a.getByRole('dialog', { name: 'Commands' });
    await dlg.getByRole('heading', { name: 'Posting' }).waitFor();
    // The admin sees the broadcast; the aliases are named beside their command.
    await dlg.getByRole('button', { name: /^\/broadcast/ }).waitFor();
    await dlg.getByText('also /kickban, /kb').waitFor();
    // The last section: what people can type, each row opening to an example.
    const things = dlg.getByRole('heading', { name: 'Things you can type' });
    await things.waitFor();
    for (const row of ['People and groups', 'Everyone at once', 'Channel links', 'Dates and times', 'References', 'Measurements', 'Colours', 'Map coordinates', 'Dice in a sentence']) {
      await dlg.locator('details.help-thing summary', { hasText: row }).waitFor();
    }
    await dlg.getByRole('heading', { name: 'Formatting' }).waitFor();
    await shot(a, '76-chat-help', { fullPage: false, phone: true });
    await dlg.getByRole('link', { name: 'Jump to things you can type' }).click();
    const refs = dlg.locator('details.help-thing', { hasText: 'References' });
    await refs.locator('summary').focus();
    await a.keyboard.press('Enter');
    // The server-wide pattern the objects flow set up, with an example.
    await refs.getByRole('heading', { name: 'Server-wide patterns' }).waitFor();
    await refs.getByText('PROJ-123 links to tracker.example').waitFor();
    const units = dlg.locator('details.help-thing', { hasText: 'Measurements' });
    await units.locator('summary').click();
    await units.getByText('lbs').first().waitFor();
    const dice = dlg.locator('details.help-thing', { hasText: 'Dice in a sentence' });
    await dice.locator('summary').click();
    await dice.getByText('I hit for [[2d6+3]] damage').waitFor();
    await dice.getByText('Not in sealed chats: the text stays as typed.').waitFor();
    await shot(a, '76-chat-help-things', { fullPage: false, phone: true });
    for (const size of [PHONE, DESKTOP]) {
      await a.setViewportSize(size);
      const over = await dlg.locator('.dialog-body').evaluate((el) => el.scrollWidth - el.clientWidth);
      assert(over <= 0, `the help scrolls sideways by ${over}px at ${size.width}px`);
      await noSidewaysScroll(a);
    }
    await dlg.getByRole('searchbox', { name: 'Search commands' }).fill('trout');
    assert(await dlg.getByRole('button', { name: /^\/ban\b/ }).count() === 0, 'the search did not narrow the list');
    // Choosing a command shows its entry; from there it goes in the box.
    await dlg.getByRole('button', { name: /^\/slap/ }).click();
    await dlg.getByRole('heading', { name: '/slap', level: 3 }).waitFor();
    await dlg.getByText('One of the fun commands').waitFor();
    await dlg.getByRole('button', { name: 'Put /slap in the message box' }).click();
    await dlg.waitFor({ state: 'detached' });
    try {
      await a.waitForFunction(() => document.querySelector('#main .editor').innerText.trim() === '/slap', null, { timeout: 4000 });
    } catch {
      assert(false, `the box holds ${JSON.stringify(await editor(a).innerText())} after /help`);
    }
    await a.keyboard.press('ControlOrMeta+a');
    await a.keyboard.press('Backspace');
  }, a);

  await step('extras: /help roll shows the full entry, and a hidden command has none', async () => {
    await say(a, '/help roll');
    const dlg = a.getByRole('dialog', { name: 'Commands' });
    await dlg.getByRole('heading', { name: '/roll', level: 3 }).waitFor();
    await dlg.getByText('/roll [dice]', { exact: true }).waitFor();
    await dlg.getByText('/roll 4d6kh3', { exact: true }).waitFor();
    await dlg.getByText('Rolls four six-sided dice and keeps the highest three; the lowest is shown struck through.').waitFor();
    await dlg.getByText('Not in sealed chats.', { exact: false }).first().waitFor();
    await shot(a, '76-chat-help-roll', { fullPage: false, phone: true });
    // A related command opens its own entry; back goes to the list.
    await dlg.getByRole('button', { name: '/flip', exact: true }).click();
    await dlg.getByRole('heading', { name: '/flip', level: 3 }).waitFor();
    await dlg.getByRole('button', { name: 'All commands' }).click();
    await dlg.getByRole('heading', { name: 'Posting' }).waitFor();
    await a.keyboard.press('Escape');
    await dlg.waitFor({ state: 'detached' });
    // /help draw lists every deck from the registry, with its count.
    await say(a, '/help draw');
    await dlg.getByRole('heading', { name: '/draw', level: 3 }).waitFor();
    await dlg.locator('.help-more li', { hasText: 'hanafuda' }).getByText('48 cards').waitFor();
    await a.keyboard.press('Escape');
    await dlg.waitFor({ state: 'detached' });
    // Someone who is not an admin is not offered the broadcast or the
    // exile at all: no entry, nothing found, and /ban does not name it.
    for (const word of ['broadcast', 'exile', 'kline']) {
      await say(b, '/help ' + word);
      const theirs = b.getByRole('dialog', { name: 'Commands' });
      await theirs.getByText('No command matches.').waitFor();
      assert(await theirs.getByRole('heading', { name: '/' + word, level: 3 }).count() === 0, `/help ${word} showed an entry to someone who is not an admin`);
      await b.keyboard.press('Escape');
      await theirs.waitFor({ state: 'detached' });
    }
    await say(b, '/help kb');
    const theirs = b.getByRole('dialog', { name: 'Commands' });
    await theirs.getByRole('heading', { name: '/ban', level: 3 }).waitFor();
    assert(await theirs.getByRole('button', { name: '/exile', exact: true }).count() === 0, '/ban named /exile to someone who is not an admin');
    await b.keyboard.press('Escape');
    await theirs.waitFor({ state: 'detached' });
  }, a);

  await step('extras: a slash after a space, or in code, is sent as text and never run', async () => {
    await say(a, ' /shrug sent as it is');
    await message(b, '/shrug sent as it is').waitFor();
    assert(!(await message(b, '/shrug sent as it is').innerText()).includes('¯'), 'the literal slash ran /shrug');
    await say(a, '`/me in code`');
    await message(b, '/me in code').locator('code').waitFor();
    await say(a, '/say /roll d20 is how you roll');
    await message(b, '/roll d20 is how you roll').waitFor();
  }, a);

  await step('extras: /broadcast is shown to everyone connected, and dismissed', async () => {
    await say(a, '/broadcast 15m Upgrading in **five minutes**');
    await a.getByText('Shown to everyone connected for 15 minutes.').waitFor();
    const banner = b.getByRole('region', { name: 'Server notice' });
    await banner.getByText('five minutes').waitFor();
    assert(await banner.locator('strong', { hasText: 'five minutes' }).count() === 1, 'the broadcast\'s Markdown was not drawn');
    await banner.getByText('from Kaity').waitFor();
    // Said once to a screen reader.
    await b.locator('#announcer', { hasText: 'Server notice: Upgrading in **five minutes**' }).waitFor({ state: 'attached' });
    await shot(b, '77-chat-broadcast', { fullPage: false });
    await banner.getByRole('button', { name: 'Dismiss the server notice' }).click();
    await banner.waitFor({ state: 'detached' });
    // A page that connects while it shows is shown it too.
    await b.reload();
    await b.locator('.conn.live').waitFor();
    await b.getByRole('region', { name: 'Server notice' }).waitFor();
    await say(a, '/broadcast clear');
    await a.getByText('The broadcast is down.').waitFor();
    await b.getByRole('region', { name: 'Server notice' }).waitFor({ state: 'detached' });
    await a.getByRole('region', { name: 'Server notice' }).waitFor({ state: 'detached' });
  }, b);

  await step('extras: a knock channel is seen in the browser, asked to join, accepted, and read', async () => {
    const room = await a.evaluate(async () => {
      const res = await fetch('/api/v1/channels', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'Council', access: 'knock', topic: 'Where it is decided', account_ids: [] }) });
      return (await res.json()).conversation.id;
    });
    await a.goto(origin + '/chat/c/' + room);
    await a.locator('#main .editor').waitFor();
    await say(a, 'Minutes of the last meeting');
    // Sam sees it in the browser, with its topic, and cannot read it.
    await b.goto(origin + '/chat/browse');
    const row = b.locator('li', { hasText: 'Council' }).filter({ hasText: 'Where it is decided' });
    await row.waitFor();
    assert(await row.getByRole('button', { name: 'Read' }).count() === 0, 'a knock channel offers Read to someone outside it');
    // (That it cannot be read from outside is held by internal/api's tests.)
    await row.getByRole('button', { name: 'Ask to join' }).click();
    await b.getByText('You asked to join #Council.').waitFor();
    // Kaity is shown the request in the channel and adds him.
    const bar = a.locator('.knocks-bar', { hasText: '1 person asks to join' });
    await bar.waitFor();
    await bar.getByRole('button', { name: 'Review' }).click();
    const dlg = a.getByRole('dialog', { name: 'Asking to join #Council' });
    await dlg.getByText('Sam Okafor').waitFor();
    await shot(a, '78-chat-knocks', { fullPage: false });
    await dlg.getByRole('button', { name: 'Add' }).click();
    await a.getByText('Sam Okafor was added.').waitFor();
    await a.keyboard.press('Escape');
    await b.getByText('You were added to #Council.').waitFor();
    const side = b.locator('.side-row').filter({ hasText: 'Council' });
    await side.waitFor();
    await side.getByRole('link').click();
    await message(b, 'Minutes of the last meeting').waitFor();
  }, a);

  await step('extras: /watch tells Kaity once when Sam is next around, and Scheduled lists it', async () => {
    const sam = await b.evaluate(async () => (await (await fetch('/api/v1/me')).json()).account);
    const back = b.url();
    const api = (path) => a.evaluate(async (p) => (await fetch(p)).json(), path);
    const until = async (what, ok) => {
      for (let i = 0; i < 100; i++) { if (await ok()) return; await a.waitForTimeout(200); }
      throw new Error('timed out waiting until ' + what);
    };
    // Sam closes the chat: no connection, so offline.
    await b.goto('about:blank');
    await until('Sam is offline', async () => (await api('/api/v1/accounts/' + sam.id)).account.presence === 'offline');
    // The trailing space closes the mention list, so Enter sends.
    await say(a, `/watch @${sam.username} `);
    await a.getByText('You will be told once when Sam Okafor is next around').waitFor();
    await a.getByRole('link', { name: 'Scheduled', exact: true }).click();
    const item = a.locator('.result[data-kind="watch"]', { hasText: 'When Sam Okafor is around' });
    await item.waitFor();
    await item.getByRole('button', { name: 'Stop watching' }).waitFor();
    await a.locator('.side-row').filter({ hasText: /^general/ }).first().getByRole('link').click();
    await a.locator('#main .editor').waitFor();
    // Sam comes back: Kaity's watch fires once, as a reminder in her own
    // channel, and is done.
    await b.goto(back);
    await b.locator('.conn.live').waitFor();
    let fired;
    await until('the watch fires', async () => {
      fired = (await api('/api/v1/scheduled?kind=watch')).scheduled.find((s) => s.watch_account_id === sam.id);
      return fired && fired.last_run_at && !fired.next_run_at;
    });
    const got = await api(`/api/v1/conversations/${fired.conversation_id}/messages?limit=5`);
    assert(got.messages.some((m) => m.text === `@${sam.username} is around` && m.via && m.via.kind === 'reminder'),
      'the watch posted no reminder: ' + JSON.stringify(got.messages.map((m) => m.text)));
    // Around now: said at once, and nothing is stored.
    await say(a, `/watch @${sam.username} `);
    await a.getByText('Sam Okafor is around now.').waitFor();
  }, a);
}
