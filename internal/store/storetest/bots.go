package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The bots area beyond the bots row itself (which small.go tests):
// listing, commands and webhooks.

func init() {
	register("bots_commands", testBotCommands)
	register("bots_webhooks", testBotWebhooks)
}

// botsNewBot makes a bot account with its bots row.
func botsNewBot(t testing.TB, s store.Areas, username string, by model.AccountID, webhookOnly bool) model.Account {
	t.Helper()
	a := newAccount(t, s, username, model.RoleBot)
	must(t, s.Bots().Create(ctx, store.BotInfo{AccountID: a.ID, Description: "about " + username, CreatedBy: by, WebhookOnly: webhookOnly}))
	return a
}

func testBotCommands(t *testing.T, s store.Store) {
	admin := newAccount(t, s, "admin", model.RoleOwner)
	one := botsNewBot(t, s, "one", admin.ID, false)
	two := botsNewBot(t, s, "two", admin.ID, false)
	hook := botsNewBot(t, s, "hook-abcdefgh", admin.ID, true)

	// Listing: oldest first, webhooks only when asked, removed bots never.
	list, next, err := s.Bots().List(ctx, false, store.Page{})
	must(t, err)
	eq(t, "bots without webhooks", len(list) == 2 && list[0].AccountID == one.ID && list[1].AccountID == two.ID && next == "", true)
	list, next, err = s.Bots().List(ctx, true, store.Page{Limit: 2})
	must(t, err)
	eq(t, "first page", len(list) == 2 && next != "", true)
	list, next, err = s.Bots().List(ctx, true, store.Page{Limit: 2, Cursor: next})
	must(t, err)
	eq(t, "second page", len(list) == 1 && list[0].AccountID == hook.ID && list[0].WebhookOnly && next == "", true)
	_, _, err = s.Bots().List(ctx, true, store.Page{Cursor: "nonsense"})
	wantErr(t, err, model.Invalid("cursor", ""))

	must(t, s.Bots().SetDescription(ctx, one.ID, "Deploys things"))
	got, _, _ := s.Bots().Get(ctx, one.ID)
	eq(t, "description", got.Description, "Deploys things")
	wantErr(t, s.Bots().SetDescription(ctx, admin.ID, "x"), model.ErrNotFound)

	// Channels: named ones only.
	ops := newChannel(t, s, "ops", false, admin.ID)
	alpha := newChannel(t, s, "alpha", true, admin.ID)
	join(t, s, ops.ID, one.ID, model.LevelMember)
	join(t, s, alpha.ID, one.ID, model.LevelReader)
	newAdhoc(t, s, admin.ID, one.ID)
	ids, err := s.Bots().ChannelIDs(ctx, one.ID)
	must(t, err)
	eq(t, "named channels by name", len(ids) == 2 && ids[0] == alpha.ID && ids[1] == ops.ID, true)

	// Commands: the whole list at once.
	must(t, s.Bots().SetCommands(ctx, one.ID, []model.Command{
		{Name: "deploy", Description: "Deploy a service", Hint: "<service> to <environment>"},
		{Name: "build", Description: "Build"},
	}))
	cmds, err := s.Bots().Commands(ctx, one.ID)
	must(t, err)
	eq(t, "by name", len(cmds) == 2 && cmds[0].Name == "build" && cmds[1].Name == "deploy", true)
	eq(t, "what a command carries", cmds[1], model.Command{Name: "deploy", Kind: model.CommandBot, Description: "Deploy a service",
		Hint: "<service> to <environment>", BotID: one.ID})

	// Another bot cannot take a name, and its own list is left alone.
	must(t, s.Bots().SetCommands(ctx, two.ID, []model.Command{{Name: "kudos", Description: "Thanks"}}))
	wantErr(t, s.Bots().SetCommands(ctx, two.ID, []model.Command{{Name: "roll", Description: "Dice"}, {Name: "deploy", Description: "Mine now"}}), model.ErrNameTaken)
	cmds, _ = s.Bots().Commands(ctx, two.ID)
	eq(t, "a refused list changes nothing", len(cmds) == 1 && cmds[0].Name == "kudos", true)

	// The same list again is fine, and a new list replaces the old.
	must(t, s.Bots().SetCommands(ctx, one.ID, []model.Command{{Name: "deploy", Description: "Deploy, reworded"}}))
	cmds, _ = s.Bots().Commands(ctx, one.ID)
	eq(t, "replaced", len(cmds) == 1 && cmds[0].Description == "Deploy, reworded" && cmds[0].Hint == "", true)
	must(t, s.Bots().SetCommands(ctx, two.ID, []model.Command{{Name: "build", Description: "Freed by the other bot"}}))

	c, found, err := s.Bots().CommandByName(ctx, "deploy")
	must(t, err)
	eq(t, "found by name", found && c.BotID == one.ID, true)
	_, found, err = s.Bots().CommandByName(ctx, "nothing")
	must(t, err)
	eq(t, "no such command", found, false)

	// Commands offered in a conversation are those of the bots in it.
	in, err := s.Bots().CommandsIn(ctx, ops.ID)
	must(t, err)
	eq(t, "only bots that are members", len(in) == 1 && in[0].Name == "deploy", true)
	join(t, s, ops.ID, two.ID, model.LevelMember)
	in, _ = s.Bots().CommandsIn(ctx, ops.ID)
	eq(t, "both bots", len(in) == 2 && in[0].Name == "build" && in[1].Name == "deploy", true)

	taken, err := s.Bots().CommunityReplyNamed(ctx, "faq")
	must(t, err)
	eq(t, "no community reply yet", taken, false)

	// An empty list removes them all.
	must(t, s.Bots().SetCommands(ctx, one.ID, nil))
	cmds, _ = s.Bots().Commands(ctx, one.ID)
	eq(t, "emptied", len(cmds), 0)
	wantErr(t, s.Bots().SetCommands(ctx, admin.ID, nil), model.ErrNotFound)

	// Removing the bot takes its commands and drops it from the list.
	_, err = s.Accounts().Remove(ctx, two.ID, admin.ID, model.Now())
	must(t, err)
	_, found, _ = s.Bots().CommandByName(ctx, "build")
	eq(t, "commands go with the bot", found, false)
	list, _, _ = s.Bots().List(ctx, false, store.Page{})
	eq(t, "a removed bot is not listed", len(list) == 1 && list[0].AccountID == one.ID, true)
}

func testBotWebhooks(t *testing.T, s store.Store) {
	admin := newAccount(t, s, "admin", model.RoleOwner)
	ann := newAccount(t, s, "ann", model.RoleMember)
	ops := newChannel(t, s, "ops", false, admin.ID)
	other := newChannel(t, s, "other", false, admin.ID)

	request := func(conv model.ConversationID, name string, at model.Time) model.Webhook {
		t.Helper()
		bot := botsNewBot(t, s, "hook-"+name, ann.ID, true)
		w := model.Webhook{ConversationID: conv, BotID: bot.ID, Name: name, Purpose: "for " + name, RequestedByID: ann.ID, RequestedAt: at}
		must(t, s.Bots().CreateWebhook(ctx, &w))
		return w
	}
	ci := request(ops.ID, "ci", 1000)
	eq(t, "filled in", ci.ID != "" && ci.State == model.WebhookWaiting, true)
	alerts := request(ops.ID, "alerts", 2000)
	jira := request(other.ID, "jira", 3000)

	n, err := s.Bots().CountWaiting(ctx, ann.ID)
	must(t, err)
	eq(t, "three waiting", n, 3)

	got, err := s.Bots().Webhook(ctx, ci.ID)
	must(t, err)
	eq(t, "read back", got, ci)
	_, err = s.Bots().Webhook(ctx, model.NewID[model.WebhookID]())
	wantErr(t, err, model.ErrNotFound)
	byBot, found, err := s.Bots().WebhookByBot(ctx, alerts.BotID)
	must(t, err)
	eq(t, "by its account", found && byBot.ID == alerts.ID, true)
	_, found, _ = s.Bots().WebhookByBot(ctx, ann.ID)
	eq(t, "an ordinary account stands behind none", found, false)

	// A waiting webhook has no address, and cannot be given one.
	wantErr(t, s.Bots().SetWebhookSecret(ctx, ci.ID, []byte("hash-1"), []byte("sealed-1"), true), model.ErrConflict)

	// Deciding happens once.
	approved, err := s.Bots().DecideWebhook(ctx, ci.ID, model.WebhookApproved, admin.ID, 5000)
	must(t, err)
	eq(t, "approved", approved.State == model.WebhookApproved && approved.DecidedByID == admin.ID && approved.DecidedAt == 5000 && !approved.HasSecret, true)
	_, err = s.Bots().DecideWebhook(ctx, ci.ID, model.WebhookDeclined, admin.ID, 6000)
	wantErr(t, err, model.ErrConflict)
	_, err = s.Bots().DecideWebhook(ctx, model.NewID[model.WebhookID](), model.WebhookApproved, admin.ID, 6000)
	wantErr(t, err, model.ErrNotFound)
	declined, err := s.Bots().DecideWebhook(ctx, jira.ID, model.WebhookDeclined, admin.ID, 7000)
	must(t, err)
	eq(t, "declined", declined.State, model.WebhookDeclined)
	n, _ = s.Bots().CountWaiting(ctx, ann.ID)
	eq(t, "one still waiting", n, 1)

	// Until it is collected nothing finds it by a token.
	_, err = s.Bots().WebhookByTokenHash(ctx, []byte("hash-1"))
	wantErr(t, err, model.ErrNotFound)
	_, err = s.Bots().WebhookByTokenHash(ctx, nil)
	wantErr(t, err, model.ErrNotFound)

	// Collected once; a reset replaces both secrets.
	must(t, s.Bots().SetWebhookSecret(ctx, ci.ID, []byte("hash-1"), []byte("sealed-1"), true))
	wantErr(t, s.Bots().SetWebhookSecret(ctx, ci.ID, []byte("hash-2"), []byte("sealed-2"), true), model.ErrConflict)
	sec, err := s.Bots().WebhookByTokenHash(ctx, []byte("hash-1"))
	must(t, err)
	eq(t, "found by its token", sec.ID == ci.ID && sec.HasSecret && string(sec.TokenHash) == "hash-1" && string(sec.SigningSecret) == "sealed-1", true)
	must(t, s.Bots().SetWebhookSecret(ctx, ci.ID, []byte("hash-2"), []byte("sealed-2"), false))
	_, err = s.Bots().WebhookByTokenHash(ctx, []byte("hash-1"))
	wantErr(t, err, model.ErrNotFound)
	sec, err = s.Bots().WebhookByTokenHash(ctx, []byte("hash-2"))
	must(t, err)
	eq(t, "the new secrets", string(sec.SigningSecret), "sealed-2")

	must(t, s.Bots().TouchWebhook(ctx, ci.ID, 9000))
	got, _ = s.Bots().Webhook(ctx, ci.ID)
	eq(t, "last used", got.LastUsedAt, model.Time(9000))

	// A channel lists what is waiting or approved, oldest first.
	of, err := s.Bots().WebhooksOf(ctx, ops.ID)
	must(t, err)
	eq(t, "the channel's webhooks", len(of) == 2 && of[0].ID == ci.ID && of[1].ID == alerts.ID, true)
	of, _ = s.Bots().WebhooksOf(ctx, other.ID)
	eq(t, "a declined request is not listed", len(of), 0)

	// The server-wide list: newest first, by state, in pages.
	all, next, err := s.Bots().ListWebhooks(ctx, "", store.Page{Limit: 2})
	must(t, err)
	eq(t, "newest first", len(all) == 2 && all[0].ID == jira.ID && all[1].ID == alerts.ID && next != "", true)
	all, next, err = s.Bots().ListWebhooks(ctx, "", store.Page{Limit: 2, Cursor: next})
	must(t, err)
	eq(t, "the rest", len(all) == 1 && all[0].ID == ci.ID && next == "", true)
	all, _, _ = s.Bots().ListWebhooks(ctx, model.WebhookWaiting, store.Page{})
	eq(t, "by state", len(all) == 1 && all[0].ID == alerts.ID, true)

	// Revoking clears the secrets, and is safe to repeat.
	revoked, err := s.Bots().RevokeWebhook(ctx, ci.ID)
	must(t, err)
	eq(t, "revoked", revoked.State == model.WebhookRevoked && !revoked.HasSecret, true)
	_, err = s.Bots().WebhookByTokenHash(ctx, []byte("hash-2"))
	wantErr(t, err, model.ErrNotFound)
	again, err := s.Bots().RevokeWebhook(ctx, ci.ID)
	must(t, err)
	eq(t, "still revoked", again.State, model.WebhookRevoked)
	still, err := s.Bots().RevokeWebhook(ctx, jira.ID)
	must(t, err)
	eq(t, "a declined request stays declined", still.State, model.WebhookDeclined)
	_, err = s.Bots().RevokeWebhook(ctx, model.NewID[model.WebhookID]())
	wantErr(t, err, model.ErrNotFound)

	// Removing the account behind a webhook revokes it.
	_, err = s.Accounts().Remove(ctx, alerts.BotID, admin.ID, model.Now())
	must(t, err)
	got, _ = s.Bots().Webhook(ctx, alerts.ID)
	eq(t, "revoked with its account", got.State, model.WebhookRevoked)

	// Declining removes the waiting account, which revokes its webhooks;
	// the request is then marked declined again.
	later := request(other.ID, "later", 8000)
	_, err = s.Bots().DecideWebhook(ctx, later.ID, model.WebhookDeclined, admin.ID, 8500)
	must(t, err)
	_, err = s.Accounts().Remove(ctx, later.BotID, admin.ID, model.Now())
	must(t, err)
	got, _ = s.Bots().Webhook(ctx, later.ID)
	eq(t, "removal revoked it", got.State, model.WebhookRevoked)
	must(t, s.Bots().MarkDeclined(ctx, later.ID))
	got, _ = s.Bots().Webhook(ctx, later.ID)
	eq(t, "declined again", got.State, model.WebhookDeclined)
	withdrawn := request(other.ID, "withdrawn", 8600)
	_, err = s.Bots().RevokeWebhook(ctx, withdrawn.ID)
	must(t, err)
	wantErr(t, s.Bots().MarkDeclined(ctx, withdrawn.ID), model.ErrNotFound)

	// The janitor's sweep of old declined requests.
	gone, err := s.Bots().DeleteDeclinedWebhooks(ctx, 6999)
	must(t, err)
	eq(t, "too recent to delete", gone, int64(0))
	gone, err = s.Bots().DeleteDeclinedWebhooks(ctx, 7001)
	must(t, err)
	eq(t, "deleted", gone, int64(1))

	// Images fetched after posting are written without an edit.
	hookBot := botsNewBot(t, s, "hook-images", ann.ID, true)
	join(t, s, ops.ID, hookBot.ID, model.LevelMember)
	m := post(t, s, ops.ID, hookBot.ID, "", "Build failed")
	file := model.NewID[model.FileID]()
	must(t, s.Bots().SetMessageImages(ctx, m.ID, []model.Card{{Title: "Build", ImageFileID: file}}, file))
	back, err := s.Messages().Get(ctx, m.ID)
	must(t, err)
	eq(t, "cards written", len(back.Cards) == 1 && back.Cards[0].ImageFileID == file, true)
	eq(t, "not an edit", back.EditedAt.IsZero() && back.Version == m.Version, true)
	wantErr(t, s.Bots().SetMessageImages(ctx, model.NewID[model.MessageID](), nil, ""), model.ErrNotFound)
}
