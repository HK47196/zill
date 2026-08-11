# PPSSPP remote debugger

The project-local bridge exposes PPSSPP's remote debugger as a persistent JSON
Lines process for runtime observation and control.

## Setup

Start PPSSPP with a game loaded. In **Developer Tools**, set a nonzero **Local
Server Port** and enable **Allow remote debugger**, then run the bridge from the
repository root:

```sh
./zill ppsspp-debugger --port PORT
```

Use the port supplied for the task. If none is supplied, read `RemoteISOPort`
from PPSSPP's `ppsspp.ini` when available. On native Linux it is usually at
`${XDG_CONFIG_HOME:-$HOME/.config}/ppsspp/PSP/SYSTEM/ppsspp.ini`; otherwise ask
for it.

The debugger has no TLS or authentication. Keep it bound to the default
loopback host. Do not enable `--allow-remote` unless the user expressly accepts
that exposure.

## JSONL lifecycle

Wait for `{"event":"ready"}` after starting the process; report
`{"event":"fatal",...}`. Send one JSON object per line, with a unique string or
numeric `id`, and match responses by that ID. The caller is responsible for
choosing unique IDs.

Successful and expected failed results respectively contain
`{"ok":true,"result":...}` and `{"ok":false,"error":...}`. An unticketed
`reconnected` event can precede a result after a restart or disconnect. Do not
retry the interrupted command because it may have had side effects.

Finish with:

```json
{"id":"quit-1","command":"quit"}
```

## Commands

| Command | Required and optional fields | Behavior |
| --- | --- | --- |
| `status` | `timeout?` | Return `game.status` and `cpu.status`. |
| `pause`, `resume` | `timeout?` | Idempotently change debugger stepping state. |
| `press` | `button`, `duration?`, `timeout?` | Press and release one PSP button while gameplay advances. |
| `buttons` | `buttons`, `timeout?` | Set named button states; send `false` to release them. |
| `analog` | `x`, `y`, `stick?`, `timeout?` | Set the left or right stick; send zeroes to center it. |
| `observe` | `path`, `restore?`, `timeout?` | Capture a verified PNG and restore prior stepping state by default. |
| `wait` | `event`, `buffered?`, `timeout?` | Consume one matching debugger broadcast. |
| `drain` | `limit?` | Consume queued broadcasts and report dropped events. |
| `read_memory` | `address`, `size`, `replacements?`, `path?`, `timeout?` | Read up to 65,536 bytes; reads over 4,096 bytes require a path by default. |
| `write_memory` | `address`, one of `base64` or `path`, `timeout?` | Write up to 65,536 bytes when mutation is enabled. |
| `raw` | `event`, `params?`, `expect?`, `wait_event?`, `timeout?` | Send any PPSSPP debugger event. |
| `quit` | none | Close the bridge cleanly. |

For `raw`, use `expect:"response"` for ticketed requests, `expect:"event"` for
asynchronous operations, and `expect:"none"` only when no acknowledgement is
needed. Memory addresses may be JSON integers or numeric strings such as
`"0x08800000"`; high-level transfers that wrap past `0xFFFFFFFF` are rejected.

## Control and capture behavior

Prefer `status`, `press`, `buttons`, `analog`, `pause`, `resume`, `read_memory`,
and `observe` over raw protocol calls. `pause` and `resume` control debugger
stepping only. A PPSSPP menu or UI pause is separate and must be cleared in
PPSSPP before gameplay can advance. Always release held buttons and center an
analog stick after use, including after errors.

`observe` captures the displayed PPSSPP X11 window with `xdotool` and
ImageMagick `import`, not a PSP framebuffer. It requires an X11 session and
exactly one visible, non-minimized PPSSPP window. Use a unique absolute PNG
path. The bridge pauses only when necessary, validates the PNG, and restores
the prior running state by default. Leave `restore` enabled unless the user
asks to leave the game stopped.

Do not pause solely for an ordinary capture, and do not use `gpu.buffer.*` as a
screenshot substitute. PPSSPP 1.20.4 performs `gpu.buffer.*` readback only
during GE stepping, which the remote protocol cannot enter; the X11 capture is
the deliberate workaround.

## Mutation safeguards

Memory and register writes, along with mutating `raw` calls, require explicit
user approval and a known exact target and value. Memory writes additionally
require starting the bridge with `--allow-memory-write`.
