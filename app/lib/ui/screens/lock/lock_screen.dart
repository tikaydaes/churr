import 'dart:ui' show ImageFilter;

import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import '../../../state/app_lock.dart';
import '../../theme/shapes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';

/// The lock screen (ruling R75, docs/CLIENTS.md section 9.27). It takes the
/// place of the whole app, its navigator and every route on it, so no
/// screen of the account is in the widget tree while it shows. Behind the
/// card is a stand-in of the layout made of empty blocks, blurred: there is
/// nothing of the account in it to blur. The card has the product mark,
/// "Locked" and one button: Unlock, or Cancel while the browser is open.
class LockScreen extends StatelessWidget {
  const LockScreen({super.key, required this.lock});
  final AppLock lock;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    return ListenableBuilder(
      listenable: lock,
      builder: (context, _) {
        final waiting = lock.unlocking;
        return Material(
          color: c.bg,
          child: Stack(
            fit: StackFit.expand,
            children: [
              // The blur is the owner's own request for this screen, the
              // one exception to "no blur" (docs/STYLE.md).
              ExcludeSemantics(
                child: ImageFiltered(
                  imageFilter: ImageFilter.blur(sigmaX: 10, sigmaY: 10),
                  child: _StandIn(colors: c),
                ),
              ),
              SafeArea(
                child: Center(
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: ClipPath(
                      clipper: const CutCornerClipper(14),
                      child: Container(
                        key: const Key('lock-card'),
                        width: 360,
                        color: c.panel,
                        padding: const EdgeInsets.all(28),
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            const Wordmark(size: 32),
                            const SizedBox(height: 16),
                            Semantics(
                              header: true,
                              child: Text(
                                t.appLockTitle.toUpperCase(),
                                style: ChurrType.display(c.ink, size: 34),
                              ),
                            ),
                            if (waiting) ...[
                              const SizedBox(height: 12),
                              Text(
                                t.appLockWaiting,
                                style: TextStyle(color: c.dim, fontSize: 15),
                              ),
                            ],
                            if (lock.problem != null) ...[
                              const SizedBox(height: 12),
                              Text(
                                lock.problem!,
                                key: const Key('lock-problem'),
                                style: TextStyle(color: c.pink, fontSize: 15),
                              ),
                            ],
                            const SizedBox(height: 20),
                            FilledButton(
                              key: const Key('lock-unlock'),
                              autofocus: true,
                              onPressed: waiting
                                  ? lock.cancelUnlock
                                  : () => lock.unlock(),
                              child: Text(
                                waiting ? t.appLockCancel : t.appLockUnlock,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}

/// Blocks where the list and the messages would be, at fixed widths, so
/// the stand-in is the same every time and says nothing.
class _StandIn extends StatelessWidget {
  const _StandIn({required this.colors});
  final ChurrColors colors;

  static const _rows = [0.7, 0.55, 0.82, 0.6, 0.48, 0.75, 0.66, 0.52];
  static const _lines = [
    [0.62, 0.4],
    [0.85],
    [0.48, 0.7, 0.3],
    [0.76],
    [0.55, 0.35],
  ];

  @override
  Widget build(BuildContext context) {
    final c = colors;
    Widget bar(double f, double h, Color col) => FractionallySizedBox(
      alignment: Alignment.centerLeft,
      widthFactor: f,
      child: Container(height: h, color: col),
    );
    return LayoutBuilder(
      builder: (context, box) {
        final wide = box.maxWidth >= 600;
        final main = Padding(
          padding: const EdgeInsets.fromLTRB(32, 56, 32, 24),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.end,
            children: [
              for (final m in _lines)
                Padding(
                  padding: const EdgeInsets.only(bottom: 18),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Container(width: 36, height: 36, color: c.raised),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          children: [
                            for (final f in m)
                              Padding(
                                padding: const EdgeInsets.only(bottom: 8),
                                child: bar(f, 12, c.line),
                              ),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              Container(height: 48, color: c.panel),
            ],
          ),
        );
        if (!wide) return main;
        return Row(
          children: [
            Container(
              width: 280,
              color: c.panel,
              padding: const EdgeInsets.fromLTRB(16, 56, 16, 16),
              child: Column(
                children: [
                  for (final f in _rows)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 14),
                      child: bar(f, 14, c.raised),
                    ),
                ],
              ),
            ),
            Expanded(child: main),
          ],
        );
      },
    );
  }
}
