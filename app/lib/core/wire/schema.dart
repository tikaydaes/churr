/// The types of the schema table in `schema.g.dart`, which
/// tools/openapi-gen writes from the same sources as docs/openapi.yaml.
/// `wire.dart` walks the table; nothing else needs these.
library;

import 'package:protobuf/protobuf.dart';

/// What one value of a field is. The names with an underscore avoid Dart's
/// own words.
enum WireKind {
  string,
  int_,
  double_,
  bool_,

  /// 16 bytes on the wire, hyphenated hex in JSON.
  uuid,

  /// int64 microseconds on the wire, RFC 3339 in JSON.
  time,

  /// Bytes on the wire, base64url in JSON.
  bytes,
  enum_,
  msg,

  /// Any JSON value.
  value,

  /// An event inside a body: the live connection's envelope.
  envelope,

  /// Always the same text, and not on the wire.
  const_,

  /// A value's data (docs/DESIGN-MODULES.md section 4.1): its kind's
  /// message as bytes on the wire, its kind's object in JSON. The field
  /// named `kind` beside it says which kind ([WireValueKind]).
  data,
}

enum WireCard {
  one,
  list,

  /// A JSON object used as a map, with text keys.
  map,

  /// The same with identifiers as keys: repeated entry messages.
  umap,
}

enum WireShape {
  object,

  /// Stands for a bare list or map: one field, number 1.
  wrap,

  /// One entry of a [WireCard.umap]: key 1, value 2.
  entry,
}

class WireField {
  const WireField(
    this.json,
    this.number,
    this.kind,
    this.card,
    this.ref, [
    this.text = '',
  ]);

  /// The field's name in JSON.
  final String json;
  final int number;
  final WireKind kind;
  final WireCard card;

  /// The message or enum, by its place in the table.
  final int ref;

  /// For [WireKind.const_]: the text.
  final String text;
}

class WireMessage {
  WireMessage(this.name, this.shape, this.extra, this.make, this.fields);
  final String name;
  final WireShape shape;

  /// The number of the map that takes members the schema does not name,
  /// or 0 if the message has none.
  final int extra;
  final GeneratedMessage Function() make;
  final List<WireField> fields;

  late final Map<String, WireField> byName = {
    for (final f in fields) f.json: f,
  };
  late final bool hasEmpties = make().info_.fieldInfo.containsKey(1);
}

class WireEnum {
  const WireEnum(this.names, this.of);

  /// The JSON text of each value, by its number; '' where there is none.
  final List<String> names;
  final ProtobufEnum? Function(int) of;
}

class WireFrame {
  const WireFrame(this.type, this.number, this.message);
  final String type;
  final int number;
  final int message;
}

/// One kind of the catalogue: its name, the lowest catalogue level that
/// reads its data in full, and the message its data is, by its place in
/// the table.
class WireValueKind {
  const WireValueKind(this.name, this.level, this.message);
  final String name;
  final int level;
  final int message;
}

class WireOperation {
  const WireOperation(this.request, this.responses);

  /// The request message, or -1.
  final int request;

  /// The response message for each status the contract gives one.
  final Map<int, int> responses;
}
