import 'json.dart';

/// The field types this build can draw (docs/MESSAGE-FORMAT.md section 7).
const knownFieldTypes = {
  'choice',
  'text',
  'longtext',
  'number',
  'date',
  'time',
  'account',
  'conversation',
  'checkbox',
};

/// The actions this build knows.
const knownActions = {'tally', 'collect', 'thread', 'channel', 'group', 'bot'};

/// The presets this build knows. Absent means a builder form.
const knownPresets = {
  'poll',
  'rsvp',
  'signup',
  'approve',
  'checkin',
  'ticket',
  'roles',
};

/// One option of a choice field.
class FormOption {
  FormOption({required this.id, required this.label, this.max, this.addedById});

  factory FormOption.fromJson(Json j) => FormOption(
    id: j['id'] as String? ?? '',
    label: j['label'] as String? ?? '',
    max: j['max'] == null ? null : asInt(j['max']),
    addedById: j['added_by_id'] as String?,
  );

  final String id;
  final String label;

  /// Places for this option (the sign-up preset).
  final int? max;
  final String? addedById;

  Json toJson() => {'id': id, 'label': label, 'max': ?max};
}

/// One field of a form.
class FormFieldDef {
  FormFieldDef({
    required this.id,
    required this.type,
    required this.label,
    this.help,
    this.required = false,
    this.multiple = false,
    this.options = const [],
    this.min,
    this.max,
    this.step,
    this.placeholder,
    this.maxLength,
  });

  factory FormFieldDef.fromJson(Json j) => FormFieldDef(
    id: j['id'] as String? ?? '',
    type: j['type'] as String? ?? '',
    label: j['label'] as String? ?? '',
    help: j['help'] as String?,
    required: j['required'] == true,
    multiple: j['multiple'] == true,
    options: listOf(j['options'], FormOption.fromJson),
    min: j['min'],
    max: j['max'],
    step: j['step'] is num ? (j['step'] as num) : null,
    placeholder: j['placeholder'] as String?,
    maxLength: j['max_length'] == null ? null : asInt(j['max_length']),
  );

  final String id;
  final String type;
  final String label;
  final String? help;
  final bool required;
  final bool multiple;
  final List<FormOption> options;

  /// For `choice`, the fewest and most options to pick; for `number` a
  /// number; for `date` a `YYYY-MM-DD` string. Kept as sent.
  final Object? min;
  final Object? max;
  final num? step;
  final String? placeholder;
  final int? maxLength;

  bool get isChoice => type == 'choice';

  /// A single choice with up to five options is drawn in the message as
  /// buttons or radio rows; with more, as a menu (MESSAGE-FORMAT.md 7).
  bool get inline => isChoice && !multiple;

  Json toJson() => {
    'id': id,
    'type': type,
    'label': label,
    'help': ?help,
    if (required) 'required': true,
    if (multiple) 'multiple': true,
    if (options.isNotEmpty) 'options': [for (final o in options) o.toJson()],
    'min': ?min,
    'max': ?max,
    'step': ?step,
    'placeholder': ?placeholder,
    'max_length': ?maxLength,
  };
}

/// A button on a form.
class FormButtonDef {
  FormButtonDef({
    required this.id,
    required this.label,
    this.style = 'default',
  });

  factory FormButtonDef.fromJson(Json j) => FormButtonDef(
    id: j['id'] as String? ?? '',
    label: j['label'] as String? ?? '',
    style: j['style'] as String? ?? 'default',
  );

  final String id;
  final String label;

  /// `default`, `primary` or `danger`.
  final String style;
}

/// A form, read from the raw JSON a message carries. Unknown fields stay
/// in [raw].
class FormModel {
  FormModel._(this.raw)
    : id = raw['id'] as String? ?? '',
      preset = raw['preset'] as String?,
      title = raw['title'] as String?,
      fields = listOf(raw['fields'], FormFieldDef.fromJson),
      buttons = listOf(raw['buttons'], FormButtonDef.fromJson),
      action = object(raw['action'])?['type'] as String? ?? '',
      anonymous = raw['anonymous'] == true,
      results = raw['results'] as String?,
      allowAddedOptions = raw['allow_added_options'] == true,
      answererGroupId = raw['answerer_group_id'] as String?,
      oneAnswer = raw['one_answer'] != false,
      closesAt = parseTime(raw['closes_at']),
      closedAt = parseTime(raw['closed_at']),
      answerCount = asInt(raw['answer_count']),
      tally = _counts(raw['tally']),
      voters = _voters(raw['voters']),
      myAnswer = object(raw['my_answer']);

  factory FormModel.fromJson(Json j) => FormModel._(j);

  final Json raw;
  final String id;
  final String? preset;
  final String? title;
  final List<FormFieldDef> fields;
  final List<FormButtonDef> buttons;

  /// `action.type`.
  final String action;
  final bool anonymous;
  final String? results;
  final bool allowAddedOptions;
  final String? answererGroupId;
  final bool oneAnswer;
  final DateTime? closesAt;
  final DateTime? closedAt;
  final int answerCount;

  /// Counts by field id, then option id. Null when the server did not
  /// send it to this reader: the rule in `results` is the server's, and
  /// the client shows exactly what came.
  final Map<String, Map<String, int>>? tally;

  /// Account ids by field id, then option id. Null when not sent, and
  /// never sent on an anonymous form.
  final Map<String, Map<String, List<String>>>? voters;

  /// The reader's own answer, or null.
  final Json? myAnswer;

  static Map<String, Map<String, int>>? _counts(Object? v) {
    if (v is! Map) return null;
    return {
      for (final e in v.entries)
        if (e.value is Map)
          '${e.key}': {
            for (final o in (e.value as Map).entries)
              '${o.key}': asInt(o.value),
          },
    };
  }

  static Map<String, Map<String, List<String>>>? _voters(Object? v) {
    if (v is! Map) return null;
    return {
      for (final e in v.entries)
        if (e.value is Map)
          '${e.key}': {
            for (final o in (e.value as Map).entries)
              '${o.key}': strings(o.value),
          },
    };
  }

  /// Whether this build can draw the form at all. An unknown field type,
  /// action or preset makes the whole form undrawable
  /// (MESSAGE-FORMAT.md section 10).
  bool get drawable =>
      knownActions.contains(action) &&
      (preset == null || knownPresets.contains(preset)) &&
      fields.every((f) => knownFieldTypes.contains(f.type));

  /// Closed by someone, or past its closing time (the server closes it
  /// when the next answer arrives; the client stops offering answers at
  /// the time).
  bool isClosed(DateTime now) =>
      closedAt != null || (closesAt != null && !closesAt!.isAfter(now));

  bool get hasAnswer => myAnswer != null && myAnswer!.isNotEmpty;

  /// Answers to `thread` and `channel` forms cannot be changed once sent;
  /// a `bot` form takes any number. `tally` and `collect` can be changed
  /// and withdrawn until closed.
  bool get changeable => action == 'tally' || action == 'collect';

  /// The one field drawn in the message itself: a single choice. Every
  /// other form opens an answer dialog.
  FormFieldDef? get inlineField =>
      fields.length == 1 && fields.first.inline ? fields.first : null;

  /// The ids the reader chose in a choice field.
  List<String> mine(String fieldId) => strings(myAnswer?[fieldId]);

  /// The ids the reader chose by button (tallied under `_button`).
  String? get myButton {
    final b = myAnswer?['_button'];
    if (b is String) return b;
    if (b is List && b.isNotEmpty) return '${b.first}';
    return null;
  }

  int count(String fieldId, String optionId) => tally?[fieldId]?[optionId] ?? 0;

  /// The total of a field's counts: what bars are measured against.
  int fieldTotal(String fieldId) =>
      tally?[fieldId]?.values.fold<int>(0, (a, b) => a + b) ?? 0;

  /// A copy with the reader's answer replaced (an optimistic write; the
  /// server's answer or event corrects it).
  Json withMyAnswer(Json? answer) {
    final j = {...raw};
    if (answer == null) {
      j.remove('my_answer');
    } else {
      j['my_answer'] = answer;
    }
    return j;
  }
}

/// Checks one value against its field as the server will (FEATURES.md
/// 7.2), so the dialog can say what is wrong before sending. Returns a
/// sentence, or null when the value is fine.
String? checkValue(FormFieldDef f, Object? v) {
  bool empty(Object? x) =>
      x == null ||
      (x is String && x.trim().isEmpty) ||
      (x is List && x.isEmpty) ||
      x == false;
  if (empty(v)) return f.required ? 'Required.' : null;
  switch (f.type) {
    case 'choice':
      final ids = v is List ? v : [v];
      if (!f.multiple && ids.length > 1) return 'Pick one.';
      final lo = f.min is num ? (f.min as num).toInt() : null;
      final hi = f.max is num ? (f.max as num).toInt() : null;
      if (lo != null && ids.length < lo) return 'Pick at least $lo.';
      if (hi != null && ids.length > hi) return 'Pick at most $hi.';
    case 'text':
      final s = '$v';
      if (s.contains('\n')) return 'One line only.';
      if (s.length > (f.maxLength ?? 500)) return 'At most ${f.maxLength ?? 500} characters.';
    case 'longtext':
      if ('$v'.length > (f.maxLength ?? 4000)) return 'At most ${f.maxLength ?? 4000} characters.';
    case 'number':
      final n = v is num ? v : num.tryParse('$v');
      if (n == null) return 'A number.';
      if (f.min is num && n < (f.min as num)) return 'At least ${f.min}.';
      if (f.max is num && n > (f.max as num)) return 'At most ${f.max}.';
      if (f.step != null && f.step! > 0) {
        final base = f.min is num ? f.min as num : 0;
        final k = (n - base) / f.step!;
        if ((k - k.round()).abs() > 1e-9) return 'In steps of ${f.step}.';
      }
    case 'date':
      final s = '$v';
      if (!RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(s)) return 'A date, YYYY-MM-DD.';
      if (f.min is String && s.compareTo(f.min as String) < 0) return 'On or after ${f.min}.';
      if (f.max is String && s.compareTo(f.max as String) > 0) return 'On or before ${f.max}.';
    case 'time':
      if (!RegExp(r'^([01]\d|2[0-3]):[0-5]\d$').hasMatch('$v')) return 'A time, HH:MM.';
    case 'account' || 'conversation':
      final ids = v is List ? v : [v];
      if (!f.multiple && ids.length > 1) return 'Pick one.';
  }
  return null;
}
