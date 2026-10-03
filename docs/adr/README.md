# Architecture Decision Records

TermMosaic's architecture decisions are recorded here as ADRs. Each is a
short, dated document that states what was decided, what was rejected, and why.

An ADR is not deleted when it stops being true. If a decision is reversed, the
original stays and a new one supersedes it, so the reasoning history survives.

## Index

| # | Title | Status | Date |
|---|---|---|---|
| [0001](0001-backend-strategy.md) | Backend strategy | Accepted | 2026-10-03 |
| [0002](0002-buffer-representation.md) | Cell buffer representation | Accepted | 2026-10-03 |
| [0003](0003-renderer-mode.md) | Renderer mode | Accepted | 2026-10-03 |
| [0004](0004-layout-engine.md) | Layout engine | Accepted | 2026-10-03 |
| [0005](0005-input-decoding.md) | Input decoding | Accepted | 2026-10-04 |

## Decisions at a glance

- **0001 — Backend: pluggable, own the core.** Two narrow interfaces
  (`Terminal`, `Sink`) with a direct `golang.org/x/sys` implementation and a
  headless memory sink. We wrap **no** terminal library. The evidence that
  settled it: `tcell`'s flush costs 280,814 ns/op on a one-row-dirty workload
  where our two-tier diff costs 7,133 ns/op, and `tcell`'s headless backend
  cannot expose the cell buffer that widget tests need.

- **0002 — Buffer: AoS with a padding-free 16-byte `Cell`.** This **overturned
  the previous leaning toward struct-of-arrays.** OpenTUI's SoA row-skip
  advantage is a Zig `mem.eql` advantage and does not transfer to Go: the
  packed AoS row skip *ties* with SoA (5,373 vs 5,128 ns/op) and is 4.4×
  *faster* when every row is dirty (147.2 vs 636.7 ns/op), because it is one
  wide memcmp instead of four. A two-tier diff on a 200×60 scene writes ~141×
  fewer bytes than a full repaint. Byte-wise row comparison additionally
  requires the compared range to be **contiguous**, not just `Cell` to be
  padding-free — see the 2026-10-04 amendment.

- **0003 — Renderer: hybrid.** A retained widget tree invalidated by rectangle,
  with widgets describing themselves on demand. No reconciler, no Elm loop.
  Static chrome is cheap because of the diff, not because of the renderer mode.

- **0004 — Layout: constraint-based, own solver.** `Length`/`Min`/`Max`/
  `Percentage`/`Ratio`/`Fill`, matching what Bubble Tea users already know.
  `Fill` is **order-insensitive** — a deliberate, tested divergence from tmux's
  priority-ordered rule. Flexbox via Yoga was rejected because **cgo breaks
  `CGO_ENABLED=0` cross-compilation**, contradicting the single-static-binary
  goal.

- **0005 — Input: a pure decoder under a resumable driver, in a new `input`
  package.** `Decode(seq []byte, cfg Config) (Event, int, Status)` is pure, so
  the worst input bug — a sequence split across two `read(2)` calls — is a
  one-line table-driven test rather than a flaky timing test. A `Parser` holds
  only the unavoidable bytes, and a `Source` merges input and resize into one
  ordered stream. Scope verdicts: **kitty keyboard IN** (progressive
  enhancement, request only `disambiguate`, 100 ms bounded probe); **paste IN**
  and always **one `EventPaste` carrying the whole payload**, never a stream;
  **mouse decoding IN** (SGR 1006, urxvt 1015, X10) but **capture OFF by
  default** because it steals selection and scrollback from the user's shell;
  **focus decoding IN**, reporting OFF by default; **IME DEFERRED and scoped
  out**, with `EventCompose` and a `Compose` payload field reserved so it is a
  later feature rather than a rewrite.

## How these were decided

Decisions 1 and 2 were made **empirically**. A scratch Go module was built
outside the repository and real numbers were measured on darwin/arm64 (Apple
M1), Go 1.23.0:

```
cd /tmp/tm-bench
go test -run '^$' -bench . -benchmem -benchtime=20000x -count=5
```

Raw output is quoted inline in ADRs 0001 and 0002, including the workloads that
did *not* produce a clean result. Benchmarks were run for the buffer and diff
design only; the renderer-mode, layout and input-decoding decisions were made on
API-surface, testability and dependency grounds and are labelled as such.
**Decision 5 explicitly records that no benchmark informed it**: input decoding
is I/O-bound, and the one number that matters — 0 allocations on the key path —
is a property the ADR specifies and a test must pin rather than a measurement
made today.

**Caveat worth repeating:** OpenTUI is a Zig core with TypeScript FFI bindings.
Its numbers do not transfer to Go, and ADR 0002 exists precisely because we
checked that assumption instead of inheriting it.

## Still open

These are tracked in [STATUS.md](../STATUS.md) and are **not** decided:

- Colour model and degradation ladder
- Theme and styling system
- Headless backend as v1 vs v0.5 — largely settled by ADR 0001 in favour of
  v1, but the assertion surface is still open
- Kitty **graphics** in v1 (the kitty *keyboard* protocol is decided by ADR 0005)

### Scoped out by ADR 0005, with triggers recorded

These are no longer open questions; they are deferrals with stated triggers,
listed in [ADR 0005 §10](0005-input-decoding.md#10-deferred-items-with-triggers):

- **IME / composition** — scoped out and documented as unsupported, with
  `EventCompose` and a `Compose` payload field reserved in the event model so
  adding it later is a feature rather than a rewrite of thirty widgets.
- **Kitty `F13`–`F35`** — when added, appended at the end of the `Key` iota
  block so existing constants do not renumber.
- **tmux / screen DCS passthrough** — a real gap, not an oversight: without it
  a program under tmux on a modern terminal can lose key and mouse reporting.
- **X11 UTF-8 extended mouse and 1016 pixel coordinates.**