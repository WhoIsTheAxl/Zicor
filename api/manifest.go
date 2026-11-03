package api

// Manifest describes a mod using manifest.json.
//
// The mod's conventional source entry file is entry.go; it is intentionally
// not configurable in this first manifest schema.
type Manifest struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	APIVersion  string       `json:"api_version"`
	Description string       `json:"description,omitempty"`
	Authors     []string     `json:"authors,omitempty"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

// Dependency declares another mod required by this mod.
// Version is a constraint string interpreted by the mod loader.
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
