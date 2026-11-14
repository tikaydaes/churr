import 'package:flutter/material.dart';

import '../../../core/models/account.dart';
import '../../../core/models/conversation.dart';
import '../../../core/sealed_client/session.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/app_state.dart';
import '../../../state/sealed_state.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import '../../widgets/avatar.dart';
import '../account/common.dart';
import '../channels/common.dart';
import 'sealed_unlock_screen.dart';

/// "New sealed chat" (docs/CLIENTS.md 9.19), asking which server first
/// where the layout needs it.
Future<void> openNewSealedChat(
  BuildContext context, {
  ServerState? server,
}) async {
  final app = AppScope.read(context);
  final s = await pickServer(context, app, preferred: server);
  if (s == null || !context.mounted) return;
  await showScreen<void>(
    context,
    title: L10n.of(context).newSealedTitle,
    width: 600,
    height: 680,
    child: NewSealedChatScreen(app: app, server: s),
  );
}

/// Pick one person. The screen says what a sealed chat is and is not,
/// where the server's code is trusted (R43), and which passkeys can
/// unlock; the account's first sealed chat makes the backup and shows
/// the recovery phrase once.
class NewSealedChatScreen extends StatefulWidget {
  const NewSealedChatScreen({
    super.key,
    required this.app,
    required this.server,
  });
  final AppState app;
  final ServerState server;

  @override
  State<NewSealedChatScreen> createState() => _NewSealedChatScreenState();
}

class _NewSealedChatScreenState extends State<NewSealedChatScreen> {
  final query = TextEditingController();
  String? error;
  bool busy = false;
  String? phrase;
  Account? waiting;

  SealedController? get ctl => SealedHub.instance.of(widget.server);

  @override
  void initState() {
    super.initState();
    ctl?.addListener(_changed);
    query.addListener(_changed);
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    ctl?.removeListener(_changed);
    query.dispose();
    super.dispose();
  }

  Future<void> _start(Account a) async {
    final s = ctl?.session;
    if (s == null) return;
    final nav = Navigator.of(context);
    setState(() {
      busy = true;
      error = null;
    });
    try {
      if (s.status == SealedStatus.noBackup) {
        final p = await s.createBackup();
        if (p != null) {
          setState(() {
            phrase = p;
            waiting = a;
            busy = false;
          });
          return;
        }
      }
      final conv = Conversation.fromJson(await s.startChat(a.id));
      if (widget.server.conversation(conv.id) == null) {
        await widget.server.db.upsertConversation(
          conv,
          syncedSeq: conv.lastSeq,
        );
      }
      widget.app.openConversation(OpenTarget(widget.server.localId, conv.id));
      if (nav.canPop()) nav.pop();
    } on Object catch (e) {
      if (mounted) setState(() => error = '$e');
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final s = ctl?.session;
    if (s == null) {
      return Padding(
        padding: const EdgeInsets.all(24),
        child: Text(t.sealedNotRunning),
      );
    }
    if (phrase != null) {
      return PhraseScreen(
        phrase: phrase!,
        onDone: () {
          final a = waiting!;
          setState(() => phrase = null);
          _start(a);
        },
      );
    }
    final me = widget.server.me?.id;
    final q = query.text.trim().toLowerCase();
    final people = [
      for (final a in widget.server.accounts.values)
        if (a.id != me &&
            !a.isBot &&
            (q.isEmpty ||
                a.name.toLowerCase().contains(q) ||
                a.username.toLowerCase().contains(q)))
          a,
    ]..sort((x, y) => x.name.toLowerCase().compareTo(y.name.toLowerCase()));
    final status = s.status;
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        Text(t.newSealedIntro),
        const SizedBox(height: 8),
        NoteLine(t.unlockPasskeyNote, icon: ChurrIcons.globe),
        NoteLine(t.sealedOtherWaysNote),
        if (s.passkeys.isNotEmpty) ...[
          SectionHead(t.sealedPasskeys),
          for (final p in s.passkeys)
            Text(
              '${p.name}: ${p.canUnlock ? t.passkeyCanUnlock : t.passkeyCannotUnlock}',
              style: TextStyle(color: p.canUnlock ? c.cyan : c.dim),
            ),
        ],
        if (status == SealedStatus.noBackup)
          NoteLine(t.sealedFirstChat, icon: ChurrIcons.shield),
        if (status == SealedStatus.locked)
          NoteLine(
            t.newSealedLocked,
            icon: ChurrIcons.locked,
            action: TextButton(
              onPressed: () => openSealedUnlock(context, widget.server),
              child: Text(t.sealedUnlock),
            ),
          ),
        SectionHead(t.newSealedPick),
        TextField(
          key: const Key('sealed-person-query'),
          controller: query,
          decoration: InputDecoration(
            prefixIcon: const Glyph(ChurrIcons.search),
            labelText: t.newSealedPick,
          ),
        ),
        if (busy) const LinearProgressIndicator(minHeight: 2),
        if (error != null) ErrorLine(error!),
        for (final a in people.take(50))
          ListTile(
            key: Key('sealed-person-${a.id}'),
            contentPadding: EdgeInsets.zero,
            leading: Avatar(server: widget.server, account: a, size: 28),
            title: Text(a.name),
            subtitle: Text('@${a.username}'),
            enabled:
                !busy &&
                (status == SealedStatus.unlocked ||
                    status == SealedStatus.noBackup),
            onTap: () => _start(a),
          ),
      ],
    );
  }
}
