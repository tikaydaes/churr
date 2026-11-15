# openapi-gen

Writes `docs/openapi.yaml`. That file is the output of this folder and
is never edited by hand: change the sources here and run the build
again.

## Regenerate

From the repository root:

    python3 tools/openapi-gen/build.py

It needs Python 3.9 or later and PyYAML. To also validate the result
against OpenAPI 3.1, install `openapi-spec-validator`; the build uses
it when it is there. One way to get both without touching the system:

    python3 -m venv /tmp/churr-openapi
    /tmp/churr-openapi/bin/pip install pyyaml openapi-spec-validator
    /tmp/churr-openapi/bin/python tools/openapi-gen/build.py

The build prints one line with the counts, then one line per problem,
and exits with status 1 if there was any.

## What the build does

1. Writes `docs/openapi.yaml`.
2. Writes `internal/api/operations_gen.go`: a Go table of every operation
   (its `operationId`, method, path, how it authenticates, its area, its
   success status and its paging). The server registers its handlers
   against this table by `operationId`, so a path or method cannot drift
   from the contract. A Go test (`internal/api/operations_test.go`)
   reads `docs/openapi.yaml` and fails if the table is stale.
3. Rewrites the table of areas in `docs/API.md`, between the comments
   `<!-- areas:begin -->` and `<!-- areas:end -->`.
4. Writes the Protocol Buffers form of the same contract
   (`docs/WIRE-PROTOBUF.md`): `proto/churr/v1/churr.proto`, the lock
   file that keeps its numbers, and the schema tables of the Go and
   Dart codecs (`internal/wirepb/schema_gen.go`,
   `app/lib/core/wire/schema.g.dart`). It also adds the protobuf media
   type to every body of `docs/openapi.yaml` that has a message. After
   a change here, run `tools/protogen.sh` in place of the build alone:
   it runs the build, then `protoc` for the app's classes, then
   rewrites the test vectors.
   The same step writes the link protocol's schema, which is not
   derived from the API: `link.py` declares what servers and hubs say
   to each other (`docs/LINK-PROTOCOL.md` section 2), and the build
   writes `proto/churr/link/v1/link.proto`, its own lock file and the
   table the link's codec walks (`internal/link/wire/schema_gen.go`).
5. Checks that the YAML parses, every `$ref` resolves, every
   `operationId` is unique, every schema is used, every error code is
   one of those in `docs/CONVENTIONS.md`, and every `operationId`
   that `docs/API.md` or `docs/EVENTS.md` mentions exists.

## The sources

| File            | Holds                                                |
|-----------------|------------------------------------------------------|
| `lib.py`        | The short notation the other files are written in. Read its opening comment first. |
| `schemas.py`    | Every object: `Account`, `Conversation`, `Message` and its parts, and the rest. |
| `ops_signin.py` | Server information, setup, sign-in, OAuth for the apps. |
| `ops_me.py`     | The caller's own account, status, other accounts, groups. |
| `ops_chat.py`   | Conversations, messages, forms, threads, files, search, catch-up, scheduling, commands. |
| `ops_bots.py`   | Bots, webhooks and their intake, calls, server integrations. |
| `ops_sealed.py` | Sealed chats and the key backup.                     |
| `ops_admin.py`  | Invites and everything under `/admin`.               |
| `link.py`       | The link protocol: its frames, what is signed, and the plaintext of direct messages and of events. Its opening comment gives its notation. |
| `ops_themes.py` | Colour themes: an account's own, the library, the server's defaults (and their two `/admin` operations). |
| `events.py`     | Every frame of the live connection (`docs/EVENTS.md`, `docs/CALLS.md` section 10), for the protobuf schema. A new event or a new field of one goes here. |
| `proto.py`      | Writes the protobuf schema, its lock and the codec tables. Called from `build.py` by one line. |
| `gotable.py`    | Writes `internal/api/operations_gen.go`, the Go table of operations. Called from `build.py` by one line. |
| `build.py`      | Puts it together: paths, shared parameters, error responses, security schemes, the checks. |

## Adding or changing an operation

An operation is one call:

    op('POST conversations/{id}/lock', 'lockConversation',
       'Lock a private channel for good',
       who='Managers of a private channel, named or ad-hoc.',
       desc='One-way. ...',
       res={'conversation': '#Conversation'},
       err='forbidden not_found conflict')

- A path that does not start with `/` is under `/api/v1/`.
- `who` becomes the first line of the description. Every permission
  statement must agree with `docs/PERMISSIONS.md`.
- `err` lists the codes beyond the ones the build adds by itself
  (`invalid_request`, `invalid_value`, `unauthenticated`,
  `second_factor_required`, `rate_limited`, `internal`, as they apply).
- A path parameter with a new name needs a line in `PATH_PARAMS` in
  `build.py`.
- A new error code needs a line in `STATUS` in `build.py`, after it
  has been added to `docs/CONVENTIONS.md`.

Object shapes follow `docs/MESSAGE-FORMAT.md`, `docs/SEALED.md` and
the other documents exactly. If one of them changes, change
`schemas.py` to match and run the build.
