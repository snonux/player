package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strings"
)

// Credentials holds parsed Subsonic/OpenSubsonic authentication parameters.
// Password holds either a plaintext account password or an API token accepted
// in place of a password. Token+Salt is the classic MD5 challenge (unsupported
// against bcrypt-only accounts — callers should return ErrTokenAuthUnsupported).
type Credentials struct {
	Username string
	Password string
	Token    string
	Salt     string
	APIKey   string
	Client   string
	Version  string
}

// ParseCredentials extracts auth parameters from query or form body.
// Password may be plain text or enc:<hex>. Credentials are never logged here.
func ParseCredentials(r *http.Request) Credentials {
	_ = r.ParseForm()
	c := Credentials{
		Username: strings.TrimSpace(firstQuery(r, "u")),
		Password: decodePassword(firstQuery(r, "p")),
		Token:    strings.TrimSpace(firstQuery(r, "t")),
		Salt:     strings.TrimSpace(firstQuery(r, "s")),
		APIKey:   strings.TrimSpace(firstQuery(r, "apiKey")),
		Client:   strings.TrimSpace(firstQuery(r, "c")),
		Version:  strings.TrimSpace(firstQuery(r, "v")),
	}
	return c
}

// UsesTokenAuth reports classic Subsonic MD5 token authentication.
func (c Credentials) UsesTokenAuth() bool {
	return c.Token != "" || c.Salt != ""
}

// UsesAPIKey reports OpenSubsonic apiKey authentication.
func (c Credentials) UsesAPIKey() bool {
	return c.APIKey != ""
}

// HasPasswordAuth reports username + password (plain or enc).
func (c Credentials) HasPasswordAuth() bool {
	return c.Username != "" && c.Password != ""
}

// MissingRequired reports missing client/version (required by Subsonic).
func (c Credentials) MissingRequired() bool {
	return c.Client == "" || c.Version == ""
}

// TokenMatches returns whether md5(password+salt) equals the provided token.
// Callers with only a bcrypt hash cannot use this; it is kept for tests and
// any future reversible Subsonic secret.
func TokenMatches(password, salt, token string) bool {
	if password == "" || salt == "" || token == "" {
		return false
	}
	sum := md5.Sum([]byte(password + salt))
	return strings.EqualFold(hex.EncodeToString(sum[:]), token)
}

func decodePassword(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	const prefix = "enc:"
	if !strings.HasPrefix(strings.ToLower(raw), prefix) {
		return raw
	}
	hexPart := raw[len(prefix):]
	decoded, err := hex.DecodeString(hexPart)
	if err != nil {
		return raw
	}
	return string(decoded)
}
