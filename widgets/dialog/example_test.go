package dialog_test

import (
	"fmt"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/dialog"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// show renders root on a w-by-h screen and returns the rows with trailing
// spaces trimmed.
//
// Every Output comment below is the CELL GRID the renderer produced, read back
// out of a headless.MemorySink rather than described. widgettest walks the whole
// widget -> buffer -> renderer -> diff -> encoder -> MemorySink path, so what a
// reader sees is what the dialog's own golden tests assert on.
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

// press offers an event to a focused dialog, which is how an example drives one
// with the same calls an event loop would make.
func press(d *dialog.Dialog, k termmosaic.Key, mod termmosaic.KeyMod) bool {
	return d.Handle(termmosaic.SpecialKeyEvent(k, mod))
}

// Example is a confirm dialog, which is the shape most dialogs in a program are:
// a message, two buttons, and the affirmative one focused.
//
// Variant is a PRESET rather than a mode. It chooses the default actions and the
// default focus, and SetActions and SetChoices replace them — so a caller who
// wants three buttons adds one and the dialog becomes a choice with a body,
// without the widget needing to know that is possible.
func Example() {
	d := dialog.New(buffer.Rect{W: 44, H: 7}, dialog.VariantConfirm)
	d.SetTitle("Delete 3 files?", buffer.Style{})
	d.SetBodyString("This cannot be undone.")
	d.SetFocused(true)

	// The focus ring is the CHOICES first and then the ACTIONS, and it is
	// one-dimensional: Tab, Shift-Tab and all four arrows traverse it identically
	// in every variant. A dialog with three choices and a Cancel button has no
	// second dimension to move in.
	fmt.Printf("ring %d items: choice %d, action %d, cancel is action %d\n",
		d.FocusCount(), d.FocusedChoice(), d.FocusedAction(), d.CancelAction())
	fmt.Println(show(44, 7, d))
	// Output:
	// ring 2 items: choice -1, action 1, cancel is action 0
	// ╭ Delete 3 files? ─────────────────────────╮
	// │                                          │
	// │This cannot be undone.                    │
	// │                                          │
	// │                                          │
	// │ Cancel   [OK]                            │
	// ╰──────────────────────────────────────────╯
}

// ExampleDialog_choice is the list variant, and it shows the two things that make
// a choice list work: the focused choice is MARKED as well as focused, and a
// choice longer than the dialog is scrolled rather than truncated away.
//
// A dialog built with VariantChoice starts on the FIRST CHOICE rather than on
// the first action, because in this variant the choice is the question.
func ExampleDialog_choice() {
	d := dialog.New(buffer.Rect{W: 44, H: 9}, dialog.VariantChoice)
	d.SetTitle("Open recent", buffer.Style{})
	d.SetChoices([]string{
		"termmosaic/widgets/data/table.go",
		"termmosaic/widgets/viz/sparkline.go",
		"termmosaic/widgets/form/textinput.go",
		"termmosaic/widgets/menu/menu.go",
	})
	d.SetBodyString("Pick a file to open.")
	d.SetFocused(true)
	d.SetFocus(2)

	fmt.Printf("%d choices, focused choice %d\n", d.ChoiceCount(), d.FocusedChoice())
	fmt.Println(show(44, 9, d))
	// Output:
	// 4 choices, focused choice 2
	// ╭ Open recent ─────────────────────────────╮
	// │Pick a file to open.                      │
	// │  termmosaic/widgets/data/table.go        │
	// │  termmosaic/widgets/viz/sparkline.go     │
	// │> termmosaic/widgets/form/textinput.go    │
	// │  termmosaic/widgets/menu/menu.go         │
	// │                                          │
	// │ Cancel                                   │
	// ╰──────────────────────────────────────────╯
}

// ExampleDialog_actions is what the dialog does NOT decide: the buttons are
// labels and indices, and what they mean is the caller's OnAction.
//
// SetActions re-clamps the focus, the cancel index and the scroll offset into the
// new set, which is why replacing the actions of a live dialog cannot leave the
// focus pointing past the end of the row.
func ExampleDialog_actions() {
	d := dialog.New(buffer.Rect{W: 40, H: 7}, dialog.VariantConfirm)
	d.SetTitle("Save changes?", buffer.Style{})
	d.SetBodyString("3 files have unsaved edits.")
	d.SetActions(
		dialog.Action{Label: "Discard"},
		dialog.Action{Label: "Cancel"},
		dialog.Action{Label: "Save all"},
	)
	// Escape activates the cancel action, which is what makes the safe answer the
	// easy one to reach. SetCancelAction names it rather than assuming index 1.
	d.SetCancelAction(1)
	d.OnAction = func(i int) { fmt.Printf("OnAction(%d) = %q\n", i, d.Actions()[i].Label) }
	d.SetFocused(true)

	// The ring is the actions here, since there are no choices. Home lands on the
	// first and Enter presses it.
	press(d, termmosaic.KeyHome, 0)
	press(d, termmosaic.KeyEnter, 0)
	fmt.Printf("dismissed: %v\n", d.Dismissed())
	fmt.Println(show(40, 7, d))
	// Output:
	// OnAction(0) = "Discard"
	// dismissed: true
	// ╭ Save changes? ───────────────────────╮
	// │                                      │
	// │3 files have unsaved edits.           │
	// │                                      │
	// │                                      │
	// │[Discard]   Cancel    Save all        │
	// ╰──────────────────────────────────────╯
}

// ExampleDialog_dismissal is how a dialog ENDS, which is the one piece of a modal
// a still frame cannot show. Dismissed is set by an activation or by an
// application calling Dismiss, and it is set WHETHER OR NOT a callback is
// installed, so a program can poll it.
//
// Reset is the other half: it clears the flag and returns the focus to the ring's
// first item, which is what a program reusing one dialog for the next prompt needs.
func ExampleDialog_dismissal() {
	d := dialog.New(buffer.Rect{W: 38, H: 6}, dialog.VariantInfo)
	d.SetTitle("Up to date", buffer.Style{})
	d.SetBodyString("v0.7.0 is the current release.")
	d.SetFocused(true)

	fmt.Printf("fresh: dismissed=%v, focus=%d\n", d.Dismissed(), d.Focus())

	// An application closing a dialog from outside the keyboard — a timeout, a
	// websocket message, a second process answering.
	d.Dismiss()
	fmt.Printf("after Dismiss: dismissed=%v\n", d.Dismissed())

	d.Reset()
	fmt.Printf("after Reset: dismissed=%v, focus=%d of %d\n",
		d.Dismissed(), d.Focus(), d.FocusCount())
	fmt.Println(show(38, 6, d))
	// Output:
	// fresh: dismissed=false, focus=0
	// after Dismiss: dismissed=true
	// after Reset: dismissed=false, focus=0 of 1
	// ╭ Up to date ────────────────────────╮
	// │                                    │
	// │v0.7.0 is the current release.      │
	// │                                    │
	// │[Dismiss]                           │
	// ╰────────────────────────────────────╯
}

// ExampleDialog_minSize is the shape a dialog asks for when it has to fit
// something, and it is the WHOLE widget including the border and the padding the
// block consumed — not an interior size.
//
// A Dialog is Minimizable, so the framework's responsive budget can ask this
// question of it rather than a program hard-coding a minimum.
func ExampleDialog_minSize() {
	for _, v := range []dialog.Variant{dialog.VariantInfo, dialog.VariantConfirm, dialog.VariantChoice} {
		d := dialog.New(buffer.Rect{}, v)
		fmt.Printf("%-8s %dx%d, %d actions, %d ring items\n",
			v.String(), d.MinSize().W, d.MinSize().H, len(d.Actions()), d.FocusCount())
	}
	// Output:
	// info     11x3, 1 actions, 1 ring items
	// confirm  16x3, 2 actions, 2 ring items
	// choice   10x3, 1 actions, 1 ring items
}
