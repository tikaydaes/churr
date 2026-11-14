package auth

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// What sealed chats need of sign-in: the fresh check, and the hand-over of
// the backup key to an app that is being signed in (docs/SEALED.md section
// 13.3 and 13.7, docs/AUTH.md sections 11.2 and 11.4). The server stores
// the hand-over with the one-time code and returns it with the tokens. It
// cannot open it.

// The sizes of the two hand-overs: the backup key sealed to the app's box
// key, and the same locked again under the pairing secret from a QR code.
const (
	HandoverSize        = 80
	PairingHandoverSize = 112
)

// Fresh refuses a session that has not been checked with a passkey or an
// authenticator app in the last ten minutes (docs/AUTH.md section 10.4).
// Resetting sealed chats asks for it.
func (s *Service) Fresh(who Who) error { return s.fresh(who) }

// WithHandover adds the approval page's hand-over to a checked
// authorization request. It is kept only if the app sent a box key for it
// to be sealed to; one of the wrong size is refused.
func (r AuthorizeRequest) WithHandover(handover []byte) (AuthorizeRequest, error) {
	if len(handover) == 0 {
		return r, nil
	}
	if len(handover) != HandoverSize || len(r.BoxKey) == 0 {
		return r, model.Invalid("handover", "A hand-over is 80 bytes, sealed to the box key the app sent.")
	}
	r.Handover = handover
	return r, nil
}

// ConfirmPairingWithHandover is ConfirmPairing for an unlocked web chat,
// which also hands the app the backup key: sealed to the app's box key and
// locked under the pairing secret from the QR code, which the server never
// sees.
func (s *Service) ConfirmPairingWithHandover(ctx context.Context, who Who, code string, handover []byte) error {
	if len(handover) == 0 {
		return s.ConfirmPairing(ctx, who, code)
	}
	c, hash, err := s.pairing(ctx, who, code)
	if err != nil {
		return err
	}
	if len(handover) != PairingHandoverSize || len(c.BoxKey) == 0 {
		return model.Invalid("handover", "A pairing hand-over is 112 bytes, for the box key the app sent.")
	}
	if c.ClientName == "" {
		return model.ErrConflict.With("No device has scanned this code yet.")
	}
	if err := s.store.Sealed().SetHandover(ctx, hash, handover, s.now()); err != nil {
		return err
	}
	return s.ConfirmPairing(ctx, who, code)
}

// unlockingPasskeyRemoved is what follows removing a passkey that had a
// locked copy of the backup key. The copy went with the passkey, which is
// enough against someone who has the passkey and nothing else; it is not
// enough against someone who also has a copy of the server's data. So the
// backup key and account key must be replaced (docs/SEALED.md section
// 13.6). The removal was made on the account page, where no unlocked
// device is at hand: the mark tells the next unlocked device to do it
// before anything else, and the account's devices are told now.
func (s *Service) unlockingPasskeyRemoved(ctx context.Context, account model.AccountID) {
	if _, err := s.store.Sealed().SetReplacementDue(ctx, account, true); err != nil {
		s.log.Warn("a backup could not be marked for replacement", "err", err)
		return
	}
	b, found, err := s.store.Sealed().Backup(ctx, account)
	if err != nil || !found {
		return
	}
	s.hub.Send(account, s.accountEvent(model.EventBackupChanged, map[string]any{"key_version": b.KeyVersion, "replacement_due": b.ReplacementDue}))
}
