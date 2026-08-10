# Contributing translations

Contributions are ordinary GitHub pull requests. Install Go 1.26.5 or newer; no
game assets, emulator, Python, or other local service is needed.

Find unfinished messages and inspect the surrounding context:

```sh
./zill search --state todo
./zill show 1136
```

For Japanese text found during runtime QA, search for a distinctive Japanese
phrase and inspect the nearby records shown by `show`.

`show` identifies the section table to edit. Make a focused change to that
message's `english` value in `translations/messages/msgsecNNN.toml`:

```toml
["1730057"]
japanese = "引き受ける<end>"
english = "Accept<end>"
```

Japanese is immutable retail-derived reference text. Do not change it. For
work that is still unfinished, use a blank English value and `todo = true`:

```toml
["1136"]
japanese = "ウィズドロー<end>"
english = ""
todo = true
```

State is inferred, not written: nonblank English is `translated`; blank
English with `todo = true` is `todo`; other blank English is `keep_japanese`.
`todo = true` is allowed only for blank English.

Keep the visible tags and substitutions already present in the English text.
Do not add line breaks to ordinary dialogue; the maintainer build handles its
reflow. An explicit `<line-break>` may be used to preserve a verified fixed
visual layout. The release build preserves authored breaks and applies its
normal control and storage validation. Runtime QA remains responsible for
visual correctness. Never add raw control bytes.

Before opening a pull request, local checks are recommended:

```sh
go test ./...
go vet ./...
./zill check
```

They are not mandatory for contributors: GitHub CI runs them. The maintainer
build later verifies the stored Japanese against retail data and is the
definitive asset-backed validation step. Runtime QA remains the authority for
in-game visual layout.

Keep ordinary pull requests to direct message English changes. Do not include
game assets, extracted retail files, disc images, local binaries, or build
output.
