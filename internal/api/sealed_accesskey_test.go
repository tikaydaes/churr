package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

func accessKeyCopy(t *testing.T, c *testkit.Client) map[string]any {
	t.Helper()
	for _, v := range c.Must(200, "GET", "/api/v1/me/backup", nil)["backup"].(map[string]any)["locked_copies"].([]any) {
		if cp := v.(map[string]any); cp["kind"] == "access_key" {
			return cp
		}
	}
	return nil
}

// TestSealedAccessKey: the access key unlocks sealed chats (ruling R85,
// docs/SEALED.md section 13.3). An account with only an access key sets up
// sealed chats; its key gets a locked copy at once; a new device that
// signs in with the key unlocks with it alone and reads earlier messages.
// Replacing the key drops the old copy and marks the keys for replacement;
// so does removing it.
func TestSealedAccessKey(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.MemberWithAccessKey("ann")
	bob := ts.Member("bob")
	st := ann.Must(200, "GET", "/api/v1/me/access-key", nil)["access_key"].(map[string]any)
	if st["unlock_public"] != ann.AccessKeyUnlockPublic() {
		t.Fatalf("the unlock key given at registration is kept: %v", st)
	}

	first := ann.Sealed("web")
	first.CreateBackup()
	if cp := accessKeyCopy(t, ann); cp == nil || cp["passkey_id"] != nil || cp["device_id"] != nil {
		t.Fatalf("setting up made no access key copy: %v", cp)
	}
	bobPhone := bob.Sealed("android")
	bobPhone.CreateBackup()
	chat := first.Open(bob.ID)
	chat.Say("before the new device")
	bobChat := bobPhone.Chat(chat.ID, ann.ID)
	bobChat.Say("a reply")

	// A new browser: signed in with the key, unlocked with the key.
	second := ts.Browser()
	second.UseAccessKeyOf(ann)
	if st, out := second.AccessKeyLogin("ann"); st != 200 {
		t.Fatalf("signing in with the key = %d %v", st, out)
	}
	second.ID, second.Username = ann.ID, ann.Username
	dev := second.Sealed("web")
	if err := dev.UnlockWithAccessKey(); err != nil {
		t.Fatal(err)
	}
	dev.Join()
	c2 := dev.Chat(chat.ID, bob.ID)
	c2.Sync()
	c2.ReadBackup()
	wantTexts(t, "the new browser", c2, "before the new device", "a reply")
	c2.Say("from the new browser")
	wantTexts(t, "bob", bobChat, "before the new device", "a reply", "from the new browser")

	// Only an access key copy for an account whose key has an unlock key.
	bad := map[string]any{"kind": "access_key", "key_version": 1, "locked_key": model.EncodeBytes(make([]byte, 144)), "device_id": dev.ID}
	second.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/me/backup/copies", map[string]any{"locked_copy": bad})

	// A new key: the old copy goes, and the keys are due for replacement.
	second.Must(200, "PUT", "/api/v1/me/access-key", map[string]any{"access_key_auth": second.NewAccessKey(), "unlock_public": second.AccessKeyUnlockPublic()})
	if cp := accessKeyCopy(t, second); cp != nil {
		t.Fatalf("the old key's copy is still there: %v", cp)
	}
	b := second.Must(200, "GET", "/api/v1/me/backup", nil)["backup"].(map[string]any)
	if b["replacement_due"] != true {
		t.Fatalf("replacing a key that unlocked does not mark the keys: %v", b)
	}
	if got := second.Must(200, "GET", "/api/v1/me/access-key", nil)["access_key"].(map[string]any); got["unlock_public"] != second.AccessKeyUnlockPublic() {
		t.Fatalf("the new key's unlock key: %v", got)
	}
	second.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/access-key", map[string]any{"access_key_auth": second.AccessKey(), "unlock_public": "short"})

	// The unlocked device lets the new key unlock.
	up, err := dev.D.ApproveAccessKey(testkit.Unb64(t, second.AccessKeyUnlockPublic()))
	if err != nil {
		t.Fatal(err)
	}
	second.Must(201, "POST", "/api/v1/me/backup/copies", map[string]any{"locked_copy": testkit.CopyBody(up.Copy, dev.D.KeyVersion),
		"account_record": map[string]any{"key_version": dev.D.KeyVersion, "revision": up.Revision, "record": model.EncodeBytes(up.AccountRecord)}})
	dev.D.AdoptRecord(up)
	third := ts.Browser()
	third.UseAccessKeyOf(second)
	if st, _ := third.AccessKeyLogin("ann"); st != 200 {
		t.Fatalf("signing in with the new key = %d", st)
	}
	third.ID, third.Username = ann.ID, ann.Username
	if err := third.Sealed("linux").UnlockWithAccessKey(); err != nil {
		t.Fatalf("the new key does not unlock: %v", err)
	}

	// Removing the key (with a passkey to keep a way in) drops its copy.
	begin := second.Must(200, "POST", "/api/v1/me/passkeys/begin", nil)
	second.Must(201, "POST", "/api/v1/me/passkeys/finish", second.CreatePasskey(begin))
	if st, _ := second.Login(); st != 200 {
		t.Fatal("the passkey signs in")
	}
	second.Must(204, "DELETE", "/api/v1/me/access-key", nil)
	if cp := accessKeyCopy(t, second); cp != nil {
		t.Fatalf("a removed key's copy is still there: %v", cp)
	}
}
