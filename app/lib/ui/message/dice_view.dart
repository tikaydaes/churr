// Inline dice in a message (ruling R73): `[[2d6+3]]` with the result the
// server drew when the message was posted, drawn in the sentence. The
// reels spin once when the message arrived live, as a /roll result does.
// Dice an edit wrote or changed show that nothing was rolled.

import 'dart:async';

import 'package:flutter/material.dart';

import '../../core/models/message.dart';
import '../../l10n/app_localizations.dart';
import '../theme/tokens.dart';
import 'chance_view.dart';

/// A dice entity. [live] says, once, whether it arrived live.
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
  final bool Function()? live;

  @override
  State<DiceChip> createState() => DiceChipState();
}

class DiceChipState extends State<DiceChip> {
  List<ChanceSlot> _slots = const [];
  List<String>? _faces;
  Timer? _timer;
  int _ticks = 0;

  Map<String, Object?> get _roll => {
    'kind': 'roll',
    'dice': widget.entity.raw['value'],
    'terms': widget.entity.raw['terms'],
    'total': widget.entity.raw['total'],
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
    if (widget.live?.call() ?? false) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) spin();
      });
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  /// Plays the reels once, stopping left to right.
  void spin() {
    _timer?.cancel();
    if (MediaQuery.maybeDisableAnimationsOf(context) ?? false) return;
    const total = 1200;
    _ticks = 0;
    void tick() {
      final ms = _ticks++ * 60;
      var moving = false;
      final next = <String>[];
      for (var i = 0; i < _slots.length; i++) {
        final s = _slots[i];
        final stop =
            total *
            (0.45 + 0.55 * (_slots.length == 1 ? 1 : i / (_slots.length - 1)));
        if (!s.still && ms < stop) {
          moving = true;
          next.add(s.reel[(ms ~/ 70) % (s.reel.length - 1)]);
        } else {
          next.add(s.text);
        }
      }
      setState(() => _faces = moving ? next : null);
      if (!moving) {
        _timer?.cancel();
        _timer = null;
      }
    }

    _timer = Timer.periodic(const Duration(milliseconds: 60), (_) => tick());
    tick();
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
      child: InkWell(
        key: Key('dice-${widget.entity.at}'),
        onTap: spin,
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
                  TextSpan(
                    text: '${faces[i]} ',
                    style: mono.copyWith(
                      fontWeight: FontWeight.w700,
                      decoration: s.dropped ? TextDecoration.lineThrough : null,
                      color: s.dropped ? c.dim : null,
                    ),
                  ),
                ],
                TextSpan(text: '= ', style: mono),
                TextSpan(
                  text: '${widget.entity.raw['total']}',
                  style: mono.copyWith(fontWeight: FontWeight.w700),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
