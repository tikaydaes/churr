// How a value of kind dice_roll looks (docs/DESIGN-MODULES.md section
// 9.1; the web chat's shared/types/dice-roll/view.js): a roll the server
// made, drawn as /roll's result always was.
//
//   card     a message whose view is a roll: a row of slots per row of
//            dice, then its total ([ChanceView], whose reels and artwork
//            the kind uses until they move with it)
//   chip     inline dice, `[[2d6+3]]`: the typed text, each die and the
//            total in the sentence, the height of the line
//   tooltip  the roll in words, on the chip
//   dialog   the chip pressed: the whole roll, row by row, as the card
//
// Both spin once when the message arrives live, stopping left to right,
// the totals and dropped dice shown once they stop; with reduced motion
// the result simply appears, and a press never rolls again. The dice are
// drawn as /roll's: a d6 from the cube sheet, any other die or a coin on
// the fly ([DieFace]); a d6 keeps its number when the sheet cannot be
// read. Dice an edit wrote or changed say that nothing was rolled.
// Everything shown is text the server's data holds, drawn as text.

import 'dart:async';

import 'package:flutter/material.dart';

import '../../../core/commands/chance_live.dart';
import '../../../core/models/json.dart';
import '../../../l10n/app_localizations.dart';
import '../../layout/breakpoints.dart';
import '../../message/chance_art.dart';
import '../../message/chance_view.dart';
import '../../theme/tokens.dart';
import 'dice_shape.dart';

/// How tall an inline die is, in the text's size: the pips and a
/// two-figure number stay legible, and it stays inside the line.
const dieInLine = 1.35;

/// A roll in the shape the reels draw. [d] is the checked data
/// (core/types/dice_roll.dart).
Json asChance(Json d) => {'kind': 'roll', 'dice': d['dice'], 'rows': d['rows']};

/// The roll in words: what a screen reader hears, and what the chip says
/// on hover.
String diceRollSpoken(L10n t, Json d) {
  final c = asChance(d);
  return chanceSpoken(t, c, chanceSlots(t, c));
}

/// A message's roll. It spins once when the message arrived live, on the
/// message's own clock ([ChanceLive], under [messageId]).
class DiceRollCard extends StatelessWidget {
  const DiceRollCard({super.key, required this.messageId, required this.data});
  final String messageId;
  final Json data;

  @override
  Widget build(BuildContext context) =>
      ChanceView(messageId: messageId, chance: asChance(data));
}

/// Shows the whole roll of inline dice typed as [typed]: a sheet on a
/// phone, a dialog elsewhere. It does not spin.
Future<void> showDiceRoll(BuildContext context, Json data, String typed) {
  final t = L10n.of(context);
  Widget body(BuildContext context) {
    final c = ChurrColors.of(context);
    return Padding(
      key: const Key('dice-roll-dialog'),
      padding: const EdgeInsets.all(16),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(t.diceTitle, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 6),
          Text(
            '“$typed”',
            style: TextStyle(color: c.dim, fontFamily: fontMono),
          ),
          const SizedBox(height: 10),
          // Never noted as arriving live, so it shows the result at once.
          ChanceView(messageId: '', chance: asChance(data)),
        ],
      ),
    );
  }

  if (WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact) {
    return showModalBottomSheet<void>(
      sheetAnimationStyle: AnimationStyle.noAnimation,
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: body,
    );
  }
  return showDialog<void>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) =>
        Dialog(child: SizedBox(width: 440, child: body(context))),
  );
}

/// Inline dice in the sentence: the checked [data] of the value over
/// [typed], or [removed] when nothing was rolled for them. [live] says how
/// far into their one live spin the reels are, or null when they do not
/// spin ([ChanceLive.spinAt]); [at] is where they stand in the text.
class DiceRollChip extends StatefulWidget {
  const DiceRollChip({
    super.key,
    required this.data,
    required this.typed,
    required this.style,
    this.at = 0,
    this.removed = false,
    this.live,
  });
  final Json? data;
  final String typed;
  final TextStyle style;
  final int at;
  final bool removed;
  final Duration? Function()? live;

  @override
  State<DiceRollChip> createState() => DiceRollChipState();
}

class DiceRollChipState extends State<DiceRollChip> {
  List<ChanceSlot> _slots = const [];
  List<String>? _faces;
  List<String>? _keys;
  Timer? _timer;

  /// The six-sided die's sheet, once read.
  ChanceSheet? _cube;
  int _ticks = 0;

  bool get _rolled => !widget.removed && widget.data != null;

  List<Json> get _rows => [
    for (final r in widget.data?['rows'] as List? ?? const [])
      (r as Map).cast<String, dynamic>(),
  ];

  /// Whether the reels are turning.
  bool get spinning => _timer != null;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final t = Localizations.of<L10n>(context, L10n);
    if (t == null || !_rolled) return;
    _slots = chanceSlots(t, asChance(widget.data!));
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
  Widget? _die(int i, ChanceSlot s) {
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
    final rows = _rows;
    final said = diceRollSpoken(t, widget.data!);
    return Semantics(
      container: true,
      button: true,
      label: t.diceOpen(widget.typed, said),
      excludeSemantics: true,
      child: Tooltip(
        message: said,
        child: InkWell(
          key: Key('dice-${widget.at}'),
          // A press shows the roll; it never rolls again.
          onTap: () => showDiceRoll(context, widget.data!, widget.typed),
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
                  for (var r = 0; r < rows.length; r++) ...[
                    if (r > 0) TextSpan(text: '; ', style: mono),
                    for (final (i, s) in _slots.indexed)
                      if (s.row == r) ...[
                        if (s.sign.isNotEmpty)
                          TextSpan(text: s.sign, style: mono),
                        if (_die(i, s) case final die?)
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
                    // A row's total shows once the dice have stopped.
                    if (!spinning) ...[
                      TextSpan(text: '= ', style: mono),
                      TextSpan(
                        text: '${rows[r]['total']}',
                        style: mono.copyWith(fontWeight: FontWeight.w700),
                      ),
                    ],
                  ],
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
