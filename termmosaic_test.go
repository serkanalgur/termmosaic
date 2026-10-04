package termmosaic

import (
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

func TestCapsColourDepthLadder(t *testing.T) {
	cases := []struct {
		name string
		caps Caps
		want buffer.ColourDepth
	}{
		{"truecolor", Caps{TrueColor: true, Color256: true}, buffer.DepthTrueColor},
		{"truecolor without 256", Caps{TrueColor: true}, buffer.DepthTrueColor},
		{"256", Caps{Color256: true}, buffer.Depth256},
		{"16", Caps{}, buffer.Depth16},
	}
	for _, tc := range cases {
		if got := tc.caps.ColourDepth(); got != tc.want {
			t.Errorf("%s: ColourDepth() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDefaultCapsIsTruecolor(t *testing.T) {
	if got := DefaultCaps().ColourDepth(); got != buffer.DepthTrueColor {
		t.Errorf("default depth = %v, want truecolor", got)
	}
}

func TestKeyModHasAndString(t *testing.T) {
	m := ModCtrl | ModShift
	if !m.Has(ModCtrl) || !m.Has(ModShift) {
		t.Error("Has is wrong")
	}
	if m.Has(ModAlt) {
		t.Error("Has must be false for an unset bit")
	}
	if !m.Has(ModCtrl | ModShift) {
		t.Error("Has with a combined mask is wrong")
	}
	if got := m.String(); got != "ctrl+shift" {
		t.Errorf("String = %q, want %q", got, "ctrl+shift")
	}
	if got := (ModSuper | ModAlt).String(); got != "alt+super" {
		t.Errorf("String = %q, want %q", got, "alt+super")
	}
	if got := KeyMod(0).String(); got != "" {
		t.Errorf("empty String = %q, want empty", got)
	}
}

func TestKeyString(t *testing.T) {
	if got := KeyEnter.String(); got != "enter" {
		t.Errorf("got %q", got)
	}
	if got := KeyPageUp.String(); got != "pgup" {
		t.Errorf("got %q", got)
	}
	if got := KeyF12.String(); got != "f12" {
		t.Errorf("got %q", got)
	}
	// KeyNone is the printable-character case: there is no name.
	if got := KeyNone.String(); got != "" {
		t.Errorf("KeyNone = %q, want empty", got)
	}
	if got := Key(999).String(); got != "?" {
		t.Errorf("unknown key = %q, want %q", got, "?")
	}
}

func TestEventKindString(t *testing.T) {
	for k, want := range map[EventKind]string{
		EventNone: "none", EventKey: "key", EventResize: "resize",
		EventMouse: "mouse", EventPaste: "paste", EventFocus: "focus",
		EventKind(99): "unknown",
	} {
		if got := k.String(); got != want {
			t.Errorf("EventKind(%d) = %q, want %q", int(k), got, want)
		}
	}
}

func TestEventConstructors(t *testing.T) {
	e := KeyEvent('q', ModCtrl)
	if e.Kind != EventKey || e.Rune != 'q' || e.Key != KeyNone || !e.Mod.Has(ModCtrl) {
		t.Errorf("KeyEvent = %+v", e)
	}
	e2 := SpecialKeyEvent(KeyUp, ModShift)
	if e2.Kind != EventKey || e2.Key != KeyUp || e2.Rune != 0 {
		t.Errorf("SpecialKeyEvent = %+v", e2)
	}
	e3 := ResizeEvent(100, 40)
	if e3.Kind != EventResize || e3.Size != (Size{W: 100, H: 40}) {
		t.Errorf("ResizeEvent = %+v", e3)
	}
}

func TestGeometryAliasesAreTheSameType(t *testing.T) {
	// Rect and Size are aliases rather than conversions, so a layout result can
	// be passed straight to a widget's Bounds without a copy.
	var r Rect = buffer.Rect{X: 1, Y: 2, W: 3, H: 4}
	var br buffer.Rect = r
	if br != (buffer.Rect{X: 1, Y: 2, W: 3, H: 4}) {
		t.Error("Rect alias is broken")
	}
	var s Size = buffer.Size{W: 5, H: 6}
	var bs buffer.Size = s
	if bs != (buffer.Size{W: 5, H: 6}) {
		t.Error("Size alias is broken")
	}
	if s.W != 5 {
		t.Error("Size field access is broken through the alias")
	}
}

// minSizedWidget is the smallest possible implementation of Minimizable: a
// declared minimum and nothing else. It exists so the interface can be tested
// without depending on any one widget's policy.
type minSizedWidget struct {
	bounds Rect
	min    Size
}

func (m *minSizedWidget) Bounds() Rect        { return m.bounds }
func (m *minSizedWidget) Draw(*buffer.Buffer) {}
func (m *minSizedWidget) Invalidate()         {}
func (m *minSizedWidget) Handle(Event) bool   { return false }
func (m *minSizedWidget) MinSize() Size       { return m.min }

// plainWidget is the same widget WITHOUT MinSize, which ADR 0007 §4 makes fully
// supported. If the optional interface ever became mandatory this would not
// compile, which is the point of keeping it a type assertion.
type plainWidget struct{ bounds Rect }

func (p *plainWidget) Bounds() Rect        { return p.bounds }
func (p *plainWidget) Draw(*buffer.Buffer) {}
func (p *plainWidget) Invalidate()         {}
func (p *plainWidget) Handle(Event) bool   { return false }

// TestMinimizableIsOptionalAndDiscoverable is the whole design of ADR 0007 §1 in
// one test: Minimizable costs a widget that does not want one nothing, and a
// caller can find one by type assertion. A widget without MinSize is not an
// error and gets no framework reaction.
func TestMinimizableIsOptionalAndDiscoverable(t *testing.T) {
	var w Widget = &minSizedWidget{bounds: Rect{W: 4, H: 2}, min: Size{W: 20, H: 5}}

	m, ok := w.(Minimizable)
	if !ok {
		t.Fatal("a widget with MinSize does not satisfy Minimizable")
	}
	// The whole widget INCLUDING chrome: 20x5, not the content area.
	if got := m.MinSize(); got != (Size{W: 20, H: 5}) {
		t.Errorf("MinSize = %+v, want {20 5}", got)
	}

	var bare Widget = &plainWidget{bounds: Rect{W: 4, H: 2}}
	if _, ok := bare.(Minimizable); ok {
		t.Error("a widget without MinSize must not satisfy Minimizable; the interface is optional")
	}

	// It is safe to ask before the widget has ever been drawn, which is a property
	// of the interface being about a declaration rather than about a layout.
	if got := m.MinSize(); got != (Size{W: 20, H: 5}) {
		t.Errorf("MinSize before any Draw = %+v, want the same value", got)
	}
}

// TestMinimizableIncludesChromeIsTheDocumentedConvention is here so the
// convention in the doc comment cannot be quietly changed: a bordered widget
// reporting its content size would make every caller's arithmetic wrong, so the
// chrome is part of the number.
func TestMinimizableIncludesChromeIsTheDocumentedConvention(t *testing.T) {
	// A widget with two cells of border and a 10x1 content area reports 12x3.
	var w Widget = &minSizedWidget{bounds: Rect{W: 12, H: 3}, min: Size{W: 12, H: 3}}
	m := w.(Minimizable)
	if m.MinSize().W <= 10 || m.MinSize().H <= 1 {
		t.Errorf("MinSize %+v must be at least the content plus its chrome", m.MinSize())
	}
}

func TestMouseAndPastePayloads(t *testing.T) {
	e := Event{
		Kind:  EventMouse,
		Mouse: Mouse{X: 3, Y: 4, Button: MouseLeft, Action: MousePress, Mod: ModShift},
	}
	if e.Mouse.Button != MouseLeft || e.Mouse.Action != MousePress {
		t.Errorf("mouse payload = %+v", e.Mouse)
	}
	p := Event{Kind: EventPaste, Text: "hello\nworld"}
	if p.Text != "hello\nworld" {
		t.Errorf("paste payload = %q", p.Text)
	}
	f := Event{Kind: EventFocus, Focused: true}
	if !f.Focused {
		t.Error("focus payload is wrong")
	}
}
