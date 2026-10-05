package keymap

import (
	"sort"

	"github.com/serkanalgur/termmosaic"
)

// Binding connects a Chord to a CommandID within a Scope.
//
// It is a value, not a heap object, and it is what Commandable returns. The
// name is Binding rather than KeyBinding because the term is unqualified
// anywhere in TermMosaic, and because form.Binding already exists and is a
// different thing (a display pair) — the collision rule of ADR 0007 §1 rule 4
// and ADR 0008 forbids a second exported Binding in the ROOT package, so the
// new vocabulary takes the unused name in this package and the existing one is
// left alone.
type Binding struct {
	// Chord is the gesture. The zero Chord is rejected by Registry.Bind.
	Chord Chord
	// ID is the command it reaches. It need not be registered yet: a binding
	// may precede its command, because a screen's bindings are naturally
	// declared while its fields are being constructed.
	ID CommandID
	// Scope is the binding's context. Zero value is ScopeGlobal.
	Scope Scope
	// Owner is the widget a ScopeFocus or ScopeScreen binding belongs to. It
	// is required when Scope is not ScopeGlobal and must be nil when it is.
	// Identity is pointer identity, so a binding follows the widget instance
	// that registered it — which is what makes a widget that is rebuilt each
	// frame a widget that must re-register, and that hazard is recorded in
	// ADR 0009 §Consequences.
	Owner termmosaic.Widget
	// Desc OVERRIDES the command's own Desc for this binding only. It exists
	// because the same command means different things in different contexts —
	// Enter is "confirm" in a dialog and "open" in a browser — and it is the
	// field that lets one command appear twice in help with two honest
	// descriptions. Empty means "use the command's Desc".
	Desc string
}

// binding is a Binding plus the two facts resolution needs that the exported
// type deliberately does not carry: whether it is a user override, and the
// order it was registered in.
type binding struct {
	Binding
	// override is true when the binding was added after the registry was
	// sealed, which is what makes an explicit Bind outrank a default.
	override bool
	// seq is the registration order, which is the tie-break within one rank.
	seq int
}

// live reports whether b is in effect for the given screen and focus. Scope
// specificity is resolved HERE rather than at Seal time, which is what lets one
// sealed candidate list serve every focus change without being rebuilt.
func (b *binding) live(screen, focus termmosaic.Widget) bool {
	switch b.Scope {
	case ScopeFocus:
		return b.Owner == focus
	case ScopeScreen:
		return b.Owner == screen
	default:
		return true
	}
}

// rank is b's position in ADR 0009 §2.1's four-rank order: lower sorts first.
//
// SPECIFICITY OUTRANKS THE OVERRIDE FLAG, and the ADR is ambiguous about this
// in a way worth recording. Its rank table lists "user override, any scope" as
// rank 1 above the three scope ranks, but the same table's "wins because"
// column says an override "outranks a default *in the same scope*", and two
// other places in the ADR are explicit that the opposite holds: the section
// headed "Why an override does not outrank a more specific scope", and
// §Consequences' bad-list entry "A user override does not beat a more specific
// scope ... This is §2.1's central decision and it will surprise somebody."
//
// Those three agree with each other and with the reasoning — a user who binds
// Esc globally must not lose the ability to close a dialog to any screen that
// binds Esc itself — so the rank column is the outlier and this is what rank
// implements. The override flag breaks ties WITHIN a scope, which is the
// reading the "in the same scope" clause describes and the only one under
// which the override flag and the specificity order are not in conflict.
func (b *binding) rank() int {
	switch b.Scope {
	case ScopeFocus:
		return 0
	case ScopeScreen:
		return 1
	default:
		return 2
	}
}

// Registry maps gestures to named commands. The zero value is not usable;
// call New.
//
// A Registry is not safe for concurrent use. Commands' Run handlers may run on
// whatever goroutine Dispatch runs on, which for an application following the
// ADR 0009 §2.2 loop is the event goroutine; mutations therefore follow ADR
// 0003's "mutate widget state only inside Post" rule, which covers the registry
// too.
type Registry struct {
	// commands is the command table, keyed by ID.
	commands map[CommandID]Command
	// bindings is every binding in registration order. It is the authority:
	// Dispatch, Describe, Chords and Warnings all walk it or the table derived
	// from it, and there is no parallel index that could disagree.
	bindings []*binding
	// seq counts registrations, for the within-rank tie-break.
	seq int
	// sealed records whether Seal has ever been called, which is what
	// distinguishes a program's own bindings from a user's.
	sealed bool
	// table is the sealed resolution order per chord, which is what makes
	// Dispatch allocation-free.
	table map[Chord][]*binding
	// screen is the widget owning the current ScopeScreen bindings.
	screen termmosaic.Widget
	// focus is the focused widget of the most recent Dispatch. Resolution does
	// not read it — Dispatch is handed focus per event — but the
	// discoverability queries do, because "what can I do right now" is a
	// question about the current context and not about any one event.
	focus termmosaic.Widget
	// attached is every widget ever passed to Attach, in attach order.
	attached []termmosaic.Widget
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		commands: make(map[CommandID]Command),
		table:    make(map[Chord][]*binding),
	}
}

// Register adds commands, replacing any with the same ID.
func (r *Registry) Register(cmds ...Command) {
	for _, c := range cmds {
		r.commands[c.ID] = c
	}
}

// Command returns a registered command by ID.
func (r *Registry) Command(id CommandID) (Command, bool) {
	c, ok := r.commands[id]
	return c, ok
}

// Bind adds a binding. A zero Chord is ignored and reported by Warnings.
// Bindings added after Seal take the user-override rank in §2.1.
//
// A binding whose (Chord, Scope, Owner) triple is already present REPLACES it,
// which is what makes an override an override rather than an error. Two
// different bindings for the same chord in the same rank are not an error
// either: they are tried in registration order, so the first one to pass
// Enabled wins and the rest are reachable by a Run that declines.
func (r *Registry) Bind(bs ...Binding) {
	for _, b := range bs {
		if b.Chord.IsZero() {
			continue
		}
		existing := r.find(b.Chord, b.Scope, b.Owner)
		if existing != nil {
			existing.Binding = b
			existing.override = r.sealed
			continue
		}
		r.bindings = append(r.bindings, &binding{Binding: b, override: r.sealed, seq: r.nextSeq()})
	}
	r.seal()
}

// BindString parses s and binds it, reporting a parse failure. This is the
// entry point for a rebinding UI and for a config file, so the parse error
// surfaces where the user typed it rather than as a binding that never fires.
func (r *Registry) BindString(s string, id CommandID, scope Scope, owner termmosaic.Widget) error {
	c, err := ParseChord(s)
	if err != nil {
		return err
	}
	r.Bind(Binding{Chord: c, ID: id, Scope: scope, Owner: owner})
	return nil
}

// Unbind removes every binding for c in scope, including user overrides. A
// chord that was defaulting to something becomes unbound rather than falling
// back to an older default, which is what a user who pressed "unbind this key"
// meant.
func (r *Registry) Unbind(c Chord, scope Scope, owner termmosaic.Widget) {
	kept := r.bindings[:0]
	for _, b := range r.bindings {
		if b.Chord == c && b.Scope == scope && sameWidget(b.Owner, owner) {
			continue
		}
		kept = append(kept, b)
	}
	r.bindings = kept
	r.seal()
}

// SetScreen declares the widget that owns the current ScopeScreen bindings.
//
// It does not re-seal, and deliberately so: a binding's liveness is resolved
// against r.screen at dispatch time rather than baked into the sealed table, so
// pushing and popping a dialog is a field write and not a rebuild.
func (r *Registry) SetScreen(w termmosaic.Widget) { r.screen = w }

// Attach pulls Bindings from every widget in ws that implements Commandable,
// registering each binding with ScopeFocus and Owner set to that widget. It is
// called when the tree is built and after any structural change; it is not
// called per frame.
//
// A widget that does not implement Commandable contributes nothing and is not
// an error. Re-attaching a widget replaces the bindings it published last
// time, so an Attach after a structural change is idempotent.
//
// ws IS THE CURRENT TREE, not a delta. A widget absent from the most recent
// Attach is no longer attached, which is the only way IsAttached and Warnings
// can report the rebuild hazard §5 names: a widget reconstructed each frame has
// a new pointer identity, so its bindings stop matching and the symptom a user
// reports is "my key stopped working". An application that attaches its
// widgets one at a time is attaching a tree of one, and is told so by
// Warnings rather than by silence.
func (r *Registry) Attach(ws ...termmosaic.Widget) {
	// The previous set's focus bindings are KEPT, and that is deliberate: they
	// are what a rebuild orphans, and keeping them is what makes the orphan
	// visible in Warnings. Dropping them silently would hide the bug the
	// warning exists to report. A stale binding is already inert, because its
	// owner is a pointer the focused widget is not.
	r.attached = r.attached[:0]
	for _, w := range ws {
		if w == nil {
			continue
		}
		r.attached = append(r.attached, w)
		// Drop whatever this widget published before: a re-attach is a
		// re-declaration, and keeping the old rows would make a widget appear
		// twice in help.
		kept := r.bindings[:0]
		for _, b := range r.bindings {
			if b.Scope == ScopeFocus && sameWidget(b.Owner, w) {
				continue
			}
			kept = append(kept, b)
		}
		r.bindings = kept

		pub, ok := w.(Commandable)
		if !ok {
			continue
		}
		for _, b := range pub.Bindings() {
			b.Scope = ScopeFocus
			b.Owner = w
			if b.Chord.IsZero() {
				continue
			}
			r.bindings = append(r.bindings, &binding{Binding: b, override: r.sealed, seq: r.nextSeq()})
		}
	}
	r.seal()
}

// IsAttached reports whether w was passed to the most recent Attach and is
// still current, which is how a rebuilt widget's stale bindings are detected.
//
// It is the only mechanism that catches the rebuild hazard before a user
// reports "my key stopped working": a widget reconstructed each frame has a new
// pointer identity, so its old ScopeFocus bindings silently stop matching. A
// nil widget is never attached, so that the zero Widget is not a hit target.
func (r *Registry) IsAttached(w termmosaic.Widget) bool {
	if w == nil {
		return false
	}
	for _, a := range r.attached {
		if sameWidget(a, w) {
			return true
		}
	}
	return false
}

// Seal finalises the resolution tables. It is called by Attach and by Bind
// after the first Seal, and it is what makes Dispatch allocation-free: the
// candidate list per chord is built here and cached.
func (r *Registry) Seal() {
	r.sealed = true
	r.seal()
}

// seal rebuilds the per-chord candidate lists in resolution order.
func (r *Registry) seal() {
	r.table = make(map[Chord][]*binding, len(r.bindings))
	for _, b := range r.bindings {
		r.table[b.Chord] = append(r.table[b.Chord], b)
	}
	for _, cands := range r.table {
		sort.SliceStable(cands, func(i, j int) bool {
			ri, rj := cands[i].rank(), cands[j].rank()
			if ri != rj {
				return ri < rj
			}
			// Within one rank a user override comes before a default — the
			// override outranks a default *in the same scope*, which is the one
			// place the flag applies. Then registration order.
			if cands[i].override != cands[j].override {
				return cands[i].override
			}
			return cands[i].seq < cands[j].seq
		})
	}
}

// Dispatch resolves ev and runs the command it names, reporting whether the
// event was consumed. It is the §2 four-step path, in order, and it allocates
// zero times for every event kind.
//
// A non-key, non-mouse event — Resize, Paste, Focus, Compose — is never
// consumed and never dispatched (§2 step 1). That is ADR 0005 §4's paste rule
// enforced one layer up: re-expanding a paste here would run the resolution
// loop once per pasted character.
func (r *Registry) Dispatch(ev termmosaic.Event, focus termmosaic.Widget) (CommandID, bool) {
	r.focus = focus
	switch ev.Kind {
	case termmosaic.EventKey:
		c, ok := ChordOf(ev)
		if !ok {
			return "", false
		}
		return r.dispatchChord(c, ev, focus)
	case termmosaic.EventMouse:
		return r.dispatchMouse(ev, focus)
	default:
		return "", false
	}
}

// dispatchChord is step 3 and step 4 for a key event. The candidate list is
// the sealed slice for this chord, so the walk allocates nothing.
func (r *Registry) dispatchChord(c Chord, ev termmosaic.Event, focus termmosaic.Widget) (CommandID, bool) {
	if !r.sealed {
		// A registry used without an explicit Seal still works; it just pays
		// the rebuild once, here, rather than per keystroke.
		r.seal()
	}
	for _, b := range r.table[c] {
		if !b.live(r.screen, focus) {
			continue
		}
		cmd, ok := r.commands[b.ID]
		if !ok || cmd.Run == nil {
			continue
		}
		if cmd.Enabled != nil && !cmd.Enabled() {
			continue
		}
		if !cmd.Run(Ctx{Event: ev, Chord: c, Focus: focus}) {
			// A declined Run is the only fallthrough mechanism in TermMosaic:
			// no flag, no preventDefault, just a Go return value. The event
			// stays unconsumed, so if no candidate accepts it the event still
			// reaches the tree.
			continue
		}
		return b.ID, true
	}
	return "", false
}

// dispatchMouse is step 2 and step 4 for a mouse event: the owning widget says
// which command the point means, because the registry holds no rectangles. Only
// a press resolves; a release, a drag and a bare motion belong to Handle,
// which is where a gesture already works (ADR 0009 §6).
func (r *Registry) dispatchMouse(ev termmosaic.Event, focus termmosaic.Widget) (CommandID, bool) {
	if ev.Mouse.Action != termmosaic.MousePress {
		return "", false
	}
	// Reverse attach order: the most recently attached widget is the one on
	// top, and a dialog is attached after the tree it covers.
	for i := len(r.attached) - 1; i >= 0; i-- {
		w := r.attached[i]
		if !w.Bounds().Contains(ev.Mouse.X, ev.Mouse.Y) {
			continue
		}
		cl, ok := w.(Clickable)
		if !ok {
			continue
		}
		id, ok := cl.Command(ev.Mouse.X, ev.Mouse.Y)
		if !ok {
			// A click on a widget's padding is a click on the widget and not a
			// command. It falls through to Handle.
			continue
		}
		cmd, found := r.commands[id]
		if !found || cmd.Run == nil {
			continue
		}
		if cmd.Enabled != nil && !cmd.Enabled() {
			continue
		}
		if !cmd.Run(Ctx{Event: ev, Focus: focus}) {
			continue
		}
		return id, true
	}
	return "", false
}

// Invoke runs a command directly by name, bypassing Enabled and bypassing
// Dispatch. It is what a palette row, a menu item and application code call,
// and it reports whether the command was found — not whether it succeeded,
// because a declined Run is expressed by Enabled.
//
// Synthesised is always true, because an Invoke is by definition not a key
// press reaching the tree. The Event is passed through so a palette row can
// hand its own selection event to the handler.
func (r *Registry) Invoke(id CommandID, ev termmosaic.Event, focus termmosaic.Widget) bool {
	cmd, ok := r.commands[id]
	if !ok || cmd.Run == nil {
		return false
	}
	cmd.Run(Ctx{Event: ev, Focus: focus, Synthesised: true})
	return true
}

// Has reports whether id is registered and currently available. A one-line
// predicate for the "enable this button" question, which is asked on every frame
// of a toolbar.
func (r *Registry) Has(id CommandID) bool {
	cmd, ok := r.commands[id]
	if !ok {
		return false
	}
	return cmd.Enabled == nil || cmd.Enabled()
}

// find returns the binding for a (Chord, Scope, Owner) triple, or nil.
func (r *Registry) find(c Chord, scope Scope, owner termmosaic.Widget) *binding {
	for _, b := range r.bindings {
		if b.Chord == c && b.Scope == scope && sameWidget(b.Owner, owner) {
			return b
		}
	}
	return nil
}

// nextSeq returns the next registration order number.
func (r *Registry) nextSeq() int {
	r.seq++
	return r.seq
}

// sameWidget compares two widgets by identity, treating two nil interfaces as
// equal. Go's == on interfaces panics only for incomparable dynamic types,
// which a Widget cannot be (it always has methods, so it is a pointer or a
// comparable struct); the explicit nil check covers the two shapes a nil widget
// arrives in.
func sameWidget(a, b termmosaic.Widget) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b
}
