package hub

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/forms"
	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/migrations"
	_ "github.com/tikaydaes/churr/internal/store/sqlite" // the holder's database: the instance's schema, in SQLite
)

// This file is the hub's holder (docs/LINK-PROTOCOL.md section 23a, "The
// hub holds the whole channel"). The hub owns the channels it is the home
// of, all of them: keys, contents, history, files, members, settings. It
// keeps them by running the code an instance runs to hold a channel (the
// link, chat, perm and the file store) against a database of its own with
// the instance's schema and migrations, in holder/ under its data
// directory. There is no second copy of the rules that apply an event.
//
// The holder dials the hub on an internal connection under the hub's own
// name and keys, and speaks the frames an instance speaks. The hub treats it
// as no server of any channel: it is in no record's list. It sends it every
// record and key of the channels it is the home of, delivers it their every
// envelope, serves it their log, and hands it the direct messages addressed
// to the hub that are not key requests: history and file requests from
// instances, and the answers to the holder's own. What the holder sends
// leaves as the hub's direct messages, signed with the hub's key.

// holderDir is where the holder keeps its database and files, under the
// hub's data directory.
const holderDir = "holder"

// holder is the running holder.
type holder struct {
	store store.Store
	link  *link.Service
	ln    net.Listener
	srv   *http.Server
}

// quietHub is the holder's stand-in for the live fan-out: nobody is
// connected to the holder, so there is nobody to tell.
type quietHub struct{}

func (quietHub) Publish(context.Context, model.Event)    {}
func (quietHub) Send(model.AccountID, any)               {}
func (quietHub) Presence(model.AccountID) model.Presence { return model.PresenceOffline }
func (quietHub) Tailored(context.Context, model.Event, model.AccountID) json.RawMessage {
	return nil
}

// startHolder opens the holder's database and starts it. The internal
// connection is a listener on the loopback address serving the hub's own
// link handler; the holder's dials go there whatever address they name.
func (h *Hub) startHolder(ctx context.Context) error {
	dir := filepath.Join(h.cfg.Data, holderDir)
	files, tmp := filepath.Join(dir, "files"), filepath.Join(dir, "tmp")
	for _, d := range []string{dir, files, tmp} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("making the holder's directory: %w", err)
		}
	}
	st, err := store.Open(migrations.WithLog(ctx, h.log), dir, "")
	if err != nil {
		return fmt.Errorf("opening the holder's database: %w", err)
	}
	fail := func(err error) error {
		st.Close()
		return err
	}
	if _, err := st.Conversations().EnsureLibrary(ctx); err != nil {
		return fail(err)
	}
	blobs, err := blob.Disk(files)
	if err != nil {
		return fail(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fail(err)
	}
	srv := &http.Server{Handler: h.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	addr := ln.Addr().String()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}}}
	pm := perm.New(st)
	c := chat.New(chat.Deps{Store: st, Perm: pm, Hub: quietHub{}, Blobs: blobs, Log: h.log})
	// Polls: without the forms package a message's poll is neither shown
	// in the snapshot the hub gives nor kept up with form.tallied and
	// form.closed.
	c.SetForms(forms.New(forms.Deps{Store: st, Perm: pm, Chat: c, Log: h.log}))
	l, err := link.New(link.Deps{Store: st, Keys: h.keys, Chat: c, Perm: pm, Blobs: blobs, Log: h.log.With("part", "holder"),
		Name: h.id.Name, HTTPClient: client, TmpDir: tmp, Now: h.now, Holder: true})
	if err == nil {
		l.SetForms(forms.Shared{})
	}
	if err == nil {
		err = l.Start(ctx)
	}
	if err == nil {
		err = l.Hold(ctx, "ws://127.0.0.1/link/v1")
	}
	if err != nil {
		srv.Close()
		return fail(err)
	}
	h.mu.Lock()
	h.holder = &holder{store: st, link: l, ln: ln, srv: srv}
	h.mu.Unlock()
	h.sweepHeld()
	return nil
}

// stopHolder stops the holder and closes its database.
func (h *Hub) stopHolder() {
	h.mu.Lock()
	hd := h.holder
	h.holder = nil
	h.mu.Unlock()
	if hd == nil {
		return
	}
	hd.link.Stop()
	hd.srv.Close()
	hd.store.Close()
}

// sweepHeld removes the holder's copy of every channel the hub no longer
// has, closed and kept the thirty days after (Sweep), and of every channel
// it handed to another hub of the cluster more than link.HandOverKeep ago.
func (h *Hub) sweepHeld() {
	l := h.Holder()
	if l == nil {
		return
	}
	ctx := context.Background()
	ids, err := l.Held(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		var one int
		if errors.Is(h.db.QueryRow(`SELECT 1 FROM streams WHERE id = ?`, string(id)).Scan(&one), sql.ErrNoRows) {
			l.ForgetHeld(ctx, id)
		}
	}
	// A channel handed to another hub: kept for the new home to fetch from,
	// then let go.
	l.ForgetHandedOver(ctx, h.now().Add(-link.HandOverKeep))
}

// isHolder reports whether a dialler that has shown the hub's own name holds
// the hub's own keys: the holder, and nobody else, can.
func (h *Hub) isHolder(d wire.Frame) bool {
	return d.Name == h.id.Name && bytes.Equal(d.SignKey, h.keys.SignPublic()) && bytes.Equal(d.BoxKey, h.keys.BoxPublic())
}

// holderConn is the holder's connection, nil while it is not connected.
func (h *Hub) holderConn() *serverConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.held
}

// holderOf reports whether a stream is a channel this hub is the home of,
// which its holder keeps: every envelope of it is delivered to the holder
// too.
func (h *Hub) holderOf(st stream) bool {
	return st.owner == h.id.Name && st.homeHub == h.id.Name
}

// serveHolder runs the holder's connection. The holder is sent what a server
// is sent on connecting, and every record of a channel this hub is the home
// of; from it the hub takes catch_up, ack and direct messages, and nothing
// else: it originates nothing in any channel.
func (h *Hub) serveHolder(r *http.Request, conn *wire.Conn) {
	sc := &serverConn{name: h.id.Name, conn: conn, key: h.keys.SignPublic(), holder: true}
	conn.Queue(sendQueueBytes)
	h.mu.Lock()
	old := h.held
	h.held = sc
	h.mu.Unlock()
	if old != nil {
		old.conn.CloseWith(wire.ErrorFrame(0, wire.EReplaced, "A newer connection for this server took over."))
	}
	defer func() {
		h.mu.Lock()
		if h.held == sc {
			h.held = nil
		}
		h.mu.Unlock()
		conn.Close()
	}()
	go conn.Watch()
	h.sendRoster(sc)
	conn.Send(h.clusterFrame())
	h.resendChannels(sc)
	h.resendHandedOver(sc)
	h.nudgeHolder("")
	ctx := r.Context()
	for {
		f, raw, err := conn.Read(ctx)
		if err != nil {
			var bad *wire.Error
			if errors.As(err, &bad) {
				conn.Send(bad.Frame(0))
				continue
			}
			return
		}
		var ferr error
		switch f.Frame {
		case wire.FBye:
			return
		case wire.FPong, wire.FAck, wire.FError:
		case wire.FPing:
			conn.AnswerPing(f, int64(h.now()), func() int64 { return int64(h.now()) })
		case wire.FCatchUp:
			ferr = h.catchUp(sc, f)
		case wire.FDirect:
			ferr = h.holderDirect(sc, f, raw)
		default:
			ferr = wire.Errf(wire.EForbidden, "The hub's holder originates nothing.")
		}
		if ferr != nil {
			e := wire.AsError(ferr)
			if e.Code == wire.EInternal {
				h.log.Warn("a frame of the holder could not be handled", "frame", f.Frame, "err", ferr)
			}
			conn.Send(e.Frame(f.ID))
		}
	}
}

// holderDirect passes on a direct message the holder sends: history or a
// file to an instance that asked for it, a file or history asked of an
// instance, or a key asked of the hub itself. It goes as the hub's own,
// unopened, to wherever the server is in the cluster.
func (h *Hub) holderDirect(sc *serverConn, f wire.Frame, raw []byte) error {
	if f.From != h.id.Name || !wire.VerifyDirect(f, sc.key) {
		return wire.Errf(wire.EBadSignature, "The direct message's signature does not verify.")
	}
	if f.To == h.id.Name {
		return h.ownDirect(h.id.Name, sc.key, f)
	}
	there := h.hubOf(f.To)
	switch {
	case there == "":
		return wire.Errf(wire.EUnknownServer, "%s is not on this hub or its cluster.", f.To)
	case there != h.id.Name:
		tc := h.toward(there)
		if tc == nil || tc.send(wire.Frame{Frame: wire.FForward, Server: f.To, Inner: raw}) != nil {
			return wire.Errf(wire.EUnavailable, "%s is on %s, which cannot be reached now.", f.To, there)
		}
		return nil
	}
	to := h.connOf(f.To)
	if to == nil {
		return wire.Errf(wire.EUnavailable, "%s is not connected.", f.To)
	}
	return to.conn.Send(wire.Frame{Frame: wire.FDirect, ID: to.conn.NextID(), From: f.From, To: f.To, Body: f.Body, Sig: f.Sig})
}

// toHolder hands the holder a direct message addressed to the hub that the
// hub does not answer itself. Nil if the holder is not connected: whoever
// sent it asks again.
func (h *Hub) toHolder(f wire.Frame) {
	if sc := h.holderConn(); sc != nil {
		sc.conn.Send(wire.Frame{Frame: wire.FDirect, ID: sc.conn.NextID(), From: f.From, To: f.To, Body: f.Body, Sig: f.Sig})
	}
}

// nudgeHolder tells the holder that a server has connected, or with "" that
// hubs or servers behind a trunk can be reached again: it asks at once
// whoever it still waits on for a channel's earlier history.
func (h *Hub) nudgeHolder(server string) {
	if l := h.Holder(); l != nil {
		go l.Nudge(server)
	}
}

// resendHandedOver tells the holder, as it connects, of every channel this
// hub was the home of and has handed to another hub of the cluster: the
// record by which it handed the channel over, and the new home's newest. A
// holder that was not connected when the channel was handed over learns it
// here, takes nothing more of the channel, answers the new home, and lets
// its copy go HandOverKeep after (link/holder.go). A holder that holds
// nothing of the channel does nothing with them.
func (h *Hub) resendHandedOver(sc *serverConn) {
	rows, err := h.db.Query(`SELECT stream_id FROM channels`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		m := h.lock(id)
		c, err := loadChannel(h.db, id)
		m.Unlock()
		if err != nil || h.owned(c) || c.rec.HomeHub == h.id.Name || len(c.signed) == 0 {
			continue
		}
		mine := false
		for _, ho := range c.handovers {
			var rec wire.ChannelRecord
			if wire.VerifyBlock(wire.Frame{Frame: wire.FChannel, Signed: ho.Signed, Sig: ho.Sig}, h.keys.SignPublic(), &rec) == nil {
				sc.conn.Send(wire.Frame{Frame: wire.FChannel, Signed: ho.Signed, Sig: ho.Sig, HomeHub: c.rec.HomeHub})
				mine = true
			}
		}
		if mine {
			sc.conn.Send(wire.Frame{Frame: wire.FChannel, Signed: c.signed, Sig: c.sig, HomeHub: c.rec.HomeHub})
		}
	}
}

// Holder is the hub's holder's link, nil while the hub is not serving. It is
// for tests and the status page.
func (h *Hub) Holder() *link.Service {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.holder == nil {
		return nil
	}
	return h.holder.link
}

// HolderStore is the holder's database, nil while the hub is not serving.
// It is for tests.
func (h *Hub) HolderStore() store.Store {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.holder == nil {
		return nil
	}
	return h.holder.store
}
