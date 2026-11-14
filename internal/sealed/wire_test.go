package sealed

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// The server's reader is written apart from the reference client. These
// tests run it over the test vectors the reference wrote, so each is a
// check on the other.

func vector(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../testdata/sealed", name))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func unhex(t *testing.T, v any) []byte {
	t.Helper()
	b, err := hex.DecodeString(v.(string))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReaderOnDeviceListVectors(t *testing.T) {
	v := vector(t, "device_list.json")
	var prev *signedList
	for i, c := range v["chain"].([]any) {
		entry := c.(map[string]any)
		s, err := parseSignedList(unhex(t, entry["signed_list"]))
		if err != nil {
			t.Fatalf("version %d does not parse: %v", i+1, err)
		}
		if string(s.encode()) != string(unhex(t, entry["signed_list"])) {
			t.Fatalf("version %d does not encode back to itself", i+1)
		}
		l, err := checkListStep(prev, s)
		if err != nil {
			t.Fatalf("version %d of the chain was refused: %v", i+1, err)
		}
		fields := entry["fields"].(map[string]any)
		if string(l.AccountID) != fields["account_id"] || l.Server != fields["server"] || float64(l.Version) != fields["version"] ||
			float64(l.KeyVersion) != fields["key_version"] || hex.EncodeToString(l.AccountKey) != fields["account_key"] ||
			len(l.Devices) != len(fields["devices"].([]any)) || l.Reset != (fields["reset"] == float64(1)) {
			t.Fatalf("version %d read as %+v, want %v", i+1, l, fields)
		}
		if h := hash(labelListHash, s.List); hex.EncodeToString(h[:]) != entry["hash"] {
			t.Fatalf("version %d: hash", i+1)
		}
		prev = &s
	}
	// Every refusal of section 4.4 is refused by the server too.
	for _, c := range v["refusals"].([]any) {
		entry := c.(map[string]any)
		pinned := signedList{List: unhex(t, entry["pinned_list"])}
		next, err := parseSignedList(unhex(t, entry["signed_list"]))
		if err != nil {
			continue // refused at the encoding
		}
		if _, err := checkListStep(&pinned, next); err == nil {
			t.Errorf("accepted a device list that must be refused: %s", entry["why"])
		}
	}
	for _, c := range vector(t, "encoding.json")["refusals"].([]any) {
		entry := c.(map[string]any)
		raw := unhex(t, entry["bytes"])
		var err error
		if entry["record"] == "device_list" {
			_, err = parseDeviceList(raw)
		} else {
			_, err = parseUnlockRequest(raw)
		}
		if err == nil {
			t.Errorf("accepted a record that must be refused: %s", entry["why"])
		}
	}
	for _, c := range vector(t, "encoding.json")["accepted"].([]any) {
		entry := c.(map[string]any)
		raw := unhex(t, entry["bytes"])
		var err error
		if entry["record"] == "device_list" {
			_, err = parseDeviceList(raw)
		} else {
			_, err = parseUnlockRequest(raw)
		}
		if err != nil {
			t.Errorf("refused a %s that is in its one valid form: %v", entry["record"], err)
		}
	}
}

func TestReaderOnEpochAndMessageVectors(t *testing.T) {
	v := vector(t, "epoch_record.json")
	lists := map[string][]byte{}
	for _, c := range v["lists_checked"].([]any) {
		entry := c.(map[string]any)
		lists[entry["account_id"].(string)+"/"+jsonNumber(entry["version"])] = unhex(t, entry["list"])
	}
	for _, name := range []string{"epoch_1", "epoch_2"} {
		entry := v[name].(map[string]any)
		stored := unhex(t, entry["stored"])
		e, err := parseEpoch(stored)
		if err != nil {
			t.Fatalf("%s does not parse: %v", name, err)
		}
		fields := entry["fields"].(map[string]any)
		if string(e.ConversationID) != fields["conversation_id"] || float64(e.KeyEpoch) != fields["key_epoch"] ||
			string(e.MakerDeviceID) != fields["maker_device_id"] || string(e.MakerAccountID) != fields["maker_account_id"] ||
			len(e.Recipients) != len(fields["recipients"].([]any)) || hex.EncodeToString(e.Prev[:]) != fields["prev"] {
			t.Fatalf("%s read as %+v", name, e)
		}
		if h := epochHash(stored); hex.EncodeToString(h[:]) != entry["hash"] {
			t.Fatalf("%s: hash", name)
		}
		// The maker's signature verifies under its key from the list the
		// record names, and each list hash is the hash of that version.
		var makerKey []byte
		for _, ref := range e.Accounts {
			raw := lists[string(ref.AccountID)+"/"+jsonNumber(float64(ref.ListVersion))]
			if hash(labelListHash, raw) != ref.ListHash {
				t.Fatalf("%s: list hash of %s", name, ref.AccountID)
			}
			l, err := parseDeviceList(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range l.Devices {
				if d.ID == e.MakerDeviceID {
					makerKey = d.SignKey
				}
			}
		}
		if !verify(makerKey, labelEpoch, e.Record, e.Sig) {
			t.Fatalf("%s: the maker's signature does not verify", name)
		}
	}
	m := vector(t, "message.json")
	for _, c := range m["messages"].([]any) {
		entry := c.(map[string]any)
		h, err := parseHeader(unhex(t, entry["sealed"]))
		if err != nil {
			t.Fatalf("a %s message does not parse: %v", entry["kind"], err)
		}
		want := entry["header"].(map[string]any)
		if string(h.ConversationID) != want["conversation_id"] || string(h.AccountID) != want["account_id"] || string(h.DeviceID) != want["device_id"] ||
			h.Reference != want["reference"] || float64(h.KeyEpoch) != want["key_epoch"] || h.Quiet != entry["quiet"] {
			t.Fatalf("a %s message's header read as %+v, want %v", entry["kind"], h, want)
		}
	}
	// Too short, an unknown format, a flag that does not exist.
	good := unhex(t, m["messages"].([]any)[0].(map[string]any)["sealed"])
	for name, change := range map[string]func(b []byte) []byte{
		"too short":             func(b []byte) []byte { return b[:minMessageSize-1] },
		"another format":        func(b []byte) []byte { b[0] = 2; return b },
		"a flag that is unused": func(b []byte) []byte { b[1] = 2; return b },
	} {
		if _, err := parseHeader(change(append([]byte{}, good...))); err == nil {
			t.Errorf("parseHeader accepted a message with %s", name)
		}
	}
	// The attach signature of section 4.1.
	u := vector(t, "unlock_request.json")
	req, err := parseUnlockRequest(unhex(t, u["request"]))
	if err != nil || string(req.AccountID) != u["fields"].(map[string]any)["account_id"] {
		t.Fatalf("the unlock request: %+v %v", req, err)
	}
	for _, c := range u["refusals"].([]any) {
		if _, err := parseUnlockRequest(unhex(t, c.(map[string]any)["request"])); err == nil {
			t.Error("accepted an unlock request whose proof does not verify")
		}
	}
	if isUUIDv7("0193a0e0-0000-4000-8000-0000000000c0") || !isUUIDv7(string(model.NewID[model.ConversationID]())) || isUUIDv7("nope") {
		t.Error("isUUIDv7")
	}
}

func jsonNumber(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
