package service

import (
	"bytes"
	"codeberg.org/snonux/player/internal/model"
	"context"
	"encoding/binary"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os/exec"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestRadioURLBoundaries(t *testing.T) {
	for _, raw := range []string{"", "file:///etc/passwd", "https://user:secret@example.com/live", "https://example.com/live#fragment", "//example.com/live", "http://example.com/\n"} {
		t.Run(raw, func(t *testing.T) {
			if err := validateRadioURL(raw); err == nil {
				t.Fatal("accepted unsafe URL")
			}
		})
	}
	item := model.RadioStation{Name: " Example ", StreamURL: "https://example.com/live"}
	if err := ValidateRadioStation(&item); err != nil || item.Name != "Example" {
		t.Fatalf("valid station: %+v %v", item, err)
	}
}

func TestRadioAddressPolicy(t *testing.T) {
	// Exercise the dial policy directly so metadata services are never contacted.
	for _, test := range []struct {
		address string
		public  bool
		local   bool
	}{
		{"fd00:ec2::254", false, false},
		{"FD00:0EC2:0000:0000:0000:0000:0000:0254", false, false},
		{"fd00:ec2::254%eth0", false, false},
		{"169.254.169.254", false, false},
		{"::ffff:169.254.169.254", false, false},
		{"::ffff:a9fe:a9fe", false, false},
		{"127.0.0.1", false, true},
		{"::ffff:127.0.0.1", false, true},
		{"::1", false, true},
		{"10.0.0.1", false, true},
		{"172.16.0.1", false, true},
		{"192.168.1.1", false, true},
		{"fd00:ec2::255", false, true},
		{"8.8.8.8", true, true},
	} {
		for _, local := range []bool{false, true} {
			t.Run(test.address+"/local="+strconv.FormatBool(local), func(t *testing.T) {
				want := test.public || local && test.local
				if got := radioAddressAllowed(netip.MustParseAddr(test.address), local); got != want {
					t.Fatalf("radioAddressAllowed = %v, want %v", got, want)
				}
			})
		}
	}
}

func TestRadioLocalOptInAndRedirectCredentials(t *testing.T) {
	hits := 0
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("Player credential leaked")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://user:password@"+r.Host+"/live", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("live"))
	}))
	defer host.Close()
	if response, err := fetchRadio(context.Background(), host.URL, false); err == nil {
		_ = response.Body.Close()
		t.Fatal("default local dial allowed")
	}
	if hits != 0 {
		t.Fatal("blocked host was contacted")
	}
	response, err := fetchRadio(context.Background(), host.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response, err := fetchRadio(context.Background(), host.URL+"/redirect", true); err == nil {
		_ = response.Body.Close()
		t.Fatal("credentialed redirect accepted")
	}
	if hits != 2 {
		t.Fatalf("unexpected redirect hit count: %d", hits)
	}
}

func TestRadioArtworkRejectsMarkupAndDecodesImages(t *testing.T) {
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/image" {
			w.Header().Set("Content-Type", "image/png")
			_ = png.Encode(w, image.NewRGBA(image.Rect(0, 0, 8, 8)))
			return
		}
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	}))
	defer host.Close()
	relay := NewRadioRelay()
	if _, err := relay.Artwork(context.Background(), model.RadioStation{ArtworkURL: host.URL, LocalNetwork: true}); err == nil {
		t.Fatal("accepted active markup")
	}
	data, err := relay.Artwork(context.Background(), model.RadioStation{ArtworkURL: host.URL + "/image", LocalNetwork: true})
	if err != nil || len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		t.Fatalf("JPEG conversion: %d %v", len(data), err)
	}
}

func TestRadioRelayBusyIsBounded(t *testing.T) {
	relay := NewRadioRelay()
	for i := 0; i < cap(relay.slots); i++ {
		relay.slots <- struct{}{}
	}
	head := httptest.NewRecorder()
	relay.Stream(head, httptest.NewRequest(http.MethodHead, "/", nil), model.RadioStation{})
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatal("HEAD must not acquire an endless live decoder")
	}
	response := httptest.NewRecorder()
	relay.Stream(response, httptest.NewRequest(http.MethodGet, "/", nil), model.RadioStation{})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("busy status %d", response.Code)
	}
}

// This verifies the actual bounded decoder, not browser/native audio acceptance.
func TestRadioRelayDecodesAudioAndRefusesManifest(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is unavailable")
	}
	var secondaryHits atomic.Int32
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondaryHits.Add(1)
		_, _ = w.Write([]byte("not audio"))
	}))
	defer secondary.Close()
	pcm := make([]byte, 44+16000)
	copy(pcm, "RIFF")
	binary.LittleEndian.PutUint32(pcm[4:], uint32(len(pcm)-8))
	copy(pcm[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(pcm[16:], 16)
	binary.LittleEndian.PutUint16(pcm[20:], 1)
	binary.LittleEndian.PutUint16(pcm[22:], 1)
	binary.LittleEndian.PutUint32(pcm[24:], 8000)
	binary.LittleEndian.PutUint32(pcm[28:], 16000)
	binary.LittleEndian.PutUint16(pcm[32:], 2)
	binary.LittleEndian.PutUint16(pcm[34:], 16)
	copy(pcm[36:], "data")
	binary.LittleEndian.PutUint32(pcm[40:], 16000)
	for _, test := range []struct {
		name   string
		body   []byte
		status int
	}{
		{"audio", pcm, http.StatusOK},
		{"manifest", []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:5\n#EXTINF:1,\n" + secondary.URL + "/private.mp3\n#EXT-X-ENDLIST\n"), http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(test.body) }))
			defer host.Close()
			response := httptest.NewRecorder()
			NewRadioRelay().Stream(response, httptest.NewRequest(http.MethodGet, "/radio", nil), model.RadioStation{StreamURL: host.URL, LocalNetwork: true})
			if secondaryHits.Load() != 0 {
				t.Fatal("manifest bypassed outbound boundary")
			}
			if response.Code != test.status {
				t.Fatalf("status %d body %s", response.Code, response.Body)
			}
			if test.status == http.StatusOK && (response.Header().Get("Content-Type") != "audio/mpeg" || response.Body.Len() < 100) {
				t.Fatalf("not decoded MP3: %s %d", response.Header().Get("Content-Type"), response.Body.Len())
			}
		})
	}
}

// A real HTTP server proves preparation and later chunks outlive its absolute
// WriteTimeout. The timing is deliberately subsecond, not a 31-second sleep.
func TestRadioRelayRenewsHTTPWriteBudget(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	encoded, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo", "-t", "2", "-c:a", "libmp3lame", "-b:a", "128k", "-write_xing", "0", "-id3v2_version", "0", "-f", "mp3", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	firstOutput := make(chan struct{})
	station := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(80 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		if _, err := w.Write(bytes.Repeat(encoded, 8)); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		select {
		case <-firstOutput:
		case <-r.Context().Done():
			return
		case <-ctx.Done():
			return
		}
		for range 4 {
			select {
			case <-time.After(70 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
			if _, err := w.Write(encoded); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer station.Close()
	relay := NewRadioRelay()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relay.Stream(w, r, model.RadioStation{StreamURL: station.URL, LocalNetwork: true})
	}))
	server.Config.WriteTimeout = 30 * time.Millisecond
	server.Start()
	defer server.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	first := make([]byte, 1024)
	n, err := response.Body.Read(first)
	if err != nil || n == 0 {
		t.Fatalf("initial live output %d: %v", n, err)
	}
	close(firstOutput)
	rest, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("later live output: %v", err)
	}
	if len(rest) < len(encoded)*8 {
		t.Fatalf("live stream truncated: %d trailing bytes", len(rest))
	}
}
