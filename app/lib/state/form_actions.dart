import 'dart:typed_data';

import '../core/api/forms.dart';
import '../core/db/cache_db.dart';
import '../core/ids.dart';
import '../core/models/form.dart';
import '../core/models/json.dart';
import '../core/models/message.dart';
import 'server_state.dart';

/// Answering, pressing, closing and posting forms, and reading a
/// checklist item's history, for one server.
///
/// Writes follow the house rule: the reader's own answer is written to
/// the cached message first, so the choice shows at once; then the
/// request; the form the server answers with replaces it, and the
/// `form.answered` event brings the same to every other device. A
/// refusal puts the earlier form back and is thrown for the screen to
/// show.
class FormActions {
  FormActions(this.server);
  final ServerState server;

  CacheDb get _db => server.db;

  /// Interactions with bots started on this device, by id: a bot's
  /// dialog is shown where the person acted (EVENTS.md section 10).
  static final startedHere = <String>{};

  Future<void> _setForm(String messageId, Json form) async {
    final m = await _db.message(messageId);
    if (m != null) await _db.upsertMessages([m.copyWith(form: form)]);
  }

  Future<FormReply> _write(
    Message m,
    Json? optimistic,
    Future<FormReply> Function() request,
  ) async {
    final before = m.form;
    if (optimistic != null) await _setForm(m.id, optimistic);
    try {
      final reply = await request();
      if (reply.form.isNotEmpty) await _setForm(m.id, reply.form);
      if (reply.interactionId != null) startedHere.add(reply.interactionId!);
      return reply;
    } catch (_) {
      if (optimistic != null && before != null) await _setForm(m.id, before);
      rethrow;
    }
  }

  /// Answers, or changes the answer.
  Future<FormReply> answer(Message m, Json answers) {
    final form = FormModel.fromJson(m.form!);
    // A `bot` form's answer goes to the bot and is stored nowhere, so
    // nothing is shown as chosen.
    final optimistic = form.action == 'bot' ? null : form.withMyAnswer(answers);
    return _write(m, optimistic, () => server.api.answerForm(m.id, answers));
  }

  Future<FormReply> withdraw(Message m) {
    final form = FormModel.fromJson(m.form!);
    return _write(
      m,
      form.withMyAnswer(null),
      () => server.api.withdrawFormAnswer(m.id),
    );
  }

  /// Presses a button, with whatever the fields hold.
  Future<FormReply> press(Message m, String buttonId, {Json? answers}) {
    final form = FormModel.fromJson(m.form!);
    final optimistic = form.action == 'tally'
        ? form.withMyAnswer({...?answers, '_button': buttonId})
        : null;
    return _write(
      m,
      optimistic,
      () => server.api.pressFormButton(m.id, buttonId, answers: answers),
    );
  }

  Future<FormReply> addOption(Message m, String fieldId, String label) =>
      _write(m, null, () => server.api.addFormOption(m.id, fieldId, label));

  Future<FormReply> close(Message m) =>
      _write(m, null, () => server.api.closeForm(m.id));

  Future<List<FormAnswerRow>> results(Message m) =>
      server.api.listFormResults(m.id);

  Future<Uint8List> csv(Message m) => server.api.downloadFormResults(m.id);

  Future<List<TaskChange>> checklistHistory(Message m, String itemId) =>
      server.api.checklistItemHistory(m.id, itemId);

  /// Posts a message carrying [form]. Not through the outbox, which holds
  /// text only: the message is written to the cache when the server
  /// answers (and again, harmlessly, when its event arrives).
  Future<Message> post(
    String conversationId,
    Json form, {
    String? parentId,
  }) async {
    final m = await server.api.postForm(
      conversationId,
      clientId: newUuid(),
      form: form,
      parentId: parentId,
    );
    // Written only when the cached run reaches the newest message, so a
    // gap is never made in it.
    final row = await _db.conversation(conversationId);
    if (row != null && m.seq <= row.syncedSeq + 1) {
      await _db.upsertMessages([m]);
    }
    return m;
  }

  /// Whether the reader may see the results list and download it:
  /// `collect` for the form's author and the channel's managers; a named
  /// `tally` for anyone the server shows the tally to.
  bool canReadResults(Message m) {
    final form = FormModel.fromJson(m.form!);
    final level = server.conversation(m.conversationId)?.membership?.level;
    if (form.action == 'collect') {
      return m.authorId == server.me?.id || level == 'manager' || _admin;
    }
    if (form.action == 'tally') return !form.anonymous && form.tally != null;
    return false;
  }

  /// The author, an account acting as manager, or an admin who can read
  /// it may close a form.
  bool canClose(Message m) {
    final level = server.conversation(m.conversationId)?.membership?.level;
    return m.authorId == server.me?.id || level == 'manager' || _admin;
  }

  bool get _admin => server.me?.role == 'owner' || server.me?.role == 'admin';
}
