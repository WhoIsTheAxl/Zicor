package api

import (
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		ID:         "example",
		Name:       "Example Mod",
		Version:    "1.2.3",
		APIVersion: CurrentAPIVersion,
	}
}

func TestManifestValidate(t *testing.T) {
	m := validManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
}

func TestManifestValidateRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Manifest)
	}{
		{"invalid id", func(m *Manifest) { m.ID = "../escape" }},
		{"missing name", func(m *Manifest) { m.Name = "  " }},
		{"invalid version", func(m *Manifest) { m.Version = "latest" }},
		{"leading zero in prerelease", func(m *Manifest) { m.Version = "1.2.3-alpha.01" }},
		{"unsupported api", func(m *Manifest) { m.APIVersion = "9.0.0" }},
		{"self dependency", func(m *Manifest) { m.Dependencies = []Dependency{{ID: m.ID}} }},
		{"duplicate dependency", func(m *Manifest) { m.Dependencies = []Dependency{{ID: "core"}, {ID: "core"}} }},
		{"invalid dependency version", func(m *Manifest) { m.Dependencies = []Dependency{{ID: "core", Version: "next"}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validManifest()
			tt.edit(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateModID(t *testing.T) {
	for _, id := range []string{"a", "example_mod", "mod-1", "mod123"} {
		if err := ValidateModID(id); err != nil {
			t.Errorf("ValidateModID(%q) error = %v", id, err)
		}
	}
	for _, id := range []string{"", "1mod", "Upper", "../escape", strings.Repeat("a", 65)} {
		if err := ValidateModID(id); err == nil {
			t.Errorf("ValidateModID(%q) unexpectedly succeeded", id)
		}
	}
}

func TestDecodeManifest(t *testing.T) {
	input := `{"id":"example","name":"Example","version":"1.0.0","api_version":"0.1.0"}`
	got, err := DecodeManifest(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeManifest() error = %v", err)
	}
	if got.ID != "example" || got.Version != "1.0.0" {
		t.Fatalf("unexpected manifest: %+v", got)
	}
}

func TestDecodeManifestRejectsMalformedInput(t *testing.T) {
	tests := map[string]string{
		"unknown field":         `{"id":"example","name":"Example","version":"1.0.0","api_version":"0.1.0","surprise":true}`,
		"trailing value":        `{"id":"example","name":"Example","version":"1.0.0","api_version":"0.1.0"} {}`,
		"invalid json":          `{"id":`,
		"duplicate field":       `{"id":"first","id":"second","name":"Example","version":"1.0.0","api_version":"0.1.0"}`,
		"nested duplicate field": `{"id":"example","name":"Example","version":"1.0.0","api_version":"0.1.0","dependencies":[{"id":"core","id":"other"}]}`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeManifest(strings.NewReader(input)); err == nil {
				t.Fatal("expected decoding error")
			}
		})
	}
}

func TestDecodeManifestRejectsOversizedInput(t *testing.T) {
	input := strings.NewReader(strings.Repeat(" ", maxManifestSize+1))
	if _, err := DecodeManifest(input); err == nil {
		t.Fatal("expected size limit error")
	}
}

func TestDecodeManifestRejectsNilReader(t *testing.T) {
	if _, err := DecodeManifest(nil); err == nil {
		t.Fatal("expected nil reader error")
	}
}
