package form

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// widgetCase is one widget in a table, built fresh for each test so no case can
// be affected by another's cached state.
type widgetCase struct {
	name string
	make func() termmosaic.Widget
	// screen is the buffer size the case is drawn into, chosen larger than every
	// rect the case is exercised at so that a widget writing outside its own
	// bounds is visible in the assertion rather than clipped by the buffer.
	screenW, screenH int
}

// allWidgets is every widget in this package, in the order a form reads top to
// bottom. It is the fixture for the catalog-wide properties — degenerate sizes,
// allocation-free draws, click-outside, purity of MinSize — that every widget in
// the set must satisfy and that would otherwise be nine copies of the same test.
func allWidgets() []widgetCase {
	return []widgetCase{
		{"TextInput", func() termmosaic.Widget {
			in := NewTextInputString(buffer.Rect{W: 20, H: 1}, "hello")
			in.SetFocused(true)
			in.SelectAll()
			return in
		}, 24, 4},
		{"TextArea", func() termmosaic.Widget {
			a := NewTextAreaString(buffer.Rect{W: 20, H: 4}, "wrapped text over\ntwo lines")
			a.SetFocused(true)
			return a
		}, 24, 6},
		{"Select", func() termmosaic.Widget {
			s := NewSelect(buffer.Rect{W: 20, H: 4}, []string{"one", "two", "three"})
			s.SetSelected(1)
			s.SetFocused(true)
			return s
		}, 24, 6},
		{"Radio", func() termmosaic.Widget {
			g := NewRadio(buffer.Rect{W: 20, H: 3}, []string{"one", "two", "three"})
			g.SetSelected(1)
			g.SetFocused(true)
			return g
		}, 24, 6},
		{"Checkbox", func() termmosaic.Widget {
			c := NewCheckbox(buffer.Rect{W: 20, H: 1}, "a label")
			c.SetState(Checked)
			c.SetFocused(true)
			return c
		}, 24, 4},
		{"Toggle", func() termmosaic.Widget {
			t := NewToggle(buffer.Rect{W: 20, H: 1}, "a label")
			t.SetOn(true)
			t.SetFocused(true)
			return t
		}, 24, 4},
		{"Tabs", func() termmosaic.Widget {
			tabs := NewTabs(buffer.Rect{W: 20, H: 1}, []string{"one", "two", "three"})
			tabs.SetSelected(1)
			tabs.SetFocused(true)
			return tabs
		}, 24, 4},
		{"Button", func() termmosaic.Widget {
			b := NewButton(buffer.Rect{W: 20, H: 1}, "Save")
			b.SetFocused(true)
			return b
		}, 24, 4},
		{"KeyHint", func() termmosaic.Widget {
			return NewKeyHint(buffer.Rect{W: 20, H: 1}, []Binding{
				NewBinding("tab", "next"), NewBinding("enter", "select"),
			})
		}, 24, 4},
	}
}

// degenerateRects are the sizes a terminal can hand a widget: nothing, one cell,
// two cells, one row, one column, and a rect entirely off-screen. Every one of
// them is in ADR 0007 §4's contract table, and a widget that panics or blanks on
// any of them is the catalog-wide bug that ADR exists to prevent.
var degenerateRects = []buffer.Rect{
	{W: 0, H: 0},
	{X: 3, Y: 2, W: 0, H: 4},
	{X: 3, Y: 2, W: 4, H: 0},
	{W: 1, H: 1},
	{X: 1, Y: 1, W: 1, H: 1},
	{W: 2, H: 2},
	{X: 2, W: 1, H: 3},
	{X: 2, W: 3, H: 1},
	{X: -4, Y: -4, W: 3, H: 3}, // entirely off-screen to the left and above
	{X: 40, Y: 40, W: 3, H: 3},
}

// TestEveryWidgetIsTotalAtEveryDegenerateSize is ADR 0007 §4 for the whole set at
// once: every widget draws at every degenerate rect without panicking, and
// nothing it writes falls outside its own bounds.
func TestEveryWidgetIsTotalAtEveryDegenerateSize(t *testing.T) {
	for _, tc := range allWidgets() {
		for _, r := range degenerateRects {
			w := tc.make()
			if s, ok := w.(interface{ SetBounds(buffer.Rect) }); ok {
				s.SetBounds(r)
			} else {
				t.Fatalf("%s has no SetBounds; every widget in this package must accept a new rect", tc.name)
			}
			buf := buffer.NewBuffer(tc.screenW, tc.screenH)
			w.Draw(buf) // must not panic
			w.Invalidate()

			// Nothing may be written outside Bounds. A widget that painted the whole
			// buffer instead of its own rect would still not panic, so this is the
			// assertion that catches it.
			assertWritesInsideBounds(t, tc.name, r, buf)
		}
	}
}

// assertWritesInsideBounds checks that every cell outside r still holds the
// buffer's default cell, which is what "a widget's space is Bounds()" means when
// observed from the outside.
func assertWritesInsideBounds(t *testing.T, name string, r buffer.Rect, buf *buffer.Buffer) {
	t.Helper()
	// Only rows and columns the widget could plausibly have reached are checked:
	// a rect entirely off-screen cannot have painted anything, so the check is
	// vacuous there and asserting it would be theatre.
	if r.X >= buf.Width() || r.Y >= buf.Height() {
		return
	}
	for y := 0; y < buf.Height(); y++ {
		for x := 0; x < buf.Width(); x++ {
			if r.Contains(x, y) {
				continue
			}
			if c := buf.CellAt(x, y); c != buffer.DefaultCell {
				t.Fatalf("%s wrote outside Bounds at (%d,%d): a widget's space is Bounds(), "+
					"never the buffer", name, x, y)
			}
		}
	}
}

// TestEveryWidgetSurvivesAWholeEventBattery is the "never panic on input" half of
// the contract, driven with events a widget should mostly refuse: keys it does
// not own, a resize, a focus report, a malformed paste and clicks far outside.
func TestEveryWidgetSurvivesAWholeEventBattery(t *testing.T) {
	events := []termmosaic.Event{
		termmosaic.Event{},
		termmosaic.ResizeEvent(0, 0),
		termmosaic.ResizeEvent(200, 60),
		{Kind: termmosaic.EventFocus, Focused: true},
		{Kind: termmosaic.EventFocus, Focused: false},
		{Kind: termmosaic.EventPaste},
		{Kind: termmosaic.EventPaste, Text: "x\ny", Truncated: true},
		{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{X: -5, Y: -5, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress}},
		{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{X: 999, Y: 999, Button: termmosaic.MouseLeft, Action: termmosaic.MouseDrag}},
		{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{X: 0, Y: 0, Button: termmosaic.MouseWheelUp, Action: termmosaic.MousePress}},
		{Kind: termmosaic.EventKey, Key: termmosaic.KeyF12, Type: termmosaic.KeyRelease},
		{Kind: termmosaic.EventKey, Rune: 0},
		{Kind: termmosaic.EventKey, Rune: 'a', Mod: termmosaic.ModCtrl | termmosaic.ModAlt | termmosaic.ModShift},
	}
	for _, k := range []termmosaic.Key{
		termmosaic.KeyUp, termmosaic.KeyDown, termmosaic.KeyLeft, termmosaic.KeyRight,
		termmosaic.KeyHome, termmosaic.KeyEnd, termmosaic.KeyPageUp, termmosaic.KeyPageDown,
		termmosaic.KeyDelete, termmosaic.KeyInsert, termmosaic.KeyTab, termmosaic.KeyBacktab,
		termmosaic.KeyEnter, termmosaic.KeySpace, termmosaic.KeyBackspace, termmosaic.KeyEscape,
	} {
		events = append(events, termmosaic.SpecialKeyEvent(k, 0))
		events = append(events, termmosaic.SpecialKeyEvent(k, termmosaic.ModCtrl))
		events = append(events, termmosaic.SpecialKeyEvent(k, termmosaic.ModShift))
	}

	for _, tc := range allWidgets() {
		w := tc.make()
		for _, ev := range events {
			w.Handle(ev) // must not panic
			w.Draw(buffer.NewBuffer(tc.screenW, tc.screenH))
		}
	}
}

// TestEveryWidgetDrawIsAllocationFreeInSteadyState is ADR 0008 §4 for the whole
// set: the first Draw at a rect may build a cache, and every Draw after it must
// not allocate at all.
func TestEveryWidgetDrawIsAllocationFreeInSteadyState(t *testing.T) {
	for _, tc := range allWidgets() {
		w := tc.make()
		buf := buffer.NewBuffer(tc.screenW, tc.screenH)
		w.Draw(buf) // warm the cache
		if got := testing.AllocsPerRun(100, func() { w.Draw(buf) }); got != 0 {
			t.Errorf("%s.Draw allocated %v times per run once warm, want 0", tc.name, got)
		}
	}
}

// TestEveryWidgetDrawIsAllocationFreeAfterAStateChange is the harder version: a
// widget whose state changed on the previous frame must still allocate nothing on
// the frame after it, because the rebuild happened on the change and the read
// happens on the frame.
func TestEveryWidgetDrawIsAllocationFreeAfterAStateChange(t *testing.T) {
	for _, tc := range allWidgets() {
		w := tc.make()
		buf := buffer.NewBuffer(tc.screenW, tc.screenH)
		w.Draw(buf)
		// Offer the event battery once, so any cache invalidated by input is
		// rebuilt, then measure the steady state that follows.
		for _, ev := range []termmosaic.Event{
			termmosaic.KeyEvent('a', 0),
			termmosaic.SpecialKeyEvent(termmosaic.KeyDown, 0),
			termmosaic.SpecialKeyEvent(termmosaic.KeyRight, 0),
		} {
			w.Handle(ev)
		}
		w.Draw(buf)
		if got := testing.AllocsPerRun(100, func() { w.Draw(buf) }); got != 0 {
			t.Errorf("%s.Draw allocated %v times per run after input, want 0", tc.name, got)
		}
	}
}

// TestEveryMinSizeIsPureAndAtLeastOneCell pins ADR 0007 §2's convention that
// MinSize is pure: it may not depend on the current bounds, on content, or on
// focus, or a layout would resize under the user.
func TestEveryMinSizeIsPureAndAtLeastOneCell(t *testing.T) {
	for _, tc := range allWidgets() {
		w := tc.make()
		m, ok := w.(termmosaic.Minimizable)
		if !ok {
			t.Errorf("%s does not implement Minimizable; every widget in this package has a "+
				"smallest meaningful size", tc.name)
			continue
		}
		first := m.MinSize()
		if first.W < 1 || first.H < 1 {
			t.Errorf("%s.MinSize = %+v, want at least 1x1", tc.name, first)
		}
		// A second call before any Draw must give the same answer: MinSize is
		// required to be safe before the widget has ever been drawn.
		if got := m.MinSize(); got != first {
			t.Errorf("%s.MinSize = %+v on a second call, want %+v", tc.name, got, first)
		}
		// And it must not have changed after a Draw and some input.
		w.Draw(buffer.NewBuffer(tc.screenW, tc.screenH))
		w.Handle(termmosaic.KeyEvent('q', 0))
		if got := m.MinSize(); got != first {
			t.Errorf("%s.MinSize changed to %+v after a draw and input, want %+v", tc.name, got, first)
		}
	}
}

// TestEveryFocusableWidgetIgnoresClicksOutsideItsBounds is the negative mouse
// case for the whole set at once: a click-through bug is invisible until a form
// has three widgets on it and the click lands on the wrong one.
func TestEveryFocusableWidgetIgnoresClicksOutsideItsBounds(t *testing.T) {
	for _, tc := range allWidgets() {
		w := tc.make()
		f, ok := w.(termmosaic.Focusable)
		if !ok {
			continue
		}
		f.SetFocused(true)
		// The bounds are known; every click outside them must be refused.
		r := w.Bounds()
		for _, at := range [][2]int{
			{r.X - 1, r.Y}, {r.Right(), r.Y}, {r.X, r.Y - 1}, {r.X, r.Bottom()},
			{-100, -100}, {10000, 10000},
		} {
			if clickAt(w, at[0], at[1]) {
				t.Errorf("%s consumed a click at %v, want it refused: outside Bounds %+v",
					tc.name, at, r)
			}
		}
		if !f.Focused() {
			t.Errorf("%s lost focus to an outside click", tc.name)
		}
	}
}

// TestEveryUnfocusedWidgetConsumesNoKeys is what makes a form usable: a widget
// that is not focused must not swallow a keystroke meant for a sibling.
func TestEveryUnfocusedWidgetConsumesNoKeys(t *testing.T) {
	for _, tc := range allWidgets() {
		w := tc.make()
		if f, ok := w.(termmosaic.Focusable); ok {
			f.SetFocused(false)
		}
		for _, seq := range []string{"a", "\r", " ", "\x1b[C", "\x1b[A"} {
			if w.Handle(decodeKey(t, seq)) {
				t.Errorf("%s consumed %q while unfocused", tc.name, seq)
			}
		}
	}
}

// TestEveryWidgetRendersThroughTheWholeStack renders every widget through the
// renderer, the two-tier diff and the ANSI encoder, which is the only way to know
// a widget's cells survive the whole path rather than merely landing in a buffer.
func TestEveryWidgetRendersThroughTheWholeStack(t *testing.T) {
	for _, tc := range allWidgets() {
		w := tc.make()
		sink := widgettest.Render(t, tc.screenW, tc.screenH, 2, w)
		if lines := strings.Split(widgettest.Screen(sink), "\n"); len(lines) != tc.screenH {
			t.Errorf("%s rendered %d screen lines, want %d", tc.name, len(lines), tc.screenH)
		}
	}
}

// TestEveryWidgetRendersAtOneCellThroughTheWholeStack covers the terminal that
// has been dragged down to a single cell, where the encoder sees a frame the
// widget had to survive.
func TestEveryWidgetRendersAtOneCellThroughTheWholeStack(t *testing.T) {
	for _, tc := range allWidgets() {
		w := tc.make()
		widgettest.Render(t, 1, 1, 2, w)
	}
}

// setBackground sets a widget's Background field.
//
// Every widget in this package carries one, and the repaint test needs to set it
// on nine different concrete types. A type switch here is the alternative to nine
// exported setters that nothing else would call.
func setBackground(w termmosaic.Widget, st buffer.Style) {
	switch v := w.(type) {
	case *TextInput:
		v.Background = st
	case *TextArea:
		v.Background = st
	case *Select:
		v.Background = st
	case *Radio:
		v.Background = st
	case *Checkbox:
		v.Background = st
	case *Toggle:
		v.Background = st
	case *Tabs:
		v.Background = st
	case *Button:
		v.Background = st
	case *KeyHint:
		v.Background = st
	default:
		panic("form: a widget with no Background field cannot be repaint-tested")
	}
}

// TestEveryWidgetPaintsItsWholeBoundsBeforeDrawing is ADR 0007 §1 rule 3 for the
// whole set, asserted in the way that can actually fail.
//
// The buffer is pre-filled with a visible marker rune in a different style, and
// the widget is given a Background that is neither the marker's nor the buffer's
// default. After one Draw, NO cell inside Bounds may still hold the marker, and
// every cell outside Bounds must still hold it. A widget that paints only the
// cells it draws content into fails the first half; one that paints the whole
// buffer fails the second.
//
// This is the assertion whose absence lets a repaint bug ship: with the default
// background, painting nothing and painting everything produce the same cells, so
// the test passes either way.
func TestEveryWidgetPaintsItsWholeBoundsBeforeDrawing(t *testing.T) {
	bg := buffer.NewStyle(buffer.NewColour(0x00, 0x00, 0x80), buffer.NewColour(0x20, 0x20, 0x20), 0)
	marker := buffer.NewStyle(buffer.NewColour(0xff, 0x00, 0x00), buffer.DefaultColour, 0)

	for _, tc := range allWidgets() {
		w := tc.make()
		r := buffer.Rect{X: 1, Y: 1, W: tc.screenW - 4, H: tc.screenH - 2}
		w.(interface{ SetBounds(buffer.Rect) }).SetBounds(r)
		setBackground(w, bg)

		buf := buffer.NewBuffer(tc.screenW, tc.screenH)
		buf.Fill(marker.Cell('Z'))

		w.Draw(buf)

		painted := 0
		for y := 0; y < tc.screenH; y++ {
			for x := 0; x < tc.screenW; x++ {
				c := buf.CellAt(x, y)
				if !r.Contains(x, y) {
					if c.Ch != 'Z' || c.BG != marker.BG {
						t.Fatalf("%s painted (%d,%d), which is outside its Bounds %+v", tc.name, x, y, r)
					}
					continue
				}
				// Inside Bounds, NO cell may still hold the pre-existing fill. A cell
				// the widget drew content into carries that content's own style — a
				// widget's Background and its text style are independent — so the
				// assertion is about the marker's absence rather than about the
				// background being present, which is what makes it catch a widget
				// that simply does not repaint.
				if c.BG == marker.BG && c.Ch == 'Z' {
					t.Fatalf("%s left the pre-existing cell at (%d,%d) inside Bounds: a widget "+
						"must repaint its whole rect before drawing content into it", tc.name, x, y)
				}
				if c.BG == bg.BG {
					painted++
				}
			}
		}
		if painted == 0 {
			t.Fatalf("%s painted no cell in its own background at all: the rect was never filled", tc.name)
		}
	}
}

// TestEveryWidgetRepaintsCellsItsContentVacated is the same rule for the case a
// static test cannot reach: the widget's rect does not change, but its content
// does, so the cells the content used to occupy must be repainted rather than left
// holding the old content.
//
// It is the reason the rule exists at all: a widget that shrinks its content and
// does not repaint leaves stale cells on screen forever, because the renderer
// diffs and never clears.
func TestEveryWidgetRepaintsCellsItsContentVacated(t *testing.T) {
	bg := buffer.NewStyle(buffer.DefaultColour, buffer.NewColour(0x10, 0x40, 0x10), 0)
	for _, tc := range allWidgets() {
		w := tc.make()
		r := buffer.Rect{X: 0, Y: 0, W: tc.screenW, H: tc.screenH}
		w.(interface{ SetBounds(buffer.Rect) }).SetBounds(r)
		setBackground(w, bg)

		staleBG := buffer.NewColour(0x80, 0x10, 0x10)
		stale := staleBG
		buf := buffer.NewBuffer(tc.screenW, tc.screenH)
		buf.Fill(buffer.NewStyle(buffer.DefaultColour, staleBG, 0).Cell('Z'))
		w.Draw(buf)
		first := 0
		for y := 0; y < tc.screenH; y++ {
			for x := 0; x < tc.screenW; x++ {
				if buf.CellAt(x, y).BG == bg.BG {
					first++
				}
			}
		}

		// Replace the content with as little as the widget can hold, then draw
		// again into the same rect.
		switch v := w.(type) {
		case *TextInput:
			v.SetText("")
		case *TextArea:
			v.SetText("")
		case *Select:
			v.SetLabels(nil)
		case *Radio:
			v.SetLabels(nil)
		case *Tabs:
			v.SetTabs(nil)
		case *Checkbox:
			v.SetLabel("")
		case *Toggle:
			v.SetLabel("")
		case *Button:
			v.SetLabel("")
		case *KeyHint:
			v.SetBindings(nil)
		}
		w.Draw(buf)

		repainted := 0
		for y := 0; y < tc.screenH; y++ {
			for x := 0; x < tc.screenW; x++ {
				c := buf.CellAt(x, y)
				if c.Ch == 'Z' && c.BG == stale {
					t.Fatalf("%s left the pre-existing cell at (%d,%d) after its content shrank: "+
						"the rows it no longer draws must be repainted", tc.name, x, y)
				}
				if c.BG == bg.BG {
					repainted++
				}
			}
		}
		if repainted <= first {
			t.Fatalf("%s repainted %d cells after its content shrank, want more than the %d it "+
				"had before: the vacated cells must be refilled", tc.name, repainted, first)
		}
	}
}
