// Inline dice in a message (ruling R73): `[[2d6+3]]` with the result the
// server drew when the message was posted, drawn in the sentence. The
// reels spin once when the message arrived live, as a /roll result does,
// the total and dropped dice shown once they stop; a press does nothing.
// The dice are drawn as /roll's: a d6 from the cube sheet, any other die or
// a coin on the fly ([DieFace]), the height of the line; a d6 keeps its
// number when the sheet cannot be read.
// Dice an edit wrote or changed show that nothing was rolled.

import 'dart:async';

import 'package:flutter/material.dart';

import '../../core/models/message.dart';
import '../../l10n/app_localizations.dart';
import '../theme/tokens.dart';
import '../../core/commands/chance_live.dart';
import 'chance_art.dart';
import 'chance_view.dart';
import 'dice_shape.dart';

/// How tall an inline die is, in the text's size: the pips and a
/// two-figure number stay legible, and it stays inside the line.
const dieInLine = 1.35;

/// A dice entity. [live] says how far into their one live spin the reels
/// are, or null when they do not spin ([ChanceLive.spinAt]).
class DiceChip extends StatefulWidget {
  const DiceChip({
    super.key,
    required this.entity,
    required this.typed,
    required this.style,
    this.live,
  });
  final Entity entity;
  final String typed;
  final TextStyle style;
  final Duration? Function()? live;

  @override
  State<DiceChip> createState() => DiceChipState();
}

class DiceChipState extends State<DiceChip> {
  List<ChanceSlot> _slots = const [];
  List<String>? _faces;
  List<String>? _keys;
  Timer? _timer;

  /// The six-sided die's sheet, once read.
  ChanceSheet? _cube;
  int _ticks = 0;

  Map<String, Object?> get _roll => {
    'kind': 'roll',
    'dice': widget.entity.raw['value'],
    'rows': [
      {
        'dice': widget.entity.raw['value'],
        'terms': widget.entity.raw['terms'],
        'total': widget.entity.raw['total'],
      },
    ],
  };

  bool get _rolled =>
      widget.entity.raw['removed'] != true && widget.entity.raw['total'] is num;

  /// Whether the reels are turning.
  bool get spinning => _timer != null;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final t = Localizations.of<L10n>(context, L10n);
    if (t == null || !_rolled) return;
    _slots = chanceSlots(t, _roll);
    if (_cube == null && _slots.any((s) => s.sides == 6)) {
      ChanceArt.load(DefaultAssetBundle.of(context), 'dice-cube').then((sheet) {
        if (sheet != null && mounted) setState(() => _cube = sheet);
      });
    }
    if (_timer != null) return;
    final at = widget.live?.call();
    // The first frame is the spin's, so the total is never seen first.
    if (at != null) _spin(at);
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  /// Plays the reels once, stopping left to right; [from] carries on a
  /// spin that began that long ago.
  void _spin(Duration from) {
    _timer?.cancel();
    _timer = null;
    if (MediaQuery.maybeDisableAnimationsOf(context) ?? false) return;
    final total = ChanceLive.spin.inMilliseconds;
    _ticks = from.inMilliseconds ~/ 60;
    void tick({bool first = false}) {
      final ms = _ticks++ * 60;
      var moving = false;
      final next = <String>[];
      final keys = <String>[];
      for (var i = 0; i < _slots.length; i++) {
        final s = _slots[i];
        final stop =
            total *
            (0.45 + 0.55 * (_slots.length == 1 ? 1 : i / (_slots.length - 1)));
        if (!s.still && ms < stop) {
          moving = true;
          final at = (ms ~/ 70) % (s.reel.length - 1);
          next.add(s.reel[at]);
          keys.add(s.keyAt(at));
        } else {
          next.add(s.text);
          keys.add(s.key);
        }
      }
      if (!moving) {
        _timer?.cancel();
        _timer = null;
      }
      if (first) {
        _faces = moving ? next : null;
        _keys = moving ? keys : null;
      } else {
        setState(() {
          _faces = moving ? next : null;
          _keys = moving ? keys : null;
        });
      }
    }

    _timer = Timer.periodic(const Duration(milliseconds: 60), (_) {
      if (mounted) tick();
    });
    tick(first: true);
  }

  /// Slot [i] drawn as a die the height of the line, or null for a plain
  /// number, and for a d6 while its sheet is not here.
  Widget? _die(int i, ChanceSlot s, ChurrColors c) {
    if (s.still || s.sides == null) return null;
    final key = _keys != null && i < _keys!.length ? _keys![i] : s.key;
    final Widget art;
    if (s.sides == 6) {
      final face = 'd6-$key';
      if (_cube == null || _cube!.cellOf(face) == null) return null;
      art = ChanceSprite(sheet: _cube!, face: face);
    } else {
      art = DieFace(sides: s.sides!, value: key);
    }
    final dim = (widget.style.fontSize ?? 14) * dieInLine;
    final dropped = s.dropped && !spinning;
    Widget out = SizedBox.square(
      dimension: dim,
      child: FittedBox(child: art),
    );
    if (dropped) {
      // Faded and struck through, never colour alone.
      out = Stack(
        children: [
          Opacity(opacity: 0.45, child: out),
          Positioned.fill(child: CustomPaint(painter: const DieStrike())),
        ],
      );
    }
    return KeyedSubtree(key: Key('inline-die-$i'), child: out);
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = Localizations.of<L10n>(context, L10n);
    final mono = widget.style.copyWith(fontFamily: 'monospace');
    if (!_rolled || t == null) {
      return Semantics(
        container: true,
        label: t?.diceRemovedLabel(widget.typed) ?? widget.typed,
        excludeSemantics: true,
        child: Text.rich(
          TextSpan(
            children: [
              TextSpan(text: widget.typed, style: mono),
              TextSpan(
                text: ' ${t?.diceRemoved ?? ''}',
                style: widget.style.copyWith(color: c.dim),
              ),
            ],
          ),
        ),
      );
    }
    final faces = _faces ?? [for (final s in _slots) s.text];
    final said = [
      for (final (i, s) in _slots.indexed)
        '${s.sign == '−' ? 'minus ' : ''}${faces[i]}${s.dropped ? ' (dropped)' : ''}',
    ].join(', ');
    return Semantics(
      container: true,
      label: t.diceLabel(widget.typed, said, '${widget.entity.raw['total']}'),
      excludeSemantics: true,
      child: KeyedSubtree(
        key: Key('dice-${widget.entity.at}'),
        child: Container(
          color: c.raised,
          padding: const EdgeInsets.symmetric(horizontal: 4),
          child: Text.rich(
            TextSpan(
              children: [
                TextSpan(
                  text: '${widget.typed} ',
                  style: mono.copyWith(color: c.dim),
                ),
                for (final (i, s) in _slots.indexed) ...[
                  if (s.sign.isNotEmpty) TextSpan(text: s.sign, style: mono),
                  if (_die(i, s, c) case final die?)
                    WidgetSpan(
                      alignment: PlaceholderAlignment.middle,
                      child: Padding(
                        padding: const EdgeInsets.only(right: 3),
                        child: die,
                      ),
                    )
                  else
                    TextSpan(
                      text: '${faces[i]} ',
                      style: mono.copyWith(
                        fontWeight: FontWeight.w700,
                        decoration: s.dropped && !spinning
                            ? TextDecoration.lineThrough
                            : null,
                        color: s.dropped && !spinning ? c.dim : null,
                      ),
                    ),
                ],
                // The total shows once the dice have stopped.
                if (!spinning) ...[
                  TextSpan(text: '= ', style: mono),
                  TextSpan(
                    text: '${widget.entity.raw['total']}',
                    style: mono.copyWith(fontWeight: FontWeight.w700),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
