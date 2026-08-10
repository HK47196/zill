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
./zill build --game-dir /path/to/PSP_GAME \
  --iso "/path/to/Zill O'll Infinite Plus (Japan).iso"
```

The build requires xdelta3 3.2.0. It stages and verifies every artifact before
replacing these outputs:

- `build/PSP_GAME/`
- `build/zill-english.iso`
- `build/zill-english.xdelta`

Both retail inputs remain read-only. The ISO writer preserves the authenticated
retail metadata, ordering, and alignment while reflowing files that no longer
fit their retail extents. The patch uses a pinned xdelta command and is decoded
and compared byte-for-byte with the translated ISO before publication. It
remains the maintainer's responsibility to perform runtime QA before release.

The title-screen attribution and the `PARAM.SFO` title displayed by PPSSPP use
the exact matching `v*` tag when `HEAD` is tagged. Otherwise the build uses the
abbreviated commit hash, with `-dirty` appended for modified tracked files.
Create an annotated release tag before the final build, for example:

```sh
git tag -a v1.0-alpha -m "v1.0 alpha"
```

Builds from a source archive without Git metadata must pass `--version VALUE`.
Official release builds should use a clean checkout at an exact release tag.

Replacement rolls back if an ordinary filesystem operation fails. Because the
three destinations are separate paths, interruption or host failure during the
final replacement can still leave a mixed set; release automation must consume
the outputs only after `zill build` exits successfully.

Maintainer-owned data is kept separate from ordinary message contributions:

- `release/strings/eboot.toml` and `release/strings/equipment.toml` hold fixed
  translated strings.
- `release/layout/categories.toml` and `release/layout/consumer-map.toml` hold
  layout configuration.
- `release/title/attribution.toml` holds the fixed title credit and URL. The
  versioned footer pixels are generated during `zill build`; no retail-derived
  title image or static title patch is stored in the repository.

Do not ask ordinary contributors to provide retail assets or run the build.

## Maintainer ISO round trip

The ISO reader/writer is validated separately from `zill build`. To prove that
the supported untouched retail ISO can be extracted and authored as a new,
byte-identical image, run:

```sh
scripts/pspiso-roundtrip.sh "/path/to/Zill O'll Infinite Plus (Japan).iso" \
  build/iso-roundtrip/maintainer-proof
```

The script checks available disk space and the supported retail fingerprint,
works only below the ignored `build/iso-roundtrip/` directory, leaves the
source ISO read-only, and finishes by comparing both SHA-256 and every byte.
The work directory must not already exist. This proof command does not create
a translated ISO, an xdelta patch, or run an emulator; those release outputs
are produced by `zill build`.
