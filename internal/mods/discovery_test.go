package mods

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/WhoIsTheAxl/Zicor/api"
)

func writeMod(t *testing.T, root, folder, id, version string, deps []api.Dependency) {
	t.Helper()
	dir := filepath.Join(root, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := api.Manifest{ID: id, Name: id, Version: version, APIVersion: api.CurrentAPIVersion, Dependencies: deps}
	data := []byte(`{"id":"` + manifest.ID + `","name":"` + manifest.Name + `","version":"` + manifest.Version + `","api_version":"` + manifest.APIVersion + `"`)
	if len(deps) > 0 {
		data = []byte(strings.TrimSuffix(string(data), "}") + `,"dependencies":[`)
		for i, dep := range deps {
			if i > 0 { data = append(data, ',') }
			data = append(data, []byte(`{"id":"`+dep.ID+`","version":"`+dep.Version+`"}`)...)
		}
		data = append(data, []byte(`]}`)...)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverSortsAndIgnoresFoldersWithoutManifest(t *testing.T) {
	root := t.TempDir()
	writeMod(t, root, "z-folder", "zeta", "1.0.0", nil)
	writeMod(t, root, "a-folder", "alpha", "1.0.0", nil)
	if err := os.Mkdir(filepath.Join(root, "not-a-mod"), 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(root, "README.txt"), []byte("ignored"), 0o644); err != nil { t.Fatal(err) }

	got, err := Discover(root)
	if err != nil { t.Fatal(err) }
	ids := []string{got[0].Manifest.ID, got[1].Manifest.ID}
	if !reflect.DeepEqual(ids, []string{"alpha", "zeta"}) {
		t.Fatalf("unexpected order: %v", ids)
	}
}

func TestDiscoverRejectsDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	writeMod(t, root, "one", "same", "1.0.0", nil)
	writeMod(t, root, "two", "same", "1.0.0", nil)
	if _, err := Discover(root); err == nil {
		t.Fatal("expected duplicate ID error")
	}
}

func TestResolveOrder(t *testing.T) {
	root := t.TempDir()
	writeMod(t, root, "app", "app", "1.0.0", []api.Dependency{{ID: "core", Version: "2.0.0"}})
	writeMod(t, root, "core", "core", "2.0.0", nil)
	discovered, err := Discover(root)
	if err != nil { t.Fatal(err) }
	ordered, err := ResolveOrder(discovered)
	if err != nil { t.Fatal(err) }
	if ordered[0].Manifest.ID != "core" || ordered[1].Manifest.ID != "app" {
		t.Fatalf("dependency order incorrect: %s, %s", ordered[0].Manifest.ID, ordered[1].Manifest.ID)
	}
}

func TestResolveOrderRejectsMissingVersionMismatchAndCycle(t *testing.T) {
	tests := []struct {
		name string
		mods []api.Manifest
	}{
		{"missing", []api.Manifest{{ID:"app", Name:"app", Version:"1.0.0", APIVersion:api.CurrentAPIVersion, Dependencies:[]api.Dependency{{ID:"core"}}}}},
		{"version mismatch", []api.Manifest{{ID:"app", Name:"app", Version:"1.0.0", APIVersion:api.CurrentAPIVersion, Dependencies:[]api.Dependency{{ID:"core", Version:"2.0.0"}}},{ID:"core", Name:"core", Version:"1.0.0", APIVersion:api.CurrentAPIVersion}}},
		{"cycle", []api.Manifest{{ID:"app", Name:"app", Version:"1.0.0", APIVersion:api.CurrentAPIVersion, Dependencies:[]api.Dependency{{ID:"core"}}},{ID:"core", Name:"core", Version:"1.0.0", APIVersion:api.CurrentAPIVersion, Dependencies:[]api.Dependency{{ID:"app"}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := make([]DiscoveredMod, 0, len(tt.mods))
			for _, manifest := range tt.mods { input = append(input, DiscoveredMod{Manifest:manifest}) }
			if _, err := ResolveOrder(input); err == nil { t.Fatal("expected resolution error") }
		})
	}
}
