package termmosaic

import (
	"testing"
	"unsafe"
)

// TestEventSizeIsBounded is the mandatory guard ADR 0005 requires, in the same
// spirit and for the same reason as TestCellHasNoPadding.
//
// The hazard here is different from ADR 0002's and the mitigation is the same
// shape. There, a padding-free type could silently make a byte compare unsound.
// Here, Event is a struct copied BY VALUE on the hot path — once per keystroke,
// through Widget.Handle — and the natural way to make it slow later is to add a
// field nobody measures. A handful of extra payloads would grow it without any
// test noticing, and the cost would show up only as input latency nobody can
// attribute.
//
// So the bound is pinned: sizeof(Event) is 112 bytes today and a change to it
// has to be a decision somebody made on purpose.
func TestEventSizeIsBounded(t *testing.T) {
	const want = 112
	if got := unsafe.Sizeof(Event{}); got != want {
		t.Fatalf("sizeof(Event) = %d, want %d; Event is copied by value on the hot path and must not grow without a deliberate decision (ADR 0005)", got, want)
	}
}

// TestEventFieldOffsets pins the layout so a future reorder that happens to keep
// the total at 112 bytes is still caught.
//
// The two fields that make it 112 rather than less are the string (Text) and the
// pointer (Compose); the pointer is the deliberate asymmetry ADR 0005 §7 names,
// and it is why this type has a field allowed to be nil at all.
func TestEventFieldOffsets(t *testing.T) {
	var e Event
	checks := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"Kind", unsafe.Offsetof(e.Kind), 0},
		{"Key", unsafe.Offsetof(e.Key), 8},
		{"Rune", unsafe.Offsetof(e.Rune), 16},
		{"Mod", unsafe.Offsetof(e.Mod), 20},
		{"Type", unsafe.Offsetof(e.Type), 21},
		{"Mouse", unsafe.Offsetof(e.Mouse), 24},
		{"Size", unsafe.Offsetof(e.Size), 64},
		{"Text", unsafe.Offsetof(e.Text), 80},
		{"Compose", unsafe.Offsetof(e.Compose), 96},
		{"Truncated", unsafe.Offsetof(e.Truncated), 104},
		{"Focused", unsafe.Offsetof(e.Focused), 105},
	}
	for _, tc := range checks {
		if tc.got != tc.want {
			t.Errorf("offsetof(Event.%s) = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// TestEventComposeIsTheOnlyNilableField states the invariant in code rather than
// only in a doc comment: every payload except Compose is a value, so a nil check
// on any other field is a compile error and a nil check on Compose is the one
// the model intends.
func TestEventComposeIsTheOnlyNilableField(t *testing.T) {
	var zero Event
	if zero.Compose != nil {
		t.Error("the zero Event must have a nil Compose")
	}

	// A synthesized composition event carries the pointer and nothing else.
	ev := Event{
		Kind: EventCompose,
		Compose: &Compose{
			Text:   "に",
			Cursor: 3,
			Phase:  ComposeUpdate,
		},
	}
	if ev.Compose.Text != "に" || ev.Compose.Cursor != 3 || ev.Compose.Phase != ComposeUpdate {
		t.Errorf("event = %+v, want its Compose payload intact", ev)
	}
	if got := ev.Compose.Phase.String(); got != "update" {
		t.Errorf("ComposePhase.String() = %q, want %q", got, "update")
	}
}

// TestEventKindStringCoversEveryKind is here because EventCompose was added after
// EventKind.String was written, and a switch that silently falls through to
// "unknown" is the kind of omission nobody notices until a widget logs it.
func TestEventKindStringCoversEveryKind(t *testing.T) {
	for k, want := range map[EventKind]string{
		EventNone:    "none",
		EventKey:     "key",
		EventResize:  "resize",
		EventMouse:   "mouse",
		EventPaste:   "paste",
		EventFocus:   "focus",
		EventCompose: "compose",
	} {
		if got := k.String(); got != want {
			t.Errorf("EventKind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
	if got := EventKind(99).String(); got != "unknown" {
		t.Errorf("EventKind(99).String() = %q, want %q", got, "unknown")
	}
}
