import 'dart:async';

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../../../core/sealed/sealed.dart' show CodeError, Uuid;
import '../../../core/sealed_client/api.dart';
import '../../../core/sealed_client/session.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/sealed_state.dart';
import '../../../state/server_state.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import '../account/common.dart';
import '../account/sign_in_screens.dart' show ScanCodeScreen, canScan;
import '../conversation/message_menu.dart' show confirm;
import 'sealed_unlock_screen.dart';
import 'sealed_words.dart';

/// Opens the list of devices and passkeys (docs/CLIENTS.md 9.20).
Future<void> openSealedDevices(BuildContext context, ServerState server) =>
    showAccountScreen(
      context,
      title: L10n.of(context).sealedChatsAndDevices,
      width: 640,
      child: SealedDevicesScreen(server: server),
    );

/// Where this device stands, the account's devices (which one this is,
/// which are browsers, which are in sealed chats, when each was last
/// seen) with removal, devices asking to be unlocked, and the passkeys
/// that can and cannot unlock.
class SealedDevicesScreen extends StatefulWidget {
  const SealedDevicesScreen({super.key, required this.server});
  final ServerState server;

  @override
  State<SealedDevicesScreen> createState() => _SealedDevicesScreenState();
}

class _SealedDevicesScreenState extends State<SealedDevicesScreen> {
  String? error, note;
  bool busy = false;

  SealedController? get ctl => SealedHub.instance.of(widget.server);
  SealedSession? get session => ctl?.session;

  @override
  void initState() {
    super.initState();
    ctl?.addListener(_changed);
    final s = session;
    if (s != null) unawaited(s.refresh().catchError((_) {}));
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    ctl?.removeListener(_changed);
    super.dispose();
  }

  Future<void> _run(Future<void> Function() f, {String? done}) async {
    setState(() {
      busy = true;
      error = null;
      note = null;
    });
    try {
      await f();
      if (mounted && done != null) setState(() => note = done);
    } on Object catch (e) {
      if (mounted) setState(() => error = '$e');
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  String _when(DateTime? t) => t == null
      ? '–'
      : DateFormat.yMMMd(Localizations.localeOf(context).toString())
            .add_Hm()
            .format(t.toLocal());

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final s = session;
    if (s == null) {
      return Padding(
        padding: const EdgeInsets.all(24),
        child: Text(t.sealedNotRunning),
      );
    }
    final me = s.myId ?? '';
    final listed = {for (final d in s.listedDevices(me)) '${d.deviceId}'};
    final thisId = '${s.device?.id ?? Uuid.zero}';
    final status = s.status;
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        NoteLine(
          switch (status) {
            SealedStatus.unlocked => t.sealedStatusUnlocked,
            SealedStatus.noBackup => t.sealedStatusNoBackup,
            SealedStatus.removed => t.sealedRemovedHere,
            _ => t.sealedStatusLocked,
          },
          icon: ChurrIcons.shield,
          action:
              status == SealedStatus.locked || status == SealedStatus.noBackup
              ? TextButton(
                  onPressed: () => openSealedUnlock(context, widget.server),
                  child: Text(t.sealedUnlock),
                )
              : status == SealedStatus.removed
              ? TextButton(
                  onPressed: ctl!.restart,
                  child: Text(t.sealedSetUpAgain),
                )
              : null,
        ),
        if (s.requests.isNotEmpty) ...[
          SectionHead(t.sealedRequestsTitle),
          for (final r in s.requests)
            if (r.deviceId != thisId)
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Glyph(ChurrIcons.devices),
                title: Text(
                  t.approveTitle(sealedPlatformNameWords(t, r.platform)),
                ),
                subtitle: Text(
                  t.approveSignedIn(r.deviceName, _when(r.createdAt)),
                ),
                trailing: TextButton(
                  onPressed: () => showApproveDevice(context, widget.server, r),
                  child: Text(t.approveYes),
                ),
              ),
        ],
        SectionHead(t.sealedYourDevices),
        for (final d in s.devices.where(
          (d) => !d.retired || listed.contains(d.id),
        ))
          ListTile(
            key: Key('sealed-device-${d.id}'),
            contentPadding: EdgeInsets.zero,
            leading: Glyph(
              d.platform == 'web' ? ChurrIcons.globe : ChurrIcons.devices,
            ),
            title: Wrap(
              spacing: 6,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                Text(
                  d.name.isEmpty
                      ? sealedPlatformNameWords(t, d.platform)
                      : d.name,
                ),
                _Tag(sealedPlatformNameWords(t, d.platform), c.dim),
                if (d.platform == 'web') _Tag(t.sealedBrowserTag, c.yellowInk),
                if (d.id == thisId) _Tag(t.sealedThisDevice, c.cyan),
                if (!listed.contains(d.id)) _Tag(t.sealedNotListed, c.dim),
              ],
            ),
            subtitle: Text(
              t.sealedLastSeen(_when(d.lastSeenAt ?? d.createdAt)),
            ),
            trailing: d.id == thisId
                ? null
                : TextButton(
                    key: Key('sealed-remove-${d.id}'),
                    onPressed: busy
                        ? null
                        : () async {
                            final name = d.name.isEmpty
                                ? sealedPlatformNameWords(t, d.platform)
                                : d.name;
                            if (!await confirm(
                              context,
                              t.sealedRemoveTitle(name),
                              t.sealedRemoveBody,
                              yes: t.sealedRemove,
                              danger: true,
                            )) {
                              return;
                            }
                            await _run(
                              () => s.removeDevice(d.id),
                              done: t.sealedRemoved,
                            );
                          },
                    child: Text(t.sealedRemove),
                  ),
          ),
        SectionHead(t.sealedPasskeys),
        for (final p in s.passkeys)
          ListTile(
            key: Key('sealed-passkey-${p.id}'),
            contentPadding: EdgeInsets.zero,
            leading: const Glyph(ChurrIcons.locked),
            title: Text(p.name),
            subtitle: Text(
              p.canUnlock
                  ? t.passkeyCanUnlock
                  : p.unlockPublic != null && status == SealedStatus.unlocked
                  ? t.passkeyWaiting
                  : t.passkeyCannotUnlock,
              style: TextStyle(color: p.canUnlock ? c.cyan : c.dim),
            ),
            trailing:
                !p.canUnlock &&
                    p.unlockPublic != null &&
                    status == SealedStatus.unlocked
                ? TextButton(
                    onPressed: busy ? null : () => _approvePasskey(p),
                    child: Text(t.passkeyApproveYes),
                  )
                : null,
          ),
        if (status == SealedStatus.unlocked) ...[
          const SizedBox(height: 12),
          OutlinedButton(
            onPressed: busy
                ? null
                : () => _run(s.replaceKeys, done: t.sealedReplaced),
            child: Text(t.sealedReplaceKeys),
          ),
        ],
        if (busy) const LinearProgressIndicator(minHeight: 2),
        if (note != null) NoteLine(note!),
        if (error != null) ErrorLine(error!),
      ],
    );
  }

  Future<void> _approvePasskey(PasskeyRow p) async {
    final t = L10n.of(context);
    if (!await confirm(
      context,
      t.passkeyApproveTitle(p.name),
      t.passkeyApproveBody,
      yes: t.passkeyApproveYes,
    )) {
      return;
    }
    await _run(() => session!.approvePasskey(p));
  }
}

class _Tag extends StatelessWidget {
  const _Tag(this.text, this.color);
  final String text;
  final Color color;
  @override
  Widget build(BuildContext context) => Text(
    text,
    style: TextStyle(color: color, fontSize: 12, fontWeight: FontWeight.w700),
  );
}

/// "Unlock a new device (Android app)?" (docs/CLIENTS.md 9.20): the
/// person scans the new device's QR code or types its 20 characters;
/// only a code that matches the request goes on. A wrong one says the
/// request may have been tampered with and offers to end that device's
/// session.
Future<void> showApproveDevice(
  BuildContext context,
  ServerState server,
  UnlockRequestRow r,
) => showAccountScreen(
  context,
  title: L10n.of(context)
      .approveTitle(sealedPlatformNameWords(L10n.of(context), r.platform)),
  child: ApproveDeviceScreen(server: server, request: r),
);

class ApproveDeviceScreen extends StatefulWidget {
  const ApproveDeviceScreen({
    super.key,
    required this.server,
    required this.request,
  });
  final ServerState server;
  final UnlockRequestRow request;

  @override
  State<ApproveDeviceScreen> createState() => _ApproveDeviceScreenState();
}

class _ApproveDeviceScreenState extends State<ApproveDeviceScreen> {
  final code = TextEditingController();
  bool busy = false, wrong = false, scanning = false;
  String? error;

  @override
  void dispose() {
    code.dispose();
    super.dispose();
  }

  Future<void> _approve(String typed) async {
    final s = SealedHub.instance.of(widget.server)?.session;
    if (s == null) return;
    final nav = Navigator.of(context);
    setState(() {
      busy = true;
      wrong = false;
      error = null;
      scanning = false;
    });
    try {
      await s.approve(widget.request, typed);
      if (nav.canPop()) nav.pop();
    } on CodeError {
      if (mounted) setState(() => wrong = true);
    } on Object catch (e) {
      if (mounted) setState(() => error = '$e');
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final r = widget.request;
    if (scanning) {
      return ScanCodeScreen(onText: (text) => _approve(text.trim()));
    }
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        Text(
          t.approveSignedIn(
            r.deviceName,
            r.createdAt == null
                ? ''
                : DateFormat.Hm().format(r.createdAt!.toLocal()),
          ),
        ),
        const SizedBox(height: 8),
        NoteLine(t.approveWhyLong),
        TextField(
          key: const Key('approve-code'),
          controller: code,
          autocorrect: false,
          textCapitalization: TextCapitalization.characters,
          decoration: InputDecoration(labelText: t.approveCodeField),
          onSubmitted: _approve,
        ),
        const SizedBox(height: 8),
        Wrap(
          spacing: 8,
          children: [
            FilledButton(
              key: const Key('approve-go'),
              onPressed: busy ? null : () => _approve(code.text),
              child: Text(t.approveYes),
            ),
            if (canScan)
              OutlinedButton.icon(
                icon: const Glyph(ChurrIcons.scan, size: 16),
                onPressed: busy ? null : () => setState(() => scanning = true),
                label: Text(t.approveScan),
              ),
            TextButton(
              onPressed: () => Navigator.of(context).maybePop(),
              child: Text(t.approveNotNow),
            ),
          ],
        ),
        if (wrong) ...[
          ErrorLine(t.approveWrongCode),
          OutlinedButton(
            key: const Key('approve-end-session'),
            onPressed: busy
                ? null
                : () async {
                    final nav = Navigator.of(context);
                    await widget.server.api.removeSealedDevice(r.deviceId);
                    if (nav.canPop()) nav.pop();
                  },
            child: Text(t.approveEndSession),
          ),
        ],
        if (busy) const LinearProgressIndicator(minHeight: 2),
        if (error != null) ErrorLine(error!),
      ],
    );
  }
}
