// Package api defines the public contract exposed to Zicor mods.
package api

// Mod is the lifecycle contract implemented by a Zicor mod.
//
// A mod is initialized once for each Zicor session. The context is scoped to
// that session and must not be retained after the mod has been shut down.
// The loader/runtime is responsible for defining how shutdown is signalled;
// this initial API deliberately keeps the required contract small.
type Mod interface {
	Init(ctx ModContext) error
}
