package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A share's max_uses counts viewings, not HTTP requests: a player fetches one
// file with many ranged requests, and counting each of them made a
// single-use link die after the first second of playback.
//
// A viewing is opened by the first request of a client (normally the share
// page or its JSON) and costs one use. The client gets a viewing credential
// and presents it on every further request; those cost nothing and keep
// working after max_uses is reached, until the viewing expires.
//
// The credential is "<expiry unix seconds>.<base64url HMAC-SHA256>". The MAC
// covers the share token and the expiry, so a credential cannot be forged,
// extended, or used for another share. Nothing is stored per viewing: the
// only state is the signing key (repository.ShareRepo.ShareViewingKey), which
// lives in the database and therefore survives restarts. Revocation and share
// expiry need no viewing state either, because every request still loads the
// share row first.
const (
	// ShareViewingLifetime is how long a viewing stays usable after it was
	// opened. It must outlast one sitting — a long film including pauses —
	// while staying short enough that a recipient of a limited link cannot
	// keep using it for days. The share's own expiry and revocation still
	// end a viewing immediately.
	ShareViewingLifetime = 6 * time.Hour

	// ShareViewParam is the query parameter that carries the viewing
	// credential for clients without a cookie jar (media players).
	ShareViewParam = "view"

	// shareViewingDomain separates this MAC from any other use the key
	// might get, and versions the credential format.
	shareViewingDomain = "player share viewing v1"

	// maxShareCredentialLen bounds what is parsed at all. A genuine
	// credential is 54 characters (10 digits, a dot, 43 base64url
	// characters) until the year 2286.
	maxShareCredentialLen = 64
)

// ShareViewing is the viewing a share request runs under. The zero value
// means the request has none (a probe without a credential).
type ShareViewing struct {
	// Credential is presented by the client on later requests.
	Credential string
	// ExpiresAt is when the credential stops being accepted.
	ExpiresAt time.Time
	// Opened is true when this request opened the viewing, i.e. a share use
	// was consumed and the client has to be handed the credential.
	Opened bool
}

// ShareAccess describes one request to a public share.
type ShareAccess struct {
	// Token is the share token from the URL path.
	Token string
	// Credential is the viewing credential the client presented, if any.
	// An invalid one is treated exactly like none.
	Credential string
	// Probe marks a request that delivers no content (HEAD). It never
	// opens a viewing and so never costs a use.
	Probe bool
}

// signShareViewing returns the credential for a viewing of the share token
// that expires at expiresAt (second precision).
func signShareViewing(key []byte, token string, expiresAt time.Time) string {
	expiry := strconv.FormatInt(expiresAt.Unix(), 10)
	return expiry + "." + base64.RawURLEncoding.EncodeToString(shareViewingMAC(key, token, expiry))
}

// verifyShareViewing reports whether credential is a genuine, unexpired
// viewing credential for token, and when it expires.
//
// Parsing is strict (canonical decimal expiry, canonical unpadded base64url)
// so every viewing has exactly one accepted spelling.
func verifyShareViewing(key []byte, token, credential string, now time.Time) (time.Time, bool) {
	if len(credential) > maxShareCredentialLen {
		return time.Time{}, false
	}
	expiry, encodedMAC, found := strings.Cut(credential, ".")
	if !found {
		return time.Time{}, false
	}
	seconds, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil || seconds <= 0 || strconv.FormatInt(seconds, 10) != expiry {
		return time.Time{}, false
	}
	mac, err := base64.RawURLEncoding.Strict().DecodeString(encodedMAC)
	if err != nil || !hmac.Equal(mac, shareViewingMAC(key, token, expiry)) {
		return time.Time{}, false
	}
	expiresAt := time.Unix(seconds, 0)
	return expiresAt, now.Before(expiresAt)
}

// shareViewingMAC authenticates (token, expiry). The expiry consists of
// digits only and comes last, so the newline-separated message has a single
// reading whatever the token contains.
func shareViewingMAC(key []byte, token, expiry string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(shareViewingDomain + "\n" + token + "\n" + expiry))
	return mac.Sum(nil)
}

// withShareView appends the viewing credential to a share URL path.
func withShareView(rawURL, credential string) string {
	if rawURL == "" || credential == "" {
		return rawURL
	}
	return rawURL + "?" + ShareViewParam + "=" + url.QueryEscape(credential)
}

// viewingKey returns the signing key, loading it from the store on first use
// and keeping it for the lifetime of the service. A failed load is not
// remembered, so a transient database error does not disable shares.
func (s *shareService) viewingKey(ctx context.Context) ([]byte, error) {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.key != nil {
		return s.key, nil
	}
	key, err := s.store.ShareViewingKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("share viewing key: %w", err)
	}
	if len(key) == 0 {
		// Refuse to sign with an empty key: every credential would be
		// forgeable.
		return nil, fmt.Errorf("share viewing key: empty")
	}
	s.key = key
	return key, nil
}

// activeViewing returns the viewing the presented credential stands for, or
// false when there is none or it is not valid for this share right now.
func (s *shareService) activeViewing(ctx context.Context, token, credential string) (ShareViewing, bool, error) {
	if credential == "" {
		return ShareViewing{}, false, nil
	}
	key, err := s.viewingKey(ctx)
	if err != nil {
		return ShareViewing{}, false, err
	}
	expiresAt, ok := verifyShareViewing(key, token, credential, s.clock.Now())
	if !ok {
		return ShareViewing{}, false, nil
	}
	return ShareViewing{Credential: credential, ExpiresAt: expiresAt}, true, nil
}

// EnsureShareViewing returns the viewing a content request runs under: the
// one the presented credential stands for, or a newly opened one. Opening
// atomically consumes one share use and fails with ErrShareExpired when none
// is left (e.g. a concurrent request took the last one).
//
// Callers check the share with ResolveSharedMedia first; this function alone
// does not tell a missing share from an exhausted one.
func (s *shareService) EnsureShareViewing(ctx context.Context, token, credential string) (ShareViewing, error) {
	viewing, active, err := s.activeViewing(ctx, token, credential)
	if err != nil || active {
		return viewing, err
	}
	key, err := s.viewingKey(ctx)
	if err != nil {
		return ShareViewing{}, err
	}
	now := s.clock.Now()
	used, err := s.store.UseShare(ctx, token, now)
	if err != nil {
		return ShareViewing{}, fmt.Errorf("use share: %w", err)
	}
	if !used {
		return ShareViewing{}, ErrShareExpired
	}
	// Whole seconds, as the credential stores them.
	expiresAt := now.Add(ShareViewingLifetime).Truncate(time.Second)
	return ShareViewing{
		Credential: signShareViewing(key, token, expiresAt),
		ExpiresAt:  expiresAt,
		Opened:     true,
	}, nil
}
