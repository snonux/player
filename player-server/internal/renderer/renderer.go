// Package renderer discovers LAN UPnP renderers and controls AVTransport.
package renderer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const servicePrefix = "urn:schemas-upnp-org:service:AVTransport:"

// Device is a discovered renderer; network addresses stay on the server.
type Device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	control string
	service string
	expires time.Time
}

// Manager keeps bounded discovery results and exclusive account ownership.
type controlVersion struct {
	Session  string
	Revision int64
	Stopped  bool
}

type Manager struct {
	mu       sync.Mutex
	scan     sync.Mutex
	devices  map[string]Device
	owners   map[string]int64
	seen     map[string]time.Time
	versions map[string]controlVersion
	retired  map[string]time.Time
	client   *http.Client
}

// New creates a LAN-only manager. Redirects and proxies are intentionally disabled.
func New() *Manager {
	return &Manager{devices: make(map[string]Device), owners: make(map[string]int64), seen: make(map[string]time.Time), versions: make(map[string]controlVersion), retired: make(map[string]time.Time), client: &http.Client{
		Timeout:       4 * time.Second,
		Transport:     &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, MaxResponseHeaderBytes: 16384},
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("renderer redirects refused") },
	}}
}

// Discover actively searches the server's LAN, never the browser's network.
func (m *Manager) Discover(ctx context.Context) ([]Device, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	m.scan.Lock()
	defer m.scan.Unlock()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return nil, fmt.Errorf("open discovery: %w", err)
	}
	defer func() { _ = conn.Close() }()
	deadline := time.Now().Add(2500 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	_, err = conn.WriteToUDP([]byte("M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\nST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n\r\n"), &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900})
	if err != nil {
		return nil, fmt.Errorf("send discovery: %w", err)
	}
	locations := map[string]bool{}
	buffer := make([]byte, 8192)
	for len(locations) < 32 {
		n, peer, e := conn.ReadFromUDP(buffer)
		if e != nil {
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		response, e := http.ReadResponse(bufio.NewReader(bytes.NewReader(buffer[:n])), nil)
		if e != nil {
			continue
		}
		_ = response.Body.Close()
		location := response.Header.Get("Location")
		u, e := privateURL(location)
		if e != nil || !net.ParseIP(u.Hostname()).Equal(peer.IP) {
			continue
		}
		locations[location] = true
	}
	for location := range locations {
		if ctx.Err() != nil {
			break
		}
		device, e := m.describe(ctx, location)
		if e != nil {
			continue
		}
		m.mu.Lock()
		m.devices[device.ID] = device
		m.mu.Unlock()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := []Device{}
	for id, d := range m.devices {
		if m.owners[id] != 0 && time.Since(m.seen[id]) >= 2*time.Minute {
			delete(m.owners, id)
			delete(m.seen, id)
		}
		if time.Now().After(d.expires) && m.owners[id] == 0 {
			delete(m.devices, id)
			delete(m.versions, id)
			continue
		}
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// Command validates account/session ownership and monotonic load/stop revisions.
// Callers serialize this check with the entire SOAP sequence. Stopped revisions
// are retained so a delayed load cannot restart playback after Stop completed.
func (m *Manager) Command(id string, user int64, session string, revision int64, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.devices[id]; !ok {
		return errors.New("renderer not discovered; search again")
	}
	now := time.Now()
	for key, until := range m.retired {
		if now.After(until) {
			delete(m.retired, key)
		}
	}
	if until, ok := m.retired[id+"/"+session]; ok && now.Before(until) {
		return errors.New("renderer session superseded")
	}
	owner := m.owners[id]
	current := m.versions[id]
	active := owner != 0 && time.Since(m.seen[id]) < 2*time.Minute
	if active && (owner != user || (current.Session != "" && current.Session != session)) {
		return errors.New("renderer is in use by another playback session")
	}
	if current.Session == session {
		if revision < current.Revision || (current.Stopped && action != "stop" && revision <= current.Revision) {
			return errors.New("renderer command superseded")
		}
	} else if action != "load" && action != "stop" {
		return errors.New("renderer session unavailable")
	}
	if action != "load" && action != "stop" {
		if current.Session != session || current.Revision != revision || current.Stopped || owner != user {
			return errors.New("renderer command superseded")
		}
	} else {
		if current.Session != "" && current.Session != session {
			if len(m.retired) >= 256 {
				return errors.New("too many playback sessions; try again later")
			}
			m.retired[id+"/"+current.Session] = now.Add(5 * time.Minute)
		}
		m.versions[id] = controlVersion{Session: session, Revision: revision, Stopped: action == "stop"}
		m.owners[id] = user
	}
	m.seen[id] = time.Now()
	return nil
}

// Release forgets a stopped account's ownership.
func (m *Manager) Release(id string, user int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owners[id] == user {
		delete(m.owners, id)
		delete(m.seen, id)
	}
}

// Action sends one validated AVTransport action to an account's renderer.
func (m *Manager) Action(ctx context.Context, id string, user int64, action string, args map[string]string) (map[string]string, error) {
	m.mu.Lock()
	device, ok := m.devices[id]
	owner := m.owners[id]
	if ok && owner == user {
		m.seen[id] = time.Now()
	}
	m.mu.Unlock()
	if !ok || owner != user {
		return nil, errors.New("renderer session unavailable")
	}
	var keys []string
	switch action {
	case "SetAVTransportURI":
		keys = []string{"CurrentURI", "CurrentURIMetaData"}
	case "Play":
		keys = []string{"Speed"}
	case "Seek":
		keys = []string{"Unit", "Target"}
	case "Pause", "Stop", "GetPositionInfo", "GetTransportInfo":
	default:
		return nil, errors.New("unsupported renderer action")
	}
	var body strings.Builder
	body.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + device.service + `"><InstanceID>0</InstanceID>`)
	// UPnP action inputs follow the service's declared argument order.
	// Alphabetic map ordering is not valid for Seek (Unit must precede Target).
	for _, key := range keys {
		body.WriteString("<" + key + ">")
		_ = xml.EscapeText(&body, []byte(args[key]))
		body.WriteString("</" + key + ">")
	}
	body.WriteString("</u:" + action + "></s:Body></s:Envelope>")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, device.control, strings.NewReader(body.String()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+device.service+"#"+action+`"`)
	response, err := m.client.Do(req)
	if err != nil {
		return nil, errors.New("renderer unreachable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("renderer refused %s", action)
	}
	decoder := xml.NewDecoder(io.LimitReader(response.Body, 128<<10))
	result := map[string]string{}
	for {
		token, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, errors.New("invalid renderer response")
		}
		if start, ok := token.(xml.StartElement); ok {
			switch start.Name.Local {
			case "RelTime", "TrackDuration", "CurrentTransportState", "TrackURI":
				var value string
				if e = decoder.DecodeElement(&value, &start); e != nil {
					return nil, e
				}
				result[start.Name.Local] = value
			}
		}
	}
	return result, nil
}

func (m *Manager) describe(ctx context.Context, location string) (Device, error) {
	base, err := privateURL(location)
	if err != nil {
		return Device{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return Device{}, err
	}
	response, err := m.client.Do(req)
	if err != nil {
		return Device{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Device{}, errors.New("description unavailable")
	}
	var description struct {
		URLBase string `xml:"URLBase"`
		Device  struct {
			Name     string `xml:"friendlyName"`
			Type     string `xml:"deviceType"`
			Services []struct {
				Type    string `xml:"serviceType"`
				Control string `xml:"controlURL"`
			} `xml:"serviceList>service"`
		} `xml:"device"`
	}
	if err = xml.NewDecoder(io.LimitReader(response.Body, 128<<10)).Decode(&description); err != nil {
		return Device{}, err
	}
	if description.URLBase != "" {
		advertised, e := privateURL(description.URLBase)
		if e != nil || advertised.Hostname() != base.Hostname() {
			return Device{}, errors.New("unsafe renderer URLBase")
		}
		base = advertised
	}
	if !strings.HasPrefix(description.Device.Type, "urn:schemas-upnp-org:device:MediaRenderer:") {
		return Device{}, errors.New("not a renderer")
	}
	for _, service := range description.Device.Services {
		if service.Type != servicePrefix+"1" && service.Type != servicePrefix+"2" && service.Type != servicePrefix+"3" {
			continue
		}
		relative, e := url.Parse(service.Control)
		if e != nil {
			continue
		}
		control := base.ResolveReference(relative)
		if _, e = privateURL(control.String()); e != nil || control.Hostname() != base.Hostname() {
			continue
		}
		sum := sha256.Sum256([]byte(control.String()))
		name := strings.TrimSpace(description.Device.Name)
		if name == "" {
			name = "Network speaker"
		}
		if len(name) > 200 {
			name = name[:200]
		}
		return Device{ID: hex.EncodeToString(sum[:12]), Name: name, control: control.String(), service: service.Type, expires: time.Now().Add(5 * time.Minute)}, nil
	}
	return Device{}, errors.New("AVTransport unavailable")
}

func privateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || !ip.IsPrivate() || u.Scheme != "http" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("renderer address must be a private LAN IP over HTTP")
	}
	return u, nil
}
