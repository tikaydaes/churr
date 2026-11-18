"""Writes the Protocol Buffers form of the contract (ruling R65, docs/WIRE-PROTOBUF.md).

build.py calls write(doc, ROOT) after it has built the OpenAPI document, so the protobuf schema comes
from the same schemas and operations as docs/openapi.yaml, plus the frames of the live connection in
events.py. It writes:

    proto/churr/v1/churr.proto        the schema
    proto/churr/v1/churr.lock.json    every field and enum number ever given out
    internal/wirepb/schema_gen.go     the same schema as a table the Go codec walks
    app/lib/core/wire/schema.g.dart   the same table for the app's codec

Numbers never change. The lock file is read first; a field already in it keeps its number, a new field
takes the next number after the highest ever used in its message, and a field that has gone stays in
the lock and is written as `reserved`, so its number is never used again. Regenerating with no change
to the sources rewrites every file byte for byte.

How JSON maps to protobuf is in docs/WIRE-PROTOBUF.md. In short: a UUID is 16 bytes, a time is int64
microseconds, base64url text is bytes, an enumeration is an enum with an UNSPECIFIED zero, anything
open-ended is a Value, and every message keeps two lists beside its fields (`empty_fields`,
`null_fields`) so that an empty list and a `null` survive exactly.
"""
import collections
import json
import os
import re
import shutil
import subprocess

import events

# The schema version a client and a server negotiate (docs/WIRE-PROTOBUF.md, "Versions"). Adding
# fields, enum values, frames and operations does not change it. It goes up only when a change is made
# that an older reader would misread; none has been.
VERSION = 1
PROTO_DIR = ('proto', 'churr', 'v1')
FIRST_FIELD = 3       # 1 and 2 are empty_fields and null_fields in every message
FIRST_BODY = 10       # the envelope's bodies; 3 to 7 are its own fields
ENVELOPE_FIELDS = [('type', 3, 'string'), ('at', 4, 'time'), ('conversation_id', 5, 'uuid'),
                   ('seq', 6, 'int'), ('json', 7, 'bytes')]
KINDS = ['string', 'int', 'double', 'bool', 'uuid', 'time', 'bytes', 'enum', 'msg', 'value', 'envelope', 'const', 'data']
CARDS = ['one', 'list', 'map', 'umap']
PROTO_TYPE = {'string': 'string', 'int': 'int64', 'double': 'double', 'bool': 'bool', 'uuid': 'bytes',
              'time': 'int64', 'bytes': 'bytes', 'data': 'bytes', 'value': 'Value', 'envelope': 'Envelope'}
MEDIA_TYPE = 'application/x-protobuf'


def camel(s):
    return ''.join(w[:1].upper() + w[1:] for w in re.split(r'[^A-Za-z0-9]+', s) if w)


def snake_upper(s):
    return re.sub(r'(?<=[a-z0-9])(?=[A-Z])', '_', s).upper()


def ident(s):
    """A JSON field name as a protobuf field name."""
    out = re.sub(r'[^A-Za-z0-9]+', '_', s).strip('_').lower()
    return out if not out[:1].isdigit() else 'f_' + out


class Builder:
    def __init__(self, doc):
        self.schemas = dict(doc['components']['schemas'])
        for name, s in events.EVENT_SCHEMAS.items():
            assert name not in self.schemas, name
            self.schemas[name] = s
        self.responses = doc['components']['responses']
        self.msgs = collections.OrderedDict()
        self.enums = collections.OrderedDict()
        self.named = {}          # schema name -> (card, kind, ref)
        self.skipped = []

    # ---- JSON Schema to the table's types

    def typ(self, s, ctx):
        """(card, kind, ref) for a schema. ctx names anything that has to be made for it."""
        if '$ref' in s:
            return self.ref(s['$ref'].split('/')[-1])
        if 'x-kinds' in s:
            return ('one', 'data', None)     # a value's data: bytes on the wire, its kind's object in JSON
        if 'x-const' in s:
            return ('one', 'const', s['x-const'])
        if 'allOf' in s:
            return ('one', 'msg', self.message(ctx, s))
        if 'oneOf' in s or 'anyOf' in s:
            return ('one', 'value', None)
        t = s.get('type')
        if isinstance(t, list):
            rest = [x for x in t if x != 'null']
            if len(rest) != 1:
                return ('one', 'value', None)
            t = rest[0]
        if t == 'array':
            card, kind, ref = self.typ(s.get('items', {}), ctx)
            if card != 'one' or kind == 'const':
                kind, ref = 'msg', self.wrapper(ctx + 'Item', card, kind, ref)
            return ('list', kind, ref)
        if t == 'object' or (t is None and 'properties' in s):
            props, extra = s.get('properties'), s.get('additionalProperties')
            if props is None and isinstance(extra, dict):
                if not extra:
                    card, kind, ref = 'one', 'value', None
                else:
                    card, kind, ref = self.typ(extra, ctx)
                if card != 'one' or kind == 'const':
                    kind, ref = 'msg', self.wrapper(ctx + 'Value', card, kind, ref)
                uuid_keys = (s.get('propertyNames') or {}).get('format') == 'uuid'
                if uuid_keys:
                    return ('umap', 'msg', self.entry(ctx + 'Entry', kind, ref))
                return ('map', kind, ref)
            if props is None:
                return ('one', 'value', None)
            return ('one', 'msg', self.message(ctx, s))
        if t == 'string':
            if 'enum' in s and all(isinstance(v, str) and v for v in s['enum']):
                return ('one', 'enum', self.enum(ctx, s['enum']))
            if s.get('format') == 'uuid':
                return ('one', 'uuid', None)
            if s.get('format') == 'date-time':
                return ('one', 'time', None)
            if s.get('contentEncoding') == 'base64url':
                return ('one', 'bytes', None)
            return ('one', 'string', None)
        if t == 'integer':
            return ('one', 'int', None)
        if t == 'number':
            return ('one', 'double', None)
        if t == 'boolean':
            return ('one', 'bool', None)
        return ('one', 'value', None)

    def ref(self, name):
        if name == 'Event':
            return ('one', 'envelope', None)     # an event inside a response is the live envelope itself
        if name not in self.named:
            s = self.schemas[name]
            if s.get('type') == 'object' and 'properties' in s or 'allOf' in s:
                self.named[name] = ('one', 'msg', name)     # set first: schemas refer to each other
                self.message(name, s)
            else:
                self.named[name] = self.typ(s, name)
        return self.named[name]

    def props_of(self, s):
        """The properties of an object schema, with every allOf part folded in."""
        props, extra = collections.OrderedDict(), False
        if '$ref' in s:
            s = self.schemas[s['$ref'].split('/')[-1]]
        for part in s.get('allOf', []):
            p, e = self.props_of(part)
            props.update(p)
            extra = extra or e
        props.update(s.get('properties') or {})
        return props, extra or s.get('additionalProperties') is True

    def message(self, name, s):
        assert name not in self.msgs and name not in ('Envelope', 'Value', 'ValueList', 'ValueMap'), name
        m = self.msgs[name] = {'name': name, 'fields': [], 'extra': False, 'shape': 'object',
                               'doc': (s.get('description') or '').split('\n')[0]}
        props, m['extra'] = self.props_of(s)
        for json_name, ps in props.items():
            card, kind, ref = self.typ(ps, name + camel(json_name))
            m['fields'].append({'json': json_name, 'card': card, 'kind': kind, 'ref': ref})
        return name

    def wrapper(self, name, card, kind, ref):
        """A message that stands for a list or map where protobuf cannot nest one directly."""
        while name in self.msgs:      # a map of maps: the outer wrapper is named after the inner
            name += 'Outer'
        self.msgs[name] = {'name': name, 'extra': False, 'shape': 'wrap', 'doc': '',
                           'fields': [{'json': 'values', 'card': card, 'kind': kind, 'ref': ref, 'num': 1}]}
        return name

    def entry(self, name, kind, ref):
        """One entry of a map whose keys are identifiers, which protobuf's own maps cannot hold."""
        assert name not in self.msgs, name
        self.msgs[name] = {'name': name, 'extra': False, 'shape': 'entry', 'doc': '',
                           'fields': [{'json': 'key', 'card': 'one', 'kind': 'uuid', 'ref': None, 'num': 1},
                                      {'json': 'value', 'card': 'one', 'kind': kind, 'ref': ref, 'num': 2}]}
        return name

    def enum(self, name, values):
        assert name not in self.enums and name not in self.msgs, name
        self.enums[name] = list(values)
        return name

    # ---- frames and operations

    def frames(self):
        out = []
        for t, s in events.FRAMES.items():
            out.append({'type': t, 'msg': self.message(camel(t) + 'Frame', s)})
        return out

    def body(self, content, ctx, where):
        """The message for a JSON body, or None when it has none or is not an object."""
        s = (content or {}).get('application/json', {}).get('schema')
        if s is None:
            return None
        key = json.dumps(s, sort_keys=True)
        if key not in self._bodies:
            if '$ref' not in s and ctx in self.msgs:
                ctx = where
            card, kind, ref = self.typ(s, ctx)
            if (card, kind) != ('one', 'msg'):
                self.skipped.append('%s: the body is not an object' % where)
                ref = None
            self._bodies[key] = ref
        return self._bodies[key]

    def operations(self, doc):
        out = []
        for path, item in doc['paths'].items():
            for method, o in item.items():
                self._bodies = {}
                name = camel(o['operationId'])
                name = name[:1].upper() + name[1:]
                op = {'id': o['operationId'], 'req': None, 'res': collections.OrderedDict(), 'node': o}
                rb = o.get('requestBody')
                if rb:
                    op['req'] = self.body(rb['content'], name + 'Request', name + 'Request')
                for status, r in o['responses'].items():
                    if '$ref' in r:
                        r = self.responses[r['$ref'].split('/')[-1]]
                    m = self.body(r.get('content'), name + 'Response', name + 'Response' + str(status))
                    if m:
                        op['res'][int(status)] = m
                out.append(op)
        return out


# ---- numbers

def number(b, frames, lock):
    """Give every field, enum value and envelope body its number, old ones from the lock."""
    lm, le, lf = (lock.setdefault(k, {}) for k in ('messages', 'enums', 'envelope'))
    for name, m in b.msgs.items():
        if m['shape'] != 'object':
            continue
        have = lm.setdefault(name, {})
        names = [f['json'] for f in m['fields'] if f['kind'] != 'const'] + (['+extra'] if m['extra'] else [])
        for n in names:
            if n not in have:
                have[n] = max(list(have.values()) + [FIRST_FIELD - 1]) + 1
        for f in m['fields']:
            f['num'] = have.get(f['json'], 0)
        m['extra_num'] = have.get('+extra', 0)
    for name, values in b.enums.items():
        have = le.setdefault(name, {})
        for v in values:
            if v not in have:
                have[v] = max(list(have.values()) + [0]) + 1
    for f in frames:
        if f['type'] not in lf:
            lf[f['type']] = max(list(lf.values()) + [FIRST_BODY - 1]) + 1
        f['num'] = lf[f['type']]


# ---- writing churr.proto

PROTO_HEADER = '''// Churr in Protocol Buffers: every body of the HTTP API and every frame of the live connection.
// Written by tools/openapi-gen/build.py from the same sources as docs/openapi.yaml. Do not edit by
// hand: change the sources there and run it again. The numbers are kept in churr.lock.json and never
// change. docs/WIRE-PROTOBUF.md gives the rules; the ones needed to read this file:
//
// Schema version %d.
//
//   - A field has the name it has in JSON. A JSON field that is absent is absent here.
//   - `bytes` holding an identifier is the 16 bytes of the UUID. JSON writes it as hyphenated hex.
//   - `int64` marked "time" is microseconds since the Unix epoch, UTC.
//   - Other `bytes` are what JSON writes as base64url.
//   - Every enum starts with an UNSPECIFIED zero, which is never sent.
//   - empty_fields lists the numbers of this message's list and map fields that are present and
//     empty in JSON (protobuf cannot tell an empty list from none). null_fields lists the numbers of
//     fields that are `null` in JSON. A reader that treats absent, null and empty alike ignores both.

syntax = "proto3";

package churr.v1;

// A JSON value of any shape, where the contract leaves one open (form answers, form limits,
// settings a provider defines). Whole numbers stay whole.
message Value {
  oneof kind {
    bool null_value = 1; // always true
    bool bool_value = 2;
    int64 int_value = 3;
    double double_value = 4;
    string string_value = 5;
    ValueList list_value = 6;
    ValueMap map_value = 7;
  }
}

message ValueList {
  repeated Value items = 1;
}

message ValueMap {
  map<string, Value> fields = 1;
}

'''


def proto_field(f):
    if f['kind'] == 'const':
        return '  // %s is always "%s" and is not sent.\n' % (f['json'], f['ref'])
    t = PROTO_TYPE.get(f['kind']) or f['ref']
    note = {'uuid': ' // identifier', 'time': ' // time'}.get(f['kind'], '')
    name = f.get('proto') or ident(f['json'])
    if f['card'] == 'one':
        opt = '' if f['kind'] in ('msg', 'value', 'envelope') else 'optional '
        return '  %s%s %s = %d;%s\n' % (opt, t, name, f['num'], note)
    if f['card'] == 'map':
        return '  map<string, %s> %s = %d;%s\n' % (t, name, f['num'], note)
    return '  repeated %s %s = %d;%s\n' % (t, name, f['num'], ' // a map, by identifier' if f['card'] == 'umap' else note)


def reserved(have, used):
    gone = sorted((n, k) for k, n in have.items() if k not in used)
    if not gone:
        return ''
    return '  reserved %s;\n' % ', '.join(str(n) for n, _ in gone)


def proto_text(b, frames, lock):
    out = [PROTO_HEADER % VERSION]
    out.append('// One frame of the live connection, and one event wherever an operation returns events.\n'
               '// The body that is set says which frame it is; `type` is sent only with `json`. A frame the\n'
               '// schema cannot hold exactly (a type or a field newer than this file) travels whole as\n'
               '// `json`, so nothing is ever dropped. A reader skips an envelope whose body it does not know.\n'
               'message Envelope {\n  repeated int32 null_fields = 2;\n'
               '  optional string type = 3;\n  optional int64 at = 4; // time\n'
               '  optional bytes conversation_id = 5; // identifier\n  optional int64 seq = 6;\n'
               '  optional bytes json = 7; // the whole frame as JSON text\n')
    out.append(reserved(lock['envelope'], {f['type'] for f in frames}))
    out.append('  oneof body {\n')
    for f in frames:
        out.append('    %s %s = %d;\n' % (f['msg'], ident(f['type']), f['num']))
    out.append('  }\n}\n\n')
    for name, m in b.msgs.items():
        if m['doc']:
            out.append('// %s\n' % m['doc'])
        out.append('message %s {\n' % name)
        if m['shape'] == 'object':
            seen = set()
            for f in m['fields']:
                f['proto'] = ident(f['json'])
                assert f['proto'] not in seen and f['proto'] not in ('empty_fields', 'null_fields', 'extra_fields'), \
                    (name, f['json'])
                seen.add(f['proto'])
            if any(f['card'] != 'one' for f in m['fields']) or m['extra']:
                out.append('  repeated int32 empty_fields = 1;\n')
            out.append('  repeated int32 null_fields = 2;\n')
            out.append(reserved(lock['messages'][name], {f['json'] for f in m['fields']} | {'+extra'} if m['extra']
                                else {f['json'] for f in m['fields']}))
        for f in m['fields']:
            out.append(proto_field(f))
        if m.get('extra_num'):
            out.append('  map<string, Value> extra_fields = %d; // members the schema does not name\n' % m['extra_num'])
        out.append('}\n\n')
    for name, values in b.enums.items():
        prefix = snake_upper(name)
        have = lock['enums'][name]
        out.append('enum %s {\n  %s_UNSPECIFIED = 0;\n' % (name, prefix))
        out.append(reserved(have, set(values)))
        seen = {'UNSPECIFIED'}
        for v in values:
            c = re.sub(r'[^A-Za-z0-9]+', '_', v).strip('_').upper() or 'EMPTY'
            assert c not in seen, (name, v)
            seen.add(c)
            out.append('  %s_%s = %d; // "%s"\n' % (prefix, c, have[v], v))
        out.append('}\n\n')
    return ''.join(out).rstrip('\n') + '\n'


# ---- writing the tables

def index(b):
    mi = {name: i for i, name in enumerate(b.msgs)}
    ei = {name: i for i, name in enumerate(b.enums)}
    return mi, ei


def field_row(f, mi, ei):
    ref, const = 0, ''
    if f['kind'] == 'msg':
        ref = mi[f['ref']]
    elif f['kind'] == 'enum':
        ref = ei[f['ref']]
    elif f['kind'] == 'const':
        const = f['ref']
    return f['json'], f.get('num', 0), f['kind'], f['card'], ref, const


def enum_rows(b, lock):
    for name, values in b.enums.items():
        have = lock['enums'][name]
        row = [''] * (max(have.values()) + 1)
        for v in values:
            row[have[v]] = v
        yield name, row


def catalogue_kinds(doc):
    """(kind, message, level) for every kind of the catalogue, from `x-kinds` beside ValueEnvelope's data."""
    data = doc['components']['schemas']['ValueEnvelope']['properties']['data']
    return [(k, v['$ref'].split('/')[-1], v['x-level']) for k, v in data['x-kinds'].items()]


def go_text(b, frames, ops, lock, kinds):
    mi, ei = index(b)
    q = json.dumps
    out = ['// Code generated by tools/openapi-gen/build.py from the API contract. DO NOT EDIT.\n'
           '// Change the sources in tools/openapi-gen and run the build again.\n\npackage wirepb\n\n'
           '// messages is every message of proto/churr/v1/churr.proto, with the JSON name of each field.\n'
           'var messages = []message{\n']
    for name, m in b.msgs.items():
        out.append('\t{name: %s, shape: %s, extra: %d, fields: []field{\n' % (
            q(name), 'shape' + camel(m['shape']), m.get('extra_num', 0)))
        for f in m['fields']:
            j, num, kind, card, ref, const = field_row(f, mi, ei)
            out.append('\t\t{%s, %d, %s, %s, %d, %s},\n' % (q(j), num, 'kind' + camel(kind), 'card' + camel(card), ref, q(const)))
        out.append('\t}},\n')
    out.append('}\n\n// enums holds the JSON text of each enum value, by its number.\nvar enums = [][]string{\n')
    for name, row in enum_rows(b, lock):
        out.append('\t{%s}, // %s\n' % (', '.join(q(v) for v in row), name))
    out.append('}\n\n// frames is every body of the Envelope: the frame type, its number and its message.\n'
               'var frames = []frame{\n')
    for f in frames:
        out.append('\t{%s, %d, %d},\n' % (q(f['type']), f['num'], mi[f['msg']]))
    out.append('}\n\n// operations gives each operation its request message (or -1) and its response messages by status.\n'
               'var operations = map[string]operation{\n')
    for o in ops:
        if o['req'] is None and not o['res']:
            continue
        res = ', '.join('%d: %d' % (s, mi[m]) for s, m in o['res'].items())
        out.append('\t%s: {%d, map[int]int{%s}},\n' % (q(o['id']), mi[o['req']] if o['req'] else -1, res))
    out.append('}\n\n// kinds is every kind of the catalogue: its name, the lowest catalogue level that reads its data in full,\n'
               '// and the message its data is.\nvar kinds = []kindDef{\n')
    for k, m, lv in kinds:
        out.append('\t{%s, %d, %d},\n' % (q(k), lv, mi[m]))
    out.append('}\n\n// errorMessage is the message of the error shape every operation answers a failure with.\n'
               'const errorMessage = %d\n\n'
               '// SchemaVersion is the version of the schema this table was written from.\n'
               'const SchemaVersion = %d\n' % (mi['Error'], VERSION))
    return ''.join(out)


def dart_text(b, frames, ops, lock):
    mi, ei = index(b)

    def q(s):
        return "'" + s.replace('\\', '\\\\').replace("'", "\\'").replace('$', '\\$') + "'"
    out = ['// Generated by tools/openapi-gen/build.py from the API contract. DO NOT EDIT.\n'
           '// Change the sources in tools/openapi-gen and run the build again.\n'
           '// dart format off\n// ignore_for_file: type=lint\n\n'
           "import 'gen/churr.pb.dart' as pb;\nimport 'schema.dart';\n\n"
           '/// Every message of proto/churr/v1/churr.proto, with the JSON name of each field.\n'
           'final List<WireMessage> wireMessages = [\n']
    for name, m in b.msgs.items():
        out.append('  WireMessage(%s, WireShape.%s, %d, pb.%s.new, const [\n' % (
            q(name), m['shape'], m.get('extra_num', 0), name))
        for f in m['fields']:
            j, num, kind, card, ref, const = field_row(f, mi, ei)
            if kind == 'data':
                kind = 'bytes'     # the app's codec keeps a value's data as bytes: base64url in its JSON
            out.append('    WireField(%s, %d, WireKind.%s, WireCard.%s, %d%s),\n' % (
                q(j), num, kind + ('_' if kind in ('double', 'int', 'bool', 'enum', 'const') else ''), card, ref,
                ', ' + q(const) if const else ''))
        out.append('  ]),\n')
    out.append('];\n\n/// The JSON text of each enum value, by its number, and how to make the value.\n'
               'final List<WireEnum> wireEnums = [\n')
    for name, row in enum_rows(b, lock):
        out.append('  WireEnum(const [%s], pb.%s.valueOf),\n' % (', '.join(q(v) for v in row), name))
    out.append('];\n\n/// Every body of the Envelope: the frame type, its number and its message.\n'
               'const List<WireFrame> wireFrames = [\n')
    for f in frames:
        out.append('  WireFrame(%s, %d, %d),\n' % (q(f['type']), f['num'], mi[f['msg']]))
    out.append('];\n\n/// Each operation\'s request message (or -1) and its response messages by status.\n'
               'const Map<String, WireOperation> wireOperations = {\n')
    for o in ops:
        if o['req'] is None and not o['res']:
            continue
        res = ', '.join('%d: %d' % (s, mi[m]) for s, m in o['res'].items())
        out.append('  %s: WireOperation(%d, {%s}),\n' % (q(o['id']), mi[o['req']] if o['req'] else -1, res))
    out.append('};\n\n/// The message of the error shape.\nconst int wireErrorMessage = %d;\n\n'
               '/// The version of the schema this table was written from.\nconst int wireSchemaVersion = %d;\n'
               % (mi['Error'], VERSION))
    return ''.join(out)


# ---- the OpenAPI document says where protobuf is accepted

def annotate(doc, ops):
    """Add the protobuf media type beside every JSON body that has a message."""
    def beside(content, msg):
        if content is not None and msg and 'application/json' in content:
            content[MEDIA_TYPE] = collections.OrderedDict([
                ('schema', {'type': 'string', 'format': 'binary'}),
                ('x-protobuf-message', 'churr.v1.' + msg)])
    for o in ops:
        node = o.pop('node')
        if o['req']:
            beside(node['requestBody']['content'], o['req'])
        for status, r in node['responses'].items():
            if '$ref' not in r:
                beside(r.get('content'), o['res'].get(int(status)))
    for r in doc['components']['responses'].values():
        beside(r.get('content'), 'Error')


def put(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    if not os.path.exists(path) or open(path).read() != text:
        with open(path, 'w') as f:
            f.write(text)


def write(doc, root):
    """Build the schema from doc and the frames, write the four files, and mark doc. Returns a summary."""
    b = Builder(doc)
    ops = b.operations(doc)
    frames = b.frames()
    b.ref('Error')
    kinds = catalogue_kinds(doc)
    for _, m, _ in kinds:
        b.ref(m)     # a kind's data is bytes on the wire; its message is reached only from here
    lock_path = os.path.join(root, *PROTO_DIR, 'churr.lock.json')
    lock = json.load(open(lock_path)) if os.path.exists(lock_path) else {}
    number(b, frames, lock)
    put(os.path.join(root, *PROTO_DIR, 'churr.proto'), proto_text(b, frames, lock))
    put(lock_path, json.dumps(lock, indent=1, sort_keys=True) + '\n')
    go = os.path.join(root, 'internal', 'wirepb', 'schema_gen.go')
    if os.path.isdir(os.path.dirname(go)):
        put(go, go_text(b, frames, ops, lock, kinds))
        if shutil.which('gofmt'):
            subprocess.run(['gofmt', '-w', go], check=True)
    dart = os.path.join(root, 'app', 'lib', 'core', 'wire', 'schema.g.dart')
    if os.path.isdir(os.path.dirname(dart)):
        put(dart, dart_text(b, frames, ops, lock))
    annotate(doc, ops)
    return ('proto/churr/v1/churr.proto: %d messages, %d enums, %d frames, %d operations with a body.'
            % (len(b.msgs), len(b.enums), len(frames), sum(1 for o in ops if o['req'] or o['res'])), b.skipped)
