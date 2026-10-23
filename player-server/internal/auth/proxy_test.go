package auth

import (
	"testing"
)

func TestParseProxyAuth(t *testing.T) {
	t.Parallel()

	t.Run("disabled by default", func(t *testing.T) {
		pa, err := ParseProxyAuth("", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pa != nil {
			t.Fatalf("expected nil ProxyAuth, got %#v", pa)
		}
	})

	t.Run("header without sources is rejected", func(t *testing.T) {
		_, err := ParseProxyAuth("", "X-User", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("cidr and custom header", func(t *testing.T) {
		pa, err := ParseProxyAuth("127.0.0.1/32, 10.0.0.0/8", "X-Remote-User", "https://idp.example/logout")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !pa.Enabled() {
			t.Fatal("expected enabled")
		}
		if pa.UserHeader != "X-Remote-User" {
			t.Fatalf("header: got %q", pa.UserHeader)
		}
		if pa.LogoutURL != "https://idp.example/logout" {
			t.Fatalf("logout: got %q", pa.LogoutURL)
		}
		if !pa.Trusted("127.0.0.1:443") {
			t.Fatal("expected 127.0.0.1 trusted")
		}
		if !pa.Trusted("10.1.2.3:9999") {
			t.Fatal("expected 10.1.2.3 trusted")
		}
		if pa.Trusted("203.0.113.9:80") {
			t.Fatal("untrusted client must not pass")
		}
	})

	t.Run("bare ip and unix", func(t *testing.T) {
		pa, err := ParseProxyAuth("192.0.2.10, @", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pa.UserHeader != DefaultProxyUserHeader {
			t.Fatalf("default header: got %q", pa.UserHeader)
		}
		if !pa.Trusted("192.0.2.10:1") {
			t.Fatal("bare IP should be /32")
		}
		if !pa.Trusted("@") {
			t.Fatal("unix @ should be trusted")
		}
		if !pa.Trusted("/run/player.sock") {
			t.Fatal("unix path should be trusted")
		}
		if pa.Trusted("198.51.100.1:80") {
			t.Fatal("other IP must not pass")
		}
	})

	t.Run("invalid cidr", func(t *testing.T) {
		_, err := ParseProxyAuth("not-a-cidr", "", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProxyAuth_IgnoresEmptyRemoteAddr(t *testing.T) {
	t.Parallel()
	pa, err := ParseProxyAuth("0.0.0.0/0", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if pa.Trusted("") {
		t.Fatal("empty RemoteAddr must not be trusted")
	}
}
