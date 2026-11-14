/// The highlighting languages of docs/MESSAGE-FORMAT.md section 2.3, with
/// their aliases. The list is fixed by the format so the apps and the web
/// chat colour the same blocks; nothing outside it is highlighted and no
/// language is ever guessed.
///
/// This file is plain Dart. The grammars themselves are loaded in
/// `lib/ui/message/highlight.dart`, because the `re_highlight` package
/// imports Flutter and `lib/core` may not.
library;

/// Canonical language name to its accepted aliases.
const Map<String, List<String>> highlightLanguages = {
  'bash': ['sh', 'shell', 'zsh'],
  'c': [],
  'cpp': ['c++', 'cc'],
  'csharp': ['cs', 'c#'],
  'css': [],
  'dart': [],
  'diff': ['patch'],
  'dockerfile': ['docker'],
  'elixir': [],
  'go': ['golang'],
  'graphql': [],
  'haskell': ['hs'],
  'html': [],
  'ini': [],
  'java': [],
  'javascript': ['js'],
  'json': [],
  'kotlin': ['kt'],
  'lua': [],
  'makefile': ['make'],
  'markdown': ['md'],
  'nginx': [],
  'objectivec': ['objc'],
  'perl': [],
  'php': [],
  'plaintext': ['text', 'txt'],
  'powershell': ['ps1'],
  'protobuf': ['proto'],
  'python': ['py'],
  'r': [],
  'ruby': ['rb'],
  'rust': ['rs'],
  'scala': [],
  'scss': [],
  'sql': [],
  'swift': [],
  'toml': [],
  'typescript': ['ts'],
  'xml': [],
  'yaml': ['yml'],
  'zig': [],
};

final Map<String, String> _byWord = {
  for (final e in highlightLanguages.entries) ...{
    e.key: e.key,
    for (final a in e.value) a: e.key,
  },
};

/// The canonical language for the word after a code fence, matched
/// without regard to case, or '' when it names none.
String canonicalLanguage(String word) => _byWord[word.toLowerCase()] ?? '';
