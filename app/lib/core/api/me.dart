import '../models/account.dart';
import '../models/json.dart';
import '../models/personal_layout.dart';
import '../models/server.dart';
import 'client.dart';
import 'operations.dart';

/// The server itself, the caller's account, and the small lists the app
/// keeps whole.
extension MeApi on ApiClient {
  /// The server's public description. Needs no sign-in, so "Add a server"
  /// can show the name before anything else.
  Future<ServerInfo> getServer() async => ServerInfo.fromJson(
    object((await call(Ops.getServer, auth: false))['server']) ?? const {},
  );

  Future<Me> getMe() async =>
      Me.fromJson(object((await call(Ops.getMe))['account']) ?? const {});

  /// The caller's own sidebar (ruling R83).
  Future<PersonalLayout> getSidebar() async => PersonalLayout.fromJson(
    object((await call(Ops.getSidebar))['sidebar']) ?? const {},
  );

  /// Applies [ops] to the caller's sidebar as it stood at [version]. A
  /// stale version is refused with `conflict`.
  Future<PersonalLayout> changeSidebar(int version, List<Json> ops) async =>
      PersonalLayout.fromJson(
        object(
              (await call(
                Ops.changeSidebar,
                body: {'version': version, 'ops': ops},
              ))['sidebar'],
            ) ??
            const {},
      );

  /// Badge and unread totals. Made to be called often.
  Future<StatusSummary> getStatusSummary() async =>
      StatusSummary.fromJson(await call(Ops.getStatusSummary));

  /// Every account the caller can see.
  Future<List<Account>> listAccounts() =>
      allPages(Ops.listAccounts, 'accounts', Account.fromJson);

  Future<List<Group>> listGroups() async =>
      listOf((await call(Ops.listGroups))['groups'], Group.fromJson);

  /// Joins or leaves an open group: joining adds the person to every
  /// channel the group is in.
  Future<void> joinGroup(String id) => call(Ops.joinGroup, path: {'id': id});
  Future<void> leaveGroup(String id) => call(Ops.leaveGroup, path: {'id': id});

  Future<List<CustomEmoji>> listEmoji() async =>
      listOf((await call(Ops.listEmoji))['emoji'], CustomEmoji.fromJson);

  /// Adds a custom emoji: [name] typed as `:name:`, [bytes] the picture
  /// (at most 256 KB and 256 px square).
  Future<CustomEmoji> createEmoji(
    String name,
    String fileName,
    List<int> bytes,
  ) async => CustomEmoji.fromJson(
    object(
          (await upload(
            Ops.createEmoji,
            fields: {'name': name},
            parts: [UploadPart('file', fileName, bytes)],
          ))['emoji'],
        ) ??
        const {},
  );
}
