/// One run of a word diff: text kept, removed or added.
enum DiffOp { same, removed, added }

class DiffPart {
  const DiffPart(this.op, this.text);
  final DiffOp op;
  final String text;

  @override
  String toString() => '${op.name}:"$text"';
}

/// The differences between an older and a newer text, word by word
/// (spaces kept with the word before them), for the history view
/// (docs/CLIENTS.md 9.7). Very long texts are compared whole.
List<DiffPart> wordDiff(String older, String newer) {
  final a = _words(older), b = _words(newer);
  if (a.length * b.length > 4000000) {
    return [
      if (older.isNotEmpty) DiffPart(DiffOp.removed, older),
      if (newer.isNotEmpty) DiffPart(DiffOp.added, newer),
    ];
  }
  // Longest common subsequence, from the end.
  final n = a.length, m = b.length;
  final lcs = List.generate(n + 1, (_) => List<int>.filled(m + 1, 0));
  for (var i = n - 1; i >= 0; i--) {
    for (var j = m - 1; j >= 0; j--) {
      lcs[i][j] = a[i] == b[j]
          ? lcs[i + 1][j + 1] + 1
          : (lcs[i + 1][j] >= lcs[i][j + 1] ? lcs[i + 1][j] : lcs[i][j + 1]);
    }
  }
  final out = <DiffPart>[];
  void add(DiffOp op, String w) {
    if (out.isNotEmpty && out.last.op == op) {
      out[out.length - 1] = DiffPart(op, out.last.text + w);
    } else {
      out.add(DiffPart(op, w));
    }
  }

  var i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] == b[j]) {
      add(DiffOp.same, a[i]);
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      add(DiffOp.removed, a[i++]);
    } else {
      add(DiffOp.added, b[j++]);
    }
  }
  while (i < n) {
    add(DiffOp.removed, a[i++]);
  }
  while (j < m) {
    add(DiffOp.added, b[j++]);
  }
  return out;
}

List<String> _words(String s) => [
  for (final m in RegExp(r'\S+\s*|\s+').allMatches(s)) m.group(0)!,
];
