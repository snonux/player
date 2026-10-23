package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
)

func TestCastTicket_RoundTrip(t *testing.T) {
	clk := &clock.MockClock{T: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	s := NewCastTicketSignerWithKey([]byte("k"), clk)
	ticket, expires := s.Issue(7, 42, time.Hour)
	if strings.ContainsAny(ticket, "/+=") {
		t.Fatalf("ticket %q is not URL-safe", ticket)
	}
	if !expires.Equal(clk.T.Add(time.Hour)) {
		t.Fatalf("expires = %v", expires)
	}
	got, err := s.Verify(ticket)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.UserID != 7 || got.MediaID != 42 || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("grant = %+v", got)
	}
}

func TestCastTicket_Rejects(t *testing.T) {
	clk := &clock.MockClock{T: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	s := NewCastTicketSignerWithKey([]byte("k"), clk)
	ticket, _ := s.Issue(7, 42, time.Hour)

	other := NewCastTicketSignerWithKey([]byte("other"), clk)
	if _, err := other.Verify(ticket); err != ErrInvalidCastTicket {
		t.Fatalf("other key: err = %v", err)
	}
	// Flip one character of the payload: the signature no longer matches.
	tampered := []byte(ticket)
	if tampered[2] == 'A' {
		tampered[2] = 'B'
	} else {
		tampered[2] = 'A'
	}
	for name, bad := range map[string]string{
		"tampered":  string(tampered),
		"empty":     "",
		"not b64":   "!!!",
		"truncated": ticket[:len(ticket)-4],
	} {
		if _, err := s.Verify(bad); err != ErrInvalidCastTicket {
			t.Fatalf("%s: err = %v", name, err)
		}
	}

	clk.T = clk.T.Add(time.Hour)
	if _, err := s.Verify(ticket); err != ErrInvalidCastTicket {
		t.Fatalf("expired: err = %v", err)
	}
}

func TestNewCastTicketSigner_RandomKey(t *testing.T) {
	a, err := NewCastTicketSigner(nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewCastTicketSigner(nil)
	ticket, _ := a.Issue(1, 1, time.Minute)
	if _, err := b.Verify(ticket); err == nil {
		t.Fatal("two signers share a key")
	}
}
