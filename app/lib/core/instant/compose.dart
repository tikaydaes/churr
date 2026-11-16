// What the composer needs for instants (ruling R69): where a phrase may be
// made into a time, keeping an accepted phrase's range as the text around
// it changes, and turning a typed instant back into plain text. The web
// chat's shared/instant/compose.js does the same.

import '../markdown/nodes.dart';
import '../markdown/parser.dart';

/// A phrase the author made into a time: its words, where they are, and
/// the hint sent with the post.
class InstantHint {
  const InstantHint({
    required this.start,
    required this.end,
    required this.typed,
    required this.time,
    required this.precision,
    required this.zone,
  });
  final int start, end;
  final String typed, time, precision, zone;

  InstantHint moved(int to) => InstantHint(
    start: to,
    end: to + typed.length,
    typed: typed,
    time: time,
    precision: precision,
    zone: zone,
  );

  /// As the request's `instant_hints` carries it.
  Map<String, Object?> toJson() => {
    'start': start,
    'end': end,
    'time': time,
    'precision': precision,
    'zone': zone,
  };
}

void _walk(String text, void Function(Inline s, bool inLink) visit) {
  void inl(List<Inline> xs, bool inLink) {
    for (final s in xs) {
      visit(s, inLink);
      inl(
        s.children,
        inLink ||
            s.kind == InlineKind.link ||
            s.kind == InlineKind.noLink ||
            s.kind == InlineKind.image,
      );
    }
  }

  void blk(List<Block> bs) {
    for (final b in bs) {
      inl(b.inlines, false);
      for (final it in b.items) {
        inl(it.inlines, false);
        blk(it.lists);
      }
      for (final row in [b.head, ...b.rows]) {
        for (final c in row) {
          inl(c.inlines, false);
        }
      }
      blk(b.blocks);
    }
  }

  blk(parseMarkdown(text).blocks);
}

/// The runs of plain text of [text] outside links and code: where the
/// server accepts a hint.
List<(int, int)> textRuns(String text) {
  final out = <(int, int)>[];
  _walk(text, (s, inLink) {
    if (!inLink &&
        (s.kind == InlineKind.text || s.kind == InlineKind.instant)) {
      out.add((s.at, s.end));
    }
  });
  return out;
}

/// Whether [start, end) may carry a hint: inside one run of plain text,
/// overlapping no typed instant and no other hint.
bool hintFits(
  String text,
  int start,
  int end, [
  List<InstantHint> others = const [],
]) {
  if (!textRuns(text).any((r) => start >= r.$1 && end <= r.$2)) return false;
  final taken = <(int, int)>[for (final o in others) (o.start, o.end)];
  _walk(text, (s, _) {
    if (s.kind == InlineKind.instant) taken.add((s.at, s.end));
  });
  return !taken.any((r) => start < r.$2 && r.$1 < end);
}

/// Moves accepted hints to where their words are in the new text: the
/// same place, or the nearest place the same words are found. A hint whose
/// words were edited is dropped, so the phrase may be offered again.
List<InstantHint> trackHints(List<InstantHint> hints, String text) {
  final out = <InstantHint>[];
  for (final h in hints) {
    var best = -1;
    if (h.end <= text.length && text.substring(h.start, h.end) == h.typed) {
      best = h.start;
    } else {
      for (
        var i = text.indexOf(h.typed);
        i >= 0;
        i = text.indexOf(h.typed, i + 1)
      ) {
        if (best < 0 || (i - h.start).abs() < (best - h.start).abs()) best = i;
      }
    }
    if (best < 0) continue;
    final m = h.moved(best);
    if (hintFits(text, m.start, m.end, out)) out.add(m);
  }
  return out;
}
