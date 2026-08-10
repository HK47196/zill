# Maintainer release build

`zill build` is maintainer-only. It is the definitive asset-backed build and
validation step because it verifies stored human-readable Japanese against the
supported, legally obtained retail `PSP_GAME` tree while producing the release
output. A successful build does not prove that every screen is visually
correct; runtime QA remains the visual authority.

Run the asset-free contributor gate, then the build:

```sh
go test ./...
go vet ./...
./zill check
./zill build --game-dir /path/to/PSP_GAME
```

The build writes `build/PSP_GAME/` and does not modify the input tree. It is
the maintainer's responsibility to verify the supported retail revision and
perform runtime QA before publication.

Maintainer-owned data is kept separate from ordinary message contributions:

- `release/strings/eboot.toml` and `release/strings/equipment.toml` hold fixed
  translated strings.
- `release/layout/categories.toml` and `release/layout/consumer-map.toml` hold
  layout configuration.

Do not ask ordinary contributors to provide retail assets or run the build.
