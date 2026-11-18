// The app's pin of a server's VRF key history (lib/core/fair/pin.dart), the
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

  test('checkPin keeps the first sight and what grows, never a change', () async {
    String? stored;
    Future<PinState> load(List<Map<String, dynamic>> shown) async =>
        (await checkPin(() async => stored, (v) async => stored = v, shown))
            .state;
    expect(await load([a]), PinState.first);
    expect(await load([a]), PinState.same);
    expect(await load([c]), PinState.changed);
    expect(await load([a]), PinState.same);
    expect(await load([a, b]), PinState.grown);
    expect(await load([a, b]), PinState.same);
    stored = 'not json';
    expect(await load([a]), PinState.first);
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
