package api

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/push"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is the part of the admin Overview that comes from outside the
// chat packages: the database's size, the queues, and how each outside
// connection is doing. Calls and links to other servers are separate
// packages; the Overview asks them through the small interfaces below,
// which the server sets when those packages are built in. Unset, each
// reports nothing: no live calls, no peers, an empty outbox.

// OverviewCalls is what the Overview needs of the calls package.
type OverviewCalls interface {
	// LiveCalls is how many calls are in progress now.
	LiveCalls() int
	// MediaAddress is the address and port clients send call media to,
	// or "" when calls are off.
	MediaAddress() string
	// MediaWarning is a sentence for the warnings while clients are told
	// only private media addresses, or "".
	MediaWarning() string
}

// OverviewLinks is what the Overview needs of the link package.
type OverviewLinks interface {
	// OverviewPeers returns the linked servers and hubs in the contract's
	// `Peer` shape.
	OverviewPeers(ctx context.Context) ([]any, error)
	// OutboxWaiting is how many events wait for a peer.
	OutboxWaiting(ctx context.Context) (int64, error)
}

// OutsideHealth is how one outside connection has been doing, as the package
// that makes the connection reports it. A zero value means nothing is known
// yet, which is shown as working.
type OutsideHealth struct {
	// LastOK is when the connection last worked.
	LastOK model.Time
	// LastError is a plain sentence about the last failure, or "" once it
	// has worked again.
	LastError string
}

// OutsideProbe reports the health of one outside connection.
type OutsideProbe func(ctx context.Context) OutsideHealth

// The outside connections a server can make, by the names the contract
// gives them (docs/openapi.yaml, OutsideConnection), in the order of the
// table in BRIEF.md.
const (
	OutsideCertificate      = "certificate"
	OutsidePushRelay        = "push_relay"
	OutsideUnifiedPush      = "unifiedpush"
	OutsideLinkPreviews     = "link_previews"
	OutsideObjectStorage    = "object_storage"
	OutsideIdentityProvider = "identity_provider"
	OutsidePeers            = "peers"
)

// Overview is what the server tells the API about itself for the Overview
// page. Every field may be left unset.
type Overview struct {
	Calls OverviewCalls
	Links OverviewLinks
	// FetchesCertificate is true when this server gets its own
	// certificate from a certificate authority: it runs without a proxy,
	// not as a local trial, and was given no certificate of its own.
	FetchesCertificate bool
	// Probes are the health reports, by connection name. A connection
	// with none is `ok` when it is on.
	Probes map[string]OutsideProbe
	// Warnings returns plain sentences for the top of the page: a
	// certificate near its end, a media port that looks closed.
	Warnings func(ctx context.Context) []string
}

// outsideConnection is one entry of `outside`.
type outsideConnection struct {
	Name      string      `json:"name"`
	Enabled   bool        `json:"enabled"`
	State     string      `json:"state"`
	LastOKAt  *model.Time `json:"last_ok_at,omitempty"`
	LastError string      `json:"last_error,omitempty"`
}

// outside lists every outside connection with whether it is switched on and
// how it is doing. What is on is read from where it is decided: the
// settings, the provider table, the peers, and how the server was started.
func (a *API) outside(ctx context.Context, filesStore string, peers int) ([]outsideConnection, error) {
	setting := func(key string, def bool) (bool, error) { return store.SettingBool(ctx, a.Store, key, def) }
	// The relay is on when it is switched on and has an address: there is
	// no relay by default.
	relay, err := setting(store.SettingPushRelayEnabled, false)
	if err != nil {
		return nil, err
	}
	if url, err := store.SettingString(ctx, a.Store, push.SettingRelayURL, ""); err != nil {
		return nil, err
	} else if url == "" {
		relay = false
	}
	unified, err := setting(store.SettingUnifiedPushEnabled, false)
	if err != nil {
		return nil, err
	}
	previews, err := setting(store.SettingPreviewsEnabled, false)
	if err != nil {
		return nil, err
	}
	providers, err := a.Store.Identities().ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	sso := false
	for _, p := range providers {
		sso = sso || p.Enabled
	}
	out := []outsideConnection{
		{Name: OutsideCertificate, Enabled: a.Overview.FetchesCertificate},
		{Name: OutsidePushRelay, Enabled: relay},
		{Name: OutsideUnifiedPush, Enabled: unified},
		{Name: OutsideLinkPreviews, Enabled: previews},
		{Name: OutsideObjectStorage, Enabled: filesStore == "s3"},
		{Name: OutsideIdentityProvider, Enabled: sso},
		{Name: OutsidePeers, Enabled: peers > 0},
	}
	for i := range out {
		c := &out[i]
		if !c.Enabled {
			c.State = "off"
			continue
		}
		c.State = "ok"
		probe := a.Overview.Probes[c.Name]
		if probe == nil {
			continue
		}
		h := probe(ctx)
		if !h.LastOK.IsZero() {
			at := h.LastOK
			c.LastOKAt = &at
		}
		if h.LastError != "" {
			c.State, c.LastError = "failing", h.LastError
		}
	}
	return out, nil
}

// overviewRest fills in the parts of the Overview this file owns.
func (a *API) overviewRest(ctx context.Context, overview map[string]any, filesStore string) error {
	totals, err := store.ReadTotals(ctx, a.Store)
	if err != nil {
		return err
	}
	database := map[string]any{"kind": a.Store.Dialect()}
	if totals.DatabaseBytes > 0 {
		database["size"] = totals.DatabaseBytes
	}
	overview["database"] = database
	if waiting, ok := overview["waiting"].(map[string]any); ok {
		waiting["webhooks"] = totals.WaitingWebhooks
	}
	if c := a.Overview.Calls; c != nil {
		overview["live_calls"] = c.LiveCalls()
		if addr := c.MediaAddress(); addr != "" {
			overview["media_address"] = addr
		}
	}
	peers := []any{}
	if l := a.Overview.Links; l != nil {
		if peers, err = l.OverviewPeers(ctx); err != nil {
			return err
		}
		waiting, err := l.OutboxWaiting(ctx)
		if err != nil {
			return err
		}
		overview["outbox"] = waiting
	}
	overview["peers"] = orEmpty(peers)
	outside, err := a.outside(ctx, filesStore, len(peers))
	if err != nil {
		return err
	}
	overview["outside"] = outside
	warnings := []string{}
	for _, c := range outside {
		if c.State == "failing" {
			warnings = append(warnings, "An outside connection is failing ("+c.Name+"): "+c.LastError)
		}
	}
	warnings = append(warnings, a.untrustedWarnings()...)
	if c := a.Overview.Calls; c != nil {
		if w := c.MediaWarning(); w != "" {
			warnings = append(warnings, w)
		}
	}
	if a.Overview.Warnings != nil {
		warnings = append(warnings, a.Overview.Warnings(ctx)...)
	}
	overview["warnings"] = warnings
	return nil
}

// OutsideOn names the outside connections that are switched on, for the
// start-up line of the log.
func (a *API) OutsideOn(ctx context.Context) ([]string, error) {
	filesStore := "disk"
	if a.Storage != nil {
		filesStore = a.Storage.FilesStore(ctx)
	}
	peers := 0
	if l := a.Overview.Links; l != nil {
		if list, err := l.OverviewPeers(ctx); err == nil {
			peers = len(list)
		}
	}
	all, err := a.outside(ctx, filesStore, peers)
	if err != nil {
		return nil, err
	}
	var on []string
	for _, c := range all {
		if c.Enabled {
			on = append(on, c.Name)
		}
	}
	return on, nil
}
