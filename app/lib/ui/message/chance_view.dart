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
      final words = [t.chanceHeads, t.chanceTails];
      final text = o == 'tails' ? t.chanceTails : t.chanceHeads;
      return [ChanceSlot(key: o, text: text, reel: _reel(words, text))];
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
      final deckCards = (cardDecks[deck] ?? cardDecks['standard']!).cards;
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
            // The reel spins through cards, so each frame keeps its colour.
            final keys = _reel(deckCards, cards[i]);
            return ChanceSlot(
              key: cards[i],
              text: l.short,
              reel: [for (final k in keys) cardLabel(t, deck, k).short],
              reelKeys: keys,
              red: suited && isRedCard(cards[i]),
              reelRed: [for (final k in keys) suited && isRedCard(k)],
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

/// A result the server drew (rulings R67, R68), as a row of slots. It
/// spins once, like slot-machine reels stopping left to right, when its
/// message arrived live ([ChanceLive]), and again when pressed. With
/// reduced motion it simply appears. The animation changes only the text
/// of each slot's face, and each face keeps the width of the longest value
/// it can show, so its last frame is exactly the static drawing.
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

  /// The artwork sheets this result's slots draw from, once read.
  final Map<String, ChanceSheet> _sheets = {};
  Timer? _timer;
  int _ticks = 0;

  /// What each slot shows now: the landed faces once still.
  List<String> get faces => _faces ?? [for (final s in _slots) s.text];

  /// Whether each slot's face is red now, frame by frame.
  List<bool> get faceRed => _red ?? [for (final s in _slots) s.red];

  /// Whether the reels are turning.
  bool get spinning => _timer != null;

  /// The key each slot shows now (a card's code or a die's value).
  List<String> get keys => _keys ?? [for (final s in _slots) s.key];

  SlotArt? _art(ChanceSlot s) => slotArt(
    widget.chance['kind'] as String?,
    widget.chance['deck'] as String?,
    sides: s.sides,
    still: s.still,
  );

  /// Reads the sheets the slots need; a slot draws from its sheet once it
  /// is here, and keeps its text face without one.
  void _loadArt() {
    final bundle = DefaultAssetBundle.of(context);
    final names = {for (final s in _slots) _art(s)?.sheet}..remove(null);
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
    final at = ChanceLive.spinAt(widget.messageId);
    if (at != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) spin(from: at);
      });
    }
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

  /// Plays the reels once; [from] carries on a spin that began that long
  /// ago.
  void spin({Duration from = Duration.zero}) {
    _timer?.cancel();
    if (MediaQuery.maybeDisableAnimationsOf(context) ?? false) {
      setState(() {
        _faces = null;
        _red = null;
        _keys = null;
      });
      return;
    }
    final total = ChanceLive.spin.inMilliseconds;
    final many = _slots.length > 10;
    // Time is counted in ticks of the timer, so a slow frame stretches the
    // spin rather than skipping it.
    _ticks = from.inMilliseconds ~/ 60;
    void tick() {
      final ms = _ticks++ * 60;
      var moving = false;
      final next = <String>[];
      final red = <bool>[];
      final keys = <String>[];
      for (var i = 0; i < _slots.length; i++) {
        final s = _slots[i];
        final stop = many
            ? total * 0.5
            : total *
                  (0.45 +
                      0.55 *
                          (_slots.length == 1 ? 1 : i / (_slots.length - 1)));
        if (!s.still && ms < stop) {
          moving = true;
          final at = (ms ~/ 70) % (s.reel.length - 1);
          next.add(s.reel[at]);
          red.add(at < s.reelRed.length && s.reelRed[at]);
          keys.add(s.keyAt(at));
        } else {
          next.add(s.text);
          red.add(s.red);
          keys.add(s.key);
        }
      }
      setState(() {
        _faces = moving ? next : null;
        _red = moving ? red : null;
        _keys = moving ? keys : null;
      });
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
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final ch = widget.chance;
    final faceStyle = monoStyle(
      context,
      size: 16,
      color: c.ink,
      weight: FontWeight.w700,
    );
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
      final artFace = sheet == null
          ? null
          : art!.text
          ? sheet.polyFace(s.sides ?? 0)
          : art.face(key);
      final sprite = artFace != null && sheet!.cellOf(artFace) != null
          ? ChanceSprite(
              key: Key('art-$i'),
              sheet: sheet,
              face: artFace,
              text: art!.text ? shown : null,
              turned: s.reversed ?? false,
              dimmed: s.dropped,
            )
          : null;
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
          if (sprite != null)
            KeyedSubtree(key: Key('slot-$i'), child: sprite)
          else
            Container(
              key: Key('slot-$i'),
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
              decoration: s.still
                  ? null
                  : BoxDecoration(
                      color: c.panel,
                      border: Border.all(
                        color: spinning
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
                      color: s.dropped
                          ? c.faint
                          : shownRed
                          ? c.pink
                          : c.ink,
                      decoration: s.dropped ? TextDecoration.lineThrough : null,
                    ),
                  ),
                ],
              ),
            ),
          if (s.dropped)
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
        body.add(
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (s.position != null)
                  Text(
                    tarotPosition(t, s.position!).toUpperCase(),
                    style: ChurrType.label(c.dim, size: 10),
                  ),
                Wrap(
                  crossAxisAlignment: WrapCrossAlignment.center,
                  spacing: 8,
                  runSpacing: 4,
                  children: [
                    face(i),
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
                Text(
                  (s.reversed ?? false)
                      ? tarotReversed(t, s.key)
                      : tarotUpright(t, s.key),
                  style: TextStyle(color: c.dim, fontSize: 13),
                ),
              ],
            ),
          ),
        );
      }
      body.add(
        Text(
          [for (final k in tarotSummaryKeys(cards, rev)) tarotSummary(t, k)]
              .join(' '),
          style: TextStyle(color: c.ink, fontSize: 13),
        ),
      );
      body.add(
        Padding(
          padding: const EdgeInsets.only(top: 4),
          child: Text(
            t.tarotForFun,
            style: TextStyle(color: c.dim, fontSize: 12),
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
            if (ch['kind'] == 'roll')
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
      if (ch['kind'] == 'draw' || ch['kind'] == 'shuffle') {
        body.add(const SizedBox(height: 6));
        for (var i = 0; i < _slots.length; i++) {
          final s = _slots[i];
          body.add(
            Text.rich(
              TextSpan(
                children: [
                  TextSpan(text: '${i + 1}. '),
                  if (s.glyph.isNotEmpty)
                    TextSpan(
                      text: s.glyph,
                      style: s.red ? TextStyle(color: c.pink) : null,
                    ),
                  TextSpan(
                    text:
                        '${s.glyph.isEmpty ? '' : ' '}${_cap(ch['kind'] == 'shuffle' ? s.text : s.caption)}${(s.reversed ?? false) ? ' · ${t.chanceReversed}' : ''}',
                  ),
                ],
              ),
              key: Key('name-$i'),
              style: TextStyle(color: c.ink, fontSize: 13),
            ),
          );
        }
      }
    }
    return Semantics(
      button: true,
      label: _spoken(t, ch),
      hint: t.chanceReplay,
      excludeSemantics: true,
      child: GestureDetector(
        onTap: () => spin(),
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
