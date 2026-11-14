// Small SDP edits the media layer needs. Plain Dart, held by unit tests.

/// The mid of each media line of an SDP, in order.
List<String> midsOf(String sdp) {
  final out = <String>[];
  var inMedia = false;
  for (final line in sdp.split(RegExp(r'\r?\n'))) {
    if (line.startsWith('m=')) {
      inMedia = true;
      out.add('');
    } else if (inMedia && line.startsWith('a=mid:')) {
      out[out.length - 1] = line.substring(6).trim();
    }
  }
  return out;
}

/// Moves VP8 to the front of every video media line's codec list, so a
/// server that takes the first codec it shares takes VP8 (docs/CALLS.md
/// section 6). With [vp8Only], other video codecs are dropped from the
/// list (their RTX lines stay harmlessly unreferenced): a sealed chat's
/// call sends VP8 only (docs/CALL-FRAMES.md).
String preferVp8(String sdp, {bool vp8Only = false}) {
  final eol = sdp.contains('\r\n') ? '\r\n' : '\n';
  final lines = sdp.split(eol);
  // Media sections: [start, end).
  final starts = <int>[
    for (var i = 0; i < lines.length; i++)
      if (lines[i].startsWith('m=')) i,
  ];
  for (var n = 0; n < starts.length; n++) {
    final start = starts[n];
    final end = n + 1 < starts.length ? starts[n + 1] : lines.length;
    final m = lines[start].split(' ');
    if (!m.first.startsWith('m=video') || m.length < 4) continue;
    final vp8 = <String>{};
    final rtxFor = <String, String>{};
    for (var i = start + 1; i < end; i++) {
      final l = lines[i];
      final rtp = RegExp(
        r'^a=rtpmap:(\d+) VP8/',
        caseSensitive: false,
      ).firstMatch(l);
      if (rtp != null) vp8.add(rtp.group(1)!);
      final apt = RegExp(r'^a=fmtp:(\d+) apt=(\d+)').firstMatch(l);
      if (apt != null) rtxFor[apt.group(1)!] = apt.group(2)!;
    }
    if (vp8.isEmpty) continue;
    final formats = m.sublist(3);
    final first = [
      for (final f in formats)
        if (vp8.contains(f)) f,
    ];
    final rest = [
      for (final f in formats)
        if (!vp8.contains(f) && (!vp8Only || vp8.contains(rtxFor[f]))) f,
    ];
    lines[start] = [...m.sublist(0, 3), ...first, ...rest].join(' ');
  }
  return lines.join(eol);
}
