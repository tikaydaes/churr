package api

import (
	"encoding/json"
	"net/http"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/sealed"
)

// Sealed chats (docs/SEALED.md): device lists, epoch records, the key
// backup and unlock requests. Every record travels as base64url and is
// stored as bytes the server cannot open. The rules are the sealed
// package's; these handlers decode and encode.

func init() {
	register(func(a *API) {
		a.handle("openSealedChat", a.openSealedChat)
		a.handle("listDeviceListVersions", a.listDeviceListVersions)
		a.handle("publishDeviceList", a.publishDeviceList)
		a.handle("postEpochRecord", a.postEpochRecord)
		a.handle("listEpochRecords", a.listEpochRecords)
		a.handle("getBackup", a.getBackup)
		a.handle("createBackup", a.createBackup)
		a.handle("addLockedCopy", a.addLockedCopy)
		a.handle("putAccountRecord", a.putAccountRecord)
		a.handle("deleteLockedCopy", a.deleteLockedCopy)
		a.handle("listBackupRecords", a.listBackupRecords)
		a.handle("putBackupRecord", a.putBackupRecord)
		a.handle("replaceBackup", a.replaceBackup)
		a.handle("resetBackup", a.resetBackup)
		a.handle("createUnlockRequest", a.createUnlockRequest)
		a.handle("listUnlockRequests", a.listUnlockRequests)
		a.handle("getUnlockRequest", a.getUnlockRequest)
		a.handle("answerUnlockRequest", a.answerUnlockRequest)
	})
	// Replacing the backup key carries every backup record of the
	// account, which outgrows the usual body limit; its handler applies
	// its own.
	largeBody["replaceBackup"] = true
}

// maxReplaceBody bounds the body of replaceBackup.
const maxReplaceBody = 32 << 20

// caller is who is asking, as the sealed package wants it.
func (r *Request) caller() sealed.Caller {
	return sealed.Caller{Account: r.Actor(), Session: r.Who.Session}
}

// listInput is the contract's DeviceListInput.
type listInput struct {
	Version *int64       `json:"version"`
	List    *model.Bytes `json:"list"`
	Sig     *model.Bytes `json:"sig"`
	PrevSig model.Bytes  `json:"prev_sig"`
}

func (l *listInput) input(field string) (sealed.ListInput, error) {
	if l == nil || l.Version == nil || l.List == nil || l.Sig == nil {
		return sealed.ListInput{}, model.Invalid(field, "A device list needs version, list and sig.")
	}
	return sealed.ListInput{Version: *l.Version, List: *l.List, Sig: *l.Sig, PrevSig: l.PrevSig}, nil
}

// copyInput is the contract's LockedCopyInput.
type copyInput struct {
	Kind       string       `json:"kind"`
	PasskeyID  string       `json:"passkey_id"`
	DeviceID   string       `json:"device_id"`
	KeyVersion *int64       `json:"key_version"`
	LockedKey  *model.Bytes `json:"locked_key"`
}

func (c *copyInput) copy() (model.LockedCopy, error) {
	if c == nil || c.Kind == "" || c.KeyVersion == nil || c.LockedKey == nil {
		return model.LockedCopy{}, model.Invalid("locked_copy", "A locked copy needs kind, key_version and locked_key.")
	}
	return model.LockedCopy{Kind: c.Kind, PasskeyID: model.PasskeyID(c.PasskeyID), DeviceID: model.DeviceID(c.DeviceID),
		KeyVersion: *c.KeyVersion, LockedKey: *c.LockedKey}, nil
}

func copies(in []copyInput) ([]model.LockedCopy, error) {
	out := make([]model.LockedCopy, 0, len(in))
	for i := range in {
		c, err := in[i].copy()
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// recordInput is the contract's AccountRecord as a request sends it.
type recordInput struct {
	KeyVersion *int64       `json:"key_version"`
	Revision   *int64       `json:"revision"`
	Record     *model.Bytes `json:"record"`
	UpdatedAt  *model.Time  `json:"updated_at"`
}

func (in *recordInput) record(field string) (model.AccountRecord, error) {
	if in == nil || in.KeyVersion == nil || in.Revision == nil || in.Record == nil {
		return model.AccountRecord{}, model.Invalid(field, "An account record needs key_version, revision and record.")
	}
	return model.AccountRecord{KeyVersion: *in.KeyVersion, Revision: *in.Revision, Record: *in.Record}, nil
}

func (a *API) openSealedChat(r *Request) error {
	var body struct {
		AccountID   string      `json:"account_id"`
		EpochRecord model.Bytes `json:"epoch_record"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	other, err := BodyID[model.AccountID]("account_id", body.AccountID)
	if err != nil {
		return err
	}
	conv, created, err := a.Sealed.Open(r.Ctx(), r.caller(), other, body.EpochRecord)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return r.JSONStatus(status, map[string]any{"conversation": conv})
}

func (a *API) listDeviceListVersions(r *Request) error {
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	var after *int64
	if v, found, err := r.queryInt("after"); err != nil {
		return err
	} else if found {
		after = &v
	}
	versions, err := a.Sealed.Lists(r.Ctx(), r.Actor(), id, after)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"versions": orEmpty(versions)})
}

func (a *API) publishDeviceList(r *Request) error {
	var body listInput
	if err := r.Decode(&body); err != nil {
		return err
	}
	in, err := body.input("list")
	if err != nil {
		return err
	}
	v, err := a.Sealed.PublishList(r.Ctx(), r.caller(), in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"version": v})
}

func (a *API) postEpochRecord(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		KeyEpoch *int64       `json:"key_epoch"`
		Record   *model.Bytes `json:"record"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.KeyEpoch == nil || body.Record == nil {
		return model.BadRequest("key_epoch and record are needed.")
	}
	epoch, err := a.Sealed.PostEpoch(r.Ctx(), r.caller(), id, *body.KeyEpoch, *body.Record)
	if e, ok := model.AsError(err); ok && e.Code == model.CodeConflict && epoch.KeyEpoch != 0 {
		// The loser of a race is handed the winning record beside the
		// error, so its device can check it and use it.
		b, merr := json.Marshal(map[string]any{"error": e, "epoch": epoch})
		if merr != nil {
			return merr
		}
		r.W.Header().Set("Content-Type", "application/json; charset=utf-8")
		r.W.WriteHeader(http.StatusConflict)
		r.W.Write(b)
		return nil
	}
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"epoch": epoch})
}

func (a *API) listEpochRecords(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	from, _, err := r.queryInt("from")
	if err != nil {
		return err
	}
	if from < 0 {
		return model.Invalid("from", "from is 1 or more.")
	}
	limit, err := r.limit()
	if err != nil {
		return err
	}
	epochs, more, err := a.Sealed.Epochs(r.Ctx(), r.caller(), id, from, limit)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"epochs": orEmpty(epochs), "has_more": more})
}

func (a *API) getBackup(r *Request) error {
	b, err := a.Sealed.Backup(r.Ctx(), r.caller())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"backup": b})
}

// backupBody is what createBackup and resetBackup carry, and most of what
// replaceBackup does.
type backupBody struct {
	ReplacesKeyVersion *int64               `json:"replaces_key_version"`
	AccountRecord      *recordInput         `json:"account_record"`
	LockedCopies       []copyInput          `json:"locked_copies"`
	Records            []model.BackupRecord `json:"records"`
	DeviceList         *listInput           `json:"device_list"`
}

func (b backupBody) parts() (model.AccountRecord, []model.LockedCopy, sealed.ListInput, error) {
	rec, err := b.AccountRecord.record("account_record")
	if err != nil {
		return rec, nil, sealed.ListInput{}, err
	}
	cps, err := copies(b.LockedCopies)
	if err != nil {
		return rec, nil, sealed.ListInput{}, err
	}
	list, err := b.DeviceList.input("device_list")
	return rec, cps, list, err
}

func (a *API) createBackup(r *Request) error {
	var body backupBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.ReplacesKeyVersion != nil || body.Records != nil {
		return model.BadRequest("A new backup carries account_record, locked_copies and device_list.")
	}
	rec, cps, list, err := body.parts()
	if err != nil {
		return err
	}
	b, err := a.Sealed.CreateBackup(r.Ctx(), r.caller(), rec, cps, list)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"backup": b})
}

func (a *API) addLockedCopy(r *Request) error {
	var body struct {
		LockedCopy    *copyInput   `json:"locked_copy"`
		AccountRecord *recordInput `json:"account_record"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	cp, err := body.LockedCopy.copy()
	if err != nil {
		return err
	}
	var rec *model.AccountRecord
	if body.AccountRecord != nil {
		v, err := body.AccountRecord.record("account_record")
		if err != nil {
			return err
		}
		rec = &v
	}
	b, err := a.Sealed.AddCopy(r.Ctx(), r.caller(), cp, rec)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"backup": b})
}

func (a *API) putAccountRecord(r *Request) error {
	var body recordInput
	if err := r.Decode(&body); err != nil {
		return err
	}
	rec, err := body.record("record")
	if err != nil {
		return err
	}
	b, err := a.Sealed.PutAccountRecord(r.Ctx(), r.caller(), rec)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"backup": b})
}

func (a *API) deleteLockedCopy(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	if err := a.Sealed.DeleteCopy(r.Ctx(), r.caller(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listBackupRecords(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	var conv model.ConversationID
	if v := r.R.URL.Query().Get("conversation_id"); v != "" {
		if conv, err = BodyID[model.ConversationID]("conversation_id", v); err != nil {
			return err
		}
	}
	records, next, err := a.Sealed.Records(r.Ctx(), r.caller(), conv, page)
	if err != nil {
		return err
	}
	return r.JSON(list("records", orEmpty(records), next))
}

func (a *API) putBackupRecord(r *Request) error {
	conv, err := PathID[model.ConversationID](r, "conversation_id")
	if err != nil {
		return err
	}
	keyEpoch, err := pathInt(r, "key_epoch")
	if err != nil {
		return err
	}
	var body struct {
		KeyVersion *int64       `json:"key_version"`
		Record     *model.Bytes `json:"record"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.KeyVersion == nil || body.Record == nil {
		return model.BadRequest("key_version and record are needed.")
	}
	rec, created, err := a.Sealed.PutRecord(r.Ctx(), r.caller(), model.BackupRecord{ConversationID: conv, KeyEpoch: keyEpoch,
		KeyVersion: *body.KeyVersion, Record: *body.Record})
	if err != nil {
		return err
	}
	// 201 for a new record; 200 with the one already stored.
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	return r.JSONStatus(status, map[string]any{"record": rec})
}

// pathInt reads a path parameter that must be a whole number. One that is
// not could name nothing.
func pathInt(r *Request, name string) (int64, error) {
	var v int64
	s := r.R.PathValue(name)
	if s == "" || len(s) > 18 {
		return 0, model.ErrNotFound
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, model.ErrNotFound
		}
		v = v*10 + int64(c-'0')
	}
	return v, nil
}

func (a *API) replaceBackup(r *Request) error {
	r.R.Body = http.MaxBytesReader(r.W, r.R.Body, maxReplaceBody)
	var body backupBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.ReplacesKeyVersion == nil {
		return model.BadRequest("replaces_key_version is needed.")
	}
	rec, cps, list, err := body.parts()
	if err != nil {
		return err
	}
	b, err := a.Sealed.Replace(r.Ctx(), r.caller(), *body.ReplacesKeyVersion, rec, cps, body.Records, list)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"backup": b})
}

func (a *API) resetBackup(r *Request) error {
	var body backupBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.ReplacesKeyVersion != nil || body.Records != nil {
		return model.BadRequest("A reset carries account_record, locked_copies and device_list.")
	}
	rec, cps, list, err := body.parts()
	if err != nil {
		return err
	}
	// Starting again throws away every way of reading earlier sealed
	// messages, so it needs a fresh check.
	if err := a.Auth.Fresh(r.Who); err != nil {
		return err
	}
	b, err := a.Sealed.Reset(r.Ctx(), r.caller(), rec, cps, list)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"backup": b})
}

func (a *API) createUnlockRequest(r *Request) error {
	var body struct {
		Request *model.Bytes `json:"request"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Request == nil {
		return model.BadRequest("request is needed.")
	}
	u, err := a.Sealed.CreateUnlockRequest(r.Ctx(), r.caller(), *body.Request)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"unlock_request": u})
}

func (a *API) listUnlockRequests(r *Request) error {
	list, err := a.Sealed.UnlockRequests(r.Ctx(), r.caller())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"unlock_requests": orEmpty(list)})
}

func (a *API) getUnlockRequest(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	u, err := a.Sealed.UnlockRequest(r.Ctx(), r.caller(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"unlock_request": u})
}

func (a *API) answerUnlockRequest(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		LockedCopy *copyInput `json:"locked_copy"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	cp, err := body.LockedCopy.copy()
	if err != nil {
		return err
	}
	u, err := a.Sealed.AnswerUnlockRequest(r.Ctx(), r.caller(), id, cp)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"unlock_request": u})
}

// postSealedMessage is postMessage for a body that carries `sealed`: then
// that is the only content allowed, beside client_id, quiet and the blobs
// the message carries.
func (a *API) postSealedMessage(r *Request, conv model.ConversationID, body messageInput) error {
	if body.Sealed == nil {
		return model.BadRequest("quiet is for sealed messages only.")
	}
	if body.Text != "" || body.ParentID != "" || body.Quote != nil || len(body.Cards) > 0 || body.Form != nil {
		// docs/PERMISSIONS.md section 17, case 147.
		return model.BadRequest("A sealed message carries only client_id, sealed, quiet and file_ids.")
	}
	files, err := fileIDs(body.FileIDs)
	if err != nil {
		return err
	}
	m, created, err := a.Sealed.Post(r.Ctx(), r.caller(), sealed.NewMessage{ConversationID: conv, ClientID: body.ClientID,
		KeyEpoch: body.Sealed.KeyEpoch, Ciphertext: body.Sealed.Ciphertext, Quiet: body.Quiet, FileIDs: files})
	if err != nil {
		return err
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	return r.JSONStatus(status, map[string]any{"message": m})
}
