package input

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
)

// TestDecodeIsZeroAllocation is the one performance claim ADR 0005 makes, pinned
// as a failing test rather than asserted in a comment.
//
// ADR 0002 set the same bar for the diff and the reasoning is identical: the
// input path is on the latency path, and a TUI that allocates per keystroke is a
// TUI whose input latency is governed by the garbage collector rather than by
// how much work the decoder does. Every event kind here is pinned at zero, which
// means the fixed-size CSI parameter storage, the Alt-chord recursion and the
// mouse decode all have to stay allocation-free — not just the trivial one-byte
// case.
//
// A paste is deliberately absent from this table, and the omission is not an
// oversight: EventPaste carries its payload as a string, so producing one copies
// it. TestPasteAllocatesOncePerPaste pins that it is once per paste and not once
// per character, which is the part that actually matters.
func TestDecodeIsZeroAllocation(t *testing.T) {
	cases := []struct {
		name string
		seq  string
	}{
		{"key", "a"},
		{"key with a modifier", "\x1b[1;5A"},
		{"key on the tilde path", "\x1b[3~"},
		{"key on the SS3 path", "\x1bOP"},
		{"key on the kitty path", "\x1b[97;5u"},
		{"Alt-chord", "\x1ba"},
		{"multi-byte rune", "é"},
		{"mouse SGR", "\x1b[<0;12;34M"},
		{"mouse urxvt", "\x1b[<0;12;34M"},
		{"mouse X10", "\x1b[M\x20\x21\x22"},
		{"focus in", "\x1b[I"},
		{"focus out", "\x1b[O"},
		{"terminal reply", "\x1b]0;a title\x07"},
		{"runaway", "\x1b[" + strings.Repeat("0", 100)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seq := []byte(tc.seq)
			cfg := Config{}
			// Warm the path so a one-off does not count against the run.
			for i := 0; i < 8; i++ {
				Decode(seq, cfg)
			}
			var sink int
			allocs := testing.AllocsPerRun(200, func() {
				_, n, _ := Decode(seq, cfg)
				sink += n
			})
			if allocs != 0 {
				t.Errorf("Decode(%q) allocated %v times per run, want 0", tc.seq, allocs)
			}
			_ = sink
		})
	}
}

// TestDecodeKeyBurstIsZeroAllocation is the same bar over a realistic burst, not
// just a single sequence: a fast typist or a held key produces many sequences in
// one buffer, and a per-call allocation would show up here even if it did not
// show up per sequence.
func TestDecodeKeyBurstIsZeroAllocation(t *testing.T) {
	var burst []byte
	burst = append(burst, "\x1b[1;5A"...)
	burst = append(burst, "hello"...)
	burst = append(burst, "\x1b[3~"...)
	burst = append(burst, "é"...)
	burst = append(burst, "\x1b[<0;1;1M"...)
	burst = append(burst, "\x1bOP"...)

	cfg := Config{}
	dst := make([]termmosaic.Event, 0, len(burst))
	for i := 0; i < 8; i++ {
		p := NewParser(cfg)
		p.Feed(dst[:0], burst)
	}
	allocs := testing.AllocsPerRun(200, func() {
		// A fresh parser each run, because a warm parser is exactly what the
		// steady state looks like and a fresh one is exactly what the first
		// keystroke after a terminal is opened looks like.
		p := NewParser(cfg)
		p.Feed(dst[:0], burst)
	})
	// Three, and the three are named: the Parser itself, the 64-byte
	// partial-sequence slot and the paste accumulator. That is a fixed cost per
	// Parser, paid once when an application starts, and not a per-keystroke one.
	// TestParserAllocatesNothingPerKeystroke is the version that shows Feed alone
	// is free; a fourth allocation here would mean the burst is growing a buffer
	// it should already have had.
	if allocs != 3 {
		t.Errorf("a %d-byte burst through NewParser+Feed allocated %v times per run, want exactly 3 (the parser and its two buffers)", len(burst), allocs)
	}
}

// TestPasteAllocatesOncePerPaste documents the one allocation ADR 0005 allows and
// pins the shape of it: once per paste, not once per character and not once per
// Feed chunk.
//
// This is the number that matters for a 10,000-character paste. If it ever became
// proportional to the payload size, a large paste would trigger a garbage
// collection in the middle of the frame that is about to render the result.
func TestPasteAllocatesOncePerPaste(t *testing.T) {
	seq := []byte("\x1b[200~" + strings.Repeat("x", 10_000) + "\x1b[201~")
	cfg := Config{}
	p := NewParser(cfg)
	dst := make([]termmosaic.Event, 0, 64)

	// Warm, because the first paste grows the accumulator and that growth is a
	// real one-off cost rather than the per-paste cost under test.
	p.Feed(dst[:0], seq)
	allocs := testing.AllocsPerRun(50, func() {
		p.Feed(dst[:0], seq)
	})
	if allocs > 1 {
		t.Errorf("a 10,000-character paste allocated %v times, want at most 1 (the payload string)", allocs)
	}
	if got := p.Stats().PastesTruncated; got != 0 {
		t.Errorf("Stats.PastesTruncated = %d, want 0", got)
	}
}

// TestParserAllocatesNothingPerKeystroke is the resolver's-eye view of the same
// bar: after the parser has been used once, decoding keys costs nothing at all,
// including the partial-sequence path a split read takes.
func TestParserAllocatesNothingPerKeystroke(t *testing.T) {
	cfg := Config{}
	p := NewParser(cfg)
	dst := make([]termmosaic.Event, 0, 64)

	// Warm the partial buffer to its working size.
	p.Feed(dst[:0], []byte("\x1b[1;5"))
	p.Feed(dst[:0], []byte("A"))

	allocs := testing.AllocsPerRun(200, func() {
		p.Feed(dst[:0], []byte("\x1b[1;5"))
		p.Feed(dst[:0], []byte("A"))
		p.Feed(dst[:0], []byte("\x1b[<0;1;1M"))
		p.Feed(dst[:0], []byte("\x1b[I"))
	})
	if allocs != 0 {
		t.Errorf("the parser allocated %v times for four keystrokes, want 0", allocs)
	}
}

// BenchmarkDecodeKey measures the hot path so the zero-allocation claim has a
// number next to it. It is a benchmark rather than a measurement ADR 0005 relies
// on: input decoding is I/O-bound in practice, and a keystroke costs a read(2)
// measured in microseconds against a 16 ms frame budget.
func BenchmarkDecodeKey(b *testing.B) {
	seq := []byte("\x1b[1;5A")
	cfg := Config{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Decode(seq, cfg)
	}
}

// BenchmarkDecodeRune measures the most common possible input.
func BenchmarkDecodeRune(b *testing.B) {
	seq := []byte("a")
	cfg := Config{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Decode(seq, cfg)
	}
}

// BenchmarkDecodeMouse measures a mouse report, which is the largest thing the
// key path has to decode.
func BenchmarkDecodeMouse(b *testing.B) {
	seq := []byte("\x1b[<0;120;40M")
	cfg := Config{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Decode(seq, cfg)
	}
}

// BenchmarkParserFeedKey measures the resumable driver's cost for one keystroke.
func BenchmarkParserFeedKey(b *testing.B) {
	cfg := Config{}
	p := NewParser(cfg)
	dst := make([]termmosaic.Event, 0, 64)
	seq := []byte("a")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p.Feed(dst[:0], seq)
	}
}
