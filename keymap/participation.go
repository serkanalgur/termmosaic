package keymap

// Commandable is the OPTIONAL interface a Widget implements to publish its own
// key bindings to the command registry — so they appear in help, in the
// command palette, and in a rebinding UI, and so an application's global
// binding can be overridden consistently with a widget's.
//
// It publishes; it does not dispatch. Widgets keep receiving unconsumed keys
// through Handle, and Dispatch never calls Commandable. The two mechanisms are
// independent by design (ADR 0009 §2.3).
//
// A widget that does not implement it is fully supported and is the v0.2 norm
// for the whole catalog. Its keys work; they are simply not discoverable
// through the registry, and KeyHint remains the way it advertises them.
//
// It lives HERE rather than beside termmosaic.Focusable, and that is forced
// rather than chosen: its method returns []Binding, so declaring it in package
// termmosaic would make the root package import keymap, which imports it back.
// The method set is exactly the one ADR 0009 §7 specifies, and the
// discoverable-by-type-assertion shape is unchanged.
type Commandable interface {
	// Bindings returns the widget's bindings. Scope is ScopeFocus and Owner is
	// the widget itself; the Registry fills both in, so an implementation
	// returns only Chord, ID and an optional Desc override.
	//
	// It is called on Attach and on every Registry.Seal, never per keystroke,
	// and it is allowed to allocate — it is not on the dispatch path.
	Bindings() []Binding
}

// Clickable is the OPTIONAL interface a Widget implements when a click inside
// its bounds can mean a command — a Button, a Table cell, a Menu row, a
// dismissible backdrop.
//
// It is one method returning a name, not a handler, because the command
// registry is the only place handlers live. A widget that implements it never
// runs application logic itself; it says which command the user meant.
//
// A widget that does not implement it is fully supported. Every widget in the
// catalog does not implement it.
//
// Like Commandable it is declared here for the same import-direction reason,
// and with the same method set as ADR 0009 §6 specifies.
type Clickable interface {
	// Command reports the command a click at (x, y) — in SCREEN coordinates,
	// the same coordinates EventMouse carries — means, or ("", false) if the
	// click is not a command activation.
	//
	// false is a normal answer, not an error: a click on a widget's padding is
	// a click on the widget and not a command. It means Dispatch continues,
	// which is how a click falls through to Handle for widgets that have both
	// interfaces.
	Command(x, y int) (CommandID, bool)
}
