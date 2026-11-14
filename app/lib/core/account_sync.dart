import 'api/account.dart';
import 'api/client.dart';
import 'api/errors.dart';
import 'api/messages.dart';
import 'db/account_tables.dart';
import 'db/cache_db.dart';
import 'models/account.dart';
import 'models/json.dart';
import 'models/message.dart';
import 'models/personal.dart';

/// Applies the account events the account area keeps (docs/EVENTS.md,
/// "Presence and status", "Accounts and groups", "Reading, modes and
/// threads"). Each carries whole values, so applying one twice changes
/// nothing. Called from `SyncEngine._applyUnstored`.
///
/// Status, availability and the do not disturb mark go to the account
/// they are about: the person's own row (`me`) gets them with the detail
/// only they see (`dnd_state`), and the `accounts` row too, since other
/// screens draw the person from there.
Future<void> applyAccountEvent(
  CacheDb db,
  String type,
  Json e, {
  String? myId,
}) async {
  final accountId = e['account_id'] as String?;
  Future<void> patchAccount(Json Function(Json raw) patch) async {
    if (accountId == null) return;
    final a = await db.account(accountId);
    if (a != null) await db.upsertAccounts([ Account.fromJson(patch({...a.raw})), ]);
    if (accountId == myId) {
      final me = await db.me();
      if (me != null) await db.putMe(Me.fromJson(patch({...me.raw})));
    }
  }

  switch (type) {
    case 'status.changed':
      await patchAccount((raw) {
        final s = object(e['status']);
        s == null ? raw.remove('status') : raw['status'] = s;
        return raw;
      });
    case 'availability.changed':
      await patchAccount((raw) {
        final v = object(e['availability']);
        v == null ? raw.remove('availability') : raw['availability'] = v;
        return raw;
      });
    case 'dnd.changed':
      await patchAccount((raw) {
        raw['dnd'] = e['dnd'] == true;
        final detail = object(e['dnd_state']);
        if (detail != null) raw['dnd_state'] = detail;
        return raw;
      });
    case 'bookmark.added':
      final id = e['message_id'] as String?;
      if (id != null) {
        await db.putBookmark(
          id,
          parseTime(e['created_at']) ?? DateTime.now().toUtc(),
          message: await db.message(id),
        );
      }
    case 'bookmark.removed':
      final id = e['message_id'] as String?;
      if (id != null) await db.deleteBookmark(id);
    case 'block.added' || 'block.removed':
      final me = await db.me();
      if (me == null || accountId == null) return;
      final ids = {...me.blockedIds};
      type == 'block.added' ? ids.add(accountId) : ids.remove(accountId);
      await db.putMe(Me.fromJson({...me.raw, 'blocked_ids': ids.toList()}));
  }
}

/// Fetches the person's own lists that have no replay: bookmarks,
/// followed threads (with their parents) and status presets. Run after
/// each `ready` by the account area, and when a screen showing one opens.
Future<void> refreshPersonalLists(ApiClient api, CacheDb db) async {
  try {
    await db.replaceBookmarks(await api.listBookmarks());
  } on ApiException {
    // Kept from the last time.
  }
  try {
    final threads = await api.listThreads(all: true);
    await db.upsertThreadStates([for (final t in threads) t.state]);
    await db.putThreadParents([for (final t in threads) t.parent]);
  } on ApiException {
    // Kept from the last time.
  }
  try {
    await db.replaceStatusPresets(await api.listStatusPresets());
  } on ApiException {
    // Kept from the last time.
  }
}

/// Fills the notifications list from what the server says now, for a
/// server whose events were not seen on this device: after a reload, or
/// one never connected here (docs/CLIENTS.md 8.3). For each conversation
/// with a badge, the messages after its read position; for each followed
/// thread with unread replies, its parent's newest replies. Items already
/// held are left alone.
///
/// Without the server's per-message `notify` decision, an item is
/// written for each message the list rules would include: everything
/// under mode `any`, and under the other modes the messages that mention
/// the person or reply in a thread they follow.
Future<int> backfillNotifications(
  ApiClient api,
  CacheDb db, {
  DateTime? now,
}) async {
  final me = await db.me();
  if (me == null) return 0;
  final groupsIn = {
    for (final g in (await db.groups()).values)
      if (g.isMember) g.id,
  };
  var added = 0;
  for (final row in await db.conversations()) {
    final m = row.conversation.membership;
    if (m == null || m.badgeCount <= 0) continue;
    try {
      final page = await api.listMessages(
        row.conversation.id,
        after: m.readSeq,
        limit: 50,
      );
      final following = {
        for (final s in page.threadStates)
          if (s.state == 'following') s.parentId,
      };
      for (final msg in page.messages) {
        if (msg.authorId == me.id || msg.isSystem || msg.isDeleted) continue;
        final reason = notificationReason(
          msg,
          myId: me.id,
          groupsIn: groupsIn,
          followed: following.contains(msg.parentId),
        );
        final include = switch (m.mode.name) {
          'any' => true,
          _ => reason != NotificationReason.newMessage,
        };
        if (!include) continue;
        await db.addNotification(msg, 'badge', reason: reason.name);
        added++;
      }
    } on ApiException {
      // That conversation is filled on the next try.
    }
  }
  return added;
}

/// Why an item is in the notifications list (docs/CLIENTS.md 9.24).
enum NotificationReason {
  mentionedYou,
  mentionedGroup,
  mentionedChannel,
  threadReply,
  newMessage,
  missedCall,
}

/// Works out why a message is in the list from its entities: a mention
/// of the person by name first, then of one of their groups, then a
/// broadcast, then a reply in a followed thread.
NotificationReason notificationReason(
  Message message, {
  required String myId,
  Set<String> groupsIn = const {},
  bool followed = false,
}) {
  for (final e in message.entities) {
    if (e.type == 'account' && e.accountId == myId) return NotificationReason.mentionedYou;
  }
  for (final e in message.entities) {
    if (e.type == 'group' && groupsIn.contains(e.groupId)) return NotificationReason.mentionedGroup;
  }
  for (final e in message.entities) {
    if (e.type == 'broadcast') return NotificationReason.mentionedChannel;
  }
  // A reply that mentions nobody is here because its thread is followed.
  if (message.parentId != null) return NotificationReason.threadReply;
  return NotificationReason.newMessage;
}
