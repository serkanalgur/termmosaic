package menu

import (
	"fmt"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

func TestDbg(t *testing.T) {
	bg := buffer.NewStyle(buffer.NewColour(0x11, 0x22, 0x33), buffer.NewColour(0x44, 0x55, 0x66), 0)
	m := newMenu(t, 3, 3, tree()...)
	m.Block().SetBackground(bg)
	fmt.Println("bg", bg, "resolved", bg.Resolved().Blank(), "block bg", m.Block().Background)
	buf := drawInto(m, 3, 3)
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			c := buf.CellAt(x, y)
			fmt.Printf("(%d,%d) %q bg=%v\n", x, y, c.Ch, c.BG)
		}
	}
	fmt.Println("interior", m.blk.Interior(), "bounds", m.bounds)
}
