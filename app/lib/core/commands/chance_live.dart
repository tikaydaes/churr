/// Which chance results arrived live (rulings R67): their reels spin once
/// when first drawn, and never for a message loaded from history or met
/// again on scrolling back. Sync notes a message with a result as it comes
/// in over the live connection; the result's widget takes the note when it
/// is first built, so it plays once.
abstract final class ChanceLive {
  static final _ids = <String>{};

  /// How old a message may be and still count as arriving live: catching
  /// up after a reconnect brings older ones, which do not spin.
  static const fresh = Duration(seconds: 30);

  /// Notes a message that came in live. [createdAt] is the message's.
  static void arrived(String id, DateTime createdAt, {DateTime? now}) {
    if ((now ?? DateTime.now()).difference(createdAt).abs() <= fresh) {
      _ids.add(id);
      // A note never taken (the conversation was not on screen) is
      // dropped after a while.
      if (_ids.length > 200) _ids.remove(_ids.first);
    }
  }

  /// Takes the note for [id]: true once, if it arrived live.
  static bool take(String id) => _ids.remove(id);
}
