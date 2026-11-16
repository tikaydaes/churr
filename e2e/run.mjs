// The whole browser run, in order, against one server. Each part leaves
// the server in the state the next one needs: the owner, then a second
// person, then both in the chat.

import { launch, person, failed, consoleErrors } from './lib.mjs';
import { chatFlow, actionsFlow, extrasFlow } from './chat.mjs';
import { themesFlow } from './themes.mjs';
import { hubFlow } from './hubs.mjs';
import { chromium } from 'playwright';
import { callsFlow, ARGS as MEDIA_ARGS } from './calls-web.mjs';
import { sealedCallFlow } from './sealed-call.mjs';
import { setupOwner, joinByInvite, signInOut, recoverWithCode, accountPage, adminConsole, adminRefusesMembers, rendererChecks, appSignIn } from './web.mjs';
import { sealedFlow } from './web-sealed.mjs';
import { instantsFlow } from './instants-web.mjs';
import { objectsFlow } from './objects-web.mjs';
import { requestsFlow, autoApprove } from './requests-web.mjs';

const [setupURL, only] = process.argv.slice(2);
const origin = new URL(setupURL).origin;
// A sealed chat's call needs the cameras and the sealed-chat keys in one
// browser, so that mode starts the whole run with fake media devices.
const browser = only === 'sealed-call' ? await chromium.launch({ args: MEDIA_ARGS }) : await launch();
const owner = await person(browser, 'kaity', origin);
const second = await person(browser, 'sam', origin);

const invite = await setupOwner(owner, setupURL);
if (invite) await joinByInvite(second, invite, 'sam', 'Sam Okafor');
// The first two people open chats with each other straight away, as the
// flows below expect; the people of the requests flow keep the default,
// where a new chat is a request (ruling R78).
if (invite) { await autoApprove(owner); await autoApprove(second); }
if (invite && (!only || only === 'chat')) { await chatFlow(owner, second, origin); await actionsFlow(owner, second, origin); }
if (invite && (!only || only === 'chat' || only === 'instants-web')) await instantsFlow(owner, second, browser, origin);
if (invite && (!only || only === 'chat' || only === 'objects-web')) await objectsFlow(owner, second, browser, origin);
if (invite && (!only || only === 'requests-web')) await requestsFlow(owner, browser, origin);
if (invite && (!only || only === 'themes')) await themesFlow(owner, second, origin);
if (invite && (!only || only === 'hub')) await hubFlow(owner, second, origin);
if (invite && only === 'calls') await callsFlow(owner, second, origin);
if (!only || only === 'web') {
  await rendererChecks(owner, origin);
  await accountPage(owner, origin);
  await adminConsole(owner, origin);
  await adminRefusesMembers(second, origin);
  if (!only) await extrasFlow(owner, second, origin);
  // Calls use the chat the extras flow opened between the two, and need
  // the owner still signed in.
  if (!only && invite) await callsFlow(owner, second, origin);
  await appSignIn(owner, browser, origin);
  await signInOut(owner, origin);
  if (second.recoveryCodes) await recoverWithCode(browser, origin, 'sam', second.recoveryCodes[0]);
}
if (invite && only === 'sealed-call') await sealedCallFlow(owner, second, browser, origin);
// Sealed chats last: they leave both accounts with sealed-chat keys.
if (invite && (!only || only === 'sealed')) await sealedFlow(owner, second, browser, origin);

console.log('console errors:', consoleErrors.length ? '\n  ' + consoleErrors.join('\n  ') : 'none');
if (consoleErrors.length) console.log('FAIL console errors were logged');
console.log(failed() ? `${failed()} step(s) failed` : 'all steps passed');
console.log('browsers: Chromium only (Firefox and WebKit were not run)');
await browser.close();
