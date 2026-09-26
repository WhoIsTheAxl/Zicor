# Zicor

**Zicor** is a proxy-based mod loader for Minecraft Bedrock Edition, written in Go
and built on [Gophertunnel](https://github.com/Sandertv/gophertunnel).

> ⚠️ **Status: Early development.** The core proxy works — the mod API does not exist yet.
> If you're reading this, the project is a foundation, not a finished product. See
> [Roadmap](#roadmap).

## What is this?

Zicor runs as a **man-in-the-middle proxy** between your Bedrock client and any Bedrock
server:

```
Client (phone/PC)  ←— RakNet —→  Zicor proxy  ←— RakNet —→  Bedrock server
```

Because every packet passes through Zicor, mods can inspect, transform, cancel, and inject
packets in **both directions**. This enables a class of mods server plugins can't do:
client-only visuals, HUD overlays, session automation, packet tooling — the server never
knows the difference.

## What works today

- ✅ Live-account authentication (Microsoft/Xbox) with cached `token.json`
- ✅ RakNet listener + upstream dial to any Bedrock server
- ✅ Bidirectional packet relay (client ↔ server)
- ✅ Spawn handshake & status provider pass-through (server MOTP shows through the proxy)
- ✅ TOML config, auto-generated with sane defaults on first run
- 🚧 Packet hook layer (`internal/packets`) — early prototype
- ❌ Mod loading / mod API — not implemented yet

## Quick start

Requires **Go 1.21+**.

```bash
git clone https://github.com/WhoIsTheAxl/Zicor.git
cd Zicor
go build -ldflags="-checklinkname=0" -o zicor ./cmd/Zicor/
./Zicor
```

# ⚠️ CAUTION
- Always use flags `-ldflags="-checklinkname=0" -o` on build, or else it will cause a **"net.zoneCache"** error.

1. On first run, a browser opens for Microsoft sign-in. The token is cached to `token.json`.
2. Edit the generated `config.toml`:

```toml
[Connection]
  LocalAddress = "127.0.0.1:19132"    # what your client connects to
  RemoteAddress = "play.cubecraft.net:19132"  # the real server
```

3. In Minecraft, add your proxy machine's address as an external server and join.
4. Packets now flow through Zicor (watch the logs: `Client -> Server: ...`).

## Architecture

| Path | Purpose |
|---|---|
| `cmd/Zicor/` | Entry point, config, auth, listener & relay loop |
| `internal/packets/` | Packet hook layer — the future seam between core and mods |
| `pkg/ProxGopher/` | Vendored, heavily-modified fork of Gophertunnel (see below) |

**ProxGopher** is our internal fork of Gophertunnel. It is intentionally in-tree because
Zicor plans deep modifications to the protocol layer. Upstream changes are ported manually;
divergence is documented as it accumulates.

## Roadmap

- [ ] Small cleanup pass (logging, error handling, build artifacts)
- [ ] First mod: client-only coordinates HUD
- [ ] Minimal `Mod` interface + registry (extracted from the HUD mod, not imagined)
- [ ] In-game `!zicor` command (list / toggle mods)
- [ ] Second mod to prove the API isn't HUD-shaped
- [ ] Heavy ProxGopher modifications (guided by real mod needs)
- [ ] (v2) Lua scripting layer — user mods without compiling Go

## Contributing

This project is open to collaborators.
The core plumbing is done; the fun part (mods) isn't. If any of this sounds like you:

- You know Go (or want to learn it via a real project)
- You play Bedrock and have ideas for client-side mods
- You know the Bedrock protocol / Gophertunnel

...open an issue, say hi, and grab a `good first issue`.

## License

[MIT](LICENSE)