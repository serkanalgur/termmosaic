package docsgen

import (
	"math"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/data"
	"github.com/serkanalgur/termmosaic/widgets/form"
	"github.com/serkanalgur/termmosaic/widgets/split"
	"github.com/serkanalgur/termmosaic/widgets/viz"
)

// The demo palette.
//
// TermMosaic ships NO default colours on purpose — colour is the application's
// choice — so a capture that left every style unset would be a picture of every
// widget in the same terminal default. That is faithful and completely
// unpersuasive, and it would misrepresent the five visualisation widgets, whose
// whole argument is what they look like. So this file supplies one theme and
// applies it, which is what a real application does and what a reader needs to
// see to judge the widgets.
//
// Every pairing below clears 4.5:1 against the surface the capture CSS paints,
// which is the WCAG 2.2 AA threshold for body text. Colour is never the only
// signal either: the widgets that can be misread carry an attribute, a marker
// glyph or a written value as well, which is the rule the widgets' own
// accessibility sections describe and the capture should not quietly break.
var (
	colAccent = buffer.NewColour(0x7F, 0xB0, 0xFF)
	colOK     = buffer.NewColour(0x8C, 0xD9, 0x8A)
	colWarn   = buffer.NewColour(0xE8, 0xC5, 0x6E)
	colCrit   = buffer.NewColour(0xF0, 0x87, 0x87)
	colMuted  = buffer.NewColour(0x9A, 0xA4, 0xB8)
	colInk    = buffer.NewColour(0x0E, 0x11, 0x18)
	colPaper  = buffer.NewColour(0xE8, 0xEC, 0xF4)
	colPanel  = buffer.NewColour(0x1C, 0x21, 0x2C)
)

// Styles derived from the palette, built with the sanctioned With* chain rather
// than by composite literal — buffer's own authoring rule, because a partial
// literal leaves the other channel as opaque black and compiles happily.
//
// styTrack carries a BACKGROUND rather than merely a dimmer hue. A meter or a
// bar whose track is only a quieter colour is an invisible track: with nothing
// painted there is no band for the fill to be read against, and the widget loses
// the property its own accessibility notes promise — that the shape is legible
// without colour.
var (
	styTitle    = buffer.NewStyle(colAccent, buffer.UnsetColour, buffer.AttrBold)
	styHeading  = buffer.NewStyle(colMuted, buffer.UnsetColour, buffer.AttrBold)
	styBody     = buffer.NewStyle(colPaper, buffer.UnsetColour, 0)
	styDim      = buffer.NewStyle(colMuted, buffer.UnsetColour, 0)
	styGood     = buffer.NewStyle(colOK, buffer.UnsetColour, 0)
	styWarn     = buffer.NewStyle(colWarn, buffer.UnsetColour, 0)
	styCrit     = buffer.NewStyle(colCrit, buffer.UnsetColour, buffer.AttrBold)
	styOnAccent = buffer.NewStyle(colInk, colAccent, buffer.AttrBold)
	styOnOK     = buffer.NewStyle(colInk, colOK, buffer.AttrBold)
	styOnWarn   = buffer.NewStyle(colInk, colWarn, buffer.AttrBold)
	styOnCrit   = buffer.NewStyle(colInk, colCrit, buffer.AttrBold)
	stySelectBG = buffer.NewStyle(colInk, colAccent, buffer.AttrBold)
	styHeaderBG = buffer.NewStyle(colAccent, buffer.UnsetColour, buffer.AttrBold|buffer.AttrUnderline)
	styTrack    = buffer.NewStyle(colPaper, colPanel, 0)
	styBorder   = buffer.NewStyle(colMuted, buffer.UnsetColour, 0)
	styPanel    = buffer.NewStyle(buffer.DefaultColour, colPanel, 0)
)

// band returns a zone's track rendition: the band's own hue as foreground over
// the panel background.
//
// A meter with three identically-styled zones draws three zones that look like
// one, and the capture then fails to show the property the widget exists for —
// that the shape of the BUDGET is visible before any of it is spent.
func band(c buffer.Colour) buffer.Style { return buffer.NewStyle(c, colPanel, 0) }

// chrome applies the border, border colour and title every widget in the catalog
// draws itself inside.
//
// It exists as one function rather than four lines in twenty-two entries because
// a widget's block defaults to NO border and NO title, which means a capture that
// forgets this step is a rectangle of floating text. A screenshot of a widget
// with no chrome misrepresents how the widget is used, which is the one thing a
// capture is not allowed to do.
//
// The title is passed in rather than derived from the name: the entry already
// knows what the frame is showing, and "Modules" says more to a reader than
// "Table".
func chrome(b *block.Block, title string) *block.Block {
	b.SetBorder(buffer.BorderRounded)
	b.SetBorderStyle(styBorder)
	b.SetTitleString(title, styTitle)
	b.SetTitleAlign(geometry.AlignLeft)
	return b
}

// Entries returns the catalog, in the order the site's widget index lists it:
// core, forms, data, visualisation.
//
// The order is the reading order rather than alphabetical because the index is a
// narrative — a reader arrives wanting to lay out text, then collect input, then
// show a collection, then graph a number — and an alphabetical catalog loses
// that. The name is carried on every Entry, so sorting for any other purpose
// costs one line.
func Entries() []Entry {
	return []Entry{
		blockEntry(),
		textEntry(),
		paragraphEntry(),
		splitEntry(),
		textInputEntry(),
		textAreaEntry(),
		selectEntry(),
		checkboxEntry(),
		radioEntry(),
		toggleEntry(),
		tabsEntry(),
		buttonEntry(),
		keyHintEntry(),
		listEntry(),
		tableEntry(),
		treeEntry(),
		pagerEntry(),
		progressBarEntry(),
		gaugeEntry(),
		meterEntry(),
		sparklineEntry(),
		barChartEntry(),
	}
}

// blockEntry is the chrome widget every other one draws itself inside, captured
// with a border, a title, padding and a background, because a bare rectangle
// shows none of that.
func blockEntry() Entry {
	return Entry{
		Name:        "Block",
		Package:     "widgets/block",
		Constructor: "block.New(r buffer.Rect) *block.Block",
		Summary:     "A bordered, titled, padded rectangle that other widgets draw inside.",
		Note: "A Block draws chrome only — it has no children. What goes inside it " +
			"is the application's business: wrap a widget, or use a Split to place " +
			"several. The capture shows the padding and the interior band, which is " +
			"the part of the widget that is actually drawn.",
		Widths:  Widths(),
		Heights: rows(9),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			b := block.New(r)
			b.SetBorder(buffer.BorderRounded)
			b.SetTitleString("Package layout", styTitle)
			b.SetTitleAlign(geometry.AlignLeft)
			b.SetPadding(1)
			b.SetBorderStyle(styBorder)
			b.SetBackground(styPanel)
			return b
		},
	}
}

// textEntry is Text with several styled spans and a right-aligned one, which is
// what makes it a demonstration of the span API rather than a string.
func textEntry() Entry {
	return Entry{
		Name:        "Text",
		Package:     "widgets/basic",
		Constructor: "basic.NewText(r buffer.Rect, spans []buffer.Span) *basic.Text",
		Summary:     "A single line of styled spans, aligned within its rectangle.",
		Widths:      Widths(),
		Heights:     rows(3),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			t := basic.NewText(r, []buffer.Span{
				buffer.NewSpan("TermMosaic", styTitle),
				buffer.NewSpan(" — ", styDim),
				buffer.NewSpan("22 widgets", styGood),
				buffer.NewSpan(", 2 dependencies", styDim),
			})
			t.SetBackground(styBody)
			return t
		},
	}
}

// paragraphEntry is Text's wrapping sibling, and the wrapping is the point: the
// 40-column capture must visibly wrap differently from the 120-column one,
// which is what ADR 0007's width story looks like when it is rendered.
func paragraphEntry() Entry {
	return Entry{
		Name:        "Paragraph",
		Package:     "widgets/basic",
		Constructor: "basic.NewParagraph(r buffer.Rect, spans []buffer.Span) *basic.Paragraph",
		Summary:     "Wrapped, styled prose that reflows to the width it is given.",
		Widths:      Widths(),
		Heights:     rows(10, 9, 7),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			return basic.NewParagraph(r, []buffer.Span{
				buffer.NewSpan("A capture is one settled frame after N frames have been ", styBody),
				buffer.NewSpan("rendered", styTitle),
				buffer.NewSpan(".\nIt cannot show a keypress, a selection moving, a cell flickering or a pager scrolling. ", styBody),
				buffer.NewSpan("Every widget page therefore links a runnable program: the capture is a still, and the program is the widget.", styGood),
			})
		},
	}
}

// splitEntry composes a Table and a List side by side, so the capture shows a
// Split doing its actual job — holding focus between two real widgets and
// dividing a width — rather than showing a divider in an empty rectangle.
func splitEntry() Entry {
	return Entry{
		Name:        "Split",
		Package:     "widgets/split",
		Constructor: "split.New(d layout.Direction, panes ...termmosaic.Widget) *split.Split",
		Summary:     "A container that divides its rectangle among focusable panes.",
		Note: "Focus moves between panes with Tab and the focused pane is the one " +
			"that takes keys. A still frame cannot show that; the marker in the " +
			"second pane's title is where the capture has to stop.",
		Widths:  Widths(),
		Heights: rows(11),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			tbl := demoTable(r)
			chrome(tbl.Block(), "modules")
			lst := demoList(r)
			chrome(lst.Block(), "packages")
			s := split.New(layout.Horizontal, tbl, lst)
			// SetBounds is not optional: split.New takes a direction and panes but
			// no rectangle, so without it the Split's own rect is empty and Draw
			// returns before drawing anything. The capture would be a blank screen
			// attributed to the widget, which is precisely the failure this tool
			// exists to make impossible.
			s.SetBounds(r)
			// Percentage rather than Ratio for the first pane: Ratio(3, 2) is THREE HALVES of
			// the available space, not three fifths of it, so it hands the first
			// pane the whole width and the second pane nothing. This is the
			// difference the plan's composition guidance warns about and the reason
			// the constraint kinds have such similar names.
			s.SetConstraints([]layout.Constraint{layout.Percentage(60), layout.Fill(1)})
			s.SetSpacing(1)
			s.SetBackground(styPanel)
			s.SetFocus(1)
			s.SetFocused(true)
			return s
		},
	}
}

// textInputEntry is a focused TextInput with a real selection, driven with key
// events rather than SelectAll, because a whole-string selection is the least
// interesting thing a TextInput can show and a partial one is what a user sees.
func textInputEntry() Entry {
	return Entry{
		Name:        "TextInput",
		Package:     "widgets/form",
		Constructor: "form.NewTextInput(r buffer.Rect) *form.TextInput",
		Summary:     "A single-line editable field with a cursor, a selection and undo.",
		Note:        "This capture is taken with the field focused, so the cursor mark is live.",
		Widths:      Widths(),
		Heights:     rows(3),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			ti := form.NewTextInput(r)
			ti.SetText("github.com/serkanalgur/termmosaic")
			ti.TextStyle = styBody
			ti.SelectionStyle = stySelectBG
			ti.SetFocused(true)
			ti.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyHome, 0))
			for i := 0; i < 5; i++ {
				ti.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyRight, termmosaic.ModShift))
			}
			return ti
		},
	}
}

// textAreaEntry is a multi-line field scrolled past its first line, because a
// TextArea at offset zero cannot show scrolling, scroll indicators or the
// difference from TextInput at all.
func textAreaEntry() Entry {
	return Entry{
		Name:        "TextArea",
		Package:     "widgets/form",
		Constructor: "form.NewTextArea(r buffer.Rect) *form.TextArea",
		Summary:     "A multi-line editable field with wrapping, selection and undo.",
		Widths:      Widths(),
		Heights:     rows(7),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			ta := form.NewTextArea(r)
			ta.SetText("# capture notes\n\nThe gauge dial is Braille: eight levels of\nvertical resolution per cell.\nThe meter track is Block Elements.\nIf a web font mis-advances either,\nthe plain-text capture is the\none that stays true.")
			ta.SetFocused(true)
			ta.SetCursor(23)
			ta.TextStyle = styBody
			ta.Background = styBody
			return ta
		},
	}
}

// selectEntry is a Select with the list open, which is the only state in which
// a Select shows more than one of its labels.
func selectEntry() Entry {
	return Entry{
		Name:        "Select",
		Package:     "widgets/form",
		Constructor: "form.NewSelect(r buffer.Rect, labels []string) *form.Select",
		Summary:     "A single-choice control that expands into a scrollable list.",
		Note:        "A Select draws its options below the field rather than over the page, so the capture shows ten of the ten labels with the fifth highlighted.",
		Widths:      Widths(),
		Heights:     rows(9),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			s := form.NewSelect(r, []string{
				"alpine", "bookworm", "cachyos", "debian", "endeavouros",
				"fedora", "gentoo", "nixos", "void", "ziglinux",
			})
			s.SetSelected(4)
			s.SetFocused(true)
			s.Marker = "> "
			s.OptionStyle = styBody
			s.SelectedStyle = stySelectBG
			return s
		},
	}
}

// checkboxEntry is three Checkboxes in a vertical Split, covering all three
// states including Indeterminate — which is the state an application computes
// and a zero Checkbox can never reach, so a capture is the honest way to show
// what it looks like.
func checkboxEntry() Entry {
	return Entry{
		Name:        "Checkbox",
		Package:     "widgets/form",
		Constructor: "form.NewCheckbox(r buffer.Rect, label string) *form.Checkbox",
		Summary:     "A tri-state box: unchecked, checked, or indeterminate.",
		Note: "Indeterminate is never produced by toggling. Only an application " +
			"that has computed it from its children can set it, so the capture sets it directly.",
		Widths:  Widths(),
		Heights: rows(5),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			all := form.NewCheckbox(r, "widgets (22)")
			all.TriState = true
			all.SetState(form.Checked)
			all.CheckedStyle = styGood
			all.LabelStyle = styBody

			some := form.NewCheckbox(r, "visualisation (5)")
			some.TriState = true
			some.SetState(form.Indeterminate)
			some.CheckedStyle = styWarn
			some.LabelStyle = styBody

			none := form.NewCheckbox(r, "release (0)")
			none.TriState = true
			none.SetState(form.Unchecked)
			none.LabelStyle = styDim

			some.SetFocused(true)
			s := split.New(layout.Vertical, all, some, none)
			s.SetBounds(r)
			s.SetSpacing(1)
			s.SetBackground(styPanel)
			return s
		},
	}
}

// radioEntry is a Radio group with a selection and focus, which is the whole of
// its behaviour: a Radio with nothing selected demonstrates nothing.
func radioEntry() Entry {
	return Entry{
		Name:        "Radio",
		Package:     "widgets/form",
		Constructor: "form.NewRadio(r buffer.Rect, labels []string) *form.Radio",
		Summary:     "A single-choice group of labels, navigated with the arrow keys.",
		Widths:      Widths(),
		Heights:     rows(6),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			g := form.NewRadio(r, []string{"horizontal", "vertical", "both"})
			g.SetSelected(2)
			g.SetFocused(true)
			g.OptionStyle = styBody
			g.SelectedStyle = stySelectBG
			g.FocusStyle = styHeading
			return g
		},
	}
}

// toggleEntry is a focused, ON Toggle: the interesting half of a two-state
// widget is the state that is not the zero value.
func toggleEntry() Entry {
	return Entry{
		Name:        "Toggle",
		Package:     "widgets/form",
		Constructor: "form.NewToggle(r buffer.Rect, label string) *form.Toggle",
		Summary:     "A two-state switch with an on and an off rendering.",
		Widths:      Widths(),
		Heights:     rows(3),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			t := form.NewToggle(r, "wrap text")
			t.SetOn(true)
			t.SetFocused(true)
			t.LabelStyle = styBody
			t.OnStyle = styOnOK
			return t
		},
	}
}

// tabsEntry is Tabs with three labels, the second selected, and a body below so
// the tab strip is not the only thing in the frame.
func tabsEntry() Entry {
	return Entry{
		Name:        "Tabs",
		Package:     "widgets/form",
		Constructor: "form.NewTabs(r buffer.Rect, labels []string) *form.Tabs",
		Summary:     "A horizontal strip of labels, one of which is the active pane.",
		Widths:      Widths(),
		Heights:     rows(4),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			t := form.NewTabs(r, []string{"Logs", "Metrics", "Traces", "Config"})
			t.SetSelected(1)
			t.SetFocused(true)
			t.TabStyle = styDim
			t.SelectedStyle = styTitle
			return t
		},
	}
}

// buttonEntry is three Buttons — the default, a focused one and a disabled one
// — because the disabled rendering is a separate code path a single button
// cannot reach.
func buttonEntry() Entry {
	return Entry{
		Name:        "Button",
		Package:     "widgets/form",
		Constructor: "form.NewButton(r buffer.Rect, label string) *form.Button",
		Summary:     "A labelled, activatable control with a default and a disabled state.",
		Widths:      Widths(),
		Heights:     rows(3),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			ok := form.NewButton(r, "Render")
			ok.LabelStyle = styOnAccent
			ok.FocusStyle = styOnAccent
			ok.SetFocused(true)

			save := form.NewButton(r, "Save")
			save.Disabled = true
			save.LabelStyle = styDim
			save.DisabledStyle = styDim

			s := split.New(layout.Horizontal, ok, save)
			s.SetBounds(r)
			s.SetConstraints([]layout.Constraint{layout.Length(14), layout.Length(10)})
			s.SetSpacing(1)
			s.SetBackground(styPanel)
			return s
		},
	}
}

// keyHintEntry is a hint bar with the bindings a real screen has, which is what
// makes it legible: a hint of invented bindings teaches nothing.
func keyHintEntry() Entry {
	return Entry{
		Name:        "KeyHint",
		Package:     "widgets/form",
		Constructor: "form.NewKeyHint(r buffer.Rect, bindings []form.Binding) *form.KeyHint",
		Summary:     "A one-line bar of key names and their meanings.",
		Note: "At 40 columns the bar has fewer cells than bindings, so it drops " +
			"the lowest-priority ones rather than truncating mid-word. That " +
			"trade is visible in the narrow capture.",
		Widths:  Widths(),
		Heights: rows(3),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			k := form.NewKeyHint(r, []form.Binding{
				form.NewBinding("tab", "next pane"),
				form.NewBinding("enter", "activate"),
				form.NewBinding("q", "quit"),
				form.NewBinding("?", "all keys"),
			})
			k.SetSep("  ")
			k.KeyStyle = styTitle
			k.HelpStyle = styDim
			k.SeparatorStyle = styBorder
			return k
		},
	}
}

// listEntry is a List with a selection, a scrollbar and more items than rows, so
// all three of the things that make a List a List are in one frame.
func listEntry() Entry {
	return Entry{
		Name:        "List",
		Package:     "widgets/data",
		Constructor: "data.NewList(r buffer.Rect, items ...data.ListItem) *data.List",
		Summary:     "A scrollable, selectable list with a marker gutter and a scrollbar.",
		Note:        "The scrollbar thumb's POSITION is the signal; its colour is not.",
		Widths:      Widths(),
		Heights:     rows(11),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			l := demoList(r)
			chrome(l.Block(), "packages")
			return l
		},
	}
}

// tableEntry is a Table with three columns, six rows, a header, a selection and
// a column scrolled off the left — which is the state that distinguishes Table
// from List, since List has no columns to lose.
func tableEntry() Entry {
	return Entry{
		Name:        "Table",
		Package:     "widgets/data",
		Constructor: "data.NewTable(r buffer.Rect, cols ...data.Column) *data.Table",
		Summary:     "Rows in columns, with a header, a selection and horizontal scrolling.",
		Widths:      Widths(),
		Heights:     rows(10),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			t := demoTable(r)
			chrome(t.Block(), "modules")
			return t
		},
	}
}

// treeEntry is a Tree with two expanded branches and one collapsed, with the
// selection on a nested row, because a Tree with everything expanded and nothing
// selected shows the least of what a Tree does.
func treeEntry() Entry {
	return Entry{
		Name:        "Tree",
		Package:     "widgets/data",
		Constructor: "data.NewTree(r buffer.Rect, nodes ...data.Node) *data.Tree",
		Summary:     "A hierarchy with expandable nodes, keyboard navigation and lazy row rendering.",
		Note: "Expansion is state, and a still frame shows it only by the twisties " +
			"and the indent. The example program is where a reader sees a node open.",
		Widths:  Widths(),
		Heights: rows(13),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			nodes := []data.Node{
				{Label: "termmosaic", Expanded: true, Children: []data.Node{
					{Label: "buffer", Expanded: true, Children: []data.Node{
						{Label: "cell.go"},
						{Label: "style.go"},
						{Label: "wrap.go"},
					}},
					{Label: "render", Expanded: true, Children: []data.Node{
						{Label: "renderer.go"},
						{Label: "diff.go"},
					}},
					{Label: "widgets"},
					{Label: "headless"},
				}},
				{Label: "examples", Expanded: true, Children: []data.Node{
					{Label: "hello"},
					{Label: "dashboard"},
				}},
				{Label: "docs", Expanded: false, Children: []data.Node{
					{Label: "SITE-PLAN.md"},
					{Label: "adr"},
				}},
				{Label: "README.md"},
			}
			t := data.NewTree(r, nodes...)
			t.SelectRow(3)
			t.SetFocused(true)
			chrome(t.Block(), "repository")
			return t
		},
	}
}

// pagerEntry is a Pager scrolled into the middle of a long text with a live
// query, because a Pager at the top of its text is indistinguishable from a
// Paragraph and the query highlight is the feature.
func pagerEntry() Entry {
	return Entry{
		Name:        "Pager",
		Package:     "widgets/data",
		Constructor: "data.NewPager(r buffer.Rect) *data.Pager",
		Summary:     "A scrollable long text with search, match highlighting and a position readout.",
		Widths:      Widths(),
		Heights:     rows(11),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			p := data.NewPager(r)
			p.SetText(pagerText)
			p.SetFocused(true)
			p.SetQuery("diff")
			p.ScrollBy(14)
			chrome(p.Block(), "docs/ARCHITECTURE.md")
			return p
		},
	}
}

// progressBarEntry is a ProgressBar two-thirds done with a label and a
// percentage, because the number is what makes the bar readable in monochrome
// and a capture should show it.
func progressBarEntry() Entry {
	return Entry{
		Name:        "ProgressBar",
		Package:     "widgets/viz",
		Constructor: "viz.NewProgressBar(r buffer.Rect) *viz.ProgressBar",
		Summary:     "A ratio from 0 to 1 drawn as a filled block-element bar.",
		Note: "Filled with U+2588 FULL BLOCK. The difference between FillStyle and " +
			"TrackStyle carries an attribute as well as a colour, so the bar is " +
			"readable without either.",
		Widths:  Widths(),
		Heights: rows(4),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			p := viz.NewProgressBar(r)
			p.Set(0.62)
			p.SetLabel("generating captures", styHeading)
			p.Percentage = true
			p.FillStyle = styOnOK
			p.TrackStyle = styTrack
			p.PercentStyle = styTitle
			chrome(p.Block(), "render")
			return p
		},
	}
}

// gaugeEntry is a Gauge at 70 with a Braille dial, the widget where the cells
// carry a value rather than a glyph and a naive ANSI conversion would fall apart.
func gaugeEntry() Entry {
	return Entry{
		Name:        "Gauge",
		Package:     "widgets/viz",
		Constructor: "viz.NewGauge(r buffer.Rect) *viz.Gauge",
		Summary:     "A bounded reading drawn as a Braille dial or a fallback bar.",
		Note: "The dial is Braille, U+2800..28FF: eight levels of vertical " +
			"resolution in each cell, two samples per cell. A web font that gives " +
			"those glyphs a different advance width destroys the arc, which is why " +
			"the plain-text capture beside it is mandatory rather than a nicety.",
		Widths:  Widths(),
		Heights: rows(11, 9, 9),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			g := viz.NewGauge(r)
			g.Set(70)
			g.SetLabel("render", styHeading)
			g.ArcStyle = styOnOK
			g.TrackStyle = styTrack
			g.ValueStyle = styTitle
			chrome(g.Block(), "dashboard")
			return g
		},
	}
}

// meterEntry is a Meter reading 78, which is past the warn boundary and inside
// the warn band: a meter sitting in its default zone would not show that the
// zones exist.
func meterEntry() Entry {
	return Entry{
		Name:        "Meter",
		Package:     "widgets/viz",
		Constructor: "viz.NewMeter(r buffer.Rect) *viz.Meter",
		Summary:     "A budget with named zones, a threshold marker and the active zone's name in words.",
		Note: "Two non-colour signals: the zone boundaries are drawn as '+', the " +
			"threshold as '|', and the active band is named in text.",
		Widths:  Widths(),
		Heights: rows(3),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			m := viz.NewMeter(r)
			m.SetZones(demoZones())
			m.Set(78)
			m.Threshold = 85
			m.ThresholdVisible = true
			chrome(m.Block(), "disk")
			return m
		},
	}
}

// sparklineEntry is a Sparkline whose series has a shape a reader can name — a
// damped oscillation with a spike — because twenty identical samples prove
// nothing about a sparkline.
func sparklineEntry() Entry {
	return Entry{
		Name:        "Sparkline",
		Package:     "widgets/viz",
		Constructor: "viz.NewSparkline(r buffer.Rect) *viz.Sparkline",
		Summary:     "A series of numbers drawn as Braille or block elements, scaled to its own range.",
		Note: "Braille encoding, two samples per cell, so the series holds twice " +
			"the values the width can show. The threshold defaults to reverse video, " +
			"which is an attribute rather than a colour.",
		Widths: Widths(),
		// A horizontal sparkline draws into a single row by design, so a tall
		// capture is mostly empty block. One row of content plus the two border
		// rows is what the widget actually uses.
		Heights: rows(3),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			s := viz.NewSparkline(r)
			s.SetValues(demoSeries())
			s.Auto = true
			s.Braille = true
			s.Style = styBody
			s.MinStyle = styCrit
			s.MaxStyle = styWarn
			s.LastStyle = styTitle
			chrome(s.Block(), "frame time (ms)")
			return s
		},
	}
}

// barChartEntry is a vertical BarChart with five categories, a labelled axis
// and per-bar values, because two bars of nearly equal height are
// indistinguishable in monochrome without the number beside them.
func barChartEntry() Entry {
	return Entry{
		Name:        "BarChart",
		Package:     "widgets/viz",
		Constructor: "viz.NewBarChart(r buffer.Rect) *viz.BarChart",
		Summary:     "Categorical magnitudes as horizontal or vertical bars, with an axis and per-bar values.",
		Note: "Bars are Block Elements. The value beside each bar is the " +
			"colour-independent reading and the capture keeps it.",
		Widths:  Widths(),
		Heights: rows(11),
		Construct: func(r buffer.Rect) termmosaic.Widget {
			c := viz.NewBarChart(r)
			c.SetData([]viz.Datum{
				{Label: "buffer", Value: 412, Style: styOnOK},
				{Label: "render", Value: 968, Style: styOnOK},
				{Label: "widgets", Value: 1_284, Style: styOnWarn},
				{Label: "headless", Value: 2_140, Style: styOnCrit},
				{Label: "input", Value: 356, Style: styOnOK},
			})
			c.Axis = true
			c.ShowValue = true
			// Vertical is a field, not a second widget: the two orientations share
			// the normalisation, the axis and the value labels, so a capture that
			// used the zero value would show the rarer orientation by default.
			c.Vertical = true
			c.AxisStyle = styBorder
			c.LabelStyle = styBody
			chrome(c.Block(), "allocations per frame")
			return c
		},
	}
}

// ---------------------------------------------------------------------------
// shared demo fixtures
//
// The Table, the List and the zone and series data live here rather than being
// inlined, because Split composes the same Table and List the Table and List
// captures show. Two constructions of the same fixture would be two chances for
// the two captures to disagree, and a capture that disagrees with its sibling is
// worse than no capture.
// ---------------------------------------------------------------------------

// tableModules is the fixture the Table and Split captures share.
var tableModules = []struct {
	name    string
	size    string
	state   string
	styling buffer.Style
}{
	{"buffer", "8.2 kB", "stable", styGood},
	{"geometry", "3.1 kB", "stable", styGood},
	{"headless", "11.4 kB", "beta", styWarn},
	{"layout", "6.7 kB", "stable", styGood},
	{"render", "14.9 kB", "beta", styWarn},
	{"virtual", "5.0 kB", "alpha", styCrit},
}

// demoTable builds the shared Table: three columns, six rows, a header, a
// selection two rows down and a column scrolled off the left edge.
//
// The horizontal scroll is deliberate rather than incidental: at 40 columns the
// table cannot fit three columns, so without the scroll it would simply lose
// the first one, and with it the reader sees the actual behaviour.
func demoTable(r buffer.Rect) *data.Table {
	cols := []data.Column{
		{Title: []buffer.Span{buffer.NewSpan("module", styHeaderBG)}, Width: 12},
		{Title: []buffer.Span{buffer.NewSpan("size", styHeaderBG)}, Width: 8, Align: geometry.AlignRight},
		{Title: []buffer.Span{buffer.NewSpan("state", styHeaderBG)}, Grow: 1},
	}
	rows := make([]data.Row, 0, len(tableModules))
	for _, m := range tableModules {
		rows = append(rows, data.Row{Cells: []data.Cell{
			{Text: m.name, Style: styBody},
			{Text: m.size, Style: styDim},
			{Text: m.state, Style: m.styling},
		}})
	}
	t := data.NewTable(r, cols...)
	t.SetRows(rows)
	// Header is off by default, so a Table that forgets it loses its header row
	// entirely rather than degrading. A capture of a table with no header is a
	// capture of the wrong widget.
	t.Header = true
	t.Select(2)
	t.SetFocused(true)
	t.SetColOffset(1)
	t.ItemStyle = styBody
	t.SelectedStyle = stySelectBG
	return t
}

// listPackages is the fixture the List and Split captures share: more items
// than rows, so the scrollbar has somewhere to be.
var listPackages = []string{
	"buffer", "geometry", "headless", "input", "layout", "render",
	"term", "virtual", "widgets/basic", "widgets/block", "widgets/data",
	"widgets/form", "widgets/split", "widgets/viz",
}

// demoList builds the shared List: fourteen items, the sixth selected, the
// marker gutter and the scrollbar both on.
func demoList(r buffer.Rect) *data.List {
	items := make([]data.ListItem, 0, len(listPackages))
	for _, p := range listPackages {
		items = append(items, data.ListItem{Label: p, Style: styBody})
	}
	l := data.NewList(r, items...)
	l.SetFocused(true)
	l.Select(5)
	l.SetOffset(3)
	l.Scrollbar = true
	l.Marker = "> "
	l.MarkerStyle = styTitle
	l.ItemStyle = styBody
	l.SelectedStyle = stySelectBG
	l.ScrollbarStyle = styTitle
	return l
}

// demoZones is a meter fixture whose bands are past the threshold of interest.
//
// The names are the point: a meter's active zone is named in words precisely so
// the reading survives a monochrome terminal and a colour-blind reader, and the
// capture should show a band being named.
func demoZones() []viz.Zone {
	return []viz.Zone{
		{Name: "free", From: 0, To: 60, Style: band(colOK), FillStyle: styOnOK},
		{Name: "used", From: 60, To: 90, Style: band(colWarn), FillStyle: styOnWarn},
		{Name: "critical", From: 90, Style: band(colCrit), FillStyle: styOnCrit},
	}
}

// demoSeries returns a sparkline fixture with a shape worth reading: a slow rise
// with two spikes and a fall, at two samples per cell.
//
// The count matters. Braille puts two samples in a cell, so a 120-column capture
// holds 240 of them; a 48-value series would occupy a quarter of the width and
// the plot would look like a series that ran out rather than one that was
// plotted. The count is generated from a formula rather than typed out, and
// formula rather than literals is a determinism decision: math.Sin is pure
// software with no platform-dependent fast path, so every run on every machine
// produces the same bytes, while a hand-written 240-number literal would be a
// thousand characters a reviewer has to trust.
func demoSeries() []float64 {
	const n = 240
	out := make([]float64, n)
	for i := range out {
		// t runs 0..1 across the series, so the shape is a function of position
		// and not of the sample count.
		t := float64(i) / float64(n-1)
		v := 18 + 10*math.Sin(2*math.Pi*2.5*t) + 6*math.Sin(2*math.Pi*7*t)
		// Two spikes: one broad, one narrow, so a reader can tell a peak from a
		// jitter.
		v += 26 * math.Exp(-math.Pow((t-0.32)/0.05, 2))
		v += 20 * math.Exp(-math.Pow((t-0.68)/0.02, 2))
		// A decline at the end, because a series that only ever rises reads as a
		// progress bar rather than a measurement.
		v -= 9 * t
		out[i] = math.Round(v*10) / 10
	}
	return out
}

// pagerText is a pager fixture long enough to scroll and varied enough that a
// query has more than one match, because a single hit does not show that the
// match indicator moves.
const pagerText = `Architecture

TermMosaic is a terminal UI framework built around one idea: a widget draws
into a buffer, and something else decides when that buffer reaches the
terminal. The split is what makes the framework testable, and it is the reason
the test suite asserts on cells rather than on escape sequences.

 1. The buffer is the contract
    Every widget's Draw receives a *buffer.Buffer and nothing else. It cannot
    read the terminal, cannot sleep, cannot write a byte. A widget that needs
    input from the environment is a widget that cannot be rendered headlessly,
    and this framework refuses to build those.

 2. The renderer owns time
    Frame pacing, the dirty-rect computation and the two-tier diff all live in
    render. A widget never decides when it repaints; it declares what changed
    and the renderer works out the smallest rectangle that covers it.

 3. The diff owns bytes
    A frame is turned into SGR sequences and cursor moves by a two-tier diff:
    a cell-level diff that finds the changed rectangle, and a byte-level diff
    within it. The encoder emits what changed and nothing else.

 4. The headless backend is a real screen model
    headless.MemorySink interprets the emitted bytes back into cells, so a test
    can assert on what a terminal would be showing. It implements exactly the
    subset TermMosaic emits and counts anything else as unknown, because a
    screen reconstructed from an incomplete model makes every assertion against
    it unsound.

 5. Colour degrades at encode time
    A colour is authored as RGB and quantised once, at emit, to whatever the
    terminal reports. A widget therefore never branches on the terminal's
    capabilities, and the same widget code produces truecolor, 256-colour,
    16-colour and NO_COLOR output without a single conditional.

 6. Layout is a solver, not a layout tree
    layout.Solve takes a direction, a list of constraints and an available
    space, and returns sizes. Fill is order-insensitive, which is what lets two
    authors compose two different splits and get the same answer.

 7. Every widget test renders through the renderer
    Calling Draw into a bare buffer proves the widget does not panic. It does
    not prove the encoder agrees with the screen model. widgettest.Render is
    the second thing, and it is the one that catches the bugs that matter.
`
