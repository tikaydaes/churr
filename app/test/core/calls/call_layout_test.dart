import 'package:churr/core/calls/call_layout.dart';
import 'package:churr/core/calls/call_model.dart';
import 'package:churr/core/calls/ringing.dart';
import 'package:churr/core/calls/sdp.dart';
import 'package:flutter_test/flutter_test.dart';

TileSpec cam(String who, {bool on = true, bool self = false}) => TileSpec(
  accountId: who,
  self: self,
  trackId: self ? null : 'cam-$who',
  videoOn: on,
);

TileSpec screen(String who) => TileSpec(
  accountId: who,
  source: TrackSource.screen,
  trackId: 'scr-$who',
  videoOn: true,
);

void main() {
  group('layout', () {
    test('a grid makes the tiles as large as fits, 16:9, centred', () {
      final l = layoutTiles(
        tiles: [cam('me', self: true), cam('a'), cam('b'), cam('c')],
        width: 1280,
        height: 720,
      );
      expect(l.mode, LayoutMode.grid);
      expect(l.placements, hasLength(4));
      final b = l.placements.first.box;
      expect(b.w / b.h, closeTo(16 / 9, 0.01));
      // Two by two in 1280×720.
      expect(b.w, closeTo((720 - tileGap) / 2 * 16 / 9, 0.5));
    });

    test('too many for a grid falls back to the speaker layout with "+N"', () {
      final tiles = [for (var i = 0; i < 30; i++) cam('p$i')];
      final l = layoutTiles(
        tiles: tiles,
        width: 800,
        height: 500,
        speaker: 'p7',
      );
      expect(l.mode, LayoutMode.speaker);
      expect(l.mainTile!.tile!.accountId, 'p7');
      final more = l.placements.where((p) => p.tile == null).single;
      final shown = l.placements.where((p) => p.tile != null).length;
      expect(more.more + shown, 30);
    });

    test(
      'two people on a phone: the other fills it, own preview in a corner',
      () {
        final l = layoutTiles(
          tiles: [cam('me', self: true), cam('a')],
          width: 390,
          height: 700,
        );
        expect(l.mode, LayoutMode.duo);
        expect(l.mainTile!.tile!.accountId, 'a');
        final self = l.placements.firstWhere((p) => p.tile!.self).box;
        expect(self.x + self.w, lessThan(390));
        expect(self.w, lessThan(200));
      },
    );

    test('a shared screen takes the main area, cameras in a strip at the side '
        'on a wide window', () {
      final l = layoutTiles(
        tiles: [cam('me', self: true), cam('a'), screen('a')],
        width: 1600,
        height: 900,
      );
      expect(l.mode, LayoutMode.screen);
      expect(l.mainTile!.tile!.source, TrackSource.screen);
      final strip = l.placements.where((p) => !p.main).toList();
      expect(strip.every((p) => p.box.x > 1000), true);
      // Others first in the strip, own preview last.
      expect(strip.last.tile!.self, true);
    });

    test('a pinned tile wins over the speaker', () {
      final l = layoutTiles(
        tiles: [cam('a'), cam('b'), cam('c')],
        width: 1000,
        height: 1000,
        pinned: 'b/camera',
        speaker: 'c',
      );
      expect(l.mainTile!.tile!.accountId, 'b');
    });
  });

  group('subscriptions', () {
    test('every tile showing someone else\'s video, in physical pixels; not '
        'my preview, not a camera that is off', () {
      final l = layoutTiles(
        tiles: [cam('me', self: true), cam('a'), cam('b', on: false)],
        width: 1200,
        height: 400,
      );
      final subs = subscriptionsFor(l, devicePixelRatio: 2);
      expect(subs.map((s) => s.trackId), ['cam-a']);
      final box = l.placements.firstWhere((p) => p.tile!.accountId == 'a').box;
      expect(subs.single.width, (box.w * 2).round());
      expect(subs.single.height, (box.h * 2).round());
    });

    test(
      'over the limit: the screen first, then the main tile, then the largest',
      () {
        final tiles = [screen('s'), for (var i = 0; i < 20; i++) cam('p$i')];
        final l = layoutTiles(tiles: tiles, width: 3000, height: 2000);
        final subs = subscriptionsFor(l, devicePixelRatio: 1, max: 3);
        expect(subs, hasLength(3));
        expect(subs.first.trackId, 'scr-s');
      },
    );

    test('the server\'s layer choice by tile height (CALLS.md section 7)', () {
      expect(expectedLayer(150, ['q', 'h', 'f']), 'q');
      expect(expectedLayer(400, ['q', 'h', 'f']), 'h');
      expect(expectedLayer(900, ['q', 'h', 'f']), 'f');
      // A phone has no f: the nearest below.
      expect(expectedLayer(900, ['q', 'h']), 'h');
      expect(expectedLayer(100, ['h', 'f']), 'h');
    });
  });

  group('sdp', () {
    const offer =
        'v=0\r\n'
        'm=audio 9 UDP/TLS/RTP/SAVPF 111 63\r\n'
        'a=mid:0\r\n'
        'a=rtpmap:111 opus/48000/2\r\n'
        'm=video 9 UDP/TLS/RTP/SAVPF 96 97 98 99 100 101\r\n'
        'a=mid:1\r\n'
        'a=rtpmap:96 H264/90000\r\n'
        'a=rtpmap:97 rtx/90000\r\n'
        'a=fmtp:97 apt=96\r\n'
        'a=rtpmap:98 H264/90000\r\n'
        'a=rtpmap:99 rtx/90000\r\n'
        'a=fmtp:99 apt=98\r\n'
        'a=rtpmap:100 VP8/90000\r\n'
        'a=rtpmap:101 rtx/90000\r\n'
        'a=fmtp:101 apt=100\r\n';

    test('mids in order', () => expect(midsOf(offer), ['0', '1']));

    test('VP8 goes first in video lines; audio is untouched', () {
      final out = preferVp8(offer);
      expect(
        out,
        contains('m=video 9 UDP/TLS/RTP/SAVPF 100 96 97 98 99 101\r\n'),
      );
      expect(out, contains('m=audio 9 UDP/TLS/RTP/SAVPF 111 63\r\n'));
      expect(
        preferVp8(offer, vp8Only: true),
        contains('m=video 9 UDP/TLS/RTP/SAVPF 100 101\r\n'),
      );
    });
  });

  group('ringing (CALLS.md section 15)', () {
    test('rings unless in this call or in do not disturb; a second call is '
        'a banner without sound', () {
      RingAction r({
        bool here = false,
        bool other = false,
        bool dnd = false,
        bool muted = false,
      }) => ringAction(
        inThisCall: here,
        inAnotherCall: other,
        doNotDisturb: dnd,
        serverMuted: muted,
      );
      expect(r(), RingAction.ring);
      expect(r(here: true), RingAction.none);
      expect(r(dnd: true), RingAction.none);
      expect(r(muted: true), RingAction.none);
      expect(r(other: true), RingAction.banner);
      expect(r(other: true, dnd: true), RingAction.none);
    });

    test('which kinds of conversation ring by default', () {
      expect(kindRings(sealed: true, named: false), true);
      expect(kindRings(sealed: false, named: false), true);
      expect(kindRings(sealed: false, named: true), false);
    });
  });
}
