# Zill

Zill is an English fan translation of *Zill O'll Infinite Plus*.

## Using the translation

The release patch is provided in xdelta3 format and must be applied with a
tool that supports xdelta3. One third-party, client-side browser patcher is
[xdelta-wasm](https://kotcrab.github.io/xdelta-wasm/).

Use a clean ISO dump of the Japanese release, serial `ULJM-05410`, version
`1.03`, as the source file and the released `.xdelta` file as the patch.

## Testing status and known issues

A full playthrough of the `Origin` starting scenario has been completed.

- Text on the name-entry screen uses the wrong character width; this is only a
  visual issue.
- The character profile list still uses Japanese kana order rather than
  alphabetical order.
- Additional buffer overflows may remain undiscovered.
- Some messages still overflow their text boxes.
- Some translation inconsistencies remain.
- Some character-creation choices are unclear due to buffer limitations; a
  workaround is planned.

## Contributing

Install Go 1.26.5 or newer, then use the contributor commands:

```sh
./zill search --state todo
./zill show 1136
./zill context --game-dir /path/to/PSP_GAME --record 1350035
./zill check
```

Text searches label Japanese and English separately and also return matching
terminology; `--state` filters message records only.
`show` prints the target, nearby Japanese/English context, and the editable
section path. `context` reads both retail PAA archives without modifying them and
provides a scene-first translation workflow. A bank query lists every recovered
scene containing records from that bank, while a record query maps one message to
its recovered scenes. Both print copyable commands for opening one complete scene:

```sh
./zill context --game-dir /path/to/PSP_GAME --bank 135
./zill context --game-dir /path/to/PSP_GAME --record 1350035
./zill context --game-dir /path/to/PSP_GAME --scene pa:cdc/01/ancsri01.cdc
./zill context --game-dir /path/to/PSP_GAME --list-scenes
```

The default scene view is a translation-oriented transcript containing record
IDs, Japanese and English, translation states, qualified speaker labels, branch
and choice context, terminology, and explicit limits on what static analysis can
establish. `--format json` emits the equivalent compact, versioned machine input.
Add `--verbose` to either format for the complete diagnostic projection: raw
offsets and paths, control predicates, actor lifecycle, cross-program references,
RBB provenance, room tables, and executable source locators.

`--list-scenes` prints the complete recovered CDC and ambient-scene catalogue in
a compact one-line-per-scene format. Its first column is a stable ID accepted by
`--scene`; storage-only message banks are intentionally excluded because they are
containers rather than recovered dialogue scenes.

For records with inline condition or selection variants, `edit-record` provides
a JSON-only patch protocol intended for automated translation agents:

```sh
./zill edit-record inspect --record 30028
./zill edit-record apply --patch variant-patch.json --dry-run
./zill edit-record apply --patch variant-patch.json
```

`inspect` returns deterministic targets such as `controls/1/blocks/0`, the
Japanese and English payload for each target, and SHA-256 hashes of the immutable
source and complete current English. A patch supplies one target, replacement
English, and both expected hashes. `apply` owns reconstruction of `<if>`,
`<select>`, expressions, and `<end>` delimiters; it rejects stale hashes,
structural changes, altered runtime substitutions, reserved control injection,
or contributor validation failures. `--dry-run` performs the same checks without
writing. Successful writes replace the canonical section file atomically.

A complete stdin patch can be generated from inspection output and then edited:

```sh
./zill edit-record inspect --record 30028 |
  jq '{schema_version, record_id,
       target: .targets[1].path,
       expected_source_hash: .source_hash,
       expected_english_hash: .english_hash,
       english: .targets[1].english}' > variant-patch.json
./zill edit-record apply --patch variant-patch.json --dry-run
```

The patch decoder rejects unknown, duplicated, or incorrectly cased fields and
trailing JSON values. Success is JSON on stdout with exit 0; invocation/schema
errors are JSON on stderr with exit 2; stale, validation, lock, and I/O errors
use exit 1. Real writes are serialized per message section. Do not modify that
section concurrently with a noncooperating editor: portable filesystems provide
atomic replacement but not compare-and-swap rename. Whole-record hashes and a
final section comparison detect stale input outside that unavoidable final
publication window.

Single-leaf patches deliberately require an already translated controlled
record. Blank controlled records need a future atomic multi-target operation so
the tool cannot create a partially translated control structure.

A message bank is treated as a storage container rather than a recovered scene.
Bank listings report records which have only storage context, and record queries
with no recovered occurrence show a bounded storage-order neighborhood explicitly
marked as not verified chronology. The storage container itself can be inspected
with a scene ID such as `bank/034`.

The first query builds a versioned immutable-retail index under the operating
system's user cache directory; later queries reuse its CDC flow analysis, RBB
catalog, and extracted room metadata while always joining current translations
and terminology. Missing, stale, corrupt, or unwritable cache data is rebuilt or
ignored without modifying `PSP_GAME`.
It preserves static branches, annotates C5
entity/name-label associations with cautious inferred speaker labels, and gives
path-sensitive actor lifecycle context by following verified CDC jumps,
single-slot calls/returns, and choice arms. Verified enclosing predicates are
decoded into structured raw selectors, comparison operators, and polarity.
Coordinates, actions, opaque relation state, portrait/name requests, and possible
addressees remain explicitly qualified static evidence. Authored but unreachable
messages remain visible, while unsupported control flow and genuine state
disagreements are labeled explicitly. Direct scenario-slot references resolve to
one retail-catalogued logical family. Exact-byte-identical physical variants are
collapsed into one content variant while every group, logical key, authored
resource name, and PAA member remains available in JSON; genuinely different
variants remain separate without choosing a runtime group. The same retail
catalog reports room-local message-bank registrations as availability evidence,
not dialogue occurrences. Current-room C14 table references report their
bounded possible slot set and counts while retaining the exact room/resource
triples on each family. Ordinary town-NPC dialogue is recovered
separately from room-authored entity records and the executable's bounded
entity-handle-to-message dispatcher. These ambient occurrences include room
provenance and an interaction-target label while remaining explicit that authored
placement is not runtime presence or global dialogue chronology. The verbose
diagnostic model retains a complete message-bank storage unit when no resolved
consumer references a record; it reports source offsets while leaving chronology,
speakers, actor presence, and reachability unresolved. For all banks, record-local
retail controls such as conditionals and selections are
decoded into their source blocks without evaluating game state. Explicit reserve
markers also select candidate rows in the statically identified event-title
authoring table; exact label matches are reported separately from executable consumer
evidence. Verified executable companion formulas and record-local authoring-table
roles are kept separate from chronology and reachability. Stable scene IDs and
physical aliases expose these distinctions without choosing a runtime variant.

`check` is the asset-free contributor gate; it does not run reflow
or the retail-consumer fixed-buffer checks performed by the maintainer build.
Local checks are recommended before a pull request, but are not required:
GitHub CI runs them.

## PPSSPP remote debugger

Use the project-local JSONL bridge to inspect or control a running PPSSPP game:

```sh
./zill ppsspp-debugger --port PORT
```

PPSSPP must have a game loaded with **Allow remote debugger** enabled. The
debugger has no authentication or TLS, so the bridge defaults to loopback and
requires an explicit opt-in for remote hosts. See
[docs/ppsspp-debugger.md](docs/ppsspp-debugger.md) for setup, the JSONL command
contract, screenshot behavior, and mutation safeguards.

## Translation data

Message data is stored in paired section tables under `translations/messages/`.
Each message has immutable `japanese` and contributor-editable `english` text:

```toml
["1730057"]
japanese = "引き受ける<end>"
english = "Accept<end>"
```

For an unfinished message, leave `english` blank and add `todo = true`:

```toml
["1136"]
japanese = "ウィズドロー<end>"
english = ""
todo = true
```

Do not add `todo` to a nonblank English translation. State is inferred: a
nonblank English value is `translated`, a blank value with `todo = true` is
`todo`, and another blank English value is `keep_japanese`. Japanese is stored
for readable contributor context, is not editable, and is verified against the
retail source by the maintainer build.

Terminology remains reviewed data under `translations/terminology/`. Ordinary
contributors should keep a pull request focused on direct English message
edits.

## Maintainer build

`./zill build --game-dir /path/to/PSP_GAME --iso /path/to/retail.iso` is
maintainer-only and is the definitive asset-backed build and validation step.
It requires matching legally obtained Japanese `ULJM05410` version `1.03`
retail sources plus xdelta3 3.2.0. It publishes `build/PSP_GAME/`,
`build/zill-english.iso`, and `build/zill-english.xdelta`; contributors should
use `zill check` instead. Runtime QA remains required before publication.

Maintainer-owned release data includes fixed strings in
`release/strings/{eboot,equipment}.toml`, title attribution in
`release/title/attribution.toml`, and layout configuration in
`release/layout/{categories,consumer-map}.toml`.

## Licensing and game assets

Code is GPL-3.0-or-later. Original English translation and editorial content
is CC BY-SA 4.0. See [NOTICE.md](NOTICE.md) and `LICENSES/`.

The repository contains no native message-bank bytes or complete retail archive
members. Editable localized images live under
[`assets/texture_overrides/`](assets/texture_overrides/). See
[CONTRIBUTING.md](CONTRIBUTING.md) for the ordinary pull-request workflow and
[RELEASING.md](RELEASING.md) for maintainer release work.
