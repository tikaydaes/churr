#!/usr/bin/env python3
"""Writes docs/openapi.yaml from the sources in this folder, and from the same operations the Go table
internal/api/operations_gen.go, which the server registers its handlers against.

    python3 tools/openapi-gen/build.py

It also refreshes the table of areas in docs/API.md, between its two marker comments, and checks
the result: the YAML parses, every $ref resolves, operationIds are unique, every schema is used,
and every operationId that docs/API.md and docs/EVENTS.md mention exists. If the package
openapi-spec-validator is installed, the file is validated against OpenAPI 3.1 as well.

Needs PyYAML. See README.md.
"""
import collections
import json
import os
import re
import sys

import yaml

sys.dont_write_bytecode = True
HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
sys.path.insert(0, HERE)

from lib import T, obj, SCHEMAS, OPS, TAGS  # noqa: E402
import schemas  # noqa: E402,F401
import ops_signin, ops_me, ops_chat, ops_bots, ops_sealed, ops_admin, ops_themes  # noqa: E402,F401
import gotable  # noqa: E402
import proto  # noqa: E402

OUT = os.path.join(ROOT, 'docs', 'openapi.yaml')
API_MD = os.path.join(ROOT, 'docs', 'API.md')
EVENTS_MD = os.path.join(ROOT, 'docs', 'EVENTS.md')

# Error codes and their statuses, in the order of CONVENTIONS.md.
STATUS = collections.OrderedDict([
    ('invalid_request', 400), ('invalid_value', 400),
    ('unauthenticated', 401), ('second_factor_required', 401),
    ('forbidden', 403), ('timed_out', 403), ('archived', 403), ('locked', 403), ('feature_off', 403),
    ('fresh_check_required', 403),
    ('not_found', 404),
    ('conflict', 409), ('name_taken', 409), ('already_exists', 409),
    ('too_large', 413), ('blocked_by_filter', 422),
    ('rate_limited', 429), ('slow_mode', 429),
    ('internal', 500), ('unavailable', 503),
])
RESPONSES = collections.OrderedDict([
    (400, ('BadRequest', 'The request was malformed (`invalid_request`) or a field was wrong (`invalid_value`, with `field`).')),
    (401, ('Unauthenticated', 'No valid credential (`unauthenticated`), or the session still owes a second factor (`second_factor_required`).')),
    (403, ('Forbidden', 'The caller may not do this: `forbidden`, `timed_out`, `archived`, `locked`, `feature_off`, or `fresh_check_required` (pass a passkey or authenticator check, then try again).')),
    (404, ('NotFound', '`not_found`. Also returned for things that exist and that the caller may not know exist.')),
    (409, ('Conflict', '`conflict`, `name_taken` (usernames, group handles, emoji and command names, and reserved names; never a channel name) or `already_exists`.')),
    (413, ('TooLarge', '`too_large`.')),
    (422, ('Unprocessable', '`blocked_by_filter`: the word filter refused the message.')),
    (429, ('TooManyRequests', '`rate_limited` or `slow_mode`, with `retry_after_seconds`.')),
    (500, ('ServerError', '`internal`.')),
    (503, ('Unavailable', '`unavailable`.')),
])

PATH_PARAMS = collections.OrderedDict([
    ('id', ('uuid', 'The identifier of the thing named just before it in the path.')),
    ('account_id', ('uuid', 'An account.')),
    ('group_id', ('uuid', 'A group.')),
    ('message_id', ('uuid', 'A message.')),
    ('conversation_id', ('uuid', 'A conversation.')),
    ('server_id', ('uuid', 'A known server.')),
    ('token_id', ('uuid', 'A token.')),
    ('timeout_id', ('uuid', 'A time-out.')),
    ('provider', ('uuid', 'A company sign-in provider.')),
    ('interaction_id', ('uuid', 'The `interaction_id` from an event the bot received.')),
    ('emoji', ('str', 'The emoji itself, percent-encoded, `:name:` for a custom one, or `:name@server.name:` for another server\'s.')),
    ('item', ('str', 'A checklist item\'s `id`.')),
    ('button', ('str', 'A form button\'s `id`.')),
    ('token', ('str', 'The webhook secret, starting `churr_hook_`.')),
    ('name', ('str', 'A name.')),
    ('code', ('str', 'A one-time code.')),
    ('key_epoch', ('int', 'A sealed chat\'s key epoch.')),
    ('index', ('int', 'A position, from 0.')),
    ('event_id', ('str', 'The event identifier a wake-up carried.')),
])


def pname(name):
    return 'Path' + ''.join(w.capitalize() for w in name.split('_'))


def qparam(spec):
    name, rest = spec.split(':', 1)
    required = name.endswith('!')
    name = name.rstrip('!')
    if rest.startswith('enum:'):
        _, values, desc = rest.split(':', 2)
        t = 'enum:' + values
    else:
        t, desc = rest.split(':', 1)
    p = collections.OrderedDict([('name', name), ('in', 'query')])
    if required:
        p['required'] = True
    p['description'] = desc
    p['schema'] = T(t)
    return p


def schema_of(x):
    return obj(x) if isinstance(x, dict) and '$raw' not in x else T(x)


def build_op(o):
    d = collections.OrderedDict()
    d['operationId'] = o['id']
    d['tags'] = [o['tag']]
    d['summary'] = o['summary']
    d['description'] = '**Who:** ' + o['who'] + ('\n\n' + o['desc'] if o['desc'] else '')

    params = [{'$ref': '#/components/parameters/' + pname(n)} for n in re.findall(r'\{(\w+)\}', o['path'])]
    params += [qparam(s) for s in o['q']]
    if o['page'] == 'cursor':
        params += [{'$ref': '#/components/parameters/Limit'}, {'$ref': '#/components/parameters/Cursor'}]
    elif o['page'] == 'seq':
        params += [{'$ref': '#/components/parameters/Before'}, {'$ref': '#/components/parameters/After'},
                   {'$ref': '#/components/parameters/Limit'}]
    if params:
        d['parameters'] = params

    auth = o['auth']
    assert auth in ('user', 'pending', 'web', 'optional', 'none', 'integration', 'hook'), auth
    if auth in ('none', 'hook'):
        d['security'] = []
    elif auth == 'optional':
        d['security'] = [{}, {'session_cookie': []}, {'bearer_token': []}]
    elif auth == 'integration':
        d['security'] = [{'integration_token': []}]
    elif auth == 'web':
        d['security'] = [{'session_cookie': []}]

    content = collections.OrderedDict()
    if o['body'] is not None:
        content['application/json'] = {'schema': schema_of(o['body'])}
    if o['multipart'] is not None:
        content['multipart/form-data'] = {'schema': obj(o['multipart'])}
    if o['form'] is not None:
        content['application/x-www-form-urlencoded'] = {'schema': obj(o['form'])}
    if content:
        d['requestBody'] = {'required': not o.get('body_optional'), 'content': content}

    responses = collections.OrderedDict()
    if o['raw'] is not None:
        responses.update(o['raw'])
    elif o['res'] is None:
        responses['204'] = {'description': 'Done. No content.'}
    else:
        r = o['res']
        if isinstance(r, dict):
            r = dict(r)
            if o['page'] == 'cursor':
                r['next_cursor?'] = ('str', 'Present when there is more. Opaque.')
            elif o['page'] == 'seq':
                r['has_more'] = 'bool'
        sch = schema_of(r)
        st = str(o['st'] or 200)
        responses[st] = {'description': 'Created.' if st == '201' else 'OK.',
                         'content': {'application/json': {'schema': sch}}}
        for status, text in (o['also'] or {}).items():
            responses[status] = {'description': text}
            if status not in ('204', '304'):
                responses[status]['content'] = {'application/json': {'schema': sch}}
        responses = collections.OrderedDict(sorted(responses.items()))

    if o['raw409']:
        responses['409'] = {
            'description': 'Codes: `conflict`. Another record already holds this key epoch. `epoch` is that record.',
            'content': {'application/json': {'schema': {'allOf': [
                {'$ref': '#/components/schemas/Error'},
                obj({'epoch': '#EpochRecord'})]}}}}
    codes = list(o['err']) + (['conflict'] if o['raw409'] else [])
    for c in codes:
        assert c in STATUS, (o['id'], c)
    if o['id'] not in ('exchangeToken', 'revokeOAuthToken'):   # these two answer in the OAuth error shape
        if content or params:
            codes.append('invalid_request')
        if content or o['q']:
            codes.append('invalid_value')
        if auth in ('user', 'web', 'integration', 'pending'):
            codes.append('unauthenticated')
        if auth in ('user', 'web'):
            codes.append('second_factor_required')
        codes += ['rate_limited', 'internal']
    order = list(STATUS)
    codes = sorted(set(codes), key=order.index)
    by_status = collections.OrderedDict()
    for c in codes:
        by_status.setdefault(STATUS[c], []).append(c)
    for st, cs in sorted(by_status.items()):
        if str(st) not in responses:
            responses[str(st)] = collections.OrderedDict([
                ('$ref', '#/components/responses/' + RESPONSES[st][0]),
                ('description', 'Codes: ' + ', '.join('`%s`' % c for c in cs) + '.')])
    d['responses'] = responses
    d['x-error-codes'] = codes
    return d


def build():
    doc = collections.OrderedDict()
    doc['openapi'] = '3.1.0'
    doc['info'] = collections.OrderedDict([
        ('title', 'Churr API'),
        ('version', '1.0.0'),
        ('summary', 'The HTTP API of a Churr server.'),
        ('license', {'name': 'AGPL-3.0', 'identifier': 'AGPL-3.0-only'}),
        ('description', INFO),
    ])
    doc['servers'] = [{'url': 'https://{host}', 'description': 'A Churr server.',
                       'variables': {'host': {'default': 'chat.example.org',
                                              'description': 'The server\'s public host name.'}}}]
    doc['security'] = [{'session_cookie': []}, {'bearer_token': []}]
    doc['tags'] = [collections.OrderedDict([('name', n), ('description', d)]) for n, d in TAGS]

    paths = collections.OrderedDict()
    ids = set()
    for o in OPS:
        assert o['id'] not in ids, 'operationId used twice: ' + o['id']
        ids.add(o['id'])
        item = paths.setdefault(o['path'], collections.OrderedDict())
        assert o['method'] not in item, (o['path'], o['method'])
        item[o['method']] = build_op(o)
    doc['paths'] = paths

    comp = collections.OrderedDict()
    comp['securitySchemes'] = collections.OrderedDict([
        ('session_cookie', {'type': 'apiKey', 'in': 'cookie', 'name': 'churr_session',
                            'description': 'A web session. The value starts `churr_ses_` and is accepted only in this cookie. HttpOnly, Secure, SameSite=Strict.'}),
        ('bearer_token', {'type': 'http', 'scheme': 'bearer',
                          'description': 'An app access token (`churr_app_`), a personal token (`churr_pat_`) or a bot token (`churr_bot_`).'}),
        ('integration_token', {'type': 'http', 'scheme': 'bearer',
                               'description': 'A server integration token (`churr_int_`). It works only on the `/api/v1/integration/` operations.'}),
    ])
    comp['parameters'] = collections.OrderedDict([
        ('Limit', {'name': 'limit', 'in': 'query', 'description': 'How many to return. Default 50, at most 200.',
                   'schema': {'type': 'integer', 'minimum': 1, 'maximum': 200, 'default': 50}}),
        ('Cursor', {'name': 'cursor', 'in': 'query', 'description': 'The `next_cursor` of the previous page. Opaque.',
                    'schema': {'type': 'string'}}),
        ('Before', {'name': 'before', 'in': 'query', 'description': 'Only things with `seq` less than this.',
                    'schema': {'type': 'integer', 'format': 'int64'}}),
        ('After', {'name': 'after', 'in': 'query', 'description': 'Only things with `seq` greater than this.',
                   'schema': {'type': 'integer', 'format': 'int64'}}),
    ])
    for name, (t, text) in PATH_PARAMS.items():
        comp['parameters'][pname(name)] = collections.OrderedDict([
            ('name', name), ('in', 'path'), ('required', True), ('description', text), ('schema', T(t))])
    comp['headers'] = collections.OrderedDict([
        ('RateLimit-Limit', {'description': 'Requests allowed in the current window.', 'schema': {'type': 'integer'}}),
        ('RateLimit-Remaining', {'description': 'Requests left in the current window.', 'schema': {'type': 'integer'}}),
        ('RateLimit-Reset', {'description': 'Seconds until the window resets.', 'schema': {'type': 'integer'}}),
    ])
    comp['responses'] = collections.OrderedDict()
    for st, (name, text) in RESPONSES.items():
        r = collections.OrderedDict([('description', text)])
        if st == 429:
            r['headers'] = collections.OrderedDict(
                (h, {'$ref': '#/components/headers/' + h}) for h in comp['headers'])
        r['content'] = {'application/json': {'schema': {'$ref': '#/components/schemas/Error'}}}
        comp['responses'][name] = r
    comp['schemas'] = SCHEMAS
    doc['components'] = comp
    return doc, ids


INFO = '''The apps, the web chat, bots and scripts all use this API. Nothing is reachable only through one client.

This file is written by `tools/openapi-gen/build.py`. Change the sources there, not this file.

Read `docs/API.md` for a guide, `docs/CONVENTIONS.md` for the rules this file follows, `docs/MESSAGE-FORMAT.md` for the message object, and `docs/EVENTS.md` for the live connection. `docs/AUTH.md`, `docs/SEALED.md`, `docs/BOTS.md`, `docs/CALLS.md`, `docs/PUSH.md` and `docs/LINK-PROTOCOL.md` say what the operations in their areas do in full.

In brief:

- Paths under `/api/v1` take and return JSON in UTF-8, except uploads (multipart) and downloads.
- Field names are `snake_case`. Identifiers are UUIDv7. Times are RFC 3339 in UTC with milliseconds. Absent and `null` mean the same; servers omit `null` fields. Unknown request fields are refused with `invalid_request`.
- Authenticate with the `churr_session` cookie or `Authorization: Bearer <token>`. A request that carries the cookie and is not a `GET` or `HEAD` must come from the server's own origin.
- Every failure has the `Error` shape. Each operation lists the codes it can return in its responses and again in `x-error-codes`. Something that exists but that the caller may not know exists answers `not_found`.
- Some operations need a fresh check: a passkey or authenticator-app check passed on this session in the last ten minutes. Without it they answer `fresh_check_required`; the client runs `beginPasskeyCheck` and `finishPasskeyCheck`, or `checkTotp`, and tries again.
- Lists ordered by `seq` take `before`, `after` and `limit` and return `has_more`. Filters by time are `since` and `until`. Other lists take `limit` and `cursor` and return `next_cursor`.
- Every response to an authenticated request carries `RateLimit-Limit`, `RateLimit-Remaining` and `RateLimit-Reset`.
- A few addresses are answered by the server but are not operations here: the form posts of its own pages, `POST /oauth/authorize` (approving an app) and `POST /sso/register` (the username at a first company sign-in); `GET /healthz`; `GET /link/v1`, where other servers and hubs connect (`docs/LINK-PROTOCOL.md`); the theme page at `/themes/{id}`; the web pages and the web chat; and, on port 80 when the server gets its own certificate, the certificate authority's challenge (every other request there is redirected to HTTPS).
- Each operation's description starts with who may call it. "Admins" includes the owner. "Acting as manager" means a manager of the channel, or an admin in a public channel or a private one they belong to.
'''


# ---- writing YAML: plain mappings, short scalar lists on one line, long text as blocks
class Dumper(yaml.SafeDumper):
    pass


def _dict(dumper, data):
    return dumper.represent_mapping('tag:yaml.org,2002:map', data.items())


def _list(dumper, data):
    flow = all(isinstance(x, (str, int, float, bool)) or x is None for x in data) and len(json.dumps(data)) < 90
    return dumper.represent_sequence('tag:yaml.org,2002:seq', data, flow_style=flow)


def _str(dumper, data):
    return dumper.represent_scalar('tag:yaml.org,2002:str', data, style='|' if '\n' in data else None)


Dumper.add_representer(collections.OrderedDict, _dict)
Dumper.add_representer(dict, _dict)
Dumper.add_representer(list, _list)
Dumper.add_representer(str, _str)
Dumper.ignore_aliases = lambda self, data: True

HEADER = '''# Churr HTTP API. OpenAPI 3.1.
# Written by tools/openapi-gen/build.py. Do not edit by hand: change the sources there and run it again.
# The rules are in docs/CONVENTIONS.md and the message object in docs/MESSAGE-FORMAT.md.
# If this file disagrees with either, they are right and the generator's sources need fixing.
'''


def refs_in(x, found):
    if isinstance(x, dict):
        for k, v in x.items():
            if k == '$ref':
                found.add(v)
            else:
                refs_in(v, found)
    elif isinstance(x, list):
        for v in x:
            refs_in(v, found)


def areas_table():
    rows = ['| Area | Operations |', '|------|------------|']
    for name, _ in TAGS:
        rows.append('| %s | %s |' % (name, ', '.join('`%s`' % o['id'] for o in OPS if o['tag'] == name)))
    return '\n'.join(rows)


def main():
    doc, ids = build()
    # The protobuf schema, from the same document; it also marks in doc where protobuf is accepted.
    proto_summary, proto_skipped = proto.write(doc, ROOT)
    text = HEADER + yaml.dump(doc, Dumper=Dumper, sort_keys=False, width=110, allow_unicode=True,
                              default_flow_style=False)
    with open(OUT, 'w') as f:
        f.write(text)
    gotable.write(OPS, ROOT)   # internal/api/operations_gen.go, from the same operations; see gotable.py

    problems = []
    loaded = yaml.safe_load(open(OUT))
    found = set()
    refs_in(loaded, found)
    for r in sorted(found):
        node = loaded
        for part in r[2:].split('/'):
            if not isinstance(node, dict) or part not in node:
                problems.append('unresolved $ref ' + r)
                break
            node = node[part]
    used = {r.split('/')[-1] for r in found if '/schemas/' in r}
    for n in sorted(set(SCHEMAS) - used):
        problems.append('schema never used: ' + n)

    # the table of areas in API.md
    begin, end = '<!-- areas:begin -->', '<!-- areas:end -->'
    if os.path.exists(API_MD):
        md = open(API_MD).read()
        if begin in md and end in md:
            a, b = md.index(begin) + len(begin), md.index(end)
            new = md[:a] + '\n' + areas_table() + '\n' + md[b:]
            if new != md:
                open(API_MD, 'w').write(new)
        else:
            problems.append('docs/API.md has no areas markers')
    for path in (API_MD, EVENTS_MD):
        if os.path.exists(path):
            names = set(re.findall(r'`([a-z]+[A-Z][A-Za-z0-9]*)`', open(path).read())) - {'operationId'}
            for n in sorted(names - ids):
                problems.append('%s mentions an operation that does not exist: %s' % (os.path.basename(path), n))

    try:
        import warnings
        warnings.simplefilter('ignore')
        from openapi_spec_validator import validate
        validate(loaded)
        checked = 'OpenAPI 3.1 validator passed'
    except ImportError:
        checked = 'openapi-spec-validator not installed, schema validation skipped'
    print('%s: %d lines, %d operations, %d paths, %d schemas. %s.'
          % (os.path.relpath(OUT, ROOT), text.count('\n'), len(ids), len(doc['paths']), len(SCHEMAS), checked))
    print(proto_summary)
    for p in proto_skipped:
        print('no protobuf form: ' + p)
    for p in problems:
        print('PROBLEM: ' + p)
    return 1 if problems else 0


if __name__ == '__main__':
    sys.exit(main())
