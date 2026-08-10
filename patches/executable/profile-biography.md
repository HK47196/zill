# Profile biography storage and rendering

The retail profile biography buffer is only `0x168` bytes and overlaps other
screen-object fields when English text grows. Two guarded instructions redirect
the biography copy destination and renderer source to the dedicated 0x400-byte
buffer at module address `0x36c000` (runtime address `0x08b70000`).

Seven additional guarded instructions support the larger rendered text:

- increase each renderer glyph bucket from 128 to 640 glyphs;
- grow the renderer stack frame;
- move the copied-text scratch pointer and terminator; and
- update every affected stack restoration.

These nine instruction changes depend on the ELF/BSS extension described in
`message-arena.md`. Release validation also constrains canonical profile text to
the verified eight-line panel geometry. The exact before/after instructions are
in `manifest.toml`.
