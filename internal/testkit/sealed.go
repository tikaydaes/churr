package testkit

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/sealed/ref"
)

// Sealed chats in tests: the reference implementation plays the part of a
// device, over the real API. A SealedDevice is one session of an account
// with its device keys; everything it knows about a sealed chat it learned
// from the server's answers and checked itself.

// SealedDevice is one device of an account.
type SealedDevice struct {
	// C is the session the device is attached to; D the device's keys and
	// state.
	C *Client
	D *ref.Device
	t testing.TB
	// ID is the identifier the server gave the device's row.
	ID string
	// seq is, per conversation, the highest sequence number taken in;
	// rows are the message rows seen, so one that could not be read yet
	// can be read again once its key arrives.
	seq  map[string]int64
	rows map[string][]ref.Row
	// Problems collects what the device refused, for tests of a server
	// that misbehaves; Dropped, OutOfOrder and Gaps count the rows it
	// found replayed, delivered out of order, or following a jump in
	// their sender's counters.
	Problems   []error
	Dropped    int
	OutOfOrder int
	Gaps       int
}

// NewSession signs another browser in to the same account with the same
// passkey, as a synced passkey would: a second session, and so room for a
// second device.
func (c *Client) NewSession() *Client {
	c.t.Helper()
	b := c.ts.Browser()
	b.CopyPasskey(c)
	if status, out := b.Login(); status != 200 && status != 201 {
		c.t.Fatalf("signing a second browser in = %d %v", status, out)
	}
	return b
}

func b64(b []byte) string { return model.EncodeBytes(b) }

// Unb64 reads base64url bytes, failing the test if they are not.
func Unb64(t testing.TB, v any) []byte { return unb64(t, v) }

func unb64(t testing.TB, v any) []byte {
	t.Helper()
	s, _ := v.(string)
	b, err := model.DecodeBytes(s)
	if err != nil {
		t.Fatalf("not base64url: %v", v)
	}
	return b
}

// Sealed registers this session's device with fresh keys and returns it.
// The device is locked until it creates or unlocks its account's backup.
func (c *Client) Sealed(platform string) *SealedDevice {
	c.t.Helper()
	d := ref.NewDevice(ref.MustUUID(string(c.ID)), c.ts.Server.Config.Hostname, ref.PlatformNumber(platform), rand.Reader,
		func() uint64 { return uint64(time.Now().UnixMilli()) })
	out := c.Must(200, "PUT", "/api/v1/me/devices/current", map[string]any{"name": c.Username + "'s " + platform, "platform": platform,
		"sign_key": b64(d.SignPublic()), "box_key": b64(d.BoxPublic())})
	sd := &SealedDevice{C: c, D: d, t: c.t, ID: out["device"].(map[string]any)["id"].(string), seq: map[string]int64{}, rows: map[string][]ref.Row{}}
	d.ID = ref.MustUUID(sd.ID)
	d.FetchLists = func(account ref.UUID, after uint64) ([]ref.SignedList, error) {
		return sd.fetchLists(account.String(), fmt.Sprintf("?after=%d", after))
	}
	return sd
}

// SealedPage is this session acting as the server's unlock page does: a
// browser signed in to the account that can unlock with a passkey and make
// a copy for an app's device, but has no device row of its own.
func (c *Client) SealedPage() *SealedDevice {
	d := ref.NewDevice(ref.MustUUID(string(c.ID)), c.ts.Server.Config.Hostname, ref.PlatformWeb, rand.Reader,
		func() uint64 { return uint64(time.Now().UnixMilli()) })
	return &SealedDevice{C: c, D: d, t: c.t, seq: map[string]int64{}, rows: map[string][]ref.Row{}}
}

// listBody is a signed list as the API takes it.
func listBody(t testing.TB, l ref.SignedList) map[string]any {
	t.Helper()
	parsed, err := ref.DecodeDeviceList(l.List)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{"version": parsed.Version, "list": b64(l.List), "sig": b64(l.Sig)}
	if l.PrevSig != nil {
		out["prev_sig"] = b64(l.PrevSig)
	}
	return out
}

// CopyBody is a locked copy as the API takes it.
func CopyBody(c ref.Copy, keyVersion uint32) map[string]any {
	out := map[string]any{"kind": ref.WayNames[c.Kind], "key_version": keyVersion, "locked_key": b64(c.Locked)}
	switch c.Kind {
	case ref.WayPasskey:
		out["passkey_id"] = c.ID.String()
	case ref.WayDevice:
		out["device_id"] = c.ID.String()
	}
	return out
}

// BundleBody is a created, replaced or reset backup as the API takes it.
func BundleBody(t testing.TB, b *ref.Bundle) map[string]any {
	t.Helper()
	copies := []map[string]any{}
	for _, c := range b.Copies {
		copies = append(copies, CopyBody(c, b.KeyVersion))
	}
	return map[string]any{
		"account_record": map[string]any{"key_version": b.KeyVersion, "revision": b.Revision, "record": b64(b.AccountRecord)},
		"locked_copies":  copies, "device_list": listBody(t, b.List),
	}
}

// CreateBackup makes the account's backup from this device and returns the
// recovery phrase.
func (sd *SealedDevice) CreateBackup() string {
	sd.t.Helper()
	var accessKey []byte
	if st := sd.C.Must(200, "GET", "/api/v1/me/access-key", nil); st["access_key"] != nil {
		if up := st["access_key"].(map[string]any)["unlock_public"]; up != nil {
			accessKey = unb64(sd.t, up)
		}
	}
	b, err := sd.D.CreateBackupWithAccessKey(accessKey)
	if err != nil {
		sd.t.Fatal(err)
	}
	sd.C.Must(201, "POST", "/api/v1/me/backup", BundleBody(sd.t, b))
	if err := sd.D.Adopt(b); err != nil {
		sd.t.Fatal(err)
	}
	return b.Phrase
}

func (sd *SealedDevice) fetchLists(account, query string) ([]ref.SignedList, error) {
	status, out := sd.C.Do("GET", "/api/v1/accounts/"+account+"/device-lists"+query, nil)
	if status != 200 {
		return nil, fmt.Errorf("device lists of %s = %d %v", account, status, out)
	}
	var lists []ref.SignedList
	for _, v := range out["versions"].([]any) {
		m := v.(map[string]any)
		l := ref.SignedList{List: unb64(sd.t, m["list"]), Sig: unb64(sd.t, m["sig"])}
		if m["prev_sig"] != nil {
			l.PrevSig = unb64(sd.t, m["prev_sig"])
		}
		lists = append(lists, l)
	}
	return lists, nil
}

// Lists fetches the versions of an account's device list this device has
// not seen and checks them. With no pin it takes the newest only.
func (sd *SealedDevice) Lists(account model.AccountID) error {
	query := ""
	if p := sd.D.Pins[ref.MustUUID(string(account))]; p != nil {
		query = fmt.Sprintf("?after=%d", p.Newest)
	}
	lists, err := sd.fetchLists(string(account), query)
	if err != nil {
		return err
	}
	return sd.D.LearnLists(ref.MustUUID(string(account)), lists)
}

// Backup fetches what the server gives a session to unlock with.
func (sd *SealedDevice) Backup() (ref.StoredBackup, map[string]any) {
	sd.t.Helper()
	b := sd.C.Must(200, "GET", "/api/v1/me/backup", nil)["backup"].(map[string]any)
	rec := b["account_record"].(map[string]any)
	return ref.StoredBackup{KeyVersion: uint32(b["key_version"].(float64)), Revision: uint64(rec["revision"].(float64)),
		AccountRecord: unb64(sd.t, rec["record"])}, b
}

// Copy finds one locked copy in a backup: the phrase's, a passkey's or a
// device's. It returns nil if there is none.
func (sd *SealedDevice) Copy(backup map[string]any, kind, id string) []byte {
	sd.t.Helper()
	for _, v := range backup["locked_copies"].([]any) {
		c := v.(map[string]any)
		if c["kind"] != kind {
			continue
		}
		if kind == "phrase" || kind == "access_key" || c["passkey_id"] == id || c["device_id"] == id {
			return unb64(sd.t, c["locked_key"])
		}
	}
	return nil
}

// UnlockWithPhrase unlocks this device with the recovery phrase.
func (sd *SealedDevice) UnlockWithPhrase(phrase string) error {
	sd.t.Helper()
	if err := sd.Lists(sd.C.ID); err != nil {
		return err
	}
	stored, backup := sd.Backup()
	return sd.D.UnlockWithPhrase(phrase, sd.Copy(backup, "phrase", ""), stored)
}

// UnlockWithAccessKey unlocks this device with the access key its browser
// holds (ruling R85).
func (sd *SealedDevice) UnlockWithAccessKey() error {
	sd.t.Helper()
	if err := sd.Lists(sd.C.ID); err != nil {
		return err
	}
	stored, backup := sd.Backup()
	cp := sd.Copy(backup, "access_key", "")
	if cp == nil {
		return errors.New("no access key copy")
	}
	return sd.D.UnlockWithAccessKey(sd.C.accessKey, cp, stored)
}

// UnlockWithDeviceCopy unlocks this device with the copy sealed to its own
// box key, which another device or the unlock page stored for it.
func (sd *SealedDevice) UnlockWithDeviceCopy() error {
	sd.t.Helper()
	if err := sd.Lists(sd.C.ID); err != nil {
		return err
	}
	stored, backup := sd.Backup()
	cp := sd.Copy(backup, "device", sd.ID)
	if cp == nil {
		return errors.New("no device copy for this device")
	}
	return sd.D.UnlockWithDeviceCopy(cp, stored)
}

// Publish posts a new version of the account's device list and pins it.
func (sd *SealedDevice) Publish(l ref.SignedList) {
	sd.t.Helper()
	sd.C.Must(201, "POST", "/api/v1/me/device-lists", listBody(sd.t, l))
	if err := sd.D.LearnLists(sd.D.Account, []ref.SignedList{l}); err != nil {
		sd.t.Fatal(err)
	}
}

// Join is what a device does once it is unlocked: it publishes the next
// version of its account's list with itself in it, and in each of the
// chats given reads the backup and makes a new epoch that includes itself.
func (sd *SealedDevice) Join(chats ...*SealedChat) {
	sd.t.Helper()
	if err := sd.Lists(sd.C.ID); err != nil {
		sd.t.Fatal(err)
	}
	l, err := sd.D.ListWithSelf()
	if err != nil {
		sd.t.Fatal(err)
	}
	sd.Publish(l)
	for _, c := range chats {
		c.Sync()
		c.ReadBackup()
		c.MakeEpoch()
	}
}

// SealedChat is one sealed chat as one device sees it.
type SealedChat struct {
	sd *SealedDevice
	// ID is the conversation; Other the other account.
	ID    string
	Other model.AccountID
	C     *ref.Chat
}

// Chat returns this device's view of a sealed chat that exists.
func (sd *SealedDevice) Chat(id string, other model.AccountID) *SealedChat {
	return &SealedChat{sd: sd, ID: id, Other: other, C: sd.D.Chat(ref.MustUUID(id), ref.MustUUID(string(other)))}
}

// Open starts the sealed chat with another account: the device makes the
// conversation's identifier and epoch record 1, sealed to every device on
// both accounts' lists, and posts them. If the chat exists already it is
// returned as it is.
func (sd *SealedDevice) Open(other model.AccountID) *SealedChat {
	sd.t.Helper()
	for _, account := range []model.AccountID{sd.C.ID, other} {
		if err := sd.Lists(account); err != nil {
			sd.t.Fatal(err)
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		sd.t.Fatal(err)
	}
	c := sd.Chat(id.String(), other)
	record, _, err := c.C.MakeEpoch()
	if err != nil {
		sd.t.Fatal(err)
	}
	status, out := sd.C.Do("POST", "/api/v1/sealed", map[string]any{"account_id": other, "epoch_record": b64(record)})
	if status != 200 && status != 201 {
		sd.t.Fatalf("opening a sealed chat = %d %v", status, out)
	}
	got := out["conversation"].(map[string]any)["id"].(string)
	if got != c.ID {
		// The pair had a chat already; the record sent was ignored.
		delete(sd.D.Chats, ref.MustUUID(c.ID))
		c = sd.Chat(got, other)
	}
	c.Sync()
	return c
}

// item is one numbered thing of a sealed chat: an epoch record or a row.
type item struct {
	seq      int64
	keyEpoch uint64
	record   []byte
	row      *ref.Row
}

// Sync brings the device up to date with the chat: both device lists, then
// every epoch record and message after what it has, in sequence order,
// each checked. What it learned is written to the account's backup.
// Anything refused is added to the device's Problems and returned.
func (c *SealedChat) Sync() []error {
	sd := c.sd
	sd.t.Helper()
	var problems []error
	note := func(err error) {
		if err != nil {
			problems = append(problems, err)
		}
	}
	for _, account := range []model.AccountID{sd.C.ID, c.Other} {
		note(sd.Lists(account))
	}
	var items []item
	from := int64(1)
	if cur := c.C.Current(); cur != nil {
		from = int64(cur.KeyEpoch) + 1
	}
	for {
		out := sd.C.Must(200, "GET", fmt.Sprintf("/api/v1/conversations/%s/sealed/epochs?from=%d&limit=200", c.ID, from), nil)
		for _, v := range out["epochs"].([]any) {
			e := v.(map[string]any)
			items = append(items, item{seq: int64(e["seq"].(float64)), keyEpoch: uint64(e["key_epoch"].(float64)), record: unb64(sd.t, e["record"])})
			from = int64(e["key_epoch"].(float64)) + 1
		}
		if out["has_more"] != true {
			break
		}
	}
	after := sd.seq[c.ID]
	for {
		out := sd.C.Must(200, "GET", fmt.Sprintf("/api/v1/conversations/%s/messages?after=%d&limit=200", c.ID, after), nil)
		for _, v := range out["messages"].([]any) {
			m := v.(map[string]any)
			after = int64(m["seq"].(float64))
			s, _ := m["sealed"].(map[string]any)
			if s == nil {
				continue // a system notice, or the marker of a deleted message
			}
			quiet, _ := m["quiet"].(bool)
			items = append(items, item{seq: after, row: &ref.Row{Seq: after, Author: ref.MustUUID(m["author_id"].(string)),
				KeyEpoch: uint64(s["key_epoch"].(float64)), Quiet: quiet, Ciphertext: unb64(sd.t, s["ciphertext"])}})
		}
		if out["has_more"] != true {
			break
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].seq < items[j].seq })
	for _, it := range items {
		if it.record != nil {
			note(c.C.AcceptEpoch(it.keyEpoch, it.seq, it.record))
			continue
		}
		sd.rows[c.ID] = append(sd.rows[c.ID], *it.row)
		got, err := c.C.Receive(*it.row)
		if err != nil && !errors.Is(err, ref.ErrNoKey) {
			note(err)
		}
		if got != nil {
			if got.Dropped {
				sd.Dropped++
			}
			if got.OutOfOrder {
				sd.OutOfOrder++
			}
			if got.Gap {
				sd.Gaps++
			}
		}
	}
	sd.seq[c.ID] = after
	c.upload()
	sd.Problems = append(sd.Problems, problems...)
	return problems
}

// upload writes the history secrets the device has just learned to its
// account's backup.
func (c *SealedChat) upload() {
	sd := c.sd
	sd.t.Helper()
	if !sd.D.Unlocked() {
		return
	}
	records, err := c.C.TakeNewSecrets()
	if err != nil {
		sd.t.Fatal(err)
	}
	for _, r := range records {
		status, out := sd.C.Do("PUT", fmt.Sprintf("/api/v1/me/backup/records/%s/%d", c.ID, r.KeyEpoch),
			map[string]any{"key_version": r.KeyVersion, "record": b64(r.Locked)})
		if status != 200 && status != 201 {
			sd.t.Fatalf("writing a backup record = %d %v", status, out)
		}
	}
}

// Records fetches every backup record of the account for this chat.
func (c *SealedChat) Records() []ref.StoredRecord {
	sd := c.sd
	sd.t.Helper()
	var out []ref.StoredRecord
	cursor := ""
	for {
		page := sd.C.Must(200, "GET", "/api/v1/me/backup/records?conversation_id="+c.ID+"&limit=200"+cursor, nil)
		for _, v := range page["records"].([]any) {
			r := v.(map[string]any)
			out = append(out, ref.StoredRecord{ConversationID: ref.MustUUID(r["conversation_id"].(string)), KeyEpoch: uint64(r["key_epoch"].(float64)),
				KeyVersion: uint32(r["key_version"].(float64)), Locked: unb64(sd.t, r["record"])})
		}
		next, _ := page["next_cursor"].(string)
		if next == "" {
			return out
		}
		cursor = "&cursor=" + next
	}
}

// ReadBackup reads the account's backup records for this chat, checks each
// against its epoch record, and then reads the rows it could not read
// before. It returns the records that did not check out.
func (c *SealedChat) ReadBackup() []error {
	sd := c.sd
	sd.t.Helper()
	var bad []error
	for _, r := range c.Records() {
		if e := c.C.Epoch(r.KeyEpoch); e != nil && e.History == nil {
			if err := c.C.ReadBackupRecord(r); err != nil {
				bad = append(bad, err)
			}
		}
	}
	c.Reread()
	return bad
}

// Reread goes over the rows the device has seen and reads those it now has
// the key for.
func (c *SealedChat) Reread() {
	for _, row := range c.sd.rows[c.ID] {
		c.C.Receive(row)
	}
	c.upload()
}

// MakeEpoch makes the chat's next key epoch from this device and posts it.
// If another device got there first the winner is taken in instead, as the
// loser of a race does. It reports whether this device's record stood.
func (c *SealedChat) MakeEpoch() bool {
	sd := c.sd
	sd.t.Helper()
	for _, account := range []model.AccountID{sd.C.ID, c.Other} {
		if err := sd.Lists(account); err != nil {
			sd.t.Fatal(err)
		}
	}
	record, keyEpoch, err := c.C.MakeEpoch()
	if err != nil {
		sd.t.Fatal(err)
	}
	status, out := sd.C.Do("POST", "/api/v1/conversations/"+c.ID+"/sealed/epochs", map[string]any{"key_epoch": keyEpoch, "record": b64(record)})
	if status != 201 && status != 409 {
		sd.t.Fatalf("posting an epoch record = %d %v", status, out)
	}
	c.Sync()
	return status == 201
}

// Send posts one sealed message: text, a reaction, an edit and so on, as
// ref's content functions make them. It makes a new epoch first when the
// rules ask for one, and encrypts again if the server says the epoch has
// moved on. It returns the stored message and the message's reference.
func (c *SealedChat) Send(content []byte, fileIDs ...string) (map[string]any, ref.UUID) {
	sd := c.sd
	sd.t.Helper()
	for attempt := 0; attempt < 3; attempt++ {
		c.Sync()
		if need, _ := c.C.NeedsEpoch(); need {
			c.MakeEpoch()
		}
		out, err := c.C.Seal(content)
		if err != nil {
			sd.t.Fatalf("sealing a message: %v", err)
		}
		body := map[string]any{"client_id": out.ClientID.String(), "quiet": out.Quiet,
			"sealed": map[string]any{"key_epoch": out.KeyEpoch, "ciphertext": b64(out.Ciphertext)}}
		if len(fileIDs) > 0 {
			body["file_ids"] = fileIDs
		}
		status, got := sd.C.Do("POST", "/api/v1/conversations/"+c.ID+"/messages", body)
		if status == 201 {
			c.Sync()
			return got["message"].(map[string]any), out.ClientID
		}
		// The epoch moved on, or a device list changed: the device
		// encrypts again in the current epoch with the same counter.
		c.C.Unsend(out)
		if status != 409 {
			sd.t.Fatalf("posting a sealed message = %d %v", status, got)
		}
	}
	sd.t.Fatal("a sealed message could not be sent in three attempts")
	return nil, ref.UUID{}
}

// Say sends a text message and returns its reference.
func (c *SealedChat) Say(text string) ref.UUID {
	c.sd.t.Helper()
	_, id := c.Send(ref.MessageContent(text, nil, nil, nil, nil))
	return id
}

// Texts lists what the device shows for the chat, in order: each message's
// text, or "(deleted)".
func (c *SealedChat) Texts() []string {
	var out []string
	for _, m := range c.C.View() {
		if m.Deleted {
			out = append(out, "(deleted)")
		} else {
			out = append(out, m.Text)
		}
	}
	return out
}
