import 'package:flutter/material.dart';

import '../../core/themes/theme.dart';
import '../../state/app_prefs.dart' show Palette;

export '../../state/app_prefs.dart' show Palette;

/// The colour tokens of the approved style guide (docs/style-guide.html,
/// ruling R60), as plain data: one [SchemeTokens] per theme, and the
/// role colours the high-distinction palette swaps.
///
/// The token names are the guide's CSS custom properties, so a scheme can
/// be written as a map of those names to `#rrggbb` ([SchemeTokens.fromHex])
/// and checked against the guide line by line. Nothing else in the app
/// writes a hex value: widgets take [ChurrColors.of].
@immutable
class SchemeTokens {
  const SchemeTokens({
    required this.bg,
    required this.panel,
    required this.raise,
    required this.line,
    required this.edge,
    required this.ink,
    required this.dim,
    required this.faint,
    required this.lav,
    required this.cyan,
    required this.pink,
    required this.yel,
    required this.onLav,
    required this.onCyan,
    required this.onPink,
    required this.onYel,
    required this.yelInk,
    required this.focus,
    required this.grid,
  });

  /// Ground: the window, the message log.
  final Color bg;

  /// Panel: the sidebar, cards, dialogs.
  final Color panel;

  /// Raised: hover, the selected row, code.
  final Color raise;

  /// Line: the rare rule. Areas are told apart by fills, not lines.
  final Color line;

  /// Edge: the outline of a control (fields, switches); at least 3:1.
  final Color edge;
  final Color ink;
  final Color dim;
  final Color faint;

  /// The four role colours, each used as a fill with its partner text
  /// colour, and as text or line on ground, panel and raised.
  final Color lav;
  final Color cyan;
  final Color pink;

  /// Yellow is a fill only; [yelInk] is yellow when it is text or line.
  final Color yel;
  final Color onLav;
  final Color onCyan;
  final Color onPink;
  final Color onYel;
  final Color yelInk;

  /// The keyboard focus outline: yellow, or [yelInk] where yellow would
  /// not reach 3:1 (the light theme).
  final Color focus;

  /// The dot grid behind empty space.
  final Color grid;

  /// The names, in the guide's spelling.
  static const names = [
    'bg', 'panel', 'raise', 'line', 'edge', 'ink', 'dim', 'faint', //
    'lav',
    'cyan',
    'pink',
    'yel',
    'on-lav',
    'on-cyan',
    'on-pink',
    'on-yel',
    'yel-ink',
    'focus',
    'grid',
  ];

  Map<String, Color> toMap() => {
    'bg': bg, 'panel': panel, 'raise': raise, 'line': line, 'edge': edge, //
    'ink': ink, 'dim': dim, 'faint': faint, 'lav': lav, 'cyan': cyan,
    'pink': pink, 'yel': yel, 'on-lav': onLav, 'on-cyan': onCyan,
    'on-pink': onPink,
    'on-yel': onYel,
    'yel-ink': yelInk,
    'focus': focus,
    'grid': grid,
  };

  /// A scheme from `{name: '#rrggbb'}`; every name in [names] is needed.
  factory SchemeTokens.fromHex(Map<String, String> hex) {
    Color c(String k) {
      final v = hex[k];
      if (v == null) throw ArgumentError('scheme has no "$k"');
      return Color(0xFF000000 | int.parse(v.replaceFirst('#', ''), radix: 16));
    }

    return SchemeTokens(
      bg: c('bg'),
      panel: c('panel'),
      raise: c('raise'),
      line: c('line'), //
      edge: c('edge'),
      ink: c('ink'),
      dim: c('dim'),
      faint: c('faint'),
      lav: c('lav'),
      cyan: c('cyan'),
      pink: c('pink'),
      yel: c('yel'),
      onLav: c('on-lav'),
      onCyan: c('on-cyan'),
      onPink: c('on-pink'),
      onYel: c('on-yel'),
      yelInk: c('yel-ink'),
      focus: c('focus'),
      grid: c('grid'),
    );
  }

  /// This scheme with some tokens replaced, by name.
  SchemeTokens override(Map<String, String> hex) {
    if (hex.isEmpty) return this;
    final m = {
      for (final e in toMap().entries)
        e.key:
            '#${(e.value.toARGB32() & 0xFFFFFF).toRadixString(16).padLeft(6, '0')}',
      ...hex,
    };
    return SchemeTokens.fromHex(m);
  }
}

/// The guide's two themes and its high-distinction palette, and every
/// other theme, as [SchemeTokens]. The tables and the derivation live in
/// `lib/core/themes` ([themeTokens]): a built-in theme and a custom one
/// reach the screen by the same path.
abstract final class GuideSchemes {
  static const darkHex = BuiltInTokens.dark;
  static const lightHex = BuiltInTokens.light;
  static const darkHighDistinction = BuiltInTokens.darkHighDistinction;
  static const lightHighDistinction = BuiltInTokens.lightHighDistinction;

  /// The system's "increase contrast": every line takes the stronger edge
  /// colour, and faint text is drawn as dim.
  static Map<String, String> increaseContrast(SchemeTokens base) => {
    'line': _hex(base.edge),
    'faint': _hex(base.dim),
  };

  static String _hex(Color c) =>
      '#${(c.toARGB32() & 0xFFFFFF).toRadixString(16).padLeft(6, '0')}';

  /// The built-in theme for one brightness and palette.
  static ChurrTheme builtIn({
    required bool dark,
    Palette palette = Palette.standard,
  }) => palette == Palette.highDistinction
      ? (dark ? BuiltInThemes.darkHd : BuiltInThemes.lightHd)
      : (dark ? BuiltInThemes.dark : BuiltInThemes.light);

  /// The tokens for any theme, with or without "increase contrast".
  static SchemeTokens forTheme(ChurrTheme theme, {bool highContrast = false}) {
    var t = SchemeTokens.fromHex(themeTokens(theme));
    if (highContrast) t = t.override(increaseContrast(t));
    return t;
  }

  /// The tokens for one built-in theme, palette and contrast setting.
  static SchemeTokens resolve({
    required bool dark,
    Palette palette = Palette.standard,
    bool highContrast = false,
  }) => forTheme(
    builtIn(dark: dark, palette: palette),
    highContrast: highContrast,
  );
}

/// The tokens as a theme extension. Widgets take colours from here
/// (`ChurrColors.of(context)`) and never write a hex value of their own.
@immutable
class ChurrColors extends ThemeExtension<ChurrColors> {
  const ChurrColors(this.t, {required this.dark});

  factory ChurrColors.resolve({
    required bool dark,
    Palette palette = Palette.standard,
    bool highContrast = false,
  }) => ChurrColors(
    GuideSchemes.resolve(
      dark: dark,
      palette: palette,
      highContrast: highContrast,
    ),
    dark: dark,
  );

  /// The colours of any theme: built-in, saved, a server's default, or
  /// one being edited or tried.
  factory ChurrColors.forTheme(ChurrTheme theme, {bool highContrast = false}) =>
      ChurrColors(
        GuideSchemes.forTheme(theme, highContrast: highContrast),
        dark: theme.dark,
      );

  final SchemeTokens t;
  final bool dark;

  Color get bg => t.bg;
  Color get panel => t.panel;
  Color get raised => t.raise;
  Color get line => t.line;

  /// The edge of a control: fields, switches, the quiet button's underline.
  Color get edge => t.edge;
  Color get ink => t.ink;
  Color get dim => t.dim;
  Color get faint => t.faint;

  /// Brand and "yours": your mentions, your selection.
  Color get lavender => t.lav;

  /// Live and go: links, the main action, online, the current channel.
  Color get cyan => t.cyan;

  /// Attention: new, alerts, destructive actions, do not disturb.
  Color get pink => t.pink;

  /// Yellow as a fill: the unread count, highlights.
  Color get yellow => t.yel;

  /// Yellow as text, line or icon.
  Color get yellowInk => t.yelInk;

  /// Green, for the verified shield alone (`fair_mark.dart`): a real green,
  /// not the cyan of "live and go", as the web chat's `--green`. The
  /// guide's green for the theme's brightness, taken lighter (on dark) or
  /// darker (on light) where a theme's surfaces would leave it under 4.5:1.
  Color get green {
    var g = dark ? _greenDark : _greenLight;
    final toward = dark ? const Color(0xFFFFFFFF) : const Color(0xFF000000);
    for (var i = 0; i < 20 && _leastContrast(g) < 4.6; i++) {
      g = Color.lerp(g, toward, 0.1)!;
    }
    return g;
  }

  static const _greenDark = Color(0xFF4ADE80);
  static const _greenLight = Color(0xFF12733A);

  double _leastContrast(Color fg) {
    final l = fg.computeLuminance();
    var least = double.infinity;
    for (final b in [t.bg, t.panel, t.raise]) {
      final m = b.computeLuminance();
      final r = (l > m ? l + 0.05 : m + 0.05) / (l > m ? m + 0.05 : l + 0.05);
      if (r < least) least = r;
    }
    return least;
  }
  Color get onLavender => t.onLav;
  Color get onCyan => t.onCyan;
  Color get onPink => t.onPink;
  Color get onYellow => t.onYel;
  Color get focus => t.focus;
  Color get link => t.cyan;
  Color get grid => t.grid;

  /// The text colour that goes on one of the four fills.
  Color on(Color fill) => fill == t.lav
      ? t.onLav
      : fill == t.cyan
      ? t.onCyan
      : fill == t.pink
      ? t.onPink
      : fill == t.yel
      ? t.onYel
      : t.ink;

  /// The four fills, in a fixed order, for things that are told apart by
  /// a colour picked from their id (avatars, server tiles).
  List<Color> get fills => [t.lav, t.cyan, t.yel, t.pink];

  /// A fill picked from an id, so a person or server keeps its colour.
  Color fillFor(String? id) => fills[(id?.hashCode ?? 0).abs() % 4];

  /// A card's or group's colour label, by the seven names of
  /// docs/MESSAGE-FORMAT.md section 6. The guide allows no fifth colour,
  /// so green reuses cyan, red reuses pink and grey is faint.
  Color label(String? name) => switch (name) {
    'lavender' => t.lav,
    'cyan' || 'green' => t.cyan,
    'pink' || 'red' => t.pink,
    'yellow' => t.yel,
    _ => t.faint,
  };

  static ChurrColors of(BuildContext context) =>
      Theme.of(context).extension<ChurrColors>()!;

  @override
  ChurrColors copyWith() => this;

  // Stepped, never blended: a theme change is a cut.
  @override
  ChurrColors lerp(ChurrColors? other, double t) =>
      t < 0.5 ? this : other ?? this;
}

/// The two bundled families: Archivo (variable, with weight and width
/// axes) for the interface, Martian Mono only for times, counts, code and
/// small tags.
const fontText = 'Archivo';
const fontMono = 'Martian Mono';

/// Archivo at a weight and width (width in percent, 62 to 125). The
/// bundled font is variable: the engine maps [FontWeight] onto its weight
/// axis itself (an explicit `wght` variation would override every later
/// weight), so only the width axis is set here.
TextStyle archivo({
  double? size,
  double weight = 400,
  double width = 100,
  Color? color,
  double? height,
  double? letterSpacing,
}) => TextStyle(
  fontFamily: fontText,
  fontSize: size,
  fontWeight: _weight(weight),
  fontVariations: width == 100 ? null : [FontVariation.width(width)],
  color: color,
  height: height,
  letterSpacing: letterSpacing,
);

FontWeight _weight(double w) =>
    FontWeight.values[((w / 100).round() - 1).clamp(0, 8)];

/// The guide's type roles (its "type" section).
abstract final class ChurrType {
  /// Display: Archivo 900, width 70%, upper case. Empty states, sign-in.
  static TextStyle display(Color color, {double size = 40}) =>
      archivo(size: size, weight: 900, width: 70, color: color, height: 0.92);

  /// Title: Archivo 800, width 85%. Conversation and screen titles.
  static TextStyle title(Color color, {double size = 19}) =>
      archivo(size: size, weight: 800, width: 85, color: color, height: 1.1);

  /// Body: Archivo 400, 16/24. Message text is never mono, upper case or tilted.
  static TextStyle body(Color color, {double size = 16}) =>
      archivo(size: size, color: color, height: 1.5);

  /// Label: Archivo 700, width 85%, 12px, upper case (the caller upper-cases).
  static TextStyle label(Color color, {double size = 11}) => archivo(
    size: size,
    weight: 700,
    width: 85,
    color: color,
    letterSpacing: size * 0.1,
    height: 1.3,
  );

  /// Button: Archivo 800, width 85%, upper case.
  static TextStyle button(Color color, {double size = 14}) => archivo(
    size: size,
    weight: 800,
    width: 85,
    color: color,
    letterSpacing: 0.56,
    height: 1,
  );
}

/// A Martian Mono style for times, counts, code and small tags.
TextStyle monoStyle(
  BuildContext context, {
  double size = 11,
  Color? color,
  FontWeight weight = FontWeight.w400,
}) => TextStyle(
  fontFamily: fontMono,
  fontSize: size,
  fontWeight: weight,
  color: color ?? ChurrColors.of(context).dim,
  height: 1.3,
);
