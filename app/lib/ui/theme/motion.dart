import 'dart:math' as math;

import 'package:flutter/widgets.dart';

/// Movement is quick and stepped, about a tenth of a second. Nothing
/// eases or bounces; with reduced motion asked for, nothing moves.
abstract final class Motion {
  /// The one duration: about a tenth of a second.
  static const quick = Duration(milliseconds: 100);

  /// Two steps, like the guide's `steps(2)`.
  static const Curve stepped = Steps(2);

  /// [quick], or no time at all when the platform asks for reduced motion.
  static Duration of(BuildContext context) =>
      MediaQuery.maybeDisableAnimationsOf(context) ?? false
      ? Duration.zero
      : quick;

  static bool reduced(BuildContext context) =>
      MediaQuery.maybeDisableAnimationsOf(context) ?? false;
}

/// A curve that jumps in [count] equal steps.
class Steps extends Curve {
  const Steps(this.count);
  final int count;

  @override
  double transformInternal(double t) =>
      math.min(1, (t * count).floor() / count + (t >= 1 ? 1 : 0));
}

/// Page transitions: a cut, with no slide or fade.
class CutPageTransitionsBuilder extends PageTransitionsBuilder {
  const CutPageTransitionsBuilder();

  @override
  Widget buildTransitions<T>(
    PageRoute<T> route,
    BuildContext context,
    Animation<double> animation,
    Animation<double> secondaryAnimation,
    Widget child,
  ) => child;
}
