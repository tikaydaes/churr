// What a reader sees beside a measurement (ruling R73): the original, and
// in muted text the equivalent in the reader's system, metric or imperial,
// or nothing when the unit is already of that system or the reader turned
// it off. testdata/objects/conversions.json holds the cases both clients
// pass.

import 'units.dart';

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
  final u = measureUnitTable[unit];
  final v = double.tryParse(value);
  if (u == null ||
      u.alt == null ||
      v == null ||
      (system != 'metric' && system != 'imperial') ||
      u.sys == system) {
    return null;
  }
  if (unit == '°C') return '${_round(v * 9 / 5 + 32)}°F';
  if (unit == '°F') return '${_round((v - 32) * 5 / 9)}°C';
  return '${measureNumber(v * u.size! / measureUnitTable[u.alt]!.size!)} ${u.alt}';
}

// The converter (testdata/objects/units.json).

double _toC(double v, String u) =>
    u == '°C' ? v : (u == '°F' ? (v - 32) * 5 / 9 : v - 273.15);
double _fromC(double c, String u) =>
    u == '°C' ? c : (u == '°F' ? c * 9 / 5 + 32 : c + 273.15);

/// [v] in [from] converted to [unit] of the same category.
double convertUnit(double v, String from, String unit) {
  final f = measureUnitTable[from]!;
  if (f.cat == 'temperature') return _fromC(_toC(v, from), unit);
  return v * f.size! / measureUnitTable[unit]!.size!;
}

const _super = {
  '-': '⁻', '0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', //
  '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
};

/// A converted value: four significant digits, halves away from zero, the
/// whole part grouped with "," and the point "." ([measureSeparators]
/// swaps them for a locale's), and a power of ten from 10^15 up and below
/// 10^-6: "1.23×10¹⁵".
String measureWritten(double v) {
  if (v == 0 || !v.isFinite) return '0';
  final sign = v < 0 ? '-' : '';
  final parts = v.abs().toStringAsExponential(3).split('e');
  final e = int.parse(parts[1]);
  final digits = parts[0].replaceAll('.', '');
  if (e >= 15 || e < -6) {
    final rest = digits.substring(1).replaceFirst(RegExp(r'0+$'), '');
    final power = '$e'.split('').map((c) => _super[c]).join();
    return '$sign${digits[0]}${rest.isEmpty ? '' : '.$rest'}×10$power';
  }
  final p = e + 1;
  String whole, frac;
  if (p <= 0) {
    whole = '0';
    frac = '${'0' * -p}$digits';
  } else if (p >= 4) {
    whole = '$digits${'0' * (p - 4)}';
    frac = '';
  } else {
    whole = digits.substring(0, p);
    frac = digits.substring(p);
  }
  frac = frac.replaceFirst(RegExp(r'0+$'), '');
  whole = whole.replaceAllMapped(
    RegExp(r'\B(?=(\d{3})+$)'),
    (m) => ',',
  );
  return '$sign$whole${frac.isEmpty ? '' : '.$frac'}';
}

/// [s] with a locale's grouping and decimal marks ([group], [point]).
String measureSeparators(String s, String group, String point) =>
    s.replaceAllMapped(RegExp('[,.]'), (m) => m[0] == ',' ? group : point);

/// A number and a unit's symbol: "3.107 mi", "22.22°C".
String measureJoined(String n, String unit) =>
    '$n${unit == '°C' || unit == '°F' ? '' : ' '}${measureUnitTable[unit]!.sym}';

/// The units the converter lists for a measurement, for a reader of
/// [system]: every other unit of the category, the reader's system first,
/// then the other system, then the rest; each with its text before the
/// locale's marks.
List<({String unit, String text})> measureRows(
  String value,
  String unit,
  String system,
) {
  final u = measureUnitTable[unit];
  final v = double.tryParse(value);
  if (u == null || v == null) return const [];
  final order = system == 'imperial'
      ? ['imperial', 'metric', 'other']
      : ['metric', 'imperial', 'other'];
  return [
    for (final s in order)
      for (final MapEntry(key: code, value: x) in measureUnitTable.entries)
        if (x.cat == u.cat && x.sys == s && code != unit)
          (
            unit: code,
            text: measureJoined(measureWritten(convertUnit(v, unit, code)), code),
          ),
  ];
}

/// The system a reader sees measurements in: their choice ([chosen]:
/// `metric`, `imperial`, `off`, or '' for the region's), else their
/// device's [region].
String viewerMeasureSystem(String chosen, String? region) =>
    measureSystems.contains(chosen) ? chosen : regionSystem(region);
