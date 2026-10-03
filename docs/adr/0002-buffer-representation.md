# 0002 — Cell buffer representation

- **Status:** Accepted
- **Date:** 2026-10-03
- **Decides:** [STATUS.md](../STATUS.md) — Core architecture / Cell buffer representation
- **Depends on:** [ADR 0001](0001-backend-strategy.md)
- **Supersedes:** the "Option AoS / SoA" section of the former ARCHITECTURE.md
  "Decision 2", **including its stated leaning toward SoA.**

## Context

The cell buffer is the single most performance-critical structure in the
framework. Everything in the widget catalog — a 100k-row virtualized table, a
live sparkline, a full-screen pager — reads and writes it every frame.

The claim that motivated our previous leaning toward struct-of-arrays came from
OpenTUI: its buffer is SoA (`char: []u32, fg: []RGBA, bg: []RGBA, attributes:
[]u32`), its tier-1 diff compares whole-row SoA slices with `mem.eql` and skips
the row if all four match, and that row-level skip is credited as the reason
its renderer is fast.

**That claim is a Zig claim and it does not transfer to Go.** Go has no
`mem.eql`; it has `bytes.Equal`, which requires byte-addressable contiguous
memory. That single difference changes the answer. We tested it.

## Method

Scratch module outside the repository: `/tmp/tm-bench`. Go 1.23.0,
darwin/arm64 (Apple M1). Four representations, all holding **identical data**
from one shared scene function, all verified to emit **byte-identical diff
output**:

| Variant | Layout | Row compare |
|---|---|---|
| `SoA_4Planes` | `Chars []uint32`, `FG []Colour`, `BG []Colour`, `Attr []Attr` | 4 × `bytes.Equal` on plane views |
| `SoA_RunePlanes` | `Chars []rune`, `FG`, `BG`, `Attr` | 4 × typed Go loops |
| `AoS_Packed` | `[]CellPacked` — `rune, Colour, Colour, Attr, [2]byte` pad | 1 × `bytes.Equal` on a struct-as-bytes view |
| `AoS_Natural` | `[]CellNatural` — `rune, Colour, Colour, Attr`, tail-padded | per-cell struct loop (byte compare unsound, see below) |

Scene: synthetic 200×60 = 12,000 cells, 99% static chrome (panel borders,
static filler), one progress-bar row and three numeric readouts changing per
frame. **42 / 12,000 cells changed (0.35%); 4 / 60 rows dirty (6.7%).**

Command:

```
cd /tmp/tm-bench
go test -run '^$' -bench . -benchmem -benchtime=20000x -count=5
```

## Results

### Bytes written (representation-independent)

| Pass | Bytes |
|---|---|
| Full repaint (AoS) | 23,240 |
| Full repaint (SoA) | 23,240 |
| Two-tier diff — **every** variant | **107** |

**217× reduction**, and all four variants emit identical bytes. The test suite
asserts this equality rather than assuming it. Bytes are decided by the diff
algorithm, not the memory layout.

### ns/op, two-tier diff (default scene, median of 5)

| Representation | ns/op | allocs/op |
|---|---|---|
| **AoS_Packed** | **7,133** | 0 |
| SoA_4Planes | 8,003 | 0 |
| SoA_RunePlanes | 23,643 | 0 |
| AoS_Natural (padded) | 21,643 | 0 |

### ns/op, row-skip in isolation, all 60 rows (median of 5)

| Row compare | ns/op |
|---|---|
| AoS_Packed — 1 × `bytes.Equal` | 5,373 |
| SoA_4Planes — 4 × `bytes.Equal` | 5,128 |
| SoA_RunePlanes — typed loops | 20,777 |
| AoS_Natural — struct loop | 14,241 |

**The row-level skip is a statistical tie between packed AoS and SoA** (5,373
vs 5,128 ns/op — overlapping ranges across 5 runs). The win is real; the
*attribution to SoA* is not. Both are just `bytes.Equal` over ~3,200
contiguous, padding-free bytes.

### ns/op, row compare when **every** row differs (skip never fires)

| Row compare | ns/op |
|---|---|
| **AoS_Packed** — one 3,200-byte memcmp | **147.2** |
| SoA_4Planes — four 800-byte memcmps | 636.7 |
| AoS_Natural — 200 struct compares | 4,642 |

Packed AoS is **4.4× faster than SoA** here, because it issues one wide memcmp
instead of four narrower ones. SoA loses precisely in the case where the skip
fails.

### Widget draw path — writing all 12,000 cells (median of 5)

| Representation | ns/op |
|---|---|
| **AoS_Packed** | **81,062** |
| SoA_4Planes | 85,639 |
| AoS_Natural | 88,761 |

All 0 allocs/op. Roughly 6.8 ns per cell either way; the layouts are within
noise of each other for sequential writes, as expected once each touches every
byte of its working set.

### All-dynamic adversarial scene (12,000 / 12,000 cells change)

| Representation | ns/op (range over 5 runs) |
|---|---|
| AoS_Packed | ~133,000 (123,445–149,426) |
| SoA_4Planes | ~126,000–240,000, **bimodal and unstable** |

Bytes: 23,162 vs a 23,240-byte full repaint — **1.00×**. The two-tier diff never
costs more than a full repaint, which is the property that matters for the
frame budget. The SoA numbers here are visibly bimodal across runs; we do not
consider that measurement reliable and draw no conclusion from it.

### The Go-specific hazard: padding makes byte-compare unsound

`CellNatural` (`rune, Colour, Colour, Attr`) is also 16 bytes — but `Attr` is
followed by 2 bytes of **tail padding whose contents Go does not define**. We
demonstrated the consequence (`TestPaddingMakesByteCompareUnsound`): two rows
with every meaningful field identical, with `0xFF 0xFF` scribbled into the tail
padding of one row only:

```
per-cell struct loop says equal: true   (correct)
byte-wise row compare says equal: false (WRONG — false dirty row)
```

So the natural Go struct **cannot** use a byte-wise row skip. This is the real
Go-specific cost in this decision, and it is *not* the one we expected. Note
`SoA_RunePlanes` hits the same wall from the other side: `rune` is not
byte-addressable, so an idiomatic `[]rune` char plane forfeits `bytes.Equal`
entirely and the row skip degrades to a typed loop at **4× the cost** (20,777
ns/op). OpenTUI's `[]u32` char plane is deliberate; in Go, the equivalent means
declaring the plane as `[]uint32`, not `[]rune`.

## Options considered

### SoA with `[]rune` planes

The idiomatic Go spelling of the OpenTUI design. **Rejected on measurement:**
20,777 ns/op for the row skip versus 5,128 for byte-addressable planes, and
23,643 ns/op for the full diff versus 7,133 for packed AoS. It pays the SoA
complexity *and* loses the benefit.

### SoA with byte-addressable planes (`[]uint32`)

The faithful port of OpenTUI. **Rejected:** the row skip ties with packed AoS
(5,128 vs 5,373), the full diff is ~12% slower (8,003 vs 7,133), and it is
**4.4× slower** when all rows are dirty (636.7 vs 147.2). It buys four
independent write streams and four index computations per cell in exchange for
performance that is at best equal. Note also that SoA's genuine theoretical
advantage — comparing one plane and skipping the rest — does not help here,
because a style change alone dirties a row and forces all four compares anyway.

### AoS with a padded struct ✅

**Chosen.**

```go
// Cell is exactly 16 bytes with no interior or tail padding, so a row can be
// compared with bytes.Equal over its backing memory.
//
// INVARIANT: any change to these fields must preserve sizeof(Cell) == 16 and
// keep every byte meaningful. TestPackedCellHasNoPaddingHazard enforces it.
type Cell struct {
    Ch   rune   // int32
    FG   Colour // uint32, 0x00RRGGBB
    BG   Colour // uint32
    Attr Attr   // uint16 bitmask
    _    [2]byte
}
```

## Decision

**Array-of-structs, with a 16-byte padding-free `Cell` and a byte-wise row skip.**

- The buffer is `[]Cell`, width × height, row-major, plus a mirror `[]Cell`
  holding the previous frame. The tier-1 row skip is
  `bytes.Equal(asBytes(cur[y]), asBytes(prev[y]))`.
- **`Cell` is a plain comparable struct.** Widget authors write
  `buf.Set(x, y, 'x', fg, bg, attr)` and compare cells with `==`. This is the
  API ergonomics argument, and it turns out to be worth more than the
  performance argument: SoA forces every widget author to touch four parallel
  slices and keep indices consistent, which is the main source of bugs in
  widget code and directly undermines the "the widget catalog is the product"
  thesis.
- `Colour` stays `uint32`, `Attr` stays `uint16`. If we later need underline
  colours or hyperlinks (tcell's `Style` carries `url`/`urlId`), we widen
  `Colour` to `uint64` and the cell becomes 24 bytes — still padding-free if
  ordered correctly, and still byte-comparable. Planned, not in v1.

## Consequences

**Good**

- Fastest measured diff of any variant on the realistic scene (7,133 ns/op),
  and 4.4× faster than SoA in the all-rows-dirty case.
- Simplest possible widget-authoring API: one struct, `==` comparison, no index
  bookkeeping, no risk of a widget writing a char plane without updating the
  colour plane.
- One cache line touched per cell on the per-cell path, versus four separate
  streams for SoA — which is why SoA loses on the dirty-row path.
- 0 allocs/op everywhere measured.

**Bad — stated plainly**

- **The padding invariant is a footgun.** If someone adds a field to `Cell`
  and forgets the pad, `bytes.Equal` silently compares garbage. We accept this
  and mitigate with a mandatory guard test
  (`TestPackedCellHasNoPaddingHazard`) that fails on any size change, plus a
  document comment stating the invariant. SoA has no equivalent hazard; that is
  a genuine robustness advantage for SoA that we are trading away.
- **Row-viewing the buffer requires `unsafe.Slice`** to reinterpret `[]Cell`
  as `[]byte`. This is sound *only* while the no-padding invariant holds, and it
  is confined to one function in the diff so the blast radius is small. We
  should keep it in exactly one place.
- **Widening for hyperlinks costs 50% more memory per cell** (16 → 24 bytes).
  Still 288 KB for a 200×60 screen, irrelevant at v1 sizes.
- **The previous leaning in ARCHITECTURE.md was wrong.** We are recording that
  explicitly rather than quietly switching, because the reasoning that produced
  it — "SoA enables cheap row skips, therefore SoA" — is a plausible-sounding
  inference that we would otherwise repeat in the next design review.

## Rejected alternatives, specifically

- **Struct-of-arrays (the prior leaning)** — measured at 8,003 ns/op for the
  diff versus 7,133 for packed AoS; 636.7 ns/op for an all-dirty row compare
  versus 147.2; and it makes the widget API materially worse. The row-skip
  advantage attributed to SoA is a tie in Go. Rejected on measurement.
- **SoA with `[]rune`** — 4× slower row skip than byte-addressable planes
  because `rune` is not byte-addressable. Strictly the worst of both worlds.
- **AoS with the natural padded struct** — cannot byte-compare rows; 14,241
  ns/op row skip and 21,643 ns/op diff, i.e. the same as SoA while also
  carrying SoA's index bookkeeping. Rejected.
- **tcell's `CellBuffer`** — `{currStr, lastStr string; currStyle, lastStyle
  Style; width int; lock bool}` with two strings per `Style`. Four string
  headers per cell and heap-allocated grapheme bytes behind them; grapheme
  correctness bought with a write-time allocation. Rejected; see
  [ADR 0001](0001-backend-strategy.md).

## Risks to revisit at v1.0

1. **The no-padding invariant.** If the cell ever needs a field that cannot be
   packed into 16 bytes, revisit whether SoA's freedom from this hazard is
   worth the measured 12% and the API cost. Likely trigger: complex text
   shaping or per-cell inline styling demanding more than 16 bytes.
2. **Wide characters.** A CJK or emoji glyph occupies two cells. Both are
   written, and the continuation cell must compare equal across frames or it
   will flicker. The row skip is unaffected. Needs its own ADR when
   internationalization is scoped — currently unaddressed and unmeasured.
3. **Benchmarks are single-machine, darwin/arm64.** The AoS-vs-SoA gap is small
   in absolute terms (7.1 vs 8.0 µs on a 16 ms budget) and could invert on
   other microarchitectures, though the 4.4× all-dirty gap is large enough to be
   real. Re-run before treating these numbers as settled at v1.0.