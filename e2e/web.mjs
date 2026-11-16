// Setup, sign-in, joining, the account page and the admin console.

import { step, shot, noSidewaysScroll, assert, person, PHONE, DESKTOP } from './lib.mjs';

// setupOwner walks first-run setup from the one-time link to the first
// invite, and returns the invite link it made.
export async function setupOwner(owner, setupURL) {
  const { page } = owner;
  let invite = '';
  await step('setup: the page opens from the link', async () => {
    await page.goto(setupURL);
    await page.getByRole('heading', { name: 'Set up this server' }).waitFor();
    await shot(page, '01-setup-owner', { phone: true });
  }, page);
  await step('setup: the owner is made with a passkey', async () => {
    await page.getByLabel('Name of the community').fill('Night Shift');
    await page.getByLabel('Your username').fill('kaity');
    await page.getByLabel('Your display name').fill('Kaity');
    await page.getByRole('button', { name: /Create the owner.s passkey/ }).click();
    await page.getByRole('heading', { name: /recovery codes/i }).waitFor();
    const codes = await page.getByRole('list', { name: 'Recovery codes' }).getByRole('listitem').allInnerTexts();
    assert(codes.length === 8, `expected 8 recovery codes, saw ${codes.length}`);
    assert(/^[0-9A-Z]{4}(-[0-9A-Z]{4}){3}$/.test(codes[0]), `odd recovery code ${codes[0]}`);
    owner.recoveryCodes = codes;
    await shot(page, '02-setup-codes');
    const go = page.getByRole('button', { name: 'Continue' });
    assert(await go.isDisabled(), 'Continue should wait for the tick');
    await page.getByLabel('I have saved these codes').check();
    await go.click();
  }, page);
  await step('setup: the outside-connection switches', async () => {
    await page.getByRole('heading', { name: 'What this server may reach' }).waitFor();
    const relay = page.getByRole('checkbox', { name: 'Push relay' });
    const previews = page.getByRole('checkbox', { name: 'Link previews' });
    assert(await relay.isChecked(), 'the push relay starts on');
    assert(!(await previews.isChecked()), 'link previews start off');
    await page.getByRole('checkbox', { name: 'UnifiedPush' }).waitFor();
    for (const row of ['Certificate renewal', 'Object storage', 'Identity provider', 'Links to other servers']) {
      await page.getByRole('rowheader', { name: row }).waitFor();
    }
    await relay.uncheck();
    await shot(page, '03-setup-connections');
    await page.setViewportSize(PHONE);
    try { await noSidewaysScroll(page); await shot(page, '03-setup-connections-phone'); } finally { await page.setViewportSize(DESKTOP); }
    await page.getByRole('button', { name: 'Confirm and continue' }).click();
  }, page);
  await step('setup: who may join', async () => {
    await page.getByRole('heading', { name: 'Who may join' }).waitFor();
    assert(await page.getByRole('radio', { name: /By invite/ }).isChecked(), 'invite is the default');
    await shot(page, '04-setup-joining');
    await page.getByRole('button', { name: 'Save and finish' }).click();
  }, page);
  await step('setup: the first invite', async () => {
    await page.getByRole('heading', { name: 'The server is ready' }).waitFor();
    await page.getByRole('button', { name: 'Make an invite link' }).click();
    const link = page.locator('code.secret');
    await link.waitFor();
    invite = (await link.innerText()).trim();
    assert(/\/(join|invite)\/[\w-]{20,}$/.test(invite), `odd invite link ${invite}`);
    await shot(page, '05-setup-done');
  }, page);
  await step('setup: the settings were saved', async () => {
    const s = await page.evaluate(async () => (await (await fetch('/api/v1/admin/settings')).json()).settings);
    assert(s['push.relay_enabled'] === false, 'the push relay switch was not saved');
    assert(s['server.name'] === 'Night Shift', `community name is ${s['server.name']}`);
    assert(s['registration.mode'] === 'invite', 'registration mode was not saved');
  }, page);
  await step('setup: the link is spent', async () => {
    const res = await page.request.post(new URL('/api/v1/setup/begin', setupURL).href, { data: { setup_token: 'x', username: 'evil', display_name: 'Evil' } });
    assert(res.status() === 404, `a second setup answered ${res.status()}`);
  }, page);
  return invite;
}

// joinByInvite registers a second person from an invite link in their own
// browser context.
export async function joinByInvite(guest, invite, username, display) {
  const { page } = guest;
  await step(`join: ${username} registers by invite`, async () => {
    await page.goto(invite);
    await page.getByRole('heading', { name: /Join Night Shift/ }).waitFor();
    await shot(page, '06-join', { phone: true });
    await page.getByLabel('Username').fill(username);
    await page.getByLabel('Display name').fill(display);
    await page.getByRole('button', { name: 'Create a passkey and join' }).click();
    await page.getByRole('heading', { name: /recovery codes/i }).waitFor();
    guest.recoveryCodes = await page.getByRole('list', { name: 'Recovery codes' }).getByRole('listitem').allInnerTexts();
    await page.getByLabel('I have saved these codes').check();
    await page.getByRole('button', { name: 'Continue' }).click();
    await page.getByRole('heading', { name: 'You are in' }).waitFor();
    await page.getByRole('link', { name: 'Open in the app' }).waitFor();
  }, page);
  await step('join: a used invite is refused', async () => {
    const other = await guest.context.browser().newContext();
    const p = await other.newPage();
    await p.goto(invite);
    await p.getByRole('heading', { name: 'This invite does not work' }).waitFor();
    await other.close();
  }, page);
}

// signInOut signs the owner out and back in with the passkey, and tries
// the recovery-code way in.
export async function signInOut(owner, origin) {
  const { page } = owner;
  await step('sign-in: the page and a passkey sign-in', async () => {
    await page.goto(origin + '/account');
    await page.getByRole('button', { name: 'Sign out' }).first().click();
    await page.getByRole('button', { name: 'Sign in with a passkey' }).waitFor();
    await shot(page, '10-login');
    await page.setViewportSize(PHONE);
    try { await noSidewaysScroll(page); await shot(page, '10-login-phone'); } finally { await page.setViewportSize(DESKTOP); }
    await page.getByRole('button', { name: 'Sign in with a passkey' }).click();
    await page.waitForURL(/\/chat\//);
  }, page);
}

// recoverWithCode uses one of a person's recovery codes in a browser that
// has no passkey for them, and makes a new passkey from the recovery
// session.
export async function recoverWithCode(browser, origin, username, code) {
  const fresh = await person(browser, 'recovering', origin);
  const { page } = fresh;
  await step('recovery: a code leads to a new passkey', async () => {
    await page.goto(origin + '/login');
    await page.getByText('Lost your passkey? Use a recovery code').click();
    await page.getByLabel('Username').fill(username);
    await page.getByLabel('Recovery code').fill(code.toLowerCase());
    await page.getByRole('button', { name: 'Use this code' }).click();
    await page.getByRole('heading', { name: 'Make a new passkey' }).waitFor();
    await shot(page, '11-recovery', { phone: true });
    await page.getByRole('button', { name: 'Create a new passkey' }).click();
    await page.getByRole('heading', { name: 'You are back in' }).waitFor();
    await page.getByText('7 recovery codes left').waitFor();
    await shot(page, '12-recovered');
  }, page);
  await fresh.context.close();
}

// accountPage walks the account page's sections with the owner.
export async function accountPage(owner, origin) {
  const { page } = owner;
  await step('account: profile saves', async () => {
    await page.goto(origin + '/account');
    await page.getByRole('heading', { name: 'Profile', level: 1 }).waitFor();
    await page.getByLabel('Title').fill('Night owl');
    await page.getByLabel('Pronouns').fill('she/her');
    await page.getByRole('button', { name: 'Save profile' }).click();
    await page.getByText('Profile saved.').waitFor();
    await shot(page, '20-account-profile', { phone: true });
  }, page);
  await step('account: passkeys with the PRF marker, a second passkey, recovery codes', async () => {
    await page.getByRole('link', { name: 'Passkeys and sign-in' }).click();
    await page.getByRole('heading', { name: /^Passkeys/ }).waitFor();
    // The virtual authenticator supports PRF, and the page reports it.
    await page.getByText(/PRF · not yet unlocking|unlocks sealed chats/).first().waitFor();
    await page.getByText('8 of 8').waitFor();
    await owner.withSecondAuthenticator(async () => {
      await page.getByRole('button', { name: 'Add a passkey' }).click();
      await page.getByText('Passkey added.').waitFor();
    });
    assert(await page.locator('.console-card').first().locator('.rows > li').count() === 2, 'the second passkey is not listed');
    await shot(page, '21-account-sign-in');
  }, page);
  await step('account: authenticator app shows a QR code', async () => {
    await page.getByRole('button', { name: 'Set up', exact: true }).click();
    const qr = page.getByRole('img', { name: 'QR code for your authenticator app' });
    await qr.waitFor();
    const secret = (await page.getByRole('dialog').locator('code.wrap').innerText()).trim();
    assert(/^[A-Z2-7]{16,}$/.test(secret), `odd authenticator secret ${secret}`);
    owner.totpSecret = secret;
    await shot(page, '22-account-totp', { fullPage: false });
    await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click();
  }, page);
  await step('account: sessions, and the pairing QR code', async () => {
    await page.getByRole('link', { name: 'Sessions and devices' }).click();
    await page.getByText('this one').first().waitFor();
    await page.getByText(/signed in with a passkey/).first().waitFor();
    await page.getByRole('button', { name: 'Add device' }).click();
    await page.getByRole('img', { name: 'Pairing QR code' }).waitFor();
    await shot(page, '23-account-pairing', { fullPage: false });
    await page.getByRole('dialog').getByRole('button', { name: 'Close' }).first().click();
  }, page);
  await step('account: a personal token is shown once', async () => {
    await page.getByRole('link', { name: 'Personal tokens' }).click();
    await page.getByRole('button', { name: 'New token' }).click();
    await page.getByLabel('What it is for').fill('Status script');
    await page.getByRole('button', { name: 'Make the token' }).click();
    const secret = await page.locator('code.secret').innerText();
    assert(secret.startsWith('churr_pat_'), `odd token ${secret.slice(0, 12)}`);
    await page.getByRole('button', { name: 'Done' }).click();
    await page.getByText('Status script').waitFor();
  }, page);
  await step('account: notification preferences and quiet hours', async () => {
    await page.getByRole('link', { name: 'Notifications' }).click();
    await page.getByLabel('Default thread view').waitFor();
    await page.getByRole('button', { name: 'Turn quiet hours on' }).click();
    await page.getByRole('button', { name: 'Skip tonight' }).waitFor();
    await shot(page, '24-account-notifications');
    await page.getByRole('button', { name: 'Turn off' }).click();
    await page.getByRole('button', { name: 'Turn quiet hours on' }).waitFor();
  }, page);
  await step('account: the remaining sections open', async () => {
    for (const name of ['Blocked accounts', 'Sealed chats', 'Leave this server']) {
      await page.getByRole('link', { name }).click();
      await page.getByRole('heading', { name, level: 1 }).waitFor();
      await page.locator('.console-card').first().waitFor();
    }
  }, page);
  await step('account: appearance in this browser', async () => {
    await page.getByRole('link', { name: 'Appearance' }).click();
    await page.getByRole('heading', { name: 'Appearance', level: 1 }).waitFor();
    const contrast = page.getByRole('group', { name: 'Increase contrast' });
    await contrast.getByRole('button', { name: 'On' }).click();
    await contrast.getByRole('button', { name: 'On', pressed: true }).waitFor();
    assert(await page.evaluate(() => document.documentElement.dataset.contrast === 'more'), 'increase contrast was not applied');
    await shot(page, '26-account-appearance');
    await contrast.getByRole('button', { name: 'System' }).click();
    assert(await page.evaluate(() => !document.documentElement.dataset.contrast), 'increase contrast was not put back');
  }, page);
  await step('account: phone width', async () => {
    await page.setViewportSize(PHONE);
    try {
      await page.goto(origin + '/account/sign-in');
      await page.getByRole('heading', { name: /^Passkeys/, level: 1 }).waitFor();
      await noSidewaysScroll(page);
      await shot(page, '25-account-phone');
    } finally { await page.setViewportSize(DESKTOP); }
  }, page);
}

// The sections of the admin console, with something each must show.
const ADMIN = [
  ['overview', 'Overview', 'The server'],
  ['general', 'General', 'Name of the community'],
  ['sign-in', 'Sign-in', 'Who can make an account'],
  ['messages', 'Messages', 'Quick-correction window'],
  ['outside', 'Outside connections', 'Push relay'],
  ['company-sign-in', 'Company sign-in', 'Providers'],
  ['people', 'People', '@kaity'],
  ['invites', 'Invites', 'Invite links'],
  ['groups', 'Groups', '@everyone'],
  ['timeouts', 'Time-outs and bans', 'Nobody is exiled.'],
  ['channels', 'Channels', 'general'],
  ['sections', 'Sections', 'New section'],
  ['reference-patterns', 'Reference patterns', 'Server-wide patterns'],
  ['emoji', 'Emoji', 'Custom emoji'],
  ['saved-replies', 'Saved replies', 'Community saved replies'],
  ['status-presets', 'Status presets', 'Community status presets'],
  ['word-filter', 'Word filter', 'Words and patterns'],
  ['reports', 'Reports', /Nothing is waiting|reported this|flagged this/],
  ['purge-requests', 'Purge requests', /Nothing is waiting|asked on/],
  ['bots', 'Bots', 'New bot'],
  ['webhooks', 'Webhooks', 'Who approves'],
  ['integrations', 'Integrations', 'New integration'],
  ['linked-servers', 'Linked servers', /The server says this is unavailable|This server is linked to no other/],
  ['shared-channels', 'Shared channels', /The server says this is unavailable|No offers waiting/],
  ['calls', 'Calls', 'Test calls'],
  ['audit', 'Audit log', 'owner.created'],
  ['export', 'Export', /The server says this is unavailable|No export has been made/],
];

// adminConsole opens every screen of the admin console and uses the main
// ones.
export async function adminConsole(owner, origin) {
  const { page } = owner;
  await step('admin: every screen opens', async () => {
    await page.goto(origin + '/admin');
    for (const [id, label, shows] of ADMIN) {
      await page.locator(`.console-nav a[href="/admin/${id}"]`).click();
      await page.getByRole('heading', { name: label, level: 1 }).waitFor({ timeout: 8000 });
      await page.locator('.console-main').getByText(shows).first().waitFor({ timeout: 8000 });
      if (['overview', 'outside', 'people', 'channels', 'audit', 'linked-servers'].includes(id)) await shot(page, `30-admin-${id}`, { phone: id === 'overview' });
    }
  }, page);
  await step('admin: a reference pattern is added and listed', async () => {
    await page.goto(origin + '/admin/reference-patterns');
    await page.getByRole('button', { name: 'Add a pattern' }).click();
    const dlg = page.getByRole('dialog', { name: 'Add a pattern' });
    await dlg.getByLabel('Name').fill('RFCs');
    await dlg.getByLabel('Prefix').fill('RFC');
    // A refused field is the API tests' to check: a 400 here would be a
    // console error.
    await dlg.getByLabel('Address').fill('https://rfc.example/rfc{value}');
    await dlg.getByRole('button', { name: 'Save' }).click();
    await dlg.waitFor({ state: 'detached' });
    await page.locator('.console-main').getByText('RFC123 → https://rfc.example/rfc123').waitFor();
  }, page);
  await step('admin: a setting saves at once and is audited', async () => {
    await page.goto(origin + '/admin/messages');
    await page.getByRole('checkbox', { name: 'Members may add emoji' }).check();
    await page.getByText('Saved.').first().waitFor();
    const s = await page.evaluate(async () => (await (await fetch('/api/v1/admin/settings')).json()).settings);
    assert(s['emoji.members_may_add'] === true, 'the setting was not saved');
    await page.goto(origin + '/admin/audit');
    await page.getByText('settings.changed').or(page.getByText(/setting/)).first().waitFor();
  }, page);
  // The session is minutes old here, so the server asks for no fresh check;
  // the dialog that answers one is not exercised by this run.
  await step('admin: a sign-in policy setting saves', async () => {
    await page.goto(origin + '/admin/sign-in');
    await page.getByLabel('Members may invite').check();
    await page.getByText('Saved.').first().waitFor();
    await shot(page, '31-admin-sign-in');
  }, page);
  await step('admin: an invite with groups, a section, a group', async () => {
    await page.goto(origin + '/admin/sections');
    await page.getByRole('button', { name: 'New section' }).click();
    await page.getByLabel('Name').fill('Projects');
    await page.getByRole('button', { name: 'Create' }).click();
    await page.locator('.rows').getByText('Projects').waitFor();
    await page.goto(origin + '/admin/groups');
    await page.getByRole('button', { name: 'New group' }).click();
    await page.getByLabel('Handle').fill('design');
    await page.getByLabel('Name', { exact: true }).fill('Design');
    await page.getByLabel('Colour of its label').selectOption('pink');
    await page.getByRole('button', { name: 'Create' }).click();
    await page.getByText('@design').waitFor();
    await page.getByRole('listitem').filter({ hasText: '@design' }).getByRole('button', { name: 'Members' }).click();
    await page.getByRole('button', { name: 'Add people' }).click();
    await page.getByRole('button', { name: /Sam Okafor/ }).click();
    await page.getByRole('button', { name: 'Add', exact: true }).click();
    await page.getByRole('dialog').getByText('@sam').waitFor();
    await shot(page, '32-admin-group-members', { fullPage: false });
    await page.getByRole('button', { name: 'Done' }).click();
    await page.goto(origin + '/admin/invites');
    await page.getByRole('button', { name: 'New invite' }).click();
    await page.getByRole('dialog').getByText('@design').click();
    await shot(page, '33-admin-new-invite', { fullPage: false });
    await page.getByRole('button', { name: 'Make the invite' }).click();
    await page.getByText('Groups: @design').waitFor();
  }, page);
  await step('admin: a bot and its token, a word filter entry, a saved reply', async () => {
    await page.goto(origin + '/admin/bots');
    await page.getByRole('button', { name: 'New bot' }).click();
    await page.getByLabel('Username').fill('deploybot');
    await page.getByLabel('Display name').fill('Deploy Bot');
    await page.getByRole('button', { name: 'Create' }).click();
    const secret = await page.locator('code.secret').innerText();
    assert(secret.startsWith('churr_bot_'), 'the bot token was not shown');
    owner.botToken = secret;
    await page.getByRole('button', { name: 'Done' }).click();
    await page.getByText('@deploybot').waitFor();
    await page.goto(origin + '/admin/word-filter');
    await page.getByRole('button', { name: 'New entry' }).click();
    await page.getByLabel('Word or pattern').fill('frobnicate');
    await page.getByRole('button', { name: 'Add', exact: true }).click();
    await page.locator('.rows').getByText('frobnicate').waitFor();
    await page.goto(origin + '/admin/saved-replies');
    await page.getByRole('button', { name: 'New saved reply' }).click();
    // /shrug is a built-in too: the community's own replaces it on this
    // server (ruling R72), and creating it says so.
    await page.getByLabel('Command').fill('shrug');
    await page.getByLabel('What it posts').fill('¯\\\\_(ツ)\\_/¯ the house shrug');
    await page.getByRole('button', { name: 'Create' }).click();
    await page.getByText('on this server this one replaces it').waitFor();
    await page.getByText('/shrug', { exact: true }).waitFor();
    // /ban is protected: the server refuses the name and the dialog says so.
    await page.getByRole('button', { name: 'New saved reply' }).click();
    await page.getByLabel('Command').fill('ban');
    await page.getByLabel('What it posts').fill('a look-alike');
    await page.getByRole('button', { name: 'Create' }).click();
    await page.getByText('/ban is a built-in command that cannot be replaced.').waitFor();
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  }, page);
  await step('admin: phone width', async () => {
    await page.setViewportSize(PHONE);
    try {
      for (const id of ['overview', 'people', 'outside']) {
        await page.goto(origin + '/admin/' + id);
        await page.locator('.console-card').first().waitFor();
        await noSidewaysScroll(page);
      }
      await shot(page, '34-admin-phone');
      await page.getByRole('button', { name: 'Outside connections' }).click();
      await page.getByRole('link', { name: 'People' }).waitFor();
      await shot(page, '35-admin-phone-nav', { fullPage: false });
    } finally { await page.setViewportSize(DESKTOP); }
  }, page);
}

// A member who opens the admin console is told it is not for them, and the
// API refuses them whatever the page does.
export async function adminRefusesMembers(member, origin) {
  const { page } = member;
  await step('admin: a member is turned away', async () => {
    await page.goto(origin + '/admin');
    await page.getByText('The admin console is for the owner and admins of this server.').waitFor();
    const status = await page.evaluate(async () => (await fetch('/api/v1/admin/settings')).status);
    assert(status === 403, `the API answered a member ${status}`);
  }, page);
}

// The renderer and the vendored highlighter, run in the page they ship in.
export async function rendererChecks(who, origin) {
  const { page } = who;
  await step('markdown: the renderer builds nodes and never markup', async () => {
    // The chat page is where messages are drawn, with its stylesheets.
    await page.goto(origin + '/chat/');
    await page.locator('.side-row').first().waitFor();
    const probe = await page.evaluate(async () => {
      const { renderMarkdown } = await import('/shared/md/render.js');
      const id = '0193a000-0000-7000-8000-000000000020';
      const text = '# Title\n\nHello **bold** and *em* and ~~gone~~ and ||secret|| `code`\n\n<img src=x onerror=alert(1)> <script>alert(2)</script>\n\n'
        + '[bad](javascript:alert(3)) [ok](https://ok.example/a) [https://bank.example](https://evil.example/login) ![pic](https://img.example/a.png)\n\n'
        + `- one\n- [ ] task @sam\n  - nested\n\n1. first\n2. second\n\n> quoted\n\n---\n\n| a | b |\n|---|:-:|\n| 1 | 2 |\n\nsee <#${id}> and <#0193a000-0000-7000-8000-00000000ffff> and :party: and :tada:\n\n` + '```go\nfunc main() {}\n```';
      const at = text.indexOf('<#');
      const el = renderMarkdown(text, [
        { type: 'conversation', at, len: 39, conversation_id: id },
        { type: 'emoji', at: text.indexOf(':party:'), len: 7, emoji_id: 'e1' },
      ], {
        conversation: (x) => (x === id ? { id, name: 'ops <b>' } : undefined),
        emoji: () => ({ name: 'party', url: '/icons/favicon.png' }),
        standardEmoji: (n) => (n === 'tada' ? '🎉' : undefined),
      });
      document.body.append(el);
      await new Promise((r) => setTimeout(r, 400));
      const out = {
        tags: [...el.querySelectorAll('*')].map((e) => e.tagName),
        hrefs: [...el.querySelectorAll('a')].map((a) => a.getAttribute('href')),
        rels: [...el.querySelectorAll('a')].map((a) => a.rel + '|' + a.target),
        text: el.innerText,
        chips: [...el.querySelectorAll('.chip')].map((c) => c.textContent),
        warned: !!el.querySelector('.link-warn'),
        spoilerHidden: getComputedStyle(el.querySelector('.spoiler-text')).visibility,
        imgs: [...el.querySelectorAll('img')].map((i) => i.getAttribute('src')),
        coloured: !!el.querySelector('.codeblock .hl-kw'),
        tada: el.querySelector('.emoji-char') && el.querySelector('.emoji-char').textContent,
        boxes: el.querySelectorAll('input[type=checkbox]').length,
      };
      el.remove();
      return out;
    });
    assert(!probe.tags.includes('SCRIPT') && !probe.tags.includes('B'), 'markup from the text became elements');
    assert(probe.imgs.length === 1 && probe.imgs[0] === '/icons/favicon.png', `images drawn: ${probe.imgs}`);
    assert(probe.hrefs.every((x) => /^https:/.test(x)) && probe.hrefs.length === 3, `links: ${JSON.stringify(probe.hrefs)}`);
    assert(probe.rels.every((x) => x.includes('noopener') && x.endsWith('_blank')), 'a link does not open outside the page');
    assert(probe.warned, 'a link whose text names another host is not marked');
    assert(probe.text.includes('<img src=x onerror=alert(1)>') && probe.text.includes('[bad](javascript:alert(3))') === false && probe.text.includes('bad'), 'unsupported markup is not shown as typed');
    assert(JSON.stringify(probe.chips) === JSON.stringify(['ops <b>', 'unknown channel']), `channel chips: ${JSON.stringify(probe.chips)}`);
    assert(probe.spoilerHidden === 'hidden', 'a spoiler is not hidden');
    assert(probe.coloured, 'the code block was not highlighted');
    assert(probe.tada === '🎉', 'a standard short code was not drawn as its emoji');
    assert(probe.boxes === 1, 'the checklist item has no tick-box');
    for (const t of ['STRONG', 'EM', 'S', 'CODE', 'UL', 'OL', 'BLOCKQUOTE', 'HR', 'TABLE', 'PRE']) assert(probe.tags.includes(t), `no ${t} was drawn`);
  }, page);
  await step('markdown: every language of the format loads from the vendored files and colours', async () => {
    const out = await page.evaluate(async () => {
      const { LANGUAGE_NAMES, tokenNames } = await import('/shared/highlight/hl.js');
      const { LANGUAGES } = await import('/shared/md/parse.js');
      const canon = [...new Set(LANGUAGES.values())].sort();
      const bad = [];
      const sample = 'const x = "a string"; // a comment\nfunction main() { return 42; }\n# heading\nkey: value\n<p class="x">text</p>\n+ added\n- removed\nSELECT 1;\n';
      for (const lang of canon) {
        const toks = await tokenNames(sample, lang);
        if (toks === null) bad.push(lang + ' did not load');
        else if (lang !== 'plaintext' && toks.length === 0) bad.push(lang + ' coloured nothing');
      }
      const zig = await tokenNames('const std = @import("std");\npub fn main() void { return; }', 'zig');
      return { bad, same: JSON.stringify(canon) === JSON.stringify([...LANGUAGE_NAMES].sort()), n: canon.length, zig };
    });
    assert(out.same, 'the highlighter’s languages are not exactly the format’s list');
    assert(out.n === 41, `the format lists 41 languages; the parser has ${out.n}`);
    assert(out.bad.length === 0, out.bad.join('; '));
    assert(out.zig.some(([t, c]) => t === 'pub' && c === 'kw') && out.zig.some(([t, c]) => t === '@import' && c === 'fn') && out.zig.some(([, c]) => c === 'str'), `zig tokens: ${JSON.stringify(out.zig)}`);
  }, page);
  await step('qr: the code drawn in the page reads back as its text', async () => {
    const out = await page.evaluate(async () => {
      const { qrMatrix } = await import('/shared/qr.js');
      const texts = ['churr://localhost/pair?code=abc123', 'otpauth://totp/Night%20Shift:kaity?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP&issuer=Night%20Shift&algorithm=SHA1&digits=6&period=30'];
      if (!('BarcodeDetector' in window)) return { skipped: 'this browser has no BarcodeDetector' };
      const det = new BarcodeDetector({ formats: ['qr_code'] });
      const read = [];
      for (const text of texts) {
        const m = qrMatrix(text), scale = 8, quiet = 4, size = (m.length + quiet * 2) * scale;
        const c = document.createElement('canvas');
        c.width = c.height = size;
        const g = c.getContext('2d');
        g.fillStyle = '#fff'; g.fillRect(0, 0, size, size);
        g.fillStyle = '#000';
        m.forEach((row, y) => row.forEach((on, x) => { if (on) g.fillRect((x + quiet) * scale, (y + quiet) * scale, scale, scale); }));
        const found = await det.detect(c).catch((e) => [{ rawValue: 'ERROR ' + e.message }]);
        read.push(found.length ? found[0].rawValue : null);
      }
      return { texts, read };
    });
    if (out.skipped) { console.log('     (skipped: ' + out.skipped + ')'); return; }
    if (out.read.some((r) => r && r.startsWith('ERROR'))) { console.log('     (skipped: the detector is not usable here: ' + out.read[0] + ')'); return; }
    assert(JSON.stringify(out.read) === JSON.stringify(out.texts), `read back ${JSON.stringify(out.read)}`);
  }, page);
}

// The pages the API draws for an app signing in, the pairing of a phone
// from the account page, and a recovery link made by an admin.
export async function appSignIn(owner, browser, origin) {
  const { page } = owner;
  const authorize = origin + '/oauth/authorize?response_type=code&client_id=churr-app&redirect_uri=' + encodeURIComponent('http://127.0.0.1:9/cb')
    + '&code_challenge=' + 'a'.repeat(43) + '&code_challenge_method=S256&state=s1&device_name=Test%20Mac&platform=macos';
  await step('unlock: the page for unlocking sealed history opens under the same policy', async () => {
    // The page's own work (docs/SEALED.md section 13.3) belongs to the
    // sealed-chat tests; this checks only that it is served and draws.
    const res = await page.goto(origin + '/unlock');
    assert(/script-src 'self'/.test(res.headers()['content-security-policy']), 'the unlock page has no policy');
    await page.locator('#door h1').waitFor();
    await shot(page, '79-unlock');
  }, page);
  await step('oauth: a signed-out browser is offered sign-in, with no inline script', async () => {
    const other = await person(browser, 'app', origin);
    const res = await other.page.goto(authorize);
    const csp = res.headers()['content-security-policy'];
    assert(/script-src 'self'/.test(csp) && !/unsafe/.test(csp), `policy of the authorize page: ${csp}`);
    await other.page.getByRole('button', { name: 'Sign in with a passkey' }).waitFor();
    assert(await other.page.locator('script:not([src])').count() === 0, 'the authorize page has an inline script');
    await shot(other.page, '80-oauth-sign-in');
    await other.context.close();
  }, page);
  await step('oauth: the approval page names the device and hands back a code', async () => {
    await page.goto(authorize);
    await page.getByRole('heading', { name: 'Approve this app' }).waitFor();
    await page.getByText('Test Mac').first().waitFor();
    await shot(page, '81-oauth-approve');
    const back = page.waitForRequest((r) => r.url().startsWith('http://127.0.0.1:9/cb?'));
    await page.getByRole('button', { name: 'Approve' }).click();
    const req = await back;
    assert(/code=[\w-]{20,}/.test(req.url()) && req.url().includes('state=s1'), `the redirect was ${req.url()}`);
  }, page);
  await step('pairing: a device that scans the code is named, confirmed, and then signed in', async () => {
    // The browser is still on its way to the app's address, which nothing
    // answers here; let that settle before going anywhere else.
    await page.waitForTimeout(800);
    await page.goto(origin + '/account/sessions').catch(() => page.goto(origin + '/account/sessions'));
    const created = page.waitForResponse((r) => r.url().endsWith('/oauth/pair') && r.status() === 201, { timeout: 20000 });
    await page.getByRole('button', { name: 'Add device' }).click();
    // If the session is past its ten fresh minutes, the shared dialog asks
    // first, and the request is then made again.
    const fresh = page.getByRole('dialog', { name: 'Check it is you' });
    if (await fresh.waitFor({ timeout: 1500 }).then(() => true, () => false)) { await shot(page, '83-fresh-check', { fullPage: false }); await fresh.getByRole('button', { name: 'Use a passkey' }).click(); }
    const pairing = encodeURIComponent((await (await created).json()).pairing_code);
    await page.getByRole('img', { name: 'Pairing QR code' }).waitFor();
    const token = () => page.request.post(origin + '/oauth/token', { form: { grant_type: 'urn:churr:params:oauth:grant-type:pairing-code', pairing_code: decodeURIComponent(pairing), device_name: 'Pixel in my hand', platform: 'android' } });
    const pending = await token();
    assert((await pending.json()).error === 'authorization_pending', `the first claim answered ${await pending.text()}`);
    await page.getByRole('dialog').getByText('Pixel in my hand').waitFor();
    await shot(page, '82-pairing-confirm', { fullPage: false });
    await page.getByRole('button', { name: 'Sign this device in' }).click();
    await page.getByText('The device is signed in.').waitFor();
    const done = await (await token()).json();
    assert(typeof done.access_token === 'string' && done.access_token.startsWith('churr_app_'), `the app got ${JSON.stringify(done).slice(0, 120)}`);
  }, page);
  await step('recovery: an admin makes a link; opening it leads to a new passkey', async () => {
    await page.goto(origin + '/admin/people');
    await page.locator('.rows > li', { hasText: '@sam' }).getByRole('button', { name: 'Actions' }).click();
    await page.getByRole('menuitem', { name: 'Make a recovery link…' }).click();
    await page.getByRole('button', { name: 'Make the link' }).click();
    const fresh = page.getByRole('dialog', { name: 'Check it is you' });
    const link = page.locator('code.secret');
    await Promise.race([link.waitFor(), fresh.waitFor()]);
    if (await fresh.isVisible()) { await shot(page, '83-fresh-check', { fullPage: false }); await fresh.getByRole('button', { name: 'Use a passkey' }).click(); }
    await link.waitFor();
    const url = (await link.innerText()).trim();
    assert(/\/recover\/[\w-]{20,}$/.test(url), `odd recovery link ${url}`);
    await page.getByRole('button', { name: 'Done' }).click();
    const lost = await person(browser, 'sam-recovering', origin);
    await lost.page.goto(url);
    await lost.page.getByRole('heading', { name: 'Recover your account' }).waitFor();
    await shot(lost.page, '84-recover-link');
    await lost.page.getByRole('button', { name: 'Recover my account' }).click();
    await lost.page.getByRole('heading', { name: 'Make a new passkey' }).waitFor();
    await lost.page.getByRole('button', { name: 'Create a new passkey' }).click();
    await lost.page.getByRole('heading', { name: 'You are back in' }).waitFor();
    // The link works once.
    const again = await lost.page.request.post(origin + '/api/v1/recovery/link', { data: { token: url.split('/recover/')[1] }, headers: { Origin: origin } });
    assert(again.status() >= 400, `a used recovery link answered ${again.status()}`);
    await lost.context.close();
  }, page);
}
