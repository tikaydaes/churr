package ref

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
)

// A Chat is one sealed chat as one device holds it: the chain of epoch
// records, the secrets it has for them, its send counter, and what it
// shows.

// Chat is a device's state for one sealed chat.
type Chat struct {
	d *Device
	// ID is the conversation; Other the other account.
	ID    UUID
	Other UUID
	// Epochs is the chain of epoch records the device was given, in
	// order, refused ones included: a refused record keeps its place so
	// every device still agrees on what `prev` must be.
	Epochs []*Epoch
	// Counter is the counter of the last message this device sent here.
	// It carries on across epochs and is kept in the device's storage.
	Counter uint64
	// ShareDue is set when the device accepts an epoch record in which
	// the other account has a device again after epochs in which it had
	// none: the device then sends a `history` message.
	ShareDue bool

	// seen maps each (device, counter) to the sequence number it was
	// seen at; byDevice holds, per sending device, the counters seen so
	// far in sequence order.
	seen     map[sent]int64
	byDevice map[UUID][]sent

	// Messages are the messages the device shows, by reference; Order
	// lists the references by sequence number.
	Messages map[UUID]*Shown
	Order    []UUID
	// Unchecked counts rows shown as "A message could not be checked".
	Unchecked int
	// NewSecrets lists key epochs whose history secret the device has
	// just learned and has not yet written to its account's backup.
	NewSecrets []uint64
}

type sent struct {
	device  UUID
	counter uint64
	seq     int64
}

// Epoch is one key epoch as a device holds it.
type Epoch struct {
	KeyEpoch uint64
	// Seq is the record's place in the conversation's order.
	Seq int64
	// Stored is the record with its signature, exactly as received.
	Stored []byte
	// Record is the parsed record; nil if it does not parse.
	Record *EpochRecord
	// Accepted says whether the record passed section 5.4; Refusal is why
	// not.
	Accepted bool
	Refusal  error
	// Devices are the recipient devices with their keys, from the list
	// versions the record names.
	Devices map[UUID]ListDevice
	// Secret is the epoch secret, kept only while the epoch is current.
	// History is the history secret, kept as long as the epoch has
	// messages.
	Secret  []byte
	History []byte
}

// Shown is one message as a device draws it.
type Shown struct {
	Reference UUID
	Author    UUID
	Device    UUID
	Seq       int64
	KeyEpoch  uint64
	Text      string
	Files     []FileEntry
	ParentID  string
	Edited    bool
	Deleted   bool
	// OutOfOrder is the mark that the server delivered it out of order.
	OutOfOrder bool
	// Reactions maps each emoji to the accounts that put it there.
	Reactions map[string]map[UUID]bool
	// Tasks maps each checklist item to its state; taskSeq to the
	// sequence number that set it, since the latest by sequence wins.
	Tasks   map[string]bool
	taskSeq map[string]int64
}

// Chat returns the device's state for a sealed chat, making it if needed.
func (d *Device) Chat(conv, other UUID) *Chat {
	if c := d.Chats[conv]; c != nil {
		return c
	}
	c := &Chat{d: d, ID: conv, Other: other, seen: map[sent]int64{}, byDevice: map[UUID][]sent{}, Messages: map[UUID]*Shown{}}
	d.Chats[conv] = c
	return c
}

// Current is the newest record in the chain, accepted or not; nil for a
// chat with no epoch yet.
func (c *Chat) Current() *Epoch {
	if len(c.Epochs) == 0 {
		return nil
	}
	return c.Epochs[len(c.Epochs)-1]
}

// Epoch returns the record of one key epoch, or nil.
func (c *Chat) Epoch(keyEpoch uint64) *Epoch {
	for _, e := range c.Epochs {
		if e.KeyEpoch == keyEpoch {
			return e
		}
	}
	return nil
}

// The seven-day rule and how far ahead of the device's clock a record's
// time may be before the device stops trusting it.
const (
	EpochMaxAge    = 7 * 24 * 60 * 60 * 1000
	EpochMaxFuture = 5 * 60 * 1000
)

// NeedsEpoch reports whether this device must make a new epoch before it
// sends (section 7.1), and why: the chat has none, the newest record was
// refused, a device list is newer than the record names, or the record is
// more than seven days old or dated in the future.
func (c *Chat) NeedsEpoch() (bool, string) {
	cur := c.Current()
	if cur == nil {
		return true, "first"
	}
	if !cur.Accepted {
		return true, "refused"
	}
	for _, a := range cur.Record.Accounts {
		if p := c.d.Pins[a.AccountID]; p != nil && p.Newest > a.ListVersion {
			return true, "device_list"
		}
	}
	if _, in := cur.Record.Recipient(c.d.ID); !in {
		return true, "device_list"
	}
	now := c.d.Now()
	if cur.Record.At+EpochMaxAge < now {
		return true, "age"
	}
	if cur.Record.At > now+EpochMaxFuture {
		return true, "clock"
	}
	return false, ""
}

// MakeEpoch builds the chat's next epoch record from the newest device
// lists the device has checked for both accounts (section 5.3). The record
// is not part of the chain until the server has stored it and the device
// has accepted it back through AcceptEpoch like any other.
func (c *Chat) MakeEpoch() (stored []byte, keyEpoch uint64, err error) {
	d := c.d
	if !d.Unlocked() {
		return nil, 0, ErrLocked
	}
	n := NewEpoch{ConversationID: c.ID, KeyEpoch: 1, MakerAccountID: d.Account, MakerDeviceID: d.ID, MakerSign: d.Sign, At: d.Now()}
	if cur := c.Current(); cur != nil {
		n.KeyEpoch, n.Prev = cur.KeyEpoch+1, EpochHash(cur.Stored)
	}
	for i, account := range []UUID{d.Account, c.Other} {
		n.Lists[i] = EpochList{AccountID: account}
		if pl := d.Pins[account].NewestList(); pl != nil {
			n.Lists[i].List = pl.Signed.List
		}
	}
	if mine := d.Pins[d.Account].NewestList(); mine == nil {
		return nil, 0, errors.New("ref: the device's account has no device list")
	} else if _, in := mine.List.Device(d.ID); !in {
		return nil, 0, errors.New("ref: the device is not in its account's device list")
	}
	n.Secret = d.random(fmt.Sprintf("epoch_secret:%d", n.KeyEpoch), 32)
	n.OneTime = func(device UUID) []byte {
		return d.random(fmt.Sprintf("epoch_seal_one_time:%d:%s", n.KeyEpoch, device), 32)
	}
	stored, err = MakeEpoch(n)
	return stored, n.KeyEpoch, err
}

// ErrSecondRecord is returned when the server gives a second, different
// record for a key epoch the device already has one for. The first stands
// and the other is ignored (section 5.5).
var ErrSecondRecord = errors.New("ref: the server gave two records for one key epoch")

// ErrChainGap is returned when the server gives a record that does not
// follow the newest one the device holds: a record was dropped or the
// records were reordered.
var ErrChainGap = errors.New("ref: an epoch record is missing before this one")

// AcceptEpoch takes one epoch record from the server, with the key epoch
// and sequence number the server filed it under, and checks it (section
// 5.4).
//
// A record that passes becomes the chat's current epoch. A record that
// fails is kept in the chain as refused, the device shows "This chat's
// keys are out of step", and the *ErrEpoch is returned; NeedsEpoch then
// says the device must make the next epoch itself.
func (c *Chat) AcceptEpoch(keyEpoch uint64, seq int64, stored []byte) error {
	d := c.d
	if have := c.Epoch(keyEpoch); have != nil {
		if string(have.Stored) == string(stored) {
			return nil
		}
		return ErrSecondRecord
	}
	prev := c.Current()
	if prev != nil && keyEpoch != prev.KeyEpoch+1 {
		return ErrChainGap
	}
	e := &Epoch{KeyEpoch: keyEpoch, Seq: seq, Stored: stored}
	// The lists the record names are fetched before it is checked.
	if rec, _, err := SplitStoredEpoch(stored); err == nil {
		if parsed, err := DecodeEpochRecord(rec); err == nil {
			e.Record = parsed
			for _, a := range parsed.Accounts {
				d.ensureList(a.AccountID, a.ListVersion)
			}
		}
	}
	in := EpochCheckInput{Stored: stored, ConversationID: c.ID, Accounts: [2]UUID{d.Account, c.Other}, List: d.ListOf, DeviceID: d.ID, Box: d.Box}
	if prev != nil {
		in.Previous = prev.Stored
	}
	ok, err := CheckEpochRecord(in)
	if err == nil && ok.Record.KeyEpoch != keyEpoch {
		err = epochErr(2, "the record is not for the key epoch the server filed it under")
	}
	c.Epochs = append(c.Epochs, e)
	if err != nil {
		e.Refusal = err
		d.Notices = append(d.Notices, Notice{Kind: NoticeOutOfStep, Account: c.Other, Conversation: c.ID})
		return err
	}
	e.Accepted, e.Record, e.Devices = true, ok.Record, ok.Devices
	if ok.Secret != nil {
		e.Secret = ok.Secret
		e.History = HistorySecret(ok.Secret, c.ID, keyEpoch)
		c.NewSecrets = append(c.NewSecrets, keyEpoch)
	}
	if prev != nil {
		// The previous epoch's epoch secret is discarded; its history
		// secret stays.
		prev.Secret = nil
		if prev.Accepted && e.Record.HasAccount(c.Other) && !prev.Record.HasAccount(c.Other) {
			c.ShareDue = true
		}
	}
	return nil
}

// TakeNewSecrets returns the backup records to upload for history secrets
// the device has just learned: one per key epoch, locked under the backup
// key (section 13.2). The server keeps the first for each epoch and
// ignores the rest.
func (c *Chat) TakeNewSecrets() ([]StoredRecord, error) {
	d := c.d
	if !d.Unlocked() {
		return nil, ErrLocked
	}
	var out []StoredRecord
	for _, keyEpoch := range c.NewSecrets {
		e := c.Epoch(keyEpoch)
		if e == nil || e.History == nil {
			continue
		}
		locked, err := LockBackupRecord(d.BackupKey, d.Account, d.KeyVersion,
			&BackupRecord{Format: 1, ConversationID: c.ID, KeyEpoch: keyEpoch, Secret: e.History, At: d.Now()},
			d.random(fmt.Sprintf("backup_record_salt:%s:%d", c.ID, keyEpoch), SaltSize))
		if err != nil {
			return nil, err
		}
		out = append(out, StoredRecord{ConversationID: c.ID, KeyEpoch: keyEpoch, KeyVersion: d.KeyVersion, Locked: locked})
	}
	c.NewSecrets = nil
	return out, nil
}

// ErrBackupRecord is the refusal of a backup record: it does not open, it
// is filed under the wrong chat or epoch, or its secret is not the one the
// epoch record announced.
var ErrBackupRecord = errors.New("ref: the backup record does not check out")

// ReadBackupRecord opens one of the account's backup records for this chat
// and, if its secret matches the epoch record's `check`, keeps the history
// secret (section 13.2, "Reading").
func (c *Chat) ReadBackupRecord(r StoredRecord) error {
	d := c.d
	if !d.Unlocked() {
		return ErrLocked
	}
	e := c.Epoch(r.KeyEpoch)
	if e == nil || !e.Accepted || r.ConversationID != c.ID {
		return ErrBackupRecord
	}
	b, err := OpenBackupRecord(d.BackupKey, d.Account, c.ID, r.KeyEpoch, r.KeyVersion, r.Locked)
	if err != nil || !CheckHistorySecret(b.Secret, e.Record) {
		return ErrBackupRecord
	}
	e.History = b.Secret
	return nil
}

// Outgoing is a sealed message ready to post.
type Outgoing struct {
	// Ciphertext is the sealed bytes; ClientID the message reference,
	// sent to the server as client_id; KeyEpoch and Quiet are sent beside
	// them.
	Ciphertext []byte
	ClientID   UUID
	KeyEpoch   uint64
	Quiet      bool
	Counter    uint64
}

// ErrHeld is returned while sending is held: the other account's identity
// was reset after it had been verified, and the person has not yet
// acknowledged the notice.
var ErrHeld = errors.New("ref: sending is held until the reset notice is acknowledged")

// ErrNeedsEpoch is returned by Seal when the device must make a new epoch
// first.
var ErrNeedsEpoch = errors.New("ref: a new key epoch is needed before sending")

// Seal encrypts and signs one message in the chat's current epoch. Every
// kind of content but `message` is sent quiet.
func (c *Chat) Seal(content []byte) (*Outgoing, error) {
	d := c.d
	if p := d.Pins[c.Other]; p != nil && p.Hold {
		return nil, ErrHeld
	}
	if need, _ := c.NeedsEpoch(); need {
		return nil, ErrNeedsEpoch
	}
	cur := c.Current()
	if cur.History == nil {
		return nil, ErrNeedsEpoch
	}
	var kind struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(content, &kind); err != nil {
		return nil, fmt.Errorf("ref: content must be a JSON object")
	}
	h := Header{Format: 1, ConversationID: c.ID, KeyEpoch: cur.KeyEpoch, AccountID: d.Account, DeviceID: d.ID, Counter: c.Counter + 1}
	if kind.Kind != KindMessage {
		h.Flags = FlagQuiet
	}
	copy(h.Reference[:], d.random("message_reference", 16))
	// A reference is a UUID; these bits make it a version 4 one.
	h.Reference[6] = h.Reference[6]&0x0f | 0x40
	h.Reference[8] = h.Reference[8]&0x3f | 0x80
	senderKey := SenderKey(ContentKey(cur.History, c.ID, cur.KeyEpoch), c.ID, cur.KeyEpoch, d.ID)
	sealed, err := SealMessage(h, senderKey, d.random("message_salt", SaltSize), content, d.Sign)
	if err != nil {
		return nil, err
	}
	c.Counter++
	return &Outgoing{Ciphertext: sealed, ClientID: h.Reference, KeyEpoch: cur.KeyEpoch, Quiet: h.Quiet(), Counter: h.Counter}, nil
}

// Unsend takes back the counter of a message the server refused, so the
// device can encrypt it again in the current epoch with the same counter.
func (c *Chat) Unsend(o *Outgoing) {
	if c.Counter == o.Counter {
		c.Counter--
	}
}

// Row is a sealed message's row as the server gives it.
type Row struct {
	Seq        int64
	Author     UUID
	KeyEpoch   uint64
	Quiet      bool
	Ciphertext []byte
}

// Received is what a device made of one row.
type Received struct {
	Header Header
	// Kind is the content's kind; Content the parsed object.
	Kind    string
	Content map[string]any
	// Dropped: the same device and counter was seen at a lower sequence
	// number. The server replayed it, and it is not shown.
	Dropped bool
	// OutOfOrder: its counter is not higher than the last one seen from
	// that device at a lower sequence number. It is shown with a mark.
	OutOfOrder bool
	// Already: this very row was taken in before; nothing changed.
	Already bool
	// Gap: the device's counters jumped. Unless a deletion marker or
	// retention explains it, the device shows "A message may be missing
	// here." It is a hint, not a proof.
	Gap bool
}

// ErrNoKey is returned for a message in an epoch the device has no history
// secret for yet. It is not a failed check: the message is read once the
// secret arrives from the backup or from the other person.
var ErrNoKey = errors.New("ref: the device has no key for that epoch yet")

// Receive checks one row (section 8.2, "Receiving") and, if it passes,
// applies its content to what the device shows. A row that fails is shown
// as "A message could not be checked" and its content is never drawn.
func (c *Chat) Receive(row Row) (*Received, error) {
	r, err := c.receive(row)
	if err != nil && errors.Is(err, ErrMessage) {
		c.Unchecked++
	}
	return r, err
}

func (c *Chat) receive(row Row) (*Received, error) {
	// 1. The header agrees with the row it arrived in.
	h, err := DecodeHeader(row.Ciphertext)
	if err != nil {
		return nil, err
	}
	if h.ConversationID != c.ID || h.KeyEpoch != row.KeyEpoch || h.AccountID != row.Author {
		return nil, messageErr("the header does not match the row it arrived in")
	}
	if h.Quiet() != row.Quiet {
		return nil, messageErr("the quiet flag was changed")
	}
	// 2. The device accepted that epoch's record, the message sits in
	// that epoch's part of the sequence, and the sending device is one of
	// its recipients.
	e := c.Epoch(h.KeyEpoch)
	if e == nil || !e.Accepted {
		return nil, messageErr("the device did not accept that epoch's record")
	}
	if row.Seq <= e.Seq {
		return nil, messageErr("the message comes before its epoch's record")
	}
	if next := c.Epoch(h.KeyEpoch + 1); next != nil && row.Seq > next.Seq {
		return nil, messageErr("the message comes after a newer epoch began")
	}
	rc, in := e.Record.Recipient(h.DeviceID)
	if !in || rc.AccountID != h.AccountID {
		return nil, messageErr("the sending device is not in that epoch")
	}
	if e.History == nil {
		return nil, ErrNoKey
	}
	// 3. The signature verifies under the device's key from the list
	// version the epoch names, and the content decrypts.
	senderKey := SenderKey(ContentKey(e.History, c.ID, e.KeyEpoch), c.ID, e.KeyEpoch, h.DeviceID)
	_, content, err := OpenMessage(row.Ciphertext, senderKey, e.Devices[h.DeviceID].SignKey)
	if err != nil {
		return nil, err
	}
	out := &Received{Header: h}
	// 4. Replays: of two rows with one device and counter, the lower
	// sequence number is the message.
	key := sent{device: h.DeviceID, counter: h.Counter}
	if at, dup := c.seen[key]; dup && at == row.Seq {
		// The same row again, as when a device goes back over rows it
		// could not read before: it is applied once.
		out.Already = true
		return out, nil
	}
	if at, dup := c.seen[key]; dup && at != row.Seq {
		if at < row.Seq {
			out.Dropped = true
			return out, nil
		}
		// The copy seen first was the replay; this one takes its place.
		// It leaves the display order too, or the message would be drawn
		// twice once the original is applied under the same reference.
		for ref, m := range c.Messages {
			if m.Seq == at && m.Device == h.DeviceID {
				delete(c.Messages, ref)
				c.Order = slices.DeleteFunc(c.Order, func(r UUID) bool { return r == ref })
			}
		}
	}
	c.seen[key] = row.Seq
	// 5. Order: the counter should be higher than the last one seen from
	// that device at a lower sequence number. Rows are not always taken
	// in in order (a device reads an old epoch once its secret arrives),
	// so the comparison is with the nearest row below this one.
	rows := c.byDevice[h.DeviceID]
	at := sort.Search(len(rows), func(i int) bool { return rows[i].seq >= row.Seq })
	if at < len(rows) && rows[at].seq == row.Seq {
		rows[at].counter = h.Counter
	} else {
		rows = append(rows, sent{})
		copy(rows[at+1:], rows[at:])
		rows[at] = sent{h.DeviceID, h.Counter, row.Seq}
		c.byDevice[h.DeviceID] = rows
	}
	switch {
	case at == 0:
		out.Gap = h.Counter > 1
	case h.Counter <= rows[at-1].counter:
		out.OutOfOrder = true
	default:
		out.Gap = h.Counter > rows[at-1].counter+1
	}
	if err := json.Unmarshal(content, &out.Content); err != nil {
		return nil, messageErr("the content is not a JSON object")
	}
	out.Kind, _ = out.Content["kind"].(string)
	c.apply(out, row.Seq, e.KeyEpoch)
	return out, nil
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

// apply draws one checked message. A kind the device does not know is
// ignored, and so is anything that refers to a message it does not have.
func (c *Chat) apply(r *Received, seq int64, keyEpoch uint64) {
	h, m := r.Header, r.Content
	target := func() *Shown {
		ref, err := ParseUUID(str(m, "message_id"))
		if err != nil {
			return nil
		}
		return c.Messages[ref]
	}
	switch r.Kind {
	case KindMessage:
		s := &Shown{Reference: h.Reference, Author: h.AccountID, Device: h.DeviceID, Seq: seq, KeyEpoch: keyEpoch, Text: str(m, "text"),
			ParentID: str(m, "parent_id"), OutOfOrder: r.OutOfOrder, Reactions: map[string]map[UUID]bool{}, Tasks: map[string]bool{}, taskSeq: map[string]int64{}}
		if raw, err := json.Marshal(m["files"]); err == nil {
			json.Unmarshal(raw, &s.Files)
		}
		if _, dup := c.Messages[h.Reference]; dup {
			return // a reference is used once
		}
		c.Messages[h.Reference] = s
		c.Order = append(c.Order, h.Reference)
		sort.SliceStable(c.Order, func(i, j int) bool { return c.seqOf(c.Order[i]) < c.seqOf(c.Order[j]) })
	case KindEdit:
		// Applied only if the sender's account wrote the target.
		if t := target(); t != nil && t.Author == h.AccountID && !t.Deleted {
			t.Text, t.Edited = str(m, "text"), true
		}
	case KindDelete:
		if t := target(); t != nil && t.Author == h.AccountID {
			t.Deleted, t.Text, t.Files = true, "", nil
		}
	case KindReaction:
		t := target()
		emoji := str(m, "emoji")
		on, _ := m["on"].(bool)
		if t == nil || emoji == "" || t.Deleted {
			return
		}
		if !on {
			delete(t.Reactions[emoji], h.AccountID)
			if len(t.Reactions[emoji]) == 0 {
				delete(t.Reactions, emoji)
			}
			return
		}
		// At most twenty different emoji on a message; one of each per
		// account.
		if t.Reactions[emoji] == nil {
			if len(t.Reactions) >= 20 {
				return
			}
			t.Reactions[emoji] = map[UUID]bool{}
		}
		t.Reactions[emoji][h.AccountID] = true
	case KindTask:
		t := target()
		item := str(m, "item_id")
		checked, _ := m["checked"].(bool)
		// Either person may tick; the state is the latest by sequence.
		if t != nil && item != "" && seq > t.taskSeq[item] {
			t.Tasks[item], t.taskSeq[item] = checked, seq
		}
	case KindHistory:
		list, _ := m["secrets"].([]any)
		for _, raw := range list {
			entry, _ := raw.(map[string]any)
			n, _ := entry["key_epoch"].(float64)
			secret := UnB64(str(entry, "secret"))
			e := c.Epoch(uint64(n))
			// The device does not take the sender's word: each secret is
			// checked against that epoch's record.
			if e == nil || !e.Accepted || e.History != nil || !CheckHistorySecret(secret, e.Record) {
				continue
			}
			e.History = secret
			c.NewSecrets = append(c.NewSecrets, e.KeyEpoch)
		}
	}
}

func (c *Chat) seqOf(ref UUID) int64 {
	if m := c.Messages[ref]; m != nil {
		return m.Seq
	}
	return 0
}

// HistoryShare builds the `history` message a device sends when the other
// account has a device again after epochs in which it had none (section
// 12.5): the history secrets of those epochs, so the other person can read
// what was written to them meanwhile. ok is false if there is nothing to
// share.
//
// Nothing is shared across a reset: if the other account's list was reset,
// no secret of an epoch from before the reset is sent, ever.
func (c *Chat) HistoryShare() (content []byte, ok bool) {
	c.ShareDue = false
	var reset uint64
	if p := c.d.Pins[c.Other]; p != nil {
		reset = p.LastReset
	}
	var secrets []HistorySecretEntry
	for _, e := range c.Epochs {
		if !e.Accepted || e.History == nil || e.Record.HasAccount(c.Other) {
			continue
		}
		if ref, _ := e.Record.Ref(c.Other); ref.ListVersion < reset {
			continue
		}
		secrets = append(secrets, HistorySecretEntry{KeyEpoch: e.KeyEpoch, Secret: B64(e.History)})
	}
	if len(secrets) == 0 {
		return nil, false
	}
	return HistoryContent(secrets), true
}

// CallKeys derives, for a call in the chat's current epoch, the key each
// device of the epoch encrypts its frames under, and the key index every
// frame carries (section 10). They come from the epoch secret, so only a
// device that was a recipient of the current epoch can make them.
func (c *Chat) CallKeys(call UUID) (keys map[UUID][]byte, index byte, err error) {
	cur := c.Current()
	if cur == nil || !cur.Accepted || cur.Secret == nil {
		return nil, 0, errors.New("ref: the device does not hold the current epoch secret")
	}
	secret := CallSecret(cur.Secret, c.ID, cur.KeyEpoch, call)
	keys = map[UUID][]byte{}
	for _, r := range cur.Record.Recipients {
		keys[r.DeviceID] = CallSenderKey(secret, c.ID, cur.KeyEpoch, r.DeviceID)
	}
	return keys, CallKeyIndex(cur.KeyEpoch), nil
}

// View lists what the device shows for the chat, in order: for each
// message its reference, author, text and marks. It is what the transcript
// vectors record after each step.
func (c *Chat) View() []Shown {
	out := make([]Shown, 0, len(c.Order))
	for _, ref := range c.Order {
		if m := c.Messages[ref]; m != nil {
			out = append(out, *m)
		}
	}
	return out
}
