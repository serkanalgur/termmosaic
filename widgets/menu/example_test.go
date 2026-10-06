package menu_test

import (
	"fmt"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/widgets/menu"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// show renders root on a w-by-h screen and returns the rows with trailing
// spaces trimmed.
//
// Every Output comment below is the CELL GRID the renderer produced, read back
// out of a headless.MemorySink rather than described. widgettest walks the whole
// widget -> buffer -> renderer -> diff -> encoder -> MemorySink path, so what a
// reader sees is what the menu's own tests assert on.
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

// press offers an event to a focused widget, which is how an example drives a
// widget with the same calls an event loop would make.
func press(w termmosaic.Widget, k termmosaic.Key, mod termmosaic.KeyMod) bool {
	return w.Handle(termmosaic.SpecialKeyEvent(k, mod))
}

// Example is a menu with a nested submenu, which is the thing that makes a Menu
// more than a list.
//
// A new Menu is CLOSED. That is deliberate and it is the first thing to know: a
// menu drawn open the moment a terminal appears has no key that reliably closes
// it, and the focus it took on startup is not focus the user asked for. Open is
// what a program calls when it wants the menu, usually from a binding.
func Example() {
	m := menu.New(buffer.Rect{W: 40, H: 9},
		menu.Item{Label: "New", Hint: "^N"},
		menu.Item{Label: "Open", Hint: "^O", Items: []menu.Item{
			{Label: "File…"},
			{Label: "Directory…"},
			{Label: "Recent", Items: []menu.Item{
				{Label: "notes.md"},
				{Label: "main.go"},
			}},
		}},
		menu.Item{Label: "Quit", Hint: "^Q"},
	)
	m.Open()
	m.SetFocused(true)

	// Down once onto the branch, then Right to descend into it. The cursor PATH is
	// what makes a nested menu a cursor rather than a set of independent
	// selections, and Depth is how deep that path currently reaches.
	//
	// Right on a LEAF is not consumed, so an application may still bind the key —
	// which is the whole reason it declines rather than returning true.
	press(m, termmosaic.KeyDown, 0)
	press(m, termmosaic.KeyRight, 0)

	item, _ := m.SelectedItem()
	fmt.Printf("depth %d, active level %d, %q\n", m.Depth(), m.ActiveLevel(), item.Label)
	fmt.Printf("the open submenu holds %d items\n", len(m.Level(1)))
	fmt.Println(show(40, 9, m))
	// Output:
	// depth 2, active level 1, "File…"
	// the open submenu holds 3 items
	//  1                   [2 Open           ]
	//  New           ^N    ›File…
	// ›Open          ^O  ▸  Directory…
	//  Quit          ^Q     Recent           ▸
}

// ExampleMenu_checkable is the toggle half. Checkable gives an item a check
// gutter and makes Enter flip Checked; SetChecked is the programmatic equivalent
// of pressing Enter on it, and it reports whether it did.
//
// A caller restoring persisted state uses SetChecked rather than simulating keys,
// because it does not want a callback per restored item.
func ExampleMenu_checkable() {
	m := menu.New(buffer.Rect{W: 30, H: 6},
		menu.Item{Label: "Word wrap", Checkable: true, Checked: true},
		menu.Item{Label: "Line numbers", Checkable: true},
		menu.Item{Label: "Invisibles", Checkable: true, Checked: true},
	)
	m.Open()
	m.SetFocused(true)

	fmt.Printf("wrap checked: %v\n", m.Checked(0, 0))
	press(m, termmosaic.KeyDown, 0)
	m.SetChecked(0, 1, true)
	fmt.Printf("after SetChecked: %v\n", m.Checked(0, 1))
	fmt.Println(show(30, 6, m))
	// Output:
	// wrap checked: true
	// after SetChecked: true
	// [1                           ]
	//  ✓Word wrap
	// ›✓Line numbers
	//  ✓Invisibles
}

// ExampleMenu_disabled is a contract rather than a preference: a disabled item is
// skipped by every navigation key and is never selected, but it is STILL DRAWN.
// A menu that hides what it cannot do cannot be navigated by reading it.
func ExampleMenu_disabled() {
	m := menu.New(buffer.Rect{W: 26, H: 5},
		menu.Item{Label: "Cut"},
		menu.Item{Label: "Copy"},
		menu.Item{Label: "Paste", Disabled: true},
		menu.Item{Label: "Delete"},
	)
	m.Open()
	m.SetFocused(true)

	// Two downs land on Delete: Paste was skipped rather than chosen.
	press(m, termmosaic.KeyDown, 0)
	press(m, termmosaic.KeyDown, 0)
	item, _ := m.SelectedItem()
	fmt.Printf("selected %q, and it is not disabled: %v\n", item.Label, !item.Disabled)
	fmt.Println(show(26, 5, m))
	// Output:
	// selected "Delete", and it is not disabled: true
	// [1                       ]
	//  Cut
	//  Copy
	//  Paste
	// ›Delete
}

// ExampleMenu_closed shows the state a menu is in most of the time, and it is not
// a blank screen: the Block still paints, so a closed menu in a frame does not
// leave a hole where something was. Its rows stop being drawn, nothing else does.
func ExampleMenu_closed() {
	m := menu.New(buffer.Rect{W: 24, H: 4},
		menu.Item{Label: "One"},
		menu.Item{Label: "Two"},
	)
	// Every catalog widget that draws itself inside a border exposes the Block it
	// draws, so chrome is configured on the Block rather than duplicated per
	// widget.
	blk := m.Block()
	blk.SetBorder(buffer.BorderRounded)
	blk.SetTitleString("tools", buffer.Style{})
	blk.SetTitleAlign(geometry.AlignLeft)

	fmt.Printf("open: %v, depth kept: %d\n", m.IsOpen(), m.Depth())
	m.Open()
	press(m, termmosaic.KeyDown, 0)
	m.Close()
	fmt.Printf("after close: open=%v, depth=%d, selected=%d\n",
		m.IsOpen(), m.Depth(), m.Selected())
	fmt.Println(show(24, 4, m))
	// Output:
	// open: false, depth kept: 1
	// after close: open=false, depth=1, selected=0
	// ╭ tools ───────────────╮
	// │                      │
	// │                      │
	// ╰──────────────────────╯
}
