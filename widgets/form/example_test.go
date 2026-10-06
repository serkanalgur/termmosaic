package form_test

import (
	"fmt"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/keymap"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/form"
	"github.com/serkanalgur/termmosaic/widgets/split"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// show renders root on a w-by-h screen and returns the rows with trailing
// spaces trimmed.
//
// Every Output comment in this file is the CELL GRID the renderer produced,
// read back out of a headless.MemorySink — not a description of it and not the
// escape sequences a real terminal would have received. widgettest walks the
// whole widget -> buffer -> renderer -> diff -> encoder -> MemorySink path, so
// what a reader sees here is what the widget's own tests assert on.
//
// widgettest.Capture rather than Render: an Example has no testing.TB to hand
// over, and Capture reports the same failure as an error rather than t.Fatal.
func show(w, h int, root termmosaic.Widget) string {
	sink, err := widgettest.Capture(w, h, 1, root)
	if err != nil {
		panic(err)
	}
	return widgettest.Screen(sink)
}

// press offers an event to a focused widget and reports whether it took it, so
// an example can drive a widget with the same calls an event loop would make.
func press(w termmosaic.Widget, k termmosaic.Key, mod termmosaic.KeyMod) bool {
	return w.Handle(termmosaic.SpecialKeyEvent(k, mod))
}

// typeRune offers one printable rune, which is what a terminal delivers for an
// ordinary keypress.
func typeRune(w termmosaic.Widget, r rune) bool {
	return w.Handle(termmosaic.KeyEvent(r, 0))
}

// stack puts widgets in a vertical Split, which is how the ones that need more
// than one row are shown in a single frame. SetBounds is not optional:
// split.New takes panes but no rectangle.
func stack(w, h int, widgets ...termmosaic.Widget) termmosaic.Widget {
	cs := make([]layout.Constraint, len(widgets))
	for i := range cs {
		cs[i] = layout.Fill(1)
	}
	s := split.New(layout.Vertical, widgets...)
	s.SetBounds(buffer.Rect{W: w, H: h})
	s.SetConstraints(cs)
	return s
}

// Example is a small settings form: a text field, a checkbox, a toggle and the
// button that would apply them.
//
// The one thing worth copying from it is that only ONE widget has focus. Keys are
// consumed by the focused widget alone, so an unfocused field cannot be typed
// into, and moving focus is the form's job — which is why nothing here has a
// switch statement.
func Example() {
	modulePath := form.NewTextInputString(buffer.Rect{W: 32, H: 1}, "github.com/serkanalgur/termmosaic")
	vendor := form.NewCheckbox(buffer.Rect{W: 32, H: 1}, "vendor dependencies")
	vendor.SetState(form.Checked)
	tidy := form.NewToggle(buffer.Rect{W: 32, H: 1}, "tidy after install")
	tidy.SetOn(true)
	apply := form.NewButton(buffer.Rect{W: 32, H: 1}, "go mod tidy")

	s := stack(32, 4, modulePath, vendor, tidy, apply)
	// Focus lives on the button, which is where a user would leave it.
	apply.SetFocused(true)

	fmt.Println(show(32, 4, s))
	// Output:
	// github.com/serkanalgur/termmosa…
	// [x] vendor dependencies
	// [on ] tidy after install
	//          [go mod tidy]
}

// ---------------------------------------------------------------------------
// TextInput
// ---------------------------------------------------------------------------

// ExampleTextInput is a field with a value, a placeholder and a caret.
//
// SetFocused is what makes it look live: an unfocused field still draws, but the
// caret mark is only drawn while the field holds focus, so this example shows
// the focused appearance.
func ExampleTextInput() {
	field := form.NewTextInput(buffer.Rect{W: 28, H: 1})
	field.Placeholder = "module path"
	field.SetFocused(true)
	fmt.Println(show(28, 1, field))
	// Output:
	// module path
}

// ExampleTextInput_editing drives the field with the same events a terminal
// delivers rather than by calling setters, because the point of a text field is
// what its keys do.
//
// OnChange fires once per accepted edit and is not fired by SetText, so a
// program can restore a saved value without being told about it as if a user had
// typed it.
func ExampleTextInput_editing() {
	field := form.NewTextInputString(buffer.Rect{W: 24, H: 1}, "ter")
	field.SetFocused(true)
	field.OnChange = func(s string) { fmt.Printf("onChange(%q)\n", s) }

	// NewTextInputString leaves the caret at zero, so a keystroke inserts at the
	// front until something moves it. SetCursor is the programmatic equivalent
	// of End.
	field.SetCursor(field.Len())
	for _, r := range "rmosaic" {
		typeRune(field, r)
	}

	// Every keystroke is its own undo step, so seven edits are seven steps on
	// the stack. An EventPaste is ONE step, so pasting ten thousand characters
	// and pressing Ctrl-Z once removes the whole of it.
	field.Undo()
	fmt.Printf("after undo: %q, depth %d\n", field.Text(), field.UndoDepth())
	fmt.Println(show(24, 1, field))
	// Output:
	// onChange("terr")
	// onChange("terrm")
	// onChange("terrmo")
	// onChange("terrmos")
	// onChange("terrmosa")
	// onChange("terrmosai")
	// onChange("terrmosaic")
	// onChange("ter")
	// after undo: "ter", depth 0
	// ter
}

// ExampleTextInput_selection shows a partial selection made with Shift, which is
// what a user actually does. The selection style defaults to the text style with
// AttrReverse, so it is visible with no configuration at all and stays visible
// under NO_COLOR, which suppresses colour but not attributes.
func ExampleTextInput_selection() {
	field := form.NewTextInputString(buffer.Rect{W: 28, H: 1}, "go test ./...")
	field.SetFocused(true)

	press(field, termmosaic.KeyHome, 0)
	for i := 0; i < 8; i++ {
		press(field, termmosaic.KeyRight, termmosaic.ModShift)
	}
	fmt.Printf("selected: %q\n", field.SelectedText())
	fmt.Println(show(28, 1, field))
	// Output:
	// selected: "go test "
	// go test ./...
}

// ---------------------------------------------------------------------------
// TextArea
// ---------------------------------------------------------------------------

// ExampleTextArea is a multi-line field. The wrapping belongs to buffer rather
// than to the widget, so the content reflows to whatever width it is given and
// the widget caches the wrap against its own rect.
//
// There is no selection style here and that is deliberate: a multi-line
// selection is not in this widget's contract, and half a selection is worse than
// none.
func ExampleTextArea() {
	area := form.NewTextAreaString(buffer.Rect{W: 30, H: 4},
		"// a comment that is comfortably longer than thirty cells,\n// so it wraps")
	area.SetFocused(true)
	fmt.Println(show(30, 4, area))
	// Output:
	// // a comment that is
	// comfortably longer than thirty
	// cells,
	// // so it wraps
}

// ExampleTextArea_newline is the one key that distinguishes a TextArea from a
// TextInput: Enter inserts a newline rather than being handed to the enclosing
// form. Everything else about the key contract is the same, which is why the two
// widgets' carets are drawn the same way.
//
// A pasted newline goes in verbatim INCLUDING newlines, which is the other half
// of the difference: a TextInput strips CR and LF from a paste because it is one
// line by definition.
func ExampleTextArea_newline() {
	area := form.NewTextArea(buffer.Rect{W: 20, H: 3})
	area.SetFocused(true)
	area.OnChange = func(s string) { fmt.Printf("onChange(%q)\n", s) }

	typeRune(area, 'a')
	press(area, termmosaic.KeyEnter, 0)
	typeRune(area, 'b')
	fmt.Printf("text: %q\n", area.Text())

	// LineCount is VISUAL lines, not newlines: it is the wrap, so it is only
	// meaningful after a frame has been drawn at a known width.
	fmt.Println(show(20, 3, area))
	fmt.Printf("%d visual lines\n", area.LineCount())
	// Output:
	// onChange("a")
	// onChange("a\n")
	// onChange("a\nb")
	// text: "a\nb"
	// a
	// b
	//
	// 2 visual lines
}

// ---------------------------------------------------------------------------
// Select
// ---------------------------------------------------------------------------

// ExampleSelect is a closed list with one option highlighted.
//
// The marker is a separate COLUMN rather than part of the label: a marker that
// moved with the text would shift every row's label one cell, which is exactly
// the misalignment a column exists to prevent.
func ExampleSelect() {
	s := form.NewSelect(buffer.Rect{W: 24, H: 5},
		[]string{"alpine", "debian", "fedora", "nixos", "void"})
	s.SetSelected(2)
	s.SetFocused(true)
	fmt.Println(show(24, 5, s))
	// Output:
	// alpine
	//   debian
	// > fedora
	//   nixos
	//   void
}

// ExampleSelect_navigation drives the widget with its keys, which is where the
// difference from a Radio shows up: a Select is a dropdown the user opens and
// browses, so scrolling and highlighting are separate operations.
//
// A closed list never changes what it contains, so there is no type-ahead and no
// Remove. A Select whose options change is a different widget.
func ExampleSelect_navigation() {
	s := form.NewSelect(buffer.Rect{W: 22, H: 5}, []string{"all", "warn", "critical"})
	s.SetFocused(true)
	s.OnSelect = func(i int) { fmt.Printf("onSelect(%d) = %q\n", i, s.SelectedLabel()) }

	press(s, termmosaic.KeyDown, 0)
	press(s, termmosaic.KeyDown, 0)
	// Enter accepts the highlighted option WITHOUT moving the highlight, so a
	// user can look at a list, close it, and come back to where they were.
	press(s, termmosaic.KeyEnter, 0)
	fmt.Println(show(22, 5, s))
	// Output:
	// onSelect(2) = "critical"
	//   all
	//   warn
	// > critical
}

// ---------------------------------------------------------------------------
// Checkbox
// ---------------------------------------------------------------------------

// ExampleCheckbox is the ordinary two-state case.
func ExampleCheckbox() {
	c := form.NewCheckbox(buffer.Rect{W: 26, H: 1}, "vendor dependencies")
	c.SetState(form.Checked)
	fmt.Println(show(26, 1, c))
	// Output:
	// [x] vendor dependencies
}

// ExampleCheckbox_threeStates is the part of Checkbox a single widget cannot
// show. Indeterminate is never produced by toggling — only an application that
// has computed it from its children can reach it — which is why the three
// states are three SHAPES rather than one shape in three colours.
func ExampleCheckbox_threeStates() {
	all := form.NewCheckbox(buffer.Rect{W: 28, H: 1}, "widgets (24)")
	all.TriState = true
	all.SetState(form.Checked)

	some := form.NewCheckbox(buffer.Rect{W: 28, H: 1}, "visualisation (5)")
	some.TriState = true
	some.SetState(form.Indeterminate)

	none := form.NewCheckbox(buffer.Rect{W: 28, H: 1}, "release (0)")
	none.TriState = true
	none.SetState(form.Unchecked)

	fmt.Println(show(28, 3, stack(28, 3, all, some, none)))
	// Output:
	// [x] widgets (24)
	// [-] visualisation (5)
	// [ ] release (0)
}

// ExampleCheckbox_toggle is the key contract, and the one surprise in it:
// toggling moves Indeterminate to Checked, because a user pressing space on a
// mixed box is asserting "yes, all of it".
//
// TriState additionally lets Left and Right walk all three states, which is what
// an application whose children can be partly selected needs in order to clear
// the mixed state from the keyboard.
func ExampleCheckbox_toggle() {
	c := form.NewCheckbox(buffer.Rect{W: 22, H: 1}, "run tests")
	c.TriState = true
	c.SetState(form.Indeterminate)
	c.SetFocused(true)

	for i := 0; i < 3; i++ {
		press(c, termmosaic.KeySpace, 0)
		fmt.Printf("after space: %v\n", c.State())
	}
	fmt.Println(show(22, 1, c))
	// Output:
	// after space: checked
	// after space: unchecked
	// after space: checked
	// [x] run tests
}

// ---------------------------------------------------------------------------
// Radio
// ---------------------------------------------------------------------------

// ExampleRadio is a one-of-N group. Two independent non-colour signals carry it:
// the chosen option's marker is "(o)" against every other option's "( )", so the
// choice is a shape difference, and the focused option carries a ">" in its own
// column, so focus is not signalled by the same thing that signals selection.
func ExampleRadio() {
	g := form.NewRadio(buffer.Rect{W: 24, H: 4},
		[]string{"horizontal", "vertical", "both"})
	g.SetSelected(2)
	g.SetFocused(true)
	fmt.Println(show(24, 4, g))
	// Output:
	// ( ) horizontal
	//   ( ) vertical
	// > (o) both
}

// ExampleRadio_navigation is the behaviour that separates Radio from Select: an
// arrow CHOOSES. There is no separate commit step, because a radio group has no
// unselected state to commit from.
func ExampleRadio_navigation() {
	g := form.NewRadio(buffer.Rect{W: 20, H: 3}, []string{"light", "dark", "auto"})
	g.SetFocused(true)
	g.OnSelect = func(i int) { fmt.Printf("onSelect(%d) = %q\n", i, g.SelectedLabel()) }

	press(g, termmosaic.KeyDown, 0)
	press(g, termmosaic.KeyDown, 0)
	// Home and End choose rather than move a cursor, by the same rule.
	press(g, termmosaic.KeyHome, 0)
	fmt.Println(show(20, 3, g))
	// Output:
	// onSelect(1) = "dark"
	// onSelect(2) = "auto"
	// onSelect(0) = "light"
	// > (o) light
	//   ( ) dark
	//   ( ) auto
}

// ---------------------------------------------------------------------------
// Toggle
// ---------------------------------------------------------------------------

// ExampleToggle is a two-state switch, shown in both states.
//
// The state is the WORD — "on" or "off" — and the marker cell is the same width
// in both states, so the label never moves when the state changes. Nothing
// about the state depends on colour.
//
// A Toggle is not a Checkbox with a different marker: a toggle means "this is
// running right now" and a checkbox means "include this in the operation", and
// users read them differently. The difference is spelled in the marker.
func ExampleToggle() {
	off := form.NewToggle(buffer.Rect{W: 22, H: 1}, "wrap long lines")
	on := form.NewToggle(buffer.Rect{W: 22, H: 1}, "wrap long lines")
	on.SetOn(true)
	on.SetFocused(true)

	fmt.Println(show(22, 2, stack(22, 2, off, on)))
	// Output:
	// [off] wrap long lines
	// [on ] wrap long lines
}

// ExampleToggle_onChange separates a user action from a programmatic one. OnChange
// fires for what the user did; SetOn is how a program restores state and does not
// report itself back to the program as a user action.
//
// Every key toggles. There is no "set to on" key, because a toggle has two
// states and reaching either is the same action.
func ExampleToggle_onChange() {
	t := form.NewToggle(buffer.Rect{W: 20, H: 1}, "dark mode")
	t.SetFocused(true)
	t.OnChange = func(on bool) { fmt.Printf("onChange(%v)\n", on) }

	t.SetOn(true)
	press(t, termmosaic.KeySpace, 0)
	press(t, termmosaic.KeyEnter, 0)
	fmt.Printf("now: %v\n", t.On())
	// Output:
	// onChange(false)
	// onChange(true)
	// now: true
}

// ---------------------------------------------------------------------------
// Tabs
// ---------------------------------------------------------------------------

// ExampleTabs is a strip of tabs with one selected. The selected tab is
// BRACKETED and every other tab is space-padded, so every tab is exactly its
// label plus two cells: choosing a different tab cannot reflow the row, and the
// brackets are unambiguously the selection indicator rather than decoration.
func ExampleTabs() {
	t := form.NewTabs(buffer.Rect{W: 34, H: 1},
		[]string{"buffer", "render", "widgets"})
	t.SetSelected(1)
	t.SetFocused(true)
	fmt.Println(show(34, 1, t))
	// Output:
	// buffer [render] widgets
}

// ExampleTabs_navigation is the same rule as Radio: an arrow SELECTS, because a
// tab bar has no unselected state to commit from.
//
// A tab bar is a horizontal strip, so it does not use the virtual scrolling
// engine — that engine counts rows in a viewport of a given height, and one row
// of tabs would mean a capacity of one tab however wide the pane was.
//
// At 20 cells five tabs do not fit, and what happens is worth naming: the strip
// scrolls just far enough to bring the SELECTED tab into view, which truncates
// the last one and leaves the offset at zero rather than re-centring the row on
// the selection. Clamping, not re-deriving — ADR 0007 §6 rule 2.
func ExampleTabs_navigation() {
	t := form.NewTabs(buffer.Rect{W: 20, H: 1},
		[]string{"alpha", "beta", "gamma", "delta", "epsilon"})
	t.SetFocused(true)
	t.OnSelect = func(i int) { fmt.Printf("onSelect(%d) = %q\n", i, t.SelectedLabel()) }

	press(t, termmosaic.KeyRight, 0)
	press(t, termmosaic.KeyEnd, 0)
	fmt.Printf("offset %d, %d tabs\n", t.Offset(), t.Count())
	fmt.Println(show(20, 1, t))
	// Output:
	// onSelect(1) = "beta"
	// onSelect(4) = "epsilon"
	// offset 0, 5 tabs
	//  gamma  delta [epsil
}

// ---------------------------------------------------------------------------
// Button
// ---------------------------------------------------------------------------

// ExampleButton shows a button unfocused, focused and disabled in one frame.
//
// Focus is a SHAPE: the label is ringed with brackets, "[Save]", and the unfocused
// ring is spaces of the same width, so a button's size does not depend on its
// focus state and the row cannot shift under the user's cursor.
//
// The label is also CENTRED in its own rect rather than left-aligned, because a
// button's ring is as wide as its label and a column of buttons of different
// lengths reads as ragged otherwise.
func ExampleButton() {
	save := form.NewButton(buffer.Rect{W: 24, H: 1}, "Save")
	save.SetFocused(true)
	plain := form.NewButton(buffer.Rect{W: 24, H: 1}, "Save")
	no := form.NewButton(buffer.Rect{W: 24, H: 1}, "Save as…")
	no.SetDisabled(true)

	fmt.Println(show(24, 3, stack(24, 3, save, plain, no)))
	// Output:
	// [Save]
	//           Save
	//         Save as…
}

// ExampleButton_activate is the activation contract, and the part that is not
// styling: a DISABLED BUTTON CONSUMES NOTHING. Disabled is a field rather than a
// convention about which keys to ignore because "consumes nothing" is behaviour,
// not appearance.
//
// Keys are consumed only while focused, so a button in a form that does not hold
// focus ignores Enter rather than firing.
func ExampleButton_activate() {
	b := form.NewButton(buffer.Rect{W: 20, H: 1}, "Build")
	b.OnActivate = func() { fmt.Println("activated") }

	// Unfocused: consumed by nobody.
	press(b, termmosaic.KeyEnter, 0)

	b.SetFocused(true)
	press(b, termmosaic.KeyEnter, 0)

	off := form.NewButton(buffer.Rect{W: 20, H: 1}, "Build")
	off.OnActivate = func() { fmt.Println("never") }
	off.SetDisabled(true)
	off.SetFocused(true)
	press(off, termmosaic.KeyEnter, 0)
	fmt.Println("done")
	// Output:
	// activated
	// done
}

// ---------------------------------------------------------------------------
// KeyHint
// ---------------------------------------------------------------------------

// ExampleKeyHint is the discoverability half of a form: the keys available right
// now, in one truncated line.
//
// Each binding's key is BRACKETED — "[tab] next field" — so the key is a shape
// rather than a word, and no colour is used at all by default.
func ExampleKeyHint() {
	hint := form.NewKeyHint(buffer.Rect{W: 46, H: 1}, []form.Binding{
		form.NewBinding("tab", "next field"),
		form.NewBinding("enter", "apply"),
		form.NewBinding("ctrl+q", "quit"),
	})
	fmt.Println(show(46, 1, hint))
	// Output:
	// [tab] next field  [enter] apply  [ctrl+q] quit
}

// ExampleKeyHint_fromARegistry is the half that keeps help honest. SetEntries
// takes what keymap.Registry.DescribeGrouped returns, so the hint line is
// computed from the same tables Dispatch walks and cannot drift from the
// bindings.
//
// DescribeGrouped and not Describe, because SetEntries joins an entry's chords
// into one bracketed key: a command with three chords is one row here, where
// Describe would give three rows each holding one chord.
//
// The chords come back in Chord.String's canonical spelling, which is
// capitalised, so the registry's answer and a hand-written Binding differ in case
// but not in meaning.
func ExampleKeyHint_fromARegistry() {
	reg := keymap.New()
	reg.Register(
		keymap.Command{ID: "next", Desc: "next field", Group: "move", Run: func(keymap.Ctx) bool { return false }},
		keymap.Command{ID: "apply", Desc: "apply changes", Group: "edit", Run: func(keymap.Ctx) bool { return false }},
		keymap.Command{ID: "quit", Desc: "quit", Group: "app", Run: func(keymap.Ctx) bool { return false }},
	)
	// BindString parses a chord from text and REJECTS a binding it cannot parse,
	// rather than binding something else — which is why it returns an error and
	// why an example cannot ignore it the way it ignores Register's.
	for _, b := range []struct{ chord, id string }{
		{"tab", "next"},
		{"enter", "apply"},
		{"ctrl+q", "quit"},
	} {
		if err := reg.BindString(b.chord, keymap.CommandID(b.id), keymap.ScopeScreen, nil); err != nil {
			panic(err)
		}
	}

	hint := form.NewKeyHint(buffer.Rect{W: 62, H: 1}, nil)
	hint.SetEntries(reg.DescribeGrouped(keymap.ScopeScreen))
	fmt.Println(show(62, 1, hint))
	// Output:
	// [Ctrl+q] quit  [Enter] apply changes  [Tab] next field
}

// ExampleKeyHint_truncation is the property that makes a hint safe to put on
// every screen: an over-long hint is TRUNCATED WITH A MARKER rather than
// wrapped or clipped, so it stays one line and the reader can tell there was
// more.
func ExampleKeyHint_truncation() {
	hint := form.NewKeyHint(buffer.Rect{W: 24, H: 1}, []form.Binding{
		form.NewBinding("tab", "next field"),
		form.NewBinding("ctrl+shift+p", "command palette"),
		form.NewBinding("ctrl+q", "quit"),
	})
	fmt.Println(show(24, 1, hint))
	// Output:
	// [tab] next field  [ctrl…
}
