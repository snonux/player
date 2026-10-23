package auth

import (
	"fmt"
	"net"
	"net/textproto"
	"strings"
)

// DefaultProxyUserHeader is the HTTP header trusted proxies set with the
// authenticated username when PROXY_AUTH_TRUSTED_SOURCES is configured.
const DefaultProxyUserHeader = "Remote-User"

// ProxyAuth holds operator-configured reverse-proxy authentication.
// It is disabled when TrustedSources was empty at parse time (ProxyAuth is nil
// on Config). Untrusted clients must never be able to set the user header and
// impersonate accounts; only peers matching TrustedSources are accepted.
type ProxyAuth struct {
	Nets       []*net.IPNet
	TrustUnix  bool
	UserHeader string
	LogoutURL  string
}

// ParseProxyAuth builds ProxyAuth from environment-style values.
// trustedSources is a comma-separated list of CIDR ranges and optional "@"
// (trust connections whose RemoteAddr is a UNIX socket). An empty
// trustedSources disables the feature and returns (nil, nil).
func ParseProxyAuth(trustedSources, userHeader, logoutURL string) (*ProxyAuth, error) {
	sources := splitCSV(trustedSources)
	if len(sources) == 0 {
		if strings.TrimSpace(userHeader) != "" || strings.TrimSpace(logoutURL) != "" {
			return nil, fmt.Errorf("PROXY_AUTH_USER_HEADER and PROXY_AUTH_LOGOUT_URL require PROXY_AUTH_TRUSTED_SOURCES")
		}
		return nil, nil
	}

	header := strings.TrimSpace(userHeader)
	if header == "" {
		header = DefaultProxyUserHeader
	}

	pa := &ProxyAuth{
		UserHeader: httpCanonicalHeader(header),
		LogoutURL:  strings.TrimSpace(logoutURL),
	}
	for _, src := range sources {
		if src == "@" {
			pa.TrustUnix = true
			continue
		}
		_, network, err := net.ParseCIDR(src)
		if err != nil {
			// Accept a bare IP as /32 or /128.
			ip := net.ParseIP(src)
			if ip == nil {
				return nil, fmt.Errorf("invalid PROXY_AUTH_TRUSTED_SOURCES entry %q: %w", src, err)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			network = &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
		}
		pa.Nets = append(pa.Nets, network)
	}
	if len(pa.Nets) == 0 && !pa.TrustUnix {
		return nil, fmt.Errorf("PROXY_AUTH_TRUSTED_SOURCES has no usable entries")
	}
	return pa, nil
}

// Enabled reports whether reverse-proxy authentication is active.
func (p *ProxyAuth) Enabled() bool {
	return p != nil && (len(p.Nets) > 0 || p.TrustUnix)
}

// Trusted reports whether remoteAddr (as in http.Request.RemoteAddr) is a
// configured trusted proxy. X-Forwarded-For is intentionally ignored: trust
// is based only on the immediate peer so clients cannot spoof identity.
func (p *ProxyAuth) Trusted(remoteAddr string) bool {
	if !p.Enabled() {
		return false
	}
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return false
	}
	if isUnixRemoteAddr(remoteAddr) {
		return p.TrustUnix
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// Some listeners report a bare IP.
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range p.Nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func splitCSV(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isUnixRemoteAddr(addr string) bool {
	// Go's net/http reports UNIX peer addresses as "@" or as a filesystem path
	// without a host:port form that ParseIP / SplitHostPort understand.
	if addr == "@" {
		return true
	}
	if strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, "@") {
		return true
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return false
	}
	return net.ParseIP(addr) == nil
}

// httpCanonicalHeader matches Go's canonical MIME header key form so lookups
// via Header.Get work regardless of how the operator spelled the env value.
func httpCanonicalHeader(name string) string {
	return textproto.CanonicalMIMEHeaderKey(name)
}
