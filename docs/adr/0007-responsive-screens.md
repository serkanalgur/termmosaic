# 0007 — Responsive screen composition

- **Status:** Accepted
- **Date:** 2026-10-04
- **Decides:** [STATUS.md](../STATUS.md) — Core architecture / Responsive screen
  composition
- **Depends on:** [ADR 0003](0003-renderer-mode.md) (the `Widget` interface),
  [ADR 0004](0004-layout-engine.md) (the constraint vocabulary),
  [ADR 0005](0005-input-decoding.md) (`EventResize` and the ordered stream),
  [ADR 0006](0006-subbuffer-cell-access.md) (`Row`, clipping semantics)
- **Answers:** the widget catalog's open question — what "responsive" means in a
  terminal, and therefore what the catalog may and may not assume. Three coders
  are about to build independent widget sets against this document in parallel.

## Context

Resize already works mechanically, and it is worth being exact about *how* well,
because the gap is narrower than it looks:

- `EventResize` arrives on the single ordered stream from
  [ADR 0005](0005-input-decoding.md) §5, coalesced keep-latest while undelivered.
- `render.Renderer.Resize` (`render/render.go:171-193`) resizes both buffers,
  clears the dirty-rect accumulator, sets `forceAll`, resets the differ's style,
  and clamps the managed cursor back inside the new screen.
- `buffer.Resize` (`buffer/buffer.go:88-103`) allocates a fresh cell slice and
  clears it, so nothing from the previous geometry survives.
- `examples/hello/main.go:276-283` recomputes its root rect through
  `centred(w, h, 46, 9)` and paints.

**So the mechanism is not missing. The design is.** And the design that exists
today is one specific anti-pattern: `examples/hello` asks for a hard-coded 46×9
block and *clamps* it to the screen (`main.go:153-162`). At 20×8 the user gets a
20×8 block whose three body rows are silently cut off by the `y >= r.Bottom()-1`
guard in `drawBody`; at 10×4 the title is dropped by `if r.W < 16` and the body
rows are dropped by the height guard. Nothing is wrong and nothing is useful —
the block does not *adapt*, it *survives*.

That is the real state of responsiveness: **clamp-and-hope**, thirty times over
once the catalog exists.

### What a terminal does not have, and why it matters

Three properties that make web responsiveness hard are simply absent:

| Web mechanism | Terminal reality |
|---|---|
| A viewport the layout can be *queried* against before paint | There is a size, but it is reported by SIGWINCH, arrives asynchronously, and can be **0×0** when a terminal detaches. |
| Resize can arrive continuously | Resize arrives **abruptly**, in a burst, during a drag — typically tens of events in a few hundred milliseconds. |
| Layout is a function of viewport width alone | Layout is a function of two axes, and one of them (height) is small: a realistic ceiling is 24–60 rows. |

And one property the web *lacks* but we have, which changes the whole answer:

> **Height is scarce in a way width is not.** A terminal is 200×60 or 80×24. A
> widget asked to show twelve rows in eight available rows does not have a
> layout problem — it has an **information budget** problem.

So "responsive" for a terminal is not reflow. It is: **decide what to leave out
when there is not enough room, and never do anything that crashes, corrupts the
frame, or silently lies about what it is showing.**

### The cost of getting this wrong, stated before the decision

Three coders are building the catalog in parallel against this document. The
failure mode is not one bad widget — it is **three widgets that each invented
their own threshold, their own priority scale, and their own degenerate-size
behaviour, and now three quarters of the catalog disagree**. That is very hard to
unify after thirty widgets exist, and it is very cheap to prevent right now. The
entire purpose of this ADR is to spend that cheapness.

## Options considered

### A framework-level size-class system (compact / medium / expanded) — rejected

The obvious thing to build, and what any CSS-shaped instinct reaches for.

- **Pros:** readable widget code (`if class == Compact`); one place to change
  thresholds; familiar to web developers.
- **Cons:** a class is a *lossy function of two numbers*, so two different
  widths in the same class must produce identical layout, which is false for
  almost every widget; thresholds are a **product** decision (is 100×30
  "medium"?), not a framework fact, and putting them in the framework means
  every application inherits them; and it creates a *second source of truth* for
  space that already has one — the rect. A widget author would be able to write
  `if Compact` and `if r.W < 40` in the same `Draw`, and be right about one of
  them.
- Ratatui and Bubble Tea do not have size classes and their users do not
  complain about it. Neither does tmux. That is not proof, but it is evidence
  that the mechanism is not load-bearing.

### Change `Draw` to receive available space: `Draw(buf *Buffer, space Size)` — rejected

The task's own strong candidate, and worth evaluating properly.

- **The information is already there.** A widget gets its space from
  `Bounds()`. `Draw` writing "into a pre-sized buffer" is not a limitation — the
  buffer is the *screen*, and `Bounds()` is the *widget's* rect, and a widget
  that confuses the two has a bug. Passing the space again duplicates one source
  of truth and adds a way for the two to disagree.
- **It costs thirty signature changes for zero new information.** That is the
  wrong side of the ADR 0003 trade in every direction.
- **Rejected — but the underlying worry is real** and is answered below: what a
  widget genuinely lacks is not the space, it is a *declared minimum*, so
  something above it can react, and a *notification* that the space changed. Both
  are addressed without touching `Draw`.

### Add `Layout(Rect)` as a fourth mandatory `Widget` method — rejected

Give the framework an explicit reflow pass instead of adapting inside `Draw`.

- **Pros:** adaptation is visible in the interface; cached layout can be
  recomputed once per size change rather than per frame.
- **Cons:** it is a *silent*-failure surface, which is exactly the class ADR
  0003 §"Consequences" names as its top risk ("silent invalidation bugs are the
  worst class of bug a TUI has"). A widget that forgets to implement `Layout`
  compiles fine and then renders content for the previous size forever. And
  thirty implementations of the same method is thirty places to get it subtly
  wrong.
- **The capability is recovered without the method**, by the lazy rule in §3:
  a widget compares its current rect against the one it last adapted to, and
  recomputes when they differ. That is self-detecting — a widget cannot forget
  it, because the check *is* the computation.

### Framework-owned minimum-size diagnostic screen — rejected

- **The decision is not ours.** Whether a terminal too small for a dashboard
  should show "enlarge your terminal", or a bare dashboard with its lowest-priority
  cards hidden, is an application decision. The framework cannot know whether
  losing the table is acceptable.
- What the framework owes the application is the *ability to ask*, which is
  `MinSize()` (§2).

### Interpolation between sizes — rejected

Nothing in a cell grid interpolates. A column is 40 cells wide or it is 39. Any
apparent smoothness would be a rounding rule pretending to be a continuum.

## Decision

**Responsiveness in TermMosaic is a budget, not a reflow. The framework provides
the arithmetic — a row clamp, a priority scale, and a declared minimum size — and
the widget author supplies the policy. There are no size classes, no breakpoints
in the framework, and no framework-owned "too small" screen.**

The dividing line:

| Concern | Owner |
|---|---|
| Choosing a widget's rectangle from available space | **Application**, using [ADR 0004](0004-layout-engine.md)'s solver. Unchanged by this ADR. |
| Deciding how many content rows to show in that rectangle | **Widget**, using `geometry.ClampCount`. |
| Deciding *which* rows to drop when even that is too many | **Widget**, using `geometry.Budget`. |
| Deciding the smallest size at which the widget is worth showing | **Widget**, via `termmosaic.Minimizable.MinSize`. |
| Deciding what the whole screen does when it is too small for its root | **Application.** Not a framework primitive. |
| Making every one of those safe at 0×0 and 1×1 | **Framework contract**, applied to every widget without exception. |

Everything in the widget column is one function call. Everything in the framework
row is a rule that holds whether or not a widget author thinks about it. That
asymmetry is the design: **policy per widget, arithmetic and safety shared.**

### 1. The shared vocabulary, by name and signature

This is the load-bearing section. Three coders implementing independent widget
sets must use these names and these meanings, and **must not define their own
equivalents.** All three live in **`geometry`**, which today imports nothing and
is depended on by both `buffer` and `layout` — so adding them there is free of
cycles and requires no import changes anywhere.

```go
// ClampCount returns how many content units of n fit in available cells.
//
// It never returns a negative number and never more than n. This one function
// IS "show 3 rows at small sizes and 10 at large ones": pass the full content
// height and the available height.
//
// It counts CELLS, not glyphs: a double-width rune occupies two cells, so a
// row budget computed here can be one row optimistic once wide characters are
// in play. See "Deferred".
func ClampCount(n, available int) int

// Priority ranks a content region for responsive budgeting. A widget drops
// regions from the lowest priority up when its available space cannot show
// them all. The four values are a total order and are the only ones.
type Priority uint8

const (
    PrioLow    Priority = iota // dropped first
    PrioNormal                 // the default; dropped before PrioHigh
    PrioHigh                   // dropped last among budgetable regions
    PrioAlways                 // never dropped, whatever the budget
)

// Region is one budgetable chunk of a widget's content, measured along
// whichever axis the widget is laying out. Budget does not know or care which
// axis that is; the axis is the caller's business.
type Region struct {
    // Size is how many cells this region occupies along the caller's axis.
    // Negative values are treated as zero.
    Size int
    // Prio is the region's drop priority.
    Prio Priority
}

// Budget reports which of regions fit in available cells, dropping from the
// lowest priority up until the total fits. The returned slice has one entry
// per region, in the same order: true means the region is shown.
//
// Rules, all of which are the contract:
//   - PrioAlways regions are never dropped and always count against the budget.
//   - Within one priority, regions are kept in declaration order: equal priority
//     means declaration order is the tiebreak, never a coin flip.
//   - If PrioAlways regions alone exceed available, every region is reported
//     kept. The caller is then over budget and clips. Budget never panics and
//     never reports a region as dropped when dropping it would not help.
//   - The returned slice is newly allocated. Callers must CACHE it and
//     recompute only when the widget's rectangle changes — see §3.
func Budget(regions []Region, available int) []bool
```

And in the root package, beside the existing `Focusable`:

```go
// Minimizable is an optional interface a Widget implements if it has a
// smallest size at which it can render something meaningful.
//
// This is the existing Focusable pattern exactly: optional, so that adding it
// costs no widget anything, and discoverable by a type assertion.
//
// MinSize is the size of the WHOLE widget INCLUDING its own chrome — a bordered
// Table with MinSize{20, 5} needs 20x5 cells, not a 20x5 content area. Pinning
// this here is deliberate: three authors would otherwise each decide whether
// their border counts, and every caller's arithmetic would then be wrong for
// some subset of the catalog.
type Minimizable interface {
    Widget
    // MinSize returns the widget's smallest meaningful size in cells. It must
    // be pure, must not depend on the current Bounds, and must be safe to
    // call before the widget has ever been drawn.
    MinSize() Size
}
```

**Collision-avoidance rules, so the vocabulary cannot drift:**

1. **A widget's available space is `Bounds()`, never `buf.Size()`.** The buffer
   is the *screen*; the widget's rect is the widget's space. A widget that
   reads `buf.Width()` as its own width is a bug even when the widget is the
   root. This is the single most likely divergence between three authors and it
   is now named and forbidden.
2. **`Bounds()` is clipped to the screen before `Draw` sees it.** A widget's
   rect can never extend past the screen; `layout.Solve` overflow is resolved
   by clipping at the composition boundary, not by handing a widget a rect it
   must defend against. (See §4.)
3. **A widget must repaint its entire `Bounds()` before drawing content into
   it.** `examples/hello/main.go:87-89` already states the rule and gives the
   reason — the renderer never clears, it diffs — but at design time that reads
   as tidiness. Under this ADR it is load-bearing on **every size change**, not
   only at construction: a widget that drew 10 rows and now draws 3 leaves seven
   stale rows on screen unless it repainted the whole rect first.
4. **No widget defines a local `clamp`, `fit`, `minRows`, `Priority`, or
   `Budget`.** If the shared one is missing something, that is a bug in this
   ADR, reported and fixed here — not a private helper in one widget's package.
5. **Thresholds are local named constants, not framework vocabulary.** A widget
   that switches variant at width 40 writes `const wideEnough = 40` in its own
   package and compares `if r.W >= wideEnough`. That is the whole breakpoint
   mechanism and it is deliberately unshared.

### 2. How a widget author expresses "3 rows when small, 10 when large"

Two levels, both plain, because the second is not worth an abstraction:

```go
// Level 1 — how many rows of my content fit. One call, no branching.
rows := geometry.ClampCount(len(items), r.H)

// Level 2 — a variant switch. An integer comparison against a local constant.
const wideEnough = 40
if r.W >= wideEnough {
    drawTwoColumns(r, rows)
} else {
    drawOneColumn(r, rows)
}
```

And the priority form, for a widget whose content is heterogeneous (a dashboard
of cards, a detail pane of labelled fields):

```go
regions := []geometry.Region{        // built ONCE, stored on the widget
    {Size: 3, Prio: geometry.PrioHigh},     // identity — always worth showing
    {Size: 4, Prio: geometry.PrioNormal},   // metrics
    {Size: 2, Prio: geometry.PrioNormal},   // sparkline
    {Size: 3, Prio: geometry.PrioAlways},   // status bar
}
show := tm.Budget(regions, r.H)       // cached per rect — see §3
```

`regions` is built at construction, never per frame, because it does not depend
on the size. `Budget` **is** called on size change, because its answer does.

### 3. Where responsiveness lives: the widget, on a rect-keyed cache

**The `Widget` interface does not change.** [ADR 0003](0003-renderer-mode.md)'s
four methods are sufficient, and this is the argument for keeping them so:

- The space is available (`Bounds()`).
- The size change is available (compare against what you last adapted to).
- The minimum is declarable (an optional interface, free for widgets that do not
  want one).

What replaces an explicit reflow pass is a **rule**, not a method:

> **Adaptation is lazy and self-detecting. A widget that caches anything derived
> from its size — column widths, a row→item mapping, a `[]bool` from `Budget`, a
> wrapped-line index — caches it against the rect it was computed for, and
> recomputes when `Bounds()` differs. On the first `Draw` after a resize the
> widget recomputes; thereafter it does not.**

This is unskippable, which is why it beats a fourth interface method: a widget
that "forgets" a `Layout` implementation is broken silently forever, whereas a
widget that caches on the wrong key is broken in exactly one frame and the very
next one repairs it.

The amendment recorded in [ADR 0003](0003-renderer-mode.md) is therefore
contract-tightening only, and it is this sentence: **`Bounds()` must reflect the
new rectangle before the next `Draw`, and `Draw` must read `Bounds()` rather than
any cached copy of it.**

### 4. Degenerate sizes — a decided contract, identical for every widget

**No panic, ever. Clip, never blank.** Restating the distinction ADR 0006 already
draws: `RowBytes` panics because it is framework-internal wiring on a
framework-chosen buffer; a widget's coordinates are **layout-derived**, and a TUI
that crashes on a rounding error — or on a user dragging the window to 1×1 — is
unusable. This is the rule every one of the three parallel widget sets inherits:

| Situation | Contract |
|---|---|
| **Size 0 on either axis** (terminal detached, or size not yet known) | A **valid** size. `Render` writes **zero bytes** and does not call the `Sink`. Widgets may be called; every write is discarded by `Buffer.SetCell`'s existing bounds check (`buffer/buffer.go:137-144`). The event loop **must stay alive** and must not treat it as an error. |
| **Negative size in `render.Config` or `Renderer.Resize`** | Clamped to 0. Already the case (`render/render.go:103-104`, `174-177`); this ADR pins it as a contract rather than an implementation detail. |
| **1×1, or any size at all** | Widgets are called. Every widget's `Draw` must be **total**: defined for every `Rect`, including empty, and must write only inside `Bounds()`. No widget may assume `W >= 4` or `H >= 4`. |
| **Bounds smaller than the widget's `MinSize()`** | The widget draws its **minimum-size layout, clipped**. It does not panic, and it does **not** blank itself. Clipping is already what `SetCell`, `FillRect` and `Rect.Clip` do everywhere; blanking loses information precisely when information is scarce, which is the one thing a shrinking screen cannot afford. |
| **`Bounds()` empty** | `Draw` returns immediately. Cheap and required — thirty widgets all testing the same condition is the point of deciding it here. |
| **Widget not implementing `Minimizable`** | Fully supported. There is no framework default minimum and no framework reaction; the widget is simply clipped like anything else. |

### 5. The resize → reflow contract

**Order of operations, and it is already almost all of what exists:**

```
1.  app receives EventResize from source.Events()
2.  app calls r.Resize(w, h)              // buffers resize, forceAll = true
3.  app recomputes its root bounds from (w, h)
4.  pacer tick → Render() → root.Draw(back), every widget reading Bounds()
5.  dirty rects = the whole screen (forceAll is set)
6.  two-tier diff → Sink
```

Four things about that sequence are decisions rather than observations:

- **Steps 2 and 3 may be in either order, and nothing else may happen between
  them and step 4.** `Renderer.Resize` never draws, so the application has the
  entire interval between receiving the event and the next `Render` to recompute
  bounds. No framework support is required or provided for this, and none is
  needed.
- **`InvalidateAll()` after `Resize` is redundant and should be dropped.**
  `Resize` already sets `forceAll` and calls `back.MarkAllDirty()`
  (`render/render.go:187-192`). `examples/hello/main.go:282` does both. This is
  a recorded simplification, not a new mechanism.
- **The whole screen is invalidated, always. Partial repaint on resize is
  refused, and it is refused for a structural reason rather than a preference:
  `buffer.Resize` discards every cell (`buffer/buffer.go:100-102`), so the
  previous frame's contents do not exist to be compared against. There is no
  correct partial-repaint-on-resize for this buffer design.**
- **An app must not assume it sees every intermediate size.** ADR 0005 §5
  coalesces resizes keep-latest while undelivered, so a drag may deliver three
  sizes out of thirty. Every size an app receives was real; none is guaranteed.
  Nothing may depend on observing a particular width on the way past.

### 6. What must be cheap, and the drag-resize answer

A 300 ms drag at 60 resize events per second is ~18 resize events. Per event:

| Step | Cost | Verdict |
|---|---|---|
| `front.Resize` + `back.Resize` | 2 × `make([]Cell, w*h)`. At 200×60 that is 2 × 192 KB. | **The dominant resize cost, and it is in `buffer.Resize`, not in anything this ADR adds.** 18 events ≈ 6.9 MB of allocation over the drag — GC-visible, not latency-visible. Recorded as-is. |
| Whole-tree `Draw` | ~81 µs for 12,000 cells (ADR 0003, measured). ×18 ≈ 1.5 ms. | Free. |
| Full repaint | 19,979 bytes ×18 ≈ 360 KB over 300 ms. | Free — and it is what the terminal must receive regardless. |
| **A virtualized List/Table/Tree** | **Must be O(visible rows), not O(item count).** | **This is the one real constraint, and it is a requirement on the catalog.** |

So the rules for a virtualized data widget, decided here because three coders
will each write one and they must not diverge:

1. **A resize costs O(visible rows).** A 100,000-row list that recomputes its
   row→item mapping on resize has a bug, not a slow path. The mapping depends on
   the rect, so it is recomputed — but only over what is visible.
2. **Clamp the scroll offset; never re-derive it.** Shrinking the screen clamps
   `offset` so `offset + visible <= len(items)`. That is **O(1)**. Re-deriving
   the offset from a "keep the selected row centred" rule is O(1) too but is a
   *product* decision each widget makes for itself, and the three must agree, so
   the rule is: **clamp, do not recentre.**
3. **Adaptation is cached per rect** (§3), so a drag that produces 18 different
   heights recomputes 18 times, not once per row per frame.

**And the anti-jank mechanism, which is the strongest thing in this ADR because
it already exists:** `Pacer` renders at the target frame rate and checks
`NeedsFrame()` (`render/render.go:464-466`). An app that calls `r.Resize` for
every `EventResize` but does **not** call `Render` itself therefore coalesces a
drag burst to at most one full repaint per frame tick, automatically, with **zero
new code**. The rule for application authors is one sentence:

> **Call `r.Resize` for every `EventResize` — `Renderer.Size()` must stay
> truthful — and let the pacer decide when to paint.**

`Renderer.Resize` is also idempotent for an unchanged size
(`render/render.go:180-182`), so a duplicate or coalesced-away event costs
nothing.

### 7. Scope: v1, deferred, and triggers

**In v1, and that is the whole of it:**

- `geometry.ClampCount`, `geometry.Priority` + `PrioLow/Normal/High/Always`,
  `geometry.Region`, `geometry.Budget`.
- `termmosaic.Minimizable` with `MinSize() Size`.
- The five collision-avoidance rules in §1.
- The degenerate-size contract in §4.
- The lazy, rect-keyed adaptation rule in §3.
- The application-side rule in §6 (resize on every event, paint on the pacer).

**No new package. No new layout constraint. `layout` is unchanged** — `Budget`
sizes content *within* one widget, which is not what a constraint list is for,
and folding it into `layout` would blur the closed constraint set ADR 0004 chose
precisely because it is predictable. **`Widget` is unchanged.**

| Deferred | Trigger to revisit |
|---|---|
| **Size classes / framework breakpoints** | **Two or more widgets independently inventing the same threshold with the same number**, from two different authors. Until that happens the duplication is cheaper than the abstraction — one constant per widget, in the widget's own package, is a two-line cost. |
| **Reflow / reordering of regions** (multi-column layouts that *move* rather than hide) | A catalog widget needing to move a region rather than drop it. `Budget` drops; it does not reorder. `layout.Fill` already covers most multi-column needs without any of this. |
| **Framework "too small" diagnostic screen** | Two applications in `examples/` needing the same minimum-size message. It stays an application concern until then. |
| **Region-level (partial) repaint on resize** | **Not revisitable by choice** — `buffer.Resize` discards cells, so it is structurally impossible for this design. If measured resize latency on a real terminal is ever a problem, the fix is coalescing at the pacer, which already exists (§6), not a partial diff. |
| **Minimum-size propagation up the tree** (a layout that reserves each child's `MinSize`) | This is ADR 0004's risk 2 (intrinsic sizing) and belongs to it. Revisit when List/Table autosize is scoped. |
| **Wide-character correctness in `ClampCount`/`Budget`** | The wide-character/grapheme ADR. `ClampCount` counts cells and a double-width rune eats two, so a row budget can be one row optimistic. Recorded as a known limitation, deferred to that decision rather than half-solved here. |

## Forced changes to existing code

| Identifier | Current | After this ADR |
|---|---|---|
| `termmosaic.Widget` | 4 methods | **unchanged** |
| `termmosaic.Focusable` | optional interface | **unchanged** |
| `termmosaic.Minimizable` | — | **new**, optional interface |
| `geometry.ClampCount`, `Priority`, `Region`, `Budget` | — | **new**; `geometry` stays dependency-free and keeps importing neither `buffer` nor `layout` |
| `examples/hello.centred` | `centred(sw, sh, 46, 9)`, clamps | Replaced by a bounds computation expressed in `layout` terms, so the example demonstrates the documented path rather than the anti-pattern that prompted this ADR |
| `examples/hello/main.go:282` `r.InvalidateAll()` after `r.Resize` | present | **removed** — redundant |
| `examples/hello/hello_test.go` `TestResizeGolden` | grows 60×14 → 72×18 | Extended to cover a **shrink** and a **degenerate** size (0×0 and a size below the block's minimum), since a resize test that only grows cannot catch stale cells |

Tests to add, in ADR 0002's spirit:

- `TestClampCountNeverNegativeOrExceeds` — including negative `available`.
- `TestBudgetDropsLowestPriorityFirst` and `TestBudgetKeepsDeclarationOrderWithinAPriority`.
- `TestBudgetNeverDropsPrioAlways` — and that it reports **all** regions kept when `PrioAlways` alone overflows, rather than dropping one to look busy.
- `TestRenderAtZeroSizeWritesNothing` — 0×0 and 0×24 and 24×0; asserts zero bytes and **no `Sink` write at all**.
- `TestResizeShrinksAndRepaintsWholeRect` — grow *and* shrink, asserting no stale cells remain (the §1 rule 3 property, at the application level).
- `TestResizeCoalescedToOneRepaintPerTick` — many `Resize` calls with no intervening `Render` produce one repaint, pinning §6.
- `TestRootBoundsClippedToScreen` — a root whose constraints overflow the screen gets a clipped rect, never an off-screen one.

## Consequences

**Good**

- **Thirty widgets get safe degenerate behaviour from a rule, not from thirty
  decisions.** The most likely catalog-wide bug — a widget that panics, or blanks,
  at a size its author never tested — is now impossible by construction.
- **The shared vocabulary is three functions and one interface**, all in packages
  that already exist and already have the right dependency position. The cost of
  this decision is close to zero and it is being paid before thirty widgets exist,
  which is the only time it is cheap.
- **No interface change.** `Widget` survives intact, so ADR 0003's four-method
  argument is untouched and no widget signature moves.
- **No allocation on the frame path** for a widget that follows the caching rule,
  which keeps the draw path clear of the 0-allocs/op bar ADR 0002 holds the diff
  to.
- **Anti-jank is free.** The pacer already coalesces; §6 turns that into a
  documented contract instead of an accident.
- **No size classes**, so no application inherits thresholds it did not choose,
  and no widget has two sources of truth for its own space.

**Bad — stated plainly**

- **`Budget` allocates**, and a widget that calls it inside `Draw` on a large
  region list adds an allocation to every frame. The caching rule is the
  mitigation and it is a *rule*, not a type — nothing stops an author from
  ignoring it. This is the most likely performance regression in the catalog.
- **The clipping-below-minimum policy shows partial widgets.** A Table with
  `MinSize{20,5}` in a 12×40 terminal draws a half-table rather than a
  "too small" message. We chose that over blanking deliberately — losing content
  is worse than losing the indication that content was lost — but it *is* a
  visible choice, and users will have opinions.
- **No framework "too small" screen means every application reinvents one**, or
  more likely none does. Accepted: it is the application's decision to make.
- **`ClampCount` counts cells, not glyphs**, so a row budget can be one row
  optimistic once wide characters ship. Deferred to the wide-character ADR, and
  recorded here so it is a known limitation rather than a surprise.
- **Width-based variant switching is thirty local constants**, so the catalog
  will contain several unrelated `wideEnough` values that mean similar things.
  We accepted the duplication because each one is visible at its use site and a
  shared threshold would be a shared *product* decision we have no standing to
  make.
- **`Renderer.Resize` allocates two full cell grids per event.** This is the
  dominant cost of a drag-resize and this ADR does not reduce it. It is ~6.9 MB
  over an 18-event drag at 200×60, which is GC-visible rather than
  latency-visible — but nobody has measured it under load.

## Rejected alternatives, specifically

- **A framework size-class system (`Compact`/`Medium`/`Expanded`)** — a lossy
  function of two numbers, a product decision in the wrong layer, and a second
  source of truth for space that already has one. Rejected on all three.
- **`Draw(buf *Buffer, space Size)`** — the space is already reachable via
  `Bounds()`; the buffer is the screen and the rect is the widget's space.
  Duplicating it invites disagreement between the two and costs thirty
  signature changes for no new information.
- **`Layout(Rect)` as a fourth mandatory method** — a silent-failure surface,
  which is the exact bug class ADR 0003 identifies as its top risk. The lazy
  rect-keyed cache delivers the same capability and cannot be forgotten.
- **`Resize(Rect)` pushed down the tree by the renderer** — moves widget
  mutation off the render goroutine and reopens ADR 0003's top risk (silent data
  races) for a capability `Bounds()` plus a lazy check already provides.
- **A framework-owned "too small" diagnostic screen** — only the application
  knows whether a shrunken dashboard is acceptable. The framework owes it
  `MinSize()` and nothing more.
- **Row hiding expressed as `layout.Min` in the parent's constraint list** —
  hides a widget's own content decision inside its parent's layout, so the
  widget cannot honour it, and the same widget placed by two different parents
  would behave differently. Rejected: content budgeting belongs to the widget.
- **Interpolating between sizes** — nothing in a cell grid interpolates.
- **A `Widget` change of any kind** — considered three ways (extra `Draw`
  parameter, `Layout` method, pushed `Resize`) and all three rejected on cost or
  on silent failure. Recorded here so the next person does not re-open it
  without new information.

## Risks to revisit at v1.0

1. **`MinSize()` includes chrome, and that is a convention.** It is stated here
   and pinned by the doc comment because it is the kind of thing three authors
   each answer differently. If it proves wrong in practice, the fix is renaming
   the method — which is thirty edits. That is the **single most expensive
   decision in this ADR**, and it is being made while the catalog is empty,
   which is the only time it is cheap.
2. **The rect-keyed caching rule is documentation, not a type.** A widget that
   caches adaptation against something other than its rect renders one stale
   frame and then looks fine, which is the *best* case; a widget that caches
   nothing is fine. The real hazard is a widget that caches and does not key it.
   Mitigation: widget tests should render at two sizes in sequence and assert the
   second is correct, not merely non-crashing.
3. **`Budget` called per frame.** Watch for allocations appearing in draw-path
   benchmarks once the catalog exists. If it shows up, the fix is a caller-side
   cache, not a change to `Budget`'s signature.
4. **The clipping-below-minimum policy will be questioned by users**, because a
   half-drawn widget reads as a bug rather than as a policy. If field feedback
   clusters on it, an application-level "too small" convention (not a framework
   one) is the likely answer.
5. **`Renderer.Resize` allocates two grids per resize event** and is unmeasured
   under a drag on a large terminal. Revisit if a real-terminal trace shows
   resize latency; the coalescing lever in §6 is the first thing to pull and it
   is already in the design.
6. **Nothing here has been run against a real terminal being resized.** Every
   claim in §5 and §6 is derived from code that exists and from ADR 0002/0003's
   measurements, not from an observed drag. The first honest test is a scripted
   resize sweep (say 80×24 → 200×60 in one-cell steps, asserting no stale cells
   and no panics at any intermediate size), and it should be written before the
   catalog is finished.