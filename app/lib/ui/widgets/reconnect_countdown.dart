import 'dart:async';

import 'package:flutter/widgets.dart';

import '../../core/live.dart';

/// Rebuilds once a second with the whole seconds left until the live
/// connection's next attempt, for "Reconnecting in N seconds"
/// (docs/CLIENTS.md section 7). The number is the wait the connection
/// really chose, read from it each time, so it is also right after the
/// wait was cut short or a new one began. [seconds] is null when no
/// attempt is waiting.
class ReconnectCountdown extends StatefulWidget {
  const ReconnectCountdown({
    super.key,
    required this.retryAt,
    required this.builder,
    this.now = DateTime.now,
  });

  /// When the next attempt will be made, or null: typically
  /// `() => server.live.retryAt`, or the soonest of several servers.
  final DateTime? Function() retryAt;
  final Widget Function(BuildContext context, int? seconds) builder;

  /// The time now. A test gives its own clock, which moves with the timers
  /// it controls; the real one does not.
  final DateTime Function() now;

  /// The soonest of several connections' next attempts, or null if none
  /// is waiting.
  static DateTime? soonest(Iterable<LiveConnection> lives) {
    DateTime? at;
    for (final l in lives) {
      final t = l.retryAt;
      if (t != null && (at == null || t.isBefore(at))) at = t;
    }
    return at;
  }

  @override
  State<ReconnectCountdown> createState() => _ReconnectCountdownState();
}

class _ReconnectCountdownState extends State<ReconnectCountdown> {
  Timer? _tick;
  int? _shown;

  int? _now() => LiveConnection.secondsUntil(widget.retryAt(), widget.now());

  @override
  void initState() {
    super.initState();
    // Looked at four times a second and redrawn only when the number
    // changes, so it steps on the second whenever the wait began.
    _tick = Timer.periodic(const Duration(milliseconds: 250), (_) {
      if (_now() != _shown) setState(() {});
    });
  }

  @override
  void dispose() {
    _tick?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      widget.builder(context, _shown = _now());
}
