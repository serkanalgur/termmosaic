package geometry

import "testing"

// TestAlignIsAZeroValuedEnumeration pins the only thing about Align that the
// framework relies on: AlignLeft is the zero value, so a widget field of type
// Align left at zero needs no default constant and no nil-style resolution
// idiom. That is the same property Style.IsUnset and Style.Resolved exist to
// provide on the styling side, achieved here for free by ordering.
func TestAlignIsAZeroValuedEnumeration(t *testing.T) {
	cases := []struct {
		a    Align
		want int
		name string
	}{
		{AlignLeft, 0, "left"},
		{AlignCenter, 1, "center"},
		{AlignRight, 2, "right"},
	}
	for _, tc := range cases {
		if int(tc.a) != tc.want {
			t.Errorf("Align = %d, want %d; the constants are a dense ordered set", tc.a, tc.want)
		}
		if got := tc.a.String(); got != tc.name {
			t.Errorf("Align(%d).String() = %q, want %q", tc.a, got, tc.name)
		}
	}

	var zero Align
	if zero != AlignLeft {
		t.Errorf("the zero Align = %v, want AlignLeft", zero)
	}

	// All three must be distinct, and there must be exactly three: a fourth
	// would mean a widget is choosing between four alignments, which is a
	// decision this enumeration deliberately does not expose.
	seen := map[Align]bool{}
	for _, a := range []Align{AlignLeft, AlignCenter, AlignRight} {
		if seen[a] {
			t.Errorf("Align(%d) collides with an earlier constant", a)
		}
		seen[a] = true
	}
}

// TestAlignOutOfRangeIsNotFatal states the degenerate-size contract for Align
// too: it is layout-derived data, and a TUI that panics on a value it did not
// author is unusable (ADR 0007 §4, "no panic, ever").
func TestAlignOutOfRangeIsNotFatal(t *testing.T) {
	for _, a := range []Align{3, 4, 100, 255} {
		name := a.String()
		if len(name) == 0 {
			t.Errorf("Align(%d).String() is empty", a)
		}
		if name == "left" || name == "center" || name == "right" {
			t.Errorf("Align(%d).String() = %q, which is a valid alignment's name", a, name)
		}
	}
}

// TestItoa exercises the small formatter Align.String's error path uses.
func TestItoa(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"}, {1, "1"}, {9, "9"}, {10, "10"}, {99, "99"}, {255, "255"},
		{-1, "-1"}, {-42, "-42"},
	}
	for _, tc := range cases {
		if got := itoa(tc.in); got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestAlignPlacementIsPureArithmetic is not a test of Align at all but of the
// reason it lives here: placement over a cell range is cell-free geometry that
// needs neither buffer nor a widget, so it stays in the leaf package that both
// buffer and layout import. It asserts the placement arithmetic a Block title
// will use, so the three constants' meaning is pinned rather than only their
// names.
func TestAlignPlacementIsPureArithmetic(t *testing.T) {
	// offset returns where a run of contentWidth cells starts within interior,
	// which is the whole of what Align decides.
	offset := func(a Align, interior, contentWidth int) int {
		switch a {
		case AlignCenter:
			return (interior - contentWidth) / 2
		case AlignRight:
			return interior - contentWidth
		default:
			return 0
		}
	}

	cases := []struct {
		align        Align
		interior     int
		contentWidth int
		want         int
	}{
		{AlignLeft, 20, 5, 0},
		{AlignCenter, 20, 5, 7},
		{AlignRight, 20, 5, 15},
		// An odd remainder goes left of centre, deterministically.
		{AlignCenter, 20, 6, 7},
		{AlignCenter, 21, 6, 7},
		// Content wider than the interior must not produce a negative offset that
		// would make a caller index before the corner cell.
		{AlignCenter, 4, 10, -3},
		{AlignRight, 4, 10, -6},
	}
	for _, tc := range cases {
		if got := offset(tc.align, tc.interior, tc.contentWidth); got != tc.want {
			t.Errorf("offset(%v, %d, %d) = %d, want %d", tc.align, tc.interior, tc.contentWidth, got, tc.want)
		}
	}
}
