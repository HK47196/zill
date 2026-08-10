# Frozen font and texture patch

`manifest.toml` describes three authenticated XOR transforms applied to exact
ULJM05410 1.03 archive members. Each compressed `.zpatch` is authenticated in
compressed and expanded form; the complete retail input and reconstructed
result are also SHA-256 guarded. The patches contain only byte deltas, not
complete retail members.

`metrics.toml` is the complete 2,637-glyph CP932 repertoire and advance table
for the exact final `zillfont.paf`. Contributor validation uses it without game
assets, and release layout uses the same advances that the builder installs.

`fs-tahoma-8px.otf` is retained CC BY-SA 3.0 design provenance. The canonical
cells and metrics were frozen after migration; the Go builder does not carry a
font renderer or require Python imaging libraries.
