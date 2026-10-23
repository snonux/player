package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseCredentialsPasswordForms(t *testing.T) {
	plain := httptest.NewRequest(http.MethodGet, "/rest/ping.view?u=alice&p=secret&v=1.16.1&c=Test", nil)
	c := ParseCredentials(plain)
	if c.Username != "alice" || c.Password != "secret" || !c.HasPasswordAuth() {
		t.Fatalf("plain: %+v", c)
	}

	enc := httptest.NewRequest(http.MethodGet, "/rest/ping.view?u=alice&p=enc:736563726574&v=1.16.1&c=Test", nil)
	c = ParseCredentials(enc)
	if c.Password != "secret" {
		t.Fatalf("enc password: %q", c.Password)
	}

	key := httptest.NewRequest(http.MethodGet, "/rest/ping.view?apiKey=pt_abc&v=1.16.1&c=Test", nil)
	c = ParseCredentials(key)
	if !c.UsesAPIKey() || c.APIKey != "pt_abc" {
		t.Fatalf("apiKey: %+v", c)
	}
}

func TestTokenMatches(t *testing.T) {
	sum := md5.Sum([]byte("sesame" + "c19b2d"))
	token := hex.EncodeToString(sum[:])
	if !TokenMatches("sesame", "c19b2d", token) {
		t.Fatal("expected match")
	}
	if TokenMatches("wrong", "c19b2d", token) {
		t.Fatal("expected mismatch")
	}
}

func TestParseCredentialsTokenAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rest/ping.view?u=joe&t=abc&s=salt&v=1.16.1&c=Test", nil)
	c := ParseCredentials(req)
	if !c.UsesTokenAuth() {
		t.Fatal("expected token auth")
	}
}
