// Package calls is the media server: call rooms, signalling over the live
// connection, and the selective forwarding unit that carries audio, video
// and screen sharing (docs/CALLS.md). It is the only package that imports
// Pion.
package calls

import (
	_ "github.com/pion/interceptor"
	_ "github.com/pion/rtcp"
	_ "github.com/pion/rtp"
	_ "github.com/pion/webrtc/v4"
)
