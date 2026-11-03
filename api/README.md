# Zicor Mod API

The `api` package is the public contract for Zicor mods. Mods should import
this package and avoid depending on `internal/` or Gophertunnel directly.

## Mod lifecycle

A mod implements `api.Mod`:

```go
package example

import (
	"context"

	"github.com/WhoIsTheAxl/Zicor/api"
)

type ExampleMod struct{}

func (ExampleMod) Init(ctx api.ModContext) error {
	ctx.Logger().Info("example mod initialized", "mod", ctx.Info().ID)
	return ctx.Client().SendMessage("Hello from ExampleMod!")
}

func (ExampleMod) Shutdown(ctx context.Context) error {
	// Stop and release mod-owned resources here.
	return nil
}
```

The runtime creates the `ModContext`; mods must not construct their own.
Its `Context()` is cancelled before `Shutdown` is called, allowing
mod-owned goroutines and operations to stop. `Shutdown` is called exactly
once after a successful initialization, with a cleanup context that has a
bounded deadline. It is not called when `Init` fails.

The context also exposes the session's Minecraft client, a mod-scoped logger,
a session event bus, and read-only mod metadata.

## Events

Events use exact concrete Go types. The generic helpers preserve type safety
for mod authors:

```go
type ChatReceived struct {
	Sender  string
	Message string
}

func (ExampleMod) Init(ctx api.ModContext) error {
	subscription, err := api.Subscribe[ChatReceived](
		ctx.Events(),
		func(eventCtx context.Context, event ChatReceived) error {
			ctx.Logger().Info("chat received", "sender", event.Sender)
			return nil
		},
	)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Context().Done()
		subscription.Cancel()
	}()
	return nil
}
```

Publishers use `api.Publish(ctx, bus, event)`. The runtime event bus invokes
handlers synchronously in subscription order. Handler errors are collected
without preventing later handlers from running; panics are recovered and
reported to the publisher. Subscription cancellation is safe to call more
than once. A handler already running when cancellation occurs may finish.

Each event is matched by its exact concrete Go type; a pointer event and a
value event are different event types. The event bus is safe for concurrent
use, but a handler should still protect its own mutable state.

## Minimum mod directory

```text
Mods/
└── ExampleMod/
    ├── entry.go
    └── manifest.json
```

The manifest describes the mod; `entry.go` is the conventional source entry
file. Discovery and source execution are separate concerns: Zicor can discover
and validate a manifest without dynamically executing arbitrary Go source.

## Current Go execution model

Zicor uses a static factory registry rather than Go's platform-restricted
`plugin` mechanism or IPC. The host executable imports each mod package and
registers its constructor:

```go
registry := mods.NewRegistry()
if err := registry.Register("examplemod", func() api.Mod {
	return example.New()
}); err != nil {
	return err
}
```

A registered factory creates a fresh mod instance for each session. The
registry validates identifiers, rejects duplicate registrations, sorts its
listed IDs, and reports missing registrations, nil instances, and factory
panics.

This means adding or changing Go mod code currently requires rebuilding the
Zicor executable. The `Mods/ModName/entry.go` layout remains the intended
source organization, but it is not a drop-in runtime compiler or loader.
The manifest discovery layer and compiled factory registry are deliberately
separate so a future execution mechanism can be added without exposing
Gophertunnel internals through the public API.

## Manifest

```json
{
  "id": "examplemod",
  "name": "Example Mod",
  "version": "1.0.0",
  "api_version": "0.1.0",
  "description": "An example Zicor mod",
  "authors": ["Axl"],
  "dependencies": [
    { "id": "corelib", "version": "1.2.0" }
  ]
}
```

Required fields are `id`, `name`, `version`, and `api_version`.

- IDs are 1–64 characters, start with a lowercase letter, and contain only
  lowercase letters, digits, underscores, or hyphens.
- Mod and dependency versions use semantic-version syntax.
- This API revision accepts only `api_version: "0.1.0"`.
- Dependency versions are exact matches in this initial implementation.
- Unknown JSON fields, malformed JSON, trailing JSON values, and manifests
  larger than 1 MiB are rejected.
- Duplicate dependency IDs and self-dependencies are rejected.

Use `api.DecodeManifest` to decode and validate a manifest from an
`io.Reader`, `api.ValidateModID` to validate an identifier, or
`Manifest.Validate` when constructing one in Go.

## Discovery and dependency ordering

The `internal/mods` package provides runtime-side helpers:

- `Discover(root)` scans immediate mod directories for `manifest.json`,
  validates each manifest, rejects duplicate mod IDs and symlinked manifests,
  and returns stable ID-sorted results.
- `ResolveOrder(mods)` verifies required dependencies and exact version
  constraints, then returns a deterministic dependency-first order. Missing
  dependencies, duplicate IDs, version mismatches, and dependency cycles are
  errors.

These helpers do not compile or execute `entry.go`.

## Design boundaries

- `api/` contains public interfaces and data types.
- `internal/` contains Zicor's implementations and runtime helpers.
- Public interfaces describe capabilities rather than exposing implementation
  details such as `*minecraft.Conn` or Gophertunnel packet types.
- Keep the public API small and add capabilities when a real mod use case
  establishes the required contract.
