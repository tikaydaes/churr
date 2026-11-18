// The chance artwork (docs/ART.md): cards, tiles and six-sided dice drawn
// from one sprite sheet per set, bundled under assets/chance/ by
// tools/art-gen/pack.py. A sheet is read the first time something from its
// set is shown. With no sheet, or one that fails to load, a slot keeps its
// text face, as before. Every other die, and a coin, is drawn on the fly
// (types/dice_roll/dice_shape.dart). Face down, a card shows the french sheet's card back
// at its own deck's size, and a tile the tile back drawn here by rule
// ([TileBack]).

import 'dart:convert';

import 'package:flutter/widgets.dart';

import '../types/dice_roll/dice_shape.dart';

/// The suit black of a card's lettering: the card's own colour, the same
/// on every theme (docs/ART.md).
const cardSuitBlack = Color(0xFF111111);

/// Where the sheets are bundled.
const chanceArtDir = 'assets/chance/';

/// Sheets are drawn at twice the pixels of their logical size.
const chanceArtScale = 0.5;

/// The sheet each deck draws from (testdata/art/sheets.json).
const chanceDeckSheets = <String, String>{
  'standard': 'french',
  'jokers': 'french',
  'jokers3': 'french',
  'piquet': 'french',
  'durak': 'french',
  'euchre': 'french',
  'pinochle': 'french',
  'canasta': 'french',
  'shoe2': 'french',
  'shoe4': 'french',
  'shoe6': 'french',
  'shoe8': 'french',
  'tarot': 'tarot',
  'spanish': 'latin',
  'italian': 'latin',
  'german': 'german',
  'austrian': 'german',
  'swiss': 'swiss',
  'hanafuda': 'hanafuda',
  'mahjong': 'mahjong',
};

/// One sheet's index, as pack.py writes it.
class ChanceSheet {
  ChanceSheet({
    required this.name,
    required this.image,
    required this.cell,
    required this.size,
    required this.faces,
  });

  factory ChanceSheet.fromJson(Map<String, dynamic> j) {
    Size size(Object? v) {
      final l = (v as List).cast<num>();
      return Size(l[0].toDouble(), l[1].toDouble());
    }

    final faces = <String, (int, int)>{
      for (final e in (j['faces'] as Map).entries)
        e.key as String: (
          ((e.value as List)[0] as num).toInt(),
          ((e.value as List)[1] as num).toInt(),
        ),
    };
    final name = j['sheet'] as String;
    return ChanceSheet(
      name: name,
      image: chanceArtDir + ((j['image'] as String?) ?? '$name.webp'),
      cell: size(j['cell']),
      size: size(j['size']),
      faces: faces,
    );
  }

  final String name;

  /// The asset path of the picture.
  final String image;
  final Size cell;
  final Size size;
  final Map<String, (int, int)> faces;

  /// The cell of a face in the sheet, in sheet pixels, or null.
  Rect? cellOf(String face) {
    final at = faces[face];
    if (at == null) return null;
    return Rect.fromLTWH(
      at.$1 * cell.width,
      at.$2 * cell.height,
      cell.width,
      cell.height,
    );
  }
}

/// Where a slot's pictures come from: the sheet, and how a frame's key (a
/// card, or a die's value) becomes a face.
typedef SlotArt = ({String sheet, String? Function(String key) face});

/// The sprite artwork of one slot of a result, or null. [sides] is a
/// die's; only a six-sided die has a sprite ([slotDie] for the others).
SlotArt? slotArt(String? kind, String? deck, {int? sides, bool still = false}) {
  if (still) return null;
  if (kind == 'draw' || kind == 'tarot') {
    final sheet = chanceDeckSheets[deck];
    return sheet == null ? null : (sheet: sheet, face: (k) => k);
  }
  if (kind == 'roll' && sides == 6) {
    return (sheet: 'dice-cube', face: (k) => 'd6-$k');
  }
  return null;
}

/// The sides of the die a slot is drawn as on the fly (dice_shape.dart):
/// any die but the six-sided one, and a flip's coin (2); or null.
int? slotDie(String? kind, {int? sides, bool still = false}) {
  if (still) return null;
  if (kind == 'flip') return 2;
  if (kind == 'roll' && sides != null && sides != 6) return sides;
  return null;
}

/// Reads the bundled sheets, each once.
abstract final class ChanceArt {
  static final _cache = <String, Future<ChanceSheet?>>{};

  /// The sheet [name] from [bundle], or null when it is not bundled or
  /// cannot be read.
  static Future<ChanceSheet?> load(AssetBundle bundle, String name) =>
      _cache.putIfAbsent(name, () async {
        try {
          final json = await bundle.loadString(
            '$chanceArtDir$name.json',
            cache: false,
          );
          final sheet = ChanceSheet.fromJson(
            jsonDecode(json) as Map<String, dynamic>,
          );
          await bundle.load(sheet.image);
          return sheet;
        } catch (_) {
          return null;
        }
      });

  /// Forgets what was read, for tests.
  @visibleForTesting
  static void reset() => _cache.clear();
}

/// One face of a sheet, at [chanceArtScale]. [turned] draws it upside down: a reversed card, from a
/// deck drawn with orientation such as tarot. [size], if given, stretches
/// it to that size (the card back, on a card of another deck).
class ChanceSprite extends StatelessWidget {
  const ChanceSprite({
    super.key,
    required this.sheet,
    required this.face,
    this.turned = false,
    this.dimmed = false,
    this.size,
  });
  final ChanceSheet sheet;
  final String face;
  final bool turned;
  final bool dimmed;
  final Size? size;

  @override
  Widget build(BuildContext context) {
    const s = chanceArtScale;
    final cell = sheet.cellOf(face)!;
    Widget out = SizedBox(
      width: sheet.cell.width * s,
      height: sheet.cell.height * s,
      child: Stack(
        clipBehavior: Clip.hardEdge,
        children: [
          Positioned(
            left: -cell.left * s,
            top: -cell.top * s,
            width: sheet.size.width * s,
            height: sheet.size.height * s,
            // Exact: the sheet is one file, never a resolution variant.
            child: Image(
              image: ExactAssetImage(sheet.image),
              fit: BoxFit.fill,
              filterQuality: FilterQuality.medium,
              gaplessPlayback: true,
              excludeFromSemantics: true,
            ),
          ),
        ],
      ),
    );
    if (size != null) {
      out = SizedBox(
        width: size!.width,
        height: size!.height,
        child: FittedBox(fit: BoxFit.fill, child: out),
      );
    }
    if (turned) out = RotatedBox(quarterTurns: 2, child: out);
    if (dimmed) out = Opacity(opacity: 0.45, child: out);
    return out;
  }
}

/// The tile back, in the units of the mahjong sheet's cell (144 × 192;
/// the web chat's tileBackDrawing): the tile's lavender thickness, its ink
/// edge and, inside, lavender with a straight grid of round ink dots 14
/// apart, clear of the cut corner. Drawn at [chanceArtScale].
class TileBack extends StatelessWidget {
  const TileBack({super.key});

  static const cell = Size(144, 192);

  /// The dots' centres, in cell units.
  static List<Offset> get dots => [
    for (var y = 14.0; y <= 172; y += 14)
      for (var x = 12.0; x <= 124; x += 14)
        if (!(x > 112 && y < 28)) Offset(x, y),
  ];

  @override
  Widget build(BuildContext context) => SizedBox(
    width: cell.width * chanceArtScale,
    height: cell.height * chanceArtScale,
    child: const CustomPaint(painter: _TileBackPainter()),
  );
}

class _TileBackPainter extends CustomPainter {
  const _TileBackPainter();

  Path _poly(List<double> xy, double s) {
    final p = Path()..moveTo(xy[0] * s, xy[1] * s);
    for (var i = 2; i < xy.length; i += 2) {
      p.lineTo(xy[i] * s, xy[i + 1] * s);
    }
    return p..close();
  }

  @override
  void paint(Canvas canvas, Size size) {
    final s = size.width / TileBack.cell.width;
    final lav = Paint()..color = CardColours.lavender;
    final ink = Paint()..color = CardColours.ink;
    canvas.drawPath(_poly([8, 8, 130, 8, 144, 22, 144, 192, 8, 192], s), lav);
    canvas.drawPath(_poly([0, 0, 122, 0, 136, 14, 136, 184, 0, 184], s), ink);
    canvas.drawPath(
      _poly([4, 4, 120.34, 4, 132, 15.66, 132, 180, 4, 180], s),
      lav,
    );
    for (final d in TileBack.dots) {
      canvas.drawCircle(d * s, 3 * s, ink);
    }
  }

  @override
  bool shouldRepaint(_TileBackPainter oldDelegate) => false;
}
