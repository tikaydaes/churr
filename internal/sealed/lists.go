package sealed

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Device lists (docs/SEALED.md section 4.5): the server stores every
// version, keyed by account and version, and serves any range of them.

// ListInput is a new version of the caller's device list, as the contract's
// DeviceListInput carries it: the three parts of the stored form.
type ListInput struct {
	Version int64
	List    []byte
	Sig     []byte
	PrevSig []byte
}

// maxListsServed bounds one answer of listDeviceListVersions. A client
// that is further behind asks again from the last version it got.
const maxListsServed = 500

// listVersion puts a stored version in the shape the API returns.
func listVersion(l store.SignedDeviceList) model.DeviceListVersion {
	out := model.DeviceListVersion{AccountID: l.AccountID, Version: l.Version, CreatedAt: l.CreatedAt}
	if s, err := parseSignedList(l.Signed); err == nil {
		out.List, out.Sig, out.PrevSig = s.List, s.Sig, s.PrevSig
	}
	return out
}

// checkOwnList makes the server's checks on the next version of a local
// account's list (section 4.5) and returns the bytes to store: it is the
// account's own, it carries this server's name, it follows the newest
// stored version, it passes the structural checks of section 4.4, and
// every device in it is a live device of the account with those keys.
func (s *Service) checkOwnList(ctx context.Context, a store.Areas, account model.AccountID, in ListInput) ([]byte, deviceList, error) {
	if len(in.Sig) != 64 || (len(in.PrevSig) != 0 && len(in.PrevSig) != 64) {
		return nil, deviceList{}, model.Invalid("sig", "A signature is 64 bytes.")
	}
	next := signedList{List: in.List, Sig: in.Sig}
	if len(in.PrevSig) != 0 {
		next.PrevSig = in.PrevSig
	}
	var prev *signedList
	newest, found, err := a.Sealed().NewestDeviceList(ctx, account)
	if err != nil {
		return nil, deviceList{}, err
	}
	if found {
		p, err := parseSignedList(newest.Signed)
		if err != nil {
			return nil, deviceList{}, err
		}
		prev = &p
	}
	if in.Version != newest.Version+1 {
		return nil, deviceList{}, model.ErrConflict.With("That is not the next version of the device list.")
	}
	l, err := checkListStep(prev, next)
	if err != nil {
		if e, ok := err.(errList); ok {
			return nil, l, model.Invalid("list", string(e))
		}
		return nil, l, err
	}
	if int64(l.Version) != in.Version {
		return nil, l, model.Invalid("version", "version is not the version inside the list.")
	}
	if l.AccountID != account {
		return nil, l, model.Invalid("list", "The device list is another account's.")
	}
	if l.Server != s.hostname {
		return nil, l, model.Invalid("list", "The device list must carry this server's name.")
	}
	devices, err := a.Devices().ForAccount(ctx, account)
	if err != nil {
		return nil, l, err
	}
	live := map[model.DeviceID]model.Device{}
	for _, d := range devices {
		if d.RetiredAt.IsZero() {
			live[d.ID] = d
		}
	}
	for _, d := range l.Devices {
		row, ok := live[d.ID]
		if !ok || string(row.SignKey) != string(d.SignKey) || string(row.BoxKey) != string(d.BoxKey) || platformNames[d.Platform] != row.Platform {
			return nil, l, model.Invalid("list", "Every device in the list must be a registered device of this account, with the keys and platform it registered.")
		}
	}
	return next.encode(), l, nil
}

// PublishList stores the next version of the caller's device list and
// announces it.
func (s *Service) PublishList(ctx context.Context, c Caller, in ListInput) (model.DeviceListVersion, error) {
	if _, err := s.keyed(ctx, s.store, c); err != nil {
		return model.DeviceListVersion{}, err
	}
	if err := s.enabled(ctx); err != nil {
		return model.DeviceListVersion{}, err
	}
	stored := store.SignedDeviceList{AccountID: c.Account.ID, Version: in.Version, CreatedAt: s.now()}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		signed, _, err := s.checkOwnList(ctx, tx, c.Account.ID, in)
		if err != nil {
			return err
		}
		stored.Signed = signed
		return tx.Sealed().PutDeviceList(ctx, stored)
	})
	if err != nil {
		return model.DeviceListVersion{}, err
	}
	s.announceList(ctx, stored)
	return listVersion(stored), nil
}

// announceList tells the account's own connections, and the other account
// of each of its sealed chats, that a new version exists (docs/EVENTS.md
// section 12), and hands it to the link for remote ones.
func (s *Service) announceList(ctx context.Context, l store.SignedDeviceList) {
	e := s.event(model.EventDeviceListChanged, map[string]any{"account_id": l.AccountID, "version": l.Version})
	s.hub.Send(l.AccountID, e)
	partners, err := s.store.Sealed().Partners(ctx, l.AccountID)
	if err != nil {
		s.log.Warn("the partners of a device list change could not be read", "err", err)
	}
	for _, p := range partners {
		s.hub.Send(p, e)
	}
	if s.outbound != nil {
		s.outbound.DeviceListPublished(ctx, l.AccountID, l.Version, l.Signed)
	}
}

// Lists returns versions of an account's device list: every version above
// *after, in order, or the newest only when after is nil.
func (s *Service) Lists(ctx context.Context, actor model.Account, account model.AccountID, after *int64) ([]model.DeviceListVersion, error) {
	if err := s.enabled(ctx); err != nil {
		return nil, err
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return nil, err
	}
	partner := false
	if actor.ID != account {
		partners, err := s.store.Sealed().Partners(ctx, actor.ID)
		if err != nil {
			return nil, err
		}
		for _, p := range partners {
			partner = partner || p == account
		}
	}
	if err := s.perm.CanSeeDeviceLists(ctx, actor, target, partner); err != nil {
		return nil, err
	}
	if !target.Local() && s.outbound != nil {
		// Someone on another server: their home server is asked for the
		// list when none is held here yet. After that it sends each new
		// version unasked (section 17).
		if _, found, err := s.store.Sealed().NewestDeviceList(ctx, account); err != nil {
			return nil, err
		} else if !found {
			if err := s.outbound.RequestDeviceLists(ctx, account, actor.ID, 0); err != nil && model.CodeOf(err) != model.CodeNotFound {
				return nil, err
			}
		}
	}
	out := []model.DeviceListVersion{}
	if after == nil {
		newest, found, err := s.store.Sealed().NewestDeviceList(ctx, account)
		if err != nil || !found {
			return out, err
		}
		return append(out, listVersion(newest)), nil
	}
	if *after < 0 {
		return nil, model.Invalid("after", "after is 0 or more.")
	}
	lists, err := s.store.Sealed().DeviceLists(ctx, account, *after, maxListsServed)
	for _, l := range lists {
		out = append(out, listVersion(l))
	}
	return out, err
}

// StoreRemoteList stores a version of a remote account's device list that
// arrived over a link, against its shadow account (section 17), and tells
// the local people who have a sealed chat with it. The list proves itself:
// it must pass the same structural checks as a local one, follow the
// newest version held, and be that account's. A version already held is
// accepted again without change, so the other server may resend.
func (s *Service) StoreRemoteList(ctx context.Context, account model.AccountID, signedBytes []byte) error {
	next, err := parseSignedList(signedBytes)
	if err != nil {
		return model.Invalid("signed_list", "The device list is not in its one valid form.")
	}
	stored := store.SignedDeviceList{AccountID: account, Signed: next.encode(), CreatedAt: s.now()}
	fresh := false
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		newest, found, err := tx.Sealed().NewestDeviceList(ctx, account)
		if err != nil {
			return err
		}
		var prev *signedList
		if found {
			p, err := parseSignedList(newest.Signed)
			if err != nil {
				return err
			}
			prev = &p
		}
		l, perr := parseDeviceList(next.List)
		if perr != nil || l.AccountID != account {
			return model.Invalid("signed_list", "The device list is not that account's.")
		}
		if found && int64(l.Version) <= newest.Version {
			held, ok, err := tx.Sealed().DeviceList(ctx, account, int64(l.Version))
			if err != nil {
				return err
			}
			if ok && string(held.Signed) == string(stored.Signed) {
				return nil
			}
			return model.ErrConflict.With("Another list is already held for that version.")
		}
		if !found && l.Version != 1 {
			// The other server begins wherever its account's chain
			// stands when the stream is made; the first version held
			// is taken as it is, as a device takes its first.
			if _, err := checkList(next); err != nil {
				return model.Invalid("signed_list", err.Error())
			}
		} else if _, err := checkListStep(prev, next); err != nil {
			return model.Invalid("signed_list", err.Error())
		}
		stored.Version = int64(l.Version)
		fresh = true
		if !found && l.Version != 1 {
			return tx.Sealed().PutFirstDeviceList(ctx, stored)
		}
		return tx.Sealed().PutDeviceList(ctx, stored)
	})
	if err != nil || !fresh {
		return err
	}
	e := s.event(model.EventDeviceListChanged, map[string]any{"account_id": account, "version": stored.Version})
	partners, err := s.store.Sealed().Partners(ctx, account)
	for _, p := range partners {
		s.hub.Send(p, e)
	}
	return err
}

// RemoteLists returns the stored versions of a local account's list above
// `after`, for the link to send to a server that asked for a range it
// lacks (section 17).
func (s *Service) RemoteLists(ctx context.Context, account model.AccountID, after int64) ([]store.SignedDeviceList, error) {
	return s.store.Sealed().DeviceLists(ctx, account, after, maxListsServed)
}
