# Message consumers

`consumer-map.toml` answers one question that message text cannot: which game
code path consumes each message? It contains runtime-analysis ID sets and
groupings that cannot be derived from the reviewed category ranges. Fixed-store
validation also covers exact executable and guild UI consumers identified by
category or message ID.

## Verified storage contracts

Consumer evidence comes from both CDC event scripts and executable UI paths.
The following checks are release-blocking encoded-byte bounds, unlike
non-blocking visual-width warnings.

### CDC opcode consumers

The C names are literal CDC event-script opcodes, not message record types:

- C5 displays dialogue. Its runtime splitter starts a new page after every
  third line break, admits at most nine pages, and requires every statically
  known page payload to use fewer than 256 encoded bytes. The generated
  portrait subset contains every message with at least one verified C5 call
  whose raw display mode requests the narrower portrait layout.
- C20 builds a choice list from contiguous message records. The complete group,
  including one terminating NUL per choice, may use at most 767 encoded bytes.
- C22 displays centered text. Plain calls are notifications; progressive calls
  are cinematic text. The expanded static text must use fewer than 512 encoded
  bytes and fit at most nine four-line pages, each below 256 encoded bytes, with
  no line above 56 encoded bytes.

### Other fixed-store consumers

The validator also protects bounded labels, guild client labels, guild postings,
guild region descriptions, equipment feedback, the trap-disarm prompt,
character-creation choices, and chronicle entries. Character-creation choices
use 31-byte C-string storage and therefore admit at most 30 encoded bytes.
Chronicle bodies use the shared 768-byte expansion buffer,
whose writer signals overflow only after a write raises its count to 766. A
chronicle entry therefore admits at most 764 known payload bytes so that writing
its terminating NUL leaves the count below that threshold. Player-name
substitutions can use at most 16 encoded bytes under the eight-character input
and 17-byte C-string storage contract. Source message-record size is unrelated
to this runtime buffer capacity.

These are CP932 encoded-byte limits, not character limits or rendered-width
limits. Line breaks and runtime-emitted color controls consume bytes;
formatter-only controls do not. Visual fit is measured separately with the
installed-font metrics. The storage checks prove only the known static payload
at the verified consumer. Unbounded runtime substitutions remain a runtime-QA
boundary, and the C20 aggregate bound does not establish the capacity of its
earlier scratch copy.

It is a generated, maintainer-owned snapshot of reverse-engineering results,
not contributor configuration. Git review freezes it for the one supported
game revision; ordinary builds consume it rather than repeating that analysis.

It does not contain translations, line breaks, layout preferences, font data,
or configurable limits. The small documented renderer and buffer limits live
next to their validators in `internal/layout/rules.go`. The reflower derives
breaks for unbroken English from installed-font metrics; explicitly authored
breaks are preserved and receive normal control and storage validation. Runtime
QA remains responsible for visual correctness. Ordinary visual corrections do
not belong in `consumer-map.toml`.

This map applies only to ULJM05410 1.03. Changing it means the game executable's
consumer analysis changed; ordinary translation and layout work never edits it.
