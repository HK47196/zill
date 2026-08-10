# Game texture overrides

This directory contains editable PNG replacements that `zill build` compiles
back into the retail game's GIM resources. They are ordinary in-game texture
edits, not a PPSSPP replacement pack.

Each path identifies the source PAA archive, six-digit outer member index,
outer member name, and each indexed PAR child. For example:

```
pa/000002/2d/icon/scrparts.par/000000_4curs.gim.png
```

Every PNG below this directory is active automatically; there is no separate
registration manifest. The build validates the encoded archive and PAR
locators against the supported retail input, requires the PNG dimensions to
match the source GIM, and compiles pixels into the source image format.
Indexed images may use only exact colors from their existing GIM palettes.
Palettes, sibling resources, container order, and unrelated bytes are
preserved.
