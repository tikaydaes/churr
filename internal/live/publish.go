package live

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// The fan-out of one conversation event.
//
// An event in a channel of fifty thousand people goes to the connections of
// those of them who are connected, and what it costs must follow that
// number, not the fifty thousand. So the members come from the roster the
// store keeps in memory; the connections are found under one hold of the
// hub's lock, walking whichever is smaller, the roster or the connected
// accounts; and the frame is encoded once for each form the event takes.
// A message.created has three forms (`notify` is none, badge or notify), so
// ten thousand readers share three byte slices and none is built for one
// reader alone.

// target is the connections of one recipient: conns[from:to] of the
// fan-out's list.
type target struct {
	account  model.AccountID
	from, to int
}

// form is one encoding of an event: the frame for every reader whose only
// difference from the plain event is one top-level field with this value.
type form struct {
	key, value string
	frame      []byte
}

func (h *Hub) publish(ctx context.Context, e model.Event) {
	norm, base, err := e.Normalize()
	if err != nil {
		h.opts.Log.Error("an event could not be encoded", "type", e.Type, "err", err)
		return
	}
	roster, err := h.perm.Roster(ctx, e.ConversationID)
	if err != nil {
		// The event is in the log; clients catch up from there.
		h.opts.Log.Error("recipients could not be read; the event was not fanned out", "type", e.Type, "err", err)
		return
	}

	var (
		targets []target
		conns   []*conn
	)
	take := func(id model.AccountID, set map[*conn]struct{}) {
		if len(set) == 0 {
			return
		}
		from := len(conns)
		for c := range set {
			conns = append(conns, c)
		}
		targets = append(targets, target{id, from, len(conns)})
	}
	h.mu.RLock()
	tailor := h.tailor
	if roster.Len() <= len(h.conns) {
		for _, id := range roster.IDs {
			take(id, h.conns[id])
		}
	} else {
		for id, set := range h.conns {
			if roster.Has(id) {
				take(id, set)
			}
		}
	}
	for i, id := range e.AlsoTo {
		again := roster.Has(id)
		for _, earlier := range e.AlsoTo[:i] {
			again = again || earlier == id
		}
		if !again {
			take(id, h.conns[id])
		}
	}
	h.mu.RUnlock()

	// A conversation that is delivered lazily goes in full only to the
	// connections that have it open (lazy.go). Events without a seq are
	// not the log's and are never held back.
	large, _ := h.limits(ctx)
	lazy := e.Seq != 0 && roster.LazyAbove(large)
	later := lazy && deferrable(e)
	isMessage := e.Type == model.EventMessageCreated
	now := time.Now()
	// What a signal counts as a new message for a reader: one at the top
	// level of the conversation, from someone else, that is not a system
	// notice or quiet. It is what a client would count as unread had it
	// been sent the message.
	var author string
	countable := false
	if lazy && isMessage {
		if m, ok := norm.Payload["message"].(map[string]any); ok {
			author, _ = m["author_id"].(string)
			parent, _ := m["parent_id"].(string)
			quiet, _ := m["quiet"].(bool)
			countable = parent == "" && !quiet && m["system"] == nil
		}
	}

	// One message to fifty thousand connections is too much for one
	// goroutine to hand out before the next message is due, with the other
	// processors idle. So the recipients are cut into parts and each part
	// is handed out on its own goroutine. Publish returns when every part
	// is done, and Publish is called in seq order within a conversation,
	// so each connection is still handed a conversation's events in order:
	// a recipient is in one part only, and the next event does not start
	// until this one has reached everyone.
	sh := &sharedForms{base: base, log: func(err error) {
		h.opts.Log.Error("an event could not be re-read", "type", e.Type, "err", err)
	}}
	var sentAll, sentBytesAll, fullSent atomic.Int64
	defer func() {
		h.count(e.Type, int(sentAll.Load()), int(sentBytesAll.Load()))
		h.lazy.stats.full.Add(fullSent.Load())
	}()
	deliver := func(part []target) {
		var (
			mine  []form // the encodings this part has used
			wants []bool // for one target: which of its connections are sent the event in full
		)
		sent, sentBytes, full := 0, 0, 0
		defer func() {
			sentAll.Add(int64(sent))
			sentBytesAll.Add(int64(sentBytes))
			fullSent.Add(int64(full))
		}()
		for _, t := range part {
			set := conns[t.from:t.to]
			only := e.Only[t.account]
			// An account outside the conversation (AlsoTo) holds nothing of
			// it to keep in order, and is sent the event as it is.
			held := lazy && roster.Has(t.account)
			anyFull := !held
			if held {
				wants = wants[:0]
				for _, c := range set {
					w := c.wantsFull(e.ConversationID)
					wants = append(wants, w)
					anyFull = anyFull || w
				}
			}
			var extra map[string]any
			// Nobody is asked what an edit or a reaction means to a reader
			// who will only be told that something changed.
			if tailor != nil && (anyFull || isMessage || !later || len(only) > 0) {
				extra = tailor.Tailor(ctx, norm, t.account, true)
			}
			level := ""
			if isMessage {
				level, _ = extra["notify"].(string)
			}
			// What a reader who is not looking is still sent at once: an
			// event that is not message traffic, a message that notifies
			// them, and anything that carries something for them alone.
			urgent := !later || level == "notify" || len(only) > 0
			var frame []byte
			if anyFull || urgent {
				frame = base
				if len(extra) > 0 || len(only) > 0 {
					// The usual difference is one field with one of a few
					// values: those readers share one encoding.
					key, value, shared := "", "", false
					if len(only) == 0 && len(extra) == 1 {
						for k, v := range extra {
							key = k
							value, shared = v.(string)
						}
					}
					frame = nil
					if shared {
						// Looked for among the ones this part has met,
						// and only then among everyone's.
						for _, f := range mine {
							if f.key == key && f.value == value {
								frame = f.frame
								break
							}
						}
					}
					if frame == nil {
						if frame = sh.spliced(key, value, shared, extra, only); frame == nil {
							return
						}
						if shared {
							mine = append(mine, form{key, value, frame})
						}
					}
				}
			}
			for i, c := range set {
				switch {
				case !held:
					c.sendEvent(e.ConversationID, e.Seq, frame)
					sent++
					sentBytes += len(frame)
				case wants[i] && c.sendIfFull(e.ConversationID, e.Seq, frame):
					// A connection that opened the conversation since it
					// was asked is not sent this in full: the catch-up that
					// opening started finds the event in the log.
					sent++
					sentBytes += len(frame)
					full++
				case urgent:
					c.sendWithin(e.ConversationID, e.Seq, frame)
				default:
					c.noteLazy(e.ConversationID, e.Seq, countable && author != string(t.account), level == "badge", now)
				}
			}
		}
	}
	parts := fanoutParts(len(targets), len(conns), h.opts.fanoutWorkers, h.opts.fanoutEach)
	if parts <= 1 {
		deliver(targets)
		return
	}
	var wg sync.WaitGroup
	per := (len(targets) + parts - 1) / parts
	for from := 0; from < len(targets); from += per {
		part := targets[from:min(from+per, len(targets))]
		wg.Add(1)
		go func() {
			defer wg.Done()
			deliver(part)
		}()
	}
	wg.Wait()
}

// fanoutEach is how many connections one goroutine hands an event to before
// a second is worth starting: below it the event is out before another
// goroutine would have been scheduled.
const fanoutEach = 512

// fanoutParts is how many goroutines hand one event out: one for every
// fanoutEach connections, at most one for each processor (or workers, which
// tests set), and never more than there are recipients.
func fanoutParts(targets, conns, workers, each int) int {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if each <= 0 {
		each = fanoutEach
	}
	return max(1, min(workers, conns/each, targets))
}

// sharedForms is the encodings of one event that several recipients share,
// kept for the whole fan-out so that the parts hand out the same bytes: a
// frame is then encoded once, and compressed once (prepared.go), however
// many goroutines send it.
type sharedForms struct {
	base []byte
	log  func(error)

	mu     sync.Mutex
	fields map[string]json.RawMessage // base decoded, made only if someone needs changes
	forms  []form
	failed bool
}

// spliced returns the event with a recipient's fields added. With shared,
// the one difference is the field key with the value given, and everyone
// with the same difference is returned the same bytes. It returns nil if
// the event could not be read, which is said once.
func (s *sharedForms) spliced(key, value string, shared bool, extra, only map[string]any) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed {
		return nil
	}
	if shared {
		for _, f := range s.forms {
			if f.key == key && f.value == value {
				return f.frame
			}
		}
	}
	if s.fields == nil {
		if err := json.Unmarshal(s.base, &s.fields); err != nil {
			s.failed = true
			s.log(err)
			return nil
		}
	}
	frame := splice(s.fields, extra, only)
	if shared {
		s.forms = append(s.forms, form{key, value, frame})
	}
	return frame
}

// ConnectedAccounts returns the accounts that have a connection open and
// that keep says yes to. Whoever tells people about an account asks for the
// connected ones among those who may know, in place of listing everyone who
// may know and finding most of them away. keep is called with the hub's
// lock held: it must answer from memory.
func (h *Hub) ConnectedAccounts(keep func(model.AccountID) bool) []model.AccountID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []model.AccountID
	for id := range h.conns {
		if keep(id) {
			out = append(out, id)
		}
	}
	return out
}

// SendAll is Send to several accounts: the frame is encoded once and every
// connection is found under one hold of the lock. An account named twice is
// sent it twice, so name each once.
func (h *Hub) SendAll(to []model.AccountID, v any) {
	if len(to) == 0 {
		return
	}
	if about, ok := presenceOf(v); ok {
		h.SendPresence(to, about, v)
		return
	}
	var conns []*conn
	h.mu.RLock()
	for _, id := range to {
		for c := range h.conns[id] {
			conns = append(conns, c)
		}
	}
	h.mu.RUnlock()
	if len(conns) == 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		h.opts.Log.Error("an account event could not be encoded", "err", err)
		return
	}
	for _, c := range conns {
		c.sendEvent("", 0, b)
	}
	h.count(kindOf(v), len(conns), len(conns)*len(b))
}
