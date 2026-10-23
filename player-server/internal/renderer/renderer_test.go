package renderer

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPrivateURL(t *testing.T) {
	for _, test := range []struct {
		raw     string
		allowed bool
	}{
		{"http://192.168.1.2:1400/device.xml", true}, {"http://10.1.1.2/root", true},
		{"http://[fd00::1]/root", true}, {"http://127.0.0.1/root", false},
		{"http://169.254.169.254/latest", false}, {"http://8.8.8.8/", false},
		{"http://speaker.local/", false}, {"http://user:pass@192.168.1.2/", false},
		{"https://192.168.1.2/", false}, {"file:///etc/passwd", false},
	} {
		t.Run(test.raw, func(t *testing.T) {
			_, err := privateURL(test.raw)
			if (err == nil) != test.allowed {
				t.Fatalf("allowed=%v, error=%v", test.allowed, err)
			}
		})
	}
}

func TestDescriptionControlBoundaries(t *testing.T) {
	for _, control := range []struct {
		url   string
		valid bool
	}{{"/control", true}, {"http://192.168.1.4:9000/control", true}, {"http://192.168.1.5/control", false}, {"http://127.0.0.1/control", false}, {"http://169.254.169.254/control", false}, {"http://attacker.example/control", false}} {
		t.Run(control.url, func(t *testing.T) {
			m := New()
			m.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				body := `<root><device><deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType><friendlyName>Kitchen &amp; dining</friendlyName><serviceList><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>` + control.url + `</controlURL></service></serviceList></device></root>`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			device, err := m.describe(context.Background(), "http://192.168.1.4/root.xml")
			if (err == nil) != control.valid {
				t.Fatalf("valid=%v err=%v", control.valid, err)
			}
			if err == nil && device.Name != "Kitchen & dining" {
				t.Fatal(device.Name)
			}
		})
	}
}

func TestOwnershipAndSOAP(t *testing.T) {
	m := New()
	m.devices["speaker"] = Device{ID: "speaker", Name: "Kitchen", control: "http://192.168.1.4/control", service: servicePrefix + "1", expires: time.Now().Add(time.Minute)}
	if err := m.Command("unknown", 1, "first", 1, "load"); err == nil {
		t.Fatal("unknown accepted")
	}
	if err := m.Command("speaker", 1, "first", 1, "load"); err != nil {
		t.Fatal(err)
	}
	if err := m.Command("speaker", 2, "second", 1, "load"); err == nil {
		t.Fatal("other account accepted")
	}
	called := false
	m.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("credential leak")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "a&amp;b") || !strings.Contains(string(body), "<InstanceID>0</InstanceID>") {
			t.Fatal(string(body))
		}
		if r.Header.Get("SOAPAction") != `"urn:schemas-upnp-org:service:AVTransport:1#SetAVTransportURI"` {
			t.Fatal(r.Header)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><RelTime>00:01:04</RelTime></s:Body></s:Envelope>`))}, nil
	})
	if _, err := m.Action(context.Background(), "speaker", 2, "Play", nil); err == nil || called {
		t.Fatal("cross-account action reached renderer")
	}
	result, err := m.Action(context.Background(), "speaker", 1, "SetAVTransportURI", map[string]string{"CurrentURI": "a&b"})
	if err != nil || result["RelTime"] != "00:01:04" {
		t.Fatalf("%v %v", result, err)
	}
	m.Release("speaker", 2)
	if err = m.Command("speaker", 2, "second", 1, "load"); err == nil {
		t.Fatal("foreign release succeeded")
	}
	m.Release("speaker", 1)
	if err = m.Command("speaker", 2, "second", 1, "load"); err != nil {
		t.Fatal(err)
	}
	m.seen["speaker"] = time.Now().Add(-3 * time.Minute)
	if err = m.Command("speaker", 1, "third", 1, "load"); err != nil {
		t.Fatal("abandoned speaker not reclaimable", err)
	}
}

func TestRendererCommandRevisions(t *testing.T) {
	m := New()
	m.devices["speaker"] = Device{ID: "speaker", expires: time.Now().Add(time.Minute)}
	// Reversed network arrival: Stop arrives before its pending initial Load.
	if err := m.Command("speaker", 1, "first", 2, "stop"); err != nil {
		t.Fatal(err)
	}
	m.Release("speaker", 1)
	if err := m.Command("speaker", 1, "first", 1, "load"); err == nil {
		t.Fatal("late initial Load superseded completed Stop")
	}
	if err := m.Command("speaker", 1, "second", 1, "load"); err != nil {
		t.Fatal(err)
	}
	if err := m.Command("speaker", 1, "first", 99, "load"); err == nil {
		t.Fatal("old session overrode newer Load")
	}
	if err := m.Command("speaker", 2, "second", 2, "stop"); err == nil {
		t.Fatal("foreign account stopped output")
	}
	if err := m.Command("speaker", 1, "second", 2, "load"); err != nil {
		t.Fatal(err)
	}
	if err := m.Command("speaker", 1, "second", 1, "seek"); err == nil {
		t.Fatal("old track seek controlled new track")
	}
	if err := m.Command("speaker", 1, "second", 3, "stop"); err != nil {
		t.Fatal(err)
	}
	m.Release("speaker", 1)
	if err := m.Command("speaker", 1, "first", 100, "load"); err == nil {
		t.Fatal("retired session restarted after newer session stopped")
	}
}

func TestRendererSOAPArgumentOrder(t *testing.T) {
	for _, test := range []struct {
		action string
		args   map[string]string
		order  []string
	}{
		{"SetAVTransportURI", map[string]string{"CurrentURI": "http://player/stream", "CurrentURIMetaData": ""}, []string{"InstanceID", "CurrentURI", "CurrentURIMetaData"}},
		{"Play", map[string]string{"Speed": "1"}, []string{"InstanceID", "Speed"}},
		{"Seek", map[string]string{"Target": "00:01:04", "Unit": "REL_TIME"}, []string{"InstanceID", "Unit", "Target"}},
		{"Pause", nil, []string{"InstanceID"}}, {"Stop", nil, []string{"InstanceID"}},
		{"GetPositionInfo", nil, []string{"InstanceID"}}, {"GetTransportInfo", nil, []string{"InstanceID"}},
	} {
		t.Run(test.action, func(t *testing.T) {
			m := New()
			m.devices["speaker"] = Device{ID: "speaker", control: "http://192.168.1.4/control", service: servicePrefix + "1", expires: time.Now().Add(time.Minute)}
			if err := m.Command("speaker", 1, "session", 1, "load"); err != nil {
				t.Fatal(err)
			}
			var received []string
			m.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				decoder := xml.NewDecoder(r.Body)
				depth := 0
				received = nil
				for {
					token, err := decoder.Token()
					if err == io.EOF {
						break
					}
					if err != nil {
						return nil, err
					}
					switch token := token.(type) {
					case xml.StartElement:
						depth++
						if depth == 4 {
							received = append(received, token.Name.Local)
						}
					case xml.EndElement:
						depth--
					}
				}
				status := http.StatusOK
				// A strict receiver rejects arguments in any other order.
				if !reflect.DeepEqual(received, test.order) {
					status = http.StatusInternalServerError
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body/></s:Envelope>`))}, nil
			})
			if test.action == "Seek" {
				wrong, err := http.NewRequest(http.MethodPost, "http://192.168.1.4/control", strings.NewReader(`<Envelope><Body><Seek><InstanceID>0</InstanceID><Target>00:01:04</Target><Unit>REL_TIME</Unit></Seek></Body></Envelope>`))
				if err != nil {
					t.Fatal(err)
				}
				rejected, err := m.client.Do(wrong)
				if err != nil {
					t.Fatal(err)
				}
				_ = rejected.Body.Close()
				if rejected.StatusCode != http.StatusInternalServerError {
					t.Fatal("receiver accepted reversed Seek inputs")
				}
			}
			if _, err := m.Action(context.Background(), "speaker", 1, test.action, test.args); err != nil {
				t.Fatalf("strict receiver refused %s with inputs %v: %v", test.action, received, err)
			}
			if !reflect.DeepEqual(received, test.order) {
				t.Fatalf("wire arguments %v, want %v", received, test.order)
			}
		})
	}
}
