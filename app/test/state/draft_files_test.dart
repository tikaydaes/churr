import 'dart:io';
import 'dart:typed_data';

import 'package:churr/state/composer_controller.dart';
import 'package:churr/state/draft_files.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/state/thread_pane_signal.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

void main() {
  test(
    'a draft keeps its files and captions, and loses them when sent',
    () async {
      final (app, _) = await makeAccountApp(null);
      final ServerState s = app.servers[0];
      final drafts = DraftFiles(s, 'general', null);
      final a = Attachment(
        name: 'notes.txt',
        bytes: Uint8List.fromList([1, 2, 3]),
      )..caption = 'minutes';
      await drafts.save([a]);
      final back = <Attachment>[];
      await DraftFiles(s, 'general', null).restore(back);
      expect(back.single.name, 'notes.txt');
      expect(back.single.bytes, [1, 2, 3]);
      expect(back.single.caption, 'minutes');
      // Another thread's draft is its own.
      final other = <Attachment>[];
      await DraftFiles(s, 'general', 'parent-1').restore(other);
      expect(other, isEmpty);
      await drafts.clear();
      final none = <Attachment>[];
      await DraftFiles(s, 'general', null).restore(none);
      expect(none, isEmpty);
    },
  );

  test('on disk: files under the server folder with an index', () async {
    final (app, _) = await makeAccountApp(null);
    final s = app.servers[0];
    final dir = await Directory.systemTemp.createTemp('drafts');
    addTearDown(() => dir.delete(recursive: true));
    final disk = ServerState(
      entry: s.entry,
      db: s.db,
      api: s.api,
      filesDirectory: dir.path,
    );
    final a = Attachment(name: 'a b.png', bytes: Uint8List.fromList([9, 9]));
    await DraftFiles(disk, 'design', null).save([a]);
    expect(Directory('${dir.path}/drafts/design/top').listSync(), hasLength(1));
    final back = <Attachment>[];
    await DraftFiles(disk, 'design', null).restore(back);
    expect(back.single.bytes, [9, 9]);
    await DraftFiles(disk, 'design', null).save([]);
    expect(Directory('${dir.path}/drafts/design/top').existsSync(), isFalse);
  });

  test('the thread pane signal: back closes the thread first', () {
    var closed = 0;
    expect(ThreadPaneSignal.closeIfOpen(), isFalse);
    ThreadPaneSignal.report(true, () => closed++);
    expect(ThreadPaneSignal.open.value, isTrue);
    expect(ThreadPaneSignal.closeIfOpen(), isTrue);
    expect(closed, 1);
    ThreadPaneSignal.report(false, () {});
    expect(ThreadPaneSignal.closeIfOpen(), isFalse);
  });
}
