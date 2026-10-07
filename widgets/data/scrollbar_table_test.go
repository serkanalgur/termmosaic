package data

import (
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// plainScrollTable returns the fixture of the scrollbar regression: a bordered
// table with a header, three fixed columns and n rows, sized w by h. It is
// plainTable's geometry with the row count the test chooses, because the thumb's
// height and position are the visible fraction of the collection.
func plainScrollTable(t *testing.T, w, h, n int) *Table {
	t.Helper()
	tb := NewTable(buffer.Rect{X: 0, Y: 0, W: w, H: h},
		named("ID", 4), named("NAME", 6), named("STATE", 6))
	tb.Header = true
	tb.SetRows(tableRows(n))
	tb.Block().SetBorder(buffer.BorderPlain)
	return tb
}

// TestTableScrollbarThumbStaysBelowTheHeaderAndNeverPaintsIntoIt is the
// regression test for the scrollbar-painter refactor.
//
// List and Tree paint their thumb over the block INTERIOR; Table paints it over
// a body-only strip whose origin is body.Y, so the thumb stays below the
// header. A painter that unified the two by handing Table the interior would
// slide the thumb under the header — silently, because every List and Tree
// test would still pass. The exact thumb rows catch that slide at any offset;
// the explicit sweep of the header row names the invariant itself.
//
// Geometry of the fixture: a 22x8 bordered table has a 20x6 interior, the
// header takes screen row 1, and the body is rows 2..6. Seven rows in five
// visible ones leave MaxOffset = 2, so a thumb is drawn at all, and its height
// is 5*5/7 = 3 cells — tall enough that the placement shows, small enough that
// the slack moves it.
func TestTableScrollbarThumbStaysBelowTheHeaderAndNeverPaintsIntoIt(t *testing.T) {
	tests := []struct {
		name string
		// offset is the vertical scroll the case draws at.
		offset int
		// wantThumbY are the screen rows the thumb must occupy, and no others.
		wantThumbY []int
		// want are the contents of screen rows 1..6: the header, then the
		// five body rows.
		want []string
	}{
		{
			name:       "at the top of the scroll",
			offset:     0,
			wantThumbY: []int{2, 3, 4},
			want: []string{
				"  ID   NAME   STATE",
				"› r0   name0  stat…█",
				"  r1   name1  stat…█",
				"  r2   name2  stat…█",
				"  r3   name3  stat…",
				"  r4   name4  stat…",
			},
		},
		{
			name:       "mid-scroll",
			offset:     1,
			wantThumbY: []int{3, 4, 5},
			want: []string{
				"  ID   NAME   STATE",
				"  r1   name1  stat…",
				"  r2   name2  stat…█",
				"  r3   name3  stat…█",
				"  r4   name4  stat…█",
				"  r5   name5  stat…",
			},
		},
		{
			name:       "at the bottom of the scroll",
			offset:     2,
			wantThumbY: []int{4, 5, 6},
			want: []string{
				"  ID   NAME   STATE",
				"  r2   name2  stat…",
				"  r3   name3  stat…",
				"  r4   name4  stat…█",
				"  r5   name5  stat…█",
				"  r6   name6  stat…█",
			},
		},
	}
	for _, tt := range tests {
		tb := plainScrollTable(t, 22, 8, 7)
		tb.SetRowOffset(tt.offset)
		buf := cellBuf(22, 8)
		tb.Draw(buf)
		// The case must actually be drawn at its own offset: SetRowOffset
		// clamps, and a case silently clamped to zero would re-test the top
		// of the scroll under three different names.
		if got := tb.RowOffset(); got != tt.offset {
			t.Fatalf("%s: RowOffset = %d after Draw, want %d", tt.name, got, tt.offset)
		}
		if max := tb.vm.MaxOffset(); max <= 0 {
			t.Fatalf("%s: MaxOffset = %d after Draw, want > 0: the fixture must overflow its viewport", tt.name, max)
		}
		// The thumb is exactly these cells, on the scrollbar column — the
		// interior's rightmost, x=20 — and nowhere else: an interior-sized
		// track would show up here as a longer thumb starting a row higher.
		wantThumb(t, thumbCells(buf, thumbRune), 20, tt.wantThumbY)
		// The header band is the first interior row, and no thumb cell may
		// land in it at any offset. This is the invariant a unified track
		// would break first.
		for x := 1; x <= 20; x++ {
			if buf.CellAt(x, 1).Ch == thumbRune {
				t.Errorf("%s: thumb cell at (%d,1) sits in the header band", tt.name, x)
			}
		}
		got := rows(t, 22, 8, tb)
		// The border row itself is Block's, and widgets/block tests it; what
		// matters here is that the frame is where it belongs.
		wantFramed(t, got, 0)
		wantFramed(t, got, 7)
		for i, expect := range tt.want {
			want(t, got, i+1, expect)
		}
	}
}

// TestTableScrollbarThumbWithoutHeaderStartsAtTheFirstBodyRow is the control
// for the regression above. With Header=false there is no header band: the
// body starts at the interior's top row, and the same painter covers the whole
// interior. The contrast — the thumb reaching screen row 1 here, and never
// reaching row 1 when a header is present — is what proves the track is the
// caller's rectangle rather than something the painter derives.
func TestTableScrollbarThumbWithoutHeaderStartsAtTheFirstBodyRow(t *testing.T) {
	tests := []struct {
		name       string
		offset     int
		wantThumbY []int
		want       []string // screen rows 1..6: the six body rows
	}{
		// Six body rows in six visible ones: MaxOffset = 1, and the thumb is
		// 6*6/7 = 5 cells tall. At the top of the scroll it starts on the
		// interior's first row; at the bottom it ends on the last.
		{
			name:       "at the top of the scroll",
			offset:     0,
			wantThumbY: []int{1, 2, 3, 4, 5},
			want: []string{
				"› r0   name0  stat…█",
				"  r1   name1  stat…█",
				"  r2   name2  stat…█",
				"  r3   name3  stat…█",
				"  r4   name4  stat…█",
				"  r5   name5  stat…",
			},
		},
		{
			name:       "at the bottom of the scroll",
			offset:     1,
			wantThumbY: []int{2, 3, 4, 5, 6},
			want: []string{
				"  r1   name1  stat…",
				"  r2   name2  stat…█",
				"  r3   name3  stat…█",
				"  r4   name4  stat…█",
				"  r5   name5  stat…█",
				"  r6   name6  stat…█",
			},
		},
	}
	for _, tt := range tests {
		// The same fixture with the header switched off: the control.
		tb := plainScrollTable(t, 22, 8, 7)
		tb.Header = false
		tb.SetRowOffset(tt.offset)
		buf := cellBuf(22, 8)
		tb.Draw(buf)
		if got := tb.RowOffset(); got != tt.offset {
			t.Fatalf("%s: RowOffset = %d after Draw, want %d", tt.name, got, tt.offset)
		}
		wantThumb(t, thumbCells(buf, thumbRune), 20, tt.wantThumbY)
		got := rows(t, 22, 8, tb)
		wantFramed(t, got, 0)
		wantFramed(t, got, 7)
		for i, expect := range tt.want {
			want(t, got, i+1, expect)
		}
	}
}
