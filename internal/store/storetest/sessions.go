package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("sessions", testSessions) }

// sessionsIDs returns the ids of a list of sessions, in order.
func sessionsIDs(list []model.Session) []model.SessionID {
	out := make([]model.SessionID, len(list))
	for i, s := range list {
		out[i] = s.ID
	}
	return out
}

// sessionsSameIDs fails the test unless got is exactly want, in order.
func sessionsSameIDs(t testing.TB, what string, got []model.SessionID, want ...model.SessionID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", what, got, want)
		}
	}
}

func testSessions(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	base := model.Now()
	hour := time.Hour

	// web makes a plain cookie session for an account.
	web := func(t *testing.T, account model.AccountID, token string, createdAt, expiresAt model.Time) model.Session {
		t.Helper()
		ses := model.Session{AccountID: account, Kind: model.SessionWeb, Label: token, SignedInBy: "passkey", CreatedAt: createdAt, ExpiresAt: expiresAt}
		must(t, s.Sessions().Create(ctx, &ses, []byte(token), nil))
		return ses
	}

	t.Run("create fills in defaults and get returns every column", func(t *testing.T) {
		ses := model.Session{AccountID: ann.ID, Kind: model.SessionPersonal}
		before := model.Now()
		must(t, s.Sessions().Create(ctx, &ses, []byte("tok-defaults"), nil))
		eq(t, "id filled", model.ValidID(string(ses.ID)), true)
		eq(t, "created_at filled", !ses.CreatedAt.Before(before), true)
		got, err := s.Sessions().Get(ctx, ses.ID)
		must(t, err)
		eq(t, "a bare session reads back as it was made", got, ses)

		full := model.Session{
			AccountID: ann.ID, Kind: model.SessionApp, Label: "Churr on Pixel", SignedInBy: "oidc", SecondFactor: "totp",
			PendingSecondFactor: true, CreatedAt: base, LastSeenAt: base.Add(time.Minute), ExpiresAt: base.Add(24 * hour),
			AccessExpiresAt: base.Add(hour), MaxExpiresAt: base.Add(24 * hour), FreshAt: base,
		}
		must(t, s.Sessions().Create(ctx, &full, []byte("tok-full"), []byte("ref-full")))
		got, err = s.Sessions().Get(ctx, full.ID)
		must(t, err)
		eq(t, "every column", got, full)
		eq(t, "not a recovery session", got.Recovery, false)
		eq(t, "current is the service's to set", got.Current, false)

		_, err = s.Sessions().Get(ctx, model.NewID[model.SessionID]())
		wantErr(t, err, model.ErrNotFound)

		// A session cannot be made without a token to find it by.
		bad := model.Session{AccountID: ann.ID, Kind: model.SessionWeb}
		if err := s.Sessions().Create(ctx, &bad, nil, nil); err == nil {
			t.Fatal("a session with no token hash was stored")
		}
	})

	t.Run("recovery is derived from how the session signed in", func(t *testing.T) {
		for by, want := range map[string]bool{"recovery_code": true, "recovery_link": true, "passkey": false, "saml": false, "pairing": false, "": false} {
			ses := model.Session{AccountID: bob.ID, Kind: model.SessionWeb, SignedInBy: by}
			must(t, s.Sessions().Create(ctx, &ses, []byte("tok-recovery-"+by), nil))
			eq(t, "set on create for "+by, ses.Recovery, want)
			got, err := s.Sessions().Get(ctx, ses.ID)
			must(t, err)
			eq(t, "read back for "+by, got.Recovery, want)
			eq(t, "signed_in_by "+by, got.SignedInBy, by)
		}
	})

	t.Run("by token hash returns the row whatever its state", func(t *testing.T) {
		ses := web(t, bob.ID, "tok-find", base, base.Add(-hour)) // already expired
		must(t, s.Sessions().Revoke(ctx, ses.ID, base))
		got, err := s.Sessions().ByTokenHash(ctx, []byte("tok-find"))
		must(t, err)
		eq(t, "found", got.ID, ses.ID)
		eq(t, "revoked_at is there for the caller to check", got.RevokedAt, base)
		eq(t, "expires_at is there for the caller to check", got.ExpiresAt, base.Add(-hour))
		_, err = s.Sessions().ByTokenHash(ctx, []byte("tok-unknown"))
		wantErr(t, err, model.ErrNotFound)
		_, err = s.Sessions().ByTokenHash(ctx, nil)
		wantErr(t, err, model.ErrNotFound)
		// A refresh token is not an access token.
		app := model.Session{AccountID: bob.ID, Kind: model.SessionApp}
		must(t, s.Sessions().Create(ctx, &app, []byte("tok-app-find"), []byte("ref-app-find")))
		_, err = s.Sessions().ByTokenHash(ctx, []byte("ref-app-find"))
		wantErr(t, err, model.ErrNotFound)
	})

	t.Run("rotate keeps the replaced refresh token as the previous one", func(t *testing.T) {
		app := model.Session{AccountID: ann.ID, Kind: model.SessionApp, SignedInBy: "passkey", CreatedAt: base, AccessExpiresAt: base.Add(hour)}
		must(t, s.Sessions().Create(ctx, &app, []byte("acc-1"), []byte("ref-1")))

		got, previous, err := s.Sessions().ByRefreshHash(ctx, []byte("ref-1"))
		must(t, err)
		eq(t, "found by the current token", got.ID, app.ID)
		eq(t, "it is the current one", previous, false)
		eq(t, "never rotated", got.RotatedAt, model.Time(0))

		at := base.Add(30 * time.Minute)
		must(t, s.Sessions().Rotate(ctx, app.ID, []byte("acc-2"), []byte("ref-2"), at.Add(hour), at))
		got, previous, err = s.Sessions().ByRefreshHash(ctx, []byte("ref-2"))
		must(t, err)
		eq(t, "new token is current", previous, false)
		eq(t, "rotated_at", got.RotatedAt, at)
		eq(t, "access expiry", got.AccessExpiresAt, at.Add(hour))
		got, previous, err = s.Sessions().ByRefreshHash(ctx, []byte("ref-1"))
		must(t, err)
		eq(t, "old token finds the session", got.ID, app.ID)
		eq(t, "and is reported as the previous one", previous, true)
		eq(t, "with the time to apply the tolerance against", got.RotatedAt, at)

		// The access token changed with it.
		_, err = s.Sessions().ByTokenHash(ctx, []byte("acc-1"))
		wantErr(t, err, model.ErrNotFound)
		byToken, err := s.Sessions().ByTokenHash(ctx, []byte("acc-2"))
		must(t, err)
		eq(t, "new access token", byToken.ID, app.ID)

		// A second rotation forgets the token before last.
		at2 := at.Add(hour)
		must(t, s.Sessions().Rotate(ctx, app.ID, []byte("acc-3"), []byte("ref-3"), at2.Add(hour), at2))
		_, _, err = s.Sessions().ByRefreshHash(ctx, []byte("ref-1"))
		wantErr(t, err, model.ErrNotFound)
		_, previous, err = s.Sessions().ByRefreshHash(ctx, []byte("ref-2"))
		must(t, err)
		eq(t, "ref-2 is now the previous one", previous, true)

		_, _, err = s.Sessions().ByRefreshHash(ctx, []byte("ref-unknown"))
		wantErr(t, err, model.ErrNotFound)
		_, _, err = s.Sessions().ByRefreshHash(ctx, nil)
		wantErr(t, err, model.ErrNotFound)
		wantErr(t, s.Sessions().Rotate(ctx, model.NewID[model.SessionID](), []byte("a"), []byte("r"), at, at), model.ErrNotFound)

		// The way a refresh really runs: look up and rotate together.
		must(t, s.Tx(ctx, func(tx store.Tx) error {
			cur, previous, err := tx.Sessions().ByRefreshHash(ctx, []byte("ref-3"))
			if err != nil {
				return err
			}
			eq(t, "current inside a transaction", previous, false)
			if _, previous, err = tx.Sessions().ByRefreshHash(ctx, []byte("ref-2")); err != nil {
				return err
			}
			eq(t, "previous inside a transaction", previous, true)
			return tx.Sessions().Rotate(ctx, cur.ID, []byte("acc-4"), []byte("ref-4"), at2.Add(2*hour), at2.Add(hour))
		}))
		_, previous, err = s.Sessions().ByRefreshHash(ctx, []byte("ref-3"))
		must(t, err)
		eq(t, "rotated in the transaction", previous, true)
	})

	t.Run("for account lists live sessions newest first", func(t *testing.T) {
		cat := newAccount(t, s, "cat", model.RoleMember)
		now := base
		oldest := web(t, cat.ID, "cat-1", now.Add(-3*hour), now.Add(hour))
		expired := web(t, cat.ID, "cat-2", now.Add(-2*hour), now.Add(-time.Minute))
		revoked := web(t, cat.ID, "cat-3", now.Add(-2*hour), now.Add(hour))
		must(t, s.Sessions().Revoke(ctx, revoked.ID, now.Add(-time.Minute)))
		forever := model.Session{AccountID: cat.ID, Kind: model.SessionBot, CreatedAt: now.Add(-hour)}
		must(t, s.Sessions().Create(ctx, &forever, []byte("cat-4"), nil))
		newest := web(t, cat.ID, "cat-5", now.Add(-time.Minute), now.Add(hour))
		endsNow := web(t, cat.ID, "cat-6", now.Add(-4*hour), now)
		web(t, bob.ID, "not-cats", now, now.Add(hour))

		list, err := s.Sessions().ForAccount(ctx, cat.ID, now)
		must(t, err)
		sessionsSameIDs(t, "live, newest first", sessionsIDs(list), newest.ID, forever.ID, oldest.ID)
		_, _ = expired, endsNow // both are left out: one ended a minute ago, one ends this instant
		// Later on the ones that had an end are gone too.
		list, err = s.Sessions().ForAccount(ctx, cat.ID, now.Add(2*hour))
		must(t, err)
		sessionsSameIDs(t, "two hours on", sessionsIDs(list), forever.ID)
	})

	t.Run("touch records use and never passes the hard end", func(t *testing.T) {
		sliding := web(t, bob.ID, "touch-sliding", base, base.Add(hour))
		must(t, s.Sessions().Touch(ctx, sliding.ID, base.Add(time.Minute), 0))
		got, err := s.Sessions().Get(ctx, sliding.ID)
		must(t, err)
		eq(t, "last_seen_at", got.LastSeenAt, base.Add(time.Minute))
		eq(t, "zero leaves expires_at", got.ExpiresAt, base.Add(hour))

		must(t, s.Sessions().Touch(ctx, sliding.ID, base.Add(2*time.Minute), base.Add(48*hour)))
		got, err = s.Sessions().Get(ctx, sliding.ID)
		must(t, err)
		eq(t, "last_seen_at moved", got.LastSeenAt, base.Add(2*time.Minute))
		eq(t, "a sliding session moves ahead", got.ExpiresAt, base.Add(48*hour))

		capped := model.Session{AccountID: bob.ID, Kind: model.SessionWeb, SignedInBy: "oidc", CreatedAt: base, ExpiresAt: base.Add(hour), MaxExpiresAt: base.Add(24 * hour)}
		must(t, s.Sessions().Create(ctx, &capped, []byte("touch-capped"), nil))
		must(t, s.Sessions().Touch(ctx, capped.ID, base.Add(time.Minute), base.Add(12*hour)))
		got, err = s.Sessions().Get(ctx, capped.ID)
		must(t, err)
		eq(t, "below the hard end it moves", got.ExpiresAt, base.Add(12*hour))
		must(t, s.Sessions().Touch(ctx, capped.ID, base.Add(time.Minute), base.Add(30*24*hour)))
		got, err = s.Sessions().Get(ctx, capped.ID)
		must(t, err)
		eq(t, "it stops at the hard end", got.ExpiresAt, base.Add(24*hour))
		eq(t, "the hard end itself never moves", got.MaxExpiresAt, base.Add(24*hour))

		wantErr(t, s.Sessions().Touch(ctx, model.NewID[model.SessionID](), base, 0), model.ErrNotFound)
		wantErr(t, s.Sessions().Touch(ctx, model.NewID[model.SessionID](), base, base.Add(hour)), model.ErrNotFound)
	})

	t.Run("set fresh", func(t *testing.T) {
		ses := web(t, bob.ID, "fresh", base, base.Add(hour))
		got, err := s.Sessions().Get(ctx, ses.ID)
		must(t, err)
		eq(t, "not set by the store", got.FreshAt, model.Time(0))
		must(t, s.Sessions().SetFresh(ctx, ses.ID, base.Add(time.Minute)))
		got, err = s.Sessions().Get(ctx, ses.ID)
		must(t, err)
		eq(t, "fresh_at", got.FreshAt, base.Add(time.Minute))
		wantErr(t, s.Sessions().SetFresh(ctx, model.NewID[model.SessionID](), base), model.ErrNotFound)
	})

	t.Run("revoke ends a session once", func(t *testing.T) {
		ses := web(t, bob.ID, "revoke-me", base, base.Add(hour))
		must(t, s.Sessions().Revoke(ctx, ses.ID, base.Add(time.Minute)))
		got, err := s.Sessions().Get(ctx, ses.ID)
		must(t, err)
		eq(t, "revoked_at", got.RevokedAt, base.Add(time.Minute))
		must(t, s.Sessions().Revoke(ctx, ses.ID, base.Add(time.Hour)))
		got, err = s.Sessions().Get(ctx, ses.ID)
		must(t, err)
		eq(t, "revoking again changes nothing", got.RevokedAt, base.Add(time.Minute))
		wantErr(t, s.Sessions().Revoke(ctx, model.NewID[model.SessionID](), base), model.ErrNotFound)
	})

	t.Run("revoke for account ends the live ones and says which", func(t *testing.T) {
		dee := newAccount(t, s, "dee", model.RoleMember)
		eve := newAccount(t, s, "eve", model.RoleMember)
		now := base
		keep := web(t, dee.ID, "dee-keep", now.Add(-hour), now.Add(hour))
		a := web(t, dee.ID, "dee-a", now.Add(-hour), now.Add(hour))
		token := model.Session{AccountID: dee.ID, Kind: model.SessionPersonal, CreatedAt: now.Add(-hour)}
		must(t, s.Sessions().Create(ctx, &token, []byte("dee-token"), nil))
		already := web(t, dee.ID, "dee-already", now.Add(-hour), now.Add(hour))
		must(t, s.Sessions().Revoke(ctx, already.ID, now.Add(-time.Minute)))
		expired := web(t, dee.ID, "dee-expired", now.Add(-2*hour), now.Add(-hour))
		others := web(t, eve.ID, "eve-a", now.Add(-hour), now.Add(hour))

		ended, err := s.Sessions().RevokeForAccount(ctx, dee.ID, keep.ID, now)
		must(t, err)
		got := map[model.SessionID]bool{}
		for _, id := range ended {
			got[id] = true
		}
		eq(t, "how many ended", len(ended), 2)
		eq(t, "the web session", got[a.ID], true)
		eq(t, "the personal token", got[token.ID], true)

		for _, c := range []struct {
			what string
			id   model.SessionID
			want model.Time
		}{
			{"the excepted one is live", keep.ID, 0},
			{"ended now", a.ID, now},
			{"the token ended now", token.ID, now},
			{"one ended before keeps its time", already.ID, now.Add(-time.Minute)},
			{"one that had run out is left alone", expired.ID, 0},
			{"another account's is untouched", others.ID, 0},
		} {
			ses, err := s.Sessions().Get(ctx, c.id)
			must(t, err)
			eq(t, c.what, ses.RevokedAt, c.want)
		}

		// With no exception the last one goes too.
		ended, err = s.Sessions().RevokeForAccount(ctx, dee.ID, "", now.Add(time.Minute))
		must(t, err)
		sessionsSameIDs(t, "sign out everywhere", ended, keep.ID)
		ended, err = s.Sessions().RevokeForAccount(ctx, dee.ID, "", now.Add(time.Minute))
		must(t, err)
		eq(t, "nothing left to end", len(ended), 0)
		list, err := s.Sessions().ForAccount(ctx, dee.ID, now)
		must(t, err)
		eq(t, "none live", len(list), 0)
	})

	t.Run("delete ended removes what expired or was revoked before the moment", func(t *testing.T) {
		fay := newAccount(t, s, "fay", model.RoleMember)
		now := base
		day := 24 * hour
		// Clear out what earlier subtests left, so the count below is
		// this subtest's own.
		_, err := s.Sessions().DeleteEnded(ctx, now.Add(-20*day))
		must(t, err)

		live := web(t, fay.ID, "fay-live", now.Add(-40*day), now.Add(day))
		forever := model.Session{AccountID: fay.ID, Kind: model.SessionBot, CreatedAt: now.Add(-400 * day)}
		must(t, s.Sessions().Create(ctx, &forever, []byte("fay-forever"), nil))
		expiredLong := web(t, fay.ID, "fay-expired-long", now.Add(-90*day), now.Add(-31*day))
		expiredLately := web(t, fay.ID, "fay-expired-lately", now.Add(-40*day), now.Add(-29*day))
		revokedLong := web(t, fay.ID, "fay-revoked-long", now.Add(-90*day), now.Add(day))
		must(t, s.Sessions().Revoke(ctx, revokedLong.ID, now.Add(-31*day)))
		revokedLately := web(t, fay.ID, "fay-revoked-lately", now.Add(-90*day), now.Add(day))
		must(t, s.Sessions().Revoke(ctx, revokedLately.ID, now.Add(-29*day)))

		n, err := s.Sessions().DeleteEnded(ctx, now.Add(-30*day))
		must(t, err)
		eq(t, "deleted", n, int64(2))
		for _, gone := range []model.SessionID{expiredLong.ID, revokedLong.ID} {
			_, err := s.Sessions().Get(ctx, gone)
			wantErr(t, err, model.ErrNotFound)
		}
		for _, kept := range []model.SessionID{live.ID, forever.ID, expiredLately.ID, revokedLately.ID} {
			_, err := s.Sessions().Get(ctx, kept)
			must(t, err)
		}
		n, err = s.Sessions().DeleteEnded(ctx, now.Add(-30*day))
		must(t, err)
		eq(t, "nothing more to delete", n, int64(0))
	})
}
