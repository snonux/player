package radio

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

// ErrStationUnreachable wraps failures to connect to a station or to get a
// successful answer from it.
var ErrStationUnreachable = errors.New("radio station unreachable")

// ErrNotAudio is returned when a station answers with something that cannot
// be played as one audio stream (a web page, a playlist, HLS).
var ErrNotAudio = errors.New("radio station did not answer with audio")

// ErrNotImage is returned when a favicon is not a raster image.
var ErrNotImage = errors.New("radio favicon is not an image")

// errBlockedAddress is returned by the dialer for addresses the relay must
// not reach.
var errBlockedAddress = errors.New("address not allowed for radio relay")

// MaxFaviconBytes bounds a relayed favicon.
const MaxFaviconBytes = 1 << 20

// Fetcher opens station streams and favicons. Station URLs come from a
// public directory anyone can add entries to, so by default it refuses to
// connect to loopback, private, link-local and other non-public addresses:
// otherwise the relay would let any signed-in user reach services on the
// server's own network. The check runs on the address actually dialled, so
// it also covers redirects and DNS names that resolve to such addresses.
type Fetcher struct {
	client    *http.Client
	userAgent string
}

// NewFetcher returns a Fetcher. allowPrivate lifts the address restriction
// (for a station on the home network, and for tests).
func NewFetcher(userAgent string, allowPrivate bool) *Fetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !allowPrivate {
		dialer.Control = refusePrivate
	}
	transport := &http.Transport{
		// No proxy: the address check must see the station, not a proxy.
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return newICYConn(conn), nil
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		// A live stream is consumed as it arrives; compressed transfer
		// would only hide the real content type.
		DisableCompression: true,
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to %s not allowed", req.URL.Scheme)
			}
			return nil
		},
	}
	return &Fetcher{client: client, userAgent: userAgent}
}

// refusePrivate is a net.Dialer Control function that rejects every
// address that is not a public unicast address.
func refusePrivate(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", errBlockedAddress, address)
	}
	if !PublicAddr(ap.Addr()) {
		return fmt.Errorf("%w: %s", errBlockedAddress, ap.Addr())
	}
	return nil
}

// nonPublic are special-purpose ranges that netip does not classify as
// private, loopback or link-local but that are no public stations either:
// "this network", shared (CGNAT) space, IETF protocol assignments,
// benchmarking, reserved space, and NAT64, which can map to any IPv4
// address including private ones.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

// PublicAddr reports whether a is a global unicast address outside every
// private, loopback, link-local and special-purpose range.
func PublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || !a.IsGlobalUnicast() || a.IsPrivate() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// Response is an open station answer. The caller must close Body.
type Response struct {
	ContentType string
	Body        io.ReadCloser
}

// OpenStream connects to a station stream. fallbackType is used when the
// station sends no content type (some Shoutcast servers).
func (f *Fetcher) OpenStream(ctx context.Context, streamURL, fallbackType string) (*Response, error) {
	resp, err := f.open(ctx, streamURL, "audio/*, application/ogg;q=0.9, */*;q=0.1")
	if err != nil {
		return nil, err
	}
	contentType, ok := audioType(resp.Header.Get("Content-Type"), fallbackType)
	if !ok {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %q", ErrNotAudio, resp.Header.Get("Content-Type"))
	}
	return &Response{ContentType: contentType, Body: resp.Body}, nil
}

// OpenFavicon fetches a favicon. Only raster images are passed on: an SVG
// served from the player's origin could run script when opened directly.
func (f *Fetcher) OpenFavicon(ctx context.Context, faviconURL string) (*Response, error) {
	resp, err := f.open(ctx, faviconURL, "image/*")
	if err != nil {
		return nil, err
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(mediaType, "image/") || strings.Contains(mediaType, "svg") {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %q", ErrNotImage, mediaType)
	}
	return &Response{ContentType: mediaType, Body: resp.Body}, nil
}

func (f *Fetcher) open(ctx context.Context, target, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStationUnreachable, err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", accept)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStationUnreachable, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: answered %d", ErrStationUnreachable, resp.StatusCode)
	}
	return resp, nil
}

// playlistTypes are answers that describe a stream instead of being one.
var playlistTypes = map[string]bool{
	"application/vnd.apple.mpegurl": true,
	"application/x-mpegurl":         true,
	"audio/mpegurl":                 true,
	"audio/x-mpegurl":               true,
	"audio/x-scpls":                 true,
	"application/pls+xml":           true,
	"audio/x-ms-asx":                true,
	"video/x-ms-asf":                true,
}

// audioType decides whether a station's content type is playable audio and
// returns the type to send to the client.
func audioType(header, fallback string) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil || mediaType == "" {
		return fallback, true
	}
	switch {
	case playlistTypes[mediaType]:
		return "", false
	case strings.HasPrefix(mediaType, "audio/"), mediaType == "application/ogg", mediaType == "video/mp2t":
		return mediaType, true
	case mediaType == "application/octet-stream":
		return fallback, true
	default:
		return "", false
	}
}

// CodecContentType guesses a stream's content type from the codec name the
// directory reports.
func CodecContentType(codec string) string {
	switch strings.ToUpper(strings.TrimSpace(codec)) {
	case "MP3":
		return "audio/mpeg"
	case "AAC", "AAC+", "AACP", "HE-AAC":
		return "audio/aac"
	case "OGG", "VORBIS", "OPUS":
		return "audio/ogg"
	case "FLAC":
		return "audio/flac"
	default:
		return "audio/mpeg"
	}
}

// icyConn makes Shoutcast v1 answers parseable: those servers start their
// answer with "ICY 200 OK" instead of an HTTP status line, which net/http
// rejects. The first line is rewritten to HTTP/1.0. TLS traffic never starts
// with "ICY ", so wrapping every connection is harmless.
type icyConn struct {
	net.Conn
	reader *bufio.Reader
	first  bool
}

func newICYConn(conn net.Conn) net.Conn {
	return &icyConn{Conn: conn, reader: bufio.NewReaderSize(conn, 4096), first: true}
}

func (c *icyConn) Read(p []byte) (int, error) {
	if c.first {
		c.first = false
		peek, err := c.reader.Peek(4)
		if err == nil && string(peek) == "ICY " {
			if _, err := c.reader.Discard(4); err != nil {
				return 0, err
			}
			c.reader = bufio.NewReaderSize(io.MultiReader(strings.NewReader("HTTP/1.0 "), c.reader), 4096)
		}
	}
	return c.reader.Read(p)
}
