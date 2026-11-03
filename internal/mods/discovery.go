// Package mods discovers and validates installed Zicor mod manifests.
package mods

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/WhoIsTheAxl/Zicor/api"
)

// DiscoveredMod is a validated manifest and its containing directory.
// It does not imply that the mod's Go source has been compiled or loaded.
type DiscoveredMod struct {
	Directory string
	Manifest  api.Manifest
}

// Discover scans the immediate child directories of root for manifest.json.
// Results are sorted by mod ID for deterministic behavior. Symlinked entries
// and non-directory entries are ignored.
func Discover(root string) ([]DiscoveredMod, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read mods directory %q: %w", root, err)
	}

	mods := make([]DiscoveredMod, 0, len(entries))
	ids := make(map[string]string, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		manifestPath := filepath.Join(dir, "manifest.json")
		file, err := os.Open(manifestPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("open manifest for %q: %w", entry.Name(), err)
		}
		manifest, decodeErr := api.DecodeManifest(file)
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("load manifest %q: %w", manifestPath, decodeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close manifest %q: %w", manifestPath, closeErr)
		}
		if previous, exists := ids[manifest.ID]; exists {
			return nil, fmt.Errorf("duplicate mod id %q in %q and %q", manifest.ID, previous, dir)
		}
		ids[manifest.ID] = dir
		mods = append(mods, DiscoveredMod{Directory: dir, Manifest: manifest})
	}

	sort.Slice(mods, func(i, j int) bool {
		return mods[i].Manifest.ID < mods[j].Manifest.ID
	})
	return mods, nil
}

// ResolveOrder returns mods in dependency-first order. Dependencies must be
// present; a declared exact version must match. Cycles are reported explicitly.
func ResolveOrder(discovered []DiscoveredMod) ([]DiscoveredMod, error) {
	byID := make(map[string]DiscoveredMod, len(discovered))
	for _, mod := range discovered {
		id := mod.Manifest.ID
		if _, exists := byID[id]; exists {
			return nil, fmt.Errorf("duplicate mod id %q", id)
		}
		if err := mod.Manifest.Validate(); err != nil {
			return nil, fmt.Errorf("invalid mod %q: %w", id, err)
		}
		byID[id] = mod
	}

	state := make(map[string]uint8, len(byID))
	order := make([]DiscoveredMod, 0, len(byID))
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			return fmt.Errorf("dependency cycle detected at mod %q", id)
		case 2:
			return nil
		}
		mod, ok := byID[id]
		if !ok {
			return fmt.Errorf("required mod %q is not installed", id)
		}
		state[id] = 1
		for _, dep := range mod.Manifest.Dependencies {
			dependency, ok := byID[dep.ID]
			if !ok {
				return fmt.Errorf("mod %q requires missing dependency %q", id, dep.ID)
			}
			if dep.Version != "" && dependency.Manifest.Version != dep.Version {
				return fmt.Errorf("mod %q requires %s version %q, found %q", id, dep.ID, dep.Version, dependency.Manifest.Version)
			}
			if err := visit(dep.ID); err != nil {
				return err
			}
		}
		state[id] = 2
		order = append(order, mod)
		return nil
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return order, nil
}
