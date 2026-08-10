# Zill

Zill is a community translation project for *Zill O'll Infinite Plus*.

Install Go 1.26.5 or newer, then use the contributor commands:

```sh
./zill search --state todo
./zill show 1136
./zill check
```

Text searches label Japanese and English separately and also return matching
terminology; `--state` filters message records only.
`show` prints the target, nearby Japanese/English context, and the editable
section path. `check` is the asset-free contributor gate; it does not run reflow
or the retail-consumer fixed-buffer checks performed by the maintainer build.
Local checks are recommended before a pull request, but are not required:
GitHub CI runs them.

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

`./zill build --game-dir /path/to/PSP_GAME` is maintainer-only and is the
definitive asset-backed build and validation step. It requires a legally
obtained Japanese `ULJM05410` version `1.03` retail `PSP_GAME` tree and
publishes `build/PSP_GAME/`; contributors should use `zill check` instead.
Runtime QA remains required before publication.

Maintainer-owned release data includes fixed strings in
`release/strings/{eboot,equipment}.toml` and layout configuration in
`release/layout/{categories,consumer-map}.toml`.

## Licensing and game assets

Code is GPL-3.0-or-later. Original English translation and editorial content
is CC BY-SA 4.0. See [NOTICE.md](NOTICE.md) and `LICENSES/`.

The repository contains no native message-bank bytes or bundled retail game
assets. See [CONTRIBUTING.md](CONTRIBUTING.md) for the ordinary pull-request
workflow and [RELEASING.md](RELEASING.md) for maintainer release work.
