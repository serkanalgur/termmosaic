package data

import (
	"fmt"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
)

// BenchmarkListRender measures a frame of a List at three collection sizes.
//
// The claim ADR 0007 §6 makes is that a virtualized widget is O(visible rows) and
// not O(item count), so the three numbers are the evidence: if the engine or the row
// painter iterated the items, the 100K case would be three orders of magnitude
// slower than the 10 case and this benchmark would show it.
func BenchmarkListRender(b *testing.B) {
	for _, n := range []int{10, 10000, 100000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			l := NewList(buffer.Rect{X: 0, Y: 0, W: 80, H: 24})
			items := make([]ListItem, n)
			for i := range items {
				items[i] = ListItem{Label: fmt.Sprintf("item %d", i)}
			}
			l.SetItems(items)
			l.Scrollbar = true
			buf := buffer.NewBuffer(80, 24)
			l.Draw(buf)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				l.vm.ScrollBy(1)
				l.Draw(buf)
			}
		})
	}
}

// BenchmarkTableRender measures a frame of a Table at three collection sizes, for
// the same reason as BenchmarkListRender and with the same expectation.
func BenchmarkTableRender(b *testing.B) {
	for _, n := range []int{10, 10000, 100000} {
		b.Run(fmt.Sprintf("rows=%d", n), func(b *testing.B) {
			tb := NewTable(buffer.Rect{X: 0, Y: 0, W: 120, H: 24},
				Column{Title: []buffer.Span{buffer.NewSpan("ID", buffer.DefaultStyle)}, Width: 8},
				Column{Title: []buffer.Span{buffer.NewSpan("NAME", buffer.DefaultStyle)}, Width: 24, Grow: 1},
				Column{Title: []buffer.Span{buffer.NewSpan("STATE", buffer.DefaultStyle)}, Width: 12, Align: geometry.AlignRight},
			)
			tb.Header = true
			rows := make([]Row, n)
			for i := range rows {
				rows[i] = Row{Cells: []Cell{
					{Text: fmt.Sprintf("%d", i)},
					{Text: fmt.Sprintf("name %d", i)},
					{Text: "running"},
				}}
			}
			tb.SetRows(rows)
			buf := buffer.NewBuffer(120, 24)
			tb.Draw(buf)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tb.vm.ScrollBy(1)
				tb.Draw(buf)
			}
		})
	}
}

// BenchmarkTreeRender measures a frame of a Tree over a wide hierarchy, plus the
// cost of an expansion change, which is the one operation that is O(visible rows)
// rather than O(1).
func BenchmarkTreeRender(b *testing.B) {
	kids := make([]Node, 5000)
	for i := range kids {
		kids[i] = Node{Label: fmt.Sprintf("node %d", i)}
	}
	tr := NewTree(buffer.Rect{X: 0, Y: 0, W: 80, H: 24}, Node{Label: "root", Expanded: true, Children: kids})
	buf := buffer.NewBuffer(80, 24)
	tr.Draw(buf)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.vm.ScrollBy(1)
		tr.Draw(buf)
	}
}

// BenchmarkTreeToggle measures one expansion change over a 5000-node hierarchy: it
// is O(visible rows) by construction, and this is the number behind that claim.
func BenchmarkTreeToggle(b *testing.B) {
	kids := make([]Node, 5000)
	for i := range kids {
		kids[i] = Node{Label: fmt.Sprintf("node %d", i), Children: []Node{{Label: "leaf"}}}
	}
	tr := NewTree(buffer.Rect{X: 0, Y: 0, W: 80, H: 24}, Node{Label: "root", Expanded: true, Children: kids})
	tr.ExpandAll()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			tr.Collapse(1)
		} else {
			tr.Expand(1)
		}
	}
}

// BenchmarkPagerRender measures a frame of a Pager over a large document, which is
// the case that proves its cost is per screen rather than per document.
func BenchmarkPagerRender(b *testing.B) {
	doc := strings.Repeat(strings.Repeat("the quick brown fox jumps over the lazy dog. ", 20)+"\n", 5000)
	p := NewPager(buffer.Rect{X: 0, Y: 0, W: 80, H: 24})
	p.SetText(doc)
	buf := buffer.NewBuffer(80, 24)
	p.Draw(buf)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.ScrollBy(1)
		p.Draw(buf)
	}
}
