# Relocated message arena

Wider translated banks do not fit the overlapping retail message arena. Six
guarded MIPS instructions relocate all three address-construction sites to the
appended BSS arena at module address `0x344000`. Three more instructions lay out
the runtime slots:

- bank 0: offset `0x0000`, capacity `0x9000`;
- sections 165 through 167: offset `0x9000`, capacity `0x4000`;
- ordinary sections: offset `0xd000`, capacity `0x17000`.

The message arena occupies `0x24000` bytes and ends at `0x368000`. A separate
profile buffer occupies `0x36c000..0x36c400`. The two guarded ELF fields extend
the writable `PT_LOAD` and `.bss` through the final end `0x36c400`. The older
pre-profile `0x368000` extent is not a valid final-build value.

The builder rejects any compiled bank that exceeds its assigned capacity,
applies the six relocation and three slot instructions as one feature, and
verifies both ELF fields structurally after patching.
