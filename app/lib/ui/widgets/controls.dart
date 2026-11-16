import 'dart:async';

import 'package:flutter/material.dart';

import '../theme/churr_icons.dart';
import '../theme/motion.dart';
import '../theme/shapes.dart';
import '../theme/tokens.dart';

/// The guide's controls that Material does not draw: the square switch,
/// presence lights, the sealed-chat shield, the drawn chevron, the busy
/// mark, the date chip, the "new" marker, the thread summary and the
/// moving marker, single-choice rows and the glitch wordmark.

/// A keyboard focus outline: 3px yellow (amber on light), drawn around
/// [child] while it has focus.
class FocusOutline extends StatelessWidget {
  const FocusOutline({
    super.key,
    required this.focused,
    required this.child,
    this.inset = false,
  });
  final bool focused;
  final bool inset;
  final Widget child;

  @override
  Widget build(BuildContext context) => focused
      ? DecoratedBox(
          position: DecorationPosition.foreground,
          decoration: BoxDecoration(
            border: Border.all(
              color: ChurrColors.of(context).focus,
              width: 3,
              strokeAlign: inset
                  ? BorderSide.strokeAlignInside
                  : BorderSide.strokeAlignOutside,
            ),
          ),
          child: child,
        )
      : child;
}

/// The switch: a 40×20 box with a 2px edge and a square thumb. On, the
/// edge and thumb are cyan and the thumb sits right. The thumb jumps.
class ChurrSwitch extends StatefulWidget {
  const ChurrSwitch({
    super.key,
    required this.value,
    required this.onChanged,
    this.label,
  });
  final bool value;
  final ValueChanged<bool>? onChanged;
  final String? label;

  @override
  State<ChurrSwitch> createState() => _ChurrSwitchState();
}

class _ChurrSwitchState extends State<ChurrSwitch> {
  bool focused = false;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final on = widget.value;
    final enabled = widget.onChanged != null;
    final box = Container(
      width: 40,
      height: 20,
      decoration: BoxDecoration(
        color: c.bg,
        border: Border.all(
          color: !enabled ? c.line : (on ? c.cyan : c.edge),
          width: 2,
        ),
      ),
      child: Align(
        alignment: on ? Alignment.centerRight : Alignment.centerLeft,
        child: Container(
          width: 12,
          height: 12,
          margin: const EdgeInsets.all(2),
          color: !enabled ? c.line : (on ? c.cyan : c.faint),
        ),
      ),
    );
    return Semantics(
      toggled: on,
      enabled: enabled,
      label: widget.label,
      child: FocusableActionDetector(
        enabled: enabled,
        onShowFocusHighlight: (v) => setState(() => focused = v),
        mouseCursor: enabled ? SystemMouseCursors.click : MouseCursor.defer,
        actions: {
          ActivateIntent: CallbackAction<ActivateIntent>(
            onInvoke: (_) => widget.onChanged?.call(!on),
          ),
        },
        child: GestureDetector(
          onTap: enabled ? () => widget.onChanged!(!on) : null,
          behavior: HitTestBehavior.opaque,
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 12, horizontal: 4),
            child: FocusOutline(focused: focused, child: box),
          ),
        ),
      ),
    );
  }
}

/// A row with a title, an optional subtitle and the switch at its end; it
/// stands in for Material's `SwitchListTile`, whose switch is a pill.
class ChurrSwitchTile extends StatelessWidget {
  const ChurrSwitchTile({
    super.key,
    required this.value,
    required this.onChanged,
    this.title,
    this.subtitle,
    this.secondary,
    this.contentPadding,
    this.dense,
  });

  final bool value;
  final ValueChanged<bool>? onChanged;
  final Widget? title;
  final Widget? subtitle;
  final Widget? secondary;
  final EdgeInsetsGeometry? contentPadding;
  final bool? dense;

  @override
  Widget build(BuildContext context) => MergeSemantics(
    child: ListTile(
      contentPadding: contentPadding,
      dense: dense,
      leading: secondary,
      title: title,
      subtitle: subtitle,
      enabled: onChanged != null,
      onTap: onChanged == null ? null : () => onChanged!(!value),
      trailing: ExcludeFocus(
        child: ChurrSwitch(value: value, onChanged: onChanged),
      ),
    ),
  );
}

/// One choice of several: a square that is filled when chosen, beside its
/// label. Stands in for Material's round radio button.
class ChoiceTile extends StatelessWidget {
  const ChoiceTile({
    super.key,
    required this.selected,
    required this.onTap,
    required this.title,
    this.subtitle,
    this.contentPadding,
  });
  final bool selected;
  final VoidCallback? onTap;
  final Widget title;
  final Widget? subtitle;
  final EdgeInsetsGeometry? contentPadding;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Semantics(
      inMutuallyExclusiveGroup: true,
      checked: selected,
      child: ListTile(
        contentPadding: contentPadding,
        onTap: onTap,
        leading: Glyph(
          selected ? ChurrIcons.boxDot : ChurrIcons.box,
          size: 20,
          color: selected ? c.cyan : c.edge,
        ),
        title: title,
        subtitle: subtitle,
      ),
    );
  }
}

/// Presence as a shape as well as a colour: a solid cyan square online, a
/// hollow yellow square away, a pink diamond for do not disturb, a grey
/// square offline.
class PresenceLight extends StatelessWidget {
  const PresenceLight(this.presence, {super.key, this.size = 9});

  /// online, away, dnd or offline.
  final String presence;
  final double size;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final Widget light = switch (presence) {
      'online' => Container(width: size, height: size, color: c.cyan),
      'away' => Container(
        width: size,
        height: size,
        decoration: BoxDecoration(
          border: Border.all(color: c.yellowInk, width: 2),
        ),
      ),
      'dnd' => Crooked(
        degrees: 45,
        child: Container(width: size, height: size, color: c.pink),
      ),
      _ => Container(width: size, height: size, color: c.faint),
    };
    return Semantics(label: presenceWords(presence), child: light);
  }
}

String presenceWords(String p) => switch (p) {
  'online' => 'online',
  'away' => 'away',
  'dnd' => 'do not disturb',
  _ => 'offline',
};

/// A sealed chat has no label: its presence light is a shield, solid when
/// online, outline when away, solid with a bar for do not disturb, dotted
/// outline when offline.
class SealedShield extends StatelessWidget {
  const SealedShield(this.presence, {super.key, this.size = 14});
  final String presence;
  final double size;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Semantics(
      label: 'sealed, ${presenceWords(presence)}',
      child: CustomPaint(
        size: Size.square(size),
        painter: _ShieldPainter(presence, c),
      ),
    );
  }
}

class _ShieldPainter extends CustomPainter {
  _ShieldPainter(this.presence, this.c);
  final String presence;
  final ChurrColors c;

  @override
  void paint(Canvas canvas, Size size) {
    final k = size.width / 14;
    canvas.scale(k);
    final path = parseSvgPath(ChurrIcons.sealedShield.stroke);
    final stroke = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.8
      ..strokeJoin = StrokeJoin.miter;
    switch (presence) {
      case 'online':
        canvas.drawPath(path, Paint()..color = c.cyan);
        canvas.drawPath(path, stroke..color = c.cyan);
      case 'away':
        canvas.drawPath(path, stroke..color = c.yellowInk);
      case 'dnd':
        canvas.drawPath(path, Paint()..color = c.pink);
        canvas.drawPath(path, stroke..color = c.pink);
        canvas.drawLine(
          const Offset(4.5, 7),
          const Offset(9.5, 7),
          Paint()
            ..color = c.panel
            ..strokeWidth = 2,
        );
      default:
        // Dotted: dashes along the outline.
        for (final m in path.computeMetrics()) {
          for (var d = 0.0; d < m.length; d += 4) {
            canvas.drawPath(m.extractPath(d, d + 2), stroke..color = c.faint);
          }
        }
    }
  }

  @override
  bool shouldRepaint(_ShieldPainter old) =>
      old.presence != presence || old.c != c;
}

/// A clearly drawn fold chevron with a large tap area: down when open,
/// right when folded. Cyan on hover.
class FoldChevron extends StatefulWidget {
  const FoldChevron({
    super.key,
    required this.open,
    this.onTap,
    required this.label,
    this.width = 26,
    this.height = 32,
  });
  final bool open;
  final VoidCallback? onTap;
  final String label;
  final double width;
  final double height;

  @override
  State<FoldChevron> createState() => _FoldChevronState();
}

class _FoldChevronState extends State<FoldChevron> {
  bool hover = false;
  bool focused = false;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final mark = ChevronMark(
      open: widget.open,
      color: hover || focused ? c.cyan : c.dim,
    );
    if (widget.onTap == null) {
      return SizedBox(
        width: widget.width,
        height: widget.height,
        child: Center(child: mark),
      );
    }
    return Semantics(
      button: true,
      expanded: widget.open,
      label: widget.label,
      excludeSemantics: true,
      child: FocusableActionDetector(
        onShowHoverHighlight: (v) => setState(() => hover = v),
        onShowFocusHighlight: (v) => setState(() => focused = v),
        mouseCursor: SystemMouseCursors.click,
        actions: {
          ActivateIntent: CallbackAction<ActivateIntent>(
            onInvoke: (_) => widget.onTap!(),
          ),
        },
        child: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: widget.onTap,
          child: FocusOutline(
            focused: focused,
            inset: true,
            child: SizedBox(
              width: widget.width,
              height: widget.height,
              child: Center(child: mark),
            ),
          ),
        ),
      ),
    );
  }
}

/// The chevron itself: two 2.5px strokes meeting at a right angle.
class ChevronMark extends StatelessWidget {
  const ChevronMark({
    super.key,
    required this.open,
    required this.color,
    this.size = 8,
  });
  final bool open;
  final Color color;
  final double size;

  @override
  Widget build(BuildContext context) => CustomPaint(
    size: Size.square(size + 4),
    painter: _ChevronPainter(open, color, size),
  );
}

class _ChevronPainter extends CustomPainter {
  _ChevronPainter(this.open, this.color, this.s);
  final bool open;
  final Color color;
  final double s;

  @override
  void paint(Canvas canvas, Size size) {
    final p = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2.5
      ..strokeCap = StrokeCap.square
      ..strokeJoin = StrokeJoin.miter;
    final cx = size.width / 2, cy = size.height / 2, h = s / 2;
    final path = open
        ? (Path()
            ..moveTo(cx - h, cy - h / 2)
            ..lineTo(cx, cy + h / 2)
            ..lineTo(cx + h, cy - h / 2))
        : (Path()
            ..moveTo(cx - h / 2, cy - h)
            ..lineTo(cx + h / 2, cy)
            ..lineTo(cx - h / 2, cy + h));
    canvas.drawPath(path, p);
  }

  @override
  bool shouldRepaint(_ChevronPainter old) =>
      old.open != open || old.color != color;
}

/// Working: three squares lit in turn, in steps. Still when reduced
/// motion is asked for. Stands in for Material's round, eased spinner.
class Busy extends StatefulWidget {
  const Busy({super.key, this.size = 18, this.label = 'Loading'});
  final double size;
  final String label;

  @override
  State<Busy> createState() => _BusyState();
}

class _BusyState extends State<Busy> {
  Timer? timer;
  int step = 0;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (Motion.reduced(context)) {
      timer?.cancel();
      timer = null;
    } else {
      timer ??= Timer.periodic(const Duration(milliseconds: 200), (_) {
        if (mounted) setState(() => step = (step + 1) % 3);
      });
    }
  }

  @override
  void dispose() {
    timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final cell = widget.size / 4;
    return Semantics(
      label: widget.label,
      child: SizedBox(
        width: widget.size,
        height: widget.size,
        child: Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            for (var i = 0; i < 3; i++)
              Container(
                width: cell,
                height: cell,
                color: i == step ? c.cyan : c.edge,
              ),
          ],
        ),
      ),
    );
  }
}

/// The date chip between days: a narrow upper-case label on raised.
class DateChip extends StatelessWidget {
  const DateChip(this.text, {super.key});
  final String text;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      color: c.raised,
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
      child: Text(
        text.toUpperCase(),
        style: ChurrType.label(c.dim, size: 11).copyWith(height: 1),
      ),
    );
  }
}

/// The "new" marker: a pink line and a crooked NEW tag.
class NewMarker extends StatelessWidget {
  const NewMarker({super.key, this.text = 'NEW'});
  final String text;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Row(
      children: [
        Expanded(child: Container(height: 2, color: c.pink)),
        Tag(text, fill: c.pink, degrees: -3),
      ],
    );
  }
}

/// The summary under a message with replies: raised, a cyan bar at its
/// left edge, cyan words.
class ThreadSummaryChip extends StatelessWidget {
  const ThreadSummaryChip({
    super.key,
    required this.text,
    this.leading = const [],
    this.onTap,
    this.unread = false,
  });
  final String text;
  final List<Widget> leading;
  final VoidCallback? onTap;

  /// Unread replies: the bar turns yellow, as the moving marker's.
  final bool unread;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return InkWell(
      onTap: onTap,
      child: Container(
        decoration: BoxDecoration(
          color: c.raised,
          border: Border(
            left: BorderSide(color: unread ? c.yellow : c.cyan, width: 4),
          ),
        ),
        padding: const EdgeInsets.fromLTRB(10, 5, 10, 5),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            ...leading,
            Flexible(
              child: Text(
                text,
                style: TextStyle(
                  color: c.cyan,
                  fontWeight: FontWeight.w600,
                  fontSize: 13,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// The moving marker: new replies to a thread, shown in the conversation
/// where they now are. Raised, a yellow bar at its left edge, the count
/// in yellow-ink.
class MovingMarker extends StatelessWidget {
  const MovingMarker({
    super.key,
    required this.count,
    required this.excerpt,
    this.onTap,
  });
  final String count;
  final String excerpt;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return InkWell(
      onTap: onTap,
      child: Container(
        decoration: BoxDecoration(
          color: c.raised,
          border: Border(left: BorderSide(color: c.yellow, width: 4)),
        ),
        padding: const EdgeInsets.fromLTRB(12, 8, 12, 8),
        child: Text.rich(
          TextSpan(
            children: [
              TextSpan(
                text: count,
                style: TextStyle(
                  color: c.yellowInk,
                  fontWeight: FontWeight.w700,
                ),
              ),
              TextSpan(text: ' $excerpt'),
            ],
          ),
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(color: c.dim, fontSize: 14, height: 1.3),
        ),
      ),
    );
  }
}

/// The split-colour glitch, for the wordmark, the start-up screen and
/// empty states only. Never on text you have to read.
class GlitchText extends StatelessWidget {
  const GlitchText(this.text, {super.key, required this.style});
  final String text;
  final TextStyle style;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final em = style.fontSize ?? 40;
    final top = style.copyWith(
      color: c.ink,
      shadows: [
        Shadow(offset: Offset(-0.028 * em, 0), color: c.cyan),
        Shadow(offset: Offset(0.028 * em, 0.012 * em), color: c.pink),
      ],
    );
    final band = style.copyWith(
      color: c.ink,
      shadows: [
        Shadow(offset: Offset(-0.05 * em, 0), color: c.pink),
        Shadow(offset: Offset(0.045 * em, 0), color: c.cyan),
      ],
    );
    return Semantics(
      label: text,
      excludeSemantics: true,
      child: Stack(
        children: [
          ClipRect(
            clipper: const _Band(0, 0.44),
            child: Text(text, style: top),
          ),
          ClipRect(
            clipper: const _Band(0.62, 1),
            child: Text(text, style: top),
          ),
          ClipRect(
            clipper: const _Band(0.436, 0.624),
            child: Transform.translate(
              offset: Offset(0.09 * em, 0),
              child: Text(text, style: band),
            ),
          ),
        ],
      ),
    );
  }
}

class _Band extends CustomClipper<Rect> {
  const _Band(this.from, this.to);
  final double from, to;

  @override
  Rect getClip(Size size) => Rect.fromLTRB(
    -size.width * 0.1,
    size.height * from,
    size.width * 1.2,
    size.height * to,
  );

  @override
  bool shouldReclip(_Band old) => old.from != from || old.to != to;
}

/// The glitched "churr" wordmark: Archivo 900, width 112%.
class Wordmark extends StatelessWidget {
  const Wordmark({super.key, this.size = 56});
  final double size;

  @override
  Widget build(BuildContext context) => GlitchText(
    'churr',
    style: archivo(
      size: size,
      weight: 900,
      width: 112,
      height: 0.9,
      letterSpacing: -0.045 * size,
    ),
  );
}

/// An empty state: the dot grid behind, a glitched display line, and the
/// plain words under it.
class EmptyState extends StatelessWidget {
  const EmptyState({super.key, required this.title, this.body, this.action});
  final String title;
  final String? body;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return DotGrid(
      child: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              GlitchText(
                title.toUpperCase(),
                style: ChurrType.display(c.ink, size: 34),
              ),
              if (body != null) ...[
                const SizedBox(height: 12),
                Container(
                  color: c.bg,
                  padding: const EdgeInsets.symmetric(
                    horizontal: 6,
                    vertical: 2,
                  ),
                  child: Text(
                    body!,
                    textAlign: TextAlign.center,
                    style: TextStyle(color: c.dim, fontSize: 15),
                  ),
                ),
              ],
              if (action != null) ...[const SizedBox(height: 16), action!],
            ],
          ),
        ),
      ),
    );
  }
}

/// A menu item with a tick when chosen: stands in for Material's
/// `CheckedPopupMenuItem`, which draws Material's own check icon.
PopupMenuItem<T> checkItem<T>({
  Key? key,
  required T value,
  required bool checked,
  required Widget child,
  bool enabled = true,
}) => PopupMenuItem<T>(
  key: key,
  value: value,
  enabled: enabled,
  child: Builder(
    builder: (context) => Semantics(
      checked: checked,
      child: Row(
        children: [
          SizedBox(
            width: 26,
            child: checked
                ? Glyph(
                    ChurrIcons.check,
                    size: 16,
                    color: ChurrColors.of(context).cyan,
                  )
                : null,
          ),
          Flexible(child: child),
        ],
      ),
    ),
  ),
);

/// The guide's picker: a row of quiet blocks, the chosen one marked by a
/// thick cyan line under it (and announced as selected), for choices like
/// theme and palette.
class PickRow<T> extends StatelessWidget {
  const PickRow({
    super.key,
    required this.value,
    required this.options,
    required this.onChanged,
  });
  final T value;
  final List<(T, String)> options;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final (v, label) in options)
          Semantics(
            selected: v == value,
            inMutuallyExclusiveGroup: true,
            child: TextButton(
              key: ValueKey('pick-$v'),
              onPressed: () => onChanged(v),
              style: v == value
                  ? ButtonStyle(
                      side: WidgetStateProperty.resolveWith(
                        (s) => s.contains(WidgetState.focused)
                            ? BorderSide(color: c.focus, width: 3)
                            : BorderSide(color: c.cyan, width: 4),
                      ),
                      textStyle: WidgetStatePropertyAll(
                        archivo(size: 14, weight: 800),
                      ),
                    )
                  : null,
              child: Text(label),
            ),
          ),
      ],
    );
  }
}
