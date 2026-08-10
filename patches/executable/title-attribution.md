# Title attribution geometry

The retail title screen draws `2d/title/title1.gim` at `(90, 7)` using only
the `(0, 0, 295, 232)` portion of its 512-by-256 texture. The generated
attribution occupies the largely transparent lower-right texture area,
including a few low-alpha fringe pixels near the retail source-rectangle edge,
so changing pixels alone cannot make the full attribution visible.

Two guarded instructions expand the source rectangle to 384 by 256. At the
unchanged draw position, the enlarged rectangle ends at `(474, 263)` within
the PSP's 480-by-272 display. The additional area is transparent except for
the attribution generated during `zill build`; the texture dimensions and draw
position remain unchanged.
