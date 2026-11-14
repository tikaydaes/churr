import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import '../../../state/sealed_state.dart';
import '../../../state/server_state.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import '../account/common.dart';
import '../account/sign_in_screens.dart' show ScanCodeScreen, canScan;
import 'sealed_qr.dart';

/// Opens "Compare codes" for the chat with [other] (docs/CLIENTS.md 9.21).
Future<void> openCompareCodes(
  BuildContext context,
  ServerState server,
  String conversationId,
  String other,
  String name,
) => showAccountScreen(
  context,
  title: L10n.of(context).sealedCompareCodes,
  child: CompareCodeScreen(server: server, other: other, name: name),
);

/// The 60-digit comparison code (docs/SEALED.md section 15) in twelve
/// groups of five, and as a QR code holding the same; scanning the other
/// person's checks it. The verified mark is kept on this device only.
class CompareCodeScreen extends StatefulWidget {
  const CompareCodeScreen({
    super.key,
    required this.server,
    required this.other,
    required this.name,
  });
  final ServerState server;
  final String other;
  final String name;

  @override
  State<CompareCodeScreen> createState() => _CompareCodeScreenState();
}

class _CompareCodeScreenState extends State<CompareCodeScreen> {
  bool scanning = false;
  bool? matched;

  /// The code as twelve groups of five digits.
  static List<String> groups(String digits) => [
    for (var i = 0; i + 5 <= digits.length; i += 5) digits.substring(i, i + 5),
  ];

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final ctl = SealedHub.instance.of(widget.server);
    final s = ctl?.session;
    final code = s?.compareCode(widget.other)?.replaceAll(RegExp(r'\D'), '');
    if (s == null || code == null) {
      return Padding(
        padding: const EdgeInsets.all(24),
        child: Text(t.compareNotYet),
      );
    }
    if (scanning) {
      return ScanCodeScreen(
        onText: (text) async => setState(() {
          scanning = false;
          matched = text.replaceAll(RegExp(r'\D'), '') == code;
        }),
      );
    }
    final verified = s.verified(widget.other);
    final g = groups(code);
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        Text(t.compareIntro(widget.name)),
        const SizedBox(height: 16),
        Semantics(
          label: t.compareDigitsLabel,
          child: Container(
            key: const Key('compare-digits'),
            color: c.raised,
            padding: const EdgeInsets.all(12),
            child: Wrap(
              spacing: 14,
              runSpacing: 8,
              alignment: WrapAlignment.center,
              children: [
                for (final x in g)
                  Text(x, style: monoStyle(context).copyWith(fontSize: 18)),
              ],
            ),
          ),
        ),
        const SizedBox(height: 12),
        SealedQr(data: code, label: t.compareDigitsLabel, size: 180),
        const SizedBox(height: 12),
        if (matched == true)
          NoteLine(t.compareScanMatch, icon: ChurrIcons.check),
        if (matched == false) ErrorLine(t.compareScanNoMatch),
        Text(t.compareProves, style: TextStyle(color: c.dim)),
        const SizedBox(height: 12),
        if (verified)
          NoteLine(t.compareMarked(widget.name), icon: ChurrIcons.check)
        else
          Wrap(
            spacing: 8,
            children: [
              FilledButton(
                key: const Key('compare-mark'),
                onPressed: matched == false
                    ? null
                    : () async {
                        await s.markVerified(widget.other);
                        if (mounted) setState(() {});
                      },
                child: Text(t.compareMark(widget.name)),
              ),
              if (canScan)
                OutlinedButton.icon(
                  icon: const Glyph(ChurrIcons.scan, size: 16),
                  onPressed: () => setState(() => scanning = true),
                  label: Text(t.compareScan),
                ),
            ],
          ),
      ],
    );
  }
}
