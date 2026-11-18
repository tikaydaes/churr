// The app's pins of servers' VRF key histories (lib/core/fair/pin.dart), the
// same cases as the web chat's in e2e/fair.mjs.

import 'package:churr/core/fair/pin.dart';
import 'package:churr/core/models/server.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final a = {'key': 'AA', 'began': '2026-11-01T00:00:00.000Z', 'sig': 'x'};
  final b = {'key': 'BB', 'began': '2026-11-10T00:00:00.000Z', 'sig': 'y'};
  final c = {'key': 'CC', 'began': '2026-11-10T00:00:00.000Z', 'sig': 'z'};

  test('comparePin', () {
    expect(comparePin(null, [a]).state, PinState.first);
    expect(comparePin([a], [a]).state, PinState.same);
    final grown = comparePin([a], [a, b]);
    expect(grown.state, PinState.grown);
    expect(grown.added, [b]);
    expect(comparePin([a, b], [a, c]).state, PinState.changed);
    expect(comparePin([a, b], [a]).state, PinState.changed);
    expect(comparePin([a], const []).state, PinState.changed);
    expect(comparePin(null, const []).state, PinState.changed);
  });

  test('the pins keep the first sight and what grows, never a change', () {
    String? stored;
    PinState? load(List<Map<String, dynamic>> shown) => (FairHistories(
      (v) async => stored = v,
    )..load(stored)).show('a.example', shown)?.state;
    expect(load([a]), PinState.first);
    expect(load([a]), PinState.same);
    expect(load([c]), PinState.changed);
    expect(load([a]), PinState.same);
    expect(load([a, b]), PinState.grown);
    expect(load([a, b]), PinState.same);
    stored = 'not json';
    expect(load([a]), PinState.first);
  });

  test('one pin per server name, from any source', () {
    final pins = FairHistories((_) async {})..load(null);
    expect(pins.of('b.example'), isNull);
    pins.showShared({
      'vrf_keys': [
        {
          'server': 'b.example',
          'keys': [b],
        },
        'junk',
      ],
    });
    expect(pins.of('b.example')!.keys, [b]);
    pins.show('b.example', [c]);
    expect(pins.of('b.example')!.changed, isTrue);
    expect(pins.of('b.example')!.keys, [b]);
    pins.show('b.example', [b]);
    expect(pins.of('b.example')!.changed, isTrue, reason: 'a change stays');
  });

  test('getServer gives the history', () {
    final info = ServerInfo.fromJson({
      'name': 'a.example',
      'vrf_keys': [a, b, 'junk'],
    });
    expect(info.vrfKeys, [a, b]);
    expect(ServerInfo.fromJson({'name': 'x'}).vrfKeys, isEmpty);
  });
}
