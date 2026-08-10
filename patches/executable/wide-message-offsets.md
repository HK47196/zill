# Wide message-bank offsets

Translated banks use a zero reserved halfword followed by `uint32` absolute
record offsets. Retail banks use `uint16` offsets. The 12 guarded instruction
entries in `manifest.toml` change the relevant table masks, index scaling,
loads, and stores from halfword behavior to word behavior.

This feature is inseparable from the complete translated-bank build: all 279
banks must use the wide format, and the executable must never see a mixture of
retail and translated tables. The release verifier reopens every compiled bank,
checks its reserved field and all absolute offsets, and confirms that all 12
instruction guards and replacements match.

The exact instruction-level semantics are recorded beside each byte tuple in
the manifest. No prose reconstruction is authoritative.
