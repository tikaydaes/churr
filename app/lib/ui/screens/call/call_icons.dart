import '../../theme/churr_icons.dart';

/// The call screen's icons, drawn to the icon set's rules (a 24px grid,
/// 2px line, square ends, cut corners, solid fill for "on"). Kept here
/// rather than in the shared icon set while only calls use them.
abstract final class CallIcons {
  static const microphone = ChurrGlyph(
    'M9 4h6v9l-1 1h-4l-1-1z M6 11v3l3 3h6l3-3v-3 M12 17v3 M8 20h8',
  );

  /// The microphone with a slash: muted.
  static const microphoneOff = ChurrGlyph(
    'M9 4h6v9l-1 1h-4l-1-1z M6 11v3l3 3h6l3-3v-3 M12 17v3 M8 20h8 M3 3l18 18',
  );
  static const camera = ChurrIcons.call;
  static const cameraOff = ChurrGlyph(
    'M4 7h10v10H4z M14 10l6-3v10l-6-3 M3 3l18 18',
  );

  /// A screen with an arrow up out of it: share.
  static const screen = ChurrGlyph(
    'M3 4h18v12H3z M8 20h8 M12 13V7 M9 10l3-3 3 3',
  );
  static const screenStop = ChurrGlyph(
    'M3 4h18v12H3z M8 20h8 M9 7l6 6M15 7l-6 6',
  );

  /// The handset, put down: leave.
  static const leave = ChurrGlyph('M3 14l3-3h12l3 3-2 3-4-1v-2H9v2l-4 1z');
  static const speaker = ChurrGlyph(
    'M4 9h4l5-4v14l-5-4H4z M16 9l2 3-2 3 M19 6l3 6-3 6',
  );
  static const earpiece = ChurrIcons.phone;
  static const switchCamera = ChurrGlyph(
    'M4 7h4l2-2h4l2 2h4v12H4z M9 12l3-3 3 3 M12 9v7',
  );
  static const expand = ChurrGlyph(
    'M4 10V4h6 M20 14v6h-6 M4 4l6 6 M20 20l-6-6',
  );
  static const shrink = ChurrGlyph(
    'M10 4v6H4 M14 20v-6h6 M4 10l6-6 M20 14l-6 6',
  );
  static const grid = ChurrGlyph(
    'M4 4h7v7H4z M13 4h7v7h-7z M4 13h7v7H4z M13 13h7v7h-7z',
  );
  static const speakerLayout = ChurrGlyph(
    'M3 4h18v11H3z M3 17h5v3H3z M10 17h4v3h-4z M16 17h5v3h-5z',
  );

  /// Three bars of different heights: speaking.
  static const speaking = ChurrGlyph(
    '',
    fill: 'M5 10h3v4H5z M10.5 6h3v12h-3z M16 9h3v6h-3z',
  );
}
