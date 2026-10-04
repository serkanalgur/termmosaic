# 0008 — Style, theme, and text

- **Status:** Accepted
- **Date:** 2026-10-04
- **Decides:** [STATUS.md](../STATUS.md) — Core architecture / Theme and styling
  system; and the widget catalog's second open question — how styled text is
  written and wrapped.
- **Depends on:** [ADR 0001](0001-backend-strategy.md) (the colour ladder,
  `NO_COLOR`, "no terminfo"), [ADR 0002](0002-buffer-representation.md)
  (`Colour`, `Attr`, `Cell`, the 0-allocs frame path),
  [ADR 0003](0003-renderer-mode.md) (`Widget.Draw(buf *Buffer)` runs every frame
  for every widget), [ADR 0007](0007-responsive-screens.md) (§1 shared-vocabulary
  convention and the five collision rules)
- **Answers:** the widget catalog's open question — three coders are about to
  build 30+ independent widgets in parallel and every one of them will need to
  style text and draw a border. This document is the vocabulary they must use
  verbatim.

## Context

What exists today:

| Already decided | Where |
|---|---|
| `Colour` — packed `uint32` `0x00RRGGBB`, plus the `DefaultColour` sentinel, `Named16()`, `Index256()`, and a `Quantiser` interface | `buffer/colour.go` |
| `Attr` — `uint16` bitflags: `AttrBold`, `AttrFaint`, `AttrItalic`, `AttrUnderline`, `AttrReverse`, `AttrStrike` | `buffer/cell.go:15-41` |
| `Cell` — 16 bytes, padding-free: `Ch rune`, `FG Colour`, `BG Colour`, `Attr Attr`, private `flags uint16` | `buffer/cell.go:89-101` |
| `NewCell(ch, fg, bg, attr)`, `Set(x, y, r, fg, bg, attr)`, `SetString(x, y, s, fg, bg, attr)`, `ContinuationCell(fg, bg, attr)`, `SetCell`, `FillRect` | `buffer/cell.go`, `buffer/buffer.go` |
| `RuneWidth` / `StringWidth` — the narrow, documented-as-provisional width function | `buffer/width.go` |
| The degradation ladder `DepthTrueColor → Depth256 → Depth16 → DepthNone`, the `Quantiser` hook, and `Encoder.NoColor` | `buffer/colour.go:197-231`, `internal/ansi/ansi.go:152-175` |
| An `ansi.Style` struct `{FG, BG, Attr}` used by the diff to track current terminal state | `internal/ansi/ansi.go:135-150` |

What does **not** exist:

- **No `Style` type in the widget-facing API.** Style is currently spelled as
  three separate arguments at six call sites, so every widget that needs a fg, a
  bg and an attribute carries three variables and three assignment paths.
- **No text-span abstraction.** There is no way to write "this word bold, that
  word in red" without a widget hand-rolling it, and a hand-rolled version is
  three hand-rolled versions.
- **No border or title vocabulary.** `examples/hello/main.go:97-125` draws eight
  box-drawing glyphs and an inset title by hand, with its own guard thresholds
  (`r.W < 4`, `r.W < 16`). That code is the *only* border implementation in the
  repository and it is an example.
- **No theme abstraction, and no stated reason why not** — STATUS.md carries
  "Theme and styling system" as OPEN with an empty note.

### The cost of leaving this open, stated before the decision

Identical in shape to [ADR 0007](0007-responsive-screens.md) §"The cost of getting
this wrong". The failure mode is not one bad widget. It is:

- three `Style` definitions — one a struct, one a `[3]any`, one a pair of
  `Style{FG, BG}` / `Style{Attr}` types — so that a `Table` cannot take a
  `List`'s style, and no application-level type unifies them;
- three `Span` types, so an app's "styled label" is not usable by any widget but
  the one whose Span it is;
- three border glyph tables, so `BorderThick` in `Table` is not the same rune as
  `BorderThick` in `Block`;
- three default-colour opinions, none of which is documented, so "unset" means
  three different things and half the widgets render black backgrounds.

Every one of those is cheap to prevent now and very expensive after 30 widgets
exist, because the fix is a signature change on every widget rather than an edit
inside one.

## Options considered

### Decision 1 — `Style` as a struct, or three loose arguments?

**Three loose arguments (`fg, bg buffer.Colour, attr buffer.Attr`).**

- *Pros:* no new type; existing signatures unchanged; a caller can vary the
  attribute without touching the colours.
- *Cons, and they are the whole argument:* (a) it is **not self-documenting** —
  `Set(x, y, 'x', bg, fg, 0)` compiles and is wrong in a way only a careful
  reader catches, and `Colour` has `DefaultColour` as a sentinel so `fg` and
  `bg` are interchangeable at the type level; (b) it does not compose — a
  `List` cannot hold "the row style" without three fields; (c) it forces every
  widget to define its own aggregation rule for "inheriting some channels",
  which is the exact space three authors will diverge in; (d) it makes the
  patch/base operation impossible to express without `Colour` having an unset
  value, which it does not.
- The ergonomic claim in [ADR 0002](0002-buffer-representation.md) — "widget
  authors write `buf.Set(x, y, 'x', fg, bg, attr)`" — is about the *cell* being
  one comparable struct rather than four parallel planes. It is not a decision to
  keep three parameters forever, and this ADR revises the illustrative snippet
  without disturbing that decision (§Forced changes).

**A `Style` struct.**

- *Pros:* one value that can be stored, embedded, inherited, patched, compared
  with `==`, and passed as a single argument. One word in a struct field means
  "the style of this thing". It is also **exactly the projection of `Cell`'s
  three styling fields**, so it converts in both directions for free.
- *Cons:* (a) a partially filled composite literal is a silent black channel;
  (b) adding a field to `Style` is a breaking change for every widget that
  compares styles literally rather than field-wise; (c) a struct copy per cell on
  the hot path.

On (c), the load-bearing question, the numbers:

- `Style` is `{uint32, uint32, uint16}` = 10 bytes of data, 12 bytes with
  trailing padding.
- Go's register-based ABI (since 1.17) assigns **struct fields** to registers
  independently when the struct is small, so `f(st Style)` with a 3-field,
  12-byte struct consumes the same three integer registers as `f(fg, bg, attr)`.
  There is no memory traffic and **no allocation** either way: `Style` is passed
  **by value**, never by pointer, so no box escapes.
- The alternative to a value copy is not free either: a `*Style` field on a
  widget would need a stable address, would make `==` comparison a pointer
  comparison unless the pointee is also compared, and would put an indirection
  on the per-cell write.
- So the per-cell cost of `Style` is **exactly zero relative to loose arguments**
  and **strictly less than a pointer-based design**. The remaining question is
  whether 12 bytes of stack traffic per cell is measurable against ADR 0002's
  measured widget draw path (141 ns for 12,000 cells at 4.4 ns/cell), and it is
  not plausibly so: the loop is dominated by a bounds check, a stride multiply
  and a 16-byte store, and the style is in registers alongside them.

**Decision: a `Style` struct, passed by value, living in `buffer`.** The
consequences are that `Set`, `SetString`, `NewCell` and `ContinuationCell` all
take one `Style` instead of three loose parameters, so there is exactly **one**
way to style a cell in the public API.

### Where does `Style` live?

Three candidates, and the existing import graph decides it:

| Candidate | Verdict |
|---|---|
| `geometry` | **Impossible.** `Style` contains `Colour` and `Attr`, which are in `buffer`. `buffer` imports `geometry` (`buffer/buffer.go:3` for `Rect`), so `geometry` importing `buffer` is a cycle. This is exactly the reasoning that put `geometry` in the dependency-free leaf position, and it is why ADR 0007's vocabulary is there. |
| A new `style` or `theme` package | **Rejected for v1.** `Cell.Style()`, `NewCell` and `SetString` all need `Style`, so `buffer` would import it; and every widget would import both. A package containing one struct that the buffer itself depends on is not a boundary, it is an extra hop. Revisit only if a theme type needs a home of its own (§Deferred). |
| `buffer` | **Chosen.** `Style` is precisely the flat `FG`/`BG`/`Attr` projection of `Cell`; `RuneWidth` and the width functions it needs for text are already here; and `buffer` is imported by every layer that could want it. Nothing new is imported anywhere. |

**And `ansi.Style` becomes an alias, not a second type.** `internal/ansi`
already defines `Style{FG, BG, Attr}` and the diff tracks it in
`internal/diff/diff.go:83`. Leaving that struct alone would be exactly the
collision this ADR exists to prevent — two types with the same name, the same
fields and no assignability between them, one in a public package and one in an
internal one, where the internal one is what the frame actually compares.

### Decision 2 — `Buffer` span API, or widgets writing cells?

Hand-rolling a mixed-style run in each widget means: a bounds check per cell, a
copy of the continuation-cell logic, and a re-derivation of ADR 0002's wide-glyph
rules. Thirty of those is thirty chances to get the continuation flag wrong, and
getting it wrong is not a visual glitch — it is a row that never compares equal
and therefore never skips, i.e. flicker on every frame forever.

**Decision: `buffer` gains the span API. It is the only supported way to write
mixed-style text, and `SetString` keeps the uniform-text role it has today.**

**Parsed once, or per frame?** A `Span` slice carries no lazy parsing — the
strings are the strings — so the real question is whether a widget re-derives
its *wrapped lines* each frame. ADR 0007 §3 already answers the general form
("a widget that caches anything derived from its size caches it against the
rect it was computed for, and recomputes when `Bounds()` differs"). So:

> **Span construction and wrapping happen at construction or on size change, never
> inside `Draw`.** A widget holding static text builds its `[]Span` in its
> constructor. A widget holding text that may reflow calls `Wrap` **from its
> size-change check**, caches the `Wrapped` alongside the rect it was computed
> for, and `Draw` only reads `wrapped.Line(i)`.

`Wrap` allocates (it must — it builds lines), which is precisely why it is not
per-frame. ADR 0007's §6 cost table already puts whole-tree `Draw` at ~81 µs for
12,000 cells; a `Wrap` per frame on a 40-line paragraph is a different order of
magnitude and would be the catalog's first real performance regression.

**Wide characters at a span boundary.** A double-width glyph occupies two cells
and the second is a `ContinuationCell`. Decided:

- **A wide glyph is never split across spans.** It belongs wholly to the span
  containing its first rune, and the continuation cell takes **that span's
  style**, not the next span's. This is load-bearing: the two halves are written
  in the same frame with the same style, which is the only reason they compare
  equal to last frame's pair and the row skip still fires. A continuation cell
  wearing the *following* span's colour is a permanently dirty row.
- **A wide glyph that does not fit the remaining width is not written at all**
  and the run stops — the existing `SetString` rule (`buffer/buffer.go:186-189`),
  extended unchanged. Half a glyph is worse than none.
- **A wide glyph with only one cell of room in a one-column area is dropped**
  rather than wrapped, and if that empties the line the line is omitted. Same
  reason, same rule, stated once.

**Combining marks at a boundary.** `RuneWidth` reports 0 for combining marks and
`SetString` currently **drops** them (`buffer/buffer.go:179-181`). `SetSpans`
**keeps that behaviour**, deliberately, and does not attempt to attach a mark to
the preceding span: since grapheme clusters are not composed anywhere in this
codebase (`buffer/width.go:3-17` says so explicitly), attaching the mark to a
different span would not make it render — each cell is written independently — it
would only make the failure less obvious. This ADR does not change that
limitation; it inherits it and says so.

**Word-wrap and truncation against styling.** Two rules, both decided here
because they are the ones three authors get differently:

- **A span boundary is not a word boundary.** Break opportunities are computed
  on the concatenated text, so a word may straddle two spans ("Se" in one style,
  "lected" in another) and must not be split by the wrap. `Wrap` merges for
  break computation and splits back, preserving styles per rune.
- **The only break opportunity in v1 is U+0020 SPACE.** Not a hyphen, not a
  CJK ideograph boundary, not a slash. The space that causes a break is
  **consumed**; every other space is preserved verbatim, because trimming would
  silently change column alignment. An over-long word is cut at the cell
  boundary with **no marker added** — one component owns the marker, and it is
  `Truncate`, which a widget calls on the last line it can actually show.

### Decision 3 — a theme in v1?

The three candidates:

1. **A map of semantic role names to `Style`** (`theme.Border`, `theme.Title`,
   `theme.Selected`). Rejected for v1: it is a string-keyed lookup on the draw
   path, it makes "unset" a third state alongside "default" and "set", and a role
   set is only the right abstraction once there are enough roles for the *same*
   name to mean the same thing across widgets — which is not true today, because
   `Border` on a `Table` and `Border` on a `Button` are different visual
   decisions that happen to share a word.
2. **Every widget carries `Style` fields, no theme.** Chosen.
3. **Nothing at all — hard-coded colours per widget.** Rejected: that is the
   literal collision this ADR is here to prevent.

**Decision: no theme abstraction in v1.** Each styleable widget exposes exported
`Style` fields, and the framework ships a small, fixed set of named default
styles that are *values, not a registry*. Specifically, the framework's defaults
use **no colours at all** — every default style resolves to the terminal's own
foreground and background, and colour is entirely the application's choice. That
is the same reasoning ADR 0007 used to reject framework size classes: a default
palette is a product decision, and every TUI library that has shipped one has
been told by users that they should not have. Attributes are cheap, portable and
not a matter of taste, so those the framework does name.

The "no widget defines its own default" risk is real but it is **narrower than it
looks**, and this is why: a default only collides when two widgets share a
*concept*. In v1 each role is a field on the one widget that owns it —
`Block.BorderStyle`, `Table.HeaderStyle`, `List.SelectedStyle` — so there is
nothing to collide with. The roles that *would* collide (an "accent" shared by
`Button` and `Tabs`) are exactly the trigger for §Deferred.

### Decision 4 — border and title conventions

**The glyph tables live in `buffer` (`buffer/border.go`), not in a widget.** The
reason is a dependency one and it is the same shape as decision 1: `Block` is a
widget, so if the tables lived with `Block`, then a widget that draws a divider
would have to import the `Block` package to reach the runes — and `Block` itself,
if it ever wraps other widgets, is the natural cycle. Glyph tables are pure data
about a rendering convention with no state, exactly like `RuneWidth`, which
already lives in `buffer`. **`buffer` owns them; `Block` consumes them; no widget
draws a border by any other route.**

**The v1 set is five styles plus `None`.** `BorderDashed` and `BorderDotted` are
deliberately *not* in v1, and the reason is specific rather than cautious: there
is no clean Unicode pair for them. U+254C/U+254D are dashed **horizontal** lines
only, so a dashed border has no vertical member and every dashed border would
either draw a solid vertical edge or a rune that some fonts render as a
horizontal dash. Shipping a border style that cannot be drawn correctly in all
four orientations is worse than shipping four that can. Deferred with a trigger.

**The ASCII rule is one boolean and one function.** A widget holds a
`buffer.BorderStyle` and calls `.Glyphs(ascii)`. It **never** branches on
`!caps.Unicode` itself and never contains a box-drawing rune literal — every rune
comes from the table. `ascii` is exactly `caps.Unicode == false`, and `Caps.Unicode`
already exists (`term/caps.go:59-60`, the UTF-8 locale heuristic).

### Decision 5 — defaults and `NO_COLOR`

The zero-value problem is real and had to be solved rather than waved at:
`Colour(0)` is **black**, a perfectly valid colour, so `Style{}` is
`{black, black, no attributes}` — not an obviously-empty value. Three ways out:

| Option | Verdict |
|---|---|
| Make `Colour(0)` mean "unset" and require `NewColour` for black | **Rejected.** It contradicts [ADR 0002](0002-buffer-representation.md) explicitly ("`Colour` stays `uint32` `0x00RRGGBB`", "a colour is always the RGB colour it means") and would break `==` for every existing golden test. |
| Add an `Optional`/`Maybe` colour wrapper (`{c Colour; set bool}`) | **Rejected for v1.** It makes `Style` a 5-byte-per-channel type that no longer converts freely to `Cell`, it puts a branch on the per-cell path, and it is only needed because of a problem a sentinel solves. |
| Reserve the exact all-zero `Style` as the "unset" sentinel, and give `Style` a `Resolved()` that maps it to `DefaultStyle` | **Chosen.** One value, one function, one documented authoring rule: *a `Style` is built by `NewStyle` or by a `With*` method, never by a partial composite literal.* |

`NO_COLOR` needs no new work, and that is the point of the design: it is applied
**once, at encode time**, by `ansi.Encoder.NoColor`
(`internal/ansi/ansi.go:155-159`), and nothing in the widget or style path
consults it. Attributes survive `NO_COLOR` — that is already pinned by
`TestNoColorSuppressesColourKeepsAttributes`. Concretely:

- `Style.Resolved()` and `Colour.Named16()`/`Index256()` **must not** read
  `NO_COLOR`, the environment, or `Caps`. A widget that branches on caps is a
  widget whose output depends on a global.
- The 16-colour rung is likewise **encode-time only** (`Encoder.Depth`). A widget
  authors an RGB colour and the quantiser degrades it; it never pre-degrades.
  The corollary, and it is a rule: **`Style` contains no authored colour depth,
  and there is no `ColourFromIndex` in v1.** An app that wants exact palette
  control on a 16-colour terminal cannot get it in v0.x. Recorded in §Deferred
  with a trigger, because a theme (the thing that would care) is itself deferred.

## Decision

**One `Style` value bundles fg, bg and attributes. One `Span` type carries
styled text. Wrapping and truncation are parsed-once, cached-per-rect helpers in
`buffer`. Borders and titles have a shared glyph and threshold vocabulary owned
by `buffer`, consumed by a single `Block`. There is no theme in v1: widgets carry
`Style` fields, and the framework's defaults are the terminal's own colours.**

### 1. The shared vocabulary, by name and signature

**Three coders must use these names with these meanings and must not define
their own equivalents.** Everything below lives in **`buffer`** except `Align`,
which lives in `geometry` for the reason given in §Reconciliation.

```go
// ---------------------------------------------------------------------------
// Style — buffer/style.go
// ---------------------------------------------------------------------------

// Style is a cell's complete rendition: foreground, background and attributes.
// It is exactly the flat projection of a Cell's three styling fields, so
// converting in either direction is a field copy and no allocation.
//
// It is 12 bytes and is passed BY VALUE everywhere. Never take or store a
// *Style: a pointer needs a stable address, breaks == comparison, and puts an
// indirection on the per-cell write for nothing.
//
// The zero Style is a RESERVED SENTINEL meaning "the author did not choose",
// not a style. Its three fields would otherwise read as opaque black on
// black, which is a legitimate style nobody means by default. Construct a real
// style with NewStyle or with a With* method on DefaultStyle; a partially
// filled composite literal is a silent black channel and is a bug.
type Style struct {
	FG   Colour
	BG   Colour
	Attr Attr
}

// DefaultStyle is what an unset Style resolves to: the terminal's own
// foreground and background, no attributes. Every framework default style in
// this file is DefaultStyle plus attributes. The framework ships NO default
// colours in v1 — colour is entirely the application's choice.
var DefaultStyle = Style{FG: DefaultColour, BG: DefaultColour}

// NewStyle returns a Style, mapping an unset colour channel (UnsetColour) to
// the terminal default. It is the constructor widgets and applications should
// reach for.
func NewStyle(fg, bg Colour, attr Attr) Style

// WithFG, WithBG and WithAttr return a copy of s with one channel replaced.
// WithAttr ORs, so attribute intent accumulates along a builder chain:
//   DefaultStyle.WithBG(bg).WithAttr(buffer.AttrBold).WithFG(accent)
// This is the ONLY sanctioned way to derive a style from another.
func (s Style) WithFG(c Colour) Style
func (s Style) WithBG(c Colour) Style
func (s Style) WithAttr(a Attr) Style

// IsUnset reports whether s is exactly the zero Style.
func (s Style) IsUnset() bool

// Resolved returns s with an unset colour channel replaced by DefaultColour
// and is a no-op for every style built by NewStyle or a With* method. It is
// idempotent.
//
// THE IDIOM: every widget field of type Style is read through exactly this
// call — `st := w.BorderStyle.Resolved()` — so that "the author set nothing"
// has one meaning in the entire catalog. Do not substitute a nil check, a
// separate *Style field, or a per-widget default constant.
func (s Style) Resolved() Style

// Patch returns s with every channel o actually specifies, and ORs o.Attr into
// s.Attr. An unset channel in o leaves s's channel alone, which is what makes
// "bold and this foreground, inherited background" expressible without a
// parallel type hierarchy.
//
// Patch cannot REMOVE attributes — it only accumulates them. To clear one,
// build a Style explicitly. That asymmetry is deliberate: accumulation is the
// operation every caller wants, and removal is rare enough to be explicit.
func (s Style) Patch(o Style) Style

// Cell returns c rendered in this style, and Blank returns a space in it.
func (s Style) Cell(r rune) Cell
func (s Style) Blank() Cell

// Style returns the cell's rendition.
func (c Cell) Style() Style

// Framework default styles. Attributes only — see Decision 3. These are the
// only named styles the framework provides, and a widget that wants a bold
// header uses HeadingStyle rather than writing AttrBold|AttrUnderline itself.
var (
	PlainStyle    = Style{} // DefaultStyle, no attributes
	BoldStyle     = Style{Attr: AttrBold}
	FaintStyle    = Style{Attr: AttrFaint}
	ItalicStyle   = Style{Attr: AttrItalic}
	UnderlineStyle = Style{Attr: AttrUnderline}
	ReverseStyle  = Style{Attr: AttrReverse}
	StrikeStyle   = Style{Attr: AttrStrike}

	// Composed defaults, because these two are requested constantly and three
	// authors will otherwise each spell them differently.
	EmphasisStyle = Style{Attr: AttrBold}               // emphasis in a label or value
	HeadingStyle  = Style{Attr: AttrBold | AttrUnderline} // a column header or title
	MutedStyle    = Style{Attr: AttrFaint}              // secondary or disabled text
)
```

```go
// ---------------------------------------------------------------------------
// UnsetColour — buffer/colour.go
// ---------------------------------------------------------------------------

// UnsetColour is the "this channel was not specified" marker for Style.Patch
// and Style.Resolved. Like DefaultColour it sits outside the 0x00RRGGBB space,
// so it can never compare equal to a real colour.
//
// Its existence is what lets Patch work. Without it a colour has no way to say
// "inherit", because Colour(0) is a real colour (opaque black).
const UnsetColour Colour = 0xFFFFFFFE

// IsUnset reports whether c is the inherit marker.
func (c Colour) IsUnset() bool
```

```go
// ---------------------------------------------------------------------------
// Text — buffer/span.go
// ---------------------------------------------------------------------------

// Span is a run of text in one style. A widget holds []Span; it does not hold a
// string plus parallel style slices.
type Span struct {
	// Text is the run's content. Its cell width is buffer.StringWidth(Text),
	// which is 0 for empty text.
	Text string
	// Style is this run's rendition. An unset Style resolves to DefaultStyle;
	// use NewSpan rather than a bare literal.
	Style Style
}

// NewSpan returns a Span.
func NewSpan(text string, st Style) Span

// SpansWidth returns the total cell width of spans, summing StringWidth.
func SpansWidth(spans []Span) int

// TruncSuffix is the marker Truncate appends: U+2026, one cell wide.
const TruncSuffix = "…"

// AscTruncSuffix is its ASCII-rung counterpart: "~", also one cell wide. The
// two are the same width on purpose, so the width arithmetic a caller does is
// identical on both rungs and the choice of Truncate variant cannot shift a
// layout.
const AscTruncSuffix = "~"

// Truncate returns the longest prefix of spans fitting maxWidth, with
// TruncSuffix appended in the style of the last span kept. If spans already fit,
// it returns spans unchanged (same backing array, no allocation).
//
// It allocates when it truncates. Call it from the widget's size-change check
// and cache the result; never from Draw. maxWidth <= 0 returns an empty slice.
func Truncate(spans []Span, maxWidth int) []Span

// TruncateASCII is Truncate with AscTruncSuffix. A widget that already knows
// caps.Unicode is false picks this variant and gets an identical layout.
func TruncateASCII(spans []Span, maxWidth int) []Span

// Wrapped is the result of Wrap: styled lines, ready to index. It is immutable
// once built and safe to cache on a widget.
type Wrapped struct {
	// Lines holds each line as a slice of spans. A line may be empty.
	Lines [][]Span
	// Width is the column width Wrap was given.
	Width int
}

// Wrap breaks spans into lines of at most width cells, preserving each rune's
// style. It allocates Lines and every Line; build it on a size change and cache
// it, per ADR 0007 §3.
//
// The rules, all of which are the contract:
//   - The ONLY break opportunity in v1 is U+0020 SPACE. No hyphen breaks, no
//     CJK boundary breaks, no slash breaks.
//   - A span boundary is NOT a break opportunity. A word may straddle two spans
//     and is never split by the wrap.
//   - The space that causes a break is consumed. Every other space is preserved
//     verbatim, so trimming cannot silently change a column alignment.
//   - A word longer than width is cut at the cell boundary with NO marker.
//     Truncate owns the marker; a widget that wants "there is more" calls it on
//     the last line it can show.
//   - A double-width glyph is never split. One that does not fit the remaining
//     cells is dropped and the line ends; if that leaves the line empty the
//     line is omitted from Lines entirely.
//   - width <= 0 returns a Wrapped with no Lines and Height() == 0. It never
//     panics.
func Wrap(spans []Span, width int) Wrapped

// Height returns the number of lines, i.e. how many cells tall the wrapped
// text is.
func (w Wrapped) Height() int

// Line returns line i's spans, or nil if i is out of range.
func (w Wrapped) Line(i int) []Span
```

```go
// ---------------------------------------------------------------------------
// Border — buffer/border.go
// ---------------------------------------------------------------------------

// BorderStyle names one of the shared box-drawing glyph sets. A widget holds a
// BorderStyle and asks for glyphs; a widget NEVER contains a box-drawing rune
// literal.
type BorderStyle uint8

const (
	// BorderNone draws no border. Its glyph set is all-zero.
	BorderNone BorderStyle = iota
	// BorderPlain is the single-line set: ─ │ ┌ ┐ └ ┘ (U+2500, U+2502, U+250C,
	// U+2510, U+2514, U+2518).
	BorderPlain
	// BorderRounded is ─ │ with ╭ ╮ ╰ ╯ (U+2500, U+2502, U+256D, U+256E,
	// U+2570, U+256F).
	BorderRounded
	// BorderDouble is ═ ║ ╔ ╗ ╚ ╝ (U+2550, U+2551, U+2554, U+2557, U+255A,
	// U+255D).
	BorderDouble
	// BorderThick is ━ ┃ ┏ ┓ ┗ ┛ (U+2501, U+2503, U+250F, U+2513, U+2517,
	// U+251B).
	BorderThick
	// BorderASCII is - | and + for every corner, tee and cross. It is the
	// degradation target for every other style, not a style in its own right:
	// a widget asks for BorderPlain and gets ASCII when the terminal cannot do
	// Unicode. It is nonetheless a named constant so tests can assert it.
	BorderASCII
)

// BorderGlyphs is the six-glyph rectangle plus the five divider glyphs a Tree
// or a Table needs.
//
// A zero rune means "this set has no glyph for this position" and the renderer
// draws NOTHING there. That is deliberate: BorderRounded and BorderThick have
// no tee or cross glyphs in Unicode, and drawing a mismatched rune is worse
// than drawing none. A widget that needs a divider therefore picks a set that
// has one (BorderPlain or BorderDouble) and documents that choice.
type BorderGlyphs struct {
	TopLeft, TopRight, BottomLeft, BottomRight rune
	Horizontal, Vertical                       rune
	TeeDown, TeeUp, TeeRight, TeeLeft, Cross   rune
}

// Glyphs returns the glyph set for s. ascii == true returns the BorderASCII
// table for EVERY style — a single boolean switches the whole catalog, and
// there is no per-glyph fallback.
//
// This is the ONLY box-drawing lookup in TermMosaic. The one boolean the widget
// path passes is `caps.Unicode == false`.
func (s BorderStyle) Glyphs(ascii bool) BorderGlyphs

// String returns the style's name for diagnostics and golden-test failure
// messages: "none", "plain", "rounded", "double", "thick", "ascii".
func (s BorderStyle) String() string
```

```go
// ---------------------------------------------------------------------------
// Align — geometry/align.go
// ---------------------------------------------------------------------------

// Align is horizontal placement within a span of cells. It is in geometry
// because it is a pure enumeration over a cell range with no dependency on
// anything, and because geometry is the leaf both buffer and any text-rendering
// code can import.
type Align uint8

const (
	AlignLeft Align = iota // the default; the zero value
	AlignCenter
	AlignRight
)
```

And the changed `buffer` write API — **one way to style a cell, not three
parameters:**

```go
// NewCell returns a Cell with the given rune in st.
func NewCell(ch rune, st Style) Cell

// ContinuationCell returns the right half of a double-width glyph in st. Its
// style MUST match the glyph cell it follows; see SetSpans.
func ContinuationCell(st Style) Cell

// Set writes a styled rune at (x, y). Out-of-range writes are silently
// ignored (unchanged).
func (b *Buffer) Set(x, y int, r rune, st Style)

// SetString writes s starting at (x, y) in st and returns the x coordinate just
// past the last cell written. It is SetSpans of one span; it exists because
// uniform text is the common case and one span should not cost a slice.
//
// Wide-character behaviour is exactly SetSpans': a double-width rune consumes
// two cells and its continuation half carries the SAME style; a rune that
// would straddle the right edge is not written at all.
func (b *Buffer) SetString(x, y int, s string, st Style) int

// SetSpans writes spans left to right on row y starting at x, in each span's
// own style, and returns the x coordinate just past the last cell written.
//
// It writes ONE row. Wrapping is not this function's job — the caller wraps
// with Wrap and writes one line per row, which is what keeps the wrapping
// cacheable per rect (ADR 0007 §3).
//
// Rules, all of which are the contract:
//   - A span's Style is resolved through Style.Resolved() exactly once, on
//     entry. An unset span style is DefaultStyle.
//   - A double-width rune writes two cells and the continuation half takes
//     THE SAME SPAN'S STYLE. Never the next span's. A mismatched continuation
//     cell does not compare equal to the previous frame and the row skip never
//     fires — permanent flicker on that row.
//   - A double-width rune that does not fit before the right edge is not
//     written and the function returns.
//   - Zero-width runes are DROPPED, matching SetString and RuneWidth's
//     documented lack of grapheme composition. A combining mark is not
//     attached to the preceding span, because each cell is written
//     independently and it would not render anyway.
//   - Writes outside the buffer are silently ignored, per SetCell.
//
// 0 allocations. It takes a slice and never builds one.
func (b *Buffer) SetSpans(x, y int, spans []Span) int
```

### 2. Block and titles — the one owner of borders

**Exactly one `Block` exists, and it is the only thing in the catalog that draws
a border or a title.** `List`, `Table`, `Tree`, `Pager`, `Tabs` and every other
chrome-bearing widget compose it or call it; none of them contains a glyph
table, a corner loop, or a title threshold. `examples/hello/main.go:97-125` is
replaced by `Block` and stops drawing its own border.

`Block` is a widget, so it is in the widget catalog's package, not in `buffer`.
Its **contract** is fixed here even though its fields are the catalog's to
choose, because the thresholds and the placement rules are what three authors
would otherwise each invent:

- **Title placement.** The title occupies the **top border row** and
  **overwrites** the border cells — it is not drawn inside the box on its own
  row, and it is not blended with the border glyphs.
- **Title padding.** `Block` inserts **one space on each side** automatically.
  A title of `"My title"` renders as `" My title "`. Widgets do not add the
  spaces themselves, and `examples/hello`'s `" termmosaic "` literal becomes
  `"termmosaic"`.
- **Title style.** `[]Span`, so a title can be mixed-style, via
  `SetTitle([]Span)`. `SetTitleString(s string, st Style)` is the uniform
  convenience.
- **Title alignment.** One `geometry.Align` field, applied within the interior
  span **between the two corner cells**, so interior width is `W - 2` and the
  title's own width is `width + 2`. `AlignLeft` is the default.
- **Title thresholds — exact, because three authors pick their own:**
  - A border needs `W >= 2 && H >= 2`. Below that, `Block` draws no border and
    no title; the widget's own content still draws (ADR 0007's "clip, never
    blank").
  - A title additionally needs `W >= 5`: two corners, plus a space, plus one
    glyph, plus a space.
- **An over-long title is TRUNCATED, not dropped**, with `Truncate` /
  `TruncateASCII` so the marker appears and the user learns there was more.
  This follows ADR 0007's "clip, never blank": dropping a title entirely while
  there is still room loses information precisely when information is scarce.
- **Padding is uniform in v1.** One `Padding int` field, applied to all four
  sides with the existing `geometry.Rect.Inset`. Per-side padding is deferred
  (§Deferred) — `Tabs` is the known claimant.
- **Sub-buffers.** A `Block` that fills its background or border in a
  sub-buffer must compose with `SubBuffer` and let the renderer diff at the top
  (ADR 0002, ADR 0006). It must never call `RowBytes`.

### 3. Defaults, `NO_COLOR`, and the degradation rungs — the decided rules

| Question | Answer |
|---|---|
| A widget field of type `Style` is left zero | Resolves to `buffer.DefaultStyle`, read through **exactly** `w.Field.Resolved()`. Terminal's own fg and bg, no attributes. |
| A widget wants bold for a header | `buffer.HeadingStyle`, or `DefaultStyle.WithAttr(buffer.AttrBold)` when it also has a colour. Never a literal `Style{Attr: …}` in a widget. |
| A widget wants a colour | The application's own colour, set through `NewStyle` or a `With*` method. **The framework ships no default colours.** |
| `NO_COLOR` is set | Nothing in the widget, style or span path changes or knows. `ansi.Encoder.NoColor` suppresses colour SGR at emit time; attributes are still emitted. Already pinned by `TestNoColorSuppressesColourKeepsAttributes`. |
| Terminal is 16-colour | Degradation happens **at encode time** via `Encoder.Depth` and `buffer.Quantiser`. A widget authors RGB and never pre-degrades. `Style` has no authored colour depth. |
| Terminal is dumb / `TERM=dumb` | `Caps.ColourDepth()` already resolves to `Depth16` (`term/caps.go:44`). Same encode-time path. |
| Locale is not UTF-8 | `Caps.Unicode == false`, so every border uses `Glyphs(true)` and truncation uses `TruncateASCII`. **This is the only capability a widget consults**, and it is a single boolean passed to a shared function. |
| A widget wants a dashed border | Not available in v1. Deferred (§Deferred). |

### 4. Parsed-once discipline — the rule that keeps `Draw` at 0 allocs

Stated as its own rule because it is the one most likely to be broken by three
people at once:

> **`Draw` never allocates. Anything derived from a widget's text — spans,
> wrapped lines, truncation, measured widths — is computed at construction or in
> the widget's size-change check, keyed on the rect, and read in `Draw`.**

`Draw` may: resolve styles, index cached lines, `SetSpans`, `SetString`,
`SetCell`, `FillRect`, `SetStyledString`-equivalent calls, and read `Bounds()`.
`Draw` may not: call `Wrap`, call `Truncate`, build a `[]Span`, call
`geometry.Budget`, or format a string. `geometry.Budget` is under the same rule
by ADR 0007 §3, and the reason is identical: both allocate.

### Reconciliation with ADR 0007

ADR 0007 §1 establishes the convention this ADR follows, and states its five
collision rules. Checked against each:

| ADR 0007 rule | Status here |
|---|---|
| 1. A widget's available space is `Bounds()`, never `buf.Size()` | **Unchanged and still binding.** `Wrap`'s width argument is the widget's content width derived from `Bounds()`, never `buf.Width()`. `Truncate`'s `maxWidth` likewise. |
| 2. `Bounds()` is clipped to the screen before `Draw` sees it | **Unchanged.** `SetSpans`/`SetString` bound-check per cell regardless, so composition is safe at any nesting depth. |
| 3. A widget repaints its entire `Bounds()` before drawing content | **Unchanged, and now cheaper to satisfy.** `Block` fills its rect via `FillRect(r, st.Blank())`; `Style.Blank()` is the sanctioned way to express the background, so every widget paints its chrome background the same way. |
| 4. No widget defines a local `clamp`, `fit`, `minRows`, `Priority`, or `Budget` | **Extended, not contradicted.** The list is about responsive helpers. The same principle is applied here: **no widget defines a local `Style`, `Span`, `Wrap`, `Truncate`, `truncate`, `truncateString`, border glyph table, or title threshold.** If a shared helper is missing something, that is a bug in this ADR, reported and fixed here. |
| 5. Thresholds are local named constants, not framework vocabulary | **Consistent, with one deliberate exception.** Widget-specific thresholds (how many columns a Table shows at width 40) stay local. The *box-drawing and truncation* thresholds are not widget-specific — `W >= 2`, `W >= 5`, one space each side of a title — and three authors would each pick their own, which is the exact collision this ADR exists to prevent. They are decided here. |

**Placement, which is the one place a reader could think this ADR contradicts
ADR 0007.** ADR 0007 §1 says "All three live in `geometry`, which today imports
nothing and is depended on by both `buffer` and `layout` — so adding them there
is free of cycles and requires no import changes anywhere."

That sentence is about **its** three, and its stated reason — free of cycles —
is exactly what excludes `Style` from `geometry`. `Style` contains `Colour` and
`Attr`, which live in `buffer`; `buffer` imports `geometry` for `Rect`; so
`geometry` importing `buffer` is a cycle. `ClampCount`, `Priority`, `Region` and
`Budget` are cell-free arithmetic and belong in the leaf; `Style` is a
projection of `Cell` and belongs beside it. **`Align` is the exception that
proves the rule** — a pure enumeration over a cell range with no dependency —
and it does go in `geometry`.

**The generalised rule, added by this ADR, in one sentence: shared widget
vocabulary lives in the lowest package that can hold it without an import
cycle — `geometry` for cell-free geometry, `buffer` for anything that touches
`Colour`, `Attr`, `Cell` or a rune.** No import changes are required anywhere:
`geometry` gains one file, `buffer` gains three, and no package gains a
dependency it did not have.

**ADR 0007 is not amended.** Its §1 wording is scoped to its own vocabulary by
its own phrasing ("All three"), its rule 4 enumerates responsive helpers, and
none of its five rules is contradicted by anything here.

### Scope: v1, deferred, and triggers

**In v1, and that is the whole of it:**

- `buffer.Style`, `NewStyle`, `WithFG`/`WithBG`/`WithAttr`, `IsUnset`,
  `Resolved`, `Patch`, `Cell`, `Blank`, `DefaultStyle`, and the named
  attribute-only styles.
- `buffer.UnsetColour`, `Colour.IsUnset`.
- `Cell.Style()`, and the `Style`-taking forms of `NewCell`,
  `ContinuationCell`, `Set`, `SetString`.
- `buffer.Span`, `NewSpan`, `SpansWidth`, `Truncate`, `TruncateASCII`,
  `TruncSuffix`, `AscTruncSuffix`.
- `buffer.Wrapped`, `Wrap`, `Height`, `Line`.
- `Buffer.SetSpans`.
- `buffer.BorderStyle`, the six constants, `BorderGlyphs`, `Glyphs`,
  `String`.
- `geometry.Align`, `AlignLeft`/`AlignCenter`/`AlignRight`.
- One `Block`, owning every border and title in the catalog.
- The parsed-once rule in §4, and the extended rule 4 in §Reconciliation.
- `ansi.Style` becoming an alias of `buffer.Style`.

**No new package. `layout` is unchanged. `geometry` keeps importing nothing.
`termmosaic.Widget` is unchanged** — as in ADR 0007, none of this needed an
interface change, because a style is data a widget stores, not behaviour the
renderer needs from it.

| Deferred | Trigger to revisit |
|---|---|
| **A theme type** (`type Theme struct{ … }` of `Style` fields, plus a `SetTheme` or a `WithTheme` option) | **The first time two widgets need a role the first widget's own field name does not express** — an "accent" shared by `Button` and `Tabs` is the canonical case; or the first application that overrides five or more unrelated styles in one place; or the first request for light/dark. Then: a `Theme` struct of `Style` fields (not a map — a map makes "unset" a third state and puts a string-keyed lookup on the draw path), resolved through `Style.Patch`, and every widget resolving its field as `w.Field.Patch(theme.Field).Resolved()` so that **a theme can override anything and a zero theme changes nothing**. |
| **Role names as a vocabulary** (`theme.Border`, `theme.Selected`) | Follows the theme. Until roles are shared, naming them is premature — `Block.BorderStyle` and `Button.SelectedStyle` are fields on the widget that owns them and cannot collide. |
| **A framework default colour palette** | Never, on our own initiative. If an application repeatedly re-declares the same four colours, the answer is a *documented example palette* in `examples/`, not a framework opinion. |
| **`ColourFromIndex(i uint8) Colour`** — author a palette index rather than an RGB value | An application needing exact control on a 16-colour terminal, or the theme type landing (a theme authored per depth is the case ADR 0002 already flagged). Until then, `Colour` is always the RGB colour it means. |
| **Dashed and dotted border sets** | A widget needing a "pending" / "in-progress" / "diff" edge distinction — `ProgressBar`, or a `Pager` showing a modified flag. When it comes, it needs the missing vertical dashed glyph resolved first, probably by drawing the dashed edge as an alternating run of the solid glyph and a space rather than by inventing a rune. |
| **Per-side padding on `Block`** | `Tabs` needing an underline bar without a bottom border, or any widget with asymmetric chrome. `geometry.Rect.Inset` is uniform by design; asymmetric padding is `Inset(1)` plus explicit field arithmetic until it is decided. |
| **Grapheme-cluster composition, and therefore correct combining-mark rendering** | Internationalization being scoped. ADR 0002 and `buffer/width.go` both already record this, and this ADR **inherits** the limitation rather than half-solving it: zero-width runes are dropped. |
| **Ragged-right / centred / justified multi-line text** | A `Text` or `Line` widget needing `WrapAlign` rather than a single `Align`. `Wrap` is left-aligned in v1; adding justification changes `Wrap`'s contract and therefore its cached results. |
| **Style in the layout engine** (a colour or attribute on a `layout.Constraint`) | Never, on our own initiative. It is CSS-shaped, and ADR 0004 chose a closed, predictable constraint set on purpose. |

## Forced changes to existing code

| Identifier | Current | After this ADR |
|---|---|---|
| `buffer.Set` | `Set(x, y, r, fg, bg, attr)` | `Set(x, y, r, st Style)` |
| `buffer.SetString` | `SetString(x, y, s, fg, bg, attr)` | `SetString(x, y, s, st Style)`; gains `SetSpans` as the mixed-style form |
| `buffer.NewCell` | `NewCell(ch, fg, bg, attr)` | `NewCell(ch, st Style)` |
| `buffer.ContinuationCell` | `ContinuationCell(fg, bg, attr)` | `ContinuationCell(st Style)` |
| `buffer.Cell` | flat `Ch, FG, BG, Attr, flags` | **unchanged, and must stay exactly 16 bytes.** `Style` is NOT embedded in `Cell`: it is the 10-byte projection, and embedding it would make the cell 28+ bytes with interior padding, which ADR 0002's `TestCellHasNoPadding` correctly fails. `Cell.Style()` and `Style.Cell(r)` convert. |
| `ansi.Style` | its own `{FG, BG, Attr}` struct | `type Style = buffer.Style` — an **alias**, so the diff's style tracking and the widget-facing type are the same type. `ansi.StyleOf(c)` becomes `c.Style()`. |
| `internal/diff.Differ.style` | `ansi.Style` | unchanged in source; identical by aliasing |
| `term.Caps` | `Unicode bool` already present (`term/caps.go:59`) | **unchanged.** It is the ASCII rung's only input. |
| `geometry` | `Rect`, `Size` | gains `Align` and the three constants; still imports nothing |
| `layout` | — | **unchanged** |
| `termmosaic.Widget` | 4 methods | **unchanged** |
| `examples/hello/main.go:102-125` | hand-drawn border + `" termmosaic "` title with `r.W < 4` / `r.W < 16` guards | replaced by a `Block`; the example stops owning border glyphs and title thresholds |
| `examples/hello/main.go:90` | `buffer.NewCell(' ', fg, bg, 0)` | `st.Blank()` |
| ADR 0002's illustrative snippet `buf.Set(x, y, 'x', fg, bg, attr)` | prose example | **stale after this ADR; its decision is unaffected.** Recorded here rather than by editing ADR 0002, whose reasoning about cell ergonomics stands. |

Tests to add, in ADR 0002/0007's spirit:

- `TestStyleIsTwelveBytesAndComparable` — pins the size and that `==` works.
- `TestCellRemainsSixteenBytesWithStyle` — `unsafe.Sizeof(Cell{}) == 16`
  **after** `Style` exists, which is the guard against someone "simplifying" by
  embedding it.
- `TestStyleZeroIsUnsetAndResolvesToDefaultStyle` — `Style{}.IsUnset()`,
  `Style{}.Resolved() == DefaultStyle`, and `Resolved` idempotent.
- `TestStylePatchUsesUnsetColourOnly` — a patch with `UnsetColour` inherits; a
  patch with opaque black overrides. This is the test that pins the
  sentinel's reason for existing.
- `TestSetSpansWritesEachSpanInItsOwnStyle` — including a span boundary landing
  mid-word.
- `TestSetSpansWideGlyphContinuationTakesOwningSpanStyle` — the flicker
  invariant: assert the continuation cell's style equals the glyph cell's, not
  the following span's.
- `TestSetSpansDropsWideGlyphThatDoesNotFit` — no partial glyph, no unpaired
  cell.
- `TestSetSpansDoesNotAllocate` — `testing.AllocsPerRun` == 0, which is the
  test that keeps this API on the frame path.
- `TestWrapNeverSplitsWideGlyphOrSpanBoundaryWord` — including a word
  straddling two spans.
- `TestWrapConsumesTheBreakSpaceAndPreservesOthers`.
- `TestWrapZeroWidthIsEmptyNotPanic`.
- `TestTruncateAppendsMarkerInLastSpanStyle` and that both suffix constants are
  one cell wide.
- `TestWrapAndTruncateAreAllocatingAndThusNotForDraw` — pins the §4 rule from
  the other side: the helpers allocate, so the widget's `Draw` benchmark must
  show 0.
- `TestEveryBorderStyleHasSixDistinctGlyphs` and
  `TestBorderGlyphsAsciiIsIndependentOfStyle` — one table, every style, so a new
  style cannot be added with a missing corner.
- `TestBlockTitleTruncatesRatherThanDrops`, `TestBlockNoBorderBelow2x2`,
  `TestBlockNoTitleBelowWidth5` — the exact thresholds of §2.
- `TestNoColorLeavesEveryWidgetStylePathUnchanged` — render a styled span tree
  with `NoColor: true` and assert only the byte stream differs, which is what
  keeps Decision 3's "nothing in the widget path knows" honest.

## Consequences

**Good**

- **One style, one span, one border table, one `Block`.** A `Table`'s header
  style is assignable to a `List`'s; an application's styled label works in any
  widget; `BorderThick` is the same rune everywhere.
- **The theme question is answered without a theme.** The dangerous version of
  "no theme" is hard-coded colours in thirty widgets; §Decision 3 removes it by
  making the framework's default *the terminal's own colours*, so the failure
  mode cannot occur.
- **`Style` is free.** Twelve bytes in registers, by value, no allocation,
  identical machine cost to the three loose arguments it replaces — so ADR 0002's
  0-allocs frame path and its ~4.4 ns/cell draw measurement are unaffected, and
  the ergonomics of one argument are gained rather than paid for.
- **Caching has one rule instead of two.** "Derived from text is parsed once" and
  "derived from size is cached per rect" (ADR 0007 §3) are the same discipline,
  so a coder learns it once.
- **`ansi.Style` collapsing into `buffer.Style` removes a real latent bug
  class**: a widget style that is `==`-comparable but not assignable to what the
  diff tracks is a class of comparison that would silently never be true.
- **The ASCII rung is one boolean.** There is exactly one place in the catalog
  that knows the terminal is not UTF-8, and it is a shared function call rather
  than a `if !caps.Unicode` sprinkled through thirty widgets.

**Bad — stated plainly**

- **`Style{}` as a reserved sentinel is a footgun with a working safe side.** A
  partially filled composite literal — `Style{FG: accent}` — is opaque black on
  black, and it compiles. `NewStyle` and the `With*` methods exist to make the
  safe path shorter than the unsafe one, and `Resolved()` is what makes the
  failure a black background rather than a wrong foreground. This is the ADR's
  sharpest edge and the one most likely to be hit.
- **`Wrap` and `Truncate` allocate**, and the rule keeping them out of `Draw` is
  documentation, not a type — the identical hazard ADR 0007 §7 names for
  `Budget`. Nothing stops an author from calling `Wrap` inside `Draw`. This is
  **the most likely performance regression in the catalog**, and it is the first
  thing to look for in a draw-path `AllocsPerRun`.
- **No theme in v1 means an application restyles thirty widgets by touching
  thirty fields.** For a first application that is fine; for a user wanting a
  dark theme across an existing app it is thirty edits. Accepted deliberately,
  and §Deferred names the trigger precisely so it is a known deferral rather
  than an omission.
- **No default colours means a bare widget is invisible-ish.** A `List` with no
  styling is plain text on the terminal's background, which is correct and
  boring. It is also indistinguishable from "the theme did not load".
- **`AttrReverse` interacts with `bg` in ways three authors will reason about
  differently** (does `BG` mean the cell's background or the paper behind the
  text?). The encoder emits both independently and the terminal decides. Not
  resolved here; recorded so nobody resolves it locally either.
- **Dropping dashed borders** means a widget that wants one either picks `Double`
  or invents a rune. Inventing a rune is the failure this ADR prevents, so the
  trigger table is written to steer away from it.
- **`SetSpans` writes one row**, so a caller that wants a paragraph must wrap
  first — which is correct for caching but is an extra step that a hand-rolled
  implementation would not have. Deliberate.

## Rejected alternatives, specifically

- **Loose `fg, bg, attr` parameters everywhere** — not self-documenting where
  `fg` and `bg` are the same type with a sentinel; does not compose into a
  field a widget can store; and forces every widget to invent its own
  inheritance rule. Rejected in §Decision 1.
- **`Style` in `geometry`** — an import cycle: `geometry` would need `Colour`
  from `buffer`, and `buffer` imports `geometry`. Structurally impossible.
- **A new `style` or `theme` package in v1** — `buffer` itself would depend on
  it for `Cell.Style()` and `NewCell`, so it is not a boundary, it is a hop. One
  struct does not justify a package.
- **`*Style` fields** — needs a stable address, breaks `==`, and puts an
  indirection on the per-cell write to save a 12-byte register copy that costs
  nothing.
- **A theme in v1** — a string-keyed map puts a lookup on the draw path and
  makes "unset" a third state; a role set is only correct once roles are shared,
  and in v1 each role is a field on the one widget that owns it. Rejected with a
  named trigger, not on principle.
- **A framework default colour palette** — a product decision with no standing,
  and the thing users most often override. The framework ships attributes (cheap,
  portable, not taste) and no colours.
- **Leaving `ansi.Style` as a separate struct** — the exact two-types-one-name
  collision this ADR exists to prevent, already present in the tree.
- **Widgets writing cells directly for mixed-style text** — thirty
  reimplementations of the continuation-cell rule, and getting it wrong is
  permanent flicker, not a glitch.
- **Re-parsing styled text or re-wrapping every frame** — `Draw` runs every frame
  for every widget (ADR 0003), and a per-frame `Wrap` is a different order of
  magnitude from the ~81 µs whole-tree draw. Rejected in §Decision 2.
- **Attaching combining marks to the preceding span** — it would not render,
  because cells are written independently and graphemes are not composed; it
  would only hide the limitation.
- **A dashed border set in v1** — no Unicode vertical dashed glyph exists, so the
  set cannot be drawn correctly in all four orientations.
- **Style attributes on `layout.Constraint`** — CSS-shaped, and ADR 0004 chose a
  closed, predictable constraint set for that reason.

## Risks to revisit at v1.0

1. **The `Style{}` sentinel is a convention that a type does not enforce.** A
   partially filled literal is black-on-black and compiles. If the catalog's
   golden tests are written from hand-set styles rather than `NewStyle` calls,
   a whole widget's defaults could be black and no test would notice. Mitigation
   is a style-construction lint rule and the `TestStylePatchUsesUnsetColourOnly`
   test, not a type change.
2. **`Wrap` and `Truncate` inside `Draw`.** Documentation, not a type — the same
   shape as ADR 0007's `Budget` risk. Watch `AllocsPerRun` on every widget's
   draw benchmark. If it appears, the fix is the caller-side cache of §4, not a
   signature change to `Wrap`.
3. **The title thresholds (`W >= 2`, `W >= 5`) are guesses until widgets are
   real.** They are decided here so the three sets agree, not because they were
   validated against thirty titles. The first application with a long title is
   the honest test.
4. **`AttrReverse` semantics are undefined at the widget level**, as noted above.
   The first widget that uses it in a composed tree (a selected row inside a
   reversed list) will have to decide, and three authors must not each decide.
5. **The wide-glyph interaction is implemented but unbenchmarked.** ADR 0002
   records that no benchmark exercises wide characters, and this ADR extends the
   affected surface from `SetString` to `SetSpans` and `Wrap`. Neither has been
   measured. The continuation-cell test above is the cheap substitute and it is
   not a substitute for a benchmark.
6. **Nothing in this ADR has been run against a real terminal.** The ASCII-rung
   rule in particular assumes `Caps.Unicode` is a sound proxy, which
   `term/caps.go:56-60` already admits it is not (a terminal configured out of
   band). The first honest test is a golden sweep rendering every border style
   and a mixed-style paragraph under `Unicode: true` and `Unicode: false`.