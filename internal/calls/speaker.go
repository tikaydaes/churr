package calls

import (
	"sort"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// Active-speaker detection (docs/CALLS.md section 8). Clients put the
// loudness of each audio packet in a header extension (RFC 6464), so the
// server can tell who is speaking without decoding anything.
const (
	// speakerWindow is how long levels are averaged over.
	speakerWindow = 300 * time.Millisecond
	// speakerThreshold: louder than -40 dBov. The extension carries the
	// level as a positive number of dB below overload, so louder is lower.
	speakerThreshold = 40
	// speakerWindows is how many windows running must be above the
	// threshold before someone counts as speaking.
	speakerWindows = 2
	// speakerHold is how long after the last loud window someone stops
	// being a speaker.
	speakerHold = 1500 * time.Millisecond
	// speakerMax is how many speakers are reported.
	speakerMax = 3
	// speakerEvery bounds the reports: at most three a second.
	speakerEvery = time.Second / 3
	// speakerPackets is how many packets a window needs before its
	// average means anything. Someone talking sends five or more in 300
	// milliseconds; silence suppression sends one now and then.
	speakerPackets = 3
	// silentLevel is the extension's value for digital silence.
	silentLevel = 127
)

// micLevel follows one microphone.
type micLevel struct {
	windowStart time.Time
	sum         int
	count       int
	// loud is how many windows running were above the threshold.
	loud int
	// speaking, lastLoud and level: the state after the last full window.
	speaking bool
	lastLoud time.Time
	level    float64
}

// close ends the window in progress, if it is over, and any empty windows
// after it. A window with hardly any packets is a silent one: a muted
// microphone sends nothing, and silence suppression sends almost nothing.
func (m *micLevel) close(now time.Time) {
	for !m.windowStart.IsZero() && now.Sub(m.windowStart) >= speakerWindow {
		avg := float64(silentLevel)
		if m.count >= speakerPackets {
			avg = float64(m.sum) / float64(m.count)
		}
		end := m.windowStart.Add(speakerWindow)
		if avg < speakerThreshold {
			m.loud++
			if m.loud >= speakerWindows {
				m.speaking, m.lastLoud, m.level = true, end, avg
			}
		} else {
			m.loud = 0
		}
		m.sum, m.count = 0, 0
		m.windowStart = end
		if now.Sub(end) > speakerHold+speakerWindow {
			// Long silence: nothing more to learn from empty windows.
			m.windowStart = time.Time{}
			m.loud = 0
		}
	}
	if m.speaking && now.Sub(m.lastLoud) >= speakerHold {
		m.speaking = false
	}
}

// speakers follows every microphone of a call and says who is speaking.
type speakers struct {
	mu   sync.Mutex
	mics map[model.AccountID]*micLevel
	// last and lastAt: the list last reported, and when.
	last   []model.AccountID
	lastAt time.Time
}

func newSpeakers() *speakers { return &speakers{mics: map[model.AccountID]*micLevel{}} }

// observe records the level of one audio packet from an account.
func (s *speakers) observe(account model.AccountID, level uint8, now time.Time) {
	s.mu.Lock()
	m := s.mics[account]
	if m == nil {
		m = &micLevel{}
		s.mics[account] = m
	}
	m.close(now)
	if m.windowStart.IsZero() {
		m.windowStart = now
	}
	m.sum += int(level & 0x7f)
	m.count++
	s.mu.Unlock()
}

// forget drops an account's microphone: it left, or stopped publishing.
func (s *speakers) forget(account model.AccountID) {
	s.mu.Lock()
	delete(s.mics, account)
	s.mu.Unlock()
}

// current returns up to three speakers, loudest first.
func (s *speakers) current(now time.Time) []model.AccountID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentLocked(now)
}

func (s *speakers) currentLocked(now time.Time) []model.AccountID {
	type entry struct {
		id    model.AccountID
		level float64
	}
	var list []entry
	for id, m := range s.mics {
		m.close(now)
		if m.speaking {
			list = append(list, entry{id, m.level})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].level != list[j].level {
			return list[i].level < list[j].level
		}
		return list[i].id < list[j].id
	})
	if len(list) > speakerMax {
		list = list[:speakerMax]
	}
	out := make([]model.AccountID, len(list))
	for i, e := range list {
		out[i] = e.id
	}
	return out
}

// changed returns the speakers and true when they should be reported now:
// the set differs from the one last reported, or someone else is loudest,
// and the last report was long enough ago.
func (s *speakers) changed(now time.Time) ([]model.AccountID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.currentLocked(now)
	if sameSpeakers(cur, s.last) || now.Sub(s.lastAt) < speakerEvery {
		return cur, false
	}
	s.last, s.lastAt = cur, now
	return cur, true
}

// loudest returns the current speaker as last reported, or "".
func (s *speakers) loudest() model.AccountID {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.last) == 0 {
		return ""
	}
	return s.last[0]
}

// sameSpeakers reports whether two lists hold the same accounts with the
// same one loudest.
func sameSpeakers(a, b []model.AccountID) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	if a[0] != b[0] {
		return false
	}
	in := make(map[model.AccountID]bool, len(a))
	for _, id := range a {
		in[id] = true
	}
	for _, id := range b {
		if !in[id] {
			return false
		}
	}
	return true
}
