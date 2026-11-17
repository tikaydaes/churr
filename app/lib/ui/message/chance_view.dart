import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

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

  /// A roll's slot: its row, its place in the row, and the row's length.
  int row = 0, at = 0, of = 1;

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

/// The slots of one row of a roll: a die or coin per die, a still slot
/// per plain number.
List<ChanceSlot> _rowSlots(L10n t, List<Object?> rowTerms) {
  final out = <ChanceSlot>[];
  final terms = rowTerms.whereType<Map>().toList();
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
}

/// The slots of a result, in order (the web chat's `slots`).
List<ChanceSlot> chanceSlots(L10n t, Json c) {
  switch (c['kind']) {
    case 'roll':
      final out = <ChanceSlot>[];
      final rows = _list(c['rows']).whereType<Map>().toList();
      for (var r = 0; r < rows.length; r++) {
        final row = _rowSlots(t, _list(rows[r]['terms']));
        for (var k = 0; k < row.length; k++) {
          out.add(
            row[k]
              ..row = r
              ..at = k
              ..of = row.length,
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

/// A tarot card is this many times as tall as it is wide.
const tarotCardRatio = 246 / 176;

/// The steps between cards on a spread's table, in card widths: a column
/// is a card and a quarter of its width, a row a card and a ninth of its
/// height (the web chat's spreadBoxes).
const _col = 1.25;
const _row = tarotCardRatio * (1 + 1 / 9);

/// Where each card of a spread of [n] lies, from its layout in the
/// registry (a spread without one is a row): x and y in card widths from
/// the table's corner, and its turn; [width] and [height] are the table's.
({double width, double height, List<({double x, double y, int turn})> cards})
spreadLayout(String? spread, int n) {
  final lay = tarotSpreads[spread]?.layout;
  final places = lay != null && lay.length == n
      ? lay
      : [for (var i = 0; i < n; i++) (x: i.toDouble(), y: 0.0, turn: 0)];
  final maxX = places.fold(0.0, (m, p) => math.max(m, p.x));
  final maxY = places.fold(0.0, (m, p) => math.max(m, p.y));
  return (
    width: maxX * _col + 1,
    height: maxY * _row + tarotCardRatio,
    cards: [
      for (final p in places) (x: p.x * _col, y: p.y * _row, turn: p.turn),
    ],
  );
}

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

  /// A tarot reading's cards: which one's details are open and where that
  /// card is on the screen, and each card's focus, popover and box.
  int? _open;
  Rect? _openAt;
  List<FocusNode> _nodes = const [];
  List<OverlayPortalController> _portals = const [];
  List<GlobalKey> _boxes = const [];

  /// Which card of a reading has just landed, while it plays; -1 if none.
  int get justLanded {
    if (_ms == null || _kind != 'tarot') return -1;
    var at = -1;
    for (var i = 0; i < _slots.length; i++) {
      if (_ms! >= _plan.says[i]) at = i;
    }
    return at;
  }

  /// Opens card [i]'s details, or closes them if they are open; one is
  /// open at a time. Null closes whatever is open.
  void openCard(int? i) {
    setState(() {
      final next = i == _open ? null : i;
      if (_open != null && _open! < _portals.length) _portals[_open!].hide();
      _open = next;
      if (next != null) {
        // Where the card is, read now that it is laid out.
        final box = _boxes[next].currentContext?.findRenderObject();
        _openAt = box is RenderBox && box.hasSize
            ? box.localToGlobal(Offset.zero) & box.size
            : null;
        _portals[next].show();
      }
    });
  }

  void _tarotParts() {
    for (final n in _nodes) {
      n.dispose();
    }
    final n = _kind == 'tarot' ? _slots.length : 0;
    _nodes = [for (var i = 0; i < n; i++) FocusNode(debugLabel: 'tarot-$i')];
    _portals = [for (var i = 0; i < n; i++) OverlayPortalController()];
    _boxes = [for (var i = 0; i < n; i++) GlobalKey()];
    _open = null;
  }

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
    if (_nodes.length != (_kind == 'tarot' ? _slots.length : 0)) _tarotParts();
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
      _tarotParts();
      _loadArt();
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    for (final n in _nodes) {
      n.dispose();
    }
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
    var moving = false;
    final next = <String>[];
    final red = <bool>[];
    final keys = <String>[];
    final turning = <bool>[];
    for (var i = 0; i < _slots.length; i++) {
      final s = _slots[i];
      if (!s.still && ms < _stop(i)) {
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

  /// When slot [i]'s reel stops, in ms: rows roll together, each stopping
  /// left to right; past ten in a row, one flurry.
  double _stop(int i) {
    final total = ChanceLive.spin.inMilliseconds;
    final s = _slots[i];
    final n = _kind == 'roll' ? s.of : _slots.length;
    final at = _kind == 'roll' ? s.at : i;
    if (n > 10) return total * 0.5;
    return total * (0.45 + 0.55 * (n == 1 ? 1 : at / (n - 1)));
  }

  /// Whether row [r] of a roll has stopped: its total and dropped dice
  /// show then.
  bool rowStopped(int r) {
    if (_ms == null) return true;
    for (var i = 0; i < _slots.length; i++) {
      if (_slots[i].row == r && !_slots[i].still && _ms! < _stop(i)) {
        return false;
      }
    }
    return true;
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
      final dropped = s.dropped && rowStopped(s.row);
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
        // A tarot card is placed, labelled and pressed by its spread.
        if (_kind == 'tarot') return body;
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
          // A dropped die is faded and, never colour alone, struck through
          // corner to corner; no word beside it.
          if (dropped && sprite != null)
            Stack(
              children: [
                body,
                Positioned.fill(
                  child: CustomPaint(
                    key: Key('struck-$i'),
                    painter: const DieStrike(),
                  ),
                ),
              ],
            )
          else
            body,
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
        ],
      ),
      const SizedBox(height: 8),
    ];
    if (ch['kind'] == 'tarot') {
      final cards = _list(ch['cards']).cast<String>();
      final rev = _list(ch['reversed']).cast<bool>();
      body.add(_spread(context, t, c, ch, face));
      body.add(const SizedBox(height: 8));
      body.add(
        wait(
          const Key('summary'),
          !heldAfter,
          Text(
            [for (final k in tarotSummaryKeys(cards, rev)) tarotSummary(t, k)]
                .join(' '),
            style: TextStyle(color: c.ink, fontSize: 13),
          ),
        ),
      );
    } else if (ch['kind'] == 'roll') {
      // One line per row: its dice, then its total once they stop; with
      // more than one row, each line starts with its expression.
      final rows = _list(ch['rows']).whereType<Map>().toList();
      for (var r = 0; r < rows.length; r++) {
        if (r > 0) body.add(const SizedBox(height: 6));
        body.add(
          Wrap(
            key: Key('row-$r'),
            spacing: 6,
            runSpacing: 6,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              if (rows.length > 1)
                ConstrainedBox(
                  constraints: const BoxConstraints(minWidth: 64),
                  child: Text(
                    '${rows[r]['dice']}',
                    key: Key('row-dice-$r'),
                    style: monoStyle(context, size: 12, color: c.dim),
                  ),
                ),
              for (var i = 0; i < _slots.length; i++)
                if (_slots[i].row == r) face(i),
              if (rowStopped(r))
                Text(
                  '= ${rows[r]['total']}',
                  key: Key('chance-total-$r'),
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
      }
    } else {
      body.add(
        Wrap(
          spacing: 6,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [for (var i = 0; i < _slots.length; i++) face(i)],
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

  /// A tarot card's label: its position, name, and upright or reversed.
  String tarotLabel(L10n t, ChanceSlot s) => t.chanceTarotCard(
    s.position == null ? '' : tarotPosition(t, s.position!),
    _cap(s.caption),
    (s.reversed ?? false) ? t.chanceReversed : t.chanceUpright,
  );

  /// A tarot reading's table: each card where its spread puts it, scaled
  /// to the width there is, the crossing card turned a quarter. A card is
  /// a button: hover shows its name, a press (or Enter or Space) its
  /// details, which Escape or a press elsewhere closes; the arrow keys and
  /// Tab go through the cards in the reading's order.
  Widget _spread(
    BuildContext context,
    L10n t,
    ChurrColors c,
    Json ch,
    Widget Function(int) face,
  ) {
    final lay = spreadLayout(ch['spread'] as String?, _slots.length);
    // A big spread is drawn smaller; the popover names each card.
    final most = _slots.length > 3 ? 72.0 : 88.0;
    return LayoutBuilder(
      builder: (context, box) {
        final cw = math.min(most, box.maxWidth / lay.width);
        final ch = cw * tarotCardRatio;
        final just = justLanded;
        return FocusTraversalGroup(
          policy: OrderedTraversalPolicy(),
          child: SizedBox(
            key: const Key('spread'),
            width: lay.width * cw,
            height: lay.height * cw,
            child: Stack(
              clipBehavior: Clip.none,
              children: [
                for (var i = 0; i < _slots.length; i++)
                  Positioned(
                    left: lay.cards[i].x * cw,
                    top: lay.cards[i].y * cw,
                    width: cw,
                    height: ch,
                    child: _tarotCard(
                      context,
                      t,
                      c,
                      i,
                      face(i),
                      turn: lay.cards[i].turn,
                      just: i == just,
                    ),
                  ),
              ],
            ),
          ),
        );
      },
    );
  }

  Widget _tarotCard(
    BuildContext context,
    L10n t,
    ChurrColors c,
    int i,
    Widget face, {
    required int turn,
    required bool just,
  }) {
    final s = _slots[i];
    final n = _slots.length;
    Widget card = SizedBox.expand(
      child: FittedBox(fit: BoxFit.contain, child: face),
    );
    // The card that has just landed wears the focus outline.
    card = Container(
      key: Key('mark-$i'),
      foregroundDecoration: just
          ? BoxDecoration(
              border: Border.all(
                color: c.focus,
                width: 3,
                strokeAlign: BorderSide.strokeAlignOutside,
              ),
            )
          : null,
      child: card,
    );
    if (turn == 90) {
      card = Transform.rotate(
        key: Key('cross-$i'),
        angle: math.pi / 2,
        child: card,
      );
    }
    return TapRegion(
      groupId: this,
      onTapOutside: (_) {
        if (_open != null) openCard(null);
      },
      child: OverlayPortal(
        controller: _portals[i],
        overlayChildBuilder: (context) => _detail(context, t, c, i),
        child: FocusTraversalOrder(
          order: NumericFocusOrder(i.toDouble()),
          child: Semantics(
            key: Key('card-$i'),
            label: tarotLabel(t, s),
            button: true,
            expanded: _open == i,
            excludeSemantics: true,
            onTap: () => openCard(i),
            child: Focus(
              focusNode: _nodes[i],
              onKeyEvent: (node, e) {
                if (e is! KeyDownEvent) return KeyEventResult.ignored;
                final k = e.logicalKey;
                if (k == LogicalKeyboardKey.enter ||
                    k == LogicalKeyboardKey.space) {
                  openCard(i);
                } else if (k == LogicalKeyboardKey.escape) {
                  if (_open == null) return KeyEventResult.ignored;
                  openCard(null);
                } else if (k == LogicalKeyboardKey.arrowRight ||
                    k == LogicalKeyboardKey.arrowDown) {
                  _nodes[(i + 1) % n].requestFocus();
                } else if (k == LogicalKeyboardKey.arrowLeft ||
                    k == LogicalKeyboardKey.arrowUp) {
                  _nodes[(i + n - 1) % n].requestFocus();
                } else {
                  return KeyEventResult.ignored;
                }
                return KeyEventResult.handled;
              },
              child: ListenableBuilder(
                listenable: _nodes[i],
                builder: (context, child) => Container(
                  key: _boxes[i],
                  foregroundDecoration: _nodes[i].hasFocus
                      ? BoxDecoration(
                          border: Border.all(
                            color: c.focus,
                            width: 3,
                            strokeAlign: BorderSide.strokeAlignOutside,
                          ),
                        )
                      : null,
                  child: child,
                ),
                child: MouseRegion(
                  cursor: SystemMouseCursors.click,
                  child: GestureDetector(
                    behavior: HitTestBehavior.opaque,
                    onTap: () {
                      _nodes[i].requestFocus();
                      openCard(i);
                    },
                    child: Tooltip(
                      message: cardName(t, s),
                      triggerMode: TooltipTriggerMode.longPress,
                      excludeFromSemantics: true,
                      child: card,
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  /// Card [i]'s details, under the card (over it near the screen's foot),
  /// kept on the screen.
  Widget _detail(BuildContext context, L10n t, ChurrColors c, int i) {
    final s = _slots[i];
    final screen = MediaQuery.sizeOf(context);
    final at = _openAt;
    if (at == null) return const SizedBox.shrink();
    final w = math.min(300.0, screen.width - 8);
    final left = (at.center.dx - w / 2).clamp(4.0, screen.width - w - 4);
    final below = screen.height - at.bottom > 180;
    return Positioned(
      left: left,
      width: w,
      top: below ? at.bottom + 6 : null,
      bottom: below ? null : screen.height - at.top + 6,
      child: TapRegion(
        groupId: this,
        child: Semantics(
          container: true,
          liveRegion: true,
          child: Container(
            key: Key('detail-$i'),
            padding: const EdgeInsets.fromLTRB(10, 8, 10, 8),
            decoration: BoxDecoration(
              color: c.panel,
              border: Border.all(color: c.ink, width: 2),
              boxShadow: [
                BoxShadow(color: c.lavender, offset: const Offset(3, 3)),
              ],
            ),
            child: DefaultTextStyle(
              style: TextStyle(color: c.ink, fontSize: 13),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (s.position != null)
                    Text(
                      tarotPosition(t, s.position!).toUpperCase(),
                      style: ChurrType.label(c.dim, size: 10),
                    ),
                  const SizedBox(height: 2),
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
                  const SizedBox(height: 3),
                  Text(
                    (s.reversed ?? false)
                        ? tarotReversed(t, s.key)
                        : tarotUpright(t, s.key),
                    style: TextStyle(color: c.dim, fontSize: 13),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  String _spoken(L10n t, Json ch) {
    final head = _headline(t, ch);
    if (ch['kind'] == 'roll') {
      // Each row its dice and its total; with more than one, each row
      // named by its expression.
      final rows = _list(ch['rows']).whereType<Map>().toList();
      final said = [
        for (var r = 0; r < rows.length; r++)
          '${[
            for (final s in _slots)
              if (s.row == r) [if (s.sign == '−') '−', s.text, if (s.dropped) '(${t.chanceDropped})'].join(' '),
          ].join(', ')}; ${t.chanceTotalSpoken((rows[r]['total'] as num? ?? 0).toInt())}',
      ];
      if (rows.length == 1) return '$head: ${said[0]}';
      return '$head: ${[for (var r = 0; r < rows.length; r++) '${rows[r]['dice']}: ${said[r]}'].join('. ')}';
    }
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
    return '$head: ${parts.join(', ')}';
  }
}

/// The line struck through a dropped die, corner to corner: the dice's
/// own ink edged in paper, so it shows on the die and beside it in any
/// theme.
class DieStrike extends CustomPainter {
  const DieStrike();

  @override
  void paint(Canvas canvas, Size size) {
    final from = Offset(0, size.height), to = Offset(size.width, 0);
    canvas.drawLine(
      from,
      to,
      Paint()
        ..color = CardColours.paper
        ..strokeWidth = 4,
    );
    canvas.drawLine(
      from,
      to,
      Paint()
        ..color = CardColours.ink
        ..strokeWidth = 2,
    );
  }

  @override
  bool shouldRepaint(DieStrike old) => false;
}
