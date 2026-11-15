package link_test

import "testing"

// Ruling R69: an instant entity, resolved on the author's server from the
// zone its client sent, reaches the other server of a shared channel as it
// was stored, though that server never saw the zone.
func TestInstantsCrossTheLink(t *testing.T) {
	s := share(t, 2, false, nil)
	b := s.Servers[1]
	s.ann.Must(201, "POST", "/api/v1/conversations/"+s.conv[0]+"/messages", map[string]any{
		"text": "kick-off 2026-12-24 09:00", "time_zone": "Asia/Tokyo"})
	got := b.Owner.HasMessage(s.conv[1], "kick-off 2026-12-24 09:00")
	ents := entities(got)
	if len(ents) != 1 || ents[0]["type"] != "instant" || ents[0]["time"] != "2026-12-24T00:00:00.000Z" ||
		ents[0]["zone"] != "Asia/Tokyo" || ents[0]["precision"] != "minute" || ents[0]["at"] != float64(9) {
		t.Fatalf("the instant on b: %v", ents)
	}
}
