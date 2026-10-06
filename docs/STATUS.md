# Status

This document is the honest state of TermMosaic. It is updated as decisions are
made. If something here is stale, that is a bug — please open an issue.

**Project stage: v1.0.0, released 2026-10-06 — the first release with a
stability promise.** The public API is frozen at that tag and Semantic
Versioning applies from it, **with one documented exception:
`widgets/widgettest` is explicitly excluded from that promise** — decided
2026-10-06, see "Two stability hazards" below. It was listed here as an open
question at release time; that question is now closed. The release did not
settle everything else; what is still open:

- The macOS test leg still runs; a further reduction was discussed and not
  done. The Windows test leg was dropped by PR #26 (2026-10-06), so the
  required checks are now 11 — see the CI criterion under "Definition of
  'usable library'".
- `deleteBranchOnMerge` is false at the repository level; no decision has been
  made about changing it.
- ADR 0003 contains a third-party technical reference in a rationale; whether
  to cut it is the maintainer's open call. It is not edited here.

## Decision states

| Marker | Meaning |
|---|---|
| **DECIDED** | Agreed, will not be reopened without a strong reason. |
| **PROPOSED** | A concrete proposal awaiting review. Expect this to move. |
| **OPEN** | Deliberately undecided. Options are being explored. |
| **DEFERRED** | Known to be needed, intentionally postponed with a stated trigger. |

## Language and packaging

| Item | State | Notes |
|---|---|---|
| Implementation language | **DECIDED** — Go 1.23+ | Single static binary, trivial cross-compilation, low CI cost. |
| Module path | **DECIDED** — `github.com/serkanalgur/termmosaic` | |
| License | **DECIDED** — MIT | |
| Minimum Go version | **DECIDED** — 1.23 | Matches current stable. |

## Core architecture

| Item | State | Notes |
|---|---|---|
| Backend strategy (own vs wrapped vs pluggable) | **DECIDED** — pluggable, own the core | Two narrow interfaces (`Terminal`, `Sink`), an impl on `golang.org/x/term` plus `x/sys` directly, headless memory sink. Wraps no terminal library: tcell's flush measured 280,814 ns/op vs our 7,133 ns/op on a one-row-dirty frame. [ADR 0001](adr/0001-backend-strategy.md) |
| Cell buffer representation (AoS vs SoA) | **DECIDED** — AoS, padding-free 16-byte `Cell` | **Overturned the prior SoA leaning.** Measured: packed-AoS row skip ties with SoA (5,373 vs 5,128 ns/op) and is 4.4× faster when all rows are dirty (147.2 vs 636.7). OpenTUI's advantage is Zig's `mem.eql`, not SoA. [ADR 0002](adr/0002-buffer-representation.md) |
| Renderer mode (immediate vs retained vs hybrid) | **DECIDED** — hybrid | Retained widget tree invalidated by rectangle; widgets describe themselves on demand. No reconciler, no Elm loop. [ADR 0003](adr/0003-renderer-mode.md) |
| Layout engine (own constraints vs flexbox) | **DECIDED** — constraint-based, own solver | `Length`/`Min`/`Max`/`Percentage`/`Ratio`/`Fill`. Yoga rejected: cgo breaks `CGO_ENABLED=0` cross-compilation. [ADR 0004](adr/0004-layout-engine.md) |
| Input decoding (where the parser lives, what it decodes) | **DECIDED** — a pure `Decode` under a resumable `Parser` in a new `input` package | Kitty keyboard **in** (progressive enhancement, `disambiguate` only); paste **in** and always **one `EventPaste` carrying the whole payload**; mouse decoding **in** (SGR 1006 / urxvt 1015 / X10) but **capture off by default**; focus decoding **in**, reporting off by default; **IME scoped out and deferred**, with `EventCompose` reserved. [ADR 0005](adr/0005-input-decoding.md) |
| Cell access on sub-buffers (`Cells()`, `RowBytes`) | **DECIDED** — `Row(y) []Cell` replaces `Cells()`; `RowBytes` is a `*Buffer` method that panics on a view | The flat slice `Cells()` returns is silently wrong for a sub-buffer. The stride never leaves `buffer`. Closes ADR 0002's v1.0 risk 1b now. [ADR 0006](adr/0006-subbuffer-cell-access.md) |
| Responsive screen composition | **DECIDED** — a **budget, not a reflow**; no size classes, no framework breakpoints | Framework shares the arithmetic (`geometry.ClampCount`, `geometry.Budget` + `Priority`, optional `termmosaic.Minimizable`); policy stays per widget. **`Widget` is unchanged.** Degenerate sizes are a contract: **no panic ever, clip never blank**; a resize always repaints the whole screen because `buffer.Resize` discards the cells. [ADR 0007](adr/0007-responsive-screens.md) |
| Color model and degradation ladder | **DECIDED — 2026-10-06, on measurement** — Lab-space (CIEDE2000) selection through the existing `buffer.Quantiser` hook | Built and working: `Colour` is truecolor/named-16/256 with a `ColourDepth` rung, plus `NO_COLOR`. **The validation was performed, and it failed** — that is why this row is DECIDED rather than still PROPOSED. PR #18's CIEDE2000 audit (`buffer/colour_quantiser_perceptual_test.go`) found the "redmean" weights inert: `rmean/256` and `(255-rmean)/256` divide to zero in `uint8` arithmetic, so the formula was fixed-weight `2*dr²+4*dg²+2*db²` in gamma-space RGB. Measured selection error: **256 rung max 21.201, 19.35% of the lattice above the JND; 16 rung max 36.821, 37.50% above** — with `markets.down` collapsing to grey and colliding with `markets.flat`. PR #19 **replaced** the quantiser through that hook (`redmean` is deleted; selection is an exhaustive CIEDE2000 search over the rung palettes behind a per-colour memo), and the audit's own numbers now read **selection error 0.000 on both rungs, 0 of 281,216 colour-rungs regressed, frame path 224.8 → 6.6 ns/op at 0 allocs**. Every selection threshold in the audit is pinned at `0`, so a non-zero measurement means the metric, the palettes or the selection wiring changed without a re-audit. **Decided because it was measured, not asserted**: redmean is rejected on recorded numbers rather than on preference, and the record of what was wrong is kept in the audit's header rather than erased. Reopening this after v1.0.0 is a release-defining re-audit, not a tweak — see gate item 6. |
| Theme and styling system | **DECIDED** — **no theme in v1**; widgets carry `Style` fields, framework defaults are the terminal's own colours plus named attribute styles | One `buffer.Style` value (fg/bg/attr, by value, 12 bytes, 0 allocs) replaces the loose-argument write API; `ansi.Style` becomes an alias of it. Trigger for a theme: the first role two widgets must share. [ADR 0008](adr/0008-style-and-text.md) |
| Text and span rendering | **DECIDED** — `Span` + `Buffer.SetSpans`, parsed once, wrapped outside `Draw` | A wide glyph's continuation cell takes its **owning span's** style or the row flickers forever. `Wrap`/`Truncate` allocate and are banned from `Draw`. Borders and titles have one vocabulary (`BorderPlain`/`Rounded`/`Double`/`Thick`/`ASCII`, one `Block`). [ADR 0008](adr/0008-style-and-text.md) |
| Commands and keymap (where the command layer sits, and whether `Widget.Handle` changes) | **DECIDED and SHIPPED** — a new `keymap` package sitting **above** `Widget.Handle`; the `Widget` interface is **unchanged** | A named action and a key that reaches it are different things. One normalised `Chord` (`KeySpace` and `Rune ' '` are one chord) is a comparable 16-byte struct, so resolution is a map lookup at **0 allocs** — a property ADR 0009 specifies and **is pinned today**: `TestDispatchIsZeroAllocation` and `TestChordIsSixteenBytes` exist in `keymap/` and pass. **Shipped in v0.6.0** as `keymap/registry.go`, `command.go`, `chord.go`, `scope.go`, `participation.go` and `describe.go`. Resolution is **focus > screen > global with no numeric priority**, and a user override wins only **within its own scope** — so a dialog's `Esc` cannot be stolen. Widgets join through **optional** `Commandable`/`Clickable` interfaces (the `Focusable` pattern); **v0.6 requires them of zero catalog widgets** and ships with none, which is deliberate. `EventResize` and `EventPaste` never enter a command layer. Help is `Describe` (one row per **chord**, for a palette and a help screen) or `DescribeGrouped` (one row per **command**, for a one-line `KeyHint`), computed from the same tables `Dispatch` walks, so it cannot drift; `examples/hello` is the first consumer, and it renders **both** its pinned hint line and its `?` overlay from the registry. A click is a command because the **widget under the pointer says so** — the registry holds no rectangles. The `Ctrl+K` palette is in scope, built on `Menu`+`Dialog`+`TextInput`, and **is not built**; new behaviour in `Menu`/`Dialog`, so a v1.1.0 minor. `Event` is untouched: a command is resolved *from* an event, never carried inside one, so no payload is added and the `unsafe.Sizeof(Event{})` guard is unaffected. Deferred with triggers: multi-stroke/leader sequences, command-line args, config persistence, release bindings, drag-as-command. [ADR 0009](adr/0009-command-and-keymap.md) |
| Mouse routing (who gets a mouse event) | **DECIDED** — **widgets hit-test themselves**; no framework routing layer | **A widget handles a pointer event only if the pointer is inside its `Bounds()`**, and declining returns `false` so the event reaches what is beneath. Covers every `Mouse` action, wheel included. **`Widget` is unchanged and no exported routing API is added** — the alternative, a `RouteMouse`-style helper or an optional `Hittable` interface, needs new API against a frozen interface, needs a tree walk `Widget` cannot express (there is no `Children()`), and can only answer "which rect" where a widget answers "which cell means what". Fix: `optionList.wheelDelta` gained a `buffer.Rect` and a `Contains`, so `Tabs`, `Select` and `Radio` — which all got the wheel-before-bounds mistake identically — are correct by construction. Two exemptions stated explicitly: a **release** ends a drag wherever the pointer is, and a **drag** continues outside `Bounds` once a press claimed it. `TextInput`/`TextArea` decline the wheel **by decision**, not oversight. This is what gives [ADR 0009](adr/0009-command-and-keymap.md) §6's "hit-testing is the one thing widgets are genuinely better at" its teeth in the shipped catalog. [ADR 0010](adr/0010-mouse-routing.md) |
| `docs/ARCHITECTURE.md` | **DECIDED** — a short orientation document, not a summary | Reduced to 105 lines at the v0.1.0 release gate. It had grown to 251 lines duplicating ADR reasoning, its decision numbering (5=colour, 6=theme, 7=input) did not match the ADR set, and it still called the colour model OPEN after this table moved it to PROPOSED. It now states what the pieces are, how they fit, and links each ADR — no duplicated reasoning — and preserves the **Non-goals** section verbatim, which is not duplicated anywhere else. |
| Documentation site | **PROPOSED** — Hugo + Pagefind on GitHub Pages; captures generated in Go from `MemorySink` cells, not screenshots | No browser TTY exists, so the only truthful picture of a widget is the cell grid the renderer produced — which is what `widgets/widgettest` already builds and what the golden tests assert on, so the docs cannot drift from behaviour. **The capture half is built** — `internal/docsgen` (~2,242 lines) and `cmd/capture` (~228 lines), 2,470 lines together, with a one-entry-per-widget registry whose `Entries()` returns
exactly 24 — `Menu` and `Dialog` are in it — plus plain-text *and* HTML output, a sorted `manifest.json`, `index.json`, and a `-check` mode that fails on a byte difference. **The Hugo/Pagefind site itself is not built here** — the plan and captures live in this repository, and the built site serves at [serkanalgur.github.io/termmosaic.github.io](https://serkanalgur.github.io/termmosaic.github.io/). A live WASM playground is rejected: `docs/ARCHITECTURE.md` lists "no WASM build" as a written non-goal and `term/terminal_windows.go` is a stub, so there is no seam to port. Plan, page tree, per-widget template and effort: [docs/SITE-PLAN.md](SITE-PLAN.md). |

### Decisions

The core architecture rows above that carry **DECIDED** — backend strategy, cell
representation, renderer mode, layout engine, input decoding, sub-buffer cell
access, responsive composition, style/theme/text, commands and the keymap, and
mouse routing —
were decided on 2026-10-03 (the first four), 2026-10-04 (input decoding,
sub-buffer cell access, responsive composition, style/theme/text) and 2026-10-05
(commands and keymap), and are recorded in full, with rejected alternatives, in
[docs/adr/](adr/README.md).

One of them was DECIDED with nothing built under it: `keymap`, and it is now
**shipped in v0.6.0** — `keymap/` exists and its named tests pass. The colour
model was the other — **PROPOSED** when this paragraph was written, and now
**DECIDED on measurement** (the row above: the audit ran, found a defect, and
the quantiser was replaced). The distinction still matters when
reading this table: DECIDED means "will not be reopened", not "shipped".

Decisions 1 and 2 were made **empirically** — a scratch benchmark module was
built outside the repo and measured on darwin/arm64 (Apple M1). The headline
result: a two-tier diff on a 200×60 scene that is 99% static chrome writes
**~141× fewer bytes than a full repaint** (141 vs 19,979 bytes as measured in
the committed implementation), at **~7,133 ns/op** with **0 allocs/op**,
comfortably inside a 16 ms frame budget.

Two findings contradict earlier assumptions and are recorded rather than
quietly dropped: **OpenTUI's struct-of-arrays advantage does not transfer from
Zig to Go**, and **`tcell` is not usable as the buffer layer** — its flush walks
every cell per frame and its headless backend cannot expose the cell buffer.

Raw benchmark output is quoted inline in ADR 0001 and ADR 0002, including the
workloads that did not produce a clean result. Decisions 3, 4 and 5 were made on
API-surface, testability and dependency grounds and involve no measurements.
ADR 0005 says so explicitly: input decoding is I/O-bound, and the one number
that matters — 0 allocations on the key path — is a property the ADR specifies
and a test must pin, not a measurement taken today. ADR 0007 makes the same
disclosure: its drag-resize costs are derived from existing code and from
ADR 0002/0003's measurements, and no resize has ever been observed against a
real terminal.

### Amendments

Four accepted ADRs were amended on 2026-10-04, after the core implementation
exposed claims that no longer described the code. Each amendment notes the date
and reason in the ADR's header; the original reasoning is preserved in place
rather than rewritten.

**A fifth amendment, also on 2026-10-04, closed ADR 0008's risk 5** — "the
wide-glyph interaction is implemented but unbenchmarked" — by benchmarking it, on
the diff, the cell writers, `Wrap` and a whole rendered frame. The amendment
records the measurements, states that the wide paths did not regress, and records
one defect the benchmark surfaced and this release deliberately does not fix: the
diff's cursor-run suppression assumes one cell per rune, so every wide glyph is
preceded by a cursor-position escape. It is the only change made to any ADR for
v0.1.0.

- **ADR 0002 — the byte-compare soundness condition was half-stated.** Padding
  is one precondition; contiguity of the compared range is the other, and it is
  a property of the call site, not of `Cell`. A `SubBuffer` that returned a
  tightly packed slice violated it while `Cell` was still exactly 16 bytes.
  Now stated as two preconditions, with `Buffer.stride` as the structural fix.
- **ADR 0002 — the byte-count figure.** "107 bytes" was an artifact of a
  single-byte-glyph benchmark scene. The claim is the **ratio (~141×)**, not the
  absolute number.
- **ADR 0004 — `Fill` is not order-sensitive.** The original ADR recorded this
  as its top usability sharp edge; the committed solver resolves all fixed
  constraints before any `Fill`, and a test pins that as a guarantee. The ADR
  now documents order-insensitivity as a deliberate divergence from tmux.
- **ADR 0002 — the `RowBytes` receiver question is settled.** Risk item 1b asked
  whether to make `RowBytes` structurally refuse non-top-level buffers by moving
  it behind a `*Buffer` receiver. [ADR 0006](adr/0006-subbuffer-cell-access.md)
  answers **yes**, and removes the v1.0 deferral: `RowBytes` becomes
  `(*Buffer).RowBytes(y, x0, n)`, panics on a sub-buffer, and `diff.Frame`
  carries `*buffer.Buffer` so the old call sites do not compile.

## Widget catalog

**24 widgets, built and tested.** (`buffer.Buffer` is deliberately not counted:
it has `Invalidate()` but no `Bounds`/`Draw`/`Handle`, so it is not a `Widget`
— it is what widgets draw into.) Flat per-frame cost is the
claim that matters and it is asserted: List renders 10k items in 13,320 ns and
100k in 14,242 — 7% for ten times the data — and Table likewise, both at zero
allocations.

The catalog is enumerated here in full, all 24, so the count above is checkable
against this list rather than against a claim:

**Core (4)** — `Block`, `Text`, `Paragraph`, `Split`. Plus the non-widget
primitives they build on: `buffer.Buffer`, `layout`, `buffer.Span`.
`Block` is the only thing in the catalog that draws a border or a title.

**Forms (9)** — `TextInput`, `TextArea`, `Select`, `Checkbox`, `Radio`, `Toggle`,
`Tabs`, `Button`, `KeyHint`. A `Form` container was **not** built; ADR 0004's
solver plus `layout` covers composition, and a `Form` type would have been a
second way to do the same thing.

**Data (4)** — `List`, `Table`, `Tree`, `Pager`, plus the `virtual/` engine they
share (flat cost from 10 to 1,000,000 items, asserted).

**Visualization (5)** — `ProgressBar`, `Gauge`, `Meter`, `Sparkline`, `BarChart`.
`Sparkline` and `Gauge` use Braille for sub-cell resolution; `Gauge` degrades
to a bar when the rect or terminal cannot hold a dial. These five are the gap in
every comparable framework — OpenTUI has 13.4k stars and ships none of them.

**Composites (2)** — `Menu`, `Dialog`. Both new in v0.2.0. `Menu` is a navigable
tree with submenus to arbitrary depth; `Dialog` is a modal with `VariantInfo`,
`VariantConfirm` and `VariantChoice`. `Dialog` is the largest widget in the
repository. Both are containers: they compose other widgets and each has its own
`Draw`, so both count toward the 24.

`Menu` and `Dialog` were the two entries this list used to omit, which is how a
list of 22 sat under a claim of 24. The count was right; the enumeration was not.

**Responsiveness is decided**, in [ADR 0007](adr/0007-responsive-screens.md),
because three coders build independent widget sets against a shared vocabulary.
This closes the question the catalog previously left open — *should the framework define
breakpoints or size classes?* — with a **no**: a size class is a lossy function
of two numbers and a product decision in the wrong layer, so each widget's
threshold is a local named constant beside its own `Draw`. What is shared is
`geometry.ClampCount`, `geometry.Budget` with the four-value `Priority` scale,
and the optional `termmosaic.Minimizable` interface. Two catalog-wide rules
inherit directly and are non-negotiable: **a widget's available space is
`Bounds()`, never `buf.Size()`**, and **a widget repaints its whole `Bounds()`
before drawing content into it**, or shrinking leaves stale cells. `Widget` is
unchanged.

**Styling and text are also decided before implementation**, in
[ADR 0008](adr/0008-style-and-text.md), on the same reasoning and against the
same collision: every one of the thirty widgets will style text and draw a
border. One `buffer.Style` value, one `Span` type, one border glyph vocabulary
owned by `buffer`, and one `Block` as the only thing that draws a border or a
title. There is **no theme in v1** — widgets carry `Style` fields and the
framework's defaults are the terminal's own colours plus named attribute styles.
`NO_COLOR` and the 16-colour rung stay encode-time only, so no widget path
consults them.

## Release gate for v0.7.0 — what closed, and what did not

**v0.7.0**, a minor, and the reason is one example: **`examples/search`**, a
search-and-results TUI on real Wikipedia data with **no API key**, plus a
`--offline` flag that runs the whole screen on a transcribed 2026-10-05 capture
so tests and CI never touch the network. A minor rather than a patch because the
example is additive but the golden corpus and the public screenshots are
observable behaviour, and because it is the first shipped consumer of a shape
`ADR 0009` reasoned about but had never exercised. Nine golden files, 74 tests.

It is **not** "another demo", and the reason is worth stating before anything
else: **it is the first example with a focusable widget in a focus ring.**
`form.TextInput` and `data.Table`, moved with `Tab`/`Backtab`, drawn as a visible
ring. Two of four examples now dispatch through a registry rather than their own
`switch`.

**Closed at this gate.**

- **The context-dependence question has an answer, and it is
  `keymap.Command.Enabled`, not `ScopeFocus`.** No binding in `examples/search`
  is focus-scoped, and the reason is specific rather than stylistic: no catalog
  widget implements `keymap.Commandable`, so a focus-scoped binding would mean
  the **application** declaring keys on a widget's behalf with an owner it chose —
  exactly what `Commandable` exists to stop being necessary for. `Enabled` false
  makes `dispatchChord` skip the command and keep looking, so the event is
  reported unconsumed and falls through to the tree, where the focused widget's
  own key contract gets it. `TestNoScreenBindingStealsAFocusedWidgetsKey` pins
  the consequence: this screen's bindings never take a key away from a widget
  that has focus.
- **A fully registry-derived key contract does not require `Commandable`.**
  This is a materially better answer to ADR 0009 risk 5 than "the interfaces are
  unused", because it moves the discoverability story off widget participation
  and onto the command's own `Enabled` predicate. Two of the four risk-5 claims
  are now answered: `KeyHint.SetEntries` has **two** consumers
  (`examples/hello`, `examples/search`), and both build their hints from
  `DescribeGrouped` and got the same answer about `Entry.Chords`. Verified:
  `grep -rn 'SetEntries' --include='*.go' .` outside `keymap/` returns those two
  plus the definition in `widgets/form/keyhint.go`.
- **`Describe(ScopeFocus)` is over-inclusive rather than incomplete, and is now
  documented as such.** `inScope` returns true on an exact-scope match without
  consulting liveness, so a focus-scoped query advertises bindings whose command
  is currently disabled — which, applied to a hint, means advertising the
  **field's** arrow bindings on a screen where the **table** has focus and those
  arrows move a selection. This was reasoned about before; an application has now
  run into it.

**Not done at this gate.**

- **No catalog widget implements `Commandable`,** and none implements
  `Clickable`. Still verified: `grep -rn 'Commandable' widgets/` returns nothing.
  Both `examples/hello` and `examples/search` are *applications* opting in, which
  is not a widget contributing its own bindings, so ADR 0009 §8's deferral is
  untouched. It is not blocking: neither example needed it.
- **`Registry.SetFocus` is still deferred to v1.1**, and `examples/search` is
  **evidence for** it rather than a consumer of it. The example has to track
  focus itself and filter its hint on `km.Has`, which is the one query an
  application makes when focus changes and the one it cannot make of the registry.
  Verified: `grep -rn 'func (r \*Registry) SetFocus' keymap/` returns nothing.
  The workaround is the shape of the gap, not a substitute for the fix.
- **There is still no command palette.** Unchanged from v0.6.0.
- **The two-mechanism overlap is avoidable by application discipline and still
  not handled by the framework.** See risk 2 in the register below;
  `examples/search` declines to bind `Home`/`End` precisely because a
  `ScopeScreen` binding outranks a focused child, and `Warnings()` is still the
  fix if that discipline is not followed.
- **`Chord` normalisation has still met no real terminal.** Unchanged from
  v0.6.0, and untouched by an example that only ever receives decoded events.

The gate below is the v0.6.1 gate, retained as history.

## Release gate for v0.6.1 — what closed, and what did not

**v0.6.1**, a minor, and the reason is one new method and one rewired example:
**the first application to actually use `keymap` found a shape the
specification had not considered.** A minor and not a patch because
`Registry.DescribeGrouped` is new exported API on a shipped type; the pinned
hint line and the help overlay in `examples/hello` are rendered text, so they
change for every reader of the screenshot and the golden corpus, and that is a
behaviour change under the release policy above.

**Closed at this gate.**

- **ADR 0009 risk 5 is retired, and the answer to its own question was no.**
  Risk 5 asked whether `Entry.Chords` as one-row-per-chord is the right shape,
  triggered by "the first `examples/` change that wants a hint line to agree
  with a binding". `examples/hello` is that change, and it dispatched through a
  real `keymap.Registry` — **six commands, twelve chords**, with `q`/`Q`/`Esc`/
  `Ctrl+c` and `?` at `ScopeGlobal` and the four navigation commands at
  `ScopeScreen` — with **both** the pinned hint line and the `?` overlay
  rendered from the registry through `KeyHint.SetEntries`.
  It is **not** the right shape for a hint: `SetEntries` joins an entry's chords
  into **one** label, so `Describe`'s output rendered a three-chord command's
  description **three times**. That is three rows of noise in a line with room
  for one. The example worked around it with a thirteen-line merge of its own;
  that workaround is now `Registry.DescribeGrouped(scope)` — one `Entry` per
  **command** — and it is deleted.
  **`Describe` is unchanged** and remains one row per chord, which is what ADR
  0009 §9 specifies for a palette and what a help screen wants. Both queries are
  views of one internal `describeRows`, so they cannot disagree about which
  bindings are in scope, about the order, or about which description a binding
  overrides; `DescribeGrouped` merges `Describe`'s already-sorted rows rather
  than re-sorting, so neither ordering can drift from the other.
  Verified: `grep -rn 'termmosaic/keymap' --include='*.go' .` outside `keymap/`
  returns `widgets/form/keyhint.go` and `examples/hello/` only.
  **Updated at the v0.7.0 gate** — it now also returns `examples/search/`.
- **`Widget.Handle` claims nothing in `examples/hello`,** and the hand-written
  hint string and the test that checked it against `Handle` are both gone. A
  binding and its description are written **once**, which is the property the
  v0.6.0 gate said was untested at scale.
- **The visible consequences are recorded rather than left in a golden diff.**
  The pinned hint reads `[Q q Esc Ctrl+c] quit  ·  [?] toggle the keys`, where it
  read `press q to quit  ·  ? keys  ·  arrows move focus` — the navigation is
  gone from the one-line hint because a merged row per navigation command is
  wider than the line. The help overlay went from **two rows to three**,
  because a derived help spells every chord in full where the prose it replaced
  said "arrows". **17 golden files moved, one row each.**

**Not done at this gate.**

- **No catalog widget implements `Commandable`,** and none implements
  `Clickable`. Still verified: `grep -rn 'Commandable' widgets/` returns nothing.
  The example that closed risk 5 is an *application* opting in, which is not a
  widget contributing its own bindings, so ADR 0009 §8's deferral is untouched.
  Risk 5's trigger is spent; this is not the same trigger and stays open.
- **`Registry` has no `SetFocus`, so `Describe(ScopeFocus)` is incomplete before
  the first dispatch** — the registry only learns what is focused from the
  `focus` argument `Dispatch` is handed. A palette is unaffected (`ScopeGlobal`,
  and §2.1's specificity order covers the rest), but an application with real
  focusable widgets cannot yet ask "what can I do right now". New API on a
  shipped type, so **deferred to v1.1**. `examples/hello` avoids it by binding
  its navigation at `ScopeScreen`.
- **There is still no command palette.** ADR 0009 §9 puts it in scope and
  explicitly outside that ADR. Unchanged from v0.6.0.
- **`examples/markets` and `examples/dashboard` still dispatch by their own
  `switch`** (`examples/markets/dashboard.go:577`,
  `examples/dashboard/main.go:329`). See risk 2 below. **Superseded at the v0.7.0
  gate:** `examples/search` now dispatches through a registry, so it is two of
  four.

The gate below is the v0.6.0 gate, retained as history.

## Release gate for v0.6.0 — what closed, and what did not

**v0.6.0**, a minor, and the reason is one new package: **`keymap` ships.** The
[ADR 0009](adr/0009-command-and-keymap.md) decision that was DECIDED-but-
unimplemented for v0.2.0, v0.3.0, v0.4.0 and v0.5.x is now code. A minor is the
right bump because `KeyHint.SetEntries` is a new method on a shipped widget and
`Describe`/`Invoke` are new API on `Menu` and `Dialog`'s vocabulary — additive,
and the release policy puts new API in a minor.

**Closed at this gate.**

- **`keymap/` exists and its two named tests pass.** `keymap/registry.go`,
  `command.go`, `chord.go`, `scope.go`, `participation.go` and `describe.go`,
  against a root `Widget` that is **unchanged** — four methods, none added,
  changed or deprecated. `TestDispatchIsZeroAllocation` (12 subtests: key match,
  `Enabled` nil / true / false, `Run` declining to a next candidate, `Run`
  declining with nothing else bound, mouse press with and without a `Clickable`
  at the point, mouse drag, and `EventPaste`/`EventResize` never dispatched) and
  `TestChordIsSixteenBytes` both pass. `TestParseChordRoundTrips` walks the
  generated `Key` × modifier × rune table and passes against the real `Key` enum.
- **The five prose-against-code contradictions in ADR 0009 are recorded, not
  silently fixed.** `Commandable`/`Clickable` live in `keymap` because naming a
  `keymap` type from the root package is the import cycle §1 forbids; `Ctx` is
  **152 bytes**, not the 128 the prose claimed; §2.1's precedence table
  contradicted itself on overrides and specificity wins; `Ctrl+k` and `Ctrl+K`
  are two chords while modifier *names* are case-insensitive; and `Entry.Chords`
  has length 1 because `Describe` emits one `Entry` per chord. See the dated
  amendment at the end of that ADR.

**Not done at this gate.**

- **No catalog widget implements `Commandable`.** Verified: `grep -rn
  'Commandable' widgets/` returns nothing. **That half is unchanged and is
  re-verified at the v0.7.0 gate.** The other half of this bullet — that
  `KeyHint.SetEntries`, the one bridge between `Describe` and the widget path, is
  called by no example — was true here and **is no longer true**: it now has two
  consumers, `examples/hello` and `examples/search`, both rendering their hint
  from `DescribeGrouped` and getting the same answer about `Entry.Chords`. The
  discoverability story is therefore tested at two applications rather than none,
  and the stronger form of the evidence is what `examples/search` shows: an
  application can have a **fully registry-derived key contract** — every chord,
  every command, every hint row written once — with `Commandable`
  unimplemented, because `Command.Enabled` is a property of the command and not
  of a widget. That is a materially better answer to ADR 0009 risk 5 than "the
  interfaces are unused": widget participation is no longer carrying the story on
  its own, so §8's deferral is not blocking. `Clickable` still has no consumer.
- **There is no command palette.** ADR 0009 §9 puts it in scope and explicitly
  outside that ADR. It is new behaviour in `Menu`/`Dialog`, so a v1.1.0 minor.
  This is the half of the keymap work that genuinely has not happened, and it is
  deliberately separated from the half that has.
- **`Chord` normalisation has still met no real terminal.** Nothing in its
  folding rules has been run against a live tty, and the ADR's byte-stream matrix
  has still not been executed. The round-trip test proves `ParseChord` and
  `Chord.String()` agree with each other; it cannot prove a real terminal agrees
  with them. A normalisation fix after v1.0.0 is a behaviour change.

The gate below is the v0.5.0 gate, retained as history.

## The v0.5.0 gate, retained as history

**v0.5.2** was released 2026-10-05 — [ADR 0010](adr/0010-mouse-routing.md)
settles who receives a mouse event, and the answer fixed three widgets that
were getting it wrong. **v0.5.0**, a minor, had the reason a
**breaking API change**: five exported widget fields became private, because each
had a working setter already and the field let a program invalidate nothing. The
reasoning is [ADR 0007](adr/0007-responsive-screens.md) §3's: a widget caches its
derived layout keyed on `Bounds()`, so a field changed without `Invalidate()`
yields a stale layout nothing ever repairs.

Eight widgets kept a stale cache after a documented setter. They were found by
the cache-audit mode ADR 0007 §3 specified as its deferred "expensive half",
which is now built and **gates the build** — this defect class is now caught
mechanically rather than by review. See [CHANGELOG.md](../CHANGELOG.md).

**v0.5.1** was released 2026-10-05 — the seven style-application defects this
gate's audit found, including two (`Dialog`'s choice label and `Button`'s
focus/disabled label) that made a focused row unreadable under default styles.
The audit found **no remaining instance** of the class across all 24 widgets.

## The v0.4.0 gate, retained as history

**v0.4.0** was released 2026-10-05 — a minor over v0.3.0, and the
reason is one `widgets/data` fix: `Tree.drawRow` computed a per-node content
style and applied it only to the expander glyph, writing the label through a
direct `SetSpansCappedIn`, so a node's `Style` and the selected row's
`SelectedStyle` reached the text not at all. The label now goes through
`paintRow` — the same path `List` and `Table` use — over a rect covering the
label region alone. That changes what a library widget draws, which is the
definition of a behaviour change, and the release policy above puts those in a
minor — a patch that changed behaviour would be a bug in the release.

This closes the defect class. v0.3.0 fixed `List` and `Table`; `Tree` was missed
because its row painter computed the style for one cell and then took a different
call for the text beside it, so the computed style stopped where the two paths
diverged.

The rest of this release — every CI action moved to a Node 24 major
(`checkout` v4→v7, `setup-go` v5→v7, `golangci-lint-action` v7→v9,
`github-script` v7→v9) and every runner image pinned by name rather than
`-latest` (`ubuntu-24.04`, `macos-15`, `windows-2025`) — is test and tooling, and
none of it is a reason for the bump. `go-version: "1.23"` is deliberately
unchanged per ADR 0001, and `shell: bash` on the gofmt step is deliberately
retained. **None of the CI change has been executed by a GitHub Actions run**;
the versions were verified statically and the first push is the actual test. That
is recorded under the release's Known Limitations rather than glossed, because a
green badge in this repository currently attests to the *previous* CI
configuration.

**Verified baseline at the v0.4.0 gate**, go1.27.1 darwin/arm64: `go build ./...`,
`go vet ./...` and `gofmt -l .` clean, and `go test ./... -count=1` green across
all 25 test packages. `GOOS=windows go vet ./...` is the only check that has ever
covered `term/terminal_windows_test.go`, and it still has never been executed.

**Closed at this gate.**

- **`Tree` node styles and `SelectedStyle` reach the label.** The third and last
  instance of the defect class v0.3.0 opened in `widgets/data`. `drawRow`
  computed the content style — node `Style`, falling back to `ItemStyle`, and
  `SelectedStyle` outright when selected — and applied it to the expander cell
  only, then wrote the label with a call taking no style at all. The label now
  goes through `paintRow` over a label-only rect, so the fill cannot reach the
  marker or the indent. Four tests pin it in `widgets/data/tree_test.go`: node
  style reaching the label, the `ItemStyle` fallback, a multi-span label keeping
  its own styles, and the ASCII path. The frame path stays zero-allocation and no
  golden moved, because `widgets/data` has no `testdata`.

**Not done at this gate.**

- **The CI posture change has never been run.** Every action moved to a Node 24
  major and every runner image pinned by name, verified statically — each action's
  `action.yml` declares `using: node24` — and not by a single GitHub Actions run.
  Nothing has been pushed since. A major bump to any of those four actions can
  change inputs, defaults or behaviour, and `macos-15` and `windows-2025` are
  new images for this project. This is the one item at this gate that is both
  unfinished *and* unobservable locally, which is why it is named here rather
  than counted as closed.
  **True at this gate. Superseded:** the configuration has been run and is
  green — every `ci` run on `main` since `8211716` (2026-10-05, the commit that
  introduced it) has passed, and PRs #17, #18 and #19 each merged at **12/12**
  on the same `ci.yml`, which has not changed since. Gate item 1 is closed.
  **Second supersession (2026-10-06):** `ci.yml` has since changed — PR #26
  (`1564ad5`) dropped the Windows test leg, so branch protection now requires
  11 checks, not 12. The 12/12 record above remains true for the PRs it names.
- **`term/terminal_windows_test.go` is still compile-only.** Unchanged from
  v0.3.0 and restated because it is the item most easily mistaken for coverage:
  `GOOS=windows go vet` proves it compiles, the Windows backend still runs zero
  tests at runtime, and the tests have never been executed anywhere.

The gate below is the v0.3.0 gate, retained as history.

## The v0.3.0 gate, retained as history

The v0.3.0 gate is preserved below. Its closed claims were true when made and
are not restated as current work; the items it left open are still open and
appear in the lists further below.

The v0.3.0 release was 2026-10-05
([CHANGELOG.md](../CHANGELOG.md)). A minor bump, and the reason is the pair of
`widgets/data` fixes: `List` now passes the per-item style it computes to
`paintRow`, so `ItemStyle` and `SelectedStyle` actually reach the text, and
`Table.drawCell`'s "write verbatim" sentinel was `buffer.DefaultStyle` where
`Style.IsUnset()` compares against `Style{}`, so the guard fired on every cell and
replaced every cell `Style` and every column `CellStyle` with the terminal
default. Both change what two library widgets draw, which is the definition of a
behaviour change, and the release policy above puts those in a minor — a patch
that changed behaviour would be a bug in the release.

Everything else in that release — the allocation assertion, the markets rune
truncation, the Windows type-checked tests, the dependency guard, the untracked
binaries, the four skips-now-fails, the golangci-lint gate and the badge — is
test, tooling, packaging or prose, and none of it was a reason for that bump.

The gate below is the v0.2.0 gate, still the substantive one.

**v0.2.0** was released 2026-10-05
([CHANGELOG.md](../CHANGELOG.md)). A minor bump, and the reason is the first fix
below: `Renderer.Post` now wakes the frame pacer, which **is** a behavioural
change.

**Verified baseline at this gate**, go1.27.1 darwin/arm64: `go build ./...`,
`go vet ./...`, `gofmt -l .` and `golangci-lint run ./...` all clean (the last
at 0 issues against the pinned v2.14.0, down from 36), and
`go test ./... -race -count=1`
fully green across all 25 test packages — ~830 tests in 80 files, against 62
golden files.

Recorded so that nothing below is silently open. "Closed" means it is done and
verified; "not done" means it is still open and is named as such rather than
left for someone to rediscover.

**Closed.**

- **`Renderer.Post` no longer deadlocks the pacer.** `needsFrameLocked` did not
  consider queued callbacks, while `Pacer.Run` gates every frame on
  `NeedsFrame()` and posted callbacks only run *inside* `Render`. An app that
  updates the screen from `Post` — the mutation path ADR 0003 documents as safe
  against a concurrent `Draw` — painted its first frame and idled forever. The
  new `examples/markets` hit it live, and `--offline` masked it from every test,
  because the instant fetch is already queued by the time the first frame runs.
  Fixed.
- **The diff no longer emits a cursor move before every wide glyph.** Run
  suppression compared against `lastX+1`, but a wide glyph advances the terminal
  cursor by two. 6,000 wide glyphs cost 6,000 CUP escapes against the narrow
  scene's 30; the tracker now advances by the glyph's **cell width**, cutting
  68,832 bytes to 19,443 for identical output. This closes the one defect the
  v0.1.0 gate recorded as deliberately unfixed.
- **`BarChart` draws horizontal-mode category labels in place.** `adapt` never
  set `axisRow`, so every label went to absolute column 0 — inside `Bounds` only
  for a chart at the origin, and outside its own rectangle everywhere else.
- **`Menu` and `Dialog` exist**, with keyboard traversal, focus save and restore,
  and per-widget key, cell and golden tests. `Dialog` alone brings six test files
  and its own golden corpus; `Menu` five.
- **`examples/markets`** — a live dashboard on real data with no API key, three
  reflowing bands, and `--offline` so tests and CI never touch the network.
  **Keyboard and mouse** work in all three examples.
- **`examples/hello` is genuinely responsive** rather than clamped.
- **The capture generator is built**: `internal/docsgen` and `cmd/capture`, the
  cell-grid-to-HTML path and a `-check` determinism gate. The Hugo/Pagefind site
  still is not.
- **Mouse routing is decided and the three wheel defects are fixed.**
  [ADR 0010](adr/0010-mouse-routing.md) settles *who gets a mouse event*:
  **a widget handles a pointer event only if the pointer is inside its
  `Bounds()`**. `form.Tabs`, `form.Select` and `form.Radio` all tested the wheel
  before their own `Contains` check and so consumed a notch anywhere on the
  screen — which meant a tab row first in `examples/markets`' focus ring ate
  every notch in the application and the table under the pointer never scrolled.
  All three are fixed in the shared `optionList.wheelDelta` helper, which gained
  a `buffer.Rect`, and pinned by `widgets/form/hittest_test.go` with
  non-vacuity proven by reverting the check and watching the tests fail.
  `termmosaic.Widget` is unchanged and no routing API was added. **This changes
  `examples/markets` behaviour**: its application-level wheel-routing workaround
  was only ever routing around the defect, and a notch over a KPI tile is now
  consumed by nobody rather than by the tab row.

**Not done, and deliberately so.**

- **`keymap` is not implemented.** [ADR 0009](adr/0009-command-and-keymap.md)
  specifies it; the package does not exist. Widgets still dispatch their own keys
  and there is no command palette. This was targeted at v0.4.0 — and v0.3.0 and
  v0.4.0 both shipped on 2026-10-05 without it — and it is the one DECIDED row
  in the table above whose implementation is entirely ahead of it.
  **True when written at this gate. Superseded: `keymap` shipped in v0.6.0** —
  see the v0.6.0 gate above. The "no command palette" half is still true and is
  still open, under that gate.

- **The documentation site is still not built** — only the capture half is.
  Separate work, separate repository.
- **The colour quantiser is still unvalidated.** PROPOSED, not DECIDED.
  **True at the v0.2.0 gate. Superseded 2026-10-06:** the audit ran (PR #18),
  found a defect, and the quantiser was replaced (PR #19) — now **DECIDED**, see
  the table row above and gate item 6.
- **tmux / screen DCS passthrough is still missing.** Deferred with a trigger.
- **IME / preedit is not implemented**, by ADR 0005's deliberate deferral. The
  cost is documented rather than mitigated.
- **The cache-poisoning debug mode is still not built.** ADR 0007's expensive
  half, still a convention rather than a check. Not a release gate.
  **True at this gate. Superseded in v0.5.0:** the cache-audit mode is built and
  **gates the build** — see the v0.5.0 note above; gate item 3 is closed.
- **`TextArea` has no rendered selection, and there is no `Form` container, no
  table column selection, no pager selection and no redo stack.** Each would be
  a widget API addition; none is in scope for a release gate.
- **Every widget still lacks a `func Example`** — see the definition of usable
  library below. Three example *programs* exist; zero runnable per-widget
  examples do.
  **True at this gate. Superseded 2026-10-06:** PR #17 added **74 `func
  Example` functions across all 24 catalog widgets** — gate item 7 is closed.
- **The lint gate is narrower than the checklist above suggests.** It is
  `golangci-lint run ./...` at a pinned v2.14.0 on Linux, with `errcheck`,
  `ineffassign`, `govet`, staticcheck's SA rules and `unused` enabled and the
  five opinionated stylistic checks disabled by a written decision in
  `.golangci.yml`. It is not a second opinion on the ADR checklist, it is not
  re-verified against newer linter releases, and `unused` cannot distinguish a
  dead field from a reserved one — which is why `labelPad` and `labelSpans` are
  kept with an inline `//nolint:unused` instead of being deleted from library
  widgets.
- **`term/terminal_windows_test.go` has still never been executed.** CI
  cross-builds Windows and never runs the suite there, so those assertions are
  verified by `GOOS=windows go vet ./...` alone: they compile, and nothing more
  is claimed for them.

## The v0.1.0 gate, retained as history

The v0.1.0 gate is preserved below. Its closed claims were true when made and
are not restated as current work; the items it left open are still open and
appear in the lists above.

**Closed.**
Recorded so that nothing below is silently open. "Closed" means it is done and
verified; "not done" means it is still open and is named as such rather than
left for someone to rediscover.

**Closed.**

- **`CHANGELOG.md` now exists**, hand-maintained, with Breaking / Added / Fixed /
  Known Limitations sections. It was a stated requirement of the definition of
  usable library and had never been written. The Known Limitations section lists
  what is genuinely unsupported rather than what is merely unpolished.
- **The four ADR 0007 §3 renderer tests exist** — `TestRenderAtZeroSizeWritesNothing`,
  `TestResizeShrinksAndRepaintsWholeRect`, `TestResizeCoalescedToOneRepaintPerTick`
  and `TestRootBoundsClippedToScreen` — each pinned by mutation rather than merely
  by presence. Writing them **found a real defect**: `Render` flushed the sink even
  when it wrote nothing, breaking ADR 0007 §4's contract. Fixed.
- **`SetSpansWindowIn`'s `skip` path question is settled.** It was recorded as
  unreachable; it is reachable, via `clampColOffset`'s ceiling rather than via
  `scrollCols`. `Table`'s behaviour is unchanged; the reasoning was wrong, and both
  the code comment and this document are corrected.
- **`examples/hello` no longer uses the clamp-and-centre pattern.** `centred(sw,
  sh, 46, 9)` is replaced by a bounds computation expressed as two `layout.Solve`
  calls, which is the documented path rather than the anti-pattern ADR 0007 was
  written about. The golden files are regenerated; the visible consequence is that
  the block sits one row lower on an odd slack, because largest-remainder
  distribution hands the leftover cell to the earliest-declared `Fill`.
- **`docs/ARCHITECTURE.md` is a pointer.** 251 lines of duplicated and stale ADR
  summary replaced by 105 lines of orientation with a link per ADR.
- **Wide-glyph paths are benchmarked.** See the open-questions entry below for the
  numbers and for the one defect found and deliberately not fixed.

**Not done at v0.1.0.** Superseded by the v0.2.0 list above; retained verbatim so
the gate is not rewritten. One item has since closed: the diff's cursor-move
overhead on wide glyphs, fixed in v0.2.0.

- **The cache-poisoning debug mode is still not built.** ADR 0007's expensive half,
  still a convention rather than a check. Not a release gate.
  **Superseded in v0.5.0** (original line kept verbatim): built as
  `render/cache_audit.go`, gated by `widgets/cacheaudit` — gate item 3 is closed.
- **The diff's cursor-move overhead on wide glyphs is not fixed.** One line, and
  specified in ADR 0008's amendment, but `internal/diff` is the most load-bearing
  code in the project and this is not the task to change it in.
- **IME / preedit is not implemented**, by ADR 0005's deliberate deferral. The cost
  is documented rather than mitigated.
- **tmux / screen DCS passthrough is still missing.** Deferred with a trigger.
- **The colour quantiser is still unvalidated.** PROPOSED, not DECIDED.
  **Superseded 2026-10-06** (the original line is left verbatim above): the
  audit was performed, found a defect, and the quantiser was replaced — the row
  is now **DECIDED**.
- **The documentation site is not built.** Separate work, separate repository.
- **`TextArea` has no rendered selection, and there is no `Form` container, no table
  column selection, no pager selection and no redo stack.** Each would be a widget
  API addition; none is in scope for a release gate.
- **`docs/adr/README.md`'s "Still open" list is stale in one entry**: it lists the
  colour model as undecided, where this table records it as PROPOSED. Correcting it
  means editing an ADR, which the v0.1.0 gate forbids except for ADR 0008's risk 5,
  so it is recorded here instead. **Resolved 2026-10-06:** that entry was
  rewritten when the colour model moved to DECIDED on measurement; it is no
  longer stale and no longer open.

## Known gaps in comparable frameworks

Recorded because they define our opportunity. Sources verified 2026-10.

- OpenTUI ships no progressbar / gauge / meter / sparkline / bar chart.
- OpenTUI ships no list / tree / pager / virtual scroll; its roadmap still
  lists text virtualization as unfinished.
- OpenTUI has no CHANGELOG; release bodies are auto-generated PR lists.
- OpenTUI pre-1.0 patch releases carry behavioral changes; the v1.0 refactor
  is already on the roadmap.
- OpenTUI has no IME/preedit support. **We do not either, by decision**
  ([ADR 0005](adr/0005-input-decoding.md) §7) — the parity here is real, and it
  is not something to present as a gap-closing feature.
- Ink repaints the full screen on update, which shows up as input lag at
  scale.

## Open questions

Answered questions have been removed; the reasoning is preserved in
[docs/adr/](adr/README.md). Still genuinely undecided:

- **Kitty graphics protocol in v1, or stay text-only?** Leaning no. Images
  undermine the grid-of-cells assumption that the whole renderer rests on, and
  the feature is not in the widget catalog. Still open because "no" has not
  been formally decided, and because a future `Image` widget may force the
  question.
- **IME / preedit scope — DEFERRED, and scoped out.** This was "the largest
  unquantified item on this list"; [ADR 0005](adr/0005-input-decoding.md) closes
  it as a deliberate deferral rather than leaving it open. **Honest note about
  what that costs:** users composing Japanese, Chinese or Korean in a
  `TextInput` get *wrong* behaviour, not degraded behaviour — on most terminals
  the committed text arrives as a burst of ordinary key events, which inserts
  correctly but pollutes the undo stack with one entry per character. We accept
  that; the mitigation is documentation. The door is deliberately left open at
  three named seams so this is a deferred feature rather than a deferred rewrite:
  `EventCompose` is declared (and never emitted in v0.x), `Event` carries a
  `Compose *Compose` payload field, and the parser's entry point is the byte
  stream rather than the event type. **Trigger to revisit:** two or more
  independent reports of CJK/IME input being unusable; or `TextInput` shipping
  with undo groups large enough to be obviously wrong on composition-shaped
  bursts; or a terminal shipping a preedit protocol with real adoption. Not
  before — there is no `TextInput` to design against and no reference
  implementation in any comparable framework, OpenTUI included.
- **tmux / screen DCS passthrough — DEFERRED, and a real gap.** It fell out of
  the input-decoding work rather than being designed by it: without it, a
  TermMosaic program under tmux on a modern terminal can lose key and mouse
  reporting. Trigger: any tmux user reporting broken keys or mouse, or v1.0,
  whichever comes first. See [ADR 0005 §10](adr/0005-input-decoding.md).
- **Wide characters (CJK, emoji) and grapheme clusters — STILL OPEN, and now
  MEASURED.** A wide glyph occupies two cells and the continuation cell must
  compare equal across frames or the row flickers; that rule is implemented,
  tested, and was **benchmarked for the first time in v0.1.0**. The wide paths
  did **not** regress: on a 200x60 scene a full repaint costs 74,139 ns/op against
  the ASCII scene's 74,078, and the row-skip tier is indistinguishable. All paths
  are 0 allocs/op. Numbers and method: the 2026-10-04 amendment to
  [ADR 0008](adr/0008-style-and-text.md) risk 5; benchmarks in
  `internal/diff/wideglyph_test.go`, `buffer/wideglyph_test.go` and
  `render/wideglyph_bench_test.go`.
  **What is still open is the decision, not the performance:** `RuneWidth`'s table
  is hand-written from East Asian Width ranges rather than generated from Unicode
  data, and grapheme clusters are not composed (a flag emoji renders as two cells'
  worth of junk). Deferred until internationalization is scoped.
- **Headless backend: v1 or v0.5?** Largely settled — [ADR 0001](adr/0001-backend-strategy.md)
  makes the headless memory sink a v1 deliverable, because the whole testability
  pillar depends on it. The remaining open sub-question is its **assertion
  surface**: it must expose the cell buffer, not just recorded bytes, since
  tcell's headless backend cannot do this and widget tests need it.
- **Windows console support.** ADR 0001 commits to owning the terminal layer,
  which means Windows console mode flags are our problem. Linux and macOS are
  expected first; Windows is currently an unquantified v1.0 risk. **Updated
  2026-10-04:** `CGO_ENABLED=0` builds are now verified for `windows` and
  `linux/arm64`, so the packaging half of the risk is closed — but the Windows
  backend is a **stub that returns a loud error** from every console operation,
  not a working console. **Updated 2026-10-05:** `term/terminal_windows_test.go`
  now exists and pins that documented error contract. It is `//go:build windows`
  and has been verified **compile-only**, via `GOOS=windows go vet`. It has
  **never been executed** — not on Windows, not under Wine, not on any machine
  with a console. So the runtime half of this risk is exactly as open as it was:
  the tests raise the floor on what a Windows implementation would have to
  satisfy, and nothing more. The remaining risk is entirely the runtime half.
- **Color model and degradation ladder — DECIDED, on measurement.** Moved from
  OPEN to PROPOSED, and from PROPOSED to DECIDED by the audit and the
  replacement it forced (PRs #18 and #19); see the table row above for the
  numbers. The **theme/styling system** is no longer in this list:
  [ADR 0008](adr/0008-style-and-text.md) decides it as "no theme in v1".
- **A cache-poisoning debug mode — CLOSED in v0.5.0. The four ADR 0007 §3 tests
  — now written.** Two separate items, previously recorded as one:
  - ~~**The cache-poisoning debug mode remains unbuilt.** The catalog surfaced
    that a rect-keyed cache is only half the contract — see ADR 0007's 2026-10-04
    amendment. The cheap half is a stated convention; the mechanical check is not
    built. A debug mode that corrupts a widget's cache after `Draw` and asserts the
    next frame is identical would catch that whole class instead of by review.~~
    **Built in v0.5.0** — written exactly as the question described it:
    `render/cache_audit.go` arms `Config.CacheAudit`, which poisons the cache
    after `Draw` and asserts the next frame is byte-identical, plus a cold-twin
    check in `widgets/widgettest` for the field-keyed half; `widgets/cacheaudit`'s
    `TestCatalogCacheAudit` walks the catalog and **gates the build**. It is the
    mechanical check the cheap half could never be, and it closed gate item 3.
  - **`TestRenderAtZeroSizeWritesNothing`, `TestResizeShrinksAndRepaintsWholeRect`,
    `TestResizeCoalescedToOneRepaintPerTick` and `TestRootBoundsClippedToScreen`
    now exist**, in `render/responsive_contract_test.go`, together with
    `TestRootBoundsEntirelyOffScreenClipsToNothing`. Each is pinned by mutation, not
    merely by presence. **Writing them found a real defect:** `Renderer.Render`
    called `Sink.Flush` even when it wrote zero bytes, which broke ADR 0007 §4's
    "does not call the Sink" contract for a zero-sized screen; `Render` now flushes
    only when bytes went out. The degenerate-size resize sweep
    (`TestResizeShrinksAndRepaintsWholeRect/through_a_degenerate_size`) and the
    example's own grow/shrink/degenerate/recover sweep
    (`examples/hello`'s `TestResizeGolden`, with two new golden files) are the
    scripted resize sweep ADR 0007 risk 6 asks for.
- **`SetSpansWindowIn`'s `skip` path — RESOLVED, and the recorded reason was
  WRONG.** This said the path was unreachable from `Table` because `scrollCols`
  always positions on a column start. That is false, and `widgets/data`'s
  `table_window_test.go` proves it. `Table` keeps two horizontal offsets: a column
  index and a CELL position, and the cell position is set to a column start only
  when a caller asks for one. `clampColOffset`'s **ceiling** is
  `totalW - contentW` — the right-hand edge of the content — which is generally not
  a column start. Two reachable consequences, both pinned: scrolling to the end
  (shift+End) clamps onto that ceiling and leaves the column before it partially
  visible; and any resize that changes `contentW` or `totalW` re-clamps onto it, so
  the partial column appears with no scrolling key pressed at all. So `skip` is on
  the production path and `Table` needs no new behaviour.
  **Neither of the two options this question offered is the right one**, which is
  why it took a measurement to settle: the primitive did not need teaching to
  scroll by cell, and it did not need documenting as unused. `Table`'s behaviour is
  **unchanged**; a misleading comment on
  `TestTableWideColumnIsMarkedRatherThanBleedingIntoTheFrame` that repeated the false
  claim has been corrected in place.

## Definition of "usable library"

The bar this project is measured against:

- SemVer honored from v0.1; **no behavioral change in a patch release**.
  **PARTIALLY MET — this line previously claimed a flat MET on a falsehood,
  and the correction is recorded here rather than quietly substituted.** What
  it read: "**MET** — every bump so far is minor, v0.1.0 → v0.4.0, and no
  patch release exists to have violated it." That is false as it stands:
  releases ran on to v0.7.0, patch releases exist, and **three patch numbers
  shipped behaviour changes while each called itself "a minor bump" in its own
  release notes** — a known labelling defect in the release record. The
  versions are not relabelled and [CHANGELOG.md](../CHANGELOG.md)'s history is
  not rewritten; this line is where the defect is on record:

  - **v0.5.1** (patch, 2026-10-05) — seven style-application fixes:
    `Dialog`'s `ChoiceFocusStyle` and `Button`'s focus/disabled styles never
    reached the label (a focused choice rendered unreadable under default
    styles), `Table`'s `SelectedStyle`/`ItemStyle` and `HeaderStyle` filled
    the row but not the cell text, `Menu`'s check glyph and submenu arrow,
    `Radio`'s focus gutter, and `BarChart`'s axis-label overrun. What several
    widgets draw changed.
  - **v0.5.2** (patch, 2026-10-05) — the ADR 0010 wheel hit-test fixes:
    `form.Tabs`, `form.Select` and `form.Radio` consumed a wheel notch from
    anywhere on screen; they now consume it only inside `Bounds()`, and that
    changes `examples/markets` behaviour with them.
  - **v0.6.1** (patch, 2026-10-05) — `Registry.DescribeGrouped` is new
    exported API, and `examples/hello`'s pinned hint line and `?` overlay now
    render from the registry, so rendered text changed for every reader of the
    screenshot and the golden corpus (**17 golden files moved**).

  The policy is honoured where it matters to a consumer: every behavioural
  change above is disclosed in [CHANGELOG.md](../CHANGELOG.md) with what
  changes for a program, and every behavioural change from v0.2.0 to v0.4.0
  took a minor — v0.2.0's `Post` waking the pacer, v0.3.0's `List` and
  `Table` style fixes, and v0.4.0's `Tree` style fix. **v0.4.1 is the one
  clean patch**: a `golang.org/x/term` pin only, no code change and no
  behaviour change.
- A hand-maintained `CHANGELOG.md` with Breaking / Added / Fixed /
  Known Limitations sections. **MET** — [CHANGELOG.md](../CHANGELOG.md) is
  written and carries the sections; v0.2.0 adds Fixed, Added, Changed and
  Known Limitations.
- CI green on **Linux and macOS** across supported architectures — **amended
  from "Linux, macOS, and Windows"** by the v1.0.0 platform decision recorded
  below, so this criterion now states the target instead of contradicting it
  ([ADR 0001](adr/0001-backend-strategy.md)). **MET on the amended target.**
  The `ci` workflow runs **11** required checks — `test (ubuntu-24.04)`,
  `test (macos-15)`, `gofmt`, `golangci-lint`, `zero-allocation diff`, and six
  `cross-compile` legs (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64,
  windows/amd64, windows/arm64) — the list read off branch protection on
  2026-10-06. Before PR #26 (`1564ad5`, 2026-10-06) the workflow ran 12
  checks: the same set plus a `test (windows-2025)` leg. Every `ci` run on
  `main` since `8211716` (2026-10-05) passed on that 12-check configuration,
  and PRs #17, #18 and #19 each merged at **12/12** on it. The v0.4.0 gate's
  "no green CI run for the current workflow configuration at all" was true when
  written and is **false now**; that gate records it as history, this line
  records the truth. One thing stays true and is not claimed away: nothing has
  ever been *run* on Windows — the backend is a loud-error stub and CI only
  cross-compiles it — which is a reason the platform decision narrows the
  target rather than a gap in the evidence for Linux and macOS. The platform
  decision's consequence for the Windows CI job — "either goes or is relabelled
  **cross-compile-only**" — is **applied and done**: PR #26 dropped the Windows
  test leg outright, and the leg is gone rather than relabelled.
- **Every widget has a runnable example and a documented public API. MET.**
  There are four example programs — `hello`, `markets`, `dashboard`, `search` —
  **and 74 `func Example` functions covering all 24 catalog widgets**, in the
  eight `widgets/*/example_test.go` files added by PR #17 (each of the eight
  widget packages also carries a package-level `Example`, so the package itself
  has a runnable example and not only its widgets). Each of the 24 widgets has a
  documented public API in godoc terms **and** a godoc-rendered example; every
  one renders through `widgets/widgettest`, so the `// Output` comment **is** the
  cell grid the renderer produced — the assertion and the documentation are one
  string, and cannot drift apart. `Menu` and `Dialog`, the two this criterion
  used to call out as having no example program, are in the sweep. The one thing
  it needed: `vocabulary_test.go` gained an `exampleFiles` exception list for
  the box-drawing rune guard, because a widget whose chrome is a border
  necessarily writes box-drawing runes into its example source — the same reason
  `buffer/border_test.go` is already excepted — and the list is explicit rather
  than suffix-matched so that adding an example package is a deliberate act at
  the place the rule is written down.
- Known limitations are enumerated in the docs, not discovered by users.
  **MET** — [CHANGELOG.md](../CHANGELOG.md)'s Known Limitations section, plus
  the "Known Limitations" in [docs/adr/](adr/README.md) and the gaps named in
  this document.
---

# The road to v1.0.0

Written before the work, so the order and the reasoning are on record rather
than reconstructed afterwards. It states what must close, what deliberately
will not, and what is deliberately later.

## The verdict

**v1.0.0 is not honest today.** Three things made it so, and they were
independent — closing any one left the other two. **Two of the three have closed
since this was written**; what keeps the verdict standing is item 2 below plus
the two housekeeping items still open in the table (8 and 9). Each struck-out
item keeps its original wording, so the reasoning that opened it is not
rewritten out of the record:

**Outcome (2026-10-06):** v1.0.0 was tagged with item 2 and gate items 8 and 9
still open; the release notes' "Open at this release" section records them
rather than closing them. The verdict above is retained as the record of the
bar, not as a claim that it was met in full.

1. ~~**The project's own bar is not met, by the project's own words.**~~
   **CLOSED.** The criterion read **NOT MET, and not close** and called itself
   the largest unmet item; shipping v1.0.0 against a bar this document declared
   unmet would have been a contradiction in the release, not a judgement call.
   It now reads **MET** — **74 `func Example` functions across all 24 catalog
   widgets** (PR #17), each rendered through `widgets/widgettest` so the
   `// Output` comment is the cell grid. What is left of this item is not the
   bar but the housekeeping around it: table items 8 (documentation accuracy)
   and 9 (prose freeze).
2. **The most consequential architectural decision has now been implemented**,
   which it was not when this was first written — it slipped its stated version
   twice before it landed. `keymap` shipped in v0.6.0, and both named tests
   exist and pass. **What remains open in this item is not the architecture but
   its exercise**: no catalog widget implements `Commandable`, **two examples of
   four** bind chords, and `Chord`'s folding rules have met no real terminal.
   Freezing the
   surface is now the right call; freezing it *unused* is the remaining risk,
   and it is ADR 0009's risks 2, 3 and 5 rather than a gap in this gate list.
   **Updated for v0.6.1:** risk 5 is retired — `examples/hello` binds twelve
   chords across six commands and both of its hints render from the registry —
   and no catalog widget implements `Commandable` still.
   **Updated for v0.7.0:** the mechanism is exercised in **two examples of four**
   — `examples/search` is the second, and the first with a focusable widget in a
   focus ring — and no catalog widget implements `Commandable` still. What
   remains is that `Chord`'s folding rules have met no real terminal, and that
   the two-mechanism overlap is avoidable by application discipline but still not
   handled by the framework.
3. ~~**CI green is unevidenced on all three platforms** — the last green badge
   attests to the previous workflow configuration.~~ **CLOSED.** The current
   configuration has been run and is green: every `ci` run on `main` since
   `8211716` (2026-10-05, which introduced it and has not changed since) has
   passed, and PRs #17, #18 and #19 each merged at **12/12**. "All three
   platforms" was also the wrong target: the platform decision below narrows
   v1.0.0 to Linux and macOS.
   **Annotated 2026-10-06:** this evidence describes the configuration that
   closed the item; PR #26 has since dropped the Windows test leg, so the
   required checks are now 11. The 12/12 record above is unchanged as history.

What is genuinely ready: the *signatures*. This codebase has done the hard part
of stability design already, and done it deliberately.

| Already frozen by decision **and** by a mechanical check | Where |
|---|---|
| `Widget` is four methods and stays four — no method added, changed or deprecated | ADR 0007, ADR 0009, and `keymap/` in v0.6.0 |
| `Event` is append-only; `Key`'s iota block is extended only at the end | [ADR 0005](adr/0005-input-decoding.md) §10 |
| Padding-free 16-byte `Cell`, 12-byte `Style`, 16-byte `Chord`, `unsafe.Sizeof(Event{})` | test-pinned sizes |
| `Cells()` is gone; `RowBytes` panics on a view; `diff.Frame` carries `*buffer.Buffer` so old call sites cannot compile | [ADR 0006](adr/0006-subbuffer-cell-access.md) |

That last row is the best stability work in the repo and it is already done:
a rule enforced by a *type* rather than a comment.

**So the v1.0 promise is really a promise about pixel output, not signatures.**
The signatures are in good shape. What is not frozen is what a widget *draws*.

## Decision: v1.0.0 supports Linux and macOS

**Decided 2026-10-05.** The Windows backend stays a loud-error stub, and the
supported matrix is narrowed to say so.

The reasoning, since it amends a criterion above rather than satisfying it:
[ADR 0001](adr/0001-backend-strategy.md) decides Windows is **late** and calls
it the "highest-probability source of v1 slippage". The stub is not an
incomplete feature but the correct posture — "Rather than ship a half-working
Windows console that passes its own tests on a developer's machine, this stub
fails every operation loudly so the gap stays visible." A rushed Windows
backend written to hit a release date is the exact failure the stub exists to
prevent.

The alternative — amending the *release statement* rather than the code — is
available at zero engineering cost, and is the honest one. Nothing in the v1.0
promise requires a platform that has not been built.

**Consequences, all of them required:**

- The "CI green on Linux, macOS, and Windows" criterion is amended to Linux and
  macOS, with this reason.
- The Windows CI job either goes or is relabelled **cross-compile-only**,
  asserting exactly that ADR 0001 commits to and nothing more.
- "Linux and macOS" appears in the **first screen** of the README, not in
  Known Limitations. A user who builds for Windows, gets `ErrWindowsStub`, and
  reads it in a footnote has been misled by the release.
- `Terminal` and `Sink` stay unchanged, so a Windows backend — ours, or a
  build-tagged one — is additive later and touches no frozen signature.

## What must close before v1.0.0

**Outcome (2026-10-06):** the bar was met — v1.0.0 was tagged on 2026-10-06.
This table is retained as the record of what closed and what did not; items 8
and 9 remain open and are tracked in the stage note at the top of this file.

| # | Work | Why it gates |
|---|---|---|
| 1 | ~~**Green CI evidence** on the current configuration~~ **CLOSED (v0.5.x)** | Minutes, and every other claim rested on it. Evidence: `ci.yml` last changed in `8211716` (2026-10-05) and **every `ci` run on `main` since has passed**; PRs #17, #18 and #19 each merged at **12/12** — `test (ubuntu-24.04)`, `test (macos-15)`, `test (windows-2025)`, `gofmt`, `golangci-lint`, `zero-allocation diff` and six `cross-compile` legs — against that unchanged configuration. **Annotated 2026-10-06:** this evidence describes the configuration at closure; PR #26 (`1564ad5`) has since dropped the Windows test leg, so the required checks are now 11 and no longer include `test (windows-2025)`. The 12/12 record above is unchanged as history. |
| 2 | ~~**Behaviour audit of all 24 widgets**~~ **CLOSED (v0.5.1)** | Three of five releases so far exist because of this defect class, and every find after v1.0.0 would be a v1.1.0. The audit found **seven** instances of the style-application class (two of them making a focused row unreadable) and **no remaining instance** of it across all 24 widgets; #3 is what keeps it that way. |
| 3 | ~~**Cache-poisoning debug mode** (ADR 0007's expensive half)~~ **CLOSED (v0.5.0)** | What makes #2 mechanical rather than a matter of review, and the highest leverage per hour here. Built as `render/cache_audit.go` — `Config.CacheAudit` arms `poisonLocked`, which corrupts the rect-keyed cache after `Draw` and asserts the next frame is byte-identical — and it **gates the build**. |
| 4 | ~~**`keymap`** (ADR 0009), with the two named tests~~ **CLOSED** | Shipped in v0.6.0 as `keymap/`, above `Widget.Handle` and with `Widget` unchanged. `TestDispatchIsZeroAllocation`, `TestChordIsSixteenBytes` and `TestParseChordRoundTrips` all pass. `KeyHint`'s help surface is no longer empty: `Describe` computes it from the same tables `Dispatch` walks. **Exercised in v0.6.1** — `examples/hello` dispatches through a registry and `DescribeGrouped` was added for its hint lines. The package still ships with no catalog widget implementing `Commandable` and no palette — both tracked as open, neither blocking. |
| 5 | ~~**Mouse routing decision + the three wheel defects**~~ **CLOSED** | Decided by [ADR 0010](adr/0010-mouse-routing.md) — widgets hit-test themselves, `Widget` unchanged — and the three wheel defects are fixed and pinned. See below. |
| 6 | ~~**Colour model: decide it**~~ **CLOSED (2026-10-06, PR #19)** | The reason it gated was exactly right: a PROPOSED row cannot survive the freeze, because replacing the quantiser later changes every program's 256/16-colour output — a v1.1.0 in the first release. It was decided the only way this row could be decided, by measurement. PR #18's CIEDE2000 audit found the redmean weights inert (`uint8` division made both weights 2) and measured selection error **256: max 21.201, 19.35% above the JND; 16: max 36.821, 37.50%**, with `markets.down` collapsing to grey and colliding with `markets.flat`. PR #19 replaced the quantiser through the existing `buffer.Quantiser` hook: **selection error 0.000 on both rungs, 0 of 281,216 colour-rungs regressed, frame path 224.8 → 6.6 ns/op at 0 allocs**, and every audit threshold pinned at 0 so a silent re-drift fails the test. The alternative — keeping redmean and calling it validated — is rejected on recorded numbers. See the table row above. |
| 7 | ~~**`func Example` per widget**~~ **CLOSED (PR #17)** | The largest unmet criterion in the project's own bar, and purely additive: **74 `func Example` functions across all 24 catalog widgets**, in the eight `widgets/*/example_test.go` files, every one rendered through `widgets/widgettest` so its `// Output` comment is the cell grid — and each of the eight widget packages carries a package-level `Example` too. It needed one exception to a written rule: `vocabulary_test.go`'s `exampleFiles` list, because a bordered widget's example must name the box-drawing runes it paints (the same reason `buffer/border_test.go` is excepted). Zero stability risk. |
| 8 | **Documentation accuracy pass** | README said "Thirty-plus widgets" against a catalogue of 24, "the eight architecture decisions" against nine, and "Not yet released as a module version" beside a `go get` line — the three claims that opened this item. For a project whose product *is* documented honesty, stale headline numbers are a release blocker. **Still open, partly worked.** All three named claims now read correctly: the ADR count reads *ten*, the "not yet released" phrasing is gone and its replacement `go get` pin reads **`@v1.0.0`** (it read `@v0.7.0` after PR #21 and was corrected again at the v1.0.0 release), and the widget-count phrasing says **twenty-four** — the last two corrected by PR #21, which landed after this row last recorded them as stale (it then read **v0.5.2 against a `v0.7.0` tag**, with "the widget-count phrasing is untouched"). This pass has corrected the colour-model row, the CI criterion, the README's platform line and — through PR #21 — the README's widget count and install pin; the v1.0.0 release pass corrected the README status block and install pin, the sponsoring line, CONTRIBUTING.md's stage and architecture claims, and this document's CI criterion (12 checks → 11 after PR #26), without closing the item: a pass that stops halfway is not a pass, and nothing yet records the full pass as complete. |
| 9 | **Prose freeze** | Status block, platform matrix, stale gate sections, and the `keymap` apology paragraph — which **has been rewritten** as a shipped-feature statement in v0.6.0, so what remains is the housekeeping around it. |

### 5 is bigger than the defect it is filed under — **and it is closed**

`docs/STATUS.md` recorded "`form.Tabs` consumes every wheel notch" as a widget
defect wanting an ADR. It was a **framework contract gap**, and it affected three
widgets:

| Widget | Bounds-checks the pointer? | Where, as audited |
|---|---|---|
| `Button` | **yes** | `form/button.go:255` |
| `Toggle` | **yes** | `form/toggle.go:196` |
| `Select` | **no** — consumed the wheel unconditionally | `form/select.go:269` |
| `Radio` | **no** — consumed the wheel unconditionally | `form/radio.go:264` |
| `Tabs` | **no** — consumed the wheel unconditionally | `form/tabs.go:331` |

The cause was that the framework had no consistent answer to *who gets a mouse
event*. Applications route every mouse event to the root and let widgets
disagree, which is still the right contract; three widgets just guessed wrong,
and they guessed wrong **identically**, all three testing the wheel before their
own `Contains` check. That is why the fix went into the shared
`optionList.wheelDelta` helper rather than into three call sites.

This was not a small thing to defer, which is the argument that was made at the
time. [ADR 0009](adr/0009-command-and-keymap.md) §6 names mouse hit-testing as
**"the one thing widgets are genuinely better at than a global registry"** — and
it is a deciding reason for its Option A. Freezing v1.0 with three widgets
mishandling exactly that would have contradicted the project's own decision
record.

**What was decided.** [ADR 0010](adr/0010-mouse-routing.md) chose the first of
the two options on the table — **widgets hit-test themselves, as `Button` and
`Toggle` already did** — and recorded the rule as a sentence: *a widget handles
a pointer event only if the pointer is inside its `Bounds()`*. The second
option, a framework hit-test/routing helper, is costed and rejected in the ADR:
it is new exported API against an interface ADR 0007 and ADR 0009 both freeze,
it needs a tree walk `Widget` cannot express because there is no `Children()`,
and it can only answer "which rect" where a widget answers "which cell means
what". **The `Widget` interface is unchanged and no routing API was added.**

Two things the ADR settled that the defect filing did not ask about, and that are
worth knowing before the next widget is written:

- **Two exemptions are stated rather than left implicit.** A **release** ends a
  drag wherever the pointer is — a gesture that can only be finished over the
  widget strands the user — and a **drag** continues outside `Bounds` once a
  press has claimed it. *The press is the claim, the drag is the continuation.*
- **`TextInput` and `TextArea` decline the wheel by decision, not oversight.**
  Wheel-to-scroll in a `TextArea` is a plausible feature; adding one under a
  routing ADR would be a feature nobody reviewed as a feature.

`split.Split` was audited for the same defect and **needed no change** — it
bounds-checks through `PaneAt` and `dividerAt`, arms a drag only on `MouseLeft`,
and ends a drag on a release anywhere, which is exemption one already in place.

## What is deliberately not built before v1.0.0

Named here so their absence at v1.0.0 is a decision on record rather than an
oversight:

- **A Windows console backend** — v1.1+, additive, interfaces unchanged.
- **A `Form` container** — ADR 0004's solver plus `layout` already covers
  composition; a `Form` type would be a second way to do the same thing.
- **IME / preedit** — ADR 0005's deliberate deferral, with three named seams
  left open (`EventCompose`, the `*Compose` field, the parser entry point). The
  cost is documented. CJK users get *wrong* behaviour rather than degraded
  behaviour; that honesty is the deliverable.
- **Kitty graphics** — currently an OPEN row, which at v1.0.0 would be a standing
  invitation to argue about scope after the freeze. **Formally decide "no for
  v1"**, one sentence in an ADR.
- **Grapheme-cluster composition** — performance is measured and good; only the
  decision is open, and the failure is cosmetic.
- **A `Ctrl+K` palette** — ADR 0009 §9 puts it in scope but explicitly not in
  that ADR. New behaviour in `Menu`/`Dialog`, so a v1.1.0 minor. **The `keymap`
  package it would be built on shipped in v0.6.0**; the palette itself still does
  not exist, and `Describe`/`Invoke`/`Chords` — the three things §9 says it needs —
  are what it would consume.
- **Making any catalog widget implement `keymap.Commandable`** — ADR 0009's own
  deferral. The two interfaces are built and **no widget in `widgets/*`
  implements either**. `examples/hello` opting in as an *application* in v0.6.1
  does not change this: a registry an application drives is not a widget
  contributing its own bindings, and §8's deferral is untouched. The v0.6.0
  trigger — the first `examples/` hint line that needs to agree with a binding —
  **has now fired and was answered**, and it was answered on the example side.
  The remaining trigger for this bullet is the first *catalog widget* whose keys
  a program would rather name than switch on.
- **`Registry.SetFocus`** — not built in v0.6.1, and **deferred to v1.1**. The
  registry learns what is focused only from the `focus` argument `Dispatch` is
  handed, so `Describe(ScopeFocus)` returns an incomplete answer before the
  first dispatch, and an application with real focusable widgets cannot ask the
  question §4 exists to answer. New API on a shipped type, so a minor.
  `examples/hello` avoids it by binding its navigation at `ScopeScreen`.
  **Strengthened at v0.7.0 — this is now the best-evidenced item in the list.**
  `examples/search` cannot avoid it by that route: its arrows mean different
  things on different panes, and it is a registry *and* a real focus ring. It
  tracks focus itself and filters its hint on `km.Has`. It also established that
  `Describe(ScopeFocus)` is **over-inclusive rather than incomplete** —
  `inScope` short-circuits an exact-scope match without consulting liveness — so
  the fix is not only a missing method but a method whose answer must be
  filtered by availability.
- **`examples/markets` and `examples/dashboard` moving off their own `switch`**
  onto `keymap` — two examples of four did this (`examples/hello` in v0.6.1,
  `examples/search` in v0.7.0) and the other two did
  not. What is missing is not the migration but the **overlap**: nothing in the
  tree now has a `keymap` binding and a widget `switch` answering the same key,
  which is the failure risk 2 above is about.
- **TextArea rendered selection, table column selection, pager selection, a
  redo stack** — additive widget API, all four fine as v1.1.0.
- **A theme system** — ADR 0008 decides "no theme in v1", with a trigger: the
  first role two widgets must share.
- **A wider lint checklist** — real, self-declared, and not a release gate.

The budget freed by not building these is what pays for items 2 through 7.

## Risks to a clean v1.0.0

The release policy promises **no behavioural change in a patch release**, so
the practical consequence is: **any behavioural defect found after v1.0.0 is a
v1.1.0.** The risk register is therefore a list of things likely to be found
late.

1. **Latent instances of the style-application class.** Highest probability by a
   wide margin — three of five releases were exactly this. The mechanism is
   per-widget divergence between the style a painter computes and the writer
   that carries the text, and it applies to every widget with a row painter, not
   only the three already fixed. Mitigated by items 2 and 3.
2. **`keymap`'s two mechanisms overlapping in a real application** — a global
   binding shadowing a widget's own `switch`. Survivable via `Warnings()`, but
   it is a behaviour change, so v1.1.0, and it is the kind of thing that erodes
   trust in a stable 1.0. **Partly exercised since v0.6.0, and the remaining
   half is the half that matters.** `examples/hello` is now the mixed-mechanism
   example the mitigation asked for: it dispatches through a real
   `keymap.Registry` and its `Handle` claims nothing, which is the *resolved*
   shape rather than the overlap. The stated mitigation was building such an
   example **before** the tag, and what it has actually established is narrower
   than "no application mixes both" — it has established that one application of
   four has moved off its `switch` entirely.
   **One example of four was not the mitigation; a second one also is not, but
   the reason has changed.** `examples/search` (v0.7.0) is the second of four to
   dispatch through a registry, and the **first with a focusable widget in a
   focus ring** — a `form.TextInput` and a `data.Table`, moved with
   `Tab`/`Backtab`. The stated mitigation, "build a mixed-mechanism example
   before the tag", has now been met by an application that genuinely has both
   mechanisms live: registry bindings at `ScopeScreen`/`ScopeGlobal` alongside a
   focused widget's own key contract.
   **What it established is that the overlap is avoidable by discipline, not
   that the framework handles it.** `search` gates every context-dependent
   binding with `Command.Enabled` — which `dispatchChord` skips, so the event
   falls through to the tree — and declines to bind any chord a focusable widget
   wants: `Home`/`End` are left to the widgets, because a `ScopeScreen` binding
   outranks both, and the ring's ends are bound to `Ctrl+Home`/`Ctrl+End` instead.
   `TestNoScreenBindingStealsAFocusedWidgetsKey` pins that. So `Handle` and the
   registry still never compete for one key, and the *failure this risk is
   actually about* — a `keymap` global binding shadowing a widget's `switch` — is
   still unobserved. What has changed is that the shape is now demonstrated
   rather than assumed, and the discipline it requires (screen bindings must not
   claim a focused widget's key) is an obligation on **applications**, which is
   the argument for `Warnings()` rather than against it. `examples/markets`
   (`dashboard.go:577`) and `examples/dashboard` (`main.go:329`) still dispatch
   by their own `switch`, and neither binds a chord or implements
   `Commandable`. The fix if it
   bites remains `Warnings()` reporting a chord that is both bound and handled
   by an attached widget, **not** a `Widget` change.
3. **`Chord` normalisation disagreeing with a real terminal.** Nothing in its
   folding rules has met a real tty, and the ADR asks for a byte-stream matrix
   that has not been run. A normalisation fix is a behaviour change.
4. ~~**The colour quantiser being wrong in a way nobody looked for** — the
   direct cost of freezing a PROPOSED row.~~ **THIS HAPPENED, AND IS CLOSED —
   2026-10-06, before any freeze.** It is the one risk on the list that
   actually materialised: PR #18's audit found the redmean weighting inert and
   the selection error unacceptable, and PR #19 replaced the quantiser through
   the existing hook — selection error **0.000 on both rungs**, **0 of 281,216
   colour-rungs regressed**, every audit threshold pinned at 0. Retired by
   measurement rather than by luck, and the surviving form of the risk is a
   rule: a quantiser replacement after v1.0.0 is a release-defining re-audit,
   which is what gate item 6's thresholds now enforce.
5. **Windows being discovered by a user after v1.0.0.** Mitigated entirely by
   saying "Linux and macOS" in the first screen.
6. ~~**Process risk: `keymap` slips a third time.**~~ **RETIRED 2026-10-05.**
   It slipped v0.3.0 and v0.4.0; it shipped in v0.6.0 and the third slip never
   happened, so the risk this entry described is gone. The worry behind it is
   not — that a v1.0 freeze ships a promise instead of a feature — but it now
   applies to a *different* promise: the command palette (deliberately not
   built) and the two optional interfaces nothing implements. Those are named in
   the "deliberately not built" list above with their triggers, which is the
   honest form of the same discipline.

## Two things `keymap` got right — now checkable, not checkable-in-principle

Recorded because they change the cost. **This section was written before the
package existed and has been updated against the built code.** It is retained
because both claims turned out to hold, and it was not obvious that they would.

- **`Event` is untouched.** ADR 0009's own delta table: a command is resolved
  *from* an event and never carried inside one, so **no payload is added and the
  `unsafe.Sizeof(Event{})` guard is unaffected.** `Chord` is a new type in a new
  package, reachable only through `keymap.Ctx`. **Verified in v0.6.0:** the
  decision-table row above no longer says "`Event.Chord` normalisation" — this
  document's old phrasing was loose shorthand that read as though a frozen
  signature moved, which is the one thing that would have made `keymap` a
  breaking change. It never did.
- **Everything is additive.** Two new *optional* interfaces (`Commandable`,
  `Clickable`) and one new method (`KeyHint.SetEntries`). Adding an optional
  interface is not a breaking change; adding a method to a struct is not either.
  **Verified in v0.6.0** — and note *where* the interfaces landed: in `keymap`,
  not in the root package beside `Focusable` as the ADR originally specified,
  because naming a `keymap` type from the root package would be the import cycle
  ADR 0009 §1 itself forbids. The reason to have built `keymap` first was
  repetition and freezing an untested surface, not SemVer; that reasoning
  survived contact with the code.

## Two stability hazards found while writing this

Neither is in an ADR, and both would freeze by accident.

1. ~~**`widgets/widgettest` is public and therefore frozen by accident.**~~
   **DECIDED and CLOSED — 2026-10-06: `widgets/widgettest` is explicitly
   excluded from the v1.0.0 compatibility promise.** It appears in no ADR's
   stability discussion, and user test suites would depend on it whether or
   not that was intended — all 74 `func Example` functions in
   `widgets/*/example_test.go` import it, so it is public in fact as well as
   in name. This hazard was kept open at release time with two options on the
   table (promote it to a decided surface, or move it under `internal/`
   **before** v1.0.0); v1.0.0 shipped without picking either, which made the
   promise cover the package by default and closed the window to relocate it
   silently. **Decision: exclude the package from the promise, explicitly, in
   the documentation.** It is a test harness whose value depends on evolving
   alongside the framework — freezing it would either prevent useful helpers
   from being added or force a major version bump every time one is, and
   serves nobody either way. The honest resolution at this point is to
   document the exclusion rather than pretend the freeze is workable. The
   package is not moved, not renamed, and unchanged in code; the hazard is
   retired because the promise no longer lands on the package. Recorded in
   `CHANGELOG.md`'s `[Unreleased]` section; the `## [1.0.0]` "Open at this
   release" entry naming the package stays as written, because released
   sections are history. The macOS test leg, `deleteBranchOnMerge` and ADR
   0003's reference review are separate open items and are unaffected.
2. **The variadic constructors are an open-ended signature.** Three of the 25
   widget constructors take `...` — `NewList(items ...ListItem)`,
   `NewTable(cols ...Column)`, `NewTree(nodes ...Node)`. That is the friendliest
   shape for growth and the subtlest stability hazard: behaviour can be added
   indefinitely without a compile break, so **the API is stable while the
   product is not.** The v1.0 policy needs one sentence to cover it: *an option
   may only be additive; no option may alter the behaviour of a program that
   does not set it.*

## The sequence

```
1  green CI evidence          ─┐
3  cache-poisoning mode       ─┤ CLOSED (v0.5.x)
2  behaviour audit of 24      ─┴─► mechanical, then human review of what it finds

6  colour model decided       ── CLOSED (2026-10-06): audit #18, replacement #19
5  mouse routing + 3 wheels   ── CLOSED (v0.5.x)
4  keymap                     ── CLOSED (v0.6.0) ──► after 2 and 5 landed

7  func Example sweep         ── CLOSED (PR #17) ──► 74 examples, all 24
8  documentation accuracy     ─┐
9  prose freeze               ─┴─► then the tag
```

Items 1–3 depended on nothing and started immediately; **all three are closed**
— the cache-audit mode in v0.5.0, the 24-widget behaviour audit in v0.5.1, and
green CI evidence from `8211716` onward, re-verified at **12/12** by PRs #17,
#18 and #19. Items 2 and 3 were the long pole in effort and are done. **Item 4
is closed** — it waited on 2 and 5, both of which
landed, and shipped in v0.6.0 with `Widget` unchanged. Everything downstream of
the documentation freeze no longer waits on it; what item 4 left open is carried
by the risk register instead, which is the correct place for it now that the
surface exists. **Items 6 and 7 are closed** — 6 by the audit and the
replacement it forced (PRs #18 and #19), 7 by the example sweep (PR #17). What
is left is 8 and 9, and then the tag.

**Postscript (2026-10-06):** the tag happened — v1.0.0 was released. Items 8
and 9 remain open; they are tracked in the stage note at the top of this file
and in gate item 8 above.
