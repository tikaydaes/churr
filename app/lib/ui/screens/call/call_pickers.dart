import 'package:flutter/material.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';

import '../../../l10n/app_localizations.dart';
import '../../../platform/calls/webrtc_media.dart';
import '../../theme/tokens.dart';

/// The desktop picker for a screen or a window to share. Returns the
/// source's identifier, or null.
Future<String?> pickScreen(BuildContext context) async {
  final List<DesktopCapturerSource> sources;
  try {
    sources = await desktopCapturer.getSources(
      types: [SourceType.Screen, SourceType.Window],
      thumbnailSize: ThumbnailSize(240, 135),
    );
  } on Object {
    return null;
  }
  if (!context.mounted) return null;
  return showDialog<String>(
    context: context,
    animationStyle: AnimationStyle.noAnimation,
    builder: (context) {
      final t = L10n.of(context);
      final c = ChurrColors.of(context);
      Widget group(String title, SourceType type) {
        final list = sources.where((s) => s.type == type).toList();
        if (list.isEmpty) return const SizedBox.shrink();
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Text(title, style: ChurrType.title(c.ink, size: 14)),
            ),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final s in list)
                  InkWell(
                    key: Key('screen-${s.id}'),
                    onTap: () => Navigator.of(context).pop(s.id),
                    child: Container(
                      width: 180,
                      color: c.raised,
                      padding: const EdgeInsets.all(6),
                      child: Column(
                        children: [
                          AspectRatio(
                            aspectRatio: 16 / 9,
                            child: s.thumbnail == null
                                ? ColoredBox(color: c.panel)
                                : Image.memory(
                                    s.thumbnail!,
                                    fit: BoxFit.contain,
                                  ),
                          ),
                          const SizedBox(height: 4),
                          Text(
                            s.name,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(color: c.ink, fontSize: 12),
                          ),
                        ],
                      ),
                    ),
                  ),
              ],
            ),
          ],
        );
      }

      return AlertDialog(
        title: Text(t.callPickScreen),
        content: SizedBox(
          width: 600,
          child: SingleChildScrollView(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                group(t.callScreens, SourceType.Screen),
                group(t.callWindows, SourceType.Window),
              ],
            ),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: Text(t.cancel),
          ),
        ],
      );
    },
  );
}

/// Choosing the microphone, camera and speaker (desktop).
Future<void> showDeviceChoice(BuildContext context, WebRtcMedia media) async {
  final List<MediaDeviceInfo> devices;
  try {
    devices = await navigator.mediaDevices.enumerateDevices();
  } on Object {
    return;
  }
  if (!context.mounted) return;
  await showDialog<void>(
    context: context,
    animationStyle: AnimationStyle.noAnimation,
    builder: (context) {
      final t = L10n.of(context);
      Widget pick(
        String label,
        String kind,
        String? current,
        Future<void> Function(String) use,
      ) {
        final list = devices.where((d) => d.kind == kind).toList();
        if (list.isEmpty) return const SizedBox.shrink();
        return Padding(
          padding: const EdgeInsets.only(bottom: 12),
          child: DropdownButtonFormField<String>(
            initialValue: list.any((d) => d.deviceId == current)
                ? current
                : null,
            decoration: InputDecoration(labelText: label),
            items: [
              for (final d in list)
                DropdownMenuItem(
                  value: d.deviceId,
                  child: Text(d.label.isEmpty ? d.deviceId : d.label),
                ),
            ],
            onChanged: (v) {
              if (v != null) use(v);
            },
          ),
        );
      }

      return AlertDialog(
        title: Text(t.callDevices),
        content: SizedBox(
          width: 420,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              pick(
                t.callMicrophoneDevice,
                'audioinput',
                media.microphoneDeviceId,
                media.useMicrophone,
              ),
              pick(
                t.callCameraDevice,
                'videoinput',
                media.cameraDeviceId,
                media.useCamera,
              ),
              pick(t.callOutputDevice, 'audiooutput', null, media.useSpeaker),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: Text(t.close),
          ),
        ],
      );
    },
  );
}
