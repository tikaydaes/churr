package model

// InstantHint is a range of a message's text that the author's client
// resolved to an instant: a phrase such as "this Friday at 5pm" that the
// author accepted in the composer (ruling R69). Start and End count UTF-16
// code units. The server checks each one and stores those that hold up as
// `instant` entities; the others are dropped without an error.
type InstantHint struct {
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Time      string `json:"time"`
	Precision string `json:"precision"`
	Zone      string `json:"zone"`
}
