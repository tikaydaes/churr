import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../core/models/json.dart';
import '../../core/commands/chance_live.dart';
import '../../core/commands/commands.dart';
import '../../core/commands/decks.dart';
import '../../l10n/app_localizations.dart';
import '../theme/shapes.dart';
import '../theme/tokens.dart';
import 'card_names.dart';
import 'chance_art.dart';
import 'dice_shape.dart';

/// A card's words: its full name, a short form for its slot, and its
/// Unicode character where it has one.
({String name, String short, String glyph}) cardLabel(
  L10n t,
  String deck,
  String code,
) {
  final d = cardDecks[deck] ?? cardDecks['standard']!;
  final glyph = cardGlyphs[code] ?? '';
  if (cardExtras.contains(code)) {
    return (
      name: cardExtraName(t, code) ?? code,
      short: cardShort[code] ?? code,
      glyph: glyph,
    );
  }
  final at = code.indexOf('-');
  final suit = at < 0 ? code : code.substring(0, at);
  final rank = at < 0 ? '' : code.substring(at + 1);
  final s = cardSuitName(t, suit);
  final r = cardRankName(t, rank);
  final rs = cardShort[rank] ?? rank;
  final name = switch (d.naming) {
    'suit_rank' => t.cardSuitAndRank(s, r),
    'suit_number' => t.cardSuitAndRank(s, rs),
    _ => t.cardRankOfSuit(r, s),
  };
  final symbol = cardSuitSymbols[suit];
  final short = symbol != null
      ? '$rs$symbol'
      : '$rs ${s.split(' ').first.substring(0, math.min(4, s.split(' ').first.length))}';
  return (name: name, short: short, glyph: glyph);
}

/// One slot of a result: a die, the coin, the pick, a shuffled place or
/// a card. [key] is what the server drew; [text] what the slot shows;
/// [reel] what it spins through, ending on [text].
class ChanceSlot {
  ChanceSlot({
    required this.key,
    required this.text,
    required this.reel,
    this.caption = '',
    this.sign = '',
    this.dropped = false,
    this.still = false,
    this.reversed,
    this.position,
    this.glyph = '',
    this.red = false,
    this.reelRed = const [],
    this.reelKeys,
    this.sides,
    this.card = false,
  });
  final String key;
  final String text;
  final List<String> reel;
  final String caption;
  final String sign;
  final bool dropped;
  final bool still;
  final bool? reversed;
  final String? position;
  final String glyph;

  /// Whether the card is printed in red ([isRedCard]); [reelRed] is the
  /// same for each frame of [reel].
  final bool red;
  final List<bool> reelRed;

  /// What each frame of [reel] shows, as a key (a card's code; for dice
  /// the reel's values are the keys).
  final List<String>? reelKeys;

  /// A die's number of sides.
  final int? sides;

  /// Whether this is a card or tile, turned over rather than spun.
  final bool card;

  /// The key of frame [at] of the reel.
  String keyAt(int at) => (reelKeys ?? reel)[at];
}

/// Whether a standard-deck card is printed in red: hearts, diamonds and the
/// red joker. Colour only repeats the suit, which the symbol and the card's
/// name already say.
bool isRedCard(String code) =>
    code.startsWith('hearts-') ||
    code.startsWith('diamonds-') ||
    code == 'joker-red';

final _random = math.Random();

List<String> _reel(List<String> pool, String text, [int n = 14]) => [
  for (var i = 0; i < n - 1; i++) pool[_random.nextInt(pool.length)],
  text,
];

List<Object?> _list(Object? v) => v is List ? v : const [];

/// A coin's reel turns it over and over: the two faces of [pair] by turns,
/// ending on [landed].
List<String> coinReel(List<String> pair, String landed, [int n = 14]) {
  final end = math.max(0, pair.indexOf(landed));
  return [for (var i = 0; i < n; i++) pair[(end + n - 1 - i) % 2]];
}

/// The slots of a result, in order (the web chat's `slots`).
List<ChanceSlot> chanceSlots(L10n t, Json c) {
  switch (c['kind']) {
    case 'roll':
      final out = <ChanceSlot>[];
      final terms = _list(c['terms']).whereType<Map>().toList();
      for (var ti = 0; ti < terms.length; ti++) {
        final term = terms[ti];
        final sign = (term['sign'] as num? ?? 1) < 0
            ? '−'
            : ti > 0
            ? '+'
            : '';
        final value = '${term['value'] ?? 0}';
        final count = (term['count'] as num?)?.toInt() ?? 0;
        if (count == 0) {
          out.add(
            ChanceSlot(
              key: value,
              text: value,
              reel: [value, value],
              still: true,
              sign: sign,
            ),
          );
          continue;
        }
        final sides = (term['sides'] as num?)?.toInt() ?? 6;
        final faces = [for (var i = 1; i <= sides; i++) '$i'];
        final dropped = _list(term['dropped'])
            .map((x) => (x as num).toInt())
            .toSet();
        final rolls = _list(term['rolls']);
        for (var i = 0; i < rolls.length; i++) {
          final v = '${rolls[i]}';
          // A two-sided die is a coin: heads is 1, tails 2.
          if (sides == 2) {
            String words(String k) =>
                t.chanceCoinValue(k == '2' ? 'tails' : 'heads', k);
            final keys = coinReel(const ['1', '2'], v);
            out.add(
              ChanceSlot(
                key: v,
                text: words(v),
                reel: [for (final k in keys) words(k)],
                reelKeys: keys,
                sides: 2,
                dropped: dropped.contains(i),
                sign: i == 0 ? sign : '',
                caption: i == 0 ? 'd2' : '',
              ),
            );
            continue;
          }
          out.add(
            ChanceSlot(
              key: v,
              text: v,
              reel: _reel(faces, v),
              sides: sides,
              dropped: dropped.contains(i),
              sign: i == 0 ? sign : '',
              caption: i == 0 ? 'd$sides' : '',
            ),
          );
        }
      }
      return out;
    case 'flip':
      final o = c['outcome'] as String? ?? '';
      String words(String k) => k == 'tails' ? t.chanceTails : t.chanceHeads;
      final keys = coinReel(const ['heads', 'tails'], o);
      return [
        ChanceSlot(
          key: o,
          text: words(o),
          reel: [for (final k in keys) words(k)],
          reelKeys: keys,
        ),
      ];
    case 'pick':
      final opts = _list(c['options']).cast<String>();
      final p = (c['picked'] as num?)?.toInt() ?? 0;
      final v = p < opts.length ? opts[p] : '';
      return [
        ChanceSlot(key: v, text: v, reel: _reel(opts.isEmpty ? [v] : opts, v)),
      ];
    case 'shuffle':
      final opts = _list(c['options']).cast<String>();
      final order = _list(c['order']).map((x) => (x as num).toInt()).toList();
      return [
        for (var at = 0; at < order.length; at++)
          ChanceSlot(
            key: opts[order[at]],
            text: opts[order[at]],
            reel: _reel(opts, opts[order[at]]),
            caption: '${at + 1}',
          ),
      ];
    case 'draw' || 'tarot':
      final deck = c['deck'] as String? ?? 'standard';
      final suited = c['kind'] == 'draw' && deck != 'mahjong';
      final cards = _list(c['cards']).cast<String>();
      final rev = c['reversed'] is List
          ? _list(c['reversed']).cast<bool>()
          : null;
      final positions = c['kind'] == 'tarot'
          ? tarotSpreads[c['spread']]?.positions ?? const []
          : const <String>[];
      return [
        for (var i = 0; i < cards.length; i++)
          () {
            final l = cardLabel(t, deck, cards[i]);
            // A card is turned over, not spun: it has no reel.
            return ChanceSlot(
              key: cards[i],
              text: l.short,
              reel: [l.short],
              card: true,
              red: suited && isRedCard(cards[i]),
              caption: l.name,
              glyph: l.glyph,
              reversed: rev == null ? null : (i < rev.length && rev[i]),
              position: i < positions.length ? positions[i] : null,
            );
          }(),
      ];
  }
  return const [];
}

String _headline(L10n t, Json c) {
  final n = _list(c['cards']).length;
  return switch (c['kind']) {
    'roll' => t.chanceRolled('${c['dice']}'),
    'flip' => t.chanceFlipped,
    'pick' => t.chancePicked(_list(c['options']).length),
    'shuffle' => t.chanceShuffled(_list(c['options']).length),
    'draw' =>
      c['deck'] == 'mahjong'
          ? t.chanceDrewTiles(n)
          : t.chanceDrew(n, '${c['deck']}'),
    'tarot' => t.chanceTarot(tarotSpreadName(t, '${c['spread']}')),
    _ => '',
  };
}

String _cap(String s) => s.isEmpty ? s : s[0].toUpperCase() + s.substring(1);

/// A result the server drew (rulings R67, R68, R87), as a row of slots.
/// It plays once when its message arrived live ([ChanceLive]), never on
/// a press, and with reduced motion it simply appears. Dice, coins,
/// picks and places spin like slot-machine reels stopping left to right,
/// and a roll's total and dropped dice show once they have stopped.
/// Cards and tiles lie face down and are turned over one after another
/// ([revealPlan]); a tarot reading's words for each card appear as it
/// lands, the summary after the last. A card's name is not listed: it
/// pops up over the card on hover and on a tap, and is the card's
/// label. The last frame is exactly the static drawing.
class ChanceView extends StatefulWidget {
  const ChanceView({super.key, required this.messageId, required this.chance});
  final String messageId;
  final Json chance;

  @override
  State<ChanceView> createState() => ChanceViewState();
}

class ChanceViewState extends State<ChanceView> {
  List<ChanceSlot> _slots = const [];
  List<String>? _faces;
  List<bool>? _red;
  List<String>? _keys;
  List<bool>? _turning;

  /// The artwork sheets this result's slots draw from, once read.
  final Map<String, ChanceSheet> _sheets = {};
  Timer? _timer;

  /// How far into its animation the result is, in ms, while it plays.
  int? _ms;

  /// What each slot shows now: the landed faces once still.
  List<String> get faces => _faces ?? [for (final s in _slots) s.text];

  /// Whether each slot's face is red now, frame by frame.
  List<bool> get faceRed => _red ?? [for (final s in _slots) s.red];

  /// Whether the animation is playing.
  bool get spinning => _ms != null;

  /// The key each slot shows now (a card's code or a die's value).
  List<String> get keys => _keys ?? [for (final s in _slots) s.key];

  /// Whether each slot's reel is still turning; a slot that has landed
  /// shows the server's value.
  List<bool> get turning => _turning ?? [for (final _ in _slots) false];

  String? get _kind => widget.chance['kind'] as String?;
  bool get _cards => _kind == 'draw' || _kind == 'tarot';
  RevealPlan get _plan => revealPlan(_kind, _slots.length);
  int get _length => ChanceLive.length(_kind, _slots.length).inMilliseconds;

  /// Whether card [i] lies face down now: until halfway through its turn.
  bool down(int i) =>
      _ms != null && _cards && _ms! < _plan.flips[i] + Reveal.flip ~/ 2;

  /// Whether card [i]'s words (a tarot reading's) are still held back.
  bool held(int i) => _ms != null && _ms! < _plan.says[i];

  /// Whether the summary is still held back.
  bool get heldAfter => _ms != null && _ms! < _plan.after;

  SlotArt? _art(ChanceSlot s) => slotArt(
    widget.chance['kind'] as String?,
    widget.chance['deck'] as String?,
    sides: s.sides,
    still: s.still,
  );

  /// Reads the sheets the slots need; a slot draws from its sheet once it
  /// is here, and keeps its text face without one. Cards also need the
  /// french sheet, for the card back.
  void _loadArt() {
    final bundle = DefaultAssetBundle.of(context);
    final names = {for (final s in _slots) _art(s)?.sheet}..remove(null);
    if (_cards && names.isNotEmpty && widget.chance['deck'] != 'mahjong') {
      names.add('french');
    }
    for (final name in names) {
      if (_sheets.containsKey(name)) continue;
      ChanceArt.load(bundle, name!).then((sheet) {
        if (sheet != null && mounted) setState(() => _sheets[name] = sheet);
      });
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _slots = chanceSlots(L10n.of(context), widget.chance);
    _loadArt();
    if (_timer != null) return;
    final at = ChanceLive.spinAt(
      widget.messageId,
      length: ChanceLive.length(_kind, _slots.length),
    );
    // The first frame is the animation's, so the total or a card's face
    // is never seen before it starts.
    if (at != null) _play(at);
  }

  @override
  void didUpdateWidget(ChanceView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.chance != widget.chance) {
      _slots = chanceSlots(L10n.of(context), widget.chance);
      _loadArt();
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  /// Plays the animation once; [from] carries on one that began that
  /// long ago.
  void _play(Duration from) {
    _timer?.cancel();
    _timer = null;
    if (MediaQuery.maybeDisableAnimationsOf(context) ?? false) return;
    if (from.inMilliseconds >= _length) return;
    _ms = from.inMilliseconds;
    _frame();
    // Time is counted in ticks of the timer, so a slow frame stretches
    // the animation rather than skipping it.
    _timer = Timer.periodic(const Duration(milliseconds: 30), (_) {
      if (!mounted) return;
      setState(() {
        _ms = _ms! + 30;
        _frame();
      });
    });
  }

  /// Sets the reels' frame at [_ms], and ends the animation when it is
  /// over.
  void _frame() {
    final ms = _ms!;
    if (ms >= _length) {
      _timer?.cancel();
      _timer = null;
      _ms = null;
      _faces = _red = _keys = _turning = null;
      return;
    }
    if (_cards) return;
    final total = ChanceLive.spin.inMilliseconds;
    final many = _slots.length > 10;
    var moving = false;
    final next = <String>[];
    final red = <bool>[];
    final keys = <String>[];
    final turning = <bool>[];
    for (var i = 0; i < _slots.length; i++) {
      final s = _slots[i];
      final stop = many
          ? total * 0.5
          : total *
                (0.45 +
                    0.55 * (_slots.length == 1 ? 1 : i / (_slots.length - 1)));
      if (!s.still && ms < stop) {
        moving = true;
        final at = (ms ~/ 70) % (s.reel.length - 1);
        next.add(s.reel[at]);
        red.add(at < s.reelRed.length && s.reelRed[at]);
        keys.add(s.keyAt(at));
        turning.add(true);
      } else {
        next.add(s.text);
        red.add(s.red);
        keys.add(s.key);
        turning.add(false);
      }
    }
    if (!moving) {
      _timer?.cancel();
      _timer = null;
      _ms = null;
      _faces = _red = _keys = _turning = null;
      return;
    }
    _faces = next;
    _red = red;
    _keys = keys;
    _turning = turning;
  }

  /// A card's name, as its popup says it and its label.
  String cardName(L10n t, ChanceSlot s) =>
      '${_cap(s.caption)}${(s.reversed ?? false) ? ' · ${t.chanceReversed}' : ''}';

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final ch = widget.chance;
    final faceStyle = monoStyle(
      context,
      size: 16,
      color: c.ink,
      weight: FontWeight.w700,
    );
    final deck = ch['deck'] as String?;

    /// A card's back, the size of its face.
    Widget back(int i, Size size) {
      if (deck == 'mahjong') return TileBack(key: Key('back-$i'));
      final french = _sheets['french'];
      if (french != null && french.cellOf('back') != null) {
        return ChanceSprite(
          key: Key('back-$i'),
          sheet: french,
          face: 'back',
          size: size,
        );
      }
      return Container(
        key: Key('back-$i'),
        width: size.width,
        height: size.height,
        decoration: BoxDecoration(
          color: CardColours.lavender,
          border: Border.all(color: CardColours.ink, width: 2),
        ),
      );
    }

    Widget face(int i) {
      final s = _slots[i];
      final longest = s.reel.reduce(
        (a, b) => a.runes.length >= b.runes.length ? a : b,
      );
      final shown = i < faces.length ? faces[i] : s.text;
      final shownRed = i < faceRed.length ? faceRed[i] : s.red;
      final art = _art(s);
      final sheet = art == null ? null : _sheets[art.sheet];
      final key = i < keys.length ? keys[i] : s.key;
      final artFace = sheet == null ? null : art!.face(key);
      // A dropped die is shown so once the dice have stopped.
      final dropped = s.dropped && !spinning;
      final die = slotDie(
        widget.chance['kind'] as String?,
        sides: s.sides,
        still: s.still,
      );
      final sprite = die != null
          ? DieFace(key: Key('art-$i'), sides: die, value: key, dimmed: dropped)
          : artFace != null && sheet!.cellOf(artFace) != null
          ? ChanceSprite(
              key: Key('art-$i'),
              sheet: sheet,
              face: artFace,
              // A reversed card lands upside down.
              turned:
                  (s.reversed ?? false) && !(i < turning.length && turning[i]),
              dimmed: dropped,
            )
          : null;
      Widget body = sprite != null
          ? KeyedSubtree(key: Key('slot-$i'), child: sprite)
          : Container(
              key: Key('slot-$i'),
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
              decoration: s.still
                  ? null
                  : BoxDecoration(
                      color: c.panel,
                      border: Border.all(
                        color: spinning && !s.card
                            ? c.cyan
                            : (s.reversed ?? false)
                            ? c.pink
                            : c.edge,
                        width: 2,
                      ),
                    ),
              // The longest value it can show holds the width, so the slot
              // does not move while it spins or when it stops.
              child: Stack(
                alignment: Alignment.center,
                children: [
                  Opacity(opacity: 0, child: Text(longest, style: faceStyle)),
                  Text(
                    shown,
                    key: Key('face-$i'),
                    style: faceStyle.copyWith(
                      color: dropped
                          ? c.faint
                          : shownRed
                          ? c.pink
                          : c.ink,
                      decoration: dropped ? TextDecoration.lineThrough : null,
                    ),
                  ),
                ],
              ),
            );
      if (s.card) {
        // Face down until halfway through its turn; the turn narrows the
        // card to nothing and opens it again.
        if (down(i)) {
          final size = sheet != null
              ? Size(
                  sheet.cell.width * chanceArtScale,
                  sheet.cell.height * chanceArtScale,
                )
              : const Size(88, 123);
          body = back(i, size);
        }
        final at = _ms == null ? null : _ms! - _plan.flips[i];
        final squeeze = at != null && at >= 0 && at < Reveal.flip
            ? math.cos(math.pi * at / Reveal.flip).abs()
            : 1.0;
        if (squeeze < 1) {
          body = Transform(
            alignment: Alignment.center,
            transform: Matrix4.diagonal3Values(squeeze, 1, 1),
            child: body,
          );
        }
        final name = cardName(t, s);
        return Semantics(
          key: Key('card-$i'),
          label: name,
          image: true,
          excludeSemantics: true,
          child: Tooltip(
            message: name,
            triggerMode: TooltipTriggerMode.tap,
            excludeFromSemantics: true,
            child: body,
          ),
        );
      }
      return Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (s.sign.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(right: 3),
              child: Text(
                s.sign,
                style: monoStyle(context, size: 14, color: c.dim),
              ),
            ),
          body,
          if (dropped)
            Padding(
              padding: const EdgeInsets.only(left: 3),
              child: Text(
                t.chanceDropped.toUpperCase(),
                style: monoStyle(context, size: 9, color: c.dim),
              ),
            ),
        ],
      );
    }

    /// Words held back (unseen, in their place) until [shown].
    Widget wait(Key key, bool shown, Widget child) => AnimatedOpacity(
      key: key,
      opacity: shown ? 1 : 0,
      duration: const Duration(milliseconds: 200),
      child: child,
    );

    final body = <Widget>[
      Row(
        children: [
          Flexible(
            child: Text(
              _headline(t, ch),
              style: TextStyle(fontWeight: FontWeight.w700, color: c.ink),
            ),
          ),
          const SizedBox(width: 8),
          Tag(t.chanceByServer, fill: c.cyan, degrees: 1.5),
        ],
      ),
      const SizedBox(height: 8),
    ];
    if (ch['kind'] == 'tarot') {
      final cards = _list(ch['cards']).cast<String>();
      final rev = _list(ch['reversed']).cast<bool>();
      for (var i = 0; i < _slots.length; i++) {
        final s = _slots[i];
        final open = !held(i);
        body.add(
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (s.position != null)
                  wait(
                    Key('pos-$i'),
                    open,
                    Text(
                      tarotPosition(t, s.position!).toUpperCase(),
                      style: ChurrType.label(c.dim, size: 10),
                    ),
                  ),
                Wrap(
                  crossAxisAlignment: WrapCrossAlignment.center,
                  spacing: 8,
                  runSpacing: 4,
                  children: [
                    face(i),
                    wait(
                      Key('say-$i'),
                      open,
                      Wrap(
                        spacing: 8,
                        crossAxisAlignment: WrapCrossAlignment.center,
                        children: [
                          Text(
                            _cap(s.caption),
                            style: TextStyle(
                              fontWeight: FontWeight.w700,
                              color: c.ink,
                            ),
                          ),
                          Text(
                            (s.reversed ?? false)
                                ? t.chanceReversed
                                : t.chanceUpright,
                            style: monoStyle(context, size: 10, color: c.dim),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
                wait(
                  Key('meaning-$i'),
                  open,
                  Text(
                    (s.reversed ?? false)
                        ? tarotReversed(t, s.key)
                        : tarotUpright(t, s.key),
                    style: TextStyle(color: c.dim, fontSize: 13),
                  ),
                ),
              ],
            ),
          ),
        );
      }
      body.add(
        wait(
          const Key('summary'),
          !heldAfter,
          Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                [
                  for (final k in tarotSummaryKeys(cards, rev))
                    tarotSummary(t, k),
                ].join(' '),
                style: TextStyle(color: c.ink, fontSize: 13),
              ),
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  t.tarotForFun,
                  style: TextStyle(color: c.dim, fontSize: 12),
                ),
              ),
            ],
          ),
        ),
      );
    } else {
      body.add(
        Wrap(
          spacing: 6,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            for (var i = 0; i < _slots.length; i++) face(i),
            // The total shows once the dice have stopped.
            if (ch['kind'] == 'roll' && !spinning)
              Text(
                '= ${ch['total']}',
                key: const Key('chance-total'),
                style: monoStyle(
                  context,
                  size: 18,
                  color: c.ink,
                  weight: FontWeight.w800,
                ),
              ),
          ],
        ),
      );
      if (ch['kind'] == 'shuffle') {
        body.add(const SizedBox(height: 6));
        for (var i = 0; i < _slots.length; i++) {
          body.add(
            Text(
              '${i + 1}. ${_cap(_slots[i].text)}',
              key: Key('name-$i'),
              style: TextStyle(color: c.ink, fontSize: 13),
            ),
          );
        }
      }
    }
    // Cards carry their own labels; any other result is read as a whole.
    return Semantics(
      container: true,
      label: _cards ? _headline(t, ch) : _spoken(t, ch),
      excludeSemantics: !_cards,
      child: Container(
        margin: const EdgeInsets.only(top: 6),
        constraints: const BoxConstraints(maxWidth: 640),
        child: ClipPath(
          clipper: const CutCornerClipper(8),
          child: Container(
            color: c.raised,
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 10),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: body,
            ),
          ),
        ),
      ),
    );
  }

  String _spoken(L10n t, Json ch) {
    final head = _headline(t, ch);
    final parts = [
      for (final s in _slots)
        [
          if (s.position != null) '${tarotPosition(t, s.position!)}:',
          if (s.sign == '−') '−',
          ch['kind'] == 'draw' || ch['kind'] == 'tarot' ? s.caption : s.text,
          if (s.dropped) '(${t.chanceDropped})',
          if (s.reversed != null)
            (s.reversed! ? t.chanceReversed : t.chanceUpright),
        ].join(' '),
    ];
    final total = ch['kind'] == 'roll'
        ? '; ${t.chanceTotalSpoken((ch['total'] as num? ?? 0).toInt())}'
        : '';
    return '$head: ${parts.join(', ')}$total';
  }
}
