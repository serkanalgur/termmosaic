# 0010 — Mouse routing

- **Status:** Accepted
- **Date:** 2026-10-05
- **Decides:** [STATUS.md](../STATUS.md) — Core architecture / Mouse routing; and
  item 5 of the v1.0.0 gate list, "Mouse routing decision + the three wheel
  defects".
- **Depends on:** [ADR 0003](0003-renderer-mode.md) (`Widget.Handle(Event) bool`,
  the interface this ADR deliberately does **not** change),
  [ADR 0005](0005-input-decoding.md) (`EventMouse`, `Mouse` with `X, Y`, `Button`,
  `Action`; SGR 1006 / urxvt 1015 / X10 decoding, capture off by default),
  [ADR 0007](0007-responsive-screens.md) (§1 rule 1, `Bounds()` as the one source
  of truth for a widget's geometry, and the `Widget`-is-frozen rule),
  [ADR 0009](0009-command-and-keymap.md) (§6, "Mouse: a hit-test, never a
  rectangle", which is the decision this ADR gives teeth to)
- **Answers:** the gap [ADR 0009](0009-command-and-keymap.md) §6 left open —
  hit-testing was named as *the one thing widgets are genuinely better at than a
  global registry* and as a deciding reason for leaving `Handle` alone, but
  nothing in the catalog actually did it consistently. `docs/STATUS.md` recorded
  the resulting defect as a `Tabs` bug with a note that it wanted an ADR
  decision. It is a framework contract gap affecting three widgets, and this is
  the decision.

## Context

There is no consistent answer to *who gets a mouse event*. Applications route
every mouse event to the root widget and let widgets decide for themselves:

```go
// examples/markets/main.go:312 — the application's own routing, and it is the
// same for every mouse event there is
case termmosaic.EventMouse:
    click := ev
    r.Post(func() { if board.Handle(click) { r.InvalidateAll() } })
```

That contract is fine. What was missing was any agreement about what a widget
is supposed to answer, and the catalog did not have one. Measured across the
twenty-four widgets on 2026-10-05:

| Widget | Bounds-checks the pointer? | Where |
|---|---|---|
| `data.List`, `data.Pager`, `data.Table`, `data.Tree` | yes | `widgets/data/*` |
| `dialog.Dialog` | yes (2 checks) | `widgets/dialog/dialog.go` |
| `menu.Menu` | yes | `widgets/menu/menu.go` |
| `form.Button` | yes | `widgets/form/button.go:255` |
| `form.Checkbox` | yes | `widgets/form/checkbox.go` |
| `form.Toggle` | yes | `widgets/form/toggle.go:196` |
| **`form.Tabs`** | **no — on the wheel path** | `widgets/form/tabs.go:331` |
| **`form.Select`** | **no — on the wheel path** | `widgets/form/select.go:269` |
| **`form.Radio`** | **no — on the wheel path** | `widgets/form/radio.go:264` |
| `form.TextInput` | yes | `widgets/form/textinput.go:429` |
| `form.TextArea` | yes | `widgets/form/textarea.go:428` |
| `split.Split` | yes | via `PaneAt` / `dividerAt`, `widgets/split/split.go:217,240` |

### The shape of the defect: one ordering mistake, three widgets

`Tabs` is the clearest case, and it is worth showing because the click path is
*correct*. `Tabs.Handle` tests the wheel first:

```go
// widgets/form/tabs.go:331, before this ADR
func (t *Tabs) Handle(ev termmosaic.Event) bool {
    if d, ok := wheelDelta(ev); ok {
        t.offset += d
        t.clampOffset()
        return true          // returned before any bounds check
    }
    switch ev.Kind {
    case termmosaic.EventMouse:
        return t.handleMouse(ev)   // this one does check Bounds
```

The `Contains` check is right there, two lines below, on a path the wheel never
reaches. `Select` and `Radio` are the same defect one indirection away: both
route the wheel through the shared `optionList.wheelDelta` helper, which had no
bounds parameter and so could not check even if its callers had wanted it to.

The consequence is not three cosmetic bugs. A widget that consumes a wheel notch
it does not own **takes the wheel from whatever is beneath it**, and
`examples/markets` is a working demonstration of how bad that gets: its
`form.Tabs` pair chooser is first in the focus ring, and before this ADR a tab
row one row tall swallowed **every wheel notch in the application** — the table
under the pointer, the thing a reader actually scrolls, never saw one. The
example had grown a thirty-line application-level workaround to route the wheel
by pointer position instead of by ring order, and its own comment conceded that
`form.Tabs` "consumes a wheel notch whether or not the pointer is over it — which
is a defensible rule for a form". It was not defensible; it was a widget
answering for a pointer that was somewhere else entirely.

### Why this is a framework gap and not three widget bugs

`docs/STATUS.md` filed it under `Tabs`. That filing was wrong in a way worth
recording, because the misclassification is why it survived: a widget bug gets
fixed by editing a widget, and there was no reason for anyone to look at `Select`
and `Radio` while fixing `Tabs`. The real gap is the sentence the framework never
wrote down — *a widget handles a pointer event only if the pointer is inside its
`Bounds`* — and three widgets guessed differently from nine that had it right.

This is also not an aesthetic point. [ADR 0009](0009-command-and-keymap.md) §6
names mouse hit-testing as **"the one thing widgets are genuinely better at than
a global registry"** and lists losing it as one of the three reasons its Option B
was rejected:

> - **It loses mouse hit-testing**, which is the one thing widgets are genuinely
>   better at than a global registry (§6).

A decision record that rejects an alternative on the grounds that widgets must
keep hit-testing, shipped with three widgets not doing it, is a contradiction
that v1.0.0 could not freeze over. This ADR is what gives that sentence teeth.

### What already exists, and constrains the options

| Already decided | Where | Why it constrains this ADR |
|---|---|---|
| `termmosaic.Widget` is **frozen** — no method may be added, changed or deprecated | [ADR 0007](0007-responsive-screens.md) §"Rejected", restated in [ADR 0009](0009-command-and-keymap.md) §2.3 | A new routing method on `Widget` is **not available**. Not expensive, not deferred — *excluded*. |
| A widget's space is `Bounds()`, and `geometry.Rect.Contains(x, y)` already exists | [ADR 0007](0007-responsive-screens.md) §1 rule 1, `geometry/geometry.go:28` | The hit test is one call on a type that ships today. Nothing needs to be built to do it. |
| The registry holds **no rectangles**; a click becomes a command because the widget under the pointer says so | [ADR 0009](0009-command-and-keymap.md) §6 | The framework must not acquire a second source of truth for geometry. A global hit-test *table* is ADR 0007 §1 rule 1's exact failure. |
| Applications route to the root and let widgets decide | `examples/markets/main.go:312` and both other examples | The contract is not the defect. Changing it would mean a new framework routing layer, which is new API on a frozen interface. |
| `Mouse` has `X, Y`, `Button`, `Action` (`MousePress`/`Release`/`Drag`/`Move`) and **no click count, no held-button mask** | `event.go`, [ADR 0005](0005-input-decoding.md) §3 | "Who gets this event" is answerable by position. "Was this a double-click" is not, so no rule here may depend on a click count. |
| `MouseDrag` reaches widgets through `Handle` today | [ADR 0009](0009-command-and-keymap.md) §6 | Drag is a three-phase gesture that belongs to the widget, not a routing question. A drag is exempt from the strict bounds rule by the same argument a release is (§2). |

## Options considered

### Option A — widgets hit-test themselves, as `Button` and `Toggle` already do

Each widget that handles a pointer event checks `Bounds().Contains(x, y)` first
and declines otherwise. This is the maintainer's decision, and the reasoning for
it is short: the rule already exists in nine widgets, it is what
[ADR 0009](0009-command-and-keymap.md) §6 assumed, and it costs one call on a
type that ships.

**Accepted.** §Decision states it as a rule rather than as a patch.

### Option B — a framework routing helper: `termmosaic.RouteMouse(root, ev) Widget`

A new exported function that walks the tree and returns the widget under the
pointer, so applications ask the framework rather than offering the event to
everyone.

Rejected on three counts, and the first is the only one that is really
decisive:

- **It is new exported API, and `Widget` is frozen.** [ADR 0007](0007-responsive-screens.md)
  §"Rejected" rejected `Layout(Rect)` as a fourth mandatory `Widget` method with
  the reasoning that **a method a widget can forget is a silent-failure surface**.
  A helper is the same hazard wearing a trenchcoat: the tree is walked by the
  framework, so a widget nested in a container that does not forward events is
  invisible to it, and a widget a caller forgets to route is invisible for the
  same reason. The `Tabs` bug is precisely a widget that was *not* asked — no
  framework helper would have found it, because the application was routing
  correctly.
- **It needs a tree walk, and there is no tree.** `Widget` has `Bounds`, `Draw`,
  `Invalidate` and `Handle`. It has no `Children()`, so "the widget under the
  pointer" is not a question the framework can currently answer without an
  application-supplied child list — which is a second source of truth for the
  tree, the exact thing [ADR 0007](0007-responsive-screens.md) §3 warns about for
  caches. A helper that takes `[]Widget` is a helper the application maintains.
- **It duplicates a decision the widget has already made.** `Tabs` does not
  merely occupy a rect; it knows which cell holds which tab, and it declines a
  click in the empty space past the last tab. A positional router can only say
  "the tab row", and every consumer that needs more re-asks. The
  `Clickable.Command(x, y)` shape in [ADR 0009](0009-command-and-keymap.md) §6
  exists for exactly this reason.

### Option C — make the lenient wheel a documented widget policy

Declare that a wheel notch is **not a positional event**: any widget that scrolls
may consume any notch, and applications that care route the wheel themselves.
`examples/markets` already does, and its comment argues the lenient rule is
"defensible for a form, where scrolling a list does not require focus".

This was the option the existing code and comment were arguing for, and it is
rejected because the argument is about focus and the defect is about position.
Those are separable:

- The focus half is right and is kept: a wheel notch is a *read*, and a widget
  must not take keyboard focus from one. That rule is orthogonal to bounds, and
  §2 keeps it as its own rule.
- The position half is wrong. A wheel notch carries `X, Y` for the same reason a
  click does — the terminal reports where the pointer is — and discarding them
  means the coordinate is a field the framework decoded and then threw away.
  Worse, it makes the outcome depend on **registration order**: the same notch
  scrolls the tab row or the table depending on which widget an application
  happened to put first in a focus ring. A user cannot predict that and cannot
  debug it by looking.

### Option D — fix the three widgets and say nothing

Correct the `wheelDelta` call sites, leave the contract unwritten.

Rejected because it is the mistake `docs/STATUS.md` already made once. The
defect was filed as a `Tabs` bug; three widgets shared it; the next widget
author will make the same ordering mistake because nothing told them the rule
exists. The fix is three lines; the *decision* is the deliverable, and the
regression tests are what keep it.

## Decision

**A widget handles a pointer event only if the pointer is inside its `Bounds()`,
and a widget that declines a pointer event returns `false` so the event reaches
whatever is beneath it. This is a rule about every `Mouse` action — press,
release, drag and wheel alike — and it is enforced by the widget, not by the
application and not by the framework.**

`termmosaic.Widget` is **unchanged**. No method added, none changed, none
deprecated, and no new exported routing API. What changes is three call sites and
one shared helper:

```
Application event loop
   │  every EventMouse, offered to the root — the existing contract, unchanged
   ▼
root.Handle(ev)
   │  a container forwards to its children; a leaf decides
   ▼
widget.Handle(ev)
   │
   ├── ev.Mouse and !ev.Mouse.inside(Bounds())   → return FALSE  (decline)
   │
   ├── ev.Mouse, inside, press on a cell this widget owns   → act, return true
   ├── ev.Mouse, inside, press on a cell it does NOT own   → return false
   └── ev.Mouse, inside, wheel notch, and this widget scrolls → scroll, return true
```

### 1. The rule, in the four cases that were ambiguous

| Case | Rule | Why |
|---|---|---|
| **Click inside `Bounds`** | Handle, if the cell means something to this widget. | Unchanged. Nine widgets already did this. |
| **Click inside `Bounds` but on nothing** — a `Select`'s empty space below the last option, a `Tabs` cell past the last tab | **Decline.** Return `false`. | The widget owns the cell and has nothing to do with it. Declining lets a container behind it see the click; swallowing it strands the user on a dead cell. `optionList.optionAt` and `Tabs.tabAt` already both do this. |
| **Any pointer event outside `Bounds`** | **Decline.** Return `false`. | The defect this ADR fixes. |
| **Wheel notch outside `Bounds`** | **Decline.** Return `false`. | Same rule, and it is the case the three widgets got wrong. |

### 2. Two exemptions, and both are already how the catalog behaves

**A release ends a drag wherever the pointer is.** `split.Split` consumes
`MouseRelease` anywhere on the screen, outside its own `Bounds` included, and that
is correct: a drag that can only be finished by releasing over the divider
strands the user in a mode they cannot see. A release carries no new intent — it
ends a gesture the press already claimed — so bounds-checking it would make a
gesture harder to complete in order to make it tidier.

**A drag continues outside `Bounds` once started.** `TextInput` and `TextArea`
extend a selection from a drag that has left the field, and that is what a
selection is. **The press is the claim; the drag is the continuation.** A widget
that has not been pressed does not respond to a drag at all — which is the rule,
stated as a rule, because "the drag path is exempt" is exactly the shape of
sentence that gets read as "the drag path is unchecked".

**The wheel never takes focus, in or out of bounds.** A wheel notch is a read.
A widget that scrolls on a notch does so without taking keyboard focus, so the
key hint under the pointer does not rewrite itself mid-read. This is a rule
*about* the gesture rather than *where* it happened, so it holds independently of
the bounds rule, and `examples/markets`' `handleMouse` keeps its own
focus-suppressing loop for exactly this reason even though the loop's bounds test
is now redundant (§Consequences).

### 3. The per-widget wheel judgement, stated because it is a judgement

The rule is "inside `Bounds()` or nothing", but whether a widget *wants* a notch
is a per-widget policy question under [ADR 0007](0007-responsive-screens.md) §1
rule 5, and it is resolved here so the next widget does not re-derive it:

| Widget | Wheel | Reasoning |
|---|---|---|
| `data.List`, `Pager`, `Table`, `Tree` | **scrolls**, inside `Bounds` only | A virtualised viewport over more rows than fit. The wheel is the primary way to move it. |
| `menu.Menu` | **scrolls**, inside `Bounds` only | Same. A long menu is a scroller. |
| `dialog.Dialog` | **scrolls its choices**, inside `Bounds` only, and only when it has choices | A dialog with nothing to scroll declines the notch, so a wheel over it reaches whatever scrolls beneath. Already the behaviour. |
| `form.Select`, `Radio`, `Tabs` | **scrolls**, inside `Bounds` only | **Changed by this ADR.** The offset moves; what moved it is now the pointer's position. |
| `form.Checkbox`, `Toggle`, `Button` | **declines** | One row, one cell, nothing to scroll. A notch over a button is not the button's event. Already the behaviour. |
| `form.TextInput` | **declines** | See below. |
| `form.TextArea` | **declines** | See below. |
| `split.Split` | **declines** | A container has no content of its own to scroll — it composes panes that each answer for themselves. Already the behaviour, by omission and now by decision. |

**`TextInput` and `TextArea` decline the wheel, and that is a decision rather than
an oversight.** Both are caret-position widgets whose vertical extent is not a
viewport of a larger document the way a `Select`'s is: `TextInput` is a single
line with a horizontal offset, and a vertical wheel notch has no axis that means
anything on it. `TextArea` *does* scroll vertically (`topLine`), and
wheel-to-scroll there is a plausible feature — but **it is a feature, and
adding it here would be answering a different question than the one this ADR
asks.** This ADR decides who gets an event; it does not decide which widgets
would like one. A `TextArea` that grows wheel-scrolling later does so as its own
change, with its own tests, and it will already be bounds-correct when it does
because the rule is now written down.

### 4. The fix, and why it is in the shared helper

`form.optionList.wheelDelta` gained a `buffer.Rect` parameter and a
`Contains` check:

```go
func wheelDelta(ev termmosaic.Event, r buffer.Rect) (int, bool) {
    if ev.Kind != termmosaic.EventMouse { return 0, false }
    if ev.Mouse.Button != termmosaic.MouseWheelUp && ev.Mouse.Button != termmosaic.MouseWheelDown {
        return 0, false
    }
    if ev.Mouse.Action != termmosaic.MousePress { return 0, false }
    if !r.Contains(ev.Mouse.X, ev.Mouse.Y) { return 0, false }
    if ev.Mouse.Button == termmosaic.MouseWheelUp { return -wheelRows, true }
    return wheelRows, true
}
```

The check goes **here** rather than in each of the three callers because all three
callers got the defect the same way, and a fix that must be applied three times
is a fix that is applied twice. The helper is where the shared decision already
lives — `wheelRows` is there for the same reason, as
[ADR 0007](0007-responsive-screens.md) §1 rule 5's "one local named constant
beside its own `Draw`" stated once for three widgets. Making the parameter
mandatory rather than adding a bounds check at each call site is what makes the
next fourth widget correct by construction.

## Forced changes to existing code

| Identifier | Current | After this ADR |
|---|---|---|
| `termmosaic.Widget` | `Bounds`/`Draw`/`Invalidate`/`Handle` | **unchanged.** No method added, changed or deprecated. |
| `termmosaic.Event`, `Mouse` | the [ADR 0005](0005-input-decoding.md) §8 union | **unchanged.** No payload added, so the `unsafe.Sizeof(Event{})` guard test is untouched. |
| `geometry.Rect.Contains` | `Contains(x, y int) bool` | **unchanged.** Already the right function; this ADR is what starts requiring it. |
| `form.optionList.wheelDelta` | `wheelDelta(ev) (int, bool)` | gains `r buffer.Rect` and one `Contains` check. Unexported; three call sites updated. |
| `form.Tabs.Handle`, `Select.Handle`, `Radio.Handle` | test the wheel before the `switch`, with no bounds | pass their own `bounds` to `wheelDelta`. **No other behaviour change.** |
| `form.Tabs`, `Select`, `Radio` doc comments | advertise `wheel` with no positional qualifier | say the notch scrolls only over the pointer being inside `Bounds`. |
| `examples/markets/dashboard.go` `handleMouse` | a wheel-routing loop working around `Tabs` | **loop kept**, rationale rewritten: it now buys the focus rule, and its `Bounds().Contains` is redundant with every widget's own. |
| `widgets/form/hittest_test.go` | — | **new.** The regression tests in §Consequences. |
| `vocabulary_test.go` | ADR 0007/0008/0009 name list | **unchanged.** This ADR introduces no new name in any package: the rule is prose plus one unexported parameter, and `Contains` was already reserved vocabulary from `geometry`. |

## Consequences

**Good**

- **The wheel reaches the widget under the pointer**, everywhere in the catalog,
  because a wheel notch outside `Bounds` is declined. The `markets` example's
  thirty-line workaround exists only because a tab row was answering for a
  pointer that was nowhere near it.
- **The rule is one sentence and it is checkable by reading.** "A widget handles
  a pointer event only if the pointer is inside its `Bounds`" is the kind of
  statement a widget author can hold in their head, which is the only kind that
  survives twenty-four widgets written in parallel.
- **The `Widget` interface is untouched, and so is every widget's click handling,
  focus-on-click, drag and the applications' routing.** Nine widgets already
  complied and did not change at all; the three that did changed on the wheel
  path only. `docs/STATUS.md`'s gate item 5 closes without a signature change.
- **[ADR 0009](0009-command-and-keymap.md) §6's "widgets are better at this" is
  now true of the shipped catalog** rather than of the design. That matters for
  the next `keymap` discussion: the reason Option A was chosen is now
  demonstrated rather than asserted.
- **The fix is in the shared helper, so the next widget is correct by
  construction.** Three callers, one check, one place to review.
- **`split.Split` needed no change, and saying so is a result.** It bounds-checks
  through `PaneAt` and `dividerAt`, arms a drag only on `MouseLeft`, and ends a
  drag on a release anywhere — §2's two exemptions are how it already behaved.

**Bad — stated plainly**

- **A wheel over a KPI tile, in `examples/markets`, is now consumed by nobody.**
  Before this ADR the lenient `Tabs` swallowed it and the tab row scrolled by
  three, which was a bug that looked like a feature. Now it reaches the
  application's loop, which does nothing with it. The visible change is that a
  notch over a non-scrolling area no longer scrolls something. That is the
  correct outcome, and it is a behaviour change in a shipped example, so it is
  called out here rather than discovered.
- **Nothing stops the next widget from getting the ordering wrong.** The
  `Tabs` code had a correct `Contains` check two lines below a wheel test that
  returned first, and it read as fine. The rule is documentation plus tests; a
  widget in a package with no such test can still ship the mistake. The
  mitigation is `widgettest` and the per-package hit-test table tests, and the
  honest statement is that it is a convention pinned by tests rather than a type
  that prevents it — the same position [ADR 0007](0007-responsive-screens.md)
  §"Risks" item 2 takes for the rect-keyed caching rule.
- **A widget whose `Bounds` is stale now declines everything.** Before this ADR, a
  widget with an unset or wrong rect still answered clicks by position in the
  sense that a lenient widget answered *every* event. Now a layout bug presents
  as "this widget ignores the mouse entirely" rather than as "this widget
  responds in the wrong place". That is a better failure, but it is a different
  one, and the first report of it will be a user saying a button stopped working.
- **The rule is about the pointer, so it says nothing about z-order.** Two
  widgets whose `Bounds` overlap both answer a click inside the overlap, and
  which one wins is the application's ring order. This ADR deliberately does not
  add a stacking order — a widget tree with no `Children()` cannot express one
  (§Options, Option B), and inventing an application-maintained child list to
  solve it would be a second source of truth for the tree. Overlap is rare
  (`examples/markets`' ring does not currently overlap) and the existing
  ring-order rule stays the answer.

## Rejected alternatives, specifically

- **A framework routing helper — `termmosaic.RouteMouse(root, ev) Widget`, or a
  new optional `termmosaic.Hittable` interface.** New exported API against a
  frozen `Widget`; it needs a tree walk and `Widget` has no `Children()`, so it
  would require the application to maintain a second source of truth for the
  tree; and it can only answer "which rect", where a widget can answer "which
  cell means what". Costed in §Options and rejected there.
- **A mandatory `Widget` method** (`HitTest(x, y int) bool`, or a `MouseTarget`
  registration). [ADR 0007](0007-responsive-screens.md) §"Rejected" already
  rejected a fourth mandatory method for exactly this reason: a method a widget
  can forget is a silent-failure surface, and 24 widgets would be forced to
  implement one. An *optional* interface has the same forgettability with none of
  the forcing, and buys nothing over a call to `Bounds().Contains` that the
  widget can already make itself.
- **The lenient wheel as documented policy** — a notch is not a positional event,
  any scroller may take any notch, applications route it themselves. Rejected in
  §Options: it is right about focus and wrong about position, and it makes the
  outcome depend on ring order, which a user cannot predict. `examples/markets`
  argued for this in a comment, and the argument is now recorded as wrong.
- **Fixing the three widgets and writing nothing down.** Rejected in §Options: it
  is the misclassification that let the defect survive across three widgets in
  the first place.
- **Bounds-checking the drag and the release too**, for tidiness. Rejected in §2:
  a release is the *end* of a claim the press already made, and requiring the
  pointer to still be over the widget to finish a gesture makes drags harder to
  complete. `split.Split` already does this and it is the right behaviour.
- **Adding a stacking order, or a `Children()` method, so overlap is decidable.**
  Rejected: `Children()` is a `Widget` method and `Widget` is frozen; and a
  stacking order is a widget-tree question, not a mouse question. Overlap stays
  the application's ring order.
- **Giving `TextArea` wheel-to-scroll as part of this fix**, since it scrolls
  vertically and the omission looks like an oversight. Rejected in §3: this ADR
  decides who gets an event, not which widgets would like one. A feature added
  under a routing ADR is a feature nobody reviewed as a feature.
- **A framework `widgettest` helper that asserts bounds-checking for a widget.**
  Deferred rather than rejected, and worth naming: `widgets/widgettest` already
  exists and is the natural home, and a helper every widget package can call from
  one table would catch the next `Tabs` without each package writing its own. It
  is not in this ADR because a helper that only three packages call today earns
  its keep on the fourth, and the three packages now have their own table tests.

## Risks to revisit at v1.0

1. **The rule is prose plus tests, and prose does not prevent a mistake.** A new
   widget package with no hit-test table can ship the `Tabs` ordering bug. The
   helper in `widgettest` named in §"Rejected alternatives" is the mitigation and
   it is not built. Trigger: a fourth widget package, or a review that finds one
   more instance. The fix is a test helper, **not** a `Widget` change.
2. **A stale `Bounds` now presents as a dead widget.** Every widget that declines
   out of bounds depends on its rect being right, and a layout that forgets to
   hand one out produces a widget that ignores the mouse with no error. Trigger:
   the first user report of "this button stopped working". Worth a
   `Bounds().Empty()` warning path in `widgettest` if it happens more than once.
3. **Overlapping widgets are still resolved by ring order, and this ADR did not
   change that.** If two widgets genuinely overlap in a real application, the
   wheel now goes to whichever is first, which is the same rule a click already
   followed — so this is not a new inconsistency, but it is one this ADR
   declined to fix. Trigger: an application with overlapping `Bounds`.
4. **The exemptions in §2 are stated as prose about gestures.** "The press is the
   claim, the drag is the continuation" is the sentence that stops a future
   reader from applying the bounds rule to a drag. It is the same class of hazard
   as [ADR 0007](0007-responsive-screens.md) §"Risks" item 2 — a rule that is
   documentation rather than a type, which each author interprets slightly
   differently. Trigger: a widget that responds to a drag it was never pressed
   for.
5. **Nothing here has been run against a real terminal's mouse reporting.** Every
   claim about *which* events a terminal sends for a wheel notch is inherited
   from [ADR 0005](0005-input-decoding.md) §3 and its decoder tests, which have
   not met a real tty — the risk ADR 0005 already records, and it is why the
   wheel's `Contains` check is stated for the coordinates the decoder delivers
   rather than for any assumption about wheel semantics. Trigger: the same
   recorded byte-stream matrix ADR 0005 asks for.
6. **The fix's reach is exactly the three widgets named here.** Thirteen other
   widgets were audited by reading their `Handle` methods and are not covered by
   a hit-test table test of their own. `data`, `menu` and `dialog` each have
   their own outside-the-bounds test, and `split` has none for the wheel because
   it declines the wheel by having no wheel path. Trigger: any new widget in
   those packages.
