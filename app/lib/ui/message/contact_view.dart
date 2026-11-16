// Telephone numbers, email addresses and places in messages (ruling
// R73): the
// typed text as a control whose menu calls or writes through the device's
// own app (tel:, mailto:) or copies. Nothing is looked up.

import 'package:flutter/foundation.dart';
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

/// What selecting a place does here: open the device's maps on a phone
/// (geo: on Android, Apple Maps' own maps: on iOS), copy elsewhere.
/// Returns whether it copied.
Future<bool> openPlace(String lat, String lon) async {
  switch (defaultTargetPlatform) {
    case TargetPlatform.android:
      await openContact(Uri.parse('geo:$lat,$lon?q=$lat,$lon'));
      return false;
    case TargetPlatform.iOS:
      await openContact(Uri.parse('maps:?q=$lat,$lon'));
      return false;
    default:
      await Clipboard.setData(ClipboardData(text: '$lat,$lon'));
      return true;
  }
}

/// A geo entity: the typed text as a control that opens maps or copies.
class PlaceChip extends StatelessWidget {
  const PlaceChip({
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
    final lat = '${entity.raw['lat']}', lon = '${entity.raw['lon']}';
    final phone =
        defaultTargetPlatform == TargetPlatform.android ||
        defaultTargetPlatform == TargetPlatform.iOS;
    return Semantics(
      container: true,
      button: true,
      label: phone
          ? (t?.placeOpenLabel(typed) ?? 'Place $typed. Open in maps')
          : (t?.placeCopyLabel(typed) ?? 'Place $typed. Copy'),
      excludeSemantics: true,
      child: InkWell(
        key: Key('geo-${entity.at}'),
        onTap: () => openPlace(lat, lon),
        child: Text(
          typed,
          style: style.copyWith(
            decoration: TextDecoration.underline,
            decorationStyle: TextDecorationStyle.dotted,
            decorationColor: c.cyan,
          ),
        ),
      ),
    );
  }
}
