// The names of Unicode code points, which the server carries; clients
// ask only when a person opens a code point.

import '../objects/codepoint.dart';
import 'client.dart';
import 'operations.dart';

extension UnicodeApi on ApiClient {
  /// The name and general category of one code point (`U+1F600`).
  Future<CodePointName?> lookupCodePoint(String value) async {
    final out = await call(Ops.lookupCodePoints, query: {'code_points': value});
    final list = out['code_points'] as List?;
    if (list == null || list.isEmpty || list.first is! Map) return null;
    final c = list.first as Map;
    return (
      name: '${c['name'] ?? ''}',
      category: '${c['category'] ?? ''}',
      categoryName: '${c['category_name'] ?? ''}',
    );
  }
}
