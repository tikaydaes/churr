import 'dart:async';

import 'api/client.dart';
import 'api/conversations.dart';
import 'api/errors.dart';
import 'api/me.dart';
import 'api/messages.dart';
import 'account_sync.dart';
import 'commands/chance_live.dart';
import 'conversation/broad_mentions.dart';
import 'db/cache_db.dart';
import 'ids.dart';
import 'live.dart';
import 'models/account.dart';
import 'models/conversation.dart';
import 'models/json.dart';
import 'models/message.dart';
import 'models/server.dart';

/// Keeps one server's `cache.db` in step with the server
/// (docs/CLIENTS.md section 7, docs/EVENTS.md sections 2 and 3).
///
/// Everything that came from the server reaches the screens through
/// here: events, replayed or live, are applied to the database, and the
/// screens watch the database. The rules that make this safe:
///
/// - **By sequence number, once.** A stored event is applied only when
///   its `seq` is exactly one more than the conversation's `synced_seq`,
///   in one transaction with the new `synced_seq`. An event at or below
///   it has been applied already and is skipped, so replays and
///   duplicates are harmless.
/// - **Gaps.** A `seq` further ahead means something was missed: the
///   missing range is fetched with `listEvents` and applied first.
/// - **Reload.** When the server cannot replay, the cached messages are
///   dropped and the conversation starts again from its newest page.
/// - **Account state is fetched, not replayed.** Events without a `seq`
///   are applied as they come, and after every `ready` the account, the
///   conversation list (read positions, modes, counts) and the small
///   lists are fetched again.
/// - **Sends are optimistic.** A message is written to the outbox with
///   a `client_id`, then posted; the answer or the server's event,
///   whichever comes first, replaces the outbox row with the message.
class SyncEngine implements LiveDelegate {
  SyncEngine({required this.db, required this.api, DateTime Function()? now})
    : _now = now ?? DateTime.now;

  final CacheDb db;
  final ApiClient api;
  final DateTime Function() _now;

  /// `interaction.responded` frames: a bot answering something the
  /// person did (EVENTS.md section 10). Not stored; the forms screens
  /// listen here to show a bot's dialog.
  final _interactions = StreamController<Json>.broadcast();
  Stream<Json> get interactions => _interactions.stream;

  /// Whether the live connection has reached `ready` since it last
  /// connected. Sends are held until it has.
  bool _ready = false;
  bool _flushing = false;

  /// The reader's own account id, needed to tell "I left" from "someone
  /// left". Read from the database, so it is known before any request.
  String? _myId;

  /// Page size for history. Small enough to draw at once, large enough
  /// that scrolling back is not a request per screenful.
  static const pageSize = 50;

  final _local = StreamController<Json>.broadcast();

  /// What is not stored but a screen may want to see as it happens:
  /// `typing.started`, `server.updated`, `theme.published`,
  /// `theme.unpublished`, `stream.channel_changed` and
  /// `stream.move_changed` events as the server sent them, and
  /// `{"type": "local.sent", "client_id", "message", "cannot_read_ids"}`
  /// when a post from the outbox is confirmed (the conversation screen),
  /// and `{"type": "local.notify", "conversation_id", "message"}` for a
  /// message event marked `notify` (lib/state/notify_center.dart). Also
  /// the sealed-chat events (`sealed.epoch_started`,
  /// `device_list.changed`, `unlock_request.*`, `backup.changed`,
  /// `device.retired`) for lib/state/sealed_state.dart,
  /// `channel_ban.created` and `channel_ban.lifted`, and `server.broadcast`
  /// (also made from the status summary at connect), `knock.created` and
  /// `knock.answered` (ruling R74).
  Stream<Json> get localEvents => _local.stream;

  Future<String?> _me() async => _myId ??= (await db.me())?.id;

  // ---- LiveDelegate ----

  @override
  Future<Map<String, int>> positions() {
    _ready = false;
    return db.positions();
  }

  @override
  Future<void> onEvent(Json event, {required bool replay}) => applyEvent(event);

  @override
  Future<void> onReload(Json frame) async {
    final id = frame['conversation_id'] as String?;
    if (id == null) return;
    if (frame['reason'] == 'too_far_behind') {
      await _reload(id, asInt(frame['last_seq']));
    } else {
      // `no_longer_member`, `unknown`, and any reason this build does
      // not know: the conversation is not ours to keep.
      await db.deleteConversation(id);
    }
  }

  @override
  Future<void> onReady(List<String> newConversationIds) async {
    _ready = true;
    await refreshAccountState(adopt: newConversationIds.toSet());
    await flushOutbox();
  }

  // ---- account state ----

  /// Fetches what is not replayed: the account, every conversation with
  /// its membership, the totals and the small lists.
  ///
  /// A conversation the cache does not hold yet (one of [adopt], or
  /// simply missing) is taken in with `synced_seq` at its `last_seq`: the
  /// device has nothing of it to catch up on, and its messages are
  /// fetched when it is first opened. A cached conversation that is no
  /// longer listed has been left, archived or deleted, and is dropped.
  Future<void> refreshAccountState({Set<String> adopt = const {}}) async {
    final me = await api.getMe();
    _myId = me.id;
    await db.putMe(me);

    final listed = await api.listConversations();
    final held = await db.positions();
    await db.transaction(() async {
      for (final c in listed) {
        await db.upsertConversation(
          c,
          syncedSeq: held.containsKey(c.id) ? null : c.lastSeq,
        );
      }
      final ids = {for (final c in listed) c.id};
      for (final id in held.keys) {
        if (!ids.contains(id)) await db.deleteConversation(id);
      }
    });

    // A sub-channel whose parent the account is not in: the server names
    // the parent only when the caller can see it, so it can be fetched.
    final ids = {for (final c in listed) c.id};
    final outside = <Conversation>[];
    for (final pid in {for (final c in listed) ?c.parentId}) {
      if (ids.contains(pid)) continue;
      try {
        outside.add(await api.getConversation(pid));
      } on ApiException {
        // Gone or no longer visible: the sub-channel is drawn top-level.
      }
    }
    await db.putOutsideParents(outside);

    final summary = await api.getStatusSummary();
    await db.putStatusSummary(summary);
    _broadcastFrom(summary);
    await db.replaceSections(await api.listSections());
    await db.replaceGroups(await api.listGroups());
    await db.replaceEmoji(await api.listEmoji());
    // Every account the caller can see, whole. Fine for a community of
    // hundreds; a larger one should fetch accounts as they are met.
    await db.upsertAccounts(await api.listAccounts());
  }

  /// Sets badges early on start, before the socket is open.
  Future<void> refreshStatusSummary() async {
    try {
      final summary = await api.getStatusSummary();
      await db.putStatusSummary(summary);
      _broadcastFrom(summary);
    } on ApiException {
      // Badges from the last run stay until the connection is up.
    }
  }

  /// The server broadcast showing when the summary was fetched (ruling
  /// R74), passed on as the live event would be, so one path shows both.
  void _broadcastFrom(StatusSummary s) =>
      _local.add({'type': 'server.broadcast', 'broadcast': ?s.broadcast});

  // ---- applying events ----

  /// Applies one event. Safe to call with the same event twice.
  Future<void> applyEvent(Json e) async {
    final type = e['type'] as String? ?? '';
    final conversationId = e['conversation_id'] as String?;
    if (conversationId != null && e['seq'] != null) {
      await _applyStored(conversationId, asInt(e['seq']), type, e);
    } else {
      await _applyUnstored(type, e);
    }
  }

  Future<void> _applyStored(String id, int seq, String type, Json e) async {
    var row = await db.conversation(id);
    if (row == null) {
      // The first event from a conversation this device has not seen.
      // For the account that joined, `member.joined` carries the whole
      // conversation; otherwise it is fetched.
      final carried = object(e['conversation']);
      final c = carried != null
          ? Conversation.fromJson(carried)
          : await api.getConversation(id);
      await db.upsertConversation(
        c,
        syncedSeq: carried != null ? seq : c.lastSeq,
      );
      if (carried != null) return;
      row = await db.conversation(id);
      if (row == null) return;
    }
    if (seq <= row.syncedSeq) return; // Applied already.
    if (seq > row.syncedSeq + 1) {
      await _fillGap(id, row.syncedSeq, seq - 1);
      row = await db.conversation(id);
      if (row == null || seq <= row.syncedSeq) return;
    }
    await db.transaction(() async {
      await _applyOne(id, seq, type, e);
      // The conversation may have been dropped by the event itself.
      await db.setSyncedSeq(id, seq);
    });
  }

  /// Fetches and applies the stored events in (`after`, `until`].
  ///
  /// In a conversation shared with other servers some numbers never
  /// arrive here, so a gap may stay a gap: whatever the server returns
  /// is applied and the position moves on regardless.
  Future<void> _fillGap(String id, int after, int until) async {
    var from = after;
    while (from < until) {
      final page = await api.listEvents(id, after: from, until: until);
      if (page.reload) {
        await _reload(id, until);
        return;
      }
      for (final ev in page.events) {
        final seq = asInt(ev['seq']);
        if (seq <= from) continue;
        await db.transaction(() async {
          await _applyOne(id, seq, ev['type'] as String? ?? '', ev);
          await db.setSyncedSeq(id, seq);
        });
        from = seq;
      }
      if (!page.hasMore) break;
    }
    if (from < until && await db.conversation(id) != null) {
      await db.setSyncedSeq(id, until);
    }
  }

  /// `reload` with `too_far_behind`: drop the cached messages, fetch the
  /// newest page, carry on from [lastSeq].
  Future<void> _reload(String id, int lastSeq) async {
    final page = await api.listMessages(id, limit: pageSize);
    await db.transaction(() async {
      await db.deleteMessages(id);
      await db.upsertMessages(page.messages);
      await db.upsertThreadStates(page.threadStates);
      await db.setHistoryComplete(id, !page.hasMore);
      await db.setSyncedSeq(id, lastSeq);
    });
  }

  Future<void> _applyOne(String id, int seq, String type, Json e) async {
    final message = object(e['message']) == null
        ? null
        : Message.fromJson(object(e['message'])!);
    switch (type) {
      case 'message.created':
        if (message == null) return;
        await db.upsertMessages([message]);
        // A result the server drew spins once when it arrives live.
        if (message.chance != null) {
          ChanceLive.arrived(message.id, message.createdAt);
        }
        // The author's own connections get the `client_id`: this is the
        // message the outbox was waiting for.
        final clientId = e['client_id'] as String?;
        if (clientId != null) await db.removeFromOutbox(clientId);
        if (message.parentId != null) {
          await _replyAdded(message);
        }
        final notify = e['notify'] as String?;
        if (notify == 'notify' || notify == 'badge') {
          await db.addNotification(message, notify!);
        }
        // The pop-up, the sound and the tray (lib/state/notify_center.dart).
        if (notify == 'notify') {
          _local.add({
            'type': 'local.notify',
            'conversation_id': id,
            'message': message.raw,
          });
        }
        await _touchConversation(id, seq, message.createdAt);

      case 'message.edited' ||
          'message.placed' ||
          'message.purged' ||
          'message.history_restored' ||
          'preview.added' ||
          'preview.removed':
        // The event carries the message whole. One outside the cached
        // run is left alone: it will be fresh when it is fetched.
        // `message.placed` changes its `order`: writing it moves it, since
        // every query sorts by `ord`.
        if (message != null && await db.message(message.id) != null) {
          await db.upsertMessages([message]);
        }

      case 'message.deleted':
        if (message == null) return;
        final before = await db.message(message.id);
        if (before == null) return;
        await db.upsertMessages([message]);
        if (message.parentId != null && !before.isDeleted) {
          await _replyRemoved(message.parentId!);
        }

      case 'reaction.added' || 'reaction.removed':
        final m = await db.message(e['message_id'] as String? ?? '');
        final r = object(e['reaction']);
        if (m == null || r == null) return;
        // `reaction` is the message's entry for that emoji after the
        // change, except `me`, which the server fills in for whoever
        // receives the frame.
        final next = Reaction.fromJson(r);
        final list = [
          for (final x in m.reactions)
            if (x.emoji != next.emoji) x,
        ];
        if (next.count > 0) {
          final at = m.reactions.indexWhere((x) => x.emoji == next.emoji);
          list.insert(at < 0 ? list.length : at, next);
        }
        await db.upsertMessages([m.copyWith(reactions: list)]);

      case 'task.changed':
        final m = await db.message(e['message_id'] as String? ?? '');
        final t = object(e['task']);
        if (m == null || t == null) return;
        final next = TaskState.fromJson(t);
        await db.upsertMessages([
          m.copyWith(
            tasks: [
              for (final x in m.tasks) x.id == next.id ? next : x,
              if (m.task(next.id) == null) next,
            ],
          ),
        ]);

      case 'form.answered' ||
          'form.answer_withdrawn' ||
          'form.option_added' ||
          'form.closed':
        final m = await db.message(e['message_id'] as String? ?? '');
        final f = object(e['form']);
        if (m != null && f != null) {
          await db.upsertMessages([m.copyWith(form: f)]);
        }

      case 'pin.added' || 'pin.removed':
        final mid =
            object(e['pin'])?['message_id'] as String? ??
            e['message_id'] as String? ??
            '';
        final m = await db.message(mid);
        if (m != null) {
          await db.upsertMessages([m.copyWith(pinned: type == 'pin.added')]);
        }

      case 'sealed.epoch_started':
        // Sealed chats (lib/state/sealed_state.dart) take it from here.
        _local.add(e);

      case 'thread.moved':
        final parent = object(e['parent']);
        final pointer = object(e['pointer']);
        if (parent == null || pointer == null) return;
        // The replies moved to the new channel; one system reply saying
        // where takes their place.
        await db.deleteReplies(parent['id'] as String? ?? '');
        await db.upsertMessages([
          Message.fromJson(parent),
          Message.fromJson(pointer),
        ]);

      case 'conversation.updated' ||
          'conversation.archived' ||
          'conversation.unarchived':
        final c = object(e['conversation']);
        if (c != null) {
          // The event is the conversation as everyone sees it. What only
          // this reader is told stays as it is held.
          final held = await db.conversation(id);
          await db.upsertConversation(
            Conversation.fromJson(keepOwn(c, held?.conversation)),
          );
        }

      case 'member.joined' || 'channel_group.added':
        final c = object(e['conversation']);
        if (c != null) await db.upsertConversation(Conversation.fromJson(c));

      case 'member.left':
        // The account that left receives this as its last event.
        if (e['account_id'] == await _me()) await db.deleteConversation(id);

      default:
        // An event this build does not know. It still takes its place in
        // the numbering, so the position moves past it.
        break;
    }
  }

  /// A reply arrived: the client updates the parent's thread summary
  /// itself (docs/EVENTS.md, `message.created`).
  Future<void> _replyAdded(Message reply) async {
    final parent = await db.message(reply.parentId!);
    if (parent == null) return;
    final t = parent.thread;
    // A parent fetched over HTTP after the reply was posted already
    // counts it.
    if (t != null && t.lastReplySeq >= reply.seq) return;
    await db.upsertMessages([
      parent.copyWith(
        thread: ThreadSummary(
          replyCount: (t?.replyCount ?? 0) + 1,
          lastReplySeq: reply.seq,
          lastReplyAt: reply.createdAt,
          participantIds: {...?t?.participantIds, reply.authorId}.toList(),
        ),
      ),
    ]);
  }

  Future<void> _replyRemoved(String parentId) async {
    final parent = await db.message(parentId);
    final t = parent?.thread;
    if (parent == null || t == null) return;
    await db.upsertMessages([
      t.replyCount <= 1
          ? parent.copyWith(clearThread: true)
          : parent.copyWith(
              thread: ThreadSummary(
                replyCount: t.replyCount - 1,
                lastReplySeq: t.lastReplySeq,
                lastReplyAt: t.lastReplyAt,
                participantIds: t.participantIds,
              ),
            ),
    ]);
  }

  /// Keeps the conversation's own `last_seq` and `last_message_at`
  /// current, which is what orders chats by newest activity.
  Future<void> _touchConversation(String id, int seq, DateTime at) async {
    final row = await db.conversation(id);
    if (row == null) return;
    await db.upsertConversation(
      Conversation.fromJson({
        ...row.conversation.toJson(),
        'last_seq': seq,
        'last_message_at': formatTime(at),
      }),
    );
  }

  /// `conversation.changed`: something happened in a conversation that is
  /// delivered lazily and is not on screen (EVENTS.md section 4.1). The
  /// list shows it; the cached messages are left as they are, and
  /// `synced_seq` with them, so opening the conversation catches up from
  /// where the cache ends.
  ///
  /// With `event`, the frame carries one event the reader should have at
  /// once. A message that notifies them is announced and, unless it is
  /// their own coming back, not put among the cached messages, where it
  /// would stand apart from the ones before it. Any other event (someone
  /// joined, the channel was renamed) is applied as it is: each is safe
  /// to apply again when the catch-up brings it.
  Future<void> _changed(Json e) async {
    final id = e['conversation_id'] as String?;
    if (id == null) return;
    final ev = object(e['event']);
    if (ev != null) {
      final type = ev['type'] as String? ?? '';
      final seq = asInt(ev['seq']);
      final m = object(ev['message']);
      if (type == 'message.created' && m != null) {
        final message = Message.fromJson(m);
        final clientId = ev['client_id'] as String?;
        if (clientId != null) {
          await db.upsertMessages([message]);
          await db.removeFromOutbox(clientId);
        }
        final notify = ev['notify'] as String?;
        if (notify == 'notify' || notify == 'badge') {
          await db.addNotification(message, notify!);
        }
        if (notify == 'notify') {
          _local.add({
            'type': 'local.notify',
            'conversation_id': id,
            'message': message.raw,
          });
        }
      } else if (await db.conversation(id) != null) {
        await db.transaction(() => _applyOne(id, seq, type, ev));
      }
    }
    final row = await db.conversation(id);
    if (row == null) return;
    var c = row.conversation;
    final last = asInt(e['last_seq']);
    final m = c.membership;
    if (ev == null && m != null) {
      if (e['unread_count'] != null) {
        c = c.withMembership(
          m.copyWith(
            unreadCount: asInt(e['unread_count']),
            mentionCount: asInt(e['mention_count']),
            badgeCount: asInt(e['badge_count']),
          ),
        );
      } else if (last > c.lastSeq && asInt(e['messages']) > 0) {
        // The server did not have the numbers at hand: what came is
        // unread until `badge.changed` or the next refresh says.
        c = c.withMembership(
          m.copyWith(unreadCount: m.unreadCount + asInt(e['messages'])),
        );
      }
      final total = object(e['total']);
      if (total != null) {
        await db.putStatusSummary(StatusSummary.fromJson(total));
      }
    }
    if (last > c.lastSeq) {
      final at = parseTime(e['at']);
      c = Conversation.fromJson({
        ...c.toJson(),
        'last_seq': last,
        if (ev == null && asInt(e['messages']) > 0 && at != null)
          'last_message_at': formatTime(at),
      });
    }
    await db.upsertConversation(c);
  }

  /// Events with no `seq`: account events. Each carries whole values, so
  /// applying one twice changes nothing.
  Future<void> _applyUnstored(String type, Json e) async {
    final id = e['conversation_id'] as String?;
    switch (type) {
      case 'read_position.moved' || 'badge.changed':
        final row = id == null ? null : await db.conversation(id);
        final m = row?.conversation.membership;
        if (row != null && m != null) {
          final next = m.copyWith(
            readSeq: e['read_seq'] == null ? null : asInt(e['read_seq']),
            unreadCount: asInt(e['unread_count']),
            mentionCount: asInt(e['mention_count']),
            badgeCount: asInt(e['badge_count']),
          );
          await db.upsertConversation(row.conversation.withMembership(next));
          if (type == 'read_position.moved') {
            await db.clearNotifications(
              row.conversation.id,
              next.readSeq,
              _now(),
            );
          }
        }
        final total = object(e['total']);
        if (total != null) {
          await db.putStatusSummary(StatusSummary.fromJson(total));
        }

      case 'membership.updated':
        final row = id == null ? null : await db.conversation(id);
        final m = object(e['membership']);
        if (row != null && m != null) {
          await db.upsertConversation(
            row.conversation.withMembership(Membership.fromJson(m)),
          );
        }

      case 'thread_state.changed':
        final s = object(e['thread_state']);
        if (s != null) {
          final state = ThreadState.fromJson(s);
          await db.upsertThreadStates([state]);
          // The notifications list: replies the thread's position passed.
          await db.clearThreadNotifications(
            state.parentId,
            state.readSeq,
            _now(),
          );
        } else if (e['parent_id'] is String) {
          await db.deleteThreadState(e['parent_id'] as String);
        }

      case 'account.updated' || 'account.removed':
        final a = object(e['account']);
        if (a != null) await db.upsertAccounts([Account.fromJson(a)]);

      case 'me.updated':
        final a = object(e['account']);
        if (a != null) await db.putMe(Me.fromJson(a));

      case 'presence.changed':
        final a = await db.account(e['account_id'] as String? ?? '');
        if (a != null) {
          await db.upsertAccounts([
            Account.fromJson({
              ...a.raw,
              'presence': e['presence'],
              'in_call': e['in_call'] == true,
            }),
          ]);
        }

      case 'presence.batch':
        // Several presence.changed objects in one frame, sent when a
        // great many people's presence changes at once (EVENTS.md
        // section 9). They are written together, so screens redraw once.
        final next = <Account>[];
        for (final p in listOf<Json>(e['changes'], (j) => j)) {
          final a = await db.account(p['account_id'] as String? ?? '');
          if (a == null) continue;
          next.add(
            Account.fromJson({
              ...a.raw,
              'presence': p['presence'],
              'in_call': p['in_call'] == true,
            }),
          );
        }
        if (next.isNotEmpty) await db.upsertAccounts(next);

      case 'conversation.changed':
        await _changed(e);

      case 'typing.started' ||
          // The online counts of large conversations and of the server
          // (ServerState keeps them; EVENTS.md section 9).
          'presence.counts' ||
          // Themes and hubs: held by ServerState and the theme screens,
          // not stored (EVENTS.md section 9 and `stream.channel_changed`).
          'server.updated' ||
          'theme.published' ||
          'theme.unpublished' ||
          'stream.channel_changed' ||
          'stream.move_changed' ||
          // The server broadcast and knocks (ruling R74): held by
          // ServerState and the hosts that show them, not stored.
          'server.broadcast' ||
          'knock.created' ||
          'knock.answered':
        _local.add(e);

      case 'message.sent':
        // The link to the other servers acknowledged a message of ours.
        final m = await db.message(e['message_id'] as String? ?? '');
        if (m != null && m.unsent) {
          await db.upsertMessages([m.copyWith(unsent: false)]);
        }

      case 'file.changed':
        // A file from another server arrived here or could not be
        // fetched: the message's entry for it is replaced.
        final m = await db.message(e['message_id'] as String? ?? '');
        final f = object(e['file']);
        if (m != null && f != null) {
          await db.upsertMessages([
            Message.fromJson({
              ...m.raw,
              'files': [
                for (final x in m.raw['files'] as List? ?? const [])
                  x is Map && x['id'] == f['id'] ? f : x,
              ],
            }),
          ]);
        }

      case 'interaction.responded':
        _interactions.add(e);

      case 'channel_ban.created' || 'channel_ban.lifted':
        // A channel ban (ruling R72): the one banned drops the channel at
        // once, even one shown without being a member, and is told
        // (lib/ui/screens/channels/ban_notice.dart); the managers' settings
        // read the list again.
        final ban = object(e['ban']);
        final cid = ban?['conversation_id'] as String?;
        if (type == 'channel_ban.created' &&
            cid != null &&
            ban?['account_id'] == await _me()) {
          await db.deleteConversation(cid);
        }
        _local.add(e);

      case 'status.changed' ||
          'availability.changed' ||
          'dnd.changed' ||
          'bookmark.added' ||
          'bookmark.removed' ||
          'block.added' ||
          'block.removed':
        // The person's own state and lists (lib/core/account_sync.dart).
        await applyAccountEvent(db, type, e, myId: await _me());

      default:
        // Typing, calls, sealed chats and the rest belong to the code
        // that builds those features.
        // Call frames and events go to the calls code
        // (lib/state/call_service.dart) through [localEvents].
        if (type.startsWith('call.')) _local.add(e);
        // Sealed-chat account events go to lib/state/sealed_state.dart.
        if (type == 'device_list.changed' ||
            type.startsWith('unlock_request.') ||
            type == 'backup.changed' ||
            type == 'device.retired') {
          _local.add(e);
        }
    }
  }

  // ---- history ----

  /// Fetches the page of messages older than what is cached (or the
  /// newest page when nothing is) and extends the cached run downwards.
  /// Returns whether there is still more to fetch.
  Future<bool> loadOlder(String conversationId) async {
    final row = await db.conversation(conversationId);
    if (row == null || row.historyComplete) return false;
    final oldest = await db.oldestSeq(conversationId);
    final page = await api.listMessages(
      conversationId,
      before: oldest,
      limit: pageSize,
    );
    await db.transaction(() async {
      await db.upsertMessages(page.messages);
      await db.upsertThreadStates(page.threadStates);
      await db.setHistoryComplete(conversationId, !page.hasMore);
    });
    return page.hasMore;
  }

  // ---- writes ----

  /// Sends a message: outbox first, then the server. Returns the
  /// `client_id`, which names the pending row until it is confirmed.
  ///
  /// [extra] carries `quote` and `file_ids` (see [OutboxItem.extra]).
  Future<String> send(
    String conversationId,
    String text, {
    String? parentId,
    Json? extra,
  }) async {
    final clientId = newUuid(_now());
    await db.addToOutbox(
      OutboxItem(
        clientId: clientId,
        conversationId: conversationId,
        parentId: parentId,
        text: text,
        createdAt: _now(),
        state: 'pending',
        extra: extra,
      ),
    );
    unawaited(flushOutbox());
    return clientId;
  }

  /// Puts a failed message back in the queue.
  Future<void> retry(String clientId) async {
    await db.setOutboxState(clientId, 'pending');
    unawaited(flushOutbox());
  }

  /// Removes a message that was never sent.
  Future<void> discard(String clientId) => db.removeFromOutbox(clientId);

  /// Posts what is waiting, oldest first. Each carries its `client_id`,
  /// so one that had in fact arrived before the connection dropped is
  /// not posted twice. Held until `ready`.
  Future<void> flushOutbox() async {
    if (!_ready || _flushing) return;
    _flushing = true;
    try {
      while (_ready) {
        final waiting = [
          for (final o in await db.outbox())
            if (o.state == 'pending') o,
        ];
        if (waiting.isEmpty) return;
        for (final item in waiting) {
          try {
            var cannotRead = const <String>[];
            var broadRefused = false;
            final m = await api.postMessage(
              item.conversationId,
              clientId: item.clientId,
              text: item.text,
              parentId: item.parentId,
              quote: object(item.extra?['quote']),
              fileIds: strings(item.extra?['file_ids']),
              action: item.extra?['action'] == true,
              timeZone: item.extra?['time_zone'] as String?,
              instantHints: item.extra?['instant_hints'] as List<Object?>?,
              onCannotRead: (ids) => cannotRead = ids,
              onBroadRefused: () => broadRefused = true,
            );
            _local.add({
              'type': 'local.sent',
              'client_id': item.clientId,
              'message': m.raw,
              'cannot_read_ids': cannotRead,
              'broad_mentions_refused': broadRefused,
            });
            await db.transaction(() async {
              await db.removeFromOutbox(item.clientId);
              // The event may have got here first; writing the message
              // again changes nothing.
              await db.upsertMessages([m]);
            });
          } on ApiException catch (err) {
            if (err.isTransient) {
              return; // Stays pending; sent after the next `ready`.
            }
            await db.setOutboxState(
              item.clientId,
              'failed',
              error: err.message,
            );
          } on SignedOutException {
            return;
          }
        }
      }
    } finally {
      _flushing = false;
    }
  }

  /// Moves the read position: the database first, so the badge clears at
  /// once, then the server, whose answer corrects the counts.
  Future<void> markRead(String conversationId, int seq) async {
    final row = await db.conversation(conversationId);
    final m = row?.conversation.membership;
    if (row == null || m == null || seq <= m.readSeq && m.unreadCount == 0) {
      return;
    }
    await db.upsertConversation(
      row.conversation.withMembership(
        m.copyWith(
          readSeq: seq,
          unreadCount: 0,
          mentionCount: 0,
          badgeCount: 0,
        ),
      ),
    );
    await db.clearNotifications(conversationId, seq, _now());
    try {
      final confirmed = await api.setReadPosition(conversationId, seq);
      final now = await db.conversation(conversationId);
      if (now != null) {
        await db.upsertConversation(now.conversation.withMembership(confirmed));
      }
    } on ApiException {
      // The next refresh of the conversation list puts it right.
    }
  }

  /// Changes `mode`, `muted_until`, `show`, `starred` or `thread_view`
  /// for a conversation; one call per change (docs/CLIENTS.md 9.12).
  Future<void> updateMembership(String conversationId, Json changes) async {
    final m = await api.updateMembership(conversationId, changes);
    final row = await db.conversation(conversationId);
    if (row != null) {
      await db.upsertConversation(row.conversation.withMembership(m));
    }
  }

  /// Adds or takes back the reader's reaction. The server's event
  /// carries the new entry to every device, this one included.
  Future<void> toggleReaction(Message m, String emoji) async {
    final mine = m.reactions.any((r) => r.emoji == emoji && r.me);
    if (mine) {
      await api.removeReaction(m.id, emoji);
    } else {
      await api.addReaction(m.id, emoji);
    }
  }

  /// Ticks or unticks a checklist item.
  Future<void> setChecklistItem(Message m, String itemId, bool checked) =>
      api.setChecklistItem(m.id, itemId, checked);
}
