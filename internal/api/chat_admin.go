package api

import (
	"encoding/json"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/version"
)

// The admin console's channels and content, its records, and the test of
// object storage.

// startedAt is when this process began, for the overview.
var startedAt = model.Now()

func init() {
	register(func(a *API) {
		a.handle("createSection", a.createSection)
		a.handle("updateSection", a.updateSection)
		a.handle("deleteSection", a.deleteSection)
		a.handle("adminListChannels", a.adminListChannels)
		a.handle("adminGetChannel", a.adminGetChannel)
		a.handle("adminUpdateChannel", a.adminUpdateChannel)
		a.handle("appointManager", a.appointManager)
		a.handle("adminArchiveChannel", a.adminArchiveChannel)
		a.handle("adminUnarchiveChannel", a.adminUnarchiveChannel)
		a.handle("adminLockChannel", a.adminLockChannel)
		a.handle("adminListEmoji", a.adminListEmoji)
		a.handle("renameEmoji", a.renameEmoji)
		a.handle("adminDeleteEmoji", a.adminDeleteEmoji)
		a.handle("listAuditLog", a.listAuditLog)
		a.handle("getOverview", a.getOverview)
		a.handle("testObjectStorage", a.testObjectStorage)
	})
}

func (a *API) createSection(r *Request) error {
	var body struct {
		Name     string `json:"name"`
		Position *int   `json:"position"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	s, err := a.Chat.CreateSection(r.Ctx(), r.Actor(), body.Name, body.Position)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"section": s})
}

func (a *API) updateSection(r *Request) error {
	id, err := PathID[model.SectionID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Name     *string `json:"name"`
		Position *int    `json:"position"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	s, err := a.Chat.UpdateSection(r.Ctx(), r.Actor(), id, body.Name, body.Position)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"section": s})
}

func (a *API) deleteSection(r *Request) error {
	id, err := PathID[model.SectionID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Chat.DeleteSection(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) adminListChannels(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	q := r.R.URL.Query()
	f := store.AdminChannelFilter{Query: q.Get("q")}
	if q.Get("private") != "" {
		private, err := r.queryBool("private")
		if err != nil {
			return err
		}
		f.Private = &private
	}
	if f.IncludeArchived, err = r.queryBool("archived"); err != nil {
		return err
	}
	if v := q.Get("parent_id"); v != "" {
		if f.ParentID, err = BodyID[model.ConversationID]("parent_id", v); err != nil {
			return err
		}
	}
	channels, next, err := a.Chat.AdminChannels(r.Ctx(), r.Actor(), f, page)
	if err != nil {
		return err
	}
	return r.JSON(list("channels", orEmpty(channels), next))
}

func (a *API) adminGetChannel(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	channel, members, err := a.Chat.AdminChannel(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"channel": channel, "members": orEmpty(members)})
}

func (a *API) adminUpdateChannel(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		SectionID        json.RawMessage `json:"section_id"`
		RetentionSeconds *int64          `json:"retention_seconds"`
		ParentID         json.RawMessage `json:"parent_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	section, err := optionalID[model.SectionID]("section_id", body.SectionID)
	if err != nil {
		return err
	}
	parent, err := optionalID[model.ConversationID]("parent_id", body.ParentID)
	if err != nil {
		return err
	}
	channel, err := a.Chat.AdminUpdateChannel(r.Ctx(), r.Actor(), id, section, body.RetentionSeconds, parent)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"channel": channel})
}

func (a *API) appointManager(r *Request) error {
	id, account, err := memberPath(r)
	if err != nil {
		return err
	}
	m, err := a.Chat.AppointManager(r.Ctx(), r.Actor(), id, account)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"member": m})
}

func (a *API) adminArchive(r *Request, archived bool) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	channel, err := a.Chat.AdminArchive(r.Ctx(), r.Actor(), id, archived)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"channel": channel})
}

func (a *API) adminArchiveChannel(r *Request) error   { return a.adminArchive(r, true) }
func (a *API) adminUnarchiveChannel(r *Request) error { return a.adminArchive(r, false) }

func (a *API) adminLockChannel(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	channel, err := a.Chat.AdminLock(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"channel": channel})
}

func (a *API) adminListEmoji(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	emoji, next, err := a.Chat.AdminEmoji(r.Ctx(), r.Actor(), page)
	if err != nil {
		return err
	}
	return r.JSON(list("emoji", orEmpty(emoji), next))
}

func (a *API) renameEmoji(r *Request) error {
	id, err := PathID[model.EmojiID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	e, err := a.Chat.RenameEmoji(r.Ctx(), r.Actor(), id, body.Name)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"emoji": e})
}

func (a *API) adminDeleteEmoji(r *Request) error {
	id, err := PathID[model.EmojiID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Chat.AdminDeleteEmoji(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listAuditLog(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	q := r.R.URL.Query()
	f := store.AuditFilter{TargetID: q.Get("target_id")}
	if v := q.Get("actor_id"); v != "" {
		id, err := BodyID[string]("actor_id", v)
		if err != nil {
			return err
		}
		f.ActorID = id
	}
	// `account.` asks for everything that starts with it; anything else
	// is one action.
	if action := q.Get("action"); len(action) > 1 && action[len(action)-1] == '.' {
		f.ActionPrefix = action
	} else {
		f.Action = action
	}
	if f.Since, err = r.queryTime("since"); err != nil {
		return err
	}
	if f.Until, err = r.queryTime("until"); err != nil {
		return err
	}
	entries, next, err := a.Chat.AuditLog(r.Ctx(), r.Actor(), f, page)
	if err != nil {
		return err
	}
	return r.JSON(list("entries", orEmpty(entries), next))
}

// getOverview gathers the numbers the admin console's first page shows.
// Calls and links to other servers are asked through a.Overview and read as
// none when the server has not set them.
func (a *API) getOverview(r *Request) error {
	ctx := r.Ctx()
	if err := perm.CheckServer(r.Actor(), perm.UseAdminConsole, perm.DefaultSettings()); err != nil {
		return err
	}
	now := model.Now()
	accounts := map[string]int64{"people": 0, "guests": 0, "bots": 0, "remote": 0}
	online := 0
	page := store.Page{Limit: model.MaxLimit}
	for {
		list, next, err := a.Store.Accounts().List(ctx, store.AccountFilter{State: "active"}, page)
		if err != nil {
			return err
		}
		for _, acc := range list {
			switch acc.Role {
			case model.RoleGuest:
				accounts["guests"]++
			case model.RoleBot:
				accounts["bots"]++
			case model.RoleRemote:
				accounts["remote"]++
			default:
				accounts["people"]++
				if a.Live.Presence(acc.ID) == model.PresenceOnline {
					online++
				}
			}
		}
		if next == "" {
			break
		}
		page.Cursor = next
	}
	files, bytes, err := a.Store.Files().Totals(ctx)
	if err != nil {
		return err
	}
	reports, err := a.Store.Moderation().WaitingReports(ctx)
	if err != nil {
		return err
	}
	purges, err := a.Store.Moderation().WaitingPurgeRequests(ctx)
	if err != nil {
		return err
	}
	filesStore := "disk"
	if a.Storage != nil {
		filesStore = a.Storage.FilesStore(ctx)
	}
	overview := map[string]any{
		"version":        version.Version,
		"started_at":     startedAt,
		"uptime_seconds": int64(now.Sub(startedAt) / time.Second),
		"database":       map[string]any{"kind": a.Store.Dialect()},
		"files":          map[string]any{"store": filesStore, "count": files, "size": bytes},
		"accounts":       accounts,
		"members_online": online,
		"connections":    a.Live.Connected(),
		"live_calls":     0,
		"outside":        []any{},
		"peers":          []any{},
		"outbox":         0,
		"waiting":        map[string]any{"webhooks": 0, "reports": reports, "purge_requests": purges},
		"warnings":       []string{},
	}
	if a.Janitor != nil && !a.Janitor.LastRun().IsZero() {
		overview["janitor_last_run_at"] = a.Janitor.LastRun()
	}
	// The database's size, the webhook queue, calls, peers and the state
	// of each outside connection (ops_overview.go).
	if err := a.overviewRest(ctx, overview, filesStore); err != nil {
		return err
	}
	return r.JSON(map[string]any{"overview": overview})
}

func (a *API) testObjectStorage(r *Request) error {
	if err := perm.CheckServer(r.Actor(), perm.ChangeSettings, perm.DefaultSettings()); err != nil {
		return err
	}
	var body struct {
		S3     json.RawMessage `json:"files.s3"`
		Secret *string         `json:"files.s3_secret"`
	}
	// With no body the stored settings are tested.
	if r.R.ContentLength != 0 {
		if err := r.Decode(&body); err != nil {
			return err
		}
	}
	if a.Storage == nil {
		return model.ErrUnavailable
	}
	if err := a.Storage.TestStorage(r.Ctx(), body.S3, body.Secret); err != nil {
		if e, ok := model.AsError(err); ok && e.Code == model.CodeInvalidValue {
			return err
		}
		// The test failing is the answer, not a failure of the request.
		// What the store said is for the admin to read.
		return r.JSON(map[string]any{"ok": false, "error": err.Error()})
	}
	return r.JSON(map[string]any{"ok": true})
}
