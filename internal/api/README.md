# The API layer

`api` is the HTTP surface. A handler decodes a request, calls a service
and encodes the answer. It holds no rules: who may do what is `perm`'s,
reached through the services.

## The contract decides the routes

`docs/openapi.yaml` is the contract. `tools/openapi-gen` writes it, and
from the same operations writes `operations_gen.go`: a table of every
operation with its `operationId`, method, path, how it authenticates,
its area, its success status and its paging. Never edit either file by
hand.

`api.New` registers every operation of the table on its method and
path. A handler is attached by `operationId`:

    a.handle("postMessage", a.postMessage)

There is nowhere to write a path or a method, so neither can drift from
the contract. `handle` panics at start for an identifier the contract
does not have, or one registered twice. An operation with no handler
answers `503 unavailable` ("This operation is not built yet"), after the
same authentication as any other.

Tests that hold this together (`operations_test.go`):

| Test | Checks |
|------|--------|
| `TestTableMatchesContract` | `operations_gen.go` is what `docs/openapi.yaml` says |
| `TestEveryOperationRoutes` | every path of the contract can be registered without a conflict |
| `TestUnimplemented` | never fails; prints how many operations have a handler and rewrites `UNIMPLEMENTED.txt` |

## Adding an operation

One file per area: `conversations.go`, `messages.go`, `accounts.go`,
`signin.go`, `server_ops.go`. A new area is a new file; nothing else is
edited.

    func init() {
        register(func(a *API) {
            a.handle("pinMessage", a.pinMessage)
        })
    }

    func (a *API) pinMessage(r *Request) error {
        conv, err := PathID[model.ConversationID](r, "id")
        if err != nil {
            return err
        }
        msg, err := PathID[model.MessageID](r, "message_id")
        if err != nil {
            return err
        }
        pin, err := a.Chat.Pin(r.Ctx(), r.Actor(), conv, msg)
        if err != nil {
            return err
        }
        return r.JSON(map[string]any{"pin": pin})
    }

(`Chat.Pin` is not built; the example shows what its builder would write.)

A handler returns the error to send, or `nil` after writing the answer.
If the operation needs a service the API does not have yet, add it to
`Deps` and let `server` supply it.

## What `Request` gives a handler

| Need | Use |
|------|-----|
| Who is asking | `r.Actor()` (the account), `r.Who.Session`, `r.SignedIn` for optional authentication |
| A path identifier | `PathID[T](r, "id")`. One that is not a UUID answers `not_found`: it could name nothing |
| An identifier from a body or query | `BodyID[T]("field", value)`, which answers `invalid_value` with the field |
| The JSON body | `r.Decode(&body)`. Unknown fields, trailing data and bodies over 1 MB are refused, as CONVENTIONS requires |
| Query parameters | `r.queryInt`, `r.queryBool`, `r.queryTime` |
| Paging by cursor | `r.Page()`, and answer with `list("name", items, next)` |
| Paging by seq | `r.SeqPage()`, and put `has_more` in the answer |
| The answer | `r.JSON(v)` with the contract's status, `r.JSONStatus(code, v)` for the few operations with two (`postMessage`, `openChat`, `openSealedChat`: 200 for found, 201 for made), `r.NoContent()` |
| A list that may be empty | `orEmpty(items)`, so it is `[]` and never `null` |

Decode into a struct with exactly the contract's fields. Use a pointer
for a field whose absence matters (`*bool`, `*string`).

## Errors

Return a `*model.Error`; services and `perm` already do. `writeError`
puts it in the one shape:

    HTTP/1.1 403 Forbidden
    {"error": {"code": "forbidden", "message": "You are not in this channel."}}

Any other error is a failure of the server: it is logged and the client
gets `internal`, with nothing of the error in it. `rate_limited` and
`slow_mode` also set `Retry-After`. A path the contract does not have
answers `not_found` in the same shape.

Something that exists but that the caller may not know exists answers
`not_found`, never `forbidden`. That is decided in `perm` and the
services, not here; a handler must not turn one into the other.

## What every request passes through

`wrap` in `api.go`, in this order:

1. A panic is caught, logged and answered as `internal`.
2. The body is capped at 1 MB.
3. Until the owner exists, everything but the server's description and
   setup answers `unavailable`.
4. Authentication, by the kind the contract gives the operation:

   | Kind | Accepts |
   |------|---------|
   | `AuthNone` | Nothing is looked at |
   | `AuthOptional` | The cookie or a bearer token if there is one |
   | `AuthUser` | The `churr_session` cookie (`churr_ses_`), or `Authorization: Bearer` with an app (`churr_app_`), personal (`churr_pat_`) or bot (`churr_bot_`) token |
   | `AuthPending` | As `AuthUser`, and a session that still owes a second factor |
   | `AuthWeb` | The cookie only |
   | `AuthIntegration`, `AuthHook` | Left to the handler: integration tokens and webhook secrets have their own tables |

   A prefix names the only place a token works: a cookie value sent as a
   bearer token, or a webhook secret, is refused.
5. The origin check: a request that carries the cookie and is not a GET
   or HEAD must have the server's own `Origin`, or `Sec-Fetch-Site:
   same-origin` if it has none. The WebSocket upgrade is a GET, so
   `openEvents` applies the check itself.
6. A pending session can only use `AuthPending` operations; a recovery
   session only the short list in `recoveryAllowed`; a personal token
   nothing that manages sign-in (`personalDenied`, `personalDeniedAreas`).
7. The rate limit, per account, with `RateLimit-Limit`,
   `RateLimit-Remaining` and `RateLimit-Reset` on every authenticated
   answer.
8. The handler.

Limits on steps reachable without signing in (setup, registration,
sign-in, credentials that match nothing) are per address and live in
`auth`, which is told the address.

## Tests

`slice_test.go` and `chat_test.go` drive a whole server through
`testkit`: accounts are made by the real setup and registration with a
virtual passkey authenticator, and live connections are real
WebSockets. They run on SQLite, and on PostgreSQL under
`tools/pgtest.sh`.

    ts := testkit.NewServer(t)                 // owner already set up
    ann, bob := ts.Member("ann"), ts.Member("bob")
    ch := ann.Channel("ops", true)
    bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+ch, nil)
    stream, _ := ann.Events(nil)               // hello, resume, ready
    ann.Post(ch, "hello")
    e := stream.Expect("message.created")

Every new operation gets a test of what it does and of each refusal the
contract lists for it. The numbered cases at the end of
`docs/PERMISSIONS.md` that need the API are tested here and cite their
number in a comment.
