import 'dart:ui';

import '../core/notify/content.dart';
import '../l10n/app_localizations.dart';

/// The words of a notification, in the reader's language, from the
/// values of `lib/core/notify/content.dart`. Usable without a widget
/// tree, so the push handlers and the foreground service word their
/// notifications the same way.
class NotifyWords {
  const NotifyWords(this.title, this.body, {this.subtitle});
  final String title;
  final String body;

  /// The server's name where the platform has a second line for it.
  final String? subtitle;
}

/// The strings for the device's language (English is the one file).
L10n notifyL10n([Locale? locale]) {
  final l = locale ?? PlatformDispatcher.instance.locale;
  try {
    return lookupL10n(l);
  } on Object {
    return lookupL10n(const Locale('en'));
  }
}

/// Title "Sam in #ops" (or "Sam" in a chat with one person), body the
/// text or a line for what the message holds. With [subtitleForServer]
/// the server goes on its own line; otherwise it ends the title.
NotifyWords wordNotification(
  L10n t,
  NotificationText n, {
  bool subtitleForServer = false,
}) {
  var title = n.channel != null
      ? t.notifyTitleInChannel(n.sender, n.channel!)
      : n.chatTitle != null
      ? t.notifyTitleInChat(n.sender, n.chatTitle!)
      : n.sender;
  if (n.server != null && !subtitleForServer) {
    title = t.notifyTitleOnServer(title, n.server!);
  }
  var body = switch (n.kind) {
    BodyKind.text => n.text,
    BodyKind.file => t.notifySentFile,
    BodyKind.form => t.notifyPostedForm,
    BodyKind.card => t.notifySentCard,
    BodyKind.placeholder => t.notifyNewMessage,
    BodyKind.locked => t.notifyLocked,
    BodyKind.test => t.notifyTestBody,
    BodyKind.request => t.notifyRequestBody,
    BodyKind.groupRequest => t.notifyGroupRequestBody,
    BodyKind.invitation => t.notifyInvitationBody,
    BodyKind.approval => t.notifyApprovalBody(n.text),
    BodyKind.knock => t.notifyKnockBody,
  };
  if (n.kind == BodyKind.test) title = t.notifyTestTitle;
  if (n.more > 0) body = t.notifyBodyWithMore(body, n.more);
  return NotifyWords(
    title,
    body,
    subtitle: subtitleForServer ? n.server : null,
  );
}
