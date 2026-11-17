package api

import (
	"context"
	"encoding/base64"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/version"
)

func init() {
	register(func(a *API) {
		a.handle("getServer", a.getServer)
		a.handle("getWellKnown", a.getWellKnown)
	})
}

// getServer says what the server is and which features are on. The apps
// read it before sign-in.
func (a *API) getServer(r *Request) error {
	server, err := a.serverObject(r.Ctx(), r.SignedIn)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"server": server})
}

// serverObject builds the Server object: for getServer, and for the
// server.updated event sent when a setting that shows in it changes.
// signedIn adds what only a signed-in caller is told.
func (a *API) serverObject(ctx context.Context, signedIn bool) (map[string]any, error) {
	s := a.Store
	need, err := a.Auth.SetupNeeded(ctx)
	if err != nil {
		return nil, err
	}
	providers, err := a.Auth.ProviderSummaries(ctx)
	if err != nil {
		return nil, err
	}
	str := func(key, def string) string { v, _ := store.SettingString(ctx, s, key, def); return v }
	flag := func(key string, def bool) bool { v, _ := store.SettingBool(ctx, s, key, def); return v }
	num := func(key string, def int64) int64 { v, _ := store.SettingIntOr(ctx, s, key, def); return v }
	limits := map[string]any{
		"message_text": model.MaxMessageText, "file_size": num(store.SettingFilesMaxBytes, model.DefaultFileSize),
		"quick_correction_seconds": num(store.SettingQuickCorrection, model.DefaultQuickCorrection),
		"call_participants":        num(store.SettingCallsMaxPeople, 50), "call_cameras": num(store.SettingCallsMaxCameras, 12),
	}
	if v, set, _ := store.SettingInt(ctx, s, store.SettingEditWindow); set {
		limits["edit_window_seconds"] = v
	}
	if v, set, _ := store.SettingInt(ctx, s, store.SettingRetention); set {
		limits["retention_seconds"] = v
	}
	server := map[string]any{
		"name": a.Hostname, "community_name": str(store.SettingServerName, a.Hostname), "time_zone": str("server.time_zone", "UTC"),
		"version": version.Version, "min_app_version": "0.0.0", "setup_needed": need,
		"sign_in": map[string]any{
			"registration": str(store.SettingRegistrationMode, "invite"), "member_sign_in": str(store.SettingMemberSignIn, "passkey"),
			"second_factor": str(store.SettingSecondFactor, "off"), "providers": providers,
			"access_keys": flag(auth.SettingAllowAccessKeys, true),
		},
		"features": map[string]any{
			"sealed_chats": flag(store.SettingSealedEnabled, true), "calls": flag(store.SettingCallsEnabled, true),
			"link_previews": flag(store.SettingPreviewsEnabled, false), "blocking": flag(store.SettingBlockingEnabled, true),
			"members_may_invite": flag(store.SettingMembersMayInvite, false), "members_may_add_emoji": flag(store.SettingMembersMayAddEmoji, false),
			"broadcast_mentions_managers_only": flag(store.SettingBroadcastManagers, false),
		},
		"push_kinds": a.pushKinds(ctx), "limits": limits,
	}
	if icon := str("server.icon_file_id", ""); icon != "" {
		server["icon_url"] = a.Origin + "/api/v1/files/" + icon + "/icon"
	}
	if signedIn {
		if w := str("welcome.text", ""); w != "" {
			server["welcome"] = w
		}
	}
	a.addDefaultThemes(ctx, server) // themes.go
	return server, nil
}

// getWellKnown gives the server's name and public keys to other servers.
func (a *API) getWellKnown(r *Request) error {
	if a.Link != nil {
		// The link protocol fixes this document's fields
		// (docs/LINK-PROTOCOL.md section 3): other servers and hubs
		// check a peer's keys against exactly this.
		r.W.Header().Set("Cache-Control", "max-age=3600")
		return r.JSON(a.Link.Identity().WellKnown())
	}
	enc := base64.RawURLEncoding.EncodeToString
	return r.JSON(map[string]any{
		"name": a.Hostname, "version": version.Version,
		"sign_key": enc(a.Keys.SignPublic()), "box_key": enc(a.Keys.BoxPublic()),
		"api": "/api/v1", "link": "/link/v1",
	})
}
