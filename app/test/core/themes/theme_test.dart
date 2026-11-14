// Themes as the app keeps them: the one path from a theme to the app's
// tokens, the .churrtheme file and theme links.
import 'package:churr/core/themes/palette.dart';
import 'package:churr/core/themes/theme.dart';
import 'package:churr/ui/theme/tokens.dart';
import 'package:flutter/painting.dart';
import 'package:flutter_test/flutter_test.dart';

ChurrTheme custom({String pink = '#ff70d6'}) => ChurrTheme(
  id: 't1',
  name: 'Night shift',
  kind: kindDark,
  colours: {...BuiltInThemes.dark.colours, 'pink': pink},
  serverAddress: 'https://chat.example.org',
);

void main() {
  test('a built-in theme is its hand-set table, not derived', () {
    expect(themeTokens(BuiltInThemes.dark), BuiltInTokens.dark);
    expect(themeTokens(BuiltInThemes.lightHd)['lav'], '#1f56c4');
    expect(themeTokens(BuiltInThemes.lightHd)['yel-ink'], '#7a5600');
    // And the app's colours come from that same table.
    expect(GuideSchemes.resolve(dark: false).bg, const Color(0xFFF5F2FF));
    expect(
      ChurrColors.forTheme(BuiltInThemes.darkHd).pink,
      const Color(0xFFFF9A4D),
    );
  });

  test('a custom theme is derived by STYLE.md and fills every app token', () {
    final t = custom();
    final tok = themeTokens(t);
    expect(tok.keys.toSet(), appTokenNames.toSet());
    final d = derive(t.parsed!);
    expect(tok['faint'], d.faint.hex);
    expect(tok['edge'], d.edge.hex);
    expect(tok['yel-ink'], d.yelInk.hex);
    expect(tok['on-pink'], d.onPink.hex);
    // What a theme does not name comes from the built-in of its kind.
    expect(tok['line'], BuiltInTokens.dark['line']);
    expect(tok['grid'], BuiltInTokens.dark['grid']);
    // The focus ring is yellow on dark, yel-ink on light.
    expect(tok['focus'], tok['yel']);
    final light = ChurrTheme(
      id: '',
      name: 'Paper',
      kind: kindLight,
      colours: BuiltInThemes.light.colours,
    );
    expect(themeTokens(light)['focus'], themeTokens(light)['yel-ink']);
    expect(ChurrColors.forTheme(t).pink, const Color(0xFFFF70D6));
    expect(ChurrColors.forTheme(t).dark, isTrue);
  });

  test('a theme that fails cannot be applied', () {
    final bad = custom(pink: '#c0207f');
    expect(bad.usable, isFalse);
    expect(bad.problem, 'pink on bg is 3.35:1; text needs at least 4.5:1.');
    expect(custom().usable, isTrue);
  });

  group('.churrtheme', () {
    test('round trip', () {
      final t = custom();
      final back = decodeThemeFile(encodeThemeFile(t));
      expect(back.name, t.name);
      expect(back.kind, t.kind);
      expect(back.colours, {for (final k in themeKeys) k: t.colours[k]});
      expect(encodeThemeFile(t), contains('"colours"'));
    });

    test('refusals', () {
      Matcher refused(String m) =>
          throwsA(isA<ThemeFileError>().having((e) => e.message, 'message', m));
      expect(
        () => decodeThemeFile('not json'),
        refused('This file is not a Churr theme.'),
      );
      expect(
        () => decodeThemeFile('{"name":"x","kind":"sepia","colours":{}}'),
        refused('The theme\'s kind must be "dark" or "light".'),
      );
      expect(
        () => decodeThemeFile(encodeThemeFile(custom(pink: '#c0207f'))),
        refused('pink on bg is 3.35:1; text needs at least 4.5:1.'),
      );
      expect(
        () => decodeThemeFile(
          '{"name":"x","kind":"dark","colours":{"bg":"#000000"}}',
        ),
        throwsA(isA<ThemeFileError>()),
      );
    });
  });

  test('theme links', () {
    const id = '0193a0e0-0000-7000-8000-0000000000c0';
    expect(
      themeLink('https://chat.example.org/', id),
      'https://chat.example.org/themes/$id',
    );
    expect(parseThemeLink('https://chat.example.org/themes/$id'), (
      'https://chat.example.org',
      id,
    ));
    expect(parseThemeLink('https://chat.example.org/themes/nope'), isNull);
    expect(parseThemeLink('https://chat.example.org/chat/c/$id'), isNull);
  });

  test('refs name where a theme lives', () {
    expect(BuiltInThemes.dark.ref, 'builtin:dark');
    expect(custom().ref, 's:https://chat.example.org#t1');
    expect(BuiltInThemes.byRef('builtin:light-hd'), BuiltInThemes.lightHd);
  });

  test('hue, saturation and lightness go back to the same colour', () {
    for (final hex in ['#0d1026', '#ff5ccf', '#5df0e4', '#ffffff', '#808080']) {
      final c = Rgb.parse(hex)!;
      final (h, s, l) = toHsl(c);
      final back = fromHsl(h, s, l);
      // Whole-number steps: within a few units per channel.
      for (var i = 0; i < 3; i++) {
        expect((back[i] - c[i]).abs(), lessThanOrEqualTo(3), reason: hex);
      }
    }
  });
}
