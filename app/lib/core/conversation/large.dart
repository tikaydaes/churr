// Large channels and announcement channels (ruling R66, docs/CLIENTS.md
// section 9.8): what the conversation object says, read in one place.
// Plain Dart: the screens word it.

import '../models/conversation.dart';

/// An announcement channel: only its managers and the members they allow
/// post; the others read and react.
bool isAnnouncement(Conversation? c) => c?.raw['announcement'] == true;

/// In an announcement channel, whether members who may not post may
/// still reply in threads.
bool announcementReplies(Conversation? c) =>
    c?.raw['announcement_replies'] == true;

/// A large conversation (more members than the server's
/// `channels.large_feature_limit`, or an announcement channel): nobody is
/// shown typing, and it shows an online count in place of who is online.
bool isLarge(Conversation? c) => c?.raw['large'] == true;

/// `server`, `on` or `off`: whether the channel is sent in full only to
/// the connections that have it open.
String lazyDelivery(Conversation? c) =>
    c?.raw['lazy_delivery'] as String? ?? 'server';

/// Whether the reader may post in an announcement channel: a manager (or
/// an admin acting as one, which the caller passes as [manager]), or a
/// member a manager has allowed.
bool mayAnnounce(Conversation c, {required bool manager}) =>
    manager || (c.membership?.mayPost ?? false);

/// Why the reader cannot write in [c], for the notice that takes the
/// message box's place; null when they can. [reply] asks about a reply in
/// a thread, which an announcement channel may allow its members.
enum NoPost { notMember, archived, reader, announcement, announcementReplyOnly }

NoPost? cannotPost(
  Conversation c, {
  required bool manager,
  bool reply = false,
}) {
  final m = c.membership;
  if (m == null) return NoPost.notMember;
  if (c.isArchived) return NoPost.archived;
  if (m.level == 'reader') return NoPost.reader;
  if (!isAnnouncement(c) || mayAnnounce(c, manager: manager)) return null;
  if (announcementReplies(c)) {
    return reply ? null : NoPost.announcementReplyOnly;
  }
  return NoPost.announcement;
}

/// Whether the member list is shown to this reader: always, except in an
/// announcement channel, where only its moderators and managers see it
/// ([keeper]).
bool showsMembers(Conversation c, {required bool keeper}) =>
    !isAnnouncement(c) || keeper;

/// The four coarse answers to "when was this person last here"
/// (`Account.last_seen`); the server never sends a time.
enum LastSeen {
  online,
  recently,
  withinWeek,
  longAgo;

  static LastSeen? parse(Object? v) => switch (v) {
    'online' => LastSeen.online,
    'recently' => LastSeen.recently,
    'within_week' => LastSeen.withinWeek,
    'long_ago' => LastSeen.longAgo,
    _ => null,
  };
}
