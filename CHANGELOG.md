# Changelog

All notable changes to TermMosaic are recorded here by hand.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
from v0.1.0.

## Release policy before v1.0.0

**The API is not stable and will break without notice.** Every release before
v1.0.0 is a pre-release, and a minor version may contain behavioural changes.
What *is* promised: **no behavioural change in a patch release.** If v0.1.1
changes behaviour, that is a bug in the release, not a policy.

`0.0.x` versions, if any, are not semver and may do anything.

Sections below use these markers:

- **Breaking** — an existing program will not compile, or will compile and behave
  differently. Every one is listed with what to change.
- **Added** — new capability.
- **Fixed** — a defect in a previous version.
- **Known Limitations** — what this release does *not* do. This section is not
  padding: a framework that ships an honest list of its gaps is more useful than
  one that leaves users to discover them.

Anything marked **PROPOSED** in [docs/STATUS.md](docs/STATUS.md) may change or be
reversed before v1.0.0.

---

## [0.4.0] — 2026-10-05

A minor bump, and the reason is the one widget fix below: **`Tree` label text now
takes the per-node `Style` and `SelectedStyle`.** Styling a program set and the
widget silently ignored now takes effect, which is the definition of a behaviour
change and therefore a minor bump under the policy above — a patch release that
changed behaviour would be a bug in the release, so it is not one.

This is the third and last of the same defect class. v0.3.0 fixed `List` and
`Table`, which shared it; `Tree` was missed because its row painter computes the
node's style for the expander glyph and then writes the label through a
different call, so the computed style stopped at one cell.

**What to do about the Breaking entry.** If you set `Style` on a node, or
`SelectedStyle` on the `Tree`, and your tree now looks different, that is this
release working, not a regression, and the new colours are the ones you asked
for. Nothing has to change to compile or to run. If you worked *around* the old
behaviour — compensating in your own styles because the widget ignored them —
that compensation is now double-counting and should come out.

### Breaking

- **`Tree` node styles and `SelectedStyle` now reach the label text.**
  `Tree.drawRow` computed a per-node content style — the node's own `Style`,
  falling back to `ItemStyle`, and `SelectedStyle` outright on the selected row —
  and applied it only to the expander glyph. The label itself was written by a
  direct `SetSpansCappedIn`, so every style the field documented was computed and
  then discarded for the text: a node's label rendered in whatever style its own
  spans carried, and `SelectedStyle` reached the row background but never the
  glyphs beside the marker. The label is now written through `paintRow`, the
  same path `List` and `Table` use, over a rect covering the label region alone
  so the fill cannot reach the marker or the indent painted above it.

  Both fields were already documented as taking effect, so this is a fix to
  behaviour that contradicted its own documentation rather than a new feature.
  The visible consequence: a `Tree` node with a `Style`, and the selected row's
  `SelectedStyle`, are now honoured, with the same precedence `List` documents —
  the selected row's style wins over the node's, and an unset node `Style` falls
  back to `ItemStyle`. The expander glyph is unchanged; it was already correct.

  **A multi-span label is the one thing that does not change**, for the same
  reason `List` did not change it: `paintRow` only applies the override when the
  row is a single span, because flattening a multi-span row would build a string
  on the frame path, and the zero-allocation claim forbids it. A label that
  deliberately carries several styles keeps them. That limit was equally true
  before; it was just invisible.

  Four tests in `widgets/data/tree_test.go` pin the new behaviour: a node `Style`
  reaching the label, the `ItemStyle` fallback, a multi-span label keeping its own
  styles, and the ASCII path. No golden file changed — `widgets/data` has no
  `testdata`, so nothing in the golden corpus rendered a `Tree`.

### Fixed

- **`Tree` label styles are no longer dropped on the way to the screen.**
  Restated as the defect itself rather than the consequence, since the
  consequence above is what a program sees. `drawRow` applied the row style to one
  cell — the expander — and then wrote the label through a call that took no
  style at all. The frame path stays zero-allocation: `paintRow`'s single-span
  override is a field write into a stack array, and the label rect is one value
  on the stack.

### Changed

- **Every CI action is on a Node 24 major, and every runner image is pinned.**
  `actions/checkout` v4 → v7, `actions/setup-go` v5 → v7,
  `golangci/golangci-lint-action` v7 → v9 and `actions/github-script` v7 → v9,
  each verified to declare `using: node24` rather than assumed from its major.
  Runner images are pinned by name instead of via `-latest`: `ubuntu-latest` to
  `ubuntu-24.04`, and the matrix legs from `ubuntu-latest` / `macos-latest` /
  `windows-latest` to `ubuntu-24.04` / `macos-15` / `windows-2025`. A `-latest`
  label changes the toolchain, the C library and the shell underneath this
  project on someone else's date with no commit and no diff to review, so a
  green run on Friday can be a different environment from the red run on Monday;
  `ubuntu-latest` migrates to Ubuntu 26.04 on 2026-10-19, which is close enough
  to be a real date rather than a hypothetical one. The matrix legs are pinned
  for the same reason — leaving them as `-latest` would leave three of the five
  gates exposed to a silent migration while the others were safe.

  `go-version: "1.23"` is **deliberately unchanged**, for the reason ADR 0001
  gives: raising that floor is what would unpin `golang.org/x/term`. `shell:
  bash` on the `gofmt` step is also retained deliberately — the step is a
  formatting gate, and a shell swap there is a change in what it asserts.

  **Nothing about this has been executed by GitHub Actions yet.** See Known
  Limitations.

### Known Limitations

- **The Windows tests are unexecuted, and this release does not change that.**
  `term/terminal_windows_test.go` is `//go:build windows` and is verified
  **compile-only**, by `GOOS=windows go vet` — which proves it compiles and not
  that it passes. It has **never been executed**: not on Windows, not under Wine,
  not on any machine with a console. CI cross-*builds* Windows and never *runs*
  the suite there, so the Windows backend still executes **zero tests at
  runtime**, and the backend itself remains a deliberate loud-error stub. A green
  Windows CI today would be asserting that a loud error is returned correctly.

- **The Node 24 action upgrades and the runner pinning are unvalidated.** Every
  version in the table above was verified statically — each action's `action.yml`
  declares `using: node24`, and each image name is one GitHub publishes. **No
  GitHub Actions run has exercised any of it.** The repository has not been
  pushed since these edits were made, so there is no run, green or red, behind
  any of it, and no badge in this repository currently reflects the new
  configuration. The first push is the actual test: a major bump to
  `actions/checkout`, `actions/setup-go`, `golangci-lint-action` or
  `github-script` can change inputs, defaults or behaviour, and `macos-15` and
  `windows-2025` are new images for this project even though they are established
  GitHub ones. Treat a red first run as an expected possibility, not a surprise,
  and read it before reverting.

- **golangci-lint still runs one version, on one platform, on Linux.** Unchanged
  by this release and restated because the action moved: the pinned `v2.14.0` is
  what this repository was verified against locally, `golangci-lint-action` v9 is
  what will run it, and nothing re-verifies the pair against a newer linter. The
  action major bump is exactly the kind of change that can surface as a red build
  at the moment of the upgrade rather than in advance.

## [0.3.0] — 2026-10-05

A minor bump, and the reason is the two widget fixes below: **both change what
`List` and `Table` put on the screen.** Styling that a program set and the widget
silently ignored now takes effect, which is the definition of a behaviour change
and therefore a minor bump under the policy above — a patch release that changed
behaviour would be a bug in the release, so it is not one.

**What to do about the two Breaking entries.** Read the first one: if you set
`ItemStyle` or `SelectedStyle` on a `List` and your screen looks different, that
is this release working, not a regression, and the new colours are the ones you
asked for. Nothing has to be changed to compile or to run. If you had worked
*around* the old behaviour — compensating in your own styles because the widget's
ignored them — that compensation is now double-counting and should come out.

### Breaking

- **`List` item styles and `SelectedStyle` now reach the text.** The per-item
  style `drawRow` computed for every row was never passed to `paintRow`, so it
  was computed and discarded: a `List` painted each item's spans with whatever
  styles the spans themselves carried, and `SelectedStyle` — documented as the
  selected row's rendition — reached only the background fill, never the glyphs.
  `paintRow` now takes the content style as an override and `List` supplies it.

  Both public style fields were already documented as taking effect, so this is
  a fix to behaviour that contradicted its own documentation rather than a new
  feature. The visible consequence: a `List` item with an `ItemStyle`, and the
  selected row's `SelectedStyle`, are now honoured. `ItemStyle` on an unselected
  row and `SelectedStyle` on the selected row both apply, and the selected row's
  style wins over the item's — which is the precedence the fields describe.

  **A multi-span row is the one thing that does not change.** `paintRow` only
  applies the override when the row is a single span; flattening a multi-span row
  would mean building a string on the frame path, which is the allocation this
  package's zero-allocation claim forbids. A row that deliberately carries
  several styles keeps them. That limit is documented at `paintRow` and is not a
  regression — it was equally true before, it was just invisible.

- **`Table` cell styles are no longer overwritten with the terminal default.**
  `drawCell` was passed `buffer.DefaultStyle` as its "write the spans verbatim"
  sentinel. That value is wrong for the purpose: `Style.IsUnset()` compares
  against `Style{}`, and `DefaultStyle` is `Style{DefaultColour, DefaultColour}`
  — a *resolved* style, not an unset one. So the guard never fired and every
  cell's `Style` was replaced with the terminal's own colours, along with every
  column's `CellStyle`. The sentinel is now `buffer.Style{}`.

  The visible consequence: `Table` cell spans and column `CellStyle` now render
  in the style they were given. A program that set colours on a table column and
  saw the terminal default will now see its own colours. `HeadingStyle` on the
  header was unaffected — it is passed as a real override, not as the sentinel —
  and still works.

  This is a library widget changing what it draws, which is why it is listed here
  rather than buried under Fixed.

### Fixed

- **A test asserted an allocation count the compiler is allowed to vary.** The
  input parser's burst test required *exactly* three allocations through
  `NewParser` + `Feed`. `NewParser` is inlinable, so the `Parser` itself can stay
  on the stack and only its two buffers reach the heap; two is a valid
  observation. The assertion is now an upper bound of three. A fourth allocation
  still fails, which is what the test was actually for.

- **`examples/markets` could cut a multi-byte rune in half.** The status line
  truncated its error text on a byte index, so one accented or non-Latin
  character from an API response would be cut mid-rune and leave invalid UTF-8.
  Truncation now backs up to the last rune start. This is an example program, not
  library code.

### Added

- **`term/terminal_windows_test.go`.** The Windows backend is a deliberate
  loud-error stub, so its correct behaviour is a documented set of exact error
  identities; these tests assert `ErrWindowsStub` by identity rather than by
  message, because a wrapped or renamed error would remove the only programmatic
  handle callers have.

  **These tests have never been executed.** CI cross-*builds* Windows but does
  not run the suite there. They are verified only by type-checking
  (`GOOS=windows go vet ./...`), which proves they compile and not that they pass.
  A green Windows CI today would be asserting that a loud error is returned
  correctly, which is still worth something and is still not the same thing.

### Changed

- **The dependency guard now opens an issue instead of printing a notice.** A
  `::notice` in a scheduled run scrolls past and is gone by morning, so the
  weekly `x/term` probe could report the same stale pin forever with nobody
  reading it. It opens a GitHub issue, deduplicated by a marker naming the
  pinned version so the human who fixes it is not buried under one identical
  issue per Monday, and every error path is `core.setFailed` rather than a
  silent pass — a guard that cannot open its issue and says nothing has stopped
  guarding. The `pull_request` trigger is gone: the probe hits the network, so
  its result on a branch says nothing about whether main's pin can move, and a
  fork's token is read-only regardless, so that step would have failed silently
  on exactly the contributions most likely to be external.

- **`hello` and `markets` are no longer tracked as committed binaries.** Both had
  been checked in at the repository root by a `go build`, and `.gitignore` only
  covered `/termmosaic` and `/examples/*/termmosaic`, so a routine build produced
  an untracked file that showed up as noise in every future `git status`. They
  are removed from tracking and ignored.

- **Four tests that skipped themselves now fail instead.** Each was calling
  `t.Skipf` on a condition that is a property of the *fixture*, not of the
  environment: the table fitting at 120 columns, the KPI tile having a
  rectangle at the golden size, the needle's spot not being window-high. A skip
  on either side of those checks is vacuous — the test asserts nothing and
  reports success. They are now `t.Fatalf`, which is the honest outcome: if the
  golden geometry stops producing the geometry the test needs, that is a real
  failure and CI should say so.

- **golangci-lint is now a CI gate, and the repository carries a config for
  it.** `golangci-lint run ./...` reported 36 issues on the previous release: 21
  `unused` and 15 staticcheck, and **not one of the 36 was a defect**. The 21
  were dead test helpers, unused private constants and two reserved-but-unused
  struct fields; they are deleted, or in the case of the two fields kept and
  marked. The 15 were stylistic (`QF1001` De Morgan, `QF1005` `math.Pow`,
  `QF1008` embedded selectors, `QF1011`/`ST1023` inferred `var` types).

  The lint run is now 0 issues. What was done to reach that matters more than
  the number:

  - **The dead code was deleted, not silenced.** Every deleted test helper was
    grepped for references first, across all test files and examples, because a
    helper that one package's test file stops calling is often still another
    one's — `blockOf` and `focusable` exist in several packages with the same
    name and the same purpose, and deleting the wrong copy would have broken a
    suite. No test was weakened or deleted to make lint pass; the count of tests
    and of packages is unchanged.
  - **Two struct fields were deliberately kept.** `labelPad` in
    `widgets/menu` and `labelSpans` in `widgets/viz` are reserved for passes
    that are written but not yet wired. Removing a field from an exported
    widget's private state is a change to a library widget's internals, and the
    alternative — a blanket `unused` exclusion that hides every dead field in
    the project forever — costs more than it buys. They carry an inline
    `//nolint:unused` with the reason, so the exception is visible at the field
    and the config needs no exclusion list.
  - **The five stylistic checks are disabled in `.golangci.yml`, with the
    reasoning written down.** They are opinionated formatting preferences, not
    correctness rules, and this project makes the opposite choice on purpose in
    each case. `errcheck`, `ineffassign`, `govet` and staticcheck's SA rules —
    the checks that find real defects — are all enabled, and none of them was
    disabled or filtered. The config's comment says this is a house-style
    decision and not suppression of findings, because that is what it is: no
    real finding is behind any of those five names.

  Enabling staticcheck's full set also surfaced three more naming and comment
  rules (`ST1003`, `ST1020`, `ST1022`). `ST1003` wants the exported field `Ascii`
  renamed to `ASCII` on three widgets, which would be a source break for every
  user of the library; that one is a real constraint of a pre-1.0 API and not a
  style preference, so it is named and excluded explicitly rather than left to
  fail the build.

- **The lint run has its own CI job, badge-pinned to one version.** As with
  `gofmt`, the job is separate from the matrix so the badge means "lint" and
  nothing else, and `golangci-lint-action` is pinned to `v2.14.0` rather than
  `@latest`, for the same reason `go-version` is pinned in ADR 0001: a lint
  upgrade is a change in what CI asserts, and it should be a commit, not a
  surprise on someone else's schedule.

- **The Go Report Card badge is gone.** The service shut down in 2025, so the
  badge was rendering as unavailable in every README view — a permanent visual
  claim of a failing check that no longer exists. Removing it is the fix; a
  golangci-lint badge takes its place, pointed at the dedicated job.

- **`cmd/capture` records its own version in `manifest.json`.** The `version`
  constant existed and was documented as being recorded there, but nothing
  consumed it, so `golangci-lint` correctly reported it as unused and a
  capture could not be traced to the program that wrote it. `Manifest` gained a
  `version` field and `Generate` takes the tool version as a parameter. Output
  is still byte-deterministic: the version is a constant of the build, not a
  timestamp.

- **Docs synced to the code.** `docs/STATUS.md` and `docs/ARCHITECTURE.md`
  corrections only — widget counts, a misleading comment on the wide-column
  test, and the release-gate self-assessment. No API surface described in the
  docs changed.

### Known Limitations

- **The Windows tests are unexecuted, and this release does not change that.**
  See above. CI cross-*builds* Windows and never *runs* the suite there, so
  `term/terminal_windows_test.go` is verified only by
  `GOOS=windows go vet ./...`, which proves it compiles and not that it passes.
  The backend itself is still a deliberate loud-error stub.

- **golangci-lint runs on one version, on one platform, on Linux.** The pinned
  `v2.14.0` is the release this repository was verified against; nothing in CI
  re-verifies it against a newer one, so a linter upgrade that introduces a
  finding will surface as a red build at the moment of the upgrade rather than
  in advance. `golangci-lint` also type-checks only under the host GOOS in the
  lint job, so a Windows-only compile error would be caught by the `test` job's
  `windows-latest` leg and not by lint.

- **`unused` cannot see a whole file's worth of intent.** It reports dead code,
  not dead *plans*. The two `//nolint:unused` fields are the honest cost of that
  limit: they are reservations, and this release does not implement them.

- **Nothing was renamed to satisfy a linter.** `Ascii` is still `Ascii` on
  `basic.Text`, `block.Block` and the other widgets that expose it. The lint
  configuration accommodates the API rather than the API accommodating the
  linter, which is the right way round for a pre-1.0 library.

---

## [0.2.0] — 2026-10-05

A minor bump, and the reason is the framework fix below: `Renderer.Post` now
wakes the frame pacer, which **is** a behavioural change.

### Fixed

- **`Renderer.Post` never woke the pacer, so async apps froze.** `needsFrameLocked`
  did not consider queued callbacks while `Pacer.Run` gates every frame on
  `NeedsFrame()` — and posted callbacks only run *inside* `Render`. The chain
  deadlocked: `Post` queued work, `Render` would run it, `Render` was gated on
  `NeedsFrame`, which cannot be true until the callback runs. An app that updates
  the screen from `Post` — which ADR 0003 documents as the safe way to mutate
  widget state, precisely so it is safe against a concurrent `Draw` — painted its
  first frame and idled forever. The new `examples/markets` hit it: live runs
  painted one empty frame and stopped.

  The `--offline` mode masked it completely, which is why every test passed: the
  offline fetch returns instantly, so the callback is already queued by the time
  the first frame runs and gets drained inside it. The pre-existing `Post` test
  called `Render` directly and so could never catch this.

- **The diff emitted a cursor move before every wide glyph.** Run suppression
  checked whether the next cell was `lastX+1`, but a wide glyph advances the
  terminal's cursor by two while `lastX` recorded only the glyph's own column. On
  6,000 wide glyphs that is 6,000 CUP escapes against the narrow scene's 30, and
  68,832 bytes against 6,233 — 11× — for identical output. The tracker now
  advances by the glyph's **cell width**: 60 moves and 19,443 bytes, 3.12×. The
  ASCII path is unchanged at ~7,200 ns/op with 0 allocations. The benchmark that
  had pinned the defective behaviour now asserts the corrected one.

- **`BarChart` drew every category label at absolute column 0** in horizontal
  mode, because `adapt` never set `axisRow`. Inside `Bounds` only for a chart at
  the origin, and a widget writing outside its own rectangle everywhere else.

### Added

- **`Menu`** — a navigable tree with submenus to arbitrary depth. Keyboard
  traversal that skips disabled items, Right to open a branch and Left to leave
  one, Escape closing a level and then the menu. Level headers are bracketed when
  active and not when inactive; the selected row carries a marker and a rule
  rather than colour alone.

- **`Dialog`** — a modal with `VariantInfo`, `VariantConfirm` and `VariantChoice`.
  Keys do not reach the tree beneath, focus is saved and restored, and Escape
  means Cancel rather than doing nothing. The focused action is ringed *and*
  reinforced with reverse video, which survives `NO_COLOR` because colour is
  suppressed only at encode time. `VariantConfirm` opens focused on the
  affirmative action on purpose: a dialog that opens on Cancel makes "walk away"
  the result of a stray Enter.

- **`examples/markets`** — a live finance dashboard on real data with no API key:
  ECB rates from Frankfurter, crypto from CoinGecko. Three bands that reflow as
  the terminal narrows, a fetch goroutine that owns every network call and
  publishes an immutable snapshot, and `--offline` for bundled sample data so
  tests and CI never touch the network.

- **Keyboard and mouse in the examples.** `hello` gains a focus ring and a `?`
  help overlay; `markets` gains a two-entry focus ring, per-panel key routing,
  wheel and click, pause/resume, and a help overlay that is modal for keys but
  deliberately not for the mouse. Mouse capture is opted into by exactly one line
  and never globally — ADR 0005's default stays off, because capturing it takes
  the user's shell selection.

- **[ADR 0009](docs/adr/0009-command-and-keymap.md)** — the command and keymap
  layer. Specified, not yet implemented; `keymap` lands in v0.3.0.

### Changed

- **`examples/hello` is genuinely responsive.** It was laid out with `Max(46)`
  inside two `Fill(1)`s, so it shrank on a small terminal and never grew on a
  large one — a 200×60 screen still drew a 46×9 block floating in the middle.
  That is clamping, not responsiveness. It now spans the terminal and re-arranges
  itself across four bands, and below 38×8 says so in one line rather than
  clipping into nonsense.

- **The widget catalog is 24** (was 22), with `docsgen` entries so the
  documentation site can show both new widgets.

### Known Limitations

- **`keymap` is specified but not implemented** ([ADR 0009](docs/adr/0009-command-and-keymap.md)).
  Widgets still dispatch their own keys. No command palette exists yet.

- **Windows is a stub.** `term.Open` validates its arguments and then returns a
  loud error for every console operation. Nothing on Windows works.

- **No IME or preedit support.** `EventCompose` exists and the parser reserves
  the entry point, but nothing emits it. Composition-heavy input is unsupported.

- **tmux DCS passthrough is missing.** ANSI passthrough may not work inside tmux.
  A known gap rather than an oversight.

- **`TextArea` has no rendered selection.** Half a selection is worse than none.

- **The colour quantiser is unvalidated.** The redmean mapping to 256 and 16
  rungs has never been checked for perceptual acceptability. `buffer.Quantiser`
  is the drop-in hook for a Lab-space replacement.

- **The width table is hand-written** from East Asian Width ranges, and grapheme
  clusters remain uncomposed. A benchmark cannot make a table correct.

- **`form.Tabs` consumes every wheel notch** regardless of where the pointer is,
  so it steals the wheel from whatever is beneath it. Worked around with
  application-level routing in the examples; the widget itself is unchanged. This
  wants an ADR decision.

---

## [0.1.0] — 2026-10-04

The first release. Everything below is new; there is no previous version to
break.

TermMosaic is a cell-buffer renderer plus a catalog of widgets for terminal
applications in Go. It owns the terminal layer outright through two narrow
interfaces and wraps no terminal library, and it is built so that a widget test
can assert on cells without a terminal being involved at any point.

### Breaking

There is nothing to break: v0.1.0 is the first release, so there are no earlier
call sites to break. What follows is the compatibility promise starting *here*.

- **The public API is unstable until v1.0.0.** Minor versions may contain
  behavioural changes. The `Widget` interface is the one thing deliberately held
  fixed — four methods, and it has not changed across the project's eight
  architecture decisions.
- **TermMosaic targets Go 1.23+** and depends only on `golang.org/x/sys` and
  `golang.org/x/term`. Builds are `CGO_ENABLED=0`, verified in CI for
  `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`
  and `windows/arm64`.
- **Nothing wraps `tcell`, Bubble Tea, Textual or any other terminal library.**
  Programs migrating from one of those will find no drop-in compatibility layer
  here; the shape of a TermMosaic program is its own.

### Added

**Rendering.** A double-buffered cell renderer with a two-tier diff: a
per-row byte skip over a per-cell pass, driven by dirty rectangles and paced by
a frame pacer. The frame path is **0 allocations per frame**, asserted rather
than benchmarked. Measured on a 200×60 scene that is 99% static chrome, the
diff writes 141 bytes where a full repaint writes 19,979 — a ~141× reduction at
~7,133 ns/op. A frame in which nothing is dirty writes zero bytes and does not
call the sink at all, so an idle application costs nothing rather than burning
CPU at the frame rate.

**A terminal layer we own.** `Terminal` (size, raw mode, alternate screen,
capability probing, reading) and `Sink` (write, flush) are two small interfaces
with a direct `x/sys` implementation. A headless in-memory `Sink` maintains the
cell grid the emitted bytes *would have produced*, which is what makes widget
tests assert on cells rather than on escape sequences.

**Layout.** A constraint solver of our own: `Length`, `Min`, `Max`,
`Percentage`, `Ratio`, `Fill`, plus spacing and nested composition. Pure Go with
no cgo, so cross-compilation stays clean and the whole solver is testable in CI
with no terminal. `Fill` is order-insensitive, which is a deliberate divergence
from tmux and is pinned by a test.

**Input.** A pure `Decode` under a resumable `Parser`, driven by a `Source` that
merges input bytes and resize notifications onto **one ordered channel** — so a
resize can never be delivered between the halves of an escape sequence, and no
input event is dropped to make room for one. Kitty keyboard disambiguation,
bracketed paste (always one event carrying the whole payload), mouse decoding
in SGR / urxvt / X10, and focus decoding. The key path is 0 allocations, pinned
by test. Mouse capture and focus reporting are **off by default**: enabling
either takes text selection and scrollback copying away from the user's shell.

**Twenty-two widgets**, in four groups:

- *Core* — `Block`, `Text`, `Paragraph`, `Split`. `Block` is the only thing in
  the project that draws a border or a title.
- *Forms* — `TextInput`, `TextArea`, `Select`, `Checkbox`, `Radio`, `Toggle`,
  `Tabs`, `Button`, `KeyHint`.
- *Data* — `List`, `Table`, `Tree`, `Pager`, sharing a row-virtualization engine.
  Cost is flat in item count and asserted: `List` renders 10,000 items in
  13,320 ns and 100,000 in 14,242 — seven percent for ten times the data, both
  at zero allocations.
- *Visualization* — `ProgressBar`, `Gauge`, `Meter`, `Sparkline`, `BarChart`.
  `Sparkline` and `Gauge` use Braille for sub-cell resolution, and `Gauge`
  degrades to a bar when the rectangle or the terminal cannot hold a dial.

**Text and styling.** One `buffer.Style` value (foreground, background,
attributes, twelve bytes, passed by value) and one `Span` type for styled text
parsed once at construction rather than per frame. Range-clipped cell writers
(`SetSpansIn`, `SetStringIn`, `SetSpansCappedIn`, `SetSpansWindowIn`) let a
widget draw text that ends before the screen's edge — a table cell, a gauge
label — without pre-truncating (which allocates) or re-deriving the wide-glyph
rules (which is how two widget packages each grew their own forty-line copy).

**Responsiveness as shared arithmetic.** `geometry.ClampCount`,
`geometry.Priority` with its four-value scale, `geometry.Region` and
`geometry.Budget`, plus the optional `termmosaic.Minimizable` interface. There
are deliberately **no framework breakpoints and no size classes**: each widget's
threshold is a local constant beside its own `Draw`, because a size class is a
lossy function of two numbers and a product decision in the wrong layer. What is
shared is the arithmetic, and the degenerate-size contract: no widget panics,
none blanks itself, and a resize always repaints the whole screen.

**Colour.** Truecolor → 256 → 16, with a redmean quantiser and a `Quantiser`
interface as the escape hatch for a better mapping. `NO_COLOR` is honoured at
encode time, so no widget path consults the environment.

**Two runnable examples.** `examples/hello` is a bordered, resize-aware panel
with golden files covering the rendered screen *and* the exact byte stream at
each rung of the colour ladder. `examples/dashboard` is a live dashboard built
from the catalog.

**Documentation.** Eight ADRs recording what was decided, what was rejected and
why, with benchmark output quoted inline — including the workloads that did not
produce a clean result. `docs/STATUS.md` distinguishes DECIDED / PROPOSED /
OPEN / DEFERRED and names the trigger for revisiting each deferral.

### Fixed

Nothing to fix: v0.1.0 is the first release. The fixes below are defects found
and corrected *before* v0.1.0 shipped, recorded because the project's own
standard is that a decision which turned out to be wrong is documented rather
than quietly rewritten.

- **The example composed a second border implementation.** `examples/hello` drew
  its own border and title with its own width thresholds. It now composes
  `Block`, and spells no border rune and invents no threshold. A test asserts the
  example's source contains no border vocabulary at all.
- **The terminal resize watcher dropped the newest size.** On a full channel it
  kept the oldest, so a drag-resize settled on a stale geometry. It now keeps
  the latest, which is the size the terminal is actually at.
- **`nil` files were rejected on Unix but accepted on Windows**, so the same
  program behaved differently per platform. The Windows backend now rejects them
  too.
- **The example scanned raw input bytes for `'q'`,** which could not tell an arrow
  key from four unrelated bytes, and maintained a second goroutine for resizes.
  It now reads keys and resizes off the one ordered stream.
- **`Wrap` silently discarded newlines,** because a newline is zero-width. A
  multi-line paragraph came back as one unwrapped block. Wrapping is now
  newline-aware, and the rune-index mapping that makes editable wrapped text
  correct is exposed rather than re-derived privately by one widget — which is
  how a single combining mark was found to be shifting every caret position
  after it by one.
- **The range-clipped writers could strand a wide glyph's continuation cell** when
  a window filled its range exactly, leaving an unpaired glyph and a row that
  never compared equal to the previous frame — permanent flicker.
- **A frame that produced no bytes still flushed the sink,** which broke the
  documented contract that a zero-sized screen touches the terminal not at all.
  A detached terminal's event loop now costs nothing.

### Known Limitations

These are real gaps, listed so nobody has to find them. Several are deliberate
deferrals with a stated trigger; where that is so it is said.

**Platform.**

- **Windows is a stub that returns a loud error from every console operation.**
  The package compiles and cross-compiles cleanly for `windows/amd64` and
  `windows/arm64`, so the packaging works and the runtime does not. On Windows
  this framework does not currently draw anything. Linux and macOS are the
  supported platforms.
- **tmux and GNU screen DCS passthrough is missing.** Under tmux on a modern
  terminal, a TermMosaic program can lose key and mouse reporting, because the
  sequences TermMosaic emits are not wrapped for the multiplexer. Deferred with a
  trigger: any tmux user reporting broken keys or mouse, or v1.0, whichever comes
  first.

**Text and internationalization.**

- **No IME or preedit support.** This is a deliberate deferral, not an oversight,
  and the cost is worth stating plainly: composing Japanese, Chinese or Korean
  in a `TextInput` produces *wrong* behaviour rather than degraded behaviour. On
  most terminals the committed text arrives as a burst of ordinary key events,
  which inserts correctly but pollutes the undo stack with one entry per
  character. The event model reserves `EventCompose` and a `Compose` payload so
  this can be added later as a feature rather than as a rewrite of every widget.
- **Wide glyphs and grapheme clusters are handled, but the handling is
  provisional.** A double-width glyph occupies two cells and its continuation
  cell takes its owning span's style, so rows do not flicker — and this release
  is the first in which those paths were actually benchmarked rather than assumed
  (see below). What is *not* done: the cell-width table is hand-written from
  East Asian Width ranges rather than generated from Unicode data, so newly
  assigned wide blocks are wrong until the table is updated; and grapheme
  clusters are not composed, so a flag emoji renders as two cells' worth of junk
  and a zero-width-joiner sequence renders as several glyphs.
- **`geometry.ClampCount` and `geometry.Budget` count cells, not glyphs,** so a
  row budget computed from them can be one row optimistic once wide characters
  are in play.
- **`TextArea` has no rendered selection.** It tracks and moves a caret and
  supports editing, but the selected range is not drawn.

**Colour.**

- **The colour quantiser is unvalidated.** The redmean mapping from truecolor to
  the 256- and 16-colour rungs is implemented and works, but nobody has checked
  that its output is perceptually acceptable. Treat the 256 and 16 rungs as
  provisional. The `buffer.Quantiser` interface exists so a Lab-space mapping can
  replace it without touching anything else.
- **`Caps.Unicode` is a proxy, not a probe.** It is the honest one available
  without querying the terminal out of band, and a terminal configured
  out-of-band will disagree with it.

**Behaviour and scope.**

- **The API is unstable before v1.0.0**, as stated at the top of this file.
- **There is no theme system in v1, by decision.** Widgets carry `Style` fields
  and the framework's defaults are the terminal's own colours plus named
  attribute styles. The trigger for adding one is the first style *role* that two
  widgets must share; until then a theme layer would be an abstraction over
  nothing.
- **There is no `Form` container,** no table column selection, no pager
  selection and no redo stack. Layout composition and focus traversal cover the
  first; the others are not built.
- **A resize allocates two full cell grids per resize event** — about 6.9 MB over
  an 18-event drag at 200×60. That is GC-visible rather than latency-visible, and
  it has not been measured under load. The drag itself coalesces to one repaint
  per frame tick, automatically, with no code from the application.
- **Nothing in this release has been run against a real terminal being
  resized.** Every cost quoted for resize is derived from code and from other
  measurements, not from an observed drag.
- **Partial repaint on resize is not possible by construction,** because resizing
  the buffer discards its cells. This is structural rather than a missing feature.
- **A wide glyph costs one cursor-position escape per glyph** in the diff, so a
  screen that is entirely wide text writes roughly eleven times the bytes of the
  same screen in ASCII. The output is correct; this release measures the cost and
  does not fix it.

### Measured, and newly so

Two performance claims that this release turns from assertion into measurement:

- **The frame path allocates nothing.** On a 200×60 scene that is 99% static
  chrome: 141 bytes written, ~7,133 ns/op, **0 allocs/op**.
- **Wide-glyph paths, measured for the first time.** Every wide path was
  implemented and none had ever been benchmarked. On an Apple M1, darwin/arm64,
  Go 1.23.0:

  | Scene | ns/op | allocs/op | bytes |
  |---|---|---|---|
  | Diff, 99% static, ASCII | 531 | 0 | 141 |
  | Diff, 99% static, wide glyphs | 565 | 0 | 300 |
  | Diff, all rows, ASCII | 6,713 | 0 | — |
  | Diff, all rows, wide glyphs | 6,757 | 0 | — |
  | Full repaint, 200×60, ASCII | 74,078 | 0 | 19,979 |
  | Full repaint, 200×60, wide glyphs | 74,139 | 0 | 21,678 |
  | Row skip, static, ASCII | 272 | 0 | 0 |
  | Row skip, static, wide glyphs | 251 | 0 | 0 |

  **The wide paths did not regress.** On the same geometry a wide-glyph scene
  costs 6% more on a partial diff, 0.1% more on a full repaint, and is
  indistinguishable on the row-skip tier — because a double-width rune costs one
  extra branch and one extra cell write, and saves the loop iteration and the
  width lookup that two narrow runes would have needed. The one genuine cost is
  bytes, and it is listed under Known Limitations above.

  The measurement also surfaced a defect, which **this release does fix**: the
  diff's cursor-run suppression assumed one cell per rune, so on a screen of
  nothing but wide text every glyph was preceded by a cursor-position escape —
  6,000 moves against 30, and 68,832 bytes against 6,233, for identical output.
  The run tracker now advances by the glyph's cell width. On the same scene:
  **60 cursor moves and 19,443 bytes, 3.12× the narrow frame** rather than 11×.
  The ASCII path is unchanged. See ADR 0008's amendment, finding 4.

[0.4.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.4.0
[0.3.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.3.0
[0.2.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.2.0
[0.1.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.1.0
