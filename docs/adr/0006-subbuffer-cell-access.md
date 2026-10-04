# 0006 — Sub-buffer cell access

- **Status:** Accepted
- **Date:** 2026-10-04
- **Decides:** [STATUS.md](../STATUS.md) — Core architecture / cell access on
  sub-buffers
- **Depends on:** [ADR 0002](0002-buffer-representation.md)
- **Answers:** the "Risks to revisit at v1.0" item 1b of
  [ADR 0002](0002-buffer-representation.md) — whether to make `RowBytes`
  structurally refuse non-top-level buffers — and closes it **now** rather than
  at v1.0.

## Context

[ADR 0002](0002-buffer-representation.md) established two preconditions for
byte-wise row comparison: (a) `Cell` is padding-free, and (b) the byte range
compared is contiguous. It fixed (b) by giving `Buffer` an explicit `stride`
field — equal to `Width` for a top-level buffer, inherited from the parent for a
sub-buffer — so every row is addressed `cx[y*stride : y*stride+w]`.

That fix made the *data structure* honest. It did not make the *API* honest.

`Buffer.Cells()` returns the raw backing slice:

```go
func (b *Buffer) Cells() []Cell { return b.cx }
```

On a top-level buffer, `stride == Width`, so the flat index `y*Width+x` is
correct and `Cells()` is a genuinely useful zero-copy fast path. On a
sub-buffer, the flat index is `y*stride+x`, and a caller who writes
`sub.Cells()[y*sub.Width()+x]` **reads the wrong cell** — no panic, no error, no
failing test, just wrong pixels. `CellAt` and `SetCell` both go through
`b.at(x, y)` and are stride-correct, so the trap is confined to `Cells()`.

**This is the same bug twice.** `SubBuffer` originally returned the naively
contiguous slice `b.cx[y*stride+x : y*stride+x+w*h]`, which violated
precondition (b) while every documented type-level invariant held. ADR 0002's
amendment calls that out explicitly. `Cells()` is that trap left open at a
different door: even with `stride` fixed, the door still hands out a flat slice
whose correctness depends on a field the caller cannot see.

Nothing in the repository indexes `Cells()` on a sub-buffer today, so there is
no live bug. But `Cells()` has two real callers that will multiply:

- `render/render.go:386-387` passes `r.back.Cells()` and `r.front.Cells()`
  into `diff.Frame`.
- `headless/memorysink.go:149-150` copies `s.screen.Cells()`.

And the widget catalog is next: List, Table, Tree, Pager, TextInput,
ProgressBar, Sparkline and the rest. Several of them will want fast bulk access
to a cell range — filling a row, painting a bar, scanning for a hit — and
`Cells()` is the obvious thing to reach for. It is the wrong thing, silently.

**What widget authors actually need**, which is what the decision should be
shaped around:

| Need | Frequency | Correct API today |
|---|---|---|
| Bulk write/read within one row | **the common case** — every row-oriented widget | none public; `row(y)` is unexported |
| Single cell | common | `CellAt` / `SetCell` ✅ |
| Whole-grid scan | rare | none needed; `Clip` gives a dense copy |
| Byte-wise row compare | the diff only, top-level only | `RowBytes`, top-level only |

The common case has **no public API at all** and the dangerous one does. That
inversion is the whole problem.

## Options considered

### 1. Delete `Cells()`, method access only

`CellAt`/`SetCell` become the only cell accessor. **Rejected:** it deletes the
one bulk primitive widget authors need, and pushes them toward `for y { for x {
buf.SetCell(x, y, c) } }` — a bounds check and a call per cell on the hot draw
path where ADR 0002 measured ~6.8 ns/cell. Correct, and slower than it needs to
be.

### 2. Make `Cells()` unusable rather than merely documented

Keep the method but panic on a sub-buffer, or restrict it to top-level buffers.
**Partly adopted** — the panic is real and is adopted for `RowBytes`, which has
a well-defined top level. For `Cells()` it is not enough: the failure it prevents
is the caller using a flat index on a strided grid, and a sub-buffer *cannot*
have a correct flat index at all. There is nothing to hand back. Making the only
possible answer a panic is making the method useless rather than safe.

### 3. Make `Cells()` stride-aware

Keep the flat slice, document that callers must use `y*stride+x`, and export
`Stride()`. **Rejected, and this is the worst of the four.** It keeps a
silent-wrong-answer API and pushes the invariant onto every one of thirty
widget authors, each of whom will get it right until one of them does not. ADR
0002's own lesson is that an invariant with no type-level canary survives until
it does not.

### 4. Deprecate `Cells()` for v0.x, remove at v1.0

Buys a migration window. **Rejected:** there is nothing to migrate *to* that has
the same shape, so a deprecated `Cells()` stays on the API surface for two minor
versions as a method that is either wrong or panics. Deprecating a trap does not
close it. The README already promises that the public API breaks without notice
until v1.0.0, and the only in-repo callers are ours.

### 5. Replace the whole-grid accessor with a per-row accessor ✅

**Chosen.** Expose the row as the unit of access and let the stride stay inside
the buffer. See below.

## Decision

**`Row(y) []Cell` replaces `Cells()` as the bulk accessor. `RowBytes` becomes a
`*Buffer` method that refuses sub-buffers. The diff takes `*Buffer`, not
`[]Cell`.**

The whole decision is one sentence: **a sub-buffer has no correct flat index, so
the stride must never leave the `buffer` package.**

### 1. `Buffer.Row(y int) []Cell` — the new bulk accessor

```go
// Row returns row y as a slice aliasing the buffer's storage, length
// Width(). It is the bulk cell accessor: correct on a top-level buffer and on
// a sub-buffer alike, because the stride is applied here rather than being
// left to the caller.
//
// An out-of-range y returns nil, so `for _, c := range buf.Row(y)` is safe for
// any y. The returned slice aliases the buffer: do not resize the buffer while
// holding it. On a sub-buffer consecutive calls are NOT adjacent in memory;
// a dense whole-buffer copy is `for y := range ... { copy(dst, b.Row(y)) }`.
func (b *Buffer) Row(y int) []Cell
```

This is not a new accessor. It is the package's existing unexported
`(*Buffer).row(y)` — already stride-correct and already used by `ClearRect`,
`FillRect` and `Clip` — promoted to the public surface, and every internal
caller moved onto it so there is exactly one row-addressing expression in the
package.

`Row` covers the common case (bulk work within a row) with zero allocations and
no invariant for the caller to remember, and it is correct on every kind of
buffer. `Cells()` covered the rare case (whole-grid flat scan) with a trap. The
rare case is the one that loses.

### 2. `RowBytes` becomes a method and refuses sub-buffers

Settling ADR 0002's risk 1b, which proposed exactly this:

```go
// RowBytes returns n consecutive cells' backing memory starting at (x0, y),
// as bytes, for a byte-wise row comparison (ADR 0002).
//
// It panics if b is a sub-buffer. A sub-buffer's rows are strided, so no range
// of its cells is contiguous, and the caller cannot be handed one — the
// correct answer is to not diff a view. This is a TermMosaic bug, not a user
// error: the diff is the only caller and it only ever receives the renderer's
// top-level front and back buffers.
//
// Returns nil for an out-of-range or empty range.
func (b *Buffer) RowBytes(y, x0, n int) []byte
```

Two properties, both deliberate:

- **The receiver makes the old call a compile error.** `buffer.RowBytes(cells,
  from, n)` no longer resolves, so every existing call site is forced through
  the checked path. That is ADR 0002's stated goal ("an unsound call is a
  compile error rather than a review comment") and it is achieved.
- **It panics rather than returning nil.** `SetCell` deliberately does *not*
  panic on an out-of-range write, because those coordinates come from widget
  layout and a TUI that crashes on a rounding error is unusable. `RowBytes` is
  the opposite case: it is called only by the diff, on buffers the framework
  itself chose, and there is no correct result to return for a view. Silently
  returning nil would make `bytes.Equal(nil, nil)` report two strided rows as
  equal — a wrong answer wearing a safe-looking signature. A panic in the diff
  is the right failure.

The free function `buffer.RowBytes([]Cell, int, int) []byte` is removed.

### 3. `diff.Frame` carries buffers, not slices — **the diff does need a guard**

Today the diff consumes `Cells()` and then indexes the result with
`y*f.Width + x`:

```go
Cur:        r.back.Cells(),
Prev:       r.front.Cells(),
...
from := y * f.Width
buffer.RowBytes(f.Cur, from, f.Width)
cur := f.Cur[rowStart+x]
```

Both of those are stride-blind. It happens to be correct because
`render.Render` only ever passes its own top-level front and back buffers, and
ADR 0002 documents that discipline — but "documented" is the mitigation ADR 0002
already identified as insufficient, and `RowBytes` cannot even detect the
violation, because a bare `[]Cell` carries no stride to check against. **So
yes, a guard is needed**, and the guard is the type:

```go
type Frame struct {
    // Cur and Prev are the new and previously-rendered cell grids. Both must be
    // top-level buffers (Stride == Width); RowBytes panics on a view, and that
    // panic is the enforcement of this comment rather than the comment itself.
    Cur, Prev *buffer.Buffer
    Width, Height int
    ...
}
```

**`Width` and `Height` stay**, despite now being derivable from `Cur`. Two
sources of truth would be its own footgun, so instead `Diff` checks them at
entry and panics on mismatch:

```go
// both buffers non-nil; Cur.Width() == Prev.Width() == f.Width;
// Cur.Height() == Prev.Height() == f.Height
```

Same rationale as `RowBytes`: this is framework-internal wiring, and a mismatch
means the renderer handed the diff two different grids, which is a bug worth
loudly failing rather than silently mis-indexing. The check runs once per
`Diff` call, not per cell.

Tier 2 gets *faster*, not slower, and this is worth stating because it is the
obvious objection to moving the diff off direct slice indexing: hoisting the row
out of the inner loop replaces `f.Cur[rowStart+x]` — a multiply-add and a bounds
check per cell — with one slice expression per row:

```go
curRow, prevRow := f.Cur.Row(y), f.Prev.Row(y)
```

`BenchmarkDiffDefaultSceneAllRows` and the 0-allocs/op assertion must be re-run
after this change. See "Risks".

### 4. `Stride()` is deliberately not exported

An exported `Stride()` invites `y*Stride()+x` at every call site, which is
option 3 with extra steps. The stride is an implementation detail of row
addressing, and ADR 0002's own invariant statement lives in `buffer`, not in
every consumer of `buffer`. `buffer_test.go` already reads `sub.Width()` and the
parent's stride through the package-internal `row`, and keeps doing so.

## Consequences

**Good**

- **The trap is closed rather than documented.** There is no longer a public
  API whose correct use depends on a field the caller cannot see.
- **`Row` is what widget authors need anyway.** A row-oriented widget writes
  `row := buf.Row(y)` then indexes `row[x]`, which is both correct on a view and
  the fastest form available — one bounds check hoisted per row.
- **Bulk access stays zero-copy and zero-alloc**, so the draw path does not
  regress; ADR 0002's ~6.8 ns/cell figure is unaffected.
- **`RowBytes` unsoundness becomes a compile error** where it was a review
  comment, which is what ADR 0002 risk 1b asked for, settled before v1.0 rather
  than at it.
- **The diff's precondition becomes checkable.** `Frame` carrying `*Buffer`
  means the "top-level only" rule is enforced by `RowBytes` at runtime instead
  of by the discipline of whoever calls `Diff`.

**Bad — stated plainly**

- **A whole-grid flat view is no longer available.** Code that wanted
  "the whole grid as one slice" must now loop rows. That is the deliberate
  trade: one API removed for thirty widgets vs one API that silently corrupts
  one of them. `MemorySink.Cells()` still returns a flat slice, because it is a
  *detached dense copy* of the whole screen, where flat is the correct shape.
- **The diff's `Frame` type changed**, which touches `render` and roughly forty
  fixture sites in `internal/diff`'s tests and benchmarks that build raw
  `[]buffer.Cell` grids. Mechanical, but it is the largest single cost of this
  ADR and it is worth not understating.
- **`RowBytes` panicking is a new panic path** in a package whose sibling
  `SetCell` is carefully panic-free. The distinction is real (framework-internal
  vs layout-derived) but it is a distinction a future reader has to be told
  about, which is why both are stated in both doc comments.
- **`Row` on a sub-buffer returns non-adjacent slices.** A caller who
  concatenates rows assuming adjacency reintroduces the original bug in a new
  place. `Row`'s doc comment says so explicitly, and there is no type-level
  canary for it — the same gap ADR 0002 records for precondition (b).

## Rejected alternatives, specifically

- **`CellAt` only, no bulk accessor** — correct and simple, but the common
  widget case (fill one row) becomes a bounds-checked call per cell on the hot
  draw path. Rejected on cost.
- **Panic on `Cells()` for a sub-buffer** — a sub-buffer has no correct flat
  index, so the only answer the method could give is a panic. That is not safety,
  it is the absence of an API. Rejected because `Row` answers the real need.
- **Stride-aware `Cells()` + exported `Stride()`** — the worst option. It keeps a
  silent-wrong-answer API and moves the invariant onto every widget author.
  Rejected because it makes the framework's one structural mitigation (the
  `stride` field, ADR 0002) into thirty opportunities to forget it.
- **Deprecate for v0.x, remove at v1.0** — a deprecated method that is either
  wrong or panics is still a footgun with a doc comment. Nothing in-repo needs
  migrating, and the README already promises pre-1.0 API breaks. Rejected as
  cost without benefit.
- **Leave `Cells()` and rely on the doc comment** — this is the status quo, and
  ADR 0002's amendment is the argument against it: that is precisely how the
  original unsoundness shipped, with every documented invariant intact.
- **Make `SubBuffer` copy into a dense buffer instead of striding** — would make
  `Cells()` sound everywhere, at the cost of a copy per composed region per
  frame, and of losing view semantics that make layout composition cheap.
  Rejected; ADR 0002 already chose views.

## Forced changes to existing code

Every exported identifier this decision moves, by name:

| Identifier | Current | Proposed |
|---|---|---|
| `buffer.Buffer.Cells` | `func (b *Buffer) Cells() []Cell` | **removed** |
| `buffer.Buffer.row` (unexported) | `func (b *Buffer) row(y int) []Cell` | exported as `func (b *Buffer) Row(y int) []Cell` |
| `buffer.RowBytes` (free function) | `func RowBytes(cells []Cell, from, n int) []byte` | **removed** |
| `buffer.Buffer.RowBytes` (new) | — | `func (b *Buffer) RowBytes(y, x0, n int) []byte`, panics on a sub-buffer |
| `diff.Frame.Cur` | `Cur []buffer.Cell` | `Cur *buffer.Buffer` |
| `diff.Frame.Prev` | `Prev []buffer.Cell` | `Prev *buffer.Buffer` |
| `headless.MemorySink.Cells` | `func (s *MemorySink) Cells() []buffer.Cell` | **unchanged** — reimplemented as a row-wise dense copy |

Unchanged and stated as such, so nobody widens them by accident:
`Buffer.CellAt`, `Buffer.SetCell`, `Buffer.Set`, `Buffer.SetString`,
`Buffer.Clip`, `Buffer.SubBuffer`, `Buffer.Size`/`Width`/`Height`,
`diff.Frame.Width`/`Height`/`Rects`/`Cursor`/`PrevCursor`/`ForceFull`/
`Encoder`.

Call sites that must move:

1. `render/render.go:386-387` — `Cur: r.back`, `Prev: r.front`.
2. `headless/memorysink.go:149-150` — build the copy with `Row(y)`.
3. `buffer/buffer.go:81,215,260` — `b.row(y)` → `b.Row(y)`.
4. `buffer/dirty_test.go:107`, `buffer/buffer_test.go:244` — same.
5. `buffer/cell_test.go:52` — `RowBytes(b.Cells(), 0, 4)` → `b.RowBytes(0, 0, 4)`.
6. `internal/diff/diff.go:147-165` — tier 1 via `RowBytes`, tier 2 via hoisted
   `Row(y)`.
7. `internal/diff/diff_test.go` and `internal/diff/bench_test.go` — the ~40
   `Frame{...}` literals that pass `make([]buffer.Cell, w*h)` grids become
   `buffer.NewBuffer` plus a fill helper.

Tests to add, in ADR 0002's spirit:

- `TestRowIsStrideCorrectOnSubBuffer` — `Row(y)[x]` on a view equals the
  parent's `CellAt(x0+x, y0+y)`, for every row and column.
- `TestRowOutOfRangeIsNil` — `Row(-1)` and `Row(Height())` return nil.
- `TestRowBytesPanicsOnSubBuffer` — the panic fires, with a message naming
  `Row`/`SubBuffer` as the alternative.
- `TestDiffRejectsMismatchedFrameSize` — `Diff` panics when `Width`/`Height`
  disagree with either buffer.
- Extend `TestSubBufferRowsAreStrided` (it already exists and already pins the
  property) to assert through the *public* `Row`, so the invariant is pinned at
  the surface callers actually use.

## Risks to revisit at v1.0

1. **The diff's measured cost must be re-confirmed.** ~~Unresolved~~
   **Resolved 2026-10-04 — measured, and it improved.** On the same machine,
   with the baseline taken by stashing the change and re-running the identical
   command, `BenchmarkDiffDefaultSceneAllRows` went **6,950 → 6,244 ns/op**
   against ADR 0002's 7,133 ns/op target, still at **0 allocs/op**.

   The two tiers moved in opposite directions, which is worth recording rather
   than reporting only the net. Tier 2 got faster — hoisting `Row(y)` out of
   the cell loop replaces a per-cell multiply-add and bounds check with one
   slice expression per row. Tier 1 got *slower* in isolation, 219 → 256 ns/op,
   because `RowBytes` is now a method that checks stride and bounds per call
   where the free function checked one length. Tier 2 more than pays for it on
   ADR 0002's workload, where 56 skipped rows are the common case.

   So the guard is **not** evidence that it belongs in the renderer. The
   `*buffer.Buffer` frame field stays.

2. **The panic on a mismatched `Frame` is unreachable from the renderer, and
   that is now a tested invariant rather than an argument.** `checkFrame`
   panics unless `Frame.Width`/`Height` match both buffers exactly. The old
   `[]Cell` API expressed "no usable previous frame" as a short `Prev`, and
   silently fell back to a full repaint. A short `Prev` is now unrepresentable,
   so that path **panics instead of degrading** — a deliberate behaviour change,
   not a pure refactor.

   `Renderer.Resize` clamps negatives and sizes *both* buffers to exactly
   `w`×`h`, so `front` and `back` cannot drift apart or disagree with
   `r.w`/`r.h`. `TestResizeNeverBreaksFrameInvariant` (`render/`) now pins
   that across 0×0, 1×1, single-axis 1-cell, negative, and 200×60 sizes, each
   followed by a real `Render()`. Verified non-vacuous: resizing only `back` by
   `w-1` makes the test fail on the width assertion *and* trip the diff panic.

   The change is therefore safe in practice, but it is still a real semantic
   change for any external caller that was relying on the silent fallback.
3. **`Row` has no type-level canary for non-adjacency.** A caller that
   concatenates rows of a sub-buffer as though they were adjacent reintroduces
   ADR 0002's original bug. The mitigation is documentation plus the existing
   `TestSubBufferRowsAreStrided`; if a widget in the catalog ever gets this
   wrong, the fix is a distinct accessor type (`Rows` returning a small
   iterator) rather than another comment.
4. **`Clip` is the escape hatch and it allocates.** A widget that genuinely
   wants a dense flat grid should call `Clip`, which copies. Worth watching that
   no widget does that per frame inside `Draw`, where it would be an
   allocation on the frame path and would break the 0-allocs/op bar ADR 0002
   holds the diff to.
