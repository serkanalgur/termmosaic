package keymap

import (
	"sort"
	"strconv"

	"github.com/serkanalgur/termmosaic"
)

// Entry is one discoverable row: a command, its description, and the chords
// that currently reach it.
//
// It is the ONLY type the help screen, the command palette and KeyHint consume,
// and it is produced from the registry, which is what makes it impossible for
// help to drift from the bindings.
//
// Entry allocates. It is built on demand, never inside Draw, and never on the
// dispatch path.
type Entry struct {
	// ID is the command.
	ID CommandID
	// Desc is the description to show — the binding's override if it has one,
	// otherwise the command's.
	Desc string
	// Group is the display group.
	Group string
	// Chords are the chords that reach this command in the current context, in
	// canonical order (modifiers ascending, then Key ascending, then rune).
	//
	// A command with several bindings produces several Rows rather than one row
	// with a chord list: a help screen that shows one row per chord is easier
	// to scan and is what every terminal help screen does. The name Rows is
	// deliberately not used for the type because Entry is the thing and a row is
	// a view of it.
	//
	// So from Registry.Describe a command with three chords arrives as three
	// Entries each holding one chord. From Registry.DescribeGrouped it arrives as
	// ONE Entry holding all three. Which one a consumer wants is a property of
	// what it does with the row, and both functions document which they are for.
	Chords []Chord
}

// describeRow is one row before it becomes an Entry: a command, the description
// that row shows, and the chord that reaches it — or no chord, for a command
// nothing is bound to.
//
// The internal row exists because there are two views of the same rows, one per
// chord and one per command, and both must see the same ordering. See
// describeRows.
type describeRow struct {
	id    CommandID
	desc  string
	group string
	chord Chord
	// unbound marks the row for a command with no reachable chord, which
	// must still be describable: a palette-only or mouse-only command is
	// invisible in a key-only help screen otherwise.
	unbound bool
}

// Describe returns the discoverable entries for scope, ONE PER CHORD, sorted by
// Group then ID. Use it for a help screen and a command palette, which each
// consume a row's Chords[0] as that row's own key — see ADR 0009 §9. For a
// one-line hint, use DescribeGrouped, which is one row per command.
//
// It allocates, it is not cheap, and it MUST NOT be called from Dispatch or
// from a widget's Draw. A help screen that is open calls it when the registry
// changes; a palette calls it when it opens; a KeyHint calls it when its
// context changes.
//
// scope names the CONTEXT the caller is asking about, and it widens: asking for
// ScopeFocus reports the commands reachable with this registry's screen and
// focus in effect, which is what a KeyHint bound to the current context wants.
// Asking for ScopeGlobal reports the application's own keys, which is what a
// global palette wants. A command reachable in more than one scope appears once
// per scope, because a dialog's Enter and a browser's Enter are different
// answers to "what does Enter do right now".
func (r *Registry) Describe(scope Scope) []Entry {
	rows := r.describeRows(scope)

	out := make([]Entry, 0, len(rows))
	for _, rw := range rows {
		e := Entry{ID: rw.id, Desc: rw.desc, Group: rw.group}
		if !rw.unbound {
			e.Chords = []Chord{rw.chord}
		}
		out = append(out, e)
	}
	return out
}

// DescribeGrouped returns the discoverable entries for scope, ONE PER COMMAND,
// in the same order Describe uses: sorted by Group then ID, with a command's
// chords in canonical order inside its single entry. Use it for a one-line hint,
// where one row per command is what a hint line wants; for a help screen and a
// palette, use Describe, which is one row per chord.
//
// The difference is not cosmetic. form.KeyHint.SetEntries joins an entry's
// chords into ONE label, so handing it Describe's output renders a command with
// three chords as the same description three times — which is not a hint, it is
// three rows of noise in a line that has room for one. DescribeGrouped gives the
// hint one entry carrying all three chords, which chordsLabel renders as the one
// label it was written for. ADR 0009 §9 specifies Describe's per-chord shape for
// the palette, so Describe is unchanged; a two-function API is the price of two
// genuinely different consumers.
//
// Two rows of it are specified rather than incidental:
//
//   - Chords is never empty. A command with no binding in scope is ABSENT, not
//     present with no chords. Describe includes such a command — a palette-only
//     or mouse-only command — as a chordless row, because a help screen that
//     listed only keys would hide a command the user can still reach. A hint
//     line cannot use that row: its key column would be empty, so the row
//     renders as a bare description with nothing to press, and a hint line whose
//     entries are mostly bare descriptions has stopped being a hint. The command
//     is still describable — that is Describe's job, and the palette that can
//     reach it is built from Describe.
//   - Entry order matches Describe's command order exactly, so a hint's rows are
//     stable across runs and diffable across versions. Describe's doc promises
//     that ordering and it is load-bearing; two queries that disagreed about it
//     would make help and hint order differ for the same registry.
//
// When several of a command's bindings override its Desc and the overrides
// disagree, the entry takes the override of the FIRST chord in canonical order.
// Describe is where each override is visible: it gives the command one row per
// chord, so a screen with room to show "open the item" and "confirm the dialog"
// separately should be reading Describe.
//
// Like Describe it allocates, it is not cheap, and it MUST NOT be called from
// Dispatch or from a widget's Draw. It shares Describe's meaning of scope: the
// query WIDENS, so a focus-level query includes the screen's and the
// application's keys.
func (r *Registry) DescribeGrouped(scope Scope) []Entry {
	// Merge Describe's own rows rather than re-deriving them. The rows are
	// already sorted, and a command's rows are already adjacent, so appending
	// consecutive chords to the entry already open produces exactly the order
	// Describe used — and produces it by construction rather than by a second
	// sort that would have to be kept in step with the first.
	rows := r.describeRows(scope)

	var out []Entry
	for _, rw := range rows {
		if rw.unbound {
			// Dropped on purpose: an entry with no chords is not a hint row.
			continue
		}
		if n := len(out); n > 0 && out[n-1].ID == rw.id {
			out[n-1].Chords = append(out[n-1].Chords, rw.chord)
			continue
		}
		out = append(out, Entry{
			ID:     rw.id,
			Desc:   rw.desc,
			Group:  rw.group,
			Chords: []Chord{rw.chord},
		})
	}
	return out
}

// describeRows collects the rows both queries are views of: one per binding in
// scope, plus one per command nothing is bound to anywhere. It is shared so that
// the per-chord and per-command answers cannot disagree about WHICH bindings are
// in scope, or about the order, or about which description a binding overrides.
func (r *Registry) describeRows(scope Scope) []describeRow {
	// Collect into a per-(command, scope) bucket first, so one command with
	// three chords is three Rows with the right description on each and not
	// three copies of the command's own Desc.
	var rows []describeRow
	seen := make(map[string]bool)
	for _, b := range r.bindings {
		if !r.inScope(b, scope) {
			continue
		}
		cmd, ok := r.commands[b.ID]
		if !ok {
			continue
		}
		desc := cmd.Desc
		if b.Desc != "" {
			desc = b.Desc
		}
		key := string(b.ID) + "\x00" + strconv.Itoa(int(b.Scope)) + "\x00" + b.Chord.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, describeRow{id: b.ID, desc: desc, group: cmd.Group, chord: b.Chord})
	}
	for id, cmd := range r.commands {
		// A chordless row is for a command NO key reaches anywhere: a
		// palette-only or mouse-only command, which a key-only help screen
		// would otherwise hide entirely. A command bound in THIS scope is
		// already a row per chord above, and a command bound in a NARROWER one
		// belongs to that scope's help, not to this one.
		if r.boundAnywhere(id) {
			continue
		}
		rows = append(rows, describeRow{id: id, desc: cmd.Desc, group: cmd.Group, unbound: true})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].group != rows[j].group {
			// The uncategorised bucket renders last, under an empty heading.
			if rows[i].group == "" {
				return false
			}
			if rows[j].group == "" {
				return true
			}
			return rows[i].group < rows[j].group
		}
		if rows[i].id != rows[j].id {
			return rows[i].id < rows[j].id
		}
		if rows[i].unbound != rows[j].unbound {
			// The chordless row sorts after the chords, so a help screen reads
			// "Enter: open" before "open (no key)".
			return rows[i].unbound
		}
		return lessChord(rows[i].chord, rows[j].chord)
	})
	return rows
}

// Chords returns the chords currently bound to id in scope, in canonical
// order. The cheap single-command query, for a KeyHint that shows one row.
//
// It is NOT allocation-free: it returns a fresh slice, because the caller owns
// it and may sort or filter it. It is on no hot path — Describe is what a help
// screen calls, and this is the one-command variant of it.
func (r *Registry) Chords(id CommandID, scope Scope) []Chord {
	var out []Chord
	for _, b := range r.bindings {
		if b.ID == id && r.inScope(b, scope) {
			out = append(out, b.Chord)
		}
	}
	sort.Slice(out, func(i, j int) bool { return lessChord(out[i], out[j]) })
	return out
}

// Warnings returns the registry's diagnostics: a binding naming an
// unregistered command, a command with an empty Desc, a scoped binding with a
// nil Owner or a global binding with a non-nil one, a chord bound twice in one
// scope, and a binding whose owner is no longer attached. Diagnostic surface,
// never control flow — the stance input.Stats takes.
//
// The order is stable because the diagnostics are ordered: a reader comparing
// two runs' warning output should see a diff, not a shuffle.
func (r *Registry) Warnings() []string {
	var out []string
	registered := func(id CommandID) bool {
		_, ok := r.commands[id]
		return ok
	}
	// live is "some binding for id is live in the current context", which is the
	// condition under which an empty Desc is a discoverability defect rather
	// than a deliberate "do not show this".
	live := make(map[CommandID]bool)
	for _, b := range r.bindings {
		if b.live(r.screen, r.currentFocus()) {
			live[b.ID] = true
		}
	}
	for id, cmd := range r.commands {
		if cmd.Desc == "" && live[id] {
			out = append(out, "command "+string(id)+" has no description")
		}
	}
	for _, b := range r.bindings {
		switch {
		case b.Chord.IsZero():
			out = append(out, "a binding for "+string(b.ID)+" has a zero chord")
		case !registered(b.ID):
			out = append(out, "chord "+b.Chord.String()+" is bound to unregistered command "+string(b.ID))
		case b.Scope == ScopeGlobal && b.Owner != nil:
			out = append(out, "global binding "+b.Chord.String()+" on "+string(b.ID)+" has a non-nil owner")
		case b.Scope != ScopeGlobal && b.Owner == nil:
			out = append(out, b.Scope.String()+" binding "+b.Chord.String()+" on "+string(b.ID)+" has no owner")
		case b.Owner != nil && !r.IsAttached(b.Owner):
			out = append(out, b.Scope.String()+" binding "+b.Chord.String()+" on "+string(b.ID)+" has an owner that is no longer attached")
		}
	}
	// A chord bound to two DIFFERENT commands in one scope is a shadowed
	// binding: the second is only reachable if the first declines, and only one
	// of them can ever be what the user meant. Two bindings to the SAME command
	// from two widgets are not a defect — that is a list and a pager agreeing on
	// what Up does — and two bindings with the same (chord, scope, owner) are
	// impossible, because Bind replaces rather than duplicating.
	type shadow struct {
		chord Chord
		scope Scope
		id    CommandID
	}
	shadows := make(map[shadow]bool)
	for _, b := range r.bindings {
		if b.Chord.IsZero() {
			continue
		}
		shadows[shadow{b.Chord, b.Scope, b.ID}] = true
	}
	reported := make(map[[2]any]bool)
	for _, b := range r.bindings {
		if b.Chord.IsZero() {
			continue
		}
		if !shadows[shadow{b.Chord, b.Scope, b.ID}] {
			continue
		}
		for _, other := range r.bindings {
			if other.Chord == b.Chord && other.Scope == b.Scope && other.ID != b.ID {
				key := [2]any{b.Chord, b.Scope}
				if reported[key] {
					continue
				}
				reported[key] = true
				out = append(out, "chord "+b.Chord.String()+" is bound to both "+
					string(b.ID)+" and "+string(other.ID)+" in "+b.Scope.String()+" scope; only one of them can run")
			}
		}
	}
	sort.Strings(out)
	return out
}

// inScope reports whether b is part of what a caller asking about scope wants to
// see. Asking about a scope reports that scope and every less specific one, so
// a focus-level query includes the screen's and the application's keys.
func (r *Registry) inScope(b *binding, scope Scope) bool {
	if b.Scope == scope {
		return true
	}
	if !b.live(r.screen, r.currentFocus()) {
		return false
	}
	// A live global binding is part of every context's answer.
	return b.Scope == ScopeGlobal || int(b.Scope) < int(scope)
}

// boundAnywhere reports whether any binding names id, in any scope. It is what
// decides whether a command needs a chordless row: a command with no chord at
// all is a palette-only or mouse-only command and must still be describable,
// while a command bound in a narrower scope belongs to that scope's help.
func (r *Registry) boundAnywhere(id CommandID) bool {
	for _, b := range r.bindings {
		if b.ID == id {
			return true
		}
	}
	return false
}

// currentFocus is the focused widget the registry knows about.
//
// The Registry is not told about focus — Dispatch is handed it per event — so
// this is the focus of the most recent Dispatch, which is what every
// discoverability query is asking about anyway. A zero value means nothing has
// been dispatched yet, and then only global bindings are live.
func (r *Registry) currentFocus() termmosaic.Widget { return r.focus }

// lessChord is the canonical chord order: modifiers ascending, then Key
// ascending, then rune. It is the order §4 promises for Entry.Chords.
func lessChord(a, b Chord) bool {
	if a.Mod != b.Mod {
		return a.Mod < b.Mod
	}
	if a.Key != b.Key {
		return a.Key < b.Key
	}
	return a.Rune < b.Rune
}
