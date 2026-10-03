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
design only; the renderer-mode and layout decisions were made on API-surface
and dependency grounds and are labelled as such.

**Caveat worth repeating:** OpenTUI is a Zig core with TypeScript FFI bindings.
Its numbers do not transfer to Go, and ADR 0002 exists precisely because we
checked that assumption instead of inheriting it.

## Still open

These are tracked in [STATUS.md](../STATUS.md) and are **not** decided:

- Colour model and degradation ladder
- Theme and styling system
- Headless backend as v1 vs v0.5 — largely settled by ADR 0001 in favour of
  v1, but the assertion surface is still open
- Kitty graphics in v1
- IME scope and cost