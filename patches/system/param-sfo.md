# PARAM.SFO large-memory request

The authenticated 472-byte ULJM05410 1.03 `PARAM.SFO` has SHA-256
`1b34f1ac553c319381bb033708facb7d6643e58403fc559a828e2663b477a404`
and does not contain `MEMSIZE`.

The builder validates the SFO magic/version, index and key/data bounds, unique
NUL-terminated ASCII keys, entry formats, lengths, and value bounds. It then
performs the only supported transform: append `MEMSIZE=1` as a four-byte integer
entry with format `0x0404`, rebuild key/data offsets while preserving every
existing entry, and align the final file to 16 bytes.

An input that already contains `MEMSIZE`, has another fingerprint, or violates
the documented structure is unsupported and fails before staging publication.
