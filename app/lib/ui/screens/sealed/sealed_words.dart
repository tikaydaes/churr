import '../../../core/sealed/sealed.dart'
    show
        noticeDeviceAdded,
        noticeDeviceRemoved,
        noticeKeyChange,
        noticeReset,
        noticeListRefused,
        noticeOutOfStep;
import '../../../core/sealed_client/session.dart';
import '../../../l10n/app_localizations.dart';

/// The kind of device a list entry is (docs/SEALED.md section 4.3): the
/// other person sees what kind of device, not its name. Platform 6 is a
/// browser and is called one wherever the list is shown.
String sealedPlatformWords(L10n t, int platform) => switch (platform) {
  1 => t.sealedPlatformAndroid,
  2 => t.sealedPlatformIos,
  3 => t.sealedPlatformMac,
  4 => t.sealedPlatformWindows,
  5 => t.sealedPlatformLinux,
  _ => t.sealedPlatformBrowser,
};

/// The same, from the API's platform name.
String sealedPlatformNameWords(L10n t, String name) => sealedPlatformWords(
  t,
  const {'android': 1, 'ios': 2, 'macos': 3, 'windows': 4, 'linux': 5}[name] ??
      6,
);

/// A notice's words (docs/SEALED.md section 4.4): about the other person
/// ([name]), or worded for the reader's own account when [mine].
String noticeWords(L10n t, SealedNotice n, String name, {required bool mine}) {
  final kind = sealedPlatformWords(t, n.platform);
  return switch (n.kind) {
    noticeDeviceAdded =>
      mine ? t.sealedNoticeAddedMine(kind) : t.sealedNoticeAdded(name, kind),
    noticeDeviceRemoved =>
      mine ? t.sealedNoticeRemovedMine : t.sealedNoticeRemoved(name),
    noticeKeyChange => mine ? t.sealedNoticeKeyMine : t.sealedNoticeKey(name),
    noticeReset => mine ? t.sealedNoticeResetMine : t.sealedReset(name),
    noticeListRefused => t.sealedNoticeListRefused,
    noticeOutOfStep => t.sealedNoticeOutOfStep,
    _ => n.kind,
  };
}
