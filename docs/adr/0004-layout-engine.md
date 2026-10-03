# 0004 — Layout engine

- **Status:** Accepted
- **Date:** 2026-10-03
- **Decides:** [STATUS.md](../STATUS.md) — Core architecture / Layout engine
- **Depends on:** [ADR 0003](0003-renderer-mode.md)
- **Supersedes:** the "constraint / flexbox" section of the former
  ARCHITECTURE.md "Decision 4".

## Context

Terminal layouts are small, fixed, and mostly rectangular: a header, a
sidebar, a main pane, a status bar. Two models compete:

- **Constraint-based** — `Length(20)`, `Percentage(30)`, `Ratio(1,3)`,
  `Min(10)`, `Fill(1)`. tmux, Ratatui, and Bubble Tea all use this.
- **Flexbox-style** — row/column containers with grow/shrink/basis, nesting,
  gap, alignment. CSS and Yoga.

OpenTUI uses Yoga per layout node. Its architecture is Zig core plus 8 native
FFI artifacts; the Yoga dependency is one of those native artifacts.

## Evidence gathered

No benchmark was run for layout. This decision was made on API-surface and
dependency grounds, and we say so plainly rather than implying measured
support. Two concrete facts were established by inspection:

- **Vendoring Yoga costs cgo.** A C dependency in a Go project that advertises
  *single static binary, trivial cross-compilation* is a direct contradiction of
  a stated project goal — cgo breaks `CGO_ENABLED=0` cross-compilation, which
  is how most Go users build release binaries for multiple platforms. This is a
  packaging regression, not just an aesthetic one.
- **Go users already have a constraint vocabulary.** Bubble Tea is the dominant
  Go TUI framework and its `lipgloss` layout is constraint-based. A Go developer
  arriving at TermMosaic already has the mental model.

## Options considered

### Flexbox via vendored Yoga

- **Pros:** matches web developer expectations; nesting, grow/shrink/basis, and
  gap are expressive and familiar; OpenTUI proves it can work for a TUI.
- **Cons:** **cgo**, which breaks the static-binary cross-compilation pitch and
  removes `CGO_ENABLED=0` builds. A 16k-LOC C dependency to lay out a few
  hundred cells. Solves nesting better than constraints do, at a cost we are
  not willing to pay.

### Flexbox, own implementation

- **Pros:** no cgo; full control; familiar model.
- **Cons:** flexbox's growth/shrink/basis resolution is genuinely fiddly and
  almost entirely irrelevant in a terminal. Rebuilding it is a large amount of
  work for features — `flex-wrap`, baseline alignment, `order`, intrinsic
  sizing — that no terminal layout will use. Over-engineering.

### Constraint-based, own implementation ✅

## Decision

**Constraint-based layout, our own implementation, no cgo, no native
dependencies.**

```go
// A layout is a direction plus constraints and spacing.
type Layout struct {
    Direction  Direction  // Horizontal | Vertical
    Constraint []Constraint
    Spacing    int
}

type Constraint interface {
    apply(available int, constraints []Constraint) int
}

func Length(n int) Constraint     // fixed cells
func Min(n int) Constraint        // at least n cells
func Max(n int) Constraint        // at most n cells
func Percentage(p int) Constraint // 0-100 of available
func Ratio(n, d int) Constraint   // n/d of available
func Fill(weight int) Constraint   // distribute remaining space by weight
```

- Constraints resolve **top-down, in one pass, in declaration order**. `Fill`
  consumes the leftover after all fixed constraints are subtracted; remaining
  `Fill` weights split what is left. This is tmux's rule and the one users know.
- **Nesting is composition, not a new engine.** A widget's `Bounds()` becomes a
  rect; placing a sub-layout inside a rect is just running the solver on that
  rect. There is no parent/child constraint negotiation to get wrong.
- **Splitting and resizing panes are first-class.** Because `Length`, `Min`, and
  `Fill` compose, a resizable split is just a layout whose separator carries a
  `Length(n)` that the drag handler mutates. This is the case that makes
  constraint-based the right choice for TUI work.
- Solver is **pure and allocation-light**: `func Solve(d Direction, cs
  []Constraint, spacing, available int) []int`, cacheable and unit-testable with
  no terminal. It must not import our buffer package.

### What flexbox users lose, and the answer

Nesting and proportional splits are covered. Genuinely absent: intrinsic
content-driven sizing, baseline alignment, `order`, wrap. Mitigation: widgets
report a natural size through `Bounds()`/`MinSize()` where cheap, so
`Fill` composes with content-aware layouts where it matters. We accept that
some web-shaped layouts are not expressible. That is the correct trade for a
terminal.

## Consequences

**Good**

- No cgo; `CGO_ENABLED=0` cross-compilation preserved; **zero native
  dependencies** and a genuinely single static binary.
- Matches what Bubble Tea users already know, lowering the adoption cost for
  our actual Go competitor.
- Splitting, nesting, and drag-resize compose from the same primitive set.
- A ~200-line pure solver with zero terminal dependency is exhaustively
  testable in CI, which serves the testability pillar.
- `Fill` + `Min` + `Max` covers nearly every real TUI layout.

**Bad — stated plainly**

- **Less expressive than flexbox.** No wrap, no `order`, no baseline alignment.
  Some web-shaped layouts are inexpressible.
- **Intrinsic content sizing is weak.** A constraint layout does not naturally
  size a pane to its content. Widgets must volunteer a size, and if they lie
  the layout will clip them. This is a real source of bugs and needs a clear
  documented convention in v1.
- **`Fill` is order-sensitive.** `Fill` before a `Length` behaves differently
  from `Length` before a `Fill`, which is a sharp edge for users. We must
  document the resolution order explicitly and cover it with tests.
- **Top-down only.** There is no CSS-style bidirectional constraint solving.
  Mutual size dependencies between siblings cannot be expressed.

## Rejected alternatives, specifically

- **Vendor Yoga** — **cgo**, which breaks `CGO_ENABLED=0` cross-compilation
  and contradicts the single-static-binary goal. Rejected on packaging grounds,
  independent of its layout quality.
- **Write our own flexbox** — over-engineering. Grow/shrink/basis resolution is
  complex and nearly all of that complexity is dead weight in a terminal.
  Rejected per YAGNI.
- **Adopt `charmbracelet/lipgloss` layout** — pulls Lip Gloss's whole styling
  layer into our dependency tree and boxes us into constraint semantics we
  would then have to match. We are writing our own solver; we borrow the
  *vocabulary*, not the code.

## Risks to revisit at v1.0

1. **`Fill` ordering sharp edges.** Highest-likelihood usability complaint in
   the first six months. Revisit if users routinely fight the solver; the escape
   hatch is a `Grid` helper widget that makes correct ordering automatic.
2. **Intrinsic sizing.** Revisit when List/Table/Tree autosize is scoped — a
   virtualized widget cannot know its content height cheaply, so `Fill` will be
   the only option there and that may not be enough.
3. **Solver complexity under nesting.** Unmeasured. A single-pass top-down
   solver is fine for one level and probably fine for ten, but we have not
   verified it for deeply nested layouts. Revisit if deep nesting appears in
   real apps.
4. **Web-developer expectations.** If field feedback shows flexbox is what
   people actually reach for, `Flex(direction, children...)` can be layered on
   top of the same solver as sugar without replacing it. Deliberately not built
   now.