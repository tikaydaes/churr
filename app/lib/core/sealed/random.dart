// Where random values come from. Every value is drawn for a named purpose
// (the `for` names of testdata/sealed/README.md), so a test can hand the
// library the vectors' values in place of the generator.

import 'dart:math';
import 'dart:typed_data';

abstract class SealedRandom {
  /// [n] random bytes for the purpose [what].
  Uint8List take(String what, int n);
}

/// The operating system's generator.
class SystemRandom implements SealedRandom {
  final Random _r = Random.secure();
  @override
  Uint8List take(String what, int n) {
    final b = Uint8List(n);
    for (var i = 0; i < n; i++) {
      b[i] = _r.nextInt(256);
    }
    return b;
  }
}

/// For tests only: values given in advance, in the order they will be
/// drawn, each with the purpose it must be drawn for. A draw for another
/// purpose or length, or past the end, throws.
class GivenRandom implements SealedRandom {
  final List<(String, Uint8List)> _values;
  int _next = 0;
  GivenRandom(this._values);

  int get used => _next;
  int get left => _values.length - _next;

  @override
  Uint8List take(String what, int n) {
    if (_next >= _values.length) {
      throw StateError('no given random value left for "$what"');
    }
    final (forWhat, value) = _values[_next];
    if (forWhat != what || value.length != n) {
      throw StateError(
        'random value ${_next + 1} is for "$forWhat" (${value.length} bytes), '
        'but "$what" ($n bytes) was drawn',
      );
    }
    _next++;
    return Uint8List.fromList(value);
  }
}
