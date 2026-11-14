import 'dart:typed_data';

import '../models/json.dart';
import '../models/message.dart';
import 'client.dart';
import 'operations.dart';

/// What answering or pressing returns: the form as the reader now sees
/// it, and for a ticket the new channel, for a bot an interaction.
class FormReply {
  FormReply(this.form, {this.conversationId, this.interactionId});
  final Json form;
  final String? conversationId;
  final String? interactionId;

  factory FormReply.fromJson(Json j) => FormReply(
    object(j['form']) ?? const {},
    conversationId: j['conversation_id'] as String?,
    interactionId: j['interaction_id'] as String?,
  );
}

/// One stored answer (`FormAnswer`).
class FormAnswerRow {
  FormAnswerRow(this.raw)
    : id = raw['id'] as String? ?? '',
      accountId = raw['account_id'] as String?,
      buttonId = raw['button_id'] as String?,
      answers = object(raw['answers']) ?? const {},
      createdAt = parseTime(raw['created_at']),
      updatedAt = parseTime(raw['updated_at']);
  final Json raw;
  final String id;

  /// Absent on an anonymous form.
  final String? accountId;
  final String? buttonId;
  final Json answers;
  final DateTime? createdAt;
  final DateTime? updatedAt;
}

/// One tick or untick of a checklist item (`TaskChange`).
class TaskChange {
  TaskChange({
    required this.itemId,
    required this.accountId,
    required this.checked,
    required this.at,
  });
  factory TaskChange.fromJson(Json j) => TaskChange(
    itemId: j['item_id'] as String? ?? '',
    accountId: j['account_id'] as String? ?? '',
    checked: j['checked'] == true,
    at:
        parseTime(j['at']) ??
        DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
  );
  final String itemId;
  final String accountId;
  final bool checked;
  final DateTime at;
}

/// Forms, checklist history and bot dialogs.
extension FormsApi on ApiClient {
  /// Posts a message carrying a form (a preset or the builder's).
  Future<Message> postForm(
    String conversationId, {
    required String clientId,
    required Json form,
    String text = '',
    String? parentId,
  }) async {
    final j = await call(
      Ops.postMessage,
      path: {'id': conversationId},
      body: {
        'client_id': clientId,
        if (text.isNotEmpty) 'text': text,
        'parent_id': ?parentId,
        'form': form,
      },
    );
    return Message.fromJson(object(j['message']) ?? const {});
  }

  Future<FormReply> answerForm(String messageId, Json answers) async =>
      FormReply.fromJson(
        await call(
          Ops.answerForm,
          path: {'id': messageId},
          body: {'answers': answers},
        ),
      );

  Future<FormReply> withdrawFormAnswer(String messageId) async =>
      FormReply.fromJson(
        await call(Ops.withdrawFormAnswer, path: {'id': messageId}),
      );

  Future<FormReply> pressFormButton(
    String messageId,
    String buttonId, {
    Json? answers,
  }) async => FormReply.fromJson(
    await call(
      Ops.pressFormButton,
      path: {'id': messageId, 'button': buttonId},
      body: {'answers': ?answers},
    ),
  );

  Future<FormReply> addFormOption(
    String messageId,
    String fieldId,
    String label,
  ) async => FormReply.fromJson(
    await call(
      Ops.addFormOption,
      path: {'id': messageId},
      body: {'field_id': fieldId, 'label': label},
    ),
  );

  Future<FormReply> closeForm(String messageId) async =>
      FormReply.fromJson(await call(Ops.closeForm, path: {'id': messageId}));

  Future<List<FormAnswerRow>> listFormResults(String messageId) => allPages(
    Ops.listFormResults,
    'answers',
    FormAnswerRow.new,
    path: {'id': messageId},
  );

  /// The answers as CSV, as the server writes it.
  Future<Uint8List> downloadFormResults(String messageId) =>
      download(Ops.downloadFormResults.fill({'id': messageId}));

  Future<List<TaskChange>> checklistItemHistory(
    String messageId,
    String itemId,
  ) async => listOf(
    (await call(
      Ops.getChecklistItemHistory,
      path: {'id': messageId, 'item': itemId},
    ))['changes'],
    TaskChange.fromJson,
  );

  Future<String?> submitDialog(
    String dialogId,
    Json answers, {
    String? buttonId,
  }) async =>
      (await call(
            Ops.submitDialog,
            path: {'id': dialogId},
            body: {'answers': answers, 'button_id': ?buttonId},
          ))['interaction_id']
          as String?;

  Future<void> dismissDialog(String dialogId) =>
      call(Ops.dismissDialog, path: {'id': dialogId});
}
