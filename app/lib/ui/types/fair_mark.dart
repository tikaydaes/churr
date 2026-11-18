// The shield (docs/DESIGN-FAIR.md section 13), as the web chat's
// shared/fair/shield.js: the one mark of a draw's proof, drawn from this
// device's OWN check of it (core/fair/verify.dart), never from what a card
// or a value says. A card's `fair_mark` names the outcomes of its message's
// draw it stands on and carries no verdict; a built-in chance kind shows one
// by default for every value of the message that took chance.
//
//   verified   a shield with a tick, in green (ChurrColors.green, a colour
//              only the verified shield uses)
//   unknown    a shield with a question mark, neutral: "Cannot be checked
//              here", the drawing server's key history was never given
//   failed     a warning triangle, in the warning tone: "Not verified"
//
// Each has its own shape and spoken label, so colour is never the only
// sign. Tapped, it opens Validate result with the draw filled in. No other
// widget draws these glyphs.

import 'package:flutter/material.dart';

import '../../core/fair/validate.dart';
import '../../core/fair/verify.dart';
import '../../core/models/message.dart';
import '../../state/server_state.dart';
import '../fair/validate_screen.dart';
import '../theme/churr_icons.dart';
import '../theme/tokens.dart';

/// What a shield says.
const shieldWords = {
  FairState.verified: 'Verified',
  FairState.failed: 'Not verified',
  FairState.unknown: 'Cannot be checked here',
};
const _says = {
  FairState.verified:
      "The server's proof of this result checks out on this device: the "
      'proof, its output and every outcome shown. Nobody, that server '
      'included, could have made it come out otherwise.',
  FairState.failed:
      'This result could not be checked, so it may not be what the server '
      'drew. It is shown as it arrived.',
  FairState.unknown:
      'This result was drawn by a server whose key history this app has not '
      'been given, so its proof cannot be checked here.',
};

final _checked = <String, FairCheck>{};

/// The check of [m]'s draw against what [server] holds, made once for each
/// state of the pins.
FairCheck fairCheckOf(ServerState server, Message m) {
  final shared = server.conversation(m.conversationId)?.raw['shared'];
  final histories = server.vrfHistories..showShared(shared);
  final key =
      '${m.id}|${(m.raw['fair'] as Map?)?['proof']}|${histories.version}';
  return _checked[key] ??= verifyMessage(
    m.raw,
    keysOf: histories.of,
    server: server.info?.name ?? '',
  );
}

/// What a shield standing on [objects] of a draw says, from the message's
/// own check [res]: none named is every value that took chance.
({FairState state, String why}) judgeShield(FairCheck res, List<int>? objects) {
  if (res.state == FairState.none) {
    return (
      state: FairState.failed,
      why: 'It stands on a draw this message does not carry.',
    );
  }
  if (res.state != FairState.verified) return (state: res.state, why: res.why);
  if (objects != null && objects.isNotEmpty) {
    for (final i in objects) {
      final o = res.objects.where((x) => x.i == i).firstOrNull;
      if (o == null) {
        return (
          state: FairState.failed,
          why:
              'It names outcome $i of the draw, which nothing in this message took.',
        );
      }
      if (!o.ok) {
        return (
          state: FairState.failed,
          why: 'An outcome it stands on is not the one the proof gives.',
        );
      }
      if (!o.known) {
        return (
          state: FairState.unknown,
          why: 'An outcome it stands on is of a kind this app cannot work out, so it is proved but not checked here.',
        );
      }
    }
  } else if (res.objects.isEmpty) {
    return (
      state: FairState.failed,
      why: 'Nothing in this message took chance from its draw.',
    );
  } else if (res.objects.any((o) => !o.known)) {
    return (
      state: FairState.unknown,
      why: 'An outcome here is of a kind this app cannot work out, so it is proved but not checked here.',
    );
  }
  return (state: FairState.verified, why: _says[FairState.verified]!);
}

/// The shield of a message's draw: [objects] are what a card's fair_mark
/// names, or null for a built-in kind's (every value that took chance).
class FairShield extends StatelessWidget {
  const FairShield({
    super.key,
    required this.server,
    required this.message,
    this.objects,
  });
  final ServerState server;
  final Message message;
  final List<int>? objects;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final j = judgeShield(fairCheckOf(server, message), objects);
    final (glyph, colour) = switch (j.state) {
      FairState.verified => (ChurrIcons.shieldCheck, c.green),
      FairState.unknown => (ChurrIcons.shieldUnknown, c.dim),
      _ => (ChurrIcons.warn, c.yellowInk),
    };
    final word = shieldWords[j.state] ?? shieldWords[FairState.failed]!;
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Semantics(
        button: true,
        label: '$word. ${j.why}',
        excludeSemantics: true,
        child: Tooltip(
          message: j.why,
          child: InkWell(
            key: Key('fair-shield-tap'),
            onTap: () => Navigator.of(context).push(
              MaterialPageRoute<void>(
                builder: (_) => ValidateScreen(
                  server: server,
                  document: exportFairMessage(message.raw, objects),
                ),
              ),
            ),
            child: Container(
              key: Key('fair-shield-${j.state.name}'),
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
              decoration: BoxDecoration(border: Border.all(color: colour)),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Glyph(glyph, size: 13, color: colour),
                  const SizedBox(width: 4),
                  Text(word, style: TextStyle(fontSize: 12, color: colour)),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
