import 'dart:async';
import '../../../core/sealed/sealed.dart' show b64, unb64;
import '../../../core/sealed_client/pending.dart';
import '../../../core/sealed_client/session.dart' show PendingDeviceKeys, SignInHandover;
import 'dart:io' show Platform;

import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../../../core/api/client.dart';
import '../../../core/api/errors.dart';
import '../../../core/api/me.dart';
import '../../../core/oauth.dart';
import '../../../core/oauth_metadata.dart';
import '../../../core/session_store.dart';
import '../../../core/setup_code.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/browser_sign_in.dart' as browser;
import '../../../state/device_setup.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';
import '../add_server/add_server_screen.dart';
import 'common.dart';

/// Sign-in beyond the first "Add a server" (docs/CLIENTS.md 9.22, 8.2,
/// 8.10): signing in again after the server ended the session, scanning
/// a pairing code or another device's set-up code, and showing this
/// device's set-up code.

// ---- signed out by the server ----

/// Signs in to [server] again. The local database is kept until the app
/// knows the same account came back; a different account replaces it.
class ReSignInScreen extends StatefulWidget {
  const ReSignInScreen({super.key, required this.server, this.signIn});
  final ServerState server;

  /// Replaced in tests, which have no browser.
  final Future<Tokens> Function(ApiClient api)? signIn;

  @override
  State<ReSignInScreen> createState() => _ReSignInScreenState();
}

class _ReSignInScreenState extends State<ReSignInScreen> {
  String? error;
  bool busy = false;

  Future<void> _go() async {
    final app = AppScope.read(context);
    final nav = Navigator.of(context);
    final t = L10n.of(context);
    final s = widget.server;
    setState(() {
      busy = true;
      error = null;
    });
    final probe = app.probe(Uri.parse(s.entry.address));
    try {
      final tokens = await (widget.signIn ?? browser.signInWithBrowser)(probe);
      final who = await probe.getMe();
      if (who.id == s.me?.id) {
        // The same account: carry on with what this device has.
        await s.api.setTokens(tokens);
        if (probe.tokenEndpoint != null) {
          s.api.tokenEndpoint = probe.tokenEndpoint;
          s.api.revocationEndpoint = probe.revocationEndpoint;
        }
        s.live.start();
      } else {
        // Another account: what was kept belonged to the old one.
        await app.removeServer(s.localId);
        await app.addServer(Uri.parse(s.entry.address), tokens);
      }
      if (nav.canPop()) nav.pop();
    } on ApiException catch (e) {
      error = e.isNetwork ? null : e.message;
      error ??= t.didNotWork(e.message);
    } on OAuthException catch (e) {
      error = e.description ?? e.error;
    } on Exception catch (e) {
      error = '$e';
    } finally {
      probe.close();
    }
    if (mounted) setState(() => busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return ListView(
      padding: const EdgeInsets.all(24),
      children: [
        Text(t.reSignInBody),
        const SizedBox(height: 16),
        FilledButton(
          key: const Key('re-sign-in'),
          onPressed: busy ? null : _go,
          child: Text(t.signInWithBrowser),
        ),
        if (busy)
          const Padding(
            padding: EdgeInsets.only(top: 12),
            child: LinearProgressIndicator(minHeight: 2),
          ),
        if (error != null) ErrorLine(error!),
      ],
    );
  }
}

/// The banner over the sidebar for each server the server signed out.
class SignedOutBanners extends StatelessWidget {
  const SignedOutBanners({super.key});

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final out = [
      for (final s in app.servers)
        if (s.api.signedOut || s.liveState.name == 'signedOut') s,
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (final s in out)
          Container(
            key: Key('signed-out-${s.localId}'),
            color: c.raised,
            padding: const EdgeInsets.fromLTRB(12, 4, 4, 4),
            child: Row(
              children: [
                Glyph(ChurrIcons.clock, size: 16, color: c.yellowInk),
                const SizedBox(width: 8),
                Expanded(child: Text(t.signedOutBanner(s.name), maxLines: 2)),
                TextButton(
                  onPressed: () => showAccountScreen(
                    context,
                    title: t.reSignInTitle(s.name),
                    child: ReSignInScreen(server: s),
                  ),
                  child: Text(t.signInAgain),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

// ---- scanning ----

/// Whether this build can use a camera to scan (mobile_scanner runs on
/// Android, iOS and macOS).
bool get canScan => Platform.isAndroid || Platform.isIOS || Platform.isMacOS;

/// Scans a QR code, or takes its text pasted. Returns the text.
class ScanCodeScreen extends StatefulWidget {
  const ScanCodeScreen({super.key, required this.onText, this.camera = true});
  final Future<void> Function(String text) onText;
  final bool camera;

  @override
  State<ScanCodeScreen> createState() => _ScanCodeScreenState();
}

class _ScanCodeScreenState extends State<ScanCodeScreen> {
  final paste = TextEditingController();
  String? last;

  @override
  void dispose() {
    paste.dispose();
    super.dispose();
  }

  void _seen(String text) {
    if (text == last) return;
    last = text;
    widget.onText(text);
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        if (widget.camera && canScan)
          SizedBox(
            height: 280,
            child: MobileScanner(
              onDetect: (capture) {
                for (final b in capture.barcodes) {
                  final v = b.rawValue;
                  if (v != null) _seen(v);
                }
              },
              errorBuilder: (context, _) => Center(
                child: Text(t.cameraUnavailable, textAlign: TextAlign.center),
              ),
            ),
          )
        else
          Text(t.cameraUnavailable),
        const SizedBox(height: 12),
        TextField(
          key: const Key('paste-code'),
          controller: paste,
          decoration: InputDecoration(labelText: t.pasteCode),
          onSubmitted: (v) => widget.onText(v.trim()),
        ),
      ],
    );
  }
}

/// Signs in by a server's pairing code (docs/AUTH.md 11.4): posts the
/// code with this device's name and asks again every two seconds until
/// the person confirms in their browser.
class PairingScreen extends StatefulWidget {
  const PairingScreen({
    super.key,
    required this.code,
    this.interval = const Duration(seconds: 2),
  });
  final PairingCode code;
  final Duration interval;

  @override
  State<PairingScreen> createState() => _PairingScreenState();
}

class _PairingScreenState extends State<PairingScreen> {
  String? error;
  bool done = false;
  bool _stop = false;

  @override
  void initState() {
    super.initState();
    unawaited(_run());
  }

  @override
  void dispose() {
    _stop = true;
    super.dispose();
  }

  Future<void> _run() async {
    final app = AppScope.read(context);
    final nav = Navigator.of(context);
    final t = L10n.of(context);
    final server = normalizeServer(widget.code.server);
    if (server == null) {
      setState(() => error = t.notAChurrCode);
      return;
    }
    final probe = app.probe(server);
    try {
      await fetchOAuthMetadata(probe);
      final deadline = DateTime.now().add(const Duration(minutes: 2));
      // Sealed chats: the box key goes with the request (SEALED.md 13.7).
      final keys = PendingDeviceKeys.generate();
      while (!_stop) {
        try {
          final tokens = await probe.claimPairing(
            pairingCode: widget.code.code,
            deviceName: browser.deviceName,
            platform: Platform.operatingSystem,
            boxKey: b64(keys.boxPublic),
          );
          leaveSignIn(
            server,
            SignInHandover(
              keys: keys,
              handover: unb64(tokens.handover),
              pairingSecret: unb64(widget.code.secret),
            ),
          );
          await app.addServer(server, tokens);
          await donePendingServer(app, server.toString());
          done = true;
          if (nav.canPop()) nav.pop();
          return;
        } on OAuthException catch (e) {
          if (e.error != 'authorization_pending') rethrow;
          if (DateTime.now().isAfter(deadline)) throw OAuthException('expired_token', t.pairingExpired);
          await Future<void>.delayed(widget.interval);
        }
      }
    } on OAuthException catch (e) {
      if (mounted) setState(() => error = e.error == 'invalid_grant' ? t.pairingExpired : (e.description ?? e.error),);
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    } finally {
      probe.close();
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(t.pairingWaiting(browser.deviceName)),
          const SizedBox(height: 12),
          if (error == null)
            const LinearProgressIndicator(minHeight: 2)
          else
            ErrorLine(error!),
        ],
      ),
    );
  }
}

/// What to do with a scanned or pasted code: a pairing code signs in, a
/// set-up code from another device is gathered and applied.
Future<void> handleScannedCode(
  BuildContext context,
  String text,
  SetupCollector collector,
  VoidCallback onProgress,
) async {
  final t = L10n.of(context);
  final pairing = PairingCode.parse(text);
  if (pairing != null) {
    await showAccountScreen(
      context,
      title: t.scanCode,
      child: PairingScreen(code: pairing),
    );
    return;
  }
  if (collector.add(text)) {
    onProgress();
    final p = collector.payload;
    if (p != null && context.mounted) {
      await applySetupPayload(AppScope.read(context), p);
      onProgress();
    }
    return;
  }
  ScaffoldMessenger.maybeOf(context)
      ?.showSnackBar(SnackBar(content: Text(t.notAChurrCode)));
}

/// "Add a server → Copy from another device" and "Scan a code": scans,
/// then walks the person through signing in to each server in turn.
class ScanAndSetUpScreen extends StatefulWidget {
  const ScanAndSetUpScreen({super.key, this.camera = true});
  final bool camera;

  @override
  State<ScanAndSetUpScreen> createState() => _ScanAndSetUpScreenState();
}

class _ScanAndSetUpScreenState extends State<ScanAndSetUpScreen> {
  final collector = SetupCollector();

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final app = AppScope.of(context);
    final pending = pendingSetupServers(app);
    return ListView(
      children: [
        if (!collector.complete)
          SizedBox(
            height: 420,
            child: ScanCodeScreen(
              camera: widget.camera,
              onText: (text) => handleScannedCode(
                context,
                text,
                collector,
                () => setState(() {}),
              ),
            ),
          ),
        if (collector.total != null && !collector.complete)
          Padding(
            padding: const EdgeInsets.all(20),
            child: Text(t.setupReceived(collector.received, collector.total!)),
          ),
        if (collector.complete)
          Padding(
            padding: const EdgeInsets.all(20),
            child: Text(t.setupApplied, key: const Key('setup-applied')),
          ),
        if (pending.isNotEmpty) ...[
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20),
            child: SectionHead(t.setupServers),
          ),
          for (final a in pending)
            ListTile(
              key: Key('pending-$a'),
              title: Text(Uri.parse(a).host),
              subtitle: Text(a),
              trailing: Wrap(
                spacing: 4,
                children: [
                  TextButton(
                    onPressed: () =>
                        donePendingServer(app, a).then((_) => setState(() {})),
                    child: Text(t.skip),
                  ),
                  FilledButton(
                    onPressed: () async {
                      await Navigator.of(context).push(
                        MaterialPageRoute<void>(
                          builder: (_) => AddServerScreen(initialAddress: a),
                        ),
                      );
                      if (mounted) setState(() {});
                    },
                    child: Text(t.signInWithBrowser),
                  ),
                ],
              ),
            ),
        ],
      ],
    );
  }
}

// ---- this device's set-up code ----

/// "Set up another device": the QR code(s) holding the app preferences
/// and the server addresses, and nothing secret. An arrangement too
/// large for one code is shown as several, one after another.
class SetupCodeScreen extends StatefulWidget {
  const SetupCodeScreen({
    super.key,
    this.interval = const Duration(milliseconds: 1500),
  });
  final Duration interval;

  @override
  State<SetupCodeScreen> createState() => _SetupCodeScreenState();
}

class _SetupCodeScreenState extends State<SetupCodeScreen> {
  List<String> codes = const [];
  int index = 0;
  Timer? timer;

  @override
  void initState() {
    super.initState();
    setupPayloadOf(AppScope.read(context)).then((p) {
      if (!mounted) return;
      setState(() => codes = p.encode());
      if (codes.length > 1) {
        timer = Timer.periodic(widget.interval, (_) {
          if (mounted) setState(() => index = (index + 1) % codes.length);
        });
      }
    });
  }

  @override
  void dispose() {
    timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        Text(t.setUpNote),
        const SizedBox(height: 16),
        if (codes.isNotEmpty) ...[
          Center(
            child: Container(
              // Navy on the dark theme's cyan in both themes, so every
              // camera reads it (dark modules on a light ground).
              color: _qr.cyan,
              padding: const EdgeInsets.all(12),
              child: QrImageView(
                key: const Key('setup-qr'),
                data: codes[index],
                size: 280,
                eyeStyle: QrEyeStyle(
                  eyeShape: QrEyeShape.square,
                  color: _qr.onCyan,
                ),
                dataModuleStyle: QrDataModuleStyle(
                  dataModuleShape: QrDataModuleShape.square,
                  color: _qr.onCyan,
                ),
                semanticsLabel: t.setUpAnotherDevice,
              ),
            ),
          ),
          if (codes.length > 1)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                t.codePart(index + 1, codes.length),
                textAlign: TextAlign.center,
                style: monoStyle(context),
              ),
            ),
        ],
      ],
    );
  }
}

/// The set-up code's colours, the same in both themes.
final _qr = ChurrColors.resolve(dark: true);
