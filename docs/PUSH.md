# Notifications when the app is not in front

How a person finds out about a message or a call when they are not
looking at Churr: phone wake-ups through the relay, UnifiedPush, the
Android foreground service, the status endpoint, the desktop tray and
browser notifications. `DESIGN.md` section 12 is the outline this
document fills in. Nothing here is built yet.

Two placeholders stand for things the owner has not chosen:

- `<RELAY-ADDRESS-NOT-CHOSEN>` is the public host name of the relay
  Tikaydaes runs.
- `<APP-ID-NOT-CHOSEN>` is the apps' bundle identifier (iPhone) and
  application identifier (Android).

Both are compiled into the published apps and are hard to change later.

## 1. The parts

| Part | Role |
|------|------|
| `push` package in the server | Decides who to wake, makes the wake-up, posts it to the relay or to a UnifiedPush endpoint, forgets dead addresses |
| `churr-relay` | Turns a wake-up into an APNs or FCM request. Holds the Apple and Google keys. Stores nothing |
| Notification code in the apps | Receives the wake-up, fetches the real content from the person's own server, shows it |
| The status endpoint | Cheap totals for the icon badge, and the fallback when no wake-up arrives |

A wake-up never contains text, a name or a conversation. It says "ask
your server about this" and nothing else.

## 2. When an event wakes an account's phones

The rule lives in one place, the `notify` package, and is used for
three things: the per-recipient hint on live events (section 12),
badges, and phone wake-ups. This section is the wake-up part.

Only two kinds of event can wake a phone: a new message, and a call
that rings. Edits, deletes, reactions, checklist ticks, form answers,
pins, typing, presence and system messages other than a call starting
never do.

For a new message M by author A in conversation C, the server looks at
each local account P that can read C and asks these questions in order.
The first "no" ends it.

1. **Is P someone to notify at all?** P is a person (not a bot, not
   the shadow of an account on another server; that account's own
   server decides for it). P is not A. The one exception is a reminder
   P set for themselves, which arrives under their own name and does
   notify.
2. **Has P blocked A?** If blocking is on for the server and P has
   blocked A, stop.
3. **Does P's mode for C notify for this message?**

   | P's mode | Notifies for |
   |----------|--------------|
   | Any activity | Every top-level message. A reply if P follows its thread, or if P views C flat and has not ignored the thread. Every mention of P |
   | Mentions | A mention of P by name, of a group P is in, `@channel`, or `@here` if P is online. A reply in a thread P follows |
   | Nothing (`nothing`), or muted | Nothing |

   - Only `mode` and `muted_until` are looked at. Whether the
     conversation has a row in P's sidebar (`show`) plays no part: a
     hidden conversation notifies and wakes phones like any other.
   - A thread P has ignored notifies only for a mention of P by name.
   - A mention inside a card never notifies (`MESSAGE-FORMAT.md`).
   - `@here` reaches only people who are online. An online person has
     an active client, so step 5 stops it: `@here` never wakes a phone.
   - In a sealed chat the server cannot see what a message is. The
     sending device marks edits, reactions and the like as `quiet`
     (`docs/SEALED.md` section 8.4), and a quiet message never
     notifies.
4. **Is P in do not disturb?** P is in do not disturb when a manual do
   not disturb is on, or it is inside P's quiet hours, or P's status
   has "pause notifications" set, or P is in a meeting and has chosen
   that meetings pause notifications. If so, stop. Turning do not
   disturb off clears all four at once (the manual one, the status's
   pause, the current quiet period and the pause from the current
   meeting), so the account is available from that moment.

   **Notify anyway** is the one way through. The author may ask, for
   a message they typed, that it be pushed through to one person. The
   request is stored as a row in `notify_anyway` and the message
   carries the public "notified anyway" mark whenever its author used
   it. The server then notifies that person as if they were not in do
   not disturb only when all of these hold: they are in do not
   disturb, they allow notify anyway, they have not blocked A, A is a
   person, they can read C, and step 3 passes because of a mention of
   them by name or because they have C on Any activity. Otherwise it
   quietly does nothing (`delivered` is false on the row). The answer
   to the author is the same either way, and no response, event or
   count shows whether a target was reached, so the feature cannot be
   used to learn who has muted or blocked whom. The request comes
   after the message, so a wake-up it causes is sent then, with the
   same event identifier the message would have had.
5. **Does P have a client that is connected and active?** A client is
   active when it has a live connection open and has reported the
   person using it within the idle time that turns presence to `away`
   (five minutes, in `docs/EVENTS.md`). If one is, stop: that client shows
   the notification itself. A desktop app sitting idle in the tray is
   connected but not active, so the phone is still woken.
6. **Does P have a phone to wake?** For each of P's devices whose push
   kind is `apns`, `fcm` or `unifiedpush` and whose address has not
   been forgotten (section 6), send one wake-up.

A call rings under the rules in `CALLS.md` section 15. When it rings
for P, steps 1, 2, 4, 5 and 6 above apply, with one difference: notify
anyway does not exist for calls, so do not disturb always stops a ring.

The server does not send a wake-up at all when the answer is no. It is
not sent and then hidden by the phone.

**Priority.** A wake-up is `high` when it is a ring, a mention of P by
name, a message in an ad-hoc channel or a sealed chat, or a reply in a
followed thread. It is `normal` for everything else, which in practice
is chatter in a named channel P has on Any activity, and group and
`@channel` mentions.

**Spreading.** One message that notifies many people (a broad
mention in a large channel) does not send all its wake-ups in the
same instant, which would have that many phones come back to the
server for the message together. The people whose wake-up is `high`
go first; then the server sends at most 200 wake-ups a second for
that message, a tenth of a second's share at a time, until all are
sent. A message that wakes fewer than twenty phones is not held back
at all. Who has a phone to wake is read for everyone at once
(ruling R64).

**Things this rule does not do.** It does not wait to see whether an
active client read the message before waking the phone, and it does not
send a second wake-up to clear a notification that was read elsewhere.
The phone clears its notifications the next time it talks to the
server (section 9).

## 3. Registering a device

A device row belongs to one app session (`devices` in `schema.sql`) and
goes away with it when the person signs out or the session ends.

An app registers its device straight after sign-in, again each time it
starts, and again whenever the operating system hands it a new push
address. Registering is idempotent.

| Field | Values | Notes |
|-------|--------|-------|
| `name` | Text | "Kaity's phone". Shown in the device list |
| `platform` | `android`, `ios`, `macos`, `windows`, `linux`, `web` | |
| `push_kind` | `apns`, `fcm`, `unifiedpush`, `none` | How this device is woken |
| `push_address` | Text, up to 4,096 characters | The APNs device token in hexadecimal, the FCM registration token, or the UnifiedPush endpoint URL. Absent for `none` |
| `push_public_key`, `push_auth` | Bytes | Only with `unifiedpush`, and only when the distributor wants wake-ups encrypted as Web Push: the device's public key and shared secret. Stored in the columns of the same names |

| Device | `push_kind` |
|--------|-------------|
| iPhone app | `apns` |
| Android app from the Play Store | `fcm` |
| Android app from F-Droid with a UnifiedPush distributor installed | `unifiedpush` |
| Android app from F-Droid with no distributor (foreground service) | `none` |
| Desktop apps, web chat | `none` |

The server's answer to a registration carries the device's `id` and a
`wake_route`: eleven characters the app stores beside the server it
belongs to. One app can be signed in to several servers and every one
of them is given the same push address, so the phone uses the route to
tell which server a wake-up came from (section 4).

The server refuses a `push_kind` it cannot serve:

- `apns` and `fcm` when the relay is switched off. The app then falls
  back to the status endpoint, and on Android offers the foreground
  service.
- `unifiedpush` when UnifiedPush is switched off, or when the endpoint
  is not an `https` address.

Both refusals use `feature_off`. The server's public description (the
"server" operation) says which push kinds it accepts, so an app can
choose before it asks.

## 4. The wake-up

The server makes one wake-up per device per event. It has four values.

**Event identifier.** `<wake_route>.<token>`, at most 256 characters
from `[A-Za-z0-9_.-]`.

- `wake_route` is the first eight bytes of
  HMAC-SHA-256(server seal key, "wake-route" ‖ device id), in
  base64url. It is the same for every wake-up to one device from one
  server and means nothing to anyone else.
- `token` is a random 12-byte nonce followed by an AES-256-GCM
  encryption, under the server's seal key in `keys/`, of: the device
  id, the kind (`message` or `ring`), the conversation id, the `seq`
  (for a message) or call id (for a ring), and the time it was made.
  In base64url.

Only the server that made it can read it, so no table of pending
wake-ups is needed. The server accepts a token back for seven days and
only from the session of the device it names.

**Collapse key.** The first sixteen bytes of HMAC-SHA-256(server seal
key, "wake-collapse" ‖ device id ‖ conversation id) in base64url: 22
characters. Two wake-ups for the same conversation on the same device
share a key, so the newer one replaces the older one on screen. The
key differs per device, so nobody outside can tell that two phones were
woken about the same conversation. A ring and the later news that the
ring stopped use the conversation's key too.

**Priority.** `high` or `normal` (section 2).

**Badge.** The account's total badge after this event, as worked out by
the server (the sum of its conversations' badges), or none. It is none
when the admin has switched off sending badge numbers
(`push.send_badge`, section 14), and none on a wake-up that reports a
ring stopping.

## 5. The relay protocol

One endpoint. The published apps' relay is at:

    POST https://<RELAY-ADDRESS-NOT-CHOSEN>/v1/wake
    Content-Type: application/json

There is no authentication and no account. Any Churr server may call
it. A server's admin can point at a different relay (`push.relay_url`),
which helps only someone who also builds and signs their own apps,
because the relay must hold the push keys of the app it wakes.

### Request

```json
{
  "push_address": "8a1f…64 or more characters…c2",
  "platform": "ios",
  "event_id": "q83vEjRWeJA.AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
  "collapse_key": "Zm9vYmFyYmF6cXV4MTIzNA",
  "priority": "high",
  "badge": 7
}
```

| Field | Required | Rule |
|-------|----------|------|
| `push_address` | Yes | 1 to 4,096 characters. The APNs or FCM token exactly as the device registered it |
| `platform` | Yes | `ios` (sent to APNs) or `android` (sent to FCM) |
| `event_id` | Yes | 1 to 256 characters from `[A-Za-z0-9_.-]`. The relay does not look inside it |
| `collapse_key` | Yes | 1 to 64 characters from `[A-Za-z0-9_-]` |
| `priority` | Yes | `high` or `normal` |
| `badge` | No | Whole number from 0 to 99,999. Absent or `null` means none |

Nothing else is accepted. An unknown field is refused with
`invalid_request`, so a server cannot send text by mistake and a
changed relay cannot quietly start taking more. The body may be at most
8 KB.

### Responses

The relay forwards to Apple or Google before it answers, so the answer
says what happened. Errors use the shape in `CONVENTIONS.md`: every
field, `scope` and `platform_off` included, sits inside `error`. A
relay started with no keys at all answers every wake-up with 503 and
`platform_off`. One started with `-dry-run` accepts and counts
wake-ups and sends nothing (`OPERATIONS.md`).

| Status | Body | Meaning | What the server does |
|--------|------|---------|----------------------|
| 200 | `{"result": "sent"}` | Apple or Google accepted it. Not proof it reached the phone | Clears the device's failure mark |
| 400 | `invalid_request`, or `invalid_value` with `field` | The request is malformed | Logs it once. Does not retry. This is a bug |
| 410 | `{"error": {"code": "gone", "message": "…"}}` | Apple or Google says the address is no longer valid, or belongs to a different app | Forgets the address (section 6) |
| 413 | `too_large` | Body over 8 KB | Does not retry |
| 429 | `rate_limited` with `retry_after_seconds`, and `scope`: `device` or `address` | A relay limit was hit, or Apple or Google asked the relay to slow down for this device | Tries once more after the stated time if that is under two minutes. Otherwise drops the wake-up |
| 502 | `unavailable` | Apple or Google could not be reached or answered with a server error | Retries |
| 503 | `unavailable`, with `platform_off: true` when the relay has no key for that platform | The relay is overloaded, or not configured for this platform | Retries, except with `platform_off`, which it treats as 400 |

### Retries

A wake-up that failed with 502, 503, a timeout (ten seconds) or a
network error is tried again after 5 seconds, 30 seconds and 2 minutes,
then dropped. Wake-ups waiting to be retried are held in memory, at
most 10,000, oldest dropped first. A restart loses them. Nothing is
lost for good by a dropped wake-up: the message is on the server, the
badge is in the status endpoint, and the app catches up when opened.

The server keeps a small pool of connections to the relay open and
sends wake-ups one request each. It does not batch, because a batch
would tell the relay which devices were woken together.

## 6. When the server forgets an address

`devices.push_failed_at` marks the first failure in the current run of
failures. Any success clears it.

| What happened | Result |
|---------------|--------|
| The relay answered 410 `gone` | `push_address` is cleared at once and `push_failed_at` is set. No more wake-ups go to that device |
| A UnifiedPush endpoint answered 404 or 410 | The same |
| Every attempt has failed for seven days running (`push_failed_at` is older than that) | The same |
| The session ended (sign-out, expiry, revoked by the person or an admin) | The device row is deleted with the session |
| The app registers again, with the same address or a new one | The address is stored and `push_failed_at` cleared |

An app registers on every start, so a phone whose address was forgotten
by mistake puts itself right the next time it is opened. Until then it
relies on the status endpoint.

## 7. Inside the relay

`churr-relay` is one Go program, built without cgo. It has no database
and no data directory beyond the certificate folder it needs when it
fetches its own certificate. Its commands and flags are in
`OPERATIONS.md` section 3.

It makes two kinds of outbound connection: to Apple's push service and
to Google's. It also fetches an access token from Google's token
address about once an hour.

### APNs

- **Connection.** HTTP/2 to `api.push.apple.com` on port 443, kept
  open. A relay started with `-apns-sandbox` talks to
  `api.sandbox.push.apple.com` instead, for development builds.
- **Authentication.** Token-based. The relay holds the `.p8` signing
  key, its key identifier and the team identifier. It signs a token
  (ES256, with the key identifier in the header and the team identifier
  and issue time in the body) and sends it as
  `authorization: bearer <token>`. One token is reused for 50 minutes
  and then replaced. Apple refuses tokens older than an hour and
  refuses a relay that makes new ones too often.
- **Request.** `POST /3/device/<push_address>` with:

  | Header | Value |
  |--------|-------|
  | `apns-topic` | `<APP-ID-NOT-CHOSEN>` |
  | `apns-push-type` | `alert` |
  | `apns-priority` | `10` for `high`, `5` for `normal` |
  | `apns-collapse-id` | The collapse key |
  | `apns-expiration` | Now plus three days |

- **Payload.**

  ```json
  {
    "aps": {
      "alert": {"loc-key": "WAKE_FALLBACK"},
      "mutable-content": 1,
      "sound": "default",
      "badge": 7
    },
    "e": "q83vEjRWeJA.AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
    "c": "Zm9vYmFyYmF6cXV4MTIzNA"
  }
  ```

  - `loc-key` names a string in the app, "New message" in English, so
    the placeholder is in the phone's language and the relay holds no
    wording.
  - `mutable-content` lets the app's notification extension change the
    notification before it is shown (section 8).
  - `badge` is left out when the request had none.
  - `e` is the event identifier and `c` the collapse key.

- **Answers.** 200 becomes `sent`. 410 `Unregistered`, and 400 with
  `BadDeviceToken` or `DeviceTokenNotForTopic`, become `gone`. 429
  `TooManyRequests` becomes `rate_limited` with scope `device`. 403
  means the relay's own key is wrong: it answers 503 and counts it
  loudly. 5xx and network errors become 502.

### FCM

- **Authentication.** The relay holds a Google service account file.
  It exchanges a signed assertion for an access token with the
  `firebase.messaging` scope and renews it before it runs out.
- **Request.** HTTP v1:
  `POST https://fcm.googleapis.com/v1/projects/<project>/messages:send`
  with `authorization: Bearer <access token>`. The project identifier
  comes from the service account file.
- **Body.** A data message. There is no `notification` part, so
  Android shows nothing by itself and hands the message to the app.

  ```json
  {
    "message": {
      "token": "<push_address>",
      "data": {
        "e": "q83vEjRWeJA.AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
        "c": "Zm9vYmFyYmF6cXV4MTIzNA",
        "b": "7"
      },
      "android": {"priority": "HIGH", "ttl": "259200s"}
    }
  }
  ```

  - `priority` is `HIGH` or `NORMAL`. `b` is left out when there is no
    badge. Data values are strings because FCM requires that.
  - The collapse key travels as data, not as FCM's own `collapse_key`.
    FCM keeps only a few different collapse keys per device while the
    device is offline and does not say which it drops. The app uses `c`
    itself, as the tag of the notification it shows.

- **Answers.** 200 becomes `sent`. `UNREGISTERED` (404), and
  `INVALID_ARGUMENT` (400) naming the token, become `gone`.
  `SENDER_ID_MISMATCH` (403) becomes `gone`: the token belongs to a
  different app. `QUOTA_EXCEEDED` (429) becomes `rate_limited`.
  `UNAVAILABLE` and `INTERNAL` become 502.

### Rate limits

Held in memory as token buckets. They reset when the relay restarts.

| Limit | Default | Flag |
|-------|---------|------|
| Per push address | 60 a minute, 2,000 a day | `-device-rate`, `-device-daily` |
| Per calling address (IPv4 address, or IPv6 /64) | 600 a minute | `-address-rate` |
| Requests in progress at once | 2,000 | `-max-inflight` |

- The buckets are keyed by a hash of the push address or the calling
  address, salted with a random value made at start, so a memory dump
  does not list push addresses.
- Behind a proxy the calling address is taken from `X-Forwarded-For`,
  believed only from the proxies named in `-trusted-proxies`.
- A bucket not touched for 24 hours is dropped.

### What the relay keeps

- **On disk:** nothing but its certificate, when it fetches its own.
- **In memory:** the rate-limit buckets, and counters.
- **Counters:** requests received, and results by platform and
  outcome (`sent`, `gone`, `rate_limited`, `invalid`, `upstream_error`),
  since start and for the current hour. Nothing else.
- **In the log:** one line of counters each hour, start and stop, and
  errors about its own keys or its connections to Apple and Google.
  It never logs a push address, an event identifier, a collapse key or
  a calling address. There is no access log.
- **Endpoints:** `POST /v1/wake`, `GET /healthz` (answers `ok`), and
  `GET /`, which answers one plain-text paragraph saying what this is.
  There is no status page and no way to read the counters from outside.

### What the relay cannot prevent

Anyone who learns a device's push address can post to the relay and
make that phone show "New message": the phone asks its server about an
event identifier the server does not recognise, and falls back to the
placeholder. Push addresses are not public, the per-device limit caps
the nuisance, and the address is replaced when the app is reinstalled.
Closing this hole needs servers to hold credentials at the relay, which
is the account system the relay is designed not to have.

## 8. What the phone does when woken

### iPhone

The app ships a notification service extension, written in Swift,
which iOS starts when a push with `mutable-content` arrives. It has
about thirty seconds and little memory. It does this:

1. Reads `e` and splits off the route. Finds the server with that route
   in the list the app keeps in the shared app-group container. No
   match: show the placeholder and stop.
2. Reads that server's app token from the keychain group it shares
   with the app.
3. Calls the server's wake-up fetch operation with the token and the
   event identifier, with a ten-second timeout. The answer (section 9)
   holds what to show.
4. If the conversation is sealed, checks and decrypts the message as
   `docs/SEALED.md` section 11 says. It reads the content key for the
   message's key epoch, and the sender's device key, from storage the
   app shares with it, which the app fills whenever it enters an
   epoch: the keychain group the app and the extension share. The
   decrypt is a small piece of Swift on CryptoKit (`CLIENTS.md`
   section 5), checked against the same test vectors as every other
   implementation. Decrypting changes no key, so the app and the
   extension cannot get in each other's way. Without the key for that
   epoch it shows the placeholder.
5. Replaces the notification's content:
   - title: the sender's display name, with the channel name for a
     named channel ("Sam in #ops");
   - body: the message as plain text, up to 300 characters, or a line
     for what it holds instead of text ("Sent a file", "Posted a
     poll");
   - thread identifier: the conversation's identifier, so iOS groups
     notifications by conversation;
   - badge: the total from the answer;
   - what opening it should show: the server, conversation and message.
6. For a ring, sets the ring sound and the time-sensitive level, and
   words it "Sam is calling" (section 13).

If any step fails, including an expired token it cannot renew, the
extension leaves the placeholder as it is: "New message", with the
badge the relay sent. If the extension runs out of time, iOS shows the
placeholder by itself.

**Tokens.** The extension uses the app's access token. If the server
answers `unauthenticated`, the extension takes the same file lock the
app uses for renewing, reads the keychain again in case the app has
just renewed, and only then renews. This is the arrangement Codemail's
app and background service use.

**What iOS will not let the extension do.** It cannot cancel the
notification. A wake-up for something the person has already read is
shown anyway. Suppressing it needs an entitlement Apple grants case by
case, and version 1 does not depend on getting it.

### Android, through FCM

The data message starts the app's background handler, a Dart entry
point with no screen. It does steps 1 to 5 above in Dart, then posts
the notification itself:

- one notification per conversation, with the collapse key as its tag,
  in the messaging style, so new lines are added to it as messages
  arrive;
- on a channel the person can configure in Android's settings:
  "Mentions and chats", "Channel activity", "Calls";
- with "Mark as read" and "Reply" actions that call the server
  directly.

Unlike iOS, Android lets the app show nothing. If the fetch says the
event has already been read, no notification is posted. If the fetch
fails, the app posts "New message" under the collapse key's tag, so
repeated failures for one conversation do not stack up.

### While the app is locked

A locked app's session (`AUTH.md` section 10.6) is still woken: the
server does not know what a wake-up is for until the phone asks, and a
person who locked their phone app still wants to know that something
arrived (the owner's decision). But the wake-up fetch of a locked
session answers 401 `session_locked`, so the phone learns nothing more,
and shows one notification that says only **"You have a new
notification"**, titled "Churr": no server, conversation, sender, text
or count. On Android every such wake-up uses the one tag
`churr-locked`, so they replace each other and nothing is told apart
by conversation; no badge is set. On iPhone the extension sees the
refusal and writes the same words, with no badge and one thread
identifier. The app does not renew its token or retry for it. After
unlocking, the app catches up as after any reconnection.

### After any wake-up

The app also calls the status endpoint (section 11), sets the icon
badge from it, and removes notifications for conversations whose read
position has moved past them.

## 9. The wake-up fetch

One operation, described here in words because the paths belong to
`docs/openapi.yaml`. The app gives the event identifier and its app
token. The server:

1. Splits off the token and decrypts it. A token it cannot decrypt, or
   that is older than seven days, is `not_found`.
2. Checks that the device named inside belongs to the calling session.
   Otherwise `not_found`.
3. Checks with `perm` that the account can still read the
   conversation. Otherwise `not_found`.
4. Answers with what the phone needs to draw one notification:

   | Field | Contents |
   |-------|----------|
   | `kind` | `message` or `ring` |
   | `conversation` | Identifier, kind, name if it has one, and for an ad-hoc channel or sealed chat the title to show |
   | `messages` | The messages in that conversation that notify this account and are after its read position, newest last, at most five, each in the ordinary message shape with the author's display name beside it. For a sealed chat they carry ciphertext |
   | `more` | How many more there are beyond those five |
   | `read` | `true` if the account's read position is already past the event |
   | `call` | For a ring: the call identifier, who is calling, and `ringing`: whether it should still ring (section 13) |
   | `badge` | The account's total badge now |

The fetch does not move the read position. It is rate-limited per
device.

## 10. UnifiedPush and the foreground service

Both are for the Android build without Google libraries (F-Droid).
The app picks one at sign-in and the person can change it in
Settings → Notifications.

### UnifiedPush

If the phone has a UnifiedPush distributor installed, the app asks it
for an endpoint and registers that as the push address, with push kind
`unifiedpush`.

To wake the device the server posts straight to the endpoint. The relay
is not involved, and neither is Tikaydaes.

    POST <the endpoint the device registered>
    Content-Type: application/json

    {"event_id": "q83vEjRWeJA.AAEC…", "collapse_key": "Zm9v…", "priority": "high", "badge": 7}

It is the same wake-up with `push_address` and `platform` left off. The
distributor hands the body to the app, which then does what the FCM
handler does (section 8).

If the device registered a `push_public_key` and `push_auth`, the
server encrypts that body to them as Web Push (RFC 8291) before
posting, because some distributors accept nothing else. The body is
still content-free either way.

The endpoint is an address a member supplied, so the server treats it
as it treats a link to preview:

- `https` only, port 443 only;
- the host must resolve to public addresses, and the check is made
  again at connection time. Private, loopback and link-local ranges are
  refused, unless the operator starts the server with
  `-unifiedpush-allow-private` because their distributor is on their
  own network;
- no redirects are followed, the timeout is ten seconds, and the
  answer's body is not read beyond 1 KB;
- any 2xx is success. 404 and 410 forget the address. 429 and 5xx are
  retried on the schedule in section 5.

UnifiedPush has its own switch, `push.unifiedpush_enabled`, and it is
**off** by default, because the brief allows only one outside
connection to be on by default and that is the relay. It has its own
row in the table of outside connections (`DESIGN.md` section 17), and
first-run setup shows the switch beside the relay's. While it is off,
the F-Droid app uses the foreground service.

### Foreground service

With no distributor, or with UnifiedPush switched off on the server,
the app offers to stay connected. This is how Codemail's Android app
works.

- A foreground service runs in its own isolate with a permanent
  notification ("Churr is connected to 2 servers"). It keeps one
  WebSocket open per server.
- It keeps running after the app is swiped away and starts again when
  the phone restarts.
- It shows notifications from the live events it receives, using the
  per-recipient hint on each event (section 12), so it applies the
  same rule as a wake-up without working the rule out itself.
- It does not report activity, so it never makes the person look
  online and never counts as an active client.
- The app and the service share one saved session per server. Each
  checks whether the other has renewed the tokens before renewing
  them.
- Its cost is battery, and the permanent notification. The app says so
  when it offers the mode, and asks to be exempted from battery
  optimisation, without which Android will stop the service.

The device registers with push kind `none`.

## 11. The status endpoint

`GET status` in `DESIGN.md` section 7. It is what keeps the icon badge
right, and what a phone has when no wake-up arrives: the relay is off
or down, the address was forgotten, or the platform delayed delivery.

```json
{
  "badge": 7,
  "mentions": 2,
  "unread_conversations": 4,
  "unread_threads": 1,
  "newest": "0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f",
  "ringing_call_id": null,
  "dnd": false,
  "dnd_until": null
}
```

| Field | Meaning |
|-------|---------|
| `badge` | The number for the app icon: the sum of the badges of the account's conversations, each counted according to its mode. A conversation counts whether or not it has a row in the sidebar |
| `mentions` | How many unread messages mention the account by name, a group it is in, `@channel` or `@here` |
| `unread_conversations` | How many conversations in the account's list have anything unread |
| `unread_threads` | How many followed threads have unread replies |
| `newest` | An opaque value that changes whenever something newer exists for this account: in practice the identifier of the newest message in any of its conversations. A client compares it with the last one it saw to decide whether to connect and catch up |
| `ringing_call_id` | A call that is ringing for this account right now, if any |
| `dnd`, `dnd_until` | Whether the account is in do not disturb, from any cause, and when that ends if it is known |

**Cheap.** The server keeps these totals per account in memory, updates
them as events pass through, and rebuilds them from the database when
an account is first asked about after a restart. Answering does not
query the database.

**Caching.**

- The response carries a strong `ETag`, a hash of the body, and
  `Cache-Control: private, no-cache`. The body has no clock time in
  it, so it changes only when something changed.
- A client sends `If-None-Match` and gets `304 Not Modified` with no
  body when nothing changed.
- The endpoint has its own rate limit: 30 a minute per session.

**When clients call it.**

| Client | When |
|--------|------|
| Phone apps | When woken by a push, before or after showing the notification. On the operating system's background refresh, which is a few times an hour at best and not guaranteed. After the live connection reconnects. When the app comes to the front, before the connection is up |
| Android foreground service | Not at all. It is connected |
| Desktop apps | After reconnecting, to set the tray and dock badge before catch-up finishes |
| Web chat | Not at all. It is connected while open |
| Scripts | Whenever they like, within the rate limit |

On background refresh, if `newest` has changed and no wake-up brought
it, the phone app may show one quiet notification: "New activity on
<server name>". It does not fetch messages in the background to do
better than that. A ringing call found this way is shown as a ring if
it is still ringing.

## 12. The hint on live events

A client that is connected does not get wake-ups. It needs to know, for
each event it receives, whether to make a sound or show a pop-up, and
it cannot work that out: group mentions, thread states, blocks and do
not disturb are decided by the server.

So each message event delivered over the live connection carries, for
its recipient, a `notify` value worked out by the same rule as section
2, steps 1 to 4:

| `notify` | Meaning | What a connected client does |
|----------|---------|------------------------------|
| `none` | The mode does not count this message | Nothing |
| `badge` | It counts towards the badge and would not have notified (a name mention in a conversation whose mode is `nothing` or that is muted, or the account is in do not disturb) | Updates badges |
| `notify` | It would have notified | Updates badges and shows a notification, unless the conversation is on screen and the window is in front |

This field is something `docs/EVENTS.md` has to define. It is listed
under "Needs from other documents" at the end.

## 13. Each kind of client

### Desktop apps: the tray

- The app stays running with its WebSocket open when its window is
  closed. Closing the window hides it; Quit is in the tray menu and
  the application menu. A setting, off by default, starts the app when
  the person logs in, with the window hidden.
- **Tray icon states:** normal; unread (a dot); mention or direct
  message (the dot in the accent colour, with the count where the
  platform can draw one); do not disturb (a small moon); disconnected
  (greyed).
- **Tray menu:** each server with its badge; "Do not disturb" with the
  usual durations and "Until I turn it off"; "Open Churr"; "Quit".
- **Badge:** the dock icon on macOS, the taskbar overlay on Windows,
  and the launcher count on Linux desktops that support one. The number
  is the sum of `badge` over the signed-in servers.
- **Notifications** are the operating system's own. Title and body are
  worded as on phones. Selecting one opens the window on that
  conversation. One is shown for each event with `notify` set to
  `notify`, unless that conversation is on screen in a focused window.
- While the app reports the person active, the server sends no phone
  wake-ups (section 2, step 5). The app reports activity from keyboard
  and pointer use in its own window, and nothing wider.
- In do not disturb the app shows no notifications and plays no
  sounds. The server marks events `badge` in that state, so the app
  needs no rule of its own.
- **While the app is locked** (`CLIENTS.md` section 9.27) the tray
  shows nothing of the account: the plain icon with no dot or count,
  no servers in the menu (only Open Churr and Quit), and no
  notifications, since the connection is closed until it is unlocked.
  The dock and taskbar number goes too.

### Web chat: browser notifications while open

- The web chat uses the browser's Notification interface from the open
  page. There is no service worker and no web push, so nothing arrives
  once the last tab is closed.
- Permission is asked for only when the person turns notifications on
  in the web chat's settings, never on page load.
- A notification is shown for an event with `notify` set to `notify`
  when the tab is hidden or the window is not focused, tagged with the
  conversation's identifier so a newer one replaces the older one.
  Selecting it focuses the tab and opens the conversation.
- The page title shows the badge count, "(3) Churr", and the tab icon
  changes when there is a mention.
- With several tabs open, each tab would otherwise notify. The tabs
  agree on one through a `BroadcastChannel`: the tab opened first
  notifies, and another takes over when it closes.
- A sealed message is decrypted in the page, so its notification can
  show its text.

### Incoming calls

Which calls ring, and for how long, is in `CALLS.md` section 15. A ring
lasts 45 seconds. This is what a ring looks like on each client.

| Client | App in front or connected | App closed |
|--------|---------------------------|------------|
| iPhone | A full-width banner in the app with Join and Decline, the ring sound, vibration | The wake-up's notification, completed by the extension: "Sam is calling", the ring sound (iOS plays at most thirty seconds), time-sensitive level so it is not held back by a Focus that allows such notifications. Selecting it opens the app on the call's join screen |
| Android | The same banner | A call-style notification on the "Calls" channel with Join and Decline, shown over the lock screen, with the ringtone and vibration, for as long as the ring lasts |
| Desktop apps | A small always-on-top window with Join and Decline and the ring sound; also a system notification | Does not apply: the app stays connected. If the app was quit, nothing |
| Web chat | A banner across the page with Join and Decline, the ring sound if the browser allows the page to play sound, and a browser notification if they are on | Nothing |

- The iPhone app does not present incoming calls through the system
  call screen in version 1. That would need a second kind of push
  (VoIP push), which requires the relay to know that an event is a
  call. The relay is told nothing about events, and this design keeps
  it that way.
- A ring stops everywhere when the person joins or declines on any
  device, when the call ends, or after 45 seconds. Connected clients
  are told by a live event. A phone that was woken is sent one more
  wake-up with the same collapse key and no badge. When the phone
  fetches it, the answer has `ringing: false`, and the phone replaces
  the ring with "Missed call from Sam" or removes it if the call was
  answered elsewhere.
- A ring that reaches the phone late does not ring: the fetch says so
  and the phone shows "Missed call".
- Someone already in a call sees a new ring as a banner without the
  ring sound.

## 14. Settings

Stored in the `settings` table; listed with every other setting in
`OPERATIONS.md` section 5.

| Key | Default | Meaning |
|-----|---------|---------|
| `push.relay_enabled` | `true` | Send wake-ups through the relay. Off: `apns` and `fcm` registrations are refused and no relay request is ever made |
| `push.relay_url` | `https://<RELAY-ADDRESS-NOT-CHOSEN>` | The relay to use |
| `push.unifiedpush_enabled` | `false` | Post wake-ups to UnifiedPush endpoints members' devices registered |
| `push.send_badge` | `true` | Include the badge number in wake-ups. Off: phones take the badge from the fetch or the status endpoint |

Each person has their own settings on top: the mode and mute of each
conversation, do not disturb, quiet hours, whether notify anyway is
allowed, and on each device the operating system's own notification
switches.

## 15. What each outside party learns

"Server address" means the network address the request came from.

| Party | Learns | Does not learn |
|-------|--------|----------------|
| The relay's operator | The push address of each woken device. The calling server's address. When, and how often. The badge number, if the server sends it. The priority. The route part of the event identifier, which is constant per device and server, and the collapse key, which is constant per device and conversation: so it can tell that two wake-ups were for the same conversation on one phone, without knowing which conversation | Any text, any account or person's name, any conversation name or identifier, who sent the message, whether it was a message or a call, which Churr account the device belongs to |
| Apple (APNs) | Everything the relay sends on: device token, event identifier, collapse key, badge, priority, timing. That the app is Churr. The relay's address, not the Churr server's | The same things the relay does not learn. Also not which Churr server woke the phone, except by guessing from the route |
| Google (FCM) | The same as Apple, for Android phones from the Play Store | The same |
| A UnifiedPush distributor and its server | The endpoint, the Churr server's address, the whole wake-up body, timing | Any content. It is chosen by the person, not by Churr |
| The phone's network | That the phone contacted its Churr server just after a push. The content is inside HTTPS | Content |
| The Churr server's operator | Everything about the message, as always, unless it is in a sealed chat. Each device's platform and push address | The content of sealed chats |
| Nobody, with the relay and UnifiedPush switched off | No request leaves the server for notifications. Phones rely on the status endpoint and, on Android, the foreground service | |

The relay cannot read a wake-up's meaning, and it also cannot be made
to carry content: the request has no field for it and unknown fields
are refused.

## 16. Decisions recorded here

- A wake-up is skipped when a client is connected **and active**
  (activity within five minutes), not merely connected. Otherwise a
  desktop app idling in the tray would stop every phone notification.
- Notify anyway always appears to succeed, and a wake-up it causes
  looks like any other.
- The event identifier is an encrypted token, not a row in a table.
- The route prefix exists because one app holds several servers and a
  wake-up names none of them.
- The relay sets a fixed three-day lifetime on every push. The phone,
  not the relay, decides that an old ring is a missed call.
- UnifiedPush is off by default and has its own switch.
- Calls ring through an ordinary notification on iPhone. No VoIP push.
- Edits and system messages never notify, even when an edit adds a
  mention.
- The web chat uses no service worker.

## 17. Needs from other documents

- **API:** device registration returns `wake_route`; the "server"
  operation lists accepted push kinds; the wake-up fetch operation of
  section 9; the status fields of section 11.
- **Events:** the per-recipient `notify` value on message events; a
  client-to-server message reporting activity; ring events (named in
  `CALLS.md`).
- **Schema:** nothing more. `notify_anyway` holds who a message was
  pushed through to, and `devices.push_public_key` and
  `devices.push_auth` hold what Web Push encryption needs.

## 18. To verify before building

- That the extension's Swift decrypt, with a fetch, stays within the
  extension's time and memory limits on an old iPhone.
- The exact APNs and FCM error names mapped in section 7.
- Which current UnifiedPush distributors require Web Push encryption,
  so the app knows when to register a key.
- Which Android foreground-service type the F-Droid build may declare
  for a connection kept open all day.
- How many different collapse keys FCM keeps per offline device (the
  reason section 7 does not use FCM's own).
