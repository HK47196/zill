# Large-memory request

The retail runtime reserves `0x4400` KiB (17 MiB) for UserSbrk. The guarded
little-endian field at file offset `0x26554c` changes that reservation to
`0x6000` KiB (24 MiB). The additional 7 MiB covers the observed transient
gameplay memory pressure without reserving all later-model RAM for the game
heap.

This executable field is used only with the companion `PARAM.SFO` transform in
`../system/param-sfo.toml`. The builder refuses an unexpected source value and
verifies the patched value before publication.
