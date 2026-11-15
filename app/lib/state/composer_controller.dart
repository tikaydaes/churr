import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/api/conversation_ops.dart';
import '../core/commands/commands.dart';
import '../core/api/errors.dart';
import '../core/api/messages.dart';
import '../core/api/operations.dart';
import '../core/conversation/broad_mentions.dart';
import '../core/conversation/composer_text.dart';
import '../core/db/conversation_queries.dart';
import '../core/models/account.dart';
import '../core/models/conversation.dart';
import '../core/models/json.dart';
import '../core/models/message.dart';
import 'chip_text_controller.dart';
import 'draft_files.dart';
import 'form_actions.dart';
import 'message_actions.dart';
import 'server_state.dart';

/// A file chosen for the message being written (docs/CLIENTS.md 9.5).
/// It is uploaded when the message is sent, so its caption goes with it.
class Attachment {
  Attachment({required this.name, required this.bytes, this.poster})
    : localId = _nextId++;
  static int _nextId = 1;

  final int localId;
  final String name;
  final Uint8List bytes;

  /// For a video: one frame the app took, sent as the poster image.
  Uint8List? poster;
  String caption = '';

  /// 0 to 1 while uploading.
  double? progress;
  FileRef? uploaded;
  String? error;

  int get size => bytes.length;
  String get extension {
    final dot = name.lastIndexOf('.');
    return dot < 0 ? '' : name.substring(dot + 1).toLowerCase();
  }

  bool get isImage =>
      const {'png', 'jpg', 'jpeg', 'gif', 'webp'}.contains(extension);
  bool get isVideo => const {'mp4', 'm4v', 'mov', 'webm'}.contains(extension);
}

/// Something the composer offers in its picker after `@`.
class MentionOption {
  const MentionOption({
    required this.insert,
    required this.label,
    this.detail,
    this.account,
    this.notice,
  });

  /// What replaces the typed `@…`, without the `@`.
  final String insert;
  final String label;
  final String? detail;
  final Account? account;

  /// What picking it means: not notified, in do not disturb, back on a
  /// date.
  final String? notice;
}

/// After a message the person just sent mentions someone in do not
/// disturb: the app offers to notify that one person anyway.
class NotifyOffer {
  const NotifyOffer(this.message, this.account);
  final Message message;
  final Account account;
}

/// What sending did.
sealed class SendOutcome {
  const SendOutcome();
}

class Sent extends SendOutcome {
  const Sent();
}

class NothingToSend extends SendOutcome {
  const NothingToSend();
}

/// A built-in command the client carries out itself (`/poll`, `/form`,
/// `/remind`…): the screen hands it to the registered handler.
class RunBuiltin extends SendOutcome {
  const RunBuiltin(this.name, this.args, {this.parsed});
  final String name;
  final String args;

  /// What the one grammar read (lib/core/commands/commands.dart), for the
  /// commands of rulings R67 and R68.
  final Map<String, Object?>? parsed;
}

class SendFailed extends SendOutcome {
  const SendFailed(this.message);
  final String message;
}

/// The controller of one composer: the conversation's, or a thread's
/// ([parentId] set). It keeps the draft, the reply being quoted, the
/// message being edited, the attachments, the commands that can be
/// typed, who else is typing, and the "notify anyway" offers for what
/// was just sent.
class ComposerController extends ChangeNotifier {
  ComposerController(
    this.server,
    this.conversationId, {
    this.parentId,
    this.posterMaker,
  }) : actions = MessageActions(server);

  final ServerState server;
  final String conversationId;
  final String? parentId;
  final MessageActions actions;

  /// Takes one frame of a video as its poster (lib/platform/video_poster.dart).
  final Future<Uint8List?> Function(String name, Uint8List bytes)? posterMaker;

  final text = ChipTextController();

  /// The reply being written quotes this (FEATURES.md 1.1).
  Message? quoted;
  int? quoteStart, quoteEnd;

  /// The message being edited in place, or null.
  Message? editing;
  String _draftBeforeEdit = '';

  final attachments = <Attachment>[];
  List<SlashCommand> commands = const [];
  ServerLimits limits = const ServerLimits();
  List<String> recentEmoji = const [];

  /// Accounts typing here, with when that runs out.
  final typing = <String, DateTime>{};
  final offers = <NotifyOffer>[];

  /// Accounts named in what was just sent that cannot read this
  /// conversation, so will not be notified.
  List<Account> notNotified = const [];

  /// The message just sent held a broad mention the server left as plain
  /// text, because this person may not use one here (ruling R64).
  bool broadRefused = false;
  bool sending = false;
  String? error;

  static const maxAttachments = 10;

  final _subs = <StreamSubscription<Object?>>[];
  final _sentClientIds = <String>{};
  Timer? _draftTimer;
  Timer? _typingTimer;
  DateTime _typingSentAt = DateTime.fromMillisecondsSinceEpoch(0);
  bool _disposed = false;
  bool _loadingDraft = false;

  String get _draftKey => parentId ?? '';
  Conversation? get conversation => server.conversation(conversationId);

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  /// Whether the channel with [id] is one the person can see, so a
  /// `<#id>` is shown as a chip.
  bool canSee(String id) {
    final c = server.conversation(id);
    return c != null && c.isNamed;
  }

  Future<void> start() async {
    _subs.add(
      server.db.watchCommands(conversationId).listen((v) {
        commands = v;
        _changed();
      }),
    );
    _subs.add(
      server.db.watchRecentEmoji().listen((v) {
        recentEmoji = v;
        _changed();
      }),
    );
    _subs.add(server.sync.localEvents.listen(_onLocal));
    limits = await server.db.serverLimits();
    _loadingDraft = true;
    text.setWire(
      await server.db.draft(conversationId, parentId: _draftKey),
      canSee,
    );
    // The draft's files come back with it (lib/state/draft_files.dart).
    if (attachments.isEmpty) await _draftFiles.restore(attachments);
    _loadingDraft = false;
    text.addListener(_onText);
    _changed();
    unawaited(_refreshFromServer());
  }

  /// The commands and the server's limits change rarely; they are
  /// fetched once per opening and kept in the cache.
  Future<void> _refreshFromServer() async {
    try {
      await server.db.putCommands(
        conversationId,
        await server.api.listCommands(conversationId),
      );
    } on Object {
      // The cached list stays.
    }
    try {
      final j = await server.api.call(Ops.getServer, auth: false);
      final l = object(j['limits']);
      if (l != null) {
        await server.db.putServerLimits(l);
        limits = ServerLimits.fromJson(l);
        _changed();
      }
    } on Object {
      // The cached limits stay.
    }
  }

  void _onText() {
    if (_loadingDraft || editing != null) return;
    _draftTimer?.cancel();
    _draftTimer = Timer(const Duration(milliseconds: 400), saveDraft);
    // Say we are typing, at most every five seconds (it lasts eight).
    final now = DateTime.now();
    if (text.text.isNotEmpty &&
        now.difference(_typingSentAt) > const Duration(seconds: 5)) {
      _typingSentAt = now;
      unawaited(
        server.api
            .sendTyping(conversationId, parentId: parentId)
            .then<void>((_) {}, onError: (Object _) {}),
      );
    }
    _changed();
  }

  Future<void> saveDraft() =>
      server.db.putDraft(conversationId, text.wireText, parentId: _draftKey);

  void _onLocal(Map<String, dynamic> e) {
    switch (e['type']) {
      case 'typing.started':
        if (e['conversation_id'] != conversationId) return;
        if ((e['parent_id'] as String?) != parentId) return;
        final who = e['account_id'] as String?;
        if (who == null || who == server.me?.id) return;
        typing[who] = DateTime.now().add(const Duration(seconds: 8));
        _typingTimer ??= Timer.periodic(
          const Duration(seconds: 1),
          (_) => _pruneTyping(),
        );
        _changed();
      case 'local.sent':
        final clientId = e['client_id'] as String?;
        if (clientId == null || !_sentClientIds.remove(clientId)) return;
        final m = Message.fromJson(object(e['message']) ?? const {});
        _offerNotifyAnyway(m);
        notNotified = [
          for (final id in strings(e['cannot_read_ids'])) ?server.accounts[id],
        ];
        broadRefused = e['broad_mentions_refused'] == true;
        _changed();
    }
  }

  void _pruneTyping() {
    final now = DateTime.now();
    final before = typing.length;
    typing.removeWhere((_, until) => until.isBefore(now));
    if (typing.isEmpty) {
      _typingTimer?.cancel();
      _typingTimer = null;
    }
    if (typing.length != before) _changed();
  }

  /// A message from [accountId] ends their typing.
  void messageArrived(String accountId) {
    if (typing.remove(accountId) != null) _changed();
  }

  /// The names of who is typing, for the line under the box.
  String? get typingLine {
    final names = [
      for (final id in typing.keys) server.accounts[id]?.name ?? 'Someone',
    ];
    if (names.isEmpty) return null;
    if (names.length == 1) return '${names.first} is typing…';
    if (names.length == 2) return '${names[0]} and ${names[1]} are typing…';
    return 'Several people are typing…';
  }

  /// Offers notify anyway for each person the message mentions by name,
  /// or each other member of a small chat, who is in do not disturb
  /// (FEATURES.md 3.8). Whether it reaches them is never shown.
  void _offerNotifyAnyway(Message m) {
    final me = server.me?.id;
    final ids = <String>{
      for (final e in m.entities)
        if (e.type == 'account' && e.accountId != null) e.accountId!,
      if (conversation?.isAdHoc ?? false) ...conversation!.memberIds,
    }..remove(me);
    for (final id in ids) {
      final a = server.accounts[id];
      if (a != null && a.dnd && !a.isBot) offers.add(NotifyOffer(m, a));
    }
  }

  Future<void> notifyAnyway(NotifyOffer o) async {
    offers.remove(o);
    _changed();
    try {
      await actions.notifyAnyway(o.message, o.account.id);
    } on ApiException catch (e) {
      error = e.message;
      _changed();
    }
  }

  void dismissOffer(NotifyOffer o) {
    offers.remove(o);
    _changed();
  }

  void dismissNotNotified() {
    notNotified = const [];
    _changed();
  }

  void dismissBroadRefused() {
    broadRefused = false;
    _changed();
  }

  /// Whether the text in the box holds `@channel`, `@here` or `@everyone`
  /// that this person may not use here, so that it will go as plain text
  /// (ruling R64). Not while editing: an edit says nothing beforehand.
  bool get broadGoesPlainNow =>
      editing == null && broadGoesPlain(text.wireText, conversation);

  /// Asked before a new message is sent: true to send. A text with a
  /// broad mention the person may use is first put to the server, and
  /// where it would notify many people, to the person through [confirm].
  Future<bool> broadOK(Future<bool?> Function(int recipients) confirm) async {
    if (editing != null) return true;
    final cmd = typedCommand();
    if (cmd != null && attachments.isEmpty) return true;
    return broadBeforeSend(
      text.wireText.trim(),
      conversation,
      ask: (t) => server.api.checkBroadMentions(conversationId, t),
      confirm: confirm,
    );
  }

  // ---- replying and editing ----

  void setQuote(Message m, {int? start, int? end}) {
    quoted = m;
    quoteStart = start;
    quoteEnd = end;
    _changed();
  }

  /// Dismissing the quote leaves a plain reply in the thread.
  void clearQuote() {
    quoted = null;
    quoteStart = quoteEnd = null;
    _changed();
  }

  void startEdit(Message m) {
    if (editing == null) _draftBeforeEdit = text.wireText;
    editing = m;
    attachments.clear();
    text.setWire(m.text, canSee);
    _changed();
  }

  void cancelEdit() {
    editing = null;
    text.setWire(_draftBeforeEdit, canSee);
    _changed();
  }

  // ---- attachments ----

  /// Adds files chosen, pasted or dropped. Returns what was refused and
  /// why.
  Future<List<String>> addFiles(
    List<(String name, Uint8List bytes)> files,
  ) async {
    final refused = <String>[];
    for (final (name, bytes) in files) {
      if (attachments.length >= maxAttachments) {
        refused.add(
          '$name: at most $maxAttachments files go with one message.',
        );
        continue;
      }
      if (limits.fileSize > 0 && bytes.length > limits.fileSize) {
        refused.add(
          '$name is ${formatBytes(bytes.length)}; this server takes files up to ${formatBytes(limits.fileSize)}.',
        );
        continue;
      }
      final a = Attachment(name: name, bytes: bytes);
      attachments.add(a);
      _changed();
      if (a.isVideo && posterMaker != null) {
        try {
          a.poster = await posterMaker!(name, bytes);
        } on Object {
          // No poster: the player shows a plain placeholder.
        }
        _changed();
      }
    }
    unawaited(_draftFiles.save(attachments));
    return refused;
  }

  void removeAttachment(Attachment a) {
    attachments.remove(a);
    _changed();
    unawaited(_draftFiles.save(attachments));
  }

  void setCaption(Attachment a, String caption) {
    a.caption = caption;
    unawaited(_draftFiles.save(attachments));
  }

  /// The draft's files, kept with its text.
  late final DraftFiles _draftFiles = DraftFiles(
    server,
    conversationId,
    parentId,
  );

  Future<bool> _uploadAll() async {
    for (final a in attachments) {
      if (a.uploaded != null) continue;
      a.error = null;
      a.progress = 0;
      _changed();
      try {
        a.uploaded = await server.api.uploadFile(
          conversationId,
          name: a.name,
          bytes: a.bytes,
          caption: a.caption,
          poster: a.poster,
          onProgress: (sent, total) {
            a.progress = total == 0 ? 1 : sent / total;
            _changed();
          },
        );
        a.progress = null;
      } on ApiException catch (e) {
        a.progress = null;
        a.error = e.code == 'too_large'
            ? 'Too large for this server.'
            : e.message;
        _changed();
        return false;
      }
    }
    return true;
  }

  // ---- sending ----

  /// The command typed, if the text is `/name …` and names one.
  (SlashCommand, String)? typedCommand() {
    final t = text.text;
    if (!t.startsWith('/')) return null;
    final m = RegExp(r'^/([a-z0-9_-]+)(?:\s+([\s\S]*))?$').firstMatch(t);
    if (m == null) return null;
    for (final c in commands) {
      if (c.name == m.group(1)) return (c, text.chips.decode(m.group(2) ?? '').trim());
    }
    // Built-in commands are known even before the list has arrived.
    if (builtinCommands.containsKey(m.group(1))) {
      return (
        SlashCommand({
          'name': m.group(1),
          'kind': 'builtin',
          'description': '',
        }),
        (m.group(2) ?? '').trim(),
      );
    }
    return null;
  }

  Json? get _quoteJson => quoted == null
      ? null
      : {'message_id': quoted!.id, 'start': ?quoteStart, 'end': ?quoteEnd};

  /// Sends what is in the box: an edit, a command, or a new message.
  Future<SendOutcome> send() async {
    if (sending) return const NothingToSend();
    final wire = text.wireText.trim();
    if (editing != null) return _saveEdit(wire);
    final cmd = typedCommand();
    if (cmd != null && attachments.isEmpty) {
      final (c, args) = cmd;
      if (c.kind == 'builtin') {
        final parsed = parseCommand(wire);
        // /me and the text conveniences post their text (ruling R67).
        if (parsed != null &&
            parsed['error'] == null &&
            parsed['post'] is String) {
          return _post(
            parsed['post'] as String,
            action: parsed['action'] == true,
          );
        }
        return RunBuiltin(c.name, args, parsed: parsed);
      }
      return _runCommand(c, args);
    }
    if (wire.isEmpty && attachments.isEmpty) return const NothingToSend();
    return _post(wire);
  }

  /// Posts [wire] as a new message with the attachments and the quote.
  /// [action] marks a /me message (ruling R67).
  Future<SendOutcome> _post(String wire, {bool action = false}) async {
    if (wire.length > limits.messageText) {
      return SendFailed(
        'That is ${wire.length} characters; a message holds up to ${limits.messageText}.',
      );
    }
    sending = true;
    error = null;
    _changed();
    try {
      if (!await _uploadAll()) return const SendFailed('A file could not be uploaded.');
      final files = [for (final a in attachments) ?a.uploaded];
      final extra = <String, dynamic>{
        'quote': ?_quoteJson,
        if (action) 'action': true,
        if (files.isNotEmpty) 'file_ids': [for (final f in files) f.id],
        if (files.isNotEmpty)
          'files': [
            for (final f in files)
              {'id': f.id, 'name': f.name, 'size': f.size, 'kind': f.kind.name},
          ],
      };
      final clientId = await server.sync.send(
        conversationId,
        wire,
        parentId: parentId,
        extra: extra.isEmpty ? null : extra,
      );
      _sentClientIds.add(clientId);
      _clearAfterSend();
      return const Sent();
    } finally {
      sending = false;
      _changed();
    }
  }

  void _clearAfterSend() {
    text.clear();
    attachments.clear();
    unawaited(_draftFiles.clear());
    clearQuote();
    _draftTimer?.cancel();
    unawaited(server.db.putDraft(conversationId, '', parentId: _draftKey));
  }

  Future<SendOutcome> _saveEdit(String wire) async {
    final m = editing!;
    if (wire.isEmpty && m.files.isEmpty) {
      return const SendFailed(
        'An edit cannot leave the message empty. Delete it instead.',
      );
    }
    sending = true;
    _changed();
    try {
      await actions.edit(m, wire);
      editing = null;
      text.setWire(_draftBeforeEdit, canSee);
      return const Sent();
    } on ApiException catch (e) {
      return SendFailed(e.message);
    } finally {
      sending = false;
      _changed();
    }
  }

  Future<SendOutcome> _runCommand(SlashCommand c, String args) async {
    sending = true;
    _changed();
    try {
      final (m, interaction) = await server.api.runCommand(
        conversationId,
        c.name,
        text: args,
        parentId: parentId,
      );
      // The bot's dialog, when it opens one, shows on the device that
      // ran the command.
      if (interaction != null) FormActions.startedHere.add(interaction);
      if (m != null && await server.db.message(m.id) == null) await server.db.upsertMessages([m]);
      _clearAfterSend();
      return const Sent();
    } on ApiException catch (e) {
      return SendFailed(
        e.code == 'unavailable'
            ? 'That bot is not connected right now.'
            : e.message,
      );
    } finally {
      sending = false;
      _changed();
    }
  }

  /// Send later (FEATURES.md 10): the message is posted at [at].
  Future<SendOutcome> schedule(DateTime at) async {
    final wire = text.wireText.trim();
    if (wire.isEmpty && attachments.isEmpty) return const NothingToSend();
    sending = true;
    _changed();
    try {
      if (!await _uploadAll()) return const SendFailed('A file could not be uploaded.');
      await server.api.scheduleMessage(conversationId, {
        'text': wire,
        'parent_id': ?parentId,
        'quote': ?_quoteJson,
        if (attachments.isNotEmpty)
          'file_ids': [for (final a in attachments) a.uploaded!.id],
      }, at);
      _clearAfterSend();
      return const Sent();
    } on ApiException catch (e) {
      return SendFailed(e.message);
    } finally {
      sending = false;
      _changed();
    }
  }

  // ---- pickers ----

  /// The people and groups offered after `@` (9.5): people in the
  /// conversation first, then everyone else, then groups, `@channel`
  /// and `@here`.
  List<MentionOption> mentionOptions(
    String query, {
    Iterable<String> recentAuthors = const [],
  }) {
    final q = foldForMatch(query);
    final c = conversation;
    final inConversation = <String>{...?c?.memberIds, ...recentAuthors};
    bool match(String s) =>
        foldForMatch(s).startsWith(q) || matchesWordStart(s, query);
    final people = [
      for (final a in server.accounts.values)
        if (a.removedAt == null &&
            a.id != server.me?.id &&
            (match(a.username) || match(a.name)))
          a,
    ];
    people.sort((a, b) {
      final x = inConversation.contains(a.id) ? 0 : 1,
          y = inConversation.contains(b.id) ? 0 : 1;
      return x != y
          ? x.compareTo(y)
          : a.name.toLowerCase().compareTo(b.name.toLowerCase());
    });
    final out = <MentionOption>[
      for (final a in people.take(20))
        MentionOption(
          insert: a.serverName == null
              ? a.username
              : '${a.username}@${a.serverName}',
          label: a.name,
          detail: a.serverName == null
              ? '@${a.username}'
              : '@${a.username}@${a.serverName}',
          account: a,
          notice: _mentionNotice(a, c),
        ),
      for (final g in server.groups.values)
        if (match(g.handle))
          MentionOption(
            insert: g.handle,
            label: '@${g.handle}',
            detail: 'group',
          ),
      if (c != null && !c.isAdHoc) ...[
        if (match('channel'))
          const MentionOption(
            insert: 'channel',
            label: '@channel',
            detail: 'everyone in this channel',
          ),
        if (match('here'))
          const MentionOption(
            insert: 'here',
            label: '@here',
            detail: 'everyone here and active',
          ),
      ],
    ];
    return out;
  }

  String? _mentionNotice(Account a, Conversation? c) {
    if (c != null && c.memberIds.isNotEmpty && !c.memberIds.contains(a.id)) {
      return 'Not in this conversation: they will not be notified.';
    }
    final av = object(a.raw['availability']);
    if (av?['value'] == 'out_of_office') {
      final until = parseTime(av?['until']);
      return until == null
          ? 'Out of office.'
          : 'Out of office, back ${shortDate(until)}.';
    }
    if (a.dnd) return 'In do not disturb.';
    return null;
  }

  /// The channels offered after `#` (9.26): channels of this server the
  /// person can see.
  List<Conversation> channelOptions(String query) => matchChannels(
    [
      for (final r in server.conversations) r.conversation,
      ...server.outsideParents.values,
    ],
    query,
    parentOf: server.conversation,
  ).take(12).toList();

  /// The commands offered after `/`.
  List<SlashCommand> commandOptions(String query) {
    final list = commands.isNotEmpty
        ? commands
        : [
            for (final n in builtinCommands.keys)
              SlashCommand({'name': n, 'kind': 'builtin', 'description': ''}),
          ];
    return [
      for (final c in list)
        if (c.name.startsWith(query)) c,
    ];
  }

  @override
  void dispose() {
    _disposed = true;
    _draftTimer?.cancel();
    _typingTimer?.cancel();
    if (editing == null) {
      unawaited(_draftFiles.save(attachments));
      // The draft outlives the screen; the database outlives it too.
      unawaited(
        server.db.putDraft(conversationId, text.wireText, parentId: _draftKey),
      );
    }
    for (final s in _subs) {
      s.cancel();
    }
    text.dispose();
    super.dispose();
  }
}

/// `482 KB`, `3.1 MB`.
String formatBytes(int bytes) {
  if (bytes < 1024) return '$bytes B';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(0)} KB';
  if (bytes < 1024 * 1024 * 1024) return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
}

/// `14 Nov`, or `14 Nov 2027` outside this year.
String shortDate(DateTime t) {
  const months = [
    'Jan',
    'Feb',
    'Mar',
    'Apr',
    'May',
    'Jun',
    'Jul',
    'Aug',
    'Sep',
    'Oct',
    'Nov',
    'Dec',
  ];
  final l = t.toLocal();
  final year = l.year == DateTime.now().year ? '' : ' ${l.year}';
  return '${l.day} ${months[l.month - 1]}$year';
}
