import 'package:churr/core/setup_code.dart';
import 'package:churr/state/device_setup.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

void main() {
  test('a set-up code carries the theme choice', () async {
    final (from, _) = await makeAccountApp(null);
    await from.prefs.putRaw('theme', 'system');
    await from.prefs.putRaw('theme_dark', 'saved:abc');
    await from.prefs.putRaw('theme_light', 'builtin:light-hd');
    await from.prefs.putRaw('theme_per_server', '1');
    await from.prefs.putRaw('theme_copies', '{"saved:abc":{"name":"Night"}}');
    final codes = (await setupPayloadOf(from)).encode();

    final (to, _) = await makeAccountApp(null, servers: 0);
    final collector = SetupCollector();
    for (final c in codes) {
      collector.add(c);
    }
    await applySetupPayload(to, collector.payload!);
    expect(to.prefs.raw('theme'), 'system');
    expect(to.prefs.raw('theme_dark'), 'saved:abc');
    expect(to.prefs.raw('theme_light'), 'builtin:light-hd');
    expect(to.prefs.raw('theme_per_server'), '1');
    expect(to.prefs.raw('theme_copies'), contains('Night'));
  });
}
