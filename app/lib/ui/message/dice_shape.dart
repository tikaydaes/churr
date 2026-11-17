// Dice other than the six-sided one, and coins, drawn on the fly
// (docs/ART.md): a die of N sides is a regular N-sided polygon, one corner
// up, with the number it shows inside; past [ngonMaxSides] sides it is a
// circle. A two-sided die is a coin, as is the result of /flip: heads for
// 1, tails for 2. The web chat draws the same, with the same constants
// (internal/web/static/shared/message/diceshape.js).

import 'dart:math' as math;

import 'package:flutter/widgets.dart';

import '../theme/tokens.dart';

/// A die of more sides than this is drawn as a circle.
const ngonMaxSides = 30;

/// The drawing's box in logical pixels: the size of a cube face.
const dieBox = 64.0;

/// The circle the polygon's corners sit on, and the edge's width.
const dieRadius = 29.0;
const dieEdge = 2.0;
const _numberMargin = 2.0;

/// Card colours, the same on every theme (tools/art-gen/spec.py).
abstract final class CardColours {
  static const paper = Color(0xFFF5F2FF);
  static const ink = Color(0xFF0D1026);
  static const black = Color(0xFF111111);
  static const cyan = Color(0xFF5DF0E4);
  static const yellow = Color(0xFFFFD95E);
  static const pink = Color(0xFFFF5CCF);
  static const lavender = Color(0xFFB9A0FF);
}

/// The number's type: Archivo 900 at 72% width, where a figure is about
/// 0.5 em wide and 0.72 em tall.
const _figureW = 0.5, _capH = 0.72;

/// Where Archivo's baseline sits in a line box one em tall.
const _baselineInEm = 0.81;

/// A coin's face, in the box's units: the inner ring, the letter's size
/// and baseline, and its marks: a cut-corner crown over the H (its base at
/// [coinCrown]), small diamonds either side of the T ([coinDiamonds] from
/// the middle). The web chat's COIN holds the same.
const coinRing = 24.0, coinLetter = 28.0;
const coinHeadsBaseline = 46.1, coinCrown = 23.9;
const coinTailsBaseline = 42.1, coinDiamonds = 13.0;

/// The corners of each of a coin face's marks.
List<List<Offset>> coinMarks(String face) {
  const x = dieBox / 2;
  if (face == 'heads') {
    const y = coinCrown, w = 7.0, h = 5.0;
    return const [
      [
        Offset(x - w, y),
        Offset(x - w, y - h),
        Offset(x - w / 2, y - h / 2),
        Offset(x, y - h - 1),
        Offset(x + w / 2, y - h / 2),
        Offset(x + w, y - h),
        Offset(x + w, y),
      ],
    ];
  }
  const y = dieBox / 2, d = 2.5;
  return [
    for (final side in const [-1, 1])
      () {
        final c = x + side * coinDiamonds;
        return [
          Offset(c, y - d),
          Offset(c + d, y),
          Offset(c, y + d),
          Offset(c - d, y),
        ];
      }(),
  ];
}

/// A die's colour: paper for every die and the coin, as the six-sided
/// cube is (ruling R87).
Color dieFill(int sides) => CardColours.paper;

/// A die's edge: a circle, or the corners of its polygon; its centre, and
/// the radius of the circle that fits inside it.
typedef DieOutline = ({
  bool circle,
  List<Offset> points,
  Offset centre,
  double inner,
});

/// The edge of a die of [sides], in a [dieBox] square. A polygon is set in
/// the middle of the box by its own height.
DieOutline dieOutline(int sides) {
  const c = dieBox / 2, r = dieRadius;
  if (sides < 3 || sides > ngonMaxSides) {
    return (
      circle: true,
      points: const [],
      centre: const Offset(c, c),
      inner: r,
    );
  }
  final pts = [
    for (var i = 0; i < sides; i++)
      Offset(
        c + r * math.cos(-math.pi / 2 + 2 * math.pi * i / sides),
        c + r * math.sin(-math.pi / 2 + 2 * math.pi * i / sides),
      ),
  ];
  final ys = pts.map((p) => p.dy);
  final dy = c - (ys.reduce(math.min) + ys.reduce(math.max)) / 2;
  return (
    circle: false,
    points: [for (final p in pts) p.translate(0, dy)],
    centre: Offset(c, c + dy),
    inner: r * math.cos(math.pi / sides),
  );
}

/// The largest box for text of [chars] characters, each [w] em wide and
/// 0.72 em tall, inside a circle of radius [rho] about [centre].
Rect fitBox(int chars, double rho, Offset centre, [double w = _figureW]) {
  final a = w * math.max(1, chars) / _capH;
  final h = 2 * rho / math.sqrt(1 + a * a);
  return Rect.fromCenter(center: centre, width: a * h, height: h);
}

/// The face a coin shows: heads for a d2's 1 or a flip's heads, tails for
/// 2 or tails.
String coinFace(String v) => v == '2' || v == 'tails' ? 'tails' : 'heads';

/// A die of [sides] showing [value], or with [sides] 2 a coin (a flip's
/// [value] is 'heads' or 'tails').
class DieFace extends StatelessWidget {
  const DieFace({
    super.key,
    required this.sides,
    required this.value,
    this.dimmed = false,
  });
  final int sides;
  final String value;
  final bool dimmed;

  @override
  Widget build(BuildContext context) {
    final o = dieOutline(sides);
    final coin = sides == 2;
    final face = coinFace(value);
    final String text;
    final double size, baseline;
    if (coin) {
      text = face == 'heads' ? 'H' : 'T';
      size = coinLetter;
      baseline = face == 'heads' ? coinHeadsBaseline : coinTailsBaseline;
    } else {
      text = value;
      final box = fitBox(
        value.runes.length,
        o.inner - dieEdge / 2 - _numberMargin,
        o.centre,
      );
      size = box.height / _capH;
      baseline = box.bottom;
    }
    Widget out = SizedBox.square(
      dimension: dieBox,
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          Positioned.fill(
            child: CustomPaint(
              painter: DiePainter(sides: sides, coinFace: coin ? face : null),
            ),
          ),
          // The letter's line box is one em; its baseline sits about 0.8
          // em down it in Archivo, so the capitals end on the baseline.
          Positioned(
            left: 0,
            right: 0,
            top: baseline - _baselineInEm * size,
            child: Text(
              text,
              key: const Key('die-number'),
              textAlign: TextAlign.center,
              softWrap: false,
              overflow: TextOverflow.visible,
              style: archivo(
                weight: 900,
                width: 72,
                color: CardColours.black,
                height: 1,
              ).copyWith(fontSize: size),
            ),
          ),
        ],
      ),
    );
    if (dimmed) out = Opacity(opacity: 0.45, child: out);
    return out;
  }
}

/// Paints a die's shape, or a coin with its ring and its face's marks: a
/// cut-corner crown over the H of heads, diamonds beside the T of tails.
class DiePainter extends CustomPainter {
  const DiePainter({required this.sides, this.coinFace});
  final int sides;
  final String? coinFace;

  /// The shape's path, in a [dieBox] square.
  static Path shape(int sides) {
    final o = dieOutline(sides);
    if (o.circle) {
      return Path()
        ..addOval(Rect.fromCircle(center: o.centre, radius: dieRadius));
    }
    return Path()..addPolygon(o.points, true);
  }

  @override
  void paint(Canvas canvas, Size size) {
    canvas.scale(size.width / dieBox, size.height / dieBox);
    final path = shape(sides);
    canvas.drawPath(path, Paint()..color = dieFill(sides));
    canvas.drawPath(
      path,
      Paint()
        ..color = CardColours.ink
        ..style = PaintingStyle.stroke
        ..strokeWidth = dieEdge
        ..strokeJoin = StrokeJoin.miter
        ..strokeMiterLimit = 10,
    );
    final face = coinFace;
    if (face == null) return;
    const c = Offset(dieBox / 2, dieBox / 2);
    canvas.drawCircle(
      c,
      coinRing,
      Paint()
        ..color = CardColours.ink
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.5,
    );
    for (final m in coinMarks(face)) {
      canvas.drawPath(
        Path()..addPolygon(m, true),
        Paint()..color = CardColours.ink,
      );
    }
  }

  @override
  bool shouldRepaint(DiePainter old) =>
      old.sides != sides || old.coinFace != coinFace;
}
