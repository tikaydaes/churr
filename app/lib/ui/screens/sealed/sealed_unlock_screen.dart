import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../core/sealed_client/session.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/sealed_state.dart';
import '../../../state/server_state.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import '../account/common.dart';
import '../conversation/message_menu.dart' show confirm;
import 'sealed_qr.dart';

/// Opens the ways to unlock this device (docs/CLIENTS.md 9.20).
Future<void> openSealedUnlock(BuildContext context, ServerState server) =>
    showAccountScreen(
      context,
      title: L10n.of(context).unlockTitle,
      width: 600,
      child: SealedUnlockScreen(server: server),
    );

enum _Way { menu, passkey, phrase, device, startAgain, newPhrase }

/// A device that is signed in and locked: use a passkey (on the
/// server's unlock page, R43), the recovery phrase, another device, carry
/// on locked, or start again. On an account with no backup yet it makes
/// one and shows the phrase once.
class SealedUnlockScreen extends StatefulWidget {
  const SealedUnlockScreen({super.key, required this.server, this.onDone});
  final ServerState server;
  final VoidCallback? onDone;

  @override
  State<SealedUnlockScreen> createState() => _SealedUnlockScreenState();
}

class _SealedUnlockScreenState extends State<SealedUnlockScreen> {
  _Way way = _Way.menu;
  String? error;
  bool busy = false;
  final phrase = TextEditingController();
  String? newPhrase;

  // Approval from another device.
  String? requestId, code;
  DateTime? expires;
  Timer? poll;

  SealedController? get ctl => SealedHub.instance.of(widget.server);
  SealedSession? get session => ctl?.session;

  @override
  void initState() {
    super.initState();
    ctl?.addListener(_onChange);
  }

  void _onChange() {
    if (!mounted) return;
    if (session?.unlocked == true && way != _Way.newPhrase) {
      _done();
    } else {
      setState(() {});
    }
  }

  void _done() {
    poll?.cancel();
    if (widget.onDone != null) {
      widget.onDone!();
    } else if (Navigator.of(context).canPop()) {
      Navigator.of(context).pop();
    }
  }

  @override
  void dispose() {
    ctl?.removeListener(_onChange);
    poll?.cancel();
    phrase.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() f) async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await f();
    } on Object catch (e) {
      if (mounted) setState(() => error = L10n.of(context).unlockFailed('$e'));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> _requestCode() => _run(() async {
    final (id, _, c) = await session!.requestApproval();
    setState(() {
      requestId = id;
      code = c;
      expires = DateTime.now().add(const Duration(minutes: 10));
    });
    poll?.cancel();
    poll = Timer.periodic(const Duration(seconds: 3), (_) async {
      final id = requestId;
      if (id == null || !mounted) return;
      try {
        if (await session!.checkApproval(id) && mounted) _done();
      } on Object {
        // Asked again in three seconds.
      }
    });
  });

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final s = session;
    if (s == null) {
      return Padding(
        padding: const EdgeInsets.all(24),
        child: Text(t.sealedNotRunning),
      );
    }
    if (way == _Way.newPhrase && newPhrase != null) {
      return PhraseScreen(phrase: newPhrase!, onDone: _done);
    }
    if (s.status == SealedStatus.noBackup) return _firstBackup(t, s);
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        Text(t.unlockIntro),
        const SizedBox(height: 12),
        _way(t, _Way.passkey, ChurrIcons.globe, t.unlockPasskey, [
          // R43, said plainly on the screen.
          NoteLine(t.unlockPasskeyNote, icon: ChurrIcons.globe),
          Wrap(
            spacing: 8,
            children: [
              FilledButton(
                key: const Key('unlock-open-page'),
                onPressed: busy
                    ? null
                    : () => _run(() async {
                        await launchUrl(
                          s.unlockPage(),
                          mode: LaunchMode.externalApplication,
                        );
                      }),
                child: Text(t.unlockOpenPage),
              ),
              OutlinedButton(
                onPressed: busy
                    ? null
                    : () => _run(() async {
                        if (!await s.checkUnlockPage()) {
                          throw t.unlockPasskeyNotYet;
                        }
                      }),
                child: Text(t.unlockCheckAgain),
              ),
            ],
          ),
        ]),
        _way(t, _Way.phrase, ChurrIcons.fileText, t.unlockPhrase, [
          Text(t.unlockPhraseNote),
          const SizedBox(height: 8),
          TextField(
            key: const Key('unlock-phrase'),
            controller: phrase,
            autocorrect: false,
            enableSuggestions: false,
            minLines: 2,
            maxLines: 3,
            decoration: InputDecoration(labelText: t.unlockPhraseField),
          ),
          const SizedBox(height: 8),
          FilledButton(
            key: const Key('unlock-phrase-go'),
            onPressed: busy
                ? null
                : () => _run(() => s.unlockWithPhrase(phrase.text)),
            child: Text(t.sealedUnlock),
          ),
        ]),
        _way(t, _Way.device, ChurrIcons.devices, t.unlockAnotherDevice, [
          Text(t.unlockAnotherNote),
          const SizedBox(height: 8),
          if (code == null)
            FilledButton(
              key: const Key('unlock-make-code'),
              onPressed: busy ? null : _requestCode,
              child: Text(t.unlockShowCode),
            )
          else ...[
            SealedQr(data: code!, size: 180, label: t.unlockCodeQr),
            const SizedBox(height: 8),
            Center(
              child: SelectableText(
                code!,
                key: const Key('unlock-code'),
                style: monoStyle(context).copyWith(fontSize: 20),
              ),
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
                const SizedBox(width: 8),
                Expanded(child: Text(t.unlockWaiting)),
                TextButton(
                  onPressed: busy ? null : _requestCode,
                  child: Text(t.unlockNewRequest),
                ),
              ],
            ),
          ],
        ]),
        _way(t, _Way.startAgain, ChurrIcons.history, t.unlockStartAgain, [
          NoteLine(t.unlockStartAgainWarning, icon: ChurrIcons.error),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: ChurrColors.of(context).pink,
            ),
            onPressed: busy
                ? null
                : () async {
                    if (!await confirm(
                      context,
                      t.unlockStartAgain,
                      t.unlockStartAgainWarning,
                      yes: t.unlockStartAgain,
                      danger: true,
                    )) {
                      return;
                    }
                    await _run(() async {
                      final p = await s.resetBackup();
                      setState(() {
                        newPhrase = p;
                        way = _Way.newPhrase;
                      });
                    });
                  },
            child: Text(t.unlockStartAgain),
          ),
        ]),
        const SizedBox(height: 8),
        ListTile(
          key: const Key('unlock-carry-on'),
          contentPadding: EdgeInsets.zero,
          leading: const Glyph(ChurrIcons.locked),
          title: Text(t.unlockCarryOn),
          subtitle: Text(t.unlockCarryOnNote),
          onTap: _done,
        ),
        if (busy) const LinearProgressIndicator(minHeight: 2),
        if (error != null) ErrorLine(error!),
      ],
    );
  }

  Widget _way(
    L10n t,
    _Way w,
    ChurrGlyph icon,
    String title,
    List<Widget> body,
  ) {
    final c = ChurrColors.of(context);
    final open = way == w;
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: Material(
        color: open ? c.raised : c.panel,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            ListTile(
              key: Key('unlock-way-${w.name}'),
              leading: Glyph(icon),
              title: Text(
                title,
                style: const TextStyle(fontWeight: FontWeight.w700),
              ),
              trailing: Glyph(
                open ? ChurrIcons.chevronUp : ChurrIcons.chevronDown,
              ),
              onTap: () => setState(() => way = open ? _Way.menu : w),
            ),
            if (open)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: body,
                ),
              ),
          ],
        ),
      ),
    );
  }

  Widget _firstBackup(L10n t, SealedSession s) => ListView(
    padding: const EdgeInsets.all(20),
    children: [
      Text(t.sealedFirstChat),
      const SizedBox(height: 8),
      NoteLine(t.unlockPasskeyNote, icon: ChurrIcons.globe),
      NoteLine(t.sealedOtherWaysNote),
      const SizedBox(height: 8),
      FilledButton(
        key: const Key('sealed-make-backup'),
        onPressed: busy
            ? null
            : () => _run(() async {
                final p = await s.createBackup();
                if (p == null) return; // another device made it: unlock instead
                setState(() {
                  newPhrase = p;
                  way = _Way.newPhrase;
                });
              }),
        child: Text(t.sealedMakeBackup),
      ),
      if (busy) const LinearProgressIndicator(minHeight: 2),
      if (error != null) ErrorLine(error!),
    ],
  );
}

/// The recovery phrase, shown once (docs/CLIENTS.md 9.19): written down,
/// then two of its words checked before going on.
class PhraseScreen extends StatefulWidget {
  const PhraseScreen({
    super.key,
    required this.phrase,
    required this.onDone,
    this.random,
  });
  final String phrase;
  final VoidCallback onDone;
  final Random? random;

  @override
  State<PhraseScreen> createState() => _PhraseScreenState();
}

class _PhraseScreenState extends State<PhraseScreen> {
  bool checking = false;
  late final List<int> asks;
  final a = TextEditingController(), b = TextEditingController();
  bool wrong = false;

  List<String> get words => widget.phrase.split(' ');

  @override
  void initState() {
    super.initState();
    final r = widget.random ?? Random.secure();
    final first = r.nextInt(12);
    var second = r.nextInt(11);
    if (second >= first) second++;
    asks = [first, second]..sort();
  }

  @override
  void dispose() {
    a.dispose();
    b.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    if (!checking) {
      return ListView(
        padding: const EdgeInsets.all(20),
        children: [
          Text(
            t.phraseTitle,
            style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 18),
          ),
          const SizedBox(height: 8),
          Text(t.phraseIntro),
          const SizedBox(height: 12),
          Container(
            key: const Key('phrase-words'),
            color: c.raised,
            padding: const EdgeInsets.all(12),
            child: Wrap(
              spacing: 16,
              runSpacing: 8,
              children: [
                for (var i = 0; i < words.length; i++)
                  SizedBox(
                    width: 130,
                    child: Text(
                      '${i + 1}. ${words[i]}',
                      style: monoStyle(context),
                    ),
                  ),
              ],
            ),
          ),
          const SizedBox(height: 12),
          FilledButton(
            key: const Key('phrase-written'),
            onPressed: () => setState(() => checking = true),
            child: Text(t.phraseWrittenDown),
          ),
        ],
      );
    }
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        Text(t.phraseCheckIntro),
        const SizedBox(height: 8),
        TextField(
          key: const Key('phrase-check-0'),
          controller: a,
          autocorrect: false,
          decoration: InputDecoration(labelText: t.phraseCheck(asks[0] + 1)),
        ),
        const SizedBox(height: 8),
        TextField(
          key: const Key('phrase-check-1'),
          controller: b,
          autocorrect: false,
          decoration: InputDecoration(labelText: t.phraseCheck(asks[1] + 1)),
        ),
        if (wrong) ErrorLine(t.phraseCheckWrong),
        const SizedBox(height: 8),
        Row(
          children: [
            TextButton(
              onPressed: () => setState(() => checking = false),
              child: Text(t.back),
            ),
            const Spacer(),
            FilledButton(
              key: const Key('phrase-check-go'),
              onPressed: () {
                final ok =
                    a.text.trim().toLowerCase() == words[asks[0]] &&
                    b.text.trim().toLowerCase() == words[asks[1]];
                if (ok) {
                  widget.onDone();
                } else {
                  setState(() => wrong = true);
                }
              },
              child: Text(t.phraseContinue),
            ),
          ],
        ),
      ],
    );
  }
}
