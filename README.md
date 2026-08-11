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
section path. `context` reads the retail CDC scripts without modifying them and
prints every complete, cross-bank scene that references one requested record or
any record in a requested bank. It preserves static branches, annotates C5
entity/name-label associations with cautious inferred speaker labels, and gives
path-sensitive actor lifecycle context by following verified CDC jumps,
single-slot calls/returns, and choice arms. Authored but unreachable messages
remain visible, while unsupported control flow and genuine state disagreements
are labeled explicitly. If no CDC scene references the query, `context` falls
back to the complete message bank in storage order and explicitly leaves
chronology, speakers, branches, and reachability unresolved. It also supports
machine-readable JSON:

```sh
./zill context --game-dir /path/to/PSP_GAME --bank 135
./zill context --game-dir /path/to/PSP_GAME --record 1350035 --format json
```

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
