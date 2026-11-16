import '../models/account.dart';
import '../models/json.dart';
import '../models/message.dart';
import '../models/personal.dart';
import 'client.dart';
import 'operations.dart';

/// The person's own state on one server: profile, status, availability,
/// do not disturb, quiet hours, appear away, presets, blocks, bookmarks,
/// followed threads, sessions and devices.
extension AccountApi on ApiClient {
  Me _me(Json j) => Me.fromJson(object(j['account']) ?? const {});

  /// Changes the profile. Only the fields given change.
  Future<Me> updateMe(Json changes) async =>
      _me(await call(Ops.updateMe, body: changes));

  Future<Me> setMyAvatar(String fileName, List<int> bytes) async => _me(
    await upload(Ops.setMyAvatar, parts: [UploadPart('file', fileName, bytes)]),
  );

  Future<void> removeMyAvatar() => call(Ops.removeMyAvatar);

  /// Default thread view, whether a meeting pauses notifications, and
  /// whether notify anyway is allowed.
  Future<Json> updateMyPrefs(Json changes) async =>
      object((await call(Ops.updatePrefs, body: changes))['prefs']) ?? const {};

  /// Sets a status from its fields, or applies the preset [presetId].
  Future<Me> setStatus({FullStatus? status, String? presetId}) async => _me(
    await call(
      Ops.setStatus,
      body: presetId != null ? {'preset_id': presetId} : status!.toRequest(),
    ),
  );

  Future<void> clearStatus() => call(Ops.clearStatus);

  Future<Me> setAvailability(String value, {DateTime? until}) async => _me(
    await call(
      Ops.setAvailability,
      body: {'value': value, if (until != null) 'until': formatTime(until)},
    ),
  );

  Future<void> clearAvailability() => call(Ops.clearAvailability);

  /// Do not disturb until [until], or with null until turned off.
  Future<DndState> setDnd({DateTime? until}) async => DndState.fromJson(
    object(
          (await call(
            Ops.setDnd,
            body: {if (until != null) 'until': formatTime(until)},
          ))['dnd'],
        ) ??
        const {},
  );

  /// Makes the account available now (ruling R37).
  Future<DndState> clearDnd() async =>
      DndState.fromJson(object((await call(Ops.clearDnd))['dnd']) ?? const {});

  Future<QuietHours?> getQuietHours() async {
    final q = object((await call(Ops.getQuietHours))['quiet_hours']);
    return q == null ? null : QuietHours.fromJson(q);
  }

  Future<QuietHours> setQuietHours(QuietHours q) async => QuietHours.fromJson(
    object(
          (await call(Ops.setQuietHours, body: q.toRequest()))['quiet_hours'],
        ) ??
        const {},
  );

  Future<void> removeQuietHours() => call(Ops.removeQuietHours);

  /// Skips the next quiet period that has not started.
  Future<QuietHours> skipQuietHours() async => QuietHours.fromJson(
    object((await call(Ops.skipQuietHours))['quiet_hours']) ?? const {},
  );

  Future<QuietHours> unskipQuietHours() async => QuietHours.fromJson(
    object((await call(Ops.unskipQuietHours))['quiet_hours']) ?? const {},
  );

  /// Appear away until [until], or with null until turned off.
  Future<Me> setAway({DateTime? until}) async => _me(
    await call(
      Ops.setAway,
      body: {if (until != null) 'until': formatTime(until)},
    ),
  );

  Future<void> clearAway() => call(Ops.clearAway);

  Future<List<StatusPreset>> listStatusPresets() async => listOf(
    (await call(Ops.listStatusPresets))['presets'],
    StatusPreset.fromJson,
  );

  Future<StatusPreset> createStatusPreset(StatusPreset p) async =>
      StatusPreset.fromJson(
        object(
              (await call(Ops.createStatusPreset, body: p.toInput()))['preset'],
            ) ??
            const {},
      );

  Future<void> deleteStatusPreset(String id) =>
      call(Ops.deleteStatusPreset, path: {'id': id});

  Future<Account> getAccount(String id) async => Account.fromJson(
    object((await call(Ops.getAccount, path: {'id': id}))['account']) ??
        const {},
  );

  Future<List<String>> listBlocks() async =>
      strings((await call(Ops.listBlocks))['account_ids']);
  Future<void> blockAccount(String id) =>
      call(Ops.blockAccount, path: {'account_id': id});
  Future<void> unblockAccount(String id) =>
      call(Ops.unblockAccount, path: {'account_id': id});

  Future<List<Bookmark>> listBookmarks() =>
      allPages(Ops.listBookmarks, 'bookmarks', Bookmark.fromJson, limit: 100);
  Future<void> saveBookmark(String messageId) =>
      call(Ops.addBookmark, path: {'id': messageId});
  Future<void> dropBookmark(String messageId) =>
      call(Ops.removeBookmark, path: {'id': messageId});

  /// Followed threads across every conversation; with [all] those with
  /// nothing unread too.
  Future<List<ThreadEntry>> listThreads({bool all = false}) => allPages(
    Ops.listThreads,
    'threads',
    ThreadEntry.fromJson,
    query: {'all': all ? 'true' : null},
    limit: 100,
  );

  /// `following`, `ignored` or `none`.
  Future<ThreadState?> putThreadState(String parentId, String state) async {
    final s = object(
      (await call(
        Ops.setThreadState,
        path: {'id': parentId},
        body: {'state': state},
      ))['thread_state'],
    );
    return s == null ? null : ThreadState.fromJson(s);
  }

  Future<ThreadState> putThreadRead(String parentId, int seq) async =>
      ThreadState.fromJson(
        object(
              (await call(
                Ops.setThreadReadPosition,
                path: {'id': parentId},
                body: {'seq': seq},
              ))['thread_state'],
            ) ??
            const {},
      );

  Future<List<SessionInfo>> listSessions() async => [
    for (final j in listOf(
      (await call(Ops.listSessions))['sessions'],
      (j) => j,
    ))
      SessionInfo(j),
  ];
  Future<void> endSession(String id) => call(Ops.endSession, path: {'id': id});

  /// Signs out every other session of the account, tokens included.
  Future<void> endOtherSessions() => call(Ops.endOtherSessions);

  Future<List<DeviceInfo>> listDevices() async => [
    for (final j in listOf((await call(Ops.listDevices))['devices'], (j) => j))
      DeviceInfo(j),
  ];
  Future<void> removeDevice(String id) =>
      call(Ops.removeDevice, path: {'id': id});
}
