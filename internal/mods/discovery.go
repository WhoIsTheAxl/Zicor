// Package mods discovers, validates, and orders installed Zicor mods.
package mods

import (
	"container/heap"
	"errors"
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
// Results are sorted by mod ID for deterministic behavior. Symlinked entries,
// non-directory entries, and directories without a manifest are ignored.
// A manifest itself must be a regular file; symlinked manifests are rejected.
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

		info, err := os.Lstat(manifestPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("inspect manifest for %q: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("manifest %q must be a regular file (symlinks are not allowed)", manifestPath)
		}

		file, err := os.Open(manifestPath)
		if err != nil {
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
// present; a declared version must exactly match. If multiple mods are ready
// at the same time, their IDs determine the order. Cycles are reported.
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

	indegree := make(map[string]int, len(byID))
	dependents := make(map[string][]string, len(byID))
	for id, mod := range byID {
		for _, dep := range mod.Manifest.Dependencies {
			dependency, ok := byID[dep.ID]
			if !ok {
				return nil, fmt.Errorf("mod %q requires missing dependency %q", id, dep.ID)
			}
			if dep.Version != "" && dependency.Manifest.Version != dep.Version {
				return nil, fmt.Errorf("mod %q requires %s version %q, found %q", id, dep.ID, dep.Version, dependency.Manifest.Version)
			}
			indegree[id]++
			dependents[dep.ID] = append(dependents[dep.ID], id)
		}
	}

	ready := &idHeap{}
	heap.Init(ready)
	for id := range byID {
		if indegree[id] == 0 {
			heap.Push(ready, id)
		}
	}

	order := make([]DiscoveredMod, 0, len(byID))
	for ready.Len() > 0 {
		id := heap.Pop(ready).(string)
		order = append(order, byID[id])
		for _, dependentID := range dependents[id] {
			indegree[dependentID]--
			if indegree[dependentID] == 0 {
				heap.Push(ready, dependentID)
			}
		}
	}
	if len(order) != len(byID) {
		cyclic := make([]string, 0, len(byID)-len(order))
		for id, degree := range indegree {
			if degree > 0 {
				cyclic = append(cyclic, id)
			}
		}
		sort.Strings(cyclic)
		return nil, fmt.Errorf("dependency cycle detected among mods: %v", cyclic)
	}
	return order, nil
}

type idHeap []string

func (h idHeap) Len() int           { return len(h) }
func (h idHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h idHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *idHeap) Push(value any) {
	*h = append(*h, value.(string))
}

func (h *idHeap) Pop() any {
	old := *h
	last := len(old) - 1
	value := old[last]
	*h = old[:last]
	return value
}
