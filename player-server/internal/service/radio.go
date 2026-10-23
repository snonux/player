package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/ffsafe"
	"codeberg.org/snonux/player/internal/model"
)

// RadioRelay fetches stations without forwarding Player credentials. Live input
// is decoded through a pipe-only ffmpeg process and emitted as universal MP3.
type RadioRelay struct {
	slots        chan struct{}
	artworkSlots chan struct{}
}

// NewRadioRelay bounds simultaneous live decoders for this server.
func NewRadioRelay() *RadioRelay {
	return &RadioRelay{slots: make(chan struct{}, 4), artworkSlots: make(chan struct{}, 4)}
}

// ValidateRadioStation checks persisted station fields before any outbound I/O.
func ValidateRadioStation(item *model.RadioStation) error {
	item.Name = strings.TrimSpace(item.Name)
	if item.Name == "" || len(item.Name) > 200 {
		return errors.New("station name must contain 1–200 characters")
	}
	for _, field := range []*string{&item.StreamURL, &item.HomepageURL, &item.ArtworkURL} {
		*field = strings.TrimSpace(*field)
		if *field == "" && field != &item.StreamURL {
			continue
		}
		if err := validateRadioURL(*field); err != nil {
			return err
		}
	}
	return nil
}

// Stream relays live MP3. The caller owns authentication and station lookup.
func (s *RadioRelay) Stream(w http.ResponseWriter, r *http.Request, item model.RadioStation) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Cache-Control", "no-store")
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		http.Error(w, "Radio is busy. Retry shortly.", http.StatusServiceUnavailable)
		return
	}
	// Live responses outlast the server's absolute response deadline. Source
	// preparation and idle reads are bounded by the watchdog; actual client
	// writes receive a fresh budget, so a slow client cannot hold a decoder.
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	fail := func(message string, status int) {
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		http.Error(w, message, status)
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	watchdog := time.AfterFunc(30*time.Second, cancel)
	defer watchdog.Stop()
	resp, err := fetchRadio(ctx, item.StreamURL, item.LocalNetwork)
	if err != nil {
		fail("Station unavailable. Check its stream URL and network access.", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	args := []string{"-hide_banner", "-max_alloc", "33554432", "-loglevel", "error", "-nostdin", "-threads", "1"}
	args = append(args, ffsafe.PipeInputArgs()...)
	args = append(args, "-map", "0:a:0", "-vn", "-threads", "1", "-ac", "2", "-ar", "44100", "-c:a", "libmp3lame", "-b:a", "128k", "-flush_packets", "1", "-f", "mp3", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdin = resp.Body
	output, err := cmd.StdoutPipe()
	if err != nil {
		fail("Radio playback unavailable.", http.StatusServiceUnavailable)
		return
	}
	if err = cmd.Start(); err != nil {
		fail("Radio decoder unavailable.", http.StatusServiceUnavailable)
		return
	}
	defer func() { cancel(); _ = resp.Body.Close(); _ = cmd.Wait() }()
	buf := make([]byte, 8192)
	n, err := output.Read(buf)
	if n == 0 {
		fail("Station could not be decoded. Retry or edit its stream URL.", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	for n > 0 {
		watchdog.Reset(30 * time.Second)
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, writeErr := w.Write(buf[:n]); writeErr != nil {
			return
		}
		if flushErr := controller.Flush(); flushErr != nil && !errors.Is(flushErr, http.ErrNotSupported) {
			return
		}
		_ = controller.SetWriteDeadline(time.Time{})
		if err != nil {
			return
		}
		n, err = output.Read(buf)
	}
}

// Artwork returns a bounded, decoded JPEG, never active external markup.
func (s *RadioRelay) Artwork(ctx context.Context, item model.RadioStation) ([]byte, error) {
	select {
	case s.artworkSlots <- struct{}{}:
		defer func() { <-s.artworkSlots }()
	default:
		return nil, errors.New("station artwork busy")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := fetchRadio(ctx, item.ArtworkURL, item.LocalNetwork)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, errors.New("station artwork too large")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 4096 || cfg.Height > 4096 || cfg.Width*cfg.Height > 4<<20 {
		return nil, errors.New("station artwork dimensions too large")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, decoded, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func validateRadioURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || strings.ContainsAny(raw, "\r\n") {
		return errors.New("station URLs must be absolute HTTP(S) URLs without credentials or fragments")
	}
	return nil
}

func fetchRadio(ctx context.Context, raw string, local bool) (*http.Response, error) {
	if err := validateRadioURL(raw); err != nil {
		return nil, err
	}
	allow := func(ip netip.Addr) bool {
		return radioAddressAllowed(ip, local)
	}
	client := newRestrictedClient(0, allow, 0)
	transport := client.Transport.(*http.Transport)
	transport.ResponseHeaderTimeout = 15 * time.Second
	defer transport.CloseIdleConnections()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := httpRedirectsOnly(req, via); err != nil {
			return err
		}
		return validateRadioURL(req.URL.String())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Player Internet Radio")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch station: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("station returned %d", resp.StatusCode)
	}
	return resp, nil
}

func radioAddressAllowed(ip netip.Addr, local bool) bool {
	ip = ip.Unmap().WithZone("")
	// Cloud instance metadata is forbidden even for opted-in local stations.
	if ip == netip.MustParseAddr("169.254.169.254") || ip == netip.MustParseAddr("fd00:ec2::254") {
		return false
	}
	return publicUnicast(ip) || local && (ip.IsLoopback() || ip.IsPrivate())
}
