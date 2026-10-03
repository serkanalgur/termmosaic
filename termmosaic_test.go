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
