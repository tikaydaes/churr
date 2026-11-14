// Every text and control pairing the app draws, in both themes, both
// palettes and with the system's "increase contrast" on and off, held to
// the style guide's floor: 4.5:1 for text, 3:1 for control edges and
// icons (docs/style-guide.html, "contrast and colour vision"; R60).
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;
import 'dart:ui';

import 'package:churr/core/themes/theme.dart';
import 'package:churr/ui/message/form_view.dart' show pollBar;
import 'package:churr/ui/theme/tokens.dart';
import 'package:flutter_test/flutter_test.dart';

double _lum(Color c) {
  double f(double x) =>
      x <= 0.03928 ? x / 12.92 : math.pow((x + 0.055) / 1.055, 2.4).toDouble();
  return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
}

double contrast(Color a, Color b) {
  final x = _lum(a), y = _lum(b);
  return (math.max(x, y) + 0.05) / (math.min(x, y) + 0.05);
}

typedef Pair = (String, Color, Color);

List<Pair> textPairs(ChurrColors c) {
  final grounds = {'bg': c.bg, 'panel': c.panel, 'raised': c.raised};
  return [
    for (final g in grounds.entries) ...[
      ('ink on ${g.key}', c.ink, g.value),
      ('dim on ${g.key}', c.dim, g.value),
      ('lavender text on ${g.key}', c.lavender, g.value),
      ('cyan text (links) on ${g.key}', c.cyan, g.value),
      ('pink text on ${g.key}', c.pink, g.value),
      ('yellow-ink text on ${g.key}', c.yellowInk, g.value),
    ],
    // Faint is used on ground and panel only: labels, times, quiet rows.
    // A selected (raised) row draws its text in ink.
    ('faint on bg', c.faint, c.bg),
    ('faint on panel', c.faint, c.panel),
    // The partner text colour of each fill.
    ('on-lavender on lavender (mentions, tags)', c.onLavender, c.lavender),
    ('on-cyan on cyan (primary button)', c.onCyan, c.cyan),
    ('on-pink on pink (hover, danger, NEW)', c.onPink, c.pink),
    ('on-yellow on yellow (unread count)', c.onYellow, c.yellow),
    // Tooltips are ink blocks with ground text.
    ('bg on ink (tooltip)', c.bg, c.ink),
    // Poll options: ink over the tally bars.
    ('ink on poll bar', c.ink, pollBar(c, mine: false)),
    ('ink on own-vote bar', c.ink, pollBar(c, mine: true)),
    // Initials on every avatar fill.
    for (final f in c.fills)
      ('initials on fill ${f.toARGB32().toRadixString(16)}', c.on(f), f),
  ];
}

List<Pair> controlPairs(ChurrColors c) {
  final grounds = {'bg': c.bg, 'panel': c.panel, 'raised': c.raised};
  return [
    for (final g in grounds.entries) ...[
      ('control edge on ${g.key}', c.edge, g.value),
      ('focus outline on ${g.key}', c.focus, g.value),
      ('cyan (switch on, selected bar) on ${g.key}', c.cyan, g.value),
      ('dim icons on ${g.key}', c.dim, g.value),
      ('faint marks on ${g.key}', c.faint, g.value),
      ('lavender edge (yours) on ${g.key}', c.lavender, g.value),
      ('pink (do not disturb) on ${g.key}', c.pink, g.value),
      ('yellow-ink (away) on ${g.key}', c.yellowInk, g.value),
    ],
    // The unread count's yellow sits inside an ink edge, which is what
    // parts it from the panel in the light theme.
    ('ink edge of the yellow count against panel', c.ink, c.panel),
    ('ink edge of the yellow count against raised', c.ink, c.raised),
  ];
}

void main() {
  for (final dark in [true, false]) {
    for (final palette in Palette.values) {
      for (final high in [false, true]) {
        final name =
            '${dark ? 'dark' : 'light'}, ${palette.name}${high ? ', increase contrast' : ''}';
        test('contrast: $name', () {
          final c = ChurrColors.resolve(
            dark: dark,
            palette: palette,
            highContrast: high,
          );
          final failures = [
            for (final (what, fg, bg) in textPairs(c))
              if (contrast(fg, bg) < 4.5)
                '$what: ${contrast(fg, bg).toStringAsFixed(2)} (needs 4.5)',
            for (final (what, fg, bg) in controlPairs(c))
              if (contrast(fg, bg) < 3)
                '$what: ${contrast(fg, bg).toStringAsFixed(2)} (needs 3)',
          ];
          expect(failures, isEmpty, reason: failures.join('\n'));
        });
      }
    }
  }

  test('increase contrast draws every line in the edge colour', () {
    for (final dark in [true, false]) {
      final c = ChurrColors.resolve(dark: dark, highContrast: true);
      expect(c.line, c.edge);
      expect(contrast(c.line, c.panel), greaterThanOrEqualTo(3));
    }
  });

  test('the tokens are the guide\'s, and a scheme is plain data', () {
    final dark = GuideSchemes.resolve(dark: true);
    expect(dark.bg, const Color(0xFF0D1026));
    expect(dark.cyan, const Color(0xFF5DF0E4));
    final light = GuideSchemes.resolve(
      dark: false,
      palette: Palette.highDistinction,
    );
    expect(light.lav, const Color(0xFF1F56C4));
    expect(light.pink, const Color(0xFFA8480A));
    // A custom scheme can be written as the guide's names to hex.
    final custom = SchemeTokens.fromHex({
      ...GuideSchemes.darkHex,
      'cyan': '#00ff00',
    });
    expect(custom.cyan, const Color(0xFF00FF00));
    expect(custom.toMap().keys, containsAll(SchemeTokens.names));
  });

  // A custom theme that passes the check of docs/STYLE.md "Themes" (the
  // generated themes the Go package accepted) holds every pairing the app
  // draws, as the built-in ones do.
  test('every theme the check accepts holds the app\'s pairings', () {
    final corpus = jsonDecode(
      File('test/core/themes/palette_corpus.json').readAsStringSync(),
    ) as Map;
    var n = 0;
    for (final e in corpus['themes'] as List) {
      if (e['refusal'] != '' || e['kind_fits'] != true) continue;
      final theme = ChurrTheme(
        id: '',
        name: 'generated',
        kind: e['kind'] as String,
        colours: Map<String, String>.from(e['colours'] as Map),
      );
      for (final high in [false, true]) {
        final c = ChurrColors.forTheme(theme, highContrast: high);
        final failures = [
          for (final (what, fg, bg) in textPairs(c))
            if (contrast(fg, bg) < 4.5)
              '$what: ${contrast(fg, bg).toStringAsFixed(2)}',
          for (final (what, fg, bg) in controlPairs(c))
            if (contrast(fg, bg) < 3)
              '$what: ${contrast(fg, bg).toStringAsFixed(2)}',
        ];
        expect(
          failures,
          isEmpty,
          reason: '${e['colours']}\n${failures.join('\n')}',
        );
      }
      n++;
    }
    expect(n, greaterThan(50));
  });
}
