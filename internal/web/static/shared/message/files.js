// How the files of a message are drawn (docs/MESSAGE-FORMAT.md section 5):
// images inline as thumbnails that open full size, several as a gallery;
// video and audio with a player; text with its first lines in monospace;
// anything else as a name, a size and a download button. A kind this
// client does not know is drawn as a file.

import { h, icon, bytes, openDialog, button } from '../dom.js';
import { codeBlock } from '../md/render.js';
import { languageForFile } from '../highlight/hl.js';
import { isThemeFile, fileTheme } from './theme.js';

const STATE = {
  arriving: 'Arriving from another server…',
  unavailable: 'This file could not be fetched from the server it was posted on.',
  too_large: 'This file is larger than this server accepts.',
};

export function renderFiles(files) {
  if (!files || !files.length) return null;
  const images = files.filter((f) => f.kind === 'image' && f.url);
  const themes = files.filter((f) => !(f.kind === 'image' && f.url) && isThemeFile(f));
  const rest = files.filter((f) => !(f.kind === 'image' && f.url) && !isThemeFile(f));
  return h('div', { class: 'files' },
    images.length ? h('div', { class: 'gallery' + (images.length > 1 ? ' many' : '') }, images.map((f) => image(f, images))) : null,
    themes.map(fileTheme),
    rest.map(file));
}

function caption(f) {
  return f.caption ? h('figcaption', {}, f.caption) : null;
}

function image(f, all) {
  // The caption is what a screen reader reads for an image.
  const img = h('img', { src: f.thumb_url || f.url, alt: f.caption || f.name, loading: 'lazy' });
  if (f.width && f.height) { img.width = f.width; img.height = f.height; }
  return h('figure', { class: 'file-image' },
    h('button', { type: 'button', class: 'image-open', 'aria-label': 'Open ' + (f.caption || f.name), onclick: () => viewer(f, all) }, img),
    caption(f));
}

// The full-size view: one image at a time, with the others a key away.
function viewer(first, all) {
  let i = all.indexOf(first);
  openDialog(first.name, (close, dlg) => {
    dlg.classList.add('viewer');
    const img = h('img', { alt: '' });
    const label = h('p', { class: 'dim small' });
    const link = h('a', { class: 'btn small', target: '_blank', rel: 'noopener' }, 'Open the original');
    const show = () => {
      const f = all[i];
      img.src = f.url; img.alt = f.caption || f.name;
      label.textContent = `${f.name} · ${bytes(f.size)}${f.width ? ` · ${f.width}×${f.height}` : ''}${all.length > 1 ? ` · ${i + 1} of ${all.length}` : ''}${f.caption ? ' · ' + f.caption : ''}`;
      link.href = f.url;
    };
    const step = (by) => { i = (i + by + all.length) % all.length; show(); };
    dlg.addEventListener('keydown', (e) => { if (e.key === 'ArrowRight') step(1); if (e.key === 'ArrowLeft') step(-1); });
    show();
    return { body: [h('div', { class: 'viewer-image' }, img), label],
      foot: [all.length > 1 && button('Previous', () => step(-1)), all.length > 1 && button('Next', () => step(1)), link, button('Close', () => close())] };
  }, { wide: true });
}

function file(f) {
  if (!f.url) {
    return h('div', { class: 'file-card' }, icon('file'), h('div', { class: 'what' }, h('strong', {}, f.name), h('small', { class: 'dim' }, STATE[f.state] || 'This file is not available.')));
  }
  if (f.kind === 'video') {
    // The server makes no poster frame; without one the player shows a
    // plain placeholder until it is played.
    const v = h('video', { controls: true, preload: 'none', src: f.url, 'aria-label': f.caption || f.name });
    if (f.poster_url) v.poster = f.poster_url;
    return h('figure', { class: 'file-video' + (f.poster_url ? '' : ' no-poster') }, v, h('figcaption', {}, f.caption || f.name, ' · ', bytes(f.size)));
  }
  if (f.kind === 'audio') {
    return h('figure', { class: 'file-audio' }, h('figcaption', {}, icon('file', null, 'small'), ' ', f.caption || f.name, ' · ', bytes(f.size)),
      h('audio', { controls: true, preload: 'none', src: f.url, 'aria-label': f.caption || f.name }));
  }
  if (f.kind === 'text' && f.preview_text != null) return textPreview(f);
  return h('div', { class: 'file-card' }, icon('file'),
    h('div', { class: 'what' }, h('strong', {}, f.name), h('small', { class: 'dim' }, [bytes(f.size), f.media_type, f.caption].filter(Boolean).join(' · '))),
    h('a', { class: 'btn small', href: f.url, download: f.name }, icon('download', null, 'small'), 'Download'));
}

// The first 20 lines, highlighted if the file's extension names a
// language, with "show all".
function textPreview(f) {
  const lang = languageForFile(f.name);
  const holder = h('div', {});
  const draw = (text) => holder.replaceChildren(codeBlock(text, lang, { title: `${f.name} · ${bytes(f.size)}` }));
  draw(f.preview_text);
  const lines = f.preview_text.split('\n').length;
  const all = h('button', { type: 'button', class: 'link-btn small', onclick: async () => {
    all.disabled = true;
    try {
      const res = await fetch(f.url, { credentials: 'same-origin' });
      if (!res.ok) throw new Error('The file could not be read.');
      draw(await res.text());
      all.remove();
    } catch (err) { all.textContent = err.message; }
  } }, 'Show all');
  return h('figure', { class: 'file-text' }, holder,
    h('figcaption', {}, lines >= 20 ? all : null, ' ', h('a', { href: f.url, download: f.name }, 'Download'), f.caption ? ' · ' + f.caption : ''));
}
