# Large-memory request

The retail runtime reserves `0x4400` KiB (17 MiB) for UserSbrk. The guarded
little-endian field at file offset `0x26554c` changes that reservation to
`0xc400` KiB (49 MiB), adding the PSP Slim model's extra 32 MiB while retaining
the original top-of-memory headroom.

This executable field is used only with the companion `PARAM.SFO` transform in
`../system/param-sfo.toml`. The builder refuses an unexpected source value and
verifies the patched value before publication.
