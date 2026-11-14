package bots

import (
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/ratelimit"
)

// Limits are the rate limits of docs/BOTS.md section 8. They are per bot
// account whichever token is used, per webhook, and per integration.
type Limits struct {
	// RequestsPerMinute bounds a bot's requests of any kind.
	RequestsPerMinute int
	// MessagesPerMinuteInOne and MessagesPerMinute bound the messages a
	// bot posts or edits, in one conversation and in all.
	MessagesPerMinuteInOne int
	MessagesPerMinute      int
	// UploadsPerMinute bounds the files a bot uploads.
	UploadsPerMinute int
	// ReactionsPerMinute bounds the reactions a bot adds or removes.
	ReactionsPerMinute int
	// CommandSetsPerHour bounds how often a bot sets its command list.
	CommandSetsPerHour int
	// Connections is how many live connections a bot may hold at once.
	Connections int
	// WebhookPostsPerMinute bounds the posts through one webhook.
	WebhookPostsPerMinute int
	// ImageFetchesPerMinute bounds the image addresses fetched for one
	// webhook.
	ImageFetchesPerMinute int
	// IntegrationRequestsPerMinute bounds one server integration.
	IntegrationRequestsPerMinute int
}

// DefaultLimits are the numbers of docs/BOTS.md section 8.
func DefaultLimits() Limits {
	return Limits{
		RequestsPerMinute: 600, MessagesPerMinuteInOne: 60, MessagesPerMinute: 300, UploadsPerMinute: 30,
		ReactionsPerMinute: 120, CommandSetsPerHour: 10, Connections: 5, WebhookPostsPerMinute: 60,
		ImageFetchesPerMinute: 30, IntegrationRequestsPerMinute: 600,
	}
}

type limiters struct {
	set                                               Limits
	requests, messagesInOne, messages, uploads        *ratelimit.Limiter
	reactions, commandSets, webhookPosts, imageFetchs *ratelimit.Limiter
	integrations                                      *ratelimit.Limiter
}

func newLimiters(l Limits) *limiters {
	per := func(n int, window time.Duration) *ratelimit.Limiter {
		if n < 1 {
			n = 1
		}
		return ratelimit.New(ratelimit.Per(n, window), n)
	}
	return &limiters{
		set:           l,
		requests:      per(l.RequestsPerMinute, time.Minute),
		messagesInOne: per(l.MessagesPerMinuteInOne, time.Minute),
		messages:      per(l.MessagesPerMinute, time.Minute),
		uploads:       per(l.UploadsPerMinute, time.Minute),
		reactions:     per(l.ReactionsPerMinute, time.Minute),
		commandSets:   per(l.CommandSetsPerHour, time.Hour),
		webhookPosts:  per(l.WebhookPostsPerMinute, time.Minute),
		imageFetchs:   per(l.ImageFetchesPerMinute, time.Minute),
		integrations:  per(l.IntegrationRequestsPerMinute, time.Minute),
	}
}

// limited turns a refused take into the rate_limited error that says when
// to try again.
func limited(r ratelimit.Result) error {
	if r.OK {
		return nil
	}
	return model.RateLimited(max(r.RetryAfterSeconds(), 1))
}

// TakeRequest counts one request of any kind against a bot. The result has
// what the RateLimit-* headers carry.
func (s *Service) TakeRequest(bot model.AccountID) ratelimit.Result {
	return s.limits.requests.Take(string(bot))
}

// AllowMessage counts one message posted or edited by a bot, in one
// conversation and in all. conv may be empty when it is not known.
func (s *Service) AllowMessage(bot model.AccountID, conv model.ConversationID) error {
	if conv != "" {
		if err := limited(s.limits.messagesInOne.Take(string(bot) + "/" + string(conv))); err != nil {
			return err
		}
	}
	return limited(s.limits.messages.Take(string(bot)))
}

// AllowUpload counts one file uploaded by a bot.
func (s *Service) AllowUpload(bot model.AccountID) error {
	return limited(s.limits.uploads.Take(string(bot)))
}

// AllowReaction counts one reaction added or removed by a bot.
func (s *Service) AllowReaction(bot model.AccountID) error {
	return limited(s.limits.reactions.Take(string(bot)))
}

// AllowConnection answers whether a bot that already holds `open` live
// connections may open one more.
func (s *Service) AllowConnection(open int) error {
	if open >= s.limits.set.Connections {
		return model.RateLimited(5).With("This bot already has as many live connections as it may hold.")
	}
	return nil
}

// TakeIntegration counts one request of a server integration. The
// integration operations call it with the integration's identifier.
func (s *Service) TakeIntegration(integration string) ratelimit.Result {
	return s.limits.integrations.Take(integration)
}
