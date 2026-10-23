package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// CurrentAPIVersion is the host extension API version this server implements.
// Manifests with a different api_version are rejected at discovery.
const CurrentAPIVersion = 1

// Known capability identifiers for the A08 boundary. Later tasks (A08b/c) add
// scheduling, storage isolation, and feature wiring; A08a only exercises probe.
const (
	CapabilityProbe = "probe"
)

// knownCapabilities lists capability ids the host recognises today.
var knownCapabilities = map[string]struct{}{
	CapabilityProbe: {},
}

// Manifest is the on-disk extension contract for one plugin directory.
type Manifest struct {
	// ID is a stable slug (directory name when empty). Letters, digits, hyphen.
	ID string `json:"id"`
	// Name is the human-readable label shown in the admin UI.
	Name string `json:"name"`
	// Version is the plugin's own semver-ish version string.
	Version string `json:"version"`
	// APIVersion must equal CurrentAPIVersion.
	APIVersion int `json:"api_version"`
	// Capabilities lists declared host capabilities the WASM implements.
	Capabilities []string `json:"capabilities"`
	// Entry is the WASM file name relative to the plugin directory.
	Entry string `json:"entry"`
	// Description is optional admin-facing text.
	Description string `json:"description,omitempty"`
}

// Validate checks required fields, API version, and capability names.
func (m Manifest) Validate() error {
	id := strings.TrimSpace(m.ID)
	if id == "" {
		return errors.New("manifest id is required")
	}
	if !validPluginID(id) {
		return fmt.Errorf("manifest id %q is invalid", id)
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("manifest name is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("manifest version is required")
	}
	if m.APIVersion != CurrentAPIVersion {
		return fmt.Errorf("unsupported api_version %d (want %d)", m.APIVersion, CurrentAPIVersion)
	}
	if len(m.Capabilities) == 0 {
		return errors.New("manifest capabilities must not be empty")
	}
	seen := make(map[string]struct{}, len(m.Capabilities))
	for _, c := range m.Capabilities {
		c = strings.TrimSpace(c)
		if c == "" {
			return errors.New("manifest capability must not be empty")
		}
		if _, ok := knownCapabilities[c]; !ok {
			return fmt.Errorf("unknown capability %q", c)
		}
		if _, dup := seen[c]; dup {
			return fmt.Errorf("duplicate capability %q", c)
		}
		seen[c] = struct{}{}
	}
	entry := strings.TrimSpace(m.Entry)
	if entry == "" {
		return errors.New("manifest entry is required")
	}
	if strings.Contains(entry, "/") || strings.Contains(entry, `\`) || entry == ".." {
		return errors.New("manifest entry must be a plain file name")
	}
	return nil
}

// ParseManifest decodes and validates a manifest JSON document.
func ParseManifest(r io.Reader) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	m.ID = strings.TrimSpace(m.ID)
	m.Name = strings.TrimSpace(m.Name)
	m.Version = strings.TrimSpace(m.Version)
	m.Entry = strings.TrimSpace(m.Entry)
	m.Description = strings.TrimSpace(m.Description)
	caps := make([]string, 0, len(m.Capabilities))
	for _, c := range m.Capabilities {
		caps = append(caps, strings.TrimSpace(c))
	}
	m.Capabilities = caps
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func validPluginID(id string) bool {
	if len(id) > 64 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0 && i < len(id)-1:
		default:
			return false
		}
	}
	return true
}
