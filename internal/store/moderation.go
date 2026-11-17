package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Moderation is the `timeouts`, `reports`, `purge_requests` and
// `word_filters` tables. (`bans` belongs to the people area and has its own
// methods there.)
type Moderation interface {
	// CreateTimeout stores a time-out. An empty ID and zero CreatedAt are
	// filled in. An empty ConversationID means the whole server.
	CreateTimeout(ctx context.Context, t *model.Timeout) error
	// ReplaceTimeout is CreateTimeout for a time-out that takes the place of
	// whatever is in force for the same account and the same scope: those
	// rows get lifted_at (docs/FEATURES.md section 15). It returns the rows
	// it lifted.
	ReplaceTimeout(ctx context.Context, t *model.Timeout) (lifted []model.Timeout, err error)
	// GetTimeout returns one time-out.
	GetTimeout(ctx context.Context, id string) (model.Timeout, error)
	// LiftTimeout ends a time-out early.
	LiftTimeout(ctx context.Context, id string, at model.Time) error
	// TimedOutUntil reports whether an account is timed out in a
	// conversation at `now`, by a time-out on that conversation or on the
	// whole server, and until when (the latest end among them). Pass an
	// empty conversation to ask about server-wide time-outs only.
	TimedOutUntil(ctx context.Context, account model.AccountID, conv model.ConversationID, now model.Time) (until model.Time, timedOut bool, err error)
	// TimedOutEverywhere is TimedOutUntil for every conversation at once:
	// the latest end of the account's server-wide time-outs in force at
	// `now` (zero if none), and the same for each conversation that has
	// one of its own. The answer for one conversation is the later of the
	// two.
	TimedOutEverywhere(ctx context.Context, account model.AccountID, now model.Time) (server model.Time, byConversation map[model.ConversationID]model.Time, err error)
	// Timeouts returns the time-outs in force at `now` (not ended, not
	// lifted), newest first.
	Timeouts(ctx context.Context, f TimeoutFilter, now model.Time, p Page) (list []model.Timeout, next string, err error)

	// CreateReport stores a report. An empty ID, zero CreatedAt and empty
	// Source (`person`) are filled in. If the same reporter already has a
	// waiting report on the message (or, for a filter's report, the message
	// already has a waiting one from the filter), that row is returned and
	// created is false.
	CreateReport(ctx context.Context, r *model.Report) (created bool, err error)
	// GetReport returns one report.
	GetReport(ctx context.Context, id string) (model.Report, error)
	// Reports returns reports the scope may handle, newest first.
	Reports(ctx context.Context, f ReportFilter, p Page) (list []model.Report, next string, err error)
	// SettleReports gives every waiting report on a message the same
	// outcome, and returns them as they now are.
	SettleReports(ctx context.Context, msg model.MessageID, outcome string, by model.AccountID, at model.Time) ([]model.Report, error)
	// WaitingReports counts the waiting reports in public channels, which
	// are the ones an admin may handle from outside.
	WaitingReports(ctx context.Context) (int64, error)

	// CreatePurgeRequest stores a waiting request. An empty ID and zero
	// RequestedAt are filled in.
	CreatePurgeRequest(ctx context.Context, p *model.PurgeRequest) error
	// GetPurgeRequest returns one request.
	GetPurgeRequest(ctx context.Context, id string) (model.PurgeRequest, error)
	// LatestPurgeRequest returns the newest request for a message. found is
	// false if there has never been one.
	LatestPurgeRequest(ctx context.Context, msg model.MessageID) (p model.PurgeRequest, found bool, err error)
	// PurgeRequests returns requests, newest first; only the waiting ones
	// if asked.
	PurgeRequests(ctx context.Context, waiting bool, p Page) (list []model.PurgeRequest, next string, err error)
	// WaitingPurgeRequestsIn returns the waiting requests for messages of
	// one conversation.
	WaitingPurgeRequestsIn(ctx context.Context, conv model.ConversationID) ([]model.PurgeRequest, error)
	// DecidePurgeRequest records an outcome on a waiting request. by is ""
	// when the server decided (the channel went private). model.ErrConflict
	// if it was already decided.
	DecidePurgeRequest(ctx context.Context, id, outcome string, by model.AccountID, at model.Time) (model.PurgeRequest, error)
	// WaitingPurgeRequests counts the requests no admin has decided.
	WaitingPurgeRequests(ctx context.Context) (int64, error)

	// WordFilters returns the whole list, oldest first.
	WordFilters(ctx context.Context) ([]model.WordFilter, error)
	// GetWordFilter returns one entry.
	GetWordFilter(ctx context.Context, id string) (model.WordFilter, error)
	// CreateWordFilter stores an entry. An empty ID and zero CreatedAt are
	// filled in.
	CreateWordFilter(ctx context.Context, f *model.WordFilter) error
	// UpdateWordFilter changes an entry's pattern, kind and action.
	UpdateWordFilter(ctx context.Context, f model.WordFilter) error
	// DeleteWordFilter removes an entry.
	DeleteWordFilter(ctx context.Context, id string) error

	// BanFromChannel stores a channel ban (docs/FEATURES.md section 19.1)
	// in place of any in force for the same account and channel, which get
	// lifted_at. An empty ID and zero CreatedAt are filled in. It does not
	// touch the membership: the caller takes the account out after, and
	// from then on working out its membership there gives none.
	BanFromChannel(ctx context.Context, b *model.ChannelBan) error
	// LiftChannelBan lifts the ban in force on an account in a channel and
	// returns it. found is false when there is none.
	LiftChannelBan(ctx context.Context, conv model.ConversationID, account model.AccountID, at model.Time) (b model.ChannelBan, found bool, err error)
	// ChannelBan returns the ban in force at now on an account in a
	// channel; found is false when there is none.
	ChannelBan(ctx context.Context, conv model.ConversationID, account model.AccountID, now model.Time) (b model.ChannelBan, found bool, err error)
	// ChannelBans returns the bans in force at now, newest first: in one
	// channel, or with an empty conv in every named channel (the admin
	// console's list).
	ChannelBans(ctx context.Context, conv model.ConversationID, now model.Time) ([]model.ChannelBan, error)
	// ChannelBansOf returns the channels an account is banned from at now.
	ChannelBansOf(ctx context.Context, account model.AccountID, now model.Time) ([]model.ConversationID, error)
}

// TimeoutFilter narrows Moderation.Timeouts.
type TimeoutFilter struct {
	// ConversationID keeps the time-outs set in this conversation.
	ConversationID model.ConversationID
	// AdminView keeps server-wide time-outs and those in public channels,
	// which is what the admin console lists. ConversationID is then ignored.
	AdminView bool
}

// ReportScope says whose reports an account may handle, in a form the query
// can join against. perm.ReportScope builds it; nothing else should.
type ReportScope struct {
	// AccountID: reports in conversations where this account's level is
	// moderator or manager.
	AccountID model.AccountID
	// PublicChannels: reports in every public channel too. Set for admins.
	PublicChannels bool
}

// ReportFilter narrows Moderation.Reports.
type ReportFilter struct {
	Scope ReportScope
	// ConversationID keeps one conversation.
	ConversationID model.ConversationID
	// Waiting keeps only reports nobody has handled.
	Waiting bool
}
