package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Bots is the `bots`, `webhooks` and `commands` tables (docs/SCHEMA.md
// section 9). The account behind a bot or a webhook is a row of `accounts`
// and is made and removed through Accounts; removing it deletes its commands
// and revokes its webhooks there.
type Bots interface {
	// Create stores the bots row for an account whose role is bot.
	Create(ctx context.Context, b BotInfo) error
	// Get returns the bots row of an account. found is false if it has
	// none.
	Get(ctx context.Context, account model.AccountID) (b BotInfo, found bool, err error)
	// SetDescription changes a bot's description. model.ErrNotFound if
	// the account has no bots row.
	SetDescription(ctx context.Context, account model.AccountID, description string) error
	// List returns the bots whose accounts have not been removed, oldest
	// first. The accounts behind webhooks are left out unless webhooks is
	// true.
	List(ctx context.Context, webhooks bool, p Page) (bots []BotInfo, next string, err error)
	// ChannelIDs returns the named channels an account is a member of,
	// by name. Ad-hoc channels and sealed chats are never listed.
	ChannelIDs(ctx context.Context, account model.AccountID) ([]model.ConversationID, error)

	// SetCommands replaces a bot's whole list of commands. A name that
	// another bot holds is model.ErrNameTaken and nothing changes. The
	// caller has checked each name's form and that it is not a built-in.
	SetCommands(ctx context.Context, bot model.AccountID, commands []model.Command) error
	// Commands returns a bot's commands, by name.
	Commands(ctx context.Context, bot model.AccountID) ([]model.Command, error)
	// CommandByName finds a bot command by its name. found is false if no
	// bot has registered it.
	CommandByName(ctx context.Context, name string) (c model.Command, found bool, err error)
	// CommandsIn returns the commands of every bot that is a member of a
	// conversation, by name.
	CommandsIn(ctx context.Context, conv model.ConversationID) ([]model.Command, error)
	// CommunityReplyNamed reports whether a community saved reply (a
	// custom command) has this name, which a bot command may not take.
	CommunityReplyNamed(ctx context.Context, name string) (bool, error)

	// CreateWebhook stores a request for a webhook in state `waiting`. An
	// empty ID and zero RequestedAt are filled in.
	CreateWebhook(ctx context.Context, w *model.Webhook) error
	// Webhook returns one webhook, whatever its state.
	Webhook(ctx context.Context, id model.WebhookID) (model.Webhook, error)
	// WebhookByBot finds the webhook an account stands behind.
	WebhookByBot(ctx context.Context, bot model.AccountID) (w model.Webhook, found bool, err error)
	// WebhookByTokenHash finds the approved webhook whose address holds
	// the token with this hash, with what the intake needs to check a
	// request. model.ErrNotFound for a hash no approved webhook has.
	WebhookByTokenHash(ctx context.Context, tokenHash []byte) (WebhookSecret, error)
	// WebhooksOf returns a conversation's webhooks and requests that are
	// waiting or approved, oldest first.
	WebhooksOf(ctx context.Context, conv model.ConversationID) ([]model.Webhook, error)
	// ListWebhooks returns every webhook on the server, newest first, or
	// only those in one state.
	ListWebhooks(ctx context.Context, state string, p Page) (webhooks []model.Webhook, next string, err error)
	// CountWaiting returns how many requests of one account are waiting.
	CountWaiting(ctx context.Context, requester model.AccountID) (int, error)
	// DecideWebhook moves a waiting request to `approved` or `declined`
	// and records who decided and when. model.ErrConflict if it is not
	// waiting.
	DecideWebhook(ctx context.Context, id model.WebhookID, state string, by model.AccountID, at model.Time) (model.Webhook, error)
	// MarkDeclined puts `declined` back on a request that was declined and
	// whose waiting account was then removed: removing an account revokes
	// its webhooks, and a declined request is not a revoked webhook. It
	// is called in the transaction that declined the request and nowhere
	// else. model.ErrNotFound if the request was never decided.
	MarkDeclined(ctx context.Context, id model.WebhookID) error
	// SetWebhookSecret stores the hash of an approved webhook's token and
	// its sealed signing secret, replacing any it had. With first set,
	// model.ErrConflict if it already has a token: an address is
	// collected once. model.ErrConflict too if it is not approved.
	SetWebhookSecret(ctx context.Context, id model.WebhookID, tokenHash, sealedSigningSecret []byte, first bool) error
	// RevokeWebhook sets a waiting or approved webhook to `revoked` and
	// clears its token hash and signing secret. Revoking one that is
	// already declined or revoked changes nothing. It returns the row as
	// it now is. Removing the account behind it is the caller's.
	RevokeWebhook(ctx context.Context, id model.WebhookID) (model.Webhook, error)
	// TouchWebhook records that a webhook posted.
	TouchWebhook(ctx context.Context, id model.WebhookID, at model.Time) error
	// DeleteDeclinedWebhooks deletes declined requests decided before the
	// given moment, for the janitor, and returns how many went.
	DeleteDeclinedWebhooks(ctx context.Context, before model.Time) (int64, error)

	// SetMessageImages writes a message's cards and the icon of its via
	// label after the images a webhook named by address were fetched.
	// This is not an edit: no version is kept and edited_at stays as it
	// is (docs/BOTS.md section 11). An empty icon leaves the icon alone.
	SetMessageImages(ctx context.Context, msg model.MessageID, cards []model.Card, viaIcon model.FileID) error
}

// BotInfo is one row of `bots`.
type BotInfo struct {
	AccountID   model.AccountID
	Description string
	CreatedBy   model.AccountID
	// WebhookOnly marks the account that stands behind a webhook.
	WebhookOnly bool
}

// WebhookSecret is a webhook with the two stored secrets the intake checks
// a request against. Neither ever leaves the server.
type WebhookSecret struct {
	model.Webhook
	// TokenHash is the SHA-256 hash of the token in the address.
	TokenHash []byte
	// SigningSecret is the sealed signing secret.
	SigningSecret []byte
}
