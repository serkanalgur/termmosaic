package buffer

import "testing"

// This file pins the frame-path cost of the degradation ladder. CI has a
// zero-allocation gate on the diff itself (internal/diff's
// TestDiffIsZeroAllocation, run at truecolor where no quantisation happens);
// these tests are the same claim for the quantiser the diff consults at the
// 256 and 16 rungs. A quantiser that allocated per call would put something on
// the heap for every style change in every frame.

// benchQuantiserColours is a representative sample: the colours the shipped
// examples paint, exact palette members (the fixed-point path), and
// off-palette colours (the search path).
var benchQuantiserColours = []Colour{
	// examples/hello
	NewColour(0x10, 0x14, 0x1c), NewColour(0xd8, 0xdc, 0xe4),
	NewColour(0x30, 0xc0, 0x80), NewColour(0x60, 0x6a, 0x7a),
	NewColour(0x30, 0x36, 0x40),
	// examples/search / examples/markets theme
	NewColour(0x0e, 0x11, 0x16), NewColour(0x14, 0x19, 0x21),
	NewColour(0x6a, 0x74, 0x82), NewColour(0x2c, 0x33, 0x3d),
	NewColour(0xd8, 0xa8, 0x40), NewColour(0x3c, 0xc8, 0x8c),
	NewColour(0xd8, 0x6a, 0x62), NewColour(0x8a, 0x93, 0xa0),
	NewColour(0x54, 0xa8, 0xf0),
	// exact palette members
	NewColour(0x00, 0x00, 0x00), NewColour(0x80, 0x00, 0x00),
	NewColour(0xff, 0xff, 0xff), NewColour(0x80, 0x80, 0x80),
	// off-palette
	NewColour(0x9b, 0x32, 0x28), NewColour(0x32, 0x73, 0x2d),
	NewColour(0xe6, 0x55, 0x3c), NewColour(0x74, 0x73, 0x34),
	NewColour(0xff, 0x88, 0x00), NewColour(0x0a, 0x08, 0x90),
}

// benchmarkQuantiserSink defeats dead-code elimination without allocating.
var benchmarkQuantiserSink uint8

// BenchmarkQuantiserNearest256 measures the steady-state frame path at the
// 256 rung: colours already seen, exactly as a frame repaints them.
func BenchmarkQuantiserNearest256(b *testing.B) {
	q := DefaultQuantiser{}
	for _, c := range benchQuantiserColours { // warm the memo/table
		q.Nearest256(c)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkQuantiserSink = q.Nearest256(benchQuantiserColours[i%len(benchQuantiserColours)])
	}
}

// BenchmarkQuantiserNearest16 is the 16-rung counterpart.
func BenchmarkQuantiserNearest16(b *testing.B) {
	q := DefaultQuantiser{}
	for _, c := range benchQuantiserColours {
		q.Nearest16(c)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkQuantiserSink = q.Nearest16(benchQuantiserColours[i%len(benchQuantiserColours)])
	}
}

// TestQuantiserSteadyStateIsAllocationFree is the assertion form of the same
// claim: after first use, consulting the quantiser for every colour a frame
// repaints must not allocate. testing.AllocsPerRun performs a warm-up run
// before measuring, so lazy population happens outside the measured window —
// which is exactly the steady-state contract.
func TestQuantiserSteadyStateIsAllocationFree(t *testing.T) {
	q := DefaultQuantiser{}
	if got := testing.AllocsPerRun(100, func() {
		for _, c := range benchQuantiserColours {
			q.Nearest16(c)
			q.Nearest256(c)
		}
	}); got != 0 {
		t.Errorf("steady-state quantisation allocated %.1f objects per run, want 0", got)
	}
}
