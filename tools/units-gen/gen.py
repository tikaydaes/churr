#!/usr/bin/env python3
"""Measurements (ruling R73): the units a message's measurement may be
typed in, and what each converts to. One registry, written out for
everyone who needs it.

    python3 tools/units-gen/gen.py

The units and their sizes come from the owner's unit converter
(DisguisedUnitConverterApp.swift), cut down to the ones a reader is
likely to want. It writes:

- internal/markdown/units_gen.go, the table the server reads measurements
  with;
- internal/web/static/shared/objects/units.js, the same table and the
  conversions for the web chat;
- app/lib/core/objects/units.dart, the same for the app, and the unit
  names as keys of app/lib/l10n/app_en.arb (with unit_names.dart to look
  them up);
- testdata/objects/units.json, the conversions both clients must show,
  worked out here.

A unit is typed exactly as one of its spellings: a number, at most one
space where the spelling allows one, then the spelling on a word edge.
Only temperatures may be negative.
"""
import json, os, re, subprocess
from decimal import Decimal, ROUND_HALF_UP

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, '..', '..')

# The categories, each with its units in the order the converter lists
# them. A unit is (code, id, symbol, English name, size in the category's
# base unit, system, the other system's unit shown beside it in a message,
# spellings it is read from). The code is what the entity carries; the id
# names it in code and in the app's wording file. A spelling is (text,
# whether a space may come before it). Temperatures have no size: they
# are worked out through Celsius.
M, I, O = 'metric', 'imperial', 'other'
CATEGORIES = [
    ('length', 'm', [
        ('mm', 'mm', 'mm', 'millimetres', 0.001, M, 'in', [('mm', 1)]),
        ('cm', 'cm', 'cm', 'centimetres', 0.01, M, 'in', [('cm', 1)]),
        ('m', 'm', 'm', 'metres', 1, M, 'ft', [('m', 1)]),
        ('km', 'km', 'km', 'kilometres', 1000, M, 'mi', [('km', 1)]),
        ('in', 'in', 'in', 'inches', 0.0254, I, 'cm', [('in', 0)]),
        ('ft', 'ft', 'ft', 'feet', 0.3048, I, 'm', [('ft', 1)]),
        ('yd', 'yd', 'yd', 'yards', 0.9144, I, 'm', [('yd', 1)]),
        ('mi', 'mi', 'mi', 'miles', 1609.344, I, 'km', [('mi', 1)]),
        ('nmi', 'nmi', 'nmi', 'nautical miles', 1852, O, None, []),
    ]),
    ('mass', 'kg', [
        ('mg', 'mg', 'mg', 'milligrams', 1e-6, M, 'oz', [('mg', 1)]),
        ('g', 'g', 'g', 'grams', 0.001, M, 'oz', [('g', 1)]),
        ('kg', 'kg', 'kg', 'kilograms', 1, M, 'lb', [('kg', 1)]),
        ('t', 't', 't', 'tonnes', 1000, M, None, []),
        ('oz', 'oz', 'oz', 'ounces', 0.028349523125, I, 'g', [('oz', 1)]),
        ('lb', 'lb', 'lb', 'pounds', 0.45359237, I, 'kg', [('lbs', 1), ('lb', 1)]),
        ('st', 'st', 'st', 'stone', 6.35029318, I, None, []),
    ]),
    ('temperature', '°C', [
        ('°C', 'c', '°C', 'degrees Celsius', None, M, '°F', [('°C', 1), ('℃', 1)]),
        ('°F', 'f', '°F', 'degrees Fahrenheit', None, I, '°C', [('°F', 1), ('℉', 1)]),
        ('K', 'k', 'K', 'kelvin', None, O, None, []),
    ]),
    ('volume', 'l', [
        ('ml', 'ml', 'ml', 'millilitres', 0.001, M, 'fl oz', [('ml', 1), ('mL', 1)]),
        ('l', 'l', 'l', 'litres', 1, M, 'gal', [('l', 1), ('L', 1)]),
        ('tsp', 'tsp', 'tsp', 'US teaspoons', 0.00492892159375, I, None, []),
        ('tbsp', 'tbsp', 'tbsp', 'US tablespoons', 0.01478676478125, I, None, []),
        ('fl oz', 'flOz', 'fl oz', 'US fluid ounces', 0.0295735295625, I, 'ml', []),
        ('cup', 'cup', 'cup', 'US cups', 0.2365882365, I, None, []),
        ('pt', 'pt', 'pt', 'US pints', 0.473176473, I, None, []),
        ('qt', 'qt', 'qt', 'US quarts', 0.946352946, I, None, []),
        ('gal', 'gal', 'gal', 'US gallons', 3.785411784, I, 'l', [('gal', 1)]),
        ('UK gal', 'ukGal', 'UK gal', 'imperial gallons', 4.54609, O, None, []),
    ]),
    ('speed', 'm/s', [
        ('m/s', 'mS', 'm/s', 'metres a second', 1, M, 'mph', [('m/s', 1)]),
        ('km/h', 'kmH', 'km/h', 'kilometres an hour', 1 / 3.6, M, 'mph', [('km/h', 1), ('kph', 1)]),
        ('ft/s', 'ftS', 'ft/s', 'feet a second', 0.3048, I, None, []),
        ('mph', 'mph', 'mph', 'miles an hour', 0.44704, I, 'km/h', [('mph', 1)]),
        ('kn', 'kn', 'kn', 'knots', 1852 / 3600, O, None, [('kn', 1)]),
    ]),
    ('area', 'm²', [
        ('cm²', 'cm2', 'cm²', 'square centimetres', 1e-4, M, None, []),
        ('m²', 'm2', 'm²', 'square metres', 1, M, 'ft²', [('m²', 1)]),
        ('ha', 'ha', 'ha', 'hectares', 1e4, M, 'ac', [('hectares', 1), ('hectare', 1)]),
        ('km²', 'km2', 'km²', 'square kilometres', 1e6, M, 'mi²', [('km²', 1)]),
        ('ft²', 'ft2', 'ft²', 'square feet', 0.09290304, I, 'm²', [('ft²', 1), ('sq ft', 1)]),
        ('yd²', 'yd2', 'yd²', 'square yards', 0.83612736, I, None, []),
        ('ac', 'ac', 'ac', 'acres', 4046.8564224, I, 'ha', [('acres', 1), ('acre', 1)]),
        ('mi²', 'mi2', 'mi²', 'square miles', 2589988.110336, I, 'km²', [('mi²', 1), ('sq mi', 1)]),
    ]),
    ('data', 'B', [
        ('B', 'byte', 'B', 'bytes', 1, O, None, []),
        ('kB', 'kB', 'kB', 'kilobytes', 1e3, O, None, [('kB', 1), ('KB', 1)]),
        ('MB', 'mB', 'MB', 'megabytes', 1e6, O, None, [('MB', 1)]),
        ('GB', 'gB', 'GB', 'gigabytes', 1e9, O, None, [('GB', 1)]),
        ('TB', 'tB', 'TB', 'terabytes', 1e12, O, None, [('TB', 1)]),
        ('KiB', 'kiB', 'KiB', 'kibibytes', 1024, O, None, [('KiB', 1)]),
        ('MiB', 'miB', 'MiB', 'mebibytes', 1024 ** 2, O, None, [('MiB', 1)]),
        ('GiB', 'giB', 'GiB', 'gibibytes', 1024 ** 3, O, None, [('GiB', 1)]),
        ('TiB', 'tiB', 'TiB', 'tebibytes', 1024 ** 4, O, None, [('TiB', 1)]),
    ]),
    ('pressure', 'Pa', [
        ('hPa', 'hPa', 'hPa', 'hectopascals', 100, M, 'inHg', [('hPa', 1), ('mbar', 1)]),
        ('kPa', 'kPa', 'kPa', 'kilopascals', 1000, M, 'psi', [('kPa', 1)]),
        ('bar', 'bar', 'bar', 'bar', 1e5, M, None, []),
        ('psi', 'psi', 'psi', 'pounds per square inch', 6894.757293168, I, 'kPa', [('psi', 1)]),
        ('inHg', 'inHg', 'inHg', 'inches of mercury', 3386.389, I, 'hPa', [('inHg', 1)]),
        ('atm', 'atm', 'atm', 'atmospheres', 101325, O, None, []),
        ('mmHg', 'mmHg', 'mmHg', 'millimetres of mercury', 133.322387415, O, None, [('mmHg', 1)]),
    ]),
    ('energy', 'J', [
        ('J', 'j', 'J', 'joules', 1, O, None, []),
        ('kJ', 'kJ', 'kJ', 'kilojoules', 1e3, O, None, [('kJ', 1)]),
        ('MJ', 'mJ', 'MJ', 'megajoules', 1e6, O, None, []),
        ('cal', 'cal', 'cal', 'calories', 4.184, O, None, []),
        ('kcal', 'kcal', 'kcal', 'kilocalories', 4184, O, None, [('kcal', 1)]),
        ('Wh', 'wh', 'Wh', 'watt-hours', 3600, O, None, [('Wh', 1)]),
        ('kWh', 'kWh', 'kWh', 'kilowatt-hours', 3.6e6, O, None, [('kWh', 1)]),
        ('BTU', 'btu', 'BTU', 'British thermal units', 1055.05585262, O, None, []),
    ]),
]

UNITS = {}
for cat, base, units in CATEGORIES:
    for code, uid, sym, name, size, system, alt, typed in units:
        assert code not in UNITS, code
        UNITS[code] = dict(cat=cat, id=uid, sym=sym, name=name, size=size, sys=system, alt=alt, typed=typed)
for code, u in UNITS.items():
    assert u['alt'] is None or UNITS[u['alt']]['cat'] == u['cat'], code
assert len({u['id'] for u in UNITS.values()}) == len(UNITS)

# The spellings, longest first so that a spelling that starts another
# (`m` and `mi`, `mm` and `mmHg`) is tried after it.
SPELLINGS = sorted(((t, code, sp, u['cat'] == 'temperature') for code, u in UNITS.items() for t, sp in u['typed']),
                   key=lambda s: -len(s[0]))
# `in` is read only straight after the number: "5 in the morning".
assert len({s[0] for s in SPELLINGS}) == len(SPELLINGS)


# --- the arithmetic, which both clients do the same way ---

def to_celsius(v, code):
    return v if code == '°C' else (v - 32) * 5 / 9 if code == '°F' else v - 273.15


def from_celsius(c, code):
    return c if code == '°C' else c * 9 / 5 + 32 if code == '°F' else c + 273.15


def convert(v, frm, to):
    if UNITS[frm]['cat'] == 'temperature':
        return from_celsius(to_celsius(v, frm), to)
    return v * UNITS[frm]['size'] / UNITS[to]['size']


SUPER = str.maketrans('-0123456789', '⁻⁰¹²³⁴⁵⁶⁷⁸⁹')


def number(v):
    """Four significant digits, halves away from zero; whole numbers grouped
    with ',' and the point '.', which a client swaps for its locale's; from
    10^15 up and below 10^-6 as a power of ten, "1.23×10¹⁵"."""
    if v == 0:
        return '0'
    sign = '-' if v < 0 else ''
    d = Decimal(abs(v))
    e = d.adjusted()
    q = d.scaleb(-e).quantize(Decimal('0.001'), rounding=ROUND_HALF_UP)
    if q >= 10:
        q, e = q / 10, e + 1
        q = q.quantize(Decimal('0.001'), rounding=ROUND_HALF_UP)
    digits = str(q).replace('.', '')
    if e >= 15 or e < -6:
        m = digits[0] + ('.' + digits[1:].rstrip('0') if digits[1:].rstrip('0') else '')
        return sign + m + '×10' + str(e).translate(SUPER)
    p = e + 1
    if p <= 0:
        whole, frac = '0', '0' * -p + digits
    elif p >= 4:
        whole, frac = digits + '0' * (p - 4), ''
    else:
        whole, frac = digits[:p], digits[p:]
    frac = frac.rstrip('0')
    groups = []
    while len(whole) > 3:
        groups.insert(0, whole[-3:])
        whole = whole[:-3]
    whole = ','.join([whole] + groups)
    return sign + whole + ('.' + frac if frac else '')


def joined(n, code):
    return n + ('' if code in ('°C', '°F') else ' ') + UNITS[code]['sym']


def rows(value, code, system):
    """The converter's rows for a reader of system: every other unit of the
    category, the reader's system first, then the other, then the rest."""
    cat = UNITS[code]['cat']
    order = [M, I, O] if system != I else [I, M, O]
    out = []
    for s in order:
        for c, u in UNITS.items():
            if u['cat'] == cat and u['sys'] == s and c != code:
                out.append(joined(number(convert(float(value), code, c)), c))
    return out


CASES = [
    ('5', 'km', M), ('5', 'km', I), ('3.2', 'mi', M), ('12', 'in', I), ('0.5', 'mm', M),
    ('72', '°F', M), ('20', '°C', I), ('-40', '°C', M), ('0', '°C', I),
    ('3', 'lb', M), ('500', 'g', I), ('2', 'kg', M), ('200', 'mg', I),
    ('2', 'l', M), ('250', 'ml', I), ('5', 'gal', M),
    ('60', 'mph', M), ('100', 'km/h', I), ('3', 'm/s', M), ('12', 'kn', M),
    ('50', 'm²', I), ('2', 'ha', M), ('10', 'ac', I), ('1500', 'ft²', M), ('3', 'km²', M), ('1', 'mi²', I),
    ('5', 'GB', M), ('512', 'MiB', M), ('2', 'TB', I), ('64', 'kB', M),
    ('1013', 'hPa', M), ('32', 'psi', M), ('200', 'kPa', I), ('120', 'mmHg', M), ('29.92', 'inHg', I),
    ('500', 'kcal', M), ('2000', 'kJ', M), ('10', 'kWh', M), ('60', 'Wh', M),
    ('123456789', 'km', M), ('0.000001', 'mg', M),
]

NUMBERS = [0, 1, -1, 3.10686, 1234.5, 12345678, 0.000123456, 0.5, 999.95, 9999.5, 1e15, 2.5e-7, 1e-6, -0.001,
           123456789012345, 6.02214076e23]

# --- outputs ---

def write(path, text):
    with open(os.path.join(ROOT, path), 'w') as f:
        f.write(text)


def j(v):
    return json.dumps(v, ensure_ascii=False)


inline_codes = [c for c, u in UNITS.items() if u['alt']]
write('testdata/objects/units.json', json.dumps({
    'description': 'The measurement converter (ruling R73, MESSAGE-FORMAT.md 3.2), written by tools/units-gen/gen.py. '
                   'units: every unit, its category, symbol, size in the base unit of its category (null for a '
                   'temperature), system, and the other system\'s unit a message shows beside it (null: only the '
                   'converter). numbers: how a converted value is written before the locale\'s separators are put in: '
                   'four significant digits, halves away from zero, the whole part grouped with "," and the point '
                   '".", and a power of ten from 10^15 up and below 10^-6. rows: what the converter lists for a '
                   'measurement and a reader\'s system, in order.',
    'units': [{'code': c, 'category': u['cat'], 'symbol': u['sym'], 'size': u['size'], 'system': u['sys'], 'alt': u['alt']}
              for c, u in UNITS.items()],
    'numbers': [{'value': v, 'shows': number(v)} for v in NUMBERS],
    'rows': [{'value': v, 'unit': c, 'system': s, 'rows': rows(v, c, s)} for v, c, s in CASES],
}, ensure_ascii=False, indent=1) + '\n')

go = ['// Code generated by tools/units-gen/gen.py. DO NOT EDIT.', '', 'package markdown', '',
      '// measureUnits are the spellings a measurement is read with (docs/MESSAGE-FORMAT.md',
      '// section 3.2): what is typed, the unit it is, whether a space may come',
      '// before it, and whether it is a temperature. Longest first.',
      'var measureUnits = []struct {', '\ttyped, code string', '\tspaced      bool', '\ttemp        bool', '}{']
for t, code, sp, temp in SPELLINGS:
    go.append('\t{%s, %s, %s, %s},' % (j(t), j(code), 'true' if sp else 'false', 'true' if temp else 'false'))
go.append('}')
write('internal/markdown/units_gen.go', '\n'.join(go) + '\n')
subprocess.run(['gofmt', '-w', os.path.join(ROOT, 'internal/markdown/units_gen.go')], check=True)

js = ['// Generated by tools/units-gen/gen.py from its registry. Do not edit.', '//',
      '// Measurements (ruling R73): the spellings a measurement is read with, and',
      '// every unit: its category, symbol, English name, size in the base unit',
      '// of its category (null for a temperature), system, and the unit of the',
      '// other system shown beside it in a message (null: only the converter).', '',
      '// [typed, code, a space may come before it, a temperature], longest first.',
      'export const MEASURE_UNITS = [']
for t, code, sp, temp in SPELLINGS:
    js.append('  [%s, %s, %d, %d],' % (j(t), j(code), sp, int(temp)))
js += ['];', '', 'export const UNITS = {']
for c, u in UNITS.items():
    js.append('  %s: { cat: %s, sym: %s, name: %s, size: %s, sys: %s, alt: %s },' % (
        j(c), j(u['cat']), j(u['sym']), j(u['name']), 'null' if u['size'] is None else repr(float(u['size'])), j(u['sys']), j(u['alt'])))
js += ['};', '']
write('internal/web/static/shared/objects/units.js', '\n'.join(js))


def dstr(s):
    return "'" + s.replace('\\', '\\\\').replace("'", "\\'").replace('$', '\\$') + "'"


def camel(prefix, s):
    return prefix + s[:1].upper() + s[1:]


dart = ['// Generated by tools/units-gen/gen.py from its registry. Do not edit.', '',
        '/// Measurements (ruling R73): the spellings a measurement is read with,',
        '/// longest first: (typed, code, a space may come before it, a temperature).',
        'const measureUnits = [']
for t, code, sp, temp in SPELLINGS:
    dart.append('  (%s, %s, %s, %s),' % (dstr(t), dstr(code), 'true' if sp else 'false', 'true' if temp else 'false'))
dart += ['];', '',
         '/// Every unit: its id (which names it in the wording file), category,',
         '/// symbol, size in the base unit of its category (null for a temperature),',
         '/// system, and the unit of the other system shown beside it in a message',
         '/// (null: only the converter).',
         'const measureUnitTable = <String, ({String id, String cat, String sym, double? size, String sys, String? alt})>{']
for c, u in UNITS.items():
    dart.append('  %s: (id: %s, cat: %s, sym: %s, size: %s, sys: %s, alt: %s),' % (
        dstr(c), dstr(u['id']), dstr(u['cat']), dstr(u['sym']), 'null' if u['size'] is None else repr(float(u['size'])),
        dstr(u['sys']), 'null' if u['alt'] is None else dstr(u['alt'])))
dart.append('};')
write('app/lib/core/objects/units.dart', '\n'.join(dart) + '\n')

names = ['// Generated by tools/units-gen/gen.py from its registry. Do not edit.', '',
         "import '../../l10n/app_localizations.dart';", '',
         '/// The name of a unit, through l10n: "miles".',
         'String unitName(L10n t, String code) => switch (code) {']
for c, u in UNITS.items():
    names.append('  %s => t.%s,' % (dstr(c), camel('unitName', u['id'])))
names += ['  _ => code,', '};']
write('app/lib/ui/message/unit_names.dart', '\n'.join(names) + '\n')

arb_path = os.path.join(ROOT, 'app/lib/l10n/app_en.arb')
with open(arb_path) as f:
    arb = json.load(f)
keys = {camel('unitName', u['id']): u['name'] for u in UNITS.values()}
for k in [k for k in arb if re.match(r'^unitName[A-Z0-9]', k) and k not in keys]:
    del arb[k]
arb.update(keys)
with open(arb_path, 'w') as f:
    f.write(json.dumps(arb, ensure_ascii=False, indent=2) + '\n')
print('%d categories, %d units, %d spellings' % (len(CATEGORIES), len(UNITS), len(SPELLINGS)))
