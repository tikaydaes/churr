import 'package:flutter/widgets.dart';

/// Churr's own icon set, drawn to the guide's rules: a 24px grid, a 2px
/// line, square ends and sharp joins, corners cut at 45° instead of
/// rounded, circles only where the thing is round (a clock, a face),
/// outline only, with a solid fill meaning "on" (a pinned pin, a starred
/// star). The first fourteen are the guide's own, path for path.
///
/// An icon is plain data: SVG path strings for the line and for the
/// solid parts, drawn by [Glyph].
@immutable
class ChurrGlyph {
  const ChurrGlyph(this.stroke, {this.fill = '', this.box = 24});

  /// Drawn as a 2px line (scaled with the icon).
  final String stroke;

  /// Drawn solid.
  final String fill;

  /// The side of the square the paths are drawn in.
  final double box;
}

/// The icons, by what they mean.
abstract final class ChurrIcons {
  // ---- the guide's fourteen ----
  static const chat = ChurrGlyph('M4 5h13l3 3v9H9l-5 4z');
  static const thread = ChurrGlyph('M4 5h16v10H10l-4 4v-4H4z M8 9h8M8 12h5');
  static const notify = ChurrGlyph('M10 4h4l4 4v6l2 3H4l2-3V8z M10 20h4');
  static const bookmark = ChurrGlyph('M6 4h12v16l-6-4-6 4z');
  static const search = ChurrGlyph('M5 5h8l3 3v5l-3 3H8l-3-3z M15 15l5 5');
  static const pin = ChurrGlyph('M9 4h6v6l3 4H6l3-4z M12 14v6');
  static const add = ChurrGlyph('M12 5v14M5 12h14');
  static const send = ChurrGlyph('M4 12L20 5l-4 15-4-6z M12 14l8-9');
  static const edit = ChurrGlyph('M14 4l-8 8v6h6l8-8z M12 6l6 6');
  static const attach = ChurrGlyph(
    'M8 13l7-7a3 3 0 014 4l-9 9a5 5 0 01-7-7l8-8',
  );
  static const call = ChurrGlyph('M4 7h10v10H4z M14 10l6-3v10l-6-3');
  static const sealed = ChurrGlyph(
    'M6 10V7l3-3h6l3 3v3 M4 10h16v10H4z M12 14v3',
  );
  static const settings = ChurrGlyph(
    'M4 6h16M4 12h16M4 18h16',
    fill: 'M7 4h4v4H7zM14 10h4v4h-4zM9 16h4v4H9z',
  );
  static const star = ChurrGlyph(
    'M12 3l3 6 6 1-4.5 4.5 1 6.5-5.5-3-5.5 3 1-6.5L3 10l6-1z',
  );

  // ---- "on" versions: solid ----
  static const starOn = ChurrGlyph(
    '',
    fill: 'M12 2l3.4 6.3 6.6 1-4.8 4.8 1.1 6.9L12 17.8 5.7 21l1.1-6.9L2 9.3l6.6-1z',
  );
  static const pinOn = ChurrGlyph(
    'M12 14v6',
    fill: 'M8 3h8v6.7l3 4.3H5l3-4.3z',
  );
  static const bookmarkOn = ChurrGlyph('', fill: 'M5 3h14v19l-7-4.7L5 22z');
  static const notifyActive = ChurrGlyph(
    'M10 4h4l4 4v6l2 3H4l2-3V8z M10 20h4 M2 9V5M22 9V5',
  );

  // ---- channels and conversations ----
  static const hash = ChurrGlyph('M10 4L8 20M16 4l-2 16M5 9h15M4 15h15');

  /// A private channel: a small padlock with a solid body.
  static const private = ChurrGlyph(
    'M8 11V7l2-2h4l2 2v4',
    fill: 'M4 10h16v11H4z',
  );

  /// A locked channel: the padlock in outline with its keyhole.
  static const locked = ChurrGlyph(
    'M8 10V7l2-2h4l2 2v3 M5 10h14v10H5z M12 13v4',
  );
  static const shield = ChurrGlyph('M12 3l8 3v7l-3 4-5 4-5-4-3-4V6z');

  /// The fair shield's states (lib/ui/types/fair_mark.dart), drawn only
  /// from this device's own check of a draw: a shield with a tick, a shield
  /// with a question mark, and a warning triangle.
  static const shieldCheck = ChurrGlyph(
    'M12 3l8 3v7l-3 4-5 4-5-4-3-4V6z M8.5 12l2.5 2.5 4.5-5',
  );
  static const shieldUnknown = ChurrGlyph(
    'M12 3l8 3v7l-3 4-5 4-5-4-3-4V6z M10 9.5l1-1h2l1 1v1.5l-2 1.5v1 M12 16v1',
  );
  static const warn = ChurrGlyph('M12 3l10 18H2z M12 10v5 M12 17v2');
  static const newChat = ChurrGlyph('M4 5h13l3 3v9H9l-5 4z M12 8v6M9 11h6');
  static const chatUnread = ChurrGlyph(
    'M4 5h9 M20 12v5H9l-5 4V5',
    fill: 'M16 3h5v5h-5z',
  );
  static const people = ChurrGlyph(
    'M5 5h6v6H5z M2 20v-4l2-2h8l2 2v4 M15 6h4v5h-4 M18 14h1l3 3v3',
  );
  static const peopleAdd = ChurrGlyph(
    'M5 5h6v6H5z M2 20v-4l2-2h8l2 2v4 M19 6v8M15 10h8',
  );
  static const person = ChurrGlyph('M9 3h6v7H9z M4 21v-5l3-3h10l3 3v5');
  static const personAdd = ChurrGlyph(
    'M6 3h6v7H6z M2 21v-5l3-3h8l3 3v5 M19 6v8M15 10h8',
  );
  static const bot = ChurrGlyph(
    'M5 8h14v12H5z M12 4v4 M9 12v2M15 12v2 M9 17h6 M2 12v4M22 12v4',
  );
  static const tree = ChurrGlyph('M4 4h6v6H4z M14 14h6v6h-6z M7 10v7h7');
  static const subArrow = ChurrGlyph('M6 4v9h12 M14 9l4 4-4 4');
  static const moveTo = ChurrGlyph('M3 5h7l2 2h9v13H3z M8 13h8M13 10l3 3-3 3');
  static const archive = ChurrGlyph('M3 4h18v5H3z M5 9v11h14V9 M10 13h4');
  static const globe = ChurrGlyph(
    'M12 3a9 9 0 110 18a9 9 0 110-18z M3 12h18 M12 3L9 7v10l3 4 M12 3l3 4v10l-3 4',
  );
  static const compass = ChurrGlyph(
    'M12 3a9 9 0 110 18a9 9 0 110-18z M16 8l-2 6-6 2 2-6z',
  );
  static const flag = ChurrGlyph('M5 21V4 M5 4h13l-3 4 3 4H5');

  // ---- arrows and marks ----
  static const close = ChurrGlyph('M6 6l12 12M18 6L6 18');
  static const check = ChurrGlyph('M4 12l5 5L20 6');
  static const checkAll = ChurrGlyph('M2 12l5 5 9-10 M12 16l1 1 9-10');
  static const chevronDown = ChurrGlyph('M6 9l6 6 6-6');
  static const chevronUp = ChurrGlyph('M6 15l6-6 6 6');
  static const chevronRight = ChurrGlyph('M9 6l6 6-6 6');
  static const chevronLeft = ChurrGlyph('M15 6l-6 6 6 6');
  /// A handle to drag a row by: three short rules.
  static const grip = ChurrGlyph('M6 8h12 M6 12h12 M6 16h12');
  static const arrowUp = ChurrGlyph('M12 20V5 M6 11l6-6 6 6');
  static const arrowDown = ChurrGlyph('M12 4v15 M6 13l6 6 6-6');
  static const arrowRight = ChurrGlyph('M4 12h15 M13 6l6 6-6 6');
  static const arrowLeft = ChurrGlyph('M20 12H5 M11 6l-6 6 6 6');
  static const external = ChurrGlyph('M13 4h7v7 M20 4l-9 9 M17 14v6H4V7h6');
  static const reply = ChurrGlyph('M9 6l-5 5 5 5 M4 11h11l5 5v3');
  static const download = ChurrGlyph('M12 4v11 M7 10l5 5 5-5 M4 20h16');
  static const moreH = ChurrGlyph(
    '',
    fill: 'M4 10h4v4H4zM10 10h4v4h-4zM16 10h4v4h-4z',
  );
  static const moreV = ChurrGlyph(
    '',
    fill: 'M10 4h4v4h-4zM10 10h4v4h-4zM10 16h4v4h-4z',
  );
  static const remove = ChurrGlyph('M4 4h16v16H4z M8 12h8');

  // ---- state ----
  static const clock = ChurrGlyph(
    'M12 3a9 9 0 110 18a9 9 0 110-18z M12 7v5l3 3',
  );
  static const history = ChurrGlyph('M12 4a8 8 0 11-8 8 M4 4v4h4 M12 8v4l3 3');
  static const error = ChurrGlyph(
    'M8 3h8l5 5v8l-5 5H8l-5-5V8z M12 7v6',
    fill: 'M11 15h2v2h-2z',
  );
  static const info = ChurrGlyph(
    'M4 4h13l3 3v13H4z M12 10v7',
    fill: 'M11 6h2v2h-2z',
  );
  static const eye = ChurrGlyph(
    'M2 12l4-5h12l4 5-4 5H6z',
    fill: 'M10 10h4v4h-4z',
  );
  static const eyeOff = ChurrGlyph(
    'M2 12l4-5h12l4 5-4 5H6z M4 3l16 18',
    fill: 'M10 10h4v4h-4z',
  );
  static const notifyOff = ChurrGlyph(
    'M10 4h4l4 4v6l2 3H4l2-3V8z M10 20h4 M3 3l18 18',
  );
  static const moon = ChurrGlyph('M15 3H9L4 8v8l5 5h6l5-5v-2h-6l-3-3V5z');
  static const dnd = ChurrGlyph('M12 2l10 10-10 10L2 12z M8 12h8');
  static const dndOff = ChurrGlyph('M12 2l10 10-10 10L2 12z M4 4l16 16');
  static const unplugged = ChurrGlyph(
    'M2 12h5 M17 12h5 M7 7h4v10H7z M13 7h4v10h-4z',
  );
  static const plane = ChurrGlyph(
    'M3 20h18 M2 14l9-2 5-7h3l-2 7 4 1-1 2-17 2z',
  );
  static const bolt = ChurrGlyph('M13 3L5 13h6l-1 8 8-10h-6z');
  static const calendar = ChurrGlyph(
    'M4 5h13l3 3v12H4z M4 10h16 M8 3v4M16 3v4',
  );
  static const delete = ChurrGlyph(
    'M4 6h16 M9 6V3h6v3 M6 6l1 15h10l1-15 M10 10v7M14 10v7',
  );
  static const logout = ChurrGlyph('M10 4H4v16h6 M9 12h11 M16 8l4 4-4 4');
  static const devices = ChurrGlyph('M3 5h12v9H3z M1 18h16 M18 9h5v11h-5z');
  static const phone = ChurrGlyph('M7 3h10v18H7z M11 18h2');
  static const qr = ChurrGlyph(
    'M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4z',
    fill: 'M14 14h3v3h-3zM17 17h3v3h-3zM14 19h2v1h-2zM19 14h1v2h-1z',
  );
  static const scan = ChurrGlyph(
    'M4 8V4h4M16 4h4v4M20 16v4h-4M8 20H4v-4 M4 12h16',
  );

  // ---- messages and files ----
  static const copy = ChurrGlyph('M8 8h12v12H8z M16 8V4H4v12h4');
  static const link = ChurrGlyph(
    'M10 14l4-4 M9 8l2-2h3l4 4v3l-2 2 M15 16l-2 2h-3l-4-4v-3l2-2',
  );
  static const emoji = ChurrGlyph(
    'M12 3a9 9 0 110 18a9 9 0 110-18z M9 9v2M15 9v2 M8 15l2 2h4l2-2',
  );
  static const addReaction = ChurrGlyph(
    'M20 12a8 8 0 11-8-8 M9 10v1M15 10v1 M8 15l2 2h4l2-2 M19 2v6M16 5h6',
  );
  static const file = ChurrGlyph('M5 3h9l5 5v13H5z M14 3v5h5');
  static const fileText = ChurrGlyph(
    'M5 3h9l5 5v13H5z M14 3v5h5 M8 13h8M8 17h8',
  );
  static const image = ChurrGlyph(
    'M4 4h16v16H4z M4 16l5-5 4 4 2-2 5 5',
    fill: 'M14 7h3v3h-3z',
  );
  static const imageBroken = ChurrGlyph(
    'M4 4h16v7 M20 15v5H4V4 M4 16l5-5 3 3 M14 11l6 4 M3 3l18 18',
  );
  static const movie = ChurrGlyph(
    'M4 5h16v14H4z M4 9h16M4 15h16 M8 5v4M16 5v4M8 15v4M16 15v4',
  );
  static const audio = ChurrGlyph(
    'M9 18V5h10v11 M9 9h10',
    fill: 'M5 15h4v4H5zM15 13h4v4h-4z',
  );
  static const play = ChurrGlyph('M7 4l13 8-13 8z');
  static const pause = ChurrGlyph('M6 5h4v14H6zM14 5h4v14h-4z');
  static const rows = ChurrGlyph('M4 5h16v6H4z M4 13h16v6H4z');
  static const button = ChurrGlyph('M3 7h15l3 3v7H3z M8 12h8');
  static const box = ChurrGlyph('M4 4h16v16H4z');
  static const boxChecked = ChurrGlyph('M4 4h16v16H4z M8 12l3 3 5-6');
  static const boxDot = ChurrGlyph('M4 4h16v16H4z', fill: 'M9 9h6v6H9z');

  // ---- the formatting bar ----
  static const bold = ChurrGlyph('M7 5h7l3 3v1l-2 2 3 3v2l-3 3H7z M7 11h8');
  static const italic = ChurrGlyph('M10 5h8M6 19h8M14 5l-4 14');
  static const strike = ChurrGlyph('M4 12h16 M17 6H9L7 8v1l2 2 M7 18h8l2-2v-1');
  static const code = ChurrGlyph('M8 7l-5 5 5 5M16 7l5 5-5 5');
  static const codeBlock = ChurrGlyph(
    'M8 4H5v6l-2 2 2 2v6h3 M16 4h3v6l2 2-2 2v6h-3',
  );
  static const bullets = ChurrGlyph(
    'M9 6h12M9 12h12M9 18h12',
    fill: 'M3 5h3v3H3zM3 11h3v3H3zM3 17h3v3H3z',
  );
  static const numbers = ChurrGlyph(
    'M10 6h11M10 12h11M10 18h11 M4 4h2v5 M3 15h4l-4 5h4',
  );
  static const checklist = ChurrGlyph(
    'M3 6l2 2 3-3M3 13l2 2 3-3 M11 7h10M11 14h10M11 20h10',
  );
  static const quote = ChurrGlyph(
    '',
    fill: 'M4 6h6v6l-3 6H5l2-6H4zM14 6h6v6l-3 6h-2l2-6h-3z',
  );
  static const textFormat = ChurrGlyph('M5 20l6-16h2l6 16 M8 14h8');

  /// The sealed chat's shield, in the guide's 14px drawing, used in place
  /// of the presence square.
  static const sealedShield = ChurrGlyph(
    'M7 1.2l5 1.8v4.2L10.5 10.5 7 12.8 3.5 10.5 2 7.2V3z',
    box: 14,
  );
}

/// Draws a [ChurrGlyph]. Takes its size and colour from the surrounding
/// [IconTheme] like Flutter's own `Icon`, so it can stand in for one in
/// buttons and list tiles.
class Glyph extends StatelessWidget {
  const Glyph(
    this.icon, {
    super.key,
    this.size,
    this.color,
    this.semanticLabel,
    this.strokeWidth,
  });
  final ChurrGlyph icon;
  final double? size;
  final Color? color;
  final String? semanticLabel;

  /// The line in the icon's own units; 2 by default.
  final double? strokeWidth;

  @override
  Widget build(BuildContext context) {
    final theme = IconTheme.of(context);
    final s = size ?? theme.size ?? 24;
    var c = color ?? theme.color ?? const Color(0xFF000000);
    if (theme.opacity != null && theme.opacity! < 1) c = c.withValues(alpha: c.a * theme.opacity!);
    final paint = CustomPaint(
      size: Size.square(s),
      painter: _GlyphPainter(icon, c, strokeWidth ?? 2),
    );
    return Semantics(
      label: semanticLabel,
      excludeSemantics: true,
      child: ExcludeSemantics(
        excluding: semanticLabel == null,
        child: SizedBox.square(dimension: s, child: paint),
      ),
    );
  }
}

class _GlyphPainter extends CustomPainter {
  _GlyphPainter(this.icon, this.color, this.stroke);
  final ChurrGlyph icon;
  final Color color;
  final double stroke;

  @override
  void paint(Canvas canvas, Size size) {
    final k = size.width / icon.box;
    canvas.save();
    canvas.scale(k);
    if (icon.stroke.isNotEmpty) {
      canvas.drawPath(
        parseSvgPath(icon.stroke),
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = stroke * (icon.box / 24)
          ..strokeCap = StrokeCap.square
          ..strokeJoin = StrokeJoin.miter
          ..color = color,
      );
    }
    if (icon.fill.isNotEmpty) {
      canvas.drawPath(parseSvgPath(icon.fill), Paint()..color = color);
    }
    canvas.restore();
  }

  @override
  bool shouldRepaint(_GlyphPainter old) =>
      old.icon != icon || old.color != color || old.stroke != stroke;
}

final _cache = <String, Path>{};

/// An SVG path's `d` as a Flutter [Path]: M L H V C S Q Z and A, in both
/// absolute and relative forms, with the compact flag syntax (`a3 3 0 014 4`).
Path parseSvgPath(String d) => _cache.putIfAbsent(d, () => _SvgPath(d).parse());

class _SvgPath {
  _SvgPath(this.d);
  final String d;
  int i = 0;

  bool get _more {
    while (i < d.length && (d[i] == ' ' || d[i] == ',' || d[i] == '\n')) {
      i++;
    }
    return i < d.length;
  }

  bool get _atNumber => _more && RegExp(r'[-+.0-9]').hasMatch(d[i]);

  double _num() {
    _more;
    final start = i;
    if (d[i] == '-' || d[i] == '+') i++;
    var dot = false;
    while (i < d.length) {
      final ch = d[i];
      if (ch == '.') {
        if (dot) break;
        dot = true;
      } else if (!RegExp(r'[0-9]').hasMatch(ch)) {
        break;
      }
      i++;
    }
    return double.parse(d.substring(start, i));
  }

  bool _flag() {
    _more;
    return d[i++] == '1';
  }

  Path parse() {
    final p = Path();
    var x = 0.0, y = 0.0, sx = 0.0, sy = 0.0;
    double? cx, cy; // the last control point, for S
    String cmd = 'M';
    while (_more) {
      if (RegExp(r'[A-Za-z]').hasMatch(d[i])) cmd = d[i++];
      final rel = cmd == cmd.toLowerCase();
      final ox = rel ? x : 0.0, oy = rel ? y : 0.0;
      switch (cmd.toUpperCase()) {
        case 'M':
          x = ox + _num();
          y = oy + _num();
          p.moveTo(x, y);
          sx = x;
          sy = y;
          cmd = rel ? 'l' : 'L';
          cx = null;
        case 'L':
          x = ox + _num();
          y = oy + _num();
          p.lineTo(x, y);
          cx = null;
        case 'H':
          x = ox + _num();
          p.lineTo(x, y);
          cx = null;
        case 'V':
          y = oy + _num();
          p.lineTo(x, y);
          cx = null;
        case 'C':
          final x1 = ox + _num(),
              y1 = oy + _num(),
              x2 = ox + _num(),
              y2 = oy + _num();
          x = ox + _num();
          y = oy + _num();
          p.cubicTo(x1, y1, x2, y2, x, y);
          cx = x2;
          cy = y2;
        case 'S':
          final x1 = cx == null ? x : 2 * x - cx,
              y1 = cy == null ? y : 2 * y - cy;
          final x2 = ox + _num(), y2 = oy + _num();
          x = ox + _num();
          y = oy + _num();
          p.cubicTo(x1, y1, x2, y2, x, y);
          cx = x2;
          cy = y2;
        case 'Q':
          final x1 = ox + _num(), y1 = oy + _num();
          x = ox + _num();
          y = oy + _num();
          p.quadraticBezierTo(x1, y1, x, y);
          cx = null;
        case 'A':
          final rx = _num(), ry = _num(), rot = _num();
          final large = _flag(), sweep = _flag();
          x = ox + _num();
          y = oy + _num();
          p.arcToPoint(
            Offset(x, y),
            radius: Radius.elliptical(rx, ry),
            rotation: rot,
            largeArc: large,
            clockwise: sweep,
          );
          cx = null;
        case 'Z':
          p.close();
          x = sx;
          y = sy;
          cx = null;
          // A Z takes no numbers; a number after it starts an implied line.
          if (_atNumber) cmd = rel ? 'l' : 'L';
          continue;
        default:
          throw FormatException('unsupported path command $cmd', d, i);
      }
    }
    return p;
  }
}
