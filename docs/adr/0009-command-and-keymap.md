# 0009 — Commands and keymap

- **Status:** Accepted
- **Date:** 2026-10-05
- **Decides:** [STATUS.md](../STATUS.md) — Core architecture / Commands and keymap;
  and the widget catalog's third open question — how "key pressed" becomes
  "intent".
- **Depends on:** [ADR 0003](0003-renderer-mode.md) (`Widget.Handle(Event) bool`,
  the interface this ADR deliberately does **not** change),
  [ADR 0005](0005-input-decoding.md) (`Event`, `Key`, `KeyMod`, `Mouse`,
  paste-as-one-event), [ADR 0007](0007-responsive-screens.md) (§1
  collision-avoidance rules), [ADR 0008](0008-style-and-text.md) (§1 "one shared
  type, named exactly this", and `NO_COLOR` as encode-time only)
- **Answers:** the gap named in [ADR 0005](0005-input-decoding.md) — decoding is
  done, and nothing sits above it. Every widget hand-rolls its own
  `switch ev.Key`, `examples/dashboard/main.go` hand-rolls `q`, `/`, `Tab`, `Esc`
  and an entire search mode, and there is no way to ask the program what keys it
  answers to.

## Context

[ADR 0005](0005-input-decoding.md) ended with a `term` package whose gap was
decoding. That gap is closed. What is left is the gap immediately above it, and
it is visible in three places:

```go
// widgets/data/list.go:309 — one case per documented binding
switch ev.Key {
case termmosaic.KeyUp:
    l.move(-1)
```

```go
// examples/dashboard/main.go:350 — the application's own routing, and it is
// three keys long on purpose
switch ev.Key {
case termmosaic.KeyTab, termmosaic.KeyBacktab:
```

```go
// widgets/form/form.go:655 — Enter *and* the space bar, accepted by hand because
// "one terminal sends the rune and the other sends the Key" is not a fact any
// widget should re-derive
if ev.Key == termmosaic.KeyEnter || ev.Key == termmosaic.KeySpace {
```

Those three are the same problem at three layers, and the third one is the
tell: `activateKey` exists because **two different `Event` values mean the same
user intent** and nothing in the framework says so. A command layer's first job
is not binding keys — it is having exactly one place where an `Event` becomes a
`Chord`, so that normalisation is stated once and every widget agrees.

### What exists that a command layer must not collide with

| Already decided | Where | Why it constrains this ADR |
|---|---|---|
| `Widget.Handle(Event) bool`, offered focused-first then to the tree | [ADR 0003](0003-renderer-mode.md), `widget.go:31` | 24 widgets implement it. Touching it is the expensive option, and this ADR argues at length below that it does not need touching. |
| `Event` is a tagged struct union, 112 bytes, `Compose` is the only nil-able field | [ADR 0005](0005-input-decoding.md) §8 | A command layer must not add a payload to `Event`. Commands are resolved **from** an `Event`, not carried inside one. |
| `input.Decode` is pure and **0 allocs/op** on every key path, pinned by `TestDecodeIsZeroAllocation` | [ADR 0005](0005-input-decoding.md) §8 | The dispatch path this ADR adds sits immediately downstream of it and inherits the bar. |
| `EventPaste` carries a whole paste as one event, and must **not** be re-expanded into key events by the framework | [ADR 0005](0005-input-decoding.md) §4 | A command layer that iterated a paste's characters to find a command would violate that contract one layer up. It must not. |
| `Mouse` has `X, Y`, `Button`, `Action` (`MousePress`/`Release`/`Drag`/`Move`), `Mod`; **no click count, no held-button mask** | `event.go` | Mouse→command design is constrained by what the decoder actually delivers. Drag is expressible; "click count 2" is not. |
| kitty `hyper`, `meta`, `caps-lock` and `num-lock` modifier bits are **decoded and dropped** — `KeyMod` is 4 bits | [ADR 0005](0005-input-decoding.md) §2 | A keymap that wants to bind `Hyper+s` cannot. Stated, not designed around. |
| `Key` is a contiguous `iota` block ending at `KeyF12`; a new key must be **appended** | `event.go:68` | The notation table must be built over the existing enum, not over an assumed one. |
| `form.Binding{Key, Help string}` and `KeyHint` already exist | `widgets/form/keyhint.go:14` | A discoverability type already ships. This ADR must generalise it, not re-invent a second one — and a type named `Binding` already exists in the tree, so **this ADR may not name anything `Binding`**. See §1. |
| `termmosaic.Minimizable`, `termmosaic.Focusable` — the optional-interface pattern, discoverable by type assertion, no widget forced | `widget.go` | The command layer's participation interface follows this precedent exactly. |

### Prior art: OpenTUI's `@opentui/keymap`

OpenTUI ships a separate `@opentui/keymap` package (host-agnostic, Bun/Node, no
FFI) for exactly this problem, and it is the only real reference implementation
available. Its shape, verified from the published docs 2026-10-05:

- **Layers**, each global or attached to a host target (`targetMode:
  focus-within` by default), sorted by explicit numeric `priority`, **newer
  first on ties**. "A local layer gets no automatic priority over a global
  layer."
- **Named commands** with metadata fields (`desc`, `group`, `title`,
  `category`) and a handler returning a result that can *reject* and let
  dispatch continue.
- **A rich key language**: literals, named keys, modifier chords, concatenated
  sequences (`dd`, `g?`), `<token>` aliases (`<leader>s`), and
  `{count}`-style runtime capture patterns — all registered through an addon
  pipeline with roughly **30 extension hooks** (`prependBindingParser`,
  `appendBindingExpander`, `registerSequencePattern`, `registerToken`,
  `preprocessLayerBindings`, …), plus emacs chords, `:command` parsing with
  `aliases`/`nargs`, and vim-neovim disambiguation.
- **Queries**: `getActiveKeys`, `getCommands`, `getCommandEntries`,
  `getCommandBindings`, `getPendingSequence` — the cheat-sheet and palette
  story is the reason the query surface exists.
- **Framework bindings** for React and Solid, a browser (HTML `KeyboardEvent`)
  host adapter, and a testing host.

**We take the ideas and refuse the pipeline.** Concretely:

| OpenTUI | TermMosaic | Why we differ |
|---|---|---|
| Explicit numeric `layer.priority`, newest-first on ties | **Scope specificity is not configurable in v0.2**: focus > screen > global | A priority integer an application gets wrong produces a binding nobody can predict and nobody can debug by reading. Our order has a justification (the focused widget is the most specific thing that can want the key) and a name. |
| `fallthrough` and `preventDefault` as separate binding flags | **No `fallthrough`. A handler returning `false` continues.** | Two flags for one concept, where one of them is a Go return value. |
| `runCommand` (ignores activation) vs `dispatchCommand` (respects it) | Same distinction, named `Invoke` and `Dispatch` | Convergent, and the names are clearer. |
| ~30 `prepend*`/`append*` extension hooks | **None.** A `keymap` package with a plugin pipeline for a framework that rejects reconcilers | ADR 0003's central argument applies here too: an extension surface is a silent-failure surface, and we have no plugin story to serve. |
| Concatenated sequences, `<leader>` tokens, `{count}` patterns | **Deferred** with a trigger (§8) | Each needs pending-sequence state, and pending-sequence state is **input-layer** state [ADR 0005](0005-input-decoding.md) already owns. Building it here creates a second ESC-ambiguity policy. |
| `:write` ex-commands with `aliases`/`nargs` | **Deferred** with a trigger (§8) | A command line is a whole sub-language — parsing, history, completion, a `KeyHint` for it. Not v0.2. |
| Two hosts (terminal + DOM `KeyboardEvent`), which is *why* the engine is host-agnostic | One host: `term mosaic.Event` | [ADR 0005](0005-input-decoding.md) already unified it. A host-agnostic engine is the right shape when you have two hosts. |
| `Ctrl+K` palette, `?` help | **Adopted verbatim** | Everyone agrees; convergence is the reason to adopt rather than invent. |

## Options considered

### Where the command layer lives in the event pipeline

This is the decision everything else follows from, so it is argued first and at
length.

**Option A — sit above the tree, leave `Widget.Handle` alone.** The application
loop asks the keymap first; if the keymap does not consume the event, the event
goes to the widget tree exactly as it does today.

**Option B — replace `Widget.Handle(Event) bool` with `Handle(Event, Context)`
or a command-only interface**, so every widget declares commands instead of
switching on keys.

Option B is what a "full command layer" usually looks like, and it is rejected on
cost, not on taste:

- **24 widgets change**, plus `render`'s dispatch, plus every `widgettest`
  helper, plus every `Handle` call site in `examples/`. That is a signature change
  on the one interface the README tells users is the framework's whole contract.
- **It does not even solve the stated problem for widgets.** A `List`'s
  `KeyPageUp` handler is `l.move(-l.vm.Visible() + 1)` — it needs
  `l.vm.Visible()`, a live field of the widget. Expressing that as a registry
  entry means the widget's internal state becomes a registry key, or the
  registry entry is a closure over the widget, or the widget keeps a private
  command table anyway. **You end up with the keymap plus a private keymap.**
- **It loses `TextInput`.** A text field must receive every printable rune,
  including the ones no command is bound to, and must see `EventPaste` as one
  event ([ADR 0005](0005-input-decoding.md) §4). A widget that only handles
  declared commands cannot accept a character nobody declared a command for.
- **It loses mouse hit-testing**, which is the one thing widgets are genuinely
  better at than a global registry (§6).

Option A's cost, stated honestly: an application can now have keys answered in
two places — the keymap and `Handle` — and precedence between them is a
discipline rather than a type. That is a real sharp edge and it is why the
resolution order below is spelled out as four numbered steps and pinned by a
test.

**Decision: Option A. `termmosaic.Widget` is unchanged.** No method added, none
changed, none deprecated. The `Handle` doc comment gains one sentence about
precedence, and that is the only edit to `widget.go`.

### One registry, or one per screen?

**A single application-wide registry** with per-command scope, versus **a
registry per screen** so switching screens swaps the bindings wholesale.

The second is what makes a modal dialog easy: a dialog owns its `Esc` and its own
navigation keys, and closing it cannot leave them behind. But it makes *help*
impossible to answer honestly ("what keys does this program have?") and makes
rebinding a global key an act of mutating every screen's table.

**Decision: one `keymap.Registry` per application, with scope on the binding.**
A screen is registered as a scope holder; pushing and popping a screen is a
binding concern, not a registry concern. This keeps `Describe` answerable, which
is the thing §5 depends on.

### Should widgets be forced to participate?

**Forcing it** would make help complete on day one. **Forcing it** also means 24
widgets, three of which are being written right now by other agents against a
different ADR, must be edited by this one.

**Decision: participation is an optional interface, and v0.2 requires it of
exactly zero catalog widgets.** The honest consequence — a `List`'s arrow keys
are not in the palette unless it opts in — is recorded in §Consequences rather
than papered over.

## Decision

**A named command is a thing an application can invoke; a key is one way to
invoke it. The keymap maps a decoded `Event` to a command through a single
normalisation step, resolves it by context specificity, and exposes the registry
itself as the only source of discoverability data. `Widget.Handle` is untouched
and sits below.**

```
                     input.Source  (ADR 0005 — decoding, ordering, paste-as-one)
                              │  Event
                              ▼
   ┌───────────────────────────────────────────────────────────────────┐
   │ APPLICATION EVENT LOOP                     (application's code)    │
   │                                                                   │
   │  1. EventResize          → resize, recompute bounds, r.Resize      │
   │  2. EventPaste/Focus     → straight to the tree, NEVER Dispatch   │
   │  3. everything else      → km.Dispatch(ev, focus)                 │
   │       ├─ consumed  → done                                         │
   │       └─ not       → focus.Handle(ev), then root.Handle(ev)        │
   └───────────────────────────────────────────────────────────────────┘
        ▲                            │                    │
        │ registry                    │ optional           │ unchanged
        │                            ▼                    ▼
   keymap.Registry ──▶ Commandable ──▶ (24 widgets)   Widget.Handle(Event) bool
        │                Clickable                       ADR 0003, untouched
        │
        └──▶ Describe() ──▶ help screen / KeyHint / command palette
```

### 1. The shared vocabulary, by name and signature

**Every name below is reserved framework vocabulary and must be added to
`vocabulary_test.go`'s `forbiddenDecls` (§Forced changes), exactly as ADR 0007
§1 rule 4 and ADR 0008 required for `Wrap`, `Style` and `Budget`. A package that
defines its own `Chord` has re-invented the thing this ADR exists to
prevent.**

Everything lives in a new package **`keymap`**, which imports `termmosaic` and
`buffer` and nothing else. That direction is forced and safe: `termmosaic` does
not import `keymap`, so `widgets/*` may import it without a cycle, and `keymap`
may hold `Widget` values without the root package holding `keymap` values.

```go
package keymap

// ---------------------------------------------------------------------------
// Command identity and the command itself
// ---------------------------------------------------------------------------

// CommandID is a command's stable, globally unique name, conventionally
// dotted and lower-case: "file.save", "view.toggle-help", "list.confirm".
//
// A string and not a distinct type because it crosses into help text, into
// config files the application owns, and into log lines. Two packages that
// both spell a command name must spell it the same way; a defined type would
// prevent the copy, not the typo.
type CommandID string

// Command is a named action an application can invoke from a key, a click, a
// palette row, or its own code.
//
// The registry holds commands and bindings SEPARATELY, because a command may
// have several chords and a chord may have no command (see Set.Bind's rule on
// ordering). This is the same separation Bubble Tea and OpenTUI use and it is
// the reason a palette can list "save" once and show three keys beside it.
type Command struct {
    // ID is the command's name. Required and unique within a Registry; a
    // second Command with an existing ID replaces the first.
    ID CommandID

    // Desc is the one-line description shown in help, in the palette and in a
    // KeyHint. It is REQUIRED for any command a user can reach — a command
    // with an empty Desc is a warning from the Registry, not a silent blank
    // row — with one exception: a command may legitimately have no description
    // if Hidden is true.
    Desc string

    // Group orders commands in help and groups palette rows. Empty means the
    // uncategorised bucket, which help renders last under an empty heading.
    // Groups are sorted alphabetically; within a group, commands are sorted by
    // ID so that help output is stable across runs and diffable across
    // versions.
    Group string

    // Run is the handler. Returning false means "I did not handle this after
    // all", and Dispatch continues to the next candidate binding. That is the
    // ONLY fallthrough mechanism in TermMosaic — there is no fallthrough flag
    // and no preventDefault flag, because a Go return value is the same
    // mechanism wearing a hat.
    //
    // Run must not mutate widget state from another goroutine. ADR 0003's rule
    // applies without exception: mutation happens inside Renderer.Post, or on
    // the event goroutine where the application's own state already lives.
    Run func(Ctx)

    // Enabled reports whether the command is currently available. It gates
    // palette rows, help rows and Dispatch: an unavailable command is not run
    // by a key press. Nil means always available, and is the common case.
    //
    // It is a plain predicate, not a condition expression language, and it MUST
    // NOT be called on the dispatch hot path more than once per candidate
    // binding. See §2's allocation note.
    Enabled func() bool
}

// Ctx is what a command handler is given.
//
// It is passed BY VALUE and is 128 bytes — see amendment 1: the shipped size
// is 152, pinned by TestCtxIsOneHundredFiftyTwoBytes.
// On a path that runs at most a few
// hundred times per second, against ADR 0002's 16 ms frame budget, that is the
// same trade ADR 0005 §8 made for the 112-byte Event: the zero-allocation bar
// is about not allocating, not about struct copies. Passing *Ctx would put an
// escaping pointer on the path and turn a stack copy into a heap object per
// keystroke.
type Ctx struct {
    // Event is the event that produced this dispatch, by value. A command
    // invoked from the palette or from code gets the zero Event, and
    // Synthesised is true.
    Event termmosaic.Event

    // Chord is the key chord that produced this dispatch, normalised as §3
    // specifies. It is the zero Chord when the command was invoked from a
    // click, from the palette, or from code — which is why a handler that
    // needs "which key was this" must check Chord.IsZero() first.
    Chord Chord

    // Focus is the widget that had keyboard focus when the command was
    // dispatched, and is nil when nothing is focused. A handler that needs to
    // act on the focused widget type-asserts it; it does not receive a
    // command's "target widget", because there is no such thing (§2).
    Focus termmosaic.Widget

    // Synthesised is true when the command was invoked without an Event —
    // from the palette, from the command line, or by Invoke from application
    // code. A handler that behaves differently for a mouse activation than for
    // a keyboard activation reads this; a handler that does not care ignores
    // it, and the overwhelmingly common case is to ignore it.
    Synthesised bool
}

// ---------------------------------------------------------------------------
// Scope — where a binding is live
// ---------------------------------------------------------------------------

// Scope says how specific a binding's context is. The three values are a total
// order and they are the only ones; there is deliberately no numeric priority
// to get wrong (see the OpenTUI comparison above).
//
// Context specificity is NOT configurable. An application that needs
// something between Screen and Focus declares a Screen binding on a narrower
// screen, which is the mechanism the value already has.
type Scope uint8

const (
    // ScopeGlobal is live everywhere, in every focus context and on every
    // screen. This is where app.quit, app.help and app.palette live.
    ScopeGlobal Scope = iota

    // ScopeScreen is live only while the screen that declared it is the
    // current one. Registry.SetScreen declares which widget is current, so a
    // dialog pushes a screen and its bindings are live for exactly as long as
    // it is up.
    ScopeScreen

    // ScopeFocus is live only while the widget that declared it holds
    // keyboard focus. This is where a list's navigation keys belong IF the
    // list chooses to declare them (§3's Commandable interface).
    ScopeFocus
)

// String returns the scope's name, for help output and diagnostics.
func (s Scope) String() string

// ---------------------------------------------------------------------------
// Chord — one normalised key plus modifiers
// ---------------------------------------------------------------------------

// Chord is one user-input gesture, normalised: exactly one key, plus exactly
// the modifiers that were held.
//
// It is comparable, which is the load-bearing property: a Chord is a map key,
// so resolution is one map lookup and zero allocations. It is also exactly 16
// bytes, and TestChordIsSixteenBytes pins that the way TestCellHasNoPadding
// pins ADR 0002's Cell — because a struct on the hot path that nobody measures
// is how a hot path becomes slow.
//
// NORMALISATION, and this is the whole reason Chord exists:
//
//   - Space is ONE chord. KeySpace and Rune ' ' are the same gesture, because
//     terminals disagree about which they send and form.activateKey already
//     works around this; the Chord type makes the workaround the default.
//   - Shift on a printable rune is FOLDED INTO THE RUNE. A terminal sends
//     Shift+A as 'A', never as 'a' with a shift bit, so Chord{'A'} and
//     Chord{'a', ModShift} would be two chords for one gesture.
//   - Ctrl on a printable rune is KEPT, because 'a' with Ctrl is genuinely
//     distinguishable from 'a'.
//   - Ctrl+I, Ctrl+M, Ctrl+J and Ctrl+H are folded to Tab, Enter, Enter and
//     Backspace where the decoder folds them, so a chord the terminal cannot
//     distinguish is not two chords either. The consequence — that Ctrl+M and
//     Enter are the same chord on a legacy encoding, and different ones under
//     kitty disambiguation — is recorded in §Consequences rather than papered
//     over.
type Chord struct {
    // Key is the non-printable key, or KeyNone when Rune carries the gesture.
    Key termmosaic.Key
    // Rune is the printable character, and is 0 whenever Key is not KeyNone.
    Rune rune
    // Mod is the normalised modifier set. ModShift is never set when Rune is
    // non-zero.
    Mod termmosaic.KeyMod
}

// IsZero reports whether c is the zero Chord, which is not a legal binding and
// is what a click, a palette activation or a direct Invoke carries.
func (c Chord) IsZero() bool

// String returns the canonical display form: "Ctrl+K", "Shift+Enter", "F1",
// "?", " " (a single space character, which is why the hint brackets it).
//
// Display is deliberately the SAME function as parsing, modulo case: what help
// prints is what ParseChord accepts. A help screen that documents a key nobody
// can bind is worse than one that documents fewer keys, and the way to make
// that impossible is for there to be one function, not two that agree.
func (c Chord) String() string

// ParseChord parses the canonical display form back into a Chord, applying the
// same normalisation as ChordOf. It is the inverse of Chord.String() for every
// chord that can be produced by a terminal, and TestParseChordRoundTrips pins
// that over a generated table.
//
// It is the API a REBINDING UI uses: the user types a key, the application
// parses it, and if it fails the application says so rather than storing a
// binding that will never fire.
func ParseChord(s string) (Chord, error)

// ChordOf converts a decoded key event into its Chord, applying §3's
// normalisation. It returns false for any event that is not an EventKey, and
// for a key event that normalises to nothing.
//
// This is the one place in TermMosaic where an Event becomes a gesture, and it
// is exported so that a widget keeping its own switch can ask "is this the key I
// care about?" without re-deriving the folding rules.
func ChordOf(ev termmosaic.Event) (Chord, bool)

// ---------------------------------------------------------------------------
// Binding — a chord, in a scope, on a target, reaching a command
// ---------------------------------------------------------------------------

// Binding connects a Chord to a CommandID within a Scope.
//
// It is a value, not a heap object, and it is what Commandable returns. The
// name is Binding rather than KeyBinding because the term is unqualified
// anywhere in TermMosaic, and because form.Binding already exists and is a
// different thing (a hint, §5) — the collision rule of ADR 0007 §1 rule 4 and
// ADR 0008 forbids a second exported Binding, so the new vocabulary takes the
// unused name and the existing one is left alone.
type Binding struct {
    // Chord is the gesture. The zero Chord is rejected by Registry.Bind.
    Chord Chord
    // ID is the command it reaches. It need not be registered yet: a binding
    // may precede its command, because a screen's bindings are naturally
    // declared while its fields are being constructed.
    ID CommandID
    // Scope is the binding's context. Zero value is ScopeGlobal.
    Scope Scope
    // Owner is the widget a ScopeFocus or ScopeScreen binding belongs to. It
    // is required when Scope is not ScopeGlobal and must be nil when it is.
    // Identity is pointer identity, so a binding follows the widget instance
    // that registered it — which is what makes a widget that is rebuilt each
    // frame a widget that must re-register, and that hazard is recorded in
    // §Consequences.
    Owner termmosaic.Widget
    // Desc OVERRIDES the command's own Desc for this binding only. It exists
    // because the same command means different things in different contexts —
    // Enter is "confirm" in a dialog and "open" in a browser — and it is the
    // field that lets one command appear twice in help with two honest
    // descriptions. Empty means "use the command's Desc".
    Desc string
}

// ---------------------------------------------------------------------------
// Entry — one row of discoverability output
// ---------------------------------------------------------------------------

// Entry is one discoverable row: a command, its description, and the chords
// that currently reach it.
//
// It is the ONLY type the help screen, the command palette and KeyHint consume,
// and it is produced from the registry, which is what makes it impossible for
// help to drift from the bindings (§5).
//
// Entry allocates. It is built on demand, never inside Draw, and never on the
// dispatch path.
type Entry struct {
    // ID is the command.
    ID CommandID
    // Desc is the description to show — the binding's override if it has one,
    // otherwise the command's.
    Desc string
    // Group is the display group.
    Group string
    // Chords are the chords that reach this command in the current context,
    // in canonical order (modifiers ascending, then Key ascending, then
    // rune). A command with several bindings produces several Rows, not one
    // row with a chord list: a help screen that shows one row per chord is
    // easier to scan and is what every terminal help screen does.
    //
    // The name Rows is deliberately not used for the type because Entry is the
    // thing and a row is a view of it.
    Chords []Chord
}
```

Three names in that block deserve their reasoning stated outside the comment,
because a coder will otherwise "improve" them.

**Why `Ctx` and not a `[]string` argv.** OpenTUI's `:write` ex-commands carry
`aliases` and `nargs`, and the natural pull here is to add
`Args []string` to `Command` so a palette can invoke `list.select` with an
argument. Deferred, with a trigger (§8). A command taking arguments is a command
*language*: parsing, completion, quoting, and a text-input mode for the palette.
Adding `Args` first would put the parsing in every handler instead of in one
place, and would make `Describe` ambiguous — is `list.select` available when the
palette filters to "list"? Keep `Ctx` argument-free in v0.2 and the door stays
open.

**Why `Enabled func() bool` and not a field.** A field is set once; a predicate
reads live state. The cost is that it is a function value called on the hot path
when a chord matches, which is why the doc comment says "not more than once per
candidate binding" and why §2 pins the allocation count including a *matching*
chord with a non-nil `Enabled`.

**Why `Owner` is a `Widget` and not a string ID.** A string ID would need a
lookup to become a widget, and the lookup is where "which of my twelve lists is
this" ambiguity lives. Pointer identity is unambiguous, and the rebuild hazard it
creates is real, nameable, and cheaper than the alternative.

### 2. Keymap resolution: four steps, and a `Handle` that never moves

**The dispatch path, in order.** This is the whole contract and it is four
steps because there are four places an event can go.

```
Dispatch(ev, focus) — in this exact order:

 1. Ev is not EventKey or EventMouse  → return not-consumed. Unconditionally.
    Resize, Paste, Focus and Compose never enter a command layer.
 2. Build the Chord (KeyOf)          → for keys; for a mouse, resolve the
                                        owning widget by hit-test (§6).
 3. Resolve the command              → §2.1 precedence.
 4. Run it. If Enabled is non-nil and
    false → NOT consumed, continue to
    the next candidate. If Run returns
    false → NOT consumed, continue.    → consumed = true, stop.
```

**Step 1 is not defensive programming, it is [ADR 0005](0005-input-decoding.md)
§4's paste rule enforced one layer up.** If `Dispatch` iterated a paste's
characters looking for a command, a 10,000-character paste would run the
resolution loop 10,000 times, which is the exact failure the parser was designed
to prevent. Paste goes to the tree, whole, once. This is stated as a hard rule
because it is the cheapest possible mistake to make and the most expensive to
discover.

**Step 4's two "not consumed" exits are one mechanism.** `Enabled` false means the
command is not available; `Run` returning false means it declined. Both continue
to the next candidate. There is no flag to remember which happened, because
there is nothing to do differently afterwards.

#### 2.1 Precedence, in order

Candidates are tried highest first; the first one that passes `Enabled` runs.

| Rank | Kind | Wins because |
|---|---|---|
| 1 | **User override**, any scope | An explicit `Bind` after `SetDefaults` is a deliberate act. It outranks a default *in the same scope*, and a `ScopeScreen` override outranks a `ScopeGlobal` default, because specificity outranks everything else. |
| 2 | `ScopeFocus` default, `Owner == focus` | The focused widget is the most specific thing that can want the key. |
| 3 | `ScopeScreen` default, `Owner == screen` | The screen is more specific than the application. |
| 4 | `ScopeGlobal` default | The application's own keys. |

**Ties within one rank are resolved by registration order, and a later
registration replaces an earlier one.** Not by an error: replacing is what makes
an override an override.

**Why an override does not outrank a more specific scope.** The tempting rule —
"the user's binding always wins" — breaks the case that matters most. A user who
binds `Esc` to `app.cancel` in order to close a dialog would, under that rule,
lose the ability to close the dialog to any screen that binds `Esc` itself. The
specificity order means the dialog's `Esc` is still the dialog's, and the user's
global `Esc` applies everywhere else. This is the same reasoning as CSS
specificity and it is the reason we did not adopt a numeric priority.

**The candidate list is a slice walked in rank order, not a lookup that returns
"the" answer.** At most a handful of candidates exist per chord, and walking
them is what makes `Enabled` and `Run` both able to decline. The per-candidate
slice is built once, when the registry is sealed or when `SetScreen` changes, and
is cached on the registry — so walking it allocates nothing.

**The allocation claim, pinned.** `Dispatch` allocates **zero** times for every
event kind, including a key event that matches a command with a non-nil
`Enabled`, and including the miss path. Pinned by
`TestDispatchIsZeroAllocation` in the shape of `input`'s existing test:

| Path | allocs | Why it is achievable |
|---|---|---|
| key, no match | **0** | Three map probes on a comparable 16-byte key, or one linear scan of a sealed slice. |
| key, match, `Enabled == nil` | **0** | Same, plus one indirect call on a `Ctx` built on the stack. |
| key, match, `Enabled != nil` | **0** | A `func() bool` field read from a struct in the registry is not a boxing allocation. |
| key, match, `Run` declines, next candidate runs | **0** | The candidate walk is over a pre-built slice. |
| mouse, press, no `Clickable` widget at point | **0** | Hit-test is a `Bounds().Contains` per registered widget. |
| `Describe`, `Chords`, `Help` | 1+ | These build strings and slices. **They must never be called from `Dispatch`**, and the test above does not call them. |

**`Dispatch` returns `(CommandID, bool)`, not `bool`.** The name comes back
because an application needs it for its own logging and for "did anything handle
this key" logic, and returning it costs nothing (a string header in a register
pair).

#### 2.2 What the application loop looks like

This is the shape every application adopts, and it is four lines longer than the
loop in `examples/dashboard/main.go` today:

```go
for ev := range src.Events() {          // ADR 0005: one ordered stream
    switch {
    case ev.Kind == termmosaic.EventKey || ev.Kind == termmosaic.EventMouse:
        if km.Dispatch(ev, focus) {      // commands first
            invalidate()
            continue
        }
        // Unconsumed: the tree, exactly as ADR 0003 §"Frame pipeline" says.
        if !focus.Handle(ev) {
            root.Handle(ev)
        }
    case ev.Kind == termmosaic.EventResize:
        // ADR 0007 §5 order, unchanged: recompute bounds, then Resize.
        root.SetBounds(rootBounds(ev.Size.W, ev.Size.H))
        r.Resize(ev.Size.W, ev.Size.H)
    default:
        // Paste, focus, compose: straight to the tree, never Dispatch (§2 step 1).
        if !focus.Handle(ev) {
            root.Handle(ev)
        }
    }
    invalidate()
}
```

**Two rules a coder must not get wrong, so both are stated as rules:**

1. **Resize is not a command.** There is no `resize` command and there will not
   be one. A resize is not an intent; it is a fact about the world, and
   [ADR 0007](0007-responsive-screens.md) §5 already gives it its place in the
   loop.
2. **A key that no command consumes still reaches the tree.** This is what keeps
   `TextInput` working (§2.3) and it is the reason `Dispatch` returning false is
   a normal outcome rather than a bug.

#### 2.3 `Widget.Handle` does not change — the argument

**`termmosaic.Widget` is unchanged. No method is added, none is changed, none is
deprecated.** Stating the cost of the alternative, because the alternative is
reasonable and a reviewer should be able to check this reasoning:

| Change | Cost |
|---|---|
| `Handle(Event) bool` → `Handle(Event, keymap.Ctx) bool` | 24 widget signatures, `render`'s dispatch, every `widgettest` helper, every `Handle` call site in three examples, and a root-package import cycle unless `Ctx` moves into `termmosaic`. |
| Add a fifth method `Commands() []keymap.Binding` to `Widget` | Not a cycle (it is an interface method, so `termmosaic` names no `keymap` type), but it forces 24 widgets to implement a method they cannot usefully implement, and ADR 0007 §"Rejected" already rejected a mandatory method for exactly this reason: **a method a widget can forget is a silent-failure surface.** |
| Make `Handle` take a `CommandID` instead of an `Event` | Breaks `TextInput` (it needs undeclared runes and whole pastes), breaks mouse hit-testing (a widget cannot name a command per row), and breaks every widget that is not a command table. |

**What the command layer would have to do to make the change worth it, which is
why it is not worth it yet:** own *all* keys, including `TextInput`'s printable
runes and `List`'s viewport-relative paging. That is a text-editing model and a
virtualised-scroll model, not a keymap. The first two of those do not exist yet.

**The one edit to `widget.go` is a doc comment**, and it is load-bearing enough
to quote:

> Handle offers the event to the widget and reports whether it consumed it.
> Events reach a widget only after the application's keymap has declined them
> (ADR 0009), so a binding in a keymap shadows a `switch` in this widget. A
> widget's own key handling is therefore the fallback, not the primary path,
> and a key that matters to both should be a command.

That is the whole concession, and it is honest: the two mechanisms can both
answer a key, and the winner is the keymap.

### 3. Key notation: one function, both directions

The notation is not a spec someone else can implement differently; it is two
functions in one file, and the second is the inverse of the first.

**Grammar.** Modifiers joined by `+` in the fixed order `Ctrl`, `Alt`, `Shift`,
`Super`, then the key. Modifier names are matched **case-insensitively** on
parse and emitted capitalised, which is what makes `ctrl+k` and `Ctrl+K` the
same binding. The full form:

| Written | Means | Canonical form |
|---|---|---|
| `Ctrl+K` | `ModCtrl` + the key `k` | `Ctrl+K` |
| `ctrl+shift+s` | `ModCtrl|ModShift` + `s` | `Ctrl+Shift+S` |
| `Shift+Enter` | `ModShift` + `KeyEnter` | `Shift+Enter` |
| `?` | the printable rune `?`, no modifiers | `?` |
| `F1` … `F12` | the function key | `F1` |
| `Space` | the space bar | `Space` — **and ` ` (a literal space) normalises to the same Chord** |
| `Esc` | `KeyEscape`. `Escape` is an accepted alias on parse | `Esc` |
| `Ctrl+?` | `ModCtrl` + `?` | `Ctrl+?` |

**Three notation decisions that would each have been a divergence:**

1. **Space has two spellings and one meaning.** `KeySpace` and `Rune ' '` are the
   same gesture; terminals disagree, and `form.activateKey` already works around
   it by hand. `Chord` folds them, `ChordOf` folds them coming from an `Event`,
   and `ParseChord("Space")` and `ParseChord(" ")` produce the same value.
   Canonical display is `Space` — a single space character in a help row is
   invisible to a reader, and the whole reason `KeyHint` **brackets** its keys
   (an accessibility rule already in that widget) is that shape carries meaning
   that whitespace cannot.
2. **The `Key.String()` table in `event.go` is the authority for key names**,
   which already yields `esc`, `del`, `pgup`, `f1`. `Chord.String()` maps them to
   display forms (`Esc`, `Del`, `PgUp`, `F1`) through one table in `keymap`, and
   **`ParseChord` maps them back through that same table.** Two hand-written
   tables would drift, and drift here is a key that is documented and does not
   work — the most expensive class of help bug.
3. **`Ctrl+I` and `Ctrl+M` are `Ctrl+Tab` and `Ctrl+Enter`.** [ADR
   0005](0005-input-decoding.md) §2 records that the kitty disambiguate flag is
   precisely what makes these distinguishable, and that they are indistinguishable
   without it. `Chord` follows the decoder: where the decoder folds, the Chord
   folds, so a binding cannot exist for a gesture the terminal cannot express. The
   cost is that `Ctrl+M` and `Enter` are one chord on a legacy encoding — stated
   in §Consequences, and it is the decoder's property, not this ADR's.

**`NO_COLOR` and the 16-colour rung: nothing to do, and that is a decision.**
The notation is plain text. A chord renders as bracketed, capitalised ASCII-ish
text in the terminal's own foreground, which is exactly what `KeyHint` already
does with no colour at all. So a help screen, a palette and a `KeyHint` row are
**identical under `NO_COLOR`, under a 16-colour terminal, and under truecolor** —
because no chord carries a `buffer.Style` field and no code path consults colour.
[ADR 0008](0008-style-and-text.md) §3's rule ("nothing in the widget path knows
about `NO_COLOR`") is inherited, not extended: this layer does not have a widget
path and does not have a colour path.

**Round-tripping is pinned, not asserted.** `TestParseChordRoundTrips` walks a
generated table of every `Key` constant × every one-modifier subset × a set of
printable runes, and asserts `ParseChord(c.String()) == c`. It runs against the
real `Key` enum, so it fails by itself when `KeyF13` is appended at the end of
the iota block (ADR 0005 §10) and nobody remembered the notation table.

### 4. Discoverability: help is the registry, or it is not help

**`Registry.Describe(scope)` is the single source of discoverability truth, and
everything user-facing is built from its output.** There is no second list of
keys anywhere in TermMosaic, which means the class of bug where help documents a
key that was renamed is structurally impossible rather than merely unlikely.

```go
// Describe returns the discoverable entries for scope, sorted by Group then
// ID. It is the ONLY data the help screen, the command palette and a KeyHint
// bound to a registry consume.
//
// It allocates, it is not cheap, and it MUST NOT be called from Dispatch or
// from a widget's Draw. A help screen that is open calls it when the registry
// changes; a palette calls it when it opens; a KeyHint calls it when its
// context changes. See §2's allocation table.
func (r *Registry) Describe(scope Scope) []Entry

// Chords returns the chords currently bound to id in scope, in canonical
// order. The cheap single-command query, for a KeyHint that shows one row.
func (r *Registry) Chords(id CommandID, scope Scope) []Chord

// Has reports whether id is registered and available. A one-line predicate for
// the "enable this button" question, which is asked on every frame of a toolbar.
func (r *Registry) Has(id CommandID) bool
```

**Three rules that make "help cannot drift" true rather than aspirational:**

1. **A command with an empty `Desc` is a warning from the Registry, not a silent
   blank row.** `Registry.Warnings() []string` reports it, along with a binding
   that names an unregistered command, a binding whose `Owner` is nil under a
   scoped `Scope`, and a chord bound twice in one scope. Warnings are
   diagnostic surface, never control flow — the same stance `input.Stats` takes
   in [ADR 0005](0005-input-decoding.md) §1.
2. **`Describe` is derived from the resolution tables, not from a parallel
   index.** There is no `[]Entry` the application maintains; it is computed from
   the same `Binding` values `Dispatch` walks. A binding that cannot be reached
   by resolution cannot appear in help.
3. **A command with no chord is still describable.** `list.confirm` bound to
   Enter on a `List` and `button.activate` reachable only from a palette row both
   appear. A discoverability list that only lists keys is a key list, not a
   command list, and a mouse-only or palette-only command is invisible in a
   key-only help screen.

#### 4.1 Should `KeyHint` be generalised? Yes, in one direction only

`KeyHint` already exists, already brackets its keys for accessibility, and
already truncates rather than clips. **It is not replaced and its constructor is
not changed.** What it gains is one method:

```go
// SetEntries replaces the bindings with rows derived from a command registry,
// so a form's hints cannot disagree with the program's bindings. It is the
// ADR 0009 discoverability path for the existing widget.
//
// SetEntries accepts what Describe returns and nothing else: a hand-written
// []Binding remains supported for the case where the application has no
// registry, and both produce identical rows.
func (k *KeyHint) SetEntries(entries []keymap.Entry)
```

This is the ADR's only touch to the widget catalog, it is additive, and it is
deliberately **one method rather than a signature change** — three agents are
building widgets in `widgets/` right now against other ADRs, and a changed
`KeyHint.Bindings` field type would collide with their work. The
`form.Binding` → `keymap.Entry` reconciliation is **deferred with a trigger**
(§8), because it is a breaking change to a shipped widget and there is no
deadline forcing it.

### 5. Rebinding: yes in process, no on disk

**Users can change bindings at runtime.** Three operations, and the precedence
rule is §2.1's rather than a new one:

```go
// Bind adds a binding. A binding added after Seal takes rank 1 — the
// user-override rank — within its scope. This is how SetDefaults distinguishes
// a program's own bindings from a user's or a config file's.
func (r *Registry) Bind(b Binding)

// Unbind removes every binding for c in scope. A chord that was defaulting to
// something becomes unbound rather than falling back to an older default,
// which is what a user who pressed "unbind this key" meant.
func (r *Registry) Unbind(c Chord, scope Scope, owner termmosaic.Widget)

// BindString parses s and binds it, reporting a parse failure. This is the
// entry point for a rebinding UI and for a config file, so the parse error
// surfaces where the user typed it rather than as a binding that never fires.
func (r *Registry) BindString(s string, id CommandID, scope Scope, owner termmosaic.Widget) error
```

**Persistence to disk is explicitly NOT a framework concern, and this is a
decision rather than an omission.** The order of operations is the reason, and it
is worth stating because "just add a JSON loader" is the obvious next request: to
persist and restore, the application must enumerate `ScopeGlobal` and `ScopeFocus`
bindings, and the framework does not know which widgets exist until the application
attaches them. A loader written before that is a loader that cannot be correct.
The trigger is named in §8.

**Runtime rebinding of a `ScopeFocus` binding whose owner was rebuilt is a real
hazard**, and it is the same shape as ADR 0007 §3's cache-keyed-on-the-wrong-
thing bug: a widget that is reconstructed gets a new pointer identity, so its
old bindings silently stop matching. `Registry.Warnings()` reports bindings whose
owner no longer answers to `Registry.IsAttached(owner)`, which is the only
mechanism that catches it before a user reports "my key stopped working".

### 6. Mouse: a hit-test, never a rectangle

**A click becomes a command because the widget under the pointer says so.** The
registry does not hold rectangles.

The alternative — binding a command to a `Rect` — is rejected on the same
argument ADR 0007 §1 rule 1 uses for `Bounds()` versus `buf.Size()`: a rect
stored in a global registry is a **second source of truth for geometry**, and it
goes stale the moment the layout solver reflows, the window resizes, or a
virtualised list scrolls. A binding registered for "the delete button at
(72, 3)" is a binding that silently misfires on a 100-column terminal. The
widgets own their geometry; the registry owns the names.

So a command reachable by mouse is reached through the focused widget, which is
the widget that already knows what is under the cursor:

```go
// Clickable is the OPTIONAL interface a Widget implements when a click inside
// its bounds can mean a command — a Button, a Table cell, a Menu row, a
// dismissible backdrop.
//
// It is one method returning a name, not a handler, because the command
// registry is the only place handlers live. A widget that implements it never
// runs application logic itself; it says which command the user meant.
//
// A widget that does not implement it is fully supported. Every widget in the
// v0.2 catalog does not implement it.
type Clickable interface {
    // Command reports the command a click at (x, y) — in SCREEN coordinates,
    // the same coordinates EventMouse carries — means, or ("", false) if the
    // click is not a command activation.
    //
    // false is a normal answer, not an error: a click on a widget's padding
    // is a click on the widget and not a command. It means Dispatch continues,
    // which is how a click falls through to Handle for widgets that have both
    // interfaces.
    Command(x, y int) (CommandID, bool)
}
```

**The drag question, answered precisely, and the answer is "not in v0.2".**
`Event.Mouse.Action` already carries `MouseDrag` ([ADR
0005](0005-input-decoding.md) §3), so a drag is *expressible* and needs no
new decoding. What a command layer cannot do in v0.2 is give a drag a **meaning**,
because a drag is a three-phase gesture — press, motion, release — and a command
is one instantaneous intent. Options considered:

| Option | Verdict |
|---|---|
| Synthesise a `Drag` command fired on `MouseDrag` motion | **Rejected.** The first motion event fires it, the user gets no drag, and the release is unhandled. A drag that fires a command on the first moved cell is a bug report, not a feature. |
| Add a `DragCommandable` interface returning three callbacks | **Rejected for v0.2.** It is the same shape as `Clickable` with a lifetime attached, and it is only worth an interface once a widget needs it. |
| Leave drag to `Handle`, which is where it already works | **Adopted.** A widget that needs a drag already receives `MouseDrag` events in `Handle` and already implements the gesture. `keymap` adds nothing, and §8 names the trigger for revisiting. |

A wheel notch is the same shape as a click and **is** expressible: a `Command(x,
y)` on a scrollable widget returning `view.scroll-down` for a wheel notch over
its rows. Whether any catalog widget does this in v0.2 is a widget decision, not
this ADR's, and none is required to.

### 7. `Commandable`: how a widget contributes, and why it is optional

The whole participation surface is one optional interface with one method. It
mirrors `termmosaic.Focusable` and `termmosaic.Minimizable` exactly: optional, so
adding it costs no widget anything, and discoverable by a type assertion.

```go
// Commandable is the OPTIONAL interface a Widget implements to publish its own
// key bindings to the command registry — so they appear in help, in the
// command palette, and in a rebinding UI, and so an application's global
// binding can be overridden consistently with a widget's.
//
// It publishes; it does not dispatch. Widgets keep receiving unconsumed keys
// through Handle, and Dispatch never calls Commandable. The two mechanisms are
// independent by design (§2.3).
//
// A widget that does not implement it is fully supported and is the v0.2 norm
// for the whole catalog. Its keys work; they are simply not discoverable
// through the registry, and KeyHint remains the way it advertises them.
type Commandable interface {
    // Bindings returns the widget's bindings. Scope is ScopeFocus and Owner is
    // the widget itself; the Registry fills both in, so an implementation
    // returns only Chord, ID and an optional Desc override.
    //
    // It is called on Attach and on every Registry.Seal, never per keystroke,
    // and it is allowed to allocate — it is not on the dispatch path.
    Bindings() []Binding
}
```

**Two rules make this safe, and both are rules about *when* it is called rather
than about what it returns:**

1. **Bindings are pulled at `Registry.Attach` and at `Registry.Seal`, never on
   the dispatch path.** A widget whose bindings depend on mutable state (a
   `TextArea` whose `Ctrl+Z` depends on the undo stack being non-empty) publishes
   the *command* and lets `Enabled` carry the condition, not the binding. This
   keeps `Bindable` off the hot path by construction.
2. **`Owner` is the widget, so a widget rebuilt per frame must re-attach.** A
   `Widget` value rebuilt each frame is already pathological under ADR 0003 (its
   identity is what focus and invalidation hang on), and this ADR inherits that
   rather than solving it. `Registry.Warnings()` reports an attached widget that
   is no longer attached.

### 8. Scope: v0.2, deferred, and triggers

**In v0.2, and that is the whole of it.** Stated as a block so it cannot be lost:

| Symbol | File |
|---|---|
| `keymap.CommandID`, `Command`, `Ctx` | `keymap/command.go` |
| `keymap.Scope` + `ScopeGlobal` / `ScopeScreen` / `ScopeFocus`, `String()` | `keymap/scope.go` |
| `keymap.Chord`, `IsZero`, `String`, `ParseChord`, `ChordOf` | `keymap/chord.go` |
| `keymap.Binding` | `keymap/registry.go` |
| `keymap.Entry` | `keymap/describe.go` |
| `keymap.Registry`, `New`, `Command`, `Bind`, `BindString`, `Unbind`, `Dispatch`, `Invoke`, `Seal`, `Attach`, `SetScreen`, `Has`, `Chords`, `Describe`, `Warnings`, `IsAttached` | `keymap/registry.go` |
| `termmosaic.Commandable` | `widget.go`, beside `Focusable` |
| `termmosaic.Clickable` | `widget.go`, beside `Commandable` |
| `KeyHint.SetEntries([]keymap.Entry)` | `widgets/form/keyhint.go` |
| `widget.go`'s `Handle` doc comment, quoted in §2.3 | `widget.go` |
| `vocabulary_test.go` gains this ADR's names | `vocabulary_test.go` |

**The Registry surface, in full, because a coder implements from this list:**

```go
// Registry maps gestures to named commands. The zero value is not usable;
// call New.
//
// A Registry is not safe for concurrent use. Commands' Run handlers may run on
// whatever goroutine Dispatch runs on, which for an application following the
// §2.2 loop is the event goroutine; mutations therefore follow ADR 0003's
// "mutate widget state only inside Post" rule, which covers the registry too.
type Registry struct{ /* unexported */ }

// New returns an empty Registry.
func New() *Registry

// Register adds commands, replacing any with the same ID.
func (r *Registry) Register(cmds ...Command)

// Command returns a registered command by ID.
func (r *Registry) Command(id CommandID) (Command, bool)

// Bind adds a binding. A zero Chord is ignored and reported by Warnings.
// Bindings added after Seal take the user-override rank in §2.1.
func (r *Registry) Bind(bs ...Binding)

// BindString parses s and binds it, reporting a parse error.
func (r *Registry) BindString(s string, id CommandID, scope Scope, owner termmosaic.Widget) error

// Unbind removes every binding for c in scope, including user overrides.
func (r *Registry) Unbind(c Chord, scope Scope, owner termmosaic.Widget)

// SetScreen declares the widget that owns the current ScopeScreen bindings.
func (r *Registry) SetScreen(w termmosaic.Widget)

// Attach pulls Bindings from every widget in ws that implements
// termmosaic.Commandable, registering each binding with ScopeFocus and Owner
// set to that widget. It is called when the tree is built and after any
// structural change; it is not called per frame.
//
// A widget that does not implement Commandable contributes nothing and is not
// an error.
func (r *Registry) Attach(ws ...termmosaic.Widget)

// IsAttached reports whether w was passed to Attach and is still current,
// which is how a rebuilt widget's stale bindings are detected (§5).
func (r *Registry) IsAttached(w termmosaic.Widget) bool

// Seal finalises the resolution tables. It is called by Attach and by Bind
// after the first Seal, and it is what makes Dispatch allocation-free: the
// candidate list per chord is built here and cached.
func (r *Registry) Seal()

// Dispatch resolves ev and runs the command it names, reporting whether the
// event was consumed. It is the §2 four-step path, in order, and it allocates
// zero times for every event kind.
//
// A non-key, non-mouse event — Resize, Paste, Focus, Compose — is never
// consumed and never dispatched (§2 step 1).
func (r *Registry) Dispatch(ev termmosaic.Event, focus termmosaic.Widget) (CommandID, bool)

// Invoke runs a command directly by name, bypassing Enabled and bypassing
// Dispatch. It is what a palette row, a menu item and application code call,
// and it reports whether the command was found — not whether it succeeded,
// because Run returns nothing and a declined Run is expressed by Enabled.
func (r *Registry) Invoke(id CommandID, ev termmosaic.Event, focus termmosaic.Widget) bool

// Has reports whether id is registered and currently available.
func (r *Registry) Has(id CommandID) bool

// Chords returns the chords bound to id in scope, in canonical order.
func (r *Registry) Chords(id CommandID, scope Scope) []Chord

// Describe returns the discoverable entries for scope, sorted by Group then ID.
func (r *Registry) Describe(scope Scope) []Entry

// Warnings returns the registry's diagnostics: a binding naming an
// unregistered command, a command with an empty Desc and not Hidden, a scoped
// binding with a nil Owner or a global binding with a non-nil one, a chord
// bound twice in one scope, and a binding whose owner is no longer attached.
// Diagnostic surface, never control flow — the stance input.Stats takes.
func (r *Registry) Warnings() []string
```

**Deferred, with triggers.** Stated as a block for the same reason ADR 0005 §10
states its deferrals.

| Deferred | Trigger to revisit |
|---|---|
| **Multi-stroke sequences and leader keys** (`g g`, `g p`, `<leader>`) | Two or more applications wanting a modal prefix scheme, **or** `Key.F13`–`F35` landing, which is the same pending-sequence machinery. The reason it cannot simply be added: pending-sequence state belongs in `input`'s `Parser`, alongside the escape deadline it already owns ([ADR 0005](0005-input-decoding.md) §2). Putting it in `keymap` creates a second ESC-ambiguity policy and a second answer to "how long do we wait". |
| **A command line** (`:write`, argument-taking commands, `Args []string` on `Command`) | An application with a real command vocabulary rather than a dozen named actions — a file browser, an editor. When it comes it is a `KeyHint` plus a `TextInput` plus a parser, and `Command.Args` is the last piece, not the first. |
| **Loading and saving bindings from a file** | The first application that needs it, and **only after** `ScopeFocus` bindings are enumerable, which requires the `Attach`-then-`Describe` order to be settled by real use. A loader that cannot enumerate what is bound cannot save it, and writing one earlier guarantees a rewrite. |
| **Key *release* bindings** | Never on our own initiative. `KittyReportReleases` is off by default ([ADR 0005](0005-input-decoding.md) §2) and no catalog widget consumes a release. Revisit if a drag-and-drop or key-up-driven widget ships. |
| **A `DragCommandable` interface**, or any drag-as-command modelling | The first widget that needs a drag gesture *and* wants it rebindable or palette-reachable — a resizable pane divider, a reorderable list. `Handle` already receives the events, so the trigger is a widget, not a user request. |
| **`ScopeScreen` as a distinct concept from "the root widget"** | The first application with two screens where only one is the tree root. `SetScreen` covers it in v0.2 without new vocabulary; the concept is deferred, not the capability. |
| **Making any catalog widget implement `Commandable`** | The first `KeyHint` in `examples/` that needs its hints and the program's bindings to agree. One widget, chosen because an example needs it — not all 24, and not for its own sake. |
| **`form.Binding` → `keymap.Entry`** (the naming collision in §1) | The next change to `widgets/form` that touches `KeyHint`. It is a breaking change to a shipped widget's exported field, which is why it is not bundled into an ADR whose subject is commands. |
| **A `Priority` field on `Binding`** | Never on our own initiative. Numeric priority is what OpenTUI has and the reason this ADR specifies a fixed three-value order instead; a priority knob an application can set wrong produces bindings nobody can predict. Revisit only if a real application genuinely cannot express its layering with focus/screen/global, and then as a new ADR that also says how it interacts with user overrides. |

### 9. Relationship to the Menu and Dialog widgets, and to the command palette

**The command palette is IN scope for v0.2, and it is not built by this ADR.** It
is built *on* `Menu` and `Dialog`, which two other agents are writing right now,
and this section states exactly what it needs from them so that work is not
blocked and this ADR is not the thing that decides their API.

**The decision.** A `Ctrl+K` palette is a widget, not a mode. It is a `Dialog`
containing a `Menu` fed from `Registry.Describe(ScopeGlobal)` filtered by a
`TextInput`. Everything it needs from the command layer is three calls that
already exist in §4: `Describe`, `Invoke`, and `Chords`. Nothing about the palette
requires a new type in `keymap`.

**What it needs from `Menu`:**

| Requirement | Why |
|---|---|
| Items with a **label, a dimmed right-aligned chord column, and a disabled state** | A palette row is `Entry.Desc` plus `Chords[0]`, and the disabled state is `Entry`'s command being unavailable. Without a distinct disabled state the palette must hide rows, and hiding them changes row count as the user types, which makes selection jump. |
| `SetItems` that is **allocation-free when the content is unchanged**, or an explicit "same length, mutate in place" path | The palette re-filters on every keystroke. If every keystroke rebuilds every row, the palette allocates on the input path, which is the thing [ADR 0002](0002-buffer-representation.md)'s bar exists to prevent. |
| Filtering done by the **application**, not the widget | Filtering is `Describe` plus a substring test over `Desc` and `ID`. It is not a widget concern, and putting it in `Menu` would make `Menu` know what a command is. |
| **No `CommandID` knowledge.** `Menu` deals in rows; the application maps row index to `Entry` to `CommandID`. | The alternative — `Menu` carrying command IDs — makes `Menu` depend on `keymap`, which makes every future list widget look like a palette. |
| `Esc` reaching the application rather than being consumed | [ADR 0005](0005-input-decoding.md) already has `KeyEscape`; a dialog that eats it would be unfocusable. |

**What it needs from `Dialog`:**

| Requirement | Why |
|---|---|
| **Modal**: keys do not reach the tree beneath | A palette that leaks `j` into a `TextInput` underneath is the classic palette bug. This is the whole reason the palette is a `Dialog` and not a `Menu` placed in the layout. |
| **Focus save and restore** | Opening the palette must not lose the focused widget. `Registry.SetScreen` then makes the palette's `ScopeScreen` bindings live, and restoring focus makes the previous ones live again — which is the mechanism working as designed rather than a special case. |
| `Enter` activating the selected row | One `Invoke`, per §4. |
| A **minimum size**, via the existing `termmosaic.Minimizable` | A palette at 20 rows in an 8-row terminal is a clipped lie. [ADR 0007](0007-responsive-screens.md) §"degenerate sizes" applies unchanged: clip, never blank, never panic. |

**And the honest dependency statement.** If `Menu` or `Dialog` does not land with
the rows above, the palette is **blocked, not compromised** — and the fallback is
a `List` in a `Block` with a `TextInput`, which the v0.2 catalog already has. The
palette is not the reason to change either widget's API, and this ADR does not
change it.

**`Dialog` and commands, one rule.** A dialog's confirm-and-cancel are commands
with `ScopeScreen` bindings, registered against the dialog widget as `Owner`. They
are therefore live exactly while `SetScreen` points at the dialog, which is what
makes "a dialog's `Esc` cannot be stolen by a global binding" true by
construction rather than by convention (§2.1).

## Forced changes to existing code

| Identifier | Current | After this ADR |
|---|---|---|
| `termmosaic.Widget` | `Bounds`/`Draw`/`Invalidate`/`Handle` | **unchanged.** No method added, changed or deprecated. The `Handle` doc comment gains the precedence paragraph quoted in §2.3. |
| `termmosaic.Event`, `EventKind` | the [ADR 0005](0005-input-decoding.md) §8 union | **unchanged.** A command is resolved *from* an event and never carried inside one; no payload is added, so the `unsafe.Sizeof(Event{})` guard test is untouched. |
| `termmosaic.Key`, `KeyMod`, `Mouse`, `MouseAction` | as decoded | **unchanged.** `Chord` is a new type over them; `Key`'s iota block is not extended, so the append-at-the-end rule (ADR 0005 §10) still governs `KeyF13`+ and the notation table picks them up automatically. |
| `widget.go` | `Focusable`, `Minimizable` | gains `Commandable` and `Clickable`, both optional, both in the same file beside their siblings, both with the same "a widget that does not implement it is fully supported" contract. |
| `input` package | decoder, `Parser`, `Source` | **unchanged.** `keymap` consumes `Event`; nothing is decoded here, and pending-sequence state stays in `Parser` (§8). |
| `widgets/form/keyhint.go` | `Bindings []Binding`, `SetBindings` | gains `SetEntries([]keymap.Entry)`. `Bindings []form.Binding` and `SetBindings` are **unchanged**, so the three in-flight widget agents are unaffected. `form.Binding` is not renamed in v0.2 (§8). |
| `vocabulary_test.go` | ADR 0007/0008 name list | gains this ADR's reserved names — see the list below — and `keymap/*.go` plus `widget.go` as the declaring files. |

**The reserved-name list to add to `forbiddenDecls`**, and the reason each is
worth reserving rather than merely documenting:

```go
// ADR 0009: the command layer. Chord, Command and Binding are the three a
// widget is most likely to define privately — "chord" for its own key
// matching, "command" for its own button click, "binding" for its own row — and
// a widget that defines one has a private version of the framework's, which is
// the exact failure ADR 0007 §1 rule 4 exists to prevent.
"Command", "CommandID", "Ctx", "Scope", "ScopeGlobal", "ScopeScreen", "ScopeFocus",
"Chord", "ParseChord", "ChordOf", "Binding", "Entry", "Registry",
"Commandable", "Clickable",
```

`forbiddenDecls` already contains `Region`, `Priority` and `Budget` from ADR 0007
and `Style`, `Span`, `Wrap`, `Truncate` and `BorderStyle` from ADR 0008, and the
scan is by top-level declaration name across every `.go` file in the module, so
this is a list edit and nothing else.

**One collision the coder must be aware of and must not resolve by renaming:**
`form.Binding` already exists and means something else (a display pair: a key
label and a description). `keymap.Binding` means a chord reaching a command.
They are different types in different packages with no assignability between
them, which is the [ADR 0008](0008-style-and-text.md) collision shape. It is
**accepted in v0.2** because the alternative — renaming a shipped widget's
exported type in the same change — is worse, and it is **reconciled with a
trigger** in §8. `keymap.Entry` is named to be the future owner of `form.Binding`'s
job, which is why the palette and help consume `Entry` and never `Binding`.

## Consequences

**Good**

- **A key meaning two things is now one thing.** `form.activateKey`'s
  hand-written "Enter *or* space" workaround, and the identical comment in
  `KeyHint`, become a property of `Chord`. The next widget does not re-derive it
  and cannot get it wrong on the terminal that disagrees.
- **Help cannot drift from the bindings**, because `Describe` is computed from
  the same tables `Dispatch` walks. A renamed command updates help because there
  is only one place its name is written.
- **`Widget` is untouched, and so is every widget's behaviour.** 24 signatures,
  `render`'s dispatch and three examples' event loops survive unchanged, and a
  text field keeps receiving undeclared printable runes and whole pastes exactly
  as [ADR 0005](0005-input-decoding.md) §4 requires.
- **Dispatch is 0 allocs, pinned by a test.** The `Chord` is a comparable
  16-byte struct, the candidate list is built at `Seal`, and `Ctx` is passed by
  value — so the bar [ADR 0002](0002-buffer-representation.md) set for the diff is
  met on the input path rather than quietly dropped one layer above it.
- **Rebinding is three calls**, and it composes with the palette because both go
  through `Describe`. An application can ship a rebinding UI without the
  framework knowing what a rebinding UI is.
- **A click is a command without a second source of truth for geometry.** The
  registry holds no rectangles, so nothing in it can go stale on resize — the
  failure mode ADR 0007 §3 names, avoided structurally rather than with a cache
  key.
- **The palette needs nothing new from `keymap`**, so it cannot block the Menu and
  Dialog work, and it cannot force an API change on either.

**Bad — stated plainly**

- **Two mechanisms can answer one key, and the winner is the keymap.** An
  application that binds `q` globally and also has a `switch` case for `q` in a
  widget will find the widget's case unreachable for the focused context. The
  `Handle` doc paragraph and §2.3 are the mitigation, and they are documentation.
  **This is the sharpest edge in this ADR** and the one a reviewer should push
  back on first.
- **The catalog advertises nothing.** No widget in v0.2 implements
  `Commandable`, so `examples/dashboard`'s `/`, `q`, `Tab`, `Esc` and its entire
  search mode are invisible to `Describe` and would be absent from a help
  screen. `KeyHint` remains the way a widget advertises, and the trigger to fix
  this is in §8. A help screen that ships before an application registers its
  commands will look empty, and that is correct but disappointing.
- **`Owner` is a pointer, so a rebuilt widget's bindings die silently.** A widget
  reconstructed each frame stops matching its `ScopeFocus` bindings with no
  error. `Warnings()` and `IsAttached` detect it; neither prevents it, and the
  failure mode is "my key stopped working", which is the worst kind of report.
- **Ctrl+M is Enter, and Ctrl+I is Tab, on a legacy encoding.** `Chord` folds
  what the decoder folds, so a user who binds `Ctrl+M` on kitty and then runs on
  a terminal without disambiguation gets Enter. This is ADR 0005's accepted
  limitation inherited, not a new one, and the alternative — inventing a chord the
  decoder cannot produce — would be worse.
- **`Run func(Ctx)` returns nothing.** A command cannot report failure, so "did
  it work" is answered by inspecting state afterwards, and `Invoke` returns
  "found", not "succeeded". A `bool` return was rejected because two different
  failure modes (unavailable vs declined) already exist and a third return value
  would make the caller handle three.
- **A user override does not beat a more specific scope.** A user who binds `Esc`
  to quit globally will find every dialog's `Esc` still cancels the dialog. This
  is §2.1's central decision and it will surprise somebody.
- **`Describe` allocates, and nothing stops a coder calling it from `Draw`.** It is
  documentation, not a type — the identical hazard ADR 0007 §7 names for `Budget`
  and ADR 0008 names for `Wrap`. The mitigation is the same shape: the
  allocation test covers `Dispatch`, and the doc comment on every allocating
  method says not to.
- **No mouse gestures.** Drag, double-click and right-click-as-distinct-from-a
  modified-left-click are all out. The first two need data `Mouse` does not carry
  (a click count) or a gesture recogniser that belongs in the widget, and the
  third is expressible but unimplemented.
- **The palette is blocked on Menu and Dialog.** If they do not land with the rows
  in §9, the palette does not ship in v0.2, and "full input support" is delivered
  without one of its most visible pieces. The `List`-in-a-`Block` fallback exists
  but is not what the palette should be.

## Rejected alternatives, specifically

- **Changing `Widget.Handle` to take a command context or a `CommandID`.** 24
  signatures plus `render`, plus it does not help widgets (a `List`'s paging
  handler needs its own viewport), plus it breaks `TextInput` (undeclared runes,
  whole pastes) and mouse hit-testing. Costed in §2.3 and rejected there.
- **A fifth mandatory `Widget` method** (`Commands() []Binding`, or `Bindings()`).
  ADR 0007 §"Rejected" already rejected a fourth mandatory method for the same
  reason — a method a widget can forget is a silent-failure surface, and 24
  widgets would be forced to implement one they cannot usefully implement. Optional
  interface instead, with zero catalog widgets required.
- **`Commandable` polled on the dispatch path** (ask each widget "do you want
  this key?"). An interface call per widget per keystroke, an allocation for the
  returned slice, and a per-widget ordering question with no principled answer.
  Bindings are pulled at `Attach`/`Seal` instead.
- **Numeric `Priority` on bindings, as OpenTUI has.** A knob an application can
  set wrong, producing bindings nobody can predict or debug by reading. Fixed
  specificity order instead; a trigger for revisiting is in §8.
- **`fallthrough` and `preventDefault` flags on bindings, as OpenTUI has.** Two
  mechanisms for one concept where one of them is a Go return value. `Run`
  returning `false` and `Enabled` returning `false` are the whole of it.
- **A binding that holds a `buffer.Rect`, so a click region is a global table
  entry.** A second source of truth for geometry that goes stale on the first
  reflow — ADR 0007 §1 rule 1's exact failure. `Clickable` asks the widget, which
  is the thing that knows.
- **Synthesising a `Drag` command from `MouseDrag` motion.** Fires on the first
  moved cell; the user gets no drag and the release is unhandled.
- **Multi-stroke sequences (`g g`, leader keys) in v0.2.** Pending-sequence state
  belongs to `input.Parser`, beside the escape deadline it already owns; a
  second implementation in `keymap` is a second ESC-ambiguity policy. Deferred
  with a trigger.
- **`:command` parsing and `Command.Args []string` in v0.2.** A command line is a
  sub-language — parsing, completion, quoting, a text-input mode. `Args` first
  would scatter that parsing across every handler.
- **A config-file loader for bindings in v0.2.** `ScopeFocus` bindings cannot be
  enumerated until `Attach`-then-`Describe` has settled under real use, and a
  loader that cannot enumerate what is bound cannot save it. The API is
  sufficient for an application to write its own, which is the right owner for a
  file format anyway.
- **Key *release* bindings.** `KittyReportReleases` is off by default and nothing
  consumes a release. Revisit when a widget does.
- **Merging `Chord` into `Event`** as extra fields. `Event` is a hot-path struct
  with a pinned `unsafe.Sizeof` guard test, and `Chord` is derived data — a
  printable rune *is* the gesture, and `Shift` is already folded into the rune by
  the decoder. Putting it in `Event` would grow 112 bytes for a field nothing
  reads at decode time.
- **A `keymap` type named `Binding`.** It is the obvious name and the collision
  rule says no: `form.Binding` already exists and means a display pair. Naming it
  `Binding` in `keymap` would leave the tree with two exported types of one name
  and no assignability between them — the exact `ansi.Style` collision ADR 0008
  removed. `Entry` is the discoverability type and `Binding` is the reachability
  type, and `form.Binding`'s reconciliation is deferred with a trigger.
- **A framework-supplied palette mode inside `keymap`.** It would make the command
  layer own a focus stack, a text input, a filter and a row cursor. All four
  belong to widgets, two of which already exist or are being built.

## Risks to revisit at v1.0

1. **The two-mechanism overlap (§Consequences) has never been observed under
   load.** The failure — a key bound globally shadowing a widget's `switch` — is
   reasoned about here and not measured, because no application in the tree mixes
   both. Trigger: the first application that does. The fix, if it bites, is
   `Warnings()` reporting a chord that is both bound and handled by an attached
   widget, **not** a `Widget` change.
   **Partly observed, and the observed half is the narrow one.** Two of four
   example programs now dispatch through a registry, and `examples/search` is the
   first with a **focusable** widget in a focus ring — a `TextInput` and a `Table`,
   with `Tab`/`Backtab` moving between them. So the two mechanisms are now in the
   same application, which is the trigger having fired. What it has *not* shown is
   the failure this risk is about: `search` uses `Command.Enabled` for every
   context-dependent binding and declines to bind a single chord a focused widget
   wants — `Home`/`End` are left to the widgets precisely because a
   `ScopeScreen` binding outranks both — so its `Handle` and its bindings never
   compete for the same key, and
   `TestNoScreenBindingStealsAFocusedWidgetsKey` pins that. It has established
   that an application *can* mix them safely by construction; it has not observed
   what happens when one is built without that discipline. `Registry.SetFocus`
   remains deferred, and `search`'s workaround — tracking focus itself and
   filtering its hint on `km.Has` — depends on nothing that closure would
   provide. The risk stays open, and so does `Warnings()`.
2. **`Chord` normalisation has never been run against a matrix of terminals.**
   Every folding rule in §3 is derived from [ADR 0005](0005-input-decoding.md)
   and `input`'s own tests, none of which have met a real tty — the risk ADR 0005
   §"Risks" item 1 already records. Trigger: the same recorded byte-stream
   matrix ADR 0005 asks for, extended to assert `ChordOf(Decode(...))` over it.
3. **`Chord`'s 16 bytes and the 0-alloc `Dispatch` are specified, not
   measured.** `TestDispatchIsZeroAllocation` and `TestChordIsSixteenBytes` are
   the pinned form, in the spirit of ADR 0002 and ADR 0005. If a future
   `Command` field pushes `Ctx` past a register set, the honest fix is a
   benchmark plus a re-measurement, not a comment.
4. **Scope specificity may be too coarse for one real application.** Three
   contexts and no numeric priority is a strong opinion. Trigger: an application
   that genuinely cannot express its layering — the signal is writing
   `if scope == ...` inside a handler, which is the shape a missing fourth scope
   takes.
5. **Nothing in the widget catalog implements `Commandable`, so the discoverability
   story is untested at scale.** `KeyHint.SetEntries` is the one bridge and no
   example uses it yet. Trigger: the first `examples/` change that wants a hint
   line to agree with a binding; that example is the honest test of `Describe`'s
   sorting and of whether `Entry.Chords` as one-row-per-chord is the right shape.
   **Triggered, and the answer to its own question is NO — one-row-per-chord is
   the wrong shape for a hint line.** See the second 2026-10-05 amendment at the
   end of this document. The trigger's other half, no catalog widget
   implementing `Commandable`, is **still open** and still deferred by §8.
   **The wrong shape was wrong twice, in the same way, in a second application.**
   `examples/search` also wanted its hint line to agree with its bindings, and
   `KeyHint.SetEntries` is now called by **two** example programs
   (`examples/hello/main.go`, `examples/search/search.go`), both through
   `DescribeGrouped`. What the two together establish is stronger than the first
   did alone, and stronger than "the interfaces are unused": an application can
   have a **fully registry-derived key contract** — every chord, every command,
   and every hint row written in one place — with `Commandable` unimplemented and
   unimplemented-by-choice, because `Enabled` is a property of the command rather
   than of a widget. That is a materially better answer to this risk than the
   interface simply having no users: the widget-participation design is no longer
   carrying the discoverability story on its own, so §8's deferral is not
   blocking. `Clickable` remains unused. What is still missing here is
   `Registry.SetFocus` (deferred to v1.1), which `search` works around — see the
   2026-10-05 amendment below.
6. **The palette's requirements on `Menu` and `Dialog` are stated here and
   implemented there.** If either ships without them, the mismatch is discovered
   at integration rather than here. Trigger: the palette's own PR, which is where
   §9's table must be checked line by line.
## Amendment 2026-10-05 — implemented, and five places the prose was wrong

The `keymap` package was built in v0.6.0. Building it turned up five points
where this ADR's prose contradicted the code it specified. All five were found
while writing the implementation, reported rather than worked around, and fixed
at the source. Original reasoning is preserved above; this section records what
changed and why. The spec and its errata are deliberately in one document, so
the next reader sees both.

**1. `Commandable` and `Clickable` live in `keymap`, not in `widget.go`.** §1's
file map and §"Forced changes to existing code" both put them in the root
package beside `Focusable` and `Minimizable`, which is where they read most
naturally. That is the one arrangement §1's own import-cycle rule forbids: a
root-package interface named for the keymap cannot name `keymap.Ctx` — or
`keymap.Scope`, or `keymap.Chord` — without the root package importing the
package that imports it. The two interfaces are therefore declared in
`keymap/participation.go`. The consequence for an adopter is nil: a widget
declares `var _ keymap.Commandable = (*MyWidget)(nil)` and the cycle never
appears in their code. What the root package keeps is its unchanged `Widget`.

**2. `Ctx` is 152 bytes, not 128.** The field list above — `Event` (ADR 0005's
112 bytes), `Chord` (16), `Focus` (16), `Synthesised` (bool) — sums to 145, and
Go pads the struct to the 8-byte alignment its largest field requires: 152. The
prose figure was an estimate written before the fields were fixed, and it was
optimistic. `TestCtxIsOneHundredFiftyTwoBytes` pins the real size. The argument
it supports is unaffected: this is a value on a path that runs at most a few
hundred times per second, and the property that matters is **0 allocations**,
which `TestDispatchIsZeroAllocation` pins. Passing `*Ctx` would put an escaping
pointer on that path and turn a stack copy into a heap object per keystroke,
which is the reason the ADR gives for by-value in the first place.

**3. §2.1's precedence table contradicted itself about overrides, and
specificity wins.** The table's rank 1 reads "**User override**, any scope" and
says it "outranks a default *in the same scope*", then goes on to claim a
`ScopeScreen` override outranks a `ScopeGlobal` default. Those two statements
describe two different rules: the first is rank-above-everything-within-a-scope,
the second is specificity-first. Under the first reading, a global `Bind` of
`Esc` would outrank the dialog's own `Esc` — which is the exact failure the very
next paragraph argues against. The shipped rule resolves the contradiction the
way the paragraph does: **scope specificity orders candidates first, and an
override is the within-scope tiebreak.** So a user's `app.cancel` on `Esc` wins
in `ScopeGlobal`, and a screen's `Esc` still wins over it. The paragraph was
right and the row was wrong.

**4. §3's notation table and §3's normalisation disagreed about `Ctrl+k` versus
`Ctrl+K`.** The notation prose says modifier names are matched
**case-insensitively**, "which is what makes `ctrl+k` and `Ctrl+K` the same
binding" — while the written-form table lists only `Ctrl+K` and its canonical
form, leaving the reader to guess. They are **two distinct chords**, and
`ParseChord` accepts either spelling. The case is the *key*, not the modifier:
the terminal reports `Shift+k` as `ModShift` plus the rune `K`, which is a
different `Chord` from `Ctrl` plus the rune `k`, and folding them would make a
documented binding unreachable on the terminal that produces it. Modifier names
themselves (`ctrl`, `Ctrl`, `CTRL`) are case-insensitive on parse and emitted
capitalised. The table now says so where a reader will look.

**5. `Entry.Chords` has length 1 in `Describe` output, not "every chord bound to
this command".** §4 describes `Chords` as "the chords currently bound to `id` in
scope, in canonical order", which reads as a slice that can hold several. What
ships is **one `Entry` per chord**: `Describe` walks the resolved binding table
and emits a separate row for each chord, each with a single-element `Chords`.
That shape is what §9's palette wants — "a palette row is `Entry.Desc` plus
`Chords[0]`" — and it is what makes a per-chord help row impossible to
mis-read, at the cost of a command with three bindings producing three rows
rather than one row with three chords. Both are defensible; the shipped one is
pinned by `keymap/describe_test.go`. The field's type stays `[]Chord` because
that is what a `KeyHint` row and a palette row both consume.

## Amendment 2026-10-05 — risk 5 is triggered, and it is a NO

The first amendment above ended with erratum 5, which recorded that `Describe`
emits one `Entry` per **chord** and defended that shape as the right one for a
palette. This one closes risk 5, the last of the six, and the trigger it named
has now fired: `examples/hello` is the first change that wanted a hint line to
agree with a binding. It dispatches through a real `keymap.Registry` — six
commands, twelve chords — and renders both its pinned hint line and its `?`
overlay from the registry through `KeyHint.SetEntries`.

**The honest answer to risk 5's own question is no: `Entry.Chords` as
one-row-per-chord is the wrong shape for a hint line.** Not wrong in principle
and wrong only for palettes — wrong for *hints*, specifically and for a reason
that is visible in one line of a terminal. `SetEntries` joins an entry's chords
into **one** label, so handing it `Describe`'s output renders a command with
three chords as the same description **three times**. That is not a hint, it is
three rows of noise in a line with room for one. `examples/hello` worked around
it with a thirteen-line merge of its own before this amendment existed; the
workaround is now a framework function and is deleted.

**The consequence is a second query rather than a change to the first.**
`Registry.DescribeGrouped(scope)` returns one `Entry` per **command**, carrying
every chord in scope for it in canonical order. `Describe` is unchanged and
remains one row per chord, which is what §9 specifies for a palette — "a palette
row is `Entry.Desc` plus `Chords[0]`" — and what a help screen wants, because one
row per chord is easier to scan. A two-function API is the price of two genuinely
different consumers, and a hint and a palette are genuinely different consumers.
The two are kept honest by construction rather than by convention: both are
views of one internal `describeRows`, so they cannot disagree about which
bindings are in scope, about the order, or about which description a binding
overrides. `DescribeGrouped` merges `Describe`'s already-sorted rows rather than
re-sorting, so its entry order and chord order cannot drift from `Describe`'s
either.

Two rows of `DescribeGrouped` are specified rather than incidental, and both
are the hint half of the difference. **`Chords` is never empty**: a command
bound in no scope is *absent* rather than present with no chords, because a row
with an empty key column renders as a bare description with nothing to press,
and a hint whose entries are mostly bare descriptions has stopped being a hint.
`Describe` keeps those rows — a palette-only or mouse-only command must stay
describable, and a key-only help screen that hid it would be wrong. And **entry
order matches `Describe`'s command order exactly**, so a hint's rows are stable
across runs and diffable across versions.

`examples/hello` is what the discoverability story is now tested against, and
the visible consequences are recorded here because they are what a reader of the
golden files will notice. The pinned hint reads
`[Q q Esc Ctrl+c] quit  ·  [?] toggle the keys`, where it read
`press q to quit  ·  ? keys  ·  arrows move focus`; the navigation is gone from
the one-line hint because a merged row per navigation command is wider than the
line, and a hint that truncates a binding label tells the reader less than a
hint naming the two keys they need first. The help overlay went from **two rows
to three**, because a derived help spells every chord in full where the prose
help it replaced said "arrows". Seventeen golden files moved, one row each. The
hand-written hint string and the test that checked it against `Handle` are both
deleted: a binding and its description are now written **once**.

**What risk 5 does not close.** Two items survive it, and both are named here
rather than left to be rediscovered.

**No catalog widget implements `Commandable`, and still none implements
`Clickable`.** Verified: `grep -rn 'Commandable' widgets/` returns nothing. The
example that triggered this trigger is an *application* opting in, which is not
the same thing as a widget contributing its own bindings, so §8's deferral is
untouched and `examples/hello` deliberately does not implement either — its
facts are cells it draws, not widgets with bindings of their own.

**`Registry` has no `SetFocus`, so `Describe(ScopeFocus)` is incomplete before
the first dispatch.** The registry learns what is focused only from the `focus`
argument `Dispatch` is handed, so a focus-level query asked before anything has
been dispatched answers as though nothing holds focus. That is not a
correctness bug for a palette — a palette asks `ScopeGlobal`, and the ADR's §2.1
specificity order is what makes a focused widget's own keys win over a global
one anyway. It is a gap for the question §4 actually asks, "what can I do right
now": an application with real focusable widgets cannot yet ask it. Adding
`SetFocus` is new API on a shipped type, so it is **deferred to v1.1**.
`examples/hello` sidesteps it rather than depending on it — its navigation is at
`ScopeScreen`, which is also the scope that degrades correctly: a focused child
added later binds its own arrows at `ScopeFocus` and outranks them by
specificity, with no change to the example.

## Amendment 2026-10-05 — `examples/search`: `SetFocus` is evidence, not a hypothetical

The second amendment above recorded that `Registry.SetFocus` is **deferred to
v1.1**, on the reasoning that an application with real focusable widgets cannot
ask "what can I do right now". That reasoning was sound and it was untested.
`examples/search` is the application that tests it, and it is the first example
with a `Focusable` widget in a focus ring — `form.TextInput` and `data.Table`,
moved with `Tab`/`Backtab`, drawn as a visible ring. It reached three findings,
and none of them closes the deferral.

**A deferral reasoned from "no application needs this yet" is now a deferral
reasoned from an application that wanted it and could not have it.** `search`
tracks focus itself and filters its hint on `km.Has` — "registered and currently
available" — because `Has` is the only predicate that consults the same liveness
`Dispatch` does. The query an application makes when focus changes is exactly the
one the registry cannot answer. That is stronger evidence for the v1.1 item than
the argument that preceded it, and it is also why the item stays open: the
workaround is eight lines an application has to write correctly on its own.

**`Describe(ScopeFocus)` is not incomplete — it is over-inclusive.** `inScope`
returns true on an exact-scope match without consulting liveness at all, so a
focus-scoped query answers "these bindings are in scope" for a binding whose
command is currently disabled. Applied to a hint, that advertises the *field's*
arrow bindings on a screen where the *table* has focus and those arrows move a
selection: a hint wrong in exactly the pane the reader is looking at. `search`
reaches for neither `ScopeFocus` nor the question, and says so in its own source.

**Context-dependence does not need a fourth scope.** Every binding in `search` is
`ScopeScreen` or `ScopeGlobal`, and every context-dependent one is gated with
`Command.Enabled` — which `dispatchChord` skips, reporting the event unconsumed,
so the tree gets it. That is the framework's own fallthrough, it is exact, and it
has a consequence worth recording against risk 1: because a screen-scoped
binding outranks a focused child, the shape is only safe if the application
declines to bind chords its focusable widgets want. `search` leaves `Home`/`End`
to the widgets and binds the ring's ends to `Ctrl+Home`/`Ctrl+End` instead. The
overlap is now demonstrated as *avoidable by discipline*; it is still not
demonstrated as *handled by the framework*, and `Warnings()` is still the fix if
it is not.

**`SetEntries` has two consumers now, not one.** `examples/hello` and
`examples/search` both build their `KeyHint` from `DescribeGrouped`, and the
answer to risk 5 was the same both times — one `Entry` per chord renders a
multi-chord command's description once per chord. No catalog widget implements
`Commandable` or `Clickable`, and §8's deferral of the first is unchanged.
