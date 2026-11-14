// The hand-over between sign-in and the sealed-chat code: sign-in makes
// the device's keys before it starts (so the box public key can go with
// the authorization or pairing request) and leaves here what came back,
// by server address; the sealed-chat code takes it when the server is
// added (docs/SEALED.md sections 13.3 and 13.7).

import 'session.dart';

final Map<String, SignInHandover> _pending = {};

String _key(Uri address) =>
    '${address.scheme}://${address.host}:${address.port}';

/// Sign-in for [address] finished with these keys and hand-over.
void leaveSignIn(Uri address, SignInHandover h) => _pending[_key(address)] = h;

/// What sign-in left for [address], once.
SignInHandover? takeSignIn(Uri address) => _pending.remove(_key(address));
