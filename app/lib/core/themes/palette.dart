/// The colour arithmetic of themes (ruling R61, docs/STYLE.md "Themes"):
/// the base colours a theme stores, the colours every client derives from
/// them, and the contrast check that decides whether a theme may be saved,
/// shared or applied.
///
/// A line-for-line port of the server's `internal/themes/palette`. The
/// web chat, the server and this file must agree exactly, so
/// `test/core/themes/palette_test.dart` runs the Go package's own table
/// cases (copied into `test/core/themes/palette_cases.json`). Change one
/// and change all three.
///
/// Pure Dart: no Flutter, no I/O.
library;

import 'dart:math' as math;

/// The base token names a theme stores, in the order they are listed and
/// checked: the three ground steps, ink, dim, and the four role colours.
const themeKeys = [
  'bg',
  'panel',
  'raise',
  'ink',
  'dim',
  'lav',
  'cyan',
  'pink',
  'yel',
];

/// The two kinds of theme.
const kindDark = 'dark';
const kindLight = 'light';

/// The contrast a pairing needs.
const textRatio = 4.5; // text against what it sits on
const edgeRatio = 3.0; // control edges, icons, presence shapes, focus ring

/// One colour, 0 to 255 per channel.
class Rgb {
  const Rgb(this.r, this.g, this.b);
  final int r, g, b;

  static const black = Rgb(0, 0, 0);
  static const white = Rgb(255, 255, 255);

  /// Reads "#rrggbb" (either case); null for anything else.
  static Rgb? parse(String s) {
    if (s.length != 7 || s[0] != '#') return null;
    final out = <int>[];
    for (var i = 0; i < 3; i++) {
      final part = s.substring(1 + 2 * i, 3 + 2 * i);
      if (!RegExp(r'^[0-9a-fA-F]{2}$').hasMatch(part)) return null;
      out.add(int.parse(part, radix: 16));
    }
    return Rgb(out[0], out[1], out[2]);
  }

  /// "#rrggbb", lower case.
  String get hex =>
      '#${[r, g, b].map((v) => v.toRadixString(16).padLeft(2, '0')).join()}';

  /// 0xFFrrggbb, for a Flutter `Color`.
  int get argb => 0xFF000000 | (r << 16) | (g << 8) | b;

  int operator [](int i) => i == 0 ? r : (i == 1 ? g : b);

  @override
  bool operator ==(Object other) =>
      other is Rgb && other.r == r && other.g == g && other.b == b;

  @override
  int get hashCode => Object.hash(r, g, b);

  @override
  String toString() => hex;
}

/// Each channel value's linear light, the inner step of WCAG 2 relative
/// luminance (v / 255 / 12.92 up to v = 10, otherwise
/// ((v / 255 + 0.055) / 1.055) ^ 2.4), worked out exactly and rounded
/// once to the nearest double. The server's table (`internal/themes/
/// palette`, which holds its generator), literal for literal: written out
/// so that no `pow` takes part, since platforms differ in its last bit.
// dart format off
const List<double> _linear = <double>[
  0, 0.0003035269835488375, 0.000607053967097675, 0.0009105809506465125,
  0.00121410793419535, 0.0015176349177441874, 0.001821161901293025, 0.0021246888848418626,
  0.0024282158683907, 0.0027317428519395373, 0.003035269835488375, 0.0033465357638991586,
  0.003676507324047435, 0.004024717018496304, 0.0043914420374102925, 0.0047769534806937275,
  0.005181516702338385, 0.005605391624202721, 0.006048833022857052, 0.006512090792594472,
  0.006995410187265385, 0.00749903204322617, 0.008023192985384993, 0.008568125618069302,
  0.009134058702220785, 0.009721217320237844, 0.010329823029626936, 0.01096009400648824,
  0.011612245179743881, 0.012286488356915867, 0.012983032342173007, 0.013702083047289683,
  0.014443843596092541, 0.015208514422912706, 0.015996293365509628, 0.016807375752887377,
  0.017641954488384078, 0.01850022012837969, 0.019382360956935723, 0.02028856305665239,
  0.021219010376003555, 0.022173884793387375, 0.023153366178110403, 0.02415763244850475,
  0.025186859627361623, 0.02624122189484989, 0.02732089163907489, 0.028426039504420787,
  0.029556834437808797, 0.03071344373299362, 0.03189603307301152, 0.03310476657088505,
  0.03433980680868216, 0.03560131487502032, 0.03688945040110002, 0.03820437159534648,
  0.03954623527673283, 0.04091519690685317, 0.042311410620809654, 0.04373502925697345,
  0.04518620438567554, 0.04666508633688008, 0.048171824226889405, 0.04970656598412722,
  0.051269458374043224, 0.05286064702318025, 0.054480276442442355, 0.05612849004960008,
  0.05780543019106721, 0.059511238162981185, 0.061246054231617594, 0.06301001765316766,
  0.06480326669290576, 0.06662593864377288, 0.06847816984440015, 0.07036009569659588,
  0.07227185068231748, 0.07421356838014961, 0.07618538148130781, 0.07818742180518633,
  0.08021982031446831, 0.0822827071298148, 0.08437621154414877, 0.08650046203654974,
  0.08865558628577294, 0.09084171118340768, 0.09305896284668742, 0.09530746663096466,
  0.09758734714186242, 0.09989872824711389, 0.10224173308810128, 0.10461648409110416,
  0.10702310297826759, 0.10946171077829933, 0.11193242783690557, 0.11443537382697372,
  0.11697066775851081, 0.1195384279883456, 0.12213877222960184, 0.12477181756095046,
  0.12743768043564743, 0.13013647669036427, 0.13286832155381792, 0.13563332965520564,
  0.13843161503245183, 0.14126329114027164, 0.14412847085805772, 0.14702726649759498,
  0.14995978981060853, 0.15292615199615014, 0.15592646370782734, 0.15896083506088035,
  0.162029375639111, 0.1651321945016676, 0.1682694001896907, 0.17144110073282254,
  0.17464740365558498, 0.17788841598362912, 0.18116424424986013, 0.1844749945004409,
  0.18782077230067779, 0.19120168274079136, 0.1946178304415757, 0.1980693195599488,
  0.20155625379439707, 0.2050787363903169, 0.20863687014525567, 0.2122307574140551,
  0.21586050011389915, 0.21952619972926918, 0.22322795731680842, 0.22696587351009834,
  0.23074004852434896, 0.23455058216100508, 0.23839757381227095, 0.24228112246555472,
  0.2462013267078354, 0.2501582847299533, 0.2541520943308267, 0.25818285292159576,
  0.262250657529696, 0.2663556048028623, 0.27049779101306576, 0.27467731206038454,
  0.27889426347681034, 0.28314874042999194, 0.2874408377269174, 0.29177064981753587,
  0.29613827079832095, 0.3005437944157764, 0.3049873140698861, 0.30946892281750843,
  0.3139887133757175, 0.31854677812509175, 0.3231432091129507, 0.32777809805654207,
  0.3324515363461792, 0.33716361504833026, 0.34191442490866075, 0.3467040563550295,
  0.3515325995004392, 0.3564001441459434, 0.36130677978350945, 0.3662525955988394,
  0.37123768047414896, 0.3762621229909062, 0.38132601143253, 0.3864294337870489,
  0.3915724777497231, 0.3967552307256268, 0.4019777798321956, 0.40724021190173665,
  0.4125426134839036, 0.41788507084813725, 0.4232676699860715, 0.42869049661390657,
  0.4341536361747488, 0.43965717384091874, 0.44520119451622775, 0.45078578283822335,
  0.4564110231804045, 0.46207699965440685, 0.4677837961121588, 0.4735314961480093,
  0.47932018310082664, 0.4851499400560704, 0.49102084984783545, 0.49693299506087035,
  0.5028864580325684, 0.5088813208549335, 0.5149176653765213, 0.5209955732043541,
  0.527115125705813, 0.533276404010505, 0.539479489012107, 0.5457244613701866,
  0.5520114015119999, 0.5583403896342677, 0.5647115057049289, 0.5711248294648729,
  0.5775804404296505, 0.584078417891164, 0.5906188409193368, 0.5972017883637631,
  0.6038273388553375, 0.6104955708078647, 0.6172065624196509, 0.6239603916750759,
  0.6307571363461467, 0.6375968739940324, 0.644479681970582, 0.6514056374198239,
  0.6583748172794482, 0.6653872982822719, 0.6724431569576873, 0.6795424696330937,
  0.6866853124353132, 0.6938717612919898, 0.7011018919329731, 0.7083757798916867,
  0.7156935005064805, 0.7230551289219689, 0.7304607400903533, 0.7379104087727307,
  0.7454042095403872, 0.7529422167760778, 0.7605245046752922, 0.7681511472475069,
  0.7758222183174234, 0.7835377915261932, 0.79129794033263, 0.7991027380144087,
  0.8069522576692514, 0.8148465722161011, 0.8227857543962833, 0.8307698767746545,
  0.8387990117407399, 0.8468732315098577, 0.8549926081242336, 0.863157213454102,
  0.8713671191987971, 0.8796223968878317, 0.8879231178819664, 0.8962693533742666,
  0.9046611743911491, 0.9130986517934189, 0.9215818562772945, 0.9301108583754234,
  0.9386857284578878, 0.9473065367331996, 0.955973353249286, 0.9646862478944651,
  0.9734452903984123, 0.982250550333117, 0.9911020971138297, 1,
];
// dart format on

/// One channel's linear light, 0 to 255.
double linear(int v) => _linear[v];

/// WCAG 2 relative luminance in the written order of docs/STYLE.md: three
/// products, then (r + g) + b. Dart rounds every double operation on its
/// own (IEEE 754) and does not fuse a multiply with an add, so this is the
/// server's answer bit for bit.
double luminance(Rgb c) {
  final r = 0.2126 * _linear[c.r];
  final g = 0.7152 * _linear[c.g];
  final b = 0.0722 * _linear[c.b];
  return (r + g) + b;
}

/// The WCAG contrast ratio of two colours, 1 to 21:
/// (Lhigh + 0.05) / (Llow + 0.05).
double ratio(Rgb a, Rgb b) {
  var x = luminance(a), y = luminance(b);
  if (x < y) (x, y) = (y, x);
  return (x + 0.05) / (y + 0.05);
}

/// A ratio as the editor and a refusal show it: cut, not rounded, to two
/// decimals, so a pairing that fails never reads as 4.50.
String measured(double r) =>
    ((r * 100).floorToDouble() / 100).toStringAsFixed(2);

/// [b] laid over [a] at [k] per cent, channel by channel, in whole
/// numbers: floor((a*(100-k) + b*k + 50) / 100).
Rgb mix(Rgb a, Rgb b, int k) {
  int ch(int x, int y) => (x * (100 - k) + y * k + 50) ~/ 100;
  return Rgb(ch(a.r, b.r), ch(a.g, b.g), ch(a.b, b.b));
}

/// A theme's base colours by key.
typedef Colours = Map<String, Rgb>;

/// What [parseColours] found wrong: the key at fault ("" for the object as
/// a whole) and a sentence.
class ColoursFault {
  const ColoursFault(this.key, this.message);
  final String key;
  final String message;
}

/// Checks a theme's stored colours: exactly the keys of [themeKeys], each
/// a six-digit hex colour. Returns the colours, or the fault.
(Colours?, ColoursFault?) parseColours(Map<String, String>? raw) {
  if (raw == null || raw.isEmpty) {
    return (null, const ColoursFault('', "Give the theme's colours."));
  }
  final all = themeKeys.join(', ');
  // Go ranges over a map in no fixed order; an unknown key is reported
  // before a missing one either way, which is what the cases check.
  for (final k in raw.keys) {
    if (!themeKeys.contains(k)) {
      return (
        null,
        ColoursFault(k, '"$k" is not a theme colour. The colours are $all.'),
      );
    }
  }
  final out = <String, Rgb>{};
  for (final k in themeKeys) {
    final v = raw[k];
    if (v == null) {
      return (
        null,
        ColoursFault(k, 'The colour "$k" is missing. The colours are $all.'),
      );
    }
    final c = Rgb.parse(v);
    if (c == null) {
      return (
        null,
        ColoursFault(
          k,
          'The colour "$k" must be six hex digits after #, like #0d1026.',
        ),
      );
    }
    out[k] = c;
  }
  return (out, null);
}

/// The colours as the stored object: each key to "#rrggbb".
Map<String, String> hexes(Colours c) => {
  for (final k in themeKeys)
    if (c[k] != null) k: c[k]!.hex,
};

/// The colours a client works out from the base colours.
class Derived {
  const Derived({
    required this.onLav,
    required this.onCyan,
    required this.onPink,
    required this.onYel,
    required this.faint,
    required this.edge,
    required this.yelInk,
  });

  /// The partner text colour of each role fill.
  final Rgb onLav, onCyan, onPink, onYel;

  /// Section names, times, placeholders.
  final Rgb faint;

  /// The edge of a control.
  final Rgb edge;

  /// Yellow where it must be text or a thin line.
  final Rgb yelInk;
}

/// Mix(from, to, k) for the smallest k from 0 to 100 at which it reaches
/// [need] against every surface, or [to] if none does.
Rgb _smallest(Rgb from, Rgb to, double need, List<Rgb> surfaces) {
  for (var k = 0; k <= 100; k++) {
    final c = mix(from, to, k);
    if (surfaces.every((s) => ratio(c, s) >= need)) return c;
  }
  return to;
}

/// Works out the derived colours (docs/STYLE.md, "Themes"):
///
/// - on-X for each role fill X is the theme's ink or its ground (bg),
///   whichever has the higher contrast against X; ink on a tie.
/// - faint is mix(bg, dim, k) for the smallest k that reaches 4.5:1
///   against bg and panel.
/// - edge is mix(bg, dim, k) for the smallest k that reaches 3:1 against
///   bg, panel and raise.
/// - yel-ink is mix(yel, ink, k) for the smallest k that reaches 4.5:1
///   against bg, panel and raise (yel itself when it already does).
Derived derive(Colours c) {
  final bg = c['bg']!, panel = c['panel']!, raise = c['raise']!;
  final ink = c['ink']!, dim = c['dim']!;
  Rgb partner(Rgb fill) => ratio(ink, fill) >= ratio(bg, fill) ? ink : bg;
  return Derived(
    onLav: partner(c['lav']!),
    onCyan: partner(c['cyan']!),
    onPink: partner(c['pink']!),
    onYel: partner(c['yel']!),
    faint: _smallest(bg, dim, textRatio, [bg, panel]),
    edge: _smallest(bg, dim, edgeRatio, [bg, panel, raise]),
    yelInk: _smallest(c['yel']!, ink, textRatio, [bg, panel, raise]),
  );
}

/// One thing drawn on another that must reach a contrast.
class Pairing {
  const Pairing(this.fore, this.back, this.need);

  /// Token names, as docs/STYLE.md writes them.
  final String fore, back;
  final double need;

  /// How the pairing is written in a refusal: "dim on raise".
  String get name => '$fore on $back';

  bool get isText => need == textRatio;
}

/// What [check] measures, in order. Every role colour either client
/// draws as text (cyan, pink, lav, and yellow as yel-ink; yel itself is
/// never text) must reach 4.5:1 on each ground step; lavender's icons and
/// edges need only 3:1, which that covers. A dark theme also has the
/// yellow focus ring on each ground; on a light theme the ring carries a dark
/// halo, so ink on each ground (already a text pairing) covers it.
List<Pairing> pairings(String kind) {
  final out = <Pairing>[];
  void on(String fore, double need, List<String> backs) {
    for (final b in backs) {
      out.add(Pairing(fore, b, need));
    }
  }

  const all = ['bg', 'panel', 'raise'];
  // Text: 4.5:1.
  on('ink', textRatio, all);
  on('dim', textRatio, all);
  on('faint', textRatio, ['bg', 'panel']); // faint is never text on raise
  on('cyan', textRatio, all); // links
  on('pink', textRatio, all); // problems, danger in a menu
  on('yel-ink', textRatio, all); // yellow as text
  on('lav', textRatio, all); // mentions, channel names, labels, code
  on('on-lav', textRatio, ['lav']);
  on('on-cyan', textRatio, ['cyan']);
  on('on-pink', textRatio, ['pink']);
  on('on-yel', textRatio, ['yel']);
  // Control edges, icons and shapes: 3:1.
  on('edge', edgeRatio, all);
  on('faint', edgeRatio, ['raise']); // the offline light, an off switch
  if (kind == kindDark) on('yel', edgeRatio, all); // the focus ring
  return out;
}

/// A base or derived colour by its docs/STYLE.md name.
Rgb token(Colours c, Derived d, String name) => switch (name) {
  'on-lav' => d.onLav,
  'on-cyan' => d.onCyan,
  'on-pink' => d.onPink,
  'on-yel' => d.onYel,
  'faint' => d.faint,
  'edge' => d.edge,
  'yel-ink' => d.yelInk,
  _ => c[name]!,
};

/// One pairing as measured.
class Measure {
  const Measure(this.pairing, this.ratio);
  final Pairing pairing;
  final double ratio;

  bool get passes => ratio >= pairing.need;

  /// Cut to two decimals: "4.06".
  String get shown => measured(ratio);

  /// What failed, for a refusal: "dim on raise is 4.06:1; text needs at
  /// least 4.5:1." (the server's `invalid_value` message, word for word).
  String get message {
    final what = pairing.isText ? 'text' : 'a control edge';
    return '${pairing.name} is $shown:1; $what needs at least '
        '${_g(pairing.need)}:1.';
  }
}

/// Go's %g for the two needs: "4.5", "3".
String _g(double v) =>
    v == v.truncateToDouble() ? v.toInt().toString() : v.toString();

/// Every pairing of a theme, measured, in [pairings] order.
List<Measure> measureAll(String kind, Colours c) {
  final d = derive(c);
  return [
    for (final p in pairings(kind))
      Measure(p, ratio(token(c, d, p.fore), token(c, d, p.back))),
  ];
}

/// The first pairing below its need, or null when the theme may be saved.
Measure? check(String kind, Colours c) {
  for (final m in measureAll(kind, c)) {
    if (!m.passes) return m;
  }
  return null;
}

/// Whether the colours are of the kind: a dark theme's ground is darker
/// than its ink, a light theme's lighter.
bool kindFits(String kind, Colours c) {
  final g = luminance(c['bg']!), i = luminance(c['ink']!);
  return switch (kind) {
    kindDark => g < i,
    kindLight => g > i,
    _ => false,
  };
}

/// Whether a theme may be saved, shared and applied: its kind fits and
/// every pairing reaches its need.
bool valid(String kind, Colours c) =>
    kindFits(kind, c) && check(kind, c) == null;

// ---- fixing (the editor's "fix"; the app's own, not in the Go package) ----

/// The base colours a token is made from.
Set<String> tokenSources(String name) => switch (name) {
  'on-lav' => {'lav', 'ink', 'bg'},
  'on-cyan' => {'cyan', 'ink', 'bg'},
  'on-pink' => {'pink', 'ink', 'bg'},
  'on-yel' => {'yel', 'ink', 'bg'},
  'faint' => {'bg', 'dim', 'panel'},
  'edge' => {'bg', 'dim', 'panel', 'raise'},
  'yel-ink' => {'yel', 'ink', 'bg', 'panel', 'raise'},
  _ => {name},
};

/// The base colour the editor changes to fix a failing pairing: the one
/// the foreground is made from (dim for faint and edge, yel for yel-ink,
/// the fill for a partner text colour, whose own choice is fixed).
String fixKey(Pairing p) => switch (p.fore) {
  'faint' || 'edge' => 'dim',
  'yel-ink' => 'yel',
  'on-lav' => 'lav',
  'on-cyan' => 'cyan',
  'on-pink' => 'pink',
  'on-yel' => 'yel',
  _ => p.fore,
};

/// The smallest change to [key] that makes every pairing it takes part in
/// pass: mix it towards black or towards white by the smallest whole k
/// (1 to 100) that does, whichever direction needs less (black on a tie).
/// The kind must still fit. Null when no amount in either direction does.
Colours? fix(String kind, Colours c, String key) {
  bool ok(Colours x) {
    if (!kindFits(kind, x)) return false;
    for (final m in measureAll(kind, x)) {
      final involved =
          tokenSources(m.pairing.fore).contains(key) ||
          tokenSources(m.pairing.back).contains(key);
      if (involved && !m.passes) return false;
    }
    return true;
  }

  if (ok(c)) return c;
  final from = c[key]!;
  for (var k = 1; k <= 100; k++) {
    for (final to in const [Rgb.black, Rgb.white]) {
      final next = {...c, key: mix(from, to, k)};
      if (ok(next)) return next;
    }
  }
  return null;
}

// ---- hue, saturation and lightness (the editor's picker) ----

/// A colour as hue (0 to 359), saturation and lightness (0 to 100), in
/// whole numbers, for the editor's sliders.
(int h, int s, int l) toHsl(Rgb c) {
  final r = c.r / 255, g = c.g / 255, b = c.b / 255;
  final mx = math.max(r, math.max(g, b)), mn = math.min(r, math.min(g, b));
  final l = (mx + mn) / 2;
  if (mx == mn) return (0, 0, (l * 100).round());
  final d = mx - mn;
  final s = l > 0.5 ? d / (2 - mx - mn) : d / (mx + mn);
  double h;
  if (mx == r) {
    h = (g - b) / d + (g < b ? 6 : 0);
  } else if (mx == g) {
    h = (b - r) / d + 2;
  } else {
    h = (r - g) / d + 4;
  }
  return ((h * 60).round() % 360, (s * 100).round(), (l * 100).round());
}

/// The colour for hue [h] (degrees), saturation [s] and lightness [l]
/// (per cent).
Rgb fromHsl(int h, int s, int l) {
  final sf = s.clamp(0, 100) / 100, lf = l.clamp(0, 100) / 100;
  final hf = (h % 360) / 360;
  if (sf == 0) {
    final v = (lf * 255).round();
    return Rgb(v, v, v);
  }
  final q = lf < 0.5 ? lf * (1 + sf) : lf + sf - lf * sf;
  final p = 2 * lf - q;
  double ch(double t) {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1 / 6) return p + (q - p) * 6 * t;
    if (t < 1 / 2) return q;
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
    return p;
  }

  int v(double x) => (x * 255).round().clamp(0, 255);
  return Rgb(v(ch(hf + 1 / 3)), v(ch(hf)), v(ch(hf - 1 / 3)));
}
