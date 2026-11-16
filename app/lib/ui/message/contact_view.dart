// Telephone numbers and email addresses in messages (ruling R73): the
// typed text as a control whose menu calls or writes through the device's
// own app (tel:, mailto:) or copies. Nothing is looked up.

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../core/models/message.dart';
import '../../l10n/app_localizations.dart';
import '../theme/tokens.dart';

/// Opens a tel: or mailto: address. Tests replace it.
Future<void> Function(Uri uri) openContact = (uri) async {
  await launchUrl(uri, mode: LaunchMode.externalApplication);
};

/// A phone or email entity.
class ContactChip extends StatelessWidget {
  const ContactChip({
    super.key,
    required this.entity,
    required this.typed,
    required this.style,
  });
  final Entity entity;
  final String typed;
  final TextStyle style;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = Localizations.of<L10n>(context, L10n);
    final phone = entity.type == 'phone';
    final value = '${entity.raw['value'] ?? ''}';
    return Semantics(
      container: true,
      button: true,
      label: phone
          ? (t?.phoneLabel(typed) ?? 'Phone number $typed')
          : (t?.emailLabel(typed) ?? 'Email address $typed'),
      excludeSemantics: true,
      child: PopupMenuButton<String>(
        key: Key('${entity.type}-${entity.at}'),
        tooltip: '',
        onSelected: (v) {
          if (v == 'copy') {
            Clipboard.setData(ClipboardData(text: value));
          } else {
            openContact(Uri.parse('${phone ? 'tel' : 'mailto'}:$value'));
          }
        },
        itemBuilder: (_) => [
          PopupMenuItem(
            key: Key('${entity.type}-open'),
            value: 'open',
            child: Text(
              phone ? (t?.contactCall ?? 'Call') : (t?.contactWrite ?? 'Write'),
            ),
          ),
          PopupMenuItem(
            key: Key('${entity.type}-copy'),
            value: 'copy',
            child: Text(t?.contactCopy ?? 'Copy'),
          ),
        ],
        child: Text(
          typed,
          style: style.copyWith(
            color: c.link,
            decoration: TextDecoration.underline,
            decorationColor: c.link,
          ),
        ),
      ),
    );
  }
}
