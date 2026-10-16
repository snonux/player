package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/clock"
)

// ErrInvalidCastTicket is returned for a cast ticket that is malformed,
// forged, or expired. Callers answer all three alike so a probe learns
// nothing about which check failed.
var ErrInvalidCastTicket = errors.New("invalid cast ticket")

// castTicketPayloadLen is user ID, media ID and expiry, 8 bytes each.
const castTicketPayloadLen = 24

// CastTicket is what a verified cast ticket grants: playback of one media
// item on behalf of one user until ExpiresAt.
type CastTicket struct {
	UserID    int64
	MediaID   int64
	ExpiresAt time.Time
}

// CastTicketSigner issues and verifies cast tickets.
//
// A cast device (Chromecast) fetches media by URL and cannot send the
// sender's session cookie or bearer token. A ticket is the credential it
// carries in the URL instead: an HMAC-signed, expiring statement of "user U
// may stream media M". It is stateless; the key lives only in memory, so a
// server restart invalidates all tickets and senders request new ones. The
// ticket never replaces the access check: every request still asks the
// media service whether U may stream M, so revoked permissions apply at once.
type CastTicketSigner struct {
	key []byte
	clk clock.Clock
}

// NewCastTicketSigner returns a signer with a fresh random key.
func NewCastTicketSigner(clk clock.Clock) (*CastTicketSigner, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate cast ticket key: %w", err)
	}
	return NewCastTicketSignerWithKey(key, clk), nil
}

// NewCastTicketSignerWithKey returns a signer using key; for tests.
func NewCastTicketSignerWithKey(key []byte, clk clock.Clock) *CastTicketSigner {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &CastTicketSigner{key: append([]byte(nil), key...), clk: clk}
}

// Issue returns a URL-safe ticket for userID and mediaID valid for ttl.
func (s *CastTicketSigner) Issue(userID, mediaID int64, ttl time.Duration) (string, time.Time) {
	expires := s.clk.Now().Add(ttl).Truncate(time.Second)
	payload := make([]byte, castTicketPayloadLen)
	binary.BigEndian.PutUint64(payload[0:8], uint64(userID))
	binary.BigEndian.PutUint64(payload[8:16], uint64(mediaID))
	binary.BigEndian.PutUint64(payload[16:24], uint64(expires.Unix()))
	ticket := append(payload, s.mac(payload)...)
	return base64.RawURLEncoding.EncodeToString(ticket), expires
}

// Verify checks ticket's signature and expiry and returns what it grants.
func (s *CastTicketSigner) Verify(ticket string) (CastTicket, error) {
	raw, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil || len(raw) != castTicketPayloadLen+sha256.Size {
		return CastTicket{}, ErrInvalidCastTicket
	}
	payload, sig := raw[:castTicketPayloadLen], raw[castTicketPayloadLen:]
	if !hmac.Equal(sig, s.mac(payload)) {
		return CastTicket{}, ErrInvalidCastTicket
	}
	grant := CastTicket{
		UserID:    int64(binary.BigEndian.Uint64(payload[0:8])),
		MediaID:   int64(binary.BigEndian.Uint64(payload[8:16])),
		ExpiresAt: time.Unix(int64(binary.BigEndian.Uint64(payload[16:24])), 0),
	}
	if !s.clk.Now().Before(grant.ExpiresAt) {
		return CastTicket{}, ErrInvalidCastTicket
	}
	return grant, nil
}

func (s *CastTicketSigner) mac(payload []byte) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("player-cast-ticket-v1\x00"))
	m.Write(payload)
	return m.Sum(nil)
}
