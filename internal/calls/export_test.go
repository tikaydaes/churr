package calls

import "github.com/tikaydaes/churr/internal/model"

// What tests in this directory reach into the service for.

// SetEstimate replaces one participant's downlink estimate, in bits a
// second; 0 goes back to the real one. A test cannot make the loopback
// address slow, so it says what the estimator would have.
func (s *Server) SetEstimate(call model.CallID, account model.AccountID, bps int) {
	if lc := s.callByID(call); lc != nil && lc.room != nil {
		if p := lc.room.get(account); p != nil {
			p.estimateOverride.Store(int64(bps))
			p.wake()
		}
	}
}

// Limit is what the participant's downlink is being held to, in bits a
// second, as worked out from the real estimator's signals: 0 is "nothing
// holds it back".
func (s *Server) Limit(call model.CallID, account model.AccountID) int {
	if lc := s.callByID(call); lc != nil && lc.room != nil {
		if p := lc.room.get(account); p != nil {
			p.linkMu.Lock()
			defer p.linkMu.Unlock()
			return p.link.limit
		}
	}
	return 0
}

// Forwarded is how many RTP packets the call's room has written to
// subscribers.
func (s *Server) Forwarded(call model.CallID) uint64 {
	if lc := s.callByID(call); lc != nil && lc.room != nil {
		return lc.room.forwarded.Load()
	}
	return 0
}

// QuickRaise makes one participant's layers come back up every interval
// instead of every five seconds.
func (s *Server) QuickRaise(call model.CallID, account model.AccountID) {
	if lc := s.callByID(call); lc != nil && lc.room != nil {
		if p := lc.room.get(account); p != nil {
			lc.room.mu.Lock()
			p.picker.raiseEvery = 0
			lc.room.mu.Unlock()
		}
	}
}
