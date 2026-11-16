package model

import "time"

// Limits of watches, "tell me when this person is around" (ruling R76).
const (
	// MaxWatches is how many watches one account may have waiting.
	MaxWatches = 5
	// WatchLifetime is how long a watch waits before it expires unfired.
	WatchLifetime = 48 * time.Hour
	// WatchesPerDay is how many watches one account may ask for in a day,
	// whatever became of them.
	WatchesPerDay = 20
)
