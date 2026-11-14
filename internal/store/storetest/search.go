package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("search", testSearch) }

func testSearch(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	pub := newChannel(t, s, "pub", false, ann.ID)
	priv := newChannel(t, s, "priv", true, ann.ID)

	deploy := post(t, s, pub.ID, ann.ID, "", "Deploy plan for Friday")
	post(t, s, pub.ID, ann.ID, "", "the plan to deploy on staging")
	secret := post(t, s, priv.ID, ann.ID, "", "zebra deploy secret")
	withFile := post(t, s, pub.ID, bob.ID, "", "deployment notes attached")
	f := filesNew(t, s, pub.ID, bob.ID, "notes.txt")
	must(t, s.Files().Attach(ctx, withFile.ID, pub.ID, bob.ID, []model.FileID{f.ID}))

	annScope := store.Scope{AccountID: ann.ID, PublicChannels: true}
	bobScope := store.Scope{AccountID: bob.ID, PublicChannels: true}
	guestScope := store.Scope{AccountID: bob.ID} // reads only what it is in
	find := func(scope store.Scope, q store.SearchQuery) []model.Message {
		t.Helper()
		q.Scope = scope
		msgs, _, err := s.Search().Messages(ctx, q, store.Page{})
		must(t, err)
		return msgs
	}
	word := func(w string) store.SearchQuery { return store.SearchQuery{Terms: []store.SearchTerm{{Text: w}}} }

	eq(t, "a word, without regard to case", len(find(annScope, word("DEPLOY"))), 3)
	eq(t, "both words, any order", len(find(annScope, store.SearchQuery{Terms: []store.SearchTerm{{Text: "plan"}, {Text: "deploy"}}})), 2)
	eq(t, "a phrase", len(find(annScope, store.SearchQuery{Terms: []store.SearchTerm{{Text: "deploy plan", Phrase: true}}})), 1)
	eq(t, "a prefix", len(find(annScope, store.SearchQuery{Terms: []store.SearchTerm{{Text: "depl", Prefix: true}}})), 4)
	eq(t, "an exclusion", len(find(annScope, store.SearchQuery{Terms: []store.SearchTerm{{Text: "deploy"}, {Text: "staging", Exclude: true}}})), 2)
	newest := find(annScope, word("deploy"))
	eq(t, "newest first", newest[0].ID, secret.ID)

	// PERMISSIONS cases 3, 11 and 15.
	eq(t, "a non-member finds nothing in a private channel", len(find(bobScope, word("zebra"))), 0)
	eq(t, "a member reads a public channel without joining", len(find(bobScope, word("friday"))), 1)
	eq(t, "a guest or bot searches only what it is in", len(find(guestScope, word("friday"))), 0)

	eq(t, "in", len(find(annScope, store.SearchQuery{Terms: word("deploy").Terms, In: priv.ID})), 1)
	eq(t, "in a conversation the asker cannot read", len(find(bobScope, store.SearchQuery{Terms: word("deploy").Terms, In: priv.ID})), 0)
	eq(t, "from", len(find(annScope, store.SearchQuery{From: bob.ID})), 1)
	eq(t, "has_file", len(find(annScope, store.SearchQuery{HasFile: true})), 1)
	eq(t, "since", len(find(annScope, store.SearchQuery{Terms: word("deploy").Terms, Since: model.Now().Add(time.Hour)})), 0)
	eq(t, "until", len(find(annScope, store.SearchQuery{Terms: word("deploy").Terms, Until: model.Now().Add(time.Hour)})), 3)
	eq(t, "nothing asked, nothing found", len(find(annScope, store.SearchQuery{})), 0)

	// Typed text is never query syntax.
	for _, odd := range []string{`"`, `deploy" OR "x`, `*`, `-`, `a:b`, `(deploy`, `deploy)`, `'; DROP TABLE messages; --`, `NEAR(deploy plan)`, `💥`} {
		find(annScope, word(odd))
		find(annScope, store.SearchQuery{Terms: []store.SearchTerm{{Text: odd, Prefix: true}, {Text: odd, Phrase: true}, {Text: odd, Exclude: true}}})
	}
	eq(t, "the table is still there", len(find(annScope, word("deploy"))), 3)

	// Deleted and edited-away text is not found.
	_, err := s.Messages().Delete(ctx, deploy.ID, ann.ID, model.Now())
	must(t, err)
	eq(t, "deleted", len(find(annScope, word("friday"))), 0)
	_, err = s.Messages().Edit(ctx, store.MessageEdit{ID: secret.ID, Text: "nothing to see", KeepVersion: true, By: ann.ID, At: model.Now()})
	must(t, err)
	eq(t, "edited away", len(find(annScope, word("zebra"))), 0)
	eq(t, "the new text is found", len(find(annScope, word("nothing"))), 1)

	// Paging.
	for i := 0; i < 5; i++ {
		post(t, s, pub.ID, ann.ID, "", "pagination fodder")
	}
	q := word("fodder")
	q.Scope = annScope
	page1, next, err := s.Search().Messages(ctx, q, store.Page{Limit: 3})
	must(t, err)
	eq(t, "first page", len(page1) == 3 && next != "", true)
	page2, next, err := s.Search().Messages(ctx, q, store.Page{Limit: 3, Cursor: next})
	must(t, err)
	eq(t, "second page", len(page2) == 2 && next == "", true)
	eq(t, "no overlap", page1[2].ID != page2[0].ID, true)
	_, _, err = s.Search().Messages(ctx, q, store.Page{Cursor: "junk"})
	wantErr(t, err, model.Invalid("cursor", ""))
}
