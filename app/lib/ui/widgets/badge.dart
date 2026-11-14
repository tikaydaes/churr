import 'package:flutter/material.dart';

import '../theme/shapes.dart';
import '../theme/tokens.dart';

/// A count. What notifies is the tight yellow price tag of the guide; the
/// outlined form is a quiet count (Martian Mono in an edge box), for
/// totals that are not themselves unread. Never colour alone: the number
/// is always there.
class CountBadge extends StatelessWidget {
  const CountBadge(this.count, {super.key, this.outlined = false, this.label});
  final int count;
  final bool outlined;

  /// What a screen reader says, such as "3 notifications".
  final String? label;

  @override
  Widget build(BuildContext context) {
    if (!outlined) return PriceTag(count, label: label);
    final c = ChurrColors.of(context);
    return Semantics(
      label: label ?? '$count',
      excludeSemantics: true,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 2),
        decoration: BoxDecoration(
          border: Border.all(color: c.edge, width: 1.5),
        ),
        child: Text(
          count > 99 ? '99+' : '$count',
          style: monoStyle(
            context,
            size: 10,
            color: c.dim,
            weight: FontWeight.w600,
          ),
        ),
      ),
    );
  }
}
