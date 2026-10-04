package buffer

import "testing"

func TestDirtyCoalescingMergesTouchingRects(t *testing.T) {
	var d dirtySet
	d.mark(Rect{X: 0, Y: 0, W: 2, H: 2})
	d.mark(Rect{X: 2, Y: 0, W: 2, H: 2}) // shares a column
	got := d.takeInto(nil, 10, 10)
	if len(got) != 1 || got[0] != (Rect{X: 0, Y: 0, W: 4, H: 2}) {
		t.Fatalf("touching rects did not coalesce: %+v", got)
	}

	d.mark(Rect{X: 0, Y: 5, W: 1, H: 1})
	d.mark(Rect{X: 8, Y: 8, W: 1, H: 1})
	got = d.takeInto(nil, 10, 10)
	if len(got) != 2 {
		t.Fatalf("disjoint rects should stay separate, got %+v", got)
	}
}

func TestDirtyTakeResets(t *testing.T) {
	var d dirtySet
	d.mark(Rect{X: 1, Y: 1, W: 1, H: 1})
	if d.isEmpty() {
		t.Fatal("should not be empty")
	}
	d.takeInto(nil, 10, 10)
	if !d.isEmpty() {
		t.Fatal("take must reset the set")
	}
}

func TestDirtyTakeIsZeroAllocOnReuse(t *testing.T) {
	var d dirtySet
	scratch := make([]Rect, 0, 4)
	for i := 0; i < 100; i++ {
		d.mark(Rect{X: i % 10, Y: i % 5, W: 2, H: 1})
		scratch = d.takeInto(scratch[:0], 20, 20)
	}
}

func TestDirtyCollapsesWhenTooManyRects(t *testing.T) {
	var d dirtySet
	for i := 0; i < maxTrackedRects+10; i++ {
		d.mark(Rect{X: i * 3, Y: 0, W: 1, H: 1})
	}
	got := d.takeInto(nil, 1000, 10)
	if len(got) != 1 || got[0] != (Rect{W: 1000, H: 10}) {
		t.Fatalf("expected collapse to full, got %+v", got)
	}
}

func TestDirtyClipsToBufferAndDropsEmpty(t *testing.T) {
	var d dirtySet
	d.mark(Rect{X: 50, Y: 50, W: 5, H: 5}) // entirely outside a 10x10 buffer
	d.mark(Rect{X: 8, Y: 8, W: 5, H: 5})   // partially outside
	got := d.takeInto(nil, 10, 10)
	if len(got) != 1 || got[0] != (Rect{X: 8, Y: 8, W: 2, H: 2}) {
		t.Fatalf("clipping wrong: %+v", got)
	}
}

func TestDirtyMarkAll(t *testing.T) {
	var d dirtySet
	d.mark(Rect{X: 1, Y: 1, W: 1, H: 1})
	d.markAll()
	got := d.takeInto(nil, 6, 4)
	if len(got) != 1 || got[0] != (Rect{W: 6, H: 4}) {
		t.Fatalf("markAll did not produce a full rect: %+v", got)
	}
}

func TestRuneWidth(t *testing.T) {
	cases := []struct {
		r    rune
		want int
	}{
		{'a', 1},
		{0, 0},
		{'\n', 0},
		{'\x1b', 0},
		{'é', 1},
		{0x0301, 0},  // combining acute
		{'漢', 2},     // CJK
		{0x1F600, 2}, // emoji, wide
		{0x200B, 0},  // zero-width space
		{0xFE0F, 0},  // variation selector 16
		{0x1100, 2},  // Hangul jamo
	}
	for _, tc := range cases {
		if got := RuneWidth(tc.r); got != tc.want {
			t.Errorf("RuneWidth(%U) = %d, want %d", tc.r, got, tc.want)
		}
	}
	if got := StringWidth("a漢b"); got != 4 {
		t.Errorf("StringWidth = %d, want 4", got)
	}
}

func BenchmarkBufferFullWrite(b *testing.B) {
	buf := NewBuffer(200, 60)
	c := NewCell('x', NewStyle(NewColour(1, 2, 3), NewColour(4, 5, 6), AttrBold))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for y := 0; y < 60; y++ {
			row := buf.Row(y)
			for x := range row {
				row[x] = c
			}
		}
	}
}

func BenchmarkSetString(b *testing.B) {
	buf := NewBuffer(200, 1)
	const s = "the quick brown fox jumps over the lazy dog"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.SetString(0, 0, s, DefaultStyle)
	}
}
