# Changelog

All notable changes to TermMosaic are recorded here by hand.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
from v0.1.0.

## Release policy before v1.0.0

**Superseded for releases from v1.0.0 (2026-10-06):** v1.0.0 is the first
release with a stability promise — the public API is frozen at that tag and
Semantic Versioning applies from it. The policy below governed every release
*before* v1.0.0 and is kept as the record of what those releases promised.

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

## [Unreleased]

### Fixed

- **Documentation corrections for the v1.0.0 release** (docs only — no code, no
  version constant, no workflow change). The README status block and install
  pin now state the v1.0.0 stability promise and pin `@v1.0.0`; the sponsoring
  line no longer calls the project pre-alpha; `docs/CONTRIBUTING.md` no longer
  describes the project as pre-alpha with undecided architecture.
  `docs/STATUS.md`'s stage note, CI criterion and gate annotations now record
  the **11** required checks after PR #26 dropped the Windows test leg — the
  v1.0.0 section's "Open at this release" item on that leg is superseded by
  PR #26 and stays as written there, because released sections are history.
  `docs/SITE-PLAN.md`'s limitations-page spec and stability-banner rows now
  describe the v1.0.0 surface rather than a pre-1.0 one. Still open and now
  recorded in `docs/STATUS.md`'s stage note: `deleteBranchOnMerge`, the macOS
  test leg, and ADR 0003's third-party reference review. (The
  `widgets/widgettest` freeze decision listed here at the time has since been
  decided — 2026-10-06, see the Changed entry below.)

### Changed

- **Refactor (2026-10-07): the three `widgets/data` scrollbar painters are now
  one unexported function.** `List`, `Table` and `Tree` each carried a
  byte-identical private `drawScrollbar`; they now call a single
  `paintScrollbarThumb` in `widgets/data/data.go`, beside the package's other
  shared helpers. Rendered output is unchanged — thumb arithmetic, the one-cell
  width, the caller-resolved thumb rune (`█`, or `#` on the ASCII rung via
  `block.Block.Ascii`) and each widget's track rectangle are exactly what they
  were, pinned by the existing cell assertions in `widgets/data`'s tests. The
  track rectangle stays a caller-supplied parameter deliberately: `List` and
  `Tree` paint over the block interior, while `Table` paints over the body rows
  below its header, and a shared track would slide the thumb under the Table
  header. Nothing exported moves and no behaviour changes, so there is no
  compatibility note. This closes the PROPOSED "standalone reusable
  `Scrollbar` widget" item in ROADMAP.md: the design review rejected a widget
  type, and this shared painter is the deduplication that proposal was after.
  The `data.Pager` proportional-scrollbar and `form.TextArea` wheel-to-scroll
  PROPOSED items are untouched.
- **Decision (2026-10-06): `widgets/widgettest` is excluded from the v1.0.0
  stability promise.** The package is public — all 74 `func Example`
  functions in `widgets/*/example_test.go` import it — so v1.0.0's promise
  would freeze it by default, and every future helper added to it would be a
  breaking change requiring a major version. The decision excludes it
  explicitly instead: it is a test harness whose value depends on evolving
  alongside the framework, and freezing it would either prevent useful
  helpers from being added or force a major bump every time one is — it
  serves nobody. The window to relocate it under `internal/` silently has
  closed, because v1.0.0 is tagged and published, so the honest resolution
  is to document the exclusion rather than pretend the freeze is workable.
  The package is not moved, not renamed, and no code changes;
  `docs/STATUS.md`'s stage note and its "Two stability hazards" section
  record the decision. The `## [1.0.0]` "Open at this release" entry naming
  `widgets/widgettest` stays as written there, because released sections are
  history — this entry is the dated clarification. The other items in that
  section are unaffected: the macOS test leg and `deleteBranchOnMerge`
  remain open.

---

## [1.0.0] — 2026-10-06

**The first release that makes a stability promise.** The public API freezes
here: from v1.0.0 the project follows Semantic Versioning in earnest, and a
behaviour change means a minor, not a quiet patch. Every release before this was
a pre-release under the policy printed above, and this one retires that
paragraph.

**Why v1.0.0 and not v0.8.0.** The colour-quantiser replacement below is a
**behaviour change** — it changes the bytes a program emits at the 256 and 16
colour rungs — so under the project's own policy it is a minor-level change, and
it must not land in a patch. A `v0.8.0` would therefore be *correct* under that
policy. It is v1.0.0 because this is the first release that promises stability,
and the project's documented policy is that the first release to do so is
1.0.0. The policy's "behaviour change belongs in a minor" rule is untouched and
remains the rule for every release after this one; v1.0.0 does not relax it, it
adopts it as a commitment rather than as a pre-release convenience.

**The version constant moved.** `cmd/capture`'s `version` — the only hard-coded
version string in the module, recorded in `manifest.json` so a capture file can
be traced to the program that wrote it — now reads `1.0.0`. Module versioning
itself remains git tags, as it always has.

### Added

- **A runnable `func Example` for every one of the 24 catalog widgets — 74
  examples**, in the eight `widgets/*/example_test.go` files. **Test-only, no
  behaviour change**: `go test` compiles and runs them and nothing else reads
  them. Each of the eight widget packages also carries a package-level
  `Example`, and every example renders through `widgets/widgettest`, so its
  `// Output` comment **is** the cell grid the renderer produced — the
  documentation and the assertion are one string and cannot drift apart. It
  required one exception in `vocabulary_test.go`: an explicit `exampleFiles`
  list exempting those files from the box-drawing rune guard, because a widget
  whose chrome is a border necessarily names the runes it paints (the same reason
  `buffer/border_test.go` is excepted).

### Changed

- **PR #22 — the release-gate self-assessment's SemVer criterion is corrected to
  PARTIALLY MET.** The gate had recorded the criterion as met; the corrected
  assessment says what actually remains open. Documentation accuracy, no code
  and no behaviour change.
- **PR #24 — the SITE-PLAN prose is frozen.** `docs/SITE-PLAN.md`'s plan text is
  now fixed as the record of what was proposed; future site work updates the
  site, not this document. Documentation-only.

### Fixed

- **The 256/16-colour quantiser selected by fixed-weight RGB rather than
  perceptual redmean; it now selects in Lab space (CIEDE2000).** **This is a
  behaviour change — it changes the bytes a program emits at the 256 and 16
  colour rungs**, so under the policy above it belongs in a **minor** and must
  not ship in a patch. The audit in `buffer/colour_quantiser_perceptual_test.go`
  found why the old code was wrong: `rmean/256` and `(255-rmean)/256` divide to
  zero in `uint8` arithmetic, so both weights were identically 2 and "redmean"
  was in practice the fixed `2*dr²+4*dg²+2*db²` in gamma-space RGB, flipping the
  hue of plausible UI colours. Measured before → after, on the audit's own
  CIEDE2000 metric and its step-5 lattice (140,608 colours × 2 rungs):

  | | redmean (before) | Lab CIEDE2000 (after) |
  |---|---|---|
  | Selection error, 256 rung | mean 1.323, refined max **21.201**, 19.35% above the JND | **0.000** everywhere |
  | Selection error, 16 rung | mean 3.800, refined max **36.821**, 37.50% above the JND | **0.000** everywhere |
  | Total error, 256 rung (lattice mean) | 6.338 | 5.015 |
  | Total error, 16 rung (lattice mean) | 17.657 | 13.857 |
  | Colour-rungs that got worse | — | **0 of 281,216** |
  | `Nearest256` steady-state frame path | 224.8 ns/op | **6.611 ns/op, 0 allocs** |
  | `Nearest16` steady-state frame path | 16.02 ns/op | **7.126 ns/op, 0 allocs** |

  Visible consequences: `markets.down` (`#d86a62`) no longer collapses to grey
  at the 16 rung and collides with `markets.flat` — the up/flat/down trichotomy
  is green/grey/red — and `#9b3228` brick red lands on a red rather than on
  olive. Two goldens move, `examples/hello/testdata/hello_256.sgr` and
  `hello_16.sgr`, the title accent only, each verified better by the audit's own
  metric; no other golden or test expectation moves. Selection goes through the
  existing `buffer.Quantiser` hook, so the diff and the encoder are untouched,
  and steady state is a memo lookup at 0 allocations — only the first use of a
  colour pays for an exhaustive CIEDE2000 search.
- **PR #23 — CI test runs bypass the test-result cache, which is live by
  default.** A manual re-run without `-count=1` could serve a cached PASS
  instead of re-executing tests — a green that did not actually run, which is
  the worst possible outcome under a "12/12 green before merge" rule.
  `cache: true` was added to the setup-go steps that did not declare it. CI
  behaviour only; no shipped code changes.
- **PR #21 — README corrections: the widget count and the install pin.** The
  README's stated catalog count and its pinned install instruction were both
  wrong; both now match the code. Documentation-only.
- **Two stale statements corrected, no behaviour change.** (a) The
  `setup-go` cache comments in `.github/workflows/ci.yml` said the cache is
  "keyed on `go.sum`"; since `setup-go` v6 the default dependency hash is
  `go.mod`, so the comments now say that. Comment-only — no step, input or
  behaviour changed. (b) `docs/STATUS.md`'s decision table said "The
  Hugo/Pagefind site itself is not built" while the site has been serving at
  [serkanalgur.github.io/termmosaic.github.io](https://serkanalgur.github.io/termmosaic.github.io/);
  the row now records that the plan and captures live in this repository and
  the built site serves there. Documentation-only.

### Known Limitations

- **Retired: "the colour quantiser is unvalidated".** The entries in the 0.1.0
  and 0.2.0 sections below — the redmean mapping "has never been checked for
  perceptual acceptability, treat the 256 and 16 rungs as provisional" — are
  **no longer true**: the check was performed, it failed, and the quantiser was
  replaced (see Fixed above). Those entries stay where they were written, as
  history; this is the entry that says so.

### Open at this release — decided by nobody, recorded here

These three are **not settled**. v1.0.0 does not close them, and reading this
section as closure would be reading the maintainer's mind, which this document
does not do.

- **`widgets/widgettest` is public and therefore frozen at v1.0 unless the
  release notes exclude it — and these do not exclude it, so v1.0.0's promise
  covers it by default.** It was written for the project's own tests, it happens
  to live in a public package, and v1.0.0's stability promise lands on it like
  any other exported identifier. `docs/STATUS.md`'s "Two stability hazards"
  section names the hazard and names two options — promote it to a decided
  surface with its own rules, or move it under `internal/` before v1.0.0 — and
  picks **neither**. That is an open product decision, not a documentation gap:
  whether this release *should* have frozen that surface is undecided, and this
  release decides it for nobody. A later release can still move it, but only by
  breaking something v1.0.0 promised.
- **The Windows CI leg still runs as a full `test (windows-2025)` matrix leg.**
  [ADR 0001](docs/adr/0001-backend-strategy.md) says it "goes or is relabelled
  cross-compile-only"; that decision has **not been applied** — the workflow on
  this branch still runs the Windows leg as a test, not as a cross-compile
  check. Whether it goes or is relabelled is open.
- **`deleteBranchOnMerge` is false at the repository level,** so merged branches
  persist on the remote. No decision has been made about changing it. Branch
  `feat/menu-dialogs-input` is unmerged on the remote and is not addressed by
  this release.

---

## [0.7.0] — 2026-10-06

A minor bump, and the reason is a fourth example: **`examples/search`** — the
first with a focusable widget in it, and therefore the first place the catalog
and `keymap` meet under load.

### Added

- **`examples/search`** — a search-and-results screen on real Wikipedia data, no
  API key and no signup: a `form.TextInput` query field, a `data.Table` of
  results (article / words / updated), and a detail pane fed by the
  article-summary endpoint. `data.Table` over `data.List` because the word counts
  span two orders of magnitude, and a right-aligned fixed column lets the eye
  find the longest article by shape; a `List` renders one string per item and
  would have had the spacing built in by hand.
- **Nine golden files and 74 tests**, all asserting on **cells** through the
  headless harness. No escape-sequence assertion anywhere.
- **`--offline`** runs the whole screen on a transcribed 2026-10-05 capture of a
  real response, mirroring `examples/markets`' `source` interface with four
  implementations (live, offline, empty, failed). The snippet markup is stored
  **raw** and stripped in the view layer, so the offline path pins the same
  stripping the live path does. The capture's clock is a pinned constant, not
  `time.Now()`, because a golden reading the wall clock fails every run.

### What it establishes

- **Context-dependence is expressed with `Command.Enabled`, not `ScopeFocus`.**
  No binding in the example is focus-scoped, and that is a finding rather than a
  simplification. No catalog widget implements `keymap.Commandable`, so a
  focus-scoped binding would mean the *application* declaring keys on a widget's
  behalf with an owner it picked — which is exactly what `Commandable` exists to
  stop being necessary for. `Enabled` is documented as "an unavailable command is
  not run by a key press", and `Dispatch` skips it and keeps looking, so a
  screen-scoped arrow binding with `Enabled` false while the table has focus is
  simply not claimed and the event falls through to the tree.
- **`TextInput` declines `KeyUp`/`KeyDown`/`KeyEnter`/`KeyTab` deliberately**, so
  the screen owns them. `TestNoScreenBindingStealsAFocusedWidgetsKey` walks every
  binding against a **per-pane** list of widget-owned chords and fails if the
  registry claims one — the per-pane split being the whole point, since
  `TextInput` declines the arrows and `Table` consumes them.
- **`Home`/`End` are deliberately unbound.** Both widgets claim the bare forms —
  the field moves the caret, the table selects first and last — and a screen
  binding outranks both, so binding them would silently break both. The ring's
  ends are `Ctrl+Home`/`Ctrl+End`, which neither widget consumes.
- **`q` has to decline.** ADR 0009 §2 asks the registry before the tree, so a
  global `q` reaches the command before the field sees it. Without a decline this
  would be a search box you cannot type "quit" into. The decline checks that the
  field has focus **and** that the chord is an unmodified printable, because
  `Ctrl-C` arrives as `Ctrl+'c'` — a printable rune with a modifier — and must
  always quit.

### Known Limitations

- **`Registry.SetFocus` is now evidenced, not hypothetical.** `Search` worked
  around it by tracking focus itself and filtering the hint on `km.Has`, which
  means **the one query an application makes when focus changes is the one query
  it cannot make**. `TestDescribeScopeFocusCannotNarrowToTheFocusedWidget` pins
  the gap so the workaround cannot be quietly deleted and the gap cannot be
  quietly forgotten. Still deferred to v1.1.
- **`Describe(ScopeFocus)` is over-inclusive, not incomplete.** `inScope` returns
  true for an exact-scope match without consulting liveness, so a focus query
  reports every focus-scoped binding whatever has focus. Harmless for a help
  screen, which arguably wants the superset; wrong for a hint. One line, once
  `SetFocus` exists.
- **`data.Table` has no `Ascii` flag for its selection marker**, so its default
  `›` has no ASCII rung and would leak onto a terminal whose caps report no
  Unicode. The example overrides the marker instead, which makes it an
  application decision rather than a per-widget one.
- **`TextInput` does not expose its horizontal scroll offset**, so the example
  computes the caret cell from the rune index and clamps. Exact for any query
  shorter than the field, approximate beyond.
- **The arrow step from the field to the table is one-way.** The table consumes
  `Up`, so at its first row `Up` moves nothing and the way back is `Shift-Tab`.
  A ring whose arrows worked both ways would need the table to *decline* `Up` at
  row 0, which is not something a widget can express. Asserted, not hidden.
- `term/terminal_windows_test.go` is still **compile-only** verified
  (`GOOS=windows go vet`) and has never been executed; the Windows backend still
  runs zero tests at runtime.

## [0.6.1] — 2026-10-05

A minor bump, and the reason is that the first application to actually use
`keymap` found a shape the specification had not considered.

### Added

- **`keymap.Registry.DescribeGrouped(scope)`** — one `Entry` per **command**,
  carrying every chord in scope for it in canonical order. `Describe` remains
  one `Entry` per **chord**, which is what ADR 0009 §9 specifies for a command
  palette, where a row consumes `Chords[0]`.
  The difference is the consumer, not the data: `form.KeyHint.SetEntries` joins
  an entry's chords into one label, so a hint line fed `Describe` printed a
  three-chord command's description **three times**. `examples/hello` worked
  around it with its own thirteen-line merge; that is now a framework function
  and the workaround is gone.
- Both orderings are inherited rather than re-sorted: `DescribeGrouped` merges
  `Describe`'s already-sorted rows, so entry order and chord order cannot drift
  apart from each other or from `Describe`.

### Changed

- **`examples/hello` dispatches through `keymap`.** Its key contract is a real
  registry — six commands, twelve chords, with `q`/`Esc`/`Ctrl+c` and `?` at
  `ScopeGlobal` and the navigation at `ScopeScreen` — and both the pinned hint
  line and the `?` overlay render from the registry. This is the first use of
  `KeyHint.SetEntries` in the tree, and it **retires risk 5 of ADR 0009**.
  The hand-written hint string and the test that checked it against `Handle`
  are both gone: a binding and its description are now written **once**, and
  `Widget.Handle` claims nothing.
  The navigation keys are at `ScopeScreen`, not `ScopeFocus`, deliberately:
  nothing in this example holds keyboard focus, so focus-scoping them would make
  them go silently dead the moment anything else took focus. Screen scope
  degrades correctly — a focused child added later binds its own arrows and
  outranks them by specificity, with no change here.

### Known Limitations

- **`Registry` has no `SetFocus`,** so `Describe(ScopeFocus)` returns an
  incomplete answer before the first dispatch — the registry only learns what
  is focused by dispatching. An application with real focusable widgets cannot
  yet answer "what can I do right now" for the focused one. Deferred to v1.1;
  `examples/hello` avoids it by using `ScopeScreen`.
- **`Attach` must be called even when no widget implements `Commandable`,**
  purely so a scope-bound owner is `IsAttached`, because a registry that
  reports no `Warnings` requires it.
- **The palette is still not built.** ADR 0009 §9 scopes it out deliberately.
- **`examples/markets` and `examples/dashboard` still dispatch by their own
  `switch`,** so the mixed-mechanism risk ADR 0009 names is live in two of
  three examples.
- `term/terminal_windows_test.go` is still **compile-only** verified
  (`GOOS=windows go vet`) and has never been executed; the Windows backend still
  runs zero tests at runtime.

## [0.6.0] — 2026-10-05

A minor bump, and the reason is the longest-deferred item in the project:
**`keymap`, specified by [ADR 0009](docs/adr/0009-command-and-keymap.md) and
accepted on 2026-10-05, was implemented.** It was targeted at v0.4.0 and
shipped in neither v0.3.0 nor v0.4.0; `docs/STATUS.md` carried an apology
paragraph about that. This is the paragraph's replacement.

### Added

- **`keymap` — named commands, and a key is one way to invoke one.** A new
  package holding `Command`, `CommandID`, `Binding`, `Entry`, `Ctx` and a
  16-byte comparable `Chord`, with `ParseChord`/`ChordOf` as the single
  notation function in both directions.
- **Resolution by context specificity: focus, then screen, then global, with no
  numeric priority.** `Dispatch` walks a pre-built candidate slice in rank
  order, which is what lets both `Enabled` and `Run` decline and fall through.
  Ties break on registration order.
- **`Dispatch` is 0 allocs/op on every event kind**, measured and pinned by
  `TestDispatchIsZeroAllocation` across all eleven paths in ADR 0009 §2's table
  — miss, match with `Enabled` nil, match with `Enabled` non-nil, `Run`
  declining, paste, resize, and each mouse case. The benchmark reports 91 ns/op
  for a hit and 29.5 ns/op for a miss, both zero-alloc.
- **`Describe` as the single source of discoverability data**, plus `Chords`,
  and `KeyHint.SetEntries` so a widget's help renders from the registry rather
  than from a hand-maintained second list.
- **`Commandable` and `Clickable`, both optional.** A widget that implements
  neither is fully supported; nothing in the catalog implements them yet, which
  is the deferred half ADR 0009 §8 scoped separately.

### Changed

- **`Widget.Handle`'s doc comment** now states the precedence: events reach a
  widget only after the application's keymap has declined them. **No method was
  added, changed or deprecated**, and the interface is byte-identical.
- ADR 0009 gained five corrections where its code did not compile or
  contradicted itself; each is recorded in the ADR and in code. The two worth
  naming:
  - **`Ctx` is 152 bytes, not the 128 the prose claimed.** The field list is the
    specification and it sums to 145, padded to 152; 128 was reachable only as
    `Event` + `Chord` with neither `Focus` nor `Synthesised`. Pinned by
    `TestCtxIsOneHundredFiftyTwoBytes`, with the arithmetic in the comment.
  - **The precedence table contradicted itself on overrides.** One table row put
    a user override above every scope, while its own justification, the section
    headed *"Why an override does not outrank a more specific scope"*, and the
    Consequences section all say the opposite. **Specificity wins, and an
    override is the within-scope tiebreak** — three passages against one row, and
    it is the only reading under which a user's global `Esc` does not steal a
    dialog's.

### Known Limitations

- **A key the keymap consumes shadows a widget's own `switch`.** This is
  documented on `Widget.Handle` and is the designed outcome, not a defect: a
  `ScopeFocus` binding is the fix, and it goes inert when focus moves. The
  registry is told a widget's bounds and its published chords, never what its
  `Handle` does, so `Warnings` **cannot** report the overlap. `TestTheShadowedKeyIsSilent`
  asserts that silence deliberately, so the limit is recorded rather than
  implied.
- **No real terminal has met `Chord`'s folding rules.**
  `TestParseChordRoundTrips` walks 467 chords, but that is the specification
  checking itself. `Ctrl+k` and `Ctrl+K` are two chords because a kitty terminal
  reports them as two gestures.
- **Three scopes may be too coarse.** Unchanged from the ADR's own risk list.
- **No palette.** ADR 0009 §9 puts a `Ctrl+K` palette in scope but explicitly
  not in that ADR. No persistence, no leader keys, no drag-as-command — all
  deferred by §8.
- `Registry` has no `Unregister`, so a command renamed at runtime leaves a
  chordless row in help. Visible rather than silent.
- `term/terminal_windows_test.go` is still **compile-only** verified
  (`GOOS=windows go vet`) and has never been executed; the Windows backend still
  runs zero tests at runtime.

## [0.5.2] — 2026-10-05

A minor bump, and the reason is a decision with a number attached:
[ADR 0010](docs/adr/0010-mouse-routing.md) settles who receives a mouse
event, and the answer exposed three widgets that were getting it wrong.

### Fixed

- **`form.Tabs`, `form.Select` and `form.Radio` consumed a wheel notch
  regardless of where the pointer was.** `Tabs.Handle` tested the wheel
  before the `switch ev.Kind`, so a notch never reached the bounds check its
  click path already used; `Select` and `Radio` reached the same place through
  the shared `optionlist` helper. A tab row, a select or a radio group
  therefore took the wheel from whatever sat beneath it.
  `docs/STATUS.md` recorded this only for `Tabs` — it is three widgets, and
  the shared helper is where the fix belongs, so a future fourth is correct by
  construction.

  **Per ADR 0010: a widget handles a pointer event only when the pointer is
  inside its `Bounds()`.** Two exemptions are stated rather than left
  implicit — a *release* ends a drag wherever the pointer is, and a *drag*
  continues outside `Bounds` once a press has claimed it, because the press is
  the claim and the drag is the continuation.

- **`examples/markets` behaviour changes.** `d.pair` is a `form.Tabs` and is
  first in the focus ring, so it swallowed **every** wheel notch in the
  application. The example carried a workaround loop arguing the lenient rule
  was "defensible for a form"; the loop stays, because what it actually buys
  is the rule that the wheel never takes focus — which no widget can do alone.
  A wheel notch over a KPI tile, which no widget in the ring owns, is now
  consumed by nobody rather than scrolling the tab row by three.

### Added

- **ADR 0010, Mouse routing.** Records that hit-testing belongs to the widget
  and not to the application or the framework, and why the alternative — a
  routing helper, or an optional `Hittable` interface — was rejected: it is new
  exported API against an interface ADR 0007 and ADR 0009 both freeze, it needs
  a tree walk `Widget` cannot express because there is no `Children()`, and it
  can only answer "which rect" where a widget answers "which cell means what".
  This is the decision that gives ADR 0009 §6 — mouse hit-testing being "the
  one thing widgets are genuinely better at than a global registry" — teeth in
  the shipped catalog rather than only in the design.

  It also states which widgets decline a wheel outright: `Button`, `Checkbox`,
  `Toggle` and `Split` have nothing to scroll, and `TextInput`/`TextArea`
  decline **by decision** — `TextArea` wheel-to-scroll is a plausible feature,
  but adding one under a routing ADR would be answering a different question,
  and it will be bounds-correct when it lands because the rule is now written
  down.

### Known Limitations

- `split.Split` consumes a wheel notch over a pane and moves focus to the pane
  under the pointer. That is in-bounds behaviour consistent with a click, and a
  `Split` has no content of its own to scroll, so it is unchanged — but it is
  stated here rather than left to be discovered.
- `term/terminal_windows_test.go` is still **compile-only** verified
  (`GOOS=windows go vet`) and has never been executed; the Windows backend still
  runs zero tests at runtime.

## [0.5.1] — 2026-10-05

A minor bump, and the reason is seven defects: **the style a widget computes for
one cell was stopping where two code paths diverged, and never reaching the text
beside it.** Three of the five releases before this existed because of this one
class.

### Fixed

- **`Dialog`: `ChoiceFocusStyle` never reached the choice label.** The row was
  filled and marked in the focus style while its *text* was written in the
  unfocused style, because the label cache is built before focus is known. With
  default styles the focused choice rendered `attr=none` on an `attr=reverse`
  row — unreadable dark-on-dark on exactly the row the reader is meant to look
  at. `drawActions` already had this right via a second cached rendition;
  `drawChoices` now does the same.
- **`Button`: `FocusStyle` and `DisabledStyle` reached the ring and the fill but
  not the label**, so a disabled button rendered blue brackets around
  default-coloured text. The field's own documentation already said the style was
  "the style of the whole button — background, label and brackets"; the code did
  not do that. One function now computes the whole-button rendition, so the
  cached label and the fill cannot diverge.
- **`Table`: `SelectedStyle` and `ItemStyle` filled the row but never reached the
  cell text**, so a selected row showed terminal-default text on the
  terminal-default background in the middle of a highlighted row. The row style
  now overrides a single-span cell, matching `List` and `Tree`. **The trade:** on
  the selected row a cell's own `Style` and its column's `CellStyle` do not show.
  A cell carrying several spans keeps them, as `drawCell` documents.
- **`Table`: `HeaderStyle` was documented as "patched with `ItemStyle`" and never
  was.** `HeadingStyle` is attribute-only, so the header text resolved to the
  terminal background inside a row just filled with `ItemStyle`. It is patched
  now. Note a visible consequence of fixing both: the header text also carries
  `ItemStyle`'s **foreground**, because `Patch` takes FG as well as BG. That is
  `Patch`'s documented semantics and matches the header's stated intent of
  sitting on the row background, but it will show for a caller who set `ItemStyle`
  with a distinct FG.
- **`Menu`: the check glyph and the submenu arrow were drawn in `ItemStyle` on the
  selected row.** The marker already had the right fallback — the file's own
  comment names the hazard: "a marker in ItemStyle on a reversed row would be the
  one unreadable thing". The check glyph and submenu arrow did not, and now do.
- **`Radio`: the focus gutter was styled as a focus mark on every row.** A blank
  cell carried `styles.focus`, which inherits `SelectedStyle`'s `AttrReverse`, so
  an unfocused group painted a reverse-video stripe down its left edge. The
  column still exists on every row so labels align; only the rendition was wrong.
- **`BarChart`: axis labels overran their column.** The centring offset was
  computed and discarded with `_ = lx`, and the label was capped to the whole
  axis row, so a label wider than its column overwrote the next category's. The
  horizontal path already did this correctly.
- **`paintRow`'s multi-span exception is now documented at both call sites**
  (`List`, `Tree`), not only in the helper. A reader auditing `List` at the call
  site would conclude `ItemStyle` reaches every row; for a multi-span item it
  does not.

### Known Limitations

- `term/terminal_windows_test.go` is still **compile-only** verified
  (`GOOS=windows go vet`) and has never been executed; the Windows backend still
  runs zero tests at runtime.
- The cache-audit gate covers the transitions it names. Other exported raw fields
  remain the same shape and produce no finding today; see the v0.5.0 entry.

## [0.5.0] — 2026-10-05

A minor bump, and the reason is the first section: **five exported fields are
now private.** Programs that assign them directly will not compile.

### Breaking

- **`Pager.Status`, `Split.Spacing`, `Meter.ShowValue`, `ProgressBar.Label` and
  `ProgressBar.Percentage` are no longer exported fields.** Each had a working
  setter already, so migration is mechanical:

  | Before | After |
  |---|---|
  | `p.Status = on` | `p.SetStatus(on)` |
  | `s.Spacing = n` | `s.SetSpacing(n)` |
  | `m.ShowValue = on` | `m.SetShowValue(on)` |
  | `p.Label = s` | `p.SetLabel(s, st)` |
  | `p.Percentage = on` | `p.SetPercentage(on)` |

  Read-only accessors exist too: `Status()`, `Spacing()`, `ShowValue()`,
  `Percentage()`, `Label()`.

  The reason is in [ADR 0007](docs/adr/0007-responsive-screens.md) §3: a widget
  caches its derived layout keyed on `Bounds()`, so a field that changes without
  an `Invalidate()` produces a stale layout that **nothing ever repairs** — the
  rect does not change, so the cache keeps hitting. A doc comment saying "call
  `Invalidate` after assigning" is a rule with no enforcement, no compile error
  and no reminder. `ProgressBar` is the proof: `SetLabel` already reset the cache
  correctly, and the field was still assignable, so the API taught the wrong
  lesson by having both.

  The new setters are behaviour-preserving on a widget that has not yet drawn,
  and strictly better on one that has.

### Fixed

- **Eight widgets kept a stale layout cache after a documented setter.** Found by
  the new cache-audit mode, not by review: `Pager.SetStatus`, `Select.SetMarker`,
  `BarChart.SetData`, `Meter.SetShowValue`, `ProgressBar.SetLabel`/`SetLabelSpans`/
  `SetPercentage`, `Sparkline.SetValues`, and `Split.Spacing` via direct
  assignment. Each now drops the cached derivation, and each has a regression
  test that fails without the fix.

### Added

- **A cache-audit mode**, specified as ADR 0007 §3's deferred "expensive half":
  it corrupts a widget's cached derivation after a `Draw` and asserts the next
  frame is byte-identical, so this defect class is caught mechanically instead of
  by review. Two mechanisms, because one provably does not catch the class — the
  poison check in `render`, and a cold-twin comparison in `widgettest`. Opt-in via
  `render.Config.CacheAudit`, and **zero-allocation when disabled**, pinned by
  `TestRenderIsAllocationFreeWithCacheAuditDisabled`.
- **`widgets/cacheaudit`** now **fails the build** when a widget in the catalog
  is flagged. It runs on every push and pull request on all three platforms via
  the existing `go test ./... -race` job.

### Known Limitations

- The cache-audit gate covers the transitions it names. Other exported raw fields
  remain — `Select.Marker`, `Gauge.ShowValue`, `BarChart.ShowValue`/`Vertical`/
  `Data`, `Sparkline.Values`/`Braille`, `TextInput.Placeholder`,
  `Checkbox.TriState`, and the `Scrollbar`/`Header` fields on `List`/`Table`/
  `Tree`. None produced a finding, so none is a confirmed defect. For the fields
  other than `Scrollbar`/`Header`, the reason is still that no transition names
  them. For `Scrollbar`/`Header` that reason is stale — transitions do name
  them, at `widgets/cacheaudit/catalog_test.go:151`, `:166`, `:167` and `:195`
  — but the `Scrollbar` transitions are vacuous, so the gate cannot detect a
  stale `barW`: `List` and `Tree` default `Scrollbar` to false
  (`widgets/data/list.go:141`, `widgets/data/tree.go:134`; neither constructor
  assigns it), so setting the field to false is a no-op; and `Table`'s audit
  subject has zero rows when its `Scrollbar` transition applies
  (`widgets/cacheaudit/catalog_test.go:156-166`; `SetRows` comes later at
  `:168`), so `MaxOffset() <= 0` and the thumb never paints
  (`widgets/data/data.go:199-201`).
- `term/terminal_windows_test.go` is still **compile-only** verified
  (`GOOS=windows go vet`) and has never been executed; the Windows backend still
  runs zero tests at runtime.

## [0.4.1] — 2026-10-05

A patch release, and the reason is the only change in it: **the `golang.org/x/term`
pin moves forward to the newest release that still supports Go 1.23.** No
TermMosaic code changed and no behaviour changed, which is precisely what a patch
release is for under the policy above.

### Changed

- **`golang.org/x/term` v0.27.0 → v0.29.0, `golang.org/x/sys` v0.28.0 →
  v0.30.0.** Bisecting `x/term`'s declared `go` directive showed v0.29.0 is the
  newest release keeping the `go 1.23` floor: v0.30.0 declares `go 1.23.0`,
  v0.35.0 and v0.40.0 declare `go 1.24.0`, and v0.46.0 (`@latest`) declares
  `go 1.26.0`. The Go 1.23 floor in `go.mod` and CI is **unchanged and
  preserved**; build, vet, gofmt, lint, the Windows cross-build and the full
  `-race` suite are green on this version.

### Known Limitations

- **The `golang.org/x/term` pin is still required.** v0.29.0 is the ceiling, not
  a new floor — the pin stays until `x/term` offers a release compatible with a
  Go version this project has separately agreed to adopt. Re-checking it remains
  a manual, ongoing cost, exactly as ADR 0001 records.

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

[Unreleased]: https://github.com/serkanalgur/termmosaic/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v1.0.0
[0.7.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.7.0
[0.6.1]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.6.1
[0.6.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.6.0
[0.5.2]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.5.2
[0.5.1]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.5.1
[0.5.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.5.0
[0.4.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.4.0
[0.3.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.3.0
[0.2.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.2.0
[0.1.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.1.0
