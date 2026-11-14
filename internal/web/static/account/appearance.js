// How the web pages look in this browser: theme, palette and contrast.
// Nothing here is kept on the server.

import { card } from '/shared/console.js';
import { appearanceControls } from '/shared/appearance.js';

export default async function appearance() {
  const el = card('In this browser');
  el.append(...appearanceControls());
  return [el];
}
