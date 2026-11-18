// How the pages look in this browser: theme, palette, contrast and the
// colour theme in use (docs/STYLE.md "Themes and settings" and "Themes").
// Loaded as a module from <head> (the pages allow module files only), so
// it runs as soon as the page is read, before the first frame on a page of
// this size, and needs nothing from the network. What is chosen is kept in
// this browser only, under "churr-look", and lasts across sign-outs; the
// settings that change it are in /shared/appearance.js and
// /shared/themes.js. Every page follows the system's light or dark and its
// "increase contrast" unless told otherwise here.
//
// A theme other than the built-in ones is kept as the colour tokens it
// works out to (shared/palette.js did the arithmetic and the contrast
// check when it was chosen), one set per kind in `paint`; this file only
// sets them on <html>. The built-in kind underneath supplies every token a
// theme does not name.
(function () {
  var KEY = 'churr-look';
  var root = document.documentElement;
  var NAMES = ['bg', 'panel', 'raise', 'line', 'grid', 'ink', 'dim', 'faint', 'edge', 'lav', 'cyan', 'pink', 'yel',
    'on-lav', 'on-cyan', 'on-pink', 'on-yel', 'yel-ink', 'on', 'paper', 'sep'];
  var HEX = /^#[0-9a-f]{6}$/;
  var trying = null; // { kind, tokens, name }: a theme being tried, for this page only
  function read() {
    try { var v = JSON.parse(localStorage.getItem(KEY) || '{}'); return v && typeof v === 'object' ? v : {}; } catch (e) { return {}; }
  }
  function media(q) { return !!(window.matchMedia && window.matchMedia(q).matches); }
  // usable returns a token set if every colour in it is a plain hex colour.
  function usable(t) {
    if (!t || typeof t !== 'object') return null;
    for (var i = 0; i < NAMES.length; i++) {
      var n = NAMES[i];
      if (n === 'sep') continue;
      if (!HEX.test(t[n] || '')) return null;
    }
    return t;
  }
  // green is the verified shield's colour (tokens.css --green), fitted to a
  // theme's surfaces: the built-in green of its kind, taken lighter (dark)
  // or darker (light) until it reaches 4.5:1 on ground, panel and raised,
  // as the app's ChurrColors.green.
  function lum(hex) {
    var out = 0, w = [0.2126, 0.7152, 0.0722];
    for (var i = 0; i < 3; i++) {
      var v = parseInt(hex.substr(1 + 2 * i, 2), 16) / 255;
      out += w[i] * (v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4));
    }
    return out;
  }
  function green(kind, paint) {
    var rgb = kind === 'light' ? [0x12, 0x73, 0x3a] : [0x4a, 0xde, 0x80];
    var to = kind === 'light' ? 0 : 255;
    var hex = function () { return '#' + rgb.map(function (v) { return ('0' + Math.round(v).toString(16)).slice(-2); }).join(''); };
    for (var step = 0; step < 20; step++) {
      var g = lum(hex()), least = Infinity;
      ['bg', 'panel', 'raise'].forEach(function (n) {
        var b = lum(paint[n]);
        least = Math.min(least, (Math.max(g, b) + 0.05) / (Math.min(g, b) + 0.05));
      });
      if (least >= 4.6) break;
      rgb = rgb.map(function (v) { return v + (to - v) * 0.1; });
    }
    return hex();
  }
  function clear() {
    root.style.removeProperty('--green');
    for (var i = 0; i < NAMES.length; i++) root.style.removeProperty('--' + NAMES[i]);
    root.removeAttribute('data-custom-theme');
  }
  function apply() {
    var v = read();
    var kind = trying ? trying.kind : (v.theme === 'dark' || v.theme === 'light') ? v.theme : media('(prefers-color-scheme: light)') ? 'light' : 'dark';
    var paint = trying ? trying.tokens : usable(v.paint && v.paint[kind]);
    var more = v.contrast === 'more' || (v.contrast !== 'normal' && media('(prefers-contrast: more)'));
    if (paint) {
      // The built-in kind underneath, then the theme's colours over it.
      root.setAttribute('data-theme', kind);
      root.removeAttribute('data-palette');
      root.setAttribute('data-custom-theme', kind);
      for (var i = 0; i < NAMES.length; i++) {
        var n = NAMES[i];
        var value = paint[n];
        if (more && n === 'line') value = paint.edge;
        if (more && n === 'faint') value = paint.dim;
        if (n === 'sep') value = more ? paint.edge : 'transparent';
        root.style.setProperty('--' + n, value);
      }
      root.style.setProperty('--green', green(kind, paint));
    } else {
      clear();
      if (v.theme === 'dark' || v.theme === 'light') root.setAttribute('data-theme', v.theme); else root.removeAttribute('data-theme');
      if (v.palette === 'hd') root.setAttribute('data-palette', 'hd'); else root.removeAttribute('data-palette');
    }
    if (v.contrast === 'more' || v.contrast === 'normal') root.setAttribute('data-contrast', v.contrast); else root.removeAttribute('data-contrast');
    var meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute('content', paint ? paint.bg : kind === 'light' ? '#f5f2ff' : '#0d1026');
  }
  apply();
  // Another tab changed it, or the system switched between light and dark
  // or changed its contrast setting.
  window.addEventListener('storage', function (e) { if (e.key === KEY) apply(); });
  if (window.matchMedia) {
    window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', apply);
    window.matchMedia('(prefers-contrast: more)').addEventListener('change', apply);
  }
  window.churrLook = {
    key: KEY, read: read, apply: apply,
    // tryTokens shows a theme on this page without keeping it; null stops.
    tryTokens: function (kind, tokens, name) { trying = tokens ? { kind: kind, tokens: tokens, name: name } : null; apply(); },
    trying: function () { return trying; },
  };
})();
