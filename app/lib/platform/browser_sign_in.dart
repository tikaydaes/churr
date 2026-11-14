import 'dart:async';
import 'dart:io';

import 'package:flutter_web_auth_2/flutter_web_auth_2.dart';
import 'package:url_launcher/url_launcher.dart';

import '../core/api/client.dart';
import '../core/api/errors.dart';
import '../core/oauth.dart';
import '../core/oauth_metadata.dart';
import '../core/sealed/sealed.dart' show b64, unb64;
import '../core/sealed_client/pending.dart';
import '../core/sealed_client/session.dart';
import '../core/session_store.dart';

/// Whether this build runs on a desktop target, which signs in through a
/// loopback redirect; phones use the apps' own address scheme.
bool get isDesktop =>
    Platform.isMacOS || Platform.isWindows || Platform.isLinux;

/// The name the person approves in the browser ("Sign in Kai's laptop").
String get deviceName {
  try {
    return Platform.localHostname;
  } on Object {
    return 'Churr app';
  }
}

/// Signs this device in to the server behind [api] with OAuth
/// authorization code and PKCE in the system browser (docs/AUTH.md
/// section 11.2), and returns the first token pair.
///
/// The redirect takes the two forms the server accepts and no other:
///
/// - **Desktop:** `http://127.0.0.1:<port>/callback`, a one-off listener
///   on a free loopback port. The address is by number: the server
///   refuses the name `localhost`.
/// - **Phones:** `churr://oauth`, returned through the system's sign-in
///   sheet (ASWebAuthenticationSession, Custom Tabs).
///
/// The app never sees a passkey or a provider: whatever the server's
/// sign-in page asks for happens in the browser.
Future<Tokens> signInWithBrowser(ApiClient api) async {
  // The endpoints come from the server's metadata, not fixed paths
  // (docs/AUTH.md 11.1). This also points `api`'s token requests there.
  final meta = await fetchOAuthMetadata(api);
  final pkce = Pkce.generate();
  // Sealed chats: the device's keys are made before sign-in, so a passkey
  // sign-in can hand this app the backup key (docs/SEALED.md 13.3).
  final keys = PendingDeviceKeys.generate();
  final Uri result;
  final String redirect;

  if (isDesktop) {
    final listener = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    redirect = desktopRedirect(listener.port);
    try {
      final url = api.authorizeUrl(
        pkce: pkce,
        redirectUri: redirect,
        deviceName: deviceName,
        platform: Platform.operatingSystem,
        endpoint: meta.authorizationEndpoint,
        boxKey: b64(keys.boxPublic),
      );
      if (!await launchUrl(url, mode: LaunchMode.externalApplication)) {
        throw OAuthException('invalid_request', 'Could not open your browser.');
      }
      final request = await listener.first.timeout(
        const Duration(minutes: 5),
        onTimeout: () => throw OAuthException(
          'invalid_request',
          'Sign-in was not completed in time.',
        ),
      );
      result = request.uri;
      request.response
        ..headers.contentType = ContentType.html
        ..write(
          '<!doctype html><meta charset="utf-8"><title>Churr</title>'
          '<body style="font-family:system-ui;background:#0d1026;color:#efeaff;'
          'padding:48px;text-align:center">'
          '<h1>You can close this tab</h1><p>Return to the Churr app.</p>',
        );
      await request.response.close();
    } finally {
      await listener.close(force: true);
    }
  } else {
    redirect = phoneRedirect;
    final url = api.authorizeUrl(
      pkce: pkce,
      redirectUri: redirect,
      deviceName: deviceName,
      platform: Platform.operatingSystem,
      endpoint: meta.authorizationEndpoint,
      boxKey: b64(keys.boxPublic),
    );
    final String callback;
    try {
      callback = await FlutterWebAuth2.authenticate(
        url: url.toString(),
        callbackUrlScheme: appScheme,
      );
    } on Exception {
      throw OAuthException('access_denied', 'Sign-in was cancelled.');
    }
    result = Uri.parse(callback);
  }

  final code = api.codeFromRedirect(result, pkce);
  final tokens = await api.exchangeCode(
    code: code,
    pkce: pkce,
    redirectUri: redirect,
  );
  leaveSignIn(
    api.base,
    SignInHandover(
      keys: keys,
      handover: unb64(tokens.handover),
      codeChallenge: pkce.challenge,
    ),
  );
  return tokens;
}
