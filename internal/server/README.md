# How the server is put together

`server.New` builds every service and connects them. It is the only
place that knows all of them. `cmd/churr` and the tests start servers
through it.

## Layers

A package imports packages in lower layers, and `model`
(`docs/STRUCTURE.md` section 3). `imports_test.go` in the repository
root reads the import graph and fails when a rule is broken.

    9  server
    8  api
    7  auth         forms, sched, bots, calls, sealed, link, …
    6  people  chat
    5  live         push
    4  search       notify, mod, preview
    3  perm  blob
    2  store
    1  config  secret  ratelimit  markdown
    0  model  version

What is built now: `model`, `version`, `config`, `secret`, `ratelimit`,
`markdown`, `store`, `blob`, `perm`, `search`, `live`, `people`, `chat`,
`auth`, `api`, `server`, `testkit`. The rest are for the feature
builders.

## The order things are built in

`server.NewWith`, bottom up:

    store, keys, blobs
    perm   := perm.New(store)
    live   := live.New(perm, store, …)
    people := people.New(store, perm, live, origin)
    chat   := chat.New(chat.Deps{Store, Perm, Hub: live, Blobs})
    live.SetTailor(chat)
    search := search.New(store, perm)
    auth   := auth.New(auth.Deps{Store, Perm, People, Chat, Hub: live, …})
    api    := api.New(api.Deps{Auth, People, Chat, Search, Live, …})

## Small interfaces, not imports

A package never imports one in its own layer or above. When it needs
something from there, it declares a small interface saying what it
needs, and `server` hands it the implementation.

| Interface | Declared by | Implemented by | For |
|-----------|-------------|----------------|-----|
| `live.Tailor` | `live` | `chat` | Filling in what differs per reader in an event: `notify`, `reactions[].me` |
| `live.ClientHandler` | `live` | `calls`, `chat` (later) | Client frames by type prefix, through `Hub.Handle` |
| `chat.Hub` | `chat` | `live.Hub` | Publishing, sending to one account, presence |
| `chat.Blobs` | `chat` | `blob` | Removing the content of deleted files |
| `chat.Moderator` | `chat` | `mod` (later) | Slow mode and the word filter, after `perm` has said yes |
| `people.Presence` | `people` | `live.Hub` | Who is online |
| `auth.Hub` | `auth` | `live.Hub` | Closing the connections of a session that ended |

A feature package in layer 7 that needs another one in layer 7 does the
same. `calls.Passes` will be implemented by `link`; `chat.Outbound` by
`link`.

## Who decides what

| Question | Answered by |
|----------|-------------|
| May this account do this here? | `perm`, and nothing else. `perm.Load` reads the state once; `perm.Check`, `perm.CheckMessage`, `perm.CheckServer` are pure functions over it |
| What is stored, and what must hold between rows? | `store` |
| Who receives a live event? | `live`, by asking `perm.Recipients` |
| What may an account search? | `store`, joined to the `store.Scope` that `perm.Readable` builds |
| What does the request say, and what shape is the answer? | `api` |

A service never queries the store to answer an access question itself,
and a handler never decides one.

## Changing a conversation: `chat.Service.Tx`

Every change that produces a conversation event goes through
`chat.Service.Tx`, whichever package makes it:

    err := s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
        // 1. change rows through tx
        // 2. say what happened
        _, err := rec.Emit(ctx, model.Event{
            Type: model.EventPinAdded, ConversationID: conv, MessageID: msg,
            Payload: map[string]any{"pin": pin},
        })
        return err
    })

The recorder gives the event its `seq`, writes it to the catch-up log in
the same transaction, and after the commit publishes it to the hub. It
also keeps a conversation's events in `seq` order on the way out, which
a plain "commit, then publish" does not.

| Recorder method | For |
|-----------------|-----|
| `Emit` | An event: takes the seq, stores it, queues it |
| `Seq` and `EmitAt` | The event that creates a message, which has the message's seq |
| `Membership` | Turns a `store.MembershipResult` into `member.joined`, `member.left` and `member.level_changed`, and removes the blobs of deleted conversations |
| `After` | Something to do after the commit: an event that belongs to no conversation, sent with `Hub.Send` and not stored |
| `RemoveBlobs` | Blobs whose rows the transaction deleted |

An event carries the changed thing in full, in the shape the API returns
it, as nobody in particular would see it. `Event.Only` adds fields for
single accounts (the author's `client_id`); `Event.AlsoTo` names accounts
that get the event although they are not members (the one who just
left). What differs per reader is filled in by `chat.Tailor`, for live
delivery and catch-up alike. A payload is plain JSON by then, so a
tailor reads maps, not structs.

## Methods that end in `Tx`

A method whose name ends in `Tx` takes the caller's transaction and
publishes nothing. `people.CreateTx` makes an account; `auth` calls it
inside the transaction that also stores the passkey, the recovery
codes, the session and the invite use, and passes the memberships it
returns to `rec.Membership`.

## What a feature package provides

1. **A service** with a constructor that takes what it needs: the
   store, `perm`, and the services below it. No globals.
2. **Storage**, by filling in its area of `store` (`../store/README.md`).
3. **Rules**, by asking `perm`. If a rule is missing, it goes into
   `perm` with its test from `docs/PERMISSIONS.md`, not into the feature.
4. **Changes through `chat.Service.Tx`** when they produce conversation
   events, with the event names of `docs/EVENTS.md`.
5. **Handlers** in a file of its own in `api`, registered by
   `operationId` (`../api/README.md`), and a field in `api.Deps`.
6. **Wiring**: a few lines in `server.NewWith`, and an interface in the
   table above for anything it needs from its own layer or above.
7. **Tests**: storage tests in `storetest`, API tests through
   `testkit`, on both databases.

Marked places where a feature plugs in are comments starting
`TODO(<area>):`. `grep -rn 'TODO(' internal` lists them.
