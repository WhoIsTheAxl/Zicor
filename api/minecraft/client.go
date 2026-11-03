// Package minecraft contains the public Minecraft-facing mod API.
package minecraft

// Client represents the local Minecraft client connected through Zicor.
//
// It intentionally exposes capabilities rather than Gophertunnel's concrete
// connection type, keeping mods independent of Zicor's networking backend.
type Client interface {
	// SendMessage sends a message to the connected Minecraft client.
	SendMessage(message string) error

	// Name returns the authenticated player's display name.
	Name() string

	// XUID returns the authenticated player's Xbox user ID, if available.
	XUID() string

	// ServerAddress returns the upstream server address for this session.
	ServerAddress() string

	// Connected reports whether the client session is currently connected.
	Connected() bool
}
