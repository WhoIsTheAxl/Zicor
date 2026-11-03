package api

import "github.com/WhoIsTheAxl/Zicor/api/minecraft"

// ModContext provides the Zicor services available to a mod.
//
// It is an application context, not context.Context from the standard
// library. Implementations are created by Zicor and scoped to a mod session.
type ModContext interface {
	// Client returns the Minecraft client represented by this session.
	Client() minecraft.Client

	// Logger returns a logger scoped to this mod.
	Logger() Logger

	// Info returns the metadata Zicor loaded for this mod.
	Info() ModInfo
}
