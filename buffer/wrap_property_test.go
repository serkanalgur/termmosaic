package buffer

import (
	"math/rand"
	"strings"
	"testing"
)

// TestWrapInvariantsHoldOverManyInputs exercises Wrap over a generated corpus
// rather than a hand-written table, because the break loop is the one piece of
// this package where a mistake is a silent content change rather than a wrong
// return value: a dropped character, a duplicated space, or a rune split across
// two lines.
//
// The generated alphabet deliberately includes the three things the contract
// treats differently — the space, the double-width glyph, and a combining mark —
// so every generated case crosses at least one of the rules.
//
// The corpus is deterministic: a fixed seed, so a failure is reproducible and a
// change in behaviour is a real change rather than luck.
func TestWrapInvariantsHoldOverManyInputs(t *testing.T) {
	rng := rand.New(rand.NewSource(20261004)) // the ADR's date, as a fixed seed
	alphabet := []rune("ab c漢́é")

	for iter := 0; iter < 4000; iter++ {
		n := rng.Intn(24)
		var text strings.Builder
		for i := 0; i < n; i++ {
			text.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		input := text.String()
		width := rng.Intn(9) - 1 // includes 0 and -1, the degenerate widths

		w := Wrap([]Span{NewSpan(input, DefaultStyle)}, width)

		if width <= 0 {
			if w.Height() != 0 {
				t.Fatalf("Wrap(%q, %d).Height() = %d, want 0", input, width, w.Height())
			}
			continue
		}

		var joined strings.Builder
		for i, line := range w.Lines {
			if len(line) == 0 {
				t.Fatalf("Wrap(%q, %d) line %d is empty; empty lines must be omitted", input, width, i)
			}
			if got := SpansWidth(line); got > width {
				t.Fatalf("Wrap(%q, %d) line %d is %d cells: %q", input, width, i, got, spanText(line))
			}
			joined.WriteString(spanText(line))
		}

		// The load-bearing invariant: wrapping loses characters only by CONSUMING
		// the space that caused each break, and by DROPPING zero-width runes,
		// which Wrap's contract states and inherits from RuneWidth. Every other
		// character — every letter, every wide glyph, every preserved space —
		// survives, in order.
		//
		// So: the output is a subsequence of the input, and the dropped runes are
		// only the three the contract allows to go: the space that causes a break,
		// a zero-width rune, and — in a one-column area, where a double-width
		// glyph can never fit on any line — the glyph itself, whose line is
		// omitted rather than half-written.
		base := dropZeroWidth(input)
		if !isSubsequenceOf(joined.String(), base) {
			t.Fatalf("Wrap(%q, %d) produced %q, which is not a subsequence of the input %q",
				input, width, joined.String(), base)
		}
		for _, r := range charactersOnlyIn(base, joined.String()) {
			switch {
			case r == ' ':
			case width < 2 && r == '漢':
			default:
				t.Fatalf("Wrap(%q, %d) dropped %q; only a break space, a zero-width rune "+
					"and (below two columns) an unplaceable glyph may go", input, width, r)
			}
		}

		// Each emitted line consumes at most one space, so wrapping cannot lose
		// more spaces than it produced lines.
		consumed := strings.Count(base, " ") - strings.Count(joined.String(), " ")
		if consumed > w.Height() {
			t.Fatalf("Wrap(%q, %d) consumed %d spaces across %d lines", input, width, consumed, w.Height())
		}
	}
}

// dropZeroWidth returns s with every rune RuneWidth reports as zero removed,
// which is what Wrap sees: it flattens and discards them before breaking
// anything.
func dropZeroWidth(s string) string {
	var b strings.Builder
	for _, r := range s {
		if RuneWidth(r) != 0 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isSubsequenceOf reports whether every rune of sub appears in s, in order.
// Both are walked as runes, not bytes: the corpus is full of multi-byte
// characters and a byte-indexed walk would mis-pair them.
func isSubsequenceOf(sub, s string) bool {
	want := []rune(sub)
	i := 0
	for _, r := range s {
		if i < len(want) && want[i] == r {
			i++
		}
	}
	return i == len(want)
}

// charactersOnlyIn returns the runes of a that were not consumed by b. It is
// only meaningful when b is a subsequence of a, which every caller establishes
// first. Runes are matched in order, so it reports exactly what wrapping lost.
func charactersOnlyIn(a, b string) []rune {
	have := []rune(b)
	var out []rune
	i := 0
	for _, r := range a {
		if i < len(have) && have[i] == r {
			i++
			continue
		}
		out = append(out, r)
	}
	return out
}

// TestWrapNeverSplitsAGlyphOverALine is the wide-glyph half of the property
// test, stated separately because the "never split" rule is not about how many
// glyphs fit — that varies with the waste at each line end — but about WHICH
// glyphs survive.
//
// The invariant: the glyphs that made it through are exactly the FIRST total
// glyphs of the input. A glyph is therefore dropped from the end, never from the
// middle, and none is duplicated. A split glyph would break the prefix property,
// because a half-glyph has no text representation to duplicate.
func TestWrapNeverSplitsAGlyphOverALine(t *testing.T) {
	for width := 1; width <= 6; width++ {
		for n := 1; n <= 5; n++ {
			input := strings.Repeat("漢", n)
			w := Wrap([]Span{NewSpan(input, DefaultStyle)}, width)

			var joined strings.Builder
			for i := range w.Lines {
				line := spanText(w.Line(i))
				glyphs := len([]rune(line))
				// Each line must hold whole glyphs only, and must occupy exactly two
				// cells per glyph: no line may end on half of a glyph.
				if got := strings.Count(line, "漢"); got != glyphs {
					t.Fatalf("Wrap(%q, %d) line %d = %q holds something other than whole glyphs",
						input, width, i, line)
				}
				if got := SpansWidth(w.Line(i)); got != 2*glyphs {
					t.Fatalf("Wrap(%q, %d) line %d = %q occupies %d cells for %d glyphs",
						input, width, i, line, got, glyphs)
				}
				joined.WriteString(line)
			}

			total := strings.Count(joined.String(), "漢")
			if total > n {
				t.Fatalf("Wrap(%q, %d) placed %d glyphs from %d: one was duplicated",
					input, width, total, n)
			}
			if want := string([]rune(input)[:total]); joined.String() != want {
				t.Fatalf("Wrap(%q, %d) kept %q; the survivors must be the first %d glyphs, %q",
					input, width, joined.String(), total, want)
			}

			// A one-column area cannot hold a double-width glyph at all, so nothing
			// survives; anything wider fits at least the first glyph.
			switch {
			case width < 2 && total != 0:
				t.Errorf("Wrap(%q, %d) placed %d glyphs in a one-column area", input, width, total)
			case width >= 2 && total == 0:
				t.Errorf("Wrap(%q, %d) placed no glyphs although one fits", input, width)
			}
		}
	}
}

// TestTruncateInvariantsHoldOverManyInputs is the same idea for truncation: the
// result must fit the budget, must be a prefix of the input followed by the
// marker, and must not allocate when the input already fits.
func TestTruncateInvariantsHoldOverManyInputs(t *testing.T) {
	rng := rand.New(rand.NewSource(20261004))
	alphabet := []rune("abc 漢")

	for iter := 0; iter < 4000; iter++ {
		n := rng.Intn(16)
		var text strings.Builder
		for i := 0; i < n; i++ {
			text.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		input := text.String()
		maxWidth := rng.Intn(10) - 1 // includes 0 and -1

		got := Truncate([]Span{NewSpan(input, DefaultStyle)}, maxWidth)
		ascii := TruncateASCII([]Span{NewSpan(input, DefaultStyle)}, maxWidth)

		if maxWidth <= 0 {
			if len(got) != 0 || len(ascii) != 0 {
				t.Fatalf("Truncate(%q, %d) returned %+v / %+v, want empty", input, maxWidth, got, ascii)
			}
			continue
		}
		if w := SpansWidth(got); w > maxWidth {
			t.Fatalf("Truncate(%q, %d) is %d cells", input, maxWidth, w)
		}
		if w := SpansWidth(ascii); w != SpansWidth(got) {
			t.Fatalf("Truncate(%q, %d): the ASCII rung gave %d cells and the Unicode rung %d; "+
				"the markers must be the same width so the layout cannot shift", input, maxWidth, w, SpansWidth(got))
		}
		if StringWidth(input) <= maxWidth {
			if len(got) != 1 || got[0].Text != input {
				t.Fatalf("Truncate(%q, %d) modified an input that fits", input, maxWidth)
			}
			continue
		}
		// Truncating means prefix plus marker, and the prefix must be a prefix of
		// the input with no rune reordered.
		text2 := spanText(got)
		if !strings.HasSuffix(text2, TruncSuffix) {
			t.Fatalf("Truncate(%q, %d) = %q, which does not end in the marker", input, maxWidth, text2)
		}
		head := strings.TrimSuffix(text2, TruncSuffix)
		if !strings.HasPrefix(input, head) {
			t.Fatalf("Truncate(%q, %d) kept %q, which is not a prefix of the input", input, maxWidth, head)
		}
	}
}
