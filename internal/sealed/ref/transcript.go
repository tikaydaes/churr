package ref

import (
	"encoding/hex"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
)

// The transcript: a whole conversation between two accounts with several
// devices each, as every byte the server would store, in order, with what
// each device must be able to read and must show after each step
// (docs/SEALED.md section 18, vector 13).
//
// It is made by real Devices talking to a stand-in for the server that
// does only what the scheme asks of a server: it files device lists and
// epoch records in order, numbers the conversation, and keeps the backup.
// Every random value a device draws is recorded with the step that drew
// it, so another implementation can be fed the same values and must then
// produce the same bytes (signatures apart; testdata/sealed/README.md).

// The fixed identifiers of the transcript.
var (
	tAnn  = MustUUID("0193a000-0000-7000-8000-00000000000a")
	tBob  = MustUUID("0193a000-0000-7000-8000-00000000000b")
	tConv = MustUUID("0193a0e0-0000-7000-8000-0000000000c0")
	// tPasskey is the passkey Ann's tablet unlocks with.
	tPasskey = MustUUID("0193a0aa-0000-7000-8000-0000000000f1")
)

const (
	tServer = "chat.example.org"
	// tStart is the transcript's clock at its first step, in milliseconds:
	// 2027-01-15T08:00:00.000Z.
	tStart uint64 = 1800000000000
	tDay   uint64 = 24 * 60 * 60 * 1000
)

// TranscriptStep is one step of the transcript.
type TranscriptStep struct {
	Step  int    `json:"step"`
	What  string `json:"what"`
	Actor string `json:"actor,omitempty"`
	// Clock is every device's clock during the step, in milliseconds.
	Clock uint64 `json:"clock"`
	// Random lists every random value drawn during the step, in order.
	Random []TranscriptRandom `json:"random"`
	// Sent is the plaintext of a message posted in the step.
	Sent *TranscriptSent `json:"sent,omitempty"`
	// Stored lists what the server stored or deleted, in order.
	Stored []map[string]any `json:"stored"`
	// Refused lists what the server or a device refused, and why.
	Refused []map[string]any `json:"refused,omitempty"`
	// After is what each device holds and shows once the step is over.
	After map[string]TranscriptView `json:"after"`
}

// TranscriptRandom is one random value a device drew.
type TranscriptRandom struct {
	Device string `json:"device"`
	For    string `json:"for"`
	Value  string `json:"value"`
}

// TranscriptSent is the inside of a message posted in a step.
type TranscriptSent struct {
	Content   string `json:"content"`
	Reference string `json:"reference"`
	Counter   uint64 `json:"counter"`
	KeyEpoch  uint64 `json:"key_epoch"`
	Quiet     bool   `json:"quiet"`
}

// TranscriptView is what one device holds and shows after a step.
type TranscriptView struct {
	Unlocked   bool   `json:"unlocked"`
	KeyVersion uint32 `json:"key_version"`
	// Pins gives, per account, the newest device list version accepted.
	Pins map[string]uint64 `json:"pins"`
	// Epochs lists the chat's epoch records as the device holds them:
	// whether it accepted each and whether it can read its messages.
	Epochs []TranscriptEpoch `json:"epochs"`
	// Shows lists the messages the device draws, in order.
	Shows []TranscriptMessage `json:"shows"`
	// Unchecked counts rows shown as "A message could not be checked".
	Unchecked int `json:"unchecked"`
	// Notices lists every notice shown so far.
	Notices []TranscriptNotice `json:"notices"`
}

// TranscriptEpoch is one epoch as a device holds it.
type TranscriptEpoch struct {
	KeyEpoch uint64 `json:"key_epoch"`
	Accepted bool   `json:"accepted"`
	CanRead  bool   `json:"can_read"`
}

// TranscriptMessage is one message as a device draws it.
type TranscriptMessage struct {
	Reference  string              `json:"reference"`
	Author     string              `json:"author_id"`
	Seq        int64               `json:"seq"`
	Text       string              `json:"text"`
	Edited     bool                `json:"edited,omitempty"`
	Deleted    bool                `json:"deleted,omitempty"`
	OutOfOrder bool                `json:"out_of_order,omitempty"`
	Reactions  map[string][]string `json:"reactions,omitempty"`
	Tasks      map[string]bool     `json:"tasks,omitempty"`
}

// TranscriptNotice is one notice a device showed.
type TranscriptNotice struct {
	Kind     string `json:"kind"`
	Account  string `json:"account_id"`
	Platform string `json:"platform,omitempty"`
}

// TranscriptDevice is one device that takes part in the transcript. Its
// keys are the first two random values listed for it.
type TranscriptDevice struct {
	Name      string `json:"name"`
	DeviceID  string `json:"device_id"`
	AccountID string `json:"account_id"`
	Platform  string `json:"platform"`
}

// simBackup is one account's backup as the server holds it.
type simBackup struct {
	keyVersion uint32
	revision   uint64
	record     []byte
	copies     []Copy
	records    []StoredRecord
}

// simItem is one numbered thing in the conversation: an epoch record or a
// message row.
type simItem struct {
	seq      int64
	keyEpoch uint64
	stored   []byte // an epoch record
	row      *Row   // or a message
}

// simDevice is one device of the transcript.
type simDevice struct {
	name string
	d    *Device
	// pos is how much of the conversation the device has taken in.
	pos int
	// gone marks a device that signed out, was revoked or was replaced.
	gone bool
}

type sim struct {
	now     uint64
	lists   map[UUID][]SignedList
	backups map[UUID]*simBackup
	items   []simItem
	nextSeq int64
	epoch   uint64
	devices []*simDevice
	steps   []TranscriptStep
	cur     *TranscriptStep
	pending []TranscriptRandom
	cast    []TranscriptDevice
}

func hx(b []byte) string { return hex.EncodeToString(b) }

func (s *sim) begin(what string, actor *simDevice) {
	s.cur = &TranscriptStep{Step: len(s.steps) + 1, What: what, Clock: s.now, Random: []TranscriptRandom{}, Stored: []map[string]any{}}
	if actor != nil {
		s.cur.Actor = actor.name
	}
	s.cur.Random = append(s.cur.Random, s.pending...)
	s.pending = nil
}

func (s *sim) end() {
	s.cur.After = map[string]TranscriptView{}
	for _, dev := range s.devices {
		if !dev.gone {
			s.cur.After[dev.name] = s.view(dev)
		}
	}
	s.steps = append(s.steps, *s.cur)
	s.cur = nil
}

func (s *sim) view(dev *simDevice) TranscriptView {
	d := dev.d
	v := TranscriptView{Unlocked: d.Unlocked(), KeyVersion: d.KeyVersion, Pins: map[string]uint64{}, Epochs: []TranscriptEpoch{},
		Shows: []TranscriptMessage{}, Notices: []TranscriptNotice{}}
	for account, p := range d.Pins {
		v.Pins[account.String()] = p.Newest
	}
	if c := d.Chats[tConv]; c != nil {
		for _, e := range c.Epochs {
			v.Epochs = append(v.Epochs, TranscriptEpoch{KeyEpoch: e.KeyEpoch, Accepted: e.Accepted, CanRead: e.History != nil})
		}
		v.Unchecked = c.Unchecked
		for _, m := range c.View() {
			tm := TranscriptMessage{Reference: m.Reference.String(), Author: m.Author.String(), Seq: m.Seq, Text: m.Text,
				Edited: m.Edited, Deleted: m.Deleted, OutOfOrder: m.OutOfOrder}
			for emoji, who := range m.Reactions {
				if tm.Reactions == nil {
					tm.Reactions = map[string][]string{}
				}
				for account := range who {
					tm.Reactions[emoji] = append(tm.Reactions[emoji], account.String())
				}
				sort.Strings(tm.Reactions[emoji])
			}
			if len(m.Tasks) > 0 {
				tm.Tasks = m.Tasks
			}
			v.Shows = append(v.Shows, tm)
		}
	}
	for _, n := range d.Notices {
		v.Notices = append(v.Notices, TranscriptNotice{Kind: n.Kind, Account: n.Account.String(), Platform: PlatformNames[n.Platform]})
	}
	return v
}

func (s *sim) store(entry map[string]any)  { s.cur.Stored = append(s.cur.Stored, entry) }
func (s *sim) refuse(entry map[string]any) { s.cur.Refused = append(s.cur.Refused, entry) }

// device makes a new device with its keys. Its identifier is one the
// server would have given its row.
func (s *sim) device(name string, account UUID, platform byte, id string) *simDevice {
	dev := &simDevice{name: name}
	d := &Device{Account: account, Server: tServer, Platform: platform, Rand: NewStream("transcript " + name),
		Now: func() uint64 { return s.now }, Pins: map[UUID]*Pin{}, Chats: map[UUID]*Chat{}}
	d.Trace = func(what string, value []byte) {
		r := TranscriptRandom{Device: name, For: what, Value: hx(value)}
		if s.cur == nil {
			// A device makes its keys before the step it first acts in;
			// they are listed with that step.
			s.pending = append(s.pending, r)
			return
		}
		s.cur.Random = append(s.cur.Random, r)
	}
	d.FetchLists = func(account UUID, after uint64) ([]SignedList, error) { return s.listsAfter(account, after), nil }
	d.Sign = SigningKey(d.random("device_sign_seed", 32))
	d.Box = d.random("device_box_private", 32)
	d.AddedAt = s.now
	d.ID = MustUUID(id)
	dev.d = d
	s.devices = append(s.devices, dev)
	s.cast = append(s.cast, TranscriptDevice{Name: name, DeviceID: id, AccountID: account.String(), Platform: PlatformNames[platform]})
	return dev
}

func (s *sim) listsAfter(account UUID, after uint64) []SignedList {
	var out []SignedList
	for i, l := range s.lists[account] {
		if uint64(i+1) > after {
			out = append(out, l)
		}
	}
	return out
}

func other(account UUID) UUID {
	if account == tAnn {
		return tBob
	}
	return tAnn
}

func (dev *simDevice) chat() *Chat { return dev.d.Chat(tConv, other(dev.d.Account)) }

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// publish stores the next version of an account's device list, if it is
// the next version.
func (s *sim) publish(dev *simDevice, l SignedList) {
	parsed, err := DecodeDeviceList(l.List)
	must(err)
	if parsed.Version != uint64(len(s.lists[parsed.AccountID]))+1 {
		panic("transcript: not the next list version")
	}
	s.lists[parsed.AccountID] = append(s.lists[parsed.AccountID], l)
	stored, err := l.Encode()
	must(err)
	s.store(map[string]any{"table": "device_lists", "account_id": parsed.AccountID.String(), "version": parsed.Version, "signed_list": hx(stored)})
}

func (s *sim) storeCopies(account UUID, keyVersion uint32, copies []Copy) {
	for _, c := range copies {
		e := map[string]any{"table": "backup_keys", "account_id": account.String(), "kind": WayNames[c.Kind], "key_version": keyVersion, "locked_key": hx(c.Locked)}
		switch c.Kind {
		case WayPasskey:
			e["passkey_id"] = c.ID.String()
		case WayDevice:
			e["device_id"] = c.ID.String()
		}
		s.store(e)
	}
}

// putBundle stores a created, replaced or reset backup with its device
// list, together.
func (s *sim) putBundle(dev *simDevice, b *Bundle, how string) {
	account := dev.d.Account
	if how != "create" {
		s.store(map[string]any{"deleted": []string{"account_records", "backup_keys", "key_backups"}, "account_id": account.String(), "because": how})
	}
	s.backups[account] = &simBackup{keyVersion: b.KeyVersion, revision: b.Revision, record: b.AccountRecord, copies: b.Copies, records: b.Records}
	s.store(map[string]any{"table": "account_records", "account_id": account.String(), "key_version": b.KeyVersion, "revision": b.Revision, "record": hx(b.AccountRecord)})
	s.storeCopies(account, b.KeyVersion, b.Copies)
	for _, r := range b.Records {
		s.store(map[string]any{"table": "key_backups", "account_id": account.String(), "conversation_id": r.ConversationID.String(),
			"key_epoch": r.KeyEpoch, "key_version": r.KeyVersion, "record": hx(r.Locked)})
	}
	s.publish(dev, b.List)
	must(dev.d.Adopt(b))
}

func (s *sim) stored(account UUID) StoredBackup {
	b := s.backups[account]
	return StoredBackup{KeyVersion: b.keyVersion, Revision: b.revision, AccountRecord: b.record}
}

func (s *sim) copyOf(account UUID, kind byte, id UUID) []byte {
	for _, c := range s.backups[account].copies {
		if c.Kind == kind && c.ID == id {
			return c.Locked
		}
	}
	panic("transcript: no such locked copy")
}

// learn gives a device the versions of both accounts' lists above what it
// has pinned; a device with no pin is given the newest only.
func (s *sim) learn(dev *simDevice) {
	for _, account := range []UUID{tAnn, tBob} {
		all := s.lists[account]
		if len(all) == 0 {
			continue
		}
		var give []SignedList
		if p := dev.d.Pins[account]; p == nil {
			give = all[len(all)-1:]
		} else {
			give = s.listsAfter(account, p.Newest)
		}
		if len(give) > 0 {
			if err := dev.d.LearnLists(account, give); err != nil {
				s.refuse(map[string]any{"device": dev.name, "refused": "device_list", "account_id": account.String(), "why": err.Error()})
			}
		}
	}
}

// sync brings one device up to date: device lists, then the conversation
// from where it left off, then the backup records for what it learned.
func (s *sim) sync(dev *simDevice) {
	if dev.gone {
		return
	}
	s.learn(dev)
	c := dev.chat()
	for ; dev.pos < len(s.items); dev.pos++ {
		it := s.items[dev.pos]
		if it.stored != nil {
			if err := c.AcceptEpoch(it.keyEpoch, it.seq, it.stored); err != nil {
				s.refuse(map[string]any{"device": dev.name, "refused": "epoch_record", "key_epoch": it.keyEpoch, "why": err.Error()})
			}
			continue
		}
		if it.row.Ciphertext == nil {
			continue // a deletion marker
		}
		if _, err := c.Receive(*it.row); err != nil && !errors.Is(err, ErrNoKey) {
			s.refuse(map[string]any{"device": dev.name, "refused": "message", "seq": it.seq, "why": err.Error()})
		}
	}
	s.upload(dev)
}

// upload writes a device's newly learned history secrets to its account's
// backup. The server keeps the first record for each epoch.
func (s *sim) upload(dev *simDevice) {
	if !dev.d.Unlocked() {
		return
	}
	recs, err := dev.chat().TakeNewSecrets()
	must(err)
	b := s.backups[dev.d.Account]
	for _, r := range recs {
		have := false
		for _, old := range b.records {
			have = have || (old.ConversationID == r.ConversationID && old.KeyEpoch == r.KeyEpoch)
		}
		if have {
			continue
		}
		b.records = append(b.records, r)
		s.store(map[string]any{"table": "key_backups", "account_id": dev.d.Account.String(), "conversation_id": r.ConversationID.String(),
			"key_epoch": r.KeyEpoch, "key_version": r.KeyVersion, "record": hx(r.Locked)})
	}
}

func (s *sim) syncAll() {
	for _, dev := range s.devices {
		s.sync(dev)
	}
}

// readBackup gives a device every backup record of its account, which it
// opens and checks against the epoch records.
func (s *sim) readBackup(dev *simDevice) {
	for _, r := range s.backups[dev.d.Account].records {
		if e := dev.chat().Epoch(r.KeyEpoch); e != nil && e.History == nil {
			if err := dev.chat().ReadBackupRecord(r); err != nil {
				s.refuse(map[string]any{"device": dev.name, "refused": "backup_record", "key_epoch": r.KeyEpoch, "why": err.Error()})
			}
		}
	}
	// What it read may make earlier rows readable.
	s.reread(dev)
}

// reread has a device go over rows it could not read before.
func (s *sim) reread(dev *simDevice) {
	c := dev.chat()
	for _, it := range s.items[:dev.pos] {
		if it.row == nil || it.row.Ciphertext == nil {
			continue
		}
		if _, seen := c.seen[sentKey(it.row.Ciphertext)]; seen {
			continue
		}
		c.Receive(*it.row)
	}
}

func sentKey(sealed []byte) sent {
	h, _ := DecodeHeader(sealed)
	return sent{device: h.DeviceID, counter: h.Counter}
}

// makeEpoch has a device make the chat's next epoch and post it. The
// server stores exactly one record per key epoch, and only the next number.
func (s *sim) makeEpoch(dev *simDevice) bool {
	stored, keyEpoch, err := dev.chat().MakeEpoch()
	must(err)
	return s.postEpoch(dev, keyEpoch, stored)
}

func (s *sim) postEpoch(dev *simDevice, keyEpoch uint64, stored []byte) bool {
	if keyEpoch != s.epoch+1 {
		s.refuse(map[string]any{"device": dev.name, "refused": "epoch_record", "by": "server", "code": "conflict", "key_epoch": keyEpoch,
			"record": hx(stored), "why": "another record already holds this key epoch"})
		return false
	}
	s.forceEpoch(keyEpoch, stored)
	return true
}

// lastEpoch is the stored record of the chat's current key epoch.
func (s *sim) lastEpoch() []byte {
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].stored != nil {
			return s.items[i].stored
		}
	}
	return nil
}

func (s *sim) forceEpoch(keyEpoch uint64, stored []byte) {
	s.epoch = keyEpoch
	s.nextSeq++
	s.items = append(s.items, simItem{seq: s.nextSeq, keyEpoch: keyEpoch, stored: stored})
	s.store(map[string]any{"table": "sealed_epochs", "conversation_id": tConv.String(), "key_epoch": keyEpoch, "seq": s.nextSeq, "record": hx(stored)})
}

// send has a device post one message. The server accepts it only in the
// chat's current key epoch.
func (s *sim) send(dev *simDevice, content []byte) UUID {
	c := dev.chat()
	out, err := c.Seal(content)
	must(err)
	if out.KeyEpoch != s.epoch {
		panic("transcript: a message in a stale epoch")
	}
	s.nextSeq++
	row := &Row{Seq: s.nextSeq, Author: dev.d.Account, KeyEpoch: out.KeyEpoch, Quiet: out.Quiet, Ciphertext: out.Ciphertext}
	s.items = append(s.items, simItem{seq: s.nextSeq, row: row})
	s.cur.Sent = &TranscriptSent{Content: string(content), Reference: out.ClientID.String(), Counter: out.Counter, KeyEpoch: out.KeyEpoch, Quiet: out.Quiet}
	s.store(map[string]any{"table": "messages", "conversation_id": tConv.String(), "seq": s.nextSeq, "author_id": dev.d.Account.String(),
		"client_id": out.ClientID.String(), "key_epoch": out.KeyEpoch, "quiet": out.Quiet, "ciphertext": hx(out.Ciphertext)})
	return out.ClientID
}

// say is one whole step: a device posts a message and everyone syncs.
func (s *sim) say(dev *simDevice, what string, content []byte) UUID {
	s.begin(what, dev)
	ref := s.send(dev, content)
	s.syncAll()
	s.end()
	return ref
}

// erase is the server deleting a row at its author's request: the
// ciphertext goes and the marker stays.
func (s *sim) erase(ref UUID) {
	for i := range s.items {
		if r := s.items[i].row; r != nil && r.Ciphertext != nil {
			if h, _ := DecodeHeader(r.Ciphertext); h.Reference == ref {
				marker := *r
				marker.Ciphertext = nil
				s.items[i].row = &marker
				s.store(map[string]any{"erased": "messages.ciphertext", "seq": r.Seq, "because": "deleted by its author"})
			}
		}
	}
}

// join is what a device does once it is unlocked (section 7.2): it reads
// the backup, publishes a list with itself in it, checks the chat's chain
// of epoch records, and makes a new epoch that includes itself.
func (s *sim) join(dev *simDevice) {
	s.learn(dev)
	list, err := dev.d.ListWithSelf()
	must(err)
	s.publish(dev, list)
	must(dev.d.LearnLists(dev.d.Account, []SignedList{list}))
	s.sync(dev)
	s.readBackup(dev)
	if len(s.items) > 0 {
		s.makeEpoch(dev)
	}
}

// Transcript plays the whole conversation and returns its steps and the
// devices that take part.
func Transcript() (steps []TranscriptStep, devices []TranscriptDevice, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("ref: the transcript went wrong: %v\n%s", p, debug.Stack())
		}
	}()
	s := &sim{now: tStart, lists: map[UUID][]SignedList{}, backups: map[UUID]*simBackup{}}
	text := func(t string) []byte { return MessageContent(t, nil, nil, nil, nil) }

	// --- Setting up: each account's first device makes its backup.
	annPhone := s.device("ann-phone", tAnn, PlatformAndroid, "0193aa00-0000-7000-8000-0000000000a1")
	s.begin("Ann's phone is the first device of her account to enter a sealed chat: it makes the backup key, the account key, the recovery phrase and version 1 of her device list.", annPhone)
	annBundle, err := annPhone.d.CreateBackup()
	must(err)
	s.putBundle(annPhone, annBundle, "create")
	s.end()
	annPhrase := annBundle.Phrase

	bobPhone := s.device("bob-phone", tBob, PlatformIOS, "0193aa00-0000-7000-8000-0000000000b1")
	s.begin("Bob's phone does the same for his account.", bobPhone)
	bobBundle, err := bobPhone.d.CreateBackup()
	must(err)
	s.putBundle(bobPhone, bobBundle, "create")
	s.end()
	bobPhrase := bobBundle.Phrase

	// --- A second device each, before the chat exists.
	annLaptop := s.device("ann-laptop", tAnn, PlatformWeb, "0193aa00-0000-7000-8000-0000000000a2")
	s.begin("Ann's browser unlocks with the recovery phrase and publishes version 2 of her list with itself in it.", annLaptop)
	s.learn(annLaptop)
	must(annLaptop.d.UnlockWithPhrase(annPhrase, s.copyOf(tAnn, WayPhrase, UUID{}), s.stored(tAnn)))
	s.join(annLaptop)
	s.syncAll()
	s.end()

	bobDesktop := s.device("bob-desktop", tBob, PlatformMacOS, "0193aa00-0000-7000-8000-0000000000b2")
	s.begin("Bob's desktop app posts an unlock request and shows its code; Bob types the code into his phone, which checks it against the request it got through the server and answers with a locked copy for the desktop's box key. The desktop opens it and publishes version 2 of Bob's list.", bobDesktop)
	s.learn(bobDesktop)
	request, code, err := bobDesktop.d.NewUnlockRequest()
	must(err)
	answer, err := bobPhone.d.AnswerUnlockRequest(request, code)
	must(err)
	s.store(map[string]any{"table": "unlock_requests", "account_id": tBob.String(), "device_id": bobDesktop.d.ID.String(),
		"request": hx(request), "code_shown": code, "answer": hx(answer)})
	must(bobDesktop.d.UnlockWithDeviceCopy(answer, s.stored(tBob)))
	s.join(bobDesktop)
	s.syncAll()
	s.end()

	// --- Creation: epoch 1, sealed to all four devices.
	s.begin("Ann's phone starts the sealed chat: epoch record 1, sealed to every device on both lists.", annPhone)
	s.learn(annPhone)
	s.makeEpoch(annPhone)
	s.syncAll()
	s.end()

	// --- Messages from every device, and every kind of content.
	hello := s.say(annPhone, "Ann's phone sends a message.", text("Hello Bob"))
	s.say(bobPhone, "Bob's phone answers.", text("Hello Ann"))
	list := s.say(annLaptop, "Ann's browser sends a checklist.", text("- [ ] bring the keys\n- [ ] lock up"))
	typo := s.say(bobDesktop, "Bob's desktop sends a message with a mistake in it.", text("See you at sevn"))
	s.say(bobPhone, "Bob reacts to Ann's first message. A reaction is quiet.", ReactionContent(hello, "👍", true))
	s.say(bobDesktop, "Bob corrects his message. An edit is a new sealed message; devices show only the latest text.", EditContent(typo, "See you at seven", nil))
	s.say(annPhone, "Ann ticks a checklist item.", TaskContent(list, "t1", true))
	oops := s.say(annLaptop, "Ann sends something she will take back.", text("wrong chat, sorry"))
	s.begin("Ann deletes it: a quiet `delete` message, and the server erases the target row's ciphertext at her device's request.", annLaptop)
	s.send(annLaptop, DeleteContent(oops))
	s.erase(oops)
	s.syncAll()
	s.end()

	// --- A device is added, through a passkey.
	prf := Fixed("transcript passkey prf output", 32)
	credential := Fixed("transcript passkey credential id", 20)
	s.begin("Ann adds a passkey that supports PRF. The page that holds it gives the server its unlock public key, and Ann's phone, asked, approves it: a locked copy for the passkey and the way added to the account record, revision 2.", annPhone)
	unlockPublic := X25519Public(UnlockKeyFromPRF(prf, tAnn, credential))
	up, err := annPhone.d.ApprovePasskey(tPasskey, unlockPublic)
	must(err)
	ab := s.backups[tAnn]
	ab.revision, ab.record = up.Revision, up.AccountRecord
	ab.copies = append(ab.copies, up.Copy)
	annPhone.d.AdoptRecord(up)
	s.store(map[string]any{"passkey_id": tPasskey.String(), "credential_id": hx(credential), "prf_output": hx(prf), "unlock_public": hx(unlockPublic),
		"note": "not stored by the server except unlock_public; given here so the unlock key can be derived"})
	s.store(map[string]any{"table": "account_records", "account_id": tAnn.String(), "key_version": ab.keyVersion, "revision": up.Revision, "record": hx(up.AccountRecord)})
	s.storeCopies(tAnn, ab.keyVersion, []Copy{up.Copy})
	s.end()

	annTablet := s.device("ann-tablet", tAnn, PlatformAndroid, "0193aa00-0000-7000-8000-0000000000a3")
	s.begin("A device is added. Ann's tablet unlocks with the passkey's PRF output, reads the account's backup records, publishes version 3 of her list, checks the chain of epoch records, and makes epoch 2, which includes itself. It reads the messages of epoch 1 with the history secret from the backup. Bob's devices show that Ann added a device.", annTablet)
	s.learn(annTablet)
	must(annTablet.d.UnlockWithPRF(prf, credential, tPasskey, s.copyOf(tAnn, WayPasskey, tPasskey), s.stored(tAnn)))
	s.join(annTablet)
	s.syncAll()
	s.end()
	s.say(annTablet, "The tablet sends in epoch 2.", text("Hello from the tablet"))

	// --- A device is removed.
	s.begin("A device is removed. Bob signs out on his desktop: as its last act it publishes version 3 of his list without itself, then wipes its keys. Bob's phone makes epoch 3, sealed only to the devices still listed.", bobDesktop)
	without, err := bobDesktop.d.ListWithout(bobDesktop.d.ID)
	must(err)
	s.publish(bobDesktop, without)
	s.learn(bobPhone)
	s.makeEpoch(bobPhone)
	s.syncAll()
	s.end()
	afterRemoval := s.say(bobPhone, "Bob's phone sends in epoch 3. The desktop, were it to fetch this row with keys it had kept, could not read it: epoch 3's secret was never sealed to it.", text("Only my phone now"))
	// The removed device is kept in step for one check, then dropped: with
	// everything it ever held it still cannot read what followed.
	if m := bobDesktop.chat().Messages[afterRemoval]; m != nil {
		panic("transcript: a removed device read a later message")
	}
	if e := bobDesktop.chat().Epoch(3); e == nil || !e.Accepted || e.History != nil {
		panic("transcript: a removed device has a key for the epoch made without it")
	}
	bobDesktop.gone = true

	// --- An epoch made for age.
	s.now += 8 * tDay
	s.begin("Eight days pass. Ann's browser is about to send and finds the current epoch more than seven days old: it makes epoch 4 first.", annLaptop)
	if need, why := annLaptop.chat().NeedsEpoch(); !need || why != "age" {
		panic("transcript: the seven-day rule did not ask for an epoch")
	}
	s.makeEpoch(annLaptop)
	s.syncAll()
	s.end()
	s.say(annLaptop, "It sends in epoch 4.", text("A week later"))

	// --- Two devices make the same epoch at once.
	s.now += 8 * tDay
	s.begin("Eight more days. Ann's phone and Bob's phone both make epoch 5 at the same moment. The server stores the first and answers the second `conflict` with the winner; Bob's phone checks the winner, finds it covers what it wanted, and makes nothing more.", annPhone)
	annFive, _, err := annPhone.chat().MakeEpoch()
	must(err)
	bobFive, _, err := bobPhone.chat().MakeEpoch()
	must(err)
	s.postEpoch(annPhone, 5, annFive)
	s.postEpoch(bobPhone, 5, bobFive)
	s.syncAll()
	if need, _ := bobPhone.chat().NeedsEpoch(); need {
		panic("transcript: the loser of the race still wants an epoch")
	}
	s.end()
	s.say(bobPhone, "Bob's phone sends in epoch 5.", text("Still here"))

	// --- An account with no device for two epochs, then a history share.
	s.begin("Bob signs out on his phone too: version 4 of his list holds no device. Ann's phone makes epoch 6, sealed to Ann's devices only.", bobPhone)
	empty, err := bobPhone.d.ListWithout(bobPhone.d.ID)
	must(err)
	s.publish(bobPhone, empty)
	bobPhone.gone = true
	s.learn(annPhone)
	s.makeEpoch(annPhone)
	s.syncAll()
	s.end()
	s.say(annPhone, "Ann writes to Bob while he has no device.", text("Call me when you are back"))
	s.now += 8 * tDay
	s.begin("Eight days later Ann's browser makes epoch 7 for age. Bob still has no device.", annLaptop)
	s.makeEpoch(annLaptop)
	s.syncAll()
	s.end()
	s.say(annLaptop, "Ann writes again.", text("Any news?"))

	bobNew := s.device("bob-new-phone", tBob, PlatformAndroid, "0193aa00-0000-7000-8000-0000000000b3")
	s.begin("Bob signs in on a new phone and unlocks with his recovery phrase. It reads the backup (epochs 1 to 5), publishes version 5 of his list, and makes epoch 8. It cannot read epochs 6 and 7 yet: no device of his account was in them.", bobNew)
	s.learn(bobNew)
	must(bobNew.d.UnlockWithPhrase(bobPhrase, s.copyOf(tBob, WayPhrase, UUID{}), s.stored(tBob)))
	s.join(bobNew)
	s.syncAll()
	s.end()
	s.begin("Ann's phone accepted an epoch record in which Bob has a device again after two epochs in which he had none: it sends a `history` message with the history secrets of epochs 6 and 7. Bob's phone checks each against that epoch's record, can then read both epochs, and writes the secrets into his own backup.", annPhone)
	if !annPhone.chat().ShareDue {
		panic("transcript: no history share is due")
	}
	share, ok := annPhone.chat().HistoryShare()
	if !ok {
		panic("transcript: nothing to share")
	}
	s.send(annPhone, share)
	s.syncAll()
	s.reread(bobNew)
	s.upload(bobNew)
	s.end()
	s.say(bobNew, "Bob answers what he missed.", text("Back now, calling"))

	// --- A key replacement.
	s.begin("Ann's tablet is lost. From her phone she revokes it, and the phone replaces the backup key and the account key: a new account record, a locked copy for each way and each remaining device, every backup record locked again, and version 4 of her list signed by the new account key and, as prev_sig, by the old one. The server swaps them together. The phone then makes epoch 9 without the tablet.", annPhone)
	annTablet.gone = true
	replacement, err := annPhone.d.ReplaceBackup(Replacement{Devices: []UUID{annTablet.d.ID}}, s.backups[tAnn].records)
	must(err)
	s.putBundle(annPhone, replacement, "replace")
	s.makeEpoch(annPhone)
	// Ann's browser sees the key change, opens its device copy with its
	// box key, checks it, and carries on.
	s.learn(annLaptop)
	must(annLaptop.d.UnlockWithDeviceCopy(s.copyOf(tAnn, WayDevice, annLaptop.d.ID), s.stored(tAnn)))
	s.syncAll()
	s.end()
	s.say(annLaptop, "Ann's browser sends in epoch 9, with the new keys.", text("Lost my tablet; it is revoked"))

	// --- A reset.
	bobReset := s.device("bob-reset-phone", tBob, PlatformIOS, "0193aa00-0000-7000-8000-0000000000b4")
	s.begin("Bob loses his phone and his recovery phrase, and has no passkey that can unlock. On a new phone he starts again: a new backup key, account key and phrase with key version 2, and version 6 of his list, marked reset, holding only the new phone. The server deletes his old locked copies, account record and backup records. The phone makes epoch 10. Ann's devices show the reset notice. The new phone can read nothing from before.", bobReset)
	bobNew.gone = true
	s.learn(bobReset)
	reset, err := bobReset.d.ResetBackup()
	must(err)
	s.putBundle(bobReset, reset, "reset")
	s.sync(bobReset)
	s.makeEpoch(bobReset)
	s.syncAll()
	s.end()
	s.say(bobReset, "Bob writes from the reset account.", text("New phone. Lost everything, sorry"))
	s.say(annPhone, "Ann answers.", text("Compare codes with me when we meet"))

	// --- A refused record and its repair.
	s.begin("The server stores, as epoch 11, a record nobody's device made: it is signed by a key that is in no device list. Every device refuses it under rule 6, shows that the chat's keys are out of step, and draws nothing sent in it. The record keeps its place in the chain.", nil)
	intruder := SigningKey(Fixed("transcript intruder sign seed", 32))
	lists := [2]EpochList{{AccountID: tAnn, List: s.lists[tAnn][len(s.lists[tAnn])-1].List}, {AccountID: tBob, List: s.lists[tBob][len(s.lists[tBob])-1].List}}
	forged, err := MakeEpoch(NewEpoch{ConversationID: tConv, KeyEpoch: 11, Prev: EpochHash(s.lastEpoch()),
		MakerAccountID: tAnn, MakerDeviceID: annPhone.d.ID, MakerSign: intruder, Lists: lists,
		Secret:  Fixed("transcript intruder epoch secret", 32),
		OneTime: func(device UUID) []byte { return Fixed("transcript intruder one-time "+device.String(), 32) }, At: s.now})
	must(err)
	s.forceEpoch(11, forged)
	s.syncAll()
	s.end()
	s.begin("Ann's phone repairs the chat: it makes epoch 12 itself, with prev the hash of the refused record's stored bytes. Every device accepts it, whether or not it had refused the bad record.", annPhone)
	if need, why := annPhone.chat().NeedsEpoch(); !need || why != "refused" {
		panic("transcript: a refused record did not ask for a repair")
	}
	s.makeEpoch(annPhone)
	s.syncAll()
	s.end()
	s.say(bobReset, "The chat carries on in epoch 12.", text("Working again"))
	return s.steps, s.cast, nil
}
