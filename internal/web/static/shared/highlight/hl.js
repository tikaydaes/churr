// Syntax highlighting for code blocks and text file previews.
//
// highlight.js core and its grammars are vendored beside this file
// (core.js, languages/*.js, LICENSE: BSD 3-Clause, version 11.11.1), and
// nothing is fetched from anywhere else. The languages are exactly the
// ones docs/MESSAGE-FORMAT.md section 2.3 lists; a grammar is loaded the
// first time its language is seen.
//
// highlight.js is asked only for its token tree, which is walked here to
// build spans. Its HTML output is never used, so no string is ever parsed
// as markup.

import hljs from './core.js';

// The file that holds the grammar of each language name. `html` is drawn
// with the XML grammar and `toml` with the INI one, as highlight.js itself
// does; Zig is Churr's own grammar.
const FILES = {
  bash: 'languages/bash.js', c: 'languages/c.js', cpp: 'languages/cpp.js', csharp: 'languages/csharp.js',
  css: 'languages/css.js', dart: 'languages/dart.js', diff: 'languages/diff.js', dockerfile: 'languages/dockerfile.js',
  elixir: 'languages/elixir.js', go: 'languages/go.js', graphql: 'languages/graphql.js', haskell: 'languages/haskell.js',
  html: 'languages/xml.js', ini: 'languages/ini.js', java: 'languages/java.js', javascript: 'languages/javascript.js',
  json: 'languages/json.js', kotlin: 'languages/kotlin.js', lua: 'languages/lua.js', makefile: 'languages/makefile.js',
  markdown: 'languages/markdown.js', nginx: 'languages/nginx.js', objectivec: 'languages/objectivec.js',
  perl: 'languages/perl.js', php: 'languages/php.js', plaintext: 'languages/plaintext.js',
  powershell: 'languages/powershell.js', protobuf: 'languages/protobuf.js', python: 'languages/python.js',
  r: 'languages/r.js', ruby: 'languages/ruby.js', rust: 'languages/rust.js', scala: 'languages/scala.js',
  scss: 'languages/scss.js', sql: 'languages/sql.js', swift: 'languages/swift.js', toml: 'languages/ini.js',
  typescript: 'languages/typescript.js', xml: 'languages/xml.js', yaml: 'languages/yaml.js', zig: 'zig.js',
};

export const LANGUAGE_NAMES = Object.keys(FILES);

// The one table from highlight.js token names to the colour tokens of
// docs/CLIENTS.md section 12: keywords pink, strings yellow, numbers and
// constants cyan, names of functions and types lavender, comments dim,
// everything else ink. A token name is looked up whole, then by its first
// part ("title.function" falls back to "title").
//
// Two additions the table in the document does not have, because a diff
// with no colour says nothing: added lines are green and removed ones red.
const COLOURS = {
  keyword: 'kw', 'selector-tag': 'kw', name: 'kw', doctag: 'kw', 'meta keyword': 'kw', operator: 'kw',
  string: 'str', regexp: 'str', char: 'str', 'template-variable': 'str', 'attr-value': 'str', subst: null,
  number: 'num', literal: 'num', symbol: 'num', 'variable.constant': 'num', 'variable.language': 'num', bullet: 'num',
  title: 'fn', type: 'fn', built_in: 'fn', class: 'fn', function: 'fn', section: 'fn', 'selector-class': 'fn',
  'selector-id': 'fn', attr: 'fn', attribute: 'fn', property: 'fn',
  comment: 'dim', quote: 'dim', meta: 'dim', 'meta prompt': 'dim',
  addition: 'add', deletion: 'del',
  emphasis: 'em', strong: 'strong',
};

function colourOf(scope) {
  if (!scope) return null;
  if (scope in COLOURS) return COLOURS[scope];
  const first = scope.split('.')[0];
  return first in COLOURS ? COLOURS[first] : null;
}

const loading = new Map();

// load registers a language's grammar, once. It resolves false if the
// grammar could not be loaded, and the code then stays uncoloured.
function load(lang) {
  if (!FILES[lang]) return Promise.resolve(false);
  if (!loading.has(lang)) {
    loading.set(lang, import('./' + FILES[lang]).then((m) => {
      hljs.registerLanguage(lang, m.default);
      return true;
    }).catch(() => false));
  }
  return loading.get(lang);
}

// tokens returns the token tree for some code as nested lists: a string is
// plain text, and [colour, children] is a coloured run.
function tokens(code, lang) {
  const result = hljs.highlight(code, { language: lang, ignoreIllegals: true });
  const walk = (node) => node.children.map((kid) => (typeof kid === 'string' ? kid : [colourOf(kid.scope), walk(kid)]));
  return walk(result._emitter.rootNode);
}

function build(parent, list) {
  for (const t of list) {
    if (typeof t === 'string') { parent.append(t); continue; }
    const [colour, kids] = t;
    if (!colour) { build(parent, kids); continue; }
    const span = document.createElement('span');
    span.className = 'hl-' + colour;
    build(span, kids);
    parent.append(span);
  }
}

// highlightInto puts code into an element: at once as plain text, and
// coloured as soon as the grammar is there. An unknown or missing language
// stays in monospace with no colour; nothing is guessed.
export function highlightInto(el, code, lang) {
  el.textContent = code;
  if (!lang || lang === 'plaintext' || !FILES[lang] || code.length > 200000) return Promise.resolve(false);
  return load(lang).then((ok) => {
    if (!ok || el.textContent !== code) return false;
    try {
      const list = tokens(code, lang);
      el.replaceChildren();
      build(el, list);
      return true;
    } catch {
      el.textContent = code;
      return false;
    }
  });
}

// tokenNames returns, for the tests, the colour of each token of some
// code: [[text, colour], …] with plain text left out.
export async function tokenNames(code, lang) {
  if (!(await load(lang))) return null;
  const out = [];
  const walk = (list, colour) => {
    for (const t of list) {
      if (typeof t === 'string') { if (colour && t.trim()) out.push([t, colour]); } else walk(t[1], t[0] || colour);
    }
  };
  walk(tokens(code, lang), null);
  return out;
}

// The language a file is highlighted as, from its extension, for the text
// previews of docs/MESSAGE-FORMAT.md section 5.
const EXTENSIONS = {
  sh: 'bash', bash: 'bash', zsh: 'bash', c: 'c', h: 'c', cc: 'cpp', cpp: 'cpp', hpp: 'cpp', cs: 'csharp', css: 'css',
  dart: 'dart', diff: 'diff', patch: 'diff', ex: 'elixir', exs: 'elixir', go: 'go', graphql: 'graphql', gql: 'graphql',
  hs: 'haskell', html: 'html', htm: 'html', ini: 'ini', java: 'java', js: 'javascript', mjs: 'javascript',
  json: 'json', kt: 'kotlin', lua: 'lua', mk: 'makefile', md: 'markdown', m: 'objectivec', pl: 'perl', php: 'php',
  ps1: 'powershell', proto: 'protobuf', py: 'python', r: 'r', rb: 'ruby', rs: 'rust', scala: 'scala', scss: 'scss',
  sql: 'sql', swift: 'swift', toml: 'toml', ts: 'typescript', xml: 'xml', yaml: 'yaml', yml: 'yaml', zig: 'zig',
};
export function languageForFile(name) {
  const lower = String(name || '').toLowerCase();
  if (lower === 'dockerfile') return 'dockerfile';
  if (lower === 'makefile') return 'makefile';
  const ext = lower.includes('.') ? lower.split('.').pop() : '';
  return EXTENSIONS[ext] || '';
}
