# Message consumers

`consumer-map.toml` answers one question that message text cannot: which game
code path consumes each message? It contains only the runtime-analysis ID sets
and groupings needed to enforce fixed buffers such as C5, C20, C22, bounded
labels, and guild postings.

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
