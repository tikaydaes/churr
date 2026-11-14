package calls

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/interceptor/pkg/report"
	"github.com/pion/interceptor/pkg/twcc"
	"github.com/pion/webrtc/v4"
)

// DefaultPort is the media port when none is configured (ruling R26).
const DefaultPort = 24877

// Config says where call media is carried (docs/CALLS.md section 5).
type Config struct {
	// Listen is the address the media port listens on, UDP and TCP both,
	// such as ":24877". Empty runs without calls.
	Listen string
	// Addresses are the public IP addresses, or host names, that clients
	// are told to send media to. Empty means whatever Hostname resolves
	// to.
	Addresses []string
	// Hostname is the server's own host name.
	Hostname string
}

// initialEstimateKbps is where Pion's estimator starts. Its target is not
// what the layer picker goes by (downlink.go says why); it has to start
// somewhere.
const initialEstimateKbps = 2000

// addressesFor is how long a host name's addresses are kept.
const addressesFor = 10 * time.Minute

// The codecs the server takes and offers (docs/CALLS.md section 6): Opus,
// then VP8 before H.264 so VP8 is what clients publish unless they cannot.
const (
	codecOpus = "opus"
	codecVP8  = "vp8"
	codecH264 = "h264"

	audioLevelURI = "urn:ietf:params:rtp-hdrext:ssrc-audio-level"
)

var videoFeedback = []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}, {Type: "ccm", Parameter: "fir"}, {Type: "transport-cc"}}

var (
	opusCodec = webrtc.RTPCodecParameters{PayloadType: 111, RTPCodecCapability: webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
		// In-band error correction and silence suppression on; one channel.
		SDPFmtpLine:  "minptime=10;useinbandfec=1;usedtx=1;stereo=0",
		RTCPFeedback: []webrtc.RTCPFeedback{{Type: "transport-cc"}}}}
	vp8Codec = webrtc.RTPCodecParameters{PayloadType: 96, RTPCodecCapability: webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, RTCPFeedback: videoFeedback}}
	h264Codec = webrtc.RTPCodecParameters{PayloadType: 102, RTPCodecCapability: webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeH264, ClockRate: 90000, RTCPFeedback: videoFeedback,
		// Constrained baseline, packetisation mode 1.
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}}
)

// codecName is the short name the API uses for a negotiated codec.
func codecName(mime string) string {
	switch strings.ToLower(mime) {
	case strings.ToLower(webrtc.MimeTypeOpus):
		return codecOpus
	case strings.ToLower(webrtc.MimeTypeVP8):
		return codecVP8
	case strings.ToLower(webrtc.MimeTypeH264):
		return codecH264
	}
	return ""
}

// codecFor is the server's own parameters for a codec name.
func codecFor(name string) (webrtc.RTPCodecParameters, bool) {
	switch name {
	case codecOpus:
		return opusCodec, true
	case codecVP8:
		return vp8Codec, true
	case codecH264:
		return h264Codec, true
	}
	return webrtc.RTPCodecParameters{}, false
}

// media is the media port and the two kinds of peer connection made on it.
// Every call and every participant shares the one port: packets are told
// apart by the ICE user names inside them.
type media struct {
	cfg  Config
	log  *slog.Logger
	udp  net.PacketConn
	tcp  net.Listener
	port int
	// The two muxes that share the port among every connection.
	udpMux ice.UDPMux
	tcpMux ice.TCPMux

	// sendAPI makes the connections clients publish on; recvAPI the ones
	// the server sends on, which estimate each subscriber's downlink.
	sendAPI *webrtc.API
	recvAPI *webrtc.API

	// recvMu makes creating a recv connection and collecting its
	// estimator one step.
	recvMu     sync.Mutex
	estimators chan cc.BandwidthEstimator

	addrMu      sync.Mutex
	addrs       []netip.Addr
	addrsAt     time.Time
	lookup      func(ctx context.Context, host string) ([]netip.Addr, error)
	initialKbps int
}

// openMedia opens the media port: one number, on UDP and on TCP.
func openMedia(cfg Config, log *slog.Logger, lost time.Duration) (*media, error) {
	host, portText, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("the media address %q is not host:port: %w", cfg.Listen, err)
	}
	want, err := strconv.Atoi(portText)
	if err != nil {
		return nil, fmt.Errorf("the media port %q is not a number", portText)
	}
	m := &media{cfg: cfg, log: log, estimators: make(chan cc.BandwidthEstimator, 1), initialKbps: initialEstimateKbps}
	m.lookup = func(ctx context.Context, h string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", h)
	}
	// Port 0 asks the system for a spare one, as tests do. The TCP side
	// must then get the same number, which another program may hold; so a
	// few are tried.
	for try := 0; ; try++ {
		udp, err := net.ListenPacket("udp", net.JoinHostPort(host, strconv.Itoa(want)))
		if err != nil {
			return nil, fmt.Errorf("listening for call media on UDP %s: %w", cfg.Listen, err)
		}
		port := udp.LocalAddr().(*net.UDPAddr).Port
		tcp, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			udp.Close()
			if want == 0 && try < 20 {
				continue
			}
			return nil, fmt.Errorf("listening for call media on TCP port %d: %w", port, err)
		}
		m.udp, m.tcp, m.port = udp, tcp, port
		break
	}

	logs := pionLogs{log: log}
	m.udpMux = webrtc.NewICEUDPMux(logs.NewLogger("ice"), m.udp)
	m.tcpMux = webrtc.NewICETCPMux(logs.NewLogger("ice"), m.tcp, 64)
	se := webrtc.SettingEngine{LoggerFactory: logs}
	// ICE-lite: the server answers connectivity checks and never sends
	// one to an address a client named.
	se.SetLite(true)
	se.SetICEUDPMux(m.udpMux)
	se.SetICETCPMux(m.tcpMux)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6, webrtc.NetworkTypeTCP4, webrtc.NetworkTypeTCP6})
	// So there is always a local candidate to answer checks from, even on
	// a host whose only address is the loopback one. What clients are
	// told is written into each SDP by candidates(), not gathered.
	se.SetIncludeLoopbackCandidate(true)
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled) // nothing is announced on the local network
	se.DisableActiveTCP(true)
	// A connection that hears nothing for this long is disconnected, and
	// failed five times as long after that. A client restarts ICE well
	// before then (docs/CALLS.md section 11).
	se.SetICETimeouts(lost, 5*lost, 2*time.Second)

	if m.sendAPI, err = newAPI(se, logs, func(me *webrtc.MediaEngine, ir *interceptor.Registry) error {
		// Feedback about what arrives, so publishers can estimate
		// their uplink.
		return webrtc.ConfigureTWCCSenderWithOptions(me, ir, twcc.WithLoggerFactory(logs))
	}); err != nil {
		m.close()
		return nil, err
	}
	if m.recvAPI, err = newAPI(se, logs, func(me *webrtc.MediaEngine, ir *interceptor.Registry) error {
		// A send-side view of each subscriber's downlink, from
		// transport-wide feedback: whether delay is building up and how
		// much is lost (downlink.go). It only watches: what is sent is
		// chosen by the layer picker, so nothing is paced or queued.
		factory, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
			return gcc.NewSendSideBWE(gcc.SendSideBWEInitialBitrate(m.initialKbps*1000), gcc.SendSideBWEMinBitrate(100_000),
				gcc.SendSideBWEMaxBitrate(50_000_000), gcc.SendSideBWEPacer(gcc.NewNoOpPacer()), gcc.WithLoggerFactory(logs))
		})
		if err != nil {
			return err
		}
		factory.OnNewPeerConnection(func(_ string, e cc.BandwidthEstimator) { m.estimators <- e })
		ir.Add(factory)
		// Added after the estimator, so it runs before it on the way
		// out: the estimator needs the number this one writes on each
		// packet.
		return webrtc.ConfigureTWCCHeaderExtensionSender(me, ir)
	}); err != nil {
		m.close()
		return nil, err
	}
	return m, nil
}

// newAPI builds a Pion API with the server's codecs and the interceptors
// both kinds of connection need, plus what more adds.
func newAPI(se webrtc.SettingEngine, logs pionLogs, more func(*webrtc.MediaEngine, *interceptor.Registry) error) (*webrtc.API, error) {
	me := &webrtc.MediaEngine{}
	if err := me.RegisterCodec(opusCodec, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	for _, c := range []webrtc.RTPCodecParameters{vp8Codec, h264Codec} {
		if err := me.RegisterCodec(c, webrtc.RTPCodecTypeVideo); err != nil {
			return nil, err
		}
	}
	if err := me.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: audioLevelURI}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	ir := &interceptor.Registry{}
	// Lost packets: the server asks a publisher again for what it missed,
	// and answers a subscriber's request from its own short buffer, so
	// one subscriber's loss never reaches the publisher.
	if err := webrtc.ConfigureNackWithOptions(me, ir, []nack.GeneratorOption{nack.WithGeneratorLoggerFactory(logs)},
		nack.WithResponderLoggerFactory(logs)); err != nil {
		return nil, err
	}
	if err := webrtc.ConfigureRTCPReportsWithOptions(ir, []report.ReceiverOption{report.WithReceiverLoggerFactory(logs)},
		report.WithSenderLoggerFactory(logs)); err != nil {
		return nil, err
	}
	if err := webrtc.ConfigureSimulcastExtensionHeaders(me); err != nil {
		return nil, err
	}
	if err := more(me, ir); err != nil {
		return nil, err
	}
	return webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(me), webrtc.WithInterceptorRegistry(ir)), nil
}

// close stops listening.
func (m *media) close() {
	// The muxes first: they stop reading before the sockets go.
	if m.udpMux != nil {
		m.udpMux.Close()
	}
	if m.tcpMux != nil {
		m.tcpMux.Close()
	}
	if m.udp != nil {
		m.udp.Close()
	}
	if m.tcp != nil {
		m.tcp.Close()
	}
}

// newSend makes a connection a client publishes on.
func (m *media) newSend() (*webrtc.PeerConnection, error) {
	return m.sendAPI.NewPeerConnection(webrtc.Configuration{})
}

// newRecv makes a connection the server sends on, with the estimator of
// that subscriber's downlink.
func (m *media) newRecv() (*webrtc.PeerConnection, cc.BandwidthEstimator, error) {
	m.recvMu.Lock()
	defer m.recvMu.Unlock()
	pc, err := m.recvAPI.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, err
	}
	select {
	case e := <-m.estimators:
		return pc, e, nil
	case <-time.After(time.Second):
		return pc, nil, nil
	}
}

// addresses returns the IP addresses clients are told to send media to: the
// configured ones, with host names looked up, or else what the server's own
// host name resolves to. A lookup is kept for ten minutes.
func (m *media) addresses(ctx context.Context) []netip.Addr {
	m.addrMu.Lock()
	defer m.addrMu.Unlock()
	if m.addrs != nil && time.Since(m.addrsAt) < addressesFor {
		return m.addrs
	}
	names := m.cfg.Addresses
	if len(names) == 0 && m.cfg.Hostname != "" {
		names = []string{m.cfg.Hostname}
	}
	var out []netip.Addr
	seen := map[netip.Addr]bool{}
	add := func(a netip.Addr) {
		a = a.Unmap()
		if a.IsValid() && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, name := range names {
		if a, err := netip.ParseAddr(name); err == nil {
			add(a)
			continue
		}
		lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		found, err := m.lookup(lctx, name)
		cancel()
		if err != nil {
			m.log.Warn("the media address could not be looked up; calls may not connect", "name", name, "err", err)
			continue
		}
		// IPv4 first, each family in a steady order.
		sort.Slice(found, func(i, j int) bool {
			a, b := found[i].Unmap(), found[j].Unmap()
			if a.Is4() != b.Is4() {
				return a.Is4()
			}
			return a.Less(b)
		})
		for _, a := range found {
			add(a)
		}
	}
	if len(out) == 0 && m.addrs != nil {
		// A failed lookup does not forget addresses that worked.
		return m.addrs
	}
	m.addrs, m.addrsAt = out, time.Now()
	return out
}

// address is the first media address with the port, as the admin console
// shows it: "203.0.113.7:24877". Empty when no address is known.
func (m *media) address(ctx context.Context) string {
	addrs := m.addresses(ctx)
	if len(addrs) == 0 {
		return ""
	}
	return netip.AddrPortFrom(addrs[0], uint16(m.port)).String()
}

// candidates rewrites the candidate lines of an SDP the server is about to
// send: whatever was gathered is replaced by the server's media addresses,
// each on UDP and on TCP at the one port. The server's candidates are fixed
// and are in every SDP it sends; it never sends one by itself.
func (m *media) candidates(ctx context.Context, sdp string) string {
	return withCandidates(sdp, m.addresses(ctx), m.port)
}

func withCandidates(sdp string, addrs []netip.Addr, port int) string {
	var lines []string
	for i, a := range addrs {
		// Earlier addresses are preferred; UDP before TCP for each.
		pref := 65535 - i
		udp := 126<<24 | pref<<8 | 255
		tcp := 90<<24 | pref<<8 | 255
		lines = append(lines,
			fmt.Sprintf("a=candidate:%d 1 udp %d %s %d typ host", 1000+i, udp, a, port),
			fmt.Sprintf("a=candidate:%d 1 tcp %d %s %d typ host tcptype passive", 2000+i, tcp, a, port))
	}
	lines = append(lines, "a=end-of-candidates")
	var out []string
	for _, l := range strings.Split(strings.TrimRight(sdp, "\r\n"), "\r\n") {
		if strings.HasPrefix(l, "a=candidate:") || l == "a=end-of-candidates" {
			continue
		}
		out = append(out, l)
		// The candidates follow each media section's ICE password,
		// which every section carries.
		if strings.HasPrefix(l, "a=ice-pwd:") {
			out = append(out, lines...)
		}
	}
	return strings.Join(out, "\r\n") + "\r\n"
}

// withoutCandidates removes the candidates, and any claim to be an ICE-lite
// agent, from an SDP a client sent. As an ICE-lite agent the server learns
// the working path from the client's checks; it must never learn an address
// to send to from something a member wrote.
func withoutCandidates(sdp string) string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(strings.ReplaceAll(sdp, "\r\n", "\n"), "\n"), "\n") {
		if strings.HasPrefix(l, "a=candidate:") || l == "a=end-of-candidates" || l == "a=ice-lite" {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\r\n") + "\r\n"
}

// describe sets a local description on pc and returns its SDP with the
// server's candidates in it.
func (m *media) describe(ctx context.Context, pc *webrtc.PeerConnection, d webrtc.SessionDescription) (string, error) {
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(d); err != nil {
		return "", err
	}
	select {
	case <-gathered:
	case <-time.After(5 * time.Second):
		return "", errors.New("gathering the server's own candidates took too long")
	}
	local := pc.LocalDescription()
	if local == nil {
		return "", errors.New("the connection has no local description")
	}
	return m.candidates(ctx, local.SDP), nil
}
