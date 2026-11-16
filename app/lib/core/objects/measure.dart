// What a reader sees beside a measurement (ruling R73): the original, and
// in muted text the equivalent in the reader's system, metric or imperial,
// or nothing when the unit is already of that system or the reader turned
// it off. testdata/objects/conversions.json holds the cases both clients
// pass.

/// Each unit's system, the unit it becomes in the other system, and its
/// size in the base unit of its kind. Temperatures are worked out apart.
const _units = <String, (String, String, double)>{
  'mm': ('metric', 'in', 0.001),
  'cm': ('metric', 'in', 0.01),
  'm': ('metric', 'ft', 1),
  'km': ('metric', 'mi', 1000),
  'in': ('imperial', 'cm', 0.0254),
  'ft': ('imperial', 'm', 0.3048),
  'yd': ('imperial', 'm', 0.9144),
  'mi': ('imperial', 'km', 1609.344),
  'mg': ('metric', 'oz', 1e-6),
  'g': ('metric', 'oz', 0.001),
  'kg': ('metric', 'lb', 1),
  'oz': ('imperial', 'g', 0.028349523125),
  'lb': ('imperial', 'kg', 0.45359237),
  'ml': ('metric', 'fl oz', 0.001),
  'l': ('metric', 'gal', 1),
  'gal': ('imperial', 'l', 3.785411784),
  'fl oz': ('imperial', 'ml', 0.0295735295625),
  'km/h': ('metric', 'mph', 1 / 3.6),
  'm/s': ('metric', 'mph', 1),
  'mph': ('imperial', 'km/h', 0.44704),
  '°C': ('metric', '°F', 0),
  '°F': ('imperial', '°C', 0),
};

/// The systems a reader may choose; '' is "from this device's region".
const measureSystems = ['metric', 'imperial', 'off'];

const _imperial = {'US', 'LR', 'MM'};

/// The system a region measures in: `US` is imperial.
String regionSystem(String? region) =>
    _imperial.contains(region?.toUpperCase()) ? 'imperial' : 'metric';

/// The system of a locale such as `en-US` or `en_US`.
String localeSystem(String locale) {
  final m = RegExp(r'[-_]([A-Za-z]{2})\b').firstMatch(locale);
  return regionSystem(m?.group(1));
}

int _round(double v) => (v + 0.5).floor();

/// A converted value: whole from 100 up, otherwise three significant
/// digits, without trailing zeros.
String measureNumber(double v) {
  if (v.abs() >= 100) return '${_round(v)}';
  var s = v.toStringAsPrecision(3);
  if (s.contains('e')) return '${double.parse(s)}';
  if (s.contains('.')) {
    s = s.replaceFirst(RegExp(r'0+$'), '').replaceFirst(RegExp(r'\.$'), '');
  }
  return s == '-0' ? '0' : s;
}

/// What a reader of [system] sees beside [value] [unit]: `3.11 mi`, or
/// null for nothing.
String? convertMeasure(String value, String unit, String system) {
  final u = _units[unit];
  final v = double.tryParse(value);
  if (u == null ||
      v == null ||
      (system != 'metric' && system != 'imperial') ||
      u.$1 == system) {
    return null;
  }
  if (unit == '°C') return '${_round(v * 9 / 5 + 32)}°F';
  if (unit == '°F') return '${_round((v - 32) * 5 / 9)}°C';
  return '${measureNumber(v * u.$3 / _units[u.$2]!.$3)} ${u.$2}';
}

/// The system a reader sees measurements in: their choice ([chosen]:
/// `metric`, `imperial`, `off`, or '' for the region's), else their
/// device's [region].
String viewerMeasureSystem(String chosen, String? region) =>
    measureSystems.contains(chosen) ? chosen : regionSystem(region);
