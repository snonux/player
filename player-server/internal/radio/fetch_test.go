package radio

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestPublicAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8":            true,
		"2a00:1450::1":       true,
		"127.0.0.1":          false,
		"10.1.2.3":           false,
		"172.16.0.1":         false,
		"192.168.1.10":       false,
		"169.254.169.254":    false,
		"100.64.0.1":         false,
		"0.1.2.3":            false,
		"198.18.0.1":         false,
		"255.255.255.255":    false,
		"224.0.0.1":          false,
		"::1":                false,
		"fe80::1":            false,
		"fd00::1":            false,
		"::ffff:127.0.0.1":   false,
		"::ffff:8.8.8.8":     true,
		"64:ff9b::a00:1":     false,
		"::":                 false,
		"192.0.0.8":          false,
		"203.0.113.1":        true, // documentation range: harmless, not filtered
		"2001:db8::1":        true,
		"::ffff:192.168.0.1": false,
	} {
		if got := PublicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("PublicAddr(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestFetcher_RefusesLoopbackIncludingRedirects(t *testing.T) {
	station := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer station.Close()
	f := NewFetcher("test", false)
	_, err := f.OpenStream(context.Background(), station.URL, "audio/mpeg")
	if !errors.Is(err, ErrStationUnreachable) || !strings.Contains(err.Error(), errBlockedAddress.Error()) {
		t.Fatalf("loopback stream err = %v", err)
	}
	// A public-looking name that resolves to loopback is refused too: the
	// check runs on the dialled address.
	if _, err := f.OpenStream(context.Background(), strings.Replace(station.URL, "127.0.0.1", "localhost", 1), "audio/mpeg"); err == nil {
		t.Fatal("localhost name was relayed")
	}

	// Allowed fetchers do connect, and a redirect is checked hop by hop.
	allowed := NewFetcher("test", true)
	resp, err := allowed.OpenStream(context.Background(), station.URL, "audio/mpeg")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	redirect := httptest.NewServer(http.RedirectHandler("ftp://example.org/x", http.StatusFound))
	defer redirect.Close()
	if _, err := allowed.OpenStream(context.Background(), redirect.URL, "audio/mpeg"); err == nil {
		t.Fatal("redirect to a non-http scheme was followed")
	}
}

func TestFetcher_ContentTypes(t *testing.T) {
	var contentType string
	station := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		} else {
			w.Header()["Content-Type"] = nil
		}
		_, _ = w.Write([]byte("data"))
	}))
	defer station.Close()
	f := NewFetcher("test", true)
	for ct, want := range map[string]string{
		"audio/mpeg":                    "audio/mpeg",
		"audio/aacp; charset=x":         "audio/aacp",
		"application/ogg":               "application/ogg",
		"application/octet-stream":      "audio/aac",
		"":                              "audio/aac",
		"text/html":                     "",
		"audio/x-mpegurl":               "",
		"application/vnd.apple.mpegurl": "",
		"audio/x-scpls":                 "",
	} {
		contentType = ct
		resp, err := f.OpenStream(context.Background(), station.URL, "audio/aac")
		if want == "" {
			if !errors.Is(err, ErrNotAudio) {
				t.Errorf("%q: err = %v, want ErrNotAudio", ct, err)
			}
			continue
		}
		if err != nil || resp.ContentType != want {
			t.Errorf("%q: %+v %v, want %s", ct, resp, err, want)
			continue
		}
		resp.Body.Close()
	}
	for ct, ok := range map[string]bool{"image/png": true, "image/x-icon": true, "image/svg+xml": false, "text/html": false} {
		contentType = ct
		resp, err := f.OpenFavicon(context.Background(), station.URL)
		if ok != (err == nil) {
			t.Errorf("favicon %q: err = %v", ct, err)
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
}

// Shoutcast v1 answers "ICY 200 OK"; the relay must still play it.
func TestFetcher_ShoutcastICYAnswer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_, _ = io.WriteString(conn, "ICY 200 OK\r\nicy-name: Test\r\ncontent-type: audio/mpeg\r\n\r\nmp3data")
	}()
	resp, err := NewFetcher("test", true).OpenStream(context.Background(), "http://"+ln.Addr().String()+"/", "audio/mpeg")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.ContentType != "audio/mpeg" || string(body) != "mp3data" {
		t.Fatalf("ICY answer = %s %q", resp.ContentType, body)
	}
}

func TestCodecContentType(t *testing.T) {
	for codec, want := range map[string]string{"MP3": "audio/mpeg", "aac+": "audio/aac", "OGG": "audio/ogg", "FLAC": "audio/flac", "UNKNOWN": "audio/mpeg"} {
		if got := CodecContentType(codec); got != want {
			t.Errorf("CodecContentType(%s) = %s, want %s", codec, got, want)
		}
	}
}
