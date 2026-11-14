package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/people"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/store"
)

// The people area: groups and blocks, the caller's own profile, preferences
// and status, thread states, bookmarks, notify anyway, and what admins do
// to accounts and groups.

func init() {
	register(func(a *API) {
		// Accounts
		a.handle("listGroups", a.listGroups)
		a.handle("getGroup", a.getGroup)
		a.handle("listGroupMembers", a.listGroupMembers)
		a.handle("joinGroup", a.joinGroup)
		a.handle("leaveGroup", a.leaveGroup)
		a.handle("listBlocks", a.listBlocks)
		a.handle("blockAccount", a.blockAccount)
		a.handle("unblockAccount", a.unblockAccount)
		// Me
		a.handle("updateMe", a.updateMe)
		a.handle("setMyAvatar", a.setMyAvatar)
		a.handle("removeMyAvatar", a.removeMyAvatar)
		a.handle("getPrefs", a.getPrefs)
		a.handle("updatePrefs", a.updatePrefs)
		a.handle("getStatusSummary", a.getStatusSummary)
		// Status
		a.handle("setStatus", a.setStatus)
		a.handle("clearStatus", a.clearStatus)
		a.handle("setAvailability", a.setAvailability)
		a.handle("clearAvailability", a.clearAvailability)
		a.handle("setDnd", a.setDnd)
		a.handle("clearDnd", a.clearDnd)
		a.handle("getQuietHours", a.getQuietHours)
		a.handle("setQuietHours", a.setQuietHours)
		a.handle("removeQuietHours", a.removeQuietHours)
		a.handle("skipQuietHours", a.skipQuietHours)
		a.handle("unskipQuietHours", a.unskipQuietHours)
		a.handle("setAway", a.setAway)
		a.handle("clearAway", a.clearAway)
		a.handle("listStatusPresets", a.listStatusPresets)
		a.handle("createStatusPreset", a.createStatusPreset)
		a.handle("updateStatusPreset", a.updateStatusPreset)
		a.handle("deleteStatusPreset", a.deleteStatusPreset)
		// Threads
		a.handle("listThreads", a.listThreads)
		a.handle("setThreadState", a.setThreadState)
		a.handle("setThreadReadPosition", a.setThreadReadPosition)
		// Conversations
		a.handle("updateMembership", a.updateMembership)
		a.handle("sendTyping", a.sendTyping)
		// Messages
		a.handle("notifyAnyway", a.notifyAnyway)
		a.handle("addBookmark", a.addBookmark)
		a.handle("removeBookmark", a.removeBookmark)
		a.handle("listBookmarks", a.listBookmarks)
		// Admin: people
		a.handle("adminListAccounts", a.adminListAccounts)
		a.handle("adminGetAccount", a.adminGetAccount)
		a.handle("setAccountRole", a.setAccountRole)
		a.handle("removeAccount", a.removeAccount)
		a.handle("listBans", a.listBans)
		a.handle("createBan", a.createBan)
		a.handle("liftBan", a.liftBan)
		a.handle("createGroup", a.createGroup)
		a.handle("updateGroup", a.updateGroup)
		a.handle("deleteGroup", a.deleteGroup)
		a.handle("addGroupMember", a.addGroupMember)
		a.handle("removeGroupMember", a.removeGroupMember)
		a.handle("createClaimLink", a.createClaimLink)
		// Admin: channels and content
		a.handle("createCommunityPreset", a.createCommunityPreset)
		a.handle("updateCommunityPreset", a.updateCommunityPreset)
		a.handle("deleteCommunityPreset", a.deleteCommunityPreset)
	})
}

// optional decodes a body that may be left out altogether: an empty body
// reads as an empty object.
func (r *Request) optional(v any) error {
	err := r.Decode(v)
	if e, ok := model.AsError(err); ok && e.Code == model.CodeInvalidRequest && e.Message == "This request needs a JSON body." {
		return nil
	}
	return err
}

// ---- Groups ----

func (a *API) listGroups(r *Request) error {
	groups, err := a.People.Groups(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"groups": orEmpty(groups)})
}

func (a *API) getGroup(r *Request) error {
	id, err := PathID[model.GroupID](r, "id")
	if err != nil {
		return err
	}
	g, err := a.People.Group(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"group": g})
}

func (a *API) listGroupMembers(r *Request) error {
	id, err := PathID[model.GroupID](r, "id")
	if err != nil {
		return err
	}
	page, err := r.Page()
	if err != nil {
		return err
	}
	ids, next, err := a.People.GroupMembers(r.Ctx(), r.Actor(), id, page)
	if err != nil {
		return err
	}
	return r.JSON(list("account_ids", orEmpty(ids), next))
}

func (a *API) joinGroup(r *Request) error {
	id, err := PathID[model.GroupID](r, "id")
	if err != nil {
		return err
	}
	g, err := a.People.JoinGroup(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"group": g})
}

func (a *API) leaveGroup(r *Request) error {
	id, err := PathID[model.GroupID](r, "id")
	if err != nil {
		return err
	}
	g, err := a.People.LeaveGroup(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"group": g})
}

// ---- Blocks ----

func (a *API) listBlocks(r *Request) error {
	ids, err := a.People.Blocks(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"account_ids": orEmpty(ids)})
}

func (a *API) blockAccount(r *Request) error {
	id, err := PathID[model.AccountID](r, "account_id")
	if err != nil {
		return err
	}
	if err := a.People.Block(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) unblockAccount(r *Request) error {
	id, err := PathID[model.AccountID](r, "account_id")
	if err != nil {
		return err
	}
	if err := a.People.Unblock(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

// ---- Me ----

func (a *API) updateMe(r *Request) error {
	var body struct {
		Username    *string `json:"username"`
		DisplayName *string `json:"display_name"`
		Title       *string `json:"title"`
		Pronouns    *string `json:"pronouns"`
		TimeZone    *string `json:"time_zone"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	me, err := a.People.UpdateMe(r.Ctx(), r.Actor(), people.ProfileChange{Username: body.Username, DisplayName: body.DisplayName,
		Title: body.Title, Pronouns: body.Pronouns, TimeZone: body.TimeZone})
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"account": me})
}

func (a *API) setMyAvatar(r *Request) error {
	mr, err := r.R.MultipartReader()
	if err != nil {
		return model.BadRequest("Send the image as multipart/form-data in a part named `file`.")
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return model.Invalid("file", "The upload has no part named `file`.")
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				return model.ErrTooLarge.With("An avatar is at most 1 MB.")
			}
			return model.BadRequest("The upload could not be read.")
		}
		if part.FormName() != "file" {
			continue
		}
		me, err := a.People.SetAvatar(r.Ctx(), r.Actor(), part)
		if err != nil {
			return err
		}
		return r.JSON(map[string]any{"account": me})
	}
}

func (a *API) removeMyAvatar(r *Request) error {
	if err := a.People.RemoveAvatar(r.Ctx(), r.Actor()); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) getPrefs(r *Request) error {
	return r.JSON(map[string]any{"prefs": a.People.Prefs(r.Actor())})
}

func (a *API) updatePrefs(r *Request) error {
	var body struct {
		ThreadView        *string `json:"thread_view"`
		MeetingPauses     *bool   `json:"meeting_pauses"`
		AllowNotifyAnyway *bool   `json:"allow_notify_anyway"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	prefs, err := a.People.UpdatePrefs(r.Ctx(), r.Actor(), people.PrefsChange{ThreadView: body.ThreadView,
		MeetingPauses: body.MeetingPauses, AllowNotifyAnyway: body.AllowNotifyAnyway})
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"prefs": prefs})
}

// statusLimiter is the status endpoint's own limit: 30 a minute per session
// (docs/PUSH.md section 11).
var statusLimiter = ratelimit.New(ratelimit.Per(30, time.Minute), 30)

func (a *API) getStatusSummary(r *Request) error {
	if res := statusLimiter.Take(string(r.Who.Session.ID)); !res.OK {
		return model.RateLimited(res.RetryAfterSeconds())
	}
	totals, newest, err := a.Notify.Summary(r.Ctx(), r.Actor().ID)
	if err != nil {
		return err
	}
	out := model.StatusSummary{Totals: totals, Newest: newest}
	if a.Calls != nil {
		// A call that is ringing for this account right now.
		out.RingingCallID = a.Calls.Ringing(r.Actor().ID)
	}
	dnd := notify.DND(r.Actor(), model.Now())
	out.Dnd, out.DndUntil = dnd.On, dnd.Until
	body, err := json.Marshal(out)
	if err != nil {
		return err
	}
	// A strong ETag over the body, which holds no clock time and so
	// changes only when something changed.
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	h := r.W.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "private, no-cache")
	for _, candidate := range strings.Split(r.R.Header.Get("If-None-Match"), ",") {
		if c := strings.TrimSpace(candidate); c == etag || c == "*" {
			r.W.WriteHeader(http.StatusNotModified)
			return nil
		}
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	r.W.WriteHeader(http.StatusOK)
	r.W.Write(body)
	return nil
}

// ---- Status ----

// untilBody is the two ways a request says when something ends.
type untilBody struct {
	Until           model.Time `json:"until"`
	DurationSeconds *int64     `json:"duration_seconds"`
}

func (u untilBody) until() people.Until {
	return people.Until{At: u.Until, DurationSeconds: u.DurationSeconds}
}

func (a *API) setStatus(r *Request) error {
	var body struct {
		Emoji string `json:"emoji"`
		Text  string `json:"text"`
		untilBody
		PauseNotifications bool   `json:"pause_notifications"`
		AppearAway         bool   `json:"appear_away"`
		PresetID           string `json:"preset_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.PresetID != "" {
		if _, err := BodyID[string]("preset_id", body.PresetID); err != nil {
			return err
		}
	}
	me, err := a.People.SetStatus(r.Ctx(), r.Actor(), people.NewStatus{Emoji: body.Emoji, Text: body.Text, Until: body.until(),
		PauseNotifications: body.PauseNotifications, AppearAway: body.AppearAway, PresetID: body.PresetID})
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"account": me})
}

func (a *API) clearStatus(r *Request) error {
	if err := a.People.ClearStatus(r.Ctx(), r.Actor()); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) setAvailability(r *Request) error {
	var body struct {
		Value string     `json:"value"`
		Until model.Time `json:"until"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	me, err := a.People.SetAvailability(r.Ctx(), r.Actor(), body.Value, body.Until)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"account": me})
}

func (a *API) clearAvailability(r *Request) error {
	if err := a.People.ClearAvailability(r.Ctx(), r.Actor()); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) setDnd(r *Request) error {
	var body untilBody
	if err := r.optional(&body); err != nil {
		return err
	}
	dnd, err := a.People.SetDnd(r.Ctx(), r.Actor(), body.until())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"dnd": dnd})
}

func (a *API) clearDnd(r *Request) error {
	dnd, err := a.People.ClearDnd(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"dnd": dnd})
}

func (a *API) getQuietHours(r *Request) error {
	out := map[string]any{}
	if q := a.People.QuietHours(r.Actor()); q != nil {
		out["quiet_hours"] = q
	}
	return r.JSON(out)
}

func (a *API) setQuietHours(r *Request) error {
	var body struct {
		Start string `json:"start"`
		End   string `json:"end"`
		Days  string `json:"days"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	q, err := a.People.SetQuietHours(r.Ctx(), r.Actor(), body.Start, body.End, body.Days)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"quiet_hours": q})
}

func (a *API) removeQuietHours(r *Request) error {
	if err := a.People.RemoveQuietHours(r.Ctx(), r.Actor()); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) skipQuietHours(r *Request) error {
	q, dnd, err := a.People.SkipQuietHours(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"quiet_hours": q, "dnd": dnd})
}

func (a *API) unskipQuietHours(r *Request) error {
	q, err := a.People.UnskipQuietHours(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"quiet_hours": q})
}

func (a *API) setAway(r *Request) error {
	var body untilBody
	if err := r.optional(&body); err != nil {
		return err
	}
	me, err := a.People.SetAway(r.Ctx(), r.Actor(), body.until())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"account": me})
}

func (a *API) clearAway(r *Request) error {
	if err := a.People.ClearAway(r.Ctx(), r.Actor()); err != nil {
		return err
	}
	return r.NoContent()
}

// presetBody is the contract's StatusPresetInput.
type presetBody struct {
	Emoji              *string `json:"emoji"`
	Text               *string `json:"text"`
	DurationSeconds    *int64  `json:"duration_seconds"`
	PauseNotifications *bool   `json:"pause_notifications"`
	AppearAway         *bool   `json:"appear_away"`
	Availability       *string `json:"availability"`
	Position           *int64  `json:"position"`
}

func (b presetBody) input() people.PresetInput {
	return people.PresetInput{Emoji: b.Emoji, Text: b.Text, DurationSeconds: b.DurationSeconds,
		PauseNotifications: b.PauseNotifications, AppearAway: b.AppearAway, Availability: b.Availability, Position: b.Position}
}

func (a *API) listStatusPresets(r *Request) error {
	presets, err := a.People.Presets(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"presets": orEmpty(presets)})
}

func (a *API) createPreset(r *Request, community bool) error {
	var body presetBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	p, err := a.People.CreatePreset(r.Ctx(), r.Actor(), community, body.input())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"preset": p})
}

func (a *API) updatePreset(r *Request, community bool) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	var body presetBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	p, err := a.People.UpdatePreset(r.Ctx(), r.Actor(), community, id, body.input())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"preset": p})
}

func (a *API) deletePreset(r *Request, community bool) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	if err := a.People.DeletePreset(r.Ctx(), r.Actor(), community, id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) createStatusPreset(r *Request) error    { return a.createPreset(r, false) }
func (a *API) updateStatusPreset(r *Request) error    { return a.updatePreset(r, false) }
func (a *API) deleteStatusPreset(r *Request) error    { return a.deletePreset(r, false) }
func (a *API) createCommunityPreset(r *Request) error { return a.createPreset(r, true) }
func (a *API) updateCommunityPreset(r *Request) error { return a.updatePreset(r, true) }
func (a *API) deleteCommunityPreset(r *Request) error { return a.deletePreset(r, true) }

// ---- Threads ----

func (a *API) listThreads(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	all, err := r.queryBool("all")
	if err != nil {
		return err
	}
	threads, next, err := a.Chat.Threads(r.Ctx(), r.Actor(), all, page)
	if err != nil {
		return err
	}
	return r.JSON(list("threads", orEmpty(threads), next))
}

func (a *API) setThreadState(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		State string `json:"state"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	ts, err := a.Chat.SetThreadState(r.Ctx(), r.Actor(), id, body.State)
	if err != nil {
		return err
	}
	out := map[string]any{}
	if ts != nil {
		out["thread_state"] = ts
	}
	return r.JSON(out)
}

func (a *API) setThreadReadPosition(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Seq *int64 `json:"seq"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Seq == nil {
		return model.Invalid("seq", "Give the seq read up to.")
	}
	ts, err := a.Chat.SetThreadRead(r.Ctx(), r.Actor(), id, *body.Seq)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"thread_state": ts})
}

// ---- Conversations ----

// isNull reports whether a field arrived as JSON null, which for
// muted_until, show and thread_view means something else than leaving it out.
func isNull(raw json.RawMessage) bool { return strings.TrimSpace(string(raw)) == "null" }

func (a *API) updateMembership(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Mode       *model.Mode     `json:"mode"`
		Show       json.RawMessage `json:"show"`
		MutedUntil json.RawMessage `json:"muted_until"`
		Starred    *bool           `json:"starred"`
		ThreadView json.RawMessage `json:"thread_view"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in := chat.MembershipChange{Mode: body.Mode, Starred: body.Starred}
	switch {
	case len(body.MutedUntil) == 0:
	case isNull(body.MutedUntil):
		in.Unmute = true
	default:
		var t model.Time
		if err := json.Unmarshal(body.MutedUntil, &t); err != nil || t.IsZero() {
			return model.Invalid("muted_until", "muted_until is an RFC 3339 time, or null to unmute.")
		}
		in.MutedUntil = &t
	}
	switch {
	case len(body.Show) == 0:
	case isNull(body.Show):
		in.AppDefaultShow = true
	default:
		var v string
		if err := json.Unmarshal(body.Show, &v); err != nil {
			return model.Invalid("show", "show is always, activity, notification, hidden, or null for the app's default.")
		}
		in.Show = &v
	}
	switch {
	case len(body.ThreadView) == 0:
	case isNull(body.ThreadView):
		in.FollowAccountView = true
	default:
		var v string
		if err := json.Unmarshal(body.ThreadView, &v); err != nil {
			return model.Invalid("thread_view", "thread_view is threaded, flat, or null to follow your preference.")
		}
		in.ThreadView = &v
	}
	ms, err := a.Chat.UpdateMembership(r.Ctx(), r.Actor(), id, in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"membership": ms})
}

func (a *API) sendTyping(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		ParentID string `json:"parent_id"`
	}
	if err := r.optional(&body); err != nil {
		return err
	}
	var parent model.MessageID
	if body.ParentID != "" {
		if parent, err = BodyID[model.MessageID]("parent_id", body.ParentID); err != nil {
			return err
		}
	}
	if err := a.Chat.Typing(r.Ctx(), r.Actor(), id, parent); err != nil {
		return err
	}
	return r.NoContent()
}

// ---- Messages ----

func (a *API) notifyAnyway(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		AccountID string `json:"account_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	target, err := BodyID[model.AccountID]("account_id", body.AccountID)
	if err != nil {
		return err
	}
	m, err := a.Chat.NotifyAnyway(r.Ctx(), r.Actor(), id, target)
	if err != nil {
		return err
	}
	// The message with its public mark, and nothing about the target.
	return r.JSON(map[string]any{"message": m})
}

func (a *API) addBookmark(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Chat.AddBookmark(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) removeBookmark(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Chat.RemoveBookmark(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listBookmarks(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	bookmarks, next, err := a.Chat.Bookmarks(r.Ctx(), r.Actor(), page)
	if err != nil {
		return err
	}
	return r.JSON(list("bookmarks", orEmpty(bookmarks), next))
}

// ---- Admin: people ----

func (a *API) adminListAccounts(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	q := r.R.URL.Query()
	f := store.AccountFilter{Query: q.Get("q"), Role: model.Role(q.Get("role")), State: q.Get("state")}
	accounts, next, err := a.People.AdminList(r.Ctx(), r.Actor(), f, page)
	if err != nil {
		return err
	}
	return r.JSON(list("accounts", orEmpty(accounts), next))
}

func (a *API) adminGetAccount(r *Request) error {
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	acc, err := a.People.AdminGet(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"account": acc})
}

func (a *API) setAccountRole(r *Request) error {
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Role model.Role `json:"role"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	acc, err := a.People.SetRole(r.Ctx(), r.Actor(), id, body.Role)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"account": acc})
}

func (a *API) removeAccount(r *Request) error {
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	if err := a.People.Remove(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listBans(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	bans, next, err := a.People.Bans(r.Ctx(), r.Actor(), page)
	if err != nil {
		return err
	}
	return r.JSON(list("bans", orEmpty(bans), next))
}

func (a *API) createBan(r *Request) error {
	var body struct {
		AccountID  string `json:"account_id"`
		ProviderID string `json:"provider_id"`
		Subject    string `json:"subject"`
		Reason     string `json:"reason"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in := people.NewBan{Subject: body.Subject, Reason: body.Reason}
	var err error
	if body.AccountID != "" {
		if in.AccountID, err = BodyID[model.AccountID]("account_id", body.AccountID); err != nil {
			return err
		}
	}
	if body.ProviderID != "" {
		if in.ProviderID, err = BodyID[string]("provider_id", body.ProviderID); err != nil {
			return err
		}
	}
	ban, err := a.People.Ban(r.Ctx(), r.Actor(), in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"ban": ban})
}

func (a *API) liftBan(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	if err := a.People.LiftBan(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

// groupBody is the body of createGroup and updateGroup.
type groupBody struct {
	Handle        *string `json:"handle"`
	Name          *string `json:"name"`
	Description   *string `json:"description"`
	Color         *string `json:"color"`
	Open          *bool   `json:"open"`
	ProviderID    *string `json:"provider_id"`
	ProviderGroup *string `json:"provider_group"`
}

func (b groupBody) input() people.GroupInput {
	return people.GroupInput{Handle: b.Handle, Name: b.Name, Description: b.Description, Color: b.Color, Open: b.Open,
		ProviderID: b.ProviderID, ProviderGroup: b.ProviderGroup}
}

func (a *API) createGroup(r *Request) error {
	var body groupBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	g, err := a.People.CreateGroup(r.Ctx(), r.Actor(), body.input())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"group": g})
}

func (a *API) updateGroup(r *Request) error {
	id, err := PathID[model.GroupID](r, "id")
	if err != nil {
		return err
	}
	var body groupBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	g, err := a.People.UpdateGroup(r.Ctx(), r.Actor(), id, body.input())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"group": g})
}

func (a *API) deleteGroup(r *Request) error {
	id, err := PathID[model.GroupID](r, "id")
	if err != nil {
		return err
	}
	if err := a.People.DeleteGroup(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) groupMember(r *Request, add bool) error {
	id, err := PathID[model.GroupID](r, "id")
	if err != nil {
		return err
	}
	account, err := PathID[model.AccountID](r, "account_id")
	if err != nil {
		return err
	}
	if add {
		err = a.People.AddGroupMember(r.Ctx(), r.Actor(), id, account)
	} else {
		err = a.People.RemoveGroupMember(r.Ctx(), r.Actor(), id, account)
	}
	if err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) addGroupMember(r *Request) error    { return a.groupMember(r, true) }
func (a *API) removeGroupMember(r *Request) error { return a.groupMember(r, false) }

func (a *API) createClaimLink(r *Request) error {
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	inv, err := a.People.ClaimLink(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"invite": inv})
}
