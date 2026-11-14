// Where tiles go on the call screen, and the subscription list that follows
// from it (docs/CALLS.md section 7, "What a subscriber asks for";
// docs/CLIENTS.md 9.14). Plain Dart and pure, so every decision is held by
// unit tests; the call screen only draws what this returns.
import 'dart:math' as math;

import 'call_model.dart';

/// One thing that can have a tile: a person's camera (or their avatar when
/// it is off), or a shared screen.
class TileSpec {
  const TileSpec({
    required this.accountId,
    this.source = TrackSource.camera,
    this.self = false,
    this.trackId,
    this.videoOn = false,
  });

  final String accountId;

  /// [TrackSource.camera] or [TrackSource.screen].
  final TrackSource source;

  /// The person's own preview: drawn from the local camera, never
  /// subscribed.
  final bool self;

  /// The video track behind the tile, when one is published.
  final String? trackId;

  /// Whether there is a picture to show (published and not muted).
  final bool videoOn;

  String get key => '$accountId/${source.name}';
}

/// A rectangle in logical pixels. (Not `dart:ui`'s Rect: this file has no
/// Flutter imports.)
class Box {
  const Box(this.x, this.y, this.w, this.h);
  final double x, y, w, h;

  @override
  String toString() =>
      'Box(${x.round()}, ${y.round()}, ${w.round()}x${h.round()})';
}

enum LayoutMode {
  /// Everyone the same size.
  grid,

  /// One main tile (the pinned tile, else the loudest speaker) with the
  /// others in a strip.
  speaker,

  /// Two people on a narrow screen: the other person fills it, the
  /// person's own preview sits in a corner.
  duo,

  /// A shared screen in the main area, cameras in a strip.
  screen,
}

/// Where one tile goes. [tile] is null for the "+N more" cell.
class TilePlacement {
  const TilePlacement(this.tile, this.box, {this.main = false, this.more = 0});
  final TileSpec? tile;
  final Box box;
  final bool main;

  /// For the overflow cell: how many tiles are not shown.
  final int more;
}

class CallLayout {
  const CallLayout(this.mode, this.placements);
  final LayoutMode mode;
  final List<TilePlacement> placements;

  TilePlacement? get mainTile {
    for (final p in placements) {
      if (p.main) return p;
    }
    return null;
  }
}

/// The gap between tiles, in logical pixels.
const tileGap = 6.0;

/// The narrowest a grid tile may be before the layout gives up on a grid.
const minGridTileWidth = 140.0;

/// Lays tiles out in a [width] by [height] area.
///
/// - A shared screen always takes the main area (unless another tile is
///   pinned), with cameras in a strip.
/// - [preferred] grid: everyone the same size while each tile can be at
///   least [minGridTileWidth] wide; past that, the speaker layout.
/// - Two tiles on a narrow area (a phone): the other person fills it and
///   the person's own preview sits in a corner.
/// - Speaker: [pinned] (a tile key), else the loudest [speaker], else the
///   first other person, in the main area.
CallLayout layoutTiles({
  required List<TileSpec> tiles,
  required double width,
  required double height,
  LayoutMode preferred = LayoutMode.grid,
  String? pinned,
  String? speaker,
}) {
  if (tiles.isEmpty || width <= 0 || height <= 0) {
    return const CallLayout(LayoutMode.grid, []);
  }
  final screen = tiles.where((t) => t.source == TrackSource.screen).toList();
  final pinnedTile = pinned == null
      ? null
      : tiles.where((t) => t.key == pinned).firstOrNull;

  if (screen.isNotEmpty || pinnedTile != null) {
    final main = pinnedTile ?? screen.first;
    return _withStrip(
      screen.isNotEmpty && main.source == TrackSource.screen
          ? LayoutMode.screen
          : LayoutMode.speaker,
      main,
      [
        for (final t in tiles)
          if (t.key != main.key) t,
      ],
      width,
      height,
    );
  }
  final narrow = width < 600 && height > width;
  if (tiles.length == 2 && narrow && tiles.any((t) => t.self)) {
    final other = tiles.firstWhere((t) => !t.self);
    final self = tiles.firstWhere((t) => t.self);
    final w = math.max(96.0, width * 0.28);
    final h = w * 4 / 3;
    return CallLayout(LayoutMode.duo, [
      TilePlacement(other, Box(0, 0, width, height), main: true),
      TilePlacement(self, Box(width - w - 12, height - h - 12, w, h)),
    ]);
  }
  if (preferred == LayoutMode.grid || tiles.length == 1) {
    final grid = _grid(tiles, width, height);
    if (grid != null) return CallLayout(LayoutMode.grid, grid);
  }
  final main =
      tiles.where((t) => t.accountId == speaker && !t.self).firstOrNull ??
      tiles.where((t) => !t.self).firstOrNull ??
      tiles.first;
  return _withStrip(
    LayoutMode.speaker,
    main,
    [
      for (final t in tiles)
        if (t.key != main.key) t,
    ],
    width,
    height,
  );
}

/// An even grid of 16:9 tiles, the column count chosen to make them as
/// large as possible. Null when they would be narrower than
/// [minGridTileWidth].
List<TilePlacement>? _grid(List<TileSpec> tiles, double width, double height) {
  final n = tiles.length;
  var best = 0.0, bestCols = 1, bestW = 0.0, bestH = 0.0;
  for (var cols = 1; cols <= n; cols++) {
    final rows = (n / cols).ceil();
    final cw = (width - tileGap * (cols - 1)) / cols;
    final ch = (height - tileGap * (rows - 1)) / rows;
    if (cw <= 0 || ch <= 0) continue;
    final tw = math.min(cw, ch * 16 / 9);
    final th = tw * 9 / 16;
    if (tw * th > best) {
      best = tw * th;
      bestCols = cols;
      bestW = tw;
      bestH = th;
    }
  }
  if (bestW < minGridTileWidth && n > 1) return null;
  final rows = (n / bestCols).ceil();
  final totalH = rows * bestH + (rows - 1) * tileGap;
  final top = (height - totalH) / 2;
  final out = <TilePlacement>[];
  for (var i = 0; i < n; i++) {
    final row = i ~/ bestCols;
    final inRow = row == rows - 1 ? n - row * bestCols : bestCols;
    final rowW = inRow * bestW + (inRow - 1) * tileGap;
    final left = (width - rowW) / 2;
    final col = i % bestCols;
    out.add(
      TilePlacement(
        tiles[i],
        Box(
          left + col * (bestW + tileGap),
          top + row * (bestH + tileGap),
          bestW,
          bestH,
        ),
        main: n == 1,
      ),
    );
  }
  return out;
}

/// A main tile and a strip of the rest: along the bottom on a tall or
/// square area, down the right side on a wide one. Tiles that do not fit
/// are summed up in a last "+N" cell.
CallLayout _withStrip(
  LayoutMode mode,
  TileSpec main,
  List<TileSpec> rest,
  double width,
  double height,
) {
  if (rest.isEmpty) {
    return CallLayout(mode, [
      TilePlacement(main, Box(0, 0, width, height), main: true),
    ]);
  }
  final side = width > height * 1.4;
  final out = <TilePlacement>[];
  if (side) {
    final sw = math.min(240.0, width * 0.22);
    final sh = sw * 9 / 16;
    out.add(
      TilePlacement(main, Box(0, 0, width - sw - tileGap, height), main: true),
    );
    _strip(out, rest, (height + tileGap) ~/ (sh + tileGap), (i) {
      return Box(width - sw, i * (sh + tileGap), sw, sh);
    });
  } else {
    final sh = math.min(120.0, math.max(72.0, height * 0.18));
    final sw = sh * 16 / 9;
    out.add(
      TilePlacement(main, Box(0, 0, width, height - sh - tileGap), main: true),
    );
    _strip(out, rest, (width + tileGap) ~/ (sw + tileGap), (i) {
      return Box(i * (sw + tileGap), height - sh, sw, sh);
    });
  }
  return CallLayout(mode, out);
}

void _strip(
  List<TilePlacement> out,
  List<TileSpec> rest,
  int fit,
  Box Function(int i) at,
) {
  if (fit <= 0) return;
  // The person's own preview goes last in the strip: others come first.
  final ordered = [
    for (final t in rest)
      if (!t.self) t,
    for (final t in rest)
      if (t.self) t,
  ];
  final shown = ordered.length <= fit ? ordered.length : fit - 1;
  for (var i = 0; i < shown; i++) {
    out.add(TilePlacement(ordered[i], at(i)));
  }
  if (shown < ordered.length) {
    out.add(TilePlacement(null, at(shown), more: ordered.length - shown));
  }
}

/// One entry of `call.subscribe`'s `video`.
class Subscription {
  const Subscription(this.trackId, this.width, this.height);
  final String trackId;
  final int width;
  final int height;

  Map<String, Object> toJson() => {
    'track_id': trackId,
    'width': width,
    'height': height,
  };

  @override
  bool operator ==(Object other) =>
      other is Subscription &&
      other.trackId == trackId &&
      other.width == width &&
      other.height == height;

  @override
  int get hashCode => Object.hash(trackId, width, height);

  @override
  String toString() => '$trackId ${width}x$height';
}

/// The subscription list for what is on screen: every tile showing
/// someone else's video, at its size in physical pixels. When there are
/// more than [max], a shared screen comes first, then the main tile, then
/// the largest.
List<Subscription> subscriptionsFor(
  CallLayout layout, {
  required double devicePixelRatio,
  int max = 16,
}) {
  final shown = [
    for (final p in layout.placements)
      if (p.tile != null &&
          !p.tile!.self &&
          p.tile!.trackId != null &&
          p.tile!.videoOn)
        p,
  ];
  int rank(TilePlacement p) =>
      (p.tile!.source == TrackSource.screen ? 2 : 0) + (p.main ? 1 : 0);
  shown.sort((a, b) {
    final r = rank(b).compareTo(rank(a));
    if (r != 0) return r;
    return (b.box.w * b.box.h).compareTo(a.box.w * a.box.h);
  });
  final seen = <String>{};
  final out = <Subscription>[];
  for (final p in shown) {
    if (out.length >= max) break;
    if (!seen.add(p.tile!.trackId!)) continue;
    out.add(
      Subscription(
        p.tile!.trackId!,
        (p.box.w * devicePixelRatio).round(),
        (p.box.h * devicePixelRatio).round(),
      ),
    );
  }
  return out;
}

/// The layer the server will pick for a tile [tileHeight] physical pixels
/// tall (docs/CALLS.md section 7, "By size" and "By what exists"): the
/// smallest layer at least 80% as tall, else the tallest; then the
/// nearest one the publisher sends, below first. Used to label tiles in
/// debug builds and to check the server in tests.
String? expectedLayer(int tileHeight, List<String> published) {
  const heights = {'q': 180, 'h': 360, 'f': 720};
  const order = ['q', 'h', 'f'];
  var want = 'f';
  for (final l in order) {
    if (heights[l]! >= tileHeight * 0.8) {
      want = l;
      break;
    }
  }
  if (published.isEmpty) return null;
  if (published.contains(want)) return want;
  final i = order.indexOf(want);
  for (var j = i - 1; j >= 0; j--) {
    if (published.contains(order[j])) return order[j];
  }
  for (var j = i + 1; j < order.length; j++) {
    if (published.contains(order[j])) return order[j];
  }
  return null;
}

/// The simulcast layers a camera is sent in (docs/CALLS.md section 7):
/// all three on a desktop, `q` and `h` on a phone.
class LayerSpec {
  const LayerSpec(this.rid, this.scaleDown, this.maxKbps, this.maxFps);
  final String rid;
  final double scaleDown;
  final int maxKbps;
  final int maxFps;
}

const cameraLayersDesktop = [
  LayerSpec('q', 4, 150, 15),
  LayerSpec('h', 2, 500, 30),
  LayerSpec('f', 1, 1200, 30),
];

/// A phone captures at 640×360 and sends that and a quarter of it.
const cameraLayersPhone = [
  LayerSpec('q', 2, 150, 15),
  LayerSpec('h', 1, 500, 30),
];
