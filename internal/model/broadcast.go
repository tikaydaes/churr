package model

// Server broadcasts (docs/FEATURES.md section 12.6, ruling R74): a notice
// from an admin to everyone connected, shown as a banner for a while. It is
// not a message, is kept nowhere but in memory and the audit log, and does
// not cross to linked servers.

// EventServerBroadcast says the broadcast showing changed: a new one, or
// none (cleared). To every connection.
const EventServerBroadcast = "server.broadcast"

// Limits of a broadcast.
const (
	BroadcastDefaultSeconds = 600
	BroadcastMinSeconds     = 60
	BroadcastMaxSeconds     = 86400
	MaxBroadcastText        = 500
)

// Broadcast is the notice showing (schema Broadcast).
type Broadcast struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	ByID      AccountID `json:"by_id"`
	CreatedAt Time      `json:"created_at"`
	Until     Time      `json:"until"`
}
