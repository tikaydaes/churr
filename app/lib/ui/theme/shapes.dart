import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'tokens.dart';

/// The guide's shapes ("shape and texture"): the notch, the hard offset
/// block, the dashed outline for provisional things, crooked tags and the
/// yellow price-tag count. No rounded corner and no blur anywhere.

/// The notch: a rectangle with its top-right corner cut at 45°. One per
/// object, on things that matter (avatars, primary buttons, bot cards),
/// never on plain containers.
class CutCornerBorder extends OutlinedBorder {
  const CutCornerBorder({this.cut = 7, super.side});

  /// The length of the cut along each edge, 6 to 8 logical pixels.
  final double cut;

  Path _path(Rect r) {
    final k = math.min(cut, math.min(r.width, r.height) / 2);
    return Path()
      ..moveTo(r.left, r.top)
      ..lineTo(r.right - k, r.top)
      ..lineTo(r.right, r.top + k)
      ..lineTo(r.right, r.bottom)
      ..lineTo(r.left, r.bottom)
      ..close();
  }

  @override
  EdgeInsetsGeometry get dimensions => EdgeInsets.all(side.strokeInset);

  @override
  Path getInnerPath(Rect rect, {TextDirection? textDirection}) =>
      _path(rect.deflate(side.strokeInset));

  @override
  Path getOuterPath(Rect rect, {TextDirection? textDirection}) => _path(rect);

  @override
  void paint(Canvas canvas, Rect rect, {TextDirection? textDirection}) {
    if (side.style == BorderStyle.none || side.width == 0) return;
    canvas.drawPath(
      _path(rect.deflate(side.width / 2)),
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = side.width
        ..strokeJoin = StrokeJoin.miter
        ..color = side.color,
    );
  }

  @override
  ShapeBorder scale(double t) =>
      CutCornerBorder(cut: cut * t, side: side.scale(t));

  @override
  CutCornerBorder copyWith({BorderSide? side, double? cut}) =>
      CutCornerBorder(cut: cut ?? this.cut, side: side ?? this.side);

  @override
  bool operator ==(Object other) =>
      other is CutCornerBorder && other.cut == cut && other.side == side;

  @override
  int get hashCode => Object.hash(cut, side);
}

/// Clips a child to the notch (images in avatars).
class CutCornerClipper extends CustomClipper<Path> {
  const CutCornerClipper(this.cut);
  final double cut;

  @override
  Path getClip(Size size) => const CutCornerBorder()
      .copyWith(cut: cut)
      .getOuterPath(Offset.zero & size);

  @override
  bool shouldReclip(CutCornerClipper old) => old.cut != cut;
}

/// The hard shadow for anything that floats (menus, dialogs, popovers):
/// a square box with a 2px ink edge and a solid block offset 4px down and
/// right. Drawn by the border itself so Material's own surfaces can wear
/// it through a theme.
class HardShadowBorder extends OutlinedBorder {
  const HardShadowBorder({super.side, required this.block, this.offset = 4});
  final Color block;
  final double offset;

  @override
  EdgeInsetsGeometry get dimensions => EdgeInsets.all(side.strokeInset);

  @override
  Path getInnerPath(Rect rect, {TextDirection? textDirection}) =>
      Path()..addRect(rect.deflate(side.strokeInset));

  @override
  Path getOuterPath(Rect rect, {TextDirection? textDirection}) =>
      Path()..addRect(rect);

  @override
  void paint(Canvas canvas, Rect rect, {TextDirection? textDirection}) {
    // The block shows only where it sticks out past the box.
    final block = Path.combine(
      PathOperation.difference,
      Path()..addRect(rect.shift(Offset(offset, offset))),
      Path()..addRect(rect),
    );
    canvas.drawPath(block, Paint()..color = this.block);
    if (side.width > 0) {
      canvas.drawRect(
        rect.deflate(side.width / 2),
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = side.width
          ..color = side.color,
      );
    }
  }

  @override
  ShapeBorder scale(double t) =>
      HardShadowBorder(side: side.scale(t), block: block, offset: offset * t);

  @override
  HardShadowBorder copyWith({BorderSide? side}) =>
      HardShadowBorder(side: side ?? this.side, block: block, offset: offset);

  @override
  bool operator ==(Object other) =>
      other is HardShadowBorder &&
      other.side == side &&
      other.block == block &&
      other.offset == offset;

  @override
  int get hashCode => Object.hash(side, block, offset);
}

/// The hard shadow for a plain box: a solid block, no blur.
List<BoxShadow> hardShadow(Color block, {double offset = 4}) => [
  BoxShadow(color: block, offset: Offset(offset, offset)),
];

/// The decoration of a floating box (menus, popovers, suggestion lists).
BoxDecoration floatingBox(ChurrColors c) => BoxDecoration(
  color: c.panel,
  border: Border.all(color: c.ink, width: 2),
  boxShadow: hardShadow(c.lavender),
);

/// A dashed outline, only for things that are provisional or waiting:
/// unsent, pending, drafts, drop targets. Nothing settled is dashed.
class DashedBox extends StatelessWidget {
  const DashedBox({
    super.key,
    required this.child,
    this.color,
    this.width = 2,
    this.padding = EdgeInsets.zero,
  });
  final Widget child;
  final Color? color;
  final double width;
  final EdgeInsetsGeometry padding;

  @override
  Widget build(BuildContext context) => CustomPaint(
    foregroundPainter: _DashPainter(
      color ?? ChurrColors.of(context).pink,
      width,
    ),
    child: Padding(padding: padding, child: child),
  );
}

class _DashPainter extends CustomPainter {
  _DashPainter(this.color, this.width);
  final Color color;
  final double width;

  @override
  void paint(Canvas canvas, Size size) {
    final p = Paint()
      ..color = color
      ..strokeWidth = width
      ..strokeCap = StrokeCap.butt;
    const dash = 6.0, gap = 4.0;
    final h = width / 2;
    void line(Offset a, Offset b) {
      final len = (b - a).distance;
      final dir = (b - a) / len;
      for (var d = 0.0; d < len; d += dash + gap) {
        canvas.drawLine(a + dir * d, a + dir * math.min(d + dash, len), p);
      }
    }

    line(Offset(0, h), Offset(size.width, h));
    line(Offset(size.width - h, 0), Offset(size.width - h, size.height));
    line(Offset(size.width, size.height - h), Offset(0, size.height - h));
    line(Offset(h, size.height), Offset(h, 0));
  }

  @override
  bool shouldRepaint(_DashPainter old) =>
      old.color != color || old.width != width;
}

/// Stickers sit crooked: badges, tags and the "new" marker are rotated a
/// couple of degrees. Reading text never is.
class Crooked extends StatelessWidget {
  const Crooked({super.key, required this.degrees, required this.child});
  final double degrees;
  final Widget child;

  @override
  Widget build(BuildContext context) =>
      Transform.rotate(angle: degrees * math.pi / 180, child: child);
}

/// A small tag: Martian Mono, upper case, on one of the four fills,
/// rotated 1 to 4°.
class Tag extends StatelessWidget {
  const Tag(this.text, {super.key, this.fill, this.degrees = -2});
  final String text;

  /// One of the four role fills; lavender when null.
  final Color? fill;
  final double degrees;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final f = fill ?? c.lavender;
    return Crooked(
      degrees: degrees,
      child: Container(
        color: f,
        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
        child: Text(
          text.toUpperCase(),
          style: monoStyle(
            context,
            size: 9,
            color: c.on(f),
            weight: FontWeight.w700,
          ).copyWith(letterSpacing: 0.6, height: 1),
        ),
      ),
    );
  }
}

/// The unread count: a tight yellow price tag with an ink edge, tilted.
class PriceTag extends StatelessWidget {
  const PriceTag(this.count, {super.key, this.label, this.size = 12.5});
  final int count;
  final String? label;
  final double size;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Semantics(
      label: label ?? '$count',
      excludeSemantics: true,
      child: Crooked(
        degrees: -4,
        child: Container(
          constraints: BoxConstraints(minWidth: size * 1.6),
          padding: const EdgeInsets.fromLTRB(5, 1, 5, 2),
          decoration: BoxDecoration(
            color: c.yellow,
            border: Border.all(color: c.ink, width: 2),
          ),
          child: Text(
            count > 99 ? '99+' : '$count',
            textAlign: TextAlign.center,
            style: archivo(
              size: size,
              weight: 900,
              width: 75,
              color: c.onYellow,
              height: 1,
            ),
          ),
        ),
      ),
    );
  }
}

/// The dot grid: behind empty space only (empty states, sign-in), never
/// behind text you read.
class DotGrid extends StatelessWidget {
  const DotGrid({super.key, this.child});
  final Widget? child;

  @override
  Widget build(BuildContext context) => CustomPaint(
    painter: _DotPainter(ChurrColors.of(context).grid),
    child: child,
  );
}

class _DotPainter extends CustomPainter {
  _DotPainter(this.color);
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final p = Paint()..color = color;
    for (var y = 0.0; y < size.height; y += 22) {
      for (var x = 0.0; x < size.width; x += 22) {
        canvas.drawRect(Rect.fromLTWH(x, y, 2, 2), p);
      }
    }
  }

  @override
  bool shouldRepaint(_DotPainter old) => old.color != color;
}
