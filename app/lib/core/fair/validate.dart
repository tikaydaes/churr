// "Validate result" in the app (docs/DESIGN-FAIR.md section 12; the
// document is docs/FAIR.md section 11), as the web chat's
// shared/fair/validate.js: a draw's alpha, beta, proof and public key, and
// the primitives asked of it with the outcome claimed, checked on the device,
// each part on its own and each with its reason: the proof, beta, each
// primitive's result, the alpha's shape, and whose key it is. Also the
// document a message's draw makes (the shield's), and its link, the
// document in the fragment. Plain Dart.

import 'dart:convert';
import 'dart:typed_data';

import '../types/catalogue.dart';
import 'derive.dart';
import 'ecvrf.dart' as ecvrf;
import 'kinds.dart';

const fairDocFormat = 1;
const fairSuite = 'ECVRF-SECP256K1-SHA256-TAI';
const fairSpec = 'https://github.com/tikaydaes/churr/blob/main/docs/FAIR.md';
const fairLibrary = 'https://github.com/atikayda/ecvrf';

String _b64(List<int> b) => base64Url.encode(b).replaceAll('=', '');

/// Bytes from base64url (padding allowed) or hex, or null.
Uint8List? fairBytes(Object? s) {
  if (s is! String) return null;
  final t = s.trim();
  if (t.length >= 64 &&
      t.length.isEven &&
      RegExp(r'^[0-9a-fA-F]+$').hasMatch(t)) {
    return Uint8List.fromList([
      for (var i = 0; i < t.length; i += 2)
        int.parse(t.substring(i, i + 2), radix: 16),
    ]);
  }
  try {
    return base64Url.decode(
      base64Url.normalize(
        t.replaceAll('=', '').replaceAll('+', '-').replaceAll('/', '_'),
      ),
    );
  } on FormatException {
    return null;
  }
}

final _alpha = RegExp(
  r'^(0|[1-9][0-9]{0,15})\|([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$',
);

/// An alpha read: its second and nonce, or null when it is not
/// `<unix seconds>|<nonce>`.
({int t, String nonce})? readAlpha(String alpha) {
  final m = _alpha.firstMatch(alpha);
  if (m == null) return null;
  return (t: int.parse(m.group(1)!), nonce: m.group(2)!);
}

/// A result or primitive in one spelling: keys in order, and nothing empty,
/// 0 or false (FAIR.md section 4: a missing number is 0).
Object? fairCanonical(Object? v) {
  if (v is List) return [for (final x in v) fairCanonical(x)];
  if (v is Map) {
    final keys = v.keys.map((k) => '$k').toList()..sort();
    return {
      for (final k in keys)
        if (!(v[k] == null ||
            v[k] == false ||
            v[k] == 0 ||
            (v[k] is List && (v[k] as List).isEmpty)))
          k: fairCanonical(v[k]),
    };
  }
  return v;
}

bool _same(Object? a, Object? b) =>
    jsonEncode(fairCanonical(a)) == jsonEncode(fairCanonical(b));

bool _sameRows(List got, Object? want) {
  if (want is! List || got.length != want.length) return false;
  for (var i = 0; i < got.length; i++) {
    final r = got[i] as Map, w = want[i];
    if (w is! Map || r['total'] != w['total']) return false;
    final rt = r['terms'] as List, wt = w['terms'];
    if (wt is! List || rt.length != wt.length) return false;
    for (var j = 0; j < rt.length; j++) {
      final x = rt[j] as Map, y = wt[j] as Map;
      if (x['value'] != y['value'] ||
          !_same(x['rolls'] ?? [], y['rolls'] ?? []) ||
          !_same(x['dropped'] ?? [], y['dropped'] ?? [])) {
        return false;
      }
    }
  }
  return true;
}

/// The document from its fields.
Map<String, Object?> fairDocument({
  required String alpha,
  required String beta,
  required String proof,
  required String pk,
  String? owner,
  List<Object?> objects = const [],
}) => {
  'churr_fair': fairDocFormat,
  'suite': fairSuite,
  'derive': 1,
  if (owner != null && owner.isNotEmpty) 'owner': owner,
  'pk': pk,
  'alpha': alpha,
  'beta': beta,
  'proof': proof,
  'objects': objects,
  'spec': fairSpec,
  'library': fairLibrary,
};

/// Reads pasted text as the document: the document, or the reason it is not
/// one.
({Map<String, Object?>? doc, String? error}) readFairDocument(String text) {
  Object? doc;
  try {
    doc = jsonDecode(text);
  } on FormatException {
    return (doc: null, error: 'This is not JSON.');
  }
  if (doc is! Map) {
    return (
      doc: null,
      error: "This is not a draw's document: it is not a JSON object.",
    );
  }
  final d = doc.cast<String, Object?>();
  if (d['churr_fair'] != null && d['churr_fair'] != fairDocFormat) {
    return (
      doc: null,
      error:
          'This document is in layout ${d['churr_fair']}, which this app does not know.',
    );
  }
  for (final k in ['alpha', 'beta', 'proof', 'pk']) {
    if (d[k] is! String) return (doc: null, error: 'The document has no $k.');
  }
  if (d['objects'] != null && d['objects'] is! List) {
    return (doc: null, error: "The document's objects are not a list.");
  }
  return (doc: {...d, 'objects': d['objects'] ?? <Object?>[]}, error: null);
}

/// The document in a link's fragment, and back.
String fairFragment(Map<String, Object?> doc) =>
    '#${_b64(utf8.encode(jsonEncode(doc)))}';
Map<String, Object?>? fairFromFragment(String hash) {
  final raw = fairBytes(hash.replaceFirst('#', ''));
  if (raw == null || raw.isEmpty) return null;
  try {
    return readFairDocument(utf8.decode(raw)).doc;
  } on FormatException {
    return null;
  }
}

/// One verdict: [id] is proof, beta, outcome, alpha or key; [ok] is true,
/// false, or null where nothing could be said (a key not attributed); an
/// outcome's also names its object and primitive.
class FairVerdict {
  const FairVerdict(
    this.id,
    this.ok,
    this.why, {
    this.i,
    this.j,
    this.op,
    this.time,
  });
  final String id;
  final bool? ok;
  final String why;
  final int? i, j;
  final String? op;
  final DateTime? time;
}

/// A server's published VRF key history, or why it could not be had.
typedef FairKeys = ({String name, List<Map>? history, String why});

Map? _inForce(List<Map> history, int t) {
  Map? out;
  for (final e in history) {
    final began = DateTime.tryParse('${e['began']}');
    if (began != null && began.millisecondsSinceEpoch <= t * 1000) out = e;
  }
  return out;
}

/// Checks a document, as the web chat's validate does.
List<FairVerdict> validateFair(Map<String, Object?> doc, {FairKeys? keys}) {
  final out = <FairVerdict>[];
  final pk = fairBytes(doc['pk']),
      proof = fairBytes(doc['proof']),
      beta = fairBytes(doc['beta']);
  final alphaText = doc['alpha'] is String ? doc['alpha'] as String : '';
  Uint8List? output;
  if (pk == null || pk.length != 33) {
    out.add(
      const FairVerdict(
        'proof',
        false,
        'The public key is not 33 bytes of base64url or hex.',
      ),
    );
  } else if (proof == null || proof.length != 81) {
    out.add(
      const FairVerdict(
        'proof',
        false,
        'The proof is not 81 bytes of base64url or hex.',
      ),
    );
  } else {
    ecvrf.VerifyResult? r;
    try {
      r = ecvrf.verify(pk, proof, Uint8List.fromList(utf8.encode(alphaText)));
    } on Object {
      r = null;
    }
    if (r != null && r.valid) {
      output = r.beta;
      out.add(
        const FairVerdict(
          'proof',
          true,
          'The proof is valid for this public key and alpha (ECVRF-SECP256K1-SHA256-TAI).',
        ),
      );
    } else {
      out.add(
        const FairVerdict(
          'proof',
          false,
          'The proof is not valid for this public key and alpha: it was made with another key, over another alpha, or changed.',
        ),
      );
    }
  }
  if (beta == null || beta.length != 32) {
    out.add(
      const FairVerdict(
        'beta',
        false,
        'Beta is not 32 bytes of base64url or hex.',
      ),
    );
  } else {
    Uint8List? hashed = output;
    if (hashed == null && proof != null && proof.length == 81) {
      try {
        hashed = ecvrf.proofToHash(proof);
      } on Object {
        hashed = null;
      }
    }
    final ok = hashed != null && _same(hashed.toList(), beta.toList());
    out.add(
      FairVerdict(
        'beta',
        ok,
        ok
            ? 'Beta is the output of this proof.'
            : hashed == null
            ? 'The proof cannot be read, so its output cannot be worked out.'
            : 'Beta is not the output of this proof.',
      ),
    );
  }
  final objects = (doc['objects'] as List?) ?? const [];
  if (doc['derive'] != null && doc['derive'] != 1) {
    out.add(
      FairVerdict(
        'outcome',
        false,
        'The outcomes are by rule derive ${doc['derive']}, which this app does not know.',
      ),
    );
  } else if (beta != null && beta.length == 32) {
    var any = false;
    for (final o in objects) {
      final i = o is Map ? o['i'] : null;
      if (i is! int || i < 0) {
        out.add(
          const FairVerdict(
            'outcome',
            false,
            'An object has no index i, so its stream is not known.',
          ),
        );
        continue;
      }
      final prims = (o as Map)['primitives'] is List
          ? o['primitives'] as List
          : const [];
      final results = o['results'] is List ? o['results'] as List : const [];
      final s = FairStream(beta, i);
      var broken = false;
      for (var j = 0; j < prims.length; j++) {
        any = true;
        final p = prims[j];
        final op = p is Map ? '${p['op']}' : null;
        if (broken) {
          out.add(
            FairVerdict(
              'outcome',
              false,
              "Not checked: an earlier primitive of this object could not be drawn, so the stream's place is lost.",
              i: i,
              j: j,
              op: op,
            ),
          );
          continue;
        }
        if (p is! Map || !isPrimitive(p.cast<String, dynamic>())) {
          broken = true;
          out.add(
            FairVerdict(
              'outcome',
              false,
              'This is not a primitive of derive 1, or is outside its limits.',
              i: i,
              j: j,
              op: op,
            ),
          );
          continue;
        }
        final got = drawPrimitive(s, p.cast<String, dynamic>());
        final want = j < results.length ? results[j] : null;
        final ok =
            want != null &&
            (op == 'dice'
                ? _sameRows(got['rows'] as List, (want as Map)['rows'])
                : _same(got, want));
        out.add(
          FairVerdict(
            'outcome',
            ok,
            ok
                ? 'Re-derived from beta, it gives the claimed result.'
                : want == null
                ? 'No result is claimed for it.'
                : 'Re-derived from beta, it gives ${jsonEncode(fairCanonical(got))}, not the claimed ${jsonEncode(fairCanonical(want))}.',
            i: i,
            j: j,
            op: op,
          ),
        );
      }
      if (results.length > prims.length) {
        out.add(
          FairVerdict(
            'outcome',
            false,
            'More results are claimed than primitives asked.',
            i: i,
            j: prims.length,
          ),
        );
      }
    }
    if (!any) {
      out.add(
        const FairVerdict(
          'outcome',
          null,
          'No primitive was given, so no outcome was checked.',
        ),
      );
    }
  } else {
    out.add(
      const FairVerdict(
        'outcome',
        false,
        'Without a beta nothing can be re-derived.',
      ),
    );
  }
  final a = readAlpha(alphaText);
  if (a == null) {
    out.add(
      const FairVerdict(
        'alpha',
        false,
        'Alpha is not "<unix seconds>|<nonce>": a second in decimal with no sign or leading zeros, a bar, and a UUID in lowercase, hyphenated.',
      ),
    );
  } else {
    final time = DateTime.fromMillisecondsSinceEpoch(a.t * 1000, isUtc: true);
    out.add(
      FairVerdict(
        'alpha',
        true,
        'Alpha is "<unix seconds>|<nonce>": the second ${time.toIso8601String()}, and the nonce ${a.nonce}.',
        time: time,
      ),
    );
  }
  if (keys == null || keys.name.isEmpty) {
    out.add(
      const FairVerdict(
        'key',
        null,
        'Key not attributed: name a server to check the key against its published history.',
      ),
    );
  } else if (keys.history == null) {
    out.add(FairVerdict('key', null, 'Key not attributed: ${keys.why}'));
  } else {
    final text = pk == null ? '' : _b64(pk);
    final entry = keys.history!.where((e) => e['key'] == text).firstOrNull;
    final now = a == null ? null : _inForce(keys.history!, a.t);
    if (entry == null) {
      out.add(
        FairVerdict(
          'key',
          false,
          'This key is not in the key history ${keys.name} publishes.',
        ),
      );
    } else if (a != null && (now == null || now['key'] != text)) {
      out.add(
        FairVerdict(
          'key',
          false,
          "This key is ${keys.name}'s, but was not its key in force at the second alpha names.",
        ),
      );
    } else {
      out.add(
        FairVerdict(
          'key',
          true,
          "This is ${keys.name}'s key${a != null ? ', in force at the second alpha names' : ''}, by the history it publishes (began ${entry['began']}).",
        ),
      );
    }
  }
  return out;
}

/// The document of a message's draw, for the objects named (all when none
/// are): each value that took chance, its primitives and its claimed
/// results, as the web chat's exportMessage.
Map<String, Object?> exportFairMessage(Map m, [List<int>? named]) {
  final block = (m['fair'] as Map?) ?? const {};
  final beta = fairBytes(block['beta']);
  final values = <Map>[
    if (m['view'] is Map && (m['view'] as Map)['draw'] is Map) m['view'] as Map,
    for (final e in (m['entities'] as List? ?? const []))
      if (e is Map &&
          e['type'] == 'object' &&
          e['object'] is Map &&
          (e['object'] as Map)['draw'] is Map)
        e['object'] as Map,
  ];
  final objects = <Object?>[];
  for (final v in values) {
    final i = (v['draw'] as Map)['i'];
    if (i is! int ||
        (named != null && named.isNotEmpty && !named.contains(i))) {
      continue;
    }
    final o = <String, Object?>{'i': i, 'kind': v['kind']};
    final r = readValue(v);
    if (r.drawn && !r.removed && beta != null) {
      final recorded = (v['draw'] as Map)['primitives'];
      o['primitives'] = recorded is List && recorded.isNotEmpty
          ? recorded
          : kindAsks(r.kind!, r.data!);
      o['results'] = kindClaims(r.kind!, r.data!, beta, i);
    }
    objects.add(o);
  }
  return fairDocument(
    alpha: '${block['alpha'] ?? ''}',
    beta: '${block['beta'] ?? ''}',
    proof: '${block['proof'] ?? ''}',
    pk: '${block['pk'] ?? ''}',
    owner: block['owner'] as String?,
    objects: objects,
  );
}
