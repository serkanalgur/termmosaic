# 0001 — Backend strategy

- **Status:** Accepted
- **Date:** 2026-10-03
- **Amended:** 2026-10-04 — recorded the `golang.org/x/term` version pin and the
  verified `CGO_ENABLED=0` cross-compilation status, including the fact that the
  Windows backend is a loud stub.
- **Amended:** 2026-10-05 — bumped the pin to `x/term` v0.29.0 / `x/sys` v0.30.0
  and recorded the measured floor boundary; the pin itself is unchanged.
- **Decides:** [STATUS.md](../STATUS.md) — Core architecture / Backend strategy
- **Supersedes:** the "Option A / B / C" section of the former
  ARCHITECTURE.md "Decision 1".

## Context

TermMosaic needs raw-mode control, alternate screen, resize handling, and input
decoding. Three shapes were on the table: wrap a library, own the terminal layer,
or define narrow interfaces and adapt at the edges.

Two requirements pull in opposite directions and both are non-negotiable
project pillars:

1. **A complete widget catalog at interactive speed**, including virtualized
   List/Table/Tree over 100k+ rows. This forces a *double-buffered cell buffer
   with a two-tier diff* — a renderer design, not a screen-buffer design.
2. **Every widget unit-testable in CI with no terminal.** This forces a
   substitutable output sink.

The pivotal question is therefore not "is tcell good?" but: **does the backend
let us own the buffer and the diff?** If not, pillar 1 is unreachable and pillar
2 is degraded.

### Reference points (verified 2026-10)

- **OpenTUI** (13.4k stars, MIT, v0.5.14) owns its terminal layer outright —
  raw mode, alt screen, mouse, bracketed paste, focus tracking, kitty keyboard,
  OSC 52 with tmux DCS passthrough, Sixel/kitty/iTerm2 graphics — and wraps
  **no** crossterm/termwiz/libvterm. Eight native artifacts; core in Zig with
  TS FFI. Notably it has no CHANGELOG (release bodies are auto-generated PR
  lists) and pre-1.0 patch releases carry behavioural changes.
- **tcell v2** (`github.com/gdamore/tcell/v2`) is the mature Go equivalent:
  cross-platform, owns terminfo, and provides a `SimulationScreen`.
- **Ratatui** (immediate-mode over crossterm/termion/termwiz) and **Bubble Tea**
  (Elm architecture over `tcell`) are the shapes Go users already know.

## Evidence gathered

All measurements were taken in a scratch module outside the repository
(`/tmp/tm-bench`), Go 1.23.0 / go1.26.8 toolchain, darwin/arm64 (Apple M1).
`go test -run '^$' -bench . -benchmem`.

### tcell inspection (source read from the module cache)

`gdamore/tcell/v2@v2.13.10`:

- **Go version conflict.** Its `go.mod` declares `go 1.24.0`. TermMosaic's
  `go.mod` declares `go 1.23`. Adopting tcell forces a floor bump we have not
  agreed to.
- **Release cadence.** v2.12.0 tagged 2025-11-25, v2.13.10 tagged 2026-05-06 —
  fourteen minor releases in ~5.5 months. Actively maintained; also actively
  moved.
- **Dependency weight.** Six direct requirements (`gdamore/encoding`,
  `lucasb-eyer/go-colorful`, `rivo/uniseg`, `x/sys`, `x/term`, `x/text`) plus
  two indirect (`mattn/go-sixel`, `soniakeys/quant`). ~18k LOC.
- **Its buffer model is AoS with heap-allocated strings.** The internal cell is
  `{currStr string, lastStr string, currStyle Style, lastStyle Style, width
  int, lock bool}`, and `Style` itself carries `url string` and `urlId string`.
  That is **four string headers per cell** plus grapheme bytes on the heap
  behind them. Grapheme-correct rendering is bought with an allocation per
  write.
- **It has no row-level skip.** `tScreen.draw()` and `simscreen.draw()` both
  walk *every* cell on *every* `Show()`, calling `drawCell(x, y)` per cell and
  consulting only a per-cell `dirty` bool. Cost is independent of how little
  changed.
- **Its headless surface cannot see the buffer.** `GetCells() *CellBuffer`
  exists on the concrete types but lives in the **unexported** `screenImpl`
  interface, commented "not part of the Screen api". Confirmed by a compile
  failure assigning a `tcell.SimulationScreen` to an interface requiring it.
  `GetContents()` allocates a fresh `[]SimCell` — each `SimCell` holding two
  slices — on every call, so it is an inspection hook, not a per-frame API.

Measured cost of tcell's paths (`BenchmarkTcellPutFullScreen`,
`BenchmarkTcellShow`, 12000-cell screen, one row changed per frame):

| Benchmark | ns/op (median of 3) |
|---|---|
| `TcellPutFullScreen` (draw path, all cells) | 466,481 |
| `TcellShow` (flush, 1 of 60 rows dirty) | 280,814 |
| **Our two-tier diff, same workload** | **7,133** |

Our diff is ~**39×** cheaper than tcell's flush and ~**65×** cheaper than tcell's
draw path. Caveat stated honestly: `SimulationScreen.draw()` performs no
escape-sequence encoding (it writes to a simulated front buffer), so **280,814
ns/op is a lower bound** for a real `tScreen` over a tty; the real figure is
higher. Also note `tcellPutFullScreen` measured **0 allocs/op** because the
benchmark pre-built its strings outside the timed region and the ASCII path
avoids the grapheme-concatenation allocation; a grapheme-heavy repaint through
`tcell.Put` is materially worse than this number suggests. The 466 µs draw path
alone is ~3.4% of a 16 ms (60 fps) frame budget before any diffing, encoding,
or widget work.

### Buffer/diff benchmark (the empirical core of [ADR 0002](0002-buffer-representation.md))

Synthetic 200×60 = 12,000-cell scene, 99% static chrome (panel borders and
static filler) and a progress-bar row plus three numeric readouts changing per
frame. Measured changed cells: **42 / 12,000 (0.35%)**, **4 / 60 rows dirty
(6.7%)**.

| Pass | Bytes written |
|---|---|
| Full repaint (AoS) | 23,240 |
| Full repaint (SoA) | 23,240 |
| Two-tier diff (SoA) | **107** |
| Two-tier diff (AoS) | **107** |
| Two-tier diff (AoS, padded) | **107** |
| Two-tier diff (SoA, rune planes) | **107** |

**217× fewer bytes**, and the byte counts are byte-for-byte identical across all
four representations — bytes are representation-independent. Only ns/op and
allocations differ.

> **Amended 2026-10-04.** Re-measured in the committed implementation on the
> same scene shape, the diff writes **141 bytes** against **19,979** for a full
> repaint — **~141×**, not 217×. The absolute byte count is dominated by the
> scene's glyph widths rather than by the algorithm (the same scene costs 206
> bytes with realistic 3-byte block glyphs), so the durable claim is the ratio.
> See [ADR 0002](0002-buffer-representation.md) for the correction in full.

### What could not be measured

- **We did not benchmark tcell's diff quality**, only its draw and flush cost.
  There is no way to ask tcell how many bytes it *would* have written without a
  real tty and a wire-level tap.
- **We did not measure Windows console behaviour, tmux passthrough, SSH, or
  IME.** Those remain unmeasured risks and are re-filed as open questions below.
- **Cross-platform claim untested.** All numbers are darwin/arm64.

## Options considered

### Option A — Wrap `tcell/v2`

- **Pros:** Windows console abstraction, terminfo database, decades of
  terminal-quirk handling, `SimulationScreen` for headless tests. Fastest route
  to a first frame.
- **Cons:** its model is a mutable screen buffer that the renderer draws into,
  with a per-cell dirty flag and a full-cell walk per flush. Adopting it means
  either (a) giving up the two-tier diff and pillar 1, or (b) keeping a *second*
  buffer of our own and pushing diffs into tcell — double work per frame plus a
  second copy of the screen. Neither is acceptable. It also forces a Go 1.24
  floor, drags in eight modules, and its headless backend cannot expose the
  buffer that our widget tests need to assert against.
- **Verdict:** rejected. This is the decisive evidence for the ADR.

### Option B — Own the terminal layer outright, no interfaces

- **Pros:** total control, no ceiling.
- **Cons:** the same control without an interface means the headless test
  backend can never exist, which breaks pillar 5 outright. Six to twelve months
  of terminal-quirk work before a first widget.

### Option C — Pluggable: own the core, adapt at the edges ✅

Narrow interfaces (`Terminal`: raw mode, alt screen, size, capabilities,
resize events; `Sink`: bytes out), a direct `golang.org/x/sys` implementation,
and a headless memory sink for tests.

## Decision

**Option C. TermMosaic owns the terminal layer outright and exposes it through
two narrow interfaces.**

```go
// Terminal controls the physical terminal. Implementations: x/sys terminal
// (build-tag selected), and a headless fake for CI.
type Terminal interface {
    Size() (w, h int)
    EnterRawMode() error
    LeaveRawMode() error
    EnterAltScreen() error
    LeaveAltScreen() error
    Capabilities() Caps   // truecolor / 256 / 16, unicode borders, mouse, kitty kbd
    Read(p []byte) (int, error)
    ResizeEvents() <-chan Size
    Close() error
}

// Sink consumes the bytes the diff produces. Implementations: os.Stdout / the
// tty fd, and a MemorySink that records frames for tests.
type Sink interface {
    Write(p []byte) (int, error)
    Flush() error
}
```

The renderer depends only on `Sink`. The widget/test harness depends only on
`Terminal` + `Sink`. Consequences:

- `MemorySink` + headless `Terminal` is a **first-class, v1 deliverable**, not a
  stretch goal — every widget is CI-testable with zero terminal and zero
  platform-specific code.
- A second real backend (SSH / piped output) is additive later, mirroring
  OpenTUI's `BufferedBackend` vs `FeedBackend` split. Not built in v1.
- Dependency footprint: `golang.org/x/sys` and `golang.org/x/term` (for raw mode
  only). Nothing else. Go 1.23 floor preserved.

### Dependency versions, and one deliberate pin

| Module | Version | Why |
|---|---|---|
| `golang.org/x/sys` | v0.30.0 | Current release compatible with the Go 1.23 floor. |
| `golang.org/x/term` | **v0.29.0 (pinned)** | The newest release that keeps the Go 1.23 floor. v0.30.0 and later raise it — see the boundary below. |

The boundary is where a release's **own** `go` directive stops being compatible with
this module's floor. `go mod` raises this module's floor to the highest directive in
the build, so a dependency declaring a higher one moves us off `go 1.23`:

| `x/term` | Its own `go` directive | Effect on this module |
|---|---|---|
| v0.28.0 | `go 1.18` | floor stays `go 1.23` |
| **v0.29.0** | **`go 1.18`** | **floor stays `go 1.23` — newest compatible** |
| v0.30.0 | `go 1.23.0` | floor becomes `go 1.23.0` |
| v0.35.0 | `go 1.24.0` | floor becomes `go 1.24.0` |
| v0.46.0 (`@latest`) | `go 1.26.0` | floor becomes `go 1.26.0` |

Note the subtlety at v0.30.0: it declares only `go 1.23.0`, which looks compatible
with a `go 1.23` floor and is not. The trailing `.0` makes it a toolchain directive
rather than a language-version one, and `go mod` rewrites our own `go` line to
`go 1.23.0`. That single character is the whole boundary — which is why the check
below is a `go get` in a scratch module rather than a reading of the version number.

So the pin is still required and still correct, and `@latest` remains unusable —
but "unusable" is a statement about v0.30.0 and above, not a permanent property of
the module. The next person to check this should compare against **v0.30.0**, not
against `@latest`.

`x/term` is a direct dependency only because `term/terminal_unix.go` needs its
raw-mode helper; it is not load-bearing for anything else in the design.

The irony is worth recording rather than hiding: this is the **same
"pin to an old version" shape we rejected tcell for.** tcell was rejected in
part because `gdamore/tcell/v2@v2.13.10` declares `go 1.24.0` against our
1.23 floor — a version floor we did not agree to. Here we meet the identical
problem from the other side and answer it by pinning rather than by refusing,
because `x/term` is a two-function dependency we can trivially vendor or
reimplement and tcell is 18k lines we cannot. **The difference in our
reasoning is cost, not principle**, and we should say so if this pin is ever
cited as evidence that version floors are fine.

**This is a real, ongoing maintenance cost, not a one-time note.** The pin has
to be re-checked every time `x/term` publishes: someone must notice the new
release, confirm whether it still declares `go 1.23`, and decide whether to bump.
Nothing enforces it. The trigger for unpinning is a Go version-floor increase
that we have separately agreed to — at which point this becomes a routine
`go get -u`. Until then, expect this line to need revisiting each quarter.

### Cross-compilation: verified status

`CGO_ENABLED=0` builds are **verified** for `windows/amd64` and `linux/arm64`
in addition to the native darwin/arm64 target. The no-cgo claim is therefore
tested rather than asserted, which matters because it was a stated project goal
and the main reason Yoga was rejected
([ADR 0004](0004-layout-engine.md)).

**Platform support, qualified.** "It builds for Windows" is *not* "it works on
Windows." The Windows backend in `term/terminal_windows.go` is deliberately a
**stub that returns a loud, exported error** (`term.ErrWindowsStub`) from every
operation that touches the console — raw mode, alt screen, read. `Open` succeeds
so that a program can start and report a readable failure rather than dying on a
message the user cannot act on. This is exactly the mitigation ADR 0001's own
guidance calls for: do not ship a half-working Windows console that passes its own
tests on a developer's machine. The escape hatch, if Windows is required before
v1.0, remains a build-tagged tcell backend behind the unchanged
`Terminal`/`Sink` interfaces.

So the honest platform matrix today is: **Linux and macOS functional; Windows
compiles and fails loudly at runtime.**

## Consequences

**Good**

- The two-tier diff and the virtualization targets are ours to design; the
  backend imposes no ceiling on either.
- Every widget is testable in CI without a terminal — a differentiator we can
  actually ship, and the direct justification for this decision.
- Minimal dependencies, static-binary cross-compilation preserved, no Go
  version floor bump.
- Capability detection lives in one place, so the truecolor→256→16 and
  Unicode→ASCII degradation ladders ([ADR pending](README.md)) have a natural
  home.

**Bad — stated plainly**

- **We now own the terminal-quirk surface.** Paste coalescing, mouse encoding
  variants (SGR 1006, urxvt 1015, legacy X10), resize races against SIGWINCH,
  tmux DCS passthrough, and Windows console mode flags are our bug list, not
  tcell's. This is the real cost and it is the main reason this project is
  pre-alpha for a long time.
- **Windows is late.** `x/sys` raw-mode handling on the Windows console is a
  distinct and fiddly problem. We should plan on Linux and macOS first and treat
  Windows as a v1.0 stretch, even though CI-green-on-Windows is in our
  definition of done.
- **We inherit a maintenance obligation** that tcell's maintainers have been
  carrying. We should reuse their *knowledge* (and, where licence permits,
  ideas) even while owning the code.
- **No terminfo.** We emit a fixed, well-chosen subset of SGR/CUP/ED/EL plus
  capability sniffing. That is fine for the overwhelming majority of terminals
  and wrong for a few exotic ones. Documented limitation.

## Rejected alternatives, specifically

- **Wrap `tcell/v2`** — its `draw()` walks every cell per `Show()` with only a
  per-cell dirty flag; measured flush 280,814 ns/op against our 7,133 ns/op on
  the same one-row-dirty workload (~39×), and its draw path alone is 466,481
  ns/op. Its internal cell also carries four string headers, so grapheme
  rendering allocates. Its `go.mod` requires Go 1.24 against our 1.23 floor.
  Rejected on measurement.
- **Wrap `golang.org/x/term`** — raw mode only. It does not solve input
  decoding, escape emission, or capabilities, so it is a dependency we adopt
  *inside* Option C rather than an alternative to it.
- **Adapt Bubble Tea instead of building a renderer** — Bubble Tea owns no
  buffer; it delegates to tcell and therefore inherits every limitation above.
  Rejected for the same reason, one layer up.
- **Adopt OpenTUI's approach wholesale** — not viable in Go: its core is Zig
  with 8 native FFI artifacts. Its *architecture* is the right lesson (own the
  terminal, SoA-ish buffers, two-tier diff, retained tree) but its numbers do
  not transfer to Go. See [ADR 0002](0002-buffer-representation.md), where we
  measured the SoA claim directly and it did not survive.

## Risks to revisit at v1.0

1. **Windows console support.** Highest-probability source of v1 slippage.
   Trigger for revisiting: if Linux/macOS land and Windows raw mode is not
   working by v0.9, consider a build-tagged `tcell` backend as an escape hatch
   for Windows only, keeping the interfaces intact.
2. **Terminal-quirk bug volume.** If the quirk backlog exceeds a few weeks of
   sustained effort, revisit whether to adapt tcell's *quirk layer* while
   keeping our own buffer and diff — decoupling quirk handling from rendering
   was always the interface's purpose.
3. **IME.** We have no IME/preedit story and no measurement of what one costs.
   This is still open; see [README.md](README.md).

## Open questions carried forward

- Headless backend is **v1** under this decision (not v0.5). The one remaining
  sub-question is whether `MemorySink` should expose the *frame bytes* or the
  *cell buffer* to assertions. We need the cell buffer — see the tcell finding
  above — so the headless backend must own a real `Buffer`, not just a byte log.
- Kitty graphics in v1 — still open, still leaning no.
- IME scope — still open, unmeasured.