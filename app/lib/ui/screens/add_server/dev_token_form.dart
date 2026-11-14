import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/oauth.dart';
import '../../../core/session_store.dart';
import '../../scopes.dart';
import '../../theme/shapes.dart';
import '../../theme/tokens.dart';

/// Development only: adds a server with tokens printed by
/// `tools/devseed`, so the app can be tried and tested without a
/// browser sign-in. `AddServerScreen` shows it only under `kDebugMode`,
/// which removes it from release builds.
class DevTokenForm extends StatefulWidget {
  const DevTokenForm({super.key, required this.first});
  final bool first;

  @override
  State<DevTokenForm> createState() => _DevTokenFormState();
}

class _DevTokenFormState extends State<DevTokenForm> {
  final address = TextEditingController(text: 'localhost:8472');
  final access = TextEditingController();
  final refresh = TextEditingController();
  String? error;
  bool busy = false;

  @override
  void dispose() {
    address.dispose();
    access.dispose();
    refresh.dispose();
    super.dispose();
  }

  Future<void> _add() async {
    final app = AppScope.read(context);
    final u = normalizeServer(address.text);
    if (u == null || access.text.trim().isEmpty) {
      setState(() => error = 'An address and an access token are needed.');
      return;
    }
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await app.addServer(
        u,
        Tokens(
          access: access.text.trim(),
          refresh: refresh.text.trim(),
          // devseed's access tokens last an hour from when they were
          // made. Treating one as nearly spent makes the client rotate
          // at its first request when a refresh token was given, which
          // also exercises refresh.
          expiresAt: DateTime.now().add(const Duration(minutes: 50)),
        ),
      );
      if (mounted && !widget.first) Navigator.of(context).pop();
    } on ApiException catch (e) {
      error = e.message;
    } on Exception catch (e) {
      error = '$e';
    }
    if (mounted) setState(() => busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    // Development only, and so provisional: dashed, on a panel.
    return Padding(
      padding: const EdgeInsets.only(top: 32),
      child: DashedBox(
      color: c.yellowInk,
      child: Container(
      color: c.panel,
      padding: const EdgeInsets.all(14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            'DEVELOPMENT BUILD · ADD WITH A DEVSEED TOKEN',
            style: monoStyle(context, size: 10, color: c.yellowInk),
          ),
          const SizedBox(height: 8),
          TextField(
            key: const Key('dev-address'),
            controller: address,
            autocorrect: false,
            decoration: const InputDecoration(labelText: 'Address'),
          ),
          const SizedBox(height: 8),
          TextField(
            key: const Key('dev-access'),
            controller: access,
            autocorrect: false,
            decoration: const InputDecoration(labelText: 'access_token'),
          ),
          const SizedBox(height: 8),
          TextField(
            key: const Key('dev-refresh'),
            controller: refresh,
            autocorrect: false,
            decoration: const InputDecoration(
              labelText: 'refresh_token (optional)',
            ),
          ),
          const SizedBox(height: 8),
          OutlinedButton(
            key: const Key('dev-add'),
            onPressed: busy ? null : _add,
            child: const Text('Add with token'),
          ),
          if (error != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                error!,
                key: const Key('dev-error'),
                style: TextStyle(color: c.pink),
              ),
            ),
        ],
      ),
    ),
    ),
    );
  }
}
