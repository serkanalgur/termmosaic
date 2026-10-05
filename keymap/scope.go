package keymap

// Scope says how specific a binding's context is. The three values are a total
// order and they are the only ones; there is deliberately no numeric priority
// to get wrong.
//
// Context specificity is NOT configurable. An application that needs
// something between Screen and Focus declares a Screen binding on a narrower
// screen, which is the mechanism the value already has.
type Scope uint8

const (
	// ScopeGlobal is live everywhere, in every focus context and on every
	// screen. This is where app.quit, app.help and app.palette live.
	ScopeGlobal Scope = iota

	// ScopeScreen is live only while the screen that declared it is the
	// current one. Registry.SetScreen declares which widget is current, so a
	// dialog pushes a screen and its bindings are live for exactly as long as
	// it is up.
	ScopeScreen

	// ScopeFocus is live only while the widget that declared it holds
	// keyboard focus. This is where a list's navigation keys belong IF the
	// list chooses to declare them (§7's Commandable interface).
	ScopeFocus
)

// String returns the scope's name, for help output and diagnostics.
func (s Scope) String() string {
	switch s {
	case ScopeGlobal:
		return "global"
	case ScopeScreen:
		return "screen"
	case ScopeFocus:
		return "focus"
	default:
		return "unknown"
	}
}
