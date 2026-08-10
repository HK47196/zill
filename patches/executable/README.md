# Executable patch manifest

`manifest.toml` is tied to the decrypted Japanese ULJM05410 1.03
`SYSDIR/BOOT.BIN` with SHA-256
`5e294dc84a7f0d50719ecd26cb24ffb3792f2d9445803690845a8f1fa1cb85a3`.

The builder applies all 35 entries in manifest order only after every source
guard matches. Before applying fixed EBOOT string translations, the complete
code/data-only result must have SHA-256
`dbcf974101a3ab04c3d3d7a5e0a607e9a3c0a186f60efef722b8429644c2c1c8`.

For `mips32le` entries, `offset` is the ELF file offset and `virtual_address`
is the module-relative address (`offset - 0x80`) used by this executable's text
mapping. `before` and `after` are Rizin's little-endian MIPS32 disassembly of
the exact guarded bytes. Header/data entries instead name the affected field.

Feature documents:

- `wide-message-offsets.md`: 12 offset-table instructions.
- `message-arena.md`: six address relocations, three slot bases, and two ELF
  extent fields.
- `profile-biography.md`: two biography redirects and seven renderer/storage
  changes.
- `title-attribution.md`: two title-logo source-rectangle instructions.
- `large-memory.md`: one UserSbrk reservation field.
