"""The short notation the generator's sources are written in.

Schemas
    S(name, description, {field: type, ...})
    A field name ending in `?` is optional; every other field is required.
    A type is one of:
        'str' 'int' 'num' 'bool' 'uuid' 'time' 'date' 'hhmm' 'days' 'b64' 'uri' 'any' 'obj' 'bin'
        '#Name'            a reference to another schema
        '[type]'           a list
        'enum:a|b|c'       one of these strings
        'map:type'         an object whose values have that type
        {field: type}      an inline object
        (type, 'text')     a type with a description
        (type, 'text', {}) the same, with extra JSON Schema keywords
        {'$raw': {...}}    JSON Schema written out by hand
    RAW(name, schema) registers a schema written out by hand.

Operations
    area(name, description) starts an area (an OpenAPI tag).
    op('METHOD path', operationId, summary, who, ...) adds an operation to the current area.
    A path that does not start with `/` is under `/api/v1/`.
"""
import collections

SCHEMAS = collections.OrderedDict()
OPS = []          # list of dicts
TAGS = []         # (name, description)

PRIMS = {
    'str': {'type': 'string'},
    'int': {'type': 'integer', 'format': 'int64'},
    'num': {'type': 'number'},
    'bool': {'type': 'boolean'},
    'uuid': {'type': 'string', 'format': 'uuid'},
    'time': {'type': 'string', 'format': 'date-time'},
    'date': {'type': 'string', 'format': 'date'},
    'hhmm': {'type': 'string', 'pattern': '^([01][0-9]|2[0-3]):[0-5][0-9]$'},
    'days': {'type': 'string', 'pattern': '^[01]{7}$'},
    'b64': {'type': 'string', 'contentEncoding': 'base64url'},
    'uri': {'type': 'string', 'format': 'uri'},
    'any': {},
    'obj': {'type': 'object', 'additionalProperties': True},
    'bin': {'type': 'string', 'format': 'binary'},
}


def T(t):
    """Turn the short type notation into a JSON Schema."""
    if isinstance(t, tuple):
        s = dict(T(t[0]))
        if '$ref' in s and len(s) == 1:
            s = {'$ref': s['$ref'], 'description': t[1]}
        else:
            s['description'] = t[1]
        if len(t) > 2:
            s.update(t[2])
        return s
    if isinstance(t, dict):
        if '$raw' in t:
            return t['$raw']
        return obj(t)
    if t.startswith('#'):
        return {'$ref': '#/components/schemas/' + t[1:]}
    if t.startswith('[') and t.endswith(']'):
        return {'type': 'array', 'items': T(t[1:-1])}
    if t.startswith('enum:'):
        return {'type': 'string', 'enum': t[5:].split('|')}
    if t.startswith('map:'):
        return {'type': 'object', 'additionalProperties': T(t[4:])}
    return dict(PRIMS[t])


def obj(props, desc=None):
    s = collections.OrderedDict()
    s['type'] = 'object'
    if desc:
        s['description'] = desc
    req = []
    p = collections.OrderedDict()
    for k, v in props.items():
        if k.endswith('?'):
            k = k[:-1]
        else:
            req.append(k)
        p[k] = T(v)
    if req:
        s['required'] = req
    s['properties'] = p
    return s


def S(name, desc, props):
    assert name not in SCHEMAS, name
    SCHEMAS[name] = obj(props, desc)


def RAW(name, schema):
    assert name not in SCHEMAS, name
    SCHEMAS[name] = schema


CUR = {'tag': None}


def area(name, desc):
    TAGS.append((name, desc))
    CUR['tag'] = name


def op(sig, opid, summary, who, desc=None, q=None, body=None, multipart=None, form=None,
       res=None, st=None, err='', auth='user', page=None, raw=None, also=None, raw409=False, body_optional=False):
    """Add an operation.

    who        who may call it; becomes the first line of the description
    desc       the rest of the description
    q          query parameters, each 'name:type:description'; `name!` is required
    body       a JSON body, in the short notation or '#Name'
    multipart  a multipart body
    form       a form-encoded body
    res        the JSON answer; None means 204
    st         the success status when it is not 200
    err        error codes it can return, space-separated, beyond the ones every operation has
    auth       'user' (cookie or bearer), 'pending' (also a session that still owes a second factor),
               'web' (cookie only), 'optional', 'none', 'integration', 'hook'
    page       'cursor' or 'seq'
    raw        responses written out by hand, by status
    also       further success statuses that answer with the same body: {status: description}
    raw409     the 409 answer carries the usual error and, beside it, the winning epoch record
    body_optional  the body may be left out altogether
    """
    method, path = sig.split(' ', 1)
    if not path.startswith('/'):
        path = '/api/v1/' + path
    OPS.append(dict(tag=CUR['tag'], method=method.lower(), path=path, id=opid, summary=summary,
                    who=who, desc=desc, q=q or [], body=body, multipart=multipart, form=form,
                    res=res, st=st, err=err.split(), auth=auth, page=page, raw=raw, also=also, raw409=raw409,
                    body_optional=body_optional))
