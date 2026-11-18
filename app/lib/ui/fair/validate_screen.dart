// "Validate result" in the app (docs/DESIGN-FAIR.md section 12), as the
// public page /validate of every instance and hub: the fields of a draw, or
// its one JSON document (docs/FAIR.md section 11), checked on the device by
// core/fair/validate.dart, with separate verdicts, each with its reason.
// Opened from a shield with the draw filled in, or empty for pasting. It asks
// nothing of any server unless a server is named, whose key history it then
// takes from this device's pin, or fetches from its well-known address.

import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;

import '../../core/fair/validate.dart';
import '../../platform/media_files.dart';
import '../../state/server_state.dart';
import '../theme/churr_icons.dart';
import '../theme/tokens.dart';

/// The Validate result view. [document] fills it in; [fetch] replaces the
/// fetch of a server's well-known document, for tests.
class ValidateScreen extends StatefulWidget {
  const ValidateScreen({super.key, this.server, this.document, this.fetch});
  final ServerState? server;
  final Map<String, Object?>? document;
  final Future<FairKeys> Function(String name)? fetch;

  @override
  State<ValidateScreen> createState() => ValidateScreenState();
}

class ValidateScreenState extends State<ValidateScreen> {
  final alpha = TextEditingController();
  final pk = TextEditingController();
  final proof = TextEditingController();
  final beta = TextEditingController();
  final objects = TextEditingController();
  final serverName = TextEditingController();
  final whole = TextEditingController();
  List<FairVerdict> verdicts = const [];
  String said = '';

  @override
  void initState() {
    super.initState();
    final d = widget.document;
    if (d != null) {
      fill(d);
      WidgetsBinding.instance.addPostFrameCallback((_) => check());
    }
  }

  @override
  void dispose() {
    for (final c in [alpha, pk, proof, beta, objects, serverName, whole]) {
      c.dispose();
    }
    super.dispose();
  }

  void fill(Map<String, Object?> d) {
    alpha.text = '${d['alpha'] ?? ''}';
    pk.text = '${d['pk'] ?? ''}';
    proof.text = '${d['proof'] ?? ''}';
    beta.text = '${d['beta'] ?? ''}';
    serverName.text = '${d['owner'] ?? ''}';
    objects.text = const JsonEncoder.withIndent(' ')
        .convert(d['objects'] ?? const []);
    whole.text = const JsonEncoder.withIndent(' ').convert(d);
  }

  /// The fields as the document, or null with why said.
  Map<String, Object?>? document() {
    Object? list = const [];
    if (objects.text.trim().isNotEmpty) {
      try {
        list = jsonDecode(objects.text);
      } on FormatException {
        setState(() => said = 'The primitives are not JSON.');
        return null;
      }
    }
    if (list is! List) {
      setState(() => said = 'The primitives are a list of objects.');
      return null;
    }
    return fairDocument(
      alpha: alpha.text.trim(),
      beta: beta.text.trim(),
      proof: proof.text.trim(),
      pk: pk.text.trim(),
      owner: serverName.text.trim(),
      objects: list,
    );
  }

  /// The history of the server named: this device's pin of it, or its
  /// published one fetched from its well-known address.
  Future<FairKeys?> keysOf(String name) async {
    if (name.isEmpty) return null;
    final pinned = widget.server?.vrfHistories.of(name);
    if (pinned != null && !pinned.changed) {
      return (name: name, history: pinned.keys.cast<Map>(), why: '');
    }
    if (widget.fetch != null) return widget.fetch!(name);
    if (!RegExp(r'^[a-z0-9.-]+(:[0-9]+)?$').hasMatch(name)) {
      return (name: name, history: null, why: "that is not a server's name.");
    }
    try {
      final r = await http
          .get(Uri.parse('https://$name/.well-known/churr'))
          .timeout(const Duration(seconds: 10));
      if (r.statusCode != 200) {
        return (
          name: name,
          history: null,
          why: '$name answered ${r.statusCode}.',
        );
      }
      final doc = jsonDecode(r.body);
      final keys = doc is Map ? doc['vrf_keys'] : null;
      if (keys is! List) {
        return (
          name: name,
          history: null,
          why: '$name publishes no VRF key history.',
        );
      }
      return (name: name, history: keys.cast<Map>(), why: '');
    } on Object {
      return (
        name: name,
        history: null,
        why: '$name could not be reached from here.',
      );
    }
  }

  Future<void> check() async {
    final d = document();
    if (d == null) return;
    whole.text = const JsonEncoder.withIndent(' ').convert(d);
    final keys = await keysOf(serverName.text.trim().toLowerCase());
    if (!mounted) return;
    setState(() {
      verdicts = validateFair(d, keys: keys);
      said = 'Checked on this device.';
    });
  }

  Future<void> copy() async {
    final d = document();
    if (d == null) return;
    await Clipboard.setData(
      ClipboardData(text: const JsonEncoder.withIndent(' ').convert(d)),
    );
    if (mounted) setState(() => said = 'The document is copied.');
  }

  Future<void> download() async {
    final d = document();
    if (d == null) return;
    final bytes = Uint8List.fromList(
      utf8.encode('${const JsonEncoder.withIndent(' ').convert(d)}\n'),
    );
    final where = await saveDownload(
      'churr-draw.json',
      bytes,
      'application/json',
    );
    if (mounted && where != null) setState(() => said = 'Saved: $where');
  }

  Future<void> copyLink() async {
    final d = document();
    if (d == null) return;
    final host = widget.server?.api.base.authority ?? '';
    final scheme = widget.server?.api.base.scheme ?? 'https';
    await Clipboard.setData(
      ClipboardData(text: '$scheme://$host/validate${fairFragment(d)}'),
    );
    if (mounted) {
      setState(() => said = 'A link with the draw after the # is copied.');
    }
  }

  void readWhole() {
    final r = readFairDocument(whole.text);
    if (r.doc == null) {
      setState(() => said = r.error ?? '');
      return;
    }
    fill(r.doc!);
    check();
  }

  static const _heads = {
    'proof': 'The proof',
    'beta': 'The output',
    'outcome': 'An outcome',
    'alpha': 'The input',
    'key': 'The key',
  };

  Widget _field(
    String label,
    TextEditingController c,
    String key, {
    int lines = 1,
  }) => Padding(
    padding: const EdgeInsets.only(bottom: 10),
    child: TextField(
      key: Key('validate-$key'),
      controller: c,
      minLines: lines,
      maxLines: lines == 1 ? 1 : lines + 4,
      autocorrect: false,
      enableSuggestions: false,
      style: TextStyle(fontFamily: fontMono, fontSize: 13),
      decoration: InputDecoration(
        labelText: label,
        border: const OutlineInputBorder(),
      ),
    ),
  );

  Widget _verdict(BuildContext context, FairVerdict v) {
    final c = ChurrColors.of(context);
    final (word, glyph, colour) = switch (v.ok) {
      true => ('Pass', ChurrIcons.check, c.cyan),
      false => ('Fail', ChurrIcons.warn, c.yellowInk),
      null => ('Not said', ChurrIcons.info, c.dim),
    };
    final head = v.id == 'outcome' && v.op != null
        ? 'Outcome of object ${v.i}, primitive ${(v.j ?? 0) + 1} (${v.op})'
        : _heads[v.id] ?? v.id;
    return Semantics(
      key: Key('verdict-${v.id}${v.i == null ? '' : '-${v.i}-${v.j}'}'),
      label: '$head: $word. ${v.why}',
      excludeSemantics: true,
      child: Container(
        margin: const EdgeInsets.only(bottom: 6),
        padding: const EdgeInsets.all(8),
        decoration: BoxDecoration(border: Border.all(color: colour)),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Glyph(glyph, size: 16, color: colour),
            const SizedBox(width: 8),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '$word · $head',
                    style: TextStyle(
                      fontWeight: FontWeight.w700,
                      color: colour,
                    ),
                  ),
                  Text(v.why),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Validate result')),
      body: SafeArea(
        child: ListView(
          key: const Key('validate'),
          padding: const EdgeInsets.all(16),
          children: [
            Text(
              'Checks a draw by a Churr server, on this device: the proof, its output, every outcome claimed, the input and whose key made it, each on its own.',
              style: TextStyle(color: c.dim),
            ),
            const SizedBox(height: 12),
            _field('Alpha (the input)', alpha, 'alpha'),
            _field('Public key (pk), base64url or hex', pk, 'pk'),
            _field('Proof, base64url or hex', proof, 'proof', lines: 2),
            _field('Beta (the output), base64url or hex', beta, 'beta'),
            _field(
              'Primitives asked, with the claimed results (JSON)',
              objects,
              'objects',
              lines: 4,
            ),
            _field(
              "Whose key: a server's name (optional)",
              serverName,
              'server',
            ),
            ExpansionTile(
              title: const Text('All of it as one JSON document'),
              children: [
                _field('The document', whole, 'document', lines: 6),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton(
                    onPressed: readWhole,
                    child: const Text('Read the document into the fields'),
                  ),
                ),
              ],
            ),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton(
                  key: const Key('validate-check'),
                  onPressed: check,
                  child: const Text('Check'),
                ),
                OutlinedButton(
                  onPressed: copy,
                  child: const Text('Copy the data'),
                ),
                OutlinedButton(
                  onPressed: download,
                  child: const Text('Download'),
                ),
                TextButton(
                  onPressed: copyLink,
                  child: const Text('Copy a link'),
                ),
              ],
            ),
            if (said.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(said, style: TextStyle(color: c.dim)),
              ),
            const SizedBox(height: 12),
            Text('Verdicts', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            if (verdicts.isEmpty)
              Text('Nothing checked yet.', style: TextStyle(color: c.dim)),
            for (final v in verdicts) _verdict(context, v),
            const SizedBox(height: 12),
            SelectableText(
              'How the draw is made and checked: $fairSpec. The proof checks with any ECVRF-SECP256K1-SHA256-TAI verifier, such as $fairLibrary; no Churr code is needed.',
              style: TextStyle(color: c.dim, fontSize: 12),
            ),
          ],
        ),
      ),
    );
  }
}
