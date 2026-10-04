// Package menu provides Menu, a keyboard-driven menu with nested submenus to
// arbitrary depth.
//
// # What a menu is, and why it is not a List
//
// A List is a flat collection of rows with one selection. A menu is a TREE of
// items with a CURSOR PATH: which item is selected at each level the user has
// descended into, and which level the next key acts on. Everything that makes a
// menu hard follows from that path being state rather than a single index:
//
//   - Left must return to the PARENT level and restore the selection that level
//     had, not to index-1 of the current level. So the path is a stack of
//     per-level selections, and Left pops one.
//   - Right must push a level, and pushing one must not lose the parent's
//     selection. So the stack grows rather than replaces.
//   - Escape must close the current level and THEN the menu, which is the same
//     pop operation applied to the root — one rule, two call sites.
//   - Re-entering a submenu the user already visited restores what was selected
//     there, because a stack that keeps its frames remembers it for free.
//
// # Item model
//
// An Item is plain data: a label, an optional submenu, an optional keybinding
// hint, an enabled flag, and an optional checkable state. There is no interface,
// no closure and no callback on the item, so a whole tree can be written as a Go
// literal and compared with reflect.DeepEqual in a test. ADR 0008 records that
// there is no theme type in v1, so the widget carries plain buffer.Style fields
// and the application picks the colours.
//
// # Non-colour signals
//
// Every piece of state this widget draws is signalled by a SHAPE as well as by a
// style, because ADR 0008's accessibility rule is that colour is never the only
// signal:
//
//	selected item   the marker glyph in the gutter ('›' by default) plus the whole
//	                row filled in SelectedStyle
//	submenu present '▸' in the right gutter ('>' on ASCII)
//	checked         '✓' in the check gutter, unchecked items get a space, and a
//	                column in which NO item is checkable gets no check column at
//	                all — so the column's existence states that these are not
//	                toggles
//	disabled        DisabledStyle, whose default carries AttrFaint rather than
//	                only a dimmer colour, and no selection marker, because a
//	                disabled item is never selected
//	which level     a header row per open level, showing the level NUMBER and
//	                the label of the item that opened it, BRACKETED for the
//	                active level and space-padded for the others — the same
//	                bracket-means-selected convention Tabs uses
//
// # Layout
//
// Each open level is one COLUMN, laid out left to right, so a two-level menu
// reads as two adjacent columns rather than as a rewritten parent. Columns share
// the interior width: each gets interior.W/count, and the remainder goes to the
// leftmost columns so no cell is left unpainted.
//
// When the width cannot give every column minColumnW, the DEEPEST columns are
// kept — the active one must always be visible — and the leftmost kept column
// says so with a leading '…' in its header. Nothing is silently missing, which
// is the same rule List follows when its gutter wins its budget against its
// scrollbar.
//
// # Key contract
//
// Consumed only while focused and while the menu is open. A closed menu consumes
// nothing, so an application can bind the same keys to "open the menu".
//
//	up / down       move the selection at the ACTIVE level, WRAPPING, skipping
//	                disabled items
//	right           open the selected item's submenu; a no-op on a leaf, and NOT
//	                consumed there, so an application may still bind right
//	left            close one level, restoring the parent's selection; at the root
//	                it closes the whole menu
//	enter, space    activate: see Activate
//	escape          close the active level, then the menu
//	home / end      first / last selectable item at the active level
//	page up/down    move by a screen, less one row of overlap
//	wheel up/down   scroll the active level WITHOUT moving the selection
//	press           select the pressed row and take focus; pressing the row that
//	                was ALREADY selected opens its submenu, which is how a menu
//	                separates "let me look at this" from "let me go in here"
//	                without a second button
//
// KeyTab is NOT consumed, exactly as in List, Table and Tree, so a form moves
// focus out of a menu with the keyboard.
//
// # Activate
//
// Enter on a CHECKABLE item flips it and calls OnToggle. Enter on an item with a
// submenu opens that submenu. Enter on a leaf calls OnActivate.
//
// The order is the contract, and it is checked in this order because a toggle's
// meaning is its state: firing OnActivate for a toggle as well would make the
// application handle one action twice, and opening a submenu for a toggle would
// contradict the check gutter in the same row.
//
// # Cost
//
// Draw is O(visible rows) and allocates nothing. Everything derived from the
// size — which columns exist, how the interior's cells are divided between the
// marker, check, label, hint and submenu gutters, and every label and hint
// truncated to the width it is drawn at — is computed once per distinct
// interior and cached, per ADR 0007 §3. Truncation and the header strings
// allocate, which is why they are in the rebuild and not in Draw.
package menu

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/virtual"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Item is one entry in a menu level.
//
// It is a plain struct so a whole tree can be written as a Go literal and
// compared with reflect.DeepEqual in a test, which is why there is no interface
// and no per-item closure: two menus that differ only in their items can be
// compared without either widget knowing about the other.
type Item struct {
	// Label is the item's text.
	Label string
	// Hint is an optional keybinding shown right-aligned, e.g. "^S". It is
	// dropped before the label is truncated, because a half-written key name is
	// worse than no key name at all.
	Hint string
	// Items is the item's submenu. A non-empty Items makes the item a branch and
	// gives it the submenu marker.
	Items []Item
	// Disabled items are skipped by every navigation key and are never selected.
	// They are still drawn: a menu that hides what it cannot do cannot be
	// navigated by reading it.
	Disabled bool
	// Checkable makes the item a toggle, which gives it a check gutter and makes
	// Enter flip Checked.
	Checkable bool
	// Checked is the toggle's state, meaningful only when Checkable is set.
	Checked bool
}

// level is one frame of the cursor path: the selection at one depth, and the
// scroll engine that keeps it visible.
//
// A virtual.Model per level rather than one per menu is what makes Left restore
// what the PARENT level had scrolled to and not merely what it had selected,
// which is the difference between a menu that feels like a tree and one that
// feels like a list that got confused.
type level struct {
	sel int
	vm  *virtual.Model
}

// Menu is a tree of items with a cursor path, nested submenus and a keyboard
// contract.
//
// It is Focusable: keys are consumed only while focused, and a press inside the
// widget both selects and takes focus, so a click is a complete interaction
// without the application writing a click handler.
//
// # Construction
//
// A new Menu is CLOSED. A menu drawn open the moment a terminal appears is a
// menu the user cannot dismiss with the key they expect, and Open is one call
// away. The zero Menu is not usable: SetItems and SetBounds are required first.
type Menu struct {
	blk    *block.Block
	bounds buffer.Rect

	// items is the ROOT level's items, copied by SetItems so a later mutation of
	// the caller's slice cannot change what a drawn frame means. Deeper levels
	// are reached through Item.Items rather than copied, so a toggle's Checked
	// survives being navigated away from and back.
	items []Item

	// levels is the cursor path. levels[0] is the root level and exists whenever
	// the menu has been constructed; len(levels) is the DEPTH.
	levels []*level

	// open reports whether the menu is showing its items at all.
	open bool
	// focused reports whether the menu has keyboard focus.
	focused bool

	// Marker is the selection gutter glyph. It is never dropped while the column
	// has room for it, because it is the signal that survives a monochrome
	// terminal and a reader who cannot tell two of the application's own colours
	// apart.
	Marker string
	// SubmenuMarker is drawn in the right gutter of an item that has a submenu.
	SubmenuMarker string
	// CheckMark is drawn for a checked item.
	CheckMark string
	// CheckMarkEmpty is drawn for an unchecked one. A SPACE by default, not a
	// distinct glyph: an unchecked box is empty, and the check COLUMN's presence
	// is what tells a toggle from a plain item.
	CheckMarkEmpty string

	// Ascii selects the ASCII marker rung, and the ASCII truncation marker. Pass
	// !caps.Unicode, the same one boolean block.Block takes: a menu whose markers
	// are Unicode while its border is ASCII would be a menu whose arithmetic
	// differs between terminals.
	//
	// It is a field rather than a read of caps.Unicode because ADR 0008 Decision 4
	// forbids a widget branching on capabilities directly. Assigning it drops the
	// cached layout, because the markers are resolved in the rebuild.
	Ascii bool

	// The resolved marker runes for the current rung, cached so Draw reads four
	// integers instead of comparing strings per visible row. truncMark is the
	// truncation marker for the same rung.
	markRune, submenuRune, checkRune, checkEmptyRune, truncMark rune

	// ItemStyle is the rendition of an ordinary row's background and text.
	ItemStyle buffer.Style
	// SelectedStyle is the rendition of the selected row's background and text.
	// An unset value means ItemStyle with AttrReverse, so a selection is legible
	// with no configuration at all.
	SelectedStyle buffer.Style
	// DisabledStyle is the rendition of a disabled row's text.
	DisabledStyle buffer.Style
	// HintStyle is the rendition of a keybinding hint.
	HintStyle buffer.Style
	// CheckStyle is the rendition of the check gutter and the submenu marker.
	CheckStyle buffer.Style
	// HeaderStyle is the rendition of a level header's text.
	HeaderStyle buffer.Style
	// HeaderActiveStyle is the rendition of the ACTIVE level's header. It should
	// carry an attribute as well as a colour, because the brackets beside it are
	// the colour-independent signal and this is only reinforcement.
	HeaderActiveStyle buffer.Style
	// MarkerStyle is the rendition of the selection marker. An unset value
	// follows SelectedStyle, because a marker in ItemStyle on a reversed row is
	// the one unreadable thing on screen.
	MarkerStyle buffer.Style

	// OnActivate is called with the depth and item index when Enter fires a leaf.
	// It is not called by SetItems, Open, or a navigation key.
	OnActivate func(depth, index int)
	// OnToggle is called with the depth and item index when Enter flips a
	// checkable item, AFTER the state has changed.
	OnToggle func(depth, index int)
	// OnOpen is called with the depth and item index of an item whose submenu a
	// key or a click opened.
	OnOpen func(depth, index int)
	// OnSelect is called with the depth and item index whenever the selection at
	// that depth changes, including by a click.
	OnSelect func(depth, index int)

	// lay is the cached per-size layout, keyed on the block's interior.
	lay layout
}

// Marker glyphs. They are exported for the same reason Tabs' markers are: the
// non-colour state indicator is part of the widget's contract, and a test that
// asserts on it has to be able to name it.
//
// The Unicode forms are U+203A, U+25B8 and U+2713. U+25B8 is in Geometric
// Shapes, which vocabulary_test.go explicitly excludes from the border scan, so
// these are legal in a widget package with no exception: buffer/border.go still
// owns every BOX-DRAWING rune in the repository. Every one of them is one cell
// wide, so a menu's arithmetic is identical on the ASCII rung.
const (
	// DefaultMarker is the selection gutter glyph.
	DefaultMarker = "›"
	// DefaultSubmenuMarker says an item opens a submenu.
	DefaultSubmenuMarker = "▸"
	// DefaultCheckMark says a checkable item is checked.
	DefaultCheckMark = "✓"
	// DefaultCheckMarkEmpty says a checkable item is unchecked. A space, not a
	// distinct glyph: an unchecked box is empty.
	DefaultCheckMarkEmpty = " "
)

// The ASCII rung's markers. They are unexported because a caller degrades by
// setting Menu.Ascii, not by choosing a second set of constants: one boolean
// switches the whole widget and there is no per-glyph fallback, which is the same
// rule buffer.BorderStyle.Glyphs follows.
//
// Every one is one cell wide, so a menu's ARITHMETIC is identical on both rungs —
// which is the property TestMenuAsciiRungGivesOneCellMarkers asserts.
const (
	// asciiMarker is DefaultMarker without Unicode.
	asciiMarker = ">"
	// asciiSubmenuMarker is DefaultSubmenuMarker without Unicode. '>' as well,
	// because the gutter column is what distinguishes the two.
	asciiSubmenuMarker = ">"
	// asciiCheckMark is DefaultCheckMark without Unicode.
	asciiCheckMark = "x"
)

// New returns a closed Menu sized r over items.
//
// Closed, because a menu that appears already open has no key that reliably
// dismisses it from the application's point of view. Open is one call away and
// is documented on it.
//
// The four marker fields get the catalog defaults, so a menu with no
// configuration still signals every state with a shape.
func New(r buffer.Rect, items ...Item) *Menu {
	m := &Menu{
		blk:            block.New(r),
		bounds:         r,
		Marker:         DefaultMarker,
		SubmenuMarker:  DefaultSubmenuMarker,
		CheckMark:      DefaultCheckMark,
		CheckMarkEmpty: DefaultCheckMarkEmpty,
	}
	m.SetItems(items)
	return m
}

// Bounds returns the menu's rectangle, safe to call before the first Draw.
func (m *Menu) Bounds() buffer.Rect { return m.bounds }

// SetBounds sets the menu's rectangle. The layout cache is keyed on the block's
// interior, so a new rectangle makes the next Draw rebuild it.
func (m *Menu) SetBounds(r buffer.Rect) {
	m.bounds = r
	m.blk.SetBounds(r)
}

// Block returns the block that draws this menu's border, title, padding and
// background, so a caller configures the chrome without Menu re-exporting every
// Block method. It is the composition point of ADR 0008 §2: this package spells
// no border rune and invents no title threshold.
//
// The Background a caller sets there is the one this menu's rows are painted
// over, which is why Menu has no Background field of its own.
func (m *Menu) Block() *block.Block { return m.blk }

// Focused reports whether the menu has focus.
func (m *Menu) Focused() bool { return m.focused }

// SetFocused gives or removes focus.
//
// The menu keeps its open state, its cursor path and its scroll offsets either
// way: losing focus is not a reason to close a menu, nor to forget where in the
// tree the user was.
func (m *Menu) SetFocused(v bool) { m.focused = v }

// MinSize returns the smallest menu that shows a header, a marker, a label and
// two columns' worth of structure: a whole-widget size including this menu's own
// chrome, per ADR 0007 §2.
func (m *Menu) MinSize() buffer.Size { return minWhole(m.blk, minMenuW, minMenuH) }

// Items returns the ROOT level's items.
//
// The result aliases the menu's own storage, which is deliberate and is what
// makes SetChecked and Enter's toggle observable through it: a caller reading
// Items sees the menu's live state rather than a copy of it.
func (m *Menu) Items() []Item { return m.items }

// SetItems replaces the root level's items and resets the cursor path to the
// root level alone, because an item index from the old tree means nothing in the
// new one.
//
// Only the root slice is copied. Checked states inside the new tree are the
// caller's, and surviving SetItems is what makes it usable as "reload this menu".
func (m *Menu) SetItems(items []Item) {
	m.items = append([]Item(nil), items...)
	if len(m.levels) == 0 {
		m.levels = append(m.levels, &level{sel: -1, vm: virtual.New(0)})
	}
	// Only the root survives: a deeper frame names an item of the old tree.
	m.levels = m.levels[:1]
	m.levels[0].vm.SetCount(len(m.items))
	m.levels[0].sel = m.firstSelectable(0)
	m.lay.invalidate()
}

// Open shows the menu's items.
//
// It is idempotent, and it does NOT reset the cursor path: reopening a menu
// returns to the level the user had descended to, which is what a menu bar does
// when it is dismissed and opened again.
func (m *Menu) Open() {
	m.open = true
	if len(m.levels) == 0 {
		m.levels = append(m.levels, &level{sel: -1, vm: virtual.New(0)})
	}
	if m.levels[0].sel < 0 {
		m.levels[0].sel = m.firstSelectable(0)
	}
	m.lay.invalidate()
}

// Close hides the menu's items, keeping the cursor path so Open restores it.
//
// A closed menu consumes no keys. Its rows stop being drawn, but Block still
// paints the whole rectangle in Background, so a menu that closes shrinks to its
// frame rather than leaving its last frame's items on the terminal.
func (m *Menu) Close() {
	m.open = false
	m.lay.invalidate()
}

// IsOpen reports whether the menu is showing its items.
func (m *Menu) IsOpen() bool { return m.open }

// Depth returns how many levels the cursor path holds: 1 for a menu showing only
// its root, 2 when one submenu is open, and so on.
//
// A CLOSED menu keeps its path, so its Depth is still what it was and Open
// restores it. That is the point of Close not popping: the alternative is that
// closing a submenu three levels deep throws away three levels of where-the-user-
// was, which is the state a menu bar has to keep.
//
// It is what an application uses to decide how much room to give the widget.
func (m *Menu) Depth() int {
	if len(m.levels) == 0 {
		return 1
	}
	return len(m.levels)
}

// ActiveLevel returns the depth the next key acts on: the deepest open level, or
// 0 for a menu at its root.
func (m *Menu) ActiveLevel() int { return m.active() }

// Level returns the item slice at depth d — 0 is the root — or nil when d is out
// of range. The result aliases the menu's storage and must not be modified.
func (m *Menu) Level(d int) []Item { return m.levelItems(d) }

// Selected returns the selected index at the ACTIVE level, or -1 when there is
// no selectable item there.
//
// -1 rather than 0, because a level whose items are all disabled has a real
// answer and a caller storing the index must be able to tell "nothing is
// selectable" from "the first item is selected".
func (m *Menu) Selected() int { return m.SelectedAt(m.active()) }

// SelectedAt returns the selected index at depth d, or -1 when d is out of range.
func (m *Menu) SelectedAt(d int) int {
	if d < 0 || d >= len(m.levels) {
		return -1
	}
	return m.levels[d].sel
}

// SelectedItem returns the selected item at the active level, and whether there
// is one.
func (m *Menu) SelectedItem() (Item, bool) {
	i := m.Selected()
	items := m.levelItems(m.active())
	if i < 0 || i >= len(items) {
		return Item{}, false
	}
	return items[i], true
}

// Offset returns the scroll offset of the ACTIVE level, which is how a test
// observes that a wheel notch scrolled without moving the selection.
func (m *Menu) Offset() int {
	if m.active() < 0 || m.active() >= len(m.levels) {
		return 0
	}
	return m.levels[m.active()].vm.Offset()
}

// Invalidate satisfies termmosaic.Widget: it drops the layout cache so the next
// Draw re-derives the per-size work.
//
// There is no dirty region to mark — the menu repaints its whole rectangle every
// frame — but there IS state derived from the rect, and this is the documented
// way to say it is stale. A caller that changes Marker, a marker or a style
// field calls Invalidate; without it the change lands whenever the rect next
// differs, which is the "broken for exactly one frame and repaired by the next"
// shape ADR 0007 §3 describes for a cache keyed on the wrong thing.
func (m *Menu) Invalidate() { m.lay.invalidate() }

// Handle consumes navigation keys while focused and open, and mouse events
// inside its own rectangle, reporting whether it took them.
func (m *Menu) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventKey:
		// A closed menu consumes nothing, so an application can bind the same
		// keys to "open the menu".
		if !m.focused || !m.open {
			return false
		}
		return m.handleKey(ev)
	case termmosaic.EventMouse:
		return m.handleMouse(ev)
	default:
		return false
	}
}

// handleKey is the key contract, one case per documented binding.
func (m *Menu) handleKey(ev termmosaic.Event) bool {
	// Only a key with a binding is consumed, and an unbound one must reach the
	// application untouched: a menu that swallowed every key would make it
	// unusable inside a form.
	if ev.Key == termmosaic.KeyNone && ev.Rune == 0 {
		return false
	}
	if ev.Type == termmosaic.KeyRelease {
		// Without the kitty protocol this never fires, and with it a release must
		// not activate a menu item twice.
		return false
	}
	// The scroll engines need their viewport before a selection can be scrolled
	// into it, and an application may handle a key before the first Draw. The
	// rebuild here is the frame path's own work, done once per distinct rect.
	m.ensureLayout()
	switch ev.Key {
	case termmosaic.KeyUp:
		m.move(-1)
		return true
	case termmosaic.KeyDown:
		m.move(1)
		return true
	case termmosaic.KeyPageUp:
		m.move(-m.pageStep())
		return true
	case termmosaic.KeyPageDown:
		m.move(m.pageStep())
		return true
	case termmosaic.KeyHome:
		m.selectIndex(m.active(), m.firstSelectable(m.active()))
		return true
	case termmosaic.KeyEnd:
		m.selectIndex(m.active(), m.lastSelectable(m.active()))
		return true
	case termmosaic.KeyRight:
		// Right on a leaf is NOT consumed, so an application may still bind it.
		return m.openSubmenu()
	case termmosaic.KeyLeft:
		m.closeLevel()
		return true
	case termmosaic.KeyEnter, termmosaic.KeySpace:
		m.activate()
		return true
	case termmosaic.KeyEscape:
		m.escape()
		return true
	default:
		return false
	}
}

// escape closes the active level, and the whole menu when the active level is
// the root. It is one rule applied at two call sites, not two rules.
func (m *Menu) escape() {
	if m.active() > 0 {
		m.closeLevel()
		return
	}
	m.Close()
}

// openSubmenu opens the selected item's submenu and reports whether it did.
//
// It returns false on a leaf rather than consuming the key, because "right does
// nothing here" is information an application may want to act on.
func (m *Menu) openSubmenu() bool {
	d := m.active()
	i := m.Selected()
	items := m.levelItems(d)
	if i < 0 || i >= len(items) || len(items[i].Items) == 0 {
		return false
	}
	m.pushLevel(d, i)
	return true
}

// pushLevel opens the submenu of item i at depth d, selecting its first
// selectable item.
//
// The new level is APPENDED, so every ancestor frame keeps the selection it had
// and Left can return to it — which is the whole reason the cursor path is a
// stack rather than an index.
func (m *Menu) pushLevel(d, i int) {
	// Anything deeper than d is replaced. The path is a stack, and pushing onto a
	// stale path would leave a frame whose parent is no longer what opened it.
	if len(m.levels) > d+1 {
		m.levels = m.levels[:d+1]
	}
	items := m.levelItems(d)[i].Items
	m.levels = append(m.levels, &level{sel: -1, vm: virtual.New(len(items))})
	m.levels[d+1].sel = m.firstSelectable(d + 1)
	m.levels[d+1].vm.ScrollIntoView(m.levels[d+1].sel)
	if m.OnOpen != nil {
		m.OnOpen(d, i)
	}
	m.lay.invalidate()
}

// closeLevel pops one level, or closes the menu when the root is active.
//
// Left and Escape at the root both land here, so "leave one level" has one
// implementation rather than two that can drift apart.
func (m *Menu) closeLevel() {
	if m.active() > 0 {
		m.levels = m.levels[:m.active()]
		m.lay.invalidate()
		return
	}
	m.Close()
}

// active returns the depth the next key acts on: the deepest open level.
func (m *Menu) active() int {
	if len(m.levels) == 0 {
		return 0
	}
	return len(m.levels) - 1
}

// activate implements Enter, in the order the package documents: a checkable item
// toggles, a branch opens, and only a leaf fires OnActivate.
func (m *Menu) activate() {
	d := m.active()
	i := m.Selected()
	items := m.levelItems(d)
	if i < 0 || i >= len(items) {
		return
	}
	switch {
	case items[i].Checkable:
		items[i].Checked = !items[i].Checked
		if m.OnToggle != nil {
			m.OnToggle(d, i)
		}
	case len(items[i].Items) > 0:
		m.pushLevel(d, i)
	default:
		if m.OnActivate != nil {
			m.OnActivate(d, i)
		}
	}
}

// move moves the active level's selection by delta rows, WRAPPING and skipping
// disabled items.
//
// Wrapping rather than clamping is the documented choice for a menu: a menu's
// items are few and known, and a list that stops dead at the end makes the user
// release the key and press again to find out there is nothing there. A level
// with no selectable item reports -1 and the key is consumed without moving,
// because the key was still a menu key.
func (m *Menu) move(delta int) {
	d := m.active()
	if len(m.levelItems(d)) == 0 {
		return
	}
	if delta == 0 {
		delta = 1
	}
	if i := m.nextSelectable(d, m.Selected(), delta); i >= 0 {
		m.selectIndex(d, i)
	}
}

// selectIndex moves level d's selection to i, clamped into the level.
//
// A NEGATIVE i means "the item that key names does not exist" — which is what
// Home and End pass when a level has nothing selectable — and it leaves the
// selection at -1 rather than clamping up to 0. Clamping here would put the
// selection on a DISABLED item, which is the one thing every other path in this
// package refuses to do.
func (m *Menu) selectIndex(d, i int) {
	if d < 0 || d >= len(m.levels) {
		// A depth the path does not reach is not an error: a caller may select
		// before opening the level it means. Panicking here would make the widget
		// unusable from a form that selects a level on open.
		return
	}
	items := m.levelItems(d)
	if len(items) == 0 {
		m.levels[d].sel = -1
		return
	}
	if i < 0 {
		m.levels[d].sel = -1
		return
	}
	if i >= len(items) {
		i = len(items) - 1
	}
	m.selectAt(d, i)
}

// selectAt selects index i at depth d and reports whether the selection changed,
// so a caller can skip a callback for a press on the row already selected.
func (m *Menu) selectAt(d, i int) bool {
	if d < 0 || d >= len(m.levels) {
		return false
	}
	items := m.levelItems(d)
	if i < 0 || i >= len(items) {
		return false
	}
	changed := i != m.levels[d].sel
	m.levels[d].sel = i
	m.levels[d].vm.ScrollIntoView(i)
	if changed && m.OnSelect != nil {
		m.OnSelect(d, i)
	}
	return changed
}

// pageStep returns how many rows a page key moves: as many as fit, less one row
// of overlap, or one when nothing has been measured. A step of zero would make
// the key a no-op, which is never what a page key means.
func (m *Menu) pageStep() int {
	vis := m.visibleRows(m.active())
	if vis < 1 {
		return 1
	}
	return vis
}

// visibleRows returns how many item rows the level at depth d has room for.
//
// It rebuilds the layout first if it is stale, so a page key arriving before the
// first Draw measures the viewport a drawn frame would use. Answering from a stale
// cache would make PageDown move by one row on the first press and by a screen on
// every press after it.
func (m *Menu) visibleRows(d int) int {
	m.ensureLayout()
	for i := range m.lay.cols {
		if m.lay.cols[i].depth == d {
			return m.lay.cols[i].itemRect.H
		}
	}
	return m.blk.Interior().H
}

// firstSelectable returns the first non-disabled index at depth d, or -1.
func (m *Menu) firstSelectable(d int) int {
	for i, it := range m.levelItems(d) {
		if !it.Disabled {
			return i
		}
	}
	return -1
}

// lastSelectable returns the last non-disabled index at depth d, or -1.
func (m *Menu) lastSelectable(d int) int {
	items := m.levelItems(d)
	for i := len(items) - 1; i >= 0; i-- {
		if !items[i].Disabled {
			return i
		}
	}
	return -1
}

// nextSelectable returns the selectable index delta positions from `from`,
// wrapping, or -1 when the level has none.
//
// A `from` of -1 — nothing selected — starts one step before the first item for a
// positive delta and one step after the last for a negative one, so the first
// Down lands on the first item and the first Up on the last rather than skipping
// one of them.
func (m *Menu) nextSelectable(d, from, delta int) int {
	items := m.levelItems(d)
	n := len(items)
	if n == 0 {
		return -1
	}
	start := from
	if start < 0 {
		start = -delta
	}
	for step := 1; step <= n; step++ {
		i := ((start+delta*step)%n + n) % n
		if !items[i].Disabled {
			return i
		}
	}
	return -1
}

// levelItems returns the items at depth d, following Item.Items down the cursor
// path.
//
// It returns nil rather than panicking for a depth the path does not reach,
// which is a state an application reaches by opening a submenu on an item that
// has none. A menu that panicked there would be a menu that crashed on a key.
func (m *Menu) levelItems(d int) []Item {
	if d < 0 || d > len(m.levels) {
		return nil
	}
	items := m.items
	for l := 0; l < d; l++ {
		if items == nil {
			return nil
		}
		i := m.levels[l].sel
		if i < 0 || i >= len(items) {
			return nil
		}
		items = items[i].Items
	}
	return items
}

// SetChecked sets the checked state of the item at depth d, index i, and reports
// whether the item existed and was checkable.
//
// It is the programmatic equivalent of pressing Enter on a toggle, without
// OnToggle: a caller restoring persisted state does not want a callback per item.
func (m *Menu) SetChecked(d, i int, checked bool) bool {
	items := m.levelItems(d)
	if i < 0 || i >= len(items) || !items[i].Checkable {
		return false
	}
	items[i].Checked = checked
	return true
}

// Checked reports whether the item at depth d, index i is checkable and checked.
func (m *Menu) Checked(d, i int) bool {
	items := m.levelItems(d)
	if i < 0 || i >= len(items) {
		return false
	}
	return items[i].Checkable && items[i].Checked
}

// ---------------------------------------------------------------------------
// drawing
// ---------------------------------------------------------------------------

// Draw paints the block's chrome and then every open level's column.
//
// It is total: defined for an empty rectangle, for a closed menu, and for a
// rectangle below MinSize. It allocates nothing, and it reads only the cached
// layout.
//
// The block paints the whole rectangle in Background before anything else, which
// is how ADR 0007 §1 rule 3 is satisfied for this widget without a FillRect of
// its own: a menu that showed two columns at 60 cells and one at 20 has its old
// second column repainted by the block before the single column is drawn, so no
// stale cell survives a SHRINK. That direction is the only one where anything
// can survive, and it is the one the responsive tests sweep.
func (m *Menu) Draw(buf *buffer.Buffer) {
	if m.bounds.Empty() {
		// ADR 0007 §4: Draw returns immediately on an empty Bounds.
		return
	}
	m.blk.Draw(buf)
	if !m.open {
		return
	}
	in := m.blk.Interior()
	if in.Empty() {
		return
	}
	if m.lay.stale(in) {
		m.rebuild(in)
	}

	// Every level's engine is resized every frame, whatever the cached layout says.
	// Resize is O(1), allocates nothing, and it is what clamps an offset when the
	// terminal shrinks (ADR 0007 §6 rule 2) — a widget that resized its engines only
	// when the layout was rebuilt would leave an offset pointing past a short list.
	m.resizeViews()

	for i := range m.lay.cols {
		m.drawColumn(buf, &m.lay.cols[i])
	}
}

// drawColumn paints one level: its header row, then its visible items.
//
// The item rows are walked directly rather than through virtual.Model.ForEach
// because a menu's row is a set of positioned gutters rather than one span run,
// so the closure signature does not fit and a plain loop over the engine's Range
// reads better.
func (m *Menu) drawColumn(buf *buffer.Buffer, c *layoutCol) {
	if !c.headRect.Empty() {
		m.drawHeader(buf, c)
	}
	if c.itemRect.Empty() || c.depth >= len(m.levels) {
		return
	}
	lv := m.levels[c.depth]
	first, last := lv.vm.Range()
	for i := first; i < last; i++ {
		row := buffer.Rect{
			X: c.itemRect.X,
			Y: c.itemRect.Y + (i - lv.vm.Offset()),
			W: c.itemRect.W,
			H: 1,
		}
		if row.Bottom() > c.itemRect.Bottom() || !c.itemRect.Contains(row.X+1, row.Y) {
			// The engine's range is bounded by the viewport it was resized to, so
			// this is defensive rather than reachable; a write past the item rect
			// would overwrite the next column.
			continue
		}
		m.drawRow(buf, c, row, i, i == lv.sel)
	}
}

// drawRow paints one item row: the row's background, then its five regions.
//
// The background is filled across the WHOLE row rather than behind the label, so
// a selection is a row and not a word — which is the difference between a menu
// item that looks selected and one that looks like part of a highlighted sentence.
func (m *Menu) drawRow(buf *buffer.Buffer, c *layoutCol, row buffer.Rect, i int, selected bool) {
	disabled := m.itemDisabled(c.depth, i)

	// Only the SELECTED row is filled. Everything else keeps the background the
	// block already painted across the whole rectangle — which is both cheaper and
	// the reason a menu can be given a Block background and have it show through.
	// Filling every row with the resolved ItemStyle instead would paint the block's
	// background away on every row but the selected one, which is exactly the
	// composition mistake block's documentation warns about.
	// A selected row is filled across its WHOLE width, not behind its text, because
	// a highlight covering only the letters is a highlight of a word rather than of a
	// row. The fill happens only when the resolved style asks for something the
	// block's own background does not already provide, so a menu with no
	// configuration shows one continuous surface instead of a stripe.
	if selected {
		if sel := m.selectedStyle().Resolved(); sel != buffer.DefaultStyle {
			buf.FillRect(row, sel.Blank())
		}
	}

	st := m.labelStyle(selected, disabled)
	if c.hasCheck {
		glyph := m.checkEmptyRune
		if m.Checked(c.depth, i) {
			glyph = m.checkRune
		}
		buf.Set(c.checkX, row.Y, glyph, m.checkStyle().Resolved())
	}
	buf.SetString(c.labelX, row.Y, m.labelAt(c, i), st.Resolved())
	if c.showHint {
		buf.SetString(c.hintX, row.Y, m.hintAt(c, i), m.hintStyle().Resolved())
	}
	if c.hasSubmenu && c.submenuX > 0 && m.itemHasSubmenu(c.depth, i) {
		buf.Set(c.submenuX, row.Y, m.submenuRune, m.checkStyle().Resolved())
	}

	// The marker is drawn LAST and only for the selected, enabled row, so a
	// disabled item can never be the one wearing it.
	if selected && !disabled {
		mst := m.MarkerStyle
		if mst.IsUnset() {
			// An unset marker style follows the selected row, because a marker in
			// ItemStyle on a reversed row would be the one unreadable thing here.
			mst = m.SelectedStyle
		}
		buf.Set(c.markerX, row.Y, m.markRune, mst.Resolved())
	}
}

// drawHeader paints a level's header row: the bracket or the space-padding, then
// the header text.
//
// The bracket is the colour-independent statement of which level is active, and
// it is drawn in the header's style rather than the header TEXT's, so a reader
// who cannot distinguish the two colours still sees which level is live.
func (m *Menu) drawHeader(buf *buffer.Buffer, c *layoutCol) {
	hst := m.headerStyle(c.active)
	// The header row is filled only when the author gave the style a BACKGROUND of
	// its own. An unset background means "inherit what is under me", which is the
	// block's — so a menu with no configuration shows one continuous surface rather
	// than a stripe across its headers, and an application that wants the stripe
	// asks for it by naming a background.
	if !hst.BG.IsUnset() {
		buf.FillRect(c.headRect, hst.Resolved().Blank())
	}
	// The bracket pair is the colour-independent statement of which level is active,
	// and it is drawn at the column's two outer edges rather than hugging the text, so
	// a header occupies the same cells whether or not it is the active one. A header
	// that changed width as the selection moved would make the whole row jump on
	// every arrow key.
	if c.active {
		buf.Set(c.headRect.X, c.headRect.Y, firstRuneOf(headerMarkOpen), hst.Resolved())
		buf.Set(c.headRect.Right()-1, c.headRect.Y, firstRuneOf(headerMarkClose), hst.Resolved())
	} else {
		buf.Set(c.headRect.X, c.headRect.Y, firstRuneOf(headerMarkIdle), hst.Resolved())
	}
	// The text starts after the marker cell and is capped before the closing
	// bracket, so a long parent label is truncated rather than overwriting it. The
	// spans are cached with the layout, so this does not allocate.
	buf.SetSpansCappedIn(c.headRect.X+markW, c.headRect.Right()-1, c.headRect.Y, c.headerSpans, m.truncMark)
}

// labelStyle returns the rendition for a row's TEXT.
//
// It is separate from the selected row's background because the two are different
// jobs: the background says "this row is selected", and the text style says what
// this row is. A disabled row's text takes DisabledStyle over the selected one,
// because a disabled item is never selected and the two states cannot both hold.
func (m *Menu) labelStyle(selected, disabled bool) buffer.Style {
	if disabled {
		if m.DisabledStyle.IsUnset() {
			// Faint rather than merely a dimmer colour: the default says "not
			// available" through an ATTRIBUTE, so it survives NO_COLOR.
			return buffer.MutedStyle
		}
		return m.DisabledStyle
	}
	if selected {
		return m.selectedStyle()
	}
	return m.ItemStyle
}

// selectedStyle returns the selected row's rendition, defaulting to ItemStyle with
// AttrReverse so a selection is legible with no configuration at all.
func (m *Menu) selectedStyle() buffer.Style {
	if m.SelectedStyle.IsUnset() {
		return m.ItemStyle.Resolved().WithAttr(buffer.AttrReverse)
	}
	return m.SelectedStyle
}

// hintStyle, checkStyle and headerStyle resolve their fields through the unset
// sentinel, so "the author set nothing" has one meaning in this package.
func (m *Menu) hintStyle() buffer.Style {
	if m.HintStyle.IsUnset() {
		return buffer.MutedStyle
	}
	return m.HintStyle
}

func (m *Menu) checkStyle() buffer.Style {
	if m.CheckStyle.IsUnset() {
		return m.ItemStyle
	}
	return m.CheckStyle
}

func (m *Menu) headerStyle(active bool) buffer.Style {
	if active {
		if m.HeaderActiveStyle.IsUnset() {
			return buffer.HeadingStyle
		}
		return m.HeaderActiveStyle
	}
	if m.HeaderStyle.IsUnset() {
		return buffer.MutedStyle
	}
	return m.HeaderStyle
}

// labelAt and hintAt return a level's cached strings for item i, or "" for an
// index outside them. The bounds test is what makes a click or a stale index in
// Handle safe rather than a panic.
func (m *Menu) labelAt(c *layoutCol, i int) string {
	if i < 0 || i >= len(c.labels) {
		return ""
	}
	return c.labels[i]
}

func (m *Menu) hintAt(c *layoutCol, i int) string {
	if i < 0 || i >= len(c.hints) {
		return ""
	}
	return c.hints[i]
}

// itemDisabled reports whether item i at depth d is disabled, tolerating an index
// outside the level.
func (m *Menu) itemDisabled(d, i int) bool {
	items := m.levelItems(d)
	return i < 0 || i >= len(items) || items[i].Disabled
}

// itemHasSubmenu reports whether item i at depth d opens a submenu, tolerating an
// index outside the level.
func (m *Menu) itemHasSubmenu(d, i int) bool {
	items := m.levelItems(d)
	return i >= 0 && i < len(items) && len(items[i].Items) > 0
}

// handleMouse consumes a press inside the menu and a wheel notch over it.
//
// A press selects the pressed row, takes focus, and opens the submenu of a row
// that was ALREADY the selection. A press OUTSIDE the menu is left for an
// ancestor, which is what lets a Split route it to a sibling.
func (m *Menu) handleMouse(ev termmosaic.Event) bool {
	if !m.bounds.Contains(ev.Mouse.X, ev.Mouse.Y) {
		return false
	}
	m.ensureLayout()
	switch ev.Mouse.Button {
	case termmosaic.MouseWheelUp:
		m.scrollBy(-wheelRows)
		return true
	case termmosaic.MouseWheelDown:
		m.scrollBy(wheelRows)
		return true
	}
	if ev.Mouse.Action != termmosaic.MousePress || ev.Mouse.Button != termmosaic.MouseLeft {
		return false
	}
	d, i, ok := m.itemAt(ev.Mouse.X, ev.Mouse.Y)
	if !ok {
		return false
	}
	m.focused = true
	// A press on a different level selects at THAT level and discards the deeper
	// path: the user pointed at a level, so the cursor belongs there.
	if d < len(m.levels) {
		m.levels = m.levels[:d+1]
	}
	changed := m.selectAt(d, i)
	if !changed {
		m.openSubmenu()
	}
	return true
}

// scrollBy moves the ACTIVE level's scroll offset by delta rows without changing
// the selection, which is what a wheel notch does and what moving a selection
// does not.
func (m *Menu) scrollBy(delta int) {
	if m.active() < 0 || m.active() >= len(m.levels) {
		return
	}
	m.levels[m.active()].vm.ScrollBy(delta)
}

// itemAt returns the depth and item index under the cell (x, y), and whether any
// item is there.
//
// It walks the cached columns and asks each level's scroll engine, so it is
// O(open levels) whatever the tree's size — the reasoning virtual.Model exists
// for, applied to a menu whose levels are few but whose items may be many.
func (m *Menu) itemAt(x, y int) (int, int, bool) {
	for d := range m.lay.cols {
		c := &m.lay.cols[d]
		if !c.itemRect.Contains(x, y) {
			continue
		}
		if d >= len(m.levels) {
			return 0, 0, false
		}
		i, ok := m.levels[d].vm.ItemAt(y - c.itemRect.Y)
		if !ok {
			return 0, 0, false
		}
		return d, i, true
	}
	return 0, 0, false
}

// Menu is a Widget, a Focusable and a Minimizable.
var (
	_ termmosaic.Widget      = (*Menu)(nil)
	_ termmosaic.Focusable   = (*Menu)(nil)
	_ termmosaic.Minimizable = (*Menu)(nil)
)
