// Reference patterns: text such as PROJ-123 that becomes a link to where
// the thing it names is kept (ruling R73). These are the server's; a
// channel's managers add their own in the channel's settings.

import { card } from '/shared/console.js';
import { patternEditor } from '/shared/objects/patterns.js';
import { t } from '/shared/objects/words.js';

export default async function referencePatternsSection(ctx) {
  return card(t('patternsServer'), await patternEditor({ redraw: () => ctx.redraw() }));
}
