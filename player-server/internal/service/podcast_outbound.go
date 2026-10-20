package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// Outbound policy for catalog feeds created by non-admin subscriptions
// (docs/podcast-sync-contract.md §7.3). Any user may now make the server
// fetch a URL, so those fetches may only reach public unicast addresses:
// the check runs in the dialer, on the resolved address of every
// connection, which covers every redirect hop and DNS answers that change
// between lookups. Feeds an admin created keep today's unrestricted client.

// errBlockedAddress reports a connection the outbound policy refused.
var errBlockedAddress = errors.New("outbound address not allowed for this feed")

// maxFeedRedirects bounds the redirect hops of one feed fetch.
const maxFeedRedirects = 10

// nonPublicPrefixes are unicast ranges netip does not classify as private
// or local but that are not reachable public addresses either.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),      // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),  // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),   // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"),  // benchmarking
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64, can embed private IPv4
	netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
}

// publicUnicast reports whether ip is a public unicast address: not
// loopback, private, link-local, unique-local, multicast or unspecified.
func publicUnicast(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// newRestrictedClient returns a client that connects only to addresses
// allow accepts, never through a proxy (the proxy's address would be the
// one checked), and follows only http and https redirects.
func newRestrictedClient(timeout time.Duration, allow func(netip.Addr) bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || !allow(ip) {
				return fmt.Errorf("%w: %s", errBlockedAddress, host)
			}
			return nil
		}}
	transport := &http.Transport{
		Proxy: nil, DialContext: dialer.DialContext, ForceAttemptHTTP2: true,
		MaxIdleConns: 10, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: httpRedirectsOnly}
}

// httpRedirectsOnly stops redirect chains that are too long or leave http(s).
func httpRedirectsOnly(req *http.Request, via []*http.Request) error {
	if len(via) >= maxFeedRedirects {
		return fmt.Errorf("stopped after %d redirects", maxFeedRedirects)
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("redirect to unsupported scheme %q", req.URL.Scheme)
	}
	return nil
}

// feedClient returns the client a catalog feed is fetched with: the
// restricted one for rows a non-admin's subscription created (and, failing
// closed, when the creator cannot be looked up or no longer exists), the
// injected client otherwise.
func (s *podcastService) feedClient(ctx context.Context, feed *model.PodcastFeed) *http.Client {
	if feed.CreatedBy == nil {
		return s.httpClient
	}
	user, err := s.store.GetUserByID(ctx, *feed.CreatedBy)
	if err == nil && user != nil && user.IsAdmin {
		return s.httpClient
	}
	return s.restrictedClient
}
