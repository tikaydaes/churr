import 'package:flutter/widgets.dart';
import 'package:re_highlight/languages/bash.dart';
import 'package:re_highlight/languages/c.dart';
import 'package:re_highlight/languages/cpp.dart';
import 'package:re_highlight/languages/csharp.dart';
import 'package:re_highlight/languages/css.dart';
import 'package:re_highlight/languages/dart.dart';
import 'package:re_highlight/languages/diff.dart';
import 'package:re_highlight/languages/dockerfile.dart';
import 'package:re_highlight/languages/elixir.dart';
import 'package:re_highlight/languages/go.dart';
import 'package:re_highlight/languages/graphql.dart';
import 'package:re_highlight/languages/haskell.dart';
import 'package:re_highlight/languages/ini.dart';
import 'package:re_highlight/languages/java.dart';
import 'package:re_highlight/languages/javascript.dart';
import 'package:re_highlight/languages/json.dart';
import 'package:re_highlight/languages/kotlin.dart';
import 'package:re_highlight/languages/lua.dart';
import 'package:re_highlight/languages/makefile.dart';
import 'package:re_highlight/languages/markdown.dart';
import 'package:re_highlight/languages/nginx.dart';
import 'package:re_highlight/languages/objectivec.dart';
import 'package:re_highlight/languages/perl.dart';
import 'package:re_highlight/languages/php.dart';
import 'package:re_highlight/languages/powershell.dart';
import 'package:re_highlight/languages/protobuf.dart';
import 'package:re_highlight/languages/python.dart';
import 'package:re_highlight/languages/r.dart';
import 'package:re_highlight/languages/ruby.dart';
import 'package:re_highlight/languages/rust.dart';
import 'package:re_highlight/languages/scala.dart';
import 'package:re_highlight/languages/scss.dart';
import 'package:re_highlight/languages/sql.dart';
import 'package:re_highlight/languages/swift.dart';
import 'package:re_highlight/languages/typescript.dart';
import 'package:re_highlight/languages/xml.dart';
import 'package:re_highlight/languages/yaml.dart';
import 'package:re_highlight/re_highlight.dart';

import '../theme/tokens.dart';

/// The grammar for each canonical language of
/// `lib/core/highlight/languages.dart`: exactly that list and nothing
/// more, so the apps and the web chat colour the same blocks.
///
/// `html` uses the XML grammar and `toml` the INI grammar, as
/// highlight.js itself does. `plaintext` has no grammar on purpose. `zig`
/// has none because `re_highlight` 0.0.3 does not carry one (it is not in
/// highlight.js core either): a zig block is accepted and shown in plain
/// monospace until a grammar is added here.
final Map<String, Mode> _grammars = {
  'bash': langBash,
  'c': langC,
  'cpp': langCpp,
  'csharp': langCsharp,
  'css': langCss,
  'dart': langDart,
  'diff': langDiff,
  'dockerfile': langDockerfile,
  'elixir': langElixir,
  'go': langGo,
  'graphql': langGraphql,
  'haskell': langHaskell,
  'html': langXml,
  'ini': langIni,
  'java': langJava,
  'javascript': langJavascript,
  'json': langJson,
  'kotlin': langKotlin,
  'lua': langLua,
  'makefile': langMakefile,
  'markdown': langMarkdown,
  'nginx': langNginx,
  'objectivec': langObjectivec,
  'perl': langPerl,
  'php': langPhp,
  'powershell': langPowershell,
  'protobuf': langProtobuf,
  'python': langPython,
  'r': langR,
  'ruby': langRuby,
  'rust': langRust,
  'scala': langScala,
  'scss': langScss,
  'sql': langSql,
  'swift': langSwift,
  'toml': langIni,
  'typescript': langTypescript,
  'xml': langXml,
  'yaml': langYaml,
};

/// The canonical languages that have a grammar, for the test that holds
/// this table to the format's list.
Set<String> get grammarLanguages => _grammars.keys.toSet();

final Highlight _highlight = Highlight()..registerLanguages(_grammars);

/// The colour of a highlight.js token name (docs/CLIENTS.md section 12):
/// keywords pink, strings yellow, numbers and constants cyan, names of
/// functions and types lavender, comments dim, everything else ink. The
/// web chat uses the same mapping.
Color? tokenColor(String scope, ChurrColors c) {
  // A scope may be dotted (`title.function`); the first part decides.
  final head = scope.split('.').first;
  return switch (head) {
    'keyword' ||
    'built_in' ||
    'selector-tag' ||
    'meta' ||
    'tag' ||
    'doctag' => c.pink,
    'string' || 'regexp' || 'char' || 'addition' || 'attr' => c.yellowInk,
    'number' ||
    'literal' ||
    'symbol' ||
    'variable' ||
    'attribute' ||
    'bullet' ||
    'link' => c.cyan,
    'title' ||
    'type' ||
    'class' ||
    'function' ||
    'name' ||
    'section' ||
    'selector-class' ||
    'selector-id' => c.lavender,
    'comment' || 'quote' || 'deletion' => c.dim,
    _ => null,
  };
}

class _SpanRenderer implements HighlightRenderer {
  _SpanRenderer(this.colors);
  final ChurrColors colors;
  final List<InlineSpan> out = [];
  final List<Color?> _stack = [];

  @override
  void addText(String text) {
    final color = _stack.lastWhere((c) => c != null, orElse: () => null);
    out.add(
      TextSpan(
        text: text,
        style: color == null ? null : TextStyle(color: color),
      ),
    );
  }

  @override
  void openNode(DataNode node) =>
      _stack.add(node.scope == null ? null : tokenColor(node.scope!, colors));

  @override
  void closeNode(DataNode node) {
    if (_stack.isNotEmpty) _stack.removeLast();
  }
}

/// The code as coloured spans, or one plain span when the language is
/// empty, has no grammar, or the grammar fails. The language is never
/// guessed.
List<InlineSpan> highlightCode(
  String code,
  String language,
  ChurrColors colors,
) {
  if (!_grammars.containsKey(language)) return [TextSpan(text: code)];
  try {
    final r = _SpanRenderer(colors);
    _highlight.highlight(code: code, language: language).render(r);
    return r.out.isEmpty ? [TextSpan(text: code)] : r.out;
  } on Object {
    return [TextSpan(text: code)];
  }
}
