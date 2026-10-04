# Documentation site plan

Status: **PROPOSED** — awaiting approval. Nothing in this document is built.
Author: architecture pass, 2026-10-04.

This is the plan for a public documentation site for TermMosaic, to be served
from GitHub Pages. It covers the site generator, the page tree, the screenshot
strategy, the per-widget page template, the example strategy, effort, and what
deliberately will not be built.

It is a plan, not a proposal in the ADR sense. If it is approved, it earns its
own ADR (`0009-documentation-site.md`) once the capture tool exists, because the
capture tool makes a real architectural claim — that documentation output is
derived from tested code — and that claim belongs in the ADR record next to the
other seven.

---

## 0. Findings that constrain the design

Four facts in the repository decide most of this, and they are worth stating
before the recommendations so the recommendations can be checked against them.

**0.1 — The widget count in the README is wrong, and the site must not repeat it.**
`README.md` says "24 widget constructors, built and tested" and its table lists
`Buffer` among the widgets. `buffer.Buffer` has `Invalidate()` but no `Bounds`,
`Draw` or `Handle`, so it does **not** satisfy `termmosaic.Widget` — it is the
thing widgets draw *into*, not a widget. `layout.Layout` is a solver and
`buffer.Span` is a value. Counting only types with an exported widget
constructor and no other:

| Category | Count | Types |
|---|---|---|
| Core | 4 | `Block` `Text` `Paragraph` `Split` |
| Forms | 9 | `TextInput` `TextArea` `Select` `Checkbox` `Radio` `Toggle` `Tabs` `Button` `KeyHint` |
| Data | 4 | `List` `Table` `Tree` `Pager` |
| Visualization | 5 | `ProgressBar` `Gauge` `Meter` `Sparkline` `BarChart` |
| **Total** | **22** | |

(`widgets/form/optionlist.go` is an unexported shared helper behind `Select`,
`Tabs` and `KeyHint`; it is not a 23rd widget. The 27 `func New*` hits in
`widgets/` include four `New*String` convenience variants and `NewBinding`,
which returns a value rather than a widget.)

**This is not a nitpick.** The site's entire value proposition is a widget
catalog, and the count is the headline number. A site that says 24 and a reader
who counts 22 loses trust in every other number on it. The site states **22**,
and correcting `README.md` and `STATUS.md` is a separate, small, prerequisite
task — flagged in §8, not done here, because those files are out of scope for
this pass.

**0.2 — The repository already contains a capture harness, and it is the good
one.** `widgets/widgettest/widgettest.go` exists precisely so that "every widget
test renders through `Render` rather than through `Draw`, and asserts on cells."
`Render` drives the full stack — widget → buffer → renderer → two-tier diff → ANSI
encoder → `MemorySink` screen model — and **fails the test** if the encoder emits
a sequence the screen model does not implement, because "a screen reconstructed
from an incomplete model makes every assertion against it unsound."

That is the whole capture mechanism. It is already written, it is already
exercised by 650+ tests, and it refuses to produce a misleading picture. See §4.

**0.3 — The cell carries everything a picture needs.** ADR 0002's `Cell` is
exactly 16 bytes: `Ch rune`, `FG Colour`, `BG Colour`, `Attr Attr`, `flags`.
`MemorySink.Cells()` returns them as a flat row-major slice; `Colour.Hex()` and
`Attr.Has()` render them. So a faithful colour capture is a straight
cell → HTML translation with **no third-party converter and no lossy step**.

**0.4 — A browser TTY is a declared non-goal.** `docs/ARCHITECTURE.md` §Non-goals
says, in full: "**Not a web framework.** No HTTP server, no browser target, no
WASM build." Option 4 (live WASM) is therefore not merely expensive, it
contradicts a written non-goal. Reopening that non-goal would itself need an ADR
and, given what the terminal layer actually is, is a rewrite: `term/terminal_unix.go`
is a direct `golang.org/x/sys` binding and `term/terminal_windows.go` is a stub
that returns a loud error from every console operation (see `STATUS.md`, Windows
console support). There is no abstraction a wasm shim could slot into.

---

## 1. Decision: the generator is **Hugo**, with **Pagefind** for search

### 1.1 The single strongest reason

**The maintainer's toolchain is Go and `git clone && go build && go test`, and
Hugo is the only serious option that adds one static binary and no language
runtime.**

`docs/CONTRIBUTING.md` promises a contributor a three-line setup. Hugo is a
single ~20 MB file with no runtime at all — `brew install hugo`, or a committed
`.github/workflows` job that downloads it. Nothing else in the comparison keeps
that promise:

| Option | Cost to a Go maintainer | Verdict |
|---|---|---|
| **Hugo** | one static binary | **chosen** |
| MkDocs Material | Python 3 + `pip`; `mkdocs.yml` + theme config; good search and admonitions | close second |
| Docusaurus / Nextra | Node 18+, `package.json`, `node_modules`, lockfile, a JS toolchain to debug | rejected — highest ongoing cost, lowest fit |
| Next.js | Node + a server or a build pipeline + hydration | rejected |
| Plain Markdown + hand-rolled theme | free to start; you then own search, nav, TOC, code highlighting, versioning and a11y forever | rejected — the maintenance is the site |

### 1.2 The rest of the reasoning, briefly

- **Go code highlighting.** Hugo embeds Chroma. `go` is a first-class lexeme.
  No configuration, no plugin, no dependency to keep current. This matters more
  than usual here because most of the site's content is Go.
- **Search.** Hugo core ships no search. **Pagefind** is the answer, and it is
  also a single static binary that indexes the built HTML after the fact. It
  therefore preserves the "no language runtime" property that decided §1.1. If
  MkDocs Material were chosen instead, search is better and one fewer binary is
  needed — that is the entire delta, and it did not outweigh the toolchain fit.
- **Publishing.** Static output plus the official
  `actions/upload-pages-artifact` / `actions/deploy-pages` pair. No plugin, no
  service, no credentials beyond `GITHUB_TOKEN`.
- **Adding a page.** One Markdown file in `site/content/widgets/`, plus one line
  in `_index.md`'s `weight`. There is no schema and no component. For a project
  whose main contributor is one person, this is the property that decides
  whether the site stays alive past the first month.
- **Binary size / build speed.** Hugo builds the entire site in well under a
  second. MkDocs Material takes several seconds per run, which is felt
  constantly.
- **Versioning.** Hugo has no first-class versioning, and neither does MkDocs
  without a plugin. For a pre-alpha project with one version in the wild this is
  a non-issue; the plan's answer is that per-widget docs are generated from the
  **current** source, so a version selector would have to render from a
  checkout, not from the site. Deferred. Recorded here so it is a decision
  rather than an oversight.
- **Theming.** One `layouts/partials/` file for the capture renderer. Nothing
  else is custom. If the theme is replaced later, the captures move with it
  because they are a shortcode, not baked HTML.

### 1.3 Where the site lives

```
site/                      the Hugo site — the only new top-level directory
  hugo.toml
  content/                 hand-written prose
  layouts/                 one custom partial: the capture renderer
  assets/css/capture.css
```

Everything **generated** lands in `site/content/generated/` and is
git-ignored. Generated content is a build artifact, not source. This is
deliberate: a reader who opens a widget page in the repository should see the
*hand-written* half of it.

---

## 2. Decision: capture is **cells → HTML, generated in Go**

### 2.1 The decision

For every widget, the site renders the widget through `widgets/widgettest` at a
fixed set of screen sizes and converts the resulting `MemorySink.Cells()` grid
into HTML: one `<span>` per run of identical `(FG, BG, Attr)`, styled with
`color`, `background-color`, `font-weight`, `font-style`, `text-decoration`,
in a container with a hard `ch`-based grid.

**Each widget page carries two captures: a colour one and a plain-text one.**
The plain-text capture is the exact `MemorySink.String()` output in a `<pre>`,
which is what golden files already compare against. They are not redundant:
the plain-text one is exact, and the colour one is the persuasive one.

### 2.2 Why not the alternatives

| Option | Why not |
|---|---|
| **1. Recorded text only** | Rejected as the *sole* strategy, kept as the mandatory second capture. Truthful but the five `viz` widgets are the project's differentiator and half the argument for them is that they look good; monochrome `<pre>` blocks argue nothing. It is also strictly worse than option 2 at zero extra cost, because the colour information is already in the cells. |
| **2. ANSI → HTML/SVG via a converter** | Rejected **in the indirect form** and adopted in the direct form. Routing through an external converter means adding a dependency, and a converter only knows SGR — it cannot know that a Braille cell is a *value* rather than a glyph, so it will render a gauge's dial as an assortment of unrelated characters and look broken. Converting cells directly means the generator knows the difference and can, for example, render a Braille column as a fixed-width unit. It is also lossless where the converter is lossy. |
| **3. Committed PNG/SVG screenshots** | Rejected as the primary mechanism, retained as an optional extra. Requires a human at a real terminal per widget per size, cannot be regenerated in CI, and goes stale silently. However — see §2.4 — this is the *only* way to show a real terminal, and one committed hero screenshot of `examples/dashboard` on the landing page is worth having. |
| **4. Live WASM** | Rejected. `docs/ARCHITECTURE.md` lists "no WASM build" as a written non-goal (§0.4), and it would be a rewrite, not a port. |

### 2.3 What this cannot show — stated precisely

The site must say all of this, in those words, on the site itself:

- **No interaction.** A capture is one settled frame after N frames have been
  rendered. It cannot show a keypress, a selection moving, a cell flickering, a
  pager scrolling, or a tree expanding. *Every widget page must therefore link a
  runnable `examples/` program* — the capture is a still, the example is the
  widget. This is the largest gap and it is not closable.
- **No animation or timing.** Frame pacing, the 30–60 fps budget and
  `render.Pacer` are invisible.
- **No live resize.** But the site can do better than a live resize would: it
  emits a capture at **three fixed widths** (e.g. 40, 80, 120 columns) side by
  side, which is *more* informative than a GIF because each frame is exact and
  the widths are named. Degradation at narrow widths is the thing ADR 0007 is
  about, and this is how the site shows it.
- **No cursor.** `MemorySink.Cursor()` does report position and visibility, so
  the generator can mark it — but a static mark cannot show it blinking, and a
  terminal cursor sits *under* a glyph, not on top of it. Mark it as a hollow
  block and say what it is.
- **No terminal fidelity, and this is the honest risk.** What the site renders
  is the **cell grid the renderer produced**, not what a terminal emulator would
  paint. Specifically not shown: the user's font and its cell aspect ratio, their
  colour scheme, their background, wide-glyph behaviour in *their* terminal, and
  any terminal-side ligature or shaping. The mitigation is mandatory: the
  capture CSS pins a monospace face at a fixed `ch` unit with
  `font-variant-ligatures: none`, and the page says "this is the cell grid, not
  a screenshot of anyone's terminal."
- **Braille and block-element glyphs are the weak spot, and they are exactly the
  differentiator.** `Gauge` and `Sparkline` use Braille (U+2800–28FF); `Meter`,
  `ProgressBar`, `Gauge` and `BarChart` use Block Elements (U+2580–259F). In a
  web font these can render at a different advance width than the terminal gives
  them, which destroys the alignment that gives those five widgets their
  meaning. Mitigations, in order: (a) always ship the plain-text capture beside
  the colour one, so the shape is verifiable; (b) emit the Braille ramp's
  *intent* as a caption — "8 levels of vertical resolution"; (c) test the capture
  CSS against a font with known 1:1 cell metrics before launch. If this cannot
  be made to work, the `viz` pages fall back to plain text only, and the site
  says so on the page rather than shipping a broken-looking gauge.
- **No `NO_COLOR`, no 256/16 ladder.** These are encode-time concerns
  (ADR 0008) and are decided by `render.Config`, so they *can* be captured — the
  `examples/hello` test already writes `hello_truecolor.sgr`,
  `hello_256.sgr`, `hello_16.sgr` and `hello_nocolor.sgr`. Two of these are worth
  showing on the colour-model page, as a genuine feature demonstration. Not
  needed on every widget page.
- **Not shown: performance.** `BytesWritten()` and `Frames()` are available, and
  a "what this costs" line per widget is generated from the benchmark numbers
  already committed in `README.md` and the package tests. Static numbers, not
  graphs.

### 2.4 The committed-screenshot exception

The landing page gets **one** hand-taken screenshot: `examples/dashboard`,
captured in a real terminal, committed as an asset, regenerated by hand when the
dashboard changes. One, on one page, explicitly labelled as a real terminal.
Justification: it is the only thing on the site that is not a cell grid, it is
the project's best artefact, and a single manual asset does not rot the way 24
per-widget ones would.

`.gitattributes` already declares `*.png binary` and `*.svg text eol=lf`, so
assets are already anticipated.

### 2.5 The one code change this requires

`widgettest.Render` takes a `testing.TB`. A documentation generator is not a
test and has no `testing.TB`. Phase 1 therefore requires:

```go
// widgettest.Capture is Render without a testing.TB, for the docs generator.
func Capture(w, h, frames int, root termmosaic.Widget) (*headless.MemorySink, error)
```

with `Render` refactored to call it. This is ~10 lines in a non-`_test.go` file,
it adds no behaviour, and every existing test keeps using `Render`. It is the
**only** non-test source change the whole plan requires.

A second, smaller item: a **widget registry**, one entry per widget, because
constructors are not uniform — `NewList(r, items ...ListItem)` takes a variadic
of a concrete type and reflection cannot call it. The registry is a plain Go
slice of `{Name, Package, Construct func(buffer.Rect) termmosaic.Widget, Sizes []int}`.
It is also the thing that makes §7's drift test possible.

---

## 3. Information architecture

```
site/content/
├── _index.md                          landing: what it is, the 22-widget claim,
│                                      one real screenshot, install, honest status
├── getting-started/
│   ├── _index.md                      three-minute path, first program
│   ├── install.md                     Go 1.23+, CGO off, no module release yet
│   ├── quickstart.md                  hello world, annotated, runnable
│   └── your-first-app.md              terminal + renderer + input + loop, end to end
├── concepts/
│   ├── _index.md
│   ├── widget.md                      the 4-method Widget contract; Focusable,
│   │                                  Minimizable; what Draw may and may not do
│   ├── renderer.md                    hybrid mode, dirty rects, the diff, frames
│   ├── buffer.md                      Cell, Buffer, SubBuffer, Row/RowBytes
│   ├── layout.md                      Length/Min/Max/Percentage/Ratio/Fill, Solve,
│   │                                  order-insensitivity of Fill
│   ├── input.md                       Decode/Parser/Source, the event model
│   ├── styling.md                     Style, Span, the authoring rule, the footgun
│   ├── colour.md                      the ladder, NO_COLOR, the quantiser,
│   │                                  and that the 256/16 rungs are provisional
│   ├── text.md                        wrapping, truncation, wide glyphs
│   ├── responsiveness.md              ClampCount, Budget, Priority; the two rules
│   ├── borders.md                     the one vocabulary, one Block
│   └── accessibility.md               colour is never the only signal; what is
│                                      and is not provided (no reduced-motion gate)
├── widgets/                           22 pages, one per widget
│   ├── _index.md                      the catalog table, all 22, linked
│   ├── block.md  text.md  paragraph.md  split.md
│   ├── textinput.md  textarea.md  select.md  checkbox.md  radio.md
│   ├── toggle.md  tabs.md  button.md  keyhint.md
│   ├── list.md  table.md  tree.md  pager.md
│   └── progressbar.md  gauge.md  meter.md  sparkline.md  barchart.md
├── guides/
│   ├── _index.md
│   ├── composing.md                   Split + layout + Block; the composition contract
│   ├── forms.md                       the nine form widgets as one workflow
│   ├── data-at-scale.md               the virtual/ engine and the flat-cost claim
│   ├── dashboards.md                  building the examples/dashboard screen
│   ├── testing.md                     widgettest, headless.Terminal, golden files
│   └── performance.md                 what is measured, what is not
├── reference/
│   ├── api.md                         generated: every package, every export
│   └── events.md                      generated from event.go
├── adr/
│   ├── _index.md                      the 8 decisions at a glance
│   └── 0001..0008.md                  the ADRs, verbatim, with anchors
├── limitations.md                     THE page that must not be missing
└── faq.md
```

**Count:** 1 landing + 4 getting-started + 12 concepts + 22 widgets + 1 catalog
index + 6 guides + 2 reference + 9 ADR + 1 limitations + 1 FAQ = **59 pages**.

### 3.1 On the ADRs

The ADRs are **included verbatim**, not summarised. They are already written as
prose with headings, and 0005/0007/0008 run to 35–62 KB each, which is fine for
a reference section. Summarising them would create a second source of truth that
drifts — the exact failure mode §7 exists to prevent. `docs/adr/README.md`'s
"Decisions at a glance" becomes the ADR index page.

### 3.2 `limitations.md` is not an appendix

It is linked from the landing page, from every widget page's footer, and from the
FAQ. Contents, every item traceable to `STATUS.md`:

- **Pre-alpha. The API will break without notice before v1.0.0.** This sentence
  appears on the landing page, above the fold, and in the site footer.
- **Not a stable SemVer surface.** No release exists yet; `go get` on a checkout.
- **Windows is a stub.** `term/terminal_windows.go` returns a loud error from
  every console operation. CI compiles it; nothing runs on it.
- **No IME / composition.** Scoped out by ADR 0005 §7. Users composing CJK in a
  `TextInput` get *wrong* behaviour, not degraded behaviour — the text arrives
  as a burst of key events which inserts correctly but pollutes the undo stack
  one entry per character. `STATUS.md` is careful about this and the site must
  be equally careful: do not present it as parity with OpenTUI, and do not
  present it as a feature gap being closed.
- **tmux / screen DCS passthrough is a real gap.** Under tmux on a modern
  terminal, key and mouse reporting can be lost.
- **The 256 and 16 colour rungs are not perceptually validated.** The redmean
  quantiser has never been checked by a human eye. `buffer.Quantiser` is the
  escape hatch.
- **Wide characters and grapheme clusters are unbenchmarked.** The semantics are
  implemented (`flagContinuation`); no benchmark exercises them.
- **Four ADR 0007 §3 tests are still unwritten**, named in the ADR. A
  documentation site that publishes a resize story should not imply the resize
  has been observed against a real terminal being dragged. ADR 0007 says so
  itself.
- **Headless backend is v1, assertion surface partly open** (ADR 0001).
- **Kitty graphics: open, leaning no.**
- **The captures on this site are cell grids, not terminal screenshots.** §2.3.

---

## 4. The capture pipeline, end to end

```
widgets/widgettest.Render ──┐
                            │  (refactored to expose Capture without testing.TB)
                            ▼
                   internal/docsgen (new, Go)
                            │
     ┌──────────────────────┼───────────────────────┐
     ▼                      ▼                       ▼
 go/ast + go/doc        Capture(...)            bench numbers
 over widgets/**        at 3 sizes per         already in _test.go
     │                  widget                  and README
     │                      │                       │
     └──────────┬───────────┴───────────────────────┘
                ▼
     site/content/generated/<widget>.md   (git-ignored)
                │
                ▼
        Hugo + Pagefind  →  GitHub Pages
```

Two independent generators, deliberately:

- **The API generator** reads the AST. It never runs code. It cannot be wrong
  about a signature.
- **The capture generator** runs the widget. It cannot be wrong about what the
  widget draws.

Neither imports the other's model. A signature and a picture cannot disagree
about each other because they were produced by different programs reading
different things.

---

## 5. The per-widget page template

Every one of the 22 pages has the same thirteen sections, in this order. The
**Source** column is the load-bearing part of this table.

| # | Section | Source | Notes |
|---|---|---|---|
| 1 | Title + one-line purpose | **auto** | First sentence of the godoc. Already written for all 22. |
| 2 | Stability banner | **auto** | `PROPOSED`-style badge, pre-alpha, from `STATUS.md` policy. |
| 3 | Rendered output | **auto** | Colour capture + plain-text capture, at 3 widths. |
| 4 | Package context | **auto** | The package doc's relevant section — e.g. `widgets/data`'s "the rules every widget here obeys". Already written. |
| 5 | Constructing it | **auto** | Constructor signature + every exported field, with its own doc comment as the description. |
| 6 | `MinSize()` | **auto** | **Called, not parsed.** Construct at a rect, call `MinSize()`. Truthful even though the thresholds are private constants. |
| 7 | Key contract | **auto** | The godoc already has a `# Key contract` section written as an indented tab-and-space table (`up / down        move the selection by one row`). It parses. Present in 9 of 22 widgets; absent → section omitted, not blank. |
| 8 | Accessibility | **auto** | `# Accessibility` and/or `# Colour is never the only signal`. Present in ~12. |
| 9 | Cost | **auto** | `# Cost` section where present; otherwise the benchmark figures from the package's own `_test.go`. |
| 10 | Example | **hand** | 15–40 lines, and a link to a runnable `examples/` program. |
| 11 | When to use it / when not to | **hand** | The only section that requires a person. |
| 12 | Related | **auto** | Siblings in the same package, plus widgets whose godoc mentions this one. |
| 13 | Limitations | **auto** | From the global list, filtered to what applies to this widget. |

**Roughly 11 of 13 sections are generated. The per-widget human cost is two
sections plus a sanity read.** That is the difference between a 22-page catalog
being a week and being a month.

**What cannot be generated, and why it is still cheap:**

- **Key bindings are prose, not data.** Every widget dispatches with a
  `switch` over `termmosaic.Key` (`widgets/data/table.go` has 9 such cases,
  `textinput.go` 7, `select.go` 8). There is no binding table to read. The
  `# Key contract` godoc section is hand-written prose today — the generator
  renders it, a human wrote it, and that division is correct and should stay.
  Deriving bindings from the switch AST would produce *what the code checks*,
  not *what the user should press*, including the negative claims
  ("`KeyTab` is NOT consumed, for the same reason `List` does not consume it")
  that make the contract useful.
- **`MinSize` is genuinely callable.** `minTableW` and friends are private, but
  the constructor builds the `block.Block` that `MinSize` reads, so
  `NewTable(rect, cols).MinSize()` returns the real answer. This works today for
  all 22; verify per widget in Phase 1.

---

## 6. Examples: **both**, with one rule about which is which

There are 2 runnable programs for 22 widgets. `CONTRIBUTING.md` says "Every
widget needs: a test, a runnable example, and a documented public API. A widget
without an example is not done." So examples are not optional — they are an
existing, unwritten debt that the site will expose.

**The rule:**

- **Runnable `examples/` programs are the source of truth for anything involving
  interaction.** A still capture cannot show a keypress (§2.3), so any widget
  with a key contract needs a program a reader can run. The page's §10 embeds
  the *same file*, so the snippet on the page and the program on disk cannot
  differ.
- **Docs-only snippets are for composition.** Showing how `Split` and a `Table`
  sit inside a `layout.Solve` is a fragment, not a program, and forcing it into
  `examples/` would produce 20 files that are not applications.

**The two cannot drift, structurally:** a docs snippet that claims to be a
program is extracted from the file, not retyped. A docs-only snippet is marked as
a fragment and is exempt.

**The catalogue target:** 22 per-widget programs under `examples/widgets/<widget>/`
— small, single-purpose, each exiting on `q` — plus the two existing ones
(`hello`, `dashboard`) kept as the "real program" tier. **22 programs is roughly
3–5 days**, and it simultaneously closes a `CONTRIBUTING.md` requirement that is
currently unmet and gives every widget page an answer to "what does it feel
like".

The per-widget programs double as the capture fixtures: the generator builds the
same widget the program builds, so the picture on the page is the program's
first frame.

---

## 7. Drift prevention

The claim the whole design rests on is that **the docs cannot drift from the
behaviour**, because they are produced by the code that the tests assert on.
Three checks make that true rather than aspirational:

1. **Every capture path must already exist as a test assertion.** The generator
   and `widgettest` share one code path. If a capture would show something no
   test pins, that is a gap in the tests, and CI should say so. Phase 1 adds a
   check that every registry entry has at least one existing test rendering it.
2. **Generated content is git-ignored and rebuilt in CI.** A committed generated
   file can go stale silently. An ignored one cannot. The deploy workflow runs
   the generator and fails if the build produces no widget page.
3. **A drift test over the registry.** For each widget: the constructor
   signature recorded in the registry must match the AST, and `MinSize()` must
   return the value the docs publish. Signature drift fails CI, not a reader.

**What still can drift, and honestly so:** sections 10 and 11 — the example and
the judgement. No amount of tooling makes "when *not* to use a `Pager`" true
automatically. The mitigation is that those are the only two sections a human
writes, which means a reviewer reading a widget page knows exactly what to
check.

---

## 8. Implementation phases

Effort is in person-days for one maintainer, and assumes familiarity with this
codebase. Total **≈ 30 person-days (6 weeks)**. These are honest numbers for
"full documentation for 22 widgets with examples"; the first useful artefact
lands at day 6.

| Phase | Contents | Days | Cumulative |
|---|---|---|---|
| **0. Reconcile the claims** | Correct "24" → 22 in `README.md` and `STATUS.md`; decide what `Buffer`/`Span`/`Layout` are called on the site; add `limitations.md` content. **Do this first** — the site must not be built on a wrong headline. | 1 | 1 |
| **1. Capture tool** | `widgettest.Capture`; the widget registry; cells → HTML renderer + `capture.css`; text capture; 3-width capture. Verified on the 5 `viz` widgets first, because Braille is the risk. **This is the phase that can fail.** | 4 | 5 |
| **2. Site skeleton** | `site/` + Hugo + Pagefind + the deploy workflow. Landing page, real screenshot, install, quickstart. | 2 | 7 |
| **3. Core concepts** | The 12 `concepts/` pages. Mostly assembled from godoc that already exists; the honest work is `colour.md`, `responsiveness.md` and `accessibility.md`. | 4 | 11 |
| **4. Widget pages** | 22 pages. Sections 1–9 and 12–13 are generated; 10 and 11 are written. **≈ 0.5 day per widget** = 11 days, less for `Block`/`Text` which are simpler. | 11 | 22 |
| **5. Examples** | 22 `examples/widgets/*` programs; wire each into its page. | 4 | 26 |
| **6. Guides, ADRs, FAQ** | 6 guides, 9 ADR pages, `faq.md`, `limitations.md` published. | 3 | 29 |
| **7. Polish** | Keyboard navigation, contrast pass on the capture theme, the drift checks from §7, link checking, a README screenshot. | 2 | **31** |

**Phase 1 is the gate.** If faithful colour capture of Braille and block
elements cannot be made to look correct in a browser, the fallback is plain-text
captures only, which costs ~1 day and weakens the `viz` pages considerably.
Decide it in Phase 1, not Phase 4.

**What can be cut, in order, if this overruns:** per-widget guides (§6) →
versioning → the 3-width captures → the `NO_COLOR`/ladder demonstrations.

---

## 9. What NOT to build, and why

- **No live WASM playground.** A declared non-goal (§0.4) and a rewrite.
- **No interactive component of any kind.** Every capture is static. A
  "click-to-send-a-key" demo would need the whole runtime in a browser.
- **No version selector.** Pre-alpha, one version. See §1.2.
- **No site-search over the ADRs' prose beyond Pagefind's default.** They are
  reference, not tutorial.
- **No hand-written HTML.** Everything is a Markdown file, a shortcode, or
  generated. Hand-written HTML is how a docs site becomes unmaintainable.
- **No per-widget PNG screenshots.** §2.4 explains the exception and the limit.
- **No interactive code playground / WASM-editor.** Same reason as the first item.
- **No analytics, no cookies, no third-party anything.** A pre-alpha Go library
  does not need a cookie banner, and adding one is a liability.
- **No "coming soon" pages.** The catalog is 22 widgets and the docs say 22. A
  page listing unbuilt widgets invites the question the project is trying to
  avoid.
- **No `Form` container widget page.** `STATUS.md` is explicit that a `Form`
  type was deliberately *not* built (ADR 0004's solver plus `layout` covers
  composition). Documenting one would document a decision the project made
  against. The `guides/forms.md` page covers composition instead.
- **No per-widget ADR pages or widget design docs.** The ADRs are the record.
  Inventing a second narrative layer creates drift.

---

## 10. What would have to be rewritten rather than reused

Honest accounting of the existing documentation:

| Existing | Verdict |
|---|---|
| **All godoc in `widgets/**`, `buffer`, `layout`, `input`, `render`, `virtual`, `geometry`, `term`, `headless`** — 100% of exported identifiers documented, with `# Key contract`, `# Accessibility`, `# Cost`, `# Values` sections already written in a parseable shape | **Reused, heavily.** This is the single largest asset in the repo. Sections 1, 4, 5, 7, 8, 9 of the widget template are all godoc that already exists. |
| **`docs/adr/*.md`** (8 files, ~250 KB) | **Reused verbatim.** Copy, do not rewrite. Add anchors only. |
| **`docs/adr/README.md`** | **Reused** as the ADR index page, with the "Decisions at a glance" section promoted. |
| **`examples/hello`, `examples/dashboard`** | **Reused** as the §6 "real program" tier and as capture fixtures. |
| **`README.md`** | **Partially reusable.** The claims are the problem, not the prose: "24 widget constructors" is wrong, the `Buffer`-as-widget row is wrong, and "Thirty-plus widgets" in Design Pillar 1 is wrong. Also `docs/ARCHITECTURE.md` refers to "Not yet released as a module version" with a `go get` that cannot work yet. Rewriting the *claims* is Phase 0. |
| **`docs/ARCHITECTURE.md`** | **Must be rewritten or retired.** Its decision numbering (1–7, with 5 = colour and 6 = theme) does not match the ADR set (0005 = input, 0008 = style/theme/text), it still says colour is `OPEN` when `STATUS.md` says `PROPOSED`, and it duplicates content the ADRs own. The site should link the ADR index and **not** publish `ARCHITECTURE.md` as a separate page. If the file is kept in the repo it should become a short pointer to `docs/adr/`. Not done here: out of scope. |
| **`docs/CONTRIBUTING.md`** | **Reused as-is**, plus one added section on adding a docs page. Its promise of a low-friction path is the constraint §1.1 optimizes for. |
| **`docs/STATUS.md`** | **Reused as the source for `limitations.md` and the stability badges.** It is the best-written document in the repo. Needs one row added for the docs site, and the widget count corrected. |
| **`examples/hello/testdata/*.sgr`** | **Reused.** Four committed `.sgr` files plus two `.txt` screens are an existing capture corpus, and the 256/16/`NO_COLOR` variants are ready-made illustrations for the colour-model page. |
| **Nothing else exists.** | There is no tutorial, no widget documentation, no quickstart, no FAQ, no limitations page. `widgets/form/form.go`, `widgets/data/data.go` and `widgets/viz/viz.go` are the closest thing to per-category narrative and are excellent — they seed §4 of each widget page. |

**Net:** the *reasoning* is documented to a standard most projects never reach;
the *usage* is not documented at all. This plan is mostly about converting
existent prose into a browsable form and writing the half that does not exist.

---

## 11. Open questions for the maintainer

1. **Is the site worth it before v0.1?** `STATUS.md`'s "usable library" bar
   requires every widget to have a runnable example and a documented public
   API, but says nothing about a *site*. A pre-alpha project with 22 widgets and
   no users may get more from `examples/` plus good godoc on pkg.go.dev. This
   plan argues the site is worth it *because* the capture tool removes drift —
   but if the answer is "not yet", the fallback is Phase 0 + Phase 5 alone
   (5 days) and pkg.go.dev, which already renders this godoc well.
2. **Domain.** `serkanalgur.github.io/termmosaic` unless a custom domain is
   wanted.
3. **Does `examples/dashboard` change often?** If yes, the one committed
   screenshot (§2.4) needs a regeneration step and a staleness check; if no, it
   is done.
4. **Are the ADRs published verbatim, or curated?** §3.1 says verbatim. A
   62 KB page is a lot for a first-time reader who wants to know why `Fill` is
   order-insensitive. Verbatim with a good index is the recommendation.
5. **Who writes sections 10 and 11 for 22 widgets?** 11 days in §4's estimate
   assumes one person. This is the largest single block of prose in the plan and
   the part most likely to be quietly skipped.