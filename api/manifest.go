package api

import (
	"bytes"
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

// ValidateModID validates a stable mod identifier.
func ValidateModID(id string) error {
	if !modIDPattern.MatchString(id) {
		return fmt.Errorf("invalid mod id %q: use 1-64 lowercase letters, digits, '_' or '-', starting with a letter", id)
	}
	return nil
}

// Validate checks the manifest's required fields and internal consistency.
// It does not check whether dependencies are installed or compatible.
func (m Manifest) Validate() error {
	if err := ValidateModID(m.ID); err != nil {
		return err
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("mod name is required")
	}
	if !isValidSemanticVersion(m.Version) {
		return fmt.Errorf("invalid mod version %q: expected semantic version", m.Version)
	}
	if m.APIVersion != CurrentAPIVersion {
		return fmt.Errorf("unsupported API version %q (this runtime supports %q)", m.APIVersion, CurrentAPIVersion)
	}

	seen := make(map[string]struct{}, len(m.Dependencies))
	for _, dep := range m.Dependencies {
		if err := ValidateModID(dep.ID); err != nil {
			return fmt.Errorf("invalid dependency: %w", err)
		}
		if dep.ID == m.ID {
			return fmt.Errorf("mod %q cannot depend on itself", m.ID)
		}
		if _, ok := seen[dep.ID]; ok {
			return fmt.Errorf("duplicate dependency %q", dep.ID)
		}
		seen[dep.ID] = struct{}{}
		if dep.Version != "" && !isValidSemanticVersion(dep.Version) {
			return fmt.Errorf("invalid version %q for dependency %q", dep.Version, dep.ID)
		}
	}
	return nil
}

// DecodeManifest decodes one manifest from r, rejects unknown or duplicate
// fields, rejects trailing JSON values, enforces a 1 MiB size limit, and
// validates the result.
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
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return manifest, fmt.Errorf("invalid manifest JSON: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
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

func isValidSemanticVersion(version string) bool {
	if !versionPattern.MatchString(version) {
		return false
	}

	coreAndPrerelease := strings.SplitN(version, "+", 2)[0]
	separator := strings.IndexByte(coreAndPrerelease, '-')
	if separator < 0 {
		return true
	}

	prerelease := coreAndPrerelease[separator+1:]
	for _, identifier := range strings.Split(prerelease, ".") {
		numeric := true
		for _, char := range identifier {
			if char < '0' || char > '9' {
				numeric = false
				break
			}
		}
		if numeric && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("manifest contains multiple JSON values")
	} else if err != io.EOF {
		return fmt.Errorf("invalid trailing JSON data: %w", err)
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object field name is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object field %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("unterminated JSON object")
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("unterminated JSON array")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}
