import '../models/conversation.dart';
import '../models/json.dart';
import '../models/message.dart';
import 'client.dart';
import 'operations.dart';

/// One hit: the message as the asker would see it anywhere, and a short
/// excerpt around the match.
class SearchHit {
  SearchHit(this.message, this.snippet, {this.conversation});
  final Message message;
  final String? snippet;

  /// FEATURES.md 24.2 says each result comes with its conversation; the
  /// contract's `SearchResult` has no such field. Read when present.
  final Conversation? conversation;
}

/// One page of hits and the cursor for the next.
class SearchPage {
  SearchPage(this.hits, this.nextCursor);
  final List<SearchHit> hits;
  final String? nextCursor;
}

/// The filters of docs/FEATURES.md 24.1, as sent.
class SearchQuery {
  const SearchQuery({
    this.q = '',
    this.inId,
    this.fromId,
    this.since,
    this.until,
    this.hasFile = false,
  });
  final String q;
  final String? inId;
  final String? fromId;
  final DateTime? since;
  final DateTime? until;
  final bool hasFile;

  bool get isEmpty =>
      q.trim().isEmpty &&
      inId == null &&
      fromId == null &&
      since == null &&
      until == null &&
      !hasFile;
}

extension SearchApi on ApiClient {
  Future<SearchPage> search(
    SearchQuery s, {
    String? cursor,
    int limit = 30,
  }) async {
    final j = await call(
      Ops.search,
      query: {
        // The contract marks `q` required while FEATURES.md 24.1 allows a
        // filter alone; an empty `q` is sent in that case.
        'q': s.q,
        'in': s.inId,
        'from': s.fromId,
        if (s.since != null) 'since': formatTime(s.since!),
        if (s.until != null) 'until': formatTime(s.until!),
        if (s.hasFile) 'has_file': true,
        'limit': limit,
        'cursor': cursor,
      },
    );
    final hits = <SearchHit>[];
    if (j['results'] is List) {
      for (final r in j['results'] as List) {
        final o = object(r);
        final m = object(o?['message']);
        if (m == null) continue;
        final c = object(o?['conversation']);
        hits.add(
          SearchHit(
            Message.fromJson(m),
            o?['snippet'] as String?,
            conversation: c == null ? null : Conversation.fromJson(c),
          ),
        );
      }
    }
    final next = j['next_cursor'] as String?;
    return SearchPage(hits, next == null || next.isEmpty ? null : next);
  }
}
