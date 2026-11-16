import 'dart:async';

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../../../core/api/channels.dart';
import '../../../core/api/errors.dart';
import '../../../core/api/messages.dart';
import '../../../core/api/operations.dart';
import '../../../core/commands/chance_live.dart';
import '../../../core/commands/commands.dart';
import '../../../core/commands/decks.dart';
import '../../../core/conversation/large.dart' show isAnnouncement;
import '../../../core/ids.dart';
import '../../../core/models/account.dart';
import '../../../core/models/conversation.dart';
import '../../../core/models/personal.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/device.dart' as device;
import '../../../state/app_state.dart';
import '../../../state/conversation_intents.dart';
import '../../../state/server_state.dart';
import '../../message/markdown_view.dart';
import '../../scopes.dart';
import '../account/person_profile.dart';
import '../channels/browse_screen.dart';
import '../channels/channel_settings_screen.dart';
import 'command_dialogs.dart';
import 'command_handlers.dart';

/// Registers the built-in commands of rulings R67 and R68 with the
/// composer. Safe to call more than once; the shell calls it with the forms
/// builder's. Each makes the ordinary requests a button elsewhere would
/// make, so every rule is the server's and a refusal is shown in the
/// server's words.
void registerChatCommands() {
  for (final name in builtinCommands.keys) {
    if (formCommandNames.contains(name)) continue;
    if (BuiltinCommands.handlerFor(name) != null) continue;
    BuiltinCommands.register(name, runChatCommand);
  }
}

/// What the person is told when a command is refused before any request:
/// its error code, or that it is not available in a sealed chat.
String commandProblem(L10n t, Map<String, Object?> c) {
  final name = c['name'] as String;
  return switch (c['error']) {
    'needs_text' => t.cmdNeedsText(name),
    'takes_nothing' => t.cmdTakesNothing(name),
    'needs_options' => t.cmdNeedsOptions(name),
    'too_many_options' => t.cmdTooManyOptions(name),
    'empty_option' => t.cmdEmptyOption(name),
    'bad_draw' => t.cmdBadDraw,
    'unknown_deck' => t.cmdUnknownDeck(cardDecks.keys.join(', ')),
    'too_many_cards' => t.cmdTooManyCards,
    'unknown_spread' => t.cmdUnknownSpread(tarotSpreads.keys.join(', ')),
    'bad_count' => t.cmdBadCount(cardDecks['mahjong']!.cards.length),
    'needs_channel' => t.cmdNeedsChannel(name),
    'needs_person' => t.cmdNeedsPerson(name),
    'bad_person' => t.cmdBadPerson(name),
    'bad_duration' => t.cmdBadDuration,
    'bad_broadcast_time' => t.cmdBadBroadcastTime,
    _ => t.cmdNotHere(name),
  };
}

/// The words for a command typed where it does not work: in a sealed chat,
/// which the server cannot write into, or which has no such thing.
String notInSealed(L10n t, String name) => chanceCommands.contains(name)
    ? t.cmdNotInSealedChance(name)
    : t.cmdNotInSealed(name);

class _Said implements Exception {
  _Said(this.text);
  final String text;
}

void _toast(BuildContext context, String text) {
  ScaffoldMessenger.maybeOf(context)
      ?.showSnackBar(SnackBar(content: Text(text)));
}

/// Carries out one command. Returns true when it was carried out, so the
/// composer clears.
Future<bool> runChatCommand(BuildContext context, CommandRequest r) async {
  final t = L10n.of(context);
  final c = r.parsed ?? parseCommand('/${r.name} ${r.args}');
  if (c == null) return false;
  if (c['error'] != null) {
    _toast(context, commandProblem(t, c));
    return false;
  }
  final app = AppScope.read(context);
  if (c['name'] == 'help') {
    // The list of commands; the one chosen goes in the message box, which
    // keeps it, so the composer is not cleared (ruling R74).
    final chosen = await showHelpDialog(
      context,
      r.server,
      r.conversationId,
      query: c['text'] as String? ?? '',
    );
    if (chosen == null || r.insert == null) return true;
    r.insert!('/$chosen ');
    return false;
  }
  try {
    final said = await _carryOut(context, t, app, r, c);
    if (said != null && context.mounted) _toast(context, said);
    return true;
  } on _Said catch (e) {
    if (context.mounted) _toast(context, e.text);
  } on ApiException catch (e) {
    if (context.mounted) _toast(context, e.message);
  }
  return false;
}

/// Finds the account a typed `@name` or `@name@server` means.
Future<Account> _person(ServerState s, L10n t, String typed) async {
  final at = typed.indexOf('@');
  final username = at < 0 ? typed : typed.substring(0, at);
  final server = at < 0 ? null : typed.substring(at + 1);
  bool match(Account a) =>
      a.username == username && a.serverName == server && a.removedAt == null;
  for (final a in s.accounts.values) {
    if (match(a)) return a;
  }
  final j = await s.api.call(
    Ops.listAccounts,
    query: {'q': username, 'limit': 20},
  );
  for (final raw in (j['accounts'] as List? ?? const [])) {
    final a = Account.fromJson((raw as Map).cast<String, dynamic>());
    if (match(a)) return a;
  }
  throw _Said(t.cmdNoSuchPerson(typed));
}

/// Finds the conversation a typed channel means: a link from the picker,
/// or a name, which must be one channel's.
Conversation _channel(ServerState s, L10n t, Map ref) {
  final id = ref['id'] as String?;
  if (id != null) {
    return s.conversation(id) ?? (throw _Said(t.cmdNoSuchChannel(id)));
  }
  final name = (ref['name'] as String).toLowerCase();
  final found = [
    for (final row in s.conversations)
      if ((row.conversation.name ?? '').toLowerCase() == name) row.conversation,
  ];
  if (found.length > 1) {
    throw _Said(t.cmdSeveralChannels(ref['name'] as String));
  }
  if (found.isEmpty) {
    // A public channel the person is not in is found by browsing.
    throw _Said(t.cmdNoSuchChannel(ref['name'] as String));
  }
  return found.first;
}

Future<String?> _carryOut(
  BuildContext context,
  L10n t,
  AppState app,
  CommandRequest r,
  Map<String, Object?> c,
) async {
  final s = r.server;
  final here = r.conversationId;
  final name = c['name'] as String;
  final conv = s.conversation(here);
  if ((conv?.isSealed ?? false) && !(builtinCommands[name]?.$2 ?? false)) {
    throw _Said(notInSealed(t, name));
  }
  final chance = chanceRequest(c);
  if (chance != null) {
    final m = await s.api.postMessageChance(here, chance, parentId: r.parentId);
    ChanceLive.arrived(m.id, m.createdAt);
    if (await s.db.message(m.id) == null) await s.db.upsertMessages([m]);
    return null;
  }
  String who(Account a) => a.name;
  switch (name) {
    case 'join':
      final to = _channel(s, t, c['channel']! as Map);
      if (to.membership == null) {
        await s.api.call(Ops.joinConversation, path: {'id': to.id});
      }
      app.openConversation(OpenTarget(s.localId, to.id));
      return t.cmdJoined(to.name ?? '');
    case 'leave':
      final from = c['channel'] == null
          ? conv
          : _channel(s, t, c['channel']! as Map);
      if (from == null) return null;
      await s.api.call(Ops.leaveConversation, path: {'id': from.id});
      return t.cmdLeft(from.name ?? '');
    case 'msg':
      final a = await _person(s, t, c['person']! as String);
      final j = await s.api.call(
        Ops.openChat,
        body: {
          'account_ids': [a.id],
        },
      );
      final id = (j['conversation'] as Map)['id'] as String;
      final text = c['text'] as String?;
      if (text != null) {
        await s.api.call(
          Ops.postMessage,
          path: {'id': id},
          body: {'client_id': newUuid(), 'text': text},
        );
      }
      app.openConversation(OpenTarget(s.localId, id));
      return null;
    case 'topic':
      final text = c['text'] as String?;
      if (text == null) {
        final topic = conv?.topic ?? '';
        return topic.isEmpty ? t.cmdNoTopic : t.cmdTopicIs(topic);
      }
      await s.api.call(
        Ops.updateConversation,
        path: {'id': here},
        body: {'topic': text},
      );
      return t.cmdTopicSet;
    case 'invite':
      final a = await _person(s, t, c['person']! as String);
      final to = c['channel'] == null
          ? here
          : _channel(s, t, c['channel']! as Map).id;
      await s.api.call(
        Ops.addMembers,
        path: {'id': to},
        body: {
          'account_ids': [a.id],
        },
      );
      return t.cmdInvited(who(a));
    case 'kick':
      final a = await _person(s, t, c['person']! as String);
      await s.api.call(
        Ops.removeMember,
        path: {'id': here, 'account_id': a.id},
      );
      return t.cmdKicked(who(a));
    case 'ban':
      // The channel ban (ruling R72): out of this channel, and kept out.
      final a = await _person(s, t, c['person']! as String);
      await s.api.banFromChannel(here, a.id, reason: c['text'] as String?);
      return t.cmdBanned(who(a));
    case 'unban':
      final a = await _person(s, t, c['person']! as String);
      await s.api.liftChannelBan(here, a.id);
      return t.cmdUnbanned(who(a));
    case serverUnbanCommand:
      // An exiled account is removed, so it is found among the bans.
      final typed = c['person']! as String;
      final j = await s.api.call(Ops.listBans, query: {'limit': 200});
      for (final raw in (j['bans'] as List? ?? const [])) {
        final b = (raw as Map).cast<String, dynamic>();
        final id = b['account_id'] as String?;
        if (b['lifted_at'] != null || id == null) continue;
        var username = s.accounts[id]?.username;
        if (username == null) {
          try {
            final got = await s.api.call(Ops.getAccount, path: {'id': id});
            username = (got['account'] as Map?)?['username'] as String?;
          } on ApiException {
            continue;
          }
        }
        if (username == typed) {
          await s.api.call(Ops.liftBan, path: {'id': b['id'] as String});
          return t.cmdUnexiled(typed);
        }
      }
      throw _Said(t.cmdNotExiled(typed));
    case serverBanCommand:
      // The server-wide ban, after asking, since it is far more than this
      // channel.
      final a = await _person(s, t, c['person']! as String);
      if (!context.mounted) return null;
      final yes = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(t.cmdExileTitle(who(a))),
          content: Text(t.cmdExileBody(who(a))),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(context).pop(false),
              child: Text(t.cancel),
            ),
            FilledButton(
              onPressed: () => Navigator.of(context).pop(true),
              child: Text(t.cmdExileYes),
            ),
          ],
        ),
      );
      if (yes != true) return null;
      await s.api.call(
        Ops.createBan,
        body: {'account_id': a.id, 'reason': ?c['text']},
      );
      return t.cmdExiled(who(a));
    case 'mute':
      final a = await _person(s, t, c['person']! as String);
      final secs = c['seconds']! as int;
      await s.api.call(
        Ops.timeOutMember,
        path: {'id': here},
        body: {'account_id': a.id, 'seconds': secs},
      );
      return t.cmdMuted(who(a), secs ~/ 60);
    case 'op' || 'deop':
      final a = await _person(s, t, c['person']! as String);
      await s.api.call(
        Ops.setMemberLevel,
        path: {'id': here, 'account_id': a.id},
        body: {'level': name == 'op' ? 'manager' : 'member'},
      );
      return name == 'op' ? t.cmdOpped(who(a)) : t.cmdDeopped(who(a));
    case 'ignore':
      final a = await _person(s, t, c['person']! as String);
      await s.api.call(Ops.blockAccount, path: {'account_id': a.id});
      return t.cmdIgnored(who(a));
    case 'unignore':
      final a = await _person(s, t, c['person']! as String);
      await s.api.call(Ops.unblockAccount, path: {'account_id': a.id});
      return t.cmdUnignored(who(a));
    case 'whois':
      final a = await _person(s, t, c['person']! as String);
      if (context.mounted) unawaited(showPersonProfile(context, s, a.id));
      return null;
    case 'nick':
      final j = await s.api.call(
        Ops.updateMe,
        body: {'display_name': c['text']},
      );
      return t.cmdNick(
        ((j['account'] as Map?)?['display_name'] as String?) ?? '${c['text']}',
      );
    case 'list':
      if (context.mounted) unawaited(openBrowse(context, server: s));
      return null;
    case 'names':
      if (context.mounted) {
        unawaited(openChannelSettings(context, s, here, tab: 1));
      }
      return null;
    case 'away':
      final secs = c['seconds'] as int?;
      await s.api.call(Ops.setAway, body: {'duration_seconds': ?secs});
      return t.cmdAway;
    case 'back':
      await s.api.call(Ops.clearAway);
      return t.cmdBack;
    case 'dnd':
      if (c['off'] == true) {
        await s.api.call(Ops.clearDnd);
        return t.cmdDndOff;
      }
      final secs = c['seconds'] as int?;
      await s.api.call(Ops.setDnd, body: {'duration_seconds': ?secs});
      return t.cmdDndOn;
    // Ruling R74.
    case 'query':
      // The unsealed chat with them, opened; nothing is sent.
      final a = await _person(s, t, c['person']! as String);
      final j = await s.api.call(
        Ops.openChat,
        body: {
          'account_ids': [a.id],
        },
      );
      app.openConversation(
        OpenTarget(s.localId, (j['conversation'] as Map)['id'] as String),
      );
      return null;
    case 'clear':
      if (conv != null) await s.sync.markRead(here, conv.lastSeq);
      ConversationIntents.instance.toNewest(app, OpenTarget(s.localId, here));
      return null;
    case 'close':
      // Hidden until something new arrives, so read first; the
      // conversation closes too.
      if (conv != null) await s.sync.markRead(here, conv.lastSeq);
      await s.api.call(
        Ops.updateMembership,
        path: {'id': here},
        body: {'show': 'activity'},
      );
      app.openConversation(null);
      return t.cmdClosed;
    case 'knock':
      // A knock channel is chosen, never typed (ruling R79).
      if (context.mounted) await showKnockPicker(context, s);
      return null;
    case 'banlist':
      if (context.mounted) await showBanListDialog(context, s, here);
      return null;
    case 'voice' || 'devoice':
      // Announcement channels (ruling R66): may_post, set by a manager.
      if (!isAnnouncement(conv)) throw _Said(t.cmdNotAnnouncement);
      final a = await _person(s, t, c['person']! as String);
      await s.api.call(
        Ops.setMemberMayPost,
        path: {'id': here, 'account_id': a.id},
        body: {'may_post': name == 'voice'},
      );
      return name == 'voice' ? t.cmdVoiced(who(a)) : t.cmdDevoiced(who(a));
    case 'whoami':
      final me = s.me?.id;
      if (me != null && context.mounted) {
        unawaited(showPersonProfile(context, s, me));
      }
      return null;
    case 'time':
      final a = await _person(s, t, c['person']! as String);
      final zone = a.timeZone;
      final local = zone == null ? null : device.timeIn(zone);
      if (zone == null || local == null) return t.cmdTimeUnknown(who(a));
      final locale = context.mounted
          ? Localizations.localeOf(context).toString()
          : null;
      return t.cmdTime(DateFormat.jm(locale).format(local), who(a), zone);
    case 'invisible':
      // Churr has no appear offline; appearing away is the nearest.
      await s.api.call(Ops.setAway, body: const {});
      return t.cmdInvisible;
    case 'motd':
      await s.refreshInfo();
      if (!context.mounted) return null;
      await _showMotd(context, t, s);
      return null;
    case 'admin':
      final names = <String>[];
      for (final role in ['owner', 'admin']) {
        final j = await s.api.call(
          Ops.listAccounts,
          query: {'role': role, 'limit': 100},
        );
        for (final raw in (j['accounts'] as List? ?? const [])) {
          final a = Account.fromJson((raw as Map).cast<String, dynamic>());
          names.add(role == 'owner' ? t.cmdOwnerMark(a.name) : a.name);
        }
      }
      return t.cmdAdmins(names.join(', '));
    case 'version':
      await s.refreshInfo();
      final info = s.info;
      return t.cmdVersion(info?.communityName ?? s.name, info?.version ?? '?');
    case 'ping':
      // The delay the heartbeat measures, taken once more now.
      final d = await s.live.measureDelay() ?? s.live.lastDelay;
      return d == null ? t.cmdPingNone : t.cmdPing(d.inMilliseconds);
    case 'lusers':
      return [
        if (conv != null) t.cmdLusersMembers(conv.memberCount),
        if (s.onlineIn[here] != null) t.cmdLusersOnlineHere(s.onlineIn[here]!),
        if (s.onlineCount != null) t.cmdLusersOnlineServer(s.onlineCount!),
      ].join(' ');
    case 'broadcast':
      if (c['clear'] == true) {
        await s.api.call(Ops.clearBroadcast);
        return t.cmdBroadcastCleared;
      }
      final secs = c['seconds']! as int;
      await s.api.call(
        Ops.sendBroadcast,
        body: {'text': c['text'], 'seconds': secs},
      );
      return t.cmdBroadcastSent(secs ~/ 60);
    case 'quiet':
      final off = c['off'] == true;
      final secs = c['seconds'] as int? ?? 3600;
      await s.api.call(
        Ops.updateMembership,
        path: {'id': here},
        body: {
          'muted_until': off
              ? null
              : DateTime.now()
                    .toUtc()
                    .add(Duration(seconds: secs))
                    .toIso8601String(),
        },
      );
      return off ? t.cmdQuietOff : t.cmdQuietOn(secs ~/ 60);
  }
  return null;
}

/// The server's welcome text (`/motd`). Churr keeps no separate rules, so
/// the dialog says the welcome text is where they are written.
Future<void> _showMotd(BuildContext context, L10n t, ServerState s) {
  final text = s.info?.welcome ?? '';
  return showDialog<void>(
    context: context,
    animationStyle: AnimationStyle.noAnimation,
    builder: (ctx) => AlertDialog(
      title: Text(t.cmdMotdTitle(s.info?.communityName ?? s.name)),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (text.trim().isEmpty)
              Text(t.cmdMotdNone)
            else
              MarkdownView(text: text),
            const SizedBox(height: 12),
            Text(t.cmdMotdNoRules, style: const TextStyle(fontSize: 13)),
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(ctx), child: Text(t.close)),
      ],
    ),
  );
}
