package link

import "sync/atomic"

// Recovery is told, not polled. Whatever a server waits for (a channel's
// history, an answer from its home hub, the record or key that makes a
// request answerable) it is told of, or given, the moment it can be: the
// holder tells each instance it gave a short snapshot as soon as its history
// is whole, a request that arrives before what makes it answerable is kept
// and answered when that arrives, and so on. The timers that ask again are a
// safety net for what was lost on the way (a dropped connection, a restart),
// not the way things normally move on.
//
// retriesOff, when a test sets it, switches that net off: the ticks that
// submit again what waits in an outbox and that take a move on again do
// nothing. With the waits between asking again also set to an hour, a test
// that still passes shows that nothing it does depends on a timer firing.
var retriesOff atomic.Bool
