# Runtime patches

This directory is the only production home for binary modifications other than
fixed-field text replacements.

- `executable/manifest.toml` is the complete ordered, guarded `BOOT.BIN` patch
  input. Its adjacent documents explain every instruction and structure field.
- `system/param-sfo.toml` describes the guarded `PARAM.SFO` memory request.
- Fixed executable strings are release data in
  `../release/strings/eboot.toml`, not runtime code patches.

The Go release builder must parse these manifests. It must not duplicate patch
bytes or parameters in source code.

The current executable changes are 30 in-place MIPS instruction replacements
and three guarded data/header fields. They do not inject or link a standalone
routine, so there is no honest standalone assembly source to preserve. The
manifest records the exact before/after assembly for every instruction. If a
future patch introduces an assembled routine, its `.s` source, fixed address or
linker contract, and reproducible assembly command belong in that feature's
directory; generated objects remain under ignored `build/`.
