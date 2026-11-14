import 'package:flutter/material.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../../theme/tokens.dart';

final _qr = ChurrColors.resolve(dark: true);

/// A QR code for the sealed screens (the approval code, the comparison
/// code): navy on the dark theme's cyan in both themes, as the set-up
/// code is, so every camera reads it.
class SealedQr extends StatelessWidget {
  const SealedQr({
    super.key,
    required this.data,
    required this.label,
    this.size = 200,
  });
  final String data;
  final String label;
  final double size;

  @override
  Widget build(BuildContext context) => Center(
    child: Container(
      color: _qr.cyan,
      padding: const EdgeInsets.all(10),
      child: QrImageView(
        data: data,
        size: size,
        eyeStyle: QrEyeStyle(eyeShape: QrEyeShape.square, color: _qr.onCyan),
        dataModuleStyle: QrDataModuleStyle(
          dataModuleShape: QrDataModuleShape.square,
          color: _qr.onCyan,
        ),
        semanticsLabel: label,
      ),
    ),
  );
}
