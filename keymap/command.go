// Package keymap is TermMosaic's command layer: a named action an application
// can invoke, and a mapping from a decoded Event to it.
//
// It is the gap immediately above ADR 0005's input package. Decoding is done
// and nothing sat above it, so every widget hand-rolled its own switch on
// ev.Key and there was no way to ask a program what keys it answers to.
//
// The shape is deliberate and is argued in ADR 0009:
//
//   - A command is a NAME plus a handler. A key is one way to reach it. Keeping
//     the two separate is what lets a palette list "save" once and show three
//     keys beside it.
//   - A Chord is the ONE normalisation of a decoded Event into a gesture, so
//     "KeySpace and Rune ' ' are the same thing" is stated once rather than
//     re-derived by each widget that has to care.
//   - Resolution is by CONTEXT SPECIFICITY — focus, then screen, then global —
//     not by a numeric priority an application can set wrong.
//   - The registry itself is the only source of discoverability data, so help
//     cannot document a key that was renamed.
//
// termmosaic.Widget is unchanged and sits BELOW this package: a key the keymap
// does not consume is still offered to the focused widget and then to the tree.
// The keymap wins a key it consumes; that is the whole of the precedence rule,
// and it is why an application may have a key answered in two places.
package keymap

import "github.com/serkanalgur/termmosaic"

// CommandID is a command's stable, globally unique name, conventionally
// dotted and lower-case: "file.save", "view.toggle-help", "list.confirm".
//
// A string and not a distinct type because it crosses into help text, into
// config files the application owns, and into log lines. Two packages that
// both spell a command name must spell it the same way; a defined type would
// prevent the copy, not the typo.
type CommandID string

// Command is a named action an application can invoke from a key, a click, a
// palette row, or its own code.
//
// The registry holds commands and bindings SEPARATELY, because a command may
// have several chords and a chord may have no command. This is the same
// separation Bubble Tea and OpenTUI use and it is the reason a palette can list
// "save" once and show three keys beside it.
type Command struct {
	// ID is the command's name. Required and unique within a Registry; a
	// second Command with an existing ID replaces the first.
	ID CommandID

	// Desc is the one-line description shown in help, in the palette and in a
	// KeyHint. It is REQUIRED for any command a user can reach — a command
	// with an empty Desc is a warning from the Registry, not a silent blank
	// row.
	Desc string

	// Group orders commands in help and groups palette rows. Empty means the
	// uncategorised bucket, which help renders last under an empty heading.
	// Groups are sorted alphabetically; within a group, commands are sorted by
	// ID so that help output is stable across runs and diffable across
	// versions.
	Group string

	// Run is the handler. Returning false means "I did not handle this after
	// all", and Dispatch continues to the next candidate binding. That is the
	// ONLY fallthrough mechanism in TermMosaic — there is no fallthrough flag
	// and no preventDefault flag, because a Go return value is the same
	// mechanism wearing a hat.
	//
	// Run must not mutate widget state from another goroutine. ADR 0003's rule
	// applies without exception: mutation happens inside Renderer.Post, or on
	// the event goroutine where the application's own state already lives.
	Run func(Ctx) bool

	// Enabled reports whether the command is currently available. It gates
	// palette rows, help rows and Dispatch: an unavailable command is not run
	// by a key press. Nil means always available, and is the common case.
	//
	// It is a plain predicate, not a condition expression language, and it MUST
	// NOT be called on the dispatch hot path more than once per candidate
	// binding. See ADR 0009 §2's allocation note.
	Enabled func() bool
}

// Ctx is what a command handler is given.
//
// It is passed BY VALUE. On a path that runs at most a few hundred times per
// second, against ADR 0002's 16 ms frame budget, that is the same trade ADR
// 0005 §8 made for the 112-byte Event: the zero-allocation bar is about not
// allocating, not about struct copies. Passing *Ctx would put an escaping
// pointer on the path and turn a stack copy into a heap object per keystroke.
//
// MEASURED SIZE. ADR 0009's prose calls this 128 bytes, and that number does
// not survive arithmetic: Event alone is 112, Chord is 16, a Widget interface
// is 16 and Synthesised is 1, so the field list the ADR specifies lays out at
// 152 bytes on amd64 and arm64. The field list is the specification and the
// prose figure is the error; TestCtxIsOneHundredFiftyTwoBytes pins what the
// compiler actually does rather than what the document wished for.
type Ctx struct {
	// Event is the event that produced this dispatch, by value. A command
	// invoked from the palette or from code gets the zero Event, and
	// Synthesised is true.
	Event termmosaic.Event

	// Chord is the key chord that produced this dispatch, normalised as ADR
	// 0009 §3 specifies. It is the zero Chord when the command was invoked from
	// a click, from the palette, or from code — which is why a handler that
	// needs "which key was this" must check Chord.IsZero() first.
	Chord Chord

	// Focus is the widget that had keyboard focus when the command was
	// dispatched, and is nil when nothing is focused. A handler that needs to
	// act on the focused widget type-asserts it; it does not receive a
	// command's "target widget", because there is no such thing.
	Focus termmosaic.Widget

	// Synthesised is true when the command was invoked without a keyboard
	// Event — from the palette, from the command line, or by Invoke from
	// application code. A handler that behaves differently for a mouse
	// activation than for a keyboard activation reads this; a handler that does
	// not care ignores it, and the overwhelmingly common case is to ignore it.
	Synthesised bool
}
