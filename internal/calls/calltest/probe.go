package calltest

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

// Probe does what the admin console's "Test calls" does in a browser: it
// makes an offer, hands it to exchange (which posts it to the server and
// returns the server's answer), and waits for a media connection. It
// reports which of UDP and TCP got through and the server address used.
// With tcpOnly the probe behaves like a network that blocks UDP.
func Probe(tcpOnly bool, exchange func(offer string) (answer string, err error)) (protocol, address string, err error) {
	c := &Client{opts: Options{TCPOnly: tcpOnly}}
	pc, err := c.api().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return "", "", err
	}
	defer pc.Close()
	// A data channel is the least a browser can offer: no camera, no
	// microphone, no permission prompt.
	if _, err := pc.CreateDataChannel("test", nil); err != nil {
		return "", "", err
	}
	connected := make(chan struct{})
	pc.OnICEConnectionStateChange(func(st webrtc.ICEConnectionState) {
		if st == webrtc.ICEConnectionStateConnected || st == webrtc.ICEConnectionStateCompleted {
			select {
			case <-connected:
			default:
				close(connected)
			}
		}
	})
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return "", "", err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return "", "", err
	}
	answer, err := exchange(offer.SDP)
	if err != nil {
		return "", "", err
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		return "", "", fmt.Errorf("the server's answer could not be used: %w", err)
	}
	select {
	case <-connected:
	case <-time.After(10 * time.Second):
		return "", "", errors.New("no media connection was made")
	}
	pair, err := pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair()
	if err != nil || pair == nil {
		return "", "", errors.New("no candidate pair is selected")
	}
	return strings.ToLower(pair.Remote.Protocol.String()), fmt.Sprintf("%s:%d", pair.Remote.Address, pair.Remote.Port), nil
}
