package api

import "context"

// ModFactory constructs a fresh mod instance for one session. Factories must
// not return a nil Mod. The runtime may invoke a factory concurrently for
// different client sessions, so a factory must be safe for concurrent use.
type ModFactory func() Mod

// Mod is the lifecycle contract implemented by a Zicor mod.
//
// Init is called once for each mod session. If Init returns an error, the
// runtime must treat initialization as failed and must not call Shutdown.
// After successful initialization, Shutdown is called exactly once when the
// mod is unloaded or its session ends. The runtime cancels ModContext.Context
// before calling Shutdown and supplies a cleanup context with a bounded
// deadline.
type Mod interface {
	Init(ctx ModContext) error
	Shutdown(ctx context.Context) error
}
