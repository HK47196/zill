# Trinity PS3 text extraction

The English and Japanese PS3 releases can be inventoried without modifying
their retail trees. Keep generated retail-derived output under the ignored
`build/` tree:

```sh
./zill trinity-extract --game-dir Trinity/TrinityEN \
  --output build/trinity/english
./zill trinity-extract --game-dir Trinity/TrinityJP \
  --output build/trinity/japanese
```

The command decodes the eight localized LINKDATA members and extracts PARAM.SFO
strings and all SFM trophy XML files. A disc `.dkey` decrypts the ISO layer, not
the SELF executable layer. To include executable strings, use RPCS3's dedicated
binary-decryption operation on a temporary `EBOOT.BIN` copy, then pass the
result without placing it in the retail tree:

```sh
rpcs3 --headless --decrypt /tmp/trinity-eboot/EBOOT.BIN
./zill trinity-extract --game-dir Trinity/TrinityEN \
  --eboot-elf /tmp/trinity-eboot/EBOOT.elf \
  --output build/trinity/english
```

Use the same temporary-copy operation for the Japanese `EBOOT.BIN`, then pass
that release's ELF to its extraction command. The extractor authenticates the
release-specific executable rather than accepting one region's ELF for the
other.

Each LINKDATA asset and optional decrypted ELF is preserved losslessly and
accompanied by a JSON inventory of printable string candidates with byte
offsets. Candidate inventories are navigation aids; the lossless binary files
are authoritative. `manifest.json` records source and output hashes, storage
metadata, and excluded non-text inputs. Output is published atomically and is
never overwritten.

With both LINKDATA extractions present, search English text with ripgrep and
display the structurally equivalent Japanese record:

```sh
./zill trinity-search --english build/trinity/english \
  --japanese build/trinity/japanese 'Areus! A fine match'
```

`trinity-search` requires `rg` on `PATH` and accepts ripgrep regular-expression
syntax plus `--ignore-case` and `--max-count`. It pairs localized strings by
LINKDATA member and nested record path; it never assumes that English and
Japanese byte offsets or heuristic candidate ordinals are equal. An ambiguous
1,200-row table in member 275 is deliberately omitted because its rows were
reordered and its metadata is not a unique identity. Executable strings remain
lossless searchable inventories, but are not claimed as cross-release
equivalents because their binaries do not provide the same record identity.
