package term

import (
	"strings"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// DetectCaps infers terminal capabilities from environment variables.
//
// This is the whole of TermMosaic's capability detection, and it is a heuristic
// rather than a probe. ADR 0001 commits to "no terminfo" and describes the
// trade plainly: a fixed, well-chosen subset plus sniffing is right for the
// overwhelming majority of terminals and wrong for a few exotic ones, and that
// is recorded as an accepted limitation rather than a gap to close.
//
// The heuristics are the ones every terminal library converged on, because they
// are the only signals a program can get without asking the terminal:
//
//   - COLORTERM=truecolor or 24bit means SGR 38;2 is understood.
//   - TERM containing "256color" means the 256-colour palette is present.
//   - Anything else falls back to the 16 named colours, which every terminal
//     since DOS has.
//   - NO_COLOR, per no-color.org, disables colour output entirely.
//
// NO_COLOR is honoured as a capability rather than only as an encoder flag so
// that a caller inspecting Caps learns the truth without consulting the
// environment separately.
//
// It is a pure function of its argument, so it is fully testable.
func DetectCaps(getenv func(string) string) termmosaic.Caps {
	term := getenv("TERM")
	colorTerm := getenv("COLORTERM")
	lang := getenv("LC_ALL")
	if lang == "" {
		lang = getenv("LC_CTYPE")
	}
	if lang == "" {
		lang = getenv("LANG")
	}

	caps := termmosaic.Caps{}

	switch {
	case colorTerm == "truecolor" || colorTerm == "24bit":
		caps.TrueColor = true
		caps.Color256 = true
	case strings.Contains(term, "256color"):
		caps.Color256 = true
	case strings.Contains(term, "truecolor") || strings.Contains(term, "direct"):
		caps.TrueColor = true
		caps.Color256 = true
	}

	// A UTF-8 locale is the practical signal for Unicode box drawing. It is
	// wrong for a terminal configured out of band and for Windows consoles,
	// which is part of why Windows is a stub here.
	caps.Unicode = strings.Contains(strings.ToUpper(lang), "UTF-8") ||
		strings.Contains(strings.ToUpper(lang), "UTF8")

	// Mouse, kitty keyboard and bracketed paste are not detectable from the
	// environment, and every terminal has historically supported them anyway.
	// They are therefore optimistically enabled: the cost of guessing wrong is
	// that a mouse click or a paste is not delivered, which is better than a
	// user having to opt in to features that work.
	caps.Mouse = true
	caps.BracketedPaste = true
	// The kitty keyboard protocol is NOT assumed: a terminal that does not
	// implement it will treat the enable sequence as garbage.
	caps.KittyKeyboard = strings.Contains(term, "kitty")

	// NO_COLOR is a colour decision, not a capability. It is applied by the
	// renderer's NoColor option, not here, because it is about the user's
	// preference rather than the device's ability. DetectCaps leaves it alone
	// and render.NoColorFromEnv reads it separately.
	return caps
}

// DepthOf returns the colour rung the capabilities select.
func DepthOf(caps termmosaic.Caps) buffer.ColourDepth { return caps.ColourDepth() }
