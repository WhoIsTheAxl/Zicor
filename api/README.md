# Zicor Mod API

The `api` module is the public contract for Zicor mods. Mods should import
this package and avoid depending on `internal/` or Gophertunnel directly.

## Minimal mod contract

A mod implements `api.Mod`:

```go
package example

import "github.com/WhoIsTheAxl/Zicor/api"

type ExampleMod struct{}

func (ExampleMod) Init(ctx api.ModContext) error {
	ctx.Logger().Info("example mod initialized", "mod", ctx.Info().ID)
	ctx.Client().SendMessage("Hello from ExampleMod!")
	return nil
}
```

Zicor supplies the `ModContext`. It exposes the session's Minecraft client,
a mod-scoped logger, and read-only mod metadata. A mod does not construct its
own context.

## Minimum mod directory

The intended minimum directory layout is:

```text
Mods/
└── ExampleMod/
    ├── entry.go
    └── manifest.json
```

The manifest describes the mod; `entry.go` is the conventional source entry
file. The runtime mechanism used to compile/load mod code is still being
designed, so this API contract does not yet promise that arbitrary source
folders can be loaded dynamically.

## Manifest

```json
{
  "id": "examplemod",
  "name": "Example Mod",
  "version": "1.0.0",
  "api_version": "0.1.0",
  "description": "An example Zicor mod",
  "authors": ["Axl"],
  "dependencies": []
}
```

- `id`, `name`, `version`, and `api_version` are required.
- `id` should be a stable, lowercase identifier.
- `version` identifies the mod release.
- `api_version` identifies the Zicor API contract the mod targets.
- `dependencies` is reserved for loader dependency resolution.

## Design boundaries

- `api/` contains public interfaces and data types.
- `internal/` contains Zicor's implementations.
- Public interfaces should describe capabilities, not expose implementation
  details such as `*minecraft.Conn` or Gophertunnel packet types.
- The API is intentionally small while the first real mods are being built.
