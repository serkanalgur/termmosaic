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
| [0006](0006-subbuffer-cell-access.md) | Sub-buffer cell access | Accepted | 2026-10-04 |
| [0007](0007-responsive-screens.md) | Responsive screen composition | Accepted | 2026-10-04 |
| [0008](0008-style-and-text.md) | Style, theme, and text | Accepted | 2026-10-04 |
| [0009](0009-command-and-keymap.md) | Commands and keymap | Accepted | 2026-10-05 |
| [0010](0010-mouse-routing.md) | Mouse routing | Accepted | 2026-10-05 |

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

- **0006 — Cell access: a row accessor, not a flat slice.** `Buffer.Cells()`
  is **removed**. It returns the flat backing slice, whose index
  `y*Width()+x` is silently wrong for a sub-buffer — the same unsoundness ADR
  0002 fixed once already, in `SubBuffer`, left open at a different door. It is
  replaced by `Row(y) []Cell`, which is correct on top-level buffers and views
  alike because the stride never leaves the `buffer` package. `RowBytes` becomes
  a `*Buffer` method that **panics on a sub-buffer** — settling ADR 0002's v1.0
  risk item 1b now rather than at v1.0 — and `diff.Frame` carries
  `*buffer.Buffer` instead of `[]buffer.Cell`, so the "diff only top-level
  buffers" rule is enforced by a type rather than by a doc comment. `Stride()` is
  deliberately not exported.

- **0007 — Responsive screens: a budget, not a reflow.** There are **no size
  classes and no framework breakpoints** — a size class is a lossy function of
  two numbers and a product decision in the wrong layer, and every threshold a
  widget needs is a local named constant beside its own `Draw`. What the
  framework shares is the *arithmetic*: `geometry.ClampCount(n, available)`,
  `geometry.Budget(regions, available)` with a four-value `Priority` scale, and
  an optional `termmosaic.Minimizable` interface declaring a widget's smallest
  meaningful size. Policy stays per widget; safety and arithmetic are shared.
  **The `Widget` interface is unchanged** — the space is already reachable from
  `Bounds()`, and adaptation is lazy and self-detecting (a widget re-derives
  anything size-derived when `Bounds()` differs), which beats a fourth mandatory
  method precisely because a widget cannot forget it. **Degenerate sizes are a
  decided contract: no panic, ever; clip, never blank** — below `MinSize()` a
  widget draws its minimum layout clipped, and 0×0 is a valid size that writes
  zero bytes. A resize always repaints the whole screen, because
  `buffer.Resize` discards the cells a partial diff would need; and drag-resize
  coalescing is **free**, because an app that calls `r.Resize` per event and
  lets the pacer decide when to paint already gets it. **Amended 2026-10-04:**
  the rect is not the only thing a size-derived cache depends on. A widget that
  caches column widths on `Bounds()` and is then handed `Header = true` renders
  the old layout *permanently* — nothing will produce a different rect to
  repair it. So `Invalidate()` now also means **drop every value the widget has
  cached**, and a widget exposing a setter for anything `Draw` reads must
  invalidate in that setter.

- **0008 — Style, theme, and text: one `Style` value, one `Span` type, one
  border vocabulary, and deliberately no theme in v1.** `buffer.Style` bundles
  fg/bg/attr and is passed **by value** — 12 bytes, three registers, no
  allocation, so it costs exactly what the three loose arguments it replaces cost
  and keeps ADR 0002's 0-allocs frame path. It lives in `buffer` because
  `geometry` cannot hold it without an import cycle (`buffer` imports
  `geometry`; `Style` needs `Colour`), which is the same cycle-exclusion
  reasoning that put ADR 0007's vocabulary in the leaf. Styled text goes through
  `Span` + `Buffer.SetSpans`, whose load-bearing rule is that **a wide glyph's
  continuation cell takes its owning span's style** — a mismatched one never
  compares equal and flickers that row forever. `Wrap`/`Truncate` **allocate
  and are therefore never called from `Draw`**; they are built on the
  size-change check ADR 0007 §3 already established. **There is no theme in
  v1**: widgets carry `Style` fields, the framework's defaults are the terminal's
  own colours plus named attribute styles, and a theme is triggered by the first
  role two widgets must share. Borders are one set of names —
  `BorderPlain`/`Rounded`/`Double`/`Thick`/`ASCII` — with the glyph tables in
  `buffer` and one `Block` as the only thing in the catalog that draws one. The
  ASCII rung is a single boolean passed to `BorderStyle.Glyphs(ascii)`. `NO_COLOR`
  and the 16-colour rung stay **encode-time only**; nothing in the widget path
  knows about them. `ansi.Style` becomes an alias of `buffer.Style`, removing a
  two-types-one-name collision already present in the tree.
  **Amended 2026-10-04:** `Wrap` now treats LF as a hard break (there is
  deliberately no `WrapLines` variant — a mode flag is a one-character slip and
  a second function leaves the broken one reachable); `Wrapped` gained
  `Ranges []LineRange` so editable text stops re-deriving line→rune offsets,
  on a **rune-index-into-the-input** basis counting zero-width runes; and four
  zero-allocation **range-clipped** writers (`SetSpansIn`, `SetStringIn`,
  `SetSpansCappedIn`, `SetSpansWindowIn`) replace the span writers two widget
  packages had each written for themselves, which also fixed a marker landing on
  a wide glyph's continuation cell and leaving an unpaired glyph flickering.

- **0009 — Commands and keymap: a named action, and a key as one way to reach
  it.** A new `keymap` package holding `Command`, `CommandID`, `Scope`
  (`Global`/`Screen`/`Focus` — a fixed three-value specificity order, with **no
  numeric priority**, because a knob an application sets wrong produces bindings
  nobody can predict), `Chord`, `Binding`, `Entry` and `Registry`. The
  load-bearing piece is `Chord`: one normalised gesture in which **`KeySpace` and
  `Rune ' '` are the same chord**, because terminals disagree and
  `form.activateKey` already works around that by hand — one place now states it,
  and it is a comparable 16-byte struct, so resolution is a map lookup at **0
  allocs**, pinned by `TestDispatchIsZeroAllocation` in ADR 0005's own shape.
  **`termmosaic.Widget` is unchanged**: the keymap sits *above* `Handle`, an
  unconsumed key falls through to the tree exactly as today, and the cost of the
  alternative — 24 signatures, plus `TextInput` losing undeclared runes and
  `List` losing its viewport-relative paging — is costed in the ADR itself.
  Widgets participate through **optional** `Commandable` and `Clickable`
  interfaces, the `Focusable`/`Minimizable` pattern, and **v0.2 requires them of
  zero catalog widgets**, which is recorded as a bad consequence rather than
  hidden. **`EventResize` and `EventPaste` never enter a command layer**, because
  re-expanding a paste to look for a command is ADR 0005 §4's failure repeated one
  layer up. Help cannot drift from the bindings: `Describe` is computed from the
  same tables `Dispatch` walks. A click becomes a command because the **widget
  under the pointer says so** — the registry holds no rectangles, since a global
  rect is a second source of truth for geometry that goes stale on the first
  reflow. The `Ctrl+K` palette is in scope but **not built here**: it is a
  `Dialog` + `Menu` + `TextInput` over `Describe`/`Invoke`/`Chords`, and the ADR
  states exactly what it needs from each rather than deciding their API. Deferred
  with triggers: multi-stroke sequences and leader keys (their pending-sequence
  state belongs to `input.Parser`, beside the escape deadline it already owns),
  command-line arguments, config-file persistence, release bindings, and
  drag-as-command. **`form.Binding` is left alone** — naming the new type
  `Binding` would recreate the two-types-one-name collision ADR 0008 removed from
  `ansi.Style`, so the new vocabulary takes the unused name and the reconciliation
  is deferred to the next `KeyHint` change.

- **0010 — Mouse routing: widgets hit-test themselves.** One sentence — **a
  widget handles a pointer event only if the pointer is inside its `Bounds()`** —
  and it covers every `Mouse` action, wheel included. The alternative considered
  and rejected is a framework routing helper (`RouteMouse(root, ev) Widget`, or
  an optional `Hittable` interface): it is new exported API against a `Widget`
  that ADR 0007 and ADR 0009 both freeze, it needs a tree walk that `Widget`
  cannot express because there is no `Children()`, and it can only answer "which
  rect" where a widget can answer "which cell means what". So the fix is three
  call sites and one shared helper — `optionList.wheelDelta` gained a
  `buffer.Rect` and a `Contains`, because all three widgets that mishandled the
  wheel got it the same way and a fix applied three times is a fix applied twice.
  The defect was real and not cosmetic: `form.Tabs`, first in `examples/markets`'
  focus ring, consumed **every wheel notch in the application** whether or not
  the pointer was over it, so the table under the pointer never scrolled and the
  example grew a thirty-line application-level workaround. This is the decision
  that gives ADR 0009 §6's "hit-testing is the one thing widgets are genuinely
  better at than a global registry" its teeth in the shipped catalog rather than
  only in the design. **Two exemptions are stated rather than left implicit**: a
  release ends a drag wherever the pointer is (a gesture that can only be
  finished over the widget strands the user), and a drag continues outside
  `Bounds` once a press has claimed it — *the press is the claim, the drag is the
  continuation*, which is the sentence that stops a future reader applying the
  bounds rule to a drag. `TextInput` and `TextArea` **decline** the wheel by
  decision, not oversight: wheel-to-scroll in a `TextArea` is a plausible
  *feature*, and adding one under a routing ADR is a feature nobody reviewed as a
  feature. `split.Split` needed no change, and saying so is a result.

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

Decision 6 follows the same pattern for the same reason: it is a narrow API
question about an existing data structure, made on the existing
`TestSubBufferRowsAreStrided` evidence rather than on a new measurement. Its one
performance claim — that the diff's row hoisting leaves ADR 0002's figures
intact — is a claim a benchmark must re-confirm, and ADR 0006 says so rather
than asserting a number it did not measure.

Decision 7 follows the same pattern and says so in its own risks section: every
cost it quotes about a drag-resize is **derived from existing code and from
ADR 0002/0003's measurements**, not from an observed resize. It has never run
against a real terminal being dragged, and it names the scripted resize sweep
that should be written before the catalog is finished.

Decision 8 follows the same pattern for a partly-different reason: the `Style`
size question was settled by reading Go's register ABI against ADR 0002's
existing measurements rather than by a new benchmark, and its wide-character and
ASCII-rung claims are **inherited** from `buffer/width.go` and `term/caps.go`,
both of which already document their own limits. Its risk section names which of
its claims are unbenchmarked.

Decision 9 follows the same pattern for the same reason: it is an API-shape
decision about where a new layer sits above an existing one, made on the cost of
the alternative (24 `Handle` signatures, plus what `TextInput` and `List` would
lose) and on the published shape of OpenTUI's `@opentui/keymap`, rather than on
a measurement of ours. Its two performance claims — 0 allocs on dispatch and a
16-byte `Chord` — are **specified and pinned by named tests**, not measured
today, and its `Chord` normalisation rules have never been run against a real
terminal, which is ADR 0005's already-recorded risk extended one layer up.

Decision 10 follows the same pattern for the same reason, and its evidence is the
sharpest of the ten because it is a **defect that was reproduced rather than
argued**: the wheel was sent to each of the twenty-four widgets at a pointer
outside its `Bounds`, and three answered `true`. Its behavioural claim is
non-vacuous by construction — the `Contains` check was removed from
`optionList.wheelDelta` and the new tests were confirmed to fail for all three
widgets before it was restored — and its risk section is explicit that the rule
itself is prose plus tests rather than a type, which is the same position ADR
0007 §"Risks" item 2 takes for the rect-keyed caching rule. It has not met a real
terminal's mouse reporting, which is ADR 0005's risk inherited unchanged.

**Caveat worth repeating:** OpenTUI is a Zig core with TypeScript FFI bindings.
Its numbers do not transfer to Go, and ADR 0002 exists precisely because we
checked that assumption instead of inheriting it.

## Still open

These are tracked in [STATUS.md](../STATUS.md) and are **not** decided:

- Colour model and degradation ladder — **built and working, but PROPOSED rather
  than decided**: the redmean quantiser to 256 and 16 rungs has never been
  checked for perceptual acceptability. `buffer.Quantiser` is the drop-in hook.
- Kitty **graphics** in v1 (the kitty *keyboard* protocol is decided by ADR 0005)

The headless backend's "v1 vs v0.5" question is **closed** — ADR 0001 settled it
in favour of v1, and `headless.MemorySink` exposes the cell buffer rather than
only recorded bytes. Its remaining open sub-question is the assertion surface,
tracked in [STATUS.md](../STATUS.md).

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