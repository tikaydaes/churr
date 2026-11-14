import 'package:audioplayers/audioplayers.dart';

/// The app's two sounds (`assets/sounds`): the start-up sound, played
/// once at launch when App settings allows it, and the ring, which the
/// calls code plays.
///
/// `audioplayers` is used because it is maintained and plays bundled
/// assets on all five targets with no extra setup: AVFoundation on iOS
/// and macOS, MediaPlayer on Android, Media Foundation on Windows and
/// GStreamer on Linux. (`just_audio` needs a second package for Windows
/// and Linux.) The files are WAV, which each of those decodes.
abstract final class Sounds {
  static const startup = 'sounds/churr-startup.wav';
  static const ring = 'sounds/churr-ring.wav';

  static bool _played = false;

  /// Plays the start-up sound, once per process. Failing to play (no
  /// audio device, a test) is not an error.
  static Future<void> playStartup() async {
    if (_played) return;
    _played = true;
    try {
      final p = AudioPlayer();
      await p.setReleaseMode(ReleaseMode.release);
      await p.play(AssetSource(startup));
    } on Object {
      // Silence is fine.
    }
  }

  /// Starts the ring, looping, and returns the player so the caller can
  /// stop it. Every sound has a visible counterpart, and the ring also
  /// vibrates on phones (docs/CLIENTS.md section 11): both are the
  /// caller's.
  static Future<AudioPlayer?> startRing() async {
    try {
      final p = AudioPlayer();
      await p.setReleaseMode(ReleaseMode.loop);
      await p.play(AssetSource(ring));
      return p;
    } on Object {
      return null;
    }
  }
}
