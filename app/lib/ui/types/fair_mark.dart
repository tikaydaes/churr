// The mark under a message whose chance was drawn (docs/FAIR.md), as the web
// chat's shared/fair/mark.js: "Verified", or plainly "Not verified" with why.
// Tapped, it shows the proof itself, so anyone can check it again elsewhere.
// The roll stays shown whatever the check says. The check is made on the
// device with nothing asked of the server (core/fair/verify.dart), once per
// message and proof.

import 'package:flutter/material.dart';

import '../../core/fair/verify.dart';
import '../../core/models/message.dart';
import '../../state/server_state.dart';
import '../theme/churr_icons.dart';
import '../theme/tokens.dart';

const _verified = 'Verified';
const _failed = 'Not verified';
const _saysVerified =
    "Every die shown here is the one the server's proof gives. The proof was "
    'made with the key this server published, for this place in the '
    'conversation, and nobody, the server included, could have made it come '
    'out otherwise.';

// The block's fields, in the order the proof is read.
const _fields = [
  ('owner', 'Drawn by'),
  ('n', 'Draw number'),
  ('derive', 'Rule'),
  ('pk', 'Key'),
  ('alpha', 'Proved over (alpha)'),
  ('proof', 'Proof'),
  ('beta', 'Output (beta)'),
  ('prev', 'Previous proof (hash)'),
];

final _checked = <String, FairCheck>{};

/// The check of [m]'s draw against what [server] holds, made once.
FairCheck fairCheckOf(ServerState server, Message m) {
  final key = '${m.id}|${(m.raw['fair'] as Map?)?['proof']}';
  return _checked[key] ??= () {
    final conv = server.conversation(m.conversationId);
    final shared = conv?.raw['shared'];
    return verifyMessage(
      m.raw,
      server: server.info?.name ?? '',
      keys: server.vrfPinned,
      chains: [
        m.conversationId,
        if (shared is Map && shared['stream_id'] is String)
          shared['stream_id'] as String,
      ],
    );
  }();
}

/// The mark for a message with a `fair` block.
class FairMark extends StatelessWidget {
  const FairMark({super.key, required this.server, required this.message});
  final ServerState server;
  final Message message;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final res = fairCheckOf(server, message);
    final ok = res.state == FairState.verified;
    final colour = ok ? c.cyan : c.yellowInk;
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Semantics(
        button: true,
        label: ok ? 'Roll verified' : 'Roll not verified: ${res.why}',
        child: Tooltip(
          message: ok ? _saysVerified : res.why,
          child: InkWell(
            key: Key('fair-mark-${message.id}'),
            onTap: () => showFairProof(context, res),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
              decoration: BoxDecoration(border: Border.all(color: colour)),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Glyph(
                    ok ? ChurrIcons.check : ChurrIcons.info,
                    size: 12,
                    color: colour,
                  ),
                  const SizedBox(width: 4),
                  Text(
                    ok ? _verified : _failed,
                    style: TextStyle(fontSize: 12, color: colour),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Shows the proof in full, with what the check found.
Future<void> showFairProof(BuildContext context, FairCheck res) {
  final ok = res.state == FairState.verified;
  return showDialog<void>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) {
      final c = ChurrColors.of(context);
      return Dialog(
        child: SizedBox(
          width: 480,
          child: SingleChildScrollView(
            key: const Key('fair-proof'),
            padding: const EdgeInsets.all(16),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'The proof of this roll',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 8),
                Text(
                  '${ok ? _verified : _failed}: ${ok ? _saysVerified : res.why}',
                  style: TextStyle(color: ok ? c.ink : c.yellowInk),
                ),
                const SizedBox(height: 10),
                for (final (k, label) in _fields) ...[
                  Text(label, style: TextStyle(color: c.dim, fontSize: 12)),
                  SelectableText(
                    '${res.block[k] ?? ''}',
                    style: TextStyle(fontFamily: fontMono, fontSize: 12),
                  ),
                  const SizedBox(height: 4),
                ],
                const SizedBox(height: 6),
                Text(
                  'docs/FAIR.md says how to check it yourself.',
                  style: TextStyle(color: c.dim, fontSize: 12),
                ),
              ],
            ),
          ),
        ),
      );
    },
  );
}
