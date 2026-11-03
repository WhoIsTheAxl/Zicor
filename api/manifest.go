package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const maxManifestSize = 1 << 20

var (
	modIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
)

// Manifest describes a mod using manifest.json. The conventional source entry
// file is entry.go; source compilation and execution are runtime concerns.
type Manifest struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	APIVersion   string       `json:"api_version"`
	Description  string       `json:"description,omitempty"`
	Authors      []string     `json:"authors,omitempty"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

// Dependency declares another mod required by this mod. Version, when
// provided, must be an exact semantic version in this initial API revision.
type Dependency struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
}

// ModInfo is the read-only identity exposed to a running mod.
type ModInfo struct {
	ID         string
	Name       string
	Version    string
	APIVersion string
}

// Validate checks the manifest's required fields and internal consistency.
// It does not check whether dependencies are installed or compatible.
func (m Manifest) Validate() error {
	if !modIDPattern.MatchString(m.ID) {
		return fmt.Errorf("invalid mod id %q: use 1-64 lowercase letters, digits, '_' or '-', starting with a letter", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("mod name is required")
	}
	if !versionPattern.MatchString(m.Version) {
		return fmt.Errorf("invalid mod version %q: expected semantic version", m.Version)
	}
	if m.APIVersion != CurrentAPIVersion {
		return fmt.Errorf("unsupported API version %q (this runtime supports %q)", m.APIVersion, CurrentAPIVersion)
	}

	seen := make(map[string]struct{}, len(m.Dependencies))
	for _, dep := range m.Dependencies {
		if !modIDPattern.MatchString(dep.ID) {
			return fmt.Errorf("invalid dependency id %q", dep.ID)
		}
		if dep.ID == m.ID {
			return fmt.Errorf("mod %q cannot depend on itself", m.ID)
		}
		if _, ok := seen[dep.ID]; ok {
			return fmt.Errorf("duplicate dependency %q", dep.ID)
		}
		seen[dep.ID] = struct{}{}
		if dep.Version != "" && !versionPattern.MatchString(dep.Version) {
			return fmt.Errorf("invalid version %q for dependency %q", dep.Version, dep.ID)
		}
	}
	return nil
}

// DecodeManifest decodes one manifest from r, rejects unknown fields and
// trailing JSON values, enforces a 1 MiB size limit, and validates the result.
func DecodeManifest(r io.Reader) (Manifest, error) {
	var manifest Manifest
	if r == nil {
		return manifest, errors.New("manifest reader is nil")
	}

	data, err := io.ReadAll(io.LimitReader(r, maxManifestSize+1))
	if err != nil {
		return manifest, fmt.Errorf("read manifest: %w", err)
	}
	if len(data) > maxManifestSize {
		return manifest, fmt.Errorf("manifest exceeds maximum size of %d bytes", maxManifestSize)
	}

	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return manifest, errors.New("manifest contains multiple JSON values")
		}
		return manifest, fmt.Errorf("decode trailing manifest data: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return manifest, fmt.Errorf("invalid manifest: %w", err)
	}
	return manifest, nil
}
