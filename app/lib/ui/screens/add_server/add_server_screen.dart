import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../core/api/client.dart';
import '../../../core/api/errors.dart';
import '../../../core/api/me.dart';
import '../../../core/models/server.dart';
import '../../../core/oauth.dart';
import '../../../core/oauth_metadata.dart';
import '../../../core/session_store.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/browser_sign_in.dart';
import '../../../state/device_setup.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/shapes.dart';
import '../../theme/churr_icons.dart';
import '../account/common.dart';
import '../account/sign_in_screens.dart';
import 'dev_token_form.dart';

/// "Add a server" (docs/CLIENTS.md 8.10 and 9.22), three ways that end
/// in the same sign-in:
///
/// - **Address.** The app calls `getServer` there and shows its name
///   before anything else; then "Sign in" opens the server's own sign-in
///   page in the system browser (OAuth with PKCE, endpoints from the
///   server's metadata).
/// - **Invite link.** A `/join/<code>` link opens that server's join page
///   in the browser; the person then signs in here as usual (the browser
///   already holds the new session, so it only asks to approve).
/// - **QR code.** A server's pairing code from a signed-in web page, or
///   another device's set-up code (8.2): "Scan a code" and "Copy from
///   another device".
class AddServerScreen extends StatefulWidget {
  const AddServerScreen({
    super.key,
    this.signIn = signInWithBrowser,
    this.first = false,
    this.initialAddress,
  });

  /// Replaced in tests, which have no browser.
  final Future<Tokens> Function(ApiClient api) signIn;

  /// True when no server is signed in yet: there is nowhere to go back.
  final bool first;

  /// An address to look up at once (a server from a set-up code).
  final String? initialAddress;

  @override
  State<AddServerScreen> createState() => _AddServerScreenState();
}

class _AddServerScreenState extends State<AddServerScreen> {
  final address = TextEditingController();
  Uri? server;
  Uri? invite;
  ServerInfo? info;
  String? error;
  bool busy = false;

  @override
  void initState() {
    super.initState();
    if (widget.initialAddress != null) {
      address.text = widget.initialAddress!;
      WidgetsBinding.instance.addPostFrameCallback((_) => _lookUp());
    }
  }

  @override
  void dispose() {
    address.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() action) async {
    final t = L10n.of(context);
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await action();
    } on ApiException catch (e) {
      error = e.isNetwork ? t.couldNotReach : e.message;
    } on OAuthException catch (e) {
      error = e.description ?? t.signInDidNotFinish(e.error);
    } on Exception catch (e) {
      error = '$e';
    }
    if (mounted) setState(() => busy = false);
  }

  /// An invite link is a server address with `/join/<code>`.
  static Uri? inviteOf(String text) {
    final u = Uri.tryParse(text.trim());
    if (u == null || !u.hasScheme || u.host.isEmpty) return null;
    final seg = u.pathSegments;
    return seg.length >= 2 && seg[0] == 'join' ? u : null;
  }

  Future<void> _lookUp() => _run(() async {
    final t = L10n.of(context);
    invite = inviteOf(address.text);
    final u = normalizeServer(
      invite == null
          ? address.text
          : invite!.replace(path: '', query: '').toString(),
    );
    if (u == null) {
      error = t.enterAddress;
      return;
    }
    final api = AppScope.read(context).probe(u);
    try {
      info = await api.getServer();
      server = u;
    } finally {
      api.close();
    }
  });

  Future<void> _signIn() => _run(() async {
    final app = AppScope.read(context);
    final nav = Navigator.of(context);
    final api = app.probe(server!);
    try {
      final tokens = await widget.signIn(api);
      final s = await app.addServer(server!, tokens);
      // The new server's token requests use its metadata's endpoints.
      if (api.tokenEndpoint != null) {
        try {
          final meta = await fetchOAuthMetadata(s.api);
          await storeOAuthMetadata(s.db, meta);
        } on Object {
          // Fetched again at the next start.
        }
      }
      await donePendingServer(app, server.toString());
    } finally {
      api.close();
    }
    if (mounted && !widget.first) nav.pop();
  });

  void _scan({required bool setUp}) {
    final t = L10n.of(context);
    showAccountScreen(
      context,
      title: setUp ? t.copyFromDevice : t.scanCode,
      child: const ScanAndSetUpScreen(),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return Scaffold(
      appBar: widget.first ? null : AppBar(title: Text(t.addAServer)),
      // The dot grid behind the empty space of sign-in; the form itself
      // sits on a panel so no text is read over the dots.
      body: DotGrid(
        child: Center(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (widget.first) ...[
                  // The glitched wordmark: one of the three places the
                  // glitch is allowed.
                  const Align(alignment: Alignment.centerLeft, child: Wordmark(size: 72)),
                  const SizedBox(height: 16),
                ],
                Container(
                  color: c.panel,
                  padding: const EdgeInsets.all(20),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                if (widget.first) ...[
                  Text(t.addServerToBegin, style: TextStyle(color: c.dim, fontSize: 15)),
                  const SizedBox(height: 16),
                ],
                TextField(
                  key: const Key('server-address'),
                  controller: address,
                  autocorrect: false,
                  keyboardType: TextInputType.url,
                  decoration: InputDecoration(
                    labelText: t.serverAddress,
                    hintText: t.serverAddressHint,
                  ),
                  onSubmitted: (_) => _lookUp(),
                ),
                const SizedBox(height: 12),
                if (info == null)
                  FilledButton(
                    key: const Key('look-up'),
                    onPressed: busy ? null : _lookUp,
                    child: Text(t.continueButton),
                  )
                else ...[
                  Container(
                    decoration: BoxDecoration(
                      color: c.panel,
                    ),
                    padding: const EdgeInsets.all(12),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          info!.communityName.isEmpty
                              ? info!.name
                              : info!.communityName,
                          style: const TextStyle(
                            fontSize: 18,
                            fontWeight: FontWeight.w800,
                          ),
                        ),
                        Text(
                          t.serverVersion(server!.host, info!.version),
                          style: monoStyle(context),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 12),
                  if (invite != null) ...[
                    Text(t.inviteJoinNote),
                    const SizedBox(height: 8),
                    OutlinedButton(
                      key: const Key('join-in-browser'),
                      onPressed: () => launchUrl(
                        invite!,
                        mode: LaunchMode.externalApplication,
                      ),
                      child: Text(t.joinInBrowser),
                    ),
                    const SizedBox(height: 8),
                  ],
                  FilledButton(
                    key: const Key('sign-in'),
                    onPressed: busy || info!.setupNeeded ? null : _signIn,
                    child: Text(t.signInWithBrowser),
                  ),
                  if (info!.setupNeeded)
                    Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Text(t.notSetUp, style: TextStyle(color: c.dim)),
                    ),
                ],
                if (busy)
                  const Padding(
                    padding: EdgeInsets.only(top: 12),
                    child: LinearProgressIndicator(minHeight: 2),
                  ),
                if (error != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Glyph(ChurrIcons.error, size: 16, color: c.pink),
                        const SizedBox(width: 6),
                        Expanded(
                          child: Text(
                            error!,
                            key: const Key('add-server-error'),
                            style: TextStyle(color: c.pink),
                          ),
                        ),
                      ],
                    ),
                  ),
                    ],
                  ),
                ),
                const SizedBox(height: 20),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  alignment: WrapAlignment.center,
                  children: [
                    if (canScan)
                      TextButton.icon(
                        key: const Key('scan-code'),
                        onPressed: () => _scan(setUp: false),
                        icon: const Glyph(ChurrIcons.scan, size: 16),
                        label: Text(t.scanCode),
                      ),
                    TextButton.icon(
                      key: const Key('copy-from-device'),
                      onPressed: () => _scan(setUp: true),
                      icon: const Glyph(ChurrIcons.devices, size: 16),
                      label: Text(t.copyFromDevice),
                    ),
                  ],
                ),
                // Compiled out of release builds: `kDebugMode` is a
                // constant false there, so the form and everything only
                // it uses are removed by the compiler.
                if (kDebugMode) DevTokenForm(first: widget.first),
              ],
            ),
          ),
        ),
        ),
      ),
    );
  }
}
